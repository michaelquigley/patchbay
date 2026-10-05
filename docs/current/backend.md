# Backend

`internal/pipewire` is Patchbay's only contact with PipeWire. It owns one connection, observes the graph into immutable snapshots, and reports discrete changes as events. The ui (when it exists) reads a snapshot once per frame and never holds a native handle.

## Contract

```go
type Conn interface {
    Snapshot() *Snapshot     // immutable; pointer identity changes only when content does
    Events() <-chan Event    // buffered; drained by the consumer
    Close()
}
```

`pipewire.Connect()` never fails. A daemon that cannot be reached is published as `disconnected` with the error text and retried.

A `Snapshot` carries a generation counter, the connection state (and the last disconnect reason), and maps keyed by `Serial` for nodes, ports, links, devices, and clients, plus the decoded `settings` metadata and the `default` metadata entries. Every object carries its serial, its protocol id (for requests only), its full property map, and the typed fields the model reads:

| type | typed fields |
| --- | --- |
| `Node` | `Name` (`node.name`), `MediaClass`, `State`, `Error` |
| `Port` | `NodeSerial`, `NodeID`, `Direction`, `Media`, `Monitor` |
| `Link` | `OutPort`, `InPort`, `OutNode`, `InNode` (all serials), `State`, `Error` |
| `Settings` | `Rate`, `Quantum`, `MinQuantum`, `MaxQuantum`, `ForceQuantum`, `ForceRate`, `Present` |

`Port.Media` comes from `format.dsp` (`midi` or `UMP` is MIDI, `audio` is audio); a port with no dsp format whose node's `media.class` contains `Video` is video; anything else is unknown. Node and link states are the bound-info states as PipeWire names them. Missing properties are empty strings; nothing panics on a malformed or absent property, and an object without a usable `object.serial` is not tracked at all.

Events are `ObjectAppeared` and `ObjectVanished` (kind and serial), `LinkStateChanged` (including a link's first state), `ConnStateChanged`, and `RequestResolved`. An event is sent only after the snapshot containing its subject is published. If the consumer falls more than 4096 events behind, further events are dropped with a log line; the snapshot remains the truth.

## Identity

Everything live is keyed by `object.serial`. Protocol ids recycle within seconds (REAPER came back with the same node and client ids after a restart on both studio machines), so an id is never used to recognize an object.

References between objects are resolved to serials when the referring object is announced, and only then: a port's owner from its `node.id`, a link's endpoints from its `link.output.*` and `link.input.*` ids. A reference whose target is not observed at that moment stays unresolved (serial 0) for the object's lifetime, with a log line, because an id that misses now may later be held by a different object. There is no late resolution. The registry announces objects in registration order, so a target always precedes its references in practice. The snapshot's `Unresolved` count (ports with no owner, links with a missing endpoint) makes any miss visible; `patchbay dump` prints it beside the connection state.

Serials come from a per-daemon counter and repeat after a daemon restart; see Connection lifecycle.

## Observation

The backend runs one `pw_thread_loop` per connection. Every registry global of type Node, Port, Link, Device, or Client is bound, as is the Metadata object named `settings` and the one named `default`; other metadata is ignored. Bound info replaces the registry-time properties, so node and link states are live.

Callbacks do not build snapshots. The cgo layer translates each callback into an `Input` value (`GlobalAdded`, `GlobalRemoved`, `NodeInfo`, `PortInfo`, `LinkInfo`, `ObjectInfo`, `MetadataProperty`, `ProxyError`, `SyncDone`) and hands it to the `Graph`, then signals a loop event. When the loop finishes the current dispatch, the event folds everything applied since the last fold into one new snapshot and publishes it with an atomic pointer swap. A burst of callbacks becomes one snapshot.

## Connection lifecycle

States are `connecting`, `live`, and `disconnected`.

A connection stays `connecting` until initial observation is complete. That barrier is two core sync round trips plus first info. The first sync, issued after the registry listener is attached, ends enumeration. The second sync, issued when the first completes, guarantees that the bind requests made during enumeration have been answered. Every object enumerated before the barrier must also have delivered its first bound info, or reported a proxy error. Snapshots published before the barrier carry `connecting`, so objects that arrive in separate callbacks are presented together in the first `live` snapshot. Once passed, the barrier latches: the connection stays `live` until it is lost, and an object announced afterwards (a hotplug, a client starting) is published as soon as it is announced and updated when its first info arrives, without affecting the connection state.

A core error, which includes socket loss (`EPIPE`), ends the connection. The backend fails every pending request with a `disconnected: …` reason, drops the session's provenance and metrics records, publishes `disconnected` with the reason, tears down the thread loop, and reconnects. The retry delay starts at one second and doubles to a ten-second ceiling; it resets once a connection reaches `live`. A failed connect attempt is published as `disconnected` with its error text. Each reconnect builds a new graph from nothing, and its barrier applies as at startup.

The disconnected snapshot keeps the last observed graph so a consumer can draw it as stale. It is never current.

## Samples

`internal/sample` reads a `pw-dump.json` capture and replays it as the inputs a live enumeration would deliver: every global first, in `object.serial` order (the daemon's registration order, which pw-dump's own output order only approximates), then each object's info and metadata properties. The serial ordering is load-bearing, since references resolve only at announcement. The replay goes through the same `Graph`, so a sample snapshot is built by the code that builds live ones. Dump property values that are not strings are rendered as their JSON text (`48000`, `true`), which is how a live `spa_dict` carries them.

`patchbay dump` prints the graph keyed by serial, and prints it again on every change while live. `patchbay dump --sample <dir>` prints a capture once.

## Native layer

`pipewire.c`/`pipewire.h` hold every libpipewire macro wrapper and listener table; `native.go` holds the exported callback trampolines. No Go pointer is stored in C memory: the connection is identified to Go by one `cgo.Handle` value, and bound objects by their serial. The code is written against the PipeWire 1.0 API surface.
