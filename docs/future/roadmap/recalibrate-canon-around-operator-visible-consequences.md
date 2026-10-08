---
title: recalibrate canon around operator-visible consequences
state: evaluating
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

## execution

Michael approved the [canon patch](../patchbay-canon-calibration.patch), and it was applied on 2026-10-07 to `terminus-canon`'s working tree, preserving the earlier uncommitted calibration. The patch is the reviewed proposal retained here; the applied rules now live in `terminus-canon/projects/patchbay/`. Do not apply it again.

The applied calibration makes four changes: permit the necessary protocol-id index and contract-backed event ordering; make snapshot notifications optional; separate observed graph state from request feedback while preserving provenance checks; and reconcile native ownership and Scarlett validity with the accepted lifecycle behavior. It also replaces retired document references. Quality ids, territories, and blocking flags stay the same.

Stage 1, canon calibration, is committed in `terminus-canon`. Stage 2, event-stream deletion, is implemented; tests and vet passed, and working-tree review `dbd8b9ab8e04` returned no findings on 2026-10-08. Stage 2 is committed. Stage 3, link-capture simplification, is implemented and validated on PipeWire 1.0.5 and 1.6.2. Working-tree review `74d3e058710a` returned no blocking findings; Michael vetoed its sole changelog advisory. Stage 3 awaits his commit. Metadata presentation, profiler attribution, and captured-create timeouts each need a product decision before implementation. Startup readiness remains a spike. No priority sidecar is changed by this sequence.
