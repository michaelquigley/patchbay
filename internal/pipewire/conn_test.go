package pipewire

import (
	"strconv"
	"testing"
	"time"

	"github.com/pkg/errors"
)

// fakeTransport stands in for libpipewire: each open hands the test a fakeSession whose inputs the test delivers by
// hand, on the test goroutine, one callback at a time.
type fakeTransport struct {
	sessions chan *fakeSession
	failures chan error
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{sessions: make(chan *fakeSession, 4), failures: make(chan error, 4)}
}

func (t *fakeTransport) open(sess *session) (transportSession, error) {
	select {
	case err := <-t.failures:
		return nil, err
	default:
	}
	fs := &fakeSession{sess: sess, bound: map[Serial]bool{}, closed: make(chan struct{})}
	sess.begin(fs)
	t.sessions <- fs
	return fs, nil
}

type fakeSession struct {
	sess    *session
	seq     int
	lastSeq int
	bound   map[Serial]bool
	closed  chan struct{}
}

func (f *fakeSession) bind(_ uint32, _ string, _ uint32, serial Serial) bool {
	f.bound[serial] = true
	return true
}

func (f *fakeSession) unbind(serial Serial) { delete(f.bound, serial) }

func (f *fakeSession) sync() int {
	f.seq++
	f.lastSeq = f.seq
	return f.seq
}

func (f *fakeSession) close() { close(f.closed) }

// callback delivers inputs as one loop dispatch: applied, then flushed by the wake event.
func (f *fakeSession) callback(inputs ...Input) {
	for _, in := range inputs {
		f.sess.apply(in)
	}
	f.sess.flush()
}

func (f *fakeSession) done() {
	f.callback(SyncDone{Seq: f.lastSeq})
}

func nodeGlobal(id uint32, serial Serial, name string) GlobalAdded {
	return GlobalAdded{ID: id, Type: typeNode, Version: 3, Props: map[string]string{
		"object.serial":    strconv.FormatUint(uint64(serial), 10),
		"node.name":        name,
		"application.name": name,
		"media.class":      "Stream/Input/Audio",
	}}
}

func portGlobal(id uint32, serial Serial, nodeID uint32, direction string) GlobalAdded {
	return GlobalAdded{ID: id, Type: typePort, Version: 3, Props: map[string]string{
		"object.serial":  strconv.FormatUint(uint64(serial), 10),
		"node.id":        strconv.FormatUint(uint64(nodeID), 10),
		"port.direction": direction,
		"format.dsp":     "32 bit float mono audio",
	}}
}

func linkGlobal(id uint32, serial Serial, outPort, inPort, outNode, inNode uint32) GlobalAdded {
	return GlobalAdded{ID: id, Type: typeLink, Version: 3, Props: map[string]string{
		"object.serial":    strconv.FormatUint(uint64(serial), 10),
		"link.output.port": strconv.FormatUint(uint64(outPort), 10),
		"link.input.port":  strconv.FormatUint(uint64(inPort), 10),
		"link.output.node": strconv.FormatUint(uint64(outNode), 10),
		"link.input.node":  strconv.FormatUint(uint64(inNode), 10),
	}}
}

func settingsGlobal(id uint32, serial Serial) GlobalAdded {
	return GlobalAdded{ID: id, Type: typeMetadata, Version: 3, Props: map[string]string{
		"object.serial": strconv.FormatUint(uint64(serial), 10),
		"metadata.name": metadataSettings,
	}}
}

func nextSession(t *testing.T, tr *fakeTransport) *fakeSession {
	t.Helper()
	select {
	case fs := <-tr.sessions:
		return fs
	case <-time.After(2 * time.Second):
		t.Fatal("no session opened")
		return nil
	}
}

func drain(b *backend) []Event {
	var out []Event
	for {
		select {
		case e := <-b.events:
			out = append(out, e)
		default:
			return out
		}
	}
}

func waitState(t *testing.T, b *backend, state ConnState) *Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := b.Snapshot(); s.State == state {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("state never reached %v (now %v)", state, b.Snapshot().State)
	return nil
}

// two candidates with the same identity key, each delivered in its own callback before the barrier, are published
// only under connecting; the first live snapshot holds both together.
func TestBarrierHoldsSeparateCallbacks(t *testing.T) {
	tr := newFakeTransport()
	b := startBackend(tr, time.Millisecond, 5*time.Millisecond)
	defer b.Close()
	fs := nextSession(t, tr)

	var seen []*Snapshot
	record := func() {
		if s := b.Snapshot(); len(seen) == 0 || seen[len(seen)-1] != s {
			seen = append(seen, s)
		}
	}
	fs.callback(nodeGlobal(60, 600, "gnome-remote-desktop-daemon"))
	record()
	fs.callback(NodeInfo{Serial: 600, State: "running"})
	record()
	fs.callback(nodeGlobal(61, 601, "gnome-remote-desktop-daemon"))
	record()
	fs.done() // first round trip: enumeration ends; the second sync is issued
	record()
	fs.callback(NodeInfo{Serial: 601, State: "running"})
	record()
	for _, s := range seen {
		if s.State != Connecting {
			t.Fatalf("snapshot %d published %v before the barrier", s.Generation, s.State)
		}
	}
	fs.done()
	live := b.Snapshot()
	if live.State != Live {
		t.Fatalf("state = %v after the barrier", live.State)
	}
	if _, ok := live.Nodes[600]; !ok {
		t.Error("first candidate missing from the first live snapshot")
	}
	if _, ok := live.Nodes[601]; !ok {
		t.Error("second candidate missing from the first live snapshot")
	}
}

// the barrier also waits for the first bound info of every object enumerated before it.
func TestBarrierWaitsForFirstInfo(t *testing.T) {
	tr := newFakeTransport()
	b := startBackend(tr, time.Millisecond, 5*time.Millisecond)
	defer b.Close()
	fs := nextSession(t, tr)

	fs.callback(nodeGlobal(60, 600, "a"), portGlobal(70, 700, 60, "out"))
	fs.done()
	fs.callback(NodeInfo{Serial: 600, State: "idle"})
	fs.done()
	if s := b.Snapshot(); s.State != Connecting {
		t.Fatalf("state = %v with a port still awaiting its first info", s.State)
	}
	fs.callback(PortInfo{Serial: 700, Direction: DirectionOut})
	if s := b.Snapshot(); s.State != Live {
		t.Fatalf("state = %v after every first info arrived", s.State)
	}
}

// a reconnect whose new graph reuses old serial numbers carries over no pending request, provenance entry, or metrics
// record; pending requests fail with a reason when the old connection is lost.
func TestReconnectCarriesNothingOver(t *testing.T) {
	tr := newFakeTransport()
	b := startBackend(tr, time.Millisecond, 5*time.Millisecond)
	defer b.Close()

	first := nextSession(t, tr)
	first.callback(nodeGlobal(60, 50, "REAPER"), portGlobal(70, 51, 60, "in"))
	first.callback(NodeInfo{Serial: 50, State: "running"}, PortInfo{Serial: 51, Direction: DirectionIn})
	first.done()
	first.done()
	waitState(t, b, Live)

	g := first.sess.g
	g.session.pending[7] = &pendingRequest{id: 7}
	g.session.createdHere[50] = struct{}{}
	g.session.metrics[50] = &metricsRecord{total: 3, baseline: 1}
	drain(b)

	first.sess.lost("connection error (broken pipe)")
	dis := b.Snapshot()
	if dis.State != Disconnected || dis.Error == "" {
		t.Fatalf("after loss: state %v, error %q", dis.State, dis.Error)
	}
	if len(g.session.pending)+len(g.session.createdHere)+len(g.session.metrics) != 0 {
		t.Error("session state not cleared on disconnect")
	}
	var failed bool
	for _, e := range drain(b) {
		if r, ok := e.(RequestResolved); ok && r.ID == 7 && !r.OK && r.Reason != "" {
			failed = true
		}
	}
	if !failed {
		t.Error("pending request did not fail on disconnect")
	}
	select {
	case <-first.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("lost session was not closed")
	}

	second := nextSession(t, tr)
	second.callback(nodeGlobal(60, 50, "REAPER"), portGlobal(70, 51, 60, "in"))
	second.callback(NodeInfo{Serial: 50, State: "running"}, PortInfo{Serial: 51, Direction: DirectionIn})
	second.done()
	second.done()
	live := waitState(t, b, Live)
	if _, ok := live.Nodes[50]; !ok {
		t.Fatal("reused serial not observed in the new graph")
	}
	ng := second.sess.g
	if ng == g {
		t.Fatal("reconnect reused the old graph")
	}
	if len(ng.session.pending) != 0 {
		t.Error("pending request carried over")
	}
	if _, ok := ng.session.createdHere[50]; ok {
		t.Error("provenance carried over to a reused serial")
	}
	if _, ok := ng.session.metrics[50]; ok {
		t.Error("metrics record carried over to a reused serial")
	}
}

// a failed connect is published as disconnected with its reason and retried.
func TestConnectFailureRetries(t *testing.T) {
	tr := newFakeTransport()
	tr.failures <- errors.New("cannot connect to pipewire: no such file or directory")
	b := startBackend(tr, time.Millisecond, 5*time.Millisecond)
	defer b.Close()

	fs := nextSession(t, tr)
	var sawFailure bool
	for _, e := range drain(b) {
		if c, ok := e.(ConnStateChanged); ok && c.State == Disconnected && c.Error != "" {
			sawFailure = true
		}
	}
	if !sawFailure {
		t.Error("connect failure was not published as disconnected")
	}
	fs.done()
	fs.done()
	waitState(t, b, Live)
}

func TestIDReuseIsANewObject(t *testing.T) {
	tr := newFakeTransport()
	b := startBackend(tr, time.Millisecond, 5*time.Millisecond)
	defer b.Close()
	fs := nextSession(t, tr)
	fs.callback(nodeGlobal(59, 4485, "REAPER"), NodeInfo{Serial: 4485, State: "running"})
	fs.done()
	fs.done()
	drain(b)

	fs.callback(GlobalRemoved{ID: 59}, nodeGlobal(59, 4591, "REAPER"), NodeInfo{Serial: 4591, State: "running"})
	s := b.Snapshot()
	if _, ok := s.Nodes[4485]; ok {
		t.Error("removed instance still present")
	}
	if n, ok := s.Nodes[4591]; !ok || n.ID != 59 {
		t.Error("new instance under the reused id not observed")
	}
	events := drain(b)
	if len(events) < 2 {
		t.Fatalf("events = %v", events)
	}
	if v, ok := events[0].(ObjectVanished); !ok || v.Serial != 4485 {
		t.Errorf("first event = %#v, want vanished 4485", events[0])
	}
	if a, ok := events[1].(ObjectAppeared); !ok || a.Serial != 4591 {
		t.Errorf("second event = %#v, want appeared 4591", events[1])
	}
	if fs.bound[4485] || !fs.bound[4591] {
		t.Errorf("bindings = %v", fs.bound)
	}
}

// a link's endpoints are resolved to serials when it appears; a port id reused afterwards does not move the link,
// and a reference that missed at announcement stays unresolved and counted even after its id is taken.
func TestLinkEndpointsResolveOnAppearance(t *testing.T) {
	g := newGraph(&replayDriver{})
	g.start()
	g.Apply(nodeGlobal(1, 10, "out"))
	g.Apply(nodeGlobal(2, 20, "in"))
	g.Apply(portGlobal(129, 1290, 1, "out"))
	g.Apply(portGlobal(130, 1300, 2, "in"))
	g.Apply(linkGlobal(140, 1400, 129, 130, 1, 2))
	g.Apply(GlobalRemoved{ID: 129})
	g.Apply(portGlobal(129, 1291, 2, "out"))
	g.Apply(LinkInfo{Serial: 1400, State: "active"})

	// link 1500 names port id 200 before any port holds it; a port then takes id 200.
	g.Apply(linkGlobal(150, 1500, 200, 130, 1, 2))
	g.Apply(portGlobal(200, 2000, 1, "out"))
	g.Apply(LinkInfo{Serial: 1500, State: "active"})

	s := g.fold()
	l := s.Links[1400]
	if l.OutPort != 1290 || l.InPort != 1300 {
		t.Errorf("link endpoints = %d -> %d, want 1290 -> 1300", l.OutPort, l.InPort)
	}
	if l.OutNode != 10 || l.InNode != 20 {
		t.Errorf("link nodes = %d -> %d, want 10 -> 20", l.OutNode, l.InNode)
	}
	if late := s.Links[1500]; late.OutPort != 0 {
		t.Errorf("unresolved endpoint later attached to port %d", late.OutPort)
	}
	if s.Unresolved != 1 {
		t.Errorf("unresolved = %d, want 1", s.Unresolved)
	}
}

func TestSettingsMetadata(t *testing.T) {
	g := newGraph(&replayDriver{})
	g.start()
	g.Apply(settingsGlobal(31, 31))
	g.Apply(MetadataProperty{Serial: 31, Key: "clock.rate", Value: "48000"})
	g.Apply(MetadataProperty{Serial: 31, Key: "clock.force-quantum", Value: "256"})
	g.Apply(MetadataProperty{Serial: 31, Key: "clock.min-quantum", Value: "32"})
	s := g.fold()
	if !s.Settings.Present || s.Settings.Rate != 48000 || s.Settings.ForceQuantum != 256 || s.Settings.MinQuantum != 32 {
		t.Errorf("settings = %+v", s.Settings)
	}
	g.Apply(MetadataProperty{Serial: 31, Key: "clock.force-quantum", Removed: true})
	if s := g.fold(); s.Settings.ForceQuantum != 0 || s.Settings.Rate != 48000 {
		t.Errorf("after key removal: %+v", s.Settings)
	}
	g.Apply(MetadataProperty{Serial: 31})
	if s := g.fold(); s.Settings.Rate != 0 {
		t.Errorf("after subject clear: %+v", s.Settings)
	}
}

func TestFoldOnlyOnChange(t *testing.T) {
	g := newGraph(&replayDriver{})
	g.start()
	if g.fold() == nil {
		t.Fatal("first fold produced nothing")
	}
	if g.fold() != nil {
		t.Error("fold without changes produced a snapshot")
	}
}

func TestPropertylessObjectsNeverPanic(t *testing.T) {
	g := newGraph(&replayDriver{})
	g.start()
	g.Apply(GlobalAdded{ID: 1, Type: typePort})
	g.Apply(GlobalAdded{ID: 2, Type: typePort, Props: map[string]string{"object.serial": "9"}})
	g.Apply(PortInfo{Serial: 9})
	g.Apply(LinkInfo{Serial: 9})
	g.Apply(NodeInfo{Serial: 404})
	s := g.fold()
	p, ok := s.Ports[9]
	if !ok || p.Media != MediaUnknown || p.Direction != DirectionUnknown || p.NodeSerial != 0 {
		t.Errorf("property-less port = %+v", p)
	}
	if s.Unresolved != 1 {
		t.Errorf("unresolved = %d, want 1 (the ownerless port)", s.Unresolved)
	}
}
