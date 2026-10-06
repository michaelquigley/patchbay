# CHANGELOG

## Unreleased

FEATURE: Patching. Pull a link between two pins to connect them, and select links and press `Delete` to remove them. A link appears on the canvas once PipeWire reports it, drawn in its reported state: dimmed while it settles, in its media's color when active, red in error. A pending or failed request is listed in the status strip with its reason, never drawn as a link. Links Patchbay creates linger after it closes, and the inspector labels each one `created here` or observed. An inspector panel on the right (`I` to show or hide it) shows the selected block or link as PipeWire reports it, with hide, unhide, and association.

FEATURE: `patchbay` opens a window: a canvas of the live graph's blocks at their remembered positions, colored by media, with video and monitor filters, Show hidden (`Shift+H`), hide selection (`H`), snap selection to grid (`S`), fit (`F`), and center (`C`). A device that appears while it runs lands inside the current view and is announced in the status strip, and `N` centers on it. The status strip also says when what the canvas shows is not current. `--sample <dir>` runs the window read-only against a capture.

FEATURE: A presentation model groups the live graph into per-owner audio and MIDI blocks and remembers their positions and visibility in `~/.config/patchbay/workspace.yaml`. A returning device or application is recognized only when the match is unambiguous; otherwise it is presented as new and offered for association. Nothing it does changes routing.

FEATURE: `patchbay dump` prints the live PipeWire graph keyed by object serial and reprints it on every change, riding through daemon restarts with a reconnect backoff. `--sample <dir>` prints a captured `pw-dump.json` through the same model instead.
