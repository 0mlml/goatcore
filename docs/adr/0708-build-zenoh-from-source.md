# ADR 0708 — Build zenoh router + plugins + DDS bridge from source under our supply-chain pipeline

| Field | Value |
|---|---|
| Status | Superseded 2026-05-18 by [ADR 0858](0858-zenoh-image-upstream-pivot.md) |
| Date | 2026-04-26 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | [ADR 0858](0858-zenoh-image-upstream-pivot.md). The original justification for build-from-source — an in-process audit-logger plugin that had to share rustc + plugin-trait ABI with zenohd — dissolved when audit-logger moved out-of-process under Block 82S. ADR 0858 step 5 (2026-05-18) verified that upstream `eclipse/zenoh` is suitable when combined with the upstream-published `libzenoh_backend_rocksdb.so` (per F-139) and pivoted there. The custom image build (`ops/zenoh/base-image/`, `ops/zenoh/image/`, `.github/workflows/zenoh-base-image.yml`, `.github/workflows/zenoh-image.yml`) was deleted in the same change-set. |
| Related | [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0501-rust-interceptors.md`](0501-rust-interceptors.md), [`0503-dds-bridge.md`](0503-dds-bridge.md), [`0700-chainguard-first.md`](0700-chainguard-first.md), [`0702-build-from-source-mgmt-plane.md`](0702-build-from-source-mgmt-plane.md), [`0706-custom-netbird-image-build-ci.md`](0706-custom-netbird-image-build-ci.md), [`0707-rust-toolchain-pinning.md`](0707-rust-toolchain-pinning.md) |

## TL;DR (for a layperson)

We build the zenoh router + its plugins + the DDS bridge from source ourselves and publish the result to our own container registry. Same pattern we already use for netbird-management. Avoids being locked to whatever Rust version Eclipse Zenoh's published image was built with, gives us end-to-end supply chain ownership, and lets our own zenoh plugins load without ABI-mismatch gymnastics.

## Context

The Eclipse Zenoh project publishes `eclipse/zenoh:1.0.4` (and other tags) as Alpine/musl images. Those images embed a specific rustc version (1.75.0 for 1.0.4) into the binary. The Zenoh plugin trait checks that any plugin loaded into the router was compiled with the **same** rustc version and a compatible feature set; mismatch raises `Plugin compatibility mismatch` at load time.

A real attempt to build our `audit-logger` interceptor against the published `eclipse/zenoh:1.0.4` (CI runs 24940005144 → 24948129649, 2026-04-25 → 04-26) walked through seven distinct failure modes:

1. PT_INTERP leak (native musl build wrote glibc dynamic-linker path) → fixed by using `cross`
2. rustc + features mismatch → fixed by pinning rustc 1.75 + dropping `default-features = false`
3. `cargo-audit` requires rustc ≥1.85 → fixed by pinning audit job to 1.88 separately
4. `cargo-audit` install picked up workspace `rust-toolchain.toml` (1.75) → fixed by installing from `/tmp`
5. `Cargo.lock` v4 needs cargo ≥1.78 → fixed by regenerating at 1.75 → v3
6. New transitive-dep CVEs after lockfile regen → fixed by extending the ignore list
7. `unzip-n 0.1.4` requires `edition2024` (cargo ≥1.85) → **no compatible workaround**

The seventh failure is structural: the wider Rust crate ecosystem has moved past 1.75. Pinning every transitive dep backwards to satisfy 1.75 is a maintenance-forever job. Each new Zenoh release the team adopts will surface the same problem at whatever rustc that release was built with at the time, fading further behind the ecosystem each year.

The project has already chosen to build mgmt-plane components from source ([`0702`](0702-build-from-source-mgmt-plane.md)) and shipped one such image (custom netbird-management — [`0706`](0706-custom-netbird-image-build-ci.md), tag `patched-53b04e5`). Building zenoh under the same pipeline is consistent with that choice, not a new one.

## Decision

goat publishes `ghcr.io/<org>/zenoh:<tag>` from `ops/zenoh/image/Dockerfile` via `.github/workflows/zenoh-image.yml`, mirroring the `netbird-mgmt` build/publish pattern.

**The image bundles, all built with one rustc + one feature set:**

- `zenohd` — router binary (from `eclipse-zenoh/zenoh` upstream)
- `libzenoh_plugin_rest.so` — REST admin + Prometheus metrics endpoint
- `libzenoh_plugin_storage_manager.so` — storage abstraction
- `libzenoh_backend_rocksdb.so` — RocksDB storage backend (from `eclipse-zenoh/zenoh-backend-rocksdb`)
- `libzenoh_plugin_audit_logger.so` — our custom interceptor (from `zenoh-plugins/`)
- `zenoh-bridge-dds` — bidirectional DDS↔Zenoh bridge binary (from `eclipse-zenoh/zenoh-plugin-dds`)

`zenoh-bridge-dds` is included in the same image because it covers **both directions** (DDS pub → Zenoh sub for ingesting DDS-published data, AND Zenoh pub → DDS sub for pushing fabric-derived events back to DDS-speaking consumers). The user-facing entry point selects whether the container runs as `zenohd` (default) or as `zenoh-bridge-dds` (`--entrypoint /usr/local/bin/zenoh-bridge-dds`). Originally scoped as Phase 3 per Block 11 — pulled forward to the image because we're building anyway and the runtime cost is negligible (~25 MB and ~10 min added CI per arch).

**Architecture:**

- Native per-arch build (no buildx cross-compile, no QEMU). One job on `ubuntu-24.04` (amd64) + one on `ubuntu-24.04-arm` (arm64); manifest-combined into one multi-arch tag. Same pattern as [`0706`](0706-custom-netbird-image-build-ci.md).
- Alpine 3.19 / musl base — matches the upstream eclipse/zenoh shape so the existing `ops/ansible/roles/zenoh/` role doesn't need glibc/musl conditionals.
- CycloneDDS (CMake-built C library) included as a build + runtime dep for `zenoh-bridge-dds`.
- Smoke test in CI per arch: drop a config that loads all plugins, run the image, assert no `Plugin load failure` in the router logs and that `audit_logger`, `rest`, `storage_manager` all appear. Same gate an earlier dead soak missed — now lives in CI.

**Build matrix vs runtime-host matrix (clarification):**

The build matrix is **`(arch)` only**, deliberately. The bits an image contains are determined by the Dockerfile + base image, not by which Ubuntu the GitHub runner uses. `ubuntu-24.04` and `ubuntu-24.04-arm` are convenient runner choices; the resulting Alpine/musl image would be byte-identical from any Ubuntu version. Arch matters because the build emits arch-specific machine code; the runner OS does not.

The **runtime-host matrix** is a separate concern and IS load-bearing because we deploy onto heterogeneous host OSes:

| Host OS | Arch | Hardware role | Validation status |
|---|---|---|---|
| Ubuntu-for-Tegra 22.04 | aarch64 | embedded edge module | gated on first deploy |
| Ubuntu 24.04 | aarch64 | arm64 edge server | gated on first deploy |
| Ubuntu 24.04 | x86_64 | x86 mini PC, cloud host | gated on first deploy |
| Ubuntu 26.04 | x86_64 | x86 edge server | gated on first deploy |

For musl/Alpine containers the host-OS-portability assumption usually holds, but it is **assumption, not proof**. The validation gate is the existing per-env Ansible bootstrap + Phase 8 smoke test (the integration env's bootstrap playbook already asserts plugin load + admin reachability). First deploy to each new host OS is the runtime smoke; if it surfaces a host-OS-dependent failure (kernel feature gap, cgroup version, AppArmor, SELinux), revisit the image base or add a per-host-OS variant only then. **No premature host-OS matrix in CI** — wait for an actual failure.

**Cross-component compatibility policy:**

- Zenoh wire protocol is stable within `1.0.x`. All deployed peers pin to the same `=1.0.4` (or whichever upstream tag we're building) via the Ansible role default `audit_logger_release_tag` and the image build tag.
- Netbird wire compat is stable within patch versions (handled by [`0706`](0706-custom-netbird-image-build-ci.md)).
- Inter-component (netbird × zenoh × audit-logger) matrix collapses to "everyone on the pinned tags." No N×M version matrix to test; one line in `ops/ansible/group_vars/<env>/vars.yml` per env.

**Tag scheme:**

- `:latest` on `main` push (floating)
- `:<zenoh-ref>-build<run>` per build (immutable, e.g. `1.0.4-build3`)
- `:sha-<short>` per commit
- `:v<version>` on `zenoh-image/v*` tag push (semver release)

**Ansible role default flips** to `ghcr.io/<org>/zenoh:<pinned-tag>` once the first build lands. The previous standalone `audit-logger.so` build/publish workflow (`zenoh-plugins.yml`) is **retired** — its job is now done inside the image build.

## Alternatives considered

- **Stay on upstream `eclipse/zenoh:1.0.4` image, pin every transitive dep backwards.** Maintenance-forever job. Each Zenoh release we adopt drifts further from the modern ecosystem; we'd accumulate per-dep version pins, vendored crates, custom patches indefinitely. Rejected.
- **Stay on upstream image, use rustc that matches the image (1.75 today).** What we tried and what failed at edition2024. Rejected.
- **Wait for upstream Zenoh to publish a rustc-1.85+ image.** Passive; out of our hands; could be months or never. Rejected.
- **Build only `zenohd`, keep using upstream-shipped plugins.** Same plugin-trait ABI mismatch we're trying to escape. Rejected.
- **Separate image per binary** (one for `zenohd`, one for `zenoh-bridge-dds`, one for `audit-logger`). Three build pipelines, three publishes, three SBOMs to track. Rejected for now; can split later if image size matters.
- **Use ZettaScale's commercial Zenoh distribution.** Not sovereign, paid, doesn't fit Phase 2's scope. Rejected.

## Consequences

**Positive.**

- Plugin ABI mismatch (rustc + features) eliminated by construction. Future plugins (cds-conditioning, dlq, schema validators) build in the same workspace, same image, same rustc.
- End-to-end supply chain ownership — Chainguard-style discipline applies; SBOM is generatable; signing in CI is straightforward.
- Modern Rust ecosystem unblocked — `edition2024`, recent transitive-dep versions, no backwards-pinning.
- DDS bridge available immediately, both directions (DDS↔Zenoh) — no separate Phase 3 build pipeline.
- Cleaner ATO story: "image built from source `<git-sha>`" beats "Eclipse-published image we trust."
- CI smoke test catches plugin-load failures at build time, not after a 3-day silent dead-soak.

**Negative.**

- One more image build pipeline to maintain. Mitigation: pattern is identical to `netbird-mgmt`, so the per-image cognitive cost is small.
- Image build is heavier (~25 min per arch including CycloneDDS + zenoh + plugins + audit-logger). Mitigation: GHA cache; rebuild only triggers on path changes.
- Responsibility for tracking upstream Zenoh CVEs and rebuilding shifts to us. Counter: we already track them — we deploy them either way.
- Minor: image size ~80-100 MB vs upstream ~50 MB (because of CycloneDDS + extra plugins).

## Assessor view (SCA-R)

This decision serves SR-x (supply chain risk management) by extending the existing build-from-source pattern ([`0702`](0702-build-from-source-mgmt-plane.md)) to one more component. CM-2 (baseline configuration) is satisfied because the image is built from a specific upstream commit, recorded in the build tag, and the SBOM enumerates every component. SI-7 (software, firmware, and information integrity) is supported by the multi-arch manifest digest + (planned) cosign signing in the same workflow.

The ABI alignment between router and plugins is itself a CM-7 (least functionality) concern — by building from source we know exactly which features are enabled in zenohd and can prove the plugin trait check is by-construction satisfied, rather than an emergent property of an upstream image we don't control.

## Trigger conditions for revisit

- Eclipse Zenoh starts publishing images with rustc tracking the modern ecosystem (e.g. always built with stable). Revisit whether we still need to build our own — we may still want to for SBOM ownership but the ABI-mismatch driver dissolves.
- Image size becomes a deployment constraint at the edge. Consider splitting `zenoh-bridge-dds` into a separate image then.
- Upstream Zenoh moves to a `cdylib`-stable plugin trait that doesn't embed rustc version. Revisit whether the plugin-only build (without zenohd) is sufficient.
- We adopt a non-Zenoh substrate (e.g. NATS fall-back per [`0500`](0500-zenoh-data-fabric.md) gate-fail path). Image build retired.

## Amendment 2026-05-08 — bump 1.0.4 → 1.9.0 + version-pin re-eval cadence

**What changed.** Image base bumped from upstream Zenoh `1.0.4` (released 2024-12-19) to `1.9.0` (released 2026-04-10). Sibling repos (`zenoh-backend-rocksdb`, `zenoh-plugin-dds`) released 1.9.0 in lockstep. Builder rustc moved 1.75 → 1.88 (forced by `time-core 0.1.8` requiring `edition2024`); rust-toolchain.toml + Dockerfile builder + CI audit-job pin all aligned at 1.88.

**Probe → promotion in worktree (2026-05-08).** Bumping 9 minors of upstream evolution surfaced **one structural change** in the plugin trait surface: the runtime type was renamed `Runtime` → `DynamicRuntime`. Adapted in `audit-logger/src/lib.rs` via a single `use ... as Runtime` import alias (plus dropping the now-removed `zenoh-result` standalone import, replaced by `zenoh::Result as ZResult`). Total source delta: 3 import lines. Cargo.lock regenerated end-to-end (~140 crate version flips, ~250+ crates touched). `cargo build --release` clean; `cargo test --workspace` 5/5 PASS. The plugin-trait churn happened once at ~Zenoh 1.5; minors 1.6 / 1.7 / 1.8 / 1.9 are plugin-trait-stable, so the cost was concentrated, not accumulated.

**Validation of the build-from-source decision.** The original ADR motivation was to escape consuming an upstream image with a frozen rustc. The probe confirmed: because we own the build, the version-bump pain is *normal* Rust-ecosystem pain (compile, fix, repeat) rather than the irrecoverable `edition2024`-vs-rustc-1.75 wall that motivated 0708. No transitive-dep pinning gymnastics, no dropped features, no vendored crates. The decision is structurally validated by this exercise.

**New: pin re-evaluation cadence policy.** ADR 0708 originally specified no upgrade cadence, which let the pin drift to 9 minors behind upstream by inertia. Establishing:

- **Re-evaluate the pin within 90 days of each new upstream Zenoh minor release.** Re-eval = run a cost-probe equivalent to this one in a worktree; either bump (PR) or document the held-back rationale (Amendment row in this ADR or follow-on). 90 days gives upstream a stabilization window without letting drift accumulate.
- **Bump-blocker classes** that justify holding back: (a) plugin-trait API churn requiring more than ~1 day of source adaptation; (b) breaking changes to the Ansible role contract; (c) wire-protocol incompatibility forcing fleet-wide synchronized cutover; (d) upstream regression caught in our CI smoke or by a blocking F-NNN finding against the new minor.
- **Operator workflow on each bump**: (1) PR lands source bump + Cargo.lock + Dockerfile ARGs; (2) CI builds new `:1.X.Y-build<N>` immutable tag; (3) follow-on PR flips role default + per-env overrides to the new tag; (4) per-env smoke verifies plugin loads. Two-PR pattern keeps the source bump and the deployment cutover separately revertible.

**Post-merge flip step (this bump).** This PR landed source-only — `ops/ansible/roles/zenoh/defaults/main.yml` still references `ghcr.io/<org>/zenoh:1.0.4-build1` (operator-fired flip after CI publishes `1.9.0-build<N>`). Same applies to per-env overrides if any are reintroduced.

**Remaining unknown after this amendment.** Plugin ABI metadata at `dlopen` time is verified by `cargo build` only on the encoding side, not the decoding side; only a real `zenohd` 1.9.0 + the rebuilt `.so` + a smoke test confirms the VTable matches. CI smoke gate in `.github/workflows/zenoh-image.yml` (start image, assert `Successfully started plugin audit_logger` in router log) is the load-time check. If smoke regresses on first 1.9.0 image build, revert is one-line in `Dockerfile` ARGs.

**RUSTSEC ignores added in this bump.**

- **RUSTSEC-2025-0134** marks `rustls-pemfile 2.2.0` as unmaintained (informational; not a vulnerability). PEM-parsing role folded upstream into `rustls-pki-types`. Transitive via `zenoh-link-tls 1.9.0`. Tracked at [eclipse-zenoh/zenoh#2596](https://github.com/eclipse-zenoh/zenoh/issues/2596) (filed 2026-05-09). Remove the ignore when upstream zenoh migrates.

Added `--ignore RUSTSEC-2025-0134` to the `zenoh-plugins` audit job in `.github/workflows/rust-audit.yml` with full rationale comments + cross-reference to this amendment.

**Re-review condition:** at every 90-day pin re-evaluation per the cadence policy above, also re-evaluate every transitive RUSTSEC ignore to check whether upstream has shipped fixes and the ignores can be removed. Same reflex applies to F-082's existing cluster.

## Amendment 2026-05-15 — base-image split for CI throughput

**What changed.** The single Dockerfile that built zenohd + plugins + rocksdb + cyclonedds + dds-bridge + our audit-logger in one shot at `ops/zenoh/image/Dockerfile` is being split into two layers:

- New base image at `ops/zenoh/base-image/Dockerfile` → `ghcr.io/<org>/zenoh-base:<ZENOH_REF>-build<N>`. Contains all upstream-toolchain builds: zenohd + zenoh-plugin-rest + zenoh-plugin-storage-manager + zenoh-backend-rocksdb + CycloneDDS + zenoh-bridge-dds. NO audit-logger. Single-stage builder image (no runtime stage) so the toolchain + sources remain available for downstream consumers.
- The main image at `ops/zenoh/image/Dockerfile` (rewritten in a follow-on PR) → `FROM ghcr.io/<org>/zenoh-base:<pinned tag>` + audit-logger build + minimal runtime stage. ~30 sec build vs ~50 min cold today.

**Why.** The motivating measurement: today's `ops/zenoh/image/Dockerfile` takes ~50 min cold per arch on GHA, ~15 min cached. ~95% of that time is compiling upstream code we didn't write (zenohd ~150 crates, librocksdb-sys C++ build, CycloneDDS cmake, dds-bridge cargo build). Our only contribution — `audit-logger` — is ~2 min of cargo work. Every audit-logger change today triggers a full upstream rebuild for no observable benefit. The base split decouples upstream-toolchain churn (rare — on ZENOH_REF or rustc bump) from plugin churn (frequent — on every audit-logger change).

**Historical posture statement (superseded).** This ADR's image build was removed when [ADR 0858](0858-zenoh-image-upstream-pivot.md) superseded it. The current Trivy advisory workflow scans the checkout and a curated set of published images, which may include Zenoh images when listed; it does not scan each image build. Grype scanning remains unimplemented. See ADR 0704 for current scanner status. The former base-split decision was operational layering, not a change to the original supply-chain target.

**New workflow + trigger discipline.** New `.github/workflows/zenoh-base-image.yml` triggers ONLY on changes to `ops/zenoh/base-image/**` or the workflow file itself — i.e., real upstream-toolchain bumps. The sibling `.github/workflows/zenoh-image.yml` keeps its existing path filter (`ops/zenoh/image/**`, `zenoh-plugins/**`) so plugin churn fires only the main image build. Same per-arch native matrix + manifest combine pattern as the existing workflow.

**Tag scheme.** Base image: `ghcr.io/<org>/zenoh-base:<ZENOH_REF>-build<run-number>` (immutable) + `:latest` (rolling on main) + `:sha-<short>` (always). Main image's `FROM` line pins a specific `<ZENOH_REF>-build<N>` tag; bumping the base requires a deliberate PR that moves the pin in the main Dockerfile.

**F-122 surface count.** The eight-surface rust+zenoh version coupling enumerated in F-122 gains a ninth surface with this change: the `ARG ZENOH_REF` default in the new `ops/zenoh/base-image/Dockerfile` must move in lockstep with the existing eight. The queued `zenoh-image-version-coupling-discipline` CI gate track will assert this invariant mechanically.

**Two-PR rollout.** PR-A (this change) lands the base image + workflow without touching the main image — main keeps building everything itself in parallel. After PR-A merges and the first main-branch base build publishes to GHCR, PR-B rewrites the main image's `FROM` line + drops the now-redundant upstream-build steps. Each PR independently verifiable; rollback is a single revert.

---

*See `GLOSSARY.md` for terms and acronyms.*
