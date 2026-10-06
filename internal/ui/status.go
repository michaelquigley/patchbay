package ui

import (
	"fmt"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/patchbay/internal/model"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// statusLines is the status strip's text: the connection state, any unresolved references, and, while the view is
// stale, the reason and the generation the drawn graph came from, so nothing old reads as current. in sample mode a
// second line says the ui is read-only.
func statusLines(v *model.View, snap *pipewire.Snapshot, sampleDir, notice string) []string {
	line := v.State.String()
	if v.Reason != "" {
		line += " (" + v.Reason + ")"
	}
	if v.Stale {
		if v.LiveGeneration > 0 {
			line += fmt.Sprintf(" · showing the graph from generation %d; it is not current", v.LiveGeneration)
		} else {
			line += " · no graph observed yet"
		}
	} else if snap != nil && snap.Unresolved > 0 {
		if snap.Unresolved == 1 {
			line += " · 1 unresolved reference"
		} else {
			line += fmt.Sprintf(" · %d unresolved references", snap.Unresolved)
		}
	}
	if v.ShowHidden {
		line += " · showing hidden"
	}
	if notice != "" {
		line += " · " + notice
	}
	lines := []string{line}
	if sampleDir != "" {
		lines = append(lines, "sample '"+sampleDir+"' · read-only: patching and quantum controls are disabled")
	}
	return lines
}

func statusHeight(lines int) float32 {
	return float32(lines)*imgui.TextLineHeightWithSpacing() + imgui.CurrentStyle().ItemSpacing().Y
}

func drawStatus(lines []string) {
	imgui.Separator()
	for _, l := range lines {
		imgui.TextUnformatted(l)
	}
}
