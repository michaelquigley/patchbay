---
title: remove unused backend event payloads
state: inbox
created: 2026-10-07
tags: [enhancement]
---

remove the backend's typed event stream and make snapshots its sole observation surface. retain the existing polling loop in `dump`; the UI already reads snapshots every frame. remove event construction, buffering, overflow logging, drain loops, and tests of event payloads and publication order. keep `ObjectKind` and `RequestID` as ordinary graph/request types, and keep the UI's performance-panel events, which are derived separately from snapshots and local gestures. update the backend contract, AGENTS.md, and the canon's snapshot boundary together.

## why

`internal/ui/app.go:226` drains and discards every backend event. `cmd/patchbay/dump.go:54` uses any event only as an early wake alongside its 50 ms ticker. No production consumer inspects the five payload types in `internal/pipewire/events.go`. Nevertheless, `objects.go`, `requests.go`, and `conn.go` construct and order them, and the backend maintains a 4096-entry lossy channel. The concrete tradeoff is up to one polling interval of extra dump latency; the canvas and request feedback retain their current observation path. This does not remove the native loop wake that publishes snapshots and services requests.
