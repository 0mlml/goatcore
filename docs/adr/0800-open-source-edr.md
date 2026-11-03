# ADR 0800 — Open-source EDR — Falco + osquery + auditd

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0820-continuous-vuln-scanning.md`](0820-continuous-vuln-scanning.md) |

## TL;DR (for a layperson)

Most defense IT systems use commercial closed-source endpoint
detection and response (EDR) tools. We use open-source tools
instead: Falco watches for suspicious behavior in containers,
osquery checks the state of every host, and auditd records
every system-level event. This combination gives us
sovereign, auditable, and modifiable EDR coverage.

## Context

DoD-approved closed-source EDR (CrowdStrike, SentinelOne,
etc.) introduces three structural problems for our posture:

- **Sovereignty.** Closed-source agents on every node we
  cannot inspect.
- **Coalition partner sharing.** Some EDR licenses restrict
  partner use.
- **Phone-home requirements.** Many EDRs depend on cloud
  back-ends; violates Island Mode.

Open-source EDR (Falco for runtime, osquery for state,
auditd for syscall) is sovereign, modifiable, and shareable.

## Decision

The open-source EDR stack:

| Tool | Layer | What it does |
|---|---|---|
| **Falco** | Container runtime | Detects anomalous container behavior; rules in YAML; alerts on policy violations. |
| **osquery / Fleet** | Host state | Queryable host inventory and configuration drift detection. |
| **auditd** | Syscall | Linux kernel audit subsystem; records every defined syscall. |

All three ship to the audit-integrity tier
([`0811`](0811-audit-log-integrity.md)). Block 18G in
`../project/implementation-plan.md`
tracks rollout. Block 17D integrity tier
([`../design/egress-proxy.md`](../design/egress-proxy.md) §5.6)
is the storage destination.

## Alternatives considered

- **DoD-approved closed-source EDR.** Loses sovereignty;
  partner-sharing limited. Rejected as primary.
- **Single open-source tool.** Each tool covers one layer;
  complementary coverage requires all three.
- **No EDR.** Disqualifies the system from accreditation. Rejected.

## Consequences

**Positive.** Sovereign, modifiable, shareable EDR. Three-
layer coverage (runtime, state, syscall). All rules
reviewable in repo.

**Negative.** Operator burden — three tools to tune. Mature
upstreams keep this manageable.

## Assessor view (SCA-R)

The stack serves SI-3 (malicious code protection — Falco
rules), SI-4 (system monitoring), AU-2 (event logging —
auditd), and CM-8 (component inventory — osquery).

## Trigger conditions for revisit

- A specific authorization explicitly forbids open-source
  EDR. Negotiate.
- Falco / osquery / auditd governance shifts. Substitute.

---

*See `GLOSSARY.md` for terms and acronyms.*
