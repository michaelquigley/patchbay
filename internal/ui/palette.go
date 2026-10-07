package ui

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/dfx"
	"github.com/michaelquigley/dfx/fonts"
	"github.com/michaelquigley/patchbay/internal/model"
)

// media hues, chosen to sit on dfx's dark theme: mid-dark enough that the theme's white title text reads on them as
// a title band, saturated enough that a link drawn in them reads against the grid. they are starting positions for
// the placement loop.
var (
	audioHue   = imgui.Vec4{X: 0.24, Y: 0.52, Z: 0.72, W: 1} // steel blue
	midiHue    = imgui.Vec4{X: 0.78, Y: 0.52, Z: 0.18, W: 1} // amber
	videoHue   = imgui.Vec4{X: 0.55, Y: 0.38, Z: 0.76, W: 1} // violet
	neutralHue = imgui.Vec4{X: 0.42, Y: 0.44, Z: 0.48, W: 1} // unknown media
)

// linkAlpha keeps links slightly under the blocks they join; settlingAlpha marks a link that exists but does not yet
// (or no longer) carry data.
const (
	linkAlpha     = 0.9
	settlingAlpha = 0.35
)

// warningHue marks a link PipeWire reports in error.
var warningHue = imgui.Vec4{X: 0.86, Y: 0.30, Z: 0.22, W: 1}

// mediaHue is a block's accent for its media.
func mediaHue(media string) imgui.Vec4 {
	switch media {
	case model.MediaAudio:
		return audioHue
	case model.MediaMIDI:
		return midiHue
	case model.MediaVideo:
		return videoHue
	}
	return neutralHue
}

// linkHue is a link's color for its media and its observed state: an observed link is always drawn, so its state
// shows in its color. active in the media hue; any state short of active (init, negotiating, allocating, paused)
// dimmed; error in the warning hue.
func linkHue(media, state string) imgui.Vec4 {
	switch state {
	case "active":
		c := mediaHue(media)
		c.W = linkAlpha
		return c
	case "error":
		return warningHue
	}
	c := mediaHue(media)
	c.W = settlingAlpha
	return c
}

// ownerGlyph leads a block's title with what kind of thing it presents.
func ownerGlyph(o model.Owner) string {
	switch o {
	case model.OwnerDevice:
		return fonts.ICON_SETTINGS_INPUT_COMPONENT
	case model.OwnerBridge:
		return fonts.ICON_PIANO
	default:
		return fonts.ICON_WEB_ASSET
	}
}

// hueLabel draws a label in a hue: the xrun badge, in the warning hue. it is a Label, so it is safe at every detent.
func hueLabel(n *dfx.NodeContext[ID], text string, hue imgui.Vec4) {
	imgui.PushStyleColorVec4(imgui.ColText, hue)
	n.Label(text)
	imgui.PopStyleColor()
}

// dimLabel draws a label in the theme's disabled text color: the hidden-count suffixes, which annotate a row rather
// than name it. it reads the style at draw time, never at construction.
func dimLabel(n *dfx.NodeContext[ID], text string) {
	imgui.PushStyleColorVec4(imgui.ColText, imgui.CurrentStyle().Colors()[imgui.ColTextDisabled])
	n.Label(text)
	imgui.PopStyleColor()
}
