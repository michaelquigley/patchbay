package pipewire

import (
	"reflect"
	"sort"
	"time"

	"github.com/michaelquigley/df/dl"
)

const (
	// summaryEvery bounds how often a metrics summary is published: at most ten a second, whatever the pod rate.
	summaryEvery = 100 * time.Millisecond
	// driverStale is how long a driver stays on the display without a pod showing followers: a driver that suspends
	// or loses its followers drops off rather than keeping its last quantum.
	driverStale = time.Second
	// currentClock is how close a driver's first pod's clock must be to CLOCK_MONOTONIC at arrival for the driver's
	// clock to count as current, so that the appearance cutoff can be applied to its pods.
	currentClock = int64(time.Second)
)

// ProfileBlock is one node's block in a profiler pod: a driver's driverBlock or a follower's followerBlock.
type ProfileBlock struct {
	ID       uint32
	Status   int32
	HasXruns bool // the block carries an xrun count; a block without one is unavailable, never substituted
	Xruns    uint32
}

// ProfilePoint is one profiler object: one driver's cycle. Nsec is the pod's clock; Arrival is the backend's
// CLOCK_MONOTONIC reading when the pod was received.
type ProfilePoint struct {
	Arrival   int64
	HasInfo   bool
	InfoXruns uint32 // the driver's own counter, kept at driver scope only
	HasClock  bool
	Nsec      int64
	Quantum   int64
	RateDenom uint32
	HasDriver bool
	Driver    ProfileBlock
	Followers []ProfileBlock
}

// ResetBaseline rebases every metrics record to its current total.
type ResetBaseline struct{}

func (ProfilePoint) input()  {}
func (ResetBaseline) input() {}

// MetricsSummary is what the ui shows of the profiler: per-driver quantum and rate, and xrun counts over the nodes
// currently tracked. it is a count over tracked nodes, not a session history: a replaced node's record is dropped,
// and a rebased counter lowers the sums.
type MetricsSummary struct {
	Available    bool            // the profiler is bound; false is no monitoring, not zero activity
	Drivers      []DriverMetrics // drivers with running followers, by name
	Total        uint64          // sum of totals over tracked nodes with a counter
	New          uint64          // sum of new (total less baseline) over the same nodes
	LastIncrease time.Time       // the most recent observed increase; zero when none
	Nodes        map[Serial]NodeMetrics
}

// DriverMetrics is one driver's observed cycle.
type DriverMetrics struct {
	Serial    Serial
	Name      string
	Quantum   int
	Rate      int
	InfoXruns uint32 // the driver's own info counter, at driver scope
}

// CycleMillis is the cycle duration in milliseconds; it is not input-to-output latency.
func (d DriverMetrics) CycleMillis() float64 {
	if d.Rate == 0 {
		return 0
	}
	return float64(d.Quantum) * 1000 / float64(d.Rate)
}

// NodeMetrics is one tracked node's record. a node whose blocks carry no counter is unavailable.
type NodeMetrics struct {
	// ClockGuard says which lifetime guard applied to the record's last accepted pod: true for clock-based (the
	// driver's pod clock is current, so pods older than the node's appearance are dropped), false for ordering only
	// (the driver's clock is not monotonic; only the serial and pod ordering guard the record).
	ClockGuard   bool
	Available    bool
	Total        uint64
	New          uint64
	LastIncrease time.Time
}

// metricsRecord is one node's counter for one lifetime (serial). appeared is the backend's own CLOCK_MONOTONIC
// reading when the node's registry appearance was received; lastPod the clock of the last accepted pod.
type metricsRecord struct {
	clockGuard   bool
	counted      bool
	available    bool
	total        uint64
	baseline     uint64
	lastPod      int64
	lastIncrease time.Time
}

// driverRecord is one driver's last accepted cycle.
type driverRecord struct {
	clockGuard    bool // the driver's pod clock was current at its first pod's arrival
	quantum, rate int
	infoXruns     uint32
	followers     int
	lastPod       int64
	seen          time.Time
}

// profile folds one profiler pod: the driver's cycle into its driver record, and each block's counter into its
// node's record. it never dirties the graph; summaries are published on their own clock. a pod without a clock is
// ignored whole: neither lifetime guard can be applied to it, so it decides no driver mode and touches no record.
func (g *Graph) profile(p ProfilePoint) {
	if !p.HasDriver || !p.HasClock {
		return
	}
	s := g.serialOf(p.Driver.ID, KindNode)
	if s == 0 {
		return
	}
	// the appearance cutoff compares pod clocks with CLOCK_MONOTONIC readings, which only means something for a
	// driver whose pod clock is current. it is decided once, on the driver's first pod with a clock, and never
	// re-evaluated: a backlog of delayed pods after a stall looks like a lagging clock, and is exactly what the
	// cutoff must keep dropping.
	d := g.session.drivers[s]
	if d == nil {
		d = &driverRecord{clockGuard: p.Arrival-p.Nsec <= currentClock && p.Nsec-p.Arrival <= currentClock}
		g.session.drivers[s] = d
		mode := "clock-based"
		if !d.clockGuard {
			mode = "ordering only"
		}
		dl.Infof("driver %d ('%s'): lifetime guard '%s' (pod clock %d, monotonic %d)", s, g.objects[s].props["node.name"], mode, p.Nsec, p.Arrival)
	}
	guard := d.clockGuard
	if (!guard || p.Nsec >= g.objects[s].appeared) && p.Nsec >= d.lastPod {
		d.quantum, d.rate, d.followers, d.lastPod, d.seen = int(p.Quantum), int(p.RateDenom), len(p.Followers), p.Nsec, g.now()
		d.infoXruns = p.InfoXruns
	}
	g.count(p.Driver, p.Nsec, guard)
	for _, f := range p.Followers {
		g.count(f, p.Nsec, guard)
	}
	g.metricsDirty = true
}

// count applies one block's counter to its node's record. with a clock-based guard, a pod older than the node's
// appearance belongs to a departed node that held the id, and is dropped before anything is initialized; with an
// ordering-only guard (the driver's clock is not monotonic) that check cannot be made. within a lifetime, a pod
// older than the last accepted one is dropped (pods flush per driver, out of order across drivers); only then does a
// counter that went backwards rebase the record.
func (g *Graph) count(b ProfileBlock, nsec int64, clockGuard bool) {
	s := g.serialOf(b.ID, KindNode)
	if s == 0 {
		return
	}
	if clockGuard && nsec < g.objects[s].appeared {
		return
	}
	rec := g.session.metrics[s]
	if rec == nil {
		rec = &metricsRecord{}
		g.session.metrics[s] = rec
	}
	if nsec < rec.lastPod {
		return
	}
	rec.lastPod = nsec
	rec.clockGuard = clockGuard
	if !b.HasXruns {
		if !rec.counted {
			rec.available = false
		}
		return
	}
	rec.available = true
	c := uint64(b.Xruns)
	switch {
	case !rec.counted:
		// the first counter, whenever it arrives, is a baseline, not new errors.
		rec.total, rec.baseline, rec.counted = c, c, true
	case c < rec.total:
		dl.Infof("xrun counter of node %d went from %d to %d; rebasing", s, rec.total, c)
		rec.total, rec.baseline = c, c
	case c > rec.total:
		rec.total = c
		rec.lastIncrease = g.now()
	}
}

// resetBaseline rebases every record to its current total: new becomes zero, totals are untouched.
func (g *Graph) resetBaseline() {
	for _, rec := range g.session.metrics {
		rec.baseline = rec.total
	}
	g.metricsDirty = true
	g.lastSummary = time.Time{}
}

// publishMetrics recomputes the summary at most every summaryEvery, and marks the graph dirty only when it changed.
// it runs on every tick, so a driver that stops sending pods ages off the display without one.
func (g *Graph) publishMetrics() {
	now := g.now()
	if now.Sub(g.lastSummary) < summaryEvery {
		return
	}
	if !g.metricsDirty && len(g.summary.Drivers) == 0 {
		return
	}
	g.lastSummary = now
	g.metricsDirty = false
	next := g.summarize(now)
	if reflect.DeepEqual(next, g.summary) {
		return
	}
	g.summary = next
	g.summaries++
	g.dirty = true
}

func (g *Graph) summarize(now time.Time) MetricsSummary {
	m := MetricsSummary{Available: g.profiler != 0}
	for s, d := range g.session.drivers {
		if d.followers == 0 || now.Sub(d.seen) > driverStale {
			continue
		}
		name := ""
		if o := g.objects[s]; o != nil {
			name = o.props["node.name"]
		}
		m.Drivers = append(m.Drivers, DriverMetrics{Serial: s, Name: name, Quantum: d.quantum, Rate: d.rate, InfoXruns: d.infoXruns})
	}
	sort.Slice(m.Drivers, func(i, j int) bool {
		if m.Drivers[i].Name != m.Drivers[j].Name {
			return m.Drivers[i].Name < m.Drivers[j].Name
		}
		return m.Drivers[i].Serial < m.Drivers[j].Serial
	})
	if len(g.session.metrics) > 0 {
		m.Nodes = map[Serial]NodeMetrics{}
	}
	for s, rec := range g.session.metrics {
		n := NodeMetrics{ClockGuard: rec.clockGuard, Available: rec.available && rec.counted, Total: rec.total, LastIncrease: rec.lastIncrease}
		if rec.total > rec.baseline {
			n.New = rec.total - rec.baseline
		}
		if n.Available {
			m.Total += n.Total
			m.New += n.New
			if n.LastIncrease.After(m.LastIncrease) {
				m.LastIncrease = n.LastIncrease
			}
		}
		m.Nodes[s] = n
	}
	return m
}
