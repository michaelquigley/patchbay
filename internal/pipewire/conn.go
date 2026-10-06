package pipewire

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/michaelquigley/df/dl"
	"github.com/pkg/errors"
)

// Conn is the backend: one pipewire connection, observed into snapshots and events. the ui reads Snapshot once per
// frame and drains Events; it never touches a native handle.
type Conn interface {
	// Snapshot returns the current immutable snapshot; its pointer identity changes only when content does.
	Snapshot() *Snapshot
	// Events returns the buffered event channel the ui drains each frame. it is lossy; outcomes are in Snapshot.
	Events() <-chan Event
	// CreateLink asks for a lingering link between two ports, named by serial within the connection session of the
	// snapshot the caller acted on. its outcome is observed, never assumed: see Snapshot.Requests.
	CreateLink(session uint64, outPort, inPort Serial) RequestID
	// DestroyLink asks for a link, named by serial within session, to be destroyed; it is confirmed by its removal.
	DestroyLink(session uint64, link Serial) RequestID
	// Close tears the connection down and stops reconnecting.
	Close()
}

const (
	eventBuffer = 4096
	minBackoff  = time.Second
	maxBackoff  = 10 * time.Second
	tickEvery   = 250 * time.Millisecond // how often the loop is woken to expire requests while nothing else happens
)

// transport reaches a pipewire daemon. open must attach the registry listener and call sess.begin with the driver
// before any input can be delivered, and must deliver every input, flush, and loss on one thread at a time.
type transport interface {
	open(sess *session) (transportSession, error)
}

// transportSession is one open connection.
type transportSession interface {
	close()
	// wake asks the delivery thread to flush; it is safe from any goroutine.
	wake()
	// end ends the session as a loss with reason, on the delivery thread's terms (the native transport takes the
	// loop lock), so queued and pending requests fail and disconnected is published as for any other loss.
	end(reason string)
}

// Connect starts the backend against the user's pipewire daemon. it never fails: a daemon that cannot be reached is
// reported as disconnected and retried with backoff.
func Connect() Conn {
	return startBackend(nativeTransport{}, minBackoff, maxBackoff)
}

type backend struct {
	tr         transport
	minBackoff time.Duration
	maxBackoff time.Duration

	snap   atomic.Pointer[Snapshot]
	events chan Event

	// mu is the backend's one lock. it guards publication, the posting queue, the current transport, and request
	// ids, so that losing a connection (detach, then publish disconnected) is one critical section a post cannot
	// interleave with. it is a leaf: while it is held nothing else is acquired; the work under it is slice
	// operations, the atomic snapshot store, non-blocking event sends, graph folds, and a transport's wake, which
	// signals an eventfd.
	mu         sync.Mutex
	generation uint64
	posted     []queued         // requests (and test invocations) waiting for the delivery thread
	current    transportSession // the open transport to wake; nil while no connection is open
	nextID     RequestID

	closing  chan struct{}
	done     chan struct{}
	once     sync.Once
	sessions uint64 // the number of connections opened; only the supervisor touches it
}

func startBackend(tr transport, minB, maxB time.Duration) *backend {
	b := &backend{
		tr:         tr,
		minBackoff: minB,
		maxBackoff: maxB,
		events:     make(chan Event, eventBuffer),
		closing:    make(chan struct{}),
		done:       make(chan struct{}),
	}
	b.snap.Store(emptySnapshot(Connecting))
	go b.run()
	go b.tick()
	return b
}

func (b *backend) CreateLink(session uint64, outPort, inPort Serial) RequestID {
	return b.post(Request{Kind: RequestCreateLink, OutPort: outPort, InPort: inPort}, session)
}

func (b *backend) DestroyLink(session uint64, link Serial) RequestID {
	return b.post(Request{Kind: RequestDestroyLink, Link: link}, session)
}

// post queues a request for the delivery thread and wakes it. a request posted while no connection is open fails at
// once into the current snapshot's request table, whose state is left as it is: under mu, no connection means the
// snapshot already says disconnected (or connecting, before the first connection), never live.
func (b *backend) post(r Request, session uint64) RequestID {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	r.ID = b.nextID
	if b.current == nil {
		now := time.Now()
		r.State, r.Reason, r.Posted, r.Resolved = RequestFailed, "not connected", now, now
		next := *b.snap.Load()
		next.Requests = append(append([]Request(nil), next.Requests...), r)
		if len(next.Requests) > resolvedKept {
			// no connection means nothing is pending; the table keeps the same number of resolved requests a
			// graph would.
			next.Requests = next.Requests[len(next.Requests)-resolvedKept:]
		}
		b.publishLocked(&next, []Event{RequestResolved{ID: r.ID, OK: false, Reason: r.Reason}})
		return r.ID
	}
	b.posted = append(b.posted, queued{request: RequestPosted{Request: r, Session: session}})
	b.current.wake()
	return r.ID
}

// queued is one entry in the posting queue: a request, or a test invocation to run on the loop thread.
type queued struct {
	request RequestPosted
	invoke  func(driver) // test-only: run on the loop thread with the loop lock held
	done    chan error   // receives the invocation's outcome
}

func (b *backend) takePosted() []queued {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.posted
	b.posted = nil
	return p
}

// install makes ts the current transport, unless its session was already lost: open releases the loop before it
// returns, so a loss can be published before the supervisor gets here. lost() closes lostCh inside its critical
// section under mu, so this check under mu is exact.
func (b *backend) install(ts transportSession, sess *session) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-sess.lostCh:
		return false
	default:
	}
	b.current = ts
	if len(b.posted) > 0 {
		ts.wake()
	}
	return true
}

// tick wakes the delivery thread periodically, so request timeouts fire even when the graph is quiet.
func (b *backend) tick() {
	t := time.NewTicker(tickEvery)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			b.mu.Lock()
			if b.current != nil {
				b.current.wake()
			}
			b.mu.Unlock()
		case <-b.closing:
			return
		}
	}
}

func (b *backend) Snapshot() *Snapshot {
	return b.snap.Load()
}

func (b *backend) Events() <-chan Event {
	return b.events
}

func (b *backend) Close() {
	b.once.Do(func() { close(b.closing) })
	<-b.done
}

// run is the supervisor: it opens a session, waits for it to be lost, tears it down, and retries with backoff.
func (b *backend) run() {
	defer close(b.done)
	backoff := b.minBackoff
	for {
		b.sessions++
		sess := newSession(b, b.sessions)
		b.publishState(Connecting, "")
		ts, err := b.tr.open(sess)
		if err != nil {
			dl.Warnf("pipewire connect failed: %v", err)
			b.publishState(Disconnected, err.Error())
		} else if !b.install(ts, sess) {
			ts.close() // lost while opening; it never became current
		} else {
			select {
			case <-sess.lostCh:
			case <-b.closing:
				// closing ends the session like any loss, so its requests fail and disconnected is published.
				ts.end("closed")
				ts.close()
				return
			}
			ts.close() // lost() has already detached it
			if sess.reachedLive {
				backoff = b.minBackoff
			}
		}
		select {
		case <-time.After(backoff):
		case <-b.closing:
			return
		}
		backoff = min(backoff*2, b.maxBackoff)
	}
}

// publish installs a new snapshot (when snap is non-nil) and then delivers events, so a consumer reacting to an event
// finds its subject already in Snapshot.
func (b *backend) publish(snap *Snapshot, events []Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.publishLocked(snap, events)
}

func (b *backend) publishLocked(snap *Snapshot, events []Event) {
	if snap != nil {
		b.generation++
		snap.Generation = b.generation
		b.snap.Store(snap)
	}
	for _, e := range events {
		select {
		case b.events <- e:
		default:
			dl.Warnf("event channel full; dropped %T", e)
		}
	}
}

// publishState republishes the current graph under a new connection state. a disconnected snapshot keeps the last
// graph so the ui can draw it as stale; it is never presented as current.
func (b *backend) publishState(state ConnState, reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cur := b.snap.Load()
	if cur.State == state && cur.Error == reason {
		return
	}
	next := *cur
	next.State = state
	next.Error = reason
	b.publishLocked(&next, []Event{ConnStateChanged{State: state, Error: reason}})
}

// session is the go side of one connection. every method runs on the transport's delivery thread.
type session struct {
	id          uint64
	b           *backend
	g           *Graph
	dead        bool
	reachedLive bool
	lostCh      chan struct{}
}

func newSession(b *backend, id uint64) *session {
	return &session{id: id, b: b, lostCh: make(chan struct{})}
}

// begin creates the session's graph and starts the initial sync round trip.
func (s *session) begin(drv driver) {
	s.g = newGraph(drv)
	s.g.connection = s.id
	s.g.start()
}

func (s *session) apply(in Input) {
	if s.dead || s.g == nil {
		return
	}
	s.g.Apply(in)
}

// flush folds pending changes into a snapshot and publishes it with the events they produced.
func (s *session) flush() {
	if s.dead || s.g == nil {
		return
	}
	for _, q := range s.b.takePosted() {
		if q.invoke != nil {
			q.invoke(s.g.drv)
			q.done <- nil
			continue
		}
		s.g.Apply(q.request)
	}
	s.g.Apply(Tick{})
	snap := s.g.fold()
	if snap != nil && snap.State == Live {
		s.reachedLive = true
	}
	s.b.publish(snap, s.g.takeEvents())
}

// lost ends the session: session state is cleared explicitly, disconnected is published, and the supervisor is
// woken to tear the transport down.
func (s *session) lost(reason string) {
	if s.dead {
		return
	}
	s.dead = true
	dl.Warnf("pipewire connection lost: %v", reason)

	// detaching the transport and publishing the disconnected snapshot are one critical section: a post either
	// lands in the queue drained here or, after it, sees no connection and fails into the disconnected snapshot.
	b := s.b
	b.mu.Lock()
	defer b.mu.Unlock()
	queued := b.posted
	b.posted, b.current = nil, nil

	why := "disconnected: " + reason
	var snap *Snapshot
	var events []Event
	if s.g != nil {
		// queued and pending requests fail into the graph's history, without asking the dead connection for
		// anything, and are folded into the snapshot published below.
		for _, q := range queued {
			if q.invoke != nil {
				q.done <- errors.New(why)
				continue
			}
			s.g.failQueued(q.request, why)
		}
		s.g.teardown(why)
		snap = s.g.fold()
		events = s.g.takeEvents()
	}
	if snap == nil {
		cur := *b.snap.Load()
		snap = &cur
		if s.g == nil && len(queued) > 0 {
			now := time.Now()
			snap.Requests = append([]Request(nil), snap.Requests...)
			for _, q := range queued {
				if q.invoke != nil {
					q.done <- errors.New(why)
					continue
				}
				r := q.request.Request
				r.State, r.Reason, r.Posted, r.Resolved = RequestFailed, why, now, now
				snap.Requests = append(snap.Requests, r)
				events = append(events, RequestResolved{ID: r.ID, OK: false, Reason: why})
			}
		}
	}
	snap.State, snap.Error = Disconnected, reason
	b.publishLocked(snap, append(events, ConnStateChanged{State: Disconnected, Error: reason}))
	close(s.lostCh)
}
