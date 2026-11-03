# wg-cp0-server role tests

Two harnesses, both run by `./syntax-check.sh`:

1. **Parse-only smoke** (`playbook.yml`) — ansible-playbook `--syntax-check`
   that validates YAML + Jinja2 of the role surface. Same coverage as the
   orchestrator-wide gate at
   [`ops/ansible/tests/wg-cp0-syntax-check.sh`](../../../tests/wg-cp0-syntax-check.sh).

2. **Classifier unit suite** (`test-classify-conf-change.sh`) — Block 22R.3
   substrate test. Runs the `files/wg-cp0-classify-conf-change.sh` script
   against eleven fixture confs in `fixtures/` and asserts the expected
   classification (first_install / shape_change / peer_change / no_change).
   This is the load-bearing test for the F-114 substrate closure: the
   classifier picks between the new `reload wg-cp0 peers` handler
   (`wg syncconf` — preserves handshakes) and the legacy `restart wg-cp0`
   handler (drops all sessions) based on whether the [Interface] stanza
   changed. Pure bash, runs anywhere — no kernel module or Ansible needed.

## What this does NOT cover

Behavioral verification that `wg syncconf` actually preserves handshakes
on a live interface is operator-fired in the bench-env harness at
[`ops/wg-cp0-mesh-reach/`](../../../../../ops/wg-cp0-mesh-reach/). That
test requires the WireGuard kernel module + privileged containers and is
not portable to macOS / non-Linux dev workstations. The Block 22R.3
acceptance bar — "a peer can be added or removed + verify no existing
peer's `latest handshake` counter resets" — is asserted there.

The classifier suite alone closes the substrate gap that produced F-114
(unconditional restart blast-radius) because it ensures the role notifies
the correct handler for each change shape. The kernel-level proof that
`wg syncconf` preserves sessions is documented behavior of `wireguard-tools`
upstream; the bench-env test promotes it from "documented" to "verified in
our deployment shape."

## Running the tests

```bash
./syntax-check.sh
```

The script skips ansible-playbook if it is not on PATH and still runs the
classifier suite, so a contributor with only bash + diff + awk available
can verify the load-bearing change.
