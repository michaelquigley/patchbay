# User interface

`internal/ui` is the dfx application that `patchbay` opens with no subcommand. It has a toolbar along the top, and beneath it the canvas between two collapsible panels: the performance panel on its left and the inspector on its right. Each frame it reads and reconciles the current snapshot into a view for that frame only (`internal/model`). It then declares the view on a dfx `NodeCanvas` and applies the canvas's intents back to the model. It draws observed state only.

Patching is built. A link gesture between two pins creates a link, and `Delete` removes the selected links (`patching.md`). The quantum control sets PipeWire's forced quantum (`quantum.md`). All three are requests whose outcome is observed, never assumed. Nothing else in the window changes the running system.

```
patchbay                                # the live daemon, the workspace at ~/.config/patchbay/workspace.yaml
patchbay --sample <dir>                 # a captured sample, read-only
patchbay --workspace <file>             # another workspace file
```

## Canvas

Every visible block is declared as a node at its workspace position. Its title bar shows:

- a glyph for what the block presents: a connector for a device, a window for an application, a piano for an ALSA client on the MIDI bridge;
- the block's title;
- the ordinal that tells same-key blocks apart (`#2`);
- its media and direction;
- `· hidden` when Show hidden is revealing a hidden block;
- `· N hidden`, dimmed, when the block's own hidden ports carry N connections;
- an xrun badge, `+N xruns` in the warning hue, when the block's owning node has N new xruns since the baseline (`monitoring.md`). Every block of that node carries it. It goes when new returns to zero, as after `reset new`. It is absent while monitoring is unavailable, and on a stale view, whose counts are not current.

Its visible ports are pin rows, inputs on the left and outputs right, declared at every zoom detent so links keep their anchors. A pin row's label gains ` (hidden)` when Show hidden is revealing it, and a dimmed `· N hidden` when it has N connections to ports that are not drawn. Title, pin text, suffixes, and the xrun badge are drawn through the canvas's `Label`, so they are detent-safe. A suffix is a second label on the same row, in the theme's disabled text color; the badge is a further label after it, in the warning hue.

### Color

Each block takes its media's hue as its dfx accent: its title band and pins, and its border when selected (thicker, in a highlight of the hue). Each link takes the hue of its output's media, and its observed state shows in its color. Every observed link is drawn, whatever its state, because hiding one would imply a route PipeWire says exists is absent:

- `active` links draw in their media hue;
- `init`, `negotiating`, `allocating`, and `paused` links in the same hue dimmed to 0.35 opacity;
- `error` links in a warning red `(0.86, 0.30, 0.22)`.

The inspector spells the state out. The hues live in `internal/ui/palette.go`:

| media | hue |
| --- | --- |
| audio | steel blue `(0.24, 0.52, 0.72)` |
| midi | amber `(0.78, 0.52, 0.18)` |
| video | violet `(0.55, 0.38, 0.76)` |
| unknown | neutral grey `(0.42, 0.44, 0.48)` |

Active links draw at 0.9 opacity. On a stale view, accents are greyed along with the rest of the canvas, and no links are drawn.

The canvas declares the model's drawn links, which are observed links between two visible ports. When the view is stale (the connection is not live), the canvas draws the last live graph desaturated and locked against dragging, and declares no links: a link is a claim about the present.

### IDs

One `ID` type spans the canvas's id space:

- **Blocks:** a block is named by its record key, or by `~` plus its live instance while it has no record.
- **Pins:** a pin is named by its block and port key, plus the port serial when the key is empty or repeated within the block.
- **Links:** a link is named by its serial.

The formatted form joins block and port with a unit separator, so no key can make two ids format alike.

Selection and stacking order are held by the model's `BlockID`, not by canvas id. A block that gains a record (its first move as a colliding block) changes canvas id but keeps its selection and stacking. Both belong to one connection session: a view from a new session clears them, because serials (and with them `BlockID`s) repeat across a daemon restart. A stale view keeps the old selection visible but inert.

### Intents

| intent | effect |
| --- | --- |
| `NodesMoved` | one `MoveBlocks` call for the whole gesture: one change, one save |
| `NodeRaised` | the block moves to the front of the stacking order (session only; not stored) |
| `SelectionChanged` | replaces the selected blocks and links |
| `LinkCreated` | the pins' port serials go to the app, which validates and posts a create request (`patching.md`); the canvas draws the link only once it is observed |

### Navigation

Pan and zoom are restored from the workspace at the first frame, with no fit and no layout. They are saved whenever a navigation changes them. Middle-drag pans and ctrl+wheel zooms, as dfx defines.

## Keys

| key | action |
| --- | --- |
| `H` | hide the selected blocks |
| `S` | snap the selected blocks to the canvas grid, as one move |
| `Shift+H` | toggle Show hidden |
| `N` | center on the newest arrivals |
| `F` | zoom to fit |
| `C` | center on the selection |
| `Delete` | remove the selected links (a request; see `patching.md`) |
| `I` | show or hide the inspector |
| `P` | show or hide the performance panel |

Ports are hidden individually from the inspector.

## Inspector

An `HCollapse` anchored to the right of the canvas shows the selection as observed. It opens 380 wide, and its resize handle is on its left edge: drag left to widen it, right to narrow it. The panel is drawn with the full available size, which is what bounds its resize. `I` toggles it, and when the panel has somehow become narrower than its collapsed width, `I` restores it to 380 and expands it instead.

Its body has its own padding and scrolls. Every view is laid out the same way:

- **A title line:** the object's name, led by its class glyph in its media hue. The glyph carries the color; the name stays in the text color.
- **Sections:** each opens 12px below the one before, under a full-width collapsing header, a filled band with a lowercase name and a fold arrow. Every section is open by default except properties. imgui keeps each header's open state by its name, so a section folded away stays folded as the selection changes.
- **Label/value tables:** labels sit in a fixed 96px column, and values wrap in the rest. Paths, names, keys, and property values are in the monospace font, so long node names never overflow or clip.

The views:

- **One block:**
  - **identity:** recognition key, record key (or, when it has none, the reason: its identifying name is empty, other live blocks share its key, several remembered records answer to it, a record from the same device is under another name after a profile change, or it appeared ambiguous and stays new), and the owning node's name, serial, id, and device serial.
  - **state:** the connection state, the node's state, and whether the block is hidden.
  - **xruns:** the node's monitoring record: its total, new, last increase, and lifetime guard (`monitoring.md`), or why there is none, including `monitoring unavailable (no profiler)`.
  - **ports:** visible ports, each with its label, serial, id, and class, and a `hide` button, or `cannot be hidden: it has no port key`. A port on a device with a Scarlett card adds a line beneath: `hardware source: Analogue Input 1` on a valid capture channel, or `hardware source: unavailable (<reason>)`, dimmed (`scarlett.md`). Other ports, and every port in sample mode, have no such line.
  - **hidden ports:** the same, each with `unhide` inline.
  - **properties:** closed by default; opened, a `filter keys` box that narrows keys by substring, above the node's properties.
  - **actions:** a button row (`hide block` or `unhide block`), then the association combo. It offers the remembered records with no live block and the same media and direction, those observed on the block's own hardware first and marked `· same device` (`model.md`). The order is a hint; nothing is chosen for the operator.
- **One link:**
  - **identity:** from, to, serial, id.
  - **state:** the link's state, its error, and its provenance: `created here`, or observed.
  - **properties:** filterable and closed by default, as for a block.
- **Several objects:** a count, and the keys that act on them.
- **Nothing:** a one-line prompt, then the raw `default` metadata table of key, subject id, type, and value. Subject 0 reads `0 (global)`; every other subject is its reported protocol id. Entries are not joined to current nodes or shown under a selected node.

Beneath the selection, the requests section shows pending and recently failed requests, read from the current snapshot; the performance panel's events carry the same requests.

The inspector resolves the selection's serials only against the snapshot the drawn view came from: the current snapshot for a live view; for a stale view, the last snapshot a live view was built from, if its session and generation match the view's. When neither matches, it shows `details unavailable: the graph has changed` rather than looking a serial up in another graph, where a reconnect may have reused it. While the view is stale the inspector says that what it shows is not current. Its actions are presentation operations only: nothing in the inspector creates or destroys a link.

## Toolbar

Two checkboxes control the category filters: `video` and `monitor`. Both are unchecked (filtered) in a fresh workspace, and both are saved. A third checkbox, `show hidden (shift+h)`, reveals everything while it is on and is never saved.

## Performance panel

An `HCollapse` anchored to the left of the canvas shows the connection and performance data. It opens 320 wide, with its resize handle on its right edge. Like the inspector, it is drawn with the full available size, which bounds its resize. `P` toggles it, and restores it to 320 when it has become narrower than its collapsed width.

The panel has four sections under collapsing headers, as in the inspector, each a label/value table in the inspector's style. Labels are in the dim text color, values in the normal color, and numbers and names in monospace. All sections share one label column, as wide as the widest label.

- **connection:**
  - `state`: `live`, `connecting`, or `disconnected` with the backend's reason;
  - `session`: the connection session;
  - `graph`, while the view is stale: the generation the drawn graph came from and that it is not current, or that none has been observed yet;
  - `unresolved`: the snapshot's unresolved references, while live and nonzero;
  - `view`: `showing hidden`, while Show hidden is on.

  In sample mode, `state` says `sample mode`, `sample` names the capture, and `access` says patching and quantum controls are disabled.
- **graph-wide quantum:** while live, the quantum control (`quantum.md`), with the scope sentence (`quantum.md`) as its tooltip. Then:
  - `requested`: the override as the settings metadata shows it, or unknown;
  - `observed`: one row per driver with running followers, `'name' quantum @ rate Hz, cycle ms`, or `monitoring unavailable (no profiler)` when the profiler is not bound.

  While not live the section says `not connected` and has no control; in sample mode it says nothing is live.
- **xruns (tracked nodes):** while live and the profiler is bound, `total` and `new` over the currently tracked nodes and `last increase`, with a `reset new` button. Otherwise one row: `monitoring unavailable (no profiler)`, `not connected`, or, in sample mode, that nothing is live.
- **events:** every current event, newest first, each with its age and a `dismiss` button, or `none recent`.

### Events

Events are:

- pending requests, `pending: 'link …'`, with their age since posting;
- requests that failed, with their reason;
- gestures refused before posting (`not connected`);
- arrival batches (below);
- notices: a refused link, or a gesture in sample mode.

Pending requests stay until they resolve. Everything else falls off thirty seconds after it happened. A dismissed event stays dismissed, but a request that changes state, pending to failed, is a new event and shows again. Confirmed requests are not events: their outcome is the drawn graph.

### Toolbar summary

Collapsing the performance panel must not hide what needs attention, so the toolbar's right end always carries:

- **the connection state:** in the warning color with `· not current` while the canvas shows a graph that is not current, or `· no graph yet`; `sample mode (read-only)` in sample mode;
- **the newest event:** clipped to 64 characters, with its age, a `dismiss` button, and, when there are more, a `+N` button that opens the performance panel.

## Arrivals

A block that appears after the graph the connection started with is placed inside the current view (see `model.md`) and announced. Each batch, the blocks that appeared in one frame, is an event: `arrived: 'REAPER · audio in', …` with their titles single-quoted, and `· N to show`. `N` centers the view on the newest batch. Dismissing the event hides it without forgetting the arrivals, so `N` still works. The list is session-scoped: it holds at most 20 entries, is cleared by a new connection session, and is never stored in the workspace. The canvas reports its visible rectangle to the model after every frame, computed with `CanvasFromScreen` on the canvas rect.

## Sample mode

`--sample <dir>` loads the capture's `pw-dump.json` as a fixed live snapshot and runs the whole window against it. Patching is disabled: a link gesture or `Delete` posts nothing and says so. By default it uses `sample-workspace.yaml` beside the live workspace, so arranging a capture never writes its records into the workspace the live graph uses. `--workspace` overrides either default.

## Lifecycle

dfx components are constructed before the imgui context exists. The canvas therefore derives its normal and desaturated styles from the theme, and restores the remembered view, at its first draw. On shutdown the canvas is destroyed, the model writes any pending workspace change, and the backend connection closes. An interrupt (`SIGINT`, `SIGTERM`) closes the window through the same path.
