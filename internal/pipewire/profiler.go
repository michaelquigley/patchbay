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
)

// ProfileBlock is one node's block in a profiler pod: a driver's driverBlock or a follower's followerBlock.
type ProfileBlock struct {
	ID       uint32
	Status   int32
	HasXruns bool // the block carries an xrun count; a block without one is unavailable, never substituted
	Xruns    uint32
}

// ProfilePoint is one profiler object: one driver's cycle. Nsec is the pod's clock, used for ordering.
type ProfilePoint struct {
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
	Available    bool
	Total        uint64
	New          uint64
	LastIncrease time.Time
}

// metricsRecord is one node's counter for one lifetime (serial); lastPod is the last accepted pod's clock.
type metricsRecord struct {
	counted      bool
	available    bool
	total        uint64
	baseline     uint64
	lastPod      int64
	lastIncrease time.Time
}

// driverRecord is one driver's last accepted cycle.
type driverRecord struct {
	quantum, rate int
	infoXruns     uint32
	followers     int
	lastPod       int64
	seen          time.Time
}

// profile folds one profiler pod: the driver's cycle into its driver record, and each block's counter into its
// node's record. it never dirties the graph; summaries are published on their own clock. a pod without a clock is
// ignored whole because its order cannot be checked.
func (g *Graph) profile(p ProfilePoint) {
	if !p.HasDriver || !p.HasClock {
		return
	}
	s := g.serialOf(p.Driver.ID, KindNode)
	if s == 0 {
		return
	}
	d := g.session.drivers[s]
	if d == nil {
		d = &driverRecord{}
		g.session.drivers[s] = d
	}
	if p.Nsec >= d.lastPod {
		d.quantum, d.rate, d.followers, d.lastPod, d.seen = int(p.Quantum), int(p.RateDenom), len(p.Followers), p.Nsec, g.now()
		d.infoXruns = p.InfoXruns
	}
	g.count(p.Driver, p.Nsec)
	for _, f := range p.Followers {
		g.count(f, p.Nsec)
	}
	g.metricsDirty = true
}

// count resolves a block's id to the current node serial. a buffered pod can seed a replacement's baseline after
// id reuse; this diagnostic attribution limit is accepted. within a record, older pods are dropped before a
// decreasing counter can rebase it (pods flush per driver and can arrive out of order across drivers).
func (g *Graph) count(b ProfileBlock, nsec int64) {
	s := g.serialOf(b.ID, KindNode)
	if s == 0 {
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
		n := NodeMetrics{Available: rec.available && rec.counted, Total: rec.total, LastIncrease: rec.lastIncrease}
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
