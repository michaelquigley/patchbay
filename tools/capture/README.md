# capture tools

Operator-run scripts that record the state of a studio machine's PipeWire graph and Scarlett interface into `samples/`. The samples are the evidence the identity, grouping, quantum, monitoring, and Scarlett work is written against, and they become headless test fixtures for the application.

Copy this directory to the studio machine, run, copy the resulting `samples/` directory back into this repo, then run the scrub before anything is committed:

```
python3 tools/capture/scrub.py samples
```

`scrub.py` rewrites systemd machine ids, hardware serials, the capturing user's name, and home paths to stable placeholders, consistently across every file, and deletes `lsusb.txt`. It is idempotent. Hostnames stay, since the docs and fixtures name the studio machines by them. The repo is public; nothing under `samples/` is committed unscrubbed.

## capture.sh — read-only

```
./capture.sh <label>
```

Reads only. Writes `samples/<host>-<timestamp>-<label>/` containing `pw-dump.json`, `pw-top.txt`, `wpctl` status and settings, every metadata object, ALSA card list, the PipeWire device profiles and routes, and if `scarlettctl` or `sessionmixer` are installed, the hardware routing, controls, mixer state, and session config.

Scenarios wanted, each one run. Have REAPER running (launched the usual way) and the interface connected unless the scenario says otherwise:

| label | when to run |
| --- | --- |
| `baseline` | normal working state, REAPER running, interface connected, a MIDI controller plugged in |
| `reaper-restart` | after quitting and relaunching REAPER |
| `two-reapers` | with a second REAPER instance open (if practical) |
| `scarlett-reconnect` | after unplugging and replugging the interface |
| `scarlett-absent` | with the interface unplugged |
| `profile-change` | after switching the interface's PipeWire profile (for example in pavucontrol or `wpctl set-profile`) |
| `reaper-closed` | with REAPER not running |

Each sample is a fixture: a new capture under `samples/` needs its expected object counts in `internal/sample/load_test.go` and, for a new scenario, its expected block set in the model's fixture suite.

## quantum-probe.sh — not read-only

```
./quantum-probe.sh [forced-quantum]   # default 256
```

Sets the runtime setting `clock.force-quantum`, snapshots, then releases it to `0`. This changes the live graph quantum and may be audible. Nothing persists past a PipeWire restart. Run on `seven` first with REAPER idle, then on `eleven` outside a session. Add a line to the sample's `meta.txt` describing anything you heard.
