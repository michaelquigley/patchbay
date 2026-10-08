# Deferred

What is still ahead of Patchbay after v1, carried forward from the v1 spec and work order when they were retired (2026-10-07; git history keeps both). Each item names the condition under which it should be revisited. None of it is a v1 requirement, and none of it should be promoted silently: an item becomes work when it is drawn into a roadmap card or a spec.

## Routing

- **Routing restoration and continuous enforcement.** v1 patches now only: nothing reconnects a remembered pair, enforces a route, or applies routing at startup. The open question is ownership, whether Patchbay restores routes itself or leaves it to WirePlumber policy. Two agents enforcing routes would fight, and a silent restore would make a comprehension tool into a policy engine. *Revisit* when re-patching after a session restart becomes a real cost, and settle ownership in a spec before any code.
- **Right-click context menus on the canvas.** v1's affordances are the inspector and keyboard actions on the selection; dfx has no context menu. *Revisit* when the inspector route proves awkward in use.

## Quantum

- **Per-driver forced quantum as a configuration control.** v1 sets only the global `clock.force-quantum`, though the observed quantum is per driver and clients can force their own (`node.force-quantum`). A per-driver or per-node control would be configuration rather than a one-off action, and touches WirePlumber's territory. *Revisit* when the global override proves too coarse, for example when one interface should run small while another runs large.

## Hardware

- **Scarlett topology in a shared module.** `internal/scarlett` imports SessionMixer's `topology` and `scarlettctl` directly, with workarounds recorded in `docs/current/scarlett.md`. *Revisit* when a second consumer of the topology appears; until then, fixes belong in those repositories' roadmaps.
- **Hardware sources on pin labels.** v1 shows the hardware source in the inspector only. *Revisit* after the inspector annotation has been lived with and the operator wants it at a glance on the canvas.
- **Expanded hardware graph.** Continuous paths through PCM channels, internal mixers, physical I/O, and direct monitoring. *Revisit* as its own design (the spec called it v3), not as an extension of the annotations.
- **Hardware routing or mixer editing.** SessionMixer is the control surface; Patchbay only reads. *Revisit* only if SessionMixer stops serving.

## Workspace and canvas

- **Persisted stacking order.** `NodeRaised` order is session-only, held by live block ids; raised blocks fall back to view order on restart. A persisted order would be keyed by record. *Revisit* when overlapping blocks are arranged deliberately and losing their order on restart annoys.
- **Workspace migrations.** The file carries a version field and refuses any other version; no migration exists. *Revisit* at the first change to the format that an old file cannot be read through.
- **Multiple named workspaces or presets.** One remembered view solved the immediate layout problem. *Revisit* when two distinct working arrangements are wanted on one machine.

## Configuration and monitoring

- **General PipeWire and WirePlumber configuration editing, and complete causal attribution of xruns.** Useful controls and trustworthy evidence should come from use, not a speculative settings dashboard. *Revisit* item by item, as a concrete need shows up.
- **Per-node performance tables and history charts.** The compact observed counts were enough for v1. *Revisit* when diagnosing a recurring xrun pattern needs more than totals, new, and the last increase.
- **A public PipeWire library.** The backend is internal on purpose, so the application could establish its real needs first. *Revisit* when another program wants the same backend.

## Discussed, not settled

These came up in v1 design as useful. Their form and release placement were never decided:

- **Display aliases:** operator-chosen names for blocks or ports, distinct from PipeWire's. *Revisit* when PipeWire's names get in the way of reading the canvas.
- **An unavailable-device view:** remembered records whose devices are absent, shown deliberately rather than only in the association chooser. *Revisit* when finding where an absent device would go matters.
- **Stereo presentation shortcuts:** treating a channel pair as one row or one gesture. *Revisit* when patching stereo pairs channel by channel becomes the common chore.
- **A readable activity history:** what changed in the graph and when, beyond the thirty-second events. *Revisit* when "what just happened" is asked after the events have fallen off.
