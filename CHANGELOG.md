# CHANGELOG

## Unreleased

Patchbay's first release: a desktop PipeWire patchbay for a working studio. It opens onto the live graph as blocks per application, device, and MIDI port, at the places you left them, and recognizes them when they return without ever guessing between two lookalikes. You patch audio and MIDI by pulling links, and Patchbay draws only what PipeWire confirms. You can set the graph's quantum and watch each driver's observed quantum and the xruns since a baseline you reset. On a supported Scarlett, it shows the hardware source behind each capture channel. It changes the running system only when you patch or set the quantum, and never restores routing on its own.

FEATURE: Scarlett hardware sources. On a Scarlett with a SessionMixer topology profile for its firmware, the inspector shows which hardware source each PCM capture channel is routed to (`hardware source: Analogue Input 1`). It follows routing changes made in SessionMixer and says `unavailable` with the reason whenever it cannot read the routing fresh. Patchbay only reads; it never changes the interface. Building now also needs `libasound2-dev`.

FEATURE: Identity across profile changes, replugs, and duplicate instances. A device whose profile changes (the Scarlett's multichannel to pro-audio) shows its new blocks as new, held for association. The inspector says the device has a remembered record under another name, and the chooser lists that device's records first. Associating once makes both profiles one block, with hidden ports carried across. A replugged device takes its records back; an unplugged one keeps them. Two instances of one application (two REAPERs) never inherit each other's arrangement. Device node names are recognized without the counter WirePlumber sometimes appends, so built-in devices are still recognized after it changes.

FEATURE: Quantum control and monitoring. The performance panel sets PipeWire's forced quantum (automatic or a power of two between the configured minimum and maximum). It shows the requested override, the quantum and rate each running driver is actually observed at, and the xruns counted over currently tracked nodes as totals and new since a resettable baseline, with the time of the last increase. The inspector shows each node's count, and on the canvas each block of a node with new xruns carries a badge of the count until it is reset.

FEATURE: Patching. Pull a link between two pins to connect them, and select links and press `Delete` to remove them. A link appears on the canvas once PipeWire reports it, drawn in its reported state: dimmed while it settles, in its media's color when active, red in error. A pending or failed request is listed as an event in the performance panel with its reason, never drawn as a link. Links Patchbay creates linger after it closes, and the inspector labels each one `created here` or observed. An inspector panel on the right (`I` to show or hide it) shows the selected block or link as PipeWire reports it, with hide, unhide, and association.

FEATURE: `patchbay` opens a window: a canvas of the live graph's blocks at their remembered positions, colored by media, with video and monitor filters, Show hidden (`Shift+H`), hide selection (`H`), snap selection to grid (`S`), fit (`F`), and center (`C`). A device that appears while it runs lands inside the current view and is announced as an event, and `N` centers on it. A performance panel on the left (`P` to show or hide it) shows the connection, the quantum, the xruns, and recent events; the toolbar's right end always shows the connection state, flagged when what the canvas shows is not current, and the newest event. `--sample <dir>` runs the window read-only against a capture.

FEATURE: A presentation model groups the live graph into per-owner audio and MIDI blocks and remembers their positions and visibility in `~/.config/patchbay/workspace.yaml`. A returning device or application is recognized only when the match is unambiguous; otherwise it is presented as new and offered for association. Nothing it does changes routing.

FEATURE: `patchbay dump` prints the live PipeWire graph keyed by object serial and reprints it on every change, riding through daemon restarts with a reconnect backoff. `--sample <dir>` prints a captured `pw-dump.json` through the same model instead.

CHANGE: `patchbay dump` now checks for graph changes every 50 ms.

CHANGE: Link creation uses PipeWire's guaranteed event order, with the same observed confirmation and wrong-route protection.
