package ui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

// logRequestFailures logs each observed failure once, regardless of toolbar clipping, dismissal, or event expiry.
// request ids are process-wide; drop bookkeeping when the backend stops retaining the request.
func (l *eventLog) logRequestFailures(pt *patching, snap *pipewire.Snapshot) {
	if snap == nil {
		return
	}
	for _, r := range snap.Requests {
		if r.State != pipewire.RequestFailed || l.loggedFailures[r.ID] {
			continue
		}
		dl.Warnf("failed: '%s': '%s' (request=%d kind='%s' observed_session=%d connection='%s' out_port=%d in_port=%d link=%d quantum=%d posted='%s' resolved='%s' observed_links=[%s])",
			pt.describe(r), r.Reason, r.ID, r.Kind, snap.Session, snap.State, r.OutPort, r.InPort, r.Link, r.Quantum,
			r.Posted.Format(time.RFC3339Nano), r.Resolved.Format(time.RFC3339Nano), requestLinkContext(r, snap))
		if l.loggedFailures == nil {
			l.loggedFailures = map[pipewire.RequestID]bool{}
		}
		l.loggedFailures[r.ID] = true
	}
	for id := range l.loggedFailures {
		if !slices.ContainsFunc(snap.Requests, func(r pipewire.Request) bool { return r.ID == id }) {
			delete(l.loggedFailures, id)
		}
	}
}

// requestLinkContext describes what the same snapshot reports for the target or requested route. a failed request
// does not imply that no link exists; the connection state in the log says whether this observation is current.
func requestLinkContext(r pipewire.Request, snap *pipewire.Snapshot) string {
	var links []string
	for serial, link := range snap.Links {
		if (r.Link != 0 && serial == r.Link) ||
			(r.Kind == pipewire.RequestCreateLink && link.OutPort == r.OutPort && link.InPort == r.InPort) {
			links = append(links, fmt.Sprintf("serial=%d state='%s' error='%s'", serial, link.State, link.Error))
		}
	}
	sort.Strings(links)
	return strings.Join(links, "; ")
}
