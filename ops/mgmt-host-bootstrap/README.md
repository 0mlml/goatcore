# mgmt-host-bootstrap

Sovereign netbird account + setup-key creation. Runs in the mgmt-host
container. Replaces the broken DB-direct seed pattern that burned the
cgnat-validation env .

ADR: [`docs/adr/0410-go-cli-peer-enrollment.md`](../../docs/adr/0410-go-cli-peer-enrollment.md).
Design: `docs/design/identity-architecture.md` §4.2.
Block: `docs/project/implementation-plan.md` Block 21A.

## What it does

Creates properly-formed `accounts` + `groups` + `setup_keys` rows in
the netbird-mgmt Postgres so a real netbird peer can enroll via the
gRPC `setup-key` flow. Hash format + JSON column shapes match what
netbird-mgmt 0.68.3 expects (verified end-to-end against
local docker-compose).

**No IdP touched. No GORM in our code** (per ADR 0413).

## Subcommands

```
mgmt-host-bootstrap --help
mgmt-host-bootstrap create-account     --owner <email> --site-name <short>      [--idempotency-key <key>]
mgmt-host-bootstrap create-setup-key   --account <id>  --role <peer|admin|operator|bootstrap> --ttl <duration>  [--idempotency-key <key>]
mgmt-host-bootstrap rotate-setup-key   --id <sk_id>                              [--idempotency-key <key>]
mgmt-host-bootstrap revoke-peer        --id <peer_id>                            [--idempotency-key <key>]
mgmt-host-bootstrap list-peers         [--account <id>]
mgmt-host-bootstrap list-setup-keys    [--account <id>]
```

`--dsn` (or `$PGDSN`) tells the CLI how to reach Postgres. In the
mgmt-host container it's the same DSN the netbird-management container
uses.

### Idempotency on writes (Block 50B.8)

Every write command is idempotent. Re-running a command with the same
inputs is a no-op + exit 0 with stdout `already-applied: <key>`. This
protects Ansible (or any orchestrator) from partial-restart corruption:
a kill at any point during a multi-command bootstrap is safe to retry.

Two ways to specify the idempotency key:

1. **Auto-derived (default):** `sha256(command || NUL || inputs)`. Two
   invocations with identical CLI arguments hit the same key; a third
   with any argument changed gets a different key.
2. **Explicit:** `--idempotency-key <key>` overrides the auto-derived
   one. Useful when you want to deliberately re-issue a command with
   the same arguments (e.g. force a fresh `rotate-setup-key`) — pick
   any operator-controlled string.

Internally: keys land in a `bootstrap_idempotency` table created on
first use; serialization across concurrent runs is via Postgres
advisory transaction locks keyed on a 63-bit hash of the key.

The plaintext setup-key returned by `create-setup-key` and
`rotate-setup-key` is **never** stored in `bootstrap_idempotency` —
replays show only `setup_key_id`. If you actually need a fresh
plaintext, run `rotate-setup-key --idempotency-key <fresh-key>`.

## Operator usage (production)

The mgmt-host compose stack defines a profile-gated `mgmt-host-bootstrap`
service ([`../compose/mgmt-host/docker-compose.yml`](../compose/mgmt-host/docker-compose.yml)).
Each invocation is one shot via `docker compose --profile bootstrap run --rm`.

A wrapper at `/usr/local/bin/mgmt-host-bootstrap` (installed by the
[`mgmt-host-bootstrap-wrapper`](../ansible/roles/mgmt-host-bootstrap-wrapper/)
Ansible role) hides the compose plumbing.

**With wrapper installed (typical):**

```bash
mgmt-host-bootstrap create-account --owner person-82@example.org --site-name vega
# returns: account_id: acc_vega_<...>

mgmt-host-bootstrap create-setup-key \
  --account acc_vega_<...> --role peer --ttl 2160h
# returns: setup_key_id, plaintext (UPPERCASE UUID — show ONCE, hand to peer)

mgmt-host-bootstrap list-peers     --account acc_vega_<...>
mgmt-host-bootstrap list-setup-keys --account acc_vega_<...>
mgmt-host-bootstrap rotate-setup-key --id sk_<...>
mgmt-host-bootstrap revoke-peer      --id <peer_id>
```

**Without wrapper (raw compose):**

```bash
cd /opt/mgmt-host
docker compose --profile bootstrap run --rm mgmt-host-bootstrap \
  create-account --owner person-82@example.org --site-name vega
```

The PGDSN comes from `POSTGRES_SUPERUSER_PASSWORD` in
`/opt/mgmt-host/.env` (rendered by the mgmt-host Ansible role from the
operator's secrets at deploy time). No additional env vars to manage.

## Local development

```bash
cd ops/mgmt-host-bootstrap
go build .
PGDSN="host=127.0.0.1 user=postgres password=... dbname=netbird sslmode=disable client_encoding=UTF8" \
  ./mgmt-host-bootstrap create-account --owner test@local --site-name dev
```

## Critical invariants the CLI enforces

Each one was a separate debug round before the CLI
existed. Documenting here for assessor + future maintainer:

1. **`setup_keys.key` = base64(sha256(UPPER(uuid)))**, not plaintext.
   Source: `netbird/management/server/types/setupkey.go:155-187`.
2. **`setup_keys.key_secret` = first-5-chars(UPPER) + "****"** —
   masked-display only, NOT a validation field.
3. **Plaintext setup key is UPPERCASE UUID** — netbird's
   `GenerateSetupKey()` uppercases before hashing.
4. **`setup_keys.auto_groups` = `'[]'`, never NULL** — the mgmt API
   serves the stored value verbatim and a null crashes the dashboard
   `/groups` page (F-140; recurred on ZAGROS from keys this
   CLI created before the column was added to the INSERT).
5. **`accounts.network_net`** — JSON-encoded `net.IPNet` mixed
   format: `{"IP":"100.64.0.0","Mask":"//8AAA=="}` (Go's `net.IP`
   marshals to a quoted string; `IPMask` falls through to base64).
6. **`accounts.settings_network_range` = NULL** — it's
   `*netip.Prefix`, a nullable pointer.
7. **`accounts.settings_jwt_groups_claim_name = ''`** — a string
   field (not pointer); empty string is fine, NULL is not.
8. **All other `settings_*` and `network_*` columns** — populated
   to safe defaults. The CLI knows the full set.
9. **A default `'All'` group MUST exist** for every account.
10. **No empty `users` rows** — netbird creates user rows on first
   enrollment; pre-existing rows with NULL `service_user_name`,
   `auto_groups`, etc. break the gRPC scan with `cannot scan NULL
   into *string`.

When netbird-mgmt bumps to a version that adds a new NOT-NULL-expected
column, the CLI must learn it. CI tests pin against specific
netbird-mgmt versions and exercise the full create-account →
peer-enrollment chain.

## Coupling to netbird release

We pin to **netbird-mgmt 0.68.3** in CI. Any version bump goes through
a pin-regression check: bring up the new netbird-mgmt locally, run the
CLI, run a real `netbird up` against it. If it fails, learn the new
columns + update the CLI in the same PR that bumps the netbird pin.

This is the *acceptable maintenance cost* of bypassing netbird's GORM
+ HTTP-API path. Per ADR 0413, the trade is worth it because the
alternative (running an IdP for what is fundamentally a one-shot
DB-write operation) introduces failure modes that are worse than
"watch this column when netbird-mgmt bumps."

## Tests

```bash
# Unit tests (~2s, no docker needed)
go test -race -count=1 ./...

# Integration tests (~3-5 min, parallel-safe via testcontainers-go,
# spins up its own postgres-spock + signal + netbird-mgmt per test)
go test -race -count=1 -tags=integration -parallel=4 -v ./...
```

Integration tests live under the `integration` build tag (Block 50B.3).
Each test spins up its own isolated stack on a fresh docker network,
so multiple tests run concurrently without port conflicts. Containers
are torn down on test exit, panic, OR signal — testcontainers' reaper
handles the kill -9 case. The legacy `testdata/docker-compose.yml`
remains in tree as a hand-poke smoke fixture (operator: `docker
compose up -d`) but is no longer driven by `go test`.

### Seed the geolocation fixture first

netbird-mgmt builds its geolocation layer during startup, before it serves
HTTP. With an empty Datadir it fetches ~82 MB from `pkgs.netbird.io` inside the
readiness window — fine when that CDN's edge is warm (`TestEndToEnd_OTLPEmit`
in 19 s), and a test failure when it is cold (3 m 50 s for the first file
alone).

```bash
./testdata/warm-geolite.sh    # once per published DB version
```

That writes `testdata/geolite/`, which the tests mount instead of downloading;
netbird skips any database already present. CI keeps the same directory in an
`actions/cache` keyed on the version the CDN publishes.

Skipping this is supported — the tests fall back to downloading and log that
they did. Details and the failure evidence: `testdata/geolite/README.md`.
