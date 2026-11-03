# wg-cp0 peer lifecycle — onboarding, rotation, drift recovery

This is the operational substrate for adding, rotating, and reconciling peers on the wg-cp0 silent control plane. Companion to `docs/design/wg-cp0-prod.md` (which covers the day-1 topology + bundle format) and `docs/operations/wg-cp0-bundle-ceremony.md` (per-device issuance steps).

Anchors ADR 0316, [ADR 0313](../adr/0313-silent-control-plane-outer-wg-wrap.md), `docs/findings/F-114-wg-cp0-rotation-serialization-gap.md`. Starter implementation of Block 50J (the peer-allowlist surgical API).

## Problem the surgical-API solves

Before this, the only operational tool for changing a relay's wg-cp0 peer set was [`wg-cp0-bootstrap.yml`](../../ops/ansible/wg-cp0-bootstrap.yml). That play has a `restart wg-cp0` handler that fires on every allowlist push — **fleet-disruptive**: every wg-cp0 client across the fleet must re-handshake from scratch every time one peer is added.

The rotation ceremony is three loosely-coupled steps:

1. `bundle-create --update-allowlist` writes the JSON-of-record on the operator laptop.
2. `wg-cp0-bundle-apply` on the device writes a new `/etc/wireguard/wg-cp0.conf` with a new Curve25519 keypair.
3. `wg-cp0-bootstrap.yml` push propagates the JSON to relays.

If step 2 runs without step 3 (or with stale state), the device's on-disk private key drifts from the relay-side expected pubkey. Worse: if `wg-quick@wg-cp0` is NOT restarted on the device after step 2, the **running daemon keeps the OLD in-memory key**, which matches the relay's old expected pubkey, so handshakes continue to work and the drift is INVISIBLE until the daemon is eventually restarted. See F-114.

## Surgical API — primary surface

[`ops/ansible/wg-cp0-peer-surgical-update.yml`](../../ops/ansible/wg-cp0-peer-surgical-update.yml) is the primary tool for per-peer changes. It runs against `wg_cp0_relay_hosts` and per relay:

1. Removes the OLD peer pubkey (if specified, idempotent).
2. Adds/updates the NEW peer pubkey with `allowed-ips` + `persistent-keepalive`.
3. `wg-quick save wg-cp0` to persist the change to `/etc/wireguard/wg-cp0.conf`.

Crucially **no `restart wg-cp0` handler**. The kernel module / userspace daemon stays running. Existing peer sessions (other peers' handshakes) are not disturbed. The change is observable on each relay within seconds; affected peer re-handshakes via persistent-keepalive immediately.

## Three lifecycle paths

### A. New peer onboarding

A peer that has never been on wg-cp0 before. Four-step orchestrator:

1. **Operator side:** `bundle-create --site <site> --device-id <id> --wg-cp0-address <ip>/24 --wg-cp0-relays <relays.json> --acl-groups <groups> --ca-id <ca> --mode dev --ca-key <key.age> --age-identity <id.txt> --tls-trust-anchor <root.pem> [--first-relay-route-subnet 198.18.0.0/24] --update-allowlist ops/enrollment/wg-cp0-bundle-allowlist.json --out <bundle.cbor>`.
2. **Operator side:** deliver `bundle.cbor` to the device.
3. **Device side:** `wg-cp0-bundle-apply --in <bundle.cbor> --ca-pubkey <pem> --out-conf /etc/wireguard/wg-cp0.conf --out-privkey /etc/wireguard/wg-cp0-priv.key` then `systemctl restart wg-quick@wg-cp0` (or `--reload` if available).
4. **Operator side:** `wg-cp0-peer-surgical-update.yml -e peer_name=<id> -e peer_new_pubkey=<from bundle> -e peer_allowed_ips=<ip>/32` (no `peer_old_pubkey` since this is brand new).

Today these four steps are operator-driven. Future Block 50J substrate work folds them into one orchestrator (`ops/scripts/wg-cp0-onboard-peer.sh`) with fail-loud assertions at each step.

### B. Existing peer rotation

A peer whose Curve25519 keypair needs to roll (compromise response, scheduled rotation, post-OS-reinstall). Same as onboarding but with `peer_old_pubkey` set in step 4 so the old entry is removed atomically:

1. `bundle-create ... --update-allowlist ...` minting a fresh keypair.
2. Deliver new `bundle.cbor` to device.
3. `wg-cp0-bundle-apply ...` + **`systemctl restart wg-quick@wg-cp0`**
4. `wg-cp0-peer-surgical-update.yml -e peer_name=<id> -e peer_new_pubkey=<NEW from bundle> -e peer_old_pubkey=<OLD from prior bundle> -e peer_allowed_ips=<ip>/32`.

Step 3's restart is non-negotiable. Skipping it leaves the device in a zombie state: the running daemon keeps the old key.

### C. Drift recovery (reconciliation)

For a fleet that's already drifted: master JSON-of-record disagrees with what's actually on the devices.

1. **Diagnose:** for each suspect peer, SSH in (via LAN, ProxyJump, or any reachable path) and run `sudo wg show wg-cp0 | head -3` to get the actual `public key`. Compare against the master allowlist JSON.
2. **Per drifted peer, run surgical update:** `wg-cp0-peer-surgical-update.yml -e peer_new_pubkey=<actual on-device key> -e peer_old_pubkey=<stale allowlist key> -e peer_allowed_ips=<ip>/32`.
3. **Update master JSON:** patch `ops/enrollment/wg-cp0-bundle-allowlist.json` with the new pubkey so the JSON-of-record matches the live state.
4. Commit the JSON change with a brief reason (rotation reconciliation).

## Drift-detection — bundle-allowlist diff

`docs/operations/wg-cp0-drift-audit.md` (queued substrate work) is the runbook for a cron-grade audit. The check shape:

1. SSH each device, read `sudo wg show wg-cp0 | head -3` → get current public key.
2. Compare against `ops/enrollment/wg-cp0-bundle-allowlist.json[<address>].pubkey`.
3. Any mismatch = alert.

This catches Path B and Path C drift on day 1 instead of day N. Today's mechanism (operator does the diff manually after observing a failure) is reactive; the audit is proactive.

## Liveness — handshake-age probe

The wg-show output's `latest handshake` field is the load-bearing signal. If any peer's last handshake is older than ~2× persistent-keepalive (50s today, so call it `>= 90s`), something is wrong: the peer's wg-cp0 socket is wedged, the WAN dropped, or there's pubkey drift. Snitch-side probe extension (filed as a follow-on to Block 61, sibling to today's surgical playbook) reports any peer with stale handshakes and emits to the observability stack.

Critical caveat: `wg show` reports the PEER's perspective. A relay's wg-show entry for a peer with no `latest handshake` line means that peer has never handshaked since the relay's wg-cp0 last restarted. A peer's wg-show entry for a relay with `latest handshake: 1 hour ago` means the peer hasn't successfully handshaked in an hour, likely because of drift or wedge.

## Substrate default: `wg syncconf` for allowlist-only changes (Block 22R.3, landed 2026-05-13)

[`ops/ansible/wg-cp0-bootstrap.yml`](../../ops/ansible/wg-cp0-bootstrap.yml) Pass 2A used to fire the `restart wg-cp0` handler unconditionally on any config render change, including pure allowlist-only updates — the wrong default, because allowlist changes do not require a listener restart and `restart wg-cp0` drops every peer's session fleet-wide on every allowlist push.

The [`wg-cp0-server` role](../../ops/ansible/roles/wg-cp0-server/) now classifies the diff between the live conf and the freshly-rendered staged conf and notifies the right handler:

| Change shape | Detector | Handler notified | Effect |
| --- | --- | --- | --- |
| Peer added / removed / pubkey rotated / allowed-ips changed | `[Peer]` stanzas differ; `[Interface]` identical | `reload wg-cp0 peers` (`wg syncconf wg-cp0 <(wg-quick strip wg-cp0)`) | In-place peer-set reload. Pre-existing handshaked sessions preserved. |
| Listener-shape change (PrivateKey / Address / ListenPort / MTU) | `[Interface]` stanza differs | `restart wg-cp0` (`systemctl restart wg-quick@wg-cp0`) | Full restart. Every peer re-handshakes. |
| First install (no live conf) | live conf absent | `restart wg-cp0` | wg-quick brings the interface up. |
| Cosmetic-only (whitespace / comments) | Both stanzas byte-identical post-normalization | none | No handler fires. |

The classifier is at [`ops/ansible/roles/wg-cp0-server/files/wg-cp0-classify-conf-change.sh`](../../ops/ansible/roles/wg-cp0-server/files/wg-cp0-classify-conf-change.sh) (~50 lines of bash, parses `/etc/wireguard/wg-cp0.conf`-shaped INI files); unit coverage at [`ops/ansible/roles/wg-cp0-server/tests/test-classify-conf-change.sh`](../../ops/ansible/roles/wg-cp0-server/tests/test-classify-conf-change.sh) with 11 fixture confs covering the cross-product of peer-set / listener-shape / cosmetic / reverse-direction changes.

Behavioral verification that `wg syncconf` actually preserves handshakes on a live interface is the bench-env scope at [`ops/wg-cp0-mesh-reach/run-handshake-preservation.sh`](../../ops/wg-cp0-mesh-reach/) — a scaffolded operator-fired driver that runs the cross-product on docker-compose containers (Linux host + WireGuard kernel module required; outside the per-PR CI tier). Driver is queued as substrate-build follow-on; the unit-scope classifier suite is the immediate CI gate.

The sequencing for both post-merge evidence steps — first real prod relay re-run AND bench-env driver build — lives at `docs/operations/wg-cp0-22r3-post-merge-evidence.md`. That runbook covers pre-flight, snapshot commands, the assertion shape, the revert path on regression, and which step is independent of which.

## Amendment 2026-05-12 — every-relay route-subnet semantics

Follow-on to F-114: `bundle-create`'s `--first-relay-route-subnet` flag is **renamed to `--mesh-route-subnet`** and now applies the override to **every** relay in the bundle, not just the first. The old flag name is preserved as a deprecated alias for back-compat — existing operator scripts continue to produce working bundles — but the pre-amendment "first-relay-only" shape is no longer reachable.

### Why

Pre-amendment, the override on `KnownEndpoint[0].AllowedIPs` made only the first relay a valid forwarder for the listed subnets. That created a receive-side asymmetry on multi-peer consumers (mgmt-host Orins via `wg-cp0-bundle-apply` — every production mgmt-host today): a packet from peer X with source IP in the override subnet arriving via any relay OTHER than the consumer's first was silently dropped at the WireGuard cryptokey layer, because that relay's AllowedIPs on the consumer's side was just `MeshAddr/32` — the source IP didn't match. The drop was silent at the wire level (no ICMP, just `i/o timeout` on the sender).

Amending the substrate eliminates the entire class — relay ordering no longer carries routing semantics on the receive side; every relay is equally valid.

### Effective behavior

When a bundle is minted with `--mesh-route-subnet 198.18.0.0/24` (or the deprecated alias):

- Every `KnownEndpoint` in the bundle has `AllowedIPs = [MeshAddr/32, 198.18.0.0/24]`.
- `wg-cp0-bundle-apply` renders the device's `wg-cp0.conf` with `AllowedIPs = X.X.X.X/32, 198.18.0.0/24` on every `[Peer]` stanza.
- Receive-side: a packet from any wg-cp0 peer arriving via ANY relay's tunnel is accepted by the consumer's WG, because every relay-peer's AllowedIPs covers the /24.
- Send-side: kernel still picks the first peer in conf order for outbound (longest-prefix tie-break is "first match wins" per the wireguard-tools manpage), so outbound takes a deterministic relay. The asymmetry no longer matters because the receive side accepts traffic regardless of which path was taken.

### What does NOT change

- The send-side single-peer model in goat-client (`tunnel.FromBundle`) is unaffected; F-113 closure (multi-peer FromBundle) is still the path for goat-client peers. After F-113 lands, the receive-side fix here composes with the send-side fix there for full symmetry.
- WireGuard's "first match wins on overlapping prefixes" send semantics — same as before. Just doesn't produce asymmetry anymore.
- Existing bundles in the field — keep working. No mandatory re-mint. Opportunistic re-mint at next rotation cycle picks up the improved shape.

### Migration path

| Operator surface | Action |
| --- | --- |
| `--first-relay-route-subnet=…` callers | None — the deprecated alias keeps producing valid bundles with the new shape. Switch to `--mesh-route-subnet` at next rotation cycle. |
| `wg-cp0-relays-prod.json` static file | Ordering no longer matters for routing. Operator can still curate the file for human readability; the bundle no longer cares which relay is first. |
| `wg-cp0-rotate-bundles.yml` Block 22P orchestrator | No change. The "active forwarder" still gets some kernel-routing preference via peer order in the rendered conf, but other relays are no longer second-class for `/24` traffic. |
| In-the-field bundles | Stay valid; will continue to work as today. Re-mint with new flag at next 90-day rotation. |

### Tests

`TestRun_MeshRouteSubnet` + `TestRun_FirstRelayRouteSubnet_BackCompat` in `ops/enrollment/cmd/bundle-create/main_test.go` cover both flag paths and assert every endpoint carries the override. `TestRun_RejectsInvalidMeshRouteSubnet` exercises typo-validation. `TestParseMeshRouteSubnets` covers the parser directly.

## Cross-refs

- F-114 wg-cp0 rotation serialization gap — the finding driving this design
- [`ops/ansible/wg-cp0-peer-surgical-update.yml`](../../ops/ansible/wg-cp0-peer-surgical-update.yml) — surgical API playbook
- `docs/operations/wg-cp0-bundle-ceremony.md` — per-device issuance (still authoritative for steps 1-3 of onboarding/rotation)
- `docs/design/wg-cp0-prod.md` — day-1 topology + key model
- `docs/project/implementation-plan.md` Block 22R.3 — `wg syncconf` substrate (landed 2026-05-13); Block 50J — remaining cluster items (orchestrator helper + cron drift-audit + snitch-side liveness probe)
