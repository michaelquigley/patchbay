// Package ui is Patchbay's dfx application: the canvas that declares the model's view each frame, the toolbar, the
// performance panel, and the inspector. it draws observed state only and turns canvas gestures into presentation
// changes on the model.
package ui

import (
	"fmt"
	"math"
	"sort"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/patchbay/internal/model"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

type idKind int

const (
	kindBlock idKind = iota
	kindPin
	kindLink
)

// ID is the canvas's one id space, spanning blocks, pins, and links. it is stable frame to frame: a block is named
// by its record key (or, while it has no record, by its live instance), a pin by its block and port key, a link by
// its serial.
type ID struct {
	Kind   idKind
	Block  string          // blocks and pins: the record key, or "~" plus the instance id for a block with no record
	Port   string          // pins: the port key
	Serial pipewire.Serial // links: the link serial; pins: the port serial when the port key cannot name it alone
}

func (id ID) String() string {
	switch id.Kind {
	case kindBlock:
		return "block:" + id.Block
	case kindPin:
		// a unit separator joins block and port, so no record key or port key can make two pins format alike.
		if id.Serial != 0 {
			return fmt.Sprintf("pin:%s\x1f%s\x1f%d", id.Block, id.Port, id.Serial)
		}
		return "pin:" + id.Block + "\x1f" + id.Port
	default:
		return fmt.Sprintf("link:%d", id.Serial)
	}
}

// blockID names a block on the canvas.
func blockID(b model.Block) ID {
	if b.Record != "" {
		return ID{Kind: kindBlock, Block: b.Record}
	}
	return ID{Kind: kindBlock, Block: "~" + string(b.ID)}
}

// selection is held by model identity, not canvas id, so it survives a block gaining a record.
type selection struct {
	blocks map[model.BlockID]bool
	links  map[pipewire.Serial]bool
}

func newSelection() selection {
	return selection{blocks: map[model.BlockID]bool{}, links: map[pipewire.Serial]bool{}}
}

type pinDecl struct {
	id     ID
	label  string
	suffix string // the hidden-count annotation, drawn dimmed after the label
	input  bool
}

type nodeDecl struct {
	id       ID
	block    model.BlockID
	pos      imgui.Vec2
	selected bool
	glyph    string
	title    string
	suffix   string // the hidden-count annotation, drawn dimmed after the title
	accent   imgui.Vec4
	pins     []pinDecl
}

type linkDecl struct {
	id       ID
	from, to ID
	selected bool
	color    imgui.Vec4
}

// frame is one frame's declarations, built from a view before anything is drawn.
type frame struct {
	nodes   []nodeDecl
	links   []linkDecl
	blockOf map[ID]model.BlockID
	linkOf  map[ID]pipewire.Serial
	portOf  map[ID]pipewire.Serial // pin id to port serial
	order   []model.BlockID        // the stacking order after this frame, back to front
}

// plan turns a view into declarations: visible blocks back to front in the stacking order (blocks not yet ordered go
// in front, in view order), their visible ports as pins, and the view's links. a stale view declares no links: what
// it shows is not current, and a link is a claim about the present.
func plan(v *model.View, sel selection, order []model.BlockID, hiddenClasses map[string]bool) frame {
	f := frame{blockOf: map[ID]model.BlockID{}, linkOf: map[ID]pipewire.Serial{}, portOf: map[ID]pipewire.Serial{}}

	visible := map[model.BlockID]model.Block{}
	for _, b := range v.Blocks {
		if b.Visible {
			visible[b.ID] = b
		}
	}
	seen := map[model.BlockID]bool{}
	for _, id := range order {
		if _, ok := visible[id]; ok && !seen[id] {
			f.order = append(f.order, id)
			seen[id] = true
		}
	}
	for _, b := range v.Blocks {
		if b.Visible && !seen[b.ID] {
			f.order = append(f.order, b.ID)
			seen[b.ID] = true
		}
	}

	pinOf := map[pipewire.Serial]ID{}
	mediaOf := map[pipewire.Serial]string{}
	for _, bid := range f.order {
		b := visible[bid]
		nid := blockID(b)
		accent := mediaHue(b.Key.Media)
		if v.Stale {
			accent = grey(accent)
		}
		title, suffix := blockTitle(b)
		n := nodeDecl{
			id:       nid,
			block:    b.ID,
			pos:      imgui.Vec2{X: b.X, Y: b.Y},
			selected: sel.blocks[b.ID],
			glyph:    ownerGlyph(b.Owner),
			title:    title,
			suffix:   suffix,
			accent:   accent,
		}
		keys := map[string]int{}
		for _, p := range b.Ports {
			keys[p.Key]++
		}
		for _, p := range b.Ports {
			if !p.Visible {
				continue
			}
			pid := ID{Kind: kindPin, Block: nid.Block, Port: p.Key}
			if p.Key == "" || keys[p.Key] > 1 {
				pid.Serial = p.Serial
			}
			pinOf[p.Serial] = pid
			f.portOf[pid] = p.Serial
			mediaOf[p.Serial] = b.Key.Media
			label, suffix := pinLabel(b, p, v.ShowHidden, hiddenClasses)
			n.pins = append(n.pins, pinDecl{
				id:     pid,
				label:  label,
				suffix: suffix,
				input:  b.Key.Direction == model.DirectionIn,
			})
		}
		f.blockOf[nid] = b.ID
		f.nodes = append(f.nodes, n)
	}

	if v.Stale {
		return f
	}
	for _, l := range v.Links {
		from, okFrom := pinOf[l.Out]
		to, okTo := pinOf[l.In]
		if !okFrom || !okTo {
			continue
		}
		lid := ID{Kind: kindLink, Serial: l.Serial}
		f.linkOf[lid] = l.Serial
		f.links = append(f.links, linkDecl{
			id:       lid,
			from:     from,
			to:       to,
			selected: sel.links[l.Serial],
			color:    linkHue(mediaOf[l.Out], l.State),
		})
	}
	return f
}

// blockTitle is a block's title bar: its name, the ordinal that tells same-key blocks apart, its media and
// direction; and, as a separate suffix, the count of connections on its own hidden ports.
func blockTitle(b model.Block) (title, suffix string) {
	title = b.Title
	if b.Ordinal > 0 {
		title += fmt.Sprintf(" #%d", b.Ordinal)
	}
	title += " · " + b.Key.Media + " " + b.Key.Direction
	if b.Hidden {
		title += " · hidden"
	}
	return title, hiddenSuffix(b.HiddenLinks)
}

// pinLabel is a pin row: the port's label and a mark when Show hidden is revealing something hidden; and, as a
// separate suffix, the count of its connections to ports that are not drawn.
func pinLabel(b model.Block, p model.Port, showHidden bool, hiddenClasses map[string]bool) (label, suffix string) {
	label = p.Label
	if showHidden && (p.Hidden || b.Hidden || hiddenClasses[p.Class]) {
		label += " (hidden)"
	}
	return label, hiddenSuffix(p.HiddenLinks)
}

func hiddenSuffix(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("· %d hidden", n)
}

// snapPlacements rounds every selected block's current position to the grid. it reads the view, not a frame's
// declarations: actions run before the frame is drawn, so the last frame's declarations can predate a move its own
// End just committed, and snapping from them would put the block back where the drag started.
func snapPlacements(v *model.View, sel selection, spacing float32) []model.Placement {
	var out []model.Placement
	for _, b := range v.Blocks {
		if !sel.blocks[b.ID] {
			continue
		}
		out = append(out, model.Placement{
			ID: b.ID,
			X:  float32(math.Round(float64(b.X/spacing))) * spacing,
			Y:  float32(math.Round(float64(b.Y/spacing))) * spacing,
		})
	}
	return out
}

// selectedIDs returns the canvas ids of the selected blocks the view draws.
func selectedIDs(v *model.View, sel selection) []ID {
	var out []ID
	for _, b := range v.Blocks {
		if b.Visible && sel.blocks[b.ID] {
			out = append(out, blockID(b))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}
