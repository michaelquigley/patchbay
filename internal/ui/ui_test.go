package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/dfx"
	"github.com/michaelquigley/patchbay/internal/model"
	"github.com/michaelquigley/patchbay/internal/pipewire"
	"github.com/michaelquigley/patchbay/internal/sample"
)

func load(t *testing.T, name string) *pipewire.Snapshot {
	t.Helper()
	snap, err := sample.Load(filepath.Join("../../samples", name))
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

const (
	elevenBaseline = "eleven-20261002-123354-baseline"
	elevenRestart  = "eleven-20261002-123403-reaper-restart"
)

func openModel(t *testing.T) *model.Model {
	t.Helper()
	m, err := model.Open(filepath.Join(t.TempDir(), "workspace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

// counting wraps the model to count presentation operations.
type counting struct {
	*model.Model
	moves int
}

func (c *counting) MoveBlocks(moves []model.Placement) error {
	c.moves++
	return c.Model.MoveBlocks(moves)
}

func nodeFor(t *testing.T, f frame, title string) nodeDecl {
	t.Helper()
	for _, n := range f.nodes {
		if strings.HasPrefix(n.title, title) {
			return n
		}
	}
	t.Fatalf("no node titled '%v'", title)
	return nodeDecl{}
}

// blocks are named by record key (or by instance while unrecorded), pins by block and port key, links by serial;
// every id formats uniquely, and every declared link names declared pins.
func TestIDMapping(t *testing.T) {
	m := openModel(t)
	v := m.Reconcile(load(t, elevenBaseline))
	f := plan(v, newSelection(), nil, m.HiddenClasses())

	seen := map[string]bool{}
	pins := map[ID]bool{}
	claim := func(id ID) {
		if seen[id.String()] {
			t.Errorf("id '%v' declared twice", id)
		}
		seen[id.String()] = true
	}
	for _, n := range f.nodes {
		claim(n.id)
		b, _ := v.Block(n.block)
		if b.Record != "" && n.id.Block != b.Record {
			t.Errorf("block '%v' named '%v', want its record key", b.ID, n.id.Block)
		}
		if b.Record == "" && n.id.Block != "~"+string(b.ID) {
			t.Errorf("unrecorded block '%v' named '%v'", b.ID, n.id.Block)
		}
		for _, p := range n.pins {
			claim(p.id)
			pins[p.id] = true
			if p.id.Block != n.id.Block {
				t.Errorf("pin '%v' not qualified by its block", p.id)
			}
		}
	}
	for _, l := range f.links {
		claim(l.id)
		if !pins[l.from] || !pins[l.to] {
			t.Errorf("link '%v' names undeclared pins", l.id)
		}
	}
	reaper := nodeFor(t, f, "REAPER · audio in")
	if reaper.id.Block != "app:REAPER|audio|in" || reaper.pins[0].id.Port != "REAPER:in1" {
		t.Errorf("REAPER audio in = %v, first pin %v", reaper.id, reaper.pins[0].id)
	}
	if len(f.links) == 0 {
		t.Error("no links declared")
	}

	// the same workspace against the restart sample names every recorded block the same.
	again := plan(m.Reconcile(load(t, elevenRestart)), newSelection(), nil, m.HiddenClasses())
	ids := map[ID]bool{}
	for _, n := range again.nodes {
		ids[n.id] = true
	}
	for _, n := range f.nodes {
		if !strings.HasPrefix(n.id.Block, "~") && !ids[n.id] {
			t.Errorf("'%v' not declared under the same id after the restart", n.id)
		}
	}
}

// a stale view declares its blocks (drawn desaturated) and no links.
func TestStaleViewDeclaresNoLinks(t *testing.T) {
	m := openModel(t)
	snap := load(t, elevenBaseline)
	m.Reconcile(snap)
	down := *snap
	down.State = pipewire.Disconnected
	down.Error = "connection error (broken pipe)"
	v := m.Reconcile(&down)
	f := plan(v, newSelection(), nil, m.HiddenClasses())
	if !v.Stale || len(f.nodes) == 0 {
		t.Fatalf("stale %v, %d nodes", v.Stale, len(f.nodes))
	}
	if len(f.links) != 0 {
		t.Errorf("stale view declared %d links", len(f.links))
	}
}

// one drag is one model operation; a raise reorders; a selection maps back to blocks and links; a link gesture
// creates nothing and says so.
func TestIntentsApply(t *testing.T) {
	m := &counting{Model: openModel(t)}
	v := m.Reconcile(load(t, elevenBaseline))
	c := &canvas{model: m, notice: func(string) {}, sel: newSelection()}
	f := plan(v, c.sel, nil, m.HiddenClasses())
	c.order = f.order
	reaperIn := nodeFor(t, f, "REAPER · audio in")
	reaperOut := nodeFor(t, f, "REAPER · audio out")

	c.apply(dfx.Intents[ID]{
		NodeRaised: &reaperIn.id,
		NodesMoved: []dfx.NodeMove[ID]{
			{ID: reaperIn.id, From: reaperIn.pos, To: imgui.Vec2{X: 1500, Y: 100}},
			{ID: reaperOut.id, From: reaperOut.pos, To: imgui.Vec2{X: 1500, Y: 700}},
		},
		SelectionChanged: &dfx.SelectionChange[ID]{
			Nodes: []ID{reaperIn.id, reaperOut.id},
			Links: []ID{f.links[0].id},
		},
	}, f)
	if m.moves != 1 {
		t.Errorf("%d move operations for one drag, want 1", m.moves)
	}
	v = m.Refresh()
	if b, _ := v.Block(reaperIn.block); b.X != 1500 || b.Y != 100 {
		t.Errorf("REAPER audio in at (%v, %v)", b.X, b.Y)
	}
	if c.order[len(c.order)-1] != reaperIn.block {
		t.Error("raised block is not frontmost")
	}
	if !c.sel.blocks[reaperIn.block] || !c.sel.blocks[reaperOut.block] || len(c.sel.links) != 1 {
		t.Errorf("selection = %+v", c.sel)
	}

	before := len(v.Links)
	var gestured [][2]pipewire.Serial
	c.link = func(out, in pipewire.Serial) { gestured = append(gestured, [2]pipewire.Serial{out, in}) }
	c.apply(dfx.Intents[ID]{LinkCreated: &dfx.LinkCreate[ID]{FromPin: reaperOut.pins[0].id, ToPin: reaperIn.pins[0].id}}, f)
	if len(gestured) != 1 || gestured[0] != [2]pipewire.Serial{f.portOf[reaperOut.pins[0].id], f.portOf[reaperIn.pins[0].id]} {
		t.Errorf("link gesture handed over as %v", gestured)
	}
	if after := len(m.Refresh().Links); after != before {
		t.Errorf("a link gesture changed the drawn links: %d -> %d", before, after)
	}

	c.hideSelection()
	v = m.Refresh()
	if b, _ := v.Block(reaperIn.block); !b.Hidden || b.Visible {
		t.Errorf("hidden selection still drawn: %+v", b.Hidden)
	}
}

// a block with no record keeps its selection and stacking when a move gives it one, though its canvas id changes.
func TestUnrecordedBlockKeepsSelectionWhenRecorded(t *testing.T) {
	m := openModel(t)
	v := m.Reconcile(load(t, elevenBaseline))
	c := &canvas{model: m, notice: func(string) {}, sel: newSelection()}
	f := plan(v, c.sel, nil, m.HiddenClasses())
	c.order = f.order
	grd := nodeFor(t, f, "gnome-remote-desktop-daemon #1 · audio in")
	if !strings.HasPrefix(grd.id.Block, "~") {
		t.Fatalf("colliding block has a record: %v", grd.id)
	}
	c.apply(dfx.Intents[ID]{
		SelectionChanged: &dfx.SelectionChange[ID]{Nodes: []ID{grd.id}},
		NodesMoved:       []dfx.NodeMove[ID]{{ID: grd.id, From: grd.pos, To: imgui.Vec2{X: 900, Y: 900}}},
	}, f)
	f = plan(m.Refresh(), c.sel, c.order, m.HiddenClasses())
	var moved nodeDecl
	for _, n := range f.nodes {
		if n.block == grd.block {
			moved = n
		}
	}
	if strings.HasPrefix(moved.id.Block, "~") || !moved.selected || moved.pos != (imgui.Vec2{X: 900, Y: 900}) {
		t.Errorf("after the move: %+v", moved)
	}
}

// selection and stacking belong to one connection session: a view from a new session clears them, even when its
// block ids are the old ones; a stale view and a same-session view keep them.
func TestNewSessionClearsSelection(t *testing.T) {
	m := openModel(t)
	snap := load(t, elevenBaseline)
	c := &canvas{model: m, notice: func(string) {}, sel: newSelection()}

	v := m.Reconcile(snap)
	c.track(v)
	f := plan(v, c.sel, c.order, m.HiddenClasses())
	c.order = f.order
	reaperIn := nodeFor(t, f, "REAPER · audio in")
	reaperOut := nodeFor(t, f, "REAPER · audio out")
	c.apply(dfx.Intents[ID]{
		NodeRaised:       &reaperOut.id,
		SelectionChanged: &dfx.SelectionChange[ID]{Nodes: []ID{reaperIn.id, reaperOut.id}},
	}, f)
	kept := func(when string) {
		t.Helper()
		if !c.sel.blocks[reaperIn.block] || !c.sel.blocks[reaperOut.block] || len(c.order) == 0 {
			t.Errorf("%v: selection or order lost", when)
		}
	}

	// the same session: kept.
	again := *snap
	c.track(m.Reconcile(&again))
	kept("same session")

	// stale, still the old session: kept, inert.
	down := *snap
	down.State = pipewire.Disconnected
	c.track(m.Reconcile(&down))
	kept("stale view")

	// a new session with the very same block ids: cleared.
	next := *snap
	next.Session = snap.Session + 1
	v = m.Reconcile(&next)
	c.track(v)
	if len(c.sel.blocks) != 0 || len(c.order) != 0 {
		t.Errorf("new session kept selection %v, order %d", c.sel.blocks, len(c.order))
	}
	if ids := selectedIDs(v, c.sel); len(ids) != 0 {
		t.Errorf("new session declared selected nodes %v", ids)
	}
}

// blocks take their media's hue as accent (greyed when stale), links take their media's hue, owners pick the glyph,
// and hidden counts ride as separate suffixes so they can be drawn dimmed.
func TestPaletteWiring(t *testing.T) {
	m := openModel(t)
	snap := load(t, elevenBaseline)
	v := m.Reconcile(snap)
	f := plan(v, newSelection(), nil, m.HiddenClasses())

	reaperMIDI := nodeFor(t, f, "REAPER · midi in")
	if reaperMIDI.accent != midiHue || reaperMIDI.glyph != ownerGlyph(model.OwnerClient) {
		t.Errorf("REAPER midi in: accent %v glyph %q", reaperMIDI.accent, reaperMIDI.glyph)
	}
	launchpad := nodeFor(t, f, "Launchpad Pro 2 · midi out")
	if launchpad.glyph != ownerGlyph(model.OwnerBridge) {
		t.Errorf("launchpad glyph %q", launchpad.glyph)
	}
	sink := nodeFor(t, f, "Scarlett 18i20 4th Gen Multichannel · audio in")
	if sink.accent != audioHue || sink.glyph != ownerGlyph(model.OwnerDevice) {
		t.Errorf("scarlett sink: accent %v glyph %q", sink.accent, sink.glyph)
	}
	for _, l := range f.links {
		if l.color != linkHue(model.MediaAudio, "active") && l.color != linkHue(model.MediaMIDI, "active") {
			t.Errorf("active link %v colored %v", l.id, l.color)
		}
	}

	// the remote-desktop inputs carry hidden counts for the filtered scarlett monitor ports, as a separate suffix.
	grd := nodeFor(t, f, "gnome-remote-desktop-daemon #1 · audio in")
	var suffixed bool
	for _, p := range grd.pins {
		if strings.Contains(p.label, "hidden") {
			t.Errorf("hidden count inside the label %q", p.label)
		}
		if p.suffix == "· 1 hidden" {
			suffixed = true
		}
	}
	if !suffixed {
		t.Errorf("no hidden-count suffix on the remote-desktop inputs: %+v", grd.pins)
	}

	down := *snap
	down.State = pipewire.Disconnected
	stale := plan(m.Reconcile(&down), newSelection(), nil, m.HiddenClasses())
	if n := nodeFor(t, stale, "REAPER · midi in"); n.accent != grey(midiHue) {
		t.Errorf("stale accent %v, want greyed", n.accent)
	}
}

// every observed link is drawn, its state in its color: active in the media hue, settling states dimmed, error in
// the warning hue.
func TestLinkStateColors(t *testing.T) {
	active := linkHue(model.MediaAudio, "active")
	if active != (imgui.Vec4{X: audioHue.X, Y: audioHue.Y, Z: audioHue.Z, W: linkAlpha}) {
		t.Errorf("active = %v", active)
	}
	for _, state := range []string{"init", "negotiating", "allocating", "paused"} {
		c := linkHue(model.MediaAudio, state)
		if c.W != settlingAlpha || c.X != audioHue.X {
			t.Errorf("%v = %v, want the hue dimmed", state, c)
		}
	}
	if linkHue(model.MediaMIDI, "error") != warningHue {
		t.Error("an error link is not in the warning hue")
	}
}

// S rounds only the selected blocks to the grid, as one move.
func TestSnapPlacements(t *testing.T) {
	v := &model.View{Blocks: []model.Block{
		{ID: "a", X: 124, Y: 76},
		{ID: "b", X: 26, Y: -24},
		{ID: "c", X: 13, Y: 13},
	}}
	sel := newSelection()
	sel.blocks["a"], sel.blocks["b"] = true, true
	got := snapPlacements(v, sel, 50)
	want := []model.Placement{{ID: "a", X: 100, Y: 100}, {ID: "b", X: 50, Y: 0}}
	if len(got) != len(want) {
		t.Fatalf("placements = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("placement %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// S pressed right after a drag release runs before the next frame is drawn, while the last frame's declarations
// still hold the pre-drag position. the snap must land at the drop, not back where the drag started.
func TestSnapRightAfterDragKeepsTheDrop(t *testing.T) {
	m := openModel(t)
	v := m.Reconcile(load(t, elevenBaseline))
	c := &canvas{model: m, notice: func(string) {}, sel: newSelection()}
	f := plan(v, c.sel, nil, m.HiddenClasses())
	c.order = f.order
	reaper := nodeFor(t, f, "REAPER · audio in")

	// the frame that commits the drag: its declarations hold the start position.
	c.apply(dfx.Intents[ID]{
		SelectionChanged: &dfx.SelectionChange[ID]{Nodes: []ID{reaper.id}},
		NodesMoved:       []dfx.NodeMove[ID]{{ID: reaper.id, From: reaper.pos, To: imgui.Vec2{X: 1013, Y: 488}}},
	}, f)

	// S, before the next frame is drawn.
	c.snapTo(50)
	b, _ := m.Refresh().Block(reaper.block)
	if b.X != 1000 || b.Y != 500 {
		t.Errorf("snapped to (%v, %v), want (1000, 500) from the drop at (1013, 488); the drag started at (%v, %v)",
			b.X, b.Y, reaper.pos.X, reaper.pos.Y)
	}
}

// C right after a click: the selection the click reported is what gets centered, not the last frame's.
func TestCenterUsesCurrentSelection(t *testing.T) {
	m := openModel(t)
	v := m.Reconcile(load(t, elevenBaseline))
	c := &canvas{model: m, notice: func(string) {}, sel: newSelection()}
	f := plan(v, c.sel, nil, m.HiddenClasses())
	reaper := nodeFor(t, f, "REAPER · audio in")
	c.apply(dfx.Intents[ID]{SelectionChanged: &dfx.SelectionChange[ID]{Nodes: []ID{reaper.id}}}, f)
	ids := selectedIDs(m.Refresh(), c.sel)
	if len(ids) != 1 || ids[0] != reaper.id {
		t.Errorf("centering on %v, want %v", ids, reaper.id)
	}
}

// a block that appears after the initial graph is announced by title, newest first; N's target is the newest batch;
// dismissal clears the list; a new connection session clears it too.
func TestArrivalsList(t *testing.T) {
	m := openModel(t)
	full := load(t, elevenBaseline)
	var reaper []pipewire.Serial
	for s, n := range full.Nodes {
		if n.Name == "REAPER" {
			reaper = append(reaper, s)
		}
	}
	partial := *full
	partial.Nodes = map[pipewire.Serial]pipewire.Node{}
	partial.Ports = map[pipewire.Serial]pipewire.Port{}
	partial.Links = map[pipewire.Serial]pipewire.Link{}
	for s, n := range full.Nodes {
		if s != reaper[0] {
			partial.Nodes[s] = n
		}
	}
	for s, p := range full.Ports {
		if p.NodeSerial != reaper[0] {
			partial.Ports[s] = p
		}
	}

	var a arrivals
	now := time.Now()
	a.record(m.Reconcile(&partial), now)
	if a.announcement(now) != "" || len(a.list) != 0 {
		t.Fatalf("the initial graph was announced: %q", a.announcement(now))
	}

	later := now.Add(time.Second)
	a.record(m.Reconcile(full), later)
	line := a.announcement(later)
	if !strings.Contains(line, "REAPER · audio in") || !strings.Contains(line, "REAPER · midi out") {
		t.Errorf("announcement = %q", line)
	}
	if got := a.newest(); len(got) != 4 {
		t.Errorf("newest batch = %v, want REAPER's four blocks", got)
	}
	if a.announcement(later.Add(arrivalsShown+time.Second)) != "" {
		t.Error("the announcement outlived its time")
	}

	a.dismiss()
	if a.announcement(later) != "" || len(a.newest()) != 0 {
		t.Error("dismissal did not clear the arrivals")
	}

	a.record(m.Reconcile(&partial), later)
	a.record(m.Reconcile(full), later.Add(time.Second))
	next := *full
	next.Session = full.Session + 1
	a.record(m.Reconcile(&next), later.Add(2*time.Second))
	if len(a.list) != 0 {
		t.Errorf("arrivals survived a new connection session: %v", a.list)
	}
}

func TestStatusLines(t *testing.T) {
	m := openModel(t)
	snap := load(t, elevenBaseline)
	live := m.Reconcile(snap)
	lines := statusLines(live, snap, "", "")
	if len(lines) != 1 || lines[0] != "live" {
		t.Errorf("live = %q", lines)
	}

	unresolved := *snap
	unresolved.Unresolved = 2
	if l := statusLines(live, &unresolved, "", "")[0]; !strings.Contains(l, "2 unresolved references") {
		t.Errorf("unresolved = %q", l)
	}

	down := *snap
	down.State = pipewire.Disconnected
	down.Error = "connection error (broken pipe)"
	stale := m.Reconcile(&down)
	l := statusLines(stale, &down, "", "")[0]
	for _, want := range []string{"disconnected", "broken pipe", "generation 1", "not current"} {
		if !strings.Contains(l, want) {
			t.Errorf("stale line %q lacks %q", l, want)
		}
	}

	if lines := statusLines(live, snap, "samples/x", "patching lands in stage 4"); len(lines) != 2 ||
		!strings.Contains(lines[0], "stage 4") || !strings.Contains(lines[1], "read-only") {
		t.Errorf("sample = %q", lines)
	}
}

// fakePatcher records the requests the ui posts.
type fakePatcher struct {
	creates  [][3]uint64
	destroys [][2]uint64
	quanta   []int
	resets   int
	next     pipewire.RequestID
}

func (p *fakePatcher) CreateLink(session uint64, out, in pipewire.Serial) pipewire.RequestID {
	p.creates = append(p.creates, [3]uint64{session, uint64(out), uint64(in)})
	p.next++
	return p.next
}

func (p *fakePatcher) DestroyLink(session uint64, link pipewire.Serial) pipewire.RequestID {
	p.destroys = append(p.destroys, [2]uint64{session, uint64(link)})
	p.next++
	return p.next
}

func (p *fakePatcher) SetForceQuantum(frames int) pipewire.RequestID {
	p.quanta = append(p.quanta, frames)
	p.next++
	return p.next
}

func (p *fakePatcher) ResetMetricsBaseline() { p.resets++ }

func aliasSerial(t *testing.T, s *pipewire.Snapshot, alias string, d pipewire.Direction) pipewire.Serial {
	t.Helper()
	for serial, p := range s.Ports {
		if p.Alias == alias && p.Direction == d {
			return serial
		}
	}
	t.Fatalf("no port '%v'", alias)
	return 0
}

// a link gesture is validated by the model and only then posted, against the session the operator saw; sample mode
// and a stale graph post nothing and say why; Delete posts one destroy per selected link.
func TestPatchingGestures(t *testing.T) {
	m := openModel(t)
	snap := load(t, elevenBaseline)
	v := m.Reconcile(snap)
	var notices []string
	note := func(s string) { notices = append(notices, s) }
	fp := &fakePatcher{}
	pt := newPatching(fp, m, note)

	out := aliasSerial(t, snap, "Launchpad Pro 2:Launchpad Pro 2 Live Port", pipewire.DirectionOut)
	in := aliasSerial(t, snap, "REAPER:MIDI Input 4", pipewire.DirectionIn)
	pt.link(v, out, in)
	if len(fp.creates) != 1 || fp.creates[0] != [3]uint64{v.Session, uint64(out), uint64(in)} {
		t.Fatalf("creates = %v", fp.creates)
	}
	if d := pt.descs[1]; !strings.Contains(d, "Launchpad Pro 2") || !strings.Contains(d, "REAPER") {
		t.Errorf("description %q", d)
	}

	audio := aliasSerial(t, snap, "REAPER:out1", pipewire.DirectionOut)
	pt.link(v, audio, in)
	if len(fp.creates) != 1 || len(notices) != 1 || !strings.Contains(notices[0], "link refused") {
		t.Errorf("an invalid link was posted or not explained: creates %v notices %v", fp.creates, notices)
	}

	readOnly := newPatching(nil, m, note)
	readOnly.link(v, out, in)
	readOnly.unlink(v, []pipewire.Serial{v.Links[0].Serial})
	if !strings.Contains(notices[len(notices)-1], "read-only") {
		t.Errorf("sample mode did not say it is read-only: %v", notices)
	}

	pt.unlink(v, []pipewire.Serial{v.Links[1].Serial, v.Links[0].Serial})
	if len(fp.destroys) != 2 || fp.destroys[0][1] != uint64(v.Links[0].Serial) || fp.destroys[0][0] != v.Session {
		t.Errorf("destroys = %v", fp.destroys)
	}
}

// a link gesture or Delete on a stale view posts nothing and records a "not connected" entry in the request list.
func TestStaleGesturePostsNothing(t *testing.T) {
	m := openModel(t)
	snap := load(t, elevenBaseline)
	m.Reconcile(snap)
	c := &canvas{model: m, notice: func(string) {}, sel: newSelection()}
	down := *snap
	down.State = pipewire.Disconnected
	stale := m.Reconcile(&down)
	f := plan(stale, c.sel, nil, m.HiddenClasses())

	fp := &fakePatcher{}
	pt := newPatching(fp, m, func(string) {})
	now := time.Unix(9000, 0)
	pt.now = func() time.Time { return now }
	c.link = func(out, in pipewire.Serial) { pt.link(stale, out, in) }
	reaperOut := nodeFor(t, f, "REAPER · audio out")
	reaperIn := nodeFor(t, f, "REAPER · audio in")
	c.apply(dfx.Intents[ID]{LinkCreated: &dfx.LinkCreate[ID]{FromPin: reaperOut.pins[0].id, ToPin: reaperIn.pins[0].id}}, f)
	pt.unlink(stale, []pipewire.Serial{stale.Links[0].Serial})

	if len(fp.creates)+len(fp.destroys) != 0 {
		t.Errorf("posted on a stale view: %v %v", fp.creates, fp.destroys)
	}
	lines := pt.requestLines(&down, now)
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "failed: link REAPER") || !strings.HasSuffix(lines[0], ": not connected") ||
		!strings.HasPrefix(lines[1], "failed: unlink") || !strings.HasSuffix(lines[1], ": not connected") {
		t.Errorf("request list = %q", lines)
	}
	if later := pt.requestLines(&down, now.Add(failedShown+time.Second)); len(later) != 0 {
		t.Errorf("refusals outlived their time: %q", later)
	}
}

// the status strip and the inspector list pending requests and recent failures, from the snapshot; confirmed and
// old failures are not listed.
func TestRequestLines(t *testing.T) {
	now := time.Unix(5000, 0)
	pt := newPatching(&fakePatcher{}, nil, func(string) {})
	pt.descs[1] = "link a → b"
	snap := &pipewire.Snapshot{Requests: []pipewire.Request{
		{ID: 1, State: pipewire.RequestPending, Posted: now.Add(-500 * time.Millisecond)},
		{ID: 2, State: pipewire.RequestFailed, Reason: "wrong route", Resolved: now.Add(-time.Second), OutPort: 7, InPort: 8},
		{ID: 3, State: pipewire.RequestFailed, Reason: "old", Resolved: now.Add(-failedShown - time.Second)},
		{ID: 4, State: pipewire.RequestConfirmed, Resolved: now},
	}}
	lines := pt.requestLines(snap, now)
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "pending: link a → b") ||
		lines[1] != "failed: link 7 → 8: wrong route" {
		t.Errorf("lines = %q", lines)
	}
}

// the inspector finds the default metadata entries that name a node.
func TestDefaultsNaming(t *testing.T) {
	snap := load(t, elevenBaseline)
	for _, n := range snap.Nodes {
		if strings.Contains(n.Name, "Scarlett") && n.MediaClass == "Audio/Sink" {
			names := defaultsNaming(snap, n)
			if len(names) == 0 || !strings.Contains(strings.Join(names, " "), "default.audio.sink") {
				t.Errorf("default metadata naming the sink = %v", names)
			}
			return
		}
	}
	t.Fatal("no scarlett sink")
}

// the inspector attributes default metadata by resolved serial: an entry whose subject resolved to a node names it,
// a stale entry under that node's reused id does not, and the metadata listing says when a subject is unresolved.
func TestDefaultsBySerial(t *testing.T) {
	node := pipewire.Node{Serial: 1611, ID: 161, Name: "another stream"}
	snap := &pipewire.Snapshot{
		Nodes: map[pipewire.Serial]pipewire.Node{1611: node},
		Default: []pipewire.MetadataEntry{
			{Subject: 161, SubjectSerial: 0, Key: "target.node", Value: "-1"},
			{Subject: 161, SubjectSerial: 1611, Key: "target.object", Value: "x"},
			{Subject: 0, Key: "default.audio.sink", Value: `{"name":"another stream"}`},
		},
	}
	names := strings.Join(defaultsNaming(snap, node), "; ")
	if strings.Contains(names, "target.node") {
		t.Errorf("a stale entry under the reused id was attributed: %q", names)
	}
	if !strings.Contains(names, "target.object") || !strings.Contains(names, "default.audio.sink") {
		t.Errorf("entries naming the node by serial or name missing: %q", names)
	}
	rows := defaultsRows(snap)
	want := map[string][2]string{
		"target.node":        {"subject unresolved (id 161)", "-1"},
		"target.object":      {"another stream · serial 1611", "x"},
		"default.audio.sink": {"global", `{"name":"another stream"}`},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v", rows)
	}
	for _, r := range rows {
		if w := want[r.key]; r.subject != w[0] || r.value != w[1] {
			t.Errorf("row %q = (%q, %q), want (%q, %q)", r.key, r.subject, r.value, w[0], w[1])
		}
	}
}

// the properties filter narrows keys by substring, sorted; an empty filter keeps every key.
func TestPropertyFilter(t *testing.T) {
	props := map[string]string{"node.name": "a", "node.nick": "b", "media.class": "c", "object.serial": "d"}
	if got := strings.Join(filteredKeys(props, "node."), ","); got != "node.name,node.nick" {
		t.Errorf("filtered = %q", got)
	}
	if got := filteredKeys(props, ""); len(got) != 4 || got[0] != "media.class" {
		t.Errorf("unfiltered = %v", got)
	}
}

// the inspector resolves selection serials only against the snapshot the drawn view came from: the current one for a
// live view, the retained last-live one for a stale view of the same session and generation, otherwise nothing,
// even when a new session's snapshot reuses the selected serial for something else.
func TestInspectSource(t *testing.T) {
	m := openModel(t)
	snap := load(t, elevenBaseline)
	live := m.Reconcile(snap)
	if inspectSource(live, snap, snap) != snap {
		t.Error("a live view does not resolve against the current snapshot")
	}

	var selected pipewire.Link
	for _, l := range snap.Links {
		selected = l
		break
	}
	next := *snap
	next.Session = snap.Session + 1
	next.Generation = snap.Generation + 5
	next.State = pipewire.Connecting
	next.Links = map[pipewire.Serial]pipewire.Link{selected.Serial: {Serial: selected.Serial, ID: 999, State: "init"}}
	stale := m.Reconcile(&next)
	if !stale.Stale {
		t.Fatal("view not stale")
	}
	got := inspectSource(stale, &next, snap)
	if got != snap {
		t.Fatalf("a stale view resolved against %p, want the last live snapshot", got)
	}
	if l := got.Links[selected.Serial]; l.ID != selected.ID {
		t.Errorf("the selected link resolved to id %d from the new session, want %d", l.ID, selected.ID)
	}

	other := *snap
	other.Generation = snap.Generation + 1
	if inspectSource(stale, &next, &other) != nil {
		t.Error("a stale view resolved against a snapshot of another generation")
	}
	if inspectSource(stale, &next, nil) != nil {
		t.Error("a stale view with nothing retained resolved against the current snapshot")
	}
}

// I toggles the inspector, and brings back a panel whose width has fallen below its collapsed width.
func TestToggleInspector(t *testing.T) {
	p := dfx.NewHCollapse(nil, dfx.HCollapseConfig{ExpandedWidth: inspectorWidth, Expanded: true, Anchor: dfx.AnchorRight})
	toggleInspector(p)
	if p.Expanded {
		t.Error("the toggle did not collapse an expanded inspector")
	}
	toggleInspector(p)
	if !p.Expanded {
		t.Error("the toggle did not expand a collapsed inspector")
	}

	for _, expanded := range []bool{true, false} {
		lost := dfx.NewHCollapse(nil, dfx.HCollapseConfig{ExpandedWidth: inspectorWidth, Expanded: expanded, Anchor: dfx.AnchorRight})
		lost.CurrentWidth, lost.ExpandedWidth = 4, 4
		toggleInspector(lost)
		if !lost.Expanded || lost.ExpandedWidth != inspectorWidth || lost.CurrentWidth < lost.MinWidth {
			t.Errorf("expanded %v: the toggle left a lost panel at expanded %v, width %v/%v",
				expanded, lost.Expanded, lost.CurrentWidth, lost.ExpandedWidth)
		}
	}
}

func TestQuantumChoices(t *testing.T) {
	got := quantumChoices(pipewire.Settings{MinQuantum: 32, MaxQuantum: 2048})
	want := []int{0, 32, 64, 128, 256, 512, 1024, 2048}
	if len(got) != len(want) {
		t.Fatalf("choices = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("choices = %v, want %v", got, want)
		}
	}
	if c := quantumChoices(pipewire.Settings{}); len(c) != 1 || c[0] != 0 {
		t.Errorf("without settings = %v, want automatic only", c)
	}
	if quantumLabel(0) != "automatic" || quantumLabel(256) != "256" {
		t.Error("labels")
	}
}

// beneath the control: the requested override as the settings metadata shows it, each driver's observed quantum, and
// the scope sentence; nothing as current while disconnected; nothing live in sample mode.
func TestQuantumLines(t *testing.T) {
	m := openModel(t)
	snap := load(t, elevenBaseline)
	live := *snap
	live.Settings.ForceQuantum = 256
	live.Settings.ForceSeen = true
	live.Metrics = pipewire.MetricsSummary{Available: true, Drivers: []pipewire.DriverMetrics{
		{Name: "alsa_output.scarlett", Quantum: 256, Rate: 48000},
		{Name: "alsa_input.webcam", Quantum: 128, Rate: 48000},
	}}
	v := m.Reconcile(&live)
	lines := strings.Join(quantumLines(v, &live, false), "\n")
	for _, want := range []string{"requested override: 256 frames", "observed: 'alsa_output.scarlett' runs 256 frames at 48000 Hz (5.33 ms cycle)",
		"observed: 'alsa_input.webcam' runs 128 frames", quantumScope} {
		if !strings.Contains(lines, want) {
			t.Errorf("lines lack %q:\n%s", want, lines)
		}
	}
	unseen := live
	unseen.Settings.ForceSeen = false
	if l := quantumLines(m.Reconcile(&unseen), &unseen, false)[0]; l != "requested override: unknown (the settings metadata does not show clock.force-quantum)" {
		t.Errorf("unseen = %q", l)
	}
	released := live
	released.Settings.ForceQuantum = 0
	if l := quantumLines(m.Reconcile(&released), &released, false)[0]; !strings.Contains(l, "none (automatic releases it") {
		t.Errorf("released = %q", l)
	}

	down := live
	down.State = pipewire.Disconnected
	if l := quantumLines(m.Reconcile(&down), &down, false); len(l) != 1 || l[0] != "quantum and monitoring: not connected" {
		t.Errorf("disconnected = %q", l)
	}
	if l := quantumLines(v, &live, true); len(l) != 1 || !strings.Contains(l[0], "sample mode") {
		t.Errorf("sample = %q", l)
	}
}

func TestMetricsLine(t *testing.T) {
	at := time.Date(2026, 10, 6, 14, 3, 22, 0, time.Local)
	if l := metricsLine(pipewire.MetricsSummary{Available: true, Total: 12, New: 3, LastIncrease: at}); l != "xruns over currently tracked nodes: 12 total, 3 new · last increase 14:03:22" {
		t.Errorf("line = %q", l)
	}
	if l := metricsLine(pipewire.MetricsSummary{Available: true, Total: 4}); !strings.HasSuffix(l, "no increase observed") {
		t.Errorf("line = %q", l)
	}
}

// with the profiler not bound, the strip and the inspector say monitoring is unavailable rather than showing zero
// counts or no drivers as observed; with it bound, they show the record.
func TestMonitoringUnavailable(t *testing.T) {
	m := openModel(t)
	snap := load(t, elevenBaseline)
	live := *snap
	live.Settings.ForceSeen = true
	live.Metrics = pipewire.MetricsSummary{Nodes: map[pipewire.Serial]pipewire.NodeMetrics{7: {Available: true, Total: 4, ClockGuard: true}}}
	v := m.Reconcile(&live)

	if l := metricsLine(live.Metrics); l != "monitoring unavailable (no profiler)" {
		t.Errorf("unavailable line = %q", l)
	}
	lines := strings.Join(quantumLines(v, &live, false), "\n")
	if !strings.Contains(lines, "monitoring unavailable (no profiler)") || strings.Contains(lines, "observed:") {
		t.Errorf("unavailable lines:\n%s", lines)
	}
	if rows := xrunRows(live.Metrics, 7); len(rows) != 1 || rows[0][1] != "monitoring unavailable (no profiler)" {
		t.Errorf("unavailable inspector rows = %v", rows)
	}

	live.Metrics.Available = true
	lines = strings.Join(quantumLines(v, &live, false), "\n")
	if strings.Contains(lines, "unavailable") || !strings.Contains(lines, "observed: no driver with running followers") {
		t.Errorf("available lines:\n%s", lines)
	}
	if rows := xrunRows(live.Metrics, 7); len(rows) != 4 || rows[0] != [2]string{"total", "4"} || rows[3][1] != "lifetime guard: clock-based" {
		t.Errorf("available inspector rows = %v", rows)
	}
	if rows := xrunRows(live.Metrics, 8); len(rows) != 1 || rows[0][1] != "no profiler data for this node yet" {
		t.Errorf("untracked inspector rows = %v", rows)
	}
}

// the control posts only the operator's choice; a stale view refuses; reset reaches the backend.
func TestQuantumPosting(t *testing.T) {
	m := openModel(t)
	snap := load(t, elevenBaseline)
	v := m.Reconcile(snap)
	fp := &fakePatcher{}
	pt := newPatching(fp, m, func(string) {})
	pt.setQuantum(v, 256)
	pt.setQuantum(v, 0)
	if len(fp.quanta) != 2 || fp.quanta[0] != 256 || fp.quanta[1] != 0 {
		t.Errorf("posted = %v", fp.quanta)
	}
	if d := pt.descs[2]; !strings.Contains(d, "automatic") {
		t.Errorf("description %q", d)
	}
	down := *snap
	down.State = pipewire.Disconnected
	pt.setQuantum(m.Reconcile(&down), 64)
	if len(fp.quanta) != 2 {
		t.Error("posted on a stale view")
	}
	pt.resetBaseline()
	if fp.resets != 1 {
		t.Error("reset did not reach the backend")
	}
}

// with clock.force-quantum unseen, the control shows unknown and every choice posts, automatic included; with it
// seen, choosing the value already shown posts nothing.
func TestQuantumUnseenOverride(t *testing.T) {
	m := openModel(t)
	v := m.Reconcile(load(t, elevenBaseline))
	fp := &fakePatcher{}
	pt := newPatching(fp, m, func(string) {})

	unseen := pipewire.Settings{Present: true, MinQuantum: 32, MaxQuantum: 2048}
	if p := quantumPreview(unseen); p != "unknown" {
		t.Errorf("unseen preview = %q", p)
	}
	pt.chooseQuantum(v, unseen, 0)
	if len(fp.quanta) != 1 || fp.quanta[0] != 0 {
		t.Fatalf("unseen automatic posted %v, want [0]", fp.quanta)
	}

	seen := unseen
	seen.ForceSeen = true
	if p := quantumPreview(seen); p != "automatic" {
		t.Errorf("seen preview = %q", p)
	}
	pt.chooseQuantum(v, seen, 0)
	if len(fp.quanta) != 1 {
		t.Errorf("seen automatic posted again: %v", fp.quanta)
	}
	pt.chooseQuantum(v, seen, 256)
	if len(fp.quanta) != 2 || fp.quanta[1] != 256 {
		t.Errorf("seen 256 posted %v", fp.quanta)
	}
}
