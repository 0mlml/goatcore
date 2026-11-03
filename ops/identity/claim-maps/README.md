# Claim-maps — the declarative per-IdP mapping layer

The **explicit, per-IdP mapping** from an external assertion's claims to goat's
native identity traits. FICAM names attribute mapping as mandatory; NIST 800-63C
requires the RP to decide what to trust. One `<provider>.jsonnet` per federated IdP
(referenced by `claim_map:` in the `icam_federation.providers` config).

Each file is a **Kratos OIDC data mapper** (jsonnet). Kratos runs it after a
successful upstream login and takes the returned `identity` — both `traits` and
`metadata_public` — as the minted/updated native identity. Design:
[`../../../docs/design/external-icam-federation.md`](../../../docs/design/external-icam-federation.md).

Two Kratos behaviors this layer is built around (both proven in the
`ops/identity/federation-proof/` harness — see its README):

- **Read claims from `std.extVar('claims').raw_claims`.** Kratos surfaces only a few
  standard claims at the top of `claims`; the full id_token (incl. non-standard claims
  like `groups`/`roles`/`edipi`/`acr`) is under `claims.raw_claims`. Reading the top
  level silently drops them.
- **The binding rides `metadata_public`, NOT a trait.** Kratos coerces a nested
  array *trait* to `{}` during oidc-registration (mint fails). `identity.metadata_public`
  passes through untouched, so the mapper sets the scalar identity in `traits` and the
  `external_subjects` binding in `metadata_public` — one mapper, no webhook.

## The load-bearing rules (every map must honor)

1. **Native UUID anchor is the key.** The external `sub`/EDIPI is written into
   `metadata_public.external_subjects[]` as a mapped attribute, keyed by
   `(issuer, client_id, subject)` — **never** used as the identity id. login.gov
   `sub` is pairwise-per-RP, Entra `sub` is per-app, EDIPI is reissued/non-unique.
2. **Never silently trust external groups as goat capability.** External
   `groups`/`roles` claims map to goat role-family / affiliation only through an
   **explicit allow-map inside this file** — an unlisted external group grants
   nothing. `traits.groups` is only ever set from that explicit map, defaulting to
   `[]`.
3. **Assurance is per-event, fail-closed.** Record `last_assurance` (ial/aal/fal/
   acr/boundary_il) in the binding as an informational snapshot; the authoritative
   effective-dated series lives in the descriptor (ADR 1051). Downstream authz gates
   on the live session assurance, refusing a low-assurance assertion for a
   high-assurance resource.
4. **scheme is fixed per file** (native / dod-piv / login-gov / entra) and must match
   `ops/identity/grammar.yaml` `identity.source_scheme`. The one generated file,
   `sovereign-pki.jsonnet`, fixes it per PKI profile from the registry instead.
5. **A card never says who a person is beyond its identifier.** No claim map sets
   `identity.nationality` or any affiliation from the issuing PKI (ADR 1171, ADR 1209). `boundary_il` is fixed per
   file to the IdP's authorization boundary / impact level.

## Files

| File | scheme | authority | Notes |
|---|---|---|---|
| `_mock.jsonnet` | native | native | test/mock OIDC upstream for the proof harness (`ops/identity/federation-proof/`). |
| `login-gov.jsonnet` | login-gov | industry | pairwise `sub`; IAL2 via `acr`; public/IL-limited (boundary_il 2). |
| `entra.jsonnet` | entra | industry | per-tenant `oid`; conditional-access; IL5 (boundary_il 5). |
| `eams-a.jsonnet` | dod-piv | doctrinal | EDIPI from an **upstream_oidc** broker (EAMS-A) or the **keycloak_frontend** path. |
| `sovereign-pki.jsonnet` | per profile (dod-piv for us-dod, sovereign-pki otherwise) | doctrinal | **GENERATED** from `ops/identity/sovereign-pkis.yaml` by `../sovereign-pkis-render.py claim-map` (ADR 1209); CI refuses drift. The **mtls_proxy** path: `preferred_username` arrives as `<pki>:<identifier>`; the binding carries `pki`, `pki_subject`, `identifier_name`; us-dod also gets `edipi` so readers of the old name keep working. An unknown profile prefix mints nothing. |

## Deployment

When `icam_federation.enabled`, the `ory` role syncs the referenced maps into
`<config>/claim-maps/` and the kratos service mounts them read-only at
`/etc/config/claim-maps/`. Default-off ⇒ the directory is empty and nothing renders.

## Future: generate from YAML

These jsonnet files are the declarative artifact today (explicit, per-file,
reviewable). A later increment can generate them from a `claim-maps/<idp>.map.yml`
via `ops/identity/generate.py`, folding the allow-maps into the grammar-SoT ethos.
