# operator-idp-hydra tests

| Layer | What | Where |
|---|---|---|
| Parse-only | `ansible-playbook --syntax-check` against `tests/playbook.yml` | `tests/syntax-check.sh` |
| Behavioral | Real Hydra boot + admin /health/ready + public OIDC discovery | Operator-fired 21H.1 bringup smoke (real Orin-C) |
| Integration | Real OAuth2 authorization-code flow + PKCE + JWT issuance | 21H.3 Grafana wire-up (first OIDC consumer) |
