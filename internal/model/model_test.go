package model

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/michaelquigley/patchbay/internal/pipewire"
	"github.com/michaelquigley/patchbay/internal/sample"
	"github.com/michaelquigley/patchbay/internal/workspace"
)

const (
	elevenBaseline = "eleven-20261002-123354-baseline"
	elevenRestart  = "eleven-20261002-123403-reaper-restart"
	sevenBaseline  = "seven-20261002-123139-baseline"
	sevenRestart   = "seven-20261002-123207-reaper-restart"
	desktop        = "fortyfive-20261002-122347-desktop-baseline"

	grdAudioIn = "app:gnome-remote-desktop-daemon|audio|in"
)

func load(t *testing.T, name string) *pipewire.Snapshot {
	t.Helper()
	snap, err := sample.Load(filepath.Join("../../samples", name))
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func withState(s *pipewire.Snapshot, state pipewire.ConnState) *pipewire.Snapshot {
	c := *s
	c.State = state
	return &c
}

// without returns a copy of the snapshot lacking the given nodes, their ports, and any link touching them.
func without(s *pipewire.Snapshot, drop ...pipewire.Serial) *pipewire.Snapshot {
	gone := map[pipewire.Serial]bool{}
	for _, d := range drop {
		gone[d] = true
	}
	c := *s
	c.Nodes, c.Ports, c.Links = map[pipewire.Serial]pipewire.Node{}, map[pipewire.Serial]pipewire.Port{}, map[pipewire.Serial]pipewire.Link{}
	for k, n := range s.Nodes {
		if !gone[k] {
			c.Nodes[k] = n
		}
	}
	for k, p := range s.Ports {
		if !gone[p.NodeSerial] {
			c.Ports[k] = p
		}
	}
	for k, l := range s.Links {
		if !gone[l.OutNode] && !gone[l.InNode] {
			c.Links[k] = l
		}
	}
	return &c
}

func nodesNamed(s *pipewire.Snapshot, name string) []pipewire.Serial {
	var out []pipewire.Serial
	for serial, n := range s.Nodes {
		if n.Name == name {
			out = append(out, serial)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func blocksWithKey(v *View, key string) []Block {
	var out []Block
	for _, b := range v.Blocks {
		if b.Key.String() == key {
			out = append(out, b)
		}
	}
	return out
}

func oneBlock(t *testing.T, v *View, key string) Block {
	t.Helper()
	bs := blocksWithKey(v, key)
	if len(bs) != 1 {
		t.Fatalf("%d blocks with key '%v', want 1", len(bs), key)
	}
	return bs[0]
}

func portKeys(b Block) []string {
	var out []string
	for _, p := range b.Ports {
		out = append(out, p.Key)
	}
	sort.Strings(out)
	return out
}

func keySet(v *View, classPrefix string) map[string][]string {
	out := map[string][]string{}
	for _, b := range v.Blocks {
		if strings.HasPrefix(b.Key.Class, classPrefix) {
			out[b.Key.String()] = portKeys(b)
		}
	}
	return out
}

func fresh(t *testing.T, name string) (*Model, *View) {
	t.Helper()
	m := New(workspace.New(), nil)
	return m, m.Reconcile(load(t, name))
}

// REAPER yields four blocks whose recognition keys and port keys survive a restart on both studio machines.
func TestReaperRestartKeepsKeys(t *testing.T) {
	for _, pair := range [][2]string{{elevenBaseline, elevenRestart}, {sevenBaseline, sevenRestart}} {
		_, before := fresh(t, pair[0])
		_, after := fresh(t, pair[1])
		b, a := keySet(before, "app:REAPER"), keySet(after, "app:REAPER")
		if len(b) != 4 {
			t.Errorf("%v: %d REAPER blocks, want 4: %v", pair[0], len(b), b)
		}
		for key, ports := range b {
			if strings.Join(a[key], ",") != strings.Join(ports, ",") {
				t.Errorf("%v: block '%v' port keys changed across the restart", pair[0], key)
			}
		}
		if len(a) != len(b) {
			t.Errorf("%v: block count %d before, %d after", pair[0], len(b), len(a))
		}
	}
}

func midiClasses(v *View, direction string) []string {
	var out []string
	for _, b := range v.Blocks {
		if strings.HasPrefix(b.Key.Class, classMIDI) && b.Key.Direction == direction {
			out = append(out, strings.TrimPrefix(b.Key.Class, classMIDI))
		}
	}
	sort.Strings(out)
	return out
}

// Midi-Bridge ports group by alias prefix: each alsa client is a block, so each VirMIDI client is its own one-port
// block in each direction.
func TestMidiBridgeBlocks(t *testing.T) {
	_, eleven := fresh(t, elevenBaseline)
	want := "Launchpad Pro 2,Midi Through,Scarlett 18i20 4th Gen"
	for _, dir := range []string{DirectionIn, DirectionOut} {
		if got := strings.Join(midiClasses(eleven, dir), ","); got != want {
			t.Errorf("eleven midi %v blocks = %v, want %v", dir, got, want)
		}
	}
	if b := oneBlock(t, eleven, "midi:Launchpad Pro 2|midi|in"); len(b.Ports) != 3 {
		t.Errorf("launchpad in block has %d ports, want 3", len(b.Ports))
	}

	_, seven := fresh(t, sevenBaseline)
	want = "Midi Through,Scarlett 16i16 4th Gen,Virtual Raw MIDI 0-0,Virtual Raw MIDI 0-1,Virtual Raw MIDI 0-2,Virtual Raw MIDI 0-3,microKEY-25"
	for _, dir := range []string{DirectionIn, DirectionOut} {
		if got := strings.Join(midiClasses(seven, dir), ","); got != want {
			t.Errorf("seven midi %v blocks = %v, want %v", dir, got, want)
		}
	}
	virmidi := 0
	for _, b := range seven.Blocks {
		if strings.HasPrefix(b.Key.Class, "midi:Virtual Raw MIDI") {
			virmidi++
			if len(b.Ports) != 1 {
				t.Errorf("'%v' has %d ports, want 1", b.Key, len(b.Ports))
			}
		}
	}
	if virmidi != 8 {
		t.Errorf("%d VirMIDI blocks, want 8 (four clients, one block per direction)", virmidi)
	}
}

// a Scarlett sink yields an audio-in block of playback ports and an out block of monitor-class ports, hidden in a
// fresh workspace. the key is whatever node.name the sample carries.
func TestScarlettSinkBlocks(t *testing.T) {
	for _, tc := range []struct {
		sample   string
		channels int
	}{{elevenBaseline, 26}, {sevenBaseline, 18}} {
		snap := load(t, tc.sample)
		var sink string
		for _, n := range snap.Nodes {
			if n.MediaClass == "Audio/Sink" && strings.Contains(n.Name, "Focusrite_Scarlett") {
				sink = n.Name
			}
		}
		if sink == "" {
			t.Fatalf("%v: no scarlett sink", tc.sample)
		}
		v := New(workspace.New(), nil).Reconcile(snap)
		in := oneBlock(t, v, "node:"+sink+"|audio|in")
		out := oneBlock(t, v, "node:"+sink+"|audio|out")
		if len(in.Ports) != tc.channels || len(out.Ports) != tc.channels {
			t.Errorf("%v: %d in, %d out ports, want %d each", tc.sample, len(in.Ports), len(out.Ports), tc.channels)
		}
		if !in.Visible || in.Ports[0].Key != "playback_AUX0" {
			t.Errorf("%v: in block visible %v, first port '%v'", tc.sample, in.Visible, in.Ports[0].Key)
		}
		for _, p := range out.Ports {
			if p.Class != workspace.ClassMonitor {
				t.Errorf("%v: sink out port '%v' class '%v', want monitor", tc.sample, p.Key, p.Class)
			}
		}
		if out.Visible {
			t.Errorf("%v: monitor block visible in a fresh workspace", tc.sample)
		}
	}
}

func TestCategoryClasses(t *testing.T) {
	for _, name := range []string{elevenBaseline, sevenBaseline, desktop} {
		snap := load(t, name)
		v := New(workspace.New(), nil).Reconcile(snap)
		counts := map[string]int{}
		for _, b := range v.Blocks {
			for _, p := range b.Ports {
				port := snap.Ports[p.Serial]
				want := ""
				if strings.Contains(snap.Nodes[port.NodeSerial].MediaClass, "Video") {
					want = workspace.ClassVideo
				} else if port.Monitor {
					want = workspace.ClassMonitor
				}
				if p.Class != want {
					t.Errorf("%v: port %d class '%v', want '%v'", name, p.Serial, p.Class, want)
				}
				if p.Class != "" && p.Visible {
					t.Errorf("%v: port %d of class '%v' visible in a fresh workspace", name, p.Serial, p.Class)
				}
				counts[p.Class]++
			}
		}
		if counts[workspace.ClassVideo] == 0 || counts[workspace.ClassMonitor] == 0 {
			t.Errorf("%v: class counts %v", name, counts)
		}
	}
}

// the two gnome-remote-desktop audio-input nodes on eleven share every key. with a remembered record for that key,
// neither inherits it, both are presented as new with ordinals, and the record stays available.
func TestCollisionInheritsNothing(t *testing.T) {
	ws := workspace.New()
	ws.Records[grdAudioIn] = &workspace.Record{
		Key: workspace.Key{Class: "app:gnome-remote-desktop-daemon", Media: MediaAudio, Direction: DirectionIn},
		X:   2000, Y: 2000,
	}
	m := New(ws, nil)
	v := m.Reconcile(load(t, elevenBaseline))
	grd := blocksWithKey(v, grdAudioIn)
	if len(grd) != 2 {
		t.Fatalf("%d gnome-remote-desktop audio-in blocks, want 2", len(grd))
	}
	ordinals := map[int]bool{}
	for _, b := range grd {
		if b.Record != "" {
			t.Errorf("block '%v' inherited record '%v'", b.ID, b.Record)
		}
		if b.X == 2000 && b.Y == 2000 {
			t.Errorf("block '%v' took the remembered position", b.ID)
		}
		ordinals[b.Ordinal] = true
	}
	if !ordinals[1] || !ordinals[2] {
		t.Errorf("ordinals = %v, want 1 and 2", ordinals)
	}
	if !absent(v, grdAudioIn) {
		t.Error("the remembered record is not offered for association")
	}
	for rk := range ws.Records {
		if strings.HasPrefix(rk, grdAudioIn+"#") {
			t.Errorf("a record '%v' was created for an ambiguous block", rk)
		}
	}
}

func absent(v *View, record string) bool {
	for _, a := range v.Absent {
		if a.Record == record {
			return true
		}
	}
	return false
}

// an assignment made while an instance was the only candidate stays with it when a second arrives; the second is
// new; when the assigned instance leaves, the record returns to the pool.
func TestAssignmentHeldThenReleased(t *testing.T) {
	full := load(t, elevenBaseline)
	grd := nodesNamed(full, "gnome-remote-desktop-daemon")
	var audio []pipewire.Serial
	for _, s := range grd {
		if full.Nodes[s].MediaClass == "Stream/Input/Audio" {
			audio = append(audio, s)
		}
	}
	if len(audio) != 2 {
		t.Fatalf("%d grd audio nodes", len(audio))
	}
	ws := workspace.New()
	ws.Records[grdAudioIn] = &workspace.Record{
		Key: workspace.Key{Class: "app:gnome-remote-desktop-daemon", Media: MediaAudio, Direction: DirectionIn},
		X:   2000, Y: 2000,
	}
	m := New(ws, nil)
	v := m.Reconcile(without(full, audio[1]))
	if b := oneBlock(t, v, grdAudioIn); b.Record != grdAudioIn || b.X != 2000 {
		t.Fatalf("sole candidate not assigned: %+v", b)
	}
	v = m.Reconcile(full)
	held := 0
	for _, b := range blocksWithKey(v, grdAudioIn) {
		if b.Record == grdAudioIn {
			held++
			if !strings.HasPrefix(string(b.ID), strconv.FormatUint(uint64(audio[0]), 10)+"|") {
				t.Errorf("record moved to '%v'", b.ID)
			}
		}
	}
	if held != 1 {
		t.Errorf("record held by %d blocks, want 1", held)
	}
	v = m.Reconcile(without(full, audio[0]))
	if b := oneBlock(t, v, grdAudioIn); b.Record != "" {
		t.Errorf("record transferred to the remaining instance without association: %+v", b)
	}
	if !absent(v, grdAudioIn) {
		t.Error("released record not offered for association")
	}
}

// one returning instance that answers to two unassigned records takes neither.
func TestMirrorCaseTakesNothing(t *testing.T) {
	key := workspace.Key{Class: "app:REAPER", Media: MediaAudio, Direction: DirectionIn}
	ws := workspace.New()
	ws.Records[key.String()] = &workspace.Record{Key: key, X: 100}
	ws.Records[key.String()+"#2"] = &workspace.Record{Key: key, X: 900}
	v := New(ws, nil).Reconcile(load(t, elevenBaseline))
	if b := oneBlock(t, v, key.String()); b.Record != "" {
		t.Errorf("REAPER took '%v' from two candidates", b.Record)
	}
	if !absent(v, key.String()) || !absent(v, key.String()+"#2") {
		t.Error("both records should stay available")
	}
}

// with the Scarlett source and REAPER's in1 hidden and in2 visible, in2 shows a pin-row count and REAPER's title
// counts in1's connection.
func TestHiddenConnectionCounts(t *testing.T) {
	m, v := fresh(t, elevenBaseline)
	source := ""
	for _, b := range v.Blocks {
		if strings.HasPrefix(b.Key.Class, "node:alsa_input.usb-Focusrite_Scarlett") && b.Key.Direction == DirectionOut {
			source = string(b.ID)
		}
	}
	reaper := oneBlock(t, v, "app:REAPER|audio|in")
	if err := m.SetBlockHidden(BlockID(source), true); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPortHidden(reaper.ID, "REAPER:in1", true); err != nil {
		t.Fatal(err)
	}
	v = m.Refresh()
	reaper = oneBlock(t, v, "app:REAPER|audio|in")
	ports := map[string]Port{}
	for _, p := range reaper.Ports {
		ports[p.Key] = p
	}
	if p := ports["REAPER:in1"]; p.Visible || !p.Hidden {
		t.Errorf("in1 = %+v, want hidden", p)
	}
	if p := ports["REAPER:in2"]; !p.Visible || p.HiddenLinks != 1 {
		t.Errorf("in2 = %+v, want visible with one hidden connection", p)
	}
	if reaper.HiddenLinks != 1 {
		t.Errorf("REAPER title count = %d, want 1 (in1's link)", reaper.HiddenLinks)
	}
	for _, l := range v.Links {
		if l.In == ports["REAPER:in2"].Serial || l.In == ports["REAPER:in1"].Serial {
			t.Errorf("link %d to a hidden endpoint drawn", l.Serial)
		}
	}

	// Show hidden reveals without touching the workspace.
	before := len(m.ws.Records)
	m.SetShowHidden(true)
	v = m.Refresh()
	reaper = oneBlock(t, v, "app:REAPER|audio|in")
	for _, p := range reaper.Ports {
		if !p.Visible || p.HiddenLinks != 0 {
			t.Errorf("show hidden left '%v' = %+v", p.Key, p)
		}
	}
	if !reaper.Ports[0].Hidden && reaper.Ports[0].Key == "REAPER:in1" {
		t.Error("show hidden cleared the preference")
	}
	m.SetShowHidden(false)
	if len(m.ws.Records) != before || !m.ws.Records["app:REAPER|audio|in"].PortHidden("REAPER:in1") {
		t.Error("show hidden changed the workspace")
	}
}

// a bridge port whose port.name and object.path change (the device moved to another usb port, the alsa client
// renumbered) while its alias holds keeps its hidden preference.
func TestUSBMoveKeepsHiddenPort(t *testing.T) {
	base := load(t, elevenBaseline)
	m, v := fresh(t, elevenBaseline)
	lp := oneBlock(t, v, "midi:Launchpad Pro 2|midi|in")
	const hidden = "Launchpad Pro 2:Launchpad Pro 2 Standalone Port"
	if err := m.SetPortHidden(lp.ID, hidden, true); err != nil {
		t.Fatal(err)
	}

	moved := *base
	moved.Ports = map[pipewire.Serial]pipewire.Port{}
	for s, p := range base.Ports {
		if p.AliasPrefix == "Launchpad Pro 2" {
			props := map[string]string{}
			for k, val := range p.Props {
				props[k] = val
			}
			props["port.name"] = strings.Replace(props["port.name"], "usb-0000:80:14-0-4-1-", "usb-0000:80:14-0-2-", 1)
			props["object.path"] = strings.Replace(props["object.path"], "client_20", "client_28", 1)
			p.Props = props
			p.Name = props["port.name"]
			p.Serial += 100000
			s = p.Serial
		}
		moved.Ports[s] = p
	}

	m2 := New(m.ws, nil)
	v2 := m2.Reconcile(&moved)
	lp2 := oneBlock(t, v2, "midi:Launchpad Pro 2|midi|in")
	if lp2.Record == "" {
		t.Fatal("moved launchpad not recognized")
	}
	for _, p := range lp2.Ports {
		if p.Key == hidden && (p.Visible || !p.Hidden) {
			t.Errorf("'%v' lost its hidden preference: %+v", p.Key, p)
		}
		if p.Key != hidden && !p.Visible {
			t.Errorf("'%v' hidden without a preference", p.Key)
		}
	}
}

// no record is assigned on a connecting snapshot. two same-key candidates published in separate snapshots before the
// barrier both reach the live snapshot unassigned.
func TestNoAssignmentWhileConnecting(t *testing.T) {
	full := load(t, elevenBaseline)
	key := workspace.Key{Class: "app:REAPER", Media: MediaAudio, Direction: DirectionIn}
	ws := workspace.New()
	ws.Records[key.String()] = &workspace.Record{Key: key, X: 1200, Y: 40}
	m := New(ws, nil)
	v := m.Reconcile(withState(full, pipewire.Connecting))
	if len(v.Blocks) != 0 || !v.Stale || len(m.assigned) != 0 {
		t.Fatalf("connecting snapshot produced %d blocks, %d assignments", len(v.Blocks), len(m.assigned))
	}
	v = m.Reconcile(full)
	if b := oneBlock(t, v, key.String()); b.Record != key.String() || b.X != 1200 {
		t.Errorf("REAPER not recognized once live: %+v", b)
	}

	var audio []pipewire.Serial
	for _, s := range nodesNamed(full, "gnome-remote-desktop-daemon") {
		if full.Nodes[s].MediaClass == "Stream/Input/Audio" {
			audio = append(audio, s)
		}
	}
	ws = workspace.New()
	ws.Records[grdAudioIn] = &workspace.Record{
		Key: workspace.Key{Class: "app:gnome-remote-desktop-daemon", Media: MediaAudio, Direction: DirectionIn},
	}
	m = New(ws, nil)
	m.Reconcile(withState(without(full, audio[1]), pipewire.Connecting))
	m.Reconcile(withState(full, pipewire.Connecting))
	v = m.Reconcile(full)
	for _, b := range blocksWithKey(v, grdAudioIn) {
		if b.Record != "" {
			t.Errorf("'%v' assigned '%v' after arriving alone before the barrier", b.ID, b.Record)
		}
	}
}

// a reconnect releases every assignment; the last live view is shown stale until live returns.
func TestReconnectReleases(t *testing.T) {
	full := load(t, elevenBaseline)
	m, v := fresh(t, elevenBaseline)
	reaper := oneBlock(t, v, "app:REAPER|audio|in")
	if reaper.Record == "" {
		t.Fatal("REAPER unrecorded")
	}
	stale := m.Reconcile(withState(full, pipewire.Disconnected))
	if !stale.Stale || stale.State != pipewire.Disconnected || len(stale.Blocks) != len(v.Blocks) {
		t.Errorf("disconnected view: stale %v state %v blocks %d", stale.Stale, stale.State, len(stale.Blocks))
	}
	if len(m.assigned) != 0 {
		t.Error("assignments survived the disconnect")
	}
	v = m.Reconcile(full)
	if b := oneBlock(t, v, "app:REAPER|audio|in"); b.Record != reaper.Record || b.X != reaper.X || b.Y != reaper.Y {
		t.Errorf("REAPER after reconnect = %+v, want %+v", b, reaper)
	}
}

// a workspace saved from the eleven baseline and reloaded against the restart sample moves nothing.
func TestSaveReloadMovesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	m, before := fresh(t, elevenBaseline)
	if err := workspace.Save(m.ws, path); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	after := New(ws, nil).Reconcile(load(t, elevenRestart))

	byRecord := map[string]Block{}
	for _, b := range after.Blocks {
		if b.Record != "" {
			byRecord[b.Record] = b
		}
	}
	for _, b := range before.Blocks {
		if b.Record == "" {
			if len(blocksWithKey(after, b.Key.String())) < 2 {
				t.Errorf("'%v' was ambiguous before but not after", b.Key)
			}
			continue
		}
		a, ok := byRecord[b.Record]
		if !ok {
			t.Errorf("record '%v' not recognized after the restart", b.Record)
			continue
		}
		if a.X != b.X || a.Y != b.Y {
			t.Errorf("'%v' moved from (%v, %v) to (%v, %v)", b.Record, b.X, b.Y, a.X, a.Y)
		}
	}
}

// a block absent from a sample keeps its record, and keeps it through another save.
func TestAbsentBlockKeepsRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	m, _ := fresh(t, sevenBaseline)
	sevenRecords := len(m.ws.Records)
	m2 := New(m.ws, nil)
	v := m2.Reconcile(load(t, elevenBaseline))
	if err := workspace.Save(m2.ws, path); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.Records) < sevenRecords {
		t.Errorf("%d records after eleven, fewer than seven's %d", len(ws.Records), sevenRecords)
	}
	key := "midi:microKEY-25|midi|out"
	if _, ok := ws.Records[key]; !ok {
		t.Errorf("record '%v' was dropped", key)
	}
	if !absent(v, key) {
		t.Errorf("record '%v' not offered as absent", key)
	}
}

// an empty workspace is laid out once in two columns; later arrivals stack in a column at the right edge and
// existing blocks never move; a departure moves nothing.
func TestPlacement(t *testing.T) {
	full := load(t, elevenBaseline)
	reaperNode := nodesNamed(full, "REAPER")
	m := New(workspace.New(), nil)
	first := m.Reconcile(without(full, reaperNode...))
	maxX := float32(0)
	for _, b := range first.Blocks {
		want := float32(0)
		if b.Key.Direction == DirectionIn {
			want = snap(blockWidth + columnGap)
		}
		if b.X != want {
			t.Errorf("first layout put '%v' at x %v, want %v", b.Key, b.X, want)
		}
		if b.Visible && b.X+blockWidth > maxX {
			maxX = b.X + blockWidth
		}
	}
	second := m.Reconcile(full)
	positions := map[BlockID][2]float32{}
	for _, b := range first.Blocks {
		positions[b.ID] = [2]float32{b.X, b.Y}
	}
	var arrivals []Block
	for _, b := range second.Blocks {
		if p, ok := positions[b.ID]; ok {
			if p != [2]float32{b.X, b.Y} {
				t.Errorf("'%v' moved on an arrival", b.Key)
			}
			continue
		}
		arrivals = append(arrivals, b)
	}
	if len(arrivals) != 4 {
		t.Fatalf("%d arrivals, want REAPER's 4", len(arrivals))
	}
	ys := map[float32]bool{}
	for _, b := range arrivals {
		if b.X != snap(maxX+columnGap) {
			t.Errorf("arrival '%v' at x %v, want %v", b.Key, b.X, snap(maxX+columnGap))
		}
		if ys[b.Y] {
			t.Errorf("arrivals overlap at y %v", b.Y)
		}
		ys[b.Y] = true
	}
	third := m.Reconcile(without(full, reaperNode...))
	for _, b := range third.Blocks {
		if positions[b.ID] != [2]float32{b.X, b.Y} {
			t.Errorf("'%v' moved on a departure", b.Key)
		}
	}
}

// association binds a live block to an absent record of the same media and direction, remembering its key.
func TestAssociate(t *testing.T) {
	ws := workspace.New()
	other := workspace.Key{Class: "app:gnome-remote-desktop", Media: MediaAudio, Direction: DirectionIn}
	ws.Records[other.String()] = &workspace.Record{Key: other, X: 3000, Y: 60}
	ws.Records["app:x|midi|in"] = &workspace.Record{Key: workspace.Key{Class: "app:x", Media: MediaMIDI, Direction: DirectionIn}}
	m := New(ws, nil)
	v := m.Reconcile(load(t, elevenBaseline))
	grd := blocksWithKey(v, grdAudioIn)
	if err := m.Associate(grd[0].ID, "app:x|midi|in"); err == nil {
		t.Error("associated across media")
	}
	if err := m.Associate(grd[0].ID, other.String()); err != nil {
		t.Fatal(err)
	}
	if err := m.Associate(grd[1].ID, other.String()); err == nil {
		t.Error("a held record was associated twice")
	}
	v = m.Refresh()
	b, _ := v.Block(grd[0].ID)
	if b.Record != other.String() || b.X != 3000 || b.Y != 60 {
		t.Errorf("associated block = %+v", b)
	}
	if !ws.Records[other.String()].Matches(grd[0].Key) {
		t.Error("the block's key was not remembered on the record")
	}
}

// gestures on the two colliding blocks create separate records; reloaded, neither is assigned automatically and
// neither overwrote the other.
func TestCollidingGesturesKeepSeparateRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	m, v := fresh(t, elevenBaseline)
	grd := blocksWithKey(v, grdAudioIn)
	if err := m.Move(grd[0].ID, 1000, 100); err != nil {
		t.Fatal(err)
	}
	if err := m.Move(grd[1].ID, 1000, 500); err != nil {
		t.Fatal(err)
	}
	if err := m.SetBlockHidden(grd[1].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Save(m.ws, path); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var recs []*workspace.Record
	for rk, r := range ws.Records {
		if strings.HasPrefix(rk, grdAudioIn) {
			recs = append(recs, r)
		}
	}
	if len(recs) != 2 {
		t.Fatalf("%d records for the colliding key, want 2", len(recs))
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Y < recs[j].Y })
	if recs[0].Y != 100 || recs[0].Hidden || recs[1].Y != 500 || !recs[1].Hidden {
		t.Errorf("records = %+v, %+v", recs[0], recs[1])
	}
	v = New(ws, nil).Reconcile(load(t, elevenBaseline))
	for _, b := range blocksWithKey(v, grdAudioIn) {
		if b.Record != "" {
			t.Errorf("'%v' assigned '%v' on reload", b.ID, b.Record)
		}
	}
}

// a node whose device did not resolve is still a device-backed node: keyed node:, never app:.
func TestUnresolvedDeviceStaysNodeClass(t *testing.T) {
	snap := &pipewire.Snapshot{
		State: pipewire.Live,
		Nodes: map[pipewire.Serial]pipewire.Node{
			5: {Serial: 5, Name: "alsa_input.test", HasDevice: true},
		},
		Ports: map[pipewire.Serial]pipewire.Port{
			6: {Serial: 6, NodeSerial: 5, Media: pipewire.MediaAudio, Direction: pipewire.DirectionOut, Name: "capture_FL"},
		},
		Unresolved: 1,
	}
	v := New(workspace.New(), nil).Reconcile(snap)
	b := oneBlock(t, v, "node:alsa_input.test|audio|out")
	if b.Ports[0].Key != "capture_FL" {
		t.Errorf("port key = %q, want the port name", b.Ports[0].Key)
	}
}

func TestPropertylessPortNeverPanics(t *testing.T) {
	snap := &pipewire.Snapshot{
		State: pipewire.Live,
		Nodes: map[pipewire.Serial]pipewire.Node{
			5: {Serial: 5},
		},
		Ports: map[pipewire.Serial]pipewire.Port{
			9:  {Serial: 9},
			10: {Serial: 10, NodeSerial: 5},
			11: {Serial: 11, NodeSerial: 5, Media: pipewire.MediaAudio, Direction: pipewire.DirectionOut},
			12: {Serial: 12, NodeSerial: 404, Media: pipewire.MediaAudio, Direction: pipewire.DirectionIn},
		},
		Links: map[pipewire.Serial]pipewire.Link{
			20: {Serial: 20, OutPort: 11, InPort: 12},
			21: {Serial: 21},
		},
	}
	m := New(workspace.New(), nil)
	v := m.Reconcile(snap)
	if len(v.Blocks) != 1 {
		t.Fatalf("%d blocks, want the one unkeyed block", len(v.Blocks))
	}
	b := v.Blocks[0]
	if b.Keyed || b.Record != "" || len(m.ws.Records) != 0 {
		t.Errorf("unkeyed block recorded: %+v", b)
	}
	if err := m.Move(b.ID, 10, 10); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPortHidden(b.ID, "", true); err == nil {
		t.Error("a keyless port was hidden by key")
	}
	if len(m.ws.Records) != 0 {
		t.Error("a gesture on an unkeyed block created a record")
	}
	if b, _ := m.Refresh().Block(b.ID); b.X != 10 {
		t.Error("unkeyed block did not keep its session position")
	}
}

// a view built while the connection is not live carries that state and is stale; Refresh never hands back the last
// live view as current.
func TestRefreshWhileNotLive(t *testing.T) {
	full := load(t, elevenBaseline)
	m := New(workspace.New(), nil)

	// 1. nothing seen yet.
	if v := m.Refresh(); !v.Stale || v.State != pipewire.Connecting || v.LiveGeneration != 0 || v.Reason != "" || len(v.Blocks) != 0 {
		t.Errorf("fresh refresh = state %v stale %v gen %d reason %q blocks %d", v.State, v.Stale, v.LiveGeneration, v.Reason, len(v.Blocks))
	}

	// 2. live.
	live := m.Reconcile(full)
	if v := m.Refresh(); v.Stale || v.State != pipewire.Live || v.LiveGeneration != full.Generation {
		t.Errorf("live refresh = state %v stale %v gen %d", v.State, v.Stale, v.LiveGeneration)
	}
	reaper := oneBlock(t, live, "app:REAPER|audio|in")

	// 3. disconnected, then connecting: the state, its reason, and the generation the blocks came from.
	down := withState(full, pipewire.Disconnected)
	down.Error = "connection error (broken pipe)"
	down.Generation = full.Generation + 1
	for _, s := range []*pipewire.Snapshot{down, withState(full, pipewire.Connecting)} {
		m.Reconcile(s)
		v := m.Refresh()
		if !v.Stale || v.State != s.State || v.Reason != s.Error || v.LiveGeneration != full.Generation {
			t.Errorf("%v refresh = state %v stale %v reason %q gen %d", s.State, v.State, v.Stale, v.Reason, v.LiveGeneration)
		}
		if len(v.Blocks) != len(live.Blocks) {
			t.Errorf("%v refresh dropped the last graph: %d blocks", s.State, len(v.Blocks))
		}

		// 4. operations fail while not live, and the next refresh stays stale.
		if err := m.Move(reaper.ID, 0, 0); err == nil {
			t.Errorf("move accepted while %v", s.State)
		}
		if v := m.Refresh(); !v.Stale || v.State != s.State {
			t.Errorf("refresh after a refused move = state %v stale %v", v.State, v.Stale)
		}
	}

	// 5. the live view handed out earlier was never modified.
	if live.State != pipewire.Live || live.Stale || live.Reason != "" {
		t.Errorf("the earlier live view changed: state %v stale %v reason %q", live.State, live.Stale, live.Reason)
	}

	// 6. live again.
	again := *full
	again.Generation = full.Generation + 2
	m.Reconcile(&again)
	if v := m.Refresh(); v.Stale || v.State != pipewire.Live || v.LiveGeneration != again.Generation || v.Reason != "" {
		t.Errorf("refresh after live returned = state %v stale %v gen %d reason %q", v.State, v.Stale, v.LiveGeneration, v.Reason)
	}
}

// show hidden and reconcile write nothing beyond what changed.
func TestShowHiddenWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	m := New(workspace.New(), workspace.NewStore(path, 10*time.Millisecond))
	m.Reconcile(load(t, elevenBaseline))
	time.Sleep(60 * time.Millisecond)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	m.SetShowHidden(true)
	m.Refresh()
	m.SetShowHidden(false)
	m.Reconcile(load(t, elevenBaseline))
	time.Sleep(60 * time.Millisecond)
	again, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(info.ModTime()) {
		t.Error("the workspace was written without a change")
	}
	m.Close()
}
