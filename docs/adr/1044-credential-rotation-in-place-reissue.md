# ADR 1044 — Credential rotation: in-place cert re-issue without anchor churn

| | |
|---|---|
| Status | Accepted (2026-06-14) — live-fired, measurement M-069 |
| Related | [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md) (revocation — the sibling cut path), [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (per-uuid slot pool; the anchor model rotation preserves), [ADR 1039](1039-slot-alias-display-layer.md) (slot alias — survives rotation unchanged), [ADR 0407](0407-internal-x509-ca-for-service-tls.md) (offline CA, no CRL/OCSP), [ADR 0859](0859-easy-rollback-engineering-principle.md) (easy rollback). Design: `credential-rotation.md`. |

## Plain-language summary

A peer's cert can need to change without the peer's *identity* changing — its
1-year validity is running out, its key may be compromised, or (the immediate
trigger) it was minted under an old URI-SAN scheme we want to retire. Until now
the only way to change an issued cert was to **delete the peer and re-onboard
it to a brand-new anchor** — which moves its write namespace, its ZID, and its
alias, and orphans everything it stored. That is a teardown of identity, not a
cert change. This ADR adds **rotation**: re-issue a fresh leaf cert for the
**same anchor**, leaving the anchor, alias, write namespace, and ACL exactly as
they were. Rotation is the missing sibling of revocation (ADR 1031): revocation
takes a principal *out*; rotation re-keys one *in place*.

## Context

The portal mints certs along exactly one path: `POST /api/vendors`, which in
per-uuid-slot-pool mode (ADR 1025) **allocates a fresh slot** and mints a leaf
whose CN is that slot's anchor. There is no way to re-issue a cert for an
*existing* anchor. Two realities forced the question at a goatnet
go-live:

1. **A scheme retrofit with no clean tool.** Separating a new goatnet from its
   predecessor made the cert URI-SAN a per-goatnet machine value
   (`urn:goat:zagros:org=` on the new goatnet, not the predecessor's
   `urn:goat-<old>:vendor=`). The already-issued synthetic certs on the new
   goatnet carry the old scheme. The slot ACL matches CN, not URI-SAN, so they
   keep working — but retiring the predecessor's string off them was parked as
   "delete + re-onboard," i.e. destroy each anchor and rebuild it. The portal's
   own error hints said it outright: *"there's no way to retrofit URI-SAN
   without a fresh leaf."*

2. **Coalition certs are 1-year and will need rotation.** Real orgs (partner
   organizations, government offices, vendors) will hit **expiry** and **compromise**, both of which
   demand a fresh leaf *without* losing their anchor/identity/stored data.
   Revocation (ADR 1031) answers "cut them off." Nothing answered "re-key them."

The building block already existed: `certmint.Minter.Mint(prefix, {CommonName:
anchor})` mints a fresh leaf for a specified anchor — it is exactly what
slot-onboard calls *after* it allocates the slot. Rotation is that mint minus
the allocation.

There is no CRL/OCSP (ADR 0407), so rotation does **not** invalidate the old
leaf cryptographically. The old cert keeps validating until it expires; the cut
of the old credential, when that matters (compromise), is the operator swapping
the cert on the host + the short cert lifetime, not PKI revocation — consistent
with the mesh-first doctrine of ADR 1031.

## Decision

1. **Rotation re-issues in place, never re-allocates.** A new operator-gated
   endpoint `POST /api/vendors/{anchor}/rotate` looks up the existing peer,
   mints a fresh leaf reusing the **same anchor** as the CN (slot mode) or the
   default per-vendor CN (per-vendor mode), and returns a fresh one-time bundle.
   It is the onboard mint path minus `AllocateSlot` + `CreateVendor`. **404 if
   the anchor doesn't exist** — rotation re-issues, it does not create.

2. **Identity is invariant across rotation.** Anchor, CN, slot assignment, slot
   alias (ADR 1039), write namespace, registry row, and the pre-baked ACL are
   **all unchanged**. No slot is consumed, no ACL is rendered, no router
   restart is needed — the rule already admits the unchanged anchor/CN. Only
   the leaf cert is new.

3. **The portal api_key rotates with the cert.** The old key's plaintext is
   unrecoverable (only its bcrypt hash is stored) and a rotation/compromise
   event wants fresh credentials anyway, so rotation mints a fresh api_key and
   swaps the stored hash. This is a credential, not identity — rotating it is
   the point, not a violation of invariance.

4. **URI-SAN is re-derived from the current minter config.** That is the
   scheme-retrofit mechanism: a peer minted under the old scheme, rotated after
   the config changed, comes back carrying the new scheme — with the same
   anchor. No special "retrofit" code path; rotation *is* the retrofit.

5. **Three surfaces, one chokepoint.** Portal endpoint → `goat admin rotate
   <anchor|alias>` (resolves alias→anchor fail-closed, prints the fresh bundle
   URL) → an ansible rotate-and-swap task for owned peers / the synth fleet
   (rotate → fetch bundle → swap cert+key → restart container). Mirrors the
   revoke surface shape.

6. **First use closes the parked retrofit non-destructively.** Re-mint the
   synthetic certs in place to `urn:goat:zagros:org=` — rotate each, swap, restart
   — with zero anchor change. Strictly better than the delete+re-onboard that
   was the only prior path.

## Consequences

**Positive.** Closes a real platform gap with a small, composable build (the
hard part — minting a leaf for a given anchor — already existed). Gives
coalition orgs an expiry/compromise rotation story that keeps their identity
and stored data. Turns the parked URI-SAN retrofit into a non-destructive
in-place re-mint. No fork, no CRL, no ACL churn, no restart.

**Negative / cost.** Without CRL/OCSP, rotation does not cryptographically kill
the old leaf — a compromised old cert stays valid until expiry unless the peer
is also cut at the mesh (ADR 1031) or the cert lifetime is short. Rotation is
therefore a re-key, not a revoke; the two compose but neither replaces the
other. The api_key necessarily rotates, so any out-of-band consumer of the old
portal token must take the new one from the bundle.

**Neutral.** Per-vendor mode rotation keeps the default `<prefix>-trial-
peer` CN and needs no ACL render either (the prefix is unchanged). Rotation
does not change the slot-pool exhaustion math — it consumes zero slots.

## Alternatives considered

- **Delete + re-onboard (the status quo).** Rejected: changes the anchor, ZID,
  alias, and write namespace and orphans stored data — a teardown of identity,
  unacceptable for a real org and overkill for a scheme retrofit.
- **A dedicated "retrofit URI-SAN" path.** Rejected: rotation re-deriving the
  URI-SAN from the current config already *is* the retrofit; a special path
  would duplicate the mint with narrower applicability.
- **Build CRL/OCSP so rotation can invalidate the old leaf.** Rejected here:
  out of scope and contrary to ADR 0407/1031 (cut-off lives at mesh/identity,
  not PKI). Compromise cut is mesh-first + short cert lifetime; rotation is the
  re-key half.
- **Keep the same api_key across rotation.** Not possible (plaintext is
  unrecoverable) and not desirable (a rotation/compromise event should issue
  fresh credentials).
