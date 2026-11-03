# identity-reconcile — live-vs-declared stray detector

A person can be live on the mesh + logged in yet have **no manifest / roster
entry**. The access COP *classifies* this — a live person with no declared record reconciles to
`out_of_band` (`../../../docs/design/data-access-cop.md`).

`reconcile.py` watches that verdict. It queries the portal's already-computed
`GET /api/access/roster` (operator-scoped), filters person nodes in state
`out_of_band`, and writes a Prometheus textfile metric so the obs stack alerts.
It adds **no reconciliation logic of its own** — the graph is the single source
of the verdict; this only surfaces + counts it.

## Output (into the node-exporter textfile dir)

`goat_identity_reconcile.prom`, written by atomic rename into the mounted
`../expected-vendors/textfile/` dir (a whole-dir mount — a single-file bind
would break on the inode swap):

- `goat_identity_strays{env,scope="all|unallowlisted"}` — stray counts.
- `goat_identity_strays_by_reason{env,recon_reason}` — unallowlisted, by reason.
- `goat_identity_reconcile_up{env}` — 1 if the roster fetch succeeded.
- `goat_identity_reconcile_last_run_timestamp_seconds` — staleness canary.

**No PII in the metric** (aggregate only). The identities themselves (WHO) are
printed to stdout → journald: `journalctl -t identity-reconcile` on the mgmt host.

## Alerts

[`../prometheus/rules/identity-reconcile.yml`](../prometheus/rules/identity-reconcile.yml):
`IdentityStrayUntracked` (unallowlisted strays > 0), `IdentityReconcileStale`
(cron dead > 90m — a silence is "we've gone blind," not "all clear"),
`IdentityReconcileDown` (portal fetch failing). Test:
`promtool test rules ../prometheus/tests/identity-reconcile.test.yml`.

## Allowlist

`identity-reconcile-allowlist.txt` — one email or `*@domain` glob per line —
exempts identities that legitimately have no person-manifest: **test/drill
accounts and machine/service bridges only**. A genuine person with no manifest
must NOT be allowlisted; the fix is to backfill their manifest,
and the alert firing is that working as intended.

## Run / deploy

Installed by `ops/ansible/obs.yml` (4c) as a `*/30` root cron on the mgmt host,
reading `PORTAL_URL` + `PORTAL_OPERATOR_API_KEY` + `GOAT_ENV` from the mode-0600
`/etc/goat-identity-reconcile.env` (rendered from the controller env). The block
is inert unless `identity_reconcile_portal_url` is set for the env.

Manual run:

```bash
PORTAL_URL=https://portal.<env>.netbird.<zone> \
PORTAL_OPERATOR_API_KEY=… GOAT_ENV=<env> \
python3 reconcile.py ../expected-vendors/textfile
```

Exit: 0 clean · 2 unallowlisted strays found · 1 config/fetch error.

## Acting on a fired alert

`journalctl -t identity-reconcile` names each stray. For each: **backfill** their
manifest (`ops/operators/<env>/<handle>.yml`)
and regenerate the roster (`ops/operators/gen-roster.py`), **or** revoke them
(`goat admin onboard <handle> --env <env> --revoke`). If it's a legitimate
test/service account, add it to the allowlist.
