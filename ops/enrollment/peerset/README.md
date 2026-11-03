# peerset — wg-cp0 peer-set accessor library

The accessor library that owns reads and writes of
[`ops/enrollment/wg-cp0-bundle-allowlist.json`](../wg-cp0-bundle-allowlist.json),
the declarative source-of-truth for the wg-cp0 peer set.

**Scope (Block 22R.1 / [ADR 0322](../../../docs/adr/0322-wg-cp0-declarative-substrate.md)):**
this package is Property 1 of the declarative substrate redesign — single
declarative source-of-truth + schema v2 + auto-migration from the prior
v1 flat array. The full operator surface (reconcile / onboard / rotate /
diff / dry-run-with-diff) lands in 22R.2 on top of this foundation.

## Public surface

```go
ps, err := peerset.Load(path)        // parses v1 or v2, validates, auto-migrates
err := ps.Save(path)                 // atomic v2 write, deterministic field order, mode 0644

err := ps.Upsert(peer)               // insert or replace by name; rejects collisions
err := ps.RotatePubkey(name, pub, reason)
err := ps.Remove(name, reason)       // soft-delete to deprecated_peers[]
err := ps.Resurrect(name)            // inverse of Remove

peer, ok := ps.Lookup(name)
peer, ok := ps.LookupByPubkey(pubkey)
dep,  ok := ps.LookupDeprecated(name)
relay, ok := ps.Relay(name)

err := ps.Validate()                 // schema-validate; called by Load
```

Sentinel errors (use `errors.Is`): `ErrPeerNotFound`, `ErrDuplicateName`,
`ErrDuplicatePubkey`, `ErrDuplicateAddress`, `ErrSchemaInvalid`,
`ErrMigrationFailed`.

## Schema v2 envelope

```jsonc
{
  "schema_version": 2,
  "peers": [
    {
      "name": "vega-a",
      "wg_cp0_address": "203.0.113.39",
      "current_pubkey": "FbFR7IV8...",
      "pubkey_history": [
        {"pubkey": "x2f4nSzj...", "rotated_at": "2026-05-12T11:50:00Z", "reason": "F-114 closure"}
      ],
      "role": "mgmt-host",
      "site": "vega",
      "bundle_expiry": "2026-08-10T00:00:00Z",
      "allowed_ips_extra": ["203.0.113.38/24"],
      "metadata": {"created_at": "...", "last_updated_at": "...", "last_operation": "upsert"}
    }
  ],
  "relays":          [{"name": "...", "site": "...", "tier": "...", "endpoint": "...", "current_pubkey": "...", "mesh_address": "..."}],
  "deprecated_peers": [{"name": "...", "wg_cp0_address": "...", "current_pubkey": "...", "removed_at": "...", "removed_reason": "..."}],
  "active_forwarder": {"tier": "relay_servers", "host": "relay-rla"},
  "metadata": {"last_modified_at": "...", "last_modified_via": "...", "schema_min_compatible_version": 2}
}
```

## Migration from v1

Today's `wg-cp0-bundle-allowlist.json` carried a flat array of
`{name, pubkey, address}` rows written by `bundle-create
--update-allowlist`. `Load` detects v1 by a leading `[`, parses it,
and returns a v2 envelope with each v1 row mapped to a `peers[]`
entry. The migration is one-way: v2 envelopes are the only thing
`Save` ever writes. Operators with a v1 local working copy get
auto-migration on first `Load` (the file on disk is not rewritten
until `Save` is invoked, which makes the migration safe to re-run).

## Schema invariants

`Validate` enforces:

- `schema_version == 2`.
- Every active peer has non-empty `name`, `wg_cp0_address`, `current_pubkey`.
- No two active peers share a `name`, `current_pubkey`, or `wg_cp0_address`.
- Every relay has a non-empty `name`; relays don't share names.
- Every `deprecated_peers[]` row has a `removed_at`.
- `active_forwarder`, when present, has both `tier` and `host`.
- `pubkey_history[]` entries are well-formed (non-empty pubkey + non-zero
  `rotated_at`).

## Failure modes the API closes

- **Silent-fleet-amputation.** `Remove` is the *only*
  path that can take a peer out of the active set, and it is explicit by
  signature — there is no "rewrite the array, dropping anything not in
  scope" affordance. The 22R.2 orchestrator builds dry-run-with-diff on
  top of this guarantee.
- **Half-state rotation.** `RotatePubkey` is transactional
  at the data layer: it appends to history *and* sets `current_pubkey`
  in one call, with collision validation against every other peer.
- **Gratuitous rotation.** `Upsert` does NOT rotate the pubkey when the
  caller passes a new value; rotation is opt-in via `RotatePubkey` (with
  a required reason). Endpoint adds / TTL extensions are pure metadata.

## Testing

`go test ./peerset/...` → all green, ≥90% line coverage, `goleak.VerifyTestMain`,
sentinel errors all reachable via `errors.Is`. Round-trip on the v1 testdata
fixture is byte-stable across two `Load → Save` cycles. CI runs the same
suite via `.github/workflows/enrollment.yml`.
