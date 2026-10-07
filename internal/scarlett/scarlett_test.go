package scarlett

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/michaelquigley/patchbay/internal/pipewire"
)

type fakeEndpoint struct {
	mu    sync.Mutex
	src   string
	err   error
	reads int
}

func (e *fakeEndpoint) refresh() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reads++
	return e.err
}

func (e *fakeEndpoint) source() (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.src, true
}

func (e *fakeEndpoint) set(src string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.src, e.err = src, err
}

func (e *fakeEndpoint) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.reads
}

// fakeSession is one opened card: events and a monitor error are fed by the test.
type fakeSession struct {
	scarlett  bool
	detectErr error
	count     int
	endpoints map[int]endpoint
	events    chan struct{}
	fail      chan error
	stopped   chan struct{}
	stopOnce  sync.Once
	closed    chan struct{}
}

func newSession(count int, endpoints map[int]endpoint) *fakeSession {
	return &fakeSession{scarlett: true, count: count, endpoints: endpoints, events: make(chan struct{}),
		fail: make(chan error), stopped: make(chan struct{}), closed: make(chan struct{})}
}

func (s *fakeSession) isScarlett() bool { return s.scarlett }

func (s *fakeSession) detect() (map[int]endpoint, int, error) {
	return s.endpoints, s.count, s.detectErr
}

func (s *fakeSession) watch(onEvent func()) error {
	for {
		select {
		case <-s.events:
			onEvent()
		case err := <-s.fail:
			return err
		case <-s.stopped:
			return nil
		}
	}
}

func (s *fakeSession) stop()  { s.stopOnce.Do(func() { close(s.stopped) }) }
func (s *fakeSession) close() { close(s.closed) }

// fakeHardware hands out sessions per card number, in order; a card with none left fails to open.
type fakeHardware struct {
	mu       sync.Mutex
	sessions map[int][]*fakeSession
}

func (h *fakeHardware) open(n int) (session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.sessions[n]) == 0 {
		return nil, errors.New("no such card")
	}
	s := h.sessions[n][0]
	h.sessions[n] = h.sessions[n][1:]
	return s, nil
}

const hardwareSerial = "Focusrite_Scarlett_16i16_4th_Gen_SCARLETT16I16"

// graph is a live snapshot: the Scarlett's source node on card 1 under the given device object, a webcam on card 2,
// and a built-in card that reports no hardware serial.
func graph(deviceSerial pipewire.Serial, ports ...pipewire.Port) *pipewire.Snapshot {
	s := &pipewire.Snapshot{State: pipewire.Live,
		Devices: map[pipewire.Serial]pipewire.Device{
			deviceSerial: {Serial: deviceSerial, HardwareSerial: hardwareSerial, ALSACard: 1, HasALSACard: true},
			20:           {Serial: 20, HardwareSerial: "046d_HD_Pro_Webcam_C920", ALSACard: 2, HasALSACard: true},
			30:           {Serial: 30, ALSACard: 0, HasALSACard: true}, // a built-in card reports no serial
		},
		Nodes: map[pipewire.Serial]pipewire.Node{
			100: {Serial: 100, DeviceSerial: deviceSerial},
			200: {Serial: 200, DeviceSerial: 20},
			300: {Serial: 300, DeviceSerial: 30},
		},
	}
	return s
}

func capture(n string) pipewire.Port {
	return pipewire.Port{NodeSerial: 100, Name: "capture_AUX" + n, Path: "alsa:pcm:1:hw:1:capture:capture_" + n}
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if f() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func expect(t *testing.T, a *Adapter, p pipewire.Port, want string, ok bool) {
	t.Helper()
	eventually(t, want, func() bool {
		got, gotOK := a.SourceFor(p)
		return got == want && gotOK == ok
	})
}

func TestCaptureChannel(t *testing.T) {
	for path, want := range map[string]int{
		"alsa:pcm:1:hw:1:capture:capture_3":   3,
		"alsa:pcm:1:hw:1,0:capture:capture_0": 0,
		"alsa:pcm:1:hw:1:capture:capture_17":  17,
	} {
		if n, ok := captureChannel(path, 1); !ok || n != want {
			t.Errorf("%q = %d, %v; want %d", path, n, ok, want)
		}
	}
	for _, path := range []string{
		"", "alsa:pcm:1:hw:1:playback:playback_3", "alsa:pcm:1:hw:1:playback:monitor_3", "alsa:pcm:2:hw:2:capture:capture_3",
		"alsa:pcm:1:hw:1,1:capture:capture_3", "alsa:pcm:1:hw:1:capture:capture_03", "alsa:pcm:1:hw:1:capture:capture_AUX3",
		"alsa:pcm:1:hw:1:capture:capture_-1", "v4l2:/dev/video0",
	} {
		if n, ok := captureChannel(path, 1); ok {
			t.Errorf("%q = %d, want no channel", path, n)
		}
	}
}

// the channel comes from object.path, and channel N is pcm-capture-(N+1): a port whose name says AUX9 but whose
// path says capture_3 is pcm-capture-4.
func TestChannelMapsToEndpoint(t *testing.T) {
	in1, mixA := &fakeEndpoint{src: "Analogue Input 1"}, &fakeEndpoint{src: "Mix A"}
	hw := &fakeHardware{sessions: map[int][]*fakeSession{1: {newSession(18, map[int]endpoint{1: in1, 4: mixA})}}}
	a := newAdapter(hw)
	a.Sync(graph(10))
	defer a.Close()

	expect(t, a, capture("0"), "Analogue Input 1", true)
	misnamed := capture("3")
	misnamed.Name = "capture_AUX9"
	expect(t, a, misnamed, "Mix A", true)
	expect(t, a, capture("1"), reasonNoControl, false)
	expect(t, a, capture("20"), reasonRange, false)
}

// one failed read makes every channel unavailable, even with the monitor running; the next successful read, on the
// next event, restores them. each event reads every endpoint.
func TestRefreshFailureAndRecovery(t *testing.T) {
	in1, in2 := &fakeEndpoint{src: "Analogue Input 1"}, &fakeEndpoint{src: "Analogue Input 2"}
	s := newSession(18, map[int]endpoint{1: in1, 2: in2})
	a := newAdapter(&fakeHardware{sessions: map[int][]*fakeSession{1: {s}}})
	a.Sync(graph(10))
	defer a.Close()
	expect(t, a, capture("0"), "Analogue Input 1", true)

	in2.set("Analogue Input 2", errors.New("read failed"))
	before := in1.count()
	s.events <- struct{}{}
	expect(t, a, capture("0"), reasonFailed, false)
	expect(t, a, capture("1"), reasonFailed, false)
	if in1.count() != before+1 {
		t.Errorf("an event read the healthy endpoint %d times, want once", in1.count()-before)
	}

	in2.set("Mix B", nil)
	s.events <- struct{}{}
	expect(t, a, capture("0"), "Analogue Input 1", true)
	expect(t, a, capture("1"), "Mix B", true)
}

// a monitor that stops with an error leaves every channel unavailable.
func TestMonitorErrorInvalidates(t *testing.T) {
	s := newSession(18, map[int]endpoint{1: &fakeEndpoint{src: "Analogue Input 1"}})
	a := newAdapter(&fakeHardware{sessions: map[int][]*fakeSession{1: {s}}})
	a.Sync(graph(10))
	defer a.Close()
	expect(t, a, capture("0"), "Analogue Input 1", true)
	s.fail <- errors.New("poll failed")
	expect(t, a, capture("0"), reasonMonitor, false)
}

// unplugged, the card is closed and its device unannotated; replugged, its annotations return only after a successful
// read on the new card.
func TestReplug(t *testing.T) {
	first := newSession(18, map[int]endpoint{1: &fakeEndpoint{src: "Analogue Input 1"}})
	ep := &fakeEndpoint{src: "Analogue Input 1", err: errors.New("not ready")}
	second := newSession(18, map[int]endpoint{1: ep})
	a := newAdapter(&fakeHardware{sessions: map[int][]*fakeSession{1: {first, second}}})
	a.Sync(graph(10))
	defer a.Close()
	expect(t, a, capture("0"), "Analogue Input 1", true)

	gone := graph(10)
	delete(gone.Devices, 10)
	delete(gone.Nodes, 100)
	a.Sync(gone)
	expect(t, a, capture("0"), "", false)
	eventually(t, "the first card to close", closed(first))

	a.Sync(graph(11)) // a new device object for the same hardware and card
	expect(t, a, pipewire.Port{NodeSerial: 100, Path: "alsa:pcm:1:hw:1:capture:capture_0"}, reasonFailed, false)
	ep.set("Analogue Input 1", nil)
	second.events <- struct{}{}
	expect(t, a, capture("0"), "Analogue Input 1", true)
}

// the reasons an annotation is unavailable, and the devices that are simply not annotated.
func TestUnavailableReasons(t *testing.T) {
	noProfile := newSession(0, nil)
	noProfile.detectErr = errors.New("incompatible firmware")
	webcam := newSession(0, nil)
	webcam.scarlett = false
	a := newAdapter(&fakeHardware{sessions: map[int][]*fakeSession{1: {noProfile}, 2: {webcam}}})
	a.Sync(graph(10))
	defer a.Close()

	expect(t, a, capture("0"), reasonNoProfile, false)
	expect(t, a, pipewire.Port{NodeSerial: 200, Path: "alsa:pcm:2:hw:2:capture:capture_0"}, "", false)
	expect(t, a, pipewire.Port{NodeSerial: 300, Path: "alsa:pcm:0:hw:0:capture:capture_0"}, "", false)

	b := newAdapter(&fakeHardware{sessions: map[int][]*fakeSession{
		1: {newSession(18, map[int]endpoint{1: &fakeEndpoint{src: "Analogue Input 1"}})}}})
	b.Sync(graph(10))
	defer b.Close()
	expect(t, b, pipewire.Port{NodeSerial: 100}, reasonNoPath, false)
	expect(t, b, pipewire.Port{NodeSerial: 100, Path: "alsa:pcm:1:hw:1:playback:playback_0"}, reasonNoPath, false)

	c := newAdapter(&fakeHardware{})
	c.Sync(graph(10))
	defer c.Close()
	expect(t, c, capture("0"), reasonNoCard, false)

	// a snapshot that is not live annotates nothing and closes the cards.
	down := graph(10)
	down.State = pipewire.Disconnected
	b.Sync(down)
	expect(t, b, capture("0"), "", false)
}

func closed(s *fakeSession) func() bool {
	return func() bool {
		select {
		case <-s.closed:
			return true
		default:
			return false
		}
	}
}

// a new device object for the same hardware and card, with no snapshot showing the device missing in between, is a
// new card: the old one closes and nothing is annotated until the new one reads successfully. a new connection is the
// same, even under the same device serial.
func TestNewDeviceObjectReopens(t *testing.T) {
	first := newSession(18, map[int]endpoint{1: &fakeEndpoint{src: "Analogue Input 1"}})
	ep := &fakeEndpoint{src: "Analogue Input 2", err: errors.New("not ready")}
	second := newSession(18, map[int]endpoint{1: ep})
	ep3 := &fakeEndpoint{src: "Analogue Input 3", err: errors.New("not ready")}
	third := newSession(18, map[int]endpoint{1: ep3})
	a := newAdapter(&fakeHardware{sessions: map[int][]*fakeSession{1: {first, second, third}}})
	defer a.Close()

	a.Sync(graph(10))
	expect(t, a, capture("0"), "Analogue Input 1", true)

	a.Sync(graph(11))
	eventually(t, "the first card to close", closed(first))
	expect(t, a, capture("0"), reasonFailed, false)
	ep.set("Analogue Input 2", nil)
	second.events <- struct{}{}
	expect(t, a, capture("0"), "Analogue Input 2", true)

	reconnected := graph(11)
	reconnected.Session = 2
	a.Sync(reconnected)
	eventually(t, "the second card to close", closed(second))
	expect(t, a, capture("0"), reasonFailed, false)
	ep3.set("Analogue Input 3", nil)
	third.events <- struct{}{}
	expect(t, a, capture("0"), "Analogue Input 3", true)
}
