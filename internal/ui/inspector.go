package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/dfx"
	"github.com/michaelquigley/dfx/fonts"
	"github.com/michaelquigley/patchbay/internal/model"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// inspector layout, in pixels.
const (
	labelColumn   = 96 // the label/value tables' label column
	bodyPadding   = 10 // between the panel's edges and its body
	sectionSpaces = 2  // spacing units before each section
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
	// filter narrows the properties tree by key substring.
	filter string
}

func newInspector(actions inspectorActions) *inspector {
	return &inspector{actions: actions, associate: map[model.BlockID]string{}}
}

// wrapped draws text wrapped at the available width, verbatim: property values can hold anything, including format
// verbs.
func wrapped(s string) {
	imgui.TextWrapped(strings.ReplaceAll(s, "%", "%%"))
}

// mono draws wrapped text in the monospace font: paths, names, keys, and values.
func mono(s string) {
	dfx.PushFont(dfx.MonospaceFont)
	wrapped(s)
	dfx.PopFont()
}

// section opens a section: two spacing units, then a separator with its header.
func section(header string) {
	for i := 0; i < sectionSpaces; i++ {
		imgui.Spacing()
	}
	imgui.SeparatorText(header)
}

// pairs is a label/value table: labels in a fixed narrow column, values wrapped in the rest.
type pairs struct{ open bool }

func beginPairs(id string) pairs {
	open := imgui.BeginTableV(id, 2, imgui.TableFlagsSizingFixedFit, imgui.Vec2{}, 0)
	if open {
		imgui.TableSetupColumnV("label", imgui.TableColumnFlagsWidthFixed, labelColumn, 0)
		imgui.TableSetupColumnV("value", imgui.TableColumnFlagsWidthStretch, 1, 0)
	}
	return pairs{open: open}
}

func (p pairs) row(label, value string, monospace bool) {
	if !p.open {
		return
	}
	imgui.TableNextRow()
	imgui.TableNextColumn()
	imgui.TextDisabled(strings.ReplaceAll(label, "%", "%%"))
	imgui.TableNextColumn()
	if monospace {
		mono(value)
	} else {
		wrapped(value)
	}
}

func (p pairs) end() {
	if p.open {
		imgui.EndTable()
	}
}

// title draws the object's name with its glyph in its media hue: the glyph carries the color, the name stays in the
// text color.
func title(glyph string, hue imgui.Vec4, name string) {
	imgui.Spacing()
	imgui.TextColored(hue, glyph)
	imgui.SameLine()
	wrapped(name)
}

// draw lays the inspector out in its own padded, scrolling body: the panel's content region has no scrollbar and no
// padding of its own.
func (in *inspector) draw(v *model.View, snap *pipewire.Snapshot, sel selection, requests []string) {
	imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, imgui.Vec2{X: bodyPadding, Y: bodyPadding})
	imgui.BeginChildStrV("##inspector-body", imgui.Vec2{}, imgui.ChildFlagsAlwaysUseWindowPadding, imgui.WindowFlagsNone)
	imgui.PopStyleVar()
	in.body(v, snap, sel, requests)
	imgui.EndChild()
}

func (in *inspector) body(v *model.View, snap *pipewire.Snapshot, sel selection, requests []string) {
	if v.Stale {
		wrapped("the graph is not current; the inspector shows what was last observed")
		imgui.Spacing()
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
		in.defaults(snap)
	default:
		wrapped(fmt.Sprintf("%d blocks and %d links selected", len(blocks), len(links)))
		imgui.Spacing()
		imgui.TextDisabled("H hides the selected blocks; Delete removes the selected links")
	}
	in.requests(requests)
}

func (in *inspector) requests(requests []string) {
	section("requests")
	if len(requests) == 0 {
		imgui.TextDisabled("none pending or recently failed")
	}
	for _, r := range requests {
		wrapped(r)
	}
}

func (in *inspector) block(v *model.View, snap *pipewire.Snapshot, b model.Block) {
	name, _ := blockTitle(b)
	title(ownerGlyph(b.Owner), mediaHue(b.Key.Media), name)
	node, hasNode := snap.Nodes[b.Node]

	section("identity")
	p := beginPairs("##identity")
	p.row("key", b.Key.String(), true)
	if b.Record != "" {
		p.row("record", b.Record, true)
	} else {
		p.row("record", b.Gap.String(), false)
	}
	if hasNode {
		p.row("node", node.Name, true)
		p.row("serial", strconv.FormatUint(uint64(node.Serial), 10), false)
		p.row("id", strconv.FormatUint(uint64(node.ID), 10), false)
		if node.DeviceSerial != 0 {
			p.row("device serial", strconv.FormatUint(uint64(node.DeviceSerial), 10), false)
		}
	}
	p.end()

	section("state")
	p = beginPairs("##state")
	p.row("connection", v.State.String(), false)
	if hasNode {
		p.row("node", node.State, false)
	}
	if b.Hidden {
		p.row("block", "hidden", false)
	}
	p.end()

	if hasNode {
		section("xruns")
		p = beginPairs("##xruns")
		for _, r := range xrunRows(snap.Metrics, node.Serial) {
			p.row(r[0], r[1], false)
		}
		p.end()
	}

	in.ports(b, snap)

	if hasNode {
		if names := defaultsNaming(snap, node); len(names) > 0 {
			section("metadata")
			imgui.TextDisabled("default metadata naming it")
			for _, e := range names {
				mono(e)
			}
		}
		section("properties")
		in.properties("node properties", node.Props)
	}

	section("actions")
	in.blockActions(v, b)
}

// ports lists the block's ports in two tables: the visible ones with their hide buttons, and the hidden ones with
// their unhide buttons. a port with no key says it cannot be hidden.
func (in *inspector) ports(b model.Block, snap *pipewire.Snapshot) {
	var shown, hidden []model.Port
	for _, p := range b.Ports {
		if p.Hidden {
			hidden = append(hidden, p)
		} else {
			shown = append(shown, p)
		}
	}
	if len(shown) > 0 {
		section("ports")
		in.portTable("##ports", b, shown, snap)
	}
	if len(hidden) > 0 {
		section("hidden ports")
		in.portTable("##hidden-ports", b, hidden, snap)
	}
}

func (in *inspector) portTable(id string, b model.Block, ports []model.Port, snap *pipewire.Snapshot) {
	if !imgui.BeginTableV(id, 3, imgui.TableFlagsSizingFixedFit, imgui.Vec2{}, 0) {
		return
	}
	imgui.TableSetupColumnV("port", imgui.TableColumnFlagsWidthStretch, 1, 0)
	imgui.TableSetupColumnV("ids", imgui.TableColumnFlagsWidthStretch, 1, 0)
	imgui.TableSetupColumnV("action", imgui.TableColumnFlagsWidthFixed, 0, 0)
	for _, p := range ports {
		imgui.PushIDStr(strconv.FormatUint(uint64(p.Serial), 10))
		imgui.TableNextRow()
		imgui.TableNextColumn()
		mono(p.Label)
		imgui.TableNextColumn()
		ids := fmt.Sprintf("serial %d", p.Serial)
		if port, ok := snap.Ports[p.Serial]; ok {
			ids += fmt.Sprintf(" · id %d", port.ID)
		}
		if p.Class != "" {
			ids += " · " + p.Class
		}
		imgui.TextDisabled(ids)
		imgui.TableNextColumn()
		switch {
		case p.Key == "":
			imgui.TextDisabled("cannot be hidden: it has no port key")
		case p.Hidden:
			if imgui.SmallButton("unhide") {
				in.do(in.actions.SetPortHidden(b.ID, p.Key, false))
			}
		default:
			if imgui.SmallButton("hide") {
				in.do(in.actions.SetPortHidden(b.ID, p.Key, true))
			}
		}
		imgui.PopID()
	}
	imgui.EndTable()
}

// blockActions is the button row, then association.
func (in *inspector) blockActions(v *model.View, b model.Block) {
	if b.Hidden {
		if imgui.Button("unhide block") {
			in.do(in.actions.SetBlockHidden(b.ID, false))
		}
	} else if imgui.Button("hide block") {
		in.do(in.actions.SetBlockHidden(b.ID, true))
	}
	in.associateControl(v, b)
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
	imgui.Spacing()
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
	media := ""
	if p, ok := snap.Ports[l.OutPort]; ok {
		media = mediaName(p.Media)
	}
	title(fonts.ICON_LINK, mediaHue(media), fmt.Sprintf("link %d", serial))

	section("identity")
	p := beginPairs("##link-identity")
	p.row("from", portLabel(v, l.OutPort), true)
	p.row("to", portLabel(v, l.InPort), true)
	p.row("serial", strconv.FormatUint(uint64(l.Serial), 10), false)
	p.row("id", strconv.FormatUint(uint64(l.ID), 10), false)
	p.end()

	section("state")
	p = beginPairs("##link-state")
	p.row("link", l.State, false)
	if l.Error != "" {
		p.row("error", "error: "+l.Error, false)
	}
	if l.CreatedHere {
		p.row("provenance", "created here", false)
	} else {
		p.row("provenance", "observed: this process has no evidence it created this link", false)
	}
	p.end()

	section("properties")
	in.properties("link properties", l.Props)
}

// defaults is the nothing-selected view of the default metadata object: a table of key, subject, value.
func (in *inspector) defaults(snap *pipewire.Snapshot) {
	rows := defaultsRows(snap)
	if len(rows) == 0 {
		return
	}
	section("default metadata")
	if !imgui.BeginTableV("##defaults", 3, imgui.TableFlagsSizingStretchProp|imgui.TableFlagsRowBg, imgui.Vec2{}, 0) {
		return
	}
	imgui.TableSetupColumnV("key", imgui.TableColumnFlagsWidthStretch, 1, 0)
	imgui.TableSetupColumnV("subject", imgui.TableColumnFlagsWidthStretch, 1, 0)
	imgui.TableSetupColumnV("value", imgui.TableColumnFlagsWidthStretch, 2, 0)
	imgui.TableHeadersRow()
	for _, r := range rows {
		imgui.TableNextRow()
		imgui.TableNextColumn()
		mono(r.key)
		imgui.TableNextColumn()
		wrapped(r.subject)
		imgui.TableNextColumn()
		mono(r.value)
	}
	imgui.EndTable()
}

// properties is the full property map, collapsed by default, with a filter box that narrows keys by substring.
func (in *inspector) properties(label string, props map[string]string) {
	if len(props) == 0 {
		return
	}
	imgui.SetNextItemWidth(-1)
	imgui.InputTextWithHint("##filter", "filter keys", &in.filter, imgui.InputTextFlagsNone, nil)
	if !imgui.TreeNodeExStrV(label, imgui.TreeNodeFlagsNone) {
		return
	}
	p := beginPairs("##" + label)
	for _, k := range filteredKeys(props, in.filter) {
		p.row(k, props[k], true)
	}
	p.end()
	imgui.TreePop()
}

// filteredKeys returns the property keys containing filter, sorted.
func filteredKeys(props map[string]string, filter string) []string {
	keys := make([]string, 0, len(props))
	for k := range props {
		if strings.Contains(k, filter) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
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

type defaultsRow struct{ key, subject, value string }

// defaultsRows lists the default metadata object itself: each entry with what its subject is. an entry whose subject
// did not resolve, or whose subject has gone, says so rather than being left out.
func defaultsRows(snap *pipewire.Snapshot) []defaultsRow {
	var out []defaultsRow
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
		out = append(out, defaultsRow{key: e.Key, subject: subject, value: e.Value})
	}
	return out
}

func mediaName(m pipewire.Media) string {
	switch m {
	case pipewire.MediaAudio:
		return model.MediaAudio
	case pipewire.MediaMIDI:
		return model.MediaMIDI
	case pipewire.MediaVideo:
		return model.MediaVideo
	}
	return ""
}

func (in *inspector) do(err error) {
	if err != nil {
		dl.Warnf("inspector action not applied: %v", err)
	}
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

// xrunRows are the inspector's xruns section for one node: its record, or why there is none. with the profiler not
// bound there is no record to show, whatever was counted before.
func xrunRows(m pipewire.MetricsSummary, serial pipewire.Serial) [][2]string {
	if !m.Available {
		return [][2]string{{"counter", monitoringUnavailable}}
	}
	rec, ok := m.Nodes[serial]
	if !ok {
		return [][2]string{{"counter", "no profiler data for this node yet"}}
	}
	var rows [][2]string
	if !rec.Available {
		rows = append(rows, [2]string{"counter", "unavailable: its profiler block carries no counter"})
	} else {
		last := "none observed"
		if !rec.LastIncrease.IsZero() {
			last = rec.LastIncrease.Format(time.TimeOnly)
		}
		rows = append(rows, [2]string{"total", strconv.FormatUint(rec.Total, 10)}, [2]string{"new", strconv.FormatUint(rec.New, 10)},
			[2]string{"last increase", last})
	}
	if rec.ClockGuard {
		return append(rows, [2]string{"guard", "lifetime guard: clock-based"})
	}
	return append(rows, [2]string{"guard", "lifetime guard: ordering only (driver clock not current at first pod)"})
}
