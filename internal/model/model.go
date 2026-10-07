package model

import (
	"strconv"
	"time"

	"github.com/michaelquigley/patchbay/internal/pipewire"
	"github.com/michaelquigley/patchbay/internal/workspace"
	"github.com/pkg/errors"
)

const saveDebounce = 500 * time.Millisecond

// Model reconciles snapshots against the one remembered workspace. it is not safe for concurrent use; the ui drives it
// from its frame loop. nothing in it issues a request to the backend: every operation changes presentation only.
type Model struct {
	ws    *workspace.Workspace
	store *workspace.Store // nil when changes are not persisted

	// assignment binds one live block instance to one record for the instance's lifetime.
	assigned map[BlockID]string
	owner    map[string]BlockID
	// session holds placement and preferences for live blocks that have no record (ambiguous, or unkeyed); a gesture
	// on a keyed one moves it into a record.
	session map[BlockID]*workspace.Record
	column  arrivalColumn
	laidOut bool

	// arrivals: the visible canvas rectangle they are placed in, and the blocks already seen in this connection, so
	// only later appearances are announced.
	viewport    Rect
	hasViewport bool
	seen        map[BlockID]bool
	seenInitial bool

	showHidden bool

	// state and reason are those of the last snapshot handed to Reconcile; last is only ever a view built from a
	// live snapshot and is never returned while state is not live.
	state      pipewire.ConnState
	reason     string
	connection uint64 // the connection session the assignments were made under
	snap       *pipewire.Snapshot
	live       map[BlockID]*derived
	last       *View
	wasLive    bool
}

// New builds a model over ws. store may be nil, in which case changes are kept in memory only.
func New(ws *workspace.Workspace, store *workspace.Store) *Model {
	return &Model{
		ws:       ws,
		store:    store,
		assigned: map[BlockID]string{},
		owner:    map[string]BlockID{},
		session:  map[BlockID]*workspace.Record{},
		seen:     map[BlockID]bool{},
		state:    pipewire.Connecting,
	}
}

// Open loads the workspace at path (empty for the default location) and builds a model that saves it.
func Open(path string) (*Model, error) {
	if path == "" {
		p, err := workspace.DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	ws, err := workspace.Load(path)
	if err != nil {
		return nil, err
	}
	return New(ws, workspace.NewStore(path, saveDebounce)), nil
}

// Close writes any pending workspace change.
func (m *Model) Close() {
	if m.store != nil {
		m.store.Close()
	}
}

// category class names, as the toolbar's filters name them.
const (
	ClassVideo   = workspace.ClassVideo
	ClassMonitor = workspace.ClassMonitor
)

// ViewState is the remembered canvas pan and zoom.
type ViewState = workspace.View

// Workspace returns the view state the canvas restores at startup.
func (m *Model) Workspace() ViewState {
	return m.ws.View
}

// DefaultWorkspacePath is where the one remembered workspace lives.
func DefaultWorkspacePath() (string, error) {
	return workspace.DefaultPath()
}

// HiddenClasses reports the category filter state.
func (m *Model) HiddenClasses() map[string]bool {
	out := make(map[string]bool, len(m.ws.HiddenClasses))
	for k, v := range m.ws.HiddenClasses {
		out[k] = v
	}
	return out
}

func (m *Model) changed() {
	if m.store != nil {
		m.store.Changed(m.ws)
	}
}

func (m *Model) liveBlock(id BlockID) (*derived, error) {
	d, ok := m.live[id]
	if !ok {
		return nil, errors.Errorf("block '%v' is not live", id)
	}
	return d, nil
}

// gestureRecord returns the record a gesture on a block edits. an assigned block edits its record; a keyed block with
// no record gets one in a fresh slot, assigned to it by this operator action; an unkeyed block edits session state.
func (m *Model) gestureRecord(id BlockID) (*workspace.Record, bool, error) {
	d, err := m.liveBlock(id)
	if err != nil {
		return nil, false, err
	}
	if rk, ok := m.assigned[id]; ok {
		return m.ws.Records[rk], true, nil
	}
	sess := m.session[id]
	if sess == nil {
		sess = &workspace.Record{Key: d.key}
		m.session[id] = sess
	}
	if !d.keyed {
		return sess, false, nil
	}
	rk := m.allocate(d.key)
	rec := *sess
	rec.Key = d.key
	m.ws.Records[rk] = &rec
	m.assign(id, rk)
	return &rec, true, nil
}

// ValidateLink checks a requested link against the current live graph: both ports live, an output to an input, the
// same media, and not already linked. it posts nothing; the caller posts a request only if it passes.
func (m *Model) ValidateLink(out, in pipewire.Serial) error {
	if m.snap == nil || m.state != pipewire.Live {
		return errors.New("the graph is not live")
	}
	o, ok := m.snap.Ports[out]
	if !ok {
		return errors.Errorf("port %d is not live", out)
	}
	i, ok := m.snap.Ports[in]
	if !ok {
		return errors.Errorf("port %d is not live", in)
	}
	if o.Direction != pipewire.DirectionOut || i.Direction != pipewire.DirectionIn {
		return errors.New("a link runs from an output to an input")
	}
	if o.Media != i.Media {
		return errors.Errorf("a %v port cannot be linked to a %v port", o.Media, i.Media)
	}
	for _, l := range m.snap.Links {
		if l.OutPort == out && l.InPort == in {
			return errors.New("those ports are already linked")
		}
	}
	return nil
}

// Placement is one block's new position.
type Placement struct {
	ID   BlockID
	X, Y float32
}

// Move places a block.
func (m *Model) Move(id BlockID, x, y float32) error {
	return m.MoveBlocks([]Placement{{ID: id, X: x, Y: y}})
}

// MoveBlocks places several blocks as one gesture: one change, one save. a block that is no longer live is skipped
// and reported; the others still move.
func (m *Model) MoveBlocks(moves []Placement) error {
	var failed error
	persisted := false
	for _, mv := range moves {
		rec, p, err := m.gestureRecord(mv.ID)
		if err != nil {
			failed = err
			continue
		}
		rec.X, rec.Y = mv.X, mv.Y
		persisted = persisted || p
	}
	if persisted {
		m.changed()
	}
	return failed
}

// SetBlockHidden hides or unhides a block. hiding is presentation only; its ports keep their links.
func (m *Model) SetBlockHidden(id BlockID, hidden bool) error {
	rec, persisted, err := m.gestureRecord(id)
	if err != nil {
		return err
	}
	rec.Hidden = hidden
	if persisted {
		m.changed()
	}
	return nil
}

// SetPortHidden hides or unhides one port of a block by its port key.
func (m *Model) SetPortHidden(id BlockID, portKey string, hidden bool) error {
	d, err := m.liveBlock(id)
	if err != nil {
		return err
	}
	if portKey == "" {
		return errors.Errorf("block '%v' has a port with no key; it cannot be hidden individually", id)
	}
	found := false
	for _, p := range d.ports {
		if p.key == portKey {
			found = true
			break
		}
	}
	if !found {
		return errors.Errorf("block '%v' has no port '%v'", id, portKey)
	}
	rec, persisted, err := m.gestureRecord(id)
	if err != nil {
		return err
	}
	rec.SetPortHidden(portKey, hidden)
	if persisted {
		m.changed()
	}
	return nil
}

// SetClassHidden sets a category filter.
func (m *Model) SetClassHidden(class string, hidden bool) {
	m.ws.HiddenClasses[class] = hidden
	m.changed()
}

// ShowHidden reports whether Show hidden is on.
func (m *Model) ShowHidden() bool {
	return m.showHidden
}

// SetShowHidden reveals every hidden object for as long as it is on. it changes nothing in the workspace.
func (m *Model) SetShowHidden(on bool) {
	m.showHidden = on
}

// SetView records the canvas pan and zoom.
func (m *Model) SetView(panX, panY, zoom float32) {
	m.ws.View = workspace.View{PanX: panX, PanY: panY, Zoom: zoom}
	m.changed()
}

// Associate binds a live block to a remembered record no live block holds, at the operator's choice. the record must
// have the block's media and direction. the block's recognition key is remembered on the record so it answers to it
// on later returns. association changes presentation only.
func (m *Model) Associate(id BlockID, record string) error {
	d, err := m.liveBlock(id)
	if err != nil {
		return err
	}
	if !d.keyed {
		return errors.Errorf("block '%v' has no recognition key to remember", id)
	}
	rec, ok := m.ws.Records[record]
	if !ok {
		return errors.Errorf("no record '%v'", record)
	}
	if holder, held := m.owner[record]; held && holder != id {
		return errors.Errorf("record '%v' is held by live block '%v'", record, holder)
	}
	if rec.Key.Media != d.key.Media || rec.Key.Direction != d.key.Direction {
		return errors.Errorf("record '%v' is %v %v; block '%v' is %v %v",
			record, rec.Key.Media, rec.Key.Direction, id, d.key.Media, d.key.Direction)
	}
	if prior, ok := m.assigned[id]; ok {
		delete(m.owner, prior)
		delete(m.assigned, id)
	}
	if !rec.Matches(d.key) {
		rec.Also = append(rec.Also, d.key)
	}
	m.assign(id, record)
	m.changed()
	return nil
}

// assign binds a live block to a record. a record that does not yet know its device's hardware serial learns it from
// the block; that is a hint for the chooser only.
func (m *Model) assign(id BlockID, record string) {
	m.assigned[id] = record
	m.owner[record] = id
	delete(m.session, id)
	if rec, d := m.ws.Records[record], m.live[id]; rec != nil && d != nil && rec.Device == "" && d.hardware != "" {
		rec.Device = d.hardware
		m.changed()
	}
}

// allocate returns the record key for a new record: the recognition key itself, or the next free ordinal slot. the
// ordinal is a storage slot only, never evidence about which instance a record belongs to.
func (m *Model) allocate(k Key) string {
	base := k.String()
	if _, taken := m.ws.Records[base]; !taken {
		return base
	}
	for n := 2; ; n++ {
		rk := base + "#" + strconv.Itoa(n)
		if _, taken := m.ws.Records[rk]; !taken {
			return rk
		}
	}
}
