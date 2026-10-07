# patchbay

A desktop PipeWire patchbay built with Go and dfx: a stable, remembered workspace over a changing graph, manual audio and MIDI patching, live quantum control, and compact xrun monitoring. A personal studio tool intended for open-source release.

## Status

Under construction against `docs/future/patchbay.md` (the spec) and `docs/future/patchbay-work-order.md` (the work order), landed in numbered stages, each gated by terminus and then by Michael. Stages 1 (scaffold, backend observation, samples), 2 (presentation model and workspace), 3 (the canvas), 4 (patching and the inspector), and 5 (quantum and monitoring) are built. What exists is described in `docs/current/`; the work order says what comes next.

## Stack

- Go, single module `github.com/michaelquigley/patchbay`.
- cgo against `libpipewire-0.3`. The API floor is PipeWire 1.0 (the studio machines run 1.0.5; the desktop runs 1.6.2). Use nothing newer than 1.0 offers.
- `github.com/spf13/cobra` for the CLI. `github.com/michaelquigley/df/dl` for logging, `github.com/michaelquigley/df/dd` for configuration and workspace files, `github.com/pkg/errors` for wrapping.
- dfx (`github.com/michaelquigley/dfx`) for the ui, pinned at the tag current when the ui stage opens.

## Building

- `make` installs, `make test` runs tests plus `go vet`, `make clean` clears the project GOBIN.
- libpipewire's pkg-config cflags include `-fno-strict-overflow`, which cgo rejects by default. The Makefile exports `CGO_CFLAGS_ALLOW=-fno-strict-overflow`; when calling `go` directly, export it yourself.
- Binaries are built on the machine they run on, because cgo links the local libpipewire. The desktop needs `libpipewire-0.3-dev`.
- `go test -tags live ./internal/pipewire` runs the live tests against the user's daemon. Live tests may create and destroy links only between null sinks the test itself creates and removes (`support.null-audio-sink`, made through the backend on the test's own connection), never between objects that existed before the test. They must leave the daemon's graph as they found it, and skip cleanly when they cannot create their sinks. Never run the live tag on a studio machine during a session.

## Layout

- `cmd/patchbay/` — cobra entry; with no subcommand it opens the window (live, or `--sample <dir>`); `dump` prints the observed graph keyed by serial.
- `internal/pipewire/` — the backend. `pipewire.c`/`pipewire.h` hold every libpipewire call and listener table; `native.go` holds the cgo transport and the exported trampolines; `objects.go` is the pure-Go graph that folds inputs into snapshots; `conn.go` is the supervisor (connection states, reconnect with backoff, publishing); `snapshot.go` and `events.go` are the contract the ui consumes.
- `internal/sample/` — `pw-dump.json` captures replayed through the same graph the live backend uses.
- `internal/model/` — recognition, placement, visibility, and the view the canvas draws.
- `internal/workspace/` — the record file and its debounced store, reached only from the model.
- `internal/ui/` — the dfx window: canvas, toolbar, performance panel, inspector. `plan.go` turns a view into declarations without an imgui context, so it is tested headless.
- `samples/` — operator-captured fixtures from the studio machines (`eleven`, `seven`) and the desktop (`fortyfive`). `tools/capture/` holds the scripts that make them.

## Key conventions

- **Nothing live is keyed by id.** Node, client, port, and link ids recycle within seconds; `object.serial` is the only instance handle, and even serials restart with the daemon. Ids are carried for requests only. Link endpoints are resolved to serials when the link appears, never later.
- **Session state is cleared explicitly on disconnect.** Pending requests fail with a reason, and provenance and metrics records are dropped, rather than relying on a reused serial to miss.
- **The ui never touches a native handle.** It reads `Snapshot()` once per frame and drains `Events()`. Every libpipewire call happens on the backend's thread loop.
- **Inputs, not callbacks.** The cgo layer translates callbacks into `Input` values and hands them to the `Graph`; tests and the sample loader drive the same `Graph` directly. Keep native types out of the graph.
- **Only explicit gestures change the running system.** `CreateLink` on a link gesture, `DestroyLink` on `Delete`, and `SetForceQuantum` on the quantum control, all through `internal/ui/patching.go`. Their outcome is read from `Snapshot.Requests`, never assumed. Nothing else in the application patches or sets anything.
- **Observed state only.** Snapshots carry what PipeWire reported. A disconnected snapshot keeps the last graph for display but is never current.
- **dfx components are built before the imgui context exists.** Anything theme- or font-dependent resolves at first draw, never in a constructor.
- **The model reads typed fields only.** `internal/model` uses the snapshot's typed fields, never its property maps, and a `View` is valid for the one frame it was built in.
- File names are camelCase; comments are lowercase except doc comments, which lead with the identifier.
- **Changelog.** `CHANGELOG.md` follows the in-house convention: newest-first releases, prose entries each led by one of `FEATURE`/`CHANGE`/`FIX`, and an `## Unreleased` slot at the top that agents write into. The full spec is the grimoire's `software/conventions/changelog-convention.md`.

## For agents arriving cold

This is `patchbay`, part of Michael Quigley's broader software practice. Related projects (`dfx`, `sessionmixer`, `scarlettctl`, `mercurius`, `terminus`) live as siblings in `~/Repos/q/products/`; the PipeWire proof of concept is `~/Repos/q/research/pipewire` (a reference for cgo idioms, not an API). The grimoire at `~/Repos/q/writing/grimoire` holds cross-project conventions; for patchbay, the in-repo `docs/` is authoritative. If you've been assigned a role, read the grimoire's `agents/agent-roles.md`.

## Project memory

Durable knowledge about this project lives in `docs/journal/`, dated files `docs/journal/YYYY-MM-DD.md`. This is project memory; it does not go in harness-local storage (`.claude/` or equivalent), where it's invisible to every other harness and collaborator and dies with the host. Concretely: do not write to your harness's memory directory or memory tool for this project — even when the harness presents it as the default place for durable knowledge. That tool is the silo this convention exists to replace; the journal is the only durable home.

On arrival, read the most recent entries to pick up where the last session left off, before you start changing things. Treat them as prior-session context, not verified truth — if an entry conflicts with the code or a `docs/current/` doc, the code wins.

Write the smallest entry that carries the session's durable insight, and nothing more. The test for every line: *would a competent agent get this wrong, or waste time rediscovering it, working from the tree alone?* If it's recoverable by reading the code, the diff, `docs/current/`, or git history, leave it out.

That filter keeps four kinds of thing and discards the rest:

- **Decisions whose rationale isn't visible in the result** — why a value was chosen, what a line guards against, why something that looks like dead code or a no-op is load-bearing.
- **Deliberate non-actions** — a change you considered and chose not to make, so the next agent doesn't "fix" it. An unchanged file leaves no trace in a diff.
- **Couplings that span files** — two places that must move together, an ordering that matters, an assumption one file makes about another.
- **Live state** — what's unverified, unfinished, or waiting on something external.

Skip change inventories, restatements of the diff, and play-by-play of how you worked. There's no write-time approval gate; Michael reviews on commit. Append to the day's file if it exists, and write the few lines you'd want the next agent to read — honest and self-contained.

## Commits

The operator commits; agents don't. Never run `git commit` or `git push` in this repo. Finish the edit, leave the change in the working tree (staged is fine), report what changed, and hand off — the uncommitted diff is the review queue and the commit is the operator's act of acceptance. Approval of a change is not direction to commit; only an explicit instruction to commit is, and only for that commit.
