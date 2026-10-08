package ui

import (
	"github.com/michaelquigley/dfx"
	"github.com/michaelquigley/patchbay/internal/model"
)

// restoreLayout creates panels directly in their remembered state, without an opening animation,
// and returns the window size to use when creating the native window.
func (a *app) restoreLayout() model.WindowState {
	layout := a.model.Layout()
	if layout.Window.Width <= 0 {
		layout.Window.Width = 1400
	}
	if layout.Window.Height <= 0 {
		layout.Window.Height = 900
	}
	if layout.Inspector.Width < dfx.HCollapseDefaultMinWidth {
		layout.Inspector.Width = inspectorWidth
	}
	if layout.Performance.Width < dfx.HCollapseDefaultMinWidth {
		layout.Performance.Width = performanceWidth
	}
	a.panel = dfx.NewHCollapse(dfx.NewFunc(a.drawInspector), dfx.HCollapseConfig{
		Title:         "inspector",
		ExpandedWidth: layout.Inspector.Width,
		Resizable:     true,
		Expanded:      !layout.Inspector.Collapsed,
		Anchor:        dfx.AnchorRight,
	})
	a.performance = dfx.NewHCollapse(dfx.NewFunc(func(*dfx.State) { a.drawPerformance(a.view, a.snap, a.frameEvents, a.now) }), dfx.HCollapseConfig{
		Title:         "performance",
		ExpandedWidth: layout.Performance.Width,
		Resizable:     true,
		Expanded:      !layout.Performance.Collapsed,
		Anchor:        dfx.AnchorLeft,
	})
	return layout.Window
}

// rememberLayout samples presentation after drawing. expanded widths are stable through collapse animations;
// temporary zero window sizes (for example while minimized) never replace the last usable size.
func (a *app) rememberLayout(width, height int) {
	layout := a.model.Layout()
	if width > 0 && height > 0 {
		layout.Window = model.WindowState{Width: width, Height: height}
	}
	layout.Inspector = model.PanelState{Width: a.panel.ExpandedWidth, Collapsed: !a.panel.Expanded}
	layout.Performance = model.PanelState{Width: a.performance.ExpandedWidth, Collapsed: !a.performance.Expanded}
	a.model.SetLayout(layout)
}
