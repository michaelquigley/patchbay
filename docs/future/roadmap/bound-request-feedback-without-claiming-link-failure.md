---
title: bound request feedback without claiming link failure
state: evaluating
created: 2026-10-07
tags: [enhancement]
---

give every create request a bounded confirmation window, including one whose captured link remains negotiating indefinitely. on expiry, report that confirmation was not observed in time, release the request proxy through the existing deferred-release path, and keep rendering the observed link and its actual state. use a timeout outcome or equally explicit wording that does not claim the link is absent or errored. settle the timeout/provenance behavior with Michael, then update the request contract, tests, and canon together.

## why

`internal/pipewire/requests.go:315` expires a create only while `r.Link == 0`. Once a link has been captured, it can keep the request and its proxy pending indefinitely. `docs/current/patching.md` justifies that exception because a timer cannot prove the link failed. That premise is true, but request confirmation and link state are already separate: a timeout can truthfully say the observation window ended while the graph continues showing the link. The uncaptured-create and destroy paths already use bounded observation windows.

This mainly simplifies the contract and resource lifetime, not a large quantity of code. A link becoming active after timeout would remain observed without `created here` under the existing provenance rule; accept that explicitly rather than inventing a second late-confirmation tracker. Resolving a request must not destroy a correctly routed lingering link.
