# Workspace

`internal/workspace` is the one remembered workspace: block positions, visibility preferences, category filters, associations, the canvas pan and zoom, and the window and side-panel layout. It holds presentation only. No record names a link, a quantum, or a routing choice, and loading a workspace changes nothing in the running system. Only `internal/model` uses it.

## File

`~/.config/patchbay/workspace.yaml` (the user config directory's `patchbay/workspace.yaml`), written through `df/dd` with sorted keys:

```yaml
hidden_classes:
    monitor: true
    video: true
layout:
    inspector:
        collapsed: false
        width: 380
    performance:
        collapsed: true
        width: 320
    window:
        height: 900
        width: 1400
records:
    app:REAPER|midi|in:
        hidden_ports:
            - REAPER:MIDI Input 9
        key:
            class: app:REAPER
            direction: in
            media: midi
        x: 400
        "y": 840
    app:gnome-remote-desktop-daemon|audio|in#2:
        also:
            - class: app:gnome-remote-desktop
              direction: in
              media: audio
        hidden: true
        key:
            class: app:gnome-remote-desktop-daemon
            direction: in
            media: audio
        x: 1000
        "y": 500
version: 1
view:
    pan_x: -40
    pan_y: 12.5
    zoom: 1.25
```

Records are keyed by record key and carry their recognition key, position, block hidden flag, hidden port keys, and the optional `also` list of other recognition keys the operator has associated with the record. A record whose block was observed on a device that reports a hardware serial also carries it as `device` (`Focusrite_Scarlett_16i16_4th_Gen_SCARLETT16I16`). It orders the association chooser and is never matched on. `hidden`, `hidden_ports`, `also`, and `device` are omitted when empty; a file written before `device` existed reads the same, and its records learn their device the next time they are assigned.

A missing file is a fresh workspace: version 1, zoom 1, `video` and `monitor` filtered, no records. A file with a version other than 1 is refused; migrations are not implemented.

`layout` remembers the window's width and height in logical screen coordinates, plus each side panel's expanded width and collapsed state. Older version 1 files without it remain valid: the window defaults to 1400×900, with the inspector open at 380 and the performance panel open at 320. Missing or nonpositive window dimensions use their defaults; a panel width below its collapsed minimum uses its default expanded width. Panel animation widths are never saved, and a temporary zero window size does not replace the last usable size. Sample mode keeps this layout in its separate sample workspace, like the canvas view.

Records are never deleted by the application. A record whose block is absent stays in the file and is offered for association.

## Saving

Saves are atomic (a temporary file beside the target, then a rename). The model saves through a debounced store: each change re-encodes the workspace immediately, and the bytes are written 500 ms after the last change. Closing the model writes anything pending. Window and panel layout is sampled after each frame and saved only when it changes. Toggling Show hidden, and reconciling a snapshot that creates no record, write nothing.
