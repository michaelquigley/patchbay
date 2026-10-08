package model

import (
	"testing"

	"github.com/michaelquigley/patchbay/internal/pipewire"
	"github.com/michaelquigley/patchbay/internal/workspace"
)

func TestInitialUnassignedPlacementIgnoresViewport(t *testing.T) {
	snap := load(t, desktop)
	ws := workspace.New()
	New(ws, nil).Reconcile(snap)
	positions := map[BlockID][2]float32{}
	for i, pan := range []float32{0, 2500, -6000} {
		ws.View = workspace.View{PanX: pan, PanY: pan, Zoom: 0.5}
		m := New(ws, nil)
		m.SetViewport(Rect{MinX: -pan, MinY: -pan, MaxX: -pan + 1800, MaxY: -pan + 1000})
		v := m.Reconcile(snap)
		blocks := blocksWithKey(v, grdAudioIn)
		if len(blocks) != 2 {
			t.Fatalf("want two ambiguous blocks, got %d", len(blocks))
		}
		for _, b := range blocks {
			if b.Record != "" || b.Gap != GapColliding {
				t.Fatal("startup placement claimed an ambiguous record")
			}
			pos := [2]float32{b.X, b.Y}
			if i == 0 {
				positions[b.ID] = pos
			} else if positions[b.ID] != pos {
				t.Fatalf("pan %v moved ambiguous block from %v to %v", pan, positions[b.ID], pos)
			}
			for _, other := range v.Blocks {
				if other.Record != "" && other.Visible && b.X < other.X+blockWidth(m.live[other.ID])+columnGap {
					t.Fatal("startup column overlaps the remembered graph")
				}
			}
		}
		m.Reconcile(withState(snap, pipewire.Disconnected))
		for _, b := range blocksWithKey(m.Reconcile(snap), grdAudioIn) {
			if positions[b.ID] != [2]float32{b.X, b.Y} {
				t.Fatal("reconnect moved the startup column")
			}
		}
	}
}

func TestAmbiguousLiveArrivalsStillUseViewport(t *testing.T) {
	snap := load(t, desktop)
	m := New(workspace.New(), nil)
	m.Reconcile(without(snap, nodesNamed(snap, "gnome-remote-desktop-daemon")...))
	viewport := Rect{MinX: 6000, MinY: 4000, MaxX: 8000, MaxY: 5000}
	m.SetViewport(viewport)
	v := m.Reconcile(snap)
	blocks := blocksWithKey(v, grdAudioIn)
	if len(blocks) != 2 {
		t.Fatalf("want two ambiguous arrivals, got %d", len(blocks))
	}
	for _, b := range blocks {
		inside(t, b, viewport, m.live[b.ID])
	}
}
