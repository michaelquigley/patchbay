package ui

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/dfx"
	"github.com/michaelquigley/patchbay/internal/model"
	"github.com/michaelquigley/patchbay/internal/pipewire"
	"github.com/michaelquigley/patchbay/internal/sample"
)

// Options select what the window shows.
type Options struct {
	// Sample runs the ui against a captured sample directory, read-only, instead of the live daemon.
	Sample string
	// Workspace overrides the workspace file. empty means the default; in sample mode the default is a separate
	// sample-workspace.yaml beside it, so a capture never writes its records into the live workspace.
	Workspace string
}

const noticeDuration = 6 * time.Second

// source is where snapshots come from: the live backend, or a fixed sample.
type source interface {
	Snapshot() *pipewire.Snapshot
	Events() <-chan pipewire.Event
	Close()
}

type sampleSource struct{ snap *pipewire.Snapshot }

func (s sampleSource) Snapshot() *pipewire.Snapshot  { return s.snap }
func (s sampleSource) Events() <-chan pipewire.Event { return nil }
func (s sampleSource) Close()                        {}

type app struct {
	opts      Options
	src       source
	model     *model.Model
	canvas    *canvas
	patching  *patching
	inspector *inspector
	panel     *dfx.HCollapse

	// this frame's snapshot and view. actions run before the frame is drawn, so they act on the ones last drawn,
	// which is what the operator saw.
	snap *pipewire.Snapshot
	view *model.View
	// lastLive is the last snapshot a live view was built from: what the inspector resolves a stale view against.
	lastLive *pipewire.Snapshot

	notice   string
	noticeAt time.Time
	arrivals arrivals
	signals  chan os.Signal
}

// Run opens the window and blocks until it is closed. it must be called on the main goroutine, locked to the main os
// thread.
func Run(opts Options) error {
	path, err := workspacePath(opts)
	if err != nil {
		return err
	}
	m, err := model.Open(path)
	if err != nil {
		return err
	}
	var src source
	var p patcher // nil in sample mode: a capture is read-only
	if opts.Sample != "" {
		snap, err := sample.Load(opts.Sample)
		if err != nil {
			m.Close()
			return err
		}
		src = sampleSource{snap: snap}
	} else {
		conn := pipewire.Connect()
		src, p = conn, conn
	}
	dl.Infof("workspace '%v'", path)

	a := &app{opts: opts, src: src, model: m, signals: make(chan os.Signal, 1), view: &model.View{Stale: true}}
	a.patching = newPatching(p, m, a.setNotice)
	a.inspector = newInspector(m)
	a.panel = dfx.NewHCollapse(dfx.NewFunc(a.drawInspector), dfx.HCollapseConfig{
		Title:         "inspector",
		ExpandedWidth: 380,
		Resizable:     true,
		Expanded:      true,
	})
	signal.Notify(a.signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(a.signals)
	a.canvas = newCanvas(m, a.setNotice, func(out, in pipewire.Serial) { a.patching.link(a.view, out, in) })

	root := dfx.NewFunc(a.draw)
	root.Actions().MustRegister("hide selection", "H", a.canvas.hideSelection)
	root.Actions().MustRegister("toggle show hidden", "Shift+H", a.toggleShowHidden)
	root.Actions().MustRegister("snap selection to grid", "S", a.canvas.snapSelection)
	root.Actions().MustRegister("show newest arrivals", "N", a.showArrivals)
	root.Actions().MustRegister("remove selected links", "Delete", a.deleteLinks)
	root.Actions().MustRegister("zoom to fit", "F", a.canvas.fit)
	root.Actions().MustRegister("center on selection", "C", a.canvas.center)

	title := "patchbay"
	if opts.Sample != "" {
		title += " · " + filepath.Base(opts.Sample)
	}
	// the window must be created and run on the calling goroutine, which main pins to the main os thread; never move
	// this into a spawned goroutine, or glfw and libdecor initialize off the main thread.
	return dfx.New(root, dfx.Config{
		Title:  title,
		Width:  1400,
		Height: 900,
		OnShutdown: func(_ *dfx.App) {
			a.canvas.destroy()
			a.model.Close()
			a.src.Close()
		},
	}).Run()
}

func workspacePath(opts Options) (string, error) {
	if opts.Workspace != "" {
		return opts.Workspace, nil
	}
	path, err := model.DefaultWorkspacePath()
	if err != nil {
		return "", err
	}
	if opts.Sample != "" {
		return filepath.Join(filepath.Dir(path), "sample-workspace.yaml"), nil
	}
	return path, nil
}

func (a *app) setNotice(text string) {
	a.notice = text
	a.noticeAt = time.Now()
}

// deleteLinks posts a destroy for each selected link. the canvas keeps drawing a link until its removal is observed.
func (a *app) deleteLinks() {
	links := make([]pipewire.Serial, 0, len(a.canvas.sel.links))
	for s := range a.canvas.sel.links {
		links = append(links, s)
	}
	a.patching.unlink(a.view, links)
}

func (a *app) drawInspector(_ *dfx.State) {
	source := inspectSource(a.view, a.snap, a.lastLive)
	a.inspector.draw(a.view, source, a.canvas.sel, a.patching.requestLines(a.snap, time.Now()))
}

// showArrivals centers the view on the newest arrivals.
func (a *app) showArrivals() {
	a.canvas.centerOnBlocks(a.arrivals.newest())
}

func (a *app) toggleShowHidden() {
	a.model.SetShowHidden(!a.model.ShowHidden())
}

// draw is one frame: drain the backend's events (stage 4 acts on them), reconcile the current snapshot into a view
// for this frame only, and draw the toolbar, the canvas, and the status strip.
func (a *app) draw(state *dfx.State) {
	select {
	case <-a.signals:
		// an interrupt closes the window the ordinary way, so shutdown destroys the canvas and flushes the workspace.
		state.App.Stop()
	default:
	}
	for drained := false; !drained; {
		select {
		case <-a.src.Events():
		default:
			drained = true
		}
	}
	snap := a.src.Snapshot()
	v := a.model.Reconcile(snap)
	a.snap, a.view = snap, v
	if !v.Stale {
		a.lastLive = snap
	}
	now := time.Now()
	a.arrivals.record(v, now)

	a.drawToolbar()

	if a.notice != "" && time.Since(a.noticeAt) > noticeDuration {
		a.notice = ""
	}
	lines := append(statusLines(v, snap, a.opts.Sample, a.notice), a.patching.requestLines(snap, now)...)
	announced := a.arrivals.announcement(now)
	strip := statusHeight(len(lines))
	if announced != "" {
		strip += imgui.FrameHeightWithSpacing() // the arrivals row carries a button
	}
	avail := imgui.ContentRegionAvail()
	height := avail.Y - strip
	cs := *state
	cs.Size = imgui.Vec2{X: avail.X - a.panel.CurrentWidth - imgui.CurrentStyle().ItemSpacing().X, Y: height}
	a.canvas.draw(&cs, v)
	imgui.SameLine()
	a.panel.Height = height
	ps := *state
	ps.Size = imgui.Vec2{X: a.panel.CurrentWidth, Y: height}
	a.panel.Draw(&ps)

	drawStatus(lines)
	if announced != "" {
		imgui.TextUnformatted(announced)
		imgui.SameLine()
		if imgui.SmallButton("dismiss") {
			a.arrivals.dismiss()
		}
	}
}

// drawToolbar draws the category filters and Show hidden.
func (a *app) drawToolbar() {
	classes := a.model.HiddenClasses()
	dfx.ToolbarExLayout("patchbay", func(t *dfx.ToolbarLayout) {
		t.CenterFrame()
		if show, changed := dfx.Checkbox("video", !classes[model.ClassVideo]); changed {
			a.model.SetClassHidden(model.ClassVideo, !show)
		}
		imgui.SameLine()
		t.CenterFrame()
		if show, changed := dfx.Checkbox("monitor", !classes[model.ClassMonitor]); changed {
			a.model.SetClassHidden(model.ClassMonitor, !show)
		}
		imgui.SameLine()
		t.CenterFrame()
		if on, changed := dfx.Checkbox("show hidden (shift+h)", a.model.ShowHidden()); changed {
			a.model.SetShowHidden(on)
		}
	})
}
