package pipewire

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michaelquigley/df/dl"
)

// requestTimeout bounds how long a request waits for observed confirmation.
const requestTimeout = 2 * time.Second

// resolvedKept is how many resolved requests a snapshot carries after the pending ones.
const resolvedKept = 16

// RequestID identifies a request posted to the backend; its outcome is carried in Snapshot.Requests.
type RequestID uint64

// RequestKind names what a request asked for.
type RequestKind int

const (
	RequestCreateLink RequestKind = iota
	RequestDestroyLink
	RequestSetForceQuantum
)

func (k RequestKind) String() string {
	switch k {
	case RequestDestroyLink:
		return "destroy link"
	case RequestSetForceQuantum:
		return "set force quantum"
	}
	return "create link"
}

// RequestState is where a request stands. a request is confirmed only by an observed outcome.
type RequestState int

const (
	RequestPending RequestState = iota
	RequestConfirmed
	RequestFailed
)

func (s RequestState) String() string {
	switch s {
	case RequestConfirmed:
		return "confirmed"
	case RequestFailed:
		return "failed"
	default:
		return "pending"
	}
}

// Request is one request and its feedback as carried in Snapshot.Requests; a timeout is not a link state.
type Request struct {
	ID       RequestID
	Kind     RequestKind
	State    RequestState
	Reason   string // why it failed
	OutPort  Serial // create: the requested endpoints
	InPort   Serial
	Link     Serial // destroy: the target; create: the link this request created, once observed
	Quantum  int    // set force quantum: the requested frames; 0 releases
	Posted   time.Time
	Resolved time.Time
}

// RequestPosted is a request handed to the loop thread. Session is the connection session of the snapshot the request
// was made against; serials name objects only within it.
type RequestPosted struct {
	Request Request
	Session uint64
}

// LinkBound reports the global id a create request's link proxy was bound to.
type LinkBound struct {
	Request RequestID
	ID      uint32
}

// LinkProxyError reports an error on a create request's link proxy.
type LinkProxyError struct {
	Request RequestID
	Error   string
}

// LinkProxyRemoved reports that the object behind a create request's link proxy was removed.
type LinkProxyRemoved struct {
	Request RequestID
}

// Tick asks the graph to expire requests that have waited too long.
type Tick struct{}

func (RequestPosted) input()    {}
func (LinkBound) input()        {}
func (LinkProxyError) input()   {}
func (LinkProxyRemoved) input() {}
func (Tick) input()             {}

// linkRequest is a pending request and the evidence gathered for it.
type linkRequest struct {
	Request
	// ids read from the serial-keyed objects at post time, used once to address the server.
	outNodeID, outPortID, inNodeID, inPortID uint32
	targetID                                 uint32
	settings                                 Serial // set force quantum: the settings metadata written
	proxy                                    bool   // a link proxy exists and must be released when the request resolves
	bound                                    bool
	boundID                                  uint32
}

func (g *Graph) postRequest(p RequestPosted) {
	r := &linkRequest{Request: p.Request}
	r.State = RequestPending
	r.Posted = g.now()
	if r.Kind != RequestSetForceQuantum && p.Session != g.connection {
		g.resolve(r, false, "the graph changed since the request was made")
		return
	}
	switch r.Kind {
	case RequestSetForceQuantum:
		settings := g.settingsObject()
		if settings == nil {
			g.resolve(r, false, "no settings metadata is observed")
			return
		}
		want := strconv.Itoa(r.Quantum)
		if e, ok := settings.entries[0][forceQuantumKey]; ok && strings.TrimSpace(e.Value) == want {
			// the metadata already says so: the echo is observed, not assumed.
			g.resolve(r, true, "")
			return
		}
		if !g.drv.setMetadata(settings.serial, 0, forceQuantumKey, "", want) {
			g.resolve(r, false, "the settings metadata could not be written")
			return
		}
		r.settings = settings.serial
	case RequestCreateLink:
		out, in := g.objects[r.OutPort], g.objects[r.InPort]
		switch {
		case out == nil || out.kind != KindPort:
			g.resolve(r, false, fmt.Sprintf("output port %d is not observed", r.OutPort))
			return
		case in == nil || in.kind != KindPort:
			g.resolve(r, false, fmt.Sprintf("input port %d is not observed", r.InPort))
			return
		case out.direction != DirectionOut || in.direction != DirectionIn:
			g.resolve(r, false, fmt.Sprintf("port %d is not an output or port %d is not an input", r.OutPort, r.InPort))
			return
		}
		r.outNodeID, r.outPortID, r.inNodeID, r.inPortID = out.nodeID, out.id, in.nodeID, in.id
		if !g.drv.createLink(r.outNodeID, r.outPortID, r.inNodeID, r.inPortID, r.ID) {
			g.resolve(r, false, "the link factory did not accept the request")
			return
		}
		r.proxy = true
	case RequestDestroyLink:
		l := g.objects[r.Link]
		if l == nil || l.kind != KindLink {
			g.resolve(r, false, fmt.Sprintf("link %d is not observed", r.Link))
			return
		}
		r.targetID = l.id
		g.drv.destroyGlobal(l.id)
	}
	g.session.pending[r.ID] = r
}

// failQueued records a request that never reached the graph as failed, without contacting the server.
func (g *Graph) failQueued(p RequestPosted, reason string) {
	r := &linkRequest{Request: p.Request}
	r.Posted = g.now()
	g.resolve(r, false, reason)
}

const forceQuantumKey = "clock.force-quantum"

// settingsObject is the bound settings metadata, if observed.
func (g *Graph) settingsObject() *object {
	for _, o := range g.objects {
		if o.kind == KindMetadata && o.metadataName == metadataSettings {
			return o
		}
	}
	return nil
}

// settingsEchoed confirms a pending force-quantum request when the settings metadata reports its value.
func (g *Graph) settingsEchoed(serial Serial, in MetadataProperty) {
	if in.Subject != 0 || in.Key != forceQuantumKey || in.Removed {
		return
	}
	for _, r := range g.pendingByID() {
		if r.Kind == RequestSetForceQuantum && r.settings == serial && strings.TrimSpace(in.Value) == strconv.Itoa(r.Quantum) {
			g.resolve(r, true, "")
		}
	}
}

// linkBound records the proxy's bound global id. PipeWire emits this before registry visibility (core.h's
// bound_id contract, present in 1.0). native callbacks apply inputs synchronously on the same loop thread, so the
// next link announcement under this id is the request's link; no earlier lifetimes need reconstruction.
func (g *Graph) linkBound(in LinkBound) {
	r := g.session.pending[in.Request]
	if r == nil || r.Kind != RequestCreateLink || r.bound {
		return
	}
	r.bound, r.boundID = true, in.ID
}

// linkAnnounced captures the first link announced under a pending create's bound id.
func (g *Graph) linkAnnounced(o *object) {
	for _, r := range g.pendingByID() {
		if r.Kind == RequestCreateLink && r.bound && r.boundID == o.id && r.Link == 0 {
			g.capture(r, o)
			return
		}
	}
}

// capture binds a request to one link lifetime: the link global with the proxy's bound id, announced after the
// bound notification. a link whose endpoints are not the requested serials is a wrong route: the request fails and
// that link, which the bound id proves this process created, is destroyed.
func (g *Graph) capture(r *linkRequest, o *object) {
	r.Link = o.serial
	if o.outPort != r.OutPort || o.inPort != r.InPort {
		g.resolve(r, false, fmt.Sprintf("wrong route: link %d connects ports %d -> %d, not %d -> %d",
			o.serial, o.outPort, o.inPort, r.OutPort, r.InPort))
		dl.Warnf("destroying wrong-route link %d (id %d) created for request %d", o.serial, o.id, r.ID)
		g.drv.destroyGlobal(o.id)
		return
	}
	g.linkState(o)
}

// linkState confirms or fails the create request captured on a link when its state settles.
func (g *Graph) linkState(o *object) {
	for _, r := range g.pendingByID() {
		if r.Kind != RequestCreateLink || r.Link != o.serial {
			continue
		}
		switch o.state {
		case "active", "paused":
			g.session.createdHere[o.serial] = struct{}{}
			g.resolve(r, true, "")
		case "error":
			g.resolve(r, false, "the link failed: "+o.err)
		}
		return
	}
}

// linkRemoved fails a create request whose captured link went away before confirmation, and confirms a destroy.
func (g *Graph) linkRemoved(serial Serial) {
	for _, r := range g.pendingByID() {
		switch {
		case r.Kind == RequestCreateLink && r.Link == serial:
			g.resolve(r, false, "the link was removed before it became active")
		case r.Kind == RequestDestroyLink && r.Link == serial:
			g.resolve(r, true, "")
		}
	}
}

func (g *Graph) linkProxyError(in LinkProxyError) {
	if r := g.session.pending[in.Request]; r != nil {
		g.resolve(r, false, "the link factory refused it: "+in.Error)
	}
}

func (g *Graph) linkProxyRemoved(in LinkProxyRemoved) {
	if r := g.session.pending[in.Request]; r != nil {
		g.resolve(r, false, "the link was removed before it became active")
	}
}

// expire ends the confirmation window for pending requests. observed links retain their state and lifetime.
func (g *Graph) expire() {
	now := g.now()
	for _, r := range g.pendingByID() {
		if now.Sub(r.Posted) < requestTimeout {
			continue
		}
		switch {
		case r.Kind == RequestCreateLink:
			g.resolve(r, false, fmt.Sprintf("confirmation not observed within %v", requestTimeout))
		case r.Kind == RequestDestroyLink:
			g.resolve(r, false, fmt.Sprintf("the link was not removed within %v", requestTimeout))
		case r.Kind == RequestSetForceQuantum:
			g.resolve(r, false, fmt.Sprintf("the settings metadata did not echo %d within %v", r.Quantum, requestTimeout))
		}
	}
}

// resolve settles a request: the proxy, if any, is released (a lingering link outlives it), the request moves to the
// resolved history, and the next snapshot carries its outcome.
func (g *Graph) resolve(r *linkRequest, ok bool, reason string) {
	if r.proxy {
		g.drv.releaseLink(r.ID)
		r.proxy = false
	}
	r.State = RequestFailed
	if ok {
		r.State = RequestConfirmed
	}
	r.Reason = reason
	r.Resolved = g.now()
	delete(g.session.pending, r.ID)
	g.resolved = append(g.resolved, r.Request)
	if len(g.resolved) > resolvedKept {
		g.resolved = g.resolved[len(g.resolved)-resolvedKept:]
	}
	g.dirty = true
}

func (g *Graph) pendingByID() []*linkRequest {
	out := make([]*linkRequest, 0, len(g.session.pending))
	for _, r := range g.session.pending {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// requests is the snapshot's request table: pending requests, then the recent resolved ones, oldest first.
func (g *Graph) requests() []Request {
	out := make([]Request, 0, len(g.session.pending)+len(g.resolved))
	for _, r := range g.pendingByID() {
		out = append(out, r.Request)
	}
	return append(out, g.resolved...)
}
