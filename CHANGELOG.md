# CHANGELOG

## Unreleased

FEATURE: `patchbay` opens a window: a canvas of the live graph's blocks at their remembered positions, colored by media, with video and monitor filters, Show hidden (`Shift+H`), hide selection (`H`), snap selection to grid (`S`), fit (`F`), center (`C`), and a status strip that says when what it shows is not current. `--sample <dir>` runs it read-only against a capture.

FEATURE: A presentation model groups the live graph into per-owner audio and MIDI blocks and remembers their positions and visibility in `~/.config/patchbay/workspace.yaml`. A returning device or application is recognized only when the match is unambiguous; otherwise it is presented as new and offered for association. Nothing it does changes routing.

FEATURE: `patchbay dump` prints the live PipeWire graph keyed by object serial and reprints it on every change, riding through daemon restarts with a reconnect backoff. `--sample <dir>` prints a captured `pw-dump.json` through the same model instead.
