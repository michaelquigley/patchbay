package model

import (
	"math"
	"sort"

	"github.com/michaelquigley/patchbay/internal/pipewire"
	"github.com/michaelquigley/patchbay/internal/workspace"
)

// placement geometry, in canvas units. block sizes are estimates from the port count and the text length; the canvas
// draws the real size, and an estimate only has to keep fresh placements from overlapping.
const (
	grid          = 20
	headerHeight  = 40
	rowHeight     = 20
	charWidth     = 8
	blockPadding  = 48
	minBlockWidth = 160
	blockGap      = 40
	columnGap     = 80
)

// viewMargin keeps arrivals off the very edge of the view.
const viewMargin = 20

// assumedCanvas is the canvas size, in screen pixels, assumed before the canvas has reported its visible rectangle:
// small enough that a block placed in it is on screen in any window patchbay opens.
const (
	assumedCanvasWidth  = 800
	assumedCanvasHeight = 500
)

// Rect is a rectangle in canvas coordinates.
type Rect struct {
	MinX, MinY, MaxX, MaxY float32
}

// arrivalColumn is where genuinely new blocks are stacked: right-aligned inside the visible canvas rectangle,
// downward from its top in arrival order. a different visible rectangle starts a new column.
type arrivalColumn struct {
	set   bool
	view  Rect
	nextY float32
}

// Reconcile derives the view for a snapshot. recognition and placement happen only on a live snapshot: before the
// startup barrier and after a reconnect the snapshot is connecting, and no record is assigned until it is live. a
// snapshot that is not live releases every assignment (serials repeat across a daemon restart) and returns the last
// live view marked stale.
func (m *Model) Reconcile(snap *pipewire.Snapshot) *View {
	m.state, m.reason = snap.State, snap.Error
	if snap.Session != m.connection {
		// a new connection is a new graph, even when every snapshot between the old one going away and this one
		// arriving was missed: serials repeat across a daemon restart, so nothing bound to the old graph carries over.
		if m.wasLive {
			m.release()
		}
		m.connection = snap.Session
	}
	if snap.State != pipewire.Live {
		if m.wasLive {
			m.release()
		}
		return m.staleView()
	}
	m.wasLive = true
	m.snap = snap
	m.live = derive(snap)
	m.forgetVanished()
	m.place(m.match())
	m.last = m.build()
	v := *m.last
	v.Appeared = m.announce()
	return &v
}

// announce returns the blocks that appeared since the last reconcile, in arrival order. the graph a connection
// starts with is not announced: it is what the operator opened onto, not something that arrived.
func (m *Model) announce() []BlockID {
	if !m.seenInitial {
		for id := range m.live {
			m.seen[id] = true
		}
		m.seenInitial = true
		return nil
	}
	var appeared []*derived
	for id, d := range m.live {
		if !m.seen[id] {
			m.seen[id] = true
			appeared = append(appeared, d)
		}
	}
	byArrival(appeared)
	out := make([]BlockID, 0, len(appeared))
	for _, d := range appeared {
		out = append(out, d.id)
	}
	return out
}

// Refresh rebuilds the view after a model operation. it rebuilds only while the last snapshot was live; otherwise it
// returns the stale view, never the last live view as if it were current.
func (m *Model) Refresh() *View {
	if m.state != pipewire.Live || m.snap == nil {
		return m.staleView()
	}
	m.last = m.build()
	return m.last
}

// staleView is the one builder for views while the connection is not live: a copy of the last live view carrying the
// current state and reason, or an empty view when nothing live has been seen.
func (m *Model) staleView() *View {
	v := View{ShowHidden: m.showHidden}
	if m.last != nil {
		v = *m.last
	}
	v.State = m.state
	v.Reason = m.reason
	v.Stale = true
	return &v
}

// release drops everything bound to the live graph: assignments return their records to the pool.
func (m *Model) release() {
	m.wasLive = false
	m.assigned = map[BlockID]string{}
	m.owner = map[string]BlockID{}
	m.session = map[BlockID]*workspace.Record{}
	m.column = arrivalColumn{}
	m.seen = map[BlockID]bool{}
	m.seenInitial = false
	m.live = nil
	m.snap = nil
}

func (m *Model) forgetVanished() {
	for id, rk := range m.assigned {
		if _, ok := m.live[id]; !ok {
			delete(m.assigned, id)
			delete(m.owner, rk)
		}
	}
	for id := range m.session {
		if _, ok := m.live[id]; !ok {
			delete(m.session, id)
		}
	}
	for id := range m.seen {
		if _, ok := m.live[id]; !ok {
			delete(m.seen, id)
		}
	}
}

func recordKeys(r *workspace.Record) []Key {
	return append([]Key{r.Key}, r.Also...)
}

// match assigns remembered records to live blocks under the conservative rule, then gives genuinely new blocks a
// record or session placement. a record is assigned only when exactly one live block has the recognition key,
// exactly one unassigned record answers to it, and no other live block answers to any key of that record; every
// other case leaves the blocks unassigned and the records available for association. it returns the blocks that need
// placing.
func (m *Model) match() []*derived {
	byKey := map[Key][]*derived{}
	for _, d := range m.live {
		if d.keyed {
			byKey[d.key] = append(byKey[d.key], d)
		}
	}
	pool := map[Key][]string{}
	for rk, rec := range m.ws.Records {
		if _, held := m.owner[rk]; held {
			continue
		}
		for _, k := range recordKeys(rec) {
			pool[k] = append(pool[k], rk)
		}
	}

	type pair struct {
		id BlockID
		rk string
	}
	var pairs []pair
	for k, insts := range byKey {
		if len(insts) != 1 {
			continue
		}
		d := insts[0]
		if _, ok := m.assigned[d.id]; ok {
			continue
		}
		if _, ok := m.session[d.id]; ok {
			// presented as new when it appeared; it stays new until the operator acts on it.
			continue
		}
		recs := pool[k]
		if len(recs) != 1 {
			continue
		}
		answering := 0
		for _, rk := range recordKeys(m.ws.Records[recs[0]]) {
			answering += len(byKey[rk])
		}
		if answering != 1 {
			continue
		}
		pairs = append(pairs, pair{d.id, recs[0]})
	}
	for _, p := range pairs {
		m.assign(p.id, p.rk)
	}

	firstLayout := len(m.ws.Records) == 0 && !m.laidOut
	var fresh []*derived
	for _, d := range m.live {
		if _, ok := m.assigned[d.id]; ok {
			continue
		}
		if _, ok := m.session[d.id]; ok {
			continue
		}
		if d.keyed && len(byKey[d.key]) == 1 && len(pool[d.key]) == 0 {
			// genuinely new and unambiguous: it gets its own record.
			rk := m.allocate(d.key)
			m.ws.Records[rk] = &workspace.Record{Key: d.key}
			m.assign(d.id, rk)
		} else {
			m.session[d.id] = &workspace.Record{Key: d.key}
		}
		fresh = append(fresh, d)
	}
	if firstLayout && len(fresh) > 0 {
		m.laidOut = true
		m.layoutColumns(fresh)
		fresh = nil
	}
	return fresh
}

// recordOf returns the record that holds a live block's placement and preferences.
func (m *Model) recordOf(id BlockID) *workspace.Record {
	if rk, ok := m.assigned[id]; ok {
		return m.ws.Records[rk]
	}
	return m.session[id]
}

func blockHeight(d *derived) float32 {
	return snap(headerHeight + rowHeight*float32(max(len(d.ports), 1)))
}

// blockWidth estimates a block's width from its longest line: the title with its media, direction, and suffixes, or
// a port label with its hidden-count suffix.
func blockWidth(d *derived) float32 {
	chars := len([]rune(d.title)) + len(" · ") + len(d.key.Media) + 1 + len(d.key.Direction) + len(" #9 · 99 hidden")
	for _, p := range d.ports {
		chars = max(chars, len([]rune(p.label))+len(" · 99 hidden"))
	}
	return snap(max(minBlockWidth, float32(chars*charWidth+blockPadding)))
}

func snap(v float32) float32 {
	return float32(math.Round(float64(v)/grid) * grid)
}

func byArrival(blocks []*derived) {
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].arrival != blocks[j].arrival {
			return blocks[i].arrival < blocks[j].arrival
		}
		return blocks[i].id < blocks[j].id
	})
}

// layoutColumns lays an empty workspace's first graph out once: outputs left, inputs right, visible blocks first.
func (m *Model) layoutColumns(blocks []*derived) {
	var outs, ins []*derived
	for _, d := range blocks {
		if d.key.Direction == DirectionOut {
			outs = append(outs, d)
		} else {
			ins = append(ins, d)
		}
	}
	stack := func(col []*derived, x float32) {
		sort.Slice(col, func(i, j int) bool {
			vi, vj := m.anyPortVisible(col[i]), m.anyPortVisible(col[j])
			if vi != vj {
				return vi
			}
			if c := naturalCompare(col[i].title, col[j].title); c != 0 {
				return c < 0
			}
			return col[i].id < col[j].id
		})
		y := float32(0)
		for _, d := range col {
			rec := m.recordOf(d.id)
			rec.X, rec.Y = x, y
			y = snap(y + blockHeight(d) + blockGap)
		}
	}
	inputsX := float32(0)
	for _, d := range outs {
		inputsX = max(inputsX, blockWidth(d))
	}
	if len(outs) > 0 {
		inputsX = snap(inputsX + columnGap)
	}
	stack(outs, 0)
	stack(ins, inputsX)
	m.changed()
}

// SetViewport tells the model which canvas rectangle is visible. the canvas reports it after every frame; arrivals
// are placed inside it.
func (m *Model) SetViewport(r Rect) {
	if r.MaxX > r.MinX && r.MaxY > r.MinY {
		m.viewport, m.hasViewport = r, true
	}
}

// visible is the canvas rectangle arrivals are placed in: the one the canvas last reported, or, before it has
// reported one, the remembered view at an assumed small canvas size, which is what the window opens onto.
func (m *Model) visible() Rect {
	if m.hasViewport {
		return m.viewport
	}
	zoom := m.ws.View.Zoom
	if zoom <= 0 {
		zoom = 1
	}
	minX, minY := -m.ws.View.PanX, -m.ws.View.PanY
	return Rect{MinX: minX, MinY: minY, MaxX: minX + assumedCanvasWidth/zoom, MaxY: minY + assumedCanvasHeight/zoom}
}

func snapDown(v float32) float32 {
	return float32(math.Floor(float64(v)/grid) * grid)
}

func snapUp(v float32) float32 {
	return float32(math.Ceil(float64(v)/grid) * grid)
}

// place puts genuinely new blocks where the operator is looking: right-aligned inside the visible canvas rectangle,
// shifted left only as far as needed to be fully visible, stacked downward from its top in arrival order. existing
// blocks never move, and a block with a remembered record is never placed here.
func (m *Model) place(fresh []*derived) {
	if len(fresh) == 0 {
		return
	}
	byArrival(fresh)
	view := m.visible()
	if !m.column.set || m.column.view != view {
		m.column = arrivalColumn{set: true, view: view, nextY: snapUp(view.MinY + viewMargin)}
	}
	persisted := false
	for _, d := range fresh {
		x := snapDown(view.MaxX - viewMargin - blockWidth(d))
		if left := snapUp(view.MinX + viewMargin); x < left {
			x = left
		}
		rec := m.recordOf(d.id)
		rec.X, rec.Y = x, m.column.nextY
		m.column.nextY = snap(m.column.nextY + blockHeight(d) + blockGap)
		if _, ok := m.assigned[d.id]; ok {
			persisted = true
		}
	}
	if persisted {
		m.changed()
	}
}

// portVisible applies the visibility rule: Show hidden reveals everything; otherwise a port is drawn unless its block
// or the port itself is hidden by preference or its category class is filtered.
func (m *Model) portVisible(rec *workspace.Record, p derivedPort) (visible, hiddenPref bool) {
	hiddenPref = p.key != "" && rec.PortHidden(p.key)
	if m.showHidden {
		return true, hiddenPref
	}
	return !rec.Hidden && !hiddenPref && !m.ws.HiddenClasses[p.class], hiddenPref
}

func (m *Model) anyPortVisible(d *derived) bool {
	rec := m.recordOf(d.id)
	if rec == nil {
		return false
	}
	for _, p := range d.ports {
		if v, _ := m.portVisible(rec, p); v {
			return true
		}
	}
	return false
}

// build assembles the view from the live blocks and the current snapshot's links.
func (m *Model) build() *View {
	v := &View{State: m.snap.State, Session: m.snap.Session, LiveGeneration: m.snap.Generation, ShowHidden: m.showHidden}
	ids := make([]BlockID, 0, len(m.live))
	for id := range m.live {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	type portRef struct{ block, port int }
	refs := map[pipewire.Serial]portRef{}
	for _, id := range ids {
		d := m.live[id]
		rec := m.recordOf(id)
		b := Block{
			ID:     id,
			Key:    d.key,
			Owner:  d.owner,
			Keyed:  d.keyed,
			Record: m.assigned[id],
			Title:  d.title,
			X:      rec.X,
			Y:      rec.Y,
			Hidden: rec.Hidden,
		}
		for _, p := range d.ports {
			visible, hiddenPref := m.portVisible(rec, p)
			b.Ports = append(b.Ports, Port{
				Serial:  p.serial,
				Key:     p.key,
				Label:   p.label,
				Class:   p.class,
				Hidden:  hiddenPref,
				Visible: visible,
			})
			if visible {
				b.Visible = true
			}
		}
		v.Blocks = append(v.Blocks, b)
		for pi, p := range b.Ports {
			refs[p.Serial] = portRef{len(v.Blocks) - 1, pi}
		}
	}

	// unassigned blocks sharing a recognition key are told apart by an arrival-ordered display ordinal.
	collisions := map[Key][]int{}
	for i, b := range v.Blocks {
		if b.Record == "" && b.Keyed {
			collisions[b.Key] = append(collisions[b.Key], i)
		}
	}
	for _, idx := range collisions {
		if len(idx) < 2 {
			continue
		}
		sort.Slice(idx, func(i, j int) bool { return m.live[v.Blocks[idx[i]].ID].arrival < m.live[v.Blocks[idx[j]].ID].arrival })
		for n, i := range idx {
			v.Blocks[i].Ordinal = n + 1
		}
	}

	links := make([]pipewire.Serial, 0, len(m.snap.Links))
	for s := range m.snap.Links {
		links = append(links, s)
	}
	sort.Slice(links, func(i, j int) bool { return links[i] < links[j] })
	visible := func(s pipewire.Serial) (portRef, bool) {
		r, ok := refs[s]
		return r, ok && v.Blocks[r.block].Ports[r.port].Visible
	}
	for _, s := range links {
		l := m.snap.Links[s]
		out, outVisible := visible(l.OutPort)
		in, inVisible := visible(l.InPort)
		switch {
		case outVisible && inVisible:
			v.Links = append(v.Links, Link{
				Serial:   s,
				Out:      l.OutPort,
				In:       l.InPort,
				OutBlock: v.Blocks[out.block].ID,
				InBlock:  v.Blocks[in.block].ID,
				State:    l.State,
			})
			continue
		case outVisible:
			v.Blocks[out.block].Ports[out.port].HiddenLinks++
		case inVisible:
			v.Blocks[in.block].Ports[in.port].HiddenLinks++
		}
		// a connection on a hidden port of a visible block is counted on that block's title.
		for _, end := range []pipewire.Serial{l.OutPort, l.InPort} {
			if r, ok := refs[end]; ok && !v.Blocks[r.block].Ports[r.port].Visible && v.Blocks[r.block].Visible {
				v.Blocks[r.block].HiddenLinks++
			}
		}
	}

	for rk, rec := range m.ws.Records {
		if _, held := m.owner[rk]; !held {
			v.Absent = append(v.Absent, AbsentRecord{Record: rk, Key: rec.Key})
		}
	}
	sort.Slice(v.Absent, func(i, j int) bool { return v.Absent[i].Record < v.Absent[j].Record })
	return v
}
