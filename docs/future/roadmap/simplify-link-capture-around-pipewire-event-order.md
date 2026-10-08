---
title: simplify link capture around PipeWire event order
state: inbox
created: 2026-10-07
tags: [enhancement]
---

simplify create-request capture to rely on PipeWire's bound-before-registry ordering. remove `linkRequest.firstUnder` and the reverse-order lifetime reconstruction in `linkBound` and `linkAnnounced`. review whether the posting watermark and `Graph.maxSerial` still serve any reachable case after this change. retain capture of one serial, endpoint verification, wrong-route cleanup, proxy error/removal handling, observed state confirmation, and deferred proxy release. replace synthetic reverse-order tests with the supported transport sequence, and verify that sequence with test-owned null sinks on the supported PipeWire versions before landing.

## why

`internal/pipewire/requests.go:120` records the first link announced under every id for every unbound create. `TestCreateConfirmedOnObservedActive` explicitly supplies both callback orders; `TestBoundAfterReplacementFails` supplies a link announcement, removal, replacement, and only then the bound notification. The latter sequence drives a substantial part of the hardening.

PipeWire 1.0.0's [core contract](https://github.com/PipeWire/pipewire/blob/1.0.0/src/pipewire/core.h#L160-L170) says the bound-id event precedes registry visibility. Its [link registration](https://github.com/PipeWire/pipewire/blob/1.0.0/src/pipewire/impl-link.c#L1550-L1553) emits initialized before registering the global; [link-factory's initialized handler](https://github.com/PipeWire/pipewire/blob/1.0.0/src/modules/module-link-factory.c#L119-L134) binds the creating client's resource there. On the client, `core_event_bound_id` calls `pw_proxy_set_bound_id`, which emits the proxy callback synchronously. Patchbay applies these callbacks directly on the same loop thread. The audit therefore finds no native path to the reverse ordering modeled by those tests. This is a contract-based deletion, not permission to associate a request with a later same-id replacement.

Dropping only the inspector's `created here` label would save little: the same capture identifies the link whose endpoints must be checked and whose wrong route can be removed. Preserve that coupling.
