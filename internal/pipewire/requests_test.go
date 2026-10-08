package pipewire

import (
	"strings"
	"testing"
	"time"
)

// requestGraph is a live graph with two nodes and an output and input port on each side, a fake driver, and a
// clock the test moves.
type requestGraph struct {
	t     *testing.T
	g     *Graph
	drv   *fakeSession
	clock time.Time
}

func newRequestGraph(t *testing.T) *requestGraph {
	t.Helper()
	drv := &fakeSession{bound: map[Serial]bool{}, proxies: map[RequestID]bool{}}
	rg := &requestGraph{t: t, drv: drv, clock: time.Unix(1000, 0)}
	g := newGraph(drv)
	g.connection = 1
	g.now = func() time.Time { return rg.clock }
	g.start()
	rg.g = g
	g.Apply(nodeGlobal(1, 10, "out"))
	g.Apply(nodeGlobal(2, 20, "in"))
	g.Apply(portGlobal(129, 1290, 1, "out"))
	g.Apply(portGlobal(130, 1300, 2, "in"))
	return rg
}

func (rg *requestGraph) post(r Request) RequestID {
	rg.t.Helper()
	r.ID = RequestID(len(rg.g.resolved) + len(rg.g.session.pending) + 1)
	rg.g.Apply(RequestPosted{Request: r, Session: 1})
	return r.ID
}

func (rg *requestGraph) request(id RequestID) Request {
	rg.t.Helper()
	for _, r := range rg.snapshot().Requests {
		if r.ID == id {
			return r
		}
	}
	rg.t.Fatalf("request %d not in the snapshot", id)
	return Request{}
}

func (rg *requestGraph) snapshot() *Snapshot {
	rg.g.dirty = true
	return rg.g.fold()
}

func (rg *requestGraph) create() RequestID {
	return rg.post(Request{Kind: RequestCreateLink, OutPort: 1290, InPort: 1300})
}

func expectState(t *testing.T, r Request, state RequestState, reason string) {
	t.Helper()
	if r.State != state {
		t.Errorf("request %d is %v (%q), want %v", r.ID, r.State, r.Reason, state)
	}
	if reason != "" && !strings.Contains(r.Reason, reason) {
		t.Errorf("request %d reason %q, want it to mention %q", r.ID, r.Reason, reason)
	}
}

// the link appears only when observed: confirmed once the bound global is announced with the requested endpoints and
// reaches active; it is then created here, and the proxy is released (the link lingers).
func TestCreateConfirmedOnObservedActive(t *testing.T) {
	for _, globalFirst := range []bool{false, true} {
		rg := newRequestGraph(t)
		id := rg.create()
		if len(rg.drv.created) != 1 || rg.drv.created[0] != [4]uint32{1, 129, 2, 130} {
			t.Fatalf("link factory asked for %v", rg.drv.created)
		}
		if globalFirst {
			rg.g.Apply(linkGlobal(140, 1400, 129, 130, 1, 2))
			rg.g.Apply(LinkBound{Request: id, ID: 140})
		} else {
			rg.g.Apply(LinkBound{Request: id, ID: 140})
			rg.g.Apply(linkGlobal(140, 1400, 129, 130, 1, 2))
		}
		rg.g.Apply(LinkInfo{Serial: 1400, State: "negotiating"})
		expectState(t, rg.request(id), RequestPending, "")
		if rg.snapshot().Links[1400].CreatedHere {
			t.Error("created here before it was active")
		}
		rg.g.Apply(LinkInfo{Serial: 1400, State: "active"})
		r := rg.request(id)
		expectState(t, r, RequestConfirmed, "")
		if r.Link != 1400 || !rg.snapshot().Links[1400].CreatedHere {
			t.Errorf("confirmed link %d, created here %v", r.Link, rg.snapshot().Links[1400].CreatedHere)
		}
		if rg.drv.proxies[id] {
			t.Error("proxy not released after confirmation")
		}
	}
}

// a request naming a port serial that has vanished fails with a reason, and nothing is asked of the factory.
func TestCreateVanishedPortFails(t *testing.T) {
	rg := newRequestGraph(t)
	rg.g.Apply(GlobalRemoved{ID: 130})
	id := rg.create()
	expectState(t, rg.request(id), RequestFailed, "input port 1300 is not observed")
	if len(rg.drv.created) != 0 {
		t.Error("the factory was asked for a link to a vanished port")
	}
	if len(rg.snapshot().Links) != 0 {
		t.Error("a phantom link appeared")
	}
}

// the target port's id is reused by a different port between validation and the link's appearance: the link the
// bound id names connects the wrong port, so the request fails, that link is destroyed, and nothing is created here.
func TestCreateWrongRouteIsDestroyed(t *testing.T) {
	rg := newRequestGraph(t)
	id := rg.create()
	rg.g.Apply(GlobalRemoved{ID: 130})
	rg.g.Apply(portGlobal(130, 1301, 2, "in"))
	rg.g.Apply(LinkBound{Request: id, ID: 140})
	rg.g.Apply(linkGlobal(140, 1400, 129, 130, 1, 2))
	rg.g.Apply(LinkInfo{Serial: 1400, State: "active"})
	expectState(t, rg.request(id), RequestFailed, "wrong route")
	if len(rg.drv.destroyed) != 1 || rg.drv.destroyed[0] != 140 {
		t.Errorf("destroyed %v, want only the wrong-route link 140", rg.drv.destroyed)
	}
	if len(rg.g.session.createdHere) != 0 || rg.snapshot().Links[1400].CreatedHere {
		t.Error("the wrong-route link was recorded as created here")
	}
}

// patchbay's link is removed before reaching active and another client's link appears under the same id between the
// same ports: the request fails, and the replacement, a new lifetime, is observed.
func TestCreateSameIDReplacementIsObserved(t *testing.T) {
	rg := newRequestGraph(t)
	id := rg.create()
	rg.g.Apply(LinkBound{Request: id, ID: 140})
	rg.g.Apply(linkGlobal(140, 1400, 129, 130, 1, 2))
	rg.g.Apply(LinkInfo{Serial: 1400, State: "init"})
	rg.g.Apply(GlobalRemoved{ID: 140})
	expectState(t, rg.request(id), RequestFailed, "removed before it became active")

	rg.g.Apply(linkGlobal(140, 1401, 129, 130, 1, 2))
	rg.g.Apply(LinkInfo{Serial: 1401, State: "active"})
	s := rg.snapshot()
	if l, ok := s.Links[1401]; !ok || l.CreatedHere {
		t.Errorf("replacement link = %+v, want observed", l)
	}
	expectState(t, rg.request(id), RequestFailed, "")
}

// patchbay's link is announced and removed, and another link takes its id between the same ports, all before the
// proxy's bound callback arrives: the request's link is the first lifetime under the bound id, which is gone, so the
// request fails; the replacement is never captured, and is drawn as observed.
func TestBoundAfterReplacementFails(t *testing.T) {
	rg := newRequestGraph(t)
	id := rg.create()
	rg.g.Apply(linkGlobal(140, 1400, 129, 130, 1, 2))
	rg.g.Apply(GlobalRemoved{ID: 140})
	rg.g.Apply(linkGlobal(140, 1401, 129, 130, 1, 2))
	rg.g.Apply(LinkInfo{Serial: 1401, State: "active"})
	rg.g.Apply(LinkBound{Request: id, ID: 140})

	r := rg.request(id)
	expectState(t, r, RequestFailed, "the link was removed before it became active")
	if r.Link != 1400 {
		t.Errorf("request bound to link %d, want the first lifetime 1400", r.Link)
	}
	s := rg.snapshot()
	l, ok := s.Links[1401]
	if !ok || l.CreatedHere {
		t.Errorf("replacement link = %+v, want observed", l)
	}
	if len(rg.g.session.createdHere) != 0 {
		t.Error("something was recorded as created here")
	}
	var listed bool
	for _, q := range s.Requests {
		listed = listed || (q.ID == id && q.State == RequestFailed && strings.Contains(q.Reason, "removed before"))
	}
	if !listed {
		t.Errorf("the failed request is not in the request table: %+v", s.Requests)
	}
}

// another client links the same ports while a request is pending: that link is observed; patchbay's own, when it
// lands, is created here.
func TestConcurrentLinkIsObserved(t *testing.T) {
	rg := newRequestGraph(t)
	id := rg.create()
	rg.g.Apply(linkGlobal(139, 1399, 129, 130, 1, 2))
	rg.g.Apply(LinkInfo{Serial: 1399, State: "active"})
	expectState(t, rg.request(id), RequestPending, "")
	rg.g.Apply(LinkBound{Request: id, ID: 140})
	rg.g.Apply(linkGlobal(140, 1400, 129, 130, 1, 2))
	rg.g.Apply(LinkInfo{Serial: 1400, State: "paused"})
	expectState(t, rg.request(id), RequestConfirmed, "")
	s := rg.snapshot()
	if s.Links[1399].CreatedHere || !s.Links[1400].CreatedHere {
		t.Errorf("other client's link created here %v, ours %v", s.Links[1399].CreatedHere, s.Links[1400].CreatedHere)
	}
}

func TestCreateFailures(t *testing.T) {
	t.Run("timeout without a bound global", func(t *testing.T) {
		rg := newRequestGraph(t)
		id := rg.create()
		rg.clock = rg.clock.Add(requestTimeout - time.Millisecond)
		rg.g.Apply(Tick{})
		expectState(t, rg.request(id), RequestPending, "")
		rg.clock = rg.clock.Add(2 * time.Millisecond)
		rg.g.Apply(Tick{})
		expectState(t, rg.request(id), RequestFailed, "no link appeared")
		if rg.drv.proxies[id] {
			t.Error("proxy not released after the timeout")
		}
	})
	t.Run("error state", func(t *testing.T) {
		rg := newRequestGraph(t)
		id := rg.create()
		rg.g.Apply(LinkBound{Request: id, ID: 140})
		rg.g.Apply(linkGlobal(140, 1400, 129, 130, 1, 2))
		rg.g.Apply(LinkInfo{Serial: 1400, State: "error", Error: "no format"})
		expectState(t, rg.request(id), RequestFailed, "no format")
	})
	t.Run("proxy error", func(t *testing.T) {
		rg := newRequestGraph(t)
		id := rg.create()
		rg.g.Apply(LinkProxyError{Request: id, Error: "File exists (file exists)"})
		expectState(t, rg.request(id), RequestFailed, "File exists")
	})
	t.Run("proxy removed", func(t *testing.T) {
		rg := newRequestGraph(t)
		id := rg.create()
		rg.g.Apply(LinkProxyRemoved{Request: id})
		expectState(t, rg.request(id), RequestFailed, "removed")
	})
	t.Run("a stale session", func(t *testing.T) {
		rg := newRequestGraph(t)
		rg.g.Apply(RequestPosted{Request: Request{ID: 9, Kind: RequestCreateLink, OutPort: 1290, InPort: 1300}, Session: 7})
		expectState(t, rg.request(9), RequestFailed, "graph changed")
		if len(rg.drv.created) != 0 {
			t.Error("a request against another session reached the factory")
		}
	})
}

// a destroy is confirmed by the link's removal, fails on a link that is not observed, and times out.
func TestDestroy(t *testing.T) {
	rg := newRequestGraph(t)
	rg.g.Apply(linkGlobal(140, 1400, 129, 130, 1, 2))
	rg.g.Apply(LinkInfo{Serial: 1400, State: "active"})

	id := rg.post(Request{Kind: RequestDestroyLink, Link: 1400})
	if len(rg.drv.destroyed) != 1 || rg.drv.destroyed[0] != 140 {
		t.Fatalf("destroyed %v", rg.drv.destroyed)
	}
	expectState(t, rg.request(id), RequestPending, "")
	if _, ok := rg.snapshot().Links[1400]; !ok {
		t.Error("the link left the snapshot before its removal was observed")
	}
	rg.g.Apply(GlobalRemoved{ID: 140})
	expectState(t, rg.request(id), RequestConfirmed, "")

	missing := rg.post(Request{Kind: RequestDestroyLink, Link: 1400})
	expectState(t, rg.request(missing), RequestFailed, "not observed")

	rg.g.Apply(linkGlobal(141, 1410, 129, 130, 1, 2))
	slow := rg.post(Request{Kind: RequestDestroyLink, Link: 1410})
	rg.clock = rg.clock.Add(requestTimeout + time.Millisecond)
	rg.g.Apply(Tick{})
	expectState(t, rg.request(slow), RequestFailed, "not removed")
}

// a pending request fails with the disconnect reason, and the last snapshot, already disconnected, shows it.
func TestDisconnectFailsPendingInSnapshot(t *testing.T) {
	tr := newFakeTransport()
	b := startBackend(tr, time.Millisecond, 5*time.Millisecond)
	defer b.Close()
	fs := nextSession(t, tr)
	fs.callback(nodeGlobal(1, 10, "out"), nodeGlobal(2, 20, "in"),
		portGlobal(129, 1290, 1, "out"), portGlobal(130, 1300, 2, "in"))
	fs.callback(NodeInfo{Serial: 10}, NodeInfo{Serial: 20},
		PortInfo{Serial: 1290, Direction: DirectionOut}, PortInfo{Serial: 1300, Direction: DirectionIn})
	fs.done()
	fs.done()
	live := waitState(t, b, Live)

	id := b.CreateLink(live.Session, 1290, 1300)
	fs.callback()
	var pending bool
	for _, r := range b.Snapshot().Requests {
		pending = pending || (r.ID == id && r.State == RequestPending)
	}
	if !pending {
		t.Fatalf("request not pending: %+v", b.Snapshot().Requests)
	}

	fs.sess.lost("connection error (broken pipe)")
	s := b.Snapshot()
	if s.State != Disconnected {
		t.Fatalf("state %v", s.State)
	}
	var failed bool
	for _, r := range s.Requests {
		failed = failed || (r.ID == id && r.State == RequestFailed && strings.Contains(r.Reason, "disconnected"))
	}
	if !failed {
		t.Errorf("the disconnected snapshot does not show the failed request: %+v", s.Requests)
	}
}

func liveFake(t *testing.T) (*fakeTransport, *backend, *fakeSession, *Snapshot) {
	t.Helper()
	tr := newFakeTransport()
	b := startBackend(tr, time.Millisecond, 5*time.Millisecond)
	fs := nextSession(t, tr)
	fs.callback(nodeGlobal(1, 10, "out"), nodeGlobal(2, 20, "in"),
		portGlobal(129, 1290, 1, "out"), portGlobal(130, 1300, 2, "in"))
	fs.callback(NodeInfo{Serial: 10}, NodeInfo{Serial: 20},
		PortInfo{Serial: 1290, Direction: DirectionOut}, PortInfo{Serial: 1300, Direction: DirectionIn})
	fs.done()
	fs.done()
	return tr, b, fs, waitState(t, b, Live)
}

func requestIn(s *Snapshot, id RequestID) (Request, bool) {
	for _, r := range s.Requests {
		if r.ID == id {
			return r, true
		}
	}
	return Request{}, false
}

// a request posted but not yet drained when the connection drops fails into the disconnected snapshot with the
// disconnect reason, and is never handed to a later connection's link factory.
func TestQueuedRequestFailsOnLoss(t *testing.T) {
	tr, b, fs, live := liveFake(t)
	defer b.Close()
	id := b.CreateLink(live.Session, 1290, 1300) // queued: the fake transport's wake does not flush

	fs.sess.lost("connection error (broken pipe)")
	s := b.Snapshot()
	r, ok := requestIn(s, id)
	if s.State != Disconnected || !ok || r.State != RequestFailed || !strings.Contains(r.Reason, "disconnected") {
		t.Fatalf("disconnected snapshot: state %v, request %+v (found %v)", s.State, r, ok)
	}
	if len(fs.created) != 0 {
		t.Error("the lost connection's factory was asked for a link")
	}

	next := nextSession(t, tr)
	next.callback(nodeGlobal(1, 10, "out"), NodeInfo{Serial: 10})
	next.done()
	next.done()
	fresh := waitState(t, b, Live)
	if len(next.created) != 0 {
		t.Error("a request from the lost connection reached the next connection's factory")
	}
	if len(fresh.Requests) != 0 {
		t.Errorf("the next connection's request table is not fresh: %+v", fresh.Requests)
	}
}

// a post after lost()'s critical section sees no connection and fails into the disconnected snapshot, which stays
// disconnected: one publication, never a live one.
func TestPostAfterLossLandsInDisconnectedSnapshot(t *testing.T) {
	tr, b, fs, live := liveFake(t)
	defer b.Close()
	tr.alwaysFail = true
	fs.sess.lost("connection error (broken pipe)")
	before := b.Snapshot()
	id := b.CreateLink(live.Session, 1290, 1300)
	after := b.Snapshot()
	r, ok := requestIn(after, id)
	if !ok || r.State != RequestFailed || r.Reason != "not connected" {
		t.Fatalf("request = %+v (found %v)", r, ok)
	}
	if after.State == Live || after.State != before.State || after.Generation != before.Generation+1 {
		t.Errorf("post republished state %v (was %v), generation %d (was %d)", after.State, before.State, after.Generation, before.Generation)
	}
}

// posts racing a loss all land somewhere the snapshot shows: queued ones fail with the disconnect, later ones fail
// at once; none is lost, none stays pending, and no snapshot after the loss reads live. the total stays under the
// request table's size, so every outcome is still in it.
func TestPostsRacingLossAllLand(t *testing.T) {
	for round := 0; round < 20; round++ {
		tr, b, fs, live := liveFake(t)
		tr.alwaysFail = true
		var ids []RequestID
		for i := 0; i < 4; i++ {
			ids = append(ids, b.CreateLink(live.Session, 1290, 1300))
		}
		racing := make(chan RequestID, 8)
		go func() {
			for i := 0; i < 8; i++ {
				racing <- b.CreateLink(live.Session, 1290, 1300)
			}
			close(racing)
		}()
		fs.sess.lost("connection error (broken pipe)")
		for id := range racing {
			ids = append(ids, id)
		}

		s := b.Snapshot()
		if s.State == Live {
			t.Fatal("a snapshot after the loss reads live")
		}
		table := map[RequestID]Request{}
		for _, r := range s.Requests {
			table[r.ID] = r
		}
		for _, id := range ids {
			r, ok := table[id]
			switch {
			case !ok:
				t.Errorf("round %d: request %d is in no snapshot", round, id)
			case r.State != RequestFailed:
				t.Errorf("round %d: request %d is %v", round, id, r.State)
			case r.Reason != "not connected" && !strings.HasPrefix(r.Reason, "disconnected"):
				t.Errorf("round %d: request %d failed for %q", round, id, r.Reason)
			}
		}
		b.Close()
	}
}

// a request posted while connect attempts keep failing fails at once, visibly, and is never queued.
func TestPostWithoutConnectionFailsAtOnce(t *testing.T) {
	tr := newFakeTransport()
	tr.alwaysFail = true
	b := startBackend(tr, time.Millisecond, 5*time.Millisecond)
	defer b.Close()
	waitState(t, b, Disconnected)

	id := b.CreateLink(1, 1290, 1300)
	r, ok := requestIn(b.Snapshot(), id)
	if !ok || r.State != RequestFailed || r.Reason != "not connected" {
		t.Fatalf("request = %+v (found %v)", r, ok)
	}
	if len(b.takePosted()) != 0 {
		t.Error("a request with no connection was queued")
	}
}

// a connection lost before open returns is never installed: a post afterwards fails at once and nothing is queued.
func TestLostWhileOpeningIsNeverCurrent(t *testing.T) {
	tr := newFakeTransport()
	tr.loseInOpen = true
	b := startBackend(tr, time.Millisecond, 5*time.Millisecond)
	defer b.Close()
	fs := nextSession(t, tr)
	select {
	case <-fs.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the transport lost while opening was not closed")
	}
	id := b.CreateLink(1, 1290, 1300)
	r, ok := requestIn(b.Snapshot(), id)
	if !ok || r.State != RequestFailed || r.Reason != "not connected" {
		t.Errorf("request = %+v (found %v)", r, ok)
	}
	if len(b.takePosted()) != 0 {
		t.Error("a request was queued against a lost transport")
	}
}

// Close ends the session like a loss: a queued request and a pending one both fail with the close, and the final
// snapshot reads disconnected.
func TestCloseFailsQueuedAndPending(t *testing.T) {
	_, b, fs, live := liveFake(t)
	pending := b.CreateLink(live.Session, 1290, 1300)
	fs.callback() // hands it to the graph: pending, the factory asked
	if r, _ := requestIn(b.Snapshot(), pending); r.State != RequestPending {
		t.Fatalf("not pending: %+v", r)
	}
	queued := b.CreateLink(live.Session, 1290, 1300) // never drained
	b.Close()

	s := b.Snapshot()
	if s.State != Disconnected || s.Error != "closed" {
		t.Errorf("final snapshot reads %v (%q)", s.State, s.Error)
	}
	for _, id := range []RequestID{pending, queued} {
		r, ok := requestIn(s, id)
		if !ok || r.State != RequestFailed || r.Reason != "disconnected: closed" {
			t.Errorf("request %d = %+v (found %v)", id, r, ok)
		}
	}
}

// the test-only invoke runs on the delivery thread through the request queue: not connected without a transport, run
// at the next flush while live, failed with the disconnect when the connection is lost first.
func TestInvoke(t *testing.T) {
	tr := newFakeTransport()
	tr.alwaysFail = true
	idle := startBackend(tr, time.Millisecond, 5*time.Millisecond)
	if err := idle.invoke(func(driver) { t.Error("ran without a connection") }); err == nil || err.Error() != "not connected" {
		t.Errorf("invoke without a connection = %v", err)
	}
	idle.Close()

	_, b, fs, _ := liveFake(t)
	defer b.Close()
	var ran driver
	result := make(chan error, 1)
	go func() { result <- b.invoke(func(d driver) { ran = d }) }()
	waitQueued(t, b)
	fs.callback()
	if err := <-result; err != nil || ran != fs {
		t.Errorf("invoke = %v, ran on %v", err, ran)
	}

	go func() { result <- b.invoke(func(driver) { t.Error("ran on a lost connection") }) }()
	waitQueued(t, b)
	fs.sess.lost("connection error (broken pipe)")
	if err := <-result; err == nil || !strings.HasPrefix(err.Error(), "disconnected") {
		t.Errorf("invoke at loss = %v", err)
	}
}

func waitQueued(t *testing.T, b *backend) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		n := len(b.posted)
		b.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("nothing was queued")
}

// clock.force-quantum is seen only as a whole number of zero or more; a garbage or negative value is unknown, never
// automatic.
func TestForceQuantumSeen(t *testing.T) {
	for _, c := range []struct {
		value string
		seen  bool
		q     int
	}{{"256", true, 256}, {" 0 ", true, 0}, {"garbage", false, 0}, {"-64", false, 0}, {"", false, 0}} {
		s := decodeSettings(map[string]MetadataEntry{"clock.force-quantum": {Key: "clock.force-quantum", Value: c.value}})
		if s.ForceSeen != c.seen || s.ForceQuantum != c.q {
			t.Errorf("%q decoded seen %v, quantum %d; want %v, %d", c.value, s.ForceSeen, s.ForceQuantum, c.seen, c.q)
		}
	}
	if s := decodeSettings(map[string]MetadataEntry{}); s.ForceSeen {
		t.Error("an absent key decoded as seen")
	}
}
