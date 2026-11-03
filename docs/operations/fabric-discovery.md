# Finding what is published on a goatnet

You have a credential and you are on the mesh. This page answers the next
question: **who else is publishing, what do they publish, and what shape are the
messages?**

Nothing here needs an operator. Every surface below accepts any authenticated
principal, including a slot key — topic visibility is advisory, not access
([ADR 1028](../adr/1028-topic-interface-visibility.md)). If a discovery surface
refuses you, that is a defect, not a policy.

---

## The short version

```bash
goat topic ls                         # every topic on the goatnet
goat topic ls <prefix>                # narrow to one org, or one namespace
goat topic show <key>                 # format, schema reference, description
goat schema show <key>                # the message and field tree
goat sub <key>                        # the live data
```

In the portal: **Catalog → Browse everything**.

---

## Two surfaces, same registry

| | `goat` CLI | Portal |
|---|---|---|
| Browse everything | `goat topic ls` | Catalog → **Browse everything** |
| One org or namespace | `goat topic ls <prefix>` | the same page: type a name, alias, prefix fragment or topic word in the search field, Enter opens it (`/` focuses search) |
| One topic's record | `goat topic show <key>` | click the topic row |
| A schema | `goat schema show <key>` | Catalog → **Schema lookup** |
| Live values | `goat sub <key>` | Inspect → **Inspector** |
| Declare your own | `goat topic create` | Catalog → **Register a topic** |

Both read the portal registry, so they agree. Use the CLI when you want to
grep, pipe or script; use the portal when you want to browse and read schemas
rendered.

---

## Walking it end to end

### 1. See everything

```console
$ goat topic ls
key                                                 format                 description
<org-anchor>/adsb/aircraft/snapshot/v1               protobuf:AircraftBatch Full ADS-B snapshot per poll cycle
...
```

One row per declared topic across every org. The `format` column tells you how
to read the bytes before you subscribe to any of them.

### 2. Narrow down

The first path segment is the org prefix. Pass any prefix of a key to filter:

```bash
goat topic ls <org-anchor>        # one org
goat topic ls <org-anchor>/adsb   # one namespace inside it
```

### 3. Read the record

```console
$ goat topic show <org-anchor>/adsb/aircraft/snapshot/v1
key_pattern:  <org-anchor>/adsb/aircraft/snapshot/v1
org_prefix:   <org-anchor>
format:       protobuf:AircraftBatch
schema_ref:   attest/schema/<org-anchor>/adsb/aircraft/snapshot/v1/v1
description:  Full ADS-B snapshot per poll cycle
last_publish: unknown — panoscope unreachable
flow_state:   unknown
```

### 4. Read the schema

```console
$ goat schema show <org-anchor>/adsb/aircraft/snapshot/v1
format:          protobuf:AircraftBatch
primary_message: AircraftBatch
all_versions:    v1

message example.adsb.v1.Aircraft
    1  string  icao_hex
    2  string  callsign
    3  double  lat
    4  double  lon
    5  int32   alt_baro_ft
    ...
```

`goat schema ls <key>` lists the versions; `goat schema diff <key> v1 v2`
compares two.

### 5. Watch it

```bash
goat sub <org-anchor>/adsb/aircraft/snapshot/v1
```

`goat sub` streams until you stop it. If nothing arrives it says which kind of
nothing after five seconds — attached and waiting on a quiet topic, attached to
no router, or subscribed to a name that can never match.

---

## What the answers do and do not mean

**A topic in the list is a declaration, not a promise.** The registry records
what an org said it would publish. It does not prove anything is on the wire
right now. `goat flow` answers liveness; the catalog answers intent.

**An empty result for one org is an answer.** On a populated goatnet most orgs
have declared nothing. The catalog
omits them by default and reports the count it omitted, so an apparently short
catalog is not a broken one. On the portal page that count is the **Nothing
declared** chip; pressing it shows those orgs.

**"Last publish" is per org, and there is no per-topic "last seen".** The
registry's per-topic `last_seen_at` column is never written (F-537 stopped
writing the per-topic activity table; it stays in place, unread), so the portal
shows no such column. What it shows, once per org card, is the audit tailer's
per-org roll-up: the most recent publish under that prefix as the fabric audit
log recorded it (batched per minute, so up to a minute behind) and the publish
count. "No publish recorded
yet" means exactly that. The same two facts ride on each org row of
`GET /api/catalog` (`last_publish_at`, `publish_count`), with the slot's display
alias (`alias`); the alias is display-only and never composes a key or an ACL
subject ([ADR 1039](../adr/1039-slot-alias-display-layer.md)).

**Nothing here tells you who is *subscribing*.** The registry records
publishers. There is no subscriber-side record, by design: the fabric is
brokerless, so no component sees the full subscription set
([zenoh-brokerless-consequences](../design/zenoh-brokerless-consequences.md)).

**Declared and observed are different columns.** A topic declared but never
published is an integration that never started. Bytes published under a key
nobody declared will not decode on any surface, because there is no schema to
decode them with.

---

## If a discovery surface refuses you

| Symptom | What it means |
|---|---|
| `invalid token` | Your key is not accepted by this portal. Check you are pointed at the right goatnet: `goat context ls`. |
| Topic list is empty on a goatnet you know has data | You are on the wrong context, or the registry is genuinely empty. Cross-check in the portal. |
| `goat sub` attaches but stays silent | The topic is quiet, or the key does not match what the publisher emits. `goat topic show` prints the declared key; compare byte for byte. |
| `last_publish: unknown` | The freshness observer was unreachable. The catalog entry is still valid; only liveness is unknown. |

---

## Related

- [zenoh-trial-onboarding](zenoh-trial-onboarding.md) — getting a credential in the first place
- zenoh-data-fabric — key expressions and namespace rules
- [ADR 1028](../adr/1028-topic-interface-visibility.md) — why topic visibility is advisory rather than access
- [ADR 1123](../adr/1123-client-demand-must-not-amplify.md) — why the catalog is one request and not a fan-out
- goat-cli — the rest of the CLI
