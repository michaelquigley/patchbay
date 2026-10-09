# CHANGELOG

## Unreleased

FEATURE: `patchbay desktop integrate` installs a Linux desktop entry and the Patchbay mark for the binary that runs it, so Patchbay launches from the application grid under its own icon; `patchbay desktop remove` takes them out again. The window reports `patchbay` as its app id, so the desktop pairs a running window with the entry.

CHANGE: `github.com/michaelquigley/scarlettctl` updated to `v0.1.0`.

## v0.1.0

Patchbay's first release: a desktop PipeWire patchbay for audio and MIDI, with a remembered workspace and manual routing.

FEATURE: Connect ports by dragging links and disconnect selected links with Delete. The canvas and inspector show the graph as PipeWire reports it; Patchbay never restores routing automatically.

FEATURE: Arrange and hide application and device blocks in a persistent workspace. Returning devices and applications recover their layout when recognized unambiguously, with manual association for uncertain matches. Window size and panel sizes and visibility are remembered too.

FEATURE: Set PipeWire's quantum and monitor each driver's observed quantum and sample rate, with xrun counts and a resettable baseline.

FEATURE: Inspect the hardware source behind Scarlett capture channels on devices with a matching SessionMixer topology profile. Routing information is read-only and follows changes made in SessionMixer.

FEATURE: Inspect the graph from the terminal with `patchbay dump`, or explore a captured graph read-only with `--sample <dir>`.

FEATURE: GitHub CI checks builds and tests, and version tags prepare draft source releases. `make push` stages a local build in the depot.
