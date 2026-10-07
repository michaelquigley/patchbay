package scarlett

import (
	"github.com/michaelquigley/scarlettctl"
	"github.com/michaelquigley/sessionmixer/topology"
	_ "github.com/michaelquigley/sessionmixer/topology/profiles" // registers the known device profiles
	"github.com/pkg/errors"
)

// alsaHardware is the machine's cards, through scarlettctl, with SessionMixer's topology for their profiles.
type alsaHardware struct{}

func (alsaHardware) open(number int) (session, error) {
	c, err := scarlettctl.OpenCard(number)
	if err != nil {
		return nil, err
	}
	// the monitor exists before the session is handed out, so a stop that arrives before the watch starts still
	// reaches it: Watch then returns at once.
	return &alsaSession{card: c, monitor: c.NewEventMonitor()}, nil
}

type alsaSession struct {
	card    *scarlettctl.Card
	monitor *scarlettctl.EventMonitor
}

func (s *alsaSession) isScarlett() bool {
	return s.card.IsScarlett()
}

// detect builds the device from the detected profile and keys its pcm capture endpoints by their port's one-based
// number, pcm-capture-N. the builder leaves out a channel whose control is missing or whose first read failed, so the
// slice's position is not the channel; the port number is.
func (s *alsaSession) detect() (map[int]endpoint, int, error) {
	profile, err := topology.DetectProfile(s.card)
	if err != nil {
		return nil, 0, err
	}
	device, err := topology.NewDeviceBuilder(s.card, profile).Build()
	if err != nil {
		return nil, 0, errors.Wrap(err, "building the topology")
	}
	out := map[int]endpoint{}
	for _, e := range device.PCMCaptureEndpoints {
		if e.Port != nil && e.RoutingControl != nil {
			out[e.Port.Number] = alsaEndpoint{e}
		}
	}
	return out, profile.PCMCaptureCount(), nil
}

// watch uses EventMonitor.Watch, not WatchControls, which skips a control it cannot read and keeps running.
func (s *alsaSession) watch(onEvent func()) error {
	return s.monitor.Watch(func(uint) error {
		onEvent()
		return nil
	})
}

// stop is called once, under the card's lock, possibly before watch has started.
func (s *alsaSession) stop() {
	s.monitor.Stop()
}

func (s *alsaSession) close() {
	s.card.Close()
}

type alsaEndpoint struct{ e *topology.RoutingEndpoint }

func (a alsaEndpoint) refresh() error {
	return a.e.RefreshFromHardware()
}

// source names the routed source, or reports false for an index outside the control's items rather than taking
// GetCurrentSource's "Unknown" as a source.
func (a alsaEndpoint) source() (string, bool) {
	i := a.e.GetCurrentSourceIndex()
	if i < 0 || i >= len(a.e.AvailableSources) {
		return "", false
	}
	return a.e.AvailableSources[i], true
}
