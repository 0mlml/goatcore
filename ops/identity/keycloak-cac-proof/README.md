# keycloak-cac-proof — prove toggle-switch PATH #3 (keycloak_frontend)

Fully local. A test CAC client cert (`../cac-test-pki/`) authenticates to Keycloak's
native **X.509 authenticator**; Keycloak validates it against the test DoD CA, extracts
the **EDIPI** from the cert Subject DN, and issues an OIDC token carrying it. That token
is exactly what Kratos's oidc broker consumes (path-#1-proven) via
`../claim-maps/eams-a.jsonnet`.

## Run

```
./run.sh              # needs docker + curl + jq
docker compose down -v
```

## Proven

A CAC cert → Keycloak x509 direct-grant → OIDC token with `edipi=1234567890` +
`preferred_username`. Reproducible from a clean realm import.

**Key config finding:** Keycloak's x509 mapping-source must be **"Match SubjectDN using
regular expression"** (with a regex capturing the EDIPI) — the "Subject's Common Name"
source ignores the regex and uses the whole CN as the identity, so it never matches the
`1234567890` user. Baked into `realm-goat.json`.

The `ops/ansible/roles/icam-keycloak-cac/` role templatizes this realm/config; a single
flag (`icam_keycloak_cac_enabled`) deploys it.
