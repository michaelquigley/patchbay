package ui

import (
	"math"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/dfx"
	"github.com/michaelquigley/patchbay/internal/model"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// fit through real imgui frames, save, and reopen twice. allow subpixel font-layout rounding, but no visible drift.
func TestFitAndReopenDoesNotDrift(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	snap := load(t, "fortyfive-20261002-122347-desktop-baseline")
	seed, err := model.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seed.Reconcile(snap)
	seed.Close()
	var saved dfx.View
	var screen imgui.Vec2
	for run := 0; run < 3; run++ {
		func() {
			ctx := imgui.CreateContext()
			defer imgui.DestroyContextV(ctx)
			io := imgui.CurrentIO()
			io.SetIniFilename("")
			io.SetDisplaySize(imgui.Vec2{X: 1720, Y: 1371})
			io.SetDeltaTime(1.0 / 60)
			io.SetBackendFlags(imgui.BackendFlagsRendererHasTextures)
			dfx.SetupFonts()
			dfx.DefaultStyle()
			dfx.SetTheme(&dfx.ModernTheme{})
			m, err := model.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			a := &app{model: m, src: sampleSource{snap: snap}, view: &model.View{Stale: true}}
			a.inspector = newInspector(m, nil)
			a.patching = newPatching(nil, m, a.setNotice)
			a.canvas = newCanvas(m, a.setNotice, func(_, _ pipewire.Serial) {})
			defer a.canvas.destroy()
			a.restoreLayout()
			if run == 0 {
				a.panel.Toggle()
				a.performance.Toggle()
			}
			for frame := 0; frame < 40; frame++ {
				imgui.NewFrame()
				imgui.SetNextWindowPos(imgui.Vec2{})
				imgui.SetNextWindowSize(imgui.Vec2{X: 1720, Y: 1371})
				imgui.BeginV("##dfx_root", nil, imgui.WindowFlagsAlwaysAutoResize|imgui.WindowFlagsNoSavedSettings|imgui.WindowFlagsNoTitleBar|imgui.WindowFlagsNoScrollbar|imgui.WindowFlagsNoScrollWithMouse)
				if frame == 10 {
					gotScreen := a.canvas.nc.ScreenFromCanvas(imgui.Vec2{})
					if run > 0 && (math.Abs(float64(gotScreen.X-screen.X)) > 0.1 || math.Abs(float64(gotScreen.Y-screen.Y)) > 0.1) {
						t.Fatalf("run %d reopening changed screen position: got %+v want %+v", run, a.canvas.nc.ScreenFromCanvas(imgui.Vec2{}), screen)
					}
					a.canvas.fit()
				}
				a.draw(&dfx.State{Size: imgui.Vec2{X: 1720, Y: 1371}, App: &dfx.App{}})
				imgui.End()
				imgui.Render()
			}
			if run == 0 {
				saved = a.canvas.nc.View()
				screen = a.canvas.nc.ScreenFromCanvas(imgui.Vec2{})
			} else if got := a.canvas.nc.View(); got.Zoom != saved.Zoom ||
				math.Abs(float64((got.Pan.X-saved.Pan.X)*got.Zoom)) > 0.1 || math.Abs(float64((got.Pan.Y-saved.Pan.Y)*got.Zoom)) > 0.1 {
				t.Fatalf("restored view/screen %+v %+v; want %+v %+v", a.canvas.nc.View(), a.canvas.nc.ScreenFromCanvas(imgui.Vec2{}), saved, screen)
			}
		}()
	}
}
