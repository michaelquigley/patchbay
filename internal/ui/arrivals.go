package ui

import (
	"strings"
	"time"

	"github.com/michaelquigley/patchbay/internal/model"
)

const (
	arrivalsShown = 10 * time.Second // how long the status strip announces an arrival
	arrivalsKept  = 20
)

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

// announcement is the status strip's arrivals line, or empty when nothing arrived recently.
func (a *arrivals) announcement(now time.Time) string {
	var titles []string
	for _, ar := range a.list {
		if now.Sub(ar.at) <= arrivalsShown {
			titles = append(titles, ar.title)
		}
	}
	if len(titles) == 0 {
		return ""
	}
	return "arrived: " + strings.Join(titles, ", ") + " · N to show"
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

func (a *arrivals) dismiss() {
	a.list = nil
}
