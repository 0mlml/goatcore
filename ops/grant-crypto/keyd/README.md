# grant-keyd — the E1 key-distribution service (native path)

The E1 (M2.1) service that mints and distributes the per-(release-set, generation)
key bundles for grant-keyed encryption. The pure state machine lives in the crypto
core (`grant_crypto::service`), with its two integration points left as traits;
this crate supplies the concrete impls that carry the heavy transport deps —
kept out of the CI-fast SDK core. grant-keyd is its own standalone workspace,
deliberately OUTSIDE the SDK subtree (`../sdk/`), and consumes the core via a path
dependency on `../sdk/core`, so its ~380-crate zenoh tree never enters the SDK build.

Design: `grant-keyed-encryption.md`
(§3 the service, §8 M2.1). Program tracking:
`data-access-program.md`.

## Built

- **`publisher::ZenohBundlePublisher`** — the native-Zenoh `BundlePublisher`. Opens
  a real session and PUTs the signed bundle on `keys/<release-set>/<generation>`.
  Synchronous (Zenoh 1.x `.wait()` — no async runtime). Publishing is the only step
  that touches the fabric; the bundle is already self-authenticating (key-service
  signature + the `keys/**` write-ACL), so the router carries opaque bytes and is
  never in the trust path.
- **`directory::DataRolesClient`** — the portal role/member resolver client
  (`GET /api/access/data-roles`, bearer auth). The audience's *identity* half: it
  returns a principal's evaluated roles + producers, and **deliberately no key
  material** — turning a member id into a recipient key is the separate F-267
  concern. Sync (`ureq`). Unit-tested against an in-process HTTP stub (real TCP +
  JSON parse): parses the response shape and sends the selector + bearer header;
  non-2xx fails closed as `Resolve`.
- **`recipient::RecipientKeys` / `CertDirRecipientKeys`** — the audience's *key*
  half (the F-267 gap). Resolves a member id → its **P-256** recipient key from an
  operator-provisioned directory of `<id>.pem` files (the E0 `gce` model as a
  daemon config path — the launch workaround; the durable member-key directory
  implements the same trait). Each PEM is a **certificate** (SVID leaf — key taken
  from its SPKI) or a **raw public key** (the directory's form); non-P-256 is
  rejected (EC-only, matching `envelope.RecipientFromCert`). Path traversal is
  refused; hierarchical ids nest as subdirs. Tested: raw-PEM resolve + wrap/unwrap
  round-trip, cert-PEM SPKI extraction (rcgen-generated), traversal refusal,
  fail-closed on missing/garbage.
- **`resolver::ConfiguredAudience`** — the launch `AudienceResolver`: operator-
  declared members (config `release_sets`) + `CertDirRecipientKeys`. Fail-closed —
  a missing member key fails the whole audience (never a partial one → never a
  silent denial). The grant-row enumeration that replaces the config list later
  implements the same trait.
- **`daemon::Daemon`** — the change-detecting sweep. `KeyService::rotate` always
  mints a new generation, so the daemon rotates a release-set only when its
  resolved audience **changed** (order-independent fingerprint of member ids +
  keys) — the honest poll realization of "rotate on access-rule change". Each
  release-set's outcome (`Rotated`/`Unchanged`/`Failed`) is reported independently.
- **`main` (the `grant-keyd` binary)** — loads config, loads the P-256 signing key,
  opens one Zenoh session, sweeps. `--once` = a single sweep (manual kick / change-
  driven); otherwise polls.
- **`svid::SvidValidator` / `SvidRecipientKeys`** — the durable, SPIRE-anchored key
  source (the F-267 fix). Validates a member's SVID against the SPIRE **trust root**
  + trust domain — vetted chain/signature/validity/EKU (`rustls-webpki`), URI-SAN
  **SPIFFE-ID match**, P-256 key — before trusting its key, so a cert stands in only
  for the member whose identity it carries. Supersedes `CertDirRecipientKeys`, which
  remains the explicit break-glass/offline path. Grounded in the live ZAGROS SPIRE
  (trust domain `spiffe://zagros.zagros.example`).
- **`metrics`** — the daemon's observability surface (see [Metrics](#metrics) below):
  a dependency-free `/metrics` + `/healthz` HTTP listener on `std::net::TcpListener`,
  started before anything that can block. No new crates — this tree's aws-lc/rustls
  pins are coupled fleet-wide and are not perturbed for an observability feature.
- **`svid::verify_bundle_signer`** — the same trust anchor for the **signer**: a
  member validates the key-service's SVID against the SPIRE root + its expected
  SPIFFE ID, takes its verifying key, and verifies the bundle signature — so bundle
  authenticity is trusted by SPIFFE identity, not a pinned key. (Where the member
  gets that SVID — carried in the bundle, self-describing — is the Option-2 follow-on.)

## Run

```bash
grant-keyd <config.json> [--once]
```

Config schema: [`example-config.json`](example-config.json) — `signer_id`, the
key-service `signing_key_pem` (PKCS#8 P-256), the `cert_dir` of per-member recipient
PEMs, `release_sets` (the operator-declared audience: release-set → member ids),
`poll_interval_secs`, and optional `zenoh_connect` endpoints. For a `tls/` router
that requires mTLS (the ZAGROS case), set `zenoh_tls_root_ca` (the router's CA) +
`zenoh_tls_cert` + `zenoh_tls_key` (this daemon's SVID cert + key, presented to the
router — its CN admits it through the router ACL); all three together. Setting `trust_bundle`
(the SPIRE root PEM) + `trust_domain` (both together) makes the daemon **validate
each member PEM as an SVID** against the SPIRE root (the durable path); omitting them
falls back to the unvalidated cert-dir source with a startup warning. **No secrets in
the config** — the signing key and member PEMs are provisioned out of band. The
end-to-end path (config-shaped resolver + cert-dir keys → signed bundle over a live
router → declared member opens, outsider denied) is proven in `tests/e2e.rs`.

## Metrics

`metrics_listen` (default `0.0.0.0:9464`, empty disables) serves two endpoints over
plaintext HTTP — no key material, no member identities, release-set names only:

| Path | Behaviour |
| --- | --- |
| `GET /metrics` | Prometheus text exposition (below). |
| `GET /healthz` | `200 ok …` while the loop is turning; **`503 stale …`** once no sweep has completed for three poll intervals (floor 90s), counted from process start — so a daemon hung before its first sweep also reads unhealthy. **Not reachable by a `wget`/`curl` container probe** — see below. |

> **Do not write a `wget`/`curl` HEALTHCHECK against `/healthz`.** The runtime image is
> `cgr.dev/chainguard/glibc-dynamic` ([Dockerfile](Dockerfile)) — no shell, no HTTP
> client — so such a probe is unrunnable and pins the container at `Up (unhealthy)`
> forever while it serves fine (F-173, which this repo already ate on distroless
> oauth2-proxy images, and which pages: grant-keyd is in the `CriticalServiceUnhealthy`
> set). The container healthcheck runs the binary's own
> [`--healthcheck`](#--healthcheck--the-container-probe) mode instead, which probes this
> same endpoint over loopback and reuses this same staleness definition.

The listener binds `0.0.0.0` because the daemon runs in a container whose port is not
published; a loopback bind would be unscrapable. Use `127.0.0.1:9464` when running on
a host directly. Under `--once` no listener starts (the container exits).

**Two clocks, deliberately distinct — alerting on the wrong one cries wolf:**

- `grant_keyd_last_sweep_timestamp_seconds` is the **liveness** clock. It advances
  every `poll_interval_secs` on a healthy daemon (including an all-`Unchanged`
  sweep). Stale ⇒ the loop is wedged or the process is gone. Alert on this.
- `grant_keyd_last_rotation_timestamp_seconds` is a **change record**, not a
  heartbeat. The daemon rotates only when an audience changes, so on a correct,
  quiet fabric it is legitimately days old. Do not alert on its age alone.

| Metric | Type | Meaning |
| --- | --- | --- |
| `grant_keyd_up` | gauge | Always 1 when scraped — alert on `absent()`, not the value. |
| `grant_keyd_build_info{version,signer_id,key_source,audience_source}` | gauge | Posture labels, value 1. |
| `grant_keyd_start_time_seconds` | gauge | Process start (restart detection). |
| `grant_keyd_poll_interval_seconds` | gauge | Expected period of the sweep clock — derive the staleness bound from it rather than hardcoding. |
| `grant_keyd_key_source_validated` | gauge | 1 = portal-CA and/or SVID validated; **0 = the UNVALIDATED break-glass cert-dir source is live**. Alert on 0 in production. |
| `grant_keyd_last_sweep_timestamp_seconds` | gauge | Liveness clock (0 = no sweep yet). |
| `grant_keyd_last_rotation_timestamp_seconds` | gauge | Last generation published, any set (0 = none since start). |
| `grant_keyd_sweeps_total` | counter | Completed sweeps. |
| `grant_keyd_rotations_total` | counter | Generations published. |
| `grant_keyd_errors_total{stage="resolve"\|"rotate"\|"enumerate"}` | counter | `resolve` = audience unbuildable (fail-closed, nothing published); `rotate` = mint/wrap/sign/publish failed; `enumerate` = the portal sweep list was unreachable and only pinned keys were swept. |
| `grant_keyd_release_sets_swept` | gauge | Size of the last sweep list — a drop in grant-row mode means the enumeration fell back to pinned. |
| `grant_keyd_release_set_generation{release_set}` | gauge | Latest generation **this process** published (a restart resets to 0; the fabric keeps the bundles). |
| `grant_keyd_release_set_members{release_set}` | gauge | Audience size at that generation. |
| `grant_keyd_release_set_last_rotation_timestamp_seconds{release_set}` | gauge | Per-set change record. |
| `grant_keyd_release_set_failures_total{release_set}` | counter | Per-set sweep failures. |
| `grant_keyd_release_set_audience_drift_seconds{release_set}` | gauge | Seconds this set's **published** generation has not matched its **resolved** audience; 0 = in sync. Non-zero means a membership change — including a revoke — has been read and not published, so the live generation still wraps to whoever the grant rows dropped. The only series describing a rotation that was *due and did not happen*; every other one describes rotations that did. |
| `grant_keyd_release_set_rotation_duration_seconds{release_set}` | gauge | Wall time of the last successful rotate. Flat DEK wrapping is O(audience size), so this is where the flat-wrap ceiling shows up on a real audience rather than in a benchmark. |

Deploy-side wiring (publishing the port, the scrape job, enabling the container
healthcheck, the alerts) lives in the `grant-keyd` ansible role and the observability
stack — not in this crate.

### `--healthcheck` — the container probe

The runtime image is Chainguard distroless: no shell, no curl, so a wget-style probe
is unrunnable and would pin the container `Up (unhealthy)` forever. The binary
is therefore its own probe:

```bash
grant-keyd --healthcheck [config.json] [--addr ip:port] [--strict]
```

The bare form is what the role's compose stanza runs
(`["CMD", "/usr/local/bin/grant-keyd", "--healthcheck"]`). Verify by hand before
flipping `grant_keyd_healthcheck_enabled`:

```bash
docker exec grant-keyd /usr/local/bin/grant-keyd --healthcheck; echo "exit=$?"
```

Address resolution, in order: `--addr` wins; else a named config path; else the
container's `/config/config.json` when it exists (so a changed `metrics_listen` is
followed without the stanza carrying the path); else the documented default
`127.0.0.1:9464`. A wildcard bind maps to loopback — the probe always runs inside the
daemon's netns.

| Exit | Meaning |
| --- | --- |
| 0 | `/healthz` says the sweep loop is turning. |
| 1 | `/healthz` returned 503 — no sweep for three poll intervals (floor 90s), counted from process start. The loop is wedged, or never reached its first sweep. |
| 2 | The endpoint was unreachable, or there is no address to probe (`metrics_listen` empty or unparseable, config unreadable). |
| 3 | `--strict` only: healthy, but resolving members through the UNVALIDATED break-glass cert-dir source. |

Two conditions are deliberately **not** failures by default. **Rotation age**: keyd
rotates only when an audience changes, so on a correct quiet fabric the last rotation
is legitimately days old, and failing on it would restart a healthy daemon.
**Break-glass mode**: a daemon deliberately running the offline break-glass source
must not be restart-looped by its own healthcheck — that posture is alertable on
`grant_keyd_key_source_validated == 0`, and `--strict` opts in for deployments that
treat it as unacceptable. The probe writes its state line to stdout/stderr, which
docker records in `Health.Log`.

## On-fabric transport proof

`tests/transport.rs` is the production analogue of the Go+Python E0 proof
(`../transport-proof`), on the path E1 actually ships. Over a **real loopback
router link** (two peer sessions, multicast off):

1. A Rust `KeyService` resolves a two-member audience, mints + signs a bundle, and
   its `ZenohBundlePublisher` PUTs it on `keys/example-set/1`.
2. A second session reads the bytes back off the router.
3. The bytes deserialize and the **key-service signature verifies** — the integrity
   proof: the signature covers the canonical transcript, so any corrupted byte in
   transit fails here.
4. Both authorized **members recover the same DEK**; the **outsider** has no slot
   and its key cannot unwrap a member's — the machine-plane read cut, enforced by
   the crypto, not the router.

```bash
# from ops/grant-crypto/keyd
cargo test -p grant-keyd --test transport -- --nocapture
```

Self-contained: the test creates both peer sessions in-process, so no external
`zenohd` is needed. CI: the `rust-transport-proof` job in
[`grant-crypto.yml`](../../../.github/workflows/grant-crypto.yml).

## Next increments

- **Grant-row member enumeration** — replace the config-declared audience with
  `GET /api/access/grants` → release-set membership (the `DataRolesClient` +
  directory), a drop-in `AudienceResolver` behind the same trait the daemon uses.
- **Wire to live ZAGROS (Option B)** — deploy prerequisites: a key-service signing
  identity, the `keys/**` write-ACL carve to it, the member-key source (cert dir at
  launch → the member-key directory durably, [F-267]), and daemon placement on the
  Thor host.
- **The member-key directory** (v1 EC + v2 hybrid KeyPackage, F-267) + E2 rotation
  hygiene.

[F-267]: ../../../docs/findings/F-267-e1-no-member-recipient-key-source.md
