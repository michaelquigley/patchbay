package pipewire

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/michaelquigley/df/dl"
)

// Conn is the backend: one pipewire connection, observed into snapshots and events. the ui reads Snapshot once per
// frame and drains Events; it never touches a native handle.
type Conn interface {
	// Snapshot returns the current immutable snapshot; its pointer identity changes only when content does.
	Snapshot() *Snapshot
	// Events returns the buffered event channel the ui drains each frame.
	Events() <-chan Event
	// Close tears the connection down and stops reconnecting.
	Close()
}

const (
	eventBuffer = 4096
	minBackoff  = time.Second
	maxBackoff  = 10 * time.Second
)

// transport reaches a pipewire daemon. open must attach the registry listener and call sess.begin with the driver
// before any input can be delivered, and must deliver every input, flush, and loss on one thread at a time.
type transport interface {
	open(sess *session) (transportSession, error)
}

// transportSession is one open connection.
type transportSession interface {
	close()
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

	publishMu  sync.Mutex
	generation uint64

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
	return b
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
		} else {
			select {
			case <-sess.lostCh:
			case <-b.closing:
				ts.close()
				return
			}
			ts.close()
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
	b.publishMu.Lock()
	defer b.publishMu.Unlock()
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
	b.publishMu.Lock()
	defer b.publishMu.Unlock()
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
	if s.g != nil {
		s.g.teardown("disconnected: " + reason)
		s.b.publish(nil, s.g.takeEvents())
	}
	s.b.publishState(Disconnected, reason)
	close(s.lostCh)
}
