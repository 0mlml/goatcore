# ADR 1002 — Two-phase enrollment for management hosts

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`1001-bootstrap-vs-refresh.md`](1001-bootstrap-vs-refresh.md), [`0307-setup-key-rotation.md`](0307-setup-key-rotation.md) |

## TL;DR (for a layperson)

A new management host goes through two enrollment phases:
first as a generic AWS instance reachable via SSM, then as a
mesh peer reachable via WireGuard over its mesh IP. The
bootstrap playbook handles phase 1; the refresh playbook
takes over from phase 2. This is the operational expression
of the bootstrap-vs-refresh pattern.

## Context

The two-phase enrollment is the mechanical companion to
[`1001`](1001-bootstrap-vs-refresh.md). Phase 1 has SSM
reachability; phase 2 has mesh reachability.

This ADR documents the *enrollment* side; [`1001`](1001-bootstrap-vs-refresh.md)
documents the *deployment* side.

## Decision

Two-phase mgmt-host enrollment:

**Phase 1 — Bootstrap (SSM reachable).**

1. Terraform provisions the EC2 / mgmt-host instance.
2. `inventory/generate.sh` produces SSM-connection inventory.
3. `<env>-bootstrap.yml` runs over SSM, installs NetBird and
   runs `sudo netbird up --management-url <env>.legacy.example
   --setup-key $KEY` from the bootstrap setup-key role
   ([`0307`](0307-setup-key-rotation.md)).
4. Host enrolls into the mesh; mesh IP assigned.

**Phase 2 — Refresh (SSH-over-mesh reachable).**

1. Operator fills mesh IPs into
   `inventory/mgmt-hosts-mesh.ini`.
2. `<env>-refresh.yml` runs over SSH-over-mesh for routine
   ops.
3. Bootstrap remains available for full rebuilds.

The setup-key used in phase 1 is `bootstrap` role with 1-day
TTL ([`0307`](0307-setup-key-rotation.md)) — auto-revokes after
the bring-up window.

## Alternatives considered

- **Single-phase enrollment via SSM forever.** SSM round-trip
  per Ansible task is slow. Rejected for routine ops.
- **Single-phase enrollment via mesh from day one.** Doesn't
  work — mesh isn't there yet. Rejected.

## Consequences

**Positive.** Clean phase boundary. Bootstrap setup-key has
short TTL and auto-revokes. Mesh-side ops are fast.

**Negative.** Operator must remember to fill mesh IPs into
inventory after phase 1. Documented in CLAUDE.md and the
runbook.

## Assessor view (SCA-R)

The two-phase pattern serves AC-2 (account management — peer
enrollment), CM-3 (change control — both phases produce
auditable Ansible runs), and IA-5 (authenticator management —
short-TTL bootstrap setup keys).

## Trigger conditions for revisit

- A new mgmt-host class can't reach SSM (bare-metal in a
  contested env). Use offline-enrollment-bundle path
  ([`0308`](0308-offline-enrollment-cbor-bundles.md)) for
  phase 1; refresh remains.

---

*See `GLOSSARY.md` for terms and acronyms.*
