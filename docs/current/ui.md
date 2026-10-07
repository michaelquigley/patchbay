# User interface

`internal/ui` is the dfx application that `patchbay` opens with no subcommand. It has a toolbar along the top, the canvas with the inspector to its right, and a status strip along the bottom. Each frame it drains the backend's events and reconciles the current snapshot into a view for that frame only (`internal/model`). It then declares the view on a dfx `NodeCanvas` and applies the canvas's intents back to the model. It draws observed state only.

Patching is built. A link gesture between two pins creates a link, and `Delete` removes the selected links. Both are requests whose outcome is observed, never assumed (`patching.md`). Nothing else in the window changes the running system.

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
- `· N hidden`, dimmed, when the block's own hidden ports carry N connections.

Its visible ports are pin rows, inputs on the left and outputs right, declared at every zoom detent so links keep their anchors. A pin row's label gains ` (hidden)` when Show hidden is revealing it, and a dimmed `· N hidden` when it has N connections to ports that are not drawn. Title, pin text, and suffixes are drawn through the canvas's `Label`, so they are detent-safe; a suffix is a second label on the same row, in the theme's disabled text color.

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

Ports are hidden individually from the inspector.

## Inspector

An `HCollapse` anchored to the right of the canvas shows the selection as observed. It opens 380 wide, and its resize handle is on its left edge: drag left to widen it, right to narrow it. The panel is drawn with the full available size, which is what bounds its resize. `I` toggles it, and when the panel has somehow become narrower than its collapsed width, `I` restores it to 380 and expands it instead.

Its body has its own padding and scrolls. Every view is laid out the same way:

- **A title line:** the object's name, led by its class glyph in its media hue. The glyph carries the color; the name stays in the text color.
- **Sections:** each opens with two spacing units and a separator carrying a lowercase header.
- **Label/value tables:** labels sit in a fixed 96px column, and values wrap in the rest. Paths, names, keys, and property values are in the monospace font, so long node names never overflow or clip.

The views:

- **One block:**
  - **identity:** recognition key, record key (or, when it has none, the reason: its identifying name is empty, other live blocks share its key, several remembered records answer to it, or it appeared ambiguous and stays new), and the owning node's name, serial, id, and device serial.
  - **state:** the connection state, the node's state, and whether the block is hidden.
  - **ports:** visible ports, each with its label, serial, id, and class, and a `hide` button, or `cannot be hidden: it has no port key`.
  - **hidden ports:** the same, each with `unhide` inline.
  - **metadata:** the `default` metadata entries that name the node, by the serial their subject resolved to, or by its `node.name` in a JSON value, never by protocol id.
  - **properties:** a `filter keys` box that narrows keys by substring, above the node's properties in a tree collapsed by default.
  - **actions:** a button row (`hide block` or `unhide block`), then the association combo. It offers the remembered records with no live block and the same media and direction.
- **One link:**
  - **identity:** from, to, serial, id.
  - **state:** the link's state, its error, and its provenance: `created here`, or observed.
  - **properties:** filterable and collapsed, as for a block.
- **Several objects:** a count, and the keys that act on them.
- **Nothing:** a one-line prompt, then the `default` metadata object as a table of key, subject, and value. The subject reads `global`, the node it names by serial, or `subject unresolved` when it did not resolve or has gone.

Beneath the selection, the request list shows pending and recently failed requests, as the status strip does, read from the current snapshot.

The inspector resolves the selection's serials only against the snapshot the drawn view came from: the current snapshot for a live view; for a stale view, the last snapshot a live view was built from, if its session and generation match the view's. When neither matches, it shows `details unavailable: the graph has changed` rather than looking a serial up in another graph, where a reconnect may have reused it. While the view is stale the inspector says that what it shows is not current. Its actions are presentation operations only: nothing in the inspector creates or destroys a link.

## Toolbar

Two checkboxes control the category filters: `video` and `monitor`. Both are unchecked (filtered) in a fresh workspace, and both are saved. A third checkbox, `show hidden (shift+h)`, reveals everything while it is on and is never saved.

## Status strip

The first line carries:

- the connection state and its reason;
- the snapshot's unresolved-reference count, while live;
- `showing hidden`, while Show hidden is on;
- a transient notice, for six seconds (a refused link, a request in sample mode or while not live).

The lines after it list every pending request with its age, and every request that failed in the last fifteen seconds with its reason.

While the view is stale, the line says which generation the drawn graph came from and that it is not current, or that no graph has been observed yet. In sample mode a second line says the window is read-only and that patching and quantum controls are disabled.

## Arrivals

A block that appears after the graph the connection started with is placed inside the current view (see `model.md`) and announced. The status strip shows `arrived: …` with each arrival's title, newest first, for ten seconds, with a `dismiss` button; `N` centers the view on the newest batch of arrivals. The list is session-scoped: it holds at most 20 entries, is cleared by dismissal or a new connection session, and is never stored in the workspace. The canvas reports its visible rectangle to the model after every frame, computed with `CanvasFromScreen` on the canvas rect.

## Sample mode

`--sample <dir>` loads the capture's `pw-dump.json` as a fixed live snapshot and runs the whole window against it. Patching is disabled: a link gesture or `Delete` posts nothing and says so. By default it uses `sample-workspace.yaml` beside the live workspace, so arranging a capture never writes its records into the workspace the live graph uses. `--workspace` overrides either default.

## Lifecycle

dfx components are constructed before the imgui context exists. The canvas therefore derives its normal and desaturated styles from the theme, and restores the remembered view, at its first draw. On shutdown the canvas is destroyed, the model writes any pending workspace change, and the backend connection closes. An interrupt (`SIGINT`, `SIGTERM`) closes the window through the same path.
