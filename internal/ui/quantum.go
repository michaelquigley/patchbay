package ui

import (
	"fmt"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/patchbay/internal/model"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// quantumScope is the sentence the control must carry: what it changes, and what its cycle time is not. the caption
// says the first half under the control; the whole sentence is the caption's tooltip.
const quantumScope = "quantum is the PipeWire graph's processing size for every client on a driver, not a private REAPER buffer; cycle time is not input-to-output latency"

// quantumChoices are the control's values: automatic (0, which releases the override), then the powers of two from
// clock.min-quantum to clock.max-quantum as the settings metadata reports them.
func quantumChoices(s pipewire.Settings) []int {
	choices := []int{0}
	if s.MinQuantum <= 0 || s.MaxQuantum < s.MinQuantum {
		return choices
	}
	for q := 1; q <= s.MaxQuantum; q *= 2 {
		if q >= s.MinQuantum {
			choices = append(choices, q)
		}
	}
	return choices
}

func quantumLabel(q int) string {
	if q == 0 {
		return "automatic"
	}
	return fmt.Sprintf("%d", q)
}

// quantumCurrent is the control's current value: the override as the settings metadata shows it. when the metadata
// does not show clock.force-quantum, there is no current value, and the absence is not read as automatic.
func quantumCurrent(s pipewire.Settings) (int, bool) {
	return s.ForceQuantum, s.ForceSeen
}

func quantumPreview(s pipewire.Settings) string {
	if q, ok := quantumCurrent(s); ok {
		return quantumLabel(q)
	}
	return "unknown"
}

// quantumCaption sits under the control in the dim color; the full scope sentence is its tooltip.
const quantumCaption = "sets the PipeWire graph quantum, not REAPER's own buffer"

// quantumRows are the quantum section's rows beneath the control: the requested override as the settings metadata shows
// it (so an override set by another tool shows too), and the quantum and rate each driver with running followers is
// observed at. the request and the observation are separate facts; neither is inferred from the other. control is
// whether the control is drawn: only while live.
func quantumRows(v *model.View, snap *pipewire.Snapshot, sampleDir string) (rows []statusRow, control bool) {
	switch {
	case sampleDir != "":
		return []statusRow{{value: "nothing live in sample mode"}}, false
	case !statusLive(v, snap, sampleDir):
		return []statusRow{{value: "not connected"}}, false
	}
	switch q := snap.Settings.ForceQuantum; {
	case !snap.Settings.ForceSeen:
		rows = append(rows, statusRow{label: "requested", value: "unknown (the settings metadata does not show clock.force-quantum)"})
	case q > 0:
		rows = append(rows, statusRow{label: "requested", value: fmt.Sprintf("%d frames", q), mono: true})
	default:
		rows = append(rows, statusRow{label: "requested", value: "none (clients such as REAPER may still force their own)"})
	}
	if !snap.Metrics.Available {
		return append(rows, statusRow{label: "observed", value: monitoringUnavailable}), true
	}
	if len(snap.Metrics.Drivers) == 0 {
		return append(rows, statusRow{label: "observed", value: "no driver with running followers"}), true
	}
	for i, d := range snap.Metrics.Drivers {
		label := ""
		if i == 0 {
			label = "observed"
		}
		rows = append(rows, statusRow{label: label, mono: true,
			value: fmt.Sprintf("'%s' %d @ %d Hz, %.2f ms", d.Name, d.Quantum, d.Rate, d.CycleMillis())})
	}
	return rows, true
}

// monitoringUnavailable stands in for driver lines and counts when the profiler is not bound: nothing was observed,
// so nothing is shown as zero.
const monitoringUnavailable = "monitoring unavailable (no profiler)"

// drawQuantumSection draws the control with its caption, then the requested and observed rows. it posts nothing
// unless the operator changes the value.
func (a *app) drawQuantumSection(v *model.View, snap *pipewire.Snapshot, labels float32) {
	rows, control := quantumRows(v, snap, a.opts.Sample)
	if control {
		current, known := quantumCurrent(snap.Settings)
		imgui.SetNextItemWidth(120)
		if imgui.BeginCombo("##quantum", quantumPreview(snap.Settings)) {
			for _, q := range quantumChoices(snap.Settings) {
				if imgui.SelectableBoolV(quantumLabel(q), known && q == current, imgui.SelectableFlagsNone, imgui.Vec2{}) {
					a.patching.chooseQuantum(v, snap.Settings, q)
				}
			}
			imgui.EndCombo()
		}
		imgui.TextDisabled(quantumCaption)
		imgui.SetItemTooltip(quantumScope)
	}
	drawStatusRows("##quantum-rows", labels, rows)
}

// chooseQuantum handles a choice from the control: it posts unless the choice is the override the settings metadata
// already shows. with no observed value, every choice posts, automatic included.
func (pt *patching) chooseQuantum(v *model.View, s pipewire.Settings, frames int) {
	if current, known := quantumCurrent(s); known && frames == current {
		return
	}
	pt.setQuantum(v, frames)
}
