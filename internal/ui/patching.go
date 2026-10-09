package ui

import (
	"fmt"
	"sort"
	"time"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/patchbay/internal/model"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// failedShown is how long the inspector's request list keeps a failed request; the performance panel's events keep it
// for eventsKept.
const failedShown = 15 * time.Second

// patcher is the backend's request surface: the only calls in the ui that change the running system (a link, and
// the forced quantum), plus the metrics baseline reset, which changes only what is shown. sample mode has none.
type patcher interface {
	CreateLink(session uint64, outPort, inPort pipewire.Serial) pipewire.RequestID
	DestroyLink(session uint64, link pipewire.Serial) pipewire.RequestID
	SetForceQuantum(frames int) pipewire.RequestID
	ResetMetricsBaseline()
}

// validator checks a link against the live graph before anything is posted.
type validator interface {
	ValidateLink(out, in pipewire.Serial) error
}

// patching turns explicit gestures into requests: a link pulled between two pins, and Delete on selected links.
// nothing else in the ui posts a request. it remembers how each request was described when it was made, so the
// performance panel can name ports that have since gone.
type patching struct {
	p        patcher
	validate validator
	notice   func(string)
	descs    map[pipewire.RequestID]string
	now      func() time.Time
	// refused holds gestures refused before posting because the graph was not live: entries in the request list
	// that never reached the backend.
	refused []refusal
}

type refusal struct {
	desc, reason string
	at           time.Time
}

func newPatching(p patcher, v validator, notice func(string)) *patching {
	return &patching{p: p, validate: v, notice: notice, descs: map[pipewire.RequestID]string{}, now: time.Now}
}

// refuse lists a gesture that was not posted.
func (pt *patching) refuse(desc, reason string) {
	dl.Warnf("failed: '%s': '%s'", desc, reason)
	pt.refused = append(pt.refused, refusal{desc: desc, reason: reason, at: pt.now()})
	if len(pt.refused) > 16 {
		pt.refused = pt.refused[len(pt.refused)-16:]
	}
}

// link posts a create request for a gesture between two ports, if the graph is live and the model accepts it.
func (pt *patching) link(v *model.View, out, in pipewire.Serial) {
	if pt.p == nil {
		pt.notice("sample mode is read-only: patching is disabled")
		return
	}
	if v.Stale {
		pt.refuse("link "+portLabel(v, out)+" → "+portLabel(v, in), "not connected")
		return
	}
	if err := pt.validate.ValidateLink(out, in); err != nil {
		pt.notice("link refused: " + err.Error())
		return
	}
	id := pt.p.CreateLink(v.Session, out, in)
	pt.descs[id] = "link " + portLabel(v, out) + " → " + portLabel(v, in)
}

// unlink posts a destroy request for each link, as the view draws them.
func (pt *patching) unlink(v *model.View, links []pipewire.Serial) {
	if len(links) == 0 {
		return
	}
	if pt.p == nil {
		pt.notice("sample mode is read-only: patching is disabled")
		return
	}
	sort.Slice(links, func(i, j int) bool { return links[i] < links[j] })
	for _, s := range links {
		desc := fmt.Sprintf("unlink %d", s)
		for _, l := range v.Links {
			if l.Serial == s {
				desc = "unlink " + portLabel(v, l.Out) + " → " + portLabel(v, l.In)
			}
		}
		if v.Stale {
			pt.refuse(desc, "not connected")
			continue
		}
		id := pt.p.DestroyLink(v.Session, s)
		pt.descs[id] = desc
	}
}

// setQuantum posts a force-quantum request for the control's new value; automatic (0) releases the override. it acts
// once, on the operator's choice, and is never reapplied.
func (pt *patching) setQuantum(v *model.View, frames int) {
	if pt.p == nil {
		pt.notice("sample mode is read-only: the quantum control is disabled")
		return
	}
	desc := fmt.Sprintf("set quantum %d", frames)
	if frames == 0 {
		desc = "set quantum automatic (release the override)"
	}
	if v.Stale {
		pt.refuse(desc, "not connected")
		return
	}
	id := pt.p.SetForceQuantum(frames)
	pt.descs[id] = desc
}

// resetBaseline rebases the displayed new-error counts; totals and the system's counters are untouched.
func (pt *patching) resetBaseline() {
	if pt.p != nil {
		pt.p.ResetMetricsBaseline()
	}
}

// describe names a request as it was described when made, or by what the snapshot says of it.
func (pt *patching) describe(r pipewire.Request) string {
	if d, ok := pt.descs[r.ID]; ok {
		return d
	}
	switch r.Kind {
	case pipewire.RequestDestroyLink:
		return fmt.Sprintf("unlink %d", r.Link)
	case pipewire.RequestSetForceQuantum:
		return fmt.Sprintf("set quantum %d", r.Quantum)
	}
	return fmt.Sprintf("link %d → %d", r.OutPort, r.InPort)
}

// requestLines lists, from the snapshot, every pending request and every request that failed recently, with its
// reason, and the gestures refused before posting. confirmed requests are not listed: their outcome is the drawn
// graph.
func (pt *patching) requestLines(snap *pipewire.Snapshot, now time.Time) []string {
	var lines []string
	for _, r := range pt.refused {
		if now.Sub(r.at) <= failedShown {
			lines = append(lines, "failed: "+r.desc+": "+r.reason)
		}
	}
	if snap == nil {
		return lines
	}
	for _, r := range snap.Requests {
		switch r.State {
		case pipewire.RequestPending:
			lines = append(lines, fmt.Sprintf("pending: %s (%.1fs)", pt.describe(r), now.Sub(r.Posted).Seconds()))
		case pipewire.RequestFailed:
			if now.Sub(r.Resolved) <= failedShown {
				lines = append(lines, "failed: "+pt.describe(r)+": "+r.Reason)
			}
		}
	}
	return lines
}

// portLabel names a port by its block and its label, as the view draws it.
func portLabel(v *model.View, serial pipewire.Serial) string {
	for _, b := range v.Blocks {
		for _, p := range b.Ports {
			if p.Serial == serial {
				return b.Title + ":" + p.Label
			}
		}
	}
	return fmt.Sprintf("port %d", serial)
}
