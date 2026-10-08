---
title: check whether two syncs can own startup readiness
state: evaluating
created: 2026-10-07
tags: [enhancement, spike]
---

verify whether the two existing PipeWire sync round trips suffice for initial readiness, allowing removal of the additional per-object `pendingInfo` gate. trace initial bound-info delivery for node, port, link, device, and client on PipeWire 1.0, including bind refusal and objects arriving during the second round trip. if the contract covers initial enumeration, latch live at the second sync and let later arrivals update normally. retain the initial enumeration barrier itself so recognition never claims one of two startup candidates prematurely.

## why

The original readiness gate required both `barrierPassed` and an empty `pendingInfo`, while `globalAdded` added every object until the latch, including arrivals after the first sync. `TestBarrierWaitsForFirstInfo` supplied a first-info callback after both syncs, but a synthetic order was not evidence the native transport could produce it for the initial bind set. A permanently missing callback with neither proxy error nor removal could hold the entire UI connecting.

The PipeWire 1.0 source trace establishes synchronous initial info for all five bound object types and synchronous bind refusals. Michael accepted removing the extra gate; the two syncs now own readiness, and arrivals after the first done update normally. The supported ordering and source references live in [the backend contract](../../current/backend.md#connection-lifecycle).
