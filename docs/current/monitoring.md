# Monitoring

Patchbay reads PipeWire's profiler, the source pw-top reads, and shows a compact summary:
- per-driver quantum and rate;
- xrun counts over the nodes currently tracked, as totals and as new since a baseline;
- the time of the last observed increase.

Counts are observations of PipeWire's counters, labeled as such. An xrun counted on a node is not a claim that the node caused it.

## The profiler

The client loads `libpipewire-module-profiler` into its own context, as pw-top does, and binds the daemon's Profiler global once (`backend.md`). If the module cannot be loaded, or the bind fails or the global goes, monitoring is unavailable rather than zero. The daemon sends one pod per driver cycle: about 750 a second at quantum 64. The C layer decodes each profiler object and hands it to Go in one call:

- **`info`:** the driver's own xrun counter, kept at driver scope only;
- **`clock`:** the pod's clock (`nsec`), quantum (`duration`), and rate (`rate.denom`);
- **`driverBlock` and `followerBlock`:** each node's id, status, and optional xrun count.

A pod without a `clock` is ignored whole: neither lifetime guard can be applied to it, so it decides no driver's guard mode and touches no record. Pods never wake the loop or fold the graph. They update records on the loop thread, and summaries are published on the backend's own tick.

## Records

Each node has a record, keyed by its serial and held only for the connection; a disconnect drops every record. A record holds:

- **`total`:** the counter as last accepted;
- **`baseline`:** where new errors count from;
- **the clock of the last accepted pod;**
- **the time of the last increase.**

New errors are `total − baseline`, clamped at zero.

- **The first counter** a node reports, whenever it arrives, sets `total` and `baseline` together and records no increase. A late first measurement is never new errors.
- **An increase** updates `total` and records the time it was observed. That is when Patchbay saw it, not necessarily when it happened.
- **A counter that goes backwards** under the same serial rebases the record (`total` and `baseline` both take the new value), with a log line, and records no increase.
- **A follower block without a counter** makes its node *unavailable*. It is never filled from the driver's `info` counter. Whether blocks carry a counter is fixed by the daemon's version, not something that varies from one pod to the next; the counter is parsed as optional only for older servers. So a node that has had a counter is not expected to lose it, and its record does not go unavailable on one counterless block.
- **A replaced node** (a new serial) starts a fresh record, and the old one is dropped.
- **Reset** (the performance panel's `reset new` button, `ResetMetricsBaseline`) rebases every record to its current total. New becomes zero and totals are untouched; PipeWire's counters are never reset.

### Lifetime guards

Profiler blocks name nodes by protocol id, and blocks are buffered per driver before delivery. So a late pod from a departed node can carry the id its replacement now holds. Two guards keep such pods off the replacement:

1. **Clock-based.** Each node records the backend's own CLOCK_MONOTONIC reading when its registry announcement was received. A pod whose clock is earlier than that belongs to a previous holder of the id and is dropped before anything is initialized.
2. **Ordering.** Within a record, a pod whose clock is older than the last accepted one is dropped. Pods flush per driver, and a node moving between drivers can deliver an older counter after a newer one.

The clock-based guard only means something for a driver whose pod clock is CLOCK_MONOTONIC "now". It is decided once per driver, on its first pod with a clock, and never re-evaluated: the driver's clock counts as current when that pod's clock is within one second of CLOCK_MONOTONIC at arrival. The decision is logged once. Re-deciding on later pods would let a backlog of pods delayed by a stall pass for a lagging clock and switch the cutoff off for exactly the stale pods it exists to drop.

A driver whose clock is not current skips the cutoff, and its nodes' records are guarded by ordering only. gnome-shell's video driver on the desktop is one: its pod clock stood an hour behind and did not advance. Each record carries the guard of its driver for its last accepted pod, and the inspector shows it: `lifetime guard: clock-based` or `lifetime guard: ordering only (driver clock not current at first pod)`.

**Accepted residuals.**

- On an ordering-only driver, a stale buffered pod from a replaced node under the same id can seed the replacement's record.
- A driver whose first pod arrives during a stall is ordering-only for its lifetime, though its clock is current. The inspector shows it as ordering only.

## The summary

At most ten times a second, and only when it changed, the backend publishes `Snapshot.Metrics`. Its `Available` is true only while the profiler is bound; when it is false, the performance panel and the inspector say `monitoring unavailable (no profiler)` instead of driver lines or counts, since nothing was observed. Removing a node or a driver marks the summary changed, so a departed node's count leaves the performance panel on the next tick even when no pods arrive. The summary carries:

- **per-driver lines:** each driver with running followers, with its quantum, rate, and `info` counter. A driver whose last pod showed no followers, or that has sent no pod for a second (it suspended), drops off rather than keeping its last quantum.
- **total and new:** sums over tracked nodes with a counter.
- **the last increase:** the most recent increase time among them.
- **per-node records:** for the inspector.

The performance panel's section is headed `xruns (tracked nodes)`: the sums are counts over currently tracked nodes, not a session history. A replaced node's record is dropped and a rebased counter lowers the sum, both without a reset. A disconnected snapshot carries an empty summary: no metrics are shown as current while disconnected.

## Where it shows

- **The performance panel:** the xruns section, `total`, `new`, and `last increase`, with a `reset new` button; and in the graph-wide quantum section, one `observed` row per driver.
- **The canvas:** each block of a node with new xruns carries a `+N xruns` badge in its title bar (`ui.md`).
- **The inspector's block view:** an xruns section with the node's total, new, last increase, and lifetime guard, or `unavailable` when its blocks carry no counter.
