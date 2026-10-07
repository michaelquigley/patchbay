// Package scarlett annotates a Scarlett's PCM capture ports with the hardware source the interface routes to each,
// read through scarlettctl and SessionMixer's topology. it is read-only and conditional: a port is annotated only when
// its channel is established from its alsa path, its device's card is open with a detected topology profile, the
// card's event monitor is running, and the channel's endpoint has a fresh successful read. nothing here writes a
// Scarlett control.
package scarlett

import (
	"strconv"
	"strings"
	"sync"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// Annotator is what the inspector asks. SourceFor returns the hardware source routed to a port's PCM capture channel
// and true; or false with the reason it is unavailable; or false with an empty reason for a port on a device with no
// Scarlett card, which is simply not annotated.
type Annotator interface {
	SourceFor(port pipewire.Port) (string, bool)
}

// reasons a port's annotation is unavailable, as the inspector shows them.
const (
	reasonNoPath    = "no PCM capture path"
	reasonNoCard    = "no card for this device"
	reasonNoProfile = "no topology profile for this firmware"
	reasonRange     = "channel outside the profile's PCM capture count"
	reasonNoControl = "no routing control for this channel"
	reasonMonitor   = "monitor not running"
	reasonFailed    = "last read failed"
	reasonUnread    = "not yet read"
)

// hardware opens cards; the real one is scarlettctl and topology (alsa.go), the tests' is a fake.
type hardware interface {
	open(card int) (session, error)
}

// session is one open card. every call on it is made from the card's own goroutine, which owns the alsa handle.
type session interface {
	isScarlett() bool
	// detect detects the topology profile and builds the capture endpoints, keyed by one-based channel.
	detect() (map[int]endpoint, int, error)
	// watch blocks, calling onEvent for every control change, until stop or an error.
	watch(onEvent func()) error
	stop()
	close()
}

// endpoint is one PCM capture channel's routing control.
type endpoint interface {
	refresh() error
	source() (string, bool)
}

// cardKey is one card's lifetime: its hardware and card number, under one PipeWire device object in one connection.
// a new device object or a new connection is a new card, opened afresh and annotated only after its own first read,
// even when no snapshot showed the device missing in between.
type cardKey struct {
	hardware string
	number   int
	device   pipewire.Serial
	session  uint64
}

// Adapter opens a card for each device in the live snapshot that has a hardware serial and an alsa card, and closes
// it when the device leaves. it is driven from the ui's frame loop; each card's reads happen on its own goroutine.
type Adapter struct {
	hw    hardware
	snap  *pipewire.Snapshot
	cards map[cardKey]*card
}

// New returns an adapter over the machine's alsa cards.
func New() *Adapter {
	return newAdapter(alsaHardware{})
}

func newAdapter(hw hardware) *Adapter {
	return &Adapter{hw: hw, cards: map[cardKey]*card{}}
}

// Sync opens cards for devices that appeared and closes those whose devices left. a snapshot that is not live holds
// no device: nothing is annotated while the graph is not current.
func (a *Adapter) Sync(snap *pipewire.Snapshot) {
	want := map[cardKey]bool{}
	if snap != nil && snap.State == pipewire.Live {
		a.snap = snap
		for _, d := range snap.Devices {
			if d.HardwareSerial != "" && d.HasALSACard {
				want[cardKey{d.HardwareSerial, d.ALSACard, d.Serial, snap.Session}] = true
			}
		}
	} else {
		a.snap = nil
	}
	for k, c := range a.cards {
		if !want[k] {
			c.close()
			delete(a.cards, k)
		}
	}
	for k := range want {
		if _, ok := a.cards[k]; !ok {
			c := &card{key: k}
			a.cards[k] = c
			go c.run(a.hw)
		}
	}
}

// Close closes every card.
func (a *Adapter) Close() {
	a.Sync(nil)
}

// SourceFor implements Annotator.
func (a *Adapter) SourceFor(port pipewire.Port) (string, bool) {
	if a.snap == nil {
		return "", false
	}
	d, ok := a.snap.Devices[a.snap.Nodes[port.NodeSerial].DeviceSerial]
	if !ok || d.HardwareSerial == "" || !d.HasALSACard {
		return "", false
	}
	c := a.cards[cardKey{d.HardwareSerial, d.ALSACard, d.Serial, a.snap.Session}]
	if c == nil {
		return "", false
	}
	return c.sourceFor(port.Path)
}

// captureChannel reads a port's zero-based channel on its card's capture pcm from object.path:
// alsa:pcm:<card>:hw:<card>[,0]:capture:capture_<n>. the channel comes from the path alone, never from the AUXn in a
// port's name or alias. only pcm device 0 is accepted: the scarlett2 driver's pcm capture routing controls name the
// channels of that pcm.
func captureChannel(path string, cardNumber int) (int, bool) {
	f := strings.Split(path, ":")
	if len(f) != 7 || f[0] != "alsa" || f[1] != "pcm" || f[3] != "hw" || f[5] != "capture" {
		return 0, false
	}
	card := strconv.Itoa(cardNumber)
	if f[2] != card || (f[4] != card && f[4] != card+",0") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(f[6], "capture_"))
	if err != nil || n < 0 || !strings.HasPrefix(f[6], "capture_") || strconv.Itoa(n) != strings.TrimPrefix(f[6], "capture_") {
		return 0, false
	}
	return n, true
}

// card is one open card and its validity. validity is the adapter's own, per channel: a channel is valid only after a
// successful read and only while the monitor runs; any failed read, a monitor stop or error, or the card closing
// invalidates every channel until the next successful read. an invalid channel's cached value is never read.
type card struct {
	key cardKey

	mu         sync.Mutex
	unrelated  bool   // the card is not a Scarlett: its device is not annotated
	reason     string // a card-wide reason: no card, no profile
	sess       session
	endpoints  map[int]endpoint
	count      int
	monitoring bool
	valid      bool
	failed     bool
	closed     bool
}

func (c *card) run(hw hardware) {
	s, err := hw.open(c.key.number)
	if err != nil {
		dl.Warnf("scarlett: card %d ('%s'): %v", c.key.number, c.key.hardware, err)
		c.set(func() { c.reason = reasonNoCard })
		return
	}
	defer s.close()
	if !s.isScarlett() {
		c.set(func() { c.unrelated = true })
		return
	}
	endpoints, count, err := s.detect()
	if err != nil {
		dl.Warnf("scarlett: card %d ('%s'): %v", c.key.number, c.key.hardware, err)
		c.set(func() { c.reason = reasonNoProfile })
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.sess, c.endpoints, c.count, c.monitoring = s, endpoints, count, true
	c.mu.Unlock()

	// the handle is subscribed to events from open, so a change between this read and the watch is still delivered.
	c.refresh()
	err = s.watch(c.refresh)
	if err != nil {
		dl.Warnf("scarlett: card %d ('%s'): monitor stopped: %v", c.key.number, c.key.hardware, err)
	}
	c.set(func() { c.monitoring, c.valid = false, false })
}

// refresh reads every channel's routing control. the Watch callback names no control (it always reports numid 0), so
// every event refreshes every endpoint.
func (c *card) refresh() {
	c.mu.Lock()
	endpoints := c.endpoints
	c.mu.Unlock()
	ok := true
	for n, e := range endpoints {
		if err := e.refresh(); err != nil {
			dl.Warnf("scarlett: card %d: reading pcm capture %d: %v", c.key.number, n, err)
			ok = false
		}
	}
	c.set(func() { c.valid, c.failed = ok && c.monitoring, !ok })
}

func (c *card) set(f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f()
}

// close invalidates every channel at once and stops the monitor; the card's goroutine closes the handle once the
// watch returns.
func (c *card) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed, c.monitoring, c.valid = true, false, false
	if c.sess != nil {
		c.sess.stop()
	}
}

func (c *card) sourceFor(path string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.unrelated:
		return "", false
	case c.reason != "":
		return c.reason, false
	}
	n, ok := captureChannel(path, c.key.number)
	switch {
	case !ok:
		return reasonNoPath, false
	case c.endpoints == nil:
		return reasonUnread, false // the card is still opening
	case !c.monitoring:
		return reasonMonitor, false
	case n >= c.count:
		return reasonRange, false
	}
	e := c.endpoints[n+1]
	switch {
	case e == nil:
		return reasonNoControl, false
	case c.failed:
		return reasonFailed, false
	case !c.valid:
		return reasonUnread, false
	}
	if s, ok := e.source(); ok {
		return s, true
	}
	return reasonFailed, false
}
