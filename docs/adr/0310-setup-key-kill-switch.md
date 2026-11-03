# ADR 0310 — Setup-key kill switch and emergency revoke

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0307-setup-key-rotation.md`](0307-setup-key-rotation.md), [`0817-credential-hygiene.md`](0817-credential-hygiene.md) |

## TL;DR (for a layperson)

If we suspect a setup key has been leaked, a single command
revokes every active setup key in the system in one step — no
peer can use a leaked key after the kill switch fires. Existing
enrolled peers keep working; only new enrollments are stopped.

## Context

Setup-key rotation ([`0307`](0307-setup-key-rotation.md)) bounds
exposure via short TTLs, but a fresh leak still allows enrollment
within the TTL window. We need a faster recovery path: revoke
all active keys at once. The mesh property that enrollment is
separate from operation ([`0201`](0201-enrollment-vs-mesh-op-separation.md))
means a kill switch is operationally cheap — existing peers are
not affected.

## Decision

The setup-key kill switch is a single command:

- `ops/ansible/scripts/setup-key-kill-switch.sh` revokes every
  active setup key across all sites (Spock-replicated).
- **Effect:** enrollment is blocked until new keys are issued
  ([`0307`](0307-setup-key-rotation.md) helpers).
- **Audit:** kill-switch invocation writes a high-priority
  audit-log event with operator identity and timestamp.
- **Reversal:** new keys can be issued immediately after; the
  kill switch does not require a system restart.

The kill switch is part of the incident-response runbook
([`0818`](0818-incident-response-runbook.md)).

## Alternatives considered

- **Per-key revocation only.** Slower under incident; an
  attacker can race against per-key revoke. Rejected as the
  primary recovery path; remains available.
- **Disable the management plane entirely.** Disrupts
  legitimate peers; over-kill. Rejected.
- **Password-protect the helpers.** Adds friction for legitimate
  use without significantly raising attacker cost. Rejected.

## Consequences

**Positive.** Rapid recovery from leaked-key incidents.
Existing peers are not affected. Audit-clear (the kill switch is
visible in the audit pipeline).

**Negative.** Active enrollment workflows are interrupted until
keys are reissued. Acceptable as the incident-response
trade-off.

## Assessor view (SCA-R)

The kill switch serves IA-5 (authenticator management — rapid
revocation), AC-2 (account management — emergency control), and
IR-4 (incident handling). It maps onto NIST SP 800-61 IR
playbook patterns for credential compromise.

## Trigger conditions for revisit

- Kill-switch usage produces operational confusion (e.g.
  legitimate workflows interrupted unnecessarily). Add a
  dry-run or confirmation step; do not weaken the recovery
  speed.
- A more granular per-role kill switch becomes useful (revoke
  only `peer` role keys, leave `admin` alone). Add as
  separate operation; do not displace the broad kill switch.

---

*See `GLOSSARY.md` for terms and acronyms.*
