# obs-traefik tests

| Layer | What | Where |
|---|---|---|
| Parse-only | `ansible-playbook --syntax-check` against `tests/playbook.yml` | `tests/syntax-check.sh` |
| Behavioral | Real Traefik boot + cert delivery + dynamic-config reload | Operator-fired 21H.3 bringup (real Orin-C, real cert ceremony) |
| Integration | TLS handshake from a wg-cp0-onboarded laptop + OIDC redirect to Hydra | 21H.3a Grafana wire-up smoke (first consumer) |
