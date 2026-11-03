# ADR 0818 — Incident response runbook + quarterly drills

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0310-setup-key-kill-switch.md`](0310-setup-key-kill-switch.md), [`0811-audit-log-integrity.md`](0811-audit-log-integrity.md), [`0817-credential-hygiene.md`](0817-credential-hygiene.md) |

## TL;DR (for a layperson)

When a security incident happens (key compromise, suspected
intrusion, partner-flagged anomaly), we follow a written
runbook. The runbook is exercised every quarter via a drill
so operators stay familiar with it under non-emergency
conditions. Drills uncover gaps before real incidents do.

## Context

Without a runbook, incident response is ad-hoc — operators
make it up under pressure, miss steps, lose evidence. Without
drills, the runbook stays untested; on first real use it
turns out to be wrong.

## Decision

The incident-response runbook lives in `docs/operations/`
and covers:

- **Detection-to-engagement**: who pages whom, on what
  signals.
- **Setup-key kill switch** ([`0310`](0310-setup-key-kill-switch.md))
  — when to use, who can authorize, audit trail.
- **Credential rotation** ([`0817`](0817-credential-hygiene.md))
  — under-incident emergency cadence.
- **Audit-log preservation**: snapshot the audit-integrity
  tier ([`0811`](0811-audit-log-integrity.md)) at incident
  start.
- **Tamper-response false positives**: recovery from
  accidental zeroization ([`0810`](0810-tamper-response.md)).
- **Logical-corruption rollback**: per-site PITR procedure
  ([`0803`](0803-no-cross-site-pitr.md)).
- **Federation-side coordination**: which partner contacts
  to notify, in what order.

Quarterly drills:

- **Tabletop**: hypothetical scenario walked through with
  operator team.
- **Live drill**: limited-scope exercise (e.g. simulated
  key compromise with kill-switch invocation in a
  validation env).
- **Post-drill review**: gaps identified; runbook updates
  land in same week.

Tracked as Block 18E in
`../project/implementation-plan.md`.

## Alternatives considered

- **Implicit / verbal incident response.** Loses the
  written trail. Rejected.
- **Annual drills only.** Memory decays; quarterly is the
  cadence that holds.
- **Outsource incident response.** Sovereignty issue.
  Rejected as primary.

## Consequences

**Positive.** Incident response is reproducible. Drills
catch gaps. Runbook is updated continuously.

**Negative.** Quarterly drill cost. Acceptable.

## Assessor view (SCA-R)

The IR runbook + drills serve IR-1 (incident response
policy), IR-3 (testing), IR-4 (handling), IR-6 (reporting),
and IR-8 (incident response plan). NIST SP 800-61
(computer security incident handling guide) is the standard
reference; our pattern aligns.

The drill cadence supports CA-7 (continuous monitoring)
in the operational dimension.

## Trigger conditions for revisit

- A real incident reveals runbook gaps. Update in the
  same recovery window.
- Drill cadence proves wrong. Adjust; document.

---

*See `GLOSSARY.md` for terms and acronyms.*
