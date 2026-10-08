---
title: recalibrate canon around operator-visible consequences
state: inbox
created: 2026-10-07
tags: [documentation]
---

revise Patchbay's Terminus canon deliberately with Michael before implementing the audit's behavioral simplifications. keep strict rules for explicit routing gestures, correct request targets, native ownership, session boundaries, persisted workspace integrity, and hardware-source claims. distinguish those from diagnostic attribution and transient request feedback. require a protocol-contract or reachable-execution argument for event-ordering findings; tests that directly inject an arbitrary order are not evidence that PipeWire can deliver it. express desired outcomes without freezing incidental mechanisms, and apply accepted residuals in the individual qualities as well as the rubric.

## why

The current rubric's evidence-bound characteristic is a useful correction, but the blocking qualities still contain broader mechanism mandates:

- `serial-keyed-live-state` treats every metric attribution like a routing target and says ids are never map keys, although `Graph.byID` is the necessary protocol-to-serial index. Permit that index explicitly; require serial/session identity for retained application state. Separate the profiler's imperfect id-only observations from exact routing identity.
- `observed-not-requested` combines the sound rule that drawn links come from observation with exact provenance and request-confirmation rules. Keep their consequences distinct, allow a timeout to describe an unconfirmed request, and let capture mechanics rely on PipeWire's ordering contract.
- `native-stays-in-the-backend` says every call runs on the loop thread under its lock, while its own discrimination already qualifies this by API requirements. State the actual ownership rule, including initialization before the loop starts, teardown after it stops, and permitted calls with the loop lock held. The journal has an explicit veto of flagging pre-start profiler-module loading.
- `hardware-annotations-verified-only` still requires the monitor to be running before displaying a read and describes hardware-serial matching to a card that exposes no serial. Reconcile the statement with `docs/current/scarlett.md` and its accepted first-read and in-refresh display windows. The endpoint-number example has already been corrected; do not redo that work.
- `conservative-recognition` still points at the retired work order for identity composition, and `presentation-never-routes` points at a future spec change for restoration. Point to the current model contract and deferred scope instead of retired intent documents.

The proposed calibration is not to make every quality advisory or require a captured incident before flagging a real native-lifetime or wrong-route defect. It is to distinguish a violated supported contract from a hypothetical hostile protocol, and to price diagnostics according to their operator-visible consequence. A hypothetical case should not generate a new state machine merely because it can be written as a sequence of Graph inputs.

The canon already had uncommitted Patchbay calibration edits when this audit began. Review those as the starting point; this audit does not authorize overwriting them. The code candidates live in this same roadmap. Canon changes belong in `terminus-canon`, after Michael's decision.
