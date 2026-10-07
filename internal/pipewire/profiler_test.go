package pipewire

import (
	"testing"
	"time"
)

// metricsGraph is a live graph with two drivers and their followers, a fake driver whose monotonic clock the test
// sets before each announcement, and a wall clock the test moves.
type metricsGraph struct {
	t     *testing.T
	g     *Graph
	drv   *fakeSession
	clock time.Time
}

func newMetricsGraph(t *testing.T) *metricsGraph {
	t.Helper()
	drv := &fakeSession{bound: map[Serial]bool{}, proxies: map[RequestID]bool{}}
	mg := &metricsGraph{t: t, drv: drv, clock: time.Unix(2000, 0)}
	g := newGraph(drv)
	g.connection = 1
	g.now = func() time.Time { return mg.clock }
	g.start()
	mg.g = g
	drv.mono = 1000
	g.Apply(GlobalAdded{ID: 2, Type: typeProfiler, Version: 3, Props: map[string]string{"object.serial": "20"}})
	g.Apply(nodeGlobal(40, 400, "alsa_output.scarlett"))
	g.Apply(nodeGlobal(41, 410, "alsa_input.webcam"))
	g.Apply(nodeGlobal(50, 500, "REAPER"))
	g.Apply(nodeGlobal(51, 510, "mpv"))
	return mg
}

func block(id uint32, xruns uint32) ProfileBlock {
	return ProfileBlock{ID: id, HasXruns: true, Xruns: xruns}
}

// pod delivers a pod from a driver whose clock is current: it arrives at its own clock.
func (mg *metricsGraph) pod(nsec int64, driver ProfileBlock, quantum int64, followers ...ProfileBlock) {
	mg.podAt(nsec, nsec, driver, quantum, followers...)
}

func (mg *metricsGraph) podAt(nsec, arrival int64, driver ProfileBlock, quantum int64, followers ...ProfileBlock) {
	mg.g.Apply(ProfilePoint{Arrival: arrival, HasClock: true, Nsec: nsec, Quantum: quantum, RateDenom: 48000, HasDriver: true,
		Driver: driver, Followers: followers, HasInfo: true, InfoXruns: 99})
}

// summary publishes and returns the current summary, past the rate limit.
func (mg *metricsGraph) summary() MetricsSummary {
	mg.clock = mg.clock.Add(summaryEvery)
	mg.g.Apply(Tick{})
	return mg.g.summary
}

func (mg *metricsGraph) node(s Serial) NodeMetrics {
	return mg.summary().Nodes[s]
}

// a node's first counter, however late it arrives, is a baseline: zero new, no increase time.
func TestLateFirstMeasurement(t *testing.T) {
	mg := newMetricsGraph(t)
	mg.pod(900000, block(40, 0), 64, block(50, 7))
	n := mg.node(500)
	if !n.Available || n.Total != 7 || n.New != 0 || !n.LastIncrease.IsZero() {
		t.Errorf("late first measurement = %+v", n)
	}
}

// a counter that goes backwards under one serial rebases, without recording an increase.
func TestCounterDecreaseRebases(t *testing.T) {
	mg := newMetricsGraph(t)
	mg.pod(2000, block(40, 0), 64, block(50, 7))
	mg.pod(3000, block(40, 0), 64, block(50, 5))
	if n := mg.node(500); n.Total != 5 || n.New != 0 || !n.LastIncrease.IsZero() {
		t.Errorf("after the decrease = %+v", n)
	}
	mg.pod(4000, block(40, 0), 64, block(50, 6))
	if n := mg.node(500); n.Total != 6 || n.New != 1 || n.LastIncrease.IsZero() {
		t.Errorf("after an increase from the new baseline = %+v", n)
	}
}

// a node removed and re-added under the same id: the first pod after the re-add is from the old lifetime, then a
// buffered one showing an increase; both predate the replacement's appearance, so its record stays uninitialized.
func TestReplacementIgnoresOldLifetimePods(t *testing.T) {
	mg := newMetricsGraph(t)
	mg.pod(2000, block(40, 0), 64, block(50, 3))
	mg.g.Apply(GlobalRemoved{ID: 50})
	mg.drv.mono = 9000
	mg.g.Apply(nodeGlobal(50, 501, "REAPER"))
	mg.pod(8000, block(40, 0), 64, block(50, 3))
	mg.pod(8500, block(40, 0), 64, block(50, 4))
	s := mg.summary()
	if _, ok := s.Nodes[500]; ok {
		t.Error("the departed node's record survived")
	}
	if n, ok := s.Nodes[501]; ok && (n.Available || n.Total != 0 || n.New != 0) {
		t.Errorf("the replacement was initialized from old pods: %+v", n)
	}
	if s.New != 0 {
		t.Errorf("new errors = %d", s.New)
	}
	mg.pod(9500, block(40, 0), 64, block(50, 4))
	if n := mg.node(501); !n.Available || n.Total != 4 || n.New != 0 {
		t.Errorf("the replacement's own first pod = %+v", n)
	}
}

// counters 12, 10, 12 where the 10 carries an older pod clock: the stale pod changes nothing.
func TestReorderedPodIsDropped(t *testing.T) {
	mg := newMetricsGraph(t)
	mg.pod(1000, block(40, 0), 64, block(50, 11))
	mg.pod(1100, block(40, 0), 64, block(50, 12))
	before := mg.node(500)
	mg.clock = mg.clock.Add(time.Second)
	mg.pod(1050, block(40, 0), 64, block(50, 10))
	mg.pod(1200, block(40, 0), 64, block(50, 12))
	after := mg.node(500)
	if after.Total != 12 || after.New != 1 || !after.LastIncrease.Equal(before.LastIncrease) {
		t.Errorf("before %+v, after %+v", before, after)
	}
}

// a follower block without a counter is unavailable, never filled from the driver's info counter, and does not
// count toward the sums.
func TestFollowerWithoutCounterIsUnavailable(t *testing.T) {
	mg := newMetricsGraph(t)
	mg.pod(2000, block(40, 2), 64, ProfileBlock{ID: 51})
	s := mg.summary()
	if n := s.Nodes[510]; n.Available || n.Total != 0 {
		t.Errorf("counterless follower = %+v", n)
	}
	if s.Total != 2 {
		t.Errorf("total = %d, want only the driver's own 2", s.Total)
	}
	if len(s.Drivers) != 1 || s.Drivers[0].InfoXruns != 99 {
		t.Errorf("driver = %+v; its info counter belongs at driver scope", s.Drivers)
	}
}

// two drivers with followers show two lines; a driver whose pod shows no followers, or that stops sending pods,
// drops off rather than keeping its last quantum.
func TestDriverLines(t *testing.T) {
	mg := newMetricsGraph(t)
	mg.pod(2000, block(40, 0), 64, block(50, 0))
	mg.pod(2000, block(41, 0), 128, block(51, 0))
	s := mg.summary()
	if len(s.Drivers) != 2 {
		t.Fatalf("drivers = %+v", s.Drivers)
	}
	for _, d := range s.Drivers {
		want := map[Serial]int{400: 64, 410: 128}[d.Serial]
		if d.Quantum != want || d.Rate != 48000 {
			t.Errorf("driver %d = %+v", d.Serial, d)
		}
	}
	if ms := s.Drivers[0].CycleMillis(); s.Drivers[0].Quantum == 64 && (ms < 1.33 || ms > 1.34) {
		t.Errorf("cycle = %v ms", ms)
	}

	mg.pod(3000, block(41, 0), 128) // lost its followers
	if s := mg.summary(); len(s.Drivers) != 1 || s.Drivers[0].Serial != 400 {
		t.Errorf("after the webcam lost its followers: %+v", s.Drivers)
	}
	mg.clock = mg.clock.Add(driverStale) // the scarlett suspends: no more pods
	if s := mg.summary(); len(s.Drivers) != 0 {
		t.Errorf("a suspended driver kept its line: %+v", s.Drivers)
	}
}

// at quantum 64 (750 pods a second) with every pod changing a counter, the summary changes at most ten times a
// second.
func TestSummaryRate(t *testing.T) {
	mg := newMetricsGraph(t)
	const seconds = 2
	period := time.Second / 750
	for i := 0; i < 750*seconds; i++ {
		mg.clock = mg.clock.Add(period)
		mg.pod(int64(2000+i), block(40, 0), 64, block(50, uint32(i)))
		mg.g.Apply(Tick{})
	}
	if mg.g.summaries > 10*seconds+1 {
		t.Errorf("%d summaries in %d seconds", mg.g.summaries, seconds)
	}
	if mg.g.summaries < 10*seconds-2 {
		t.Errorf("only %d summaries in %d seconds", mg.g.summaries, seconds)
	}
}

// a removed node's record leaves the summary on the next tick, even with no driver line and no new pods.
func TestRemovedNodeLeavesSummary(t *testing.T) {
	mg := newMetricsGraph(t)
	mg.pod(2000, block(40, 2), 64, block(50, 5))
	mg.pod(2100, block(40, 2), 64) // the driver loses its follower: no driver line
	s := mg.summary()
	if _, ok := s.Nodes[500]; !ok || len(s.Drivers) != 0 || s.Total != 7 {
		t.Fatalf("before removal: drivers %v, total %d, nodes %v", s.Drivers, s.Total, s.Nodes)
	}
	mg.g.Apply(GlobalRemoved{ID: 50})
	s = mg.summary()
	if _, ok := s.Nodes[500]; ok || s.Total != 2 {
		t.Errorf("after removal: total %d, nodes %v", s.Total, s.Nodes)
	}
}

// a baseline reset zeroes new without touching totals.
func TestResetBaseline(t *testing.T) {
	mg := newMetricsGraph(t)
	mg.pod(2000, block(40, 0), 64, block(50, 3))
	mg.pod(3000, block(40, 0), 64, block(50, 8))
	if n := mg.node(500); n.New != 5 {
		t.Fatalf("new = %d", n.New)
	}
	mg.g.Apply(ResetBaseline{})
	s := mg.summary()
	if n := s.Nodes[500]; n.New != 0 || n.Total != 8 || s.New != 0 || s.Total != 8 {
		t.Errorf("after reset: node %+v, sums %d/%d", n, s.Total, s.New)
	}
}

// a driver whose pods run an hour behind CLOCK_MONOTONIC (gnome-shell's video driver does) cannot be held to the
// appearance cutoff: its nodes stay visible, guarded by ordering only, while the monotonic driver's nodes keep the
// clock-based guard and its cutoff.
func TestLaggingDriverClockIsOrderingOnly(t *testing.T) {
	mg := newMetricsGraph(t)
	hour := int64(time.Hour)
	// every pod from driver 41 is stamped an hour before it arrives, so before its nodes' appearance (1000).
	mg.podAt(500, 500+hour, block(41, 0), 1024, block(51, 2))
	mg.podAt(600, 600+hour, block(41, 0), 1024, block(51, 3))
	// the monotonic driver: one stale pod before appearance, then current ones.
	mg.pod(900, block(40, 0), 64, block(50, 9))
	mg.pod(2000, block(40, 0), 64, block(50, 4))
	s := mg.summary()

	if n, ok := s.Nodes[510]; !ok || !n.Available || n.ClockGuard || n.Total != 3 || n.New != 1 {
		t.Errorf("lagging driver's follower = %+v (found %v), want visible, ordering only", n, ok)
	}
	if n := s.Nodes[410]; !n.Available || n.ClockGuard {
		t.Errorf("lagging driver's own record = %+v", n)
	}
	if n := s.Nodes[500]; !n.ClockGuard || n.Total != 4 || n.New != 0 {
		t.Errorf("monotonic driver's follower = %+v, want clock-based with the stale pod dropped", n)
	}
	lines := map[Serial]int{}
	for _, d := range s.Drivers {
		lines[d.Serial] = d.Quantum
	}
	if lines[410] != 1024 || lines[400] != 64 {
		t.Errorf("driver lines = %v", lines)
	}
}

// a stall delays a backlog of pods past the current-clock window; the driver's guard was decided on its first pod
// and stays clock-based, so a stale pod from a departed node is still dropped under its replacement.
func TestStallBacklogKeepsClockGuard(t *testing.T) {
	mg := newMetricsGraph(t)
	stall := int64(3 * time.Second)
	mg.pod(2000, block(40, 0), 64, block(50, 3))
	mg.g.Apply(GlobalRemoved{ID: 50})
	mg.drv.mono = 9000
	mg.g.Apply(nodeGlobal(50, 501, "REAPER"))
	mg.podAt(8000, 8000+stall, block(40, 0), 64, block(50, 7))
	mg.podAt(8500, 8500+stall, block(40, 0), 64, block(50, 8))
	if d := mg.g.session.drivers[400]; d == nil || !d.clockGuard {
		t.Fatalf("driver after the backlog = %+v, want clock-based", d)
	}
	if _, ok := mg.g.session.metrics[501]; ok {
		t.Errorf("the backlog seeded the replacement: %+v", mg.g.session.metrics[501])
	}
	mg.pod(9500, block(40, 0), 64, block(50, 1))
	if n := mg.node(501); !n.ClockGuard || n.Total != 1 || n.New != 0 {
		t.Errorf("the replacement's own first pod = %+v", n)
	}
}

// a driver whose first pod lags is ordering-only for its lifetime, even once its pods arrive current.
func TestFirstLaggingPodDecidesOrderingOnly(t *testing.T) {
	mg := newMetricsGraph(t)
	mg.podAt(500, 500+int64(time.Hour), block(41, 0), 1024, block(51, 2))
	mg.pod(2000, block(41, 0), 1024, block(51, 3))
	if d := mg.g.session.drivers[410]; d == nil || d.clockGuard {
		t.Fatalf("driver = %+v, want ordering only", d)
	}
	if n := mg.node(510); n.ClockGuard || n.Total != 3 || n.New != 1 {
		t.Errorf("follower = %+v", n)
	}
}

// the summary is available only while the profiler is bound: bound, then removed; and announced but not bindable.
func TestMetricsAvailableWhileProfilerBound(t *testing.T) {
	mg := newMetricsGraph(t)
	mg.pod(2000, block(40, 0), 64, block(50, 3))
	if s := mg.summary(); !s.Available || s.Total != 3 {
		t.Errorf("bound summary = %+v", s)
	}
	mg.g.Apply(GlobalRemoved{ID: 2})
	if s := mg.summary(); s.Available {
		t.Errorf("summary after the profiler went = %+v", s)
	}

	drv := &fakeSession{bound: map[Serial]bool{}, proxies: map[RequestID]bool{}, refuse: typeProfiler}
	g := newGraph(drv)
	g.connection = 1
	clock := time.Unix(2000, 0)
	g.now = func() time.Time { return clock }
	g.start()
	g.Apply(GlobalAdded{ID: 2, Type: typeProfiler, Version: 3, Props: map[string]string{"object.serial": "20"}})
	g.Apply(nodeGlobal(40, 400, "alsa_output.scarlett"))
	clock = clock.Add(summaryEvery)
	g.Apply(Tick{})
	if g.summary.Available {
		t.Errorf("summary with an unbindable profiler = %+v", g.summary)
	}
}

// a pod without a clock is ignored whole: no driver mode is decided (so none is logged) and no record is created.
func TestClocklessPodIsIgnored(t *testing.T) {
	mg := newMetricsGraph(t)
	mg.g.Apply(ProfilePoint{Arrival: 5000, Quantum: 64, RateDenom: 48000, HasDriver: true, Driver: block(40, 1),
		Followers: []ProfileBlock{block(50, 2)}, HasInfo: true, InfoXruns: 99})
	if len(mg.g.session.drivers) != 0 || len(mg.g.session.metrics) != 0 {
		t.Errorf("clockless pod left drivers %v, metrics %v", mg.g.session.drivers, mg.g.session.metrics)
	}
	if s := mg.summary(); len(s.Drivers) != 0 || len(s.Nodes) != 0 {
		t.Errorf("summary = %+v", s)
	}
}

// the force-quantum request is confirmed only by the settings metadata's echo; an override already in place is
// confirmed from what the metadata says; no echo times out.
func TestSetForceQuantum(t *testing.T) {
	rg := newRequestGraph(t)
	rg.g.Apply(settingsGlobal(31, 31))
	rg.g.Apply(MetadataProperty{Serial: 31, Key: forceQuantumKey, Value: "0"})

	rg.g.Apply(RequestPosted{Request: Request{ID: 1, Kind: RequestSetForceQuantum, Quantum: 256}})
	if len(rg.drv.metadata) != 1 || rg.drv.metadata[0] != [3]string{forceQuantumKey, "", "256"} {
		t.Fatalf("metadata written = %v", rg.drv.metadata)
	}
	expectState(t, rg.request(1), RequestPending, "")
	rg.g.Apply(MetadataProperty{Serial: 31, Key: forceQuantumKey, Value: "256"})
	expectState(t, rg.request(1), RequestConfirmed, "")
	if rg.snapshot().Settings.ForceQuantum != 256 {
		t.Error("settings do not show the override")
	}

	rg.g.Apply(RequestPosted{Request: Request{ID: 2, Kind: RequestSetForceQuantum, Quantum: 256}})
	expectState(t, rg.request(2), RequestConfirmed, "")

	rg.g.Apply(RequestPosted{Request: Request{ID: 3, Kind: RequestSetForceQuantum, Quantum: 0}})
	rg.clock = rg.clock.Add(requestTimeout + time.Millisecond)
	rg.g.Apply(Tick{})
	expectState(t, rg.request(3), RequestFailed, "did not echo 0")
}

// the disconnected snapshot shows no metrics as current.
func TestDisconnectClearsMetrics(t *testing.T) {
	_, b, fs, _ := liveFake(t)
	defer b.Close()
	fs.mono = 0
	fs.callback(ProfilePoint{HasClock: true, Nsec: 5000, Quantum: 64, RateDenom: 48000, HasDriver: true,
		Driver: block(1, 0), Followers: []ProfileBlock{block(2, 4)}})
	time.Sleep(summaryEvery + 10*time.Millisecond)
	fs.callback()
	if len(b.Snapshot().Metrics.Drivers) == 0 {
		t.Fatal("no metrics published while live")
	}
	fs.sess.lost("connection error (broken pipe)")
	if m := b.Snapshot().Metrics; len(m.Drivers) != 0 || m.Total != 0 || len(m.Nodes) != 0 {
		t.Errorf("disconnected snapshot shows metrics: %+v", m)
	}
}
