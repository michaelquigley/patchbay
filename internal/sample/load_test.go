package sample

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/michaelquigley/patchbay/internal/pipewire"
)

const samplesRoot = "../../samples"

type counts struct {
	nodes, ports, links, devices, clients int
	forceQuantum                          int
}

// expected object counts per capture, written from each pw-dump.json. a capture added to samples/ without a row here
// fails the test, so every fixture's shape is stated rather than assumed.
var expected = map[string]counts{
	"eleven-20261002-123354-baseline":                     {11, 150, 45, 3, 18, 0},
	"eleven-20261002-123403-reaper-restart":               {11, 150, 45, 3, 18, 0},
	"eleven-20261002-123408-quantum-probe-256/1-before":   {11, 150, 45, 3, 18, 0},
	"eleven-20261002-123408-quantum-probe-256/2-forced":   {11, 150, 45, 3, 18, 256},
	"eleven-20261002-123408-quantum-probe-256/3-released": {11, 150, 45, 3, 18, 0},
	"fortyfive-20261002-122347-desktop-baseline":          {10, 22, 5, 3, 17, 0},
	"seven-20261002-123139-baseline":                      {12, 124, 38, 6, 20, 0},
	"seven-20261002-123207-reaper-restart":                {12, 124, 38, 6, 20, 0},
	"seven-20261002-123224-quantum-probe-256/1-before":    {12, 124, 38, 6, 20, 0},
	"seven-20261002-123224-quantum-probe-256/2-forced":    {12, 124, 38, 6, 20, 256},
	"seven-20261002-123224-quantum-probe-256/3-released":  {12, 124, 38, 6, 20, 0},
	"seven-20261007-143705-two-reapers":                   {13, 165, 74, 6, 19, 0},
	"seven-20261007-143755-scarlett-reconnect":            {11, 83, 2, 6, 17, 0},
	"seven-20261007-143826-scarlett-absent":               {9, 27, 2, 5, 17, 0},
	"seven-20261007-144112-profile-change":                {11, 83, 2, 6, 17, 0},
}

func captures(t *testing.T) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(samplesRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == DumpFile {
			rel, err := filepath.Rel(samplesRoot, filepath.Dir(path))
			if err != nil {
				return err
			}
			dirs = append(dirs, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(dirs)
	return dirs
}

func TestEveryCaptureHasExpectations(t *testing.T) {
	found := map[string]bool{}
	for _, dir := range captures(t) {
		found[dir] = true
		if _, ok := expected[dir]; !ok {
			t.Errorf("capture '%v' has no expected counts", dir)
		}
	}
	for dir := range expected {
		if !found[dir] {
			t.Errorf("expected capture '%v' is missing", dir)
		}
	}
}

func TestLoadCounts(t *testing.T) {
	for _, dir := range captures(t) {
		t.Run(dir, func(t *testing.T) {
			want, ok := expected[dir]
			if !ok {
				t.Skip("no expectations")
			}
			snap, err := Load(filepath.Join(samplesRoot, dir))
			if err != nil {
				t.Fatal(err)
			}
			if snap.State != pipewire.Live {
				t.Errorf("state = %v, want live", snap.State)
			}
			got := counts{len(snap.Nodes), len(snap.Ports), len(snap.Links), len(snap.Devices), len(snap.Clients), snap.Settings.ForceQuantum}
			if got != want {
				t.Errorf("counts = %+v, want %+v", got, want)
			}
			if snap.Unresolved != 0 {
				t.Errorf("%d unresolved references", snap.Unresolved)
			}
			if snap.Settings.Rate != 48000 || snap.Settings.MinQuantum != 32 || snap.Settings.MaxQuantum != 2048 {
				t.Errorf("settings = %+v", snap.Settings)
			}
		})
	}
}

// every link resolves to observed ports whose owners and directions agree with it, and every port to its node.
func TestLoadResolvesReferences(t *testing.T) {
	for _, dir := range captures(t) {
		t.Run(dir, func(t *testing.T) {
			snap, err := Load(filepath.Join(samplesRoot, dir))
			if err != nil {
				t.Fatal(err)
			}
			for serial, p := range snap.Ports {
				if _, ok := snap.Nodes[p.NodeSerial]; !ok {
					t.Errorf("port %d: node serial %d not observed", serial, p.NodeSerial)
				}
				if p.Direction == pipewire.DirectionUnknown {
					t.Errorf("port %d: unknown direction", serial)
				}
			}
			for serial, l := range snap.Links {
				out, okOut := snap.Ports[l.OutPort]
				in, okIn := snap.Ports[l.InPort]
				if !okOut || !okIn {
					t.Errorf("link %d: endpoints %d -> %d not observed", serial, l.OutPort, l.InPort)
					continue
				}
				if out.Direction != pipewire.DirectionOut || in.Direction != pipewire.DirectionIn {
					t.Errorf("link %d: directions %v -> %v", serial, out.Direction, in.Direction)
				}
				if out.NodeSerial != l.OutNode || in.NodeSerial != l.InNode {
					t.Errorf("link %d: node serials disagree with its ports", serial)
				}
				if l.State == "" {
					t.Errorf("link %d: no state", serial)
				}
			}
		})
	}
}

// references resolve only at announcement, so the loader's serial ordering is what makes a capture resolve. the
// capture is fed in reverse, so every link precedes its ports in the file; announced by serial, every reference still
// resolves.
func TestSerialOrderResolvesEveryReference(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(samplesRoot, "eleven-20261002-123354-baseline", DumpFile))
	if err != nil {
		t.Fatal(err)
	}
	var objects []json.RawMessage
	if err := json.Unmarshal(data, &objects); err != nil {
		t.Fatal(err)
	}
	slices.Reverse(objects)
	reversed, err := json.Marshal(objects)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := Inputs(reversed)
	if err != nil {
		t.Fatal(err)
	}
	snap := pipewire.Replay(inputs)
	if snap.Unresolved != 0 {
		t.Fatalf("%d unresolved references", snap.Unresolved)
	}
	if len(snap.Links) != 45 || len(snap.Ports) != 150 {
		t.Errorf("%d links, %d ports", len(snap.Links), len(snap.Ports))
	}
}

// pw-dump writes numeric and boolean properties as json values; the live registry delivers every property as a
// string. the loader renders them in the live form.
func TestPropertiesArriveInLiveForm(t *testing.T) {
	snap, err := Load(filepath.Join(samplesRoot, "eleven-20261002-123354-baseline"))
	if err != nil {
		t.Fatal(err)
	}
	monitor, ok := snap.Ports[1229] // monitor_AUX0 on the scarlett sink, id 230, owned by node id 144
	if !ok {
		t.Fatal("port 1229 missing")
	}
	want := map[string]string{"node.id": "144", "object.id": "230", "object.serial": "1229", "port.id": "0", "port.monitor": "true"}
	for k, v := range want {
		if got := monitor.Props[k]; got != v {
			t.Errorf("%v = %q, want %q", k, got, v)
		}
	}
	if q := snap.Clients[35].Props["default.clock.quantum"]; q != "1024" {
		t.Errorf("default.clock.quantum = %q, want \"1024\"", q)
	}
}

// the identity inputs the model reads arrive as typed fields: the alias prefix, the owning device's serial, and the
// node's description.
func TestTypedIdentityFields(t *testing.T) {
	snap, err := Load(filepath.Join(samplesRoot, "eleven-20261002-123354-baseline"))
	if err != nil {
		t.Fatal(err)
	}
	var launchpad, reaper bool
	for _, p := range snap.Ports {
		switch p.Alias {
		case "Launchpad Pro 2:Launchpad Pro 2 Live Port":
			launchpad = true
			if p.AliasPrefix != "Launchpad Pro 2" {
				t.Errorf("launchpad alias prefix = %q", p.AliasPrefix)
			}
		case "REAPER:in1":
			reaper = true
			if p.AliasPrefix != "REAPER" || p.Name != "in1" {
				t.Errorf("REAPER:in1 alias prefix %q, name %q", p.AliasPrefix, p.Name)
			}
		}
	}
	if !launchpad || !reaper {
		t.Fatalf("ports not found: launchpad %v, REAPER %v", launchpad, reaper)
	}

	var card pipewire.Serial
	for serial, d := range snap.Devices {
		if d.Props["device.name"] == "alsa_card.usb-Focusrite_Scarlett_18i20_4th_Gen_SCARLETT18I20-00" {
			card = serial
		}
	}
	if card == 0 {
		t.Fatal("scarlett alsa_card device not found")
	}
	var sink bool
	for _, n := range snap.Nodes {
		if n.Name == "alsa_output.usb-Focusrite_Scarlett_18i20_4th_Gen_SCARLETT18I20-00.multichannel-output" {
			sink = true
			if n.DeviceSerial != card {
				t.Errorf("sink device serial = %d, want %d", n.DeviceSerial, card)
			}
			if n.Description != "Scarlett 18i20 4th Gen Multichannel" || n.Nick != "Scarlett 18i20 4th Gen" {
				t.Errorf("sink description %q, nick %q", n.Description, n.Nick)
			}
		}
		if n.Name == "REAPER" && n.DeviceSerial != 0 {
			t.Errorf("REAPER has device serial %d", n.DeviceSerial)
		}
	}
	if !sink {
		t.Fatal("scarlett sink not found")
	}
}

func TestLoadClassifiesMedia(t *testing.T) {
	snap, err := Load(filepath.Join(samplesRoot, "eleven-20261002-123354-baseline"))
	if err != nil {
		t.Fatal(err)
	}
	byMedia := map[pipewire.Media]int{}
	monitors := 0
	for _, p := range snap.Ports {
		byMedia[p.Media]++
		if p.Monitor {
			monitors++
		}
	}
	if byMedia[pipewire.MediaUnknown] != 0 {
		t.Errorf("%d ports with unknown media", byMedia[pipewire.MediaUnknown])
	}
	if byMedia[pipewire.MediaMIDI] == 0 || byMedia[pipewire.MediaAudio] == 0 || byMedia[pipewire.MediaVideo] == 0 {
		t.Errorf("media counts = %v", byMedia)
	}
	if monitors == 0 {
		t.Error("no monitor ports on the scarlett sink")
	}
	if len(snap.Default) == 0 {
		t.Error("default metadata not exposed")
	}
}

func TestPropertylessDump(t *testing.T) {
	data := []byte(`[{"id": 7, "type": "PipeWire:Interface:Port", "version": 3, "info": {}},
		{"id": 8, "type": "PipeWire:Interface:Node", "version": 3}]`)
	inputs, err := Inputs(data)
	if err != nil {
		t.Fatal(err)
	}
	snap := pipewire.Replay(inputs)
	if len(snap.Ports)+len(snap.Nodes) != 0 {
		t.Errorf("objects without serials were kept: %d ports, %d nodes", len(snap.Ports), len(snap.Nodes))
	}
}
