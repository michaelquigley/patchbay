# patchbay

A desktop PipeWire patchbay for a working studio: a remembered workspace over a changing graph, manual audio and MIDI patching, live quantum control, and compact xrun monitoring.

Patchbay is under construction. Today it observes: `patchbay dump` prints the live graph keyed by object serial and reprints it on every change, riding through PipeWire restarts.

## Building

Patchbay links `libpipewire-0.3` through cgo, so it is built on the machine it runs on.

```
sudo apt install libpipewire-0.3-dev
make
```

PipeWire 1.0 or newer is required.

## Usage

```
patchbay dump                      # print the live graph, and again on every change
patchbay dump --sample <dir>       # print a captured pw-dump.json sample instead
```

## License

Apache 2.0.
