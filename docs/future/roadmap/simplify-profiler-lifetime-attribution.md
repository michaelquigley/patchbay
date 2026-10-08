---
title: simplify profiler lifetime attribution
state: inbox
created: 2026-10-07
tags: [enhancement]
---

decide whether compact xrun monitoring needs the appearance-time lifetime heuristic. the audit recommendation is to use the existing ordering-only behavior for all drivers: resolve ids to the current serial, drop records on removal and reconnect, baseline the first counter, reject older pods, and rebase a counter that decreases. remove first-pod clock classification, arrival/appearance timestamps used only by that classification, and the inspector's guard-mode diagnostic. document the resulting attribution limit and update `serial-keyed-live-state` with the decision before changing code.

## why

`internal/pipewire/profiler.go:117` classifies a driver's first pod by a one-second distance from arrival time, stores that decision for its lifetime, and propagates it into each node's metrics. This needs native monotonic clock readings, registry appearance timestamps, two guard modes, special handling of clockless pods, and guard explanation in the UI. The recorded desktop video clock already requires the weaker path, and a delayed first pod chooses it permanently even for an otherwise current clock.

The cost of removing the stronger path is specific: after id reuse, a buffered pod can initialize the replacement's baseline and distort its total/new counts until subsequent observations settle it. Serial-keyed records alone cannot solve this because the profiler payload names an id. This is a product tradeoff, not a claim of equivalent correctness. Preserve the counter/version availability handling and the distinction between unknown data and zero; neither requires this heuristic. Keep the small out-of-order check initially rather than replacing the heuristic with another grace period, epoch scheme, or timing threshold.
