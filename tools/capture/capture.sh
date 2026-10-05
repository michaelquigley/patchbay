#!/usr/bin/env bash
# read-only capture of the local pipewire graph and scarlett state.
#
# usage: ./capture.sh <label>
#
# writes samples/<hostname>-<yyyymmdd-hhmmss>-<label>/ next to this script's
# repo root. nothing here modifies pipewire, wireplumber, or the interface;
# every command is a read.

set -u

label="${1:?usage: capture.sh <label>   (e.g. baseline, reaper-restart, two-reapers, scarlett-reconnect)}"
label="$(echo "$label" | tr -c 'a-zA-Z0-9-\n' '-')"
root="$(cd "$(dirname "$0")/../.." && pwd)"
out="$root/samples/$(hostname)-$(date +%Y%m%d-%H%M%S)-$label"
mkdir -p "$out"

run() {
    # run <file> <cmd...>: capture stdout+stderr, never fail the script.
    local file="$1"; shift
    if command -v "$1" >/dev/null 2>&1; then
        "$@" >"$out/$file" 2>&1 || echo "(exit $?)" >>"$out/$file"
    else
        echo "($1 not installed)" >"$out/$file"
    fi
}

{
    echo "host: $(hostname)"
    echo "date: $(date -Is)"
    echo "label: $label"
    echo "kernel: $(uname -r)"
    echo "pipewire: $(pipewire --version 2>&1 | tr '\n' ' ')"
    echo "wireplumber: $(wireplumber --version 2>&1 | tr '\n' ' ')"
    echo "reaper processes:"
    pgrep -af reaper | grep -v oom_reaper | grep -v "$0" || echo "  (none)"
} >"$out/meta.txt"

run pw-dump.json           pw-dump
run pw-top.txt             pw-top -b -n 3
run wpctl-status.txt       wpctl status
run wpctl-settings.txt     wpctl settings
run metadata-list.txt      pw-metadata -l
for name in settings sm-settings schema-sm-settings default route-settings; do
    run "metadata-$name.txt" pw-metadata -n "$name"
done
run asound-cards.txt       cat /proc/asound/cards
run lsusb.txt              lsusb

# scarlett side: one routing/controls dump per scarlett card.
if command -v scarlettctl >/dev/null 2>&1; then
    run scarlettctl-list.txt scarlettctl list
    grep -i scarlett /proc/asound/cards | awk '{print $1}' | while read -r card; do
        run "scarlettctl-routing-card$card.txt"  scarlettctl routing "$card"
        run "scarlettctl-controls-card$card.txt" scarlettctl controls "$card"
        run "scarlettctl-mixer-card$card.txt"    scarlettctl mixer "$card"
    done
else
    echo "(scarlettctl not installed)" >"$out/scarlettctl-list.txt"
fi
run sessionmixer-topology.txt sessionmixer topology --routing --mixes
cp -f "$HOME/.config/sessionmixer/session.yaml" "$out/sessionmixer-session.yaml" 2>/dev/null || true

# the pipewire-side card profiles, which decide how many channels the
# scarlett exposes and what they're called.
run alsa-card-profiles.txt bash -c 'for d in $(pw-cli ls Device 2>/dev/null | awk "/^\tid/ {print \$2}" | tr -d ,); do echo "== device $d =="; pw-cli enum-params "$d" Profile 2>&1 | head -200; echo; pw-cli enum-params "$d" Route 2>&1 | head -400; done'

echo "wrote $out"
ls -1 "$out"
