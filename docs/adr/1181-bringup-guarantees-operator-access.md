# ADR 1181 — the site build-out guarantees and verifies operator access on every encrypted node

- **Status:** **Proposed.** On merge: `mgmt-host-baseline.yml` seeds the platform-admin roster into the booted-OS `authorized_keys` and fails closed on an empty roster; `mgmt-host-luks.yml` invokes `dropbear-initramfs` (gated) on `:2222` fed the same roster; phase-14 acceptance fails closed if operator SSH login to a node is absent. No live node changes until the chain is re-run against it.
- **Date:** 2026-09-15
- **Deciders:** operator
- **Evidence:** F-539 — a node came up encrypted with no operator key on the full OS; the dropbear role was orphaned; single-key seeding; no access gate.
- **Related:** host-site-node-disk-encryption §"operator-access invariant" · [platform-admin-lifecycle](../design/platform-admin-lifecycle.md) · ADR 0612

## Decision
One platform-admin roster (`admin_authorized_keys`, sourced from `PLATFORM_ADMIN_SSH_PUBKEYS` — env → list, never a committed pubkey file) is the single source of truth, and the bring-up chain seeds it into **both** access doors and verifies it before a box is "done":

1. **Full OS** — `mgmt-host-baseline.yml` installs the roster into the login account's `authorized_keys` (additive, never wiping the bootstrap key), and **refuses to build** if the roster is empty.
2. **Unlock door** — `mgmt-host-luks.yml` invokes the `dropbear-initramfs` role (gated `dropbear_remote_unlock`) fed the same roster, on `:2222` (the `:2222`=unlock / `:22`=full-OS convention).
3. **Verification** — `acceptance-tests.yaml` carries `operator-ssh-login-*` rows so phase 14 **fails closed** if any node's operator access is absent — loudly, while the box is still in hand.

## Alternatives
- Seed only the builder's key (status quo) — rejected: a re-provision or image swap strands everyone, undetected.
- Trust the seed, verify nothing — rejected: a silent lockout can survive for weeks.
- Fold CA-operator access into this roster — rejected: CA trust is a separate, gated grant ([platform-admin-lifecycle](../design/platform-admin-lifecycle.md)).
