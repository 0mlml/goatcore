# Design — wg-cp0 enrollment service

Operator-facing API service running on each wg-cp0 relay that replaces the operator-driven `ansible-playbook wg-cp0-bootstrap.yml` peer-add flow with `POST /v1/enroll` against the relay's authenticated HTTPS endpoint. Codifies the architectural decision in [ADR 0865](../adr/0865-wg-cp0-enrollment-service.md).

> **Status.** Design. No code in this repo yet; substrate work scheduled post-v0.2.0 goat-client ship.

## Why this exists

Today, adding a peer to the wg-cp0 mesh requires the operator to:

1. Mint a bundle (`bundle-create` with `--update-allowlist`).
2. Run `ansible-playbook wg-cp0-bootstrap.yml --limit relay_servers ...` against the relay tier via AWS SSM.
3. Wait 3-5 minutes for SSM connect + ansible execution.
4. Manually verify the new peer appears in each relay's `wg show`.
5. Hand the bundle to the client.

Five issues, in increasing severity:

1. **Step 2 has a brittle, undocumented incantation.** The right `--limit`, `--skip-tags`, and `OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES` magic lives only in the operator's head. The playbook interleaves client-only and relay-only tasks; running it against just relay hosts produces confusing `--check`-mode false positives (the "Lock down privkey" task fails in dry-run because the keygen step is short-circuited but the lock-down step looks for the file).
2. **The flow is operator-only.** No other team member can do it. This ADR is part of decoupling.
3. **Two sources of truth.** `wg-cp0-bundle-allowlist.json` and the actual bundle both encode the same `{pubkey, mesh_addr, acl_groups}` tuple. They can drift. The bundle is CA-signed and immutable; the JSON file is operator-edited and mutable. The relay believes whichever the orchestrator was last told.
4. **No real revocation path.** Today "revoke" = "remove from allowlist JSON + re-push." Multi-step, ansible-required, no audit trail. If a laptop is lost, the operator notices, opens the JSON, edits, runs ansible, waits 5 minutes. Compare to an HTTPS `POST /v1/revoke` that takes <1 second per relay.
5. **No room for client-self-enroll.** The architectural prize — "hand someone a CA-signed bundle, they connect, no operator involvement" — is impossible while the operator is gating every peer-add via ansible. This ADR is the prerequisite for that follow-on.

## Architecture

### Topology

```
Operator laptop ────HTTPS POST + mTLS────► relay-a:443 ─── enrollment service ─── wg-cp0 conf
                                       │                  ├── state.db (BoltDB)
                                       │                  └── OTLP audit emit
                                       │
                                       ├──► relay-b:443 ─── (same shape)
                                       └──► relay-c:443 ─── (same shape)
                                       └──► goat-relay-{a,b,c,d}:443 ─── (same shape, post-Phase 3)
```

One service instance per relay. No central coordinator. The operator's tool (initially `goat-cli admin enroll`, eventually a thin wrapper invocable from any of N team members' laptops) fans out to all relays in parallel and reports success/failure per relay.

Failure isolation: if one relay returns 5xx, the operator's tool reports "N-1 of N enrolled; relay-b failed with X" and the operator retries that one relay. No partial-state cleanup is needed in the success path because the bundle is immutable — re-presenting it later just reaffirms the existing enrollment.

### Service internals

```go
// ops/wg-cp0-enrollment-service/main.go (sketch)

type Service struct {
    CAPubkey       []byte          // pinned offline-CA root (loaded at start)
    OperatorCAPool *x509.CertPool  // for mTLS client cert verification
    State          *bolt.DB        // /var/lib/wg-cp0-enrollment/state.db
    WGCtrl         *wgctrl.Client  // wgctrl-go for runtime config updates
    AuditEmitter   otel.Emitter    // Block 27 OTLP
    Config         RelayConfig     // relay name, acl-groups policy, etc.
}

// POST /v1/enroll
func (s *Service) handleEnroll(w http.ResponseWriter, r *http.Request) {
    // 1. mTLS auth (handled by tls.Config.ClientAuth + VerifyPeerCertificate)
    operatorIdentity := extractMTLSIdentity(r)

    // 2. Decode bundle
    var b bundle.Envelope
    if err := cbor.NewDecoder(r.Body).Decode(&b); err != nil {
        writeError(w, 400, "malformed bundle: "+err.Error())
        return
    }

    // 3. Verify signature
    if err := bundle.Verify(b, s.CAPubkey); err != nil {
        writeError(w, 403, "signature verification failed: "+err.Error())
        return
    }

    // 4. Check expiry windows
    now := time.Now()
    if b.ExpiresAt.Before(now) {
        writeError(w, 403, "bundle expired at "+b.ExpiresAt.Format(time.RFC3339))
        return
    }
    if b.ActivationDeadline.Before(now) {
        writeError(w, 403, "bundle activation deadline passed; re-mint required")
        return
    }

    // 5. Check ACL-group intersection with relay policy
    if !aclGroupsIntersect(b.ACLGroups, s.Config.AcceptedACLGroups) {
        writeError(w, 403, fmt.Sprintf("ACL groups %v don't include any of relay's %v", b.ACLGroups, s.Config.AcceptedACLGroups))
        return
    }

    // 6. IP conflict check
    existing, err := s.State.GetByIP(b.CPDeviceAddress)
    if err != nil { /* db error */ }
    if existing != nil && !bytes.Equal(existing.Pubkey, b.CPDevicePubkey) {
        writeError(w, 409, fmt.Sprintf("IP %s already enrolled with different pubkey", b.CPDeviceAddress))
        return
    }

    // 7. Update WG config (idempotent)
    peerConfig := wgctrl.PeerConfig{
        PublicKey:  b.CPDevicePubkey,
        AllowedIPs: []net.IPNet{{IP: ipFromCIDR(b.CPDeviceAddress), Mask: net.CIDRMask(32, 32)}},
    }
    if err := s.WGCtrl.ConfigureDevice("wg-cp0", wgctrl.Config{Peers: []wgctrl.PeerConfig{peerConfig}}); err != nil {
        writeError(w, 500, "WG config update failed: "+err.Error())
        return
    }

    // 8. Persist state
    if err := s.State.Upsert(EnrollmentRecord{
        Pubkey:      b.CPDevicePubkey,
        IP:          b.CPDeviceAddress,
        BundleHash:  sha256.Sum256(rawBundleBytes),
        ExpiresAt:   b.ExpiresAt,
        EnrolledAt:  now,
        EnrolledBy:  operatorIdentity,
    }); err != nil { /* err */ }

    // 9. Audit emit
    s.AuditEmitter.Emit("wg_cp0_enrollment", map[string]any{
        "action":      "enroll",
        "peer_pubkey": base64.StdEncoding.EncodeToString(b.CPDevicePubkey),
        "peer_ip":     b.CPDeviceAddress,
        "device_id":   b.DeviceID,
        "operator":    operatorIdentity,
        "relay":       s.Config.RelayName,
    })

    writeJSON(w, 200, EnrollResponse{
        PeerID:    b.DeviceID,
        MeshAddr:  b.CPDeviceAddress,
        ExpiresAt: b.ExpiresAt,
    })
}
```

Lines of Go: ~400 for the service core (`main.go` + `enroll.go` + `state.go` + `audit.go`). Tests another ~600 (table-driven enroll/revoke/idempotency, malformed-bundle fuzz, IP-conflict cases). One-week build for a focused engineer; two if migration tooling proves fiddly.

### Storage

`/var/lib/wg-cp0-enrollment/state.db` — BoltDB or sqlite. Read at startup to repopulate WG config on relay reboot (so peers survive without re-presenting bundles). Schema:

```
peers/<pubkey-b64> → {
    pubkey:       []byte
    ip:           string  // "198.18.0.15"
    bundle_hash:  []byte  // sha256 of the canonical bundle bytes (for dedup)
    device_id:    string
    acl_groups:   []string
    expires_at:   time.Time
    enrolled_at:  time.Time
    enrolled_by:  string  // operator identity from mTLS cert
}

tombstones/<pubkey-b64> → {
    pubkey:       []byte
    revoked_at:   time.Time
    revoked_by:   string
    reason:       string
}
```

A periodic sweep (every 1h) removes peers whose `expires_at < now` and emits a `wg_cp0_expiry` audit event per removed peer.

### mTLS client cert classes

Operator-class certs are issued by the offline-CA with EKU `clientAuth` + a custom SAN type indicating `operator` role. Service's `VerifyPeerCertificate` callback inspects the SAN to decide whether the connection can call `/v1/enroll` (anyone with a valid operator cert), `/v1/revoke` (operator-class), `/v1/peers` (operator-class).

Two cert tiers planned, in this ADR's MVP scope:

| Tier | Issued by | Lifetime | Allowed endpoints |
|---|---|---|---|
| Operator | Offline-CA, operator-onboarding ceremony | 90 days, renewable | All |
| Client-self (post-MVP) | Offline-CA, embedded in bundle | Bundle lifetime | `/v1/enroll` only, *and only the bundle the cert came from* |

For MVP, only the Operator tier is active. The client-self path is a separate ADR (probably 0866) once the trust-model questions are resolved (who's responsible if a stolen device self-enrolls? what about ACL-group escalation via crafted bundle?).

### Deployment role

`ops/ansible/roles/wg-cp0-enrollment-service/` mirrors the shape of `roles/goat-public-mtls-crutch/`:

```
tasks/main.yml      ─── pull docker image (or systemd-native binary), render config, start
templates/
  config.yml.j2     ─── relay name, CA pubkey path, OTLP endpoint, accepted-ACL-groups
  systemd-unit.j2   ─── unit file with hardening (NoNewPrivileges, ProtectSystem, etc.)
defaults/main.yml   ─── default config values + image pin
handlers/main.yml   ─── restart on config change
files/
  ca-pubkey.pem     ─── the pinned offline-CA root (intermediate, not air-gapped root)
```

The role deploys *alongside* the existing wg-cp0 relay role. No replacement of existing functionality until Phase 4.

### Observability

OTLP traces emitted per request:
- `enroll.received` (with operator identity + bundle metadata)
- `enroll.verified` (CA signature ok, ACL groups match)
- `enroll.applied` (WG config updated successfully)
- `enroll.failed` (with reason: malformed / expired / ACL mismatch / IP conflict / wgctrl error)

Metrics (Prometheus-style, scraped by Block 27 collector):
- `wg_cp0_enrollment_requests_total{result=ok|malformed|expired|...}`
- `wg_cp0_enrolled_peers` (gauge of current count)
- `wg_cp0_enrollment_expiry_sweeps_total`

Dashboard: a single Grafana panel showing per-relay enrollment activity + current peer count + expiry warnings. Alerts: 5xx rate spike, peer-count diverges between relays >5%, expired-peer sweep failure.

## Sliced delivery

### Phase 1 — Greenfield single relay

- New EC2 `relay-a-enrollment-test` (not in production wg-cp0 path).
- Deploy service + state.db + relay role.
- Synthetic operator cert minted via existing `ops/enrollment/cmd/cert-sign` against the offline-CA.
- `curl` against `POST /v1/enroll` with a freshly-minted bundle.
- Verify state.db row + WG config + audit event + Grafana panel update.
- Soak 1 week with periodic synthetic enrollments + revocations.

**Exit gate**: 100% of synthetic operations succeed; observability + audit emit correctly; team review of the deployed shape signs off; pen-test of the public surface returns no findings beyond the known mTLS trust-model.

### Phase 2 — Side-by-side prod legacy relays

- Deploy service alongside existing wg-cp0 orchestrator on relay-a, relay-b, relay-c.
- Migration script: read existing `/etc/wireguard/wg-cp0.conf` peers, seed into state.db with `enrolled_by: "legacy-allowlist-seed"` and `expires_at: max time.Time` (never auto-expire).
- New enrollments go through `POST /v1/enroll`; old peers untouched but visible in `GET /v1/peers`.
- `wg-cp0-bootstrap.yml` Pass 2A remains operational. Operator can choose either path. New `goat-cli admin enroll` defaults to API.
- Add a *prod* operator cert to the offline-CA's issuance flow.
- One real new-peer enrollment (next operator onboarding or a captain's new laptop) goes through the API end-to-end.

**Exit gate**: ≥1 real prod enrollment via API succeeds; existing peers unaffected (gauge of peers steady); 2 weeks of operator practice with the API path; no operator has needed to fall back to ansible.

### Phase 3 — Extend to goat-relay tier

Same as Phase 2 but against goat-relay-a, goat-relay-b, goat-relay-c, goat-relay-d. The Block 33D Rust rewrite tier has a different config path (per the inventory file `goat_relay_*`); the service deployment needs a parallel role version (`wg-cp0-enrollment-service-rust` or shared via deploy-time conditionals).

**Exit gate**: API path active on all prod relays; observed steady state with mixed legacy+API operations for ≥1 week.

### Phase 4 — Sunset ansible Pass 2A

- `bundle-create --update-allowlist` emits a deprecation warning; flag still works for backward compat but the operator runbook moves to "mint bundle + run `goat-cli admin enroll`".
- `wg-cp0-bootstrap.yml` Pass 2A removed; the playbook keeps Pass 1A (relay install + key-gen) and Pass 2B (legacy client tunnel-up) for the cases that still need them.
- `wg-cp0-bundle-allowlist.json` becomes a frozen historical artifact; documented as the seed for state.db; no further commits expected.
- `goat-cli admin enroll` is the canonical path.

**Exit gate**: 30 days of no ansible-via-SSM operator runs; no enrollment-related on-call alerts; one operator + one team member who's never touched the old ansible flow both successfully run enrollments via API.

### Phase 5 — Client-self-enroll (separate ADR)

The architectural prize. Bundle becomes the client's enrollment credential. Service verifies the bundle and admits the client without operator involvement. Gated on satisfactory ADR 0866 (TBD) covering the trust-model questions enumerated in [ADR 0865](../adr/0865-wg-cp0-enrollment-service.md) "Out of scope."

## Open questions

- **Should the service run in the same VPC as the relays, or behind the Block 80 crutch-tier-style public endpoint?** The crutch-tier is publicly-reachable mTLS-gated. The enrollment service could ride the same FQDN pattern (`enroll-a.<zone>` etc.) so operators connect over the same path they already trust. Argues for: zero new public-DNS records. Argues against: the enrollment service is per-relay, the crutch-tier is per-region; namespacing might get confusing.
- **What does the service expose during a relay's bootstrap window?** Before the operator-CA pubkey is loaded, the service can't validate any request. Returning 503 cleanly is the right shape, but the bootstrap window (deploy → first config-render → service-up) is hard to compress.
- **Audit retention.** OTLP events go through Block 27. Should enrollment audit have a longer retention than typical telemetry (e.g., 1 year vs 30 days)? Probably yes; revocation forensics need it.
- **Idempotency under bundle-rotation.** If the same operator re-mints a bundle for the same device-id but with a fresh keypair, the new pubkey conflicts with the old one's `cp_device_address`. Service correctly returns 409. Operator's workflow: revoke old pubkey first. Is this surprising? Should the service auto-revoke the older entry when an authenticated operator re-enrolls the same device-id with a new pubkey? Argues for: matches operator intent. Argues against: silent state mutation. Probably keep explicit.
- **Goat-cli command shape.** `goat-cli admin enroll <bundle.cbor>` is the obvious shape but ADR 1019 also names `goat profile init` for the client-side bundle handling. Need to think through whether `admin enroll` is operator-only or whether end-users also call it (in which case it's not "admin" — it's the normal client flow). Cleaner: `goat-cli admin enroll` (operator-class cert, fan-out to all relays) vs `goat-cli connect` (client-class cert, single relay self-enroll path). Two CLI verbs for two trust models.

## Cross-links

- [ADR 0865](../adr/0865-wg-cp0-enrollment-service.md) — the decision this design implements.
- [ADR 0313](../adr/0313-silent-control-plane-outer-wg-wrap.md) — wg-cp0 substrate.
- [ADR 0407](../adr/0407-internal-x509-ca-for-service-tls.md) — the offline-CA the mTLS rooting depends on.
- [ADR 0843](../adr/0843-public-mgmt-signal-relay-mobile-crutch.md) — architectural sibling; same pattern for mgmt-API access.
- [ADR 1019](../adr/1019-goat-cli-unified-operator-binary.md) — the CLI this service's operator interface lives in.
- [`docs/operations/block-80-crutch-activate.md`](../operations/block-80-crutch-activate.md) — runbook template for the per-relay deploy this will mirror.
- [`ops/ansible/wg-cp0-bootstrap.yml`](../../ops/ansible/wg-cp0-bootstrap.yml) — the operator-driven playbook this replaces (Pass 2A only; Pass 1A stays).
- [`ops/enrollment/wg-cp0-bundle-allowlist.json`](../../ops/enrollment/wg-cp0-bundle-allowlist.json) — the JSON file that becomes a legacy seed.
- [`ops/enrollment/cmd/bundle-create/`](../../ops/enrollment/cmd/bundle-create/) — bundle minter; `--update-allowlist` flag deprecated post-Phase 4.
