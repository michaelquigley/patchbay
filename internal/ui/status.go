package ui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/patchbay/internal/model"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// the performance panel is a collapsible panel on the window's left, like the inspector on its right: the connection,
// the quantum, the xruns, and the events, each a section of label/value rows. what must never be missed (that the
// canvas is not current, and the newest event) also sits at the toolbar's right end, so collapsing the panel hides
// nothing that needs attention.

// performanceWidth is the performance panel's opening width, and the width the P toggle restores it to when it has
// become narrower than its collapsed width.
const performanceWidth = 320

// statusLabels are every label a status row uses: the panel's label column is as wide as the widest of them.
var statusLabels = []string{"state", "session", "graph", "unresolved", "view", "sample", "access", "requested", "observed",
	"total", "new", "last increase"}

// statusRow is one label/value row of the performance panel. an empty label continues the row above it; mono values are
// numbers and names.
type statusRow struct {
	label, value string
	mono         bool
}

// statusLive is whether the frame shows a live connection: only then is anything about the quantum or the xruns
// current.
func statusLive(v *model.View, snap *pipewire.Snapshot, sampleDir string) bool {
	return sampleDir == "" && !v.Stale && snap != nil && snap.State == pipewire.Live
}

// connectionRows are the connection section: the state (with the backend's reason), the session, while stale the
// generation the drawn graph came from so nothing old reads as current, unresolved references when there are any, and
// whether hidden blocks are shown. in sample mode, the capture and that it is read-only.
func connectionRows(v *model.View, snap *pipewire.Snapshot, sampleDir string) []statusRow {
	var rows []statusRow
	if sampleDir != "" {
		rows = append(rows,
			statusRow{label: "state", value: "sample mode"},
			statusRow{label: "sample", value: sampleDir, mono: true},
			statusRow{label: "access", value: "read-only: patching and quantum controls are disabled"})
	} else {
		state := v.State.String()
		if v.Reason != "" {
			state += " (" + v.Reason + ")"
		}
		rows = append(rows, statusRow{label: "state", value: state})
		if v.Session > 0 {
			rows = append(rows, statusRow{label: "session", value: strconv.FormatUint(v.Session, 10), mono: true})
		}
	}
	switch {
	case v.Stale && v.LiveGeneration > 0:
		rows = append(rows, statusRow{label: "graph", value: fmt.Sprintf("from generation %d; it is not current", v.LiveGeneration)})
	case v.Stale:
		rows = append(rows, statusRow{label: "graph", value: "none observed yet"})
	case snap != nil && snap.Unresolved > 0:
		rows = append(rows, statusRow{label: "unresolved", value: strconv.Itoa(snap.Unresolved), mono: true})
	}
	if v.ShowHidden {
		rows = append(rows, statusRow{label: "view", value: "showing hidden"})
	}
	return rows
}

// xrunStatusRows are the xruns section: totals and new errors over the currently tracked nodes and the last increase,
// or why there are none. counts are shown only while live and while the profiler is bound.
func xrunStatusRows(v *model.View, snap *pipewire.Snapshot, sampleDir string) []statusRow {
	switch {
	case sampleDir != "":
		return []statusRow{{value: "nothing live in sample mode"}}
	case !statusLive(v, snap, sampleDir):
		return []statusRow{{value: "not connected"}}
	case !snap.Metrics.Available:
		return []statusRow{{value: monitoringUnavailable}}
	}
	m := snap.Metrics
	last := statusRow{label: "last increase", value: "none observed"}
	if !m.LastIncrease.IsZero() {
		last = statusRow{label: "last increase", value: m.LastIncrease.Format(time.TimeOnly), mono: true}
	}
	return []statusRow{
		{label: "total", value: strconv.FormatUint(m.Total, 10), mono: true},
		{label: "new", value: strconv.FormatUint(m.New, 10), mono: true},
		last,
	}
}

// toolbarStatus is what stays visible at the toolbar's right end with the performance panel collapsed: the connection
// state, flagged when what the canvas shows is not current, and the newest event with how many more there are.
type toolbarStatus struct {
	state string
	warn  bool   // the canvas is not current
	event *event // the newest event; nil when there is none
	more  int
}

func newToolbarStatus(v *model.View, sampleDir string, events []event) toolbarStatus {
	var ts toolbarStatus
	switch {
	case sampleDir != "":
		ts.state = "sample mode (read-only)"
	case v.Stale && v.LiveGeneration > 0:
		ts.state, ts.warn = v.State.String()+" · not current", true
	case v.Stale:
		ts.state, ts.warn = v.State.String()+" · no graph yet", true
	default:
		ts.state = v.State.String()
	}
	if len(events) > 0 {
		ts.event, ts.more = &events[0], len(events)-1
	}
	return ts
}

// toolbarEventChars bounds the newest event's text on the toolbar; the performance panel has it whole.
const toolbarEventChars = 64

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// drawToolbarStatus draws the toolbar summary right-aligned: the newest event with its age, its dismiss button, the
// count of the rest, then the connection state. it measures its own width first, so it ends at the toolbar's edge.
func (a *app) drawToolbarStatus(ts toolbarStatus, now time.Time) {
	style := imgui.CurrentStyle()
	button := func(label string) float32 { return imgui.CalcTextSize(label).X + 2*style.FramePadding().X }
	gap := style.ItemSpacing().X
	var text, age, more string
	width := imgui.CalcTextSize(ts.state).X
	if ts.event != nil {
		text, age = clip(ts.event.text, toolbarEventChars), eventAge(now.Sub(ts.event.at))
		width += imgui.CalcTextSize(text).X + gap + imgui.CalcTextSize(age).X + gap + button("dismiss") + 2*gap
		if ts.more > 0 {
			more = fmt.Sprintf("+%d", ts.more)
			width += button(more) + gap
		}
	}
	x := imgui.CursorPosX() + imgui.ContentRegionAvail().X - width - gap
	if x > imgui.CursorPosX() {
		imgui.SetCursorPosX(x)
	}
	if ts.event != nil {
		imgui.TextUnformatted(text)
		imgui.SameLine()
		imgui.TextDisabled(age)
		imgui.SameLine()
		if imgui.SmallButton("dismiss##toolbar") {
			a.events.dismiss(ts.event.key)
		}
		if more != "" {
			imgui.SameLine()
			if imgui.SmallButton(more) {
				openPanel(a.performance, performanceWidth)
			}
			imgui.SetItemTooltip("more events in the performance panel (P)")
		}
		imgui.SameLine()
		imgui.TextDisabled("·")
		imgui.SameLine()
	}
	if ts.warn {
		imgui.TextColored(warningHue, ts.state)
	} else {
		imgui.TextUnformatted(ts.state)
	}
}

func statusLabelWidth() float32 {
	var w float32
	for _, l := range statusLabels {
		w = max(w, imgui.CalcTextSize(l).X)
	}
	return w + imgui.CurrentStyle().ItemSpacing().X
}

// drawStatus draws the performance panel's body: the connection, the quantum, the xruns, and the events.
func (a *app) drawPerformance(v *model.View, snap *pipewire.Snapshot, events []event, now time.Time) {
	imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, imgui.Vec2{X: bodyPadding, Y: bodyPadding})
	imgui.BeginChildStrV("##performance-body", imgui.Vec2{}, imgui.ChildFlagsAlwaysUseWindowPadding, imgui.WindowFlagsNone)
	imgui.PopStyleVar()
	labels := statusLabelWidth()

	imgui.SeparatorText("connection")
	drawStatusRows("##connection-rows", labels, connectionRows(v, snap, a.opts.Sample))

	section("quantum")
	a.drawQuantumSection(v, snap, labels)

	section("xruns (tracked nodes)")
	drawStatusRows("##xruns-rows", labels, xrunStatusRows(v, snap, a.opts.Sample))
	if statusLive(v, snap, a.opts.Sample) && snap.Metrics.Available && imgui.SmallButton("reset new") {
		a.patching.resetBaseline()
	}

	section("events")
	a.events.draw(events, now)
	imgui.EndChild()
}

// drawStatusRows draws rows as a label/value table: labels dim in the shared column, values in the normal color,
// numbers and names in monospace.
func drawStatusRows(id string, labels float32, rows []statusRow) {
	if len(rows) == 0 {
		return
	}
	p := beginPairsV(id, labels)
	for _, r := range rows {
		p.row(r.label, r.value, r.mono)
	}
	p.end()
}
