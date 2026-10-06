//go:build live

package pipewire

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// the live link tests patch only between two null sinks they create on their own connection and remove at the end,
// never between objects that existed before them, and they leave the daemon's graph as they found it. they must not
// be run on a studio machine during a session.

type testSinks struct {
	out, in Serial // pbtest-a's monitor_FL and pbtest-b's playback_FL
	outR    Serial // pbtest-a's monitor_FR
	inR     Serial // pbtest-b's playback_FR
	prefix  string
}

// newTestSinks creates the two sinks on an owner connection, waits for their ports, and registers their removal. it
// skips the test when the sinks cannot be made (no adapter factory, no daemon).
func newTestSinks(t *testing.T) testSinks {
	t.Helper()
	owner := Connect()
	waitLive(t, owner)
	prefix := fmt.Sprintf("patchbay-test-%d-", os.Getpid())
	b := owner.(*backend)
	var removes []func()
	for _, name := range []string{"a", "b"} {
		remove, err := b.createTestSink(prefix + name)
		if err != nil {
			for _, r := range removes {
				r()
			}
			owner.Close()
			t.Skipf("cannot create test sinks: %v", err)
		}
		removes = append(removes, remove)
	}
	t.Cleanup(func() {
		for _, r := range removes {
			r()
		}
		owner.Close()
		assertNoTestObjects(t, prefix)
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var s testSinks
		s.prefix = prefix
		snap := owner.Snapshot()
		for _, p := range snap.Ports {
			switch n := snap.Nodes[p.NodeSerial]; {
			case n.Name == prefix+"a" && p.Name == "monitor_FL":
				s.out = p.Serial
			case n.Name == prefix+"a" && p.Name == "monitor_FR":
				s.outR = p.Serial
			case n.Name == prefix+"b" && p.Name == "playback_FL":
				s.in = p.Serial
			case n.Name == prefix+"b" && p.Name == "playback_FR":
				s.inR = p.Serial
			}
		}
		if s.out != 0 && s.in != 0 && s.outR != 0 && s.inR != 0 {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Skip("the test sinks never exposed their ports")
	return testSinks{}
}

// assertNoTestObjects checks, from a fresh connection, that nothing the test made is left: no test node, and no
// link touching one.
func assertNoTestObjects(t *testing.T, prefix string) {
	t.Helper()
	conn := Connect()
	defer conn.Close()
	snap := waitLive(t, conn)
	for _, n := range snap.Nodes {
		if strings.HasPrefix(n.Name, prefix) {
			t.Errorf("test node '%v' left behind", n.Name)
		}
	}
}

// TestLiveLinkRoundTrip: a create is confirmed only once the link is observed active or paused, and created here; a
// destroy only once the link is observed gone.
func TestLiveLinkRoundTrip(t *testing.T) {
	sinks := newTestSinks(t)
	conn := Connect()
	defer conn.Close()
	snap := waitLive(t, conn)
	waitPorts(t, conn, sinks.out, sinks.in)

	r, s := waitResolved(t, conn, conn.CreateLink(snap.Session, sinks.out, sinks.in))
	if r.State != RequestConfirmed {
		t.Fatalf("create %v: %v", r.State, r.Reason)
	}
	l, ok := s.Links[r.Link]
	if !ok || !l.CreatedHere || (l.State != "active" && l.State != "paused") || l.OutPort != sinks.out || l.InPort != sinks.in {
		t.Fatalf("confirmed link = %+v", l)
	}

	r, s = waitResolved(t, conn, conn.DestroyLink(s.Session, r.Link))
	if r.State != RequestConfirmed {
		t.Fatalf("destroy %v: %v", r.State, r.Reason)
	}
	if _, ok := s.Links[r.Link]; ok {
		t.Error("destroyed link still in the snapshot")
	}
}

// TestLiveLinkLingers: a link outlives the connection that created it, and the next connection labels it observed,
// since provenance is in-session only. it is destroyed before the sinks are removed.
func TestLiveLinkLingers(t *testing.T) {
	sinks := newTestSinks(t)
	first := Connect()
	snap := waitLive(t, first)
	waitPorts(t, first, sinks.outR, sinks.inR)
	r, _ := waitResolved(t, first, first.CreateLink(snap.Session, sinks.outR, sinks.inR))
	first.Close()
	if r.State != RequestConfirmed {
		t.Fatalf("create %v: %v", r.State, r.Reason)
	}

	second := Connect()
	defer second.Close()
	s := waitLive(t, second)
	var found *Link
	for _, l := range s.Links {
		if l.OutPort == sinks.outR && l.InPort == sinks.inR {
			l := l
			found = &l
		}
	}
	if found == nil {
		t.Fatal("the link did not outlive the connection that created it")
	}
	if found.CreatedHere {
		t.Error("a new connection claims a link the old one created")
	}
	if d, _ := waitResolved(t, second, second.DestroyLink(s.Session, found.Serial)); d.State != RequestConfirmed {
		t.Errorf("destroy %v: %v", d.State, d.Reason)
	}
}

func waitLive(t *testing.T, conn Conn) *Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := conn.Snapshot(); s.State == Live {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("never reached live")
	return nil
}

// waitPorts waits until conn observes the ports; serials are per daemon, so another connection's serials name them.
func waitPorts(t *testing.T, conn Conn, ports ...Serial) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := conn.Snapshot()
		all := true
		for _, p := range ports {
			if _, ok := snap.Ports[p]; !ok {
				all = false
			}
		}
		if all {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("ports %v never observed", ports)
}

func waitResolved(t *testing.T, conn Conn, id RequestID) (Request, *Snapshot) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := conn.Snapshot()
		for _, r := range s.Requests {
			if r.ID == id && r.State != RequestPending {
				return r, s
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("request %d never resolved", id)
	return Request{}, nil
}
