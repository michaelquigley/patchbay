# Workspace

`internal/workspace` is the one remembered workspace: block positions, visibility preferences, category filters, associations, and the canvas pan and zoom. It holds presentation only. No record names a link, a quantum, or a routing choice, and loading a workspace changes nothing in the running system. Only `internal/model` uses it.

## File

`~/.config/patchbay/workspace.yaml` (the user config directory's `patchbay/workspace.yaml`), written through `df/dd` with sorted keys:

```yaml
hidden_classes:
    monitor: true
    video: true
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

Records are keyed by record key and carry their recognition key, position, block hidden flag, hidden port keys, and the optional `also` list of other recognition keys the operator has associated with the record. `hidden`, `hidden_ports`, and `also` are omitted when empty.

A missing file is a fresh workspace: version 1, zoom 1, `video` and `monitor` filtered, no records. A file with a version other than 1 is refused; migrations are not implemented.

Records are never deleted by the application. A record whose block is absent stays in the file and is offered for association.

## Saving

Saves are atomic (a temporary file beside the target, then a rename). The model saves through a debounced store: each change re-encodes the workspace immediately, and the bytes are written 500 ms after the last change. Closing the model writes anything pending. Toggling Show hidden, and reconciling a snapshot that creates no record, write nothing.
