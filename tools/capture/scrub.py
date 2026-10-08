#!/usr/bin/env python3
"""scrub identifying values from capture samples before they enter the repo.

usage: scrub.py <samples-dir-or-sample-dir>...

rewrites, consistently across every file under the given directories:
  (placeholders for serials come from tools/capture/serials.local, see below)
  - systemd machine ids (application.process.machine-id) to stable placeholders
  - hardware serials found in device.serial values (and everywhere the same
    token appears, such as node and device names) to stable placeholders
  - the capturing user's name and home path to 'operator' and '/home/operator'
and deletes lsusb.txt, which nothing reads and which inventories the host.

hostnames are left alone: the docs and fixtures name the studio machines by hostname and
they identify nothing on their own. the script is idempotent; run it again
after new captures land. the replacement tables below keep placeholders stable
across runs so fixture tests keep their meaning.
"""
import hashlib
import os
import re
import sys

# placeholders for serials come from an optional local map file that is never
# committed (tools/capture/serials.local, gitignored): one 'SERIAL=PLACEHOLDER'
# per line, so the operator can keep readable names such as SCARLETT18I20.
# without a mapping, a serial becomes SN- plus eight hex digits of its digest,
# which is stable across runs and carries no part of the original. the real
# serials must never appear in this file or anywhere else in the repo.
LOCAL_MAP = os.path.join(os.path.dirname(os.path.abspath(__file__)), "serials.local")


def load_local_map():
    table = {}
    try:
        with open(LOCAL_MAP, encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                serial, placeholder = line.split("=", 1)
                table[serial.strip()] = placeholder.strip()
    except FileNotFoundError:
        pass
    return table


KNOWN_SERIALS = load_local_map()
USER = os.environ.get("SCRUB_USER", "michael")

MACHINE_ID = re.compile(r'("application\.process\.machine-id":\s*")([0-9a-f]{32})(")')
DEVICE_SERIAL = re.compile(r'"device\.serial":\s*"([^"]+)"')
SERIAL_TOKEN = re.compile(r'(?<![0-9A-Za-z])([0-9A-Z]{8,})(?![0-9A-Za-z])')


PLACEHOLDERS = set(KNOWN_SERIALS.values())
DIGEST_PLACEHOLDER = re.compile(r"SN-[0-9A-F]{8}")


def already_scrubbed(token):
    """a placeholder from the local map or a digest name is not a serial."""
    return token in PLACEHOLDERS or DIGEST_PLACEHOLDER.fullmatch(token) is not None


def placeholder_for(serial, table):
    if serial in table:
        return table[serial]
    if serial in KNOWN_SERIALS:
        table[serial] = KNOWN_SERIALS[serial]
    else:
        table[serial] = "SN-" + hashlib.sha256(serial.encode()).hexdigest()[:8].upper()
        print(f"note: unmapped serial -> '{table[serial]}'; add a line to serials.local for a readable name", file=sys.stderr)
    return table[serial]


def collect(paths):
    """first pass: learn the serial and machine-id tokens present."""
    serials, machine_ids = {}, {}
    for root in paths:
        for dirpath, _, files in os.walk(root):
            for name in files:
                p = os.path.join(dirpath, name)
                try:
                    text = open(p, encoding="utf-8", errors="surrogateescape").read()
                except OSError:
                    continue
                for m in MACHINE_ID.finditer(text):
                    mid = m.group(2)
                    if mid not in machine_ids:
                        machine_ids[mid] = f"{len(machine_ids) + 1:032x}"
                for m in DEVICE_SERIAL.finditer(text):
                    value = m.group(1)
                    last = value.rsplit("_", 1)[-1]
                    if already_scrubbed(last):
                        continue
                    if SERIAL_TOKEN.fullmatch(last) and any(c.isdigit() for c in last):
                        placeholder_for(last, serials)
    return serials, machine_ids


def rewrite(text, serials, machine_ids):
    out = MACHINE_ID.sub(lambda m: m.group(1) + machine_ids[m.group(2)] + m.group(3), text)
    for serial, placeholder in serials.items():
        out = re.sub(r'(?<![0-9A-Za-z])' + re.escape(serial) + r'(?![0-9A-Za-z])', placeholder, out)
    out = out.replace(f"/home/{USER}", "/home/operator")
    out = re.sub(r'(?<![0-9A-Za-z])' + re.escape(USER) + r'(?![0-9A-Za-z])', "operator", out)
    return out


def main(paths):
    serials, machine_ids = collect(paths)
    changed = removed = 0
    for root in paths:
        for dirpath, _, files in os.walk(root):
            for name in files:
                p = os.path.join(dirpath, name)
                if name == "lsusb.txt":
                    os.remove(p)
                    removed += 1
                    continue
                try:
                    text = open(p, encoding="utf-8", errors="surrogateescape").read()
                except OSError:
                    continue
                new = rewrite(text, serials, machine_ids)
                if new != text:
                    open(p, "w", encoding="utf-8", errors="surrogateescape").write(new)
                    changed += 1
    print(f"serials: {serials}")
    print(f"machine ids: {len(machine_ids)} rewritten")
    print(f"files changed: {changed}, lsusb files removed: {removed}")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    main(sys.argv[1:])
