package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

func captureEventLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	dl.Init(dl.DefaultOptions().JSON().SetOutput(&out))
	t.Cleanup(func() { dl.Init(dl.DefaultOptions()) })
	return &out
}

func TestNoticesAndRefusalsAreLoggedOncePerOccurrence(t *testing.T) {
	out := captureEventLogs(t)
	now := time.Unix(5000, 0)
	var events eventLog
	text := "link refused: " + strings.Repeat("long endpoint name ", 8)
	events.notice(text, now)
	pt := newPatching(nil, nil, func(string) {})
	pt.now = func() time.Time { return now }
	pt.refuse("unlink source → destination", "not connected")
	events.collect(pt, &arrivals{}, nil, now)
	events.collect(pt, &arrivals{}, nil, now)
	if strings.Count(out.String(), "\n") != 2 || !strings.Contains(out.String(), "ui notice: '"+text+"'") ||
		!strings.Contains(out.String(), "failed: 'unlink source → destination': 'not connected'") {
		t.Fatalf("missing, clipped, or repeated warnings: %s", out)
	}
	events.notice(text, now.Add(time.Second))
	if strings.Count(out.String(), "\n") != 3 {
		t.Fatal("a repeated gesture's notice was suppressed")
	}
}

func TestRequestFailureLogsContextOnce(t *testing.T) {
	out := captureEventLogs(t)
	now := time.Unix(6000, 0)
	pt := newPatching(nil, nil, func(string) {})
	pt.descs[1] = "link source:output → destination:input"
	var events eventLog
	snap := &pipewire.Snapshot{
		Session: 7, State: pipewire.Live,
		Requests: []pipewire.Request{{ID: 1, Kind: pipewire.RequestCreateLink, State: pipewire.RequestPending,
			OutPort: 41, InPort: 42, Link: 91, Posted: now}},
		Links: map[pipewire.Serial]pipewire.Link{
			91: {Serial: 91, OutPort: 41, InPort: 42, State: "active"},
			92: {Serial: 92, OutPort: 51, InPort: 52, State: "error", Error: "unrelated"},
		},
	}
	events.collect(pt, &arrivals{}, snap, now)
	events.dismiss("request:1:0")
	if out.Len() != 0 {
		t.Fatal("pending request logged a warning")
	}
	snap.Requests[0].State = pipewire.RequestFailed
	snap.Requests[0].Reason = "confirmation not observed within 2s"
	snap.Requests[0].Resolved = now.Add(2 * time.Second)
	events.collect(pt, &arrivals{}, snap, now.Add(2*time.Second))
	for _, want := range []string{"WARN", pt.descs[1], "'" + snap.Requests[0].Reason + "'", "request=1", "kind='create link'",
		"observed_session=7", "connection='live'", "out_port=41", "in_port=42", "link=91",
		"posted='" + now.Format(time.RFC3339Nano) + "'", "resolved='" + now.Add(2*time.Second).Format(time.RFC3339Nano) + "'",
		"serial=91 state='active' error=''"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in log: %s", want, out)
		}
	}
	if strings.Contains(out.String(), "unrelated") {
		t.Fatal("logged an unrelated link as request context")
	}
	events.dismiss("request:1:2")
	events.collect(pt, &arrivals{}, snap, now.Add(3*time.Second))
	events.collect(pt, &arrivals{}, snap, now.Add(time.Minute))
	if strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("failure was logged again on redraw, dismissal, or expiry: %s", out)
	}
	// even a failure first seen after its toolbar expiry gets a log entry.
	snap.Requests = append(snap.Requests, pipewire.Request{ID: 2, Kind: pipewire.RequestSetForceQuantum,
		State: pipewire.RequestFailed, Quantum: 256, Reason: "permission denied", Posted: now, Resolved: now})
	events.collect(pt, &arrivals{}, snap, now.Add(time.Minute))
	if strings.Count(out.String(), "\n") != 2 || !strings.Contains(out.String(), "quantum=256") {
		t.Fatalf("new failure was not logged: %s", out)
	}
	snap.Requests = nil
	events.collect(pt, &arrivals{}, snap, now.Add(time.Minute))
	if len(events.loggedFailures) != 0 {
		t.Fatal("kept log bookkeeping after requests left the snapshot")
	}
}
