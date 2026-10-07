package ui

import (
	"runtime"
	"testing"

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
	in := newInspector(m)
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
