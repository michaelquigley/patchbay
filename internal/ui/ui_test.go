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
	var notices []string
	c := &canvas{model: m, notice: func(s string) { notices = append(notices, s) }, sel: newSelection()}
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
	c.apply(dfx.Intents[ID]{LinkCreated: &dfx.LinkCreate[ID]{FromPin: reaperOut.pins[0].id, ToPin: reaperIn.pins[0].id}}, f)
	if len(notices) != 1 || !strings.Contains(notices[0], "stage 4") {
		t.Errorf("notices = %v", notices)
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
		if l.color != linkHue(model.MediaAudio) && l.color != linkHue(model.MediaMIDI) {
			t.Errorf("link %v colored %v", l.id, l.color)
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
