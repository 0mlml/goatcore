# operator-idp-postgres tests

## Coverage

| Layer | What | Where |
|---|---|---|
| Parse-only | `ansible-playbook --syntax-check` against `tests/playbook.yml` | `tests/syntax-check.sh` |
| Behavioral | Real container start + pg_isready + dump round-trip | Operator-fired 21H.1 bringup smoke (see `docs/project/implementation-plan.md` Block 21H.1 `Validation` row) |
| Integration | Kratos + Hydra connect as the `operator_idp` app user against this DB | 21H.1b / 21H.1c smoke fixtures (`operator-idp-kratos` + `operator-idp-hydra` roles) |

## Why parse-only here

The role does host-modifying things (creates compose project, runs `docker compose up`, installs cron, runs containers) that are not safe to exercise locally without a real Orin-C. Behavioral coverage rides on the operator-fired bringup against the real host; the syntax-check catches the cheap class of error (typos, undefined vars in templates, malformed YAML) at every Ansible-touching commit per Block 50H.
