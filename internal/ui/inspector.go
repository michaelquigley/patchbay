package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/patchbay/internal/model"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// inspectorActions is what the inspector may do: presentation operations only. hide, unhide, and associate never
// touch routing.
type inspectorActions interface {
	SetBlockHidden(id model.BlockID, hidden bool) error
	SetPortHidden(id model.BlockID, portKey string, hidden bool) error
	Associate(id model.BlockID, record string) error
}

// inspector shows the selected object as observed: its properties, ids and serial, link state and provenance, the
// default metadata that names it, why it has no record or a port cannot be hidden, and the presentation actions.
type inspector struct {
	actions inspectorActions
	// associate holds the record chosen in the association combo, per block.
	associate map[model.BlockID]string
}

func newInspector(actions inspectorActions) *inspector {
	return &inspector{actions: actions, associate: map[model.BlockID]string{}}
}

// text draws a line verbatim: property values can hold anything, including format verbs.
func text(s string) {
	imgui.TextUnformatted(s)
}

func wrapped(s string) {
	imgui.TextWrapped(strings.ReplaceAll(s, "%", "%%"))
}

// inspectSource is the snapshot the inspector resolves selection serials against: the one the drawn view came from.
// a live view came from the current snapshot; a stale view from the retained last-live snapshot, if its session and
// generation are the view's. anything else is nil: a serial from one graph must never be looked up in another.
func inspectSource(v *model.View, current, lastLive *pipewire.Snapshot) *pipewire.Snapshot {
	if !v.Stale {
		return current
	}
	if lastLive != nil && lastLive.Session == v.Session && lastLive.Generation == v.LiveGeneration {
		return lastLive
	}
	return nil
}

// draw shows the selection, resolved against snap, the snapshot the view came from (see inspectSource); requests
// come from the current snapshot.
func (in *inspector) draw(v *model.View, snap *pipewire.Snapshot, sel selection, requests []string) {
	if v.Stale {
		wrapped("the graph is not current; the inspector shows what was last observed")
		imgui.Separator()
	}
	if snap == nil {
		wrapped("details unavailable: the graph has changed")
		in.requests(requests)
		return
	}
	blocks := make([]model.Block, 0, len(sel.blocks))
	for _, b := range v.Blocks {
		if sel.blocks[b.ID] {
			blocks = append(blocks, b)
		}
	}
	links := make([]pipewire.Serial, 0, len(sel.links))
	for s := range sel.links {
		links = append(links, s)
	}
	sort.Slice(links, func(i, j int) bool { return links[i] < links[j] })

	switch {
	case len(blocks) == 1 && len(links) == 0:
		in.block(v, snap, blocks[0])
	case len(blocks) == 0 && len(links) == 1:
		in.link(v, snap, links[0])
	case len(blocks) == 0 && len(links) == 0:
		wrapped("select a block or a link")
		if lines := defaultsLines(snap); len(lines) > 0 && imgui.TreeNodeStr("default metadata") {
			for _, l := range lines {
				wrapped(l)
			}
			imgui.TreePop()
		}
	default:
		text(fmt.Sprintf("%d blocks and %d links selected", len(blocks), len(links)))
		wrapped("H hides the selected blocks; Delete removes the selected links")
	}

	in.requests(requests)
}

func (in *inspector) requests(requests []string) {
	imgui.SeparatorText("requests")
	if len(requests) == 0 {
		imgui.TextDisabled("none pending or recently failed")
	}
	for _, r := range requests {
		wrapped(r)
	}
}

func (in *inspector) block(v *model.View, snap *pipewire.Snapshot, b model.Block) {
	title, _ := blockTitle(b)
	imgui.SeparatorText(ownerGlyph(b.Owner) + " " + title)
	text("key: " + b.Key.String())
	if b.Record != "" {
		text("record: " + b.Record)
	} else {
		wrapped(b.Gap.String())
	}
	if b.Hidden {
		if imgui.Button("unhide block") {
			in.do(in.actions.SetBlockHidden(b.ID, false))
		}
	} else if imgui.Button("hide block") {
		in.do(in.actions.SetBlockHidden(b.ID, true))
	}
	in.associateControl(v, b)

	imgui.SeparatorText("ports")
	for _, p := range b.Ports {
		imgui.PushIDStr(strconv.FormatUint(uint64(p.Serial), 10))
		line := fmt.Sprintf("%s · serial %d", p.Label, p.Serial)
		if port, ok := snap.Ports[p.Serial]; ok {
			line += fmt.Sprintf(" · id %d", port.ID)
		}
		if p.Class != "" {
			line += " · " + p.Class
		}
		text(line)
		switch {
		case p.Key == "":
			imgui.TextDisabled("cannot be hidden: it has no port key")
		case p.Hidden:
			imgui.SameLine()
			if imgui.SmallButton("unhide") {
				in.do(in.actions.SetPortHidden(b.ID, p.Key, false))
			}
		default:
			imgui.SameLine()
			if imgui.SmallButton("hide") {
				in.do(in.actions.SetPortHidden(b.ID, p.Key, true))
			}
		}
		imgui.PopID()
	}

	node, ok := snap.Nodes[b.Node]
	if !ok {
		return
	}
	imgui.SeparatorText("node")
	text(fmt.Sprintf("%s · serial %d · id %d · %s", node.Name, node.Serial, node.ID, node.State))
	if node.DeviceSerial != 0 {
		text(fmt.Sprintf("device serial %d", node.DeviceSerial))
	}
	if names := defaultsNaming(snap, node); len(names) > 0 {
		imgui.SeparatorText("default metadata naming it")
		for _, e := range names {
			wrapped(e)
		}
	}
	properties("node properties", node.Props)
}

// associateControl offers the remembered records with no live block and the block's media and direction.
func (in *inspector) associateControl(v *model.View, b model.Block) {
	if !b.Keyed {
		return
	}
	var records []string
	for _, a := range v.Absent {
		if a.Key.Media == b.Key.Media && a.Key.Direction == b.Key.Direction {
			records = append(records, a.Record)
		}
	}
	if len(records) == 0 {
		return
	}
	chosen := in.associate[b.ID]
	preview := chosen
	if preview == "" {
		preview = "associate with…"
	}
	imgui.SetNextItemWidth(-1)
	if imgui.BeginCombo("##associate", preview) {
		for _, r := range records {
			if imgui.SelectableBool(r) {
				in.associate[b.ID] = r
			}
		}
		imgui.EndCombo()
	}
	if chosen != "" && imgui.Button("associate") {
		in.do(in.actions.Associate(b.ID, chosen))
		delete(in.associate, b.ID)
	}
}

func (in *inspector) link(v *model.View, snap *pipewire.Snapshot, serial pipewire.Serial) {
	l, ok := snap.Links[serial]
	if !ok {
		wrapped(fmt.Sprintf("link %d is no longer observed", serial))
		return
	}
	imgui.SeparatorText(fmt.Sprintf("link %d", serial))
	text(portLabel(v, l.OutPort) + " →")
	text("  " + portLabel(v, l.InPort))
	text(fmt.Sprintf("serial %d · id %d · %s", l.Serial, l.ID, l.State))
	if l.Error != "" {
		wrapped("error: " + l.Error)
	}
	if l.CreatedHere {
		text("created here")
	} else {
		wrapped("observed: this process has no evidence it created this link")
	}
	properties("link properties", l.Props)
}

// defaultsNaming returns the default metadata entries that name a node: by the serial its subject resolved to, or by
// its node.name in a json value. a subject id is never compared with the node's id, which may be reused.
func defaultsNaming(snap *pipewire.Snapshot, n pipewire.Node) []string {
	var out []string
	for _, e := range snap.Default {
		if (e.SubjectSerial != 0 && e.SubjectSerial == n.Serial) || (n.Name != "" && strings.Contains(e.Value, `"`+n.Name+`"`)) {
			out = append(out, fmt.Sprintf("%s = %s", e.Key, e.Value))
		}
	}
	return out
}

// defaultsLines lists the default metadata object itself: each entry with what its subject is. an entry whose subject
// did not resolve, or whose subject has gone, says so rather than being left out.
func defaultsLines(snap *pipewire.Snapshot) []string {
	var out []string
	for _, e := range snap.Default {
		subject := "global"
		switch {
		case e.Subject == 0:
		case e.SubjectSerial == 0:
			subject = fmt.Sprintf("subject unresolved (id %d)", e.Subject)
		default:
			subject = fmt.Sprintf("serial %d", e.SubjectSerial)
			if n, ok := snap.Nodes[e.SubjectSerial]; ok {
				subject = n.Name + " · " + subject
			}
		}
		out = append(out, fmt.Sprintf("%s = %s · %s", e.Key, e.Value, subject))
	}
	return out
}

func properties(label string, props map[string]string) {
	if len(props) == 0 || !imgui.TreeNodeStr(label) {
		return
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		wrapped(k + " = " + props[k])
	}
	imgui.TreePop()
}

func (in *inspector) do(err error) {
	if err != nil {
		dl.Warnf("inspector action not applied: %v", err)
	}
}
