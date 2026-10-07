package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/michaelquigley/dfx"
	"github.com/michaelquigley/patchbay/internal/pipewire"
)

const (
	eventsKept = 30 * time.Second // a resolved event falls off this long after it happened
)

// event is one entry of the performance panel's events. its key names it across frames, so a dismissal holds; a request
// that changes state is a new event under a new key.
type event struct {
	key  string
	text string
	at   time.Time
}

// eventLog is the performance panel's events: requests pending or recently failed, gestures refused before posting,
// arrival batches, and notices, newest first. pending requests are unresolved and stay until they resolve; everything
// else falls off after eventsKept. requests and arrivals are read from their owners each frame; only notices and
// dismissals live here. request descriptions and arrival titles are single-quoted in event text.
type eventLog struct {
	notices   []event
	seq       int
	dismissed map[string]bool
}

// notice records a notice: a refusal or a read-only gesture that never became a request.
func (l *eventLog) notice(text string, at time.Time) {
	l.seq++
	l.notices = append(l.notices, event{key: fmt.Sprintf("notice:%d", l.seq), text: text, at: at})
}

func (l *eventLog) dismiss(key string) {
	if l.dismissed == nil {
		l.dismissed = map[string]bool{}
	}
	l.dismissed[key] = true
}

// collect is this frame's events, newest first, without the dismissed and the resolved that have fallen off.
func (l *eventLog) collect(pt *patching, ar *arrivals, snap *pipewire.Snapshot, now time.Time) []event {
	kept := l.notices[:0]
	for _, n := range l.notices {
		if now.Sub(n.at) <= eventsKept {
			kept = append(kept, n)
		}
	}
	l.notices = kept

	var all []event
	resolved := func(e event) {
		if now.Sub(e.at) <= eventsKept {
			all = append(all, e)
		}
	}
	for _, n := range l.notices {
		resolved(n)
	}
	for _, r := range pt.refused {
		resolved(event{key: fmt.Sprintf("refused:%d:%s", r.at.UnixNano(), r.desc), text: fmt.Sprintf("failed: '%s': %s", r.desc, r.reason), at: r.at})
	}
	if snap != nil {
		for _, r := range snap.Requests {
			key := fmt.Sprintf("request:%d:%d", r.ID, r.State)
			switch r.State {
			case pipewire.RequestPending:
				all = append(all, event{key: key, text: fmt.Sprintf("pending: '%s'", pt.describe(r)), at: r.Posted})
			case pipewire.RequestFailed:
				resolved(event{key: key, text: fmt.Sprintf("failed: '%s': %s", pt.describe(r), r.Reason), at: r.Resolved})
			}
		}
	}
	for _, b := range ar.batches() {
		titles := make([]string, len(b.titles))
		for i, t := range b.titles {
			titles[i] = "'" + t + "'"
		}
		resolved(event{key: fmt.Sprintf("arrived:%d", b.at.UnixNano()), text: "arrived: " + strings.Join(titles, ", ") + " · N to show", at: b.at})
	}

	present := map[string]bool{}
	events := all[:0]
	for _, e := range all {
		present[e.key] = true
		if !l.dismissed[e.key] {
			events = append(events, e)
		}
	}
	for k := range l.dismissed {
		if !present[k] {
			delete(l.dismissed, k)
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if !events[i].at.Equal(events[j].at) {
			return events[i].at.After(events[j].at)
		}
		return events[i].key < events[j].key
	})
	return events
}

// eventAge is how long ago an event happened, compactly.
func eventAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// draw lists the events in the performance panel, newest first, each with its age and a dismiss button.
func (l *eventLog) draw(events []event, now time.Time) {
	if len(events) == 0 {
		imgui.TextDisabled("none recent")
		return
	}
	for _, e := range events {
		imgui.PushIDStr(e.key)
		wrapped(e.text)
		dfx.PushFont(dfx.MonospaceFont)
		imgui.TextDisabled(eventAge(now.Sub(e.at)))
		dfx.PopFont()
		imgui.SameLine()
		if imgui.SmallButton("dismiss") {
			l.dismiss(e.key)
		}
		imgui.PopID()
	}
}
