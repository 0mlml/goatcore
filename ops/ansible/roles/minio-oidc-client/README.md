# minio-oidc-client

Provisions an OAuth2 client for MinIO at operator-IdP Hydra per [ADR 0842](../../../../docs/adr/0842-dedicated-operator-idp-for-observability.md).

## Claim → policy mapping (the fiddly part)

MinIO uses the JWT `groups` claim as a list of MinIO IAM policy names. Each value in the claim that matches an existing MinIO IAM policy gets attached to the user's session. Group strings without a matching policy are silently ignored.

The companion change to the [`minio`](../minio/) role's `mc-init.sh` (in this batch) creates **alias policies** named after operator-IdP groups:

| Operator group → MinIO policy alias | Effective access |
|---|---|
| `obs-admin` → `consoleAdmin` | full admin |
| `minio-admin` → `consoleAdmin` | full admin |
| `obs-readonly` → `readonly` | read-only |
| `minio-readonly` → `readonly` | read-only |

An operator with `["obs-admin", "grafana-admin"]` → MinIO matches `obs-admin` only → consoleAdmin policy attaches. The `grafana-admin` value is silently ignored at MinIO; no error.

An operator with only `["grafana-admin"]` → no MinIO match → login completes but session has zero policies → all bucket actions denied. This is the desired UX: per-service authorization is consumer-side; cross-service operators don't accidentally get access.

## Required env var

```
export <SITE>_C_OPERATOR_IDP_MINIO_CLIENT_SECRET=$(openssl rand -hex 32)
```

## MinIO env vars consumed

In the [`minio`](../minio/) role's compose template, conditional on `operator_idp_enabled`:

| Var | Purpose |
|---|---|
| `MINIO_IDENTITY_OPENID_CONFIG_URL` | Hydra well-known config URL |
| `MINIO_IDENTITY_OPENID_CLIENT_ID` | `minio` |
| `MINIO_IDENTITY_OPENID_CLIENT_SECRET` | shared with this role |
| `MINIO_IDENTITY_OPENID_CLAIM_NAME` | `groups` (default upstream is `policy`; override) |
| `MINIO_IDENTITY_OPENID_DISPLAY_NAME` | "Operator IdP" |
| `MINIO_IDENTITY_OPENID_SCOPES` | `openid,profile,email,groups` |
| `MINIO_IDENTITY_OPENID_REDIRECT_URI` | `https://minio.<site>.netbird-prod.goat.example/oauth_callback` |

## Tests

`tests/syntax-check.sh` — Ansible parse-only smoke + register-uniqueness lint.
