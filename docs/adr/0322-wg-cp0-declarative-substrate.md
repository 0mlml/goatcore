# ADR 0322 — wg-cp0 declarative substrate: one source-of-truth, one reconcile pipeline

**Status:** Proposed (2026-05-13).
**Date:** 2026-05-13
**Series:** silent-control-plane (composes with ADR 0313 / 0314 / 0315 / 0316 / 0319 / 0321).
**Prerequisite:** [ADR 0321](0321-wg-cp0-perimeter-not-acl.md) — open-perimeter must ship first; this ADR is "additive hardening on top of a working mesh," not a co-requisite for one.
**Re-do of:** ADR 0320 (drafted 2026-05-12, branch never merged — abandoned in place per F-117 when the parallel ADR 0321 "perimeter, not an ACL layer" landed and changed the parent framing). This ADR salvages the substrate decision, re-anchored on the post-0321 mesh shape.
**Repo scope (post-monorepo-split):** core.

---

> **For the technical contributor:** promote [`ops/enrollment/wg-cp0-bundle-allowlist.json`](../../ops/enrollment/wg-cp0-bundle-allowlist.json) to the **single declarative source-of-truth** for the wg-cp0 peer set. Collapse the four-tool ceremony (`bundle-create` + `wg-cp0-bootstrap.yml` Pass 2A + `wg-cp0-rotate-bundles.yml` + `wg-cp0-peer-surgical-update.yml`) into one reconcile-driven pipeline anchored on a thin Go accessor library (`ops/enrollment/peerset/`). Every operation runs **dry-run-by-default**, prints a diff against live state, requires explicit `--apply` to mutate. Allowlist sync is **upsert-only**; removal requires explicit `--remove <peer-name>`. `bundle-create` gains `--reuse-keypair-from` so additive-metadata operations (endpoint adds, TTL extension, ACL group changes) stop forcing gratuitous key rotation. Allowlist propagation moves from `restart wg-cp0` to `wg syncconf` so an additive change does not disturb existing peer sessions. One operator command per logical intent: `wg-cp0 reconcile`, `wg-cp0 onboard`, `wg-cp0 rotate`, `wg-cp0 update-endpoints`, `wg-cp0 remove`.
>
> **For the layperson sponsor:** today the small set of tools we use to manage who can talk on the silent control plane each read a slightly different "list of valid peers" from a slightly different place, and each tool's default behavior is to *rewrite* the live state rather than *reconcile* it. Whenever an operator runs the wrong tool with the wrong subset in scope, the others' work is silently undone. We have seen this fail in three different ways. This ADR collapses the tools into one workflow that reads from one place, shows the operator exactly what would change before any change is made, and refuses to silently delete anyone. It does not change *what the network is allowed to do* — that is settled by the prior ADR. It changes *how operators safely change membership* without breaking the mesh as a side effect.
>
> **For the SCA-R:** wg-cp0 (silent outer WireGuard wrap per ADR 0313) preserves all three protocol-layer properties — silent under unauthenticated handshakes, no public DNS, offline-CA-rooted membership per ADR 0407 + Block 22K. ADR 0321 settled the WireGuard-layer security claim: membership = full mesh reach; per-peer policy lives at application altitude. This ADR governs the **operator ceremony substrate** through which membership changes — adds, rotations, removals, endpoint additions — flow. The substrate change adds: (1) a single declarative artifact under the offline-CA trust root (no new key custody, no new external dependency), (2) a dry-run-with-diff gate on every operation (operator visibility, not a new control), (3) upsert-not-rewrite as the default semantics (failure-mode reduction; an operator-side mistake can no longer silently drop authorized peers), (4) an audit-record per ceremony invocation (`bundle-create.audit.json`) feeding the existing audit-integrity tier (ADR 0811). No new trust roots; no new keys; no new network surface; no change to the listener or wire protocol. The closed gap is operator-action safety, not posture.

---

## Context

[ADR 0321](0321-wg-cp0-perimeter-not-acl.md) settled the *what* layer of the wg-cp0 silent control plane. WireGuard-layer `AllowedIPs` carries `[MeshAddr/32, 198.18.0.0/24]` on every relay peer block; membership = full mesh reach; fine-grained access controls live one altitude up. The asymmetric-`/24` failure mode is closed by construction.

That left the *how* layer of operator ceremony unresolved. The substrate that an operator uses to add a peer, rotate a peer's key, extend a TTL, add a new relay endpoint to existing peers' bundles, or recover from drift is today four loosely-coupled tools:

- **`ops/enrollment/cmd/bundle-create/`** (Go CLI) — mints a CBOR bundle for a peer; with `--update-allowlist`, also writes a JSON allowlist entry.
- **`ops/ansible/wg-cp0-bootstrap.yml`** (Ansible) — relay-side standup; Pass 2A renders the relay's allowlist from an Ansible inventory group (`[wg_cp0_clients]`); fires a `restart wg-cp0` handler on changes.
- **`ops/ansible/wg-cp0-rotate-bundles.yml`** (Ansible) — Block 22P-era ceremony; walks the inventory clients, delegates `bundle-create` to a controller, ships + applies new bundles per peer.
- **`ops/ansible/wg-cp0-peer-surgical-update.yml`** (Ansible) — F-114 closure; per-peer `wg set` + `wg-quick save` without listener restart; takes explicit pubkey arguments from the operator.

Each of these reads peer-set truth from a slightly different artifact: the master JSON, the Ansible inventory ini file, per-device `/etc/wireguard/wg-cp0.conf`, or operator-supplied invocation arguments. Every tool's default behavior is to *rewrite* the in-scope state rather than *reconcile* it. As a result, every operation against the substrate carries a non-trivial risk that something outside the running tool's view of the peer set gets silently dropped, or that an in-flight rotation leaves a peer in a half-state that survives invisibly until the next listener restart turns latent drift into a hard outage.

This failure shape produces three operator-visible regressions:

- **Drift then restart** — management hosts drift across pubkey generations between bundle, allowlist, and in-memory daemon state; it surfaces when an unrelated allowlist push fires the relay-side restart handler and the drifted peers cannot re-handshake (F-114).
- **Missed peer** — a peer goes dark in the same shape because a per-peer recovery sweep did not iterate its entry.
- **Partial-scope rewrite (Incident-C class)** — a session runs `wg-cp0-bootstrap.yml` against an inventory `--limit` that excludes canonical peers; Pass 2A's render-then-write silently rewrites every relay's allowlist to the in-scope subset, dropping every peer outside it.

The recovery from each is the same shape — drive `wg-cp0-peer-surgical-update.yml` against ground truth, one peer at a time, until the live state matched what the operator believed the canonical set to be. The surgical playbook is the only tool in the toolchain that operates *additively* and *per-peer* against explicitly-supplied truth, and it is by accident the safest one in the set. The redesign generalizes that shape across the whole substrate.

F-114 names the rotation-ceremony serialization gap. F-116 names the asymmetric-AllowedIPs failure mode that ADR 0321 settled. F-117 names the tracker-vs-merge gap that lost the original ADR 0320 draft. The substrate redesign closes the operator-action class of failures that all three findings sit inside.

## Decision

Adopt a declarative substrate for wg-cp0 peer-set management, anchored on five composing properties. Each is independently valuable; the full set is what makes the ceremony surface safe for an operator to fire any operation without risk that an in-scope tool will silently invalidate the work of an out-of-scope one.

**Property 1 — Single declarative source-of-truth.** Promote [`ops/enrollment/wg-cp0-bundle-allowlist.json`](../../ops/enrollment/wg-cp0-bundle-allowlist.json) to the canonical artifact for the wg-cp0 peer set. Extend its schema to carry every field downstream tools need (current pubkey, pubkey-history list with rotation reasons, role, site, allowed-ips extras, bundle expiry, active-forwarder selection). Introduce a thin Go accessor module at `ops/enrollment/peerset/` (sibling to the existing `bundle/`, `ca/`, `x509ca/`, `crl/` packages) as the *only* path that reads or writes the file. Every wg-cp0 tool — `bundle-create`, the new reconcile binary, every Ansible play — passes through this library. Ansible inventory ini files stop being a source of peer-set truth and revert to their proper role: enumerating Ansible connection targets.

**Property 2 — Dry-run-with-diff by default.** Every operation that mutates the peer set or the live wire state defaults to dry-run. Output is a structured diff against the current state, showing `WOULD ADD`, `WOULD REMOVE`, `WOULD ROTATE`, `WOULD UPDATE ENDPOINTS`, `WOULD KEEP` in a shape an operator can scan in five seconds. `--apply` is required to make any change. The diff is fast — never opens a wg-set, never restarts a daemon, never writes a file outside the dry-run report. The dry-run for the relay-side reconcile is the catch for the Incident-C class: an operator with a partial input would see `WOULD REMOVE: operator-laptop, mgmt-a, mgmt-b, lab-ci` in the preview, recognize the input view was incomplete, cancel, re-run with the canonical set.

**Property 3 — Upsert-not-rewrite semantics.** The relay-side allowlist sync is upsert-only by default. Adding or updating a peer is `wg set ... peer <pubkey> allowed-ips <ip>/32`; idempotent on no-change; runs as a fan-out across relays with bounded concurrency. **Removal is never implicit.** Peers absent from the input set are not deleted; an `--remove <peer-name>` flag is required, and the operator sees the removal list in the dry-run. A separate `reconcile` mode does sync the live allowlist to exactly match the declarative peer set, including removals, but only as an explicit operator intent with the removal list printed in the dry-run.

**Property 4 — Reuse-keypair-from on bundle-create.** Of the three lifecycle paths in [`docs/design/wg-cp0-peer-lifecycle.md`](../design/wg-cp0-peer-lifecycle.md) — onboarding, rotation, drift recovery — only rotation needs a fresh Curve25519 keypair. Today every `bundle-create` invocation mints a new keypair, so endpoint additions, TTL extensions, ACL group changes, and forwarder-tier flips all force gratuitous rotation. Every gratuitous rotation is a chance for F-114-class drift. `bundle-create` gains `--reuse-keypair-from <peer-name>` (read pubkey from peerset, preserve it in the new bundle) and `--rotate-keypair --reason "..."` (explicit rotation, written to the audit record). Ambiguous invocations (no flag, existing peer) fail-loud. A bundle redistribution for a new relay tier becomes a pure additive metadata operation.

**Property 5 — Atomic ceremony orchestrator.** One Go binary, `wg-cp0`, owns the operator surface. Subcommands `reconcile`, `onboard`, `rotate`, `update-endpoints`, `update-ttl`, `remove`, `add-relay` each wrap a complete logical intent — bundle mint, device-side apply, relay-side upsert, audit-record emit — as a single transactional unit, with per-step success/failure reported in the dry-run preview and the apply phase. Failures inside an `--apply` run fail-loud and emit a recovery hint; partial-step apply state is observable in the audit log. The orchestrator replaces the operator-driven sequencing of today's four-tool ceremony.

**Allowlist propagation moves from listener-restart to in-place reload.** This is a sixth implementation property that lands in 22R.3, which was authored under a parallel track (`wg-cp0-syncconf-migration`) and merged to main during the same window as this design. Allowlist-only changes use `wg syncconf` (no listener restart, no peer-session disturbance); the existing `restart wg-cp0` handler is reserved for actual listener-shape changes (port, address, MTU). This composes with Properties 1-5 but is engineering-orthogonal and tracked separately.

The substrate operates the same Curve25519 keys, the same offline-CA bundle root (ADR 0407), the same WireGuard listener shape, the same wg-cp0 mesh subnet (ADR 0319), the same address plan. The change is entirely in the operator-facing ceremony layer. No new network surface, no new trust roots, no new key custody story.

## Cost of NOT doing this (first-hand evidence)

The failure shapes detailed in F-114 and the abandoned-ADR-0320 design doc each cost operator recovery time and block dependent work. The cost class is monotone with fleet size — every peer added, every key rotated, every endpoint added, every relay added is another opportunity for an instance of the same class. Every bundle redistribution fires against the substrate as-is unless the redesign lands first. With operator-laptop additions and mobile client rollouts, the rate of peer-set operations increases rather than decreases.

ADR 0321 closed the worst-case operational consequence (mesh unreachability from asymmetric `/24` placement) but did not close the operator-action failure mode (silent peer drops from partial-input rewrites). The substrate redesign is what closes the second class.

## Consequences

### Positive

- **One mental model.** Operators reason about wg-cp0 ceremony through one orchestrator and one canonical artifact. The "which tool reads which input?" question goes away.
- **Pre-mutation visibility.** Every operation is previewable. The class of mistakes where an operator runs the wrong tool with the wrong scope and silently invalidates peers becomes structurally impossible — they would see the removals in the diff first.
- **Failure-mode reduction.** Three of the four named failure shapes (F-114 drift-then-restart-surfaces, Incident-C silent-fleet-amputation, F-117 ceremony-step-skipped) become structurally inaccessible rather than discipline-enforced.
- **Reduced gratuitous rotation.** A new-relay-tier bundle redistribution and every analogous "add an endpoint to every peer" operation runs without rotating any keypair, eliminating the largest single class of drift triggers.
- **Composable with concurrent work.** The accessor library is a single integration surface; 22R.3 `wg syncconf` migration, future Block 22H peer-key attestation (which adds handshake-time signature verification), and future Block 22K.9 bundle auto-renew all compose with the accessor library rather than coordinating ad-hoc with each Ansible play.
- **No security posture change.** The substrate operates the same keys, the same trust roots, the same wire protocol. The only thing that changes is operator workflow, which gains visibility and fail-loud semantics.

### Neutral

- The declarative artifact is JSON-of-record at v2 schema. Future migration to a small queryable backing store (sqlite, or a Spock-replicated table) is gated by the schema_version field and changes only the accessor library's internals; downstream tools are not affected.
- Ansible inventory remains the source of Ansible-connection-target truth (relay hosts, mgmt-host groups). It stops being a source of peer-set truth. The `[wg_cp0_clients]` group is retired from the operator workflow but may remain in the ini file as an empty stub for compatibility during deprecation.
- The new `wg-cp0` orchestrator binary is a Go workload-shape-shaped tool ([ADR 0112](0112-tech-strategy-workload-shape-language-boundary.md)): per-request control + CLI; Go is the correct language. No Rust here.

### Negative

- **Net new code surface.** The accessor library (~500 LOC Go + tests), the orchestrator binary (~1500 LOC Go + tests), schema v2 migration, audit-record emission, dry-run-diff formatter. The cost is bounded: the four existing Ansible plays + the operator-side `bundle-create` together total more code than the replacement, so the substrate change is net code reduction at the end of the deprecation cycle, but the transition adds code temporarily.
- **Migration story.** During the transition, both the old and new tools must produce correct results against the same fleet. The migration plan in `docs/design/wg-cp0-declarative-substrate.md` §"Migration path" specifies a side-by-side period in which the new tools read from the JSON-of-record and the old tools continue to work, then a flip date after which the old tools are deprecated. The risk is that an operator runs an old tool during the side-by-side window and silently drops peers; mitigated by adding a Pass-0 read of the new schema to the old tools as a warning-only assertion.
- **Single-artifact contention.** The JSON-of-record becomes the load-bearing artifact for the substrate. Loss of the file (operator-laptop corruption, accidental commit revert) is a higher-impact event than today. Mitigations: the file is in git so history is recoverable; the accessor library writes atomically; the audit log records every mutation; a Phase 3+ move to a Spock-replicated backing store moves the single-point-of-failure into the production substrate's existing high-availability layer.

## Alternatives considered

### A. Keep the 4-tool ceremony, rely on operator discipline + checklists

**Rejected.** Operator discipline is what we have today; it produces the three failure shapes above. Checklists cannot reach across the toolchain's silent-default semantics — the operator who fires `wg-cp0-bootstrap.yml` with a `--limit` flag is following a checklist for a relay standup task, not for the peer-set audit task, and the checklist they are following gives them no signal that they are about to amputate the fleet. Discipline cannot substitute for substrate invariants that the tools do not themselves carry.

### B. Promote the Ansible inventory ini file to source-of-truth instead of the JSON

**Rejected.** Inventory files are written for Ansible's connection-target purposes (host groups, `ansible_user`, `ansible_aws_ssm_bucket_name`). Putting peer-set metadata in the ini schema mixes two different artifact concerns. Worse: the ini format does not naturally express pubkey-history with rotation reasons, ACL group sets, or audit metadata. The JSON-of-record is closer to the natural shape; the accessor library makes the schema explicit and validated; the migration cost is lower than reshaping inventory.

### C. Move to a Spock-replicated peer-set table now

**Rejected for v1, but kept as the v3 migration target.** Moving the canonical store from a JSON file to a small Spock-replicated postgres table eliminates the operator-laptop single-point-of-truth concern and makes the table directly queryable from on-site policy engines. The cost: introduces a Spock dependency for operations that today work fully offline on an operator's laptop, and the Spock layer's own operator-discipline concerns (per [ADR 1010](1010-ory-on-spock-mindful-operation.md)) compound with the wg-cp0 ceremony concerns this ADR is closing. The right sequencing is: ship Property 1 (accessor library + JSON-of-record) first; iterate Properties 2-5 against that surface; revisit the storage tier in Phase 3+ when Block 22H peer-key attestation is also pulling on the same artifact.

### D. NixOS-style fully-declarative-system-state configuration

**Rejected.** Generating the entire relay-side wg-cp0.conf + device-side conf from one declarative document is conceptually clean but operationally over-reaching. The transactional unit operators want is "add a peer," "rotate a peer's key" — not "redeploy the entire wg-cp0 substrate from a config flake." A NixOS-shaped system would force every operation through a full apply cycle, which is more disruption per change than the upsert pattern this ADR adopts. Composable benefits (reproducibility, drift detection) are accessible without the full NixOS commitment via the dry-run diff + audit log.

### E. Terraform-providers-style state-then-apply for WireGuard configs

**Rejected.** Terraform's model — declarative resources, a state file, plan-then-apply — is conceptually close to what this ADR adopts. The mismatch: Terraform's state file is its own source of truth, and the plan reconciles real-world drift against state, not state against an explicit higher-level intent. For our case, the JSON-of-record is the intent (operator-curated), and the live state is what the tools reconcile against the intent. The Terraform model would invert that — the live state would be authoritative and the JSON would be a record of past actions. The operator-curated-intent model fits the threat model better: an attacker who modifies the live state (e.g., adds an unauthorized peer to a relay) should be detected by the reconcile as a "WOULD REMOVE" line, not silently absorbed into a refreshed state.

### F. ConfigMap-like push-based approach (operator edits a YAML, a daemon applies)

**Rejected.** A long-running reconcile daemon on each relay watching a shared config and applying drift introduces a new always-on substrate, a new daemon to harden, a new failure mode (the daemon goes wedged and stops reconciling). The operator-fired model is sufficient for the operation rate we expect (single-digit peer-set changes per week even at fleet steady-state) and keeps the substrate small. A ConfigMap-style reconciliation loop becomes interesting if and when peer-set churn rises by an order of magnitude; the accessor library makes that future change isolated to one component.

### G. Adopt an existing IaC tool (Pulumi, Crossplane, etc.) instead of building

**Rejected.** Every existing IaC tool in this space carries its own substrate footprint (separate state backend, separate cloud-or-API integrations, separate auth model) that does not compose with the offline-CA / wg-cp0 trust model. A bespoke Go orchestrator that runs locally on an operator's laptop, reads a file under the offline-CA's age-encrypted protection, and talks to the relays over wg-cp0 itself is smaller and sovereign-respecting. The IaC-style benefits we want (declarative state, dry-run, audit) are achievable in ~2000 LOC of Go; importing them through a 50,000-LOC general-purpose tool is the wrong trade for this scope.

## Implementation

Tracked at `docs/project/implementation-plan.md` Block 22R sub-blocks 22R.1 through 22R.4. The full design — schema definitions, accessor library API, orchestrator command surface, migration path, deprecation timeline — lives at `docs/design/wg-cp0-declarative-substrate.md`.

- **22R.1** — Single declarative source-of-truth: schema v2, accessor library, schema validator, dry-run plumbing primitive. Foundation for the rest.
- **22R.2** — Reconcile-not-rewrite orchestrator: `wg-cp0` binary with `reconcile` / `onboard` / `rotate` / `update-endpoints` / `update-ttl` / `remove` / `add-relay` subcommands, all dry-run-by-default with structured diff output.
- **22R.3** — ✅ landed via parallel track `wg-cp0-syncconf-migration` (PR #31): Pass 2A's `restart wg-cp0` handler becomes `wg syncconf` for allowlist-only changes; listener-restart reserved for listener-shape changes (port/address/MTU). Engineering-orthogonal to 22R.1+22R.2 — does not depend on the accessor library and shipped independently.
- **22R.4** — Deprecation of the four-tool ceremony: side-by-side period during which old tools continue to work but emit a deprecation warning when invoked; documentation flip; old playbooks moved to a `deprecated/` subtree; operator runbooks rewritten to use the new orchestrator.

Acceptance for the parent block (Property 5 orchestrator passes its dry-run against the production peer set with no unexpected diffs; one end-to-end ceremony flows entirely through the new orchestrator without falling back to surgical-update; audit log carries one full ceremony) is what flips this ADR from Proposed to Accepted.

## Related

- [ADR 0321 — wg-cp0 is a perimeter, not an ACL layer](0321-wg-cp0-perimeter-not-acl.md) — prerequisite; this ADR composes with it.
- [ADR 0313 — silent control plane outer WG wrap](0313-silent-control-plane-outer-wg-wrap.md) — substrate this ceremony layer manages.
- ADR 0319 — wg-cp0 mesh subnet 198.18.0.0/24 — addressing the substrate operates against.
- [ADR 0407 — internal X.509 CA for service TLS](0407-internal-x509-ca-for-service-tls.md) — trust root for the bundle signing the peer-set artifact composes with.
- [ADR 0112 — workload-shape language boundary](0112-tech-strategy-workload-shape-language-boundary.md) — Go is the correct language for the per-request CLI orchestrator.
- [ADR 1017 — substrate-invariant test class](1017-substrate-invariant-test-class.md) — pattern this ADR inherits for combinatorial invariant assertions.
- F-114 — wg-cp0 rotation serialization gap — drift class this substrate closes.
- F-116 — wg-cp0 asymmetric AllowedIPs — architectural-inversion half settled by ADR 0321; substrate-level recurrence protection is here.
- F-117 — `work release` no branch-content merge check — gap that lost the original ADR 0320 draft; this ADR is the re-do.
- `docs/design/wg-cp0-declarative-substrate.md` — full design.
- [`docs/design/wg-cp0-peer-lifecycle.md`](../design/wg-cp0-peer-lifecycle.md) — current ceremony shape (replaced by this substrate at the operator surface).
- `docs/design/wg-cp0-prod.md` — day-1 topology + bundle format; unchanged.

---

*See `GLOSSARY.md` for terms and acronyms.*
