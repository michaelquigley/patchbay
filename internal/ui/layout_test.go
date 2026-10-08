package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/michaelquigley/patchbay/internal/model"
)

func openLayoutApp(t *testing.T, path string) *app {
	t.Helper()
	m, err := model.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return &app{model: m}
}

func TestLayoutRestoresOlderWorkspaceDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nview:\n    zoom: 0.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := openLayoutApp(t, path)
	window := a.restoreLayout()
	if window != (model.WindowState{Width: 1400, Height: 900}) {
		t.Fatalf("old workspace window = %+v", window)
	}
	if !a.panel.Expanded || a.panel.CurrentWidth != inspectorWidth ||
		!a.performance.Expanded || a.performance.CurrentWidth != performanceWidth {
		t.Fatal("older workspace did not open both panels at their default widths")
	}
	if a.model.Workspace().Zoom != 0.7 {
		t.Fatal("restoring window layout changed the remembered canvas view")
	}
}

func TestLayoutPersistsAcrossCloseAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	a := openLayoutApp(t, path)
	a.restoreLayout()
	a.panel.ExpandedWidth = 470
	a.performance.ExpandedWidth = 280
	togglePanel(a.panel, inspectorWidth)
	// an in-flight collapse must not overwrite the inspector's expanded width.
	a.panel.CurrentWidth = 200
	a.rememberLayout(1720, 1020)
	a.rememberLayout(0, 0)
	a.model.Close() // flush without waiting for the debounce

	b := openLayoutApp(t, path)
	window := b.restoreLayout()
	if window != (model.WindowState{Width: 1720, Height: 1020}) {
		t.Fatalf("restored window = %+v", window)
	}
	if b.panel.Expanded || b.panel.ExpandedWidth != 470 || b.panel.CurrentWidth != b.panel.MinWidth {
		t.Fatalf("collapsed inspector did not restore its state and expanded width: %+v", b.panel)
	}
	if !b.performance.Expanded || b.performance.ExpandedWidth != 280 || b.performance.CurrentWidth != 280 {
		t.Fatalf("performance panel did not restore its state and width: %+v", b.performance)
	}
	togglePanel(b.panel, inspectorWidth)
	togglePanel(b.performance, performanceWidth)
	b.rememberLayout(1600, 950)
	b.model.Close()

	c := openLayoutApp(t, path)
	c.restoreLayout()
	if !c.panel.Expanded || c.panel.CurrentWidth != 470 || c.performance.Expanded || c.performance.ExpandedWidth != 280 {
		t.Fatal("reopened inspector or newly collapsed performance panel lost its remembered width")
	}
}
