# ADR 1091 — Quarantine isolation lane for suspected threat actors

| Field | Value |
|---|---|
| Status | Accepted (2026-07-17) |
| Date | 2026-07-17 |
| Related | [membership-lifecycle-and-transfer.md](../design/membership-lifecycle-and-transfer.md), [ADR 1088](1088-membership-transfer-engine.md) (transfer engine), [ADR 1089](1089-probation-isolation-lifecycle.md) (probation — the dormancy sibling), [ADR 1090](1090-microscope-per-entity-behavior-lens.md) (watch lens), [ADR 1068](1068-anomaly-probation-lifecycle.md), [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md) (sever path) |

## Context

When an identity or peer shows possible or confirmed threat-actor behavior, the
operator needs three graduated responses, none of which existed as a first-class
mesh state:

1. **Observe in place** — leave the entity's access untouched and watch it.
2. **Quarantine and observe** — cut its reach to a minimal, fully-logged surface
   while the investigation runs.
3. **Sever** — deliberate removal (mesh-first revoke, ADR 1031).

Probation (ADR 1089) is the wrong lane for this: it is dormancy-driven,
auto-entered, and deliberately *self-service-recoverable* — it serves an
explainer page and a petition control. A suspected threat actor must get none of
that: no notification that they are being watched, and no self-service path out.

## Decision

**A `QUARANTINED` satellite group — one flat group per goatnet prefix, declared
in the taxonomy and stamped like every canonical suffix — implementing the
middle disposition. Entry, exit, and the other two dispositions are
operator-driven; nothing about quarantine is automatic or self-service.**

- **The triad maps to existing machinery.** Observe-in-place is the microscope
  watch mission (ADR 1090) plus flow-log attention — no group change, no new
  mechanism. Quarantine is a transfer-engine move (ADR 1088) into
  `<PREFIX>-QUARANTINED`, out of **all** other groups (roles, P2P, labels).
  Sever is ADR 1031, unchanged. The group exists only for the middle case.
- **Isolation posture (mesh, L4):** a quarantined peer initiates only to
  `INFRA` on tcp `:443` + DNS (tcp/udp `:53`/`:5353`). **No fabric ports**
  (`:7447`/`:7448`) — a compromised slot cert must not keep publishing.
  `INFRA → QUARANTINED` stays open on any port so the substrate can probe and
  inspect. No lateral reach, no external-infra, and `QUARANTINED` never appears
  in a P2P-rule destination.
- **Why keep :443 at all:** total L4 severance is indistinguishable from an
  outage and tips the actor off, and NetBird denies are silent (no rich deny
  log). Keeping the web path means every attempt the actor makes lands on
  traefik/oauth2-proxy — the one surface where their behavior is fully logged.
  The observation point *is* the reach we leave them.
- **No explainer, no petition** — the deliberate inversions of ADR 1089.
  The Stage-3 route-override middleware, when it lands, must treat the
  `quarantine` marker as *deny-as-normal* (ordinary auth failure), never the
  probation explainer.
- **Flat, not role-suffixed** — the deliberate inversion of ADR 1089's
  `PROBATION-<role>` choice, and for a reason: probation's restore is
  mechanical (cron-scale, the group name is the restore target); quarantine's
  exit is the close-out of a human investigation, entry is rare, and the
  transfer-engine audit record (`OperatorActionV1`) already names the prior
  groups exactly. A role-suffixed quarantine would also leak the account's
  role into a lane where we deliberately minimize signal.
- **Ory plane: no new group** (F-213 discipline — mesh vocabulary never enters
  Ory). Quarantine entry sets a `quarantine` trait marker beside ADR 1089's
  `probation` marker. v1 enforcement is the mesh move; L7 recognition of the
  marker rides the Stage-3 middleware build. **Privileged accounts are never
  merely quarantined**: an Ory `PLATFORM-ADMIN`/`IAM-ADMIN` under suspicion is
  severed and rotated immediately — quarantine-with-console-rights would leave
  an actor able to mint identities while "contained."
- **Replication:** `QUARANTINED` is part of the canonical suffix set, so site
  bootstrap and the exercise stamp template carry it to every prefixed
  environment (locations, exercises, activities) automatically.

## Alternatives

- **Reuse `PROBATION-<role>`:** wrong semantics in both directions — probation
  notifies and offers self-service return; quarantine must do neither. Rejected.
- **Zero-reach quarantine (no :443/DNS):** removes the only logged surface and
  signals the actor. Rejected — severance that complete is the sever
  disposition, done deliberately via ADR 1031.
- **`QUARANTINED-<role>`:** symmetry with probation, but restore rides the
  audit record anyway, entry is rare and human, and the suffix leaks role
  signal. Rejected.
- **Auto-quarantine on anomaly signals:** ADR 1068 already deferred
  auto-reduce-access as the pattern's footgun; a false-positive quarantine of a
  legitimate admin is an availability incident. Entry stays operator-driven.

## Consequences

One new group + two matrix rules per goatnet, a `quarantine` trait marker, and
a documented triad runbook — no new daemon or enforcement tier. The operator
gains a reversible, silent containment state between "watch" and "sever," with
the actor's every remaining move logged at the L7 gates. Costs: the Stage-3
middleware must distinguish `quarantine` from `probation` markers (deny vs
explain), and until the transfer engine (ADR 1088 Stage 1) ships, quarantine
moves are dashboard/API operations following the same
strip-all-groups-add-QUARANTINED recipe by hand. Risk accepted: a quarantined
actor retains a logged web surface; if even that is too much, sever.
