# patchbay

A desktop PipeWire patchbay for a working studio: a remembered workspace over a changing graph, manual audio and MIDI patching, live quantum control, and compact xrun monitoring.

`patchbay` opens a canvas of the live graph that remembers where you put things, and recognizes a device or application when it returns. Pull a link between two pins to connect them, or select links and press `Delete` to remove them; each is a request whose result Patchbay shows only once PipeWire reports it. A performance panel (`P`) sets the graph's quantum and shows each driver's observed quantum and the xruns counted since a resettable baseline. An inspector (`I`) shows the selection as PipeWire reports it, and on a Scarlett with a SessionMixer profile, the hardware source behind each capture channel. `patchbay dump` prints the graph keyed by object serial.

## Building

Patchbay links `libpipewire-0.3`, and `libasound` for its Scarlett annotations, through cgo, so it is built on the machine it runs on.

```
sudo apt install libpipewire-0.3-dev libasound2-dev
make
```

PipeWire 1.0 or newer is required.

## Usage

```
patchbay                           # open the window against the live graph
patchbay --sample <dir>            # open it read-only against a captured sample
patchbay --workspace <file>        # use another workspace file
patchbay dump                      # print the live graph, and again on every change
patchbay dump --sample <dir>       # print a captured pw-dump.json sample instead
```

## License

Apache 2.0.
