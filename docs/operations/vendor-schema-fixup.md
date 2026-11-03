# Fixing a vendor whose topics all FAIL on schema-body

When stethoscope's vendor page shows N topics with N FAILs on the `schema-body` check — usually with healthy publish counts in the cardinality card — the vendor is publishing real bytes but their declared `format: protobuf:<Name>` doesn't match a top-level message in the `.fds` they uploaded. The portal viewer can't decode anything; downstream subscribers that lean on the registered schema see "not found" too.

This runbook is the corrective procedure. The write-time validator that refuses this state with a `422 format_mismatch` (portal `internal/api/handlers.go`, PR #599) is **live on nova-a** as of 2026-05-29 (deployed in the portal `sha-2166ff5` bump). So this state can no longer be *created* through the API — it can only persist for *legacy* registrations made before the gate went up. The runbook stays around because legacy state outlives gates. To confirm the deployed portal still carries the validator: check the top `sha:`/`pr:` row in `ops/ansible/roles/goat-zenoh-portal/deploy-history.yml` is at or past `sha-2166ff5`.

**Two layers stack here — read F-162 first.** The registration-shape mismatch this runbook fixes is read by the *portal decoded view* and *stethoscope* (both key off `topic.format`). Panoscope is a separate decode path: it previously ignored `format` and decoded against the first message in the `.fds`, so a `format` PATCH fixed the portal/stethoscope but not panoscope. The F-162 decoder fix (track `vendor-decode-rca`) is **live on nova-a** as of 2026-05-29 (panoscope `sha-aed2a50`), so panoscope now honors `format` too. If panoscope ever shows wire-type errors again after a green PATCH, confirm the deployed panoscope image is at or past `sha-aed2a50`.

## Prerequisites

- mesh access to `portal.nova.netbird.nova.example` (nova-a peer).
- `$PORTAL_OPERATOR_API_KEY` exported from your envrc.
- `protoc` available locally (only if rebuilding a `.fds` is the right fix; see below).

## Step 1 — Confirm the failure shape

Open stethoscope for the vendor:

```
https://stethoscope.nova.netbird.nova.example/?vendor=<prefix>
```

You're looking for:

- `topic-roster` card: "N topics: N FAIL · 0 WARN · 0 PASS" with red chips per topic.
- `schema-body` per-topic: each chip's hover detail reads "Format declares 'protobuf:<X>' but the uploaded .fds contains no top-level message named '<X>'. Found: <Y>, <Z>, …".
- `cardinality` card: non-zero publish counts in the last 24h.

If the topics are zero-traffic, this is not the right runbook — that's a publisher-down problem, not a registration-shape problem.

## Step 2 — Read the live state

```bash
curl -sS -H "Authorization: Bearer $PORTAL_OPERATOR_API_KEY" \
  "https://portal.nova.netbird.nova.example/api/vendors/<prefix>/topics" \
  | jq '.topics[] | {id, key_pattern, format}'
```

For each topic, also pull the latest schema's top-level messages. The portal's `/api/topics/decoded` and `/api/schemas/<key>` endpoints serve the bytes directly; the simplest path is to fetch the `.fds` and let `protoc --decode_raw` show you the message names:

```bash
curl -sS -H "Authorization: Bearer $PORTAL_OPERATOR_API_KEY" \
  "https://portal.nova.netbird.nova.example/api/schemas/<key_pattern>" \
  --output /tmp/topic.fds
protoc --decode_raw < /tmp/topic.fds | head -40
```

Or use the stethoscope drill-down — its `schema-body` failure detail names the top-level messages it actually found.

You now have, per topic, two strings:

- Declared `format` (e.g. `protobuf:MyMessage`).
- Top-level message names present in the `.fds` (e.g. `Status`, `SoftwareVersion`).

## Step 3 — Decide: PATCH format, or re-upload `.fds`?

Two questions to answer before you act:

1. **What does the publisher actually serialize on the wire?** Look at the publisher's source or the deployed binary — what `Message.SerializeToString()` is it calling? That answer is the *real* message name.
2. **Is the uploaded `.fds` the right descriptor for the on-wire bytes?**

Three branches:

- **Wire bytes are message `Status`, `.fds` contains `Status`, declared format wrong**: PATCH the topic's `format` to `protobuf:Status`. Fast.
- **Wire bytes are message `MyMessage`, `.fds` doesn't contain it**: the operator has the wrong descriptor. Have the vendor send the correct `.fds` (or generate one from their `.proto`) and re-upload it. Keep `format` as `protobuf:MyMessage`.
- **Wire bytes are message `MyMessage`, but `MyMessage` is a *nested* type inside `Outer`**: schema-body matches top-level only by convention. Have the vendor rebuild the `.fds` with the message hoisted to top-level (or change the publisher to serialize the outer envelope).

## Step 4a — Repair by PATCHing format

```bash
curl -sS -X PATCH \
  -H "Authorization: Bearer $PORTAL_OPERATOR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"format":"protobuf:Status"}' \
  "https://portal.nova.netbird.nova.example/api/vendors/<prefix>/topics/<topic_id>"
```

The write-time validator (PR #599) is live on nova-a (see the note at the top of this runbook), so the PATCH checks the new format against the existing `.fds` and refuses with `422 format_mismatch` if you got it wrong, with the suggested correction in the response body. Still verify the result in Step 5 — a green PATCH means the name matches the descriptor, not that the wire bytes decode (that's the decode-roundtrip check's job).

## Step 4b — Repair by re-uploading `.fds`

If the existing `.fds` is wrong, build the corrected one with `protoc`:

```bash
protoc --include_imports --descriptor_set_out=out.fds your.proto
```

Then POST it as the next version (don't reuse a version string — uniqueness is per `(topic_id, version)`):

```bash
curl -sS -X POST \
  -H "Authorization: Bearer $PORTAL_OPERATOR_API_KEY" \
  -F "version=v2" \
  -F "compatibility=backward" \
  -F "format=protobuf-fds" \
  -F "file=@out.fds" \
  "https://portal.nova.netbird.nova.example/api/vendors/<prefix>/topics/<topic_id>/schemas"
```

If your declared format references a name that's not in `out.fds`, the upload returns 422 with the suggestion baked into the error. Iterate from there.

## Step 5 — Verify

Hard-refresh the stethoscope vendor page. The topic-roster chip flips green; `schema-body` reads "vN uploaded (M bytes). Format 'protobuf:<Name>' matches a top-level message in the descriptor."

Sanity-check the decoded view too:

```
https://portal.nova.netbird.nova.example/schema.html?key=<key_pattern>
```

The viewer should render the message fields. If it still 404s, the `.fds` parse failed silently — re-run `protoc --decode_raw` on the bytes.

## Step 5b — Replay historical samples through panoscope

Panoscope honors `topic.format` as of the F-162 decoder fix (deployed `sha-aed2a50`, 2026-05-29; track `vendor-decode-rca`), so the replay below re-renders every topic against its declared message. (Before that fix, panoscope decoded against the first message in the `.fds` regardless of `format`, so only topics whose wire message happened to sort first re-rendered correctly — the rest stayed broken even after a green PATCH. If you ever see that asymmetry again, the deployed `panoscope_image` has regressed below `sha-aed2a50`.)

The substrate already retains the previous messages: panoscope's per-topic ring buffer holds the last ~50 raw-byte samples per topic, and the audit-subscriber's MinIO history covers the last 7 days. Both decoded against the *previous* (broken) schema before this fixup; we want them re-rendered against the *corrected* schema so panoscope visibly shows the fix on what already happened, not just on traffic that arrives next.

Two surfaces, in this order:

1. **Per-topic ring buffer (immediate).** Open panoscope at `https://panoscope.nova.netbird.nova.example/?key=<key_pattern>`. Hard-refresh the page (the schema descriptor is fetched into the frontend on stream open). In the stream pane, confirm the format pill flipped to `<p>` (protobuf). Toggle the `RAW / DECODED` switch to DECODED — the last several minutes of samples should render as decoded protobuf field/value rows, not raw hex. If they still show as hex with a "decode error" gutter, the portal-side schema cache hasn't refreshed yet; wait ~30s and reload.

2. **MinIO audit replay (full 7-day window).** Restart the panoscope container on nova-a so the boot-time `minio_replay` path re-hydrates the topology + flow state from the audit NDJSON history:

```bash
ssh goat@<nova-a-mesh-ip> 'docker compose -f /opt/panoscope/docker-compose.yml restart panoscope'
```

The boot log line `minio_replay: complete` reports `events_replayed=N` for the configured `WINDOW_HOURS` (168 on nova-a). Once it's back up, the topic's stream pane will continue from where it left off; the audit-replay path doesn't itself re-render decoded samples (it re-hydrates the peer/topic graph), but it ensures the format pill + edge labels on the topology view are sourced from the corrected schema, not the stale one.

If panoscope had already been showing red format pills (`?? ` / `hex`) for these topics due to the broken decode, restarting clears the stale classification on the next sample arrival.

## Step 6 — Tell the vendor

Send the vendor a short writeup. The point is to land the fact that we fixed it on our side this once AND surface the durable change (write-time validator) so future uploads can't recreate the state.

## The failure mode this runbook does NOT cover — a base64 layer

Everything above assumes the bytes on the key are the payload and the argument
is about which schema reads them. There is a fourth shape where the bytes are
**not the payload at all**: the topic carries the base64 TEXT of the real
payload, so no `(format, fds)` pair will ever decode it and every repair step
above is wasted.

Tell it apart in one command — this is what the symptom looks like:

```bash
# Decode ONE base64 layer off the sample. If what falls out is ITSELF base64
# (an XML doc that starts "PD94bWw...", a protobuf that is pure A-Za-z0-9+/),
# the publisher is double-encoding and the registry is innocent.
curl -s "http://<router>:8000/<key>" | python3 -c '
import base64,json,sys
v=json.load(sys.stdin)[0]["value"]
b=base64.b64decode(v); print(repr(b[:80]))'
```

Cause: a publisher that reads samples back through the zenoh REST plugin and
republishes `entry["value"]` without decoding. That projection returns
non-JSON payloads as a base64 string, so forwarding it verbatim ships base64
ASCII. Nothing in the path errors — the PUT returns 2xx and the key looks
healthy. See topic-decode-health.md §2.

**Repair is on the publisher, not here.** PATCHing format or re-uploading
`.fds` cannot fix it. Confirm the whole slot with:

```bash
ops/ci/check-topic-decode-health.py --portal <portal> --router <router> \
  --token "$PORTAL_OPERATOR_API_KEY"
```

which grades `DOUBLE-ENCODED` distinctly from `UNDECODABLE` for exactly this
reason. Note `vendor-decode-rca`'s H1–H5 do **not** include this hypothesis
(H3 is a 1–4 byte prefix, not a full base64 layer), and it is protobuf-only —
it will not diagnose an XML topic in this state.

## Why this happens — design background

The schema-body check on stethoscope is read-side validation (`crates/stethoscope-checks/src/checks/schema_body.rs`). The portal accepted any `(format, fds)` pair at write-time; the inconsistency only surfaced when stethoscope (or a decoded-view subscriber) tried to render. The write-time validator (merged PR #599) mirrors the read-side check at `POST /api/vendors/<v>/topics/<id>/schemas` and `PATCH /api/vendors/<v>/topics/<id>`, returning HTTP 422 with the suggested correction — once it is deployed to nova-a, future vendors hit the gate at upload and never land in this failure state.

Note the panoscope path is its own decode site — distinct from stethoscope's read-side check and the portal's write-side validator. F-162 documents the panoscope decoder bug (decoded the first message in the `.fds` instead of the one named by `format`), fixed and deployed. Stethoscope's `decode-roundtrip` check (track `vendor-decode-rca`) samples recent payloads via `/api/topics/decoded` and reports the decode success ratio (Pass ≥95% / Warn 50-95% / Fail <50%), so a "descriptor parses but bytes don't decode" state trips read-side instead of being spotted by eye in panoscope.
