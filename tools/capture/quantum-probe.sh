#!/usr/bin/env bash
# quantum probe: the ONE capture step that is not read-only.
#
# usage: ./quantum-probe.sh [forced-quantum]   (default 256)
#
# sets the runtime setting clock.force-quantum, observes, then sets it back to
# 0 (automatic). this is a live change to the graph's processing quantum and
# may cause an audible interruption; run with reaper launched the usual way
# (pw-jack -p64 ...) but not mid-session. nothing persists: the setting lives
# in runtime metadata only and a pipewire restart clears it.
#
# produces one sample directory with three phases: before, forced, released.

set -u

q="${1:-256}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
out="$root/samples/$(hostname)-$(date +%Y%m%d-%H%M%S)-quantum-probe-$q"
mkdir -p "$out"

snap() {
    local phase="$1"
    mkdir -p "$out/$phase"
    pw-metadata -n settings >"$out/$phase/metadata-settings.txt" 2>&1
    pw-top -b -n 5 >"$out/$phase/pw-top.txt" 2>&1
    pw-dump >"$out/$phase/pw-dump.json" 2>&1
    # reaper's node(s), with the properties that express its own quantum demands.
    python3 - "$out/$phase/pw-dump.json" >"$out/$phase/reaper-nodes.txt" 2>&1 <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
for o in d:
    if o.get("type") != "PipeWire:Interface:Node":
        continue
    p = o.get("info", {}).get("props", {})
    blob = " ".join(str(v) for v in p.values()).lower()
    if "reaper" not in blob:
        continue
    print("node", o["id"], p.get("node.name"), "|", p.get("media.class"))
    for k in sorted(p):
        if any(s in k for s in ("quantum", "latency", "rate", "force", "jack", "application", "node.name", "node.nick", "node.description", "client", "object.serial")):
            print("   ", k, "=", p[k])
    print("    state:", o.get("info", {}).get("state"))
PY
    date -Is >"$out/$phase/time.txt"
}

echo "phase 1: before (force-quantum as found)"
snap 1-before
sleep 1

echo "phase 2: forcing clock.force-quantum=$q"
pw-metadata -n settings 0 clock.force-quantum "$q" >"$out/set-forced.txt" 2>&1
sleep 3
snap 2-forced

echo "phase 3: releasing clock.force-quantum=0 (automatic)"
pw-metadata -n settings 0 clock.force-quantum 0 >"$out/set-released.txt" 2>&1
sleep 3
snap 3-released

{
    echo "host: $(hostname)"; echo "date: $(date -Is)"; echo "forced quantum: $q"
    echo "reaper processes:"; pgrep -af reaper | grep -v oom_reaper || echo "  (none)"
    echo; echo "launch note: record here anything you heard (dropout, glitch, silence) and for how long:"
    echo "  "
} >"$out/meta.txt"

echo "wrote $out"
