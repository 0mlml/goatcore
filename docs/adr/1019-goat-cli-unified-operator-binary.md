# ADR 1019 — `goat-cli` unifies the everyday-operator surface as a single Rust binary on `panoscope-core`

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-27 (Proposed) / 2026-05-28 (Accepted — Phase B landed) |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | ADR 0844 (Zenoh deployment topology + ACL — namespace + auth substrate goat-cli rides), ADR 0859 (easy-rollback engineering principle — applies to goat-cli releases), ADR 1016 (complexity-budget substrate encapsulation — `panoscope-core` is the encapsulated substrate goat-cli composes), ADR 0857 (patched Ory consent UI — portal API auth path), `docs/design/goat-cli.md`, [`docs/design/stethoscope.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/stethoscope.md), [`docs/design/flashlight.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/flashlight.md), [`docs/design/goat-client.md`](../design/goat-client.md). |

## TL;DR (for a layperson)

Operators today need three or four different tools to do simple things against the data fabric: a web UI to browse the catalog of topics, separate diagnostic surfaces for "is this healthy?" questions, ad-hoc shell scripts for publishing and subscribing, and a hand-edited config file to remember which credentials go with which deployment. This ADR commits to a single command-line tool, `goat`, that bundles all of those into one program with a one-time setup step. After the operator runs `goat profile init <bundle>` once, every other command — listing vendors, creating or deleting topics, publishing, subscribing, diagnosing — just works without re-authenticating. The decision matters because the current scatter of tools is a real onboarding tax: new operators spend most of their first hour figuring out which command lives where, instead of doing the work.

## Context

The goat data fabric has reached a substrate-completeness point where the everyday operator workflow needs unification. The pieces exist:

- **Portal** (`ops/goat-zenoh-portal/`) exposes a complete REST API for vendor/topic/schema CRUD; the Go reconciler client (`internal/pubsubstate/client.go`) demonstrates the full write surface (`POST`/`PATCH`/`DELETE` against `/api/vendors` and `/api/vendors/{v}/topics`).
- **Inspection surfaces** (panoscope, stethoscope, flashlight) are live on the trial fabric and share `crates/panoscope-core/` as their substrate (portal HTTP client, Zenoh mTLS config, audit-tail S3 walker, peer registry).
- **Operator onboarding** (`docs/design/operator-onboard-utilities.md`) defines the manifest-driven bundle issuance pattern; bundles carry mTLS cert + CA roots + portal token + endpoint set as CBOR.
- **Bundle redemption** is implemented today only for the wg-cp0 outer tunnel (`ops/enrollment/cmd/wg-cp0-bundle-apply`, plus goat-client v0.1+ on desktop/mobile); there is no equivalent that establishes a *Zenoh-fabric* working profile on an operator laptop.

The gap is a unified operator-side entry point. Today an engineer who wants to "publish a test event to topic X" has to: locate the right portal URL, find their mTLS cert in whatever directory the last onboarding step dropped it in, write a one-off Zenoh Python script, hard-code the router endpoint, and copy/paste their portal API token from a chat thread. Operators are doing this; we have evidence of the pain in the trial vendor onboarding cycle.

The decision is whether to:

- (a) accept the fragmentation as the natural state and document the existing scatter better;
- (b) build a single Rust binary that composes the existing substrate into one operator surface;
- (c) embed CLI surfaces inside each of stethoscope / flashlight / panoscope as `--cli` modes;
- (d) write a thin Python or Bash wrapper that shells to existing tools.

This ADR adopts (b).

## Decision

We build `goat-cli`, a single Rust binary that:

1. **Lives in its own crate** at `crates/goat-cli/` and ships as a single statically-linked binary `goat`. No daemon, no system service, no companion process. Subcommands: `profile`, `doctor`, `vendor`, `topic`, `pub`, `sub`, `inspect`, `version`.

2. **Imports `panoscope-core`** for the portal REST client, Zenoh TLS config, audit-tail walker, role taxonomy, and URI-SAN extraction. No new code paths for substrate-level concerns. The only goat-cli-specific code is the operator-facing surface (clap subcommands, profile management, doctor prober, terminal rendering, SSE consumer for `goat inspect`).

3. **Establishes a reusable local profile** at `~/.goat/` (overridable via `GOAT_PROFILE_DIR`) containing the mTLS cert + key + CA roots + portal Bearer token + endpoint set, written once by `goat profile init <bundle.cbor>`. Subsequent invocations reuse the profile with no re-auth prompt. The profile directory uses filesystem permissions (mode 0700 / 0600) as its protection tier; we rely on operator-laptop LUKS rather than app-layer encryption. Bundle verification uses an embedded offline-CA root via `include_bytes!`, mirroring goat-client's `go:embed` pattern validated by F-090. **Amended 2026-09-02 — see §"2026-09-02" below: no CA root is compiled in; trust roots are operator-supplied at runtime.** **The on-disk layout is context-shaped from day one** — `~/.goat/config.toml` + `~/.goat/contexts/<name>/...` — so adding a second goatnet later is additive, not a flag-day. Phase A only writes/reads `default`; Phase C activates `goat context use <name>` and `--context <name>`. Context resolution precedence: `--context` > `GOAT_CONTEXT` env > `current_context` in `config.toml` > `default`. Token resolution precedence: `GOAT_PORTAL_TOKEN` env > `--token` flag > file (matches `flyctl`'s `FLY_API_TOKEN` model — CI escape hatch).

4. **Mediates topic create / delete through the portal REST API**, not through local CA tooling. `goat topic create` and `goat topic delete` POST and DELETE against `/api/vendors/{v}/topics{/...}` authenticated by the profile's Bearer token. The portal enforces vendor-prefix ownership and ADR 0844 reserved-prefix rules server-side. goat-cli never has access to offline-CA private keys and never invokes `cert-sign`.

5. **Delegates deep inspection to stethoscope** via HTTP+SSE. `goat inspect vendor=<v>` opens an SSE connection to the operator's nearest stethoscope service (URL from profile) and renders verdicts in the terminal as they stream in. Stethoscope remains the source of truth for the check set; goat-cli is the terminal-side renderer. If no stethoscope endpoint is configured, `goat inspect` exits non-zero with a configuration hint — it does not silently fall back to local checks.

6. **Names the ZTOL (Zero-Trust Overlay Layer = the WireGuard mesh) as a first-class precondition** in the `goat doctor` stepped prober. Step 1 is "am I on the mesh?"; failure points the operator at goat-client or the Block 80 crutch tier. Every other subcommand runs an abbreviated cached-for-60-s ZTOL precheck so the most-common failure mode ("operator not on mesh") produces a useful error instead of a "connection refused" cascade.

7. **Hierarchical drilldown + flat search** for topic discovery: `goat topic ls`, `goat topic ls <vendor>`, `goat topic ls <vendor>/<system>`, and `goat topic ls --search <q>` all hit the same portal endpoints and cache results client-side for 30 s. The drilldown is a CLI-side prefix filter, not a new portal API.

8. **Releases through the ADR 0859 image ledger** for reproducibility. Each `goat` build is tagged with its source SHA + dependency lockfile; `goat profile rotate --version <sha>` is reproducible across operator laptops.

9. **Phases land separately**: A (this PR — scaffold + design + ADR), B (profile + doctor + conventions), C (portal browse + topic CRUD + context mgmt), D (pub/sub + compose + narrate), E (inspect delegation + extensions). Each phase gates on a named criterion documented in `docs/project/in-flight.md`.

10. **Stable `--json` schema with `schema_version: 1` and `--jq <expr>` on every structured-output command.** Locked-in contract, snapshot-tested with `insta`. Adopted from `gh` (where it was retrofitted painfully) and Terraform's `-json` discipline. Without this, downstream scripts pin to incidental whitespace.

11. **Documented exit-code set + stable error-code namespace.** Exit codes published in `docs/design/goat-cli.md` (success / generic / usage / profile / portal / mesh / cert / timeout / SIGINT — a small documented superset of conventional codes, not `sysexits.h`). Error codes (`E_PROFILE_MISSING`, `E_OFF_MESH`, `E_CERT_EXPIRED`, `E_TOPIC_CONFLICT`, ...) are paired with the cargo-style `error: <summary>\n  help: <next-step>` format and verified by `trycmd` integration tests. Phase E adds `goat explain <code>` (cargo's `--explain` pattern).

12. **Mutator trait makes `--dry-run` structural, not per-command.** Every mutating subcommand implements `Mutator`, which forces a `dry_run(&self) -> PlannedRequest` method. Terraform's `plan` set the bar; we enforce it at the type level — shipping a mutator without `--dry-run` is a compile-time error.

13. **Shared `OutputContext` for `NO_COLOR` / TTY / `CI=true` discipline.** One struct that every spinner, color, table-render, and confirmation prompt routes through. Recognizes `NO_COLOR` and `CLICOLOR_FORCE` per [no-color.org](https://no-color.org/), `--color=auto|never|always`, `is_tty(stdout)`, `is_tty(stderr)`, and `CI=true` (separate from TTY — Jenkins has TTYs). Ten lines in Phase B; a half-day chore later.

14. **`goat compose <subcmd>` + `?` keystroke as the Phase D centerpiece, paired with `--narrate`.** The composer is a `ratatui` overlay that fuses help-mode with command-construction: the typed command-in-progress is preserved when help opens, position-aware grammar shows only what's valid at the current cursor, and the right pane is **live substrate context** (your write-prefixes, recent topics, declared schemas, conflict warnings) pulled from the same `panoscope-core::PortalClient` the rest of goat-cli uses. `--narrate` is the dual: a global flag that emits the substrate cascade (audit-tail event, stethoscope verdict, panoscope topology, subscriber receipt) for every mutating command. Compose shows what to *type*; narrate shows what the substrate *did*. Both are uniquely possible because goat operates against a fully-audited substrate — `gh` and `kubectl` cannot do either well because their backends are black-box. TTY-gated, opt-in; no overhead on scripted invocations.

15. **PATH-based extension discovery (kubectl pattern), not a plugin manager.** When `goat <unknown>` is invoked and `goat-<unknown>` exists on PATH, exec it with remaining args + `GOAT_PROFILE_DIR` / `GOAT_CONTEXT` exported. Vendors can ship `goat-<vendor>-*` binaries that reuse the profile. Phase E. Explicitly *not* a Krew-style plugin manager — overkill for the user-base size and inconsistent with the no-daemon discipline.

## Alternatives considered

**(a) Accept the fragmentation; document the existing scatter better.** Rejected. The operator-onboarding pain is real and growing as more vendors enroll on the trial fabric. Documentation has compounding limits: every new tool added to the scatter compounds the "where does X live?" tax. The complexity-budget discipline ([ADR 1016](1016-complexity-budget-substrate-encapsulation.md)) is explicit that operator-facing surfaces are exactly where encapsulation earns its complexity — a single CLI is the encapsulation.

**(c) Embed CLI surfaces inside each of stethoscope / flashlight / panoscope as `--cli` modes.** Rejected. The inspection surfaces are scoped to one diagnostic posture each (panoscope = whole-fabric, stethoscope = one target, flashlight = one slice); operator-facing CRUD against the portal is none of their concerns. Mixing the surfaces would muddy each service's purpose and trade away the clear "one service answers one question" boundary the design system has established. Furthermore, a per-service CLI means three separate profile files, three separate auth flows, three separate doctors — the exact fragmentation we're trying to close.

**(d) Write a thin Python or Bash wrapper that shells to existing tools.** Rejected. The portal write API is HTTP, the Zenoh client requires a real session and mTLS, the bundle is CBOR with a signature to verify. A wrapper script would either re-implement these (in which case we have a CLI, just in Python with looser type guarantees and worse error UX) or shell to a tangle of subprocesses (in which case error handling is degraded and the operator sees five layers of stack trace on any failure). Rust + clap + `panoscope-core` reuse is the lower-cost path to a real CLI with structured errors; the language matches the substrate the CLI composes.

**Variant of (b): build it in Go to match the existing portal client.** Considered. The portal's reconciler client is Go; we could port it. Rejected because `panoscope-core` is Rust, and the substrate gravity (Zenoh TLS config, audit-tail walker, role taxonomy, URI-SAN parser) lives in Rust. Re-implementing these in Go to keep the CLI in one language is double the code paths to maintain. The portal Go client and goat-cli Rust client are independent surfaces against the same HTTP API; that is acceptable duplication for a public REST contract.

## Consequences

**Positive.**

- One operator-facing surface for the everyday workflow, replacing the current scatter.
- One profile, one auth flow, one reachability prober. Operators learn the tool once.
- `panoscope-core` reuse means goat-cli is genuinely a composition layer, not a parallel substrate. Bug fixes to portal client / Zenoh TLS config / audit-tail flow into all four consumers (panoscope, stethoscope, flashlight, goat-cli) without re-implementation.
- Topic create / delete becomes scriptable. Operators can ship onboarding flows that call `goat topic create` in a shell loop without writing custom HTTP clients.
- The `goat doctor` stepped prober makes the "operator not on the mesh" failure mode visible and remediable instead of confusing.
- Easy-rollback discipline applies cleanly: goat-cli is a single binary with a profile directory; rollback is "use the previous binary, profile unchanged" or "rotate profile back via `goat profile rotate ~/.goat/contexts/<name>.bak/bundle.cbor`."
- The audit-tail substrate (MinIO Object-Lock per [ADR 0811](0811-audit-log-integrity.md)) gains a high-leverage operator-side reader in `--narrate`. The cost of writing every fabric event to retention storage now pays back directly in operator visibility — "what just happened across the substrate?" becomes a one-flag question rather than a three-tool correlation exercise.
- The interactive composer (`?` keystroke) compresses operator learning time. New operators see live write-prefixes, conflict warnings, and real portal examples while building their first command — empirical learning beats reading flat `--help` text.

**Negative.**

- One more surface to maintain. The mitigation is `panoscope-core` reuse: every shared concern lives there, and goat-cli is a thin composition layer.
- Cross-platform build matrix grows (Linux + macOS Intel + macOS Apple Silicon in Phase A; Windows deferred). Each platform adds CI surface.
- Profile storage on the filesystem (mode 0600) is weaker than app-layer encryption for shared-workstation scenarios. We accept this for Phase A; a Phase E follow-on can add an encrypted-blob option if operator profile pressure surfaces.
- We commit to staying in sync with portal REST API changes. Versioning discipline: the portal API is treated as a public contract; breaking changes require a coordinated release. (This is already true for stethoscope and flashlight, which use the same client.)
- The `goat inspect` delegation creates a soft dependency: an operator running `goat inspect` against a deployment without a stethoscope service sees "no stethoscope endpoint configured." We treat this as informative-error-by-design rather than a feature gap; the alternative (silent local fallback) would be worse.

**Residual risk.** A buggy goat-cli could spam the portal with topic create / delete calls or saturate a Zenoh subscription. Mitigations: client-side rate limiting on `goat topic create` (one create per 5 s) so scripted loops don't accidentally DoS the portal; subscription buffer sizes documented and tuned in Phase D. Neither risk is qualitatively different from a buggy stethoscope check or a buggy panoscope subscription.

## Assessor view (SCA-R)

The decision touches **IA-5 (Authenticator Management)** and **SC-13 (Cryptographic Protection)** via the profile storage shape, and **AC-6 (Least Privilege)** via the portal-mediated topic CRUD authorization model. Profile storage uses filesystem permissions (mode 0700 / 0600) on a LUKS-encrypted operator laptop; this is the same posture as `~/.ssh/` and is consistent with the existing operator-laptop hygiene control set (see `docs/operations/operator-laptop-hygiene.md`). Bundle verification uses an operator-supplied offline-CA root (amended 2026-09-02; originally embedded) with explicit expiry enforcement, consistent with the wg-cp0 bundle redemption pattern validated by F-090. The trust store is a directory of public keys at mode 0644 beside the contexts; adding one is an explicit operator action that prints a fingerprint for out-of-band confirmation, which is a **stronger** IA-5 posture than a build-time pin — the party who decides what to trust is the operator rather than whoever cut the release. Topic create / delete authorization is enforced server-side at the portal, not client-side — the CLI is a credential carrier, not a policy decision point — which aligns with **AC-3 (Access Enforcement)** centralization. Residual risk: a compromised operator laptop with an unlocked profile directory grants the attacker the operator's portal Bearer token and mTLS cert until those rotate; the compensating controls are operator-laptop FDE (LUKS), per-operator token scope, short bundle expiry, and the `goat profile rotate` path. This is the same posture as every other operator-onboarding artifact in the goat substrate.

## 2026-05-28 — Phase B landed (Status → Accepted)

Phase B (the load-bearing slice — operator profile init + 4-step reachability prober) landed end-to-end across the Go and Rust sides:

- **Go side:** `EnrollmentBundle.GoatCLIExtras` CBOR field (Step 2A); `bundle-create --goat-cli-*` flags with atomicity + PEM validation (Step 2B); operator-bundle mode (no wg-cp0 when only `--goat-cli-portal-endpoint` is set — Step 2C.1); `operator-onboard.yml` Phase 4.5 that downloads the per-vendor tarball, mints the CBOR via bundle-create, and zeroizes the staging tempdir (Step 2C.2); `bundle.md.j2` adds a "## goat-cli profile" section with three render branches (minted / 409 / skipped).
- **Rust side:** `crates/goat-cli/src/bundle.rs` (Step 2D) with `parse_and_verify` using ECDSA P-256 over SHA-256(canonical CBOR ex-signature), CA root pinned via `include_bytes!` of a dev CA public key under `ops/enrollment/public-keys/`; profile init/show/rotate wired with `0700`/`0600` mode discipline; `goat doctor` four-step prober (Step 3) — ZTOL TCP probe → Zenoh router TCP → full Zenoh mTLS session → portal `/api/vendors` — with verdict glyphs through `ctx.output`, `--json` NDJSON contract, `--all` mode, and outer-frame ZTOL cache at `~/.goat/contexts/<name>/last-doctor.json` (60s TTL).
- **Tests:** decisions 11 (exit codes), 12 (Mutator), 13 (OutputContext) from this ADR are exercised — 13 Rust unit tests + 6 trycmd goldens + 1 insta NDJSON snapshot for the doctor wrapper-script contract on the Rust side; the new Go-side bundle field + bundle-create flag surface land green across the existing enrollment test packages. Cross-side E2E (Go-mint → Rust-verify against the embedded CA root) is operator-driven on the trial goatnet; the Rust unit tests prove the canonical CBOR encoder + ECDSA P-256 verifier round-trip with a per-test keypair, so an encoder-divergence regression would surface at that run rather than silently passing.

The decision moves Proposed → Accepted because (a) every load-bearing operator-visible surface lands in Phase B, and (b) the Path B integrity property — CA signature covers the (cert, key, ca-roots, endpoints, token) tuple together — is asserted by the at-rest-tamper unit test on both sides. The trial-goatnet E2E is the next gate.

Phase C (vendor browse, topic CRUD, pub/sub) and Phase D (compose TUI, narrate) build on this substrate and do not require any further changes to decisions 1-15.

## 2026-06-02 — TUI inspection surface (amends decision 14 + reverses the "no primary TUI" rejection)

A design pass on extending goat-cli concluded that the four web inspection surfaces (stethoscope, flashlight, portal, panoscope) can be rendered in the terminal as an opt-in interactive surface inside the existing `goat` binary, because they already share `panoscope-core` *and* the Block 94 cross-surface navigation contract ([`docs/design/zenoh-surface-deep-links.md`](../design/zenoh-surface-deep-links.md)). Design of record: [`docs/design/goat-cli-tui.md`](../design/goat-cli-tui.md).

This amends two earlier positions in this ADR and its design doc:

1. **Decision 14 framed the `compose` ratatui overlay as "the deliberate exception" to a line-oriented binary.** It is no longer the only TUI surface. The TUI inspection surface is a second, larger ratatui surface. The principle that *survives* is narrower and now explicit: **line-oriented stays the default and the only non-TTY path; every TUI surface is opt-in and TTY-gated by construction** (refuses to launch on a non-TTY, like `compose`). `--json` and scriptable behavior are unchanged.

2. **`docs/design/goat-cli.md` listed "a built-in TUI as primary surface" under *patterns explicitly NOT adopted*.** That rejection is reversed in its strong form and replaced with the precise rule above: a TUI is adopted as an **opt-in inspection surface**, never as the primary or default surface. The design doc is updated in the same change-set.

The decision rests on three load-bearing constraints, all carried into the design doc:

- **It is a renderer, not a service.** No new deployment, daemon, socket, or background process — a mode of the existing binary. The web surfaces remain the canonical source-of-truth deployments with their own rollback ledgers.
- **The delegation guardrail (this ADR's founding principle) holds unchanged.** Diagnose renders stethoscope's `/api/diagnose` SSE (no local checks); Tail subscribes via panoscope-core and decodes via the portal's `fetch_fds`, with heavy analytics delegated to `flashlight-analytics` over `POST /api/compute` (no forked logic). The picker may *aggregate and mark* declared-vs-live membership but emits no *verdicts* — anomaly classification stays in stethoscope, one keystroke away.
- **The decomposition follows operator posture, not the web's service boundary.** The web split four ways for deployment isolation, which a single binary doesn't inherit. In the terminal the surfaces collapse to **one picker+detail (portal ⊕ panoscope side-panel, declared ⊕ live merged) plus two question-lenses (Diagnose = stethoscope, Tail = flashlight Tier 1)**. panoscope's force-graph and flashlight's Tier 2 WASM do not port (the latter has a native `/api/compute` equivalent, so no capability is lost).

Phasing: Phase A is planning (this addendum + the design doc + tracking, no code). Phase B (Diagnose lens) and Phase C (Tail lens) build directly on the existing `goat inspect` SSE consumer and the already-planned `goat sub --tui`; Phase D (the merged picker) is gated on those proving useful plus operator demand. Decisions 1–13 and 15 are unaffected.

## 2026-09-02 — no CA root is compiled in (amends decision 3)

Decision 3 said bundle verification uses an embedded offline-CA root via
`include_bytes!`, mirroring goat-client's `go:embed`. That is reversed for
goat-cli. The Go `goat-client` is a separate binary and this does not speak
for it.

**What went wrong with the original.** The pin was single-valued, so one
release could verify bundles from one goatnet; every other goatnet's bundle
failed to install with an error that reads like file corruption
(F-406).
The first fix widened the pin to four keys and shipped in 0.4.44. It unblocked
the four goatnets that existed and left the decision's actual defect in place:
while the keys live in the binary, a new goatnet or a rotated CA needs a CLI
release, and that release then has to reach every installed binary before
anyone can onboard. Widening a compile-time list is not the same as removing
the compile-time dependency.

**What replaces it.** `ca_root_verifying_keys()` reads PEMs at runtime from
`$GOAT_CA_ROOTS` (a file, or a directory of `*.pem`) then
`<profile-dir>/ca-roots/*.pem`; a bundle verifies if any trusted root signed
it. `goat ca ls | add | rm` manages the store. A unit test asserts a process
with an empty trust configuration trusts nothing, which is what would catch a
key being compiled back in.

**F-090 is not contradicted.** F-090 is about the macOS Keychain refusing
operator-side root import, which is why this is a file-level trust store the
tool owns rather than an OS trust-store lookup. That constraint is honoured;
only the *location* of the file moved, from inside the binary to beside the
profile.

**The cost, stated plainly.** A fresh binary trusts nothing, so removing the
floor without a distribution path would have been a regression. Two channels
carry roots — the onboarding tarball ships the verifying key beside the `.cbor`
it verifies, and `get.<site>/ca-roots/` serves every root with fingerprints.
Neither establishes trust; the out-of-band fingerprint check is the gate.
Operators on 0.4.44 or earlier run `goat ca add` once after upgrading, and
`names.rs` name-fallback reads the same store, so a rootless install resolves
nothing from a name snapshot — fail-closed, same one-command fix.

## Trigger conditions for revisit

- Windows operator demand emerges and Phase A's "no Windows" assumption blocks an enrollment.
- Shared-workstation deployments surface (operator profiles are accessed by multiple unprivileged accounts on the same machine) and filesystem permissions are no longer sufficient; revisit profile-encryption decision.
- Portal REST API breaks contract (vendor prefix model changes, auth shape changes) such that goat-cli's client diverges materially from `panoscope-core`'s read-only client; revisit whether goat-cli should own a forked client or fold its write surface back into `panoscope-core`.
- Operator demand emerges for `goat bundle mint <handle>` (offline-CA-mediated bundle generation from the CLI); revisit whether goat-cli grows into operator-onboarding territory or stays consumer-only.
- A second inspection sibling adds a CLI need (e.g., a panoscope-CLI request); revisit whether `goat inspect` should grow sub-targets (`goat inspect --via panoscope`) or stay stethoscope-only.

## 2026-06-07 — Windows support added (revisit trigger fired)

The first revisit trigger above ("Windows operator demand emerges") fired. goat-cli now builds and ships for **windows/amd64** alongside the four Unix targets. The Phase-A "no Windows" assumption is retired.

What changed:

- **Build matrix.** `.github/workflows/goat-cli-release.yml` gains a native `windows-latest` (MSVC, `x86_64-pc-windows-msvc`) leg. ring's x86_64 assembly needs NASM under MSVC, so the Windows leg installs it; Linux + macOS are unaffected. The release asset is `goat-windows-amd64.exe` with the same per-binary SHA-256 + aggregate `SHA256SUMS` treatment as the other targets.
- **Source portability.** The unix-only seams are now `cfg`-split rather than assumed: profile/context secret files drop the POSIX `0600`/`0700` mode bits on Windows (confidentiality rests on the `%USERPROFILE%\.goat` directory ACLs instead); the default profile dir resolves `%USERPROFILE%` when `HOME` is unset; plugin (`goat-<name>`) discovery probes `.exe`/`.bat`/`.cmd` on Windows; and plugin hand-off spawns-and-waits (no Unix `exec`).
- **One deliberate gap.** `goat client` — which drives the **goat-clientd** tunnel daemon over a Unix domain socket — returns a clear not-yet-available error on Windows. goat-clientd *is* designed for Windows (named pipe `\\.\pipe\goat-client`, per [`docs/design/goat-client.md`](../design/goat-client.md) §Daemon↔GUI IPC and [ADR 0840](0840-goat-client-cross-platform-daemon-gui.md)), but that daemon + its named-pipe IPC are not built yet. Until the Windows goat-clientd ships, Windows operators manage the mesh tunnel with the NetBird client; the cfg-split transport stub is where the named-pipe client lands when it does. Every other subcommand (`doctor`, `vendor`, `topic`, pub/sub, `services`, the TUI lenses) runs natively today.

This is a target-coverage expansion, not a change to any of decisions 1–15. The shared-workstation profile-encryption trigger is unaffected and still open (filesystem ACLs remain the Windows confidentiality story, mirroring the `0600` posture on Unix).

## 2026-10-30 — `goat compose` live substrate context completed (decision 14 built, not amended)

Decision 14's composer now ships its full live-context half: all four feeds it named — write-prefixes, recent topics, declared schemas, conflict warnings — are built. This is the decision realized, not a change to it; the spec above stands as written.

The build (`crates/goat-cli/src/cmd/compose_context.rs` + the `// CONTEXT-PANE:` seams in `compose.rs`), detailed in `docs/design/goat-cli.md` §`goat compose`:

- **Where each feed surfaces** — write-prefixes at a write namespace slot (`pub` / `topic create|delete`); recent topics at a read key slot (`sub` / `flow`); declared schemas at an unconstrained `--format` / `--encoding` value; conflict warnings (prefix you can't write, or a key that already exists) computed from the key being typed at a write slot.
- **One snapshot, no loop network** — two read-only reads at launch (`GET /api/orgs`, `GET /api/catalog`) build an in-memory snapshot; the sync event loop never touches the network. This is the deliberate simplification of decision 14's sketched "PortalClient calls inside the loop, cached 30 s" — a session snapshot is adequate for a short compose session. Each feed degrades independently (no profile / portal down / slow / a portal predating `/api/catalog`) to an "unavailable" note, and the composer stays fully usable.
- **Isolation held** — one module behind the `ContextSource` seam, read-only, `// CONTEXT-PANE:`-tagged wiring for clean removal, `--no-context` kill-switch. The composer is also now taught as a `goat tutor` lesson, so operators discover it.

Decisions 1–15 are otherwise unaffected.
