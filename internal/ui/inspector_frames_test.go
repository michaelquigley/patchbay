package ui

import (
	"runtime"
	"testing"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/dfx"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// every inspector view draws through real imgui frames, headless, without unbalancing a stack: a push without a pop,
// a table or child left open, or a tree node left unpopped would assert.
func TestInspectorFramesBalance(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext()
	defer imgui.DestroyContextV(ctx)
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 1000, Y: 800})
	io.SetDeltaTime(1.0 / 60)
	io.SetBackendFlags(imgui.BackendFlagsRendererHasTextures)
	dfx.SetupFonts()

	m := openModel(t)
	snap := load(t, elevenBaseline)
	v := m.Reconcile(snap)
	reaper := nodeFor(t, plan(v, newSelection(), nil, m.HiddenClasses()), "REAPER · audio in")
	if err := m.SetPortHidden(reaper.block, "REAPER:in1", true); err != nil {
		t.Fatal(err)
	}
	v = m.Refresh()
	withMetrics := *snap
	withMetrics.Metrics = pipewire.MetricsSummary{Available: true, Nodes: map[pipewire.Serial]pipewire.NodeMetrics{}}
	for serial, n := range snap.Nodes {
		if n.Name == "REAPER" {
			withMetrics.Metrics.Nodes[serial] = pipewire.NodeMetrics{Available: true, Total: 4, New: 1}
		}
	}
	snap = &withMetrics
	in := newInspector(m, nil)
	in.filter = "node"

	block := newSelection()
	block.blocks[reaper.block] = true
	link := newSelection()
	link.links[v.Links[0].Serial] = true
	both := newSelection()
	both.blocks[reaper.block] = true
	both.links[v.Links[0].Serial] = true
	down := *snap
	down.State = pipewire.Disconnected
	stale := m.Reconcile(&down)

	cases := []struct {
		name string
		draw func()
	}{
		{"nothing selected", func() { in.draw(v, snap, newSelection(), nil) }},
		{"one block", func() { in.draw(v, snap, block, []string{"pending: link a → b (0.5s)"}) }},
		{"one link", func() { in.draw(v, snap, link, nil) }},
		{"several", func() { in.draw(v, snap, both, nil) }},
		{"stale, unavailable", func() { in.draw(stale, nil, block, nil) }},
	}
	for _, c := range cases {
		for frame := 0; frame < 2; frame++ {
			imgui.NewFrame()
			imgui.SetNextWindowPos(imgui.Vec2{})
			imgui.SetNextWindowSize(imgui.Vec2{X: 380, Y: 800})
			imgui.BeginV("inspector-test", nil, imgui.WindowFlagsNoDecoration|imgui.WindowFlagsNoMove)
			c.draw()
			imgui.End()
			imgui.Render()
		}
	}
}

// the performance panel and the toolbar summary draw through real imgui frames, headless, in every state, with their
// tables and children balanced.
func TestStatusFramesBalance(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext()
	defer imgui.DestroyContextV(ctx)
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 1400, Y: 900})
	io.SetDeltaTime(1.0 / 60)
	io.SetBackendFlags(imgui.BackendFlagsRendererHasTextures)
	dfx.SetupFonts()

	m := openModel(t)
	snap := load(t, elevenBaseline)
	live := *snap
	live.Settings = pipewire.Settings{Present: true, MinQuantum: 32, MaxQuantum: 2048, ForceSeen: true, ForceQuantum: 256}
	live.Metrics = pipewire.MetricsSummary{Available: true, Total: 3, New: 1, Drivers: []pipewire.DriverMetrics{{Name: "alsa_output.scarlett", Quantum: 256, Rate: 48000}}}
	unbound := live
	unbound.Metrics = pipewire.MetricsSummary{}
	down := live
	down.State = pipewire.Disconnected

	a := &app{model: m}
	a.performance = dfx.NewHCollapse(nil, dfx.HCollapseConfig{ExpandedWidth: performanceWidth, Expanded: true})
	a.patching = newPatching(&fakePatcher{}, m, a.setNotice)
	a.setNotice("link refused: same direction")
	a.setNotice("another notice")
	now := time.Now()
	cases := []struct {
		name   string
		sample string
		snap   *pipewire.Snapshot
	}{
		{"live", "", &live},
		{"profiler unbound", "", &unbound},
		{"disconnected", "", &down},
		{"sample", "samples/x", snap},
	}
	for _, c := range cases {
		a.opts.Sample = c.sample
		v := m.Reconcile(c.snap)
		events := a.events.collect(a.patching, &a.arrivals, c.snap, now)
		for frame := 0; frame < 2; frame++ {
			imgui.NewFrame()
			imgui.SetNextWindowPos(imgui.Vec2{})
			imgui.SetNextWindowSize(imgui.Vec2{X: performanceWidth, Y: 800})
			imgui.BeginV("status-test", nil, imgui.WindowFlagsNoDecoration|imgui.WindowFlagsNoMove)
			a.drawToolbarStatus(newToolbarStatus(v, c.sample, events), now)
			a.drawPerformance(v, c.snap, events, now)
			imgui.End()
			imgui.Render()
		}
	}
}
