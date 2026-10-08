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
	"github.com/michaelquigley/patchbay/internal/scarlett"
)

// inspector layout, in pixels.
const (
	labelColumn = 96 // the label/value tables' label column
	bodyPadding = 10 // between the panel's edges and its body
	sectionGap  = 12 // the space above each section header, so one section's end reads apart from the next's band
)

// inspectorActions is what the inspector may do: presentation operations only. hide, unhide, and associate never
// touch routing.
type inspectorActions interface {
	SetBlockHidden(id model.BlockID, hidden bool) error
	SetPortHidden(id model.BlockID, portKey string, hidden bool) error
	Associate(id model.BlockID, record string) error
}

// inspector shows the selected object as observed: its properties, ids and serial, link state and provenance, the
// reasons it has no record or a port cannot be hidden, and the presentation actions.
type inspector struct {
	actions inspectorActions
	// hardware annotates Scarlett capture ports; nil in sample mode, where nothing is live.
	hardware scarlett.Annotator
	// associate holds the record chosen in the association combo, per block.
	associate map[model.BlockID]string
	// filter narrows the properties tree by key substring.
	filter string
}

func newInspector(actions inspectorActions, hardware scarlett.Annotator) *inspector {
	return &inspector{actions: actions, hardware: hardware, associate: map[model.BlockID]string{}}
}

// hardwareLine is a port's hardware annotation: the source routed to its Scarlett capture channel, or why that is
// unavailable; empty for a port on a device with no Scarlett card, which is not annotated.
func hardwareLine(hardware scarlett.Annotator, port pipewire.Port) (string, bool) {
	if hardware == nil {
		return "", false
	}
	source, ok := hardware.SourceFor(port)
	switch {
	case ok:
		return "hardware source: " + source, true
	case source != "":
		return "hardware source: unavailable (" + source + ")", false
	}
	return "", false
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

// section opens a section under a full-width header band, open by default, and reports whether it is open: the
// caller draws the section's content only then. imgui keeps each header's open state by its label, so a section the
// operator folds away stays folded as the selection changes.
func section(header string) bool {
	return sectionV(header, true)
}

// sectionV is section with its default state: closed, for a section too long to show unasked.
func sectionV(header string, open bool) bool {
	imgui.Dummy(imgui.Vec2{Y: sectionGap})
	flags := imgui.TreeNodeFlagsNone
	if open {
		flags = imgui.TreeNodeFlagsDefaultOpen
	}
	return imgui.CollapsingHeaderTreeNodeFlagsV(header, flags)
}

// pairs is a label/value table: labels in a fixed narrow column, values wrapped in the rest.
type pairs struct{ open bool }

func beginPairs(id string) pairs {
	return beginPairsV(id, labelColumn)
}

// beginPairsV opens a label/value table with the given label column width.
func beginPairsV(id string, labels float32) pairs {
	open := imgui.BeginTableV(id, 2, imgui.TableFlagsSizingFixedFit, imgui.Vec2{}, 0)
	if open {
		imgui.TableSetupColumnV("label", imgui.TableColumnFlagsWidthFixed, labels, 0)
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
	if !section("requests") {
		return
	}
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

	if section("identity") {
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
	}

	if section("state") {
		p := beginPairs("##state")
		p.row("connection", v.State.String(), false)
		if hasNode {
			p.row("node", node.State, false)
		}
		if b.Hidden {
			p.row("block", "hidden", false)
		}
		p.end()
	}

	if hasNode && section("xruns") {
		p := beginPairs("##xruns")
		for _, r := range xrunRows(snap.Metrics, node.Serial) {
			p.row(r[0], r[1], false)
		}
		p.end()
	}

	in.ports(b, snap)

	if hasNode {
		in.properties(node.Props)
	}

	if section("actions") {
		in.blockActions(v, b)
	}
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
	if len(shown) > 0 && section("ports") {
		in.portTable("##ports", b, shown, snap)
	}
	if len(hidden) > 0 && section("hidden ports") {
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
		if port, ok := snap.Ports[p.Serial]; ok {
			if line, valid := hardwareLine(in.hardware, port); valid {
				wrapped(line)
			} else if line != "" {
				imgui.TextDisabled(line)
			}
		}
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

// associateControl offers the remembered records with no live block and the block's media and direction, records from
// the block's own device first and marked as such. the order is a hint; nothing is chosen for the operator.
func (in *inspector) associateControl(v *model.View, b model.Block) {
	if !b.Keyed {
		return
	}
	candidates := v.Candidates(b)
	if len(candidates) == 0 {
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
		for _, c := range candidates {
			if imgui.SelectableBool(candidateLabel(b, c)) {
				in.associate[b.ID] = c.Record
			}
		}
		imgui.EndCombo()
	}
	if chosen != "" && imgui.Button("associate") {
		in.do(in.actions.Associate(b.ID, chosen))
		delete(in.associate, b.ID)
	}
}

// candidateLabel is one chooser entry: the record key, marked when the record was observed on the block's own device.
func candidateLabel(b model.Block, c model.AbsentRecord) string {
	if b.Hardware != "" && c.Device == b.Hardware {
		return c.Record + " · same device"
	}
	return c.Record
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

	if section("identity") {
		p := beginPairs("##link-identity")
		p.row("from", portLabel(v, l.OutPort), true)
		p.row("to", portLabel(v, l.InPort), true)
		p.row("serial", strconv.FormatUint(uint64(l.Serial), 10), false)
		p.row("id", strconv.FormatUint(uint64(l.ID), 10), false)
		p.end()
	}

	if section("state") {
		p := beginPairs("##link-state")
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
	}

	in.properties(l.Props)
}

// defaults is the nothing-selected view of the raw default metadata: key, subject id, type, and value.
func (in *inspector) defaults(snap *pipewire.Snapshot) {
	rows := defaultsRows(snap)
	if len(rows) == 0 {
		return
	}
	if !section("default metadata") {
		return
	}
	if !imgui.BeginTableV("##defaults", 4, imgui.TableFlagsSizingStretchProp|imgui.TableFlagsRowBg, imgui.Vec2{}, 0) {
		return
	}
	imgui.TableSetupColumnV("key", imgui.TableColumnFlagsWidthStretch, 1, 0)
	imgui.TableSetupColumnV("subject id", imgui.TableColumnFlagsWidthStretch, 1, 0)
	imgui.TableSetupColumnV("type", imgui.TableColumnFlagsWidthStretch, 1, 0)
	imgui.TableSetupColumnV("value", imgui.TableColumnFlagsWidthStretch, 2, 0)
	imgui.TableHeadersRow()
	for _, r := range rows {
		imgui.TableNextRow()
		imgui.TableNextColumn()
		mono(r.key)
		imgui.TableNextColumn()
		wrapped(r.subject)
		imgui.TableNextColumn()
		mono(r.typ)
		imgui.TableNextColumn()
		mono(r.value)
	}
	imgui.EndTable()
}

// properties is the full property map in a section closed by default, with a filter box that narrows keys by
// substring.
func (in *inspector) properties(props map[string]string) {
	if len(props) == 0 || !sectionV("properties", false) {
		return
	}
	imgui.SetNextItemWidth(-1)
	imgui.InputTextWithHint("##filter", "filter keys", &in.filter, imgui.InputTextFlagsNone, nil)
	p := beginPairs("##properties")
	for _, k := range filteredKeys(props, in.filter) {
		p.row(k, props[k], true)
	}
	p.end()
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

type defaultsRow struct{ key, subject, typ, value string }

// defaultsRows displays reported subject ids without attributing entries to a current node.
func defaultsRows(snap *pipewire.Snapshot) []defaultsRow {
	var out []defaultsRow
	for _, e := range snap.Default {
		subject := strconv.FormatUint(uint64(e.Subject), 10)
		if e.Subject == 0 {
			subject += " (global)"
		}
		out = append(out, defaultsRow{key: e.Key, subject: subject, typ: e.Type, value: e.Value})
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
	return rows
}
