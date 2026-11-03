# ADR 1082 — Name fallback resolver: single-signer signed snapshots, graded staleness, in-process client fallback

**Status:** Accepted. Phase 1 built (this PR); phases 2/3 designed-not-built, adoption-gated. Design: [`name-fallback-resolver.md`](../design/name-fallback-resolver.md).
**Repo scope:** goat core — generator (`ops/dns/generate-mesh-hosts.py`), signer (`ops/enrollment/cmd/name-snapshot-sign/`), CI gates (`ops/ci/check-name-snapshot-sig.sh` + the `dns-registry-sync` job), serving (`ops/ansible/roles/goat-get/`), client resolver (`crates/goat-cli/src/names.rs` + `status`/`doctor` wiring). goat-clientd (separate repo) adoption is a named follow-on.
**Related:** `dns-service-discovery.md` · [`bootstrap-bundle.md`](../design/bootstrap-bundle.md) (the signing-machinery precedent) · [`federated-trust-model.md`](../design/federated-trust-model.md) (recognition ≠ authorization — why couriers carry artifacts, not authority) · [`complexity-budget-and-encapsulation.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/complexity-budget-and-encapsulation.md) ([ADR 1016](1016-complexity-budget-substrate-encapsulation.md)) · F-039 · F-197 · [ADR 1019](1019-goat-cli-unified-operator-binary.md)

## Plain-language summary

When the per-goatnet mesh DNS is down or unreachable, every service dies *by name* while the WireGuard paths to those services still work. This ADR adopts a last-known-good name layer: the registry that already generates the CoreDNS zone also emits a versioned, TTL-declared **snapshot** of the name set, signed by the goatnet's existing offline-CA; goat-cli caches the newest validly-signed snapshot while names work and falls back to it — honestly labeled with serial + age — only when live DNS fails. Trust lives entirely in the signature, so any transport can carry the artifact; nobody but the one zone authority can ever change a mapping.

## Context

- Mesh names are served by one CoreDNS per goatnet on the mgmt host (`dns-service-discovery.md`). The failure class is precedented: F-039 (zone silently loads zero records), and F-197 (declared-but-unresolvable deploys dark).
- goat names are slowly-changing, **single-writer** data: `ops/dns/generate-mesh-hosts.py` renders them from the group_vars service registry — one author, the repo.
- The substrate already owns every primitive needed: an ECDSA P-256 per-goatnet offline-CA with an age-encrypted-key signer (`ops/enrollment/ca/age_ca.go`), a client that embeds that CA root and verifies DER-over-SHA-256 signatures (`crates/goat-cli/src/bundle.rs`), a drift-gated generated-and-committed zone pattern, and a per-goatnet mesh download tier (`get.<site>.*`, the `goat-get` role).
- goat-cli today has **no** custom name resolution anywhere — all three connect paths (TCP probes, reqwest, Zenoh endpoints) delegate to the OS resolver.

## Decision

1. **Versioned signed snapshots, single signer.** Every registry change regenerates `ops/dns/<site>/name-snapshot.json` with a monotonic DNS-SOA-style serial and a declared TTL, signed (detached `.sig`, ASN.1 DER ECDSA P-256 over SHA-256 of the file bytes) by the site's existing offline-CA via the existing `AgeSigner` — the bundle-signing machinery reused verbatim, **no new key infrastructure**. Full-state replacement; no tombstones. Committed + drift-gated + signature-gated in CI, opt-in per goatnet (`name_snapshot_enabled`).
2. **In-process fallback resolver in goat-cli — not a hosts-shim.** A resolver module tries live DNS first (never shadowed), then the cached verified snapshot. Wired at goat-cli's connect sites (`status`/`doctor` probes, Zenoh endpoint, portal HTTP). Cache refreshed opportunistically from `get.<site>.*` while names work; re-verified at every use; a lower serial never replaces a higher one.
3. **Graded staleness, fail-closed honesty.** Every fallback answer is labeled source + serial + age in `goat status` / `goat doctor`; aging snapshots warn; past-TTL or bad-signature snapshots are **refused**, never silently served.
4. **Couriers are untrusted transports (phase 2, designed-not-built).** Any peer may hand any peer a newer snapshot; the signature makes the courier irrelevant to trust — recognition of the signer, never authority to the carrier (`federated-trust-model.md`). Gossip the artifact, never records. Adoption gate: observed staleness in a real outage, or a denied-edge deployment that cannot reach `get.<site>.*`.
5. **Signed single-owner self-claims (phase 3).** A peer may assert only its own name→IP under its own leaf cert; never overrides snapshot service names. ~~Adoption gate: finding-grade evidence of mesh-IP churn outpacing snapshot regeneration, plus a concrete peer-name consumer.~~ *Superseded by Amendment 1.*

## Amendment 1 (2026-07-15) — device-wide consumer, shared store, observed tier, self-claims pulled forward

Operator review of phase 1 redirected three things (design detail in [`name-fallback-resolver.md`](../design/name-fallback-resolver.md) §4.1–§4.3, §7):

1. **The primary consumer is goat-clientd (device-wide), not goat-cli.** goat-cli's resolver covers operator tooling only; browsers and other apps still lose names. Committed follow-on: goat-clientd (repo `goat-maintainer/goat-client`) consumes the same artifact and answers at the DNS layer. **Anti-double-implementation contract:** one shared on-disk store per device — flat signed files, daemon is sole refresher when present, goat-cli degrades to reader (its per-context cache remains only for daemon-less laptops). Trust is in the signature (verify-at-read), so shared bytes carry no trust cost.
2. **Noncanonical observed-records tier (committed).** The signed snapshot carries only registry-declared names, but live DNS legitimately holds more (peer records via `sync-peers.sh`; operator out-of-band hotfixes that the repo never captured before DNS died). Successful live resolutions persist to `observed-names.json` in the same store; consulted after the snapshot with gap-fill + fresher-wins-on-conflict rules; every answer labeled **noncanonical (ad hoc)**, logged as a codification-debt gap, and rendered visibly flagged in every surface (CLI/TUI/GUI). Closes the hotfix-then-outage window without online signing.
3. **Self-claims (decision 5) pulled forward** from adoption-gated backlog to committed follow-on: peer names are live-only and failing them in fallback is a real hole today. The observed tier is the interim (unsigned, labeled) answer; self-claims are its signed replacement for peer names. Sequenced with the goat-clientd work.

## Amendment 2 (2026-07-17) — self-claims design settled: snapshot-carried bindings, fabric-collected distribution

Design detail in [`name-fallback-resolver.md`](../design/name-fallback-resolver.md) §7.1–§7.4. Three decisions:

1. **Offline verification via snapshot-carried `peer_bindings`.** The leaf cert names its anchor but not its DNS peer name, and an outage-time verifier can consult no registry. The signed snapshot gains an additive registry-derived `peer_bindings` (name→anchor) field — same serial/signature/TTL/distribution machinery, no new trust root. A claim verifies only when the cached verified snapshot binds the claimed name to the claim's anchor: own-name-only and never-overrides-service-records hold **by construction** (service names carry no binding).
2. **Claim artifact** `goat-name-claim-v1` `{name, ip, anchor, issued_at_unix, ttl_seconds}`, leaf-signed with chain, short TTL (default 1 h — claims exist for fast-moving IPs), freshest-valid-per-name wins (single-owner register).
3. **Channel: fabric publish + passive collector + the existing `get.<site>` tier** (consume-while-healthy, serve-while-broken — the snapshot's own philosophy). Claims publish to `ops/names/claim/<anchor>`; a reveal-only collector (audit-subscriber pattern, no enforcement path) serves `claims.json` beside the snapshot; clients cache it into the shared store and verify at read (the server is untrusted transport — a compromised collector can deny, never forge). Rejected: direct peer claim-query (needs the claimant reachable exactly when names are broken; leaves no cached artifact) and the mesh-management peer list (alive precisely when the name plane is alive — failure-coupled to the outage being survived; bypasses leaf-signature trust).

Resolution precedence becomes: live DNS → valid self-claim (peer names only) → signed snapshot → observed (demoted to gap-fill-only for names with no valid claim).

## Why an in-process resolver, not a hosts-shim

The design left this open ("whichever fits the client architecture better"). The client evidence decides it: goat-cli is a short-lived unprivileged CLI whose every hostname→connection moment passes through a handful of owned call sites, with no existing `/etc/hosts` interaction. A hosts-shim would (a) require root on every fallback activation, (b) mutate name resolution for **every process on the machine** — far beyond the CLI's blast radius, and a footgun beside NetBird's own pushed-nameserver management and macOS's `mDNSResponder` caching; (c) leave stale entries behind if the CLI dies mid-fallback; and (d) be structurally unable to label answers with serial + age — the honesty requirement. An in-process resolver is per-command, privilege-free, self-cleaning, and labels every answer. The same reasoning holds for goat-clientd (long-lived daemon, Go, owns its dial sites) when it adopts the library shape.

## Alternatives considered

- **Distributed record-level CRDT name store — rejected.** goat names are slowly-changing single-writer data (generated from the registry by one authority), so there is no multi-writer concurrency for a CRDT to converge. A gossiped *writable* name table is a poisoning/interception surface that would force rebuilding DNSSEC-grade origin authentication inside a gossip layer. Signed full-state snapshots dissolve the problem: replace, don't merge; verify the signer, don't trust the transport.
- **Hosts-shim (`/etc/hosts` writer) — rejected.** See above: root, machine-wide blast radius, NetBird-DNS collision, no honesty labeling.
- **Local caching DNS daemon on every client (dnsmasq/CoreDNS sidecar) — rejected.** A new long-running service per laptop for a fallback path violates the complexity budget (ADR 1016), and it still needs a signed data source — this design *is* that data source, minus the daemon.
- **Tune CoreDNS/negative-cache TTLs — rejected.** OS resolver caches don't survive "resolver unreachable" for the durations that matter, give no cross-reboot persistence, and can't tell the operator they're running on stale data.
- **Second CoreDNS replica per site — orthogonal, not rejected.** Redundancy shrinks the window but cannot close the class (F-039 was a *content* failure both replicas would share; a partitioned client loses both). The fallback layer is what makes the class survivable; HA remains a separate availability lever.

## Consequences

- Clients keep resolving goat names through name-plane outages, with honest provenance on every answer — this failure class becomes a labeled degradation instead of an outage.
- Registry changes on snapshot-enabled sites now require an operator holding the site CA age key to re-sign before merge (CI-enforced). Deliberate: same one-human-signs posture as the bootstrap bundle; registry changes are infrequent.
- Staleness grading inherits local-clock UTC confidence (ADR 1037 caveat, accepted).
- A compromised courier/download tier is bounded to replaying old-valid snapshots within TTL; it can never forge a mapping or roll a client back past its cached serial.
- Complexity-budget position: maintainer-only substrate. Operators see one `names` status line; the four onboarding paths are untouched (zero new required steps — the snapshot rides existing channels).
