# External ICAM federation — sovereign PKI cards, login.gov, Entra ID

**Status:** card sign-in generalised to **any sovereign PKI profile** ([ADR 1209](../adr/1209-card-sign-in-is-per-sovereign-pki-profile.md), amends [ADR 1069](../adr/1069-external-icam-cac-federation.md)): one registry `ops/identity/sovereign-pkis.yaml`, `us-dod` the first live profile, `est-id` and `nato-npki` sketched. The three ingress modes of ADR 1069 stand: `upstream_oidc` (config-built, pending RP registration), `mtls_proxy` (the `icam-mtls-proxy` role, registry-driven, default-off; deployed on the dev goatnet, profile `us-dod`, server certificate = the site's Let's Encrypt wildcard behind a Traefik SNI passthrough), `keycloak_frontend` (`icam-keycloak-cac`, us-dod only, default-off). A real CAC was driven through `mtls_proxy` and `keycloak_frontend` on 2026-07-02 / 2026-07-07 in the earlier CN-based shape. Runbook: [`../operations/dod-pki-cac-federation-setup.md`](../operations/dod-pki-cac-federation-setup.md).
**Related:** identity-architecture.md §9 · [identity-descriptor-standard.md](identity-descriptor-standard.md) ([ADR 1051](../adr/1051-identity-descriptor-standard.md)) · identity-grammar.md §1.2a (a person's nationality is a fact of its own, [ADR 1171](../adr/1171-person-nationality-and-org-ownership-are-two-facts.md)) · `ops/identity/grammar.yaml` ([ADR 1067](../adr/1067-identity-grammar-generated-sot.md)) · [federated-trust-model.md](federated-trust-model.md) (the workload analog).

## What it is

A person already authenticated by an authoritative external source becomes a first-class goat identity without goat pretending to be their identity source:

| Source | Protocol | What identifies the person | goat records it as |
|---|---|---|---|
| A **smart card from a sovereign PKI** (US DoD CAC first; any nation's card as a profile) | x509 client-certificate mTLS | `<pki>:<identifier>` — the profile id and the number the profile says the card carries (us-dod: the EDIPI) | `metadata_public.external_subjects[{pki, pki_subject, identifier_name, scheme}]` |
| **login.gov** | OIDC | pairwise `sub` | `external_subjects[{subject, scheme: login-gov}]` |
| **Entra ID (Gov)** | OIDC / SAML / SCIM | per-tenant `oid` | `external_subjects[{subject, scheme: entra}]` |

The rule that holds all three: **goat's native UUID anchor is the key; the external subject is a mapped attribute.** login.gov `sub` is pairwise per relying party, Entra `sub` is per app, an EDIPI is reissued over time. FICAM's own model is link-external, keep-local-governance.

A card says who issued it and which number it carries. It does not say who the person is in goat, what they may do, or what nationality they hold: role and affiliation are assigned locally (claim-map rule 2), and `identity.nationality` is the person's own declaration at intake, never derived from the PKI's `nation`.

## Card sign-in: the PKI registry

`ops/identity/sovereign-pkis.yaml` is the only place a PKI is described. One profile per PKI:

| Field | What it says | `us-dod` |
|---|---|---|
| `id`, `display`, `nation` | the profile's slug, name and the sovereign that runs the PKI (ISO 3166-1 alpha-3, or `NATO`) | `us-dod`, U.S. DoD (CAC/PIV), `USA` |
| `status` | `live` (every field verified, rendered into every door) or `sketch` (public documents only, rendered nowhere, `unverified` lists what is not confirmed) | `live` |
| `issuer_match` | regex over the RFC 2253 issuer DN; selects the profile | `,OU=PKI,OU=DoD,O=U\.S\. Government,C=US$` |
| `identity` | `san_upn` \| `san_email` \| `san_othername`+OID \| `cn_regex` \| `serial`, with an optional `identifier_regex` whose first group is the identifier | `san_upn`, `^(\d{10})(?:\d{6})?@mil$` — a live DoD ID certificate's UPN is `<EDIPI><6-digit persona code>@mil`; older cards carry `<EDIPI>@mil` |
| `subject_cn_format` | how the subject CN is built; the store binds a card by its exact CN | `<LAST>.<FIRST>.<MIDDLE>.<EDIPI>` |
| `trust_anchor` | source URL, verification, refresh cadence, `load: issuing` (the CAs that issue the card's authentication certificate plus their roots) or `all` | DISA "PKCS#7 for DoD PKI Only" v5.14, CMS signature verified; 14 of 49 loaded (DoD Root CA 3 + 6, DOD ID CA 62–81) |
| `revocation` | CRL URL(s) or pattern, OCSP, cadence, what is loaded | `http://crl.disa.mil/crl/DODIDCA_<NN>.crl` per issuing CA, `http://ocsp.disa.mil` |
| `example` | a synthetic card in the PKI's shape; the test PKI generator builds it and the gate applies the identity rule to it | CN `DOE.TEST.MI.1234567890`, UPN `1234567890121002@mil` |
| `sources`, `unverified` | where each fact was read and when; what is not confirmed | verified 2026-10-30 |

`ops/ci/check-sovereign-pkis.py` (workflow `sovereign-pki-registry`) refuses a malformed profile, a live profile with unverified fields, a regex that does not compile or has more than one capture group, an example the profile's own rule does not resolve, and a committed rendering that differs from the registry.

### Every door renders from it

```
ops/identity/sovereign-pkis.yaml
   ├─ goatnets   roles/icam-mtls-proxy  ── filter_plugins/sovereign_pki.py ── nginx map chain per profile
   │                                       + files/pki.js (njs: SAN UPN / e-mail / otherName)
   ├─ Kratos     claim-maps/sovereign-pki.jsonnet          (generated; CI refuses drift)
   ├─ collab     collab-services var.sovereign_pkis   (sovereign-pkis-render.py terraform)
   └─ store      OPENBAO_SOVEREIGN_PKI_DIR/<id>/ca-bundle.pem    (sovereign-pkis-render.py openbao-dir)
```

| Door | Reads | Identity it yields | Binding to a person |
|---|---|---|---|
| goatnet `icam-mtls-proxy` | the registry at deploy time; `icam_mtls_proxy_pkis` names the live profiles a goatnet accepts; trust anchors at `icam_mtls_proxy_pki_dir/<id>/ca-bundle.pem` | `X-Remote-User: <pki>:<identifier>`, `X-Remote-Pki: <pki>` → Dex `authproxy` → Kratos | Kratos links `(issuer, client_id, "<pki>:<identifier>")` to the identity it minted or an admin links it to an existing one |
| collab `cert-front` | `var.sovereign_pkis` (generated), ALB trust store (25-CA cap) | `<pki>:<identifier>` → Kanidm mail value `<pki>.<identifier>@cert-bind.…` | `onboard-collab.py bind-cert <person> <pki> <identifier>` |
| store (OpenBao) | one cert role per `<id>/ca-bundle.pem` | the card's subject CN, allow-listed per role | `people.py bind-card <name> --pki <id> --cn "<subject CN>"` |

`sovereign-pkis-render.py terraform` reproduces the collab value byte for byte (unit-tested), so the three doors cannot hold a PKI fact differently. The `mtls_proxy` claim map carries `pki`, `pki_subject` and `identifier_name`; for `us-dod` it also fills `edipi`, so readers of the old name keep working, and `scheme` stays `dod-piv` for us-dod (`sovereign-pki` for every other profile — the grammar value added for this).

### Adding a nation's PKI

1. Add a profile to the registry from the PKI's published certificate profile: issuer DN, where the identifier sits, the subject CN form, the CA repository and how its downloads are verified, the CRL/OCSP endpoints, a synthetic example. List under `unverified` everything not read from the publisher's document or a live card; `status: sketch` until that list is empty.
2. `ops/identity/sovereign-pkis-render.py check`; build a synthetic card with `ops/identity/cac-test-pki/gen-test-card.sh <id>`; run `ops/identity/mtls-proxy-proof/run.sh <id>`.
3. When `live`: regenerate the claim map (`render claim-map`), the collab value (`render terraform` → a collab-services PR with the CA bundle under `environments/aws/prod/pki/<id>/`), the store directory (`render openbao-dir`), and name the id in a goatnet's `icam_mtls_proxy_pkis`.

Sketched today, to the depth public documents support: `est-id` (Estonian ID-card, ESTEID2018: personal identity code from the CN, SK ID Solutions CA repository, CRL at c.sk.ee) and `nato-npki` (NCI Agency pages, not readable without a browser; every rule field unverified).

## The three ingress modes (ADR 1069, unchanged in shape)

```
  icam_federation.providers[].mode:
    upstream_oidc     → issuer_url = an external IdP that already did the card (EAMS-A / Azure Gov / login.gov)
    mtls_proxy        → issuer_url = the local icam-mtls-proxy (nginx mTLS, registry profiles → Dex)
    keycloak_frontend → issuer_url = the local icam-keycloak-cac (Keycloak x509 authenticator; us-dod)
                                    │
                                    ▼   (every mode → one Kratos oidc provider)
   Kratos oidc broker ─→ claim-map jsonnet (ops/identity/claim-maps/<claim_map>.jsonnet)
                                    │
                                    ▼
   native anchor + metadata_public.external_subjects[(issuer, client_id, subject, …)] + assurance snapshot
```

| Piece | Where | State |
|---|---|---|
| grammar dimensions `identity.source_scheme` (`native`, `dod-piv`, `login-gov`, `entra`, `sovereign-pki`) + `assurance.{ial,aal,fal}` | `ops/identity/grammar.yaml` | built; `generate.py --check` green |
| toggle contract `icam_federation` (3 modes) | `ory` role defaults + `group_vars/_examples/icam-federation.example.yml` | built, default-off |
| Kratos `oidc` broker block, mode-uniform | `ory/kratos.yaml.j2` | built; renders nothing when off |
| binding on `metadata_public` (Kratos coerces an array *trait* to `{}` at oidc-registration) + `last_assurance` | claim-map output | built, proven |
| claim maps: `_mock`, `login-gov`, `entra`, `eams-a` (upstream broker / Keycloak), **`sovereign-pki` (generated)** | `ops/identity/claim-maps/` | built; read `claims.raw_claims`; fail-closed group allow-map |
| `icam-mtls-proxy` role, registry-driven | `roles/icam-mtls-proxy/` + `filter_plugins/sovereign_pki.py` | built, default-off; templates + maps unit-tested, maps run in a real nginx with mTLS in CI |
| `icam-keycloak-cac` role | `roles/icam-keycloak-cac/` | built, default-off, us-dod shape |
| test PKI per profile + headless proof per profile | `ops/identity/cac-test-pki/gen-test-card.sh`, `ops/identity/mtls-proxy-proof/run.sh [<id>]` | built; the proof needs a Docker daemon |
| real-card full-chain kit | `ops/identity/cac-live-test/` | earlier CN-based shape; drove a physical CAC to a native identity on 2026-07-02 / 07-07; not re-run on the registry shape |

**Secrets:** per-provider OIDC client secrets ride Kratos env-override into `.env` (0600) as `ICAM_OIDC_<ID>_CLIENT_SECRET`; the proxy's Dex client secret is `ICAM_MTLS_KRATOS_CLIENT_SECRET`. Nothing is inlined into the 0644 `kratos.yaml`.

**Scope:** federation attaches to the **site IdP** (`ory` role); the **operator IdP** stays native (sovereign break-glass).

## Hard constraints

- **Assurance propagates or authz silently degrades.** A downstream RP inherits only what the assertion carries; re-check IAL/AAL/FAL per hop and gate on it fail-closed. The enforcement side is designed, not built.
- **FAL2** for any PII-bearing assertion.
- **Boundary tagging** per provider (`boundary_il`); refuse a low-boundary assertion for a high-boundary resource.
- **Revocation is the deployment's job.** Each profile names its CRL/OCSP; a door is not relied on until it loads them (the collab ALB accepts `crl_paths`; the goatnet role takes nginx `ssl_crl`).
- Mesh-only, no public plane.

## Per-provider notes

| Provider | Person id | Assurance | Notes |
|---|---|---|---|
| sovereign PKI card (`mtls_proxy`) | `<pki>:<identifier>` | AAL3-class hardware, IAL3 issuance | validate to the profile's trust anchor; extract at the proxy (break-and-inspect loses certificate attributes) |
| **login.gov** | pairwise `sub` | IAL via `acr`; `aal/2?hspd12=true` forces PIV/CAC | public / IL-limited; `private_key_jwt` client auth (runbook D.2) |
| **Entra ID (Gov)** | `oid` | conditional access | IL2/4/5; CBA binds cert → UPN directly (runbook D.1) |

## Remaining

1. Re-run `ops/identity/cac-live-test/` on the registry shape with a physical card.
2. SAML shim + SCIM adapter (Kratos is OIDC-only) — when a SAML-only source or Entra provisioning is required.
3. Descriptor schema generation of the assurance / external-subject fields from the SoT.

## Basis

NIST SP 800-63-3/-4 (IAL/AAL/FAL) · 800-63C (federation) · FICAM Architecture · DoDI 8520.03 · DISA DoD Cyber Exchange PKI/PKE (bundle v5.14, 2026-10-30) · SK ID Solutions ESTEID2018 certificate profile v1.3 and CA repository (2026-10-30) · NCI Agency identity-management pages (2026-10-30, search index only) · login.gov developer docs · Entra CBA on IDManagement.
