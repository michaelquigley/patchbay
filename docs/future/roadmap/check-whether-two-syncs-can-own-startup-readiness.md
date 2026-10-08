---
title: check whether two syncs can own startup readiness
state: inbox
created: 2026-10-07
tags: [enhancement, spike]
---

verify whether the two existing PipeWire sync round trips suffice for initial readiness, allowing removal of the additional per-object `pendingInfo` gate. trace initial bound-info delivery for node, port, link, device, and client on PipeWire 1.0, including bind refusal and objects arriving during the second round trip. if the contract covers initial enumeration, latch live at the second sync and let later arrivals update normally. retain the initial enumeration barrier itself so recognition never claims one of two startup candidates prematurely.

## why

`internal/pipewire/objects.go:315` requires both `barrierPassed` and an empty `pendingInfo`, while `globalAdded` adds every object until the latch, including arrivals after the first sync. `TestBarrierWaitsForFirstInfo` supplies a first-info callback after both syncs, but a synthetic order is not evidence the native transport can produce it for the initial bind set. A permanently missing callback with neither proxy error nor removal can hold the entire UI connecting.

The [PipeWire 1.0 sync contract](https://github.com/PipeWire/pipewire/blob/1.0.0/src/pipewire/core.h#L232-L245) orders previous methods and their resulting events before done. That supports this investigation, but the audit has not established every bind implementation's timing or the correct treatment of arrivals during the second round trip. This is a spike, not a proven deletion. Do not substitute a startup delay or timeout heuristic for the barrier.
