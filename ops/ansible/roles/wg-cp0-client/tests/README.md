# wg-cp0-client role docker-fixture test

Block 22M follow-on. Validates the `wg-cp0-client` Ansible role's bundle-flow branch (Block 22K F-053 closure) end-to-end against a clean Ubuntu container without needing real WireGuard tooling, a running systemd, or live mesh peers.

Complements the CLI-level integration test at [`ops/enrollment/cmd/wg-cp0-bundle-apply/integration_test.go`](../../../../enrollment/cmd/wg-cp0-bundle-apply/integration_test.go) which validates the binary surface; this fixture validates the role's plumbing on top of that surface — task ordering, file-mode discipline, the bundle-mode-vs-legacy-mode `when:` branching, atomic write semantics, idempotency, shred-after-apply.

## What it tests

- The role's bundle-rooted block (`when: wg_cp0_use_bundle`) runs end-to-end without errors against a clean Ubuntu 24.04 container
- Pre-built `wg-cp0-bundle-apply` binary delivery via `ansible.builtin.copy` (cross-compiled for the container's arch via `GOOS=linux GOARCH=...`)
- CA pubkey delivery to `/etc/goat/ca.pub` (mode 0644)
- Bundle delivery to `/etc/goat/bundle.cbor` (mode 0600, `no_log: true`)
- `wg-cp0-bundle-apply` invocation produces:
  - `/etc/wireguard/wg-cp0.conf` (mode 0600) with the expected `[Interface]` + `[Peer]` structure including `--first-relay-route-subnet` AllowedIPs override
  - `/etc/wireguard/wg-cp0-priv.key` (mode 0600) matching the conf's `PrivateKey =` line
- Bundle is shredded after apply (`/etc/goat/bundle.cbor` no longer exists)
- Re-running the playbook is idempotent (no errors, files still present)

## What it skips

The role's `wg_cp0_test_mode: true` var (defaulted false in [`defaults/main.yml`](../defaults/main.yml)) gates these tasks so the role runs to completion in a container without:

- `apt-get install wireguard wireguard-tools / wireguard-go`
- The Tegra kernel-module probe + userspace-fallback systemd drop-in
- `systemctl enable + start wg-quick@wg-cp0`
- The `restart wg-cp0` handler
- The `wg show wg-cp0` smoke + status print

These are all production-side concerns that real WireGuard + systemd handle. The fixture intentionally exercises the role's role-specific logic (delivery, render, shred, idempotency) and not the `apt` / `systemd` orchestration around it.

## Prerequisites

The harness auto-skips with exit 0 when any of these are missing:

- `docker` (with daemon reachable)
- `ansible-playbook`
- `go` (to build the binary for the container's arch)
- `age-keygen` (to generate the ephemeral CA — `brew install age` on macOS, `apt install age` on Debian/Ubuntu)
- `python3` (used to compose the relays-roster JSON fixture)

## Run

```bash
ops/ansible/roles/wg-cp0-client/tests/role-fixture.sh
```

Expected output: `wg-cp0-client role-fixture: PASS` on success. Takes ~30–60 seconds depending on docker pull + apt install latency inside the container.

## Debug a failure

```bash
GOAT_FIXTURE_KEEP=1 ops/ansible/roles/wg-cp0-client/tests/role-fixture.sh
```

Leaves the container + workdir behind for post-mortem inspection. Re-run the failing step manually:

```bash
# Find the most recent fixture container
CONTAINER=$(docker ps -aq --filter "name=wg-cp0-client-fixture-" --latest)

# Inspect bundle / binary / output state
docker exec "$CONTAINER" ls -la /etc/goat/ /etc/wireguard/ /usr/local/bin/wg-cp0-bundle-apply

# Re-invoke the apply tool manually
docker exec "$CONTAINER" /usr/local/bin/wg-cp0-bundle-apply \
    --in /etc/goat/bundle.cbor \
    --ca-pubkey /etc/goat/ca.pub \
    --out-conf /etc/wireguard/wg-cp0.conf \
    --out-privkey /etc/wireguard/wg-cp0-priv.key \
    --interface-name wg-cp0 \
    --persistent-keepalive 25

# Cleanup when done
docker rm -f "$CONTAINER"
```

## Known gaps

- **Legacy-flow branch** (`wg_cp0_legacy_local_keygen=true`) is NOT covered by this fixture today. Validating the legacy branch requires `wireguard-tools` inside the container to provide `wg genkey` / `wg pubkey`. The legacy flow's behavior is preserved by existing deployments until Block 22K.4 retires it; adding a dedicated fixture is queued as a follow-on.
- **Real systemd / wg-quick interaction** is intentionally out of scope. Production deployments exercise this on every Ansible run; CI's role-level concern is whether the role's own logic is correct, which this fixture covers.

## Hooking into CI

Once a CI runner with docker is available, add to the test pipeline:

```yaml
- name: wg-cp0-client role-fixture
  run: ops/ansible/roles/wg-cp0-client/tests/role-fixture.sh
```

The harness's auto-skip discipline keeps the gate non-blocking on environments without docker. A green fixture run + green Go integration test = full vertical coverage from CLI flags down to rendered config files.
