# 12. Errors and troubleshooting

Every error goatbuilder prints says what happened, the likely cause, what to do
and where to read more. Errors from the tools it runs (Ansible, SSH, 1Password,
AWS, git, GitHub, cosign, Docker, NetBird) are carried through and interpreted
the same way.

| Field | Value |
|---|---|
| Catalog | `ops/errors/catalog.yaml`, compiled into both `goat` and `goatbuilder`, so `explain` works offline |
| Codes | `GB-NNN` goatbuilder's own · `X-<TOOL>-NNN` a tool it runs · `E_*` the `goat` CLI's, including one per portal answer (not found exit 7, rate-limited 8, server error 9) |
| Decision | [ADR 1199](../../adr/1199-one-error-catalog-interpreted-by-both-clis.md) |
| Design | [error-interpretation.md](../../design/error-interpretation.md) |
| Full table | [Appendix A](appendix-a-error-codes.md) |

## Reading a refusal

At a terminal, the interpretation prints under the error. The tool's own words
come first and goatbuilder's last, so the first fix printed is the one closest
to the cause.



```text
goatbuilder: deploy FAILED — log <path>/goat-deploy-<component>-<group>-<stamp>.log
  [matched: <group> | UNREACHABLE! => {"msg": "Failed to connect to the host via ssh: … Permission denied (publickey)."}]
  X-SSH-001 — the host refused the key
    what: the connection reached the host and SSH rejected every key offered.
    why:  your public key is not on the host's operator user
          the key is not loaded in the agent
    fix:  ssh -v <user>@<host> true 2>&1 | grep -i 'offering\|denied'
  X-ANSIBLE-001 — a host was unreachable
    …
  GB-029 — the play failed on a group
    …
```

| Setting | Effect |
|---|---|
| a terminal on stderr | the interpretation prints under the error |
| captured output (a log, CI) | one line: `(GB-029 — goatbuilder explain GB-029)` |
| `GOAT_EXPLAIN=1` | always print the interpretation |
| `GOAT_EXPLAIN=0` | never print it |
| `-o json` or `-o yaml` | the full interpretation on stdout as `goatbuilder.error/v1` |

## `explain`

```bash
goatbuilder explain GB-007                      # one code; also gb-7, x-ssh-1
goatbuilder explain "Permission denied (publickey)"
pbpaste | goatbuilder explain -                 # any pasted text
goatbuilder explain --list
goatbuilder explain --report -                  # save an unknown error to ~/.goat/error-reports/
```

Three matchers run over the whole text, in order:

| Matcher | Finds |
|---|---|
| code | any `GB-NNN`, `X-<TOOL>-NNN` or `E_*` code in the text |
| pattern | entries whose `matches:` regular expression hits anywhere in the text |
| lexical | when nothing else matched: the nearest entries by weighted word overlap, at most three. Deterministic |

When nothing matches, `explain` says so and prints the `--report` command.

## Troubleshooting by symptom

| Symptom | Likely code | First thing to run |
|---|---|---|
| refused before anything ran: "not a current main checkout" | GB-009 | `git checkout main && git pull --ff-only origin main` |
| a variable is NOT SET | GB-007, GB-044 | `goatbuilder secrets missing <NAME>`, then `secrets hint <NAME>` |
| `op run` refuses the whole command | GB-031, X-OP-002 | `env \| grep op://` to find the incomplete reference |
| 1Password signed out | X-OP-001 | sign in, then re-run |
| the door did not open | GB-006, X-AWS-003, X-AWS-004 | `ops/scripts/env-door.sh <goatnet> --print` |
| unreachable host | X-ANSIBLE-001, X-SSH-002 | check you are on the mesh or the door is open; `goatbuilder doctor --env <goatnet>` |
| `sudo` wants a password | X-ANSIBLE-002 | the host's operator user lacks passwordless sudo |
| deploy exits 3 | GB-017 | `goatbuilder verify <component> --env <goatnet>` to see which host is pending or unknown |
| play failed on a group | GB-029 | open the log path in the row; pipe it to `goatbuilder explain -` |
| rollback exits 3 or 4 | GB-023 | read the artifact type; follow the printed restore or revert steps |
| the live rows cannot run here | GB-042, X-SMOKE-001 | the candidate is untested, not failed; install the browser toolchain the row names |
| pins out of date in CI | GB-034 | `goatbuilder pins`, commit the regenerated files |
| a mutation refused with no terminal | GB-008 | add `--yes` |
| a standup input missing | GB-060, GB-061 | `goatbuilder doctor --site <site>` lists all of them |
| refused: checkout behind the trunk | GB-064 | the fast-forward or rebase command the row prints |
| refused: trunk could not be fetched | GB-065 | restore the route to the remote, or `--offline` on an islanded site |
| assay exits 3 | GB-048 | read each FAIL row's meaning; an UNKNOWN row needs its probe fixed, often a goat context (`--var goat_via=…`) |
| a control-node script lacks PyYAML | X-PY-001 | deploys run those scripts under Ansible's own Python; run one directly with that Python, or install PyYAML |
| no tool could read an image digest | X-DIGEST-001 | install crane, skopeo or docker |

## Adding an error

1. Add an entry to `ops/errors/catalog.yaml` with `code`, `tool`, `title`,
   `what`, `causes`, `fixes`, `read`, `exit` (goatbuilder codes) and, for a
   tool's words, `matches`.
2. Cite it from the code with `goat_errors::coded("GB-NNN", …)`.
3. Run `python3 ops/ci/check-error-catalog.py --write` to regenerate the
   reference and the English message catalog. The `error-catalog.yml` workflow
   refuses an entry missing a field, a `read` path that does not exist, a code
   cited in code but absent from the catalog, or a stale reference.
