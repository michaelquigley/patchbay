package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func sampleWorkspace() *Workspace {
	ws := New()
	ws.View = View{PanX: -40, PanY: 12.5, Zoom: 1.25}
	ws.Layout = Layout{
		Window:      Window{Width: 1680, Height: 1050},
		Inspector:   Panel{Width: 460, Collapsed: true},
		Performance: Panel{Width: 290},
	}
	ws.Records["app:REAPER|midi|in"] = &Record{
		Key:         Key{Class: "app:REAPER", Media: "midi", Direction: "in"},
		X:           400,
		Y:           120,
		HiddenPorts: []string{"REAPER:MIDI Input 9"},
	}
	ws.Records["app:REAPER|midi|in#2"] = &Record{
		Key:    Key{Class: "app:REAPER", Media: "midi", Direction: "in"},
		X:      400,
		Y:      600,
		Hidden: true,
		Also:   []Key{{Class: "app:REAPER-7", Media: "midi", Direction: "in"}},
	}
	return ws
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "patchbay", "workspace.yaml")
	ws := sampleWorkspace()
	if err := Save(ws, path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ws, back) {
		data, _ := os.ReadFile(path)
		t.Fatalf("round trip differs:\n%s\n%+v", data, back)
	}
}

func TestMissingFileIsFresh(t *testing.T) {
	ws, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if ws.Version != Version || !ws.HiddenClasses[ClassVideo] || !ws.HiddenClasses[ClassMonitor] || len(ws.Records) != 0 {
		t.Errorf("fresh workspace = %+v", ws)
	}
}

func TestVersionMismatchRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	if err := os.WriteFile(path, []byte("version: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("a version 2 workspace was accepted")
	}
}

func TestStoreDebouncesAndFlushesOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	s := NewStore(path, 50*time.Millisecond)
	ws := sampleWorkspace()
	s.Changed(ws)
	ws.Records["app:REAPER|midi|in"].X = 800
	s.Changed(ws)
	if _, err := os.Stat(path); err == nil {
		t.Fatal("written before the debounce elapsed")
	}
	time.Sleep(150 * time.Millisecond)
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Records["app:REAPER|midi|in"].X != 800 {
		t.Error("debounced save did not carry the latest change")
	}

	ws.Records["app:REAPER|midi|in"].X = 900
	s.Changed(ws)
	s.Close()
	back, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Records["app:REAPER|midi|in"].X != 900 {
		t.Error("close did not flush the pending change")
	}
	s.Changed(ws)
}

func TestRecordPortHidden(t *testing.T) {
	r := &Record{}
	r.SetPortHidden("a", true)
	r.SetPortHidden("b", true)
	r.SetPortHidden("a", true)
	r.SetPortHidden("b", false)
	if !r.PortHidden("a") || r.PortHidden("b") || len(r.HiddenPorts) != 1 {
		t.Errorf("hidden ports = %v", r.HiddenPorts)
	}
}
