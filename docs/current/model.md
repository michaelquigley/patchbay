# Presentation model

`internal/model` turns an observed snapshot into what the canvas draws. It groups ports into blocks, recognizes blocks against the remembered workspace, places new ones, applies visibility, and counts connections to hidden endpoints. It changes presentation only: nothing in the model issues a backend request, and no operation creates or destroys a link or writes a setting.

## Blocks

A block is a set of ports that share an owner, a media kind, and a direction. Grouping runs on ports, not nodes:

| owner | when | recognition class | port key | title |
| --- | --- | --- | --- | --- |
| Midi-Bridge port | the node's `media.class` is `Midi/Bridge` | `midi:<alias prefix>` | `port.alias` | the alias prefix |
| device-backed node | the node has a `device.id` (`HasDevice`), resolved or not | `node:<node.name>`, without WirePlumber's counter | `port.name` | `node.description`, `node.nick`, or `node.name` |
| client node | anything else | `app:<application.name or node.name>` | `port.alias` | the class name |

The model reads only the snapshot's typed fields, never its property maps. The alias prefix (`Port.AliasPrefix`) is the part of `port.alias` before the first colon, which on a bridge port is the ALSA client name (`Launchpad Pro 2`, `Midi Through`, `Virtual Raw MIDI 0-1`). `port.name` and `object.path` on bridge ports carry the USB path and the ALSA sequencer client number, which change on replug, so they are never used.

WirePlumber appends a counter to a device node's name when the name is already taken at creation (`alsa_input.pci-0000_0d_00.4.analog-stereo.3`). The counter depends on what was created before, so it is a runtime value. On `seven` the built-in, HDMI, S/PDIF, and webcam nodes carried `.3` and `.4` on 2026-10-02 and none on 2026-10-07. The device-backed class therefore drops one trailing `.N`, for N from 2 to 99 written without leading zeros; any other ending is kept (`v4l2_input.…-usb-0_3_1.0` keeps its `.0`). Two live nodes that differ only by the counter share a key and are presented as new, as any collision is.

A device-backed block also carries its device's hardware serial (`Device.HardwareSerial`, from `device.serial`), when the device reports one. It is a hint for association and is never part of a key.

Media is `audio`, `midi`, or `video`, from the backend's `Port.Media`; direction is `in` or `out`. A port with no observed owner, or no known media or direction, belongs to no block. So REAPER, one JACK node carrying both media in both directions, presents four blocks, and each VirMIDI client on `seven` presents a one-port block in each direction.

A block instance is identified for its lifetime by a `BlockID` built from the owning node's serial (plus the alias prefix for bridge blocks). A block whose identifying name is empty is *unkeyed*: it is drawn and can be moved within the session, but it is never matched to a record and never remembered. A port with an empty port key cannot be hidden individually. Hiding is by port key, so if two ports of one block ever shared a key, a preference set on either would apply to both. That has never occurred in any sample, and its consequence is presentation-only: it hides or shows a port, never touches a link. It is an accepted residual.

## Recognition

The *recognition key* is `(class, media, direction)`. The *record key* names a workspace record: the recognition key's text (`app:REAPER|audio|in`), or that text plus an ordinal slot (`#2`, `#3`) when a second record for the same recognition key is needed. Matching always runs on recognition keys. The ordinal is a storage slot and is never evidence about which live instance a record belongs to.

A remembered record is assigned to a live block only when all of these hold:

- exactly one live block has the recognition key;
- exactly one unassigned record answers to that key, either directly or through its `also` list;
- no other live block answers to any key of that record.

Every other case (two live candidates for one key, or one candidate and two records) assigns nothing. The blocks are presented as new and the records stay available for association. An assignment holds for as long as its block instance is observed. A second instance arriving later is new and does not displace it. When the assigned instance disappears, its record returns to the pool. A block presented as new stays new until the operator acts on it, even if the block it collided with leaves.

A snapshot from a different connection session than the assignments were made under releases every assignment before anything else happens, whether that snapshot is live or not. The model therefore never depends on having seen the disconnect. Recognition happens only on a `live` snapshot. On a snapshot that is not live (before the startup barrier, or while disconnected and reconnecting) the model assigns nothing. It releases every assignment, because serials repeat across a daemon restart, and returns the last live view marked stale. Recognition starts over when `live` returns.

Blocks that appear after the graph a connection started with are listed in the view's `Appeared`, once, in arrival order, on the `Reconcile` that first sees them; the initial graph, and every reconnect's, is not announced.

Unassigned live blocks that share a recognition key carry a display ordinal (1, 2, …) in arrival order, so the canvas can tell them apart. It is a label, not an assignment.

## Records and placement

- **Remembered block.** A block assigned to a record goes to the record's position.
- **New and unambiguous.** A block that is genuinely new and unambiguous (the only live block for its key, with no unassigned record answering to it) gets a record at once under the next free record key, so its placement is remembered.
- **New, from a remembered device.** The exception: a new device-backed block is held for association when an unassigned record of its media and direction was observed on the same hardware (same `device`) under another recognition key. It gets session placement only, like an ambiguous block, until the operator associates, moves, or hides it. A record of its own would answer to its key alongside the record the operator later associates, and the block's next return would then be ambiguous.
- **New and ambiguous.** An ambiguous block gets session placement only. The first gesture on it (move, hide, hide a port) stores it in a record under a fresh ordinal slot assigned to it. That is the operator's act, not a tie-break.
- **First layout.** An empty workspace lays the first graph out once, in two columns: outputs at x 0, inputs to the right of the widest output block, visible blocks first and ordered by title.
- **Arrivals column.** After that, new blocks are placed where the operator is looking: inside the visible canvas rectangle the canvas reports after every frame (`SetViewport`), right-aligned to its right edge with a 20-unit margin, shifted left only as far as needed to be fully visible, stacked downward from its top in arrival order (lowest port serial first). Arrivals keep stacking while the view stays put; a different visible rectangle starts a new column at its top. Before the canvas has reported a rectangle, the model uses the remembered pan and zoom at an assumed 800×500 canvas, which is the view the window opens onto. A block with a remembered record is never placed by this rule.

Existing blocks never move. Placement sizes are estimates on a 20-unit grid: height from the port count, width from the longest title or port label at 8 units a character. The canvas draws the real size.

Association binds a live block to a record that no live block holds, chosen by the operator from the view's `Absent` list; the record must have the block's media and direction. The block's recognition key is added to the record's `also` list when it differs, so the record answers to it on later returns.

`View.Candidates(block)` is what the chooser offers: the absent records of the block's media and direction (the only ones `Associate` accepts), records from the block's own hardware first, then the rest, each group in record-key order. The order is a hint; nothing is assigned by it.

A record learns its device's hardware serial the first time it is assigned to a block that has one, and keeps the first it learns.

### Profile changes

A PipeWire profile change replaces a device's nodes with new ones under new names. The Scarlett's `…multichannel-output` and `…multichannel-input` become `…pro-output-0` and `…pro-input-0`, while every port name and alias stays the same. The pro-audio blocks have new recognition keys, so no record answers to them, and they are not recognized. That is correct: the two profiles are different PipeWire objects. What bridges them is the hardware serial:

- the pro-audio blocks are held for association (above), with the gap `its name is new, but a remembered record from the same device is under another name`;
- the multichannel records are untouched, and the chooser lists them first, marked `same device`;
- associating adds the pro-audio key to the record's `also` list, so either profile's block then takes the one record, and the multichannel block still matches it by its original key;
- hidden-port preferences carry across, because port keys are port names.

### Why a block has no record

The inspector names the reason (`RecordGap`): its identifying name is empty; other live blocks share its key; more than one remembered record answers to its key; a record from the same device is under another name (the profile case); or it appeared alongside a block with the same key and stays new.

## Visibility

- **Port visibility.** A port is visible unless its block is hidden, the port is hidden by its port key, or its category class is filtered.
- **Show hidden.** While Show hidden is on, everything is visible. Turning it on or off changes nothing in the workspace and is not saved.
- **Block visibility.** A block is visible when any of its ports is.

Category classes are computed, never stored: `video` for every port of a node whose `media.class` contains `Video`, `monitor` for ports with `port.monitor`. A fresh workspace filters both.

The view draws only links whose two ports are visible. A visible port with links to ports that are not visible carries a `HiddenLinks` count, which becomes the pin-row suffix (`· 2 hidden`). A visible block counts the links carried by its own hidden ports on its title, so a connection on a hidden port always has an indication somewhere on the block.

## The view

`Reconcile(snapshot)` returns a `View`:

- the connection state, whether the view is stale, the state's reason, and the generation and session of the live snapshot the blocks came from;
- blocks ordered by `BlockID`, each with its key, record, ordinal, title, position, preference and visibility flags, ports, and hidden-link counts;
- the drawn links;
- the `Absent` records: remembered, with no live block, and offered for association.

`Refresh()` rebuilds the view after an operation, but only while the last snapshot handed to `Reconcile` was live.

While the connection is not live, `Reconcile` and `Refresh()` both return a stale view built by one function. It is a copy of the last live view's blocks and links, marked `Stale`, carrying the current `State` and its `Reason` (the backend's text for the state; empty when connecting at startup). `LiveGeneration` names the snapshot generation the blocks were built from, zero when nothing live has been seen. Nothing in a stale view is presented as current, and block operations fail until the connection is live again.

A `View` is a value for the frame it was built in and is never cached across frames. Its `State`, `Stale`, `Reason`, and `LiveGeneration` are authoritative only for that frame. The model's operations are `Move`, `SetBlockHidden`, `SetPortHidden`, `SetClassHidden`, `SetShowHidden`, `SetView`, and `Associate`.
