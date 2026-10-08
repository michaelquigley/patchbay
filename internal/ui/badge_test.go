package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/dfx"
	"github.com/michaelquigley/patchbay/internal/pipewire"
	"github.com/michaelquigley/patchbay/internal/sample"
)

const profilerType = "PipeWire:Interface:Profiler"

// a synthetic feed through the backend's own graph: a captured graph, a bound profiler, and two profiler pods in which
// REAPER's counter goes from 0 to 3, replayed into a snapshot. REAPER's blocks carry the badge and no other block
// does; a reset clears it; without a profiler, or on a stale view, nothing is badged.
func TestXrunBadges(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("../../samples", elevenBaseline, sample.DumpFile))
	if err != nil {
		t.Fatal(err)
	}
	all, err := sample.Inputs(data)
	if err != nil {
		t.Fatal(err)
	}
	// the capture carries the daemon's own Profiler global; it is left out, so each case says whether one is bound.
	var inputs []pipewire.Input
	for _, in := range all {
		if g, ok := in.(pipewire.GlobalAdded); ok && g.Type == profilerType {
			continue
		}
		inputs = append(inputs, in)
	}
	var reaper, driver pipewire.Node
	for _, n := range pipewire.Replay(inputs).Nodes {
		switch {
		case n.Name == "REAPER":
			reaper = n
		case n.MediaClass == "Audio/Sink" && driver.ID == 0:
			driver = n
		}
	}
	if reaper.ID == 0 || driver.ID == 0 {
		t.Fatalf("sample lacks REAPER (%d) or a sink to drive it (%d)", reaper.ID, driver.ID)
	}
	pod := func(nsec int64, xruns uint32) pipewire.ProfilePoint {
		return pipewire.ProfilePoint{HasClock: true, Nsec: nsec, Quantum: 256, RateDenom: 48000, HasDriver: true,
			Driver:    pipewire.ProfileBlock{ID: driver.ID, HasXruns: true},
			Followers: []pipewire.ProfileBlock{{ID: reaper.ID, HasXruns: true, Xruns: xruns}}}
	}
	profiler := pipewire.GlobalAdded{ID: 90000, Type: profilerType, Version: 3,
		Props: map[string]string{"object.serial": "900000"}}
	feed := func(extra ...pipewire.Input) *pipewire.Snapshot {
		in := append(append([]pipewire.Input{}, inputs...), extra...)
		return pipewire.Replay(append(in, pipewire.Tick{}))
	}
	badges := func(snap *pipewire.Snapshot) map[pipewire.Serial][]string {
		m := openModel(t)
		v := m.Reconcile(snap)
		f := plan(v, newSelection(), nil, m.HiddenClasses())
		f.badgeXruns(v, snap.Metrics)
		out := map[pipewire.Serial][]string{}
		for _, n := range f.nodes {
			if n.badge != "" {
				out[n.node] = append(out[n.node], n.badge)
			}
		}
		return out
	}

	live := feed(profiler, pod(1000, 0), pod(2000, 3))
	if n := live.Metrics.Nodes[reaper.Serial]; !live.Metrics.Available || n.New != 3 {
		t.Fatalf("the feed did not count REAPER's xruns: %+v", live.Metrics)
	}
	got := badges(live)
	if len(got) != 1 || len(got[reaper.Serial]) < 2 {
		t.Fatalf("badges = %v, want only REAPER's blocks", got)
	}
	for _, b := range got[reaper.Serial] {
		if b != "+3 xruns" {
			t.Errorf("badge = %q", b)
		}
	}

	if got := badges(feed(profiler, pod(1000, 0), pod(2000, 3), pipewire.ResetBaseline{})); len(got) != 0 {
		t.Errorf("badges after a reset = %v", got)
	}
	if got := badges(feed(pod(1000, 0), pod(2000, 3))); len(got) != 0 {
		t.Errorf("badges without a profiler = %v", got)
	}
	down := *live
	down.State = pipewire.Disconnected
	if got := badges(&down); len(got) != 0 {
		t.Errorf("badges on a stale view = %v", got)
	}
	if xrunBadge(1) != "+1 xrun" {
		t.Errorf("singular = %q", xrunBadge(1))
	}

	// the badged canvas draws through real imgui frames, headless, at the editing detent and below it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext()
	defer imgui.DestroyContextV(ctx)
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 1200, Y: 800})
	io.SetDeltaTime(1.0 / 60)
	io.SetBackendFlags(imgui.BackendFlagsRendererHasTextures)
	dfx.SetupFonts()
	m := openModel(t)
	c := newCanvas(m, func(string) {}, func(_, _ pipewire.Serial) {})
	defer c.destroy()
	v := m.Reconcile(live)
	for _, zoom := range []float32{1.0, 0.5} {
		for frame := 0; frame < 2; frame++ {
			imgui.NewFrame()
			imgui.SetNextWindowPos(imgui.Vec2{})
			imgui.SetNextWindowSize(imgui.Vec2{X: 1200, Y: 800})
			imgui.BeginV("canvas-test", nil, imgui.WindowFlagsNoDecoration|imgui.WindowFlagsNoMove)
			if frame == 0 && c.started {
				c.nc.SetView(dfx.View{Zoom: zoom})
			}
			c.draw(&dfx.State{Size: imgui.Vec2{X: 1200, Y: 800}}, v, live.Metrics)
			imgui.End()
			imgui.Render()
		}
	}
}
