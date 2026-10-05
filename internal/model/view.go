package model

import "github.com/michaelquigley/patchbay/internal/pipewire"

// View is what the canvas draws: blocks with positions and visibility, links between visible ports, and the
// remembered records with no live block. it is rebuilt from a snapshot and never edited by its reader.
//
// a view is a value for the frame it was built in. its State, Stale, Reason, and LiveGeneration describe that frame
// only; a reader must not cache a view across frames.
type View struct {
	State          pipewire.ConnState
	Stale          bool   // the view of the last live snapshot, shown while the connection is not live
	Reason         string // the backend's reason for the current state; empty when live or connecting at startup
	LiveGeneration uint64 // the snapshot generation the blocks were built from; zero when nothing live has been seen
	ShowHidden     bool
	Blocks         []Block // ordered by BlockID
	Links          []Link  // links whose ports are both visible, ordered by serial
	Absent         []AbsentRecord
}

// Block is one presented block.
type Block struct {
	ID          BlockID
	Key         Key
	Keyed       bool   // false when the block's identifying property is empty; it is never matched or remembered
	Record      string // the record key this instance is assigned to; empty when it has none
	Ordinal     int    // 1-based display ordinal among unassigned live blocks sharing a recognition key; 0 when unique
	Title       string
	X, Y        float32
	Hidden      bool // hidden by preference
	Visible     bool // drawn: at least one port is visible
	Ports       []Port
	HiddenLinks int // links carried by this block's own hidden ports, shown on the title while the block is visible
}

// Port is one presented port.
type Port struct {
	Serial      pipewire.Serial
	Key         string
	Label       string
	Class       string // category class: video, monitor, or empty
	Hidden      bool   // hidden by preference
	Visible     bool
	HiddenLinks int // links from this visible port to ports that are not visible
}

// Link is a link drawn between two visible ports.
type Link struct {
	Serial   pipewire.Serial
	Out, In  pipewire.Serial
	OutBlock BlockID
	InBlock  BlockID
	State    string
}

// AbsentRecord is a remembered record no live block is assigned to; it is what association offers.
type AbsentRecord struct {
	Record string
	Key    Key
}

// Block returns the block with id, if present.
func (v *View) Block(id BlockID) (Block, bool) {
	for _, b := range v.Blocks {
		if b.ID == id {
			return b, true
		}
	}
	return Block{}, false
}
