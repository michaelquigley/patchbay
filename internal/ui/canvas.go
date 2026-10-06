package ui

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/dfx"
	"github.com/michaelquigley/patchbay/internal/model"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// presenter is what the canvas asks of the model: the presentation operations only. nothing here can create or
// destroy a link or write a setting.
type presenter interface {
	MoveBlocks(moves []model.Placement) error
	SetBlockHidden(id model.BlockID, hidden bool) error
	SetView(panX, panY, zoom float32)
	Refresh() *model.View
	SetViewport(r model.Rect)
	Workspace() model.ViewState
	HiddenClasses() map[string]bool
}

// canvas declares the model's view on a dfx NodeCanvas each frame and applies the canvas's intents to the model.
type canvas struct {
	nc     *dfx.NodeCanvas[ID]
	model  presenter
	notice func(string)
	// link receives a link gesture as port serials; the app validates and posts it. the canvas never patches.
	link func(out, in pipewire.Serial)

	sel     selection
	order   []model.BlockID
	session uint64 // the connection session of the last drawn view; selection and order belong to it

	started   bool
	savedView dfx.View
	normal    dfx.NodeCanvasStyle
	dim       dfx.NodeCanvasStyle
	dimmed    bool
}

func newCanvas(m presenter, notice func(string), link func(out, in pipewire.Serial)) *canvas {
	return &canvas{
		nc:     dfx.NewNodeCanvas[ID](dfx.NodeCanvasConfig{}),
		model:  m,
		notice: notice,
		link:   link,
		sel:    newSelection(),
	}
}

// start runs once, at the first draw, when the imgui context and theme exist: the styles are derived from the theme
// and the remembered pan and zoom are restored. there is no fit and no layout at startup.
func (c *canvas) start() {
	c.normal = dfx.DefaultNodeCanvasStyle()
	c.dim = desaturated(c.normal)
	c.nc.SetStyle(c.normal)
	ws := c.model.Workspace()
	c.nc.SetView(dfx.View{Pan: imgui.Vec2{X: ws.PanX, Y: ws.PanY}, Zoom: ws.Zoom})
	c.savedView = c.nc.View()
	c.started = true
}

// draw declares one frame. a stale view (the connection is not live) is drawn desaturated and locked, with no links.
func (c *canvas) draw(state *dfx.State, v *model.View) {
	if !c.started {
		c.start()
	}
	if v.Stale != c.dimmed {
		c.dimmed = v.Stale
		if c.dimmed {
			c.nc.SetStyle(c.dim)
		} else {
			c.nc.SetStyle(c.normal)
		}
		c.nc.SetLocked(c.dimmed)
	}

	c.track(v)
	f := plan(v, c.sel, c.order, c.model.HiddenClasses())
	c.order = f.order

	if c.dimmed {
		imgui.PushStyleColorVec4(imgui.ColText, dimText(imgui.CurrentStyle().Colors()[imgui.ColText]))
	}
	origin := imgui.CursorScreenPos()
	c.nc.Begin(state)
	for i := range f.nodes {
		n := &f.nodes[i]
		c.nc.Node(n.id, n.pos, dfx.NodeFlags{Selected: n.selected, Accent: n.accent}, func(ctx *dfx.NodeContext[ID]) {
			ctx.TitleBar(func() {
				ctx.Label(n.glyph + " " + n.title)
				if n.suffix != "" {
					imgui.SameLine()
					dimLabel(ctx, n.suffix)
				}
			})
			for _, p := range n.pins {
				if p.input {
					ctx.Input(p.id, p.label)
				} else {
					ctx.Output(p.id, p.label)
				}
				if p.suffix != "" {
					imgui.SameLine()
					dimLabel(ctx, p.suffix)
				}
			}
		})
	}
	for _, l := range f.links {
		c.nc.Link(l.id, l.from, l.to, dfx.LinkFlags{Selected: l.selected, Color: l.color})
	}
	intents := c.nc.End()
	if c.dimmed {
		imgui.PopStyleColor()
	}
	c.reportViewport(origin, state.Size)

	c.apply(intents, f)
	c.persistView()
}

// track clears the selection and stacking order when the view comes from a different connection session. both are
// held by BlockID, which carries a serial, and serials repeat across a daemon restart: a block in the new session must
// not inherit what was selected or raised in the old one. a stale view keeps the old session, so the old selection
// stays visible, inert, on the desaturated canvas until the new session's first view.
func (c *canvas) track(v *model.View) {
	if v.Session == c.session {
		return
	}
	c.session = v.Session
	c.sel = newSelection()
	c.order = nil
}

// apply turns the canvas's intents into model operations. a link pulled between pins is a request, handed to the app
// as port serials; what the canvas draws stays the observed graph.
func (c *canvas) apply(in dfx.Intents[ID], f frame) {
	if in.NodeRaised != nil {
		if b, ok := f.blockOf[*in.NodeRaised]; ok {
			c.raise(b)
		}
	}
	if sc := in.SelectionChanged; sc != nil {
		c.sel = newSelection()
		for _, id := range sc.Nodes {
			if b, ok := f.blockOf[id]; ok {
				c.sel.blocks[b] = true
			}
		}
		for _, id := range sc.Links {
			if s, ok := f.linkOf[id]; ok {
				c.sel.links[s] = true
			}
		}
	}
	if len(in.NodesMoved) > 0 {
		moves := make([]model.Placement, 0, len(in.NodesMoved))
		for _, mv := range in.NodesMoved {
			if b, ok := f.blockOf[mv.ID]; ok {
				moves = append(moves, model.Placement{ID: b, X: mv.To.X, Y: mv.To.Y})
			}
		}
		if err := c.model.MoveBlocks(moves); err != nil {
			dl.Warnf("move not applied: %v", err)
		}
	}
	if lc := in.LinkCreated; lc != nil {
		out, okOut := f.portOf[lc.FromPin]
		in, okIn := f.portOf[lc.ToPin]
		if !okOut || !okIn {
			dl.Warnf("link gesture '%v' -> '%v' names a pin not declared this frame", lc.FromPin, lc.ToPin)
			return
		}
		c.link(out, in)
	}
}

func (c *canvas) raise(b model.BlockID) {
	for i, id := range c.order {
		if id == b {
			copy(c.order[i:], c.order[i+1:])
			c.order[len(c.order)-1] = b
			return
		}
	}
	c.order = append(c.order, b)
}

// reportViewport tells the model which canvas rectangle this frame showed, so arrivals land inside it.
func (c *canvas) reportViewport(origin, size imgui.Vec2) {
	lo := c.nc.CanvasFromScreen(origin)
	hi := c.nc.CanvasFromScreen(imgui.Vec2{X: origin.X + size.X, Y: origin.Y + size.Y})
	c.model.SetViewport(model.Rect{MinX: lo.X, MinY: lo.Y, MaxX: hi.X, MaxY: hi.Y})
}

// persistView saves the pan and zoom when a navigation changed them.
func (c *canvas) persistView() {
	v := c.nc.View()
	if v == c.savedView {
		return
	}
	c.savedView = v
	c.model.SetView(v.Pan.X, v.Pan.Y, v.Zoom)
}

// hideSelection hides the selected blocks. ports are hidden from the inspector, which lands in stage 4.
func (c *canvas) hideSelection() {
	for b := range c.sel.blocks {
		if err := c.model.SetBlockHidden(b, true); err != nil {
			dl.Warnf("hide not applied: %v", err)
			continue
		}
		delete(c.sel.blocks, b)
	}
}

// snapSelection rounds every selected block's position to the canvas grid, as one gesture: one change, one save.
func (c *canvas) snapSelection() {
	c.snapTo(c.nc.GridSpacing())
}

// snapTo snaps the selection from the model's current positions, which include a move committed in the frame just
// drawn.
func (c *canvas) snapTo(spacing float32) {
	moves := snapPlacements(c.model.Refresh(), c.sel, spacing)
	if len(moves) == 0 {
		return
	}
	if err := c.model.MoveBlocks(moves); err != nil {
		dl.Warnf("snap not applied: %v", err)
	}
}

func (c *canvas) fit() {
	c.nc.ZoomToFit()
}

// center centers on the current selection, read from the model and the selection set rather than the last frame's
// declarations, which predate a selection change their own End just reported.
func (c *canvas) center() {
	if ids := selectedIDs(c.model.Refresh(), c.sel); len(ids) > 0 {
		c.nc.CenterOn(ids...)
	}
}

// centerOnBlocks centers the view on the given blocks, as the current view names them.
func (c *canvas) centerOnBlocks(blocks []model.BlockID) {
	want := selection{blocks: map[model.BlockID]bool{}}
	for _, b := range blocks {
		want.blocks[b] = true
	}
	if ids := selectedIDs(c.model.Refresh(), want); len(ids) > 0 {
		c.nc.CenterOn(ids...)
	}
}

func (c *canvas) destroy() {
	c.nc.Destroy()
}

func desaturated(s dfx.NodeCanvasStyle) dfx.NodeCanvasStyle {
	for _, c := range []*imgui.Vec4{
		&s.GridColor, &s.NodeBodyColor, &s.NodeBorderColor, &s.NodeBorderColorHovered, &s.NodeBorderColorSelected,
		&s.TitleBandColor, &s.TitleBandColorSelected, &s.PinColor, &s.PinColorHovered,
		&s.LinkColor, &s.LinkColorHovered, &s.LinkColorSelected,
	} {
		*c = grey(*c)
	}
	return s
}

// grey drops a color's saturation and some of its opacity: the look of a graph that is not current.
func grey(c imgui.Vec4) imgui.Vec4 {
	l := 0.299*c.X + 0.587*c.Y + 0.114*c.Z
	return imgui.Vec4{X: l, Y: l, Z: l, W: c.W * 0.6}
}

func dimText(c imgui.Vec4) imgui.Vec4 {
	g := grey(c)
	g.W = c.W * 0.5
	return g
}
