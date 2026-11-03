# ADR 0810 — Tamper response — physical chassis intrusion handling (operational)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0606-tamper-response-zeroization.md`](0606-tamper-response-zeroization.md), [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md), ADR 0903 |

## TL;DR (for a layperson)

[`0606`](0606-tamper-response-zeroization.md) is the crypto
side of tamper response — what happens to the keys when the
chassis is opened. This ADR covers the operational side: how
we arm and disarm the response, who can authorize maintenance,
how we handle false positives, and how the workflow runs
through the policy engine.

## Context

Crypto zeroization on tamper trip is destructive. Operationally
this requires:

- **Arming policy** per site (deployed sites: armed; lab
  sites: disarmed).
- **Maintenance disarm** before opening the chassis;
  re-arm after.
- **Audit trail** for every arm / disarm event.
- **False-positive handling** — if a sensor trips during
  shipping or accidental bump, recovery procedure must be
  documented.

The crypto mechanics are in
[`0606`](0606-tamper-response-zeroization.md). The operational
mechanics are this ADR.

## Decision

Tamper-response operations:

- **Arm by default at deployed sites.** All forward sites
  ship armed.
- **Disarm via policy engine** ([`0805`](0805-policy-engine-active-passive.md))
  — fast-path approval for routine maintenance with two-of-N
  peer approval.
- **Disarm window**: 4-hour default; auto-rearm after window.
- **Audit**: every arm/disarm event written to the
  audit-integrity tier ([`0811`](0811-audit-log-integrity.md)).
- **False-positive recovery**: documented in the incident-
  response runbook ([`0818`](0818-incident-response-runbook.md));
  full bring-up from the bootstrap bundle
  ([`../design/bootstrap-bundle.md`](../design/bootstrap-bundle.md)).
- **Per-site policy**: lab sites and validation envs ship
  disarmed by default.

## Alternatives considered

- **Always-armed; manual disarm via SSH/console.** Easier
  to bypass; no audit trail. Rejected.
- **Always-disarmed; arm only on demand.** Loses the
  protection. Rejected as default for deployed sites.
- **No policy-engine integration.** Loses approval audit.
  Rejected.

## Consequences

**Positive.** Tamper response is operationally usable.
Maintenance flow is documented and auditable. False-positive
recovery is bounded.

**Negative.** Disarm-rearm discipline costs operator
attention. Acceptable for the protection.

## Assessor view (SCA-R)

Operational tamper response serves PE-3 (physical access
control), PE-6 (monitoring physical access), and CM-3
(change control — arm/disarm is a tracked event). The
policy-engine-mediated arming workflow gives an auditable
trail that purely-mechanical tamper systems don't.

## Trigger conditions for revisit

- Disarm-rearm friction is unsustainable. Lengthen the
  default window; do not abandon the workflow.
- A new threat model emerges (e.g. rapid extraction
  attempts). Re-evaluate response speed.

---

*See `GLOSSARY.md` for terms and acronyms.*
