# mtls-proxy-proof — prove the mtls_proxy path for one sovereign PKI profile

Fully local. A synthetic card from `../cac-test-pki/gen-test-card.sh <pki id>` →
**nginx** terminates mTLS against that profile's test CA, selects the profile by
issuer DN and reads the identifier the registry says the card carries (the maps from
`../sovereign-pkis-render.py nginx-maps`, SAN fields through the role's `pki.js`) →
`X-Remote-User = <pki>:<identifier>` to **Dex**'s `authproxy` connector → OIDC token
carrying it in `preferred_username`. That token is what Kratos consumes via
`../claim-maps/sovereign-pki.jsonnet`.

## Run

```
./run.sh              # us-dod: expects preferred_username us-dod:1234567890
./run.sh est-id       # the sketch profile: est-id:38001085718 (rendering only)
docker compose down -v
```

Needs docker, openssl, and a python3 with PyYAML (`PYTHON=...`).

## Proven

- the earlier shape (EDIPI from the subject CN, no registry): a CAC-shaped
  cert → `preferred_username=1234567890`.
- The registry-driven shape above is built; running it needs a Docker daemon, which
  the authoring session did not have. Its pieces were checked separately: the maps
  pass `nginx -t` (nginx 1.31.8) and `pki.js` returns the UPN and e-mail of a
  synthetic `us-dod` card under node.

**Findings baked in:** the card is required only on `/auth`+`/callback` (the RP
back-channel has no cert, so `ssl_verify_client optional` + per-location enforce);
Dex's `sub` is opaque, so the identity rides `preferred_username` (needs `profile`
scope on the authorize request); the nginx service needs the issuer hostname as a
network alias.

`ops/ansible/roles/icam-mtls-proxy/` deploys the same nginx/Dex shape from the same
registry; one flag (`icam_mtls_proxy_enabled`) enables it.
