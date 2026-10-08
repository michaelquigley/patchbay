# Patching

Patching is the only way Patchbay changes the running system, and it has exactly two paths: create a link on a link gesture between two pins, and destroy the selected links on `Delete`. Each posts a request to the backend. A request's outcome is observed, never assumed. The canvas draws a link once its Link global is observed, in its observed state, and never from a request:

- a link still negotiating is drawn dimmed;
- an active link is drawn in its media hue;
- an errored link is drawn in a warning red.

A pending or failed request is listed, never drawn.

## From gesture to request

1. **The gesture.** Pulling a link from one pin to another produces a dfx `LinkCreated` intent between two pins. The canvas maps the pins to port serials and hands them to the app; it posts nothing itself.
2. **Validation.** The model checks the pair against the current live graph. Both ports must be in it, the link must run from an output to an input, the media must match, and the pair must not already be linked. A refusal is reported as an event in the performance panel and on the toolbar, and nothing is posted.
3. **The request.** The app posts `CreateLink(session, out, in)`. `session` is the connection session of the view the operator acted on, since serials name objects only within one session.

`Delete` posts `DestroyLink(session, link)` for each selected link.

No request is posted in sample mode, which says so in a notice. None is posted while the view is stale (the connection is not live) either: a link gesture or `Delete` then records a `not connected` entry in the request list, failed, without reaching the backend.

Every request description, `link REAPER:out1 → Scarlett 18i20 4th Gen Multichannel:playback_AUX0`, is captured when the request is made, so the performance panel can still name a port that has since gone.

## Confirmation

Requests are handled on the backend's loop thread, against the graph of the session they name. A request against any other session fails with `the graph changed since the request was made`.

**Create.** The request's port serials must name an observed output and input, or it fails at once (`output port 1290 is not observed`). The ids are read from those serial-keyed ports and used once to ask the link factory for a link with `object.linger=true`. libpipewire then reports the global id the link proxy was bound to.

- **One link lifetime.** PipeWire delivers the creation proxy's bound id before the link becomes visible in the registry. The request captures the serial of the first Link global announced under that id, then uses only that serial for confirmation and removal. If the captured link goes before confirmation, the request fails (`the link was removed before it became active`). A later link between the same ports, even under the same reused id, is a new lifetime and is observed.
- **Wrong route.** If the captured link's endpoints are not the requested port serials, the request fails (`wrong route: link 1400 connects ports 1290 -> 1301, not 1290 -> 1300`). That link, which the bound id proves this process created, is destroyed. This happens when a port id is reused between validation and the server's processing.
- **Confirmed.** When the captured link's bound info reaches `active` or `paused`, the request is confirmed and the link is recorded as created here.
- **Failed:**
  - a proxy error (`the link factory refused it: …`);
  - an `error` link state;
  - removal of the proxy or the captured link before confirmation;
  - no captured link within two seconds of posting.

The capture order is part of PipeWire's [1.0 core contract](https://github.com/PipeWire/pipewire/blob/1.0.0/src/pipewire/core.h#L160-L170). Link-factory binds the creating client's resource during initialization, before registering the link global. libpipewire delivers the proxy's bound callback synchronously, and Patchbay applies it and registry callbacks directly on the same loop thread. Capture therefore needs neither a history of pre-bound announcements nor a serial watermark.

**Destroy.** The link must be observed, or the request fails at once. The id is read from the serial-keyed link and passed to a registry destroy. The request is confirmed when the link's removal is observed, and fails if that has not happened within two seconds. The canvas keeps drawing the link until its removal is observed.

When a request resolves, its link proxy is released; the lingering link outlives it. A release decided inside the proxy's own callback is carried out at the loop's next wake, where destroying a proxy is safe. A backend ticker wakes the loop ten times a second, so timeouts fire even when the graph is quiet.

## Provenance

`Link.CreatedHere` is true only for the one link lifetime a request of this process was confirmed on. Every other link is observed, including one another client made between the same ports while a request was pending, and every link seen by a later connection. REAPER's own JACK links are lingering links with no client, indistinguishable by their properties from Patchbay's, which is why provenance comes only from in-session evidence.

Provenance and pending requests belong to one connection. Losing a connection is one critical section under the backend's single lock: the transport is detached, the queue of requests not yet handed to the loop thread is drained, those and every pending request fail with `disconnected: <reason>` (the dead connection is never asked for anything), the created-here record is dropped, and the disconnected snapshot is published. A post takes the same lock. So it either joins the queue that the loss drains, or, after the loss, sees no connection and fails at once with `not connected`. In that case the backend appends the failure to the current snapshot's request table (kept to the most recent 16) and republishes it with its state unchanged. Under the lock, "no connection" means that snapshot already reads disconnected (or connecting, before the first connection), so a failure is never published into a snapshot that reads live. `Close` detaches and publishes a disconnected snapshot (`closed`) the same way. A request that reaches a later graph against an older session is failed by that graph's session check.

## Where outcomes are found

Request outcomes are read from `Snapshot.Requests`, which carries:

- every pending request;
- the sixteen most recent resolved ones, each with its kind, state, reason, requested endpoints or target, captured link, and posted and resolved times.

The last snapshot of a lost connection, already marked disconnected, shows the requests the disconnect failed. A new connection's graph starts with an empty table.

The inspector's request list shows every pending request with its age and every request that failed in the last fifteen seconds with its reason. The performance panel's events carry the same requests, keeping a failure for thirty seconds unless dismissed. Confirmed requests are not listed: their outcome is the drawn graph.

## Accepted residuals

- **Provenance is in-session only.** `created here` is known only for links this process confirmed in the current connection. After a reconnect, a daemon restart, or a restart of Patchbay, every link is `observed`, including the ones Patchbay made: link properties cannot tell its links from REAPER's.
- **A request whose link never settles stays pending.** The two-second timeout covers only a create whose link never appeared. A captured link that stays below `active` or `paused` without erroring or being removed keeps its request pending, with its age shown, until it settles, goes, or the connection is lost. Failing it on a timer would claim to know the link failed when PipeWire has not said so.
