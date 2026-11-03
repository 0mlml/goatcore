# ADR 0858 — Zenoh runtime image: pivot from build-from-source to upstream off-the-shelf

| Field | Value |
|---|---|
| Status | Accepted (2026-05-18) — all five steps landed. Step 5 took **path (a)** with a refinement: Eclipse publishes prebuilt `libzenoh_backend_rocksdb.so` artifacts at the matching upstream release tag (per F-139), so the Ansible role downloads + verifies + bind-mounts that single `.so` rather than running a sidecar container. |
| Supersedes | Supersedes [ADR 0708](0708-build-zenoh-from-source.md). With audit-logger retired in Block 82S, the supply-chain build-from-source rationale dissolved; the upstream image is now pinned by content digest, and the only required addition is the upstream-published rocksdb backend `.so` (bind-mounted from the host plugin dir). |
| Related | [ADR 0708](0708-build-zenoh-from-source.md), [ADR 0822](0822-wide-event-observability-otlp-openobserve.md), [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md), [ADR 0811](0811-audit-log-integrity.md), [ADR 0112](0112-tech-strategy-workload-shape-language-boundary.md), F-122, F-139, [`zenoh-audit-external-subscriber.md`](../design/zenoh-audit-external-subscriber.md) |

## Context

ADR 0708 (2026-04-26) chose to build the zenoh router from source under our supply-chain pipeline. The driving constraint at the time was the **eight-surface rustc/zenoh version coupling** (F-122) — our in-process `audit-logger` plugin had to share a plugin-trait ABI with `zenohd`. Building both from source under the same toolchain made that achievable.

Since then we've paid the recurring tax twice:

- **F-092** (2026-05-15): audit-logger plugin trait ABI mismatch; fixed by aligning rustc channel + `patch.crates-io` defense-in-depth on audit-logger's deps.
- **F-136** (2026-05-17, resolved 2026-05-18): same class — `libzenoh_backend_rocksdb.so` ABI mismatch with `zenohd`; the real fix turned out to require vendoring zenoh-backend-rocksdb INTO the zenoh workspace tree at build time so cargo's feature-unification pass produces ONE compile of the `zenoh` crate satisfying both binaries. An earlier `patch.crates-io` attempt redirected source paths but didn't unify feature flags (cargo's feature pass runs per-workspace), so it looked like F-092's fix shape transferred but didn't. Two ABI-class findings inside two weeks; the second cost ~a day of layered-symptom debugging before the workspace-membership mechanism was identified.

A separate review of audit-logger as an architectural choice ([`docs/design/zenoh-audit-external-subscriber.md`](../design/zenoh-audit-external-subscriber.md)) concluded it should move out of process into an external subscriber service for production. **Once audit-logger leaves the router process, the original justification for build-from-source dissolves** — there's no in-process plugin we own that has to share an ABI with zenohd.

## Decision

When the external-subscriber audit service lands and audit-logger is decommissioned from the router image:

1. **Stop building the zenoh runtime image from source.** Use the upstream `eclipse/zenoh` image directly, pinned by content digest. Eclipse does not currently sign their container images with cosign, so the security gate today is the digest pin; revisit if Eclipse adopts cosign upstream.
2. **Move `zenoh-bridge-dds` into its own sibling container** (upstream `eclipse/zenoh-bridge-dds`), don't co-bake it. We have no live consumer of the DDS bridge today, so this becomes "add a sibling container at the time the first DDS-translation consumer lands."
3. **Storage backend (`rocksdb`) — path (a) chosen.** Probe results in F-139: upstream `eclipse/zenoh:1.9.0` ships rest + storage_manager but not the rocksdb backend `.so`. Eclipse publishes prebuilt `libzenoh_backend_rocksdb.so` artifacts at the matching `eclipse-zenoh/zenoh-backend-rocksdb` 1.9.0 release (aarch64-musl + x86_64-musl) — same upstream pipeline as the image, so the plugin-trait ABI is aligned by construction. The Ansible role downloads + SHA256-verifies the matching artifact and bind-mounts the `.so` via the host plugin dir; config.json5 carries `storage_manager.backend_search_dirs` + `volumes: { rocksdb: {} }`. No second container needed — just one verified `.so` per host.

## Consequences

**Drops:**
- `ops/zenoh/base-image/`, `ops/zenoh/image/`, `.github/workflows/zenoh-base-image.yml`, `.github/workflows/zenoh-image.yml` (or kept as deprecated until pivot lands)
- ~30-90 min wall-clock per zenoh release bump (no more base + main image rebuilds)
- Entire F-092 / F-136 / F-122 plugin-ABI bug class — eliminated by construction (no custom `.so` loaded into zenohd)
- CI compute on every zenoh-touching PR

**Adds:**
- Cosign verification step on operator deploy — pin upstream digest, verify before pull
- Sibling-container deploy shape for DDS bridge
- A trust-model note: we now trust upstream Eclipse Zenoh's build process for the router binary itself. Mitigation: digest pinning + cosign + supply-chain audit on the upstream image's SBOM at adoption time.

**Trade-offs accepted:**
- Less control over the router binary's build inputs (in exchange for not paying the ABI tax)
- DDS bridge ergonomic cost (separate container vs co-baked) — acceptable; matches the "one process, one purpose" pattern we already follow elsewhere
- If we ever want another in-process plugin, this ADR re-opens — but the external-subscriber pattern is what we'd reach for first anyway

## Addendum (2026-06-05) — non-default zenoh cargo features also reopen this

The stock `eclipse/zenoh` image is built by `eclipse-zenoh/ci` with `cargo build --release --bins --lib` and **no `--features`** (verified 2026-06-05). Several useful zenoh features are **not** in the crate's default set, so enabling any of them requires building zenohd ourselves. And because we bind-mount the upstream-built `libzenoh_backend_rocksdb.so` (path a above), a custom-feature zenohd risks the same plugin-ABI divergence that bit twice (F-122 / F-136) — so enabling any of these means rebuilding **zenohd *and* the rocksdb backend from source as a matched pair**, plus a per-version-bump rebuild pipeline. The full build-from-source tax this ADR retired comes back. Candidates tracked so the question isn't re-litigated:

| Feature | Buys | Driver / status |
|---|---|---|
| `unstable` | AdvancedPublisher/Subscriber — sample-miss recovery, history, late-joiner (must-land + lossy-link reliability, F-178) | **Strongest candidate** — reliability for control-grade / degraded-network data, relevant to any network scenario |
| `stats` | router `/metrics`: throughput + an aggregate `AccessControl` denial counter | Observability; the denial counter is aggregate (no per-vendor attribution) and **largely redundant** with the expected-vs-actual monitor (F-177). Adds per-message overhead (why it is off by default) |
| `shared-memory` | intra-host shared-memory transport | On-box throughput for co-located containers on one Orin |
| `transport_serial` | pub/sub directly over a serial / RF link | The radio/RF/MANET tier — sibling repo `ice-goat`; not now |

`transport_compression` (wire compression for bandwidth-constrained links) is **already in the default build** — no action needed. **Recommendation: stay stock** until one of these is a hard requirement. `unstable` is the most likely to justify reopening; `stats` and `shared-memory` are worthwhile riders only once the build is owned anyway.

> **Correction (2026-06-11, F-193 / [ADR 1040](1040-zenoh-advanced-pubsub-liveliness-adoption.md)):** the `unstable → AdvancedPublisher/Subscriber` row above is wrong on one point — **AdvancedPub/Sub (and liveliness) are CLIENT-side `zenoh`/`zenoh-ext` features, not zenohd build features.** They are built on a queryable-backed PublicationCache + querying subscriber + liveliness, all routed by the **stock router**; adopting them needs only a client `zenoh-ext`/`unstable` dependency (which our client crates already enable) and does **not** require building zenohd. So `unstable` is **not** the strongest reason to own the build — that distinction now belongs to genuinely router-side features: **`stats`, `shared-memory`, `transport_serial`** (and any custom interceptor/zenohd fork). This ADR's *router-image* boundary is unchanged; only the misclassification of AdvancedPub/Sub is corrected.

## Sequencing

1. Land [`zenoh-audit-external-subscriber.md`](../design/zenoh-audit-external-subscriber.md) substrate (Block 82S, separate track) — **done 2026-05-18**
2. Cut over audit observation from in-process plugin to external subscriber on nova-a trial fabric — **done 2026-05-18**
3. Verify rocksdb behavior on upstream image (or pick path a/b/c) — **done 2026-05-18: path (a) chosen; see F-139**
4. Drop audit-logger from router image build; rebuild without it; verify zenohd loads cleanly — **done 2026-05-18**
5. Switch to upstream image; remove our zenoh image build workflows — **done 2026-05-18**
6. Mark ADR 0708 as superseded by this ADR — **done in the same change-set**
