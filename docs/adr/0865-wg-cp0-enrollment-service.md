# ADR 0865 — Per-relay wg-cp0 enrollment service replaces operator-driven ansible+SSM peer adds

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-31 |
| Repo scope | goat core (new `ops/wg-cp0-enrollment-service/` Go service, new `ops/ansible/roles/wg-cp0-enrollment-service/` deploy role, `ops/enrollment/cmd/bundle-create` decoupling from `--update-allowlist`, `goat-cli admin enroll` subcommand) |
| Supersedes | — |
| Superseded by | — |
| Related | [`0313-silent-control-plane-outer-wg-wrap.md`](0313-silent-control-plane-outer-wg-wrap.md), [`0407-internal-x509-ca-for-service-tls.md`](0407-internal-x509-ca-for-service-tls.md), [`0843-public-mgmt-signal-relay-mobile-crutch.md`](0843-public-mgmt-signal-relay-mobile-crutch.md) (architectural sibling — both turn an operator-fired ansible flow into a deployed authenticated-API service), [`1019-goat-cli-unified-operator-binary.md`](1019-goat-cli-unified-operator-binary.md) (consumes the API via `goat-cli admin enroll`) |

## TL;DR (for a layperson)

Today, adding a new computer to our wg-cp0 mesh requires an operator at a laptop with special credentials to mint a bundle, manually edit a JSON file, and run a fragile ansible playbook over AWS SSM against every relay. The ansible playbook contains client-side housekeeping that doesn't apply to relays, so even running it the "right" way produces confusing failures, and only the operator who built it knows the right `--limit` and `--skip-tags` incantation. We're proposing to replace that whole motion with a tiny service that runs on each relay and accepts the CA-signed bundle directly. The operator (or eventually the client itself) presents the bundle to the relay's `POST /v1/enroll` endpoint over mTLS; the service verifies the CA signature, writes the peer into the running WireGuard config, returns success. No ansible, no SSM, no JSON-file-of-record, no per-relay operator-knowledge. The bundle is *already* CA-signed and *already* declares the peer's pubkey + claimed mesh IP — the relay just needs to be taught to trust it.

## Context

The wg-cp0 mesh per [ADR 0313](0313-silent-control-plane-outer-wg-wrap.md) carries all the load-bearing control plane traffic (netbird mgmt, signal, replication). New peers join by:

1. Operator mints a CA-signed enrollment bundle on the air-gapped workstation (`bundle-create`).
2. Operator appends the peer's pubkey + mesh IP to `ops/enrollment/wg-cp0-bundle-allowlist.json` (Block 22K v2 envelope).
3. Operator runs `ops/ansible/wg-cp0-bootstrap.yml` against `wg_cp0_relay_hosts` to push the allowlist file into each relay's wg-cp0 orchestrator. Pass 2A regenerates `/etc/wireguard/wg-cp0.conf` from the merged (v1 ansible facts + v2 allowlist JSON) peer set.
4. Operator hands the bundle to the client; client's `wg-cp0-bundle-apply` (legacy) or goat-client daemon (v0.2 path) materializes the wg-cp0 conf from the bundle and brings up the tunnel.

Step 3 — the ansible push — is the brittle one. Concrete pathologies hit during this session's laptop test:

- `wg-cp0-bootstrap.yml` interleaves client-only tasks (Pass 1B legacy local keygen, Pass 2B client tunnel-up) and relay-only tasks (Pass 1A relay install + key-gen, Pass 2A relay config + listener). Running `--limit relay_servers:goat_relay_hosts` runs the *whole* playbook against just those hosts, which means relay-only tasks like "Lock down privkey" fire in `--check` mode and produce false-positive failures.
- The new `goat_relay_hosts` tier (Block 33D Rust rewrite) and the legacy `relay_servers` tier have different wg-cp0 setup paths; the playbook treats them as one group but they aren't.
- Ansible+SSM transport adds a 2-3 minute startup cost per run; debugging cycles are slow.
- Operator-curated `wg-cp0-bundle-allowlist.json` becomes the source of truth for "who's allowed," but the bundle is *also* a source of truth (it carries the same pubkey + IP). Two sources, two ways for them to disagree.
- The whole flow requires a cloud-credential wrapper, per-operator SSH config, the right `OBJC_DISABLE_INITIALIZE_FORK_SAFETY` incantation on macOS. That knowledge lives with the operator, not in a runbook.

[ADR 0843](0843-public-mgmt-signal-relay-mobile-crutch.md) faced a parallel problem for the mgmt-API access tier (mobile + locked-down devices can't reach mesh-internal mgmt; ansible-driving every new device's network access is unsustainable) and resolved it with a deployed mTLS-gated public service. This ADR proposes the same architectural move for wg-cp0 peer onboarding.

## Decision

**Build a small Go service (`wg-cp0-enrollment-service`) that runs on every wg-cp0 relay, accepts CA-signed bundles via an authenticated `POST /v1/enroll` endpoint, verifies the bundle, and updates the relay's running WireGuard config and persistent state.** Decouple `bundle-create` from `--update-allowlist`; the JSON file becomes a *legacy seed* loaded by the service on first deploy and then read-only. Add `goat-cli admin enroll` as the operator's path; add a client-side optional self-enroll path (gated by a separate ADR for the trust-model decision).

### Service surface

```
POST /v1/enroll
Content-Type: application/cbor
Authorization: mTLS (offline-CA-signed client cert; operator OR client-self)

Body: bundle.cbor bytes (canonical wire format).

Verification (server-side):
  - Decode CBOR; reject malformed.
  - Verify Ed25519/ECDSA signature against pinned offline-CA root pubkey for the bundle's ca_id.
  - Reject if expires_at < now.
  - Reject if activation_deadline < now (peer never picked it up).
  - Reject if cp_device_address conflicts with an existing peer not matching cp_device_pubkey.
  - Reject if ACL groups don't intersect with this relay's accepted-groups policy.
  - Idempotent: re-presenting the same bundle for an already-enrolled peer returns 200 with "already enrolled" + the same effective config.

Side effects (server-side, atomic-or-rollback):
  - Append [Peer] row to /etc/wireguard/wg-cp0.conf (or run `wg set wg-cp0 peer <pk> allowed-ips <ip>/32` directly for hot reload).
  - Persist {pubkey, ip, bundle_hash, expires_at, enrolled_at} to /var/lib/wg-cp0-enrollment/state.db (sqlite or BoltDB).
  - Emit audit event to observability pipeline (Block 27 OTLP).

Response: 200 OK + {peer_id, mesh_addr, expires_at}
         400 (malformed)
         401 (mTLS auth failed)
         403 (bundle validation failed; reason in response body)
         409 (IP conflict with existing non-matching pubkey)
         500 (server error; state preserved)

POST /v1/revoke
  Body: {peer_pubkey: <b64>}
  Auth: mTLS, operator-class cert only.
  Side effects: remove [Peer] from /etc/wireguard/wg-cp0.conf + persist tombstone.

GET /v1/peers
  Auth: mTLS, operator-class cert only.
  Response: list of enrolled peers + state for audit / dashboard.

GET /healthz
  Liveness + count of enrolled peers.

GET /v1/openapi.yaml
  Auto-generated spec for goat-cli + future tooling.
```

### Trust model

- Service trusts the offline-CA root pubkey hardcoded at deploy time (operator-rotatable via redeploy).
- Service requires mTLS with a client cert signed by the same offline-CA. Two cert classes:
  - **Operator cert**: human operators; allows `/v1/enroll`, `/v1/revoke`, `/v1/peers`. Issued at operator-onboarding ceremony, short-lived (90 day) with renewal flow.
  - **Client-self cert** (post-MVP, separate ADR): the bundle's `cp_device_pubkey` becomes the client identity. Allows `/v1/enroll` only. Enables zero-operator self-onboarding. **Out of scope for this ADR's MVP; documented as path B in the design doc.**
- For MVP: operator-only path. Identical operator semantics to today's ansible flow, just over an API instead of SSM.
- The `--mobile-cert` Block 80F crutch-tier pattern from [ADR 0843](0843-public-mgmt-signal-relay-mobile-crutch.md) is the template.

### Sliced deployment

Phase 1: stand up service on **one** relay (a new test EC2 isolated from prod traffic). Exercise the full operator-cert + enroll + revoke loop against synthetic test peers. Verify audit emission. Soak for 1 week. Single-author code review.

Phase 2: deploy alongside existing flow on **prod legacy relays**. Both paths active. Migration script seeds the service's state.db from the existing `wg-cp0-bundle-allowlist.json` on first start. `wg-cp0-bootstrap.yml` Pass 2A stays operational. New peer enrollments go through the API; old peers untouched. Soak for 2 weeks; run one real new-peer enrollment via API end-to-end.

Phase 3: extend to **goat-relay tier**. Same migration shape as Phase 2.

Phase 4: deprecate `bundle-create --update-allowlist` and the ansible push. `wg-cp0-bundle-allowlist.json` becomes the historical-seed-only file. `goat-cli admin enroll` becomes the canonical operator interface. Ansible playbook keeps its other responsibilities (relay install, key gen) but loses Pass 2A.

Phase 5 (separate ADR): client-self-enroll path, contingent on satisfactory trust-model review.

## Why not alternatives

### "Keep ansible, document it better"
Doesn't address the per-run setup cost, the SSM transport overhead, the client/relay-task interleaving, or the "two sources of truth" problem (allowlist file vs. bundle). The operator-knowledge tax is the real cost.

### "Pull-based: each relay polls a canonical S3 object"
Considered. Tradeoffs:
- ✅ No inbound port on the relay (the service approach opens a new attack surface).
- ✅ Simpler to implement (just a periodic job + an S3 GET).
- ❌ Onboarding latency = poll interval. For "operator hands bundle to client at 2 PM, client tries to connect at 2:01 PM" UX, the poll interval has to be <60s, which means 6 relays × 60 polls/hr × 24h = 8.6k pollings/day per relay. Cheap but feels indirect.
- ❌ S3 becomes the single point of operator authentication; service can't reject malformed/expired bundles at upload time (the relay just sees the final state).
- ❌ Revocation requires the operator to *remove* an entry from the S3 object and wait for all relays to converge. Push (`POST /v1/revoke`) is immediate per-relay.

The push model is closer to what operators expect ("I added the peer, they can connect now") and matches the [ADR 0843](0843-public-mgmt-signal-relay-mobile-crutch.md) pattern already in use. Pull is a reasonable v2 if push proves too expensive.

### "Custom WireGuard handshake layer that verifies bundles inline"
Considered. Tradeoffs:
- ✅ No operator action at all — bundle in hand → connect → handshake includes bundle claim → relay verifies + admits dynamically.
- ❌ Vanilla WG kernel module has no hook for unknown-peer policy callback. Either patch kernel WG (intrusive, hard to maintain across kernel upgrades), or run userspace WG (`wireguard-go`) on the relay tier which sacrifices ~10× throughput.
- ❌ Even doing this, WG handshakes carry no application data — the bundle has to come over a separate channel, which means the "out-of-band" channel is doing the work and the WG-protocol integration is theater.
- ❌ Race-prone: client retries handshake while server is async-validating bundle.

The "handshake adapter" pattern collapses on inspection to "an API on the relay that validates bundles + updates WG config" — i.e., this ADR's proposal. The custom-handshake framing was misleading; we've corrected it here.

### "Centralized control plane in mgmt-host that fans out to relays"
Considered. Tradeoffs:
- ✅ Single authoritative store + per-relay sync agent. Matches Tailscale/Headscale architecture.
- ❌ Adds a new dependency (the central control plane must be highly available; if it's down, no enrollments). Per-relay fan-out also opens a fan-out failure mode where some relays got the update and others didn't.
- ❌ Replicates state that's already in the bundle. The bundle is the operator's signed intent; the relay's WG config is the relay's view of operator intent. A central store would be a third copy of the same data.
- ❌ More moving parts than the per-relay service. The per-relay service is *itself* the control plane, with one node per relay — simpler, no fan-out coordination.

If we later want a cross-relay observability layer, that's a *read-only* aggregator (`GET /v1/peers` from each relay, combined into a dashboard), not a central writer.

## Consequences

### Positive
- Operator onboarding step drops from "ansible-via-SSM + JSON-file editing + correct `--limit`" to `goat-cli admin enroll bundle.cbor`. Estimated 95% reduction in operator time per peer-add.
- The bundle becomes the unambiguous source of truth. The relay's accepted-peers list is *derived* from validated bundles; no parallel JSON of record.
- Revocation gets a real API. Today revocation is "remove from allowlist + re-push" — a multi-step ansible run. Becomes `goat-cli admin revoke <pubkey>`.
- Auditability: every enrollment + revocation emits a structured OTLP event. Block 27 observability already in place.
- Composes with `goat-cli` development trajectory. PR #576 Phase A scaffold is in main; this ADR's API surface is what Phase D-ish would target.
- Foundation for client-self-enroll (separate ADR), which is the real architectural prize — zero-operator peer onboarding.

### Negative
- New service to deploy, monitor, version, patch. ~300-500 LOC + role + tests + ADR + design doc. Estimated 1-2 engineer-weeks for the MVP.
- New attack surface per relay. mTLS gating mitigates but doesn't eliminate. The crutch-tier pattern (Block 80) is the precedent for exposing such a service, but each new instance is its own audit.
- Migration risk: existing peers seeded into the new service's state must be *byte-identical* to the current allowlist; off-by-one bugs leave peers offline. Phase 2's "deploy alongside, dual-source" gate mitigates.
- One more thing to break during a release cut. Mitigation: service version pins independently of relay OS / kernel WG version.

### Out of scope (named so they don't drift back in)
- Client-self-enroll trust model — separate ADR.
- The goat-relay tier rewrite (Block 33D) — orthogonal substrate concern.
- Replacing the offline-CA — Block 22M YubiKey-PIV substrate work is the prerequisite for any CA-rotation story.
- Mesh-DNS for the enrollment service endpoints — the service is reachable via the existing wg-cp0 mesh DNS or via the public crutch-tier-style FQDN for operator-cert holders.

## How we will know this is right

- **Pre-MVP gate**: the design doc + this ADR survives one operator review without "you'll regret this when X happens" objections that aren't already named in "Why not alternatives."
- **Phase 1 gate**: synthetic peer enrolls + revokes + re-enrolls cleanly against the single test relay; audit events visible in Block 27 OpenObserve.
- **Phase 2 gate**: one real prod peer (the next operator-onboarding) goes through `goat-cli admin enroll` end-to-end, end-user reports connection success without operator intervention beyond bundle handoff.
- **Phase 4 gate (final)**: `bundle-create --update-allowlist` produces a deprecation warning; `ansible-playbook wg-cp0-bootstrap.yml` Pass 2A is removed; no operator has run the ansible flow in 30 days; no enrollment-related on-call alerts have fired.

If any of those gates produce a "no, this isn't actually better," roll back to the ansible flow and write up the lessons in `docs/findings/`.
