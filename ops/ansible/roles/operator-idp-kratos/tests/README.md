# operator-idp-kratos tests

| Layer | What | Where |
|---|---|---|
| Parse-only | `ansible-playbook --syntax-check` against `tests/playbook.yml` | `tests/syntax-check.sh` |
| Behavioral | Real Kratos boot + admin /health/ready over loopback | Operator-fired 21H.1 bringup smoke (real Orin-C) |
| Integration | Real OIDC flow + WebAuthn enrollment + JWT issuance via Hydra | 21H.2 enrollment runbook + 21H.3 first consumer (Grafana) |
