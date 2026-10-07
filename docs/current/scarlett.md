# Scarlett annotations

`internal/scarlett` shows, in the inspector, which hardware source a Scarlett routes to each of its PCM capture channels: `hardware source: Analogue Input 1`. It is read-only and conditional. Nothing in Patchbay writes a Scarlett control; SessionMixer remains the control surface. A port that cannot be annotated stays an ordinary PipeWire port.

It reads the card through `scarlettctl` and the device's layout through SessionMixer's `topology` package, at the commits pinned in `go.mod`. Patchbay keeps no device definitions of its own.

## The join

A PipeWire port is joined to a Scarlett routing control by the kernel driver's own indexing, not by resemblance between names.

- **Channel.** The port's `object.path` names its PCM and channel: `alsa:pcm:1:hw:1:capture:capture_3` (or `hw:1,0` under the pro-audio profile). The channel index, 3, is read from the path alone. Only PCM device 0 is accepted. The `AUXn` in a port's name or alias is never read.
- **Control.** The scarlett2 mixer driver names its PCM capture routing controls by one-based channel on that same PCM. So channel N is SessionMixer's `pcm-capture-(N+1)`: `capture_AUX3` is `pcm-capture-4` by the driver's definition.
- **Card.** The card is matched through PipeWire. A device that reports a hardware serial (`Device.HardwareSerial`, from `device.serial`) and an ALSA card number (`Device.ALSACard`, from `api.alsa.card`) has that card opened with `scarlettctl.OpenCard`. A port is annotated only when the card number in its `object.path` is that device's card.

The join was established from this indexing, not measured. A signal routed through a channel would confirm the driver against itself, and is optional.

## When a port is annotated

All of these must hold:

- the port's device has a hardware serial and an ALSA card, and the card opened and reports itself a Scarlett (`Card.IsScarlett`);
- SessionMixer's `DetectProfile` accepted the card's model and firmware, and the topology built;
- the port's `object.path` is a capture channel on that card's PCM device 0, within the profile's PCM capture count;
- the channel has an endpoint with a routing control;
- the card's event monitor is running;
- the last read of every channel succeeded.

Otherwise the inspector shows `hardware source: unavailable (<reason>)`:

| reason | when |
| --- | --- |
| `no PCM capture path` | the port's path is not a capture channel on the card's PCM device 0 (playback and monitor ports, for instance) |
| `no card for this device` | the card could not be opened |
| `no topology profile for this firmware` | SessionMixer did not recognize the model or firmware, or its topology did not build |
| `channel outside the profile's PCM capture count` | the path's channel is beyond the profile's capture channels |
| `no routing control for this channel` | the topology has no endpoint for `pcm-capture-(N+1)` |
| `monitor not running` | the event monitor stopped or failed |
| `last read failed` | the most recent read of any channel failed |
| `not yet read` | the card is still opening, or no read has completed yet |

A device with no hardware serial or no ALSA card, or whose card is not a Scarlett, is not annotated at all: no line is shown.

## Validity

The adapter tracks validity itself and does not trust SessionMixer's cache. A channel is valid only after a successful read, and only while the event monitor runs.

- **A failed read.** If any channel's `RefreshFromHardware` fails, every channel becomes invalid, including when the read was triggered by a control-change event while the monitor keeps running. The next successful read of every channel restores them.
- **A stopped monitor.** A monitor that stops or fails invalidates every channel.
- **A closed card.** A card that closes, because its device left the graph or the connection is not live, invalidates every channel.
- **No stale reads.** `GetCurrentSource` is never consulted for an invalid channel, because SessionMixer keeps its last value through a failed read. An index outside the control's items is treated as a failed read rather than shown as `Unknown`.

Changes are watched with `EventMonitor.Watch`, not `WatchControls`, which skips a control it cannot read and keeps running. `Watch` reports numid 0 for every event, so each event re-reads every channel. The card's handle subscribes to events when it opens, before the first read. So the first read is shown as soon as it succeeds, though `Watch` has not yet started polling. That is deliberate (decided 2026-10-07):

- nothing in the gap before `Watch` polls can be lost or go stale, because any change after the read is queued on the handle and delivered when `Watch` runs, which then reads every channel again;
- if `Watch` fails to start, it invalidates every channel like any monitor stop;
- a pre-check of `Watch`'s start conditions would add nothing, and holding the read back for a delay would be a timing heuristic.

While an event's re-read is in progress, the channels stay valid and show the last successful read until the reads finish, and a failure then invalidates every channel (decided 2026-10-07). That window equals the read latency, which every display lags the hardware by. Invalidating for the duration of each re-read would flicker every annotation on each SessionMixer fader move, since every control change is an event.

## Lifecycle

- The ui calls `Sync` with every snapshot. It opens a card for each device in a live snapshot that has a hardware serial and an ALSA card, and closes it when the device leaves. A snapshot that is not live holds no devices, so nothing is annotated while the graph is not current.
- Each card has its own goroutine. That goroutine owns the ALSA handle: it opens the card, detects the profile, makes every read, and runs the watch. Closing marks every channel invalid at once and stops the monitor; the goroutine closes the handle when the watch returns.
- A card's lifetime is one PipeWire device object in one connection: hardware serial, card number, device serial, and connection session. A replugged interface is a new device object, and a reconnect is a new session. Either reopens the card, even when no snapshot showed the device missing in between, and its annotations return only after the new card's first successful read.
- In sample mode there is no adapter: a capture has no cards.

## Workarounds

These are gaps in `scarlettctl` and SessionMixer that Patchbay works around without changing them; each is a roadmap item in its own repository.

1. **The card has no serial.** `scarlettctl.Card` exposes only its number and name. The card is therefore matched through PipeWire's `api.alsa.card` on the device that carries the hardware serial, and cross-checked against the card number in each port's path, rather than by comparing serials on the card's side.
2. **`PCMCaptureEndpoints` is not positional.** The topology builder skips a channel whose routing control is missing or whose first read fails, so `PCMCaptureEndpoints[N]` is not necessarily channel N. Endpoints are keyed by their port's one-based `Number` (`pcm-capture-N`) instead.
3. **`EventMonitor.Stop` races `Watch`.** `Stop` writes the monitor's `running` flag while `Watch` reads it on the card's goroutine, unsynchronized. It is harmless in practice: `Watch` also checks a closed channel. But the race detector would flag it in a live run. The monitor is created when the card opens, so a stop that arrives before the watch starts still reaches it.

## Building

`scarlettctl` links `libasound` through cgo, so any machine that builds Patchbay needs the ALSA development headers (`libasound2-dev`), as well as `libpipewire-0.3-dev`. Binaries are built on the machine they run on, so that includes the studio machines.
