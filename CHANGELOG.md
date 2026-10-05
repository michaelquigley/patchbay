# CHANGELOG

## Unreleased

FEATURE: `patchbay dump` prints the live PipeWire graph keyed by object serial and reprints it on every change, riding through daemon restarts with a reconnect backoff. `--sample <dir>` prints a captured `pw-dump.json` through the same model instead.
