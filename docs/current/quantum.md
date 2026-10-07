# Quantum

Patchbay's quantum control sets PipeWire's forced quantum, `clock.force-quantum` in the `settings` metadata. It is one of only two ways Patchbay changes the running system, alongside patching, and it acts once, on the operator's choice. Nothing is reapplied at startup, and closing Patchbay leaves the setting as it is.

## Scope

The control changes the graph's processing size for every client on a driver, not a private REAPER buffer. Its cycle time (quantum ÷ rate) is not input-to-output latency. The status strip carries this sentence beneath the control:

> quantum is the PipeWire graph's processing size for every client on a driver, not a private REAPER buffer; cycle time is not input-to-output latency

**Automatic** writes 0, which releases the override; it does not restore a previous value. Clients can still force their own quantum: under `pw-jack -p64`, REAPER's node carries `node.force-quantum = 64` and `node.lock-quantum = true`. The quantum probes in `samples/*quantum-probe-256*` recorded what follows:

- a global forced quantum of 256 wins over REAPER's per-node 64 while REAPER runs;
- releasing it returns the driver to REAPER's forced value, not to the 1024 default;
- follower streams can xrun across a switch.

So switching is not glitch-free.

## The control

The status strip's quantum row offers **automatic**, then the powers of two from `clock.min-quantum` to `clock.max-quantum` as the settings metadata reports them. Choosing a different value posts `SetForceQuantum(frames)`.

Three kinds of line sit beneath it, and none is inferred from another:

- **requested override:** the override as the settings metadata shows it, so an override set by another tool (`pw-metadata`) shows too. When the metadata does not show `clock.force-quantum` at all, the line says the override is unknown rather than reading the absence as automatic, and so does the control: it shows `unknown`, and any choice posts a request, automatic included. The control stays enabled, because setting the key creates it and the request is still confirmed by its echo;
- **observed:** for each driver with running followers, the profiler's quantum and rate and its cycle in milliseconds (`observed: 'alsa_output.…' runs 256 frames at 48000 Hz (5.33 ms cycle)`);
- **the scope sentence.**

The requested override and the observed quantum are separate facts. A driver can run at a client-forced value with no override requested, or at 256 while REAPER asked for 64.

While the connection is not live, the strip says `quantum and monitoring: not connected`, and nothing about the quantum is shown as current. In sample mode it says that nothing is live and offers no control.

## The request

`SetForceQuantum(frames)` is a backend request like a link request (`patching.md`). It is not bound to a connection session, since it names no serial.

- The graph writes `clock.force-quantum` on the bound `settings` metadata.
- The request is confirmed when the metadata's property event echoes the requested value.
- If the metadata already shows the value, the request is confirmed at once from what the metadata says.
- It fails if no settings metadata is observed, or if the echo does not arrive within two seconds.

Pending and failed quantum requests are listed in the status strip and the inspector like any other request.
