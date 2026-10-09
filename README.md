# patchbay

A desktop PipeWire patchbay for a working studio: a remembered workspace over a changing graph, manual audio and MIDI patching, live quantum control, and compact xrun monitoring.

`patchbay` opens a canvas of the live graph that remembers where you put things, and recognizes a device or application when it returns. Pull a link between two pins to connect them, or select links and press `Delete` to remove them; each is a request whose result Patchbay shows only once PipeWire reports it. A performance panel (`P`) sets the graph's quantum and shows each driver's observed quantum and the xruns counted since a resettable baseline. An inspector (`I`) shows the selection as PipeWire reports it, and on a Scarlett with a SessionMixer profile, the hardware source behind each capture channel. `patchbay dump` prints the graph keyed by object serial.

## Building

Patchbay links `libpipewire-0.3`, and `libasound` for its Scarlett annotations, through cgo, so it is built on the machine it runs on.

```
sudo apt install build-essential pkg-config libpipewire-0.3-dev libasound2-dev libgtk-3-dev libx11-dev libgl1-mesa-dev
make
```

PipeWire 1.0 or newer is required.

`make test` runs tests and vet. `make push` builds and stages the local binary in the depot through `push vendor`.

GitHub CI runs tests with race detection, vet, and a build on Ubuntu 24.04, using the Go version in `go.mod`. It runs headless, without a PipeWire daemon or the live-test tag. Version tags run the same checks and prepare a draft GitHub release from the matching changelog section. Releases contain source only; build on the machine where Patchbay will run.

## Usage

```
patchbay                           # open the window against the live graph
patchbay --sample <dir>            # open it read-only against a captured sample
patchbay --workspace <file>        # use another workspace file
patchbay dump                      # watch the live graph (50 ms polling)
patchbay dump --sample <dir>       # print a captured pw-dump.json sample instead
patchbay desktop integrate         # add a launcher and icon for this binary to the linux desktop
patchbay desktop remove            # remove them
```

## License

Apache 2.0.
