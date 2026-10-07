package ui

import (
	"time"

	"github.com/michaelquigley/patchbay/internal/model"
)

const arrivalsKept = 20

type arrival struct {
	block model.BlockID
	title string
	at    time.Time
}

// arrivals is the session's list of blocks that appeared after the graph the connection started with, newest first,
// so a block never lands silently where the operator is not looking. it belongs to one connection session (block ids
// carry serials) and is never stored in the workspace.
type arrivals struct {
	list    []arrival
	session uint64
}

// record takes this frame's appearances from the view.
func (a *arrivals) record(v *model.View, now time.Time) {
	if v.Session != a.session {
		a.session = v.Session
		a.list = nil
	}
	for _, id := range v.Appeared {
		b, ok := v.Block(id)
		if !ok {
			continue
		}
		title, _ := blockTitle(b)
		a.list = append([]arrival{{block: id, title: title, at: now}}, a.list...)
	}
	if len(a.list) > arrivalsKept {
		a.list = a.list[:arrivalsKept]
	}
}

// batch is the blocks that appeared in one frame: one event.
type batch struct {
	titles []string
	at     time.Time
}

// batches groups the arrivals by the frame they appeared in, newest first.
func (a *arrivals) batches() []batch {
	var out []batch
	for _, ar := range a.list {
		if len(out) == 0 || !out[len(out)-1].at.Equal(ar.at) {
			out = append(out, batch{at: ar.at})
		}
		out[len(out)-1].titles = append(out[len(out)-1].titles, ar.title)
	}
	return out
}

// newest is the most recent batch of arrivals: the blocks that appeared in the same frame as the newest one.
func (a *arrivals) newest() []model.BlockID {
	if len(a.list) == 0 {
		return nil
	}
	var out []model.BlockID
	for _, ar := range a.list {
		if !ar.at.Equal(a.list[0].at) {
			break
		}
		out = append(out, ar.block)
	}
	return out
}
