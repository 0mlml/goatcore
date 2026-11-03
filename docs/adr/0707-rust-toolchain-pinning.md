# ADR 0707 — Rust toolchain pinning + cargo-audit deferred-list policy

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0501-rust-interceptors.md`](0501-rust-interceptors.md), `0704-multi-scanner-regime.md` |

## TL;DR (for a layperson)

Rust components in the system are built with a specific Rust
compiler version that we pin in the source. We also keep a
deliberate list of known-but-not-yet-fixed advisories in our
Rust dependencies (pulled in transitively from upstream
libraries). The list is reviewed regularly and entries get
removed when fixes ship upstream. This is how we run Rust
components without panicking on every transitive advisory.

## Context

Rust's tooling (rustup, cargo) supports per-project toolchain
pinning via `rust-toolchain.toml`. This is essential for
reproducibility — different toolchain versions produce
different binaries.

cargo-audit checks dependency advisories. In a healthy Rust
project, most advisories come from transitive dependencies
that haven't been updated upstream yet. Failing CI on every
transitive advisory is too noisy; ignoring all advisories is
unsafe. The middle path: a deferred list with named
advisories, justifications, and review dates.

## Decision

Rust toolchain pinning + deferred-list policy:

- **Toolchain pin location:** two places that must agree —
  - `zenoh-plugins/rust-toolchain.toml` (`channel = "1.88.0"`)
    so local `cargo` invocations honor the pin via rustup.
  - `.github/workflows/zenoh-image.yml` via
    `dtolnay/rust-toolchain@stable` with `toolchain: "1.88.0"`
    for CI.

  Bumps are deliberate ADR-tracked changes that update both
  locations in the same PR.
- **CI Rust version:** matches the pinned toolchain; cargo-
  audit needs Rust 1.85+ but the build pipeline uses the
  full pinned version.
- **cargo-audit in CI:** runs on every Rust workspace; fails
  on advisories *not* in the deferred list.
- **Deferred list:** in `zenoh-plugins/.cargo-audit-ignore`
  (or equivalent), each entry includes:
  - Advisory ID (RUSTSEC-YYYY-NNNN).
  - Justification (e.g. "transitive via X; upstream PR Y
    not yet merged").
  - Next review date.
- **Quarterly review:** the deferred list is reviewed
  quarterly; entries with shipped upstream fixes are
  removed; long-stalled entries are escalated.

## Alternatives considered

- **No pinning.** Build reproducibility broken. Rejected.
- **No deferred-list policy (block on every advisory).** CI
  becomes too noisy; engineers lose discipline. Rejected.
- **Block-list every advisory permanently.** Loses tracking
  of unfixed issues. Rejected.

## Consequences

**Positive.** Reproducible builds. Advisory hygiene without
panic-on-every-transitive. Quarterly review keeps the list
small.

**Negative.** Discipline cost. Acceptable.

## Assessor view (SCA-R)

Toolchain pinning serves CM-2 (baseline configuration), SR-3
(supply chain controls), and SR-4 (provenance). The
deferred-list policy is the operational expression of accepted
residual risk for transitive advisories — auditable per
entry. RA-5 (vulnerability scanning) is satisfied by the
combination of cargo-audit + the review cadence.

## Trigger conditions for revisit

- Toolchain bump required (e.g. by a dependency demanding
  a newer Rust version). Bump; document; rebuild.
- Deferred-list grows past ~20 entries. Review for stalls;
  escalate or remove.

---

*See `GLOSSARY.md` for terms and acronyms.*
