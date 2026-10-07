package model

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/michaelquigley/patchbay/internal/pipewire"
	"github.com/michaelquigley/patchbay/internal/workspace"
)

// the samples captured on seven on 2026-10-07 for stage 6.
const (
	twoReapers        = "seven-20261007-143705-two-reapers"
	scarlettReconnect = "seven-20261007-143755-scarlett-reconnect"
	scarlettAbsent    = "seven-20261007-143826-scarlett-absent"
	profileChange     = "seven-20261007-144112-profile-change"

	scarlettHardware = "Focusrite_Scarlett_16i16_4th_Gen_SCARLETT16I16"
	scarlettOut      = "node:alsa_output.usb-Focusrite_Scarlett_16i16_4th_Gen_SCARLETT16I16-00.multichannel-output"
	scarlettIn       = "node:alsa_input.usb-Focusrite_Scarlett_16i16_4th_Gen_SCARLETT16I16-00.multichannel-input"
	proOut           = "node:alsa_output.usb-Focusrite_Scarlett_16i16_4th_Gen_SCARLETT16I16-00.pro-output-0"
	proIn            = "node:alsa_input.usb-Focusrite_Scarlett_16i16_4th_Gen_SCARLETT16I16-00.pro-input-0"
)

// the blocks every 2026-10-07 capture shares: mpv, the bridge's alsa clients other than the Scarlett, and the
// built-in and webcam devices. their node names carry no WirePlumber counter in these captures.
var sharedBlocks = []string{
	"app:mpv|audio|out 2",
	"midi:Midi Through|midi|in 1", "midi:Midi Through|midi|out 1",
	"midi:Virtual Raw MIDI 0-0|midi|in 1", "midi:Virtual Raw MIDI 0-0|midi|out 1",
	"midi:Virtual Raw MIDI 0-1|midi|in 1", "midi:Virtual Raw MIDI 0-1|midi|out 1",
	"midi:Virtual Raw MIDI 0-2|midi|in 1", "midi:Virtual Raw MIDI 0-2|midi|out 1",
	"midi:Virtual Raw MIDI 0-3|midi|in 1", "midi:Virtual Raw MIDI 0-3|midi|out 1",
	"midi:microKEY-25|midi|in 1", "midi:microKEY-25|midi|out 1",
	"node:alsa_input.pci-0000_0d_00.4.analog-stereo|audio|out 2",
	"node:alsa_input.usb-046d_HD_Pro_Webcam_C920_WEBCAM-02.analog-stereo|audio|out 2",
	"node:alsa_output.pci-0000_0b_00.1.hdmi-stereo|audio|in 2", "node:alsa_output.pci-0000_0b_00.1.hdmi-stereo|audio|out 2",
	"node:alsa_output.pci-0000_0d_00.4.iec958-stereo|audio|in 2", "node:alsa_output.pci-0000_0d_00.4.iec958-stereo|audio|out 2",
	"node:v4l2_input.pci-0000_0d_00.3-usb-0_3_1.0|video|out 1",
}

// the Scarlett's blocks under its multichannel profile: the sink's playback and monitor ports, the source, and its
// midi port on the bridge.
var multichannelBlocks = []string{
	"midi:Scarlett 16i16 4th Gen|midi|in 1", "midi:Scarlett 16i16 4th Gen|midi|out 1",
	scarlettIn + "|audio|out 18", scarlettOut + "|audio|in 18", scarlettOut + "|audio|out 18",
}

// the fixture suite for the 2026-10-07 captures: each sample's expected block set in a fresh workspace, as recognition
// key, port count, and display ordinal.
func TestFixtureBlockSets(t *testing.T) {
	reapers := []string{}
	for _, k := range []string{"app:REAPER|audio|in 18", "app:REAPER|audio|out 18", "app:REAPER|midi|in 4", "app:REAPER|midi|out 1"} {
		reapers = append(reapers, k+" #1", k+" #2")
	}
	expected := map[string][]string{
		twoReapers:        concat(sharedBlocks, multichannelBlocks, reapers),
		scarlettReconnect: concat(sharedBlocks, multichannelBlocks),
		scarlettAbsent:    sharedBlocks,
		profileChange: concat(sharedBlocks, []string{
			"midi:Scarlett 16i16 4th Gen|midi|in 1", "midi:Scarlett 16i16 4th Gen|midi|out 1",
			proIn + "|audio|out 18", proOut + "|audio|in 18", proOut + "|audio|out 18",
		}),
	}
	for name, want := range expected {
		t.Run(name, func(t *testing.T) {
			_, v := fresh(t, name)
			var got []string
			for _, b := range v.Blocks {
				s := fmt.Sprintf("%v %d", b.Key, len(b.Ports))
				if b.Ordinal > 0 {
					s += fmt.Sprintf(" #%d", b.Ordinal)
				}
				got = append(got, s)
			}
			sort.Strings(got)
			want = append([]string(nil), want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("blocks:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func concat(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// WirePlumber's name counter is a runtime value: one trailing .2 to .99 is dropped, anything else stays.
func TestDeviceNodeName(t *testing.T) {
	for in, want := range map[string]string{
		"alsa_input.pci-0000_0d_00.4.analog-stereo.3":      "alsa_input.pci-0000_0d_00.4.analog-stereo",
		"alsa_output.pci-0000_0b_00.1.hdmi-stereo.4":       "alsa_output.pci-0000_0b_00.1.hdmi-stereo",
		"alsa_input.pci-0000_0d_00.4.analog-stereo":        "alsa_input.pci-0000_0d_00.4.analog-stereo",
		"v4l2_input.pci-0000_0d_00.3-usb-0_3_1.0":          "v4l2_input.pci-0000_0d_00.3-usb-0_3_1.0",
		"bluez_output.00_11_22_33_44_55.1":                 "bluez_output.00_11_22_33_44_55.1",
		"x.100":                                            "x.100",
		"x.02":                                             "x.02",
		"alsa_output.usb-X-00.multichannel-output":         "alsa_output.usb-X-00.multichannel-output",
		"alsa_output.usb-X-00.multichannel-output.2.extra": "alsa_output.usb-X-00.multichannel-output.2.extra",
	} {
		if got := deviceNodeName(in); got != want {
			t.Errorf("deviceNodeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// savedBaseline is the seven baseline's workspace, after edit, saved and loaded back: what the operator would have on
// disk when the later captures were taken.
func savedBaseline(t *testing.T, edit func(m *Model, v *View)) (*workspace.Workspace, string) {
	t.Helper()
	m, v := fresh(t, sevenBaseline)
	if edit != nil {
		edit(m, v)
	}
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	if err := workspace.Save(m.ws, path); err != nil {
		t.Fatal(err)
	}
	return reload(t, path), path
}

func reload(t *testing.T, path string) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func save(t *testing.T, ws *workspace.Workspace, path string) {
	t.Helper()
	if err := workspace.Save(ws, path); err != nil {
		t.Fatal(err)
	}
}

func copyRecords(ws *workspace.Workspace) map[string]workspace.Record {
	out := map[string]workspace.Record{}
	for rk, r := range ws.Records {
		out[rk] = *r
	}
	return out
}

// two REAPER instances share every key and all 41 port aliases. neither takes any of the four REAPER records; both
// are new with ordinals; each is offered the records; associating one binds that instance only. a reload against the
// same two instances assigns neither, as the conservative rule requires; a reload against the restart sample, with
// one REAPER, matches normally.
func TestTwoReapers(t *testing.T) {
	ws, path := savedBaseline(t, nil)
	reaperKeys := []string{"app:REAPER|audio|in", "app:REAPER|audio|out", "app:REAPER|midi|in", "app:REAPER|midi|out"}
	before := copyRecords(ws)
	for _, k := range reaperKeys {
		if _, ok := before[k]; !ok {
			t.Fatalf("the baseline workspace has no record '%v'", k)
		}
	}

	m := New(ws, nil)
	v := m.Reconcile(load(t, twoReapers))
	aliases := map[BlockID][]string{}
	for _, k := range reaperKeys {
		bs := blocksWithKey(v, k)
		if len(bs) != 2 {
			t.Fatalf("%d blocks for '%v', want 2", len(bs), k)
		}
		ordinals := map[int]bool{}
		for _, b := range bs {
			if b.Record != "" || b.Gap != GapColliding {
				t.Errorf("'%v' took record '%v' (gap %v)", b.ID, b.Record, b.Gap)
			}
			ordinals[b.Ordinal] = true
			offered := false
			for _, c := range v.Candidates(b) {
				offered = offered || c.Record == k
			}
			if !offered {
				t.Errorf("'%v' is not offered '%v'", b.ID, k)
			}
			aliases[b.ID] = portKeys(b)
		}
		if !ordinals[1] || !ordinals[2] {
			t.Errorf("'%v' ordinals = %v, want 1 and 2", k, ordinals)
		}
		if !reflect.DeepEqual(aliases[bs[0].ID], aliases[bs[1].ID]) {
			t.Errorf("'%v' instances differ in port aliases", k)
		}
	}
	total := 0
	for _, a := range aliases {
		total += len(a)
	}
	if total != 82 {
		t.Errorf("%d REAPER ports, want 41 per instance", total)
	}

	audioIn := blocksWithKey(v, reaperKeys[0])
	chosen, other := audioIn[0], audioIn[1]
	if chosen.Ordinal != 1 {
		chosen, other = other, chosen
	}
	if err := m.Associate(chosen.ID, reaperKeys[0]); err != nil {
		t.Fatal(err)
	}
	for _, v := range []*View{m.Refresh(), m.Reconcile(load(t, twoReapers))} {
		c, _ := v.Block(chosen.ID)
		o, _ := v.Block(other.ID)
		if c.Record != reaperKeys[0] || c.X != before[reaperKeys[0]].X || c.Y != before[reaperKeys[0]].Y {
			t.Errorf("associated instance = %+v", c)
		}
		if o.Record != "" {
			t.Errorf("the other instance took '%v'", o.Record)
		}
	}
	save(t, ws, path)

	// reloaded against the same two instances: an association lasts for its instance's lifetime, and a new process
	// cannot know which instance that was, so neither takes the record. it is intact and offered.
	again := reload(t, path)
	v = New(again, nil).Reconcile(load(t, twoReapers))
	for _, b := range blocksWithKey(v, reaperKeys[0]) {
		if b.Record != "" {
			t.Errorf("'%v' took '%v' on reload with two instances live", b.ID, b.Record)
		}
	}
	if !absent(v, reaperKeys[0]) || !reflect.DeepEqual(*again.Records[reaperKeys[0]], before[reaperKeys[0]]) {
		t.Errorf("record after reload = %+v, want %+v and offered", *again.Records[reaperKeys[0]], before[reaperKeys[0]])
	}

	// reloaded against the restart sample, where one REAPER returns: the single candidate matches normally.
	v = New(reload(t, path), nil).Reconcile(load(t, sevenRestart))
	for _, k := range reaperKeys {
		if b := oneBlock(t, v, k); b.Record != k || b.X != before[k].X || b.Y != before[k].Y {
			t.Errorf("'%v' after the restart = record '%v' at (%v, %v)", k, b.Record, b.X, b.Y)
		}
	}
}

// gestures on the two REAPER instances create separate records; reloaded with both live, neither is assigned and
// neither overwrote the other.
func TestTwoReaperGesturesKeepSeparateRecords(t *testing.T) {
	m, v := fresh(t, twoReapers)
	key := "app:REAPER|audio|in"
	bs := blocksWithKey(v, key)
	if err := m.Move(bs[0].ID, 1000, 100); err != nil {
		t.Fatal(err)
	}
	if err := m.Move(bs[1].ID, 1000, 500); err != nil {
		t.Fatal(err)
	}
	if err := m.SetBlockHidden(bs[1].ID, true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	save(t, m.ws, path)
	ws := reload(t, path)
	var recs []workspace.Record
	for rk, r := range ws.Records {
		if strings.HasPrefix(rk, key) {
			recs = append(recs, *r)
		}
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Y < recs[j].Y })
	if len(recs) != 2 || recs[0].Y != 100 || recs[0].Hidden || recs[1].Y != 500 || !recs[1].Hidden {
		t.Fatalf("records = %+v", recs)
	}
	for _, b := range blocksWithKey(New(ws, nil).Reconcile(load(t, twoReapers)), key) {
		if b.Record != "" {
			t.Errorf("'%v' assigned '%v' on reload", b.ID, b.Record)
		}
	}
}

// the Scarlett's hidden-port preferences in the baseline workspace: one playback port on the sink, one capture port on
// the source.
func hideScarlettPorts(t *testing.T) func(m *Model, v *View) {
	return func(m *Model, v *View) {
		t.Helper()
		if err := m.SetPortHidden(oneBlock(t, v, scarlettOut+"|audio|in").ID, "playback_AUX2", true); err != nil {
			t.Fatal(err)
		}
		if err := m.SetPortHidden(oneBlock(t, v, scarlettIn+"|audio|out").ID, "capture_AUX5", true); err != nil {
			t.Fatal(err)
		}
	}
}

func isScarlett(key string) bool {
	return strings.Contains(key, "Scarlett")
}

// a Scarlett replug: against the baseline's workspace, every Scarlett block takes its record back with its position
// and port preferences. serials differ throughout; keys do not.
func TestScarlettReconnectRestores(t *testing.T) {
	baseline := load(t, sevenBaseline)
	ws, _ := savedBaseline(t, hideScarlettPorts(t))
	before := copyRecords(ws)
	baseView := New(workspace.New(), nil).Reconcile(baseline)
	reconnect := load(t, scarlettReconnect)
	v := New(ws, nil).Reconcile(reconnect)

	restored := 0
	for rk, rec := range before {
		if !isScarlett(rk) {
			continue
		}
		b := oneBlock(t, v, rk)
		if b.Record != rk || b.X != rec.X || b.Y != rec.Y {
			t.Errorf("'%v' = record '%v' at (%v, %v), want (%v, %v)", rk, b.Record, b.X, b.Y, rec.X, rec.Y)
		}
		for _, p := range b.Ports {
			if p.Hidden != rec.PortHidden(p.Key) {
				t.Errorf("'%v' port '%v' hidden %v, want %v", rk, p.Key, p.Hidden, rec.PortHidden(p.Key))
			}
		}
		// the keys match while every port serial differs; a device-backed block's node and device serials differ
		// too. a bridge block's node is the Midi-Bridge, which outlives the replug.
		was := oneBlock(t, baseView, rk)
		old := map[pipewire.Serial]bool{}
		for _, p := range was.Ports {
			old[p.Serial] = true
		}
		for _, p := range b.Ports {
			if old[p.Serial] {
				t.Errorf("'%v' port '%v' kept serial %d across the replug", rk, p.Key, p.Serial)
			}
		}
		if b.Owner == OwnerDevice && (was.Node == b.Node || reconnect.Nodes[b.Node].DeviceSerial == baseline.Nodes[was.Node].DeviceSerial) {
			t.Errorf("'%v' kept its node or device serial across the replug", rk)
		}
		restored++
	}
	if restored != len(multichannelBlocks) {
		t.Errorf("%d Scarlett records restored, want %d", restored, len(multichannelBlocks))
	}
	hidden := 0
	for _, b := range v.Blocks {
		for _, p := range b.Ports {
			if p.Hidden {
				hidden++
			}
		}
	}
	if hidden != 2 {
		t.Errorf("%d hidden ports after the replug, want the two preferences", hidden)
	}
}

// the Scarlett unplugged: no Scarlett block is drawn, every Scarlett record is kept unchanged and offered, and every
// other block is where the baseline put it, with no record created.
func TestScarlettAbsentKeepsRecords(t *testing.T) {
	ws, _ := savedBaseline(t, hideScarlettPorts(t))
	before := copyRecords(ws)
	v := New(ws, nil).Reconcile(load(t, scarlettAbsent))
	for _, b := range v.Blocks {
		if isScarlett(b.Key.String()) {
			t.Errorf("Scarlett block '%v' drawn while absent", b.Key)
			continue
		}
		rec, ok := before[b.Key.String()]
		if b.Record != b.Key.String() || !ok || b.X != rec.X || b.Y != rec.Y {
			t.Errorf("'%v' = record '%v' at (%v, %v); baseline (%v, %v)", b.Key, b.Record, b.X, b.Y, rec.X, rec.Y)
		}
	}
	if !reflect.DeepEqual(copyRecords(ws), before) {
		t.Error("the workspace's records changed while the Scarlett was absent")
	}
	for rk := range before {
		if isScarlett(rk) && !absent(v, rk) {
			t.Errorf("'%v' not offered while absent", rk)
		}
	}
}

// a profile change renames the Scarlett's nodes and keeps its port names. the pro-audio blocks are new and get no
// record of their own; the multichannel records are intact, named as the same device, and offered first. associating
// adds the pro-audio key to the record; the multichannel baseline still matches it by its original key; a port
// hidden under either profile is hidden under the other.
func TestProfileChange(t *testing.T) {
	ws, path := savedBaseline(t, hideScarlettPorts(t))
	before := copyRecords(ws)
	if before[scarlettOut+"|audio|in"].Device != scarlettHardware {
		t.Fatalf("the baseline record does not know its device: %+v", before[scarlettOut+"|audio|in"])
	}
	m := New(ws, nil)
	v := m.Reconcile(load(t, profileChange))

	for _, k := range []string{proOut + "|audio|in", proOut + "|audio|out", proIn + "|audio|out"} {
		b := oneBlock(t, v, k)
		if b.Record != "" || b.Gap != GapSameDevice || b.Hardware != scarlettHardware {
			t.Errorf("'%v' = record '%v', gap %v, hardware '%v'", k, b.Record, b.Gap, b.Hardware)
		}
	}
	for rk := range ws.Records {
		if strings.Contains(rk, "pro-") {
			t.Errorf("a record '%v' was created for a pro-audio block", rk)
		}
	}
	for rk, rec := range before {
		if isScarlett(rk) && strings.HasPrefix(rk, "node:") && (!reflect.DeepEqual(*ws.Records[rk], rec) || !absent(v, rk)) {
			t.Errorf("multichannel record '%v' = %+v, want intact and offered", rk, *ws.Records[rk])
		}
	}

	pro := oneBlock(t, v, proOut+"|audio|in")
	candidates := v.Candidates(pro)
	if len(candidates) < 2 || candidates[0].Record != scarlettOut+"|audio|in" {
		t.Fatalf("candidates = %+v, want the multichannel sink first", candidates)
	}
	for _, c := range candidates[1:] {
		if c.Device == scarlettHardware {
			t.Errorf("same-device record '%v' listed after another device's", c.Record)
		}
	}
	if GapSameDevice.String() == "" {
		t.Error("the profile gap has no sentence")
	}

	if err := m.Associate(pro.ID, scarlettOut+"|audio|in"); err != nil {
		t.Fatal(err)
	}
	rec := ws.Records[scarlettOut+"|audio|in"]
	if !reflect.DeepEqual(rec.Also, []workspace.Key{pro.Key}) {
		t.Errorf("also = %v, want the pro-audio key", rec.Also)
	}
	b, _ := m.Refresh().Block(pro.ID)
	if !portHidden(b, "playback_AUX2") {
		t.Error("playback_AUX2, hidden under multichannel, is visible under pro-audio")
	}
	if err := m.SetPortHidden(pro.ID, "playback_AUX4", true); err != nil {
		t.Fatal(err)
	}
	save(t, ws, path)

	b = oneBlock(t, New(reload(t, path), nil).Reconcile(load(t, sevenBaseline)), scarlettOut+"|audio|in")
	if b.Record != scarlettOut+"|audio|in" || !portHidden(b, "playback_AUX2") || !portHidden(b, "playback_AUX4") {
		t.Errorf("multichannel after association = record '%v', hidden %v", b.Record, hiddenKeys(b))
	}
	b = oneBlock(t, New(reload(t, path), nil).Reconcile(load(t, profileChange)), proOut+"|audio|in")
	if b.Record != scarlettOut+"|audio|in" || !portHidden(b, "playback_AUX4") {
		t.Errorf("pro-audio after association = record '%v', hidden %v", b.Record, hiddenKeys(b))
	}
}

func portHidden(b Block, key string) bool {
	for _, p := range b.Ports {
		if p.Key == key {
			return p.Hidden
		}
	}
	return false
}

func hiddenKeys(b Block) []string {
	var out []string
	for _, p := range b.Ports {
		if p.Hidden {
			out = append(out, p.Key)
		}
	}
	return out
}

// the assignment lifecycle on the two REAPER instances: the sole instance takes the record; a second arriving later is
// new and does not displace it; when the assigned instance leaves, the remaining one does not inherit the record; and
// one instance facing two records for its key takes neither.
func TestTwoReaperAssignmentLifecycle(t *testing.T) {
	full := load(t, twoReapers)
	reapers := nodesNamed(full, "REAPER")
	if len(reapers) != 2 {
		t.Fatalf("%d REAPER nodes, want 2", len(reapers))
	}
	key := workspace.Key{Class: "app:REAPER", Media: MediaAudio, Direction: DirectionIn}
	ws := workspace.New()
	ws.Records[key.String()] = &workspace.Record{Key: key, X: 2000, Y: 2000}
	m := New(ws, nil)

	if b := oneBlock(t, m.Reconcile(without(full, reapers[1])), key.String()); b.Record != key.String() {
		t.Fatalf("sole instance not assigned: %+v", b)
	}
	v := m.Reconcile(full)
	for _, b := range blocksWithKey(v, key.String()) {
		first := b.Node == reapers[0]
		if first != (b.Record == key.String()) {
			t.Errorf("instance %d record '%v' after the second arrived", b.Node, b.Record)
		}
	}
	v = m.Reconcile(without(full, reapers[0]))
	if b := oneBlock(t, v, key.String()); b.Record != "" || !absent(v, key.String()) {
		t.Errorf("the remaining instance inherited the record: %+v", b)
	}

	ws = workspace.New()
	ws.Records[key.String()] = &workspace.Record{Key: key, X: 100}
	ws.Records[key.String()+"#2"] = &workspace.Record{Key: key, X: 900}
	if b := oneBlock(t, New(ws, nil).Reconcile(without(full, reapers[1])), key.String()); b.Record != "" || b.Gap != GapSeveralRecords {
		t.Errorf("one instance facing two records = %+v", b)
	}
}
