# Patchbay

## Purpose

Patchbay is a desktop PipeWire patchbay built with Go and dfx. It is a personal studio tool intended for open-source release, not a product whose design is organized around marketing or universal Linux audio coverage.

The immediate work is manual patching: connecting MIDI devices to REAPER and connecting particular audio inputs and outputs. Existing tools expose this graph, but their presentation, clutter, and repeated layout work get in the way. Patchbay provides a stable workspace showing a changing graph. Arranging the patchbay should not be a prerequisite to using it each time.

A second purpose is comprehension. PipeWire's running graph, WirePlumber's session policy, manual patches, and configurable hardware routing contribute different parts of the system's behavior. Patchbay should help make those relationships understandable without claiming knowledge it cannot establish. It will also become an instrument for exploring the local PipeWire/WirePlumber setup in real-world use; that exploration will inform later configuration features.

## Status and scope

This is the v1 design developed with Michael. Settled decisions, provisional scope, and investigation questions are distinguished below. It is an intent document, not a claim about implemented behavior or a work order.

### Settled v1 scope

- Manual audio and MIDI patching against the live PipeWire graph.
- Separately positionable application/device blocks for audio inputs, audio outputs, MIDI inputs, and MIDI outputs.
- Remembered positions and visibility in one workspace.
- Live reflection of external graph changes and non-destructive startup.
- Individual block/port hiding, broad category filters, and temporary revelation of hidden objects.
- Conservative recognition of returning endpoints, with a way to associate ambiguous endpoints with remembered blocks.
- Live graph quantum control and observation of the resulting running state.
- Compact performance/error monitoring.
- A read-only inspector, apart from the explicitly provided patching and quantum controls.

### Provisional v1 scope

Lightweight Scarlett hardware-routing annotations belong in v1 only if existing topology support makes them a small addition. The desired level is simply knowing that a PipeWire-facing endpoint such as `AUX3` is routed to “Mic 1” in the interface. This is an illustrative relationship, not an already verified channel mapping.

If obtaining these annotations requires a substantial hardware integration effort, bring the scope decision back to Michael. Do not silently expand the build or silently treat this conditional feature as a mandatory hardware subsystem.

### Later direction

Persistent routing and broader configuration controls remain follow-on possibilities with their mechanisms undecided. The expanded hardware graph is explicitly v3; this does not establish a v2 feature list.

## Working surface

### Presentation blocks

A visible block is a useful presentation of endpoints, not necessarily one PipeWire node. Group related endpoints by application/device, media kind, and direction. Only show blocks with relevant ports.

For example, REAPER can have independently positioned MIDI-input, MIDI-output, audio-input, and audio-output blocks. Shared naming and presentation should make their relationship understandable without requiring an enclosing container that moves them together. The inspector retains access to underlying PipeWire object identities and boundaries.

Additional channels update the existing block rather than create a replacement identity or move its anchor. Exact grouping and naming must be grounded in actual REAPER and device properties.

Grouping runs on ports, not nodes. Hardware MIDI endpoints arrive as ports on a single bridge node, distinguishable only by port properties such as the alias, while a JACK client such as REAPER owns one node whose ports carry both MIDI and audio in both directions. A block is therefore a set of ports that share an owner, a media kind, and a direction, where the owner comes from the node for ordinary clients and devices and from the port alias for the bridge; the backend model must carry both routes to an owner.

### Stable workspace

The application owns block positions and presentation state. Returning endpoints recover their remembered arrangement. Disappearing endpoints do not trigger rearrangement; their workspace records survive their absence. New endpoints do not displace existing blocks.

Pan and zoom are restored with the workspace. Opening the application is not an implicit zoom-to-fit or auto-layout command. Any automatic arrangement offered is explicitly requested, not a response to graph churn. Placement of genuinely new objects remains a presentation detail to resolve during development; it must preserve existing positions.

V1 has one remembered workspace, not named layouts or presets.

### Identity

PipeWire runtime IDs identify current instances, not enduring workspace identities. Matching uses identifying properties appropriate to each endpoint class, not runtime IDs or display labels alone.

The exact strategy requires real graph samples across application restarts, device reconnects, profile changes, and multiple application instances. Do not claim universal matching from a heuristic validated only on one case.

Where identity is ambiguous, do not silently inherit an unrelated remembered identity. Present the endpoint as new and provide a way to associate it with a remembered block. In v1 this association affects presentation only, never automatic routing.

### Filtering

Users can hide individual blocks or ports and broad categories such as video and monitor ports. Returning endpoints recover visibility preferences. Genuinely new endpoints appear unless covered by a category filter.

A temporary Show hidden action reveals hidden objects without clearing preferences or rearranging the workspace. Visible blocks indicate connections to hidden endpoints, so a curated view does not falsely imply the absence of a route.

Hiding changes presentation only. It never disconnects ports, changes hardware routing, or removes an object from diagnostics.

In v1 these actions, along with link removal and association of an ambiguous endpoint with a remembered block, are triggered from the inspector and keyboard actions against the current canvas selection. Canvas context menus are not part of v1; dfx reserves the right button and defers menus until a client needs them. If the inspector route proves awkward in use, adding a reusable right-click intent to dfx is the recorded revisit.

The exact visual treatment of hidden connections remains open. The current dfx canvas skips links whose endpoint pins are not declared, so the application must deliberately provide the hidden-connection indication rather than rely on omitted links.

## Live graph and manual patching

Startup observes the current graph and restores presentation without changing routing. External changes are reflected while the application runs. A stale or disconnected backend must not be presented as confirmed live state. If PipeWire goes away, Patchbay shows the disconnected state and reconnects with backoff rather than exiting; on reconnect the graph is treated as new and reconciled against the workspace like any other return.

V1 supports patch now only: explicitly create or remove a live connection. Closing Patchbay does not intentionally remove the connections it made. Links created with PipeWire's `object.linger` behavior can outlive the patchbay client, but this is not a promise that they survive endpoint disappearance or a PipeWire restart.

There is no automatic connection restoration, continuous enforcement, or startup application of a saved routing arrangement in v1. The future ownership choice between application-managed restoration and WirePlumber policy is undecided.

Canvas gestures express requests. The backend validates and submits them; the displayed confirmed graph follows observed outcomes. An accepted local gesture or a non-null native proxy is not sufficient evidence that a link became active. Failures should be visible without inventing successful graph state.

## Quantum control

V1 provides Automatic and an explicit quantum frame count. It displays observed running quantum and sample rate separately from the requested override. The observed value is a per-driver fact: PipeWire runs more than one driver at once, and the interface is not always the driver, so the display names each driver that has running followers rather than claiming one graph-wide number.

The control affects PipeWire graph processing, not an isolated private REAPER buffer. Its UI must explain that scope. Cycle duration can be shown in milliseconds, but it is not total input-to-output latency.

Quantum changes occur only on explicit action. Patchbay does not reapply a remembered override at startup. Closing Patchbay leaves the runtime setting unchanged.

Automatic releases the forced-quantum override; it does not restore a promised previous value. REAPER and other clients may continue to influence negotiation. PipeWire exposes this through settings metadata, including `clock.force-quantum`, with zero releasing the override.

Changing quantum during processing may cause audible interruption. Runtime support exists, but REAPER/plugin behavior and interaction with Michael's `pw-jack -p<quantum>` launch setting must be tested before claiming seamless switching. The first probe on both studio machines established that the global forced quantum overrides REAPER's per-node forcing while REAPER runs, that releasing it returns the driver to REAPER's forced value rather than the system default, and that follower streams can xrun across the switch. Whether the switch is audible in REAPER is still unrecorded.

## Performance monitoring

The v1 surface is compact:

- Observed rate and quantum, with cycle duration.
- Available xrun/error counters, distinguishing existing totals from new errors observed since Patchbay opened.
- Time of the most recently observed counter increase, so ongoing trouble is distinguishable from old accumulated errors.
- A resettable display baseline, without resetting underlying system counters.

PipeWire's profiler interface, the source pw-top reads, is the expected source of per-cycle timing and xrun counts; it reports every cycle, so aggregation happens in the backend and the display receives summaries. Metric availability, scope, and collection overhead still require investigation. Labels must describe the underlying measurements rather than call every error a dropped audio buffer or imply that a reporting node caused the problem. Observation time is not necessarily the exact occurrence time. Counter resets, object replacement, and reconnects must not appear as negative error counts or fabricated continuity.

Detailed per-node timing tables and historical charts are deferred. A particular processing-load metric is not a settled v1 commitment.

## Inspector and configuration comprehension

The inspector exposes underlying PipeWire objects and properties, relevant device/profile and routing information where available, and lightweight Scarlett annotations if feasible.

It distinguishes connections known to have been created here from connections merely observed. That distinction must remain supported by evidence; a previously remembered endpoint pair is not proof that a newly appearing link was created by Patchbay.

PipeWire provides the running graph and engine settings. WirePlumber supplies session policy, including decisions about devices, defaults, targeting, and restoration. Applications may also express preferences. These are contributors to one running system, not three independent graphs with universally attributable cables.

The inspector must distinguish observed facts from unknown causes. “Connection appeared” is valid without attribution. “WirePlumber created this because of rule X” requires additional evidence that may not be available. V1 does not promise complete provenance or a general policy/configuration editor.

Future controls should emerge from using Patchbay to investigate real scenarios: device profiles and channel exposure, automatic routing, buffering and clock behavior, or virtual endpoints. These are exploration topics, not promised features.

## Supported hardware routing annotations

All interfaces exposed by PipeWire remain usable without a hardware-specific integration. Class compliance and internal routing are not mutually exclusive; additional capability determines whether hardware detail is available.

For supported Scarlett configurations, provisional v1 annotations expose the current hardware source behind an endpoint. Hardware routing remains read-only in Patchbay; SessionMixer remains the cue-mix/hardware control surface.

Require an established correspondence between PipeWire channels and Scarlett PCM/topology endpoints for the relevant device/profile combination. Do not infer it from plausible names or ordering. The correspondence is established from the kernel driver's own indexing: PipeWire's `object.path` carries each port's channel index on the ALSA capture PCM, and the scarlett2 mixer driver names its routing controls by one-based channel on that same PCM, so `capture_AUXn` is PCM capture `n+1` by definition rather than by resemblance. That basis is documented with the annotation; a signal test is optional confirmation, not a prerequisite (decided 2026-10-07, replacing an earlier per-channel signal requirement that would have cost forty-four routings for no additional knowledge). Start with Michael's configurations, not an unverified promise covering every model and firmware: the 18i20 Gen 4 on studio A (`eleven`) and the 16i16 Gen 4 on studio B (`seven`), both of which already have SessionMixer profiles.

The correspondence applies only to nodes whose ports carry an ALSA PCM path (`alsa:pcm:…`), which both the multichannel and pro-audio profiles do; a port without one is not annotated. If the mapping or current hardware state is unavailable, omit the annotation or mark it unavailable. The endpoint remains a normal, fully usable PipeWire endpoint. Do not display a guessed route as fact or stale hardware state as current.

Reuse or extract SessionMixer's topology knowledge rather than maintain a second set of device definitions. The extraction mechanism is a planning question and must respect the conditional scope of these annotations.

An expanded hardware graph is v3. It may eventually explain continuous paths through PCM channels, internal mixers, physical I/O, and direct monitoring, but v1 does not include that graph, hardware editing, or a new mixer interface.

## Scenarios

### Connect a MIDI controller to REAPER

Patchbay opens without changing routing. The controller appears in its remembered position. REAPER's MIDI-input block appears in its remembered position when REAPER launches. The user patches a connection and sees its confirmed result. Closing Patchbay leaves the live connection in place.

If REAPER restarts, its presentation returns but Patchbay does not recreate the connection. Workspace persistence and routing persistence are different promises.

### Work without graph clutter

The user hides video and monitor ports and arranges relevant audio/MIDI blocks. Subsequent graph changes do not clear those preferences or move the blocks. A visible block with a connection to a hidden endpoint retains an indication of that connection. Show hidden reveals the underlying objects without resetting the working view.

### Change processing quantum

While REAPER is running, the user explicitly requests a quantum. Patchbay shows the request and the observed result, with rate, cycle duration, and new errors. Selecting Automatic releases the override and displays the resulting negotiated behavior rather than claiming a return to an earlier value.

### Understand a Scarlett channel

For a verified supported configuration, the user inspects a PipeWire-facing Scarlett endpoint and sees its current internal source. Changes made through SessionMixer are reflected rather than requiring the user to remember a second routing diagram. If the integration cannot establish the mapping or current state, it says so without affecting manual patching.

## Technical starting points

- New repository: `/home/michael/Repos/q/products/patchbay`.
- UI library: `/home/michael/Repos/q/products/dfx`.
- PipeWire proof-of-concept: `/home/michael/Repos/q/research/pipewire`.
- Graph samples: `tools/capture/` holds operator-run capture scripts; `samples/` holds their output from the studio machines and the development desktop. Samples are the evidence the identity, quantum, monitoring, and Scarlett work is written against, and they double as headless test fixtures.
- Scarlett topology and existing control surface: `/home/michael/Repos/q/products/sessionmixer`.

The graph widget was referred to conversationally as `dfx.NodeGraph`; its current API is `dfx.NodeCanvas`. It already separates app-owned graph declarations from canvas-owned view/gesture state and exposes persistable pan/zoom. Reusable capabilities may be added to dfx where actual Patchbay interactions require them; PipeWire-specific behavior stays in Patchbay.

Build the application backend as `internal/pipewire`, using the research implementation as reference rather than treating its API as a compatibility contract. The proof-of-concept provides registry enumeration and link creation/destruction, but not the continuous observation, reliable operation confirmation, monitoring, or policy inspection needed here. Its README differs from its code; planning should ground itself in the implementation.

## Boundary decisions

This is the design's seam census. Calls reflect the discussion; unspecified mechanisms remain planning questions rather than implied requirements.

| Boundary | Call and reason | Revisit condition |
| --- | --- | --- |
| Live PipeWire state / remembered workspace | Separate. Runtime instances change; positions and visibility must survive. Workspace restoration must not perform routing restoration. | Persistent routing is designed explicitly in later work. |
| PipeWire integration / application model | Separate through `internal/pipewire`. Native protocol, object observation, control, and metrics belong in the backend; presentation grouping, recognition, and workspace behavior belong to the application. Native transport details should not become workspace identity. | A demonstrated external consumer justifies extracting a public library. |
| Application / canvas | App owns graph truth, layout, filtering, and semantic validation; dfx owns reusable drawing and interaction. Canvas intents are not confirmed PipeWire changes. | A reusable interaction need warrants extending dfx. |
| Generic patchbay / hardware integration | Additive. Ordinary PipeWire behavior never requires a Scarlett integration. Verified hardware knowledge enriches the view without defining the baseline. | Further supported hardware establishes reusable needs. |
| Hardware annotations / hardware control | Read-only for the provisional v1 addition. Reuse topology knowledge; leave control in SessionMixer. | Expanded hardware work is taken up for v3 or Michael explicitly changes scope. |
| Observations / causal explanation | Separate. Record what is known and leave provenance unknown when evidence is absent. | Policy/configuration investigation provides trustworthy attribution. |

Detailed backend error/lifecycle contracts and any extraction mechanism should be made explicit during planning; this conversation did not settle their implementation.

## Investigation before implementation commitments

1. Capture actual REAPER and hardware objects across restarts, reconnects, profile changes, and multiple instances to establish identity/grouping behavior. Baseline and restart captures from both studio machines are in `samples/`; reconnect, profile change, and multiple instances remain.
2. Test runtime quantum changes with Michael's REAPER launch method and representative plugins, including release to Automatic. The runtime behavior is captured in `samples/`; the audible result and plugin behavior remain.
3. Establish available performance counters, their scope and lifetime, and collection overhead.
4. Verify Scarlett-to-PipeWire channel mappings for Michael's configurations and assess whether live annotations remain a small addition.

The development machine has neither REAPER nor a Scarlett, so captures are operator-run on the studio machines and returned as samples. Studio B is the default test bed; studio A supplies the REAPER and quantum scenarios that touch the real working setup. The quantum test is the one capture step that changes runtime state; it is run with REAPER idle and never mid-session.

These investigations ground the intended behavior. If they change feasibility or materially expand scope, return the decision to Michael instead of silently substituting a weaker promise or a larger build.

## Deferred (and Why)

- **Automatic reconnection and continuous routing enforcement:** v1 is patch now only. Persistence behavior and ownership have not been selected.
- **Expanded hardware graph:** explicitly v3. Lightweight routing annotations address the immediate comprehension need without building a hardware editor or complete signal-flow surface.
- **Hardware routing/mixer editing:** not needed for the read-only v1 insight; SessionMixer already provides hardware controls.
- **General PipeWire/WirePlumber configuration editing and complete causal attribution:** the useful controls and trustworthy evidence should emerge from real-world exploration rather than a speculative settings dashboard.
- **Detailed per-node performance tables and historical charts:** a compact observed-state/error display is enough for the first cut.
- **Multiple named workspaces or presets:** one remembered working view addresses the immediate layout problem.
- **Public general-purpose PipeWire library:** the proof-of-concept is not an API contract; an internal backend allows the application to establish its real needs first.

Display aliases, an unavailable-device view, stereo presentation shortcuts, and a readable activity history were discussed as useful possibilities. Their exact form and release placement were not settled; they must not be silently promoted into v1 requirements.

## References

- dfx `docs/current/node-canvas.md`: current canvas ownership and interaction model.
- SessionMixer `AGENTS.md` and topology implementation: hardware/session abstractions and device profiles.
- [PipeWire runtime settings](https://docs.pipewire.org/page_man_pipewire_1.html): runtime metadata settings including `clock.force-quantum`.
