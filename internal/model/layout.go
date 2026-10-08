package model

import "github.com/michaelquigley/patchbay/internal/workspace"

// LayoutState is the remembered window and side-panel presentation.
type LayoutState = workspace.Layout

// WindowState is the remembered window size.
type WindowState = workspace.Window

// PanelState is a side panel's expanded width and collapsed state.
type PanelState = workspace.Panel

// Layout returns a copy of the presentation state the window restores at startup.
func (m *Model) Layout() LayoutState {
	return m.ws.Layout
}

// SetLayout remembers a changed layout through the workspace's debounced store.
func (m *Model) SetLayout(layout LayoutState) {
	if m.ws.Layout == layout {
		return
	}
	m.ws.Layout = layout
	m.changed()
}
