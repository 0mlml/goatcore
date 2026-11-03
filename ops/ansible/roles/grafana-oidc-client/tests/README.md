# grafana-oidc-client tests

| Layer | What | Where |
|---|---|---|
| Parse-only | `ansible-playbook --syntax-check` | `tests/syntax-check.sh` |
| Behavioral | Real Hydra POST/PUT roundtrip | Operator-fired 21H.3a bringup |
| Integration | End-to-end OIDC: laptop → Grafana → Hydra → Kratos → token + Grafana role mapping | 21H.3a smoke after orchestrator runs |
