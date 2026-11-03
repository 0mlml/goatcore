# Offline Enrollment Bundle Runbook

Operational procedures for the offline CA, bundle issuance, revocation, and key ceremony. Design doc: [`docs/design/offline-enrollment.md`](../design/offline-enrollment.md). Work tracking: `docs/project/implementation-plan.md` Block 2C.

> **Scope:** x86 hosts + arm64 edge hosts. RPi is **not** a target.

---

## Quick reference

| Item | Value |
|---|---|
| CA signing key (production) | Ed25519 on YubiKey PIV slot 9c, touch policy "always", PIN required, firmware 5.7.0+ |
| CA signing key (dev fallback, pre-YubiKey) | Ed25519 in age-encrypted file at `ops/enrollment/ca/dev/ca.key.age`. `ca_id` carries `dev-` prefix. |
| Backup CA key | Second YubiKey with identical material, stored separately (production only) |
| CA public key(s) | `ops/enrollment/public-keys/*.pem` (committed). Validator trusts all present. |
| Bundle format | CBOR, ~400-600 bytes, Ed25519 signature |
| Bundle activation deadline | 14 days (first enrollment must happen before this) |
| Bundle expiry (peer record horizon) | 90 days |
| Device registry | `ops/enrollment/registry.jsonl` (append-only, committed). States: `issued` → `in_transit` → `delivered` → `enrolled`; or `suspected_lost`. |
| Validator service | `enrollment-validator.service` on each management host |
| Validator endpoint | `https://netbird.legacy.example/enrollment/validate` (mesh-only via Traefik `IPAllowList 100.64.0.0/10`) |
| CRL source (Phase 2A) | Single-source HTTP pull from issuance endpoint (`GET /enrollment/crl`, mesh-only) |
| CRL source (Phase 3) | Postgres table `enrollment_crl`, Spock-replicated (migration after HA multi-site lands) |
| CRL refresh interval | 60s (validator polls) |
| Fail-closed threshold | 72h without CRL refresh (warn at 24h) |
| Rate limit (per source IP) | 10 enrollment attempts/minute |
| Rate limit (per bundle nonce) | 3 attempts/hour |
| Circuit breaker threshold | >50 invalid signatures in 5 minutes → 10min cooldown |
| Auto-revoke-no-show cron | Daily on CA host; revokes bundles past `activation_deadline` in state `issued`/`in_transit` |

---

## Dev-mode CA (pre-YubiKey, testing only)

**Use case:** Until YubiKey 5.7.0+ hardware is on hand, dev mode unblocks end-to-end testing of the bundle format, validator, CRL, and device consumer while preserving identical protocol behavior.

**One-time dev CA setup:**

```
cd ops/enrollment/ca
./ca-init.sh --mode dev --recipients <age-recipient-1>,<age-recipient-2>
# Generates Ed25519 keypair, encrypts private key with age, writes to ca/dev/ca.key.age
# Exports public key to ops/enrollment/public-keys/dev-goat-ca-<date>.pem
# Commits both (the private key is encrypted; the public key is meant to be distributed)
```

**Issue bundles in dev mode:**

```
./issue-bundle.sh --mode dev \
  --device orin-<site>-01.c2 \
  --site <site>-cp \
  --acl-groups c2 \
  --endpoints relay-1.legacy.example:51820 \
  --out bundles/orin-<site>-01.cbor
# Prompts for age passphrase
# Signs bundle with `ca_id = dev-goat-ca-<date>`
```

**Gate to switch off dev mode — all must be true:**

- YubiKey hardware received
- Firmware 5.7.0+ verified (`ykman info` shows `Firmware version: 5.7.x`)
- Production ceremony dry-run completed (throwaway keys, §7.2)
- Production CA public key distributed to all management hosts via Ansible

**Switchover procedure:**

1. Run production ceremony §7.2 with real YubiKeys, generating new `ca_id` without `dev-` prefix.
2. Distribute new public key to all management hosts (Ansible push).
3. Verify: issue a test bundle with the production CA, enroll a test device, confirm success.
4. Re-issue all active bundles with production CA (devices in the field get replacement bundles on their next scheduled update).
5. After 7-day overlap, remove `dev-goat-ca-*.pem` from `ops/enrollment/public-keys/`, run Ansible to propagate the removal.
6. Delete `ops/enrollment/ca/dev/ca.key.age` from the working tree (history preserves the record).
7. Add a CI check that rejects any PR invoking `--mode dev`: `grep -r "mode dev" ops/enrollment/ && exit 1`.

**What dev mode is NOT for:**

- Shipping bundles to any production site.
- Issuing bundles to devices that will leave the lab.
- Any bundle whose theft window would matter operationally.

---

## Initial CA key ceremony (one-time, air-gapped)

Performed once at project inception. Repeated only on CA rotation or compromise.

**Prerequisites:**

- Air-gapped workstation (Tails USB, no persistent storage, no network)
- Two YubiKeys (firmware 5.7.0+ required for Ed25519 PIV support — verify with `ykman info`)
- Trusted operator(s), physically present
- Clean USB stick for public key export

**Procedure:**

1. Boot workstation from trusted USB; verify no network interfaces are up (`ip link | grep -v 'lo:'`)
2. Insert both YubiKeys
3. On each YubiKey: `ykman piv access change-pin` → set PIN; `ykman piv access change-puk` → set PUK; `ykman piv access change-management-key` → generate new mgmt key, record in sealed envelope
4. On YubiKey A: `ykman piv keys generate --algorithm ED25519 --pin-policy ALWAYS --touch-policy ALWAYS 9c -` → emits public key to stdout
5. Clone private key to YubiKey B — **this requires the YubiKey attestation + backup procedure from YubiKey docs; do not skip**
6. Export public key: `ykman piv certificates export 9c ca-pub.pem` on each YubiKey; diff to confirm identical
7. Copy `ca-pub.pem` to clean USB stick; rename to `goat-ca-YYYY-MM.pem`
8. Record `ca_id = goat-ca-YYYY-MM` in sealed envelope alongside management key
9. Remove both YubiKeys. Key A to primary issuing location. Key B to backup custody (different operator, different location)
10. Workstation wiped (Tails does this automatically on shutdown)
11. Transfer `goat-ca-YYYY-MM.pem` from USB stick into the repo at `ops/enrollment/public-keys/`; commit and push
12. Ansible distributes the public key to all management hosts on next run

**Do not skip the dry run.** Perform steps 1-10 once with throwaway keys before the real ceremony. Ed25519 on YubiKey PIV is newer than the rest of the PIV stack and has edge cases around `pin-policy=ALWAYS` + firmware versions.

---

## Bundle lifecycle tracking (issuance registry)

Every issued bundle is recorded in `ops/enrollment/registry.jsonl` with its current lifecycle state. Operators update state as the bundle moves:

**States:**

| State | Meaning | Transitions from |
|---|---|---|
| `issued` | CA signed the bundle, it exists on the issuing operator's workstation | (new) |
| `in_transit` | Bundle has left the issuing operator's hands for delivery to the forward site | `issued` |
| `delivered` | Bundle has been confirmed received at the destination site | `in_transit` |
| `enrolled` | Device successfully enrolled using this bundle (validator observed the enrollment) | `delivered` or `in_transit` (if shipped and enrolled before confirmation) |
| `suspected_lost` | Operator has a reason to believe the bundle is compromised or missing | any |

**State transitions:**

```
./bundle-state.sh --nonce <nonce-hex> --state delivered --recipient <operator-id> --notes "received at <site>, <date>"
# Appends entry to registry.jsonl; no mutation of prior records
```

**Operational cadence:**

- **Issuer commits `issued` state at bundle generation time** (automatic in `issue-bundle.sh`)
- **Issuer transitions to `in_transit` when bundle leaves their workstation for delivery** (explicit operator action)
- **Forward operator transitions to `delivered` within 72 hours of receipt** (operational requirement, not enforced by code)
- **Validator sidecar transitions to `enrolled` automatically on successful first enrollment** (writes a registry update event)

**Audit:** state transitions are append-only. Bundle that has been in state `issued` or `in_transit` longer than 7 days triggers an operator reminder via Prometheus alert (`enrollment_bundle_transit_stale`). Bundle past its activation deadline in those states is auto-revoked by the daily cron (see below).

**Daily auto-revoke cron (runs on CA host):**

```
# /etc/cron.d/enrollment-no-show-revoke
0 2 * * * goat /opt/enrollment/bin/auto-revoke-no-show.sh
```

Checks all bundles where `state in {issued, in_transit}` AND `now() > activation_deadline`. Each matching bundle gets a CRL entry with reason `expired-no-show`. Log file at `/var/log/enrollment/auto-revoke.log`. Prometheus metric `enrollment_auto_revoked_total` incremented.

---

## Issue a bundle

Routine operation. Can be performed on a developer workstation with the issuing YubiKey inserted.

```
cd ops/enrollment
./issue-bundle.sh \
  --device orin-<site>-01.c2 \
  --site <site>-cp \
  --acl-groups c2 \
  --endpoints relay-1.legacy.example:51820,relay-2.legacy.example:51820,<site>-lan-peer:51820 \
  --out bundles/orin-<site>-01.cbor
```

YubiKey prompts for PIN, then touch. Outputs:

- `bundles/orin-<site>-01.cbor` — the bundle, delivered to the device provisioning image
- `bundles/orin-<site>-01.wg.key` — the generated WG private key for the device
- Appends an entry to `ops/enrollment/registry.jsonl`

**Delivery paths:**

- **Ubuntu-for-Tegra (Orin):** Place bundle at `/etc/goat/enrollment/enrollment.cbor` and private key at `/etc/goat/enrollment/enrollment.wg.key` via cloud-init seed on the first-boot image (see [`../design/orin-os-decision.md`](../design/orin-os-decision.md))
- **Ubuntu (x86):** Place in `/etc/goat/enrollment/` before first boot, referenced from cloud-init
- **Air-gapped provisioning:** USB stick with both files; device's systemd oneshot reads them on first boot

---

## Revoke a bundle

Triggered by: device lost, compromised, decommissioned, or re-issued.

```
cd ops/enrollment
./revoke-bundle.sh \
  --device orin-<site>-01.c2 \
  --reason compromised
```

This:

1. Looks up the bundle's nonce in `registry.jsonl`
2. Signs a CRL delta with the active YubiKey
3. Writes the delta to `crl/pending/`
4. Operator pushes the delta to any reachable management server (HTTPS POST to `/enrollment/crl/publish`)
5. Management server appends to `enrollment_crl` Postgres table
6. Spock replicates to all sites within seconds-to-minutes (depending on WAN)
7. Validator sidecars pick up the CRL delta within 60s

**If the bundle was already used to enroll an active peer:** bundle revocation stops *new* enrollments but does not disable the active peer. To disable the peer, run `netbird-revoke <peer_id>` on the management server. Separation is deliberate — see design doc §5.4.

**Periodic revocation drill:** run a revocation against a test bundle once a month. Verify CRL propagation time, validator refresh, enrollment rejection. Log results.

---

## Rotate the CA (annual or on compromise)

Annual rotation is scheduled; compromise rotation is triggered.

1. Perform ceremony "Initial CA key ceremony" above with new YubiKey pair
2. New `ca_id = goat-ca-<new-date>` and new public key `goat-ca-<new-date>.pem`
3. Distribute new public key via Ansible to all management hosts (validator accepts bundles signed by *any* CA in `ops/enrollment/public-keys/`)
4. New bundles signed with new key; old bundles remain valid until their natural expiry
5. After 90 days (longest possible old-bundle lifetime), remove old CA public key from `ops/enrollment/public-keys/` and re-run Ansible
6. Archive old YubiKeys in sealed tamper-evident bag; destroy per security policy after retention period

---

## Compromise response (CA key lost or suspected compromised)

**Do immediately (within 1 hour):**

1. Bulk-revoke all outstanding bundles: `./revoke-all.sh --ca-id goat-ca-<old-date> --reason ca-compromise`
2. Push CRL to all reachable management servers
3. Alert all operators that no new bundle enrollments will succeed until new CA is online

**Within 24 hours:**

1. Perform the initial CA ceremony with new YubiKey pair and new `ca_id`
2. Distribute new public key
3. Verify: issue and enroll a test bundle against the new CA

**Within 72 hours:**

1. Re-issue bundles for all active devices with the new CA
2. Deliver new bundles to devices via whatever path is available (ideally out-of-band provisioning rather than mesh, but mesh works if the peer is already enrolled)
3. Confirm each device successfully uses the new bundle when its existing peer record is next updated

**Within 1 week:**

1. Remove old CA public key from `ops/enrollment/public-keys/`
2. Post-mortem: how was the key lost/compromised? What changes to ceremony or custody?

---

## Validator rate limit and abuse tuning

The validator has layered DoS defenses; operators may need to tune rate limits if legitimate traffic patterns change.

**Current defaults (set in `ops/ansible/roles/enrollment-validator/defaults/main.yml`):**

| Setting | Default | Raise if... | Lower if... |
|---|---|---|---|
| `rate_limit_per_source_ip_per_min` | 10 | Legitimate Island-Mode enrollment needs more tries (rare) | Operator sees elevated 429 responses from known-good sources |
| `rate_limit_per_nonce_per_hour` | 3 | Bundle retry logic evolves to need more attempts | Same-nonce hammering pattern observed |
| `slow_reject_delay_ms` | 200 | Attacker cost needs to rise further | Legitimate clients timing out on bad-signature recovery (unlikely) |
| `circuit_breaker_threshold` | 50 invalid/5min | Noise from known benign sources (e.g., load testing) | Real attack patterns observed below current threshold |
| `circuit_breaker_cooldown_min` | 10 | Ongoing attack continues past cooldown | Legitimate traffic starved for too long during incidents |
| `crl_stale_warn_hours` | 24 | — (keep) | — (keep) |
| `crl_stale_fail_closed_hours` | 72 | Site is regularly disconnected longer than 72h | Threat model demands tighter staleness |

**Changing a rate limit:**

1. Update the value in `ops/ansible/roles/enrollment-validator/defaults/main.yml` or per-host `host_vars/<host>.yml` for site-specific overrides.
2. Run `ansible-playbook` against the affected validator hosts.
3. Validator reloads on SIGHUP; no downtime.
4. Verify metrics after 15 minutes — `enrollment_rate_limited_total` should match expectation.

**When the circuit breaker fires:**

- Prometheus alert `enrollment_circuit_breaker_active` fires immediately.
- Operator investigates: is it a misconfigured device retrying on a rejected bundle? An active attack? A load test that forgot to coordinate?
- Cooldown auto-releases after 10 minutes. During cooldown, all enrollment returns HTTP 503. Existing enrolled peers are unaffected.
- Post-incident: review `enrollment_signature_invalid_total` breakdown by source IP and `ca_id` to identify the offender.

**NTP-class fail-closed:**

The validator refuses to issue enrollment timestamps if its local NTP class is below threshold per the universal NTP hygiene policy (doc path: `docs/design/universal-ntp-hygiene.md` — landing from a parallel workstream; may not yet be on origin/main). If the validator is rejecting enrollments with `ntp_class_insufficient`, fix NTP before tuning the enrollment layer — timestamp integrity is a prerequisite.

---

## Validator sidecar operations

Standard service-operations mechanics. Runs on each management host.

```
# Status
systemctl status enrollment-validator

# Logs
journalctl -u enrollment-validator -f

# Metrics
curl http://localhost:9100/metrics | grep enrollment_

# CRL refresh status
curl http://localhost:9100/metrics | grep crl_last_refresh_seconds
```

**Key metrics to alert on:**

| Metric | Alert condition |
|---|---|
| `crl_last_refresh_seconds` | > 3600 (warn), > 86400 (page) |
| `enrollment_signature_invalid_total` | any increase (page — possible attack) |
| `enrollment_unknown_ca_total` | any increase (page — possible misprovisioned device or attack) |
| `enrollment_success_total` | absence of expected traffic during known enrollment windows |

---

## References

- Design: [`docs/design/offline-enrollment.md`](../design/offline-enrollment.md)
- Work tracking: `docs/project/implementation-plan.md` Block 2C
- CA tooling source: `ops/enrollment/ca/`
- Validator source: `ops/enrollment/validator/`
- YubiKey PIV Ed25519 requires firmware 5.7.0+: https://www.yubico.com/blog/
