# ADR 1209 — Card sign-in is per sovereign PKI profile; the US DoD card is the first profile, not the design

- **Status:** **Proposed.** Amends [ADR 1069](1069-external-icam-cac-federation.md) (its `mtls_proxy` mode). On merge nothing changes on a live goatnet: the `icam-mtls-proxy` role stays default-off and is deployed nowhere; the collab front and the store keep `us-dod` as their one live profile.
- **Date:** 2026-10-30
- **Deciders:** operator
- **Evidence:** a live DoD ID certificate's SAN UPN is `<EDIPI><6-digit persona code>@mil` (16 digits), not `<EDIPI>@mil`; every reader that took "the part before `@`" as the EDIPI bound the wrong value. The operator's words: "this process of using goat with a pki item needs to be generalized for other nations, not specialized only for us cacs" and "generalize it so it would work with any sovereign pki not just us".
- **Related:** [ADR 1069](1069-external-icam-cac-federation.md) · [ADR 1171](1171-person-nationality-and-org-ownership-are-two-facts.md) (a person's nationality is a fact of its own) · [ADR 1051](1051-identity-descriptor-standard.md) (the binding rides the descriptor) · [`external-icam-federation.md`](../design/external-icam-federation.md) · `identity-grammar.md` §1.2a · registry `ops/identity/sovereign-pkis.yaml`

## Plain-language summary

A smart card from any nation's government PKI can sign a person in to goat, the collab environment and the credential store. Each PKI is one *profile*: how to recognise its cards, where on the card the person's number is, what the card's name field looks like, where its CA certificates come from and how they are checked, and where it publishes revocations. The US Department of Defense card (CAC) is the first profile. The profiles live in one file; every door reads that file or a rendering of it. A card identifies a person only by `<profile>:<number>`; who that person is, what they may do and what nationality they hold are decided in goat, by an admin, never read off the card.

## Context

Card sign-in was built three times around the US CAC: the goatnet proxy read the EDIPI from the subject CN with a DoD-shaped regex; the collab front's `san_upn` rule took everything before `@`; the store bound a card by its subject CN with a `us-dod` cert role. The three held the DoD facts separately and two of them held the EDIPI wrong. There was no place to add a second nation's PKI without copying the CAC path and renaming it.

ADR 1069 chose the architecture that still stands: a card is validated at an mTLS edge, the edge hands Kratos an upstream-OIDC assertion, a claim map records the external subject on a native anchor. What it fixed in place was the subject: EDIPI, from the DoD PKI.

## Decision

1. **One registry describes every PKI: `ops/identity/sovereign-pkis.yaml`.** A profile has `id`, `display`, `nation` (ISO 3166-1 alpha-3, or `NATO`), `status` (`live` or `sketch`), `issuer_match` (regex over the RFC 2253 issuer DN), `identity` (`san_upn` | `san_email` | `san_othername` + OID | `cn_regex` | `serial`, each with an optional `identifier_regex` whose first capture group is the identifier), `subject_cn_format`, `trust_anchor`, `revocation`, `identifier_name`, an `example` card, `sources` and `unverified`. A `live` profile carries no unverified field and its own identity rule must resolve its example card; `ops/ci/check-sovereign-pkis.py` refuses anything else. A `sketch` profile renders nowhere.
2. **Every door renders from the registry.** The goatnet role `icam-mtls-proxy` reads it at deploy time and emits one nginx `map` chain per enabled profile (`ops/ansible/filter_plugins/sovereign_pki.py`); `ops/identity/sovereign-pkis-render.py` emits the collab front's `var.sovereign_pkis` value, the store's `OPENBAO_SOVEREIGN_PKI_DIR/<id>/ca-bundle.pem` layout and the Kratos claim map `claim-maps/sovereign-pki.jsonnet`. There is no per-PKI variable, regex or branch anywhere else.
3. **The identity a card yields is `<pki>:<identifier>`.** The proxy sends `X-Remote-User = <pki>:<identifier>` and `X-Remote-Pki = <pki>`; the collab front resolves `<pki>.<identifier>@cert-bind.…`; the store's cert role is the profile id. Two profiles can never collide on a number. The Kratos binding records `pki`, `pki_subject` and `identifier_name`; the `us-dod` binding also carries `edipi` so readers of the old name keep working.
4. **Nationality is never inferred from the PKI.** `nation` says who issued the card. `identity.nationality` is the person's own fact, declared at intake (ADR 1171). No claim map, filter or front writes one from the other, and the registry has no field for it.
5. **Binding a card to an existing person is explicit and admin-done**, on every plane: `people.py bind-card` (store), `onboard-collab.py bind-cert` (collab), an admin linking the external subject in goat. A card that resolves to no bound person is refused, or mints a groupless identity where first sign-in is allowed; it never grants.
6. **Trust stores with a cap load issuing CAs only.** A profile's `trust_anchor.load: issuing` names the CAs that issue the card's authentication certificate plus the roots they chain to (`us-dod`: 14 of DISA's 49). Revocation sources are declared per profile; loading them is the deployment's job.
7. **"CAC" survives only as the name of the `us-dod` card.** Variables, headers, connector ids and documents say card, profile and identifier.

## Alternatives considered

- **Keep the CAC path and add a second role per nation.** Rejected: three copies already disagreed about one PKI; a fourth copy per nation multiplies that.
- **Per-door PKI lists (Terraform variable, group_vars, store directory) edited by hand.** Rejected: the EDIPI defect was exactly one door holding a fact the others held differently.
- **Derive the person's nationality from the issuing PKI.** Rejected: a Canadian officer at a US command holds a CAC (ADR 1171); the card says who issued it, not who the person is.
- **Load every CA the publisher ships.** Rejected: an ALB trust store holds 25; DISA ships 49, of which 14 issue or root the card's authentication certificate.
- **One generic claim map that trusts the prefix.** Rejected in favour of a generated map with the live profile table baked in: an unknown prefix mints nothing.

## Consequences

- Adding a nation's PKI is one registry entry plus its CA bundle, then a render and a deploy per door; `est-id` and `nato-npki` are sketched to the depth public documents support, with every unread fact listed under `unverified`.
- The collab front gains `identifier_regex`; its `us-dod` entry becomes `^(\d{10})(?:\d{6})?@mil$`, which reads the EDIPI from both UPN shapes. Bindings made with the 10-digit EDIPI start working.
- The goatnet role's test PKI generator, headless proof and templates run per profile; the real-card full-chain kit (`ops/identity/cac-live-test/`) still carries the earlier shape and has not been re-run.
- The grammar gains `identity.source_scheme: sovereign-pki`; `dod-piv` stays the scheme of `us-dod`.
- Owed when a goatnet enables the role: a refreshed CRL for each enabled profile (`revocation` names the source), and a mesh-CA server certificate for the proxy.

## Rollback

Registry-only additions and a default-off role: delete the registry entry, re-render, redeploy the door. The collab front's `identifier_regex` is optional; removing it restores the "before `@`" reading. The grammar value is additive (deprecate-never-delete).
