# ADR 0501 — Rust interceptors as the integration extension point

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0508-audit-logger-interceptor.md`](0508-audit-logger-interceptor.md) |

## TL;DR (for a layperson)

The way we add custom logic to the data fabric — auditing every
message, transforming a data type, enforcing a policy — is by
writing a small Rust program that the data fabric loads as a
plugin. We use Rust because the data fabric is Rust-native (so
no language boundary), Rust gives us memory safety guarantees
that are valuable at the integration layer, and the team has
made a strategic move toward Rust for high-performance / data-
path work.

## Context

Zenoh interceptors run inside the router process. Two-language
interceptors (e.g. WASM / Go FFI) add complexity and overhead
without commensurate benefit. Rust interceptors compile to
native, share Zenoh's memory model, and give the team a
consistent target for fabric extensions.

The 2026-04-24 tech-strategy update
(`../design/tech-strategy.md`)
codifies the Rust direction for data-services work.

## Decision

Interceptors are written in Rust:

- **Workspace:** `zenoh-plugins/` Cargo workspace.
- **CI:** cargo-audit, cargo-clippy, cargo-test, deny
  unsafe except where reviewed.
- **Toolchain:** pinned in `rust-toolchain` (currently 1.88;
  bumps tracked in implementation plan).
- **Plugin name = config key.** Plugin names use the
  underscore convention (e.g. `audit_logger`).
- **Distribution:** built from source, signed, pinned by
  digest, deployed via Ansible.
- **Reference:** the audit-logger interceptor
  ([`0508`](0508-audit-logger-interceptor.md)) is the
  reference implementation pattern.

Future interceptor classes (transform, policy enforcement,
quarantine, schema-evolution adapter) all follow this pattern.

## Alternatives considered

- **WASM-based interceptors.** Newer model; cross-language
  flexibility but operational maturity not yet there.
  Rejected.
- **Go FFI interceptors.** Doable but adds language boundary
  overhead and FFI risk. Rejected.
- **Out-of-process sidecar interceptors.** Adds latency and
  delivery-semantics complexity. Rejected.

## Consequences

**Positive.** Native performance. Zenoh-native memory model.
Rust safety properties at the integration layer. CI tooling
mature.

**Negative.** Rust learning curve for contributors used to
Go or Python. We accept; the strategic direction is Rust for
data services.

## Assessor view (SCA-R)

Rust interceptors serve SR-3 (supply chain — Rust toolchain
+ cargo-audit), SI-7 (software, firmware, integrity — memory
safety reduces specific CVE classes), and CM-7 (least
functionality — interceptors are explicit, named, signed).
The audit-logger ([`0508`](0508-audit-logger-interceptor.md))
is itself an audit control (AU-2 / AU-12).

## Trigger conditions for revisit

- WASM interceptor model matures. Re-evaluate; consider
  hybrid (WASM for partner-supplied extensions, Rust for
  ours).
- A specific interceptor class doesn't fit Rust well.
  Carve out the exception; do not abandon the default.

---

*See `GLOSSARY.md` for terms and acronyms.*
