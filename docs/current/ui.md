# User interface

`internal/ui` is the dfx application that `patchbay` opens with no subcommand. It has three parts, top to bottom: a toolbar, the canvas, and a status strip. Each frame it drains the backend's events and reconciles the current snapshot into a view for that frame only (`internal/model`). It then declares the view on a dfx `NodeCanvas` and applies the canvas's intents back to the model. It draws observed state only. Nothing in the window creates or destroys a link or changes a setting yet; patching lands in stage 4.

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

Each block takes its media's hue as its dfx accent: its title band and pins, and its border when selected (thicker, in a highlight of the hue). Each link takes the hue of its output's media. The hues live in `internal/ui/palette.go`:

| media | hue |
| --- | --- |
| audio | steel blue `(0.24, 0.52, 0.72)` |
| midi | amber `(0.78, 0.52, 0.18)` |
| video | violet `(0.55, 0.38, 0.76)` |
| unknown | neutral grey `(0.42, 0.44, 0.48)` |

Links draw at 0.9 opacity. On a stale view, accents are greyed along with the rest of the canvas.

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
| `LinkCreated` | logged and reported in the status strip as "patching lands in stage 4"; nothing is created |

### Navigation

Pan and zoom are restored from the workspace at the first frame, with no fit and no layout. They are saved whenever a navigation changes them. Middle-drag pans and ctrl+wheel zooms, as dfx defines.

## Keys

| key | action |
| --- | --- |
| `H` | hide the selected blocks |
| `S` | snap the selected blocks to the canvas grid, as one move |
| `Shift+H` | toggle Show hidden |
| `F` | zoom to fit |
| `C` | center on the selection |

Ports are hidden individually from the inspector, which lands in stage 4. `Delete` does nothing yet.

## Toolbar

Two checkboxes control the category filters: `video` and `monitor`. Both are unchecked (filtered) in a fresh workspace, and both are saved. A third checkbox, `show hidden (shift+h)`, reveals everything while it is on and is never saved.

## Status strip

The first line carries:

- the connection state and its reason;
- the snapshot's unresolved-reference count, while live;
- `showing hidden`, while Show hidden is on;
- a transient notice, for six seconds.

While the view is stale, the line says which generation the drawn graph came from and that it is not current, or that no graph has been observed yet. In sample mode a second line says the window is read-only and that patching and quantum controls are disabled.

## Sample mode

`--sample <dir>` loads the capture's `pw-dump.json` as a fixed live snapshot and runs the whole window against it. By default it uses `sample-workspace.yaml` beside the live workspace, so arranging a capture never writes its records into the workspace the live graph uses. `--workspace` overrides either default.

## Lifecycle

dfx components are constructed before the imgui context exists. The canvas therefore derives its normal and desaturated styles from the theme, and restores the remembered view, at its first draw. On shutdown the canvas is destroyed, the model writes any pending workspace change, and the backend connection closes. An interrupt (`SIGINT`, `SIGTERM`) closes the window through the same path.
