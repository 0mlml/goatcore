# federation-proof — prove toggle-switch PATH #1 end-to-end

Fully local, no external dependency. A mock OIDC provider stands in for an enterprise
CAC-validating IdP; Kratos federates to it using the **same**
oidc-broker block + `_mock` claim-map the real `ory` role deploys. This is the design's
**definition-of-done** (`docs/design/external-icam-federation.md`).

## Run

```
cd ops/identity/federation-proof
./run.sh          # needs docker + docker compose + curl + jq
docker compose down -v   # cleanup
```

## What it proves (both checks GREEN — live-run)

- **CHECK A:** the admin API mints an identity carrying
  `metadata_public.external_subjects` (binding + assurance) — accepted intact.
- **CHECK B:** a login through the mock OIDC upstream makes Kratos mint a **native**
  identity whose `traits.groups` is `["ADMINS"]` (mock group `goat-admins` → `ADMINS`
  via the fail-closed allow-map) AND whose `metadata_public.external_subjects` carries
  `(issuer, client_id, subject, scheme, last_assurance)` — all mapped by
  `../claim-maps/_mock.jsonnet`. The native anchor (Kratos UUID) is the key; the
  external `sub` is a mapped attribute.

The claim-map logic is also unit-proven by jsonnet eval (all four maps in
`../claim-maps/`); this harness proves the **Kratos wiring** around it.

## Findings that shaped the design (live run Kratos v1.3.1 + navikt/mock-oauth2-server 2.1.10)

Getting to green surfaced four real things, all now baked into the artifacts:

1. **Mock port + in-network hostnames.** The mock listens on **8080**; the whole OIDC
   redirect chain (kratos → authorize → callback) must speak one consistent hostname
   set, so `base_url: http://kratos:4433` and the flow is driven from a curl container
   on the compose network.
2. **Mock token-callback must match on `grant_type`.** Kratos sends `client_id` via HTTP
   Basic auth, so it is absent from the `/token` body — matching on `scope`/`client_id`
   silently issues a claimless token ("provider did not return a subject").
3. **Kratos nests non-standard claims under `claims.raw_claims`.** Only a few standard
   claims sit at the top of the mapper's `claims`; `groups`/`roles`/`edipi`/`acr` are
   under `raw_claims`. All claim-maps read from there.
4. **The binding must ride `metadata_public`, not a trait.** Kratos coerces a nested
   array *trait* to `{}` during oidc-registration (mint fails, "expected array, got
   object" — reproduced with a flat mapper and with `default: []` removed, so it is
   Kratos internals not our schema). But `identity.metadata_public` set by the *same*
   mapper passes through untouched. So the mapper sets scalar identity in `traits` and
   the `external_subjects` binding in `metadata_public` — one mapper, **no webhook**.
   This is also the correct home: the binding is admin/registry-managed descriptor state
   (ADR 1051), and Kratos already tracks the (provider, subject) linkage natively for
   login-matching, so `external_subjects` is purely goat's descriptor projection.

Re-run `./run.sh` any time; both checks are deterministic.

## Switching the harness to other modes/providers

Point `kratos.yaml`'s provider `issuer_url` at a different upstream and swap
`mapper_url` to `login-gov.jsonnet` / `entra.jsonnet` / `eams-a.jsonnet` to exercise
the other claim-maps against a matching mock token set (`mock-oidc` `JSON_CONFIG`
claims). Modes #2/#3 present as OIDC too, so the same harness validates them once the
`icam-mtls-proxy` / `icam-keycloak-cac` roles are built.
