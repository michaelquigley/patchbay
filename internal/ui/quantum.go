package ui

import (
	"fmt"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/patchbay/internal/model"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// quantumScope is the sentence the control must carry: what it changes, and what its cycle time is not.
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

// quantumLines are the lines beneath the control: the requested override as the settings metadata shows it (so an
// override set by another tool shows too), the quantum and rate each driver with running followers is observed at,
// and the scope sentence. the request and the observation are separate facts; neither is inferred from the other.
func quantumLines(v *model.View, snap *pipewire.Snapshot, sample bool) []string {
	switch {
	case sample:
		return []string{"sample mode: nothing live, so no quantum control or monitoring"}
	case v.Stale || snap == nil || snap.State != pipewire.Live:
		return []string{"quantum and monitoring: not connected"}
	}
	var lines []string
	if !snap.Settings.ForceSeen {
		lines = append(lines, "requested override: unknown (the settings metadata does not show clock.force-quantum)")
	} else if q := snap.Settings.ForceQuantum; q > 0 {
		lines = append(lines, fmt.Sprintf("requested override: %d frames (clock.force-quantum in the settings metadata)", q))
	} else {
		lines = append(lines, "requested override: none (automatic releases it; clients such as REAPER may still force their own quantum)")
	}
	if !snap.Metrics.Available {
		return append(lines, monitoringUnavailable, quantumScope)
	}
	if len(snap.Metrics.Drivers) == 0 {
		lines = append(lines, "observed: no driver with running followers")
	}
	for _, d := range snap.Metrics.Drivers {
		lines = append(lines, fmt.Sprintf("observed: '%s' runs %d frames at %d Hz (%.2f ms cycle)", d.Name, d.Quantum, d.Rate, d.CycleMillis()))
	}
	return append(lines, quantumScope)
}

// monitoringUnavailable stands in for driver lines and counts when the profiler is not bound: nothing was observed,
// so nothing is shown as zero.
const monitoringUnavailable = "monitoring unavailable (no profiler)"

// metricsLine is the compact xrun line: counts over the nodes currently tracked, not a session history.
func metricsLine(m pipewire.MetricsSummary) string {
	if !m.Available {
		return monitoringUnavailable
	}
	last := "no increase observed"
	if !m.LastIncrease.IsZero() {
		last = "last increase " + m.LastIncrease.Format(time.TimeOnly)
	}
	return fmt.Sprintf("xruns over currently tracked nodes: %d total, %d new · %s", m.Total, m.New, last)
}

// drawQuantumRow draws the control, the compact metrics, and the baseline reset on one row. it posts nothing unless
// the operator changes the value or presses reset.
func (a *app) drawQuantumRow() {
	live := a.opts.Sample == "" && !a.view.Stale && a.snap != nil && a.snap.State == pipewire.Live
	if !live {
		return
	}
	current, known := quantumCurrent(a.snap.Settings)
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("quantum")
	imgui.SameLine()
	imgui.SetNextItemWidth(120)
	if imgui.BeginCombo("##quantum", quantumPreview(a.snap.Settings)) {
		for _, q := range quantumChoices(a.snap.Settings) {
			if imgui.SelectableBoolV(quantumLabel(q), known && q == current, imgui.SelectableFlagsNone, imgui.Vec2{}) {
				a.patching.chooseQuantum(a.view, a.snap.Settings, q)
			}
		}
		imgui.EndCombo()
	}
	imgui.SameLine()
	imgui.TextUnformatted(metricsLine(a.snap.Metrics))
	if !a.snap.Metrics.Available {
		return
	}
	imgui.SameLine()
	if imgui.SmallButton("reset new") {
		a.patching.resetBaseline()
	}
}

// chooseQuantum handles a choice from the control: it posts unless the choice is the override the settings metadata
// already shows. with no observed value, every choice posts, automatic included.
func (pt *patching) chooseQuantum(v *model.View, s pipewire.Settings, frames int) {
	if current, known := quantumCurrent(s); known && frames == current {
		return
	}
	pt.setQuantum(v, frames)
}
