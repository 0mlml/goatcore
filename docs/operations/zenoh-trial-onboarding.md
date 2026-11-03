# Zenoh trial fabric — community onboarding packet

> **Template.** Concrete connection details (Zenoh router LAN IP, cert tarball delivery, portal URL, sunset date) are filled in at deploy time by the operator-fired bringup session. See [`../design/zenoh-trial-fabric.md`](../design/zenoh-trial-fabric.md) for the design rationale and what the trial is for.

Welcome. This is a short trial of a shared pub/sub bus you can use to move data. The trial is time-bounded and intentionally relaxed about access control — the point is for you to use it and tell us what hurts.

## What this is in one paragraph

A single Zenoh router sitting on the trial LAN. You plug your laptop or sensor / effector into the same network, install a Zenoh client (CLI or SDK in your language of choice), point it at the router, and you can `put` / `sub` / `get` against topics on the fabric. **Each vendor (you or your team) gets a top-level prefix; you own everything under it.** Reads are wide-open — anyone on the trial fabric can subscribe to any topic, including yours. Writes are vendor-scoped — only you can publish under your prefix. Everything you publish is captured by an audit-logger for the trial period; this is not a private channel. If you want replay (catch up on missed history), the substrate supports it.

## Access model — who can do what

Both the Zenoh router (port 7447) and the portal (HTTPS) are mesh-internal only; you must be enrolled in the netbird mesh to reach either at the TCP layer. What you can *do* once you're on the mesh depends on what credential you carry:

| If you have… | You can… |
|---|---|
| A mesh tunnel and nothing else | Browse the portal catalog (`/vendor.html`, `/schema.html`, the inspector UI) + view schemas + see who's publishing what. No publish, no subscribe. |
| A vendor cert tarball (from the OOB bundle) | Publish under your own prefix, subscribe to anything on the fabric, register topics under your prefix via the portal API, upload your own schemas. |
| The portal vendor API key (issued with your bundle) | Same as above + mutate your topics' metadata via the portal API (PATCH format / schema_ref / description; PUT per-language code examples). |
| The operator API key | Mint new vendors (POST), revoke vendors (DELETE — symmetric to mint, tears down the wire layer + per-vendor RocksDB dir + SIGHUPs the router), fix vendor display names (PATCH). |
| No mesh tunnel | Nothing — both surfaces are mesh-internal only. |

The credentials are independent: the **Zenoh router** authenticates you by your X.509 cert (mTLS handshake — the cert's CN gates which prefix you can publish under). The **portal** authenticates you by a Bearer token in the `Authorization` header (vendor-scoped or operator-scoped). Your portal API key has no effect on the Zenoh side; your Zenoh cert has no effect on the portal side. Both are issued together at vendor mint time.

## Connection details

| Field | Value |
|---|---|
| Zenoh router address | `<router-endpoint>` (mTLS — present your vendor cert at handshake) |
| Zenoh REST endpoint | `http://<router-host>:8000` (read-only; for ad-hoc `GET <key-expr>` queries) |
| Vendor cert tarball | `<filled-in-at-deploy>` — distributed OOB at vendor registration. Contains your per-vendor X.509 cert + key + the nova CA root cert. |
| Your vendor prefix | `<filled-in-at-deploy>` — assigned by operator at vendor registration; your cert URI-SAN binds your peer identity to this prefix; you own everything under `<vendor>/**` |
| Vendor self-service portal | `https://<portal-host>.<trial-zone>/` — substrate landed; turns on at first nova-a deploy. Pre-portal flow described below. |
| Time sync | Not served by the fabric. Keep your device on its normal OS time sync (NTP/NTS); the router and portal do not answer NTP. |
| Trial sunset | `<filled-in-at-deploy>` |
| Feedback channel | `<filled-in-at-deploy>` |

## Three things to do once you have your cert tarball

### 1. Install your cert + trust the fabric CA root

Unpack the tarball. It contains:

- `<vendor>-cert.pem` — your per-vendor X.509 leaf cert (1-year validity)
- `<vendor>-key.pem` — your private key (keep this file's mode 0400)
- `nova-ca-root.pem` — the offline-CA root cert your Zenoh client trusts when validating the router

Two paths for the trust anchor, depending on whether you want the trust scoped to just this trial or installed system-wide:

```bash
# Path A — per-process (recommended for trial scope): pass --root-ca-cert directly to your Zenoh client (see next section).

# Path B — system-wide on Linux (optional; the bundled scripts use Path A and need no trust-store install): add the fabric root to your OS trust store.
sudo cp nova-ca-root.pem /usr/local/share/ca-certificates/fabric-ca-root.crt
sudo update-ca-certificates
```

The fabric does not serve time. TLS checks your cert's validity window against your own clock, so a device whose clock is badly off (typically an RTC-less board after a cold boot) sees a TLS failure at connect. Fix it with your OS's normal time sync (for example `sudo chronyc makestep` or `timedatectl set-ntp true` on Linux) against your usual NTP/NTS source, not against the router.

### 2. Install a Zenoh client

The shortest path is Python:

```bash
python3 -m venv venv
. venv/bin/activate
pip install eclipse-zenoh protobuf
```

On **Windows**, activate with `venv\Scriptsctivate` (not `. venv/bin/activate`) and run the scripts with `python` instead of `python3` — `python3` bypasses the venv on Windows and hits system Python, causing `ModuleNotFoundError`. On **macOS/Linux** the commands above work as written.

This is what `first-publisher.py` (also in this tarball) needs. It's the fastest "you're publishing in under a minute" path on every platform.

Other supported clients (pick what fits your workflow):

- **Rust SDK** — `cargo add zenoh` (library; you write the binary)
- **TypeScript / JavaScript** — `npm install @eclipse-zenoh/zenoh-ts`
- **Go** — `go get github.com/eclipse-zenoh/zenoh-go`
- **C++** — see the [zenoh-cpp repo](https://github.com/eclipse-zenoh/zenoh-cpp)
- **CLI release binaries** — download the prebuilt `zenoh`, `z_put`, `z_sub`, `z_get` binaries from the [Zenoh releases page](https://github.com/eclipse-zenoh/zenoh/releases) (no canonical `cargo install zenoh-cli` crate exists — the example binaries ship as release artifacts, not via cargo install)

### 3. Publish + subscribe round-trip

The shortest path is the Python script that's already in this tarball (`first-publisher.py`). It has the right mTLS config baked in:

```bash
python3 first-publisher.py
# PUT i=00 key=<vendor>/hello/v1 payload=hello from <vendor> at <ts>
# PUT i=01 key=<vendor>/hello/v1 payload=hello from <vendor> at <ts>
# …
```

If you'd rather use the prebuilt CLI binaries (download per #2 above), publish-subscribe round-trip looks like this. The router speaks **mTLS over TCP** — pass your cert + key + the fabric root via the Zenoh `--*-cert` flags:

```bash
# In one shell — subscribe (you can subscribe to any topic on the fabric)
z_sub -e <router-endpoint> \
      --root-ca-cert  nova-ca-root.pem \
      --connect-certificate  <vendor>-cert.pem \
      --connect-private-key  <vendor>-key.pem \
      -k "<vendor>/**"

# In another shell — publish (only to your own vendor prefix)
z_put -e <router-endpoint> \
      --root-ca-cert  nova-ca-root.pem \
      --connect-certificate  <vendor>-cert.pem \
      --connect-private-key  <vendor>-key.pem \
      -k "<vendor>/<system>/hello/v1" -v "first message"
```

If you see the publish land in the subscriber within a second, you're connected. To see everything flowing on the fabric, subscribe to **`*/**`** rather than `**` — same traffic, and it is the spelling the router grants to everyone (see "Wide-open reads" below for why). Exact Zenoh client flag names depend on your client version; the upstream client docs are the authoritative reference, the shapes above are illustrative.

**mTLS gotchas worth knowing up front.** Your Zenoh client must (a) set `enable_mtls: true` on the TLS transport block — sending the client cert is not automatic, (b) connect by hostname (`<router-endpoint>` per the OOB bundle) so standard TLS hostname verification matches the router cert's DNS SAN — `verify_name_on_connect: true` (closes F-149 née F-DOG-10; the bundle's `first-publisher.py` is pre-wired this way), (c) insert the TLS config as a single JSON5 block via the SDK rather than per-key sub-inserts (per-key inserts don't enable the client-cert send path on at least eclipse-zenoh-python 1.x).

## Namespace convention

**Shape:** `<vendor>/<system>/<your-discretion>/v<N>` (deeper paths fine; version tag optional but encouraged)

Where:

- **`<vendor>`** — your assigned vendor prefix. Operator hands this out at registration. You own everything under it; no one else can publish into your subtree.
- **`<system>`** — your choice. System codename, function name, integration name. Invent it without asking.
- **`<your-discretion>`** — arbitrary depth. Sub-class, instance, region, mission, whatever shape the data wants. Examples below.
- **`v<N>`** — optional trailing version tag (`v1`, `v2`, …). Strongly encouraged. Subscribers can dial in to specific versions or subscribe to `<topic>/v*` to accept any.

**Examples:**

```text
example-vendor/tracker/entity/track/v1
org-2/power/site-3/v2
goat/snitch/probe-report/v1
community/user-1-test/foo/bar/v1
```

**Reserved prefixes (you cannot publish under these):**

| Pattern | Who | Purpose |
|---|---|---|
| `<vendor>/**` (your prefix) | you | Your subtree. Publish anything beneath; subscribers can read freely. |
| `<other-vendor>/**` | not you | You can READ from any other vendor's prefix; you cannot publish under it. |
| `attest/**` | operator | Reserved top-level prefix for fabric-native attestation streams. Schema bodies for your topics go through the portal API instead (see "Register your topics + upload protobuf schemas"). |
| `system/announcements` | operator | Trial status, planned maintenance, sunset warning |
| `system/audit-stream` | operator | Where the audit-logger publishes its own emission stream |

## Wide-open reads, vendor-scoped writes

**Anyone with a fabric-CA-rooted cert can read any topic.** No per-topic subscribe ACL. This is intentional — the trial is community-of-trust at the read layer. If your data is too sensitive for that posture, this trial isn't the right place for it.

Which raises the obvious question: **read what?** `goat topic ls` lists every topic every org on the goatnet has declared, and from a topic you can reach its schema and then its live data. The portal shows the same under **Catalog → Browse everything**. Full walkthrough, including what an empty answer means: [fabric-discovery.md](fabric-discovery.md).

**One shape is reserved: the literal `**`.** `z_sub -k "**"` asks the router to push the entire fabric into one subscription, and one slow subscriber doing that can stall delivery for everybody. It is now granted per certificate, to a named list. Everything else is unchanged:

| you want | subscribe to | grant needed |
|---|---|---|
| everything on the fabric | `*/**` | none |
| one vendor, across tenants | `**/<vendor>/**` | must be in the goatnet's declared shape list |
| one topic, across tenants | `**/tracks/v1` | same |
| one vendor's subtree | `<vendor>/**` | none |
| literally `**` | `**` | a named grant — ask, with a reason |

The middle two carry one gotcha worth knowing before it costs you an afternoon: a `**`-leading filter has to be **declared** by the platform (`zenoh_peer_cross_tenant_read`), because Zenoh cannot express "any `**`-leading filter" as a single access-control rule. If you use a cross-tenant filter nobody has declared yet, your subscriber connects, declares without error, and then **receives nothing** — indistinguishable from a quiet topic. If a brand-new cross-tenant filter comes up empty, send us the exact key expression rather than debugging your client; adding it is one line of platform config and a router restart.

**Only the per-vendor cert holder can write to that vendor's prefix.** The Zenoh router enforces this **at the wire layer** by reading the URI-SAN from your verified client cert at session establishment (`<uri-san-prefix><vendor>`) and matching it against the per-vendor write rule. If you try `z_put -k "example-vendor/foo"` and your cert URI-SAN is `<uri-san-prefix>community`, the publish is dropped before it leaves your client. You can confirm your own writes via the inspector (see below) or by subscribing to your own prefix from a second shell.

## Verify your own publishes — self-validation

Before you wire your real producer/consumer pair, prove the round-trip works with the two scripts in the tarball. Open two shells, both with the venv activated:

```bash
# shell A — subscribe to your whole subtree
python3 first-subscriber.py
# connecting to <router-endpoint> ...
# subscribed: <vendor>/**
# waiting for samples (Ctrl-C to exit)

# shell B — publish five "hello" messages on <vendor>/hello/v1
python3 first-publisher.py
# PUT  i=00  key=<vendor>/hello/v1  payload=hello from <vendor> at ...
```

Within a second of running shell B, shell A should print one `HH:MM:SS  <vendor>/hello/v1  text(...): '...'` line per publish. If you see them, your cert authenticates correctly, the router accepts your writes, AND your subscription receives them — full round-trip.

`first-subscriber.py` accepts a key expression as its first argument so you can narrow:

```bash
python3 first-subscriber.py "<vendor>/hello/v1"     # one specific topic
python3 first-subscriber.py "<vendor>/sensors/**"   # one subtree
python3 first-subscriber.py "**"                    # everything on the fabric (reads are wide-open)
```

If shell A stays silent after shell B prints, walk down the Connectivity Probe at `/peering/probe.html` on the portal — DNS → TCP → TLS → cert chain → URI-SAN matches your declared prefix.

## Register your topics + upload protobuf schemas

The pieces above get you publishing bytes. To get the portal's schema viewer + sample-decode + per-language code examples lighting up for your topics, two more steps: **register the topic** (so the portal knows it exists) and **upload a `.fds` descriptor** (so the portal can decode your wire bytes against your message definitions).

This is encouraged, not enforced — the substrate runs in audit mode and you can publish whatever bytes you want with no schema. But schema-rich topics are dramatically easier for consumers to integrate against, and the portal's surfaces (schema viewer at `/schema.html?key=<topic>`, inspector decode at `/inspector.html`, autogen code examples for Rust/Go/Python/TypeScript) only render when both pieces are in place.

> ⚠️ **`key_pattern` is a LITERAL string, not a template.** Every angle-bracket placeholder you see in the examples below (`<vendor>`, `<system>`, `<arbitrary>`, `<MsgName>`, …) is a fill-in-the-blank convention for the docs. The portal stores `key_pattern` byte-for-byte and matches it against your incoming publishes by exact-string equality — there is no wildcard, no template substitution, no `{{ vars }}`. **Substitute every `<...>` to a real value here AND in your publisher code; the two strings must be identical.** If your publisher emits `org-1/radar1/RADAR-EXAMPLE/position_attitude` and you register `org-1/radar1/<system>/position_attitude`, those are two different strings — `/schema.html` will render "topic not found" even while your bytes are flowing live on the wire.
>
> The same rule applies to the URL path: `POST /api/vendors/<vendor>/topics` means your real vendor prefix in the path. If your prefix is `org-1`, the URL is `/api/vendors/org-1/topics`.

The portal's API key for this is in your bundle (delivered out-of-band; one per vendor). Set it in your shell:

```bash
export PORTAL_TOKEN=<your-portal-api-key>          # ← your real API key here
export PORTAL=<portal-url>
export VENDOR=<your-vendor-prefix>                  # ← your real prefix, e.g. org-1
```

### Step 1 — register the topic

For each topic you publish, POST a row to the portal. Pick `format: "protobuf:<MsgName>"` where `<MsgName>` is the fully-qualified protobuf message type your topic carries.

**Worked example.** Suppose you're vendor `org-1`, your publisher does this:

```python
pub = session.declare_publisher("org-1/radar1/RADAR-EXAMPLE/position_attitude")
# (RADAR-EXAMPLE is your concrete system tag — it can be whatever you want;
#  what matters is the EXACT SAME STRING appears in the registration below.)
```

Then your registration call carries the same key:

```bash
curl -sS -X POST "$PORTAL/api/vendors/$VENDOR/topics" \
  -H "Authorization: Bearer $PORTAL_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "key_pattern": "org-1/radar1/RADAR-EXAMPLE/position_attitude",
    "format": "protobuf:org1.radar.v1.PositionAttitude",
    "description": "one-line summary of what this topic carries"
  }'
# {"id": 42, "key_pattern": "org-1/radar1/RADAR-EXAMPLE/position_attitude", ...}
```

The pieces you should substitute when adapting for your topic — marked here so you can see them at a glance:

| Field | Substitute with | Notes |
|---|---|---|
| `$VENDOR` in URL path | your vendor prefix (e.g. `org-1`) | from your OOB bundle |
| `key_pattern` value | the EXACT key your publisher uses | identical string on both sides |
| `format` value | `protobuf:<your-fully-qualified-MsgName>` | matches the message type in your `.proto` |
| `description` value | one-line human-readable summary | shown in the catalog + schema viewer |

The response carries the new topic's `id` — you need it for step 2.

After the POST, browse to `/topics.html` and check that the row you just created has `key_pattern` equal to what your publisher actually emits. If it doesn't match, edit the row (or DELETE + re-POST) — don't try to make the publisher emit a templated string.

Other valid `format` values: `json`, `cbor`, `msgpack`, `text`, `raw`. Only `protobuf:<MsgName>` unlocks the schema viewer + decode surfaces; the others render samples as raw bytes / hex.

### Step 2 — upload the schema body

The portal accepts **two shapes**. Most vendors should use (a):

**(a) Easy path — upload `.proto` source directly.** No `protoc` install needed; the portal compiles your source in-process. Single file with `google/protobuf/*` imports only:

```bash
curl -sS -X POST "$PORTAL/api/vendors/<vendor>/topics/$TOPIC_ID/schemas" \
  -H "Authorization: Bearer $PORTAL_TOKEN" \
  -F "file=@your_message.proto" \
  -F "format=protobuf-source" \
  -F "version=v1" \
  -F "compatibility=none"
```

Syntax errors come back as HTTP 400 with a `your_message.proto:<line>:<col>: <msg>` diagnostic — same shape as `protoc` locally.

**(b) Power path — upload a pre-compiled `FileDescriptorSet` (`.fds`).** Required when your `.proto` imports other `.proto` files of your own (multi-file source), which the v1.1 server-side compile doesn't yet support:

```bash
# Compile your .proto(s) into a FileDescriptorSet binary.
# --include_imports walks transitive imports; --include_source_info preserves
# field-level comments so the schema viewer renders them.
protoc \
  --descriptor_set_out=topic.fds \
  --include_imports \
  --include_source_info \
  -I . your_message.proto

# Upload it to the topic row from step 1 (TOPIC_ID is the id you got back).
curl -sS -X POST "$PORTAL/api/vendors/<vendor>/topics/$TOPIC_ID/schemas" \
  -H "Authorization: Bearer $PORTAL_TOKEN" \
  -F "file=@topic.fds" \
  -F "format=protobuf-fds" \
  -F "version=v1" \
  -F "compatibility=none"
```

Once both pieces land, browse to `<portal-url>/schema.html?key=<your-real-topic-key>` (e.g. `?key=org-1/radar1/RADAR-EXAMPLE/position_attitude`) — you should see your messages, fields, enums, reconstructed `.proto` source, and per-language autogen code examples. The portal inspector at `/inspector.html` will decode live samples against the descriptor instead of showing raw bytes.

> ⚠️ **Your `format: protobuf:<Name>` MUST match a top-level message in the `.fds` you upload.** This is the second can't-skip rule alongside the literal-string `key_pattern` rule above. If you register `format: protobuf:PositionAttitude` and the `.fds` you upload contains only `Status` + `SoftwareVersion` (no `PositionAttitude`), the portal viewer will say "topic not found" or render nothing, and stethoscope will surface a red `schema-body` failure for that topic. (panoscope decodes against the message your `format` names, as of the F-162 fix; older panoscope builds decoded the first message in the `.fds` regardless, which read as wire-type errors.) **The portal refuses this upload at write-time with HTTP 422 `format_mismatch`**, naming the messages it *did* find so you can either re-aim `format` (PATCH the topic) or rebuild the `.fds` with the declared message as a top-level (not nested) type. Nested messages don't count — hoist to top-level if that's where your publisher serializes.

**If `/schema.html` shows "topic not found"**, step 1 didn't land OR the registered key doesn't match what your publisher actually emits. The most common cause is the literal-string-vs-template mistake called out at the top of this section — open `/topics.html` and verify the `key_pattern` column equals the key URL you're querying byte-for-byte. If they differ (e.g. you registered `org-1/radar1/<system>/position_attitude` but the publisher emits `org-1/radar1/RADAR-EXAMPLE/position_attitude`), edit the row to match the publisher's actual key — don't try to make the publisher emit a templated string.

**If `/schema.html` shows the topic but says no schema is uploaded**, step 2 didn't land — check the upload curl returned 200.

**If the upload returned HTTP 422 with code `format_mismatch`**, your declared `format: protobuf:<Name>` doesn't match a top-level message in the `.fds`. The error body names the messages the portal found and suggests the most likely correction — apply it via `PATCH /api/vendors/<vendor>/topics/<id>` (to change `format`) or rebuild the `.fds` so `<Name>` is a top-level message. See [`vendor-schema-fixup.md`](vendor-schema-fixup.md) for the operator-side runbook on the same failure mode.

### Re-uploading + versioning

Schemas are versioned. Upload `version=v2` to ship a new descriptor without losing v1; the viewer's version picker lets consumers walk back. Compatibility is informational today (`none` / `backward` / `forward` / `full`) — the portal records what you declare but does not enforce it on the trial fabric.

### Validate against your live wire

After upload, the portal inspector's "decoded samples" path calls `GET /api/topics/decoded?key=<topic>` which fetches recent samples via the Zenoh proxy and decodes them against your uploaded descriptor. If decode fails per-sample, the inspector surfaces the per-sample error — usually a sign that what you're publishing on the wire doesn't match the `.proto` you uploaded (wire bytes are JSON when format says protobuf; descriptor name doesn't match message; field-number mismatch). Inspect → publish a fresh sample → re-inspect; iterate until decode succeeds for every sample.

## Message formats

Pick a format at topic registration time so the inspector can render your messages instead of showing raw bytes. Supported in the trial:

| Format | When to pick |
|---|---|
| `json` | Default. Human-readable, structured, easy to debug. |
| `cbor` | Compact binary. Same data model as JSON; smaller wire. |
| `protobuf:<msg-name>` | High-performance binary; upload your compiled `.fds` to the portal (see "Register your topics + upload protobuf schemas" above) so the inspector can decode. |
| `msgpack` | Compact binary; popular in some ecosystems. |
| `text` | Plaintext (logs, free-form events); UTF-8. |
| `raw` | Opaque bytes; you don't want the inspector to interpret. |

Unset format = inspector treats as `raw`. Set the format via the portal `POST /api/vendors/<vendor>/topics` payload (see walkthrough above) or `PATCH /api/vendors/<vendor>/topics/{id}` to fix it after the fact.

## Vendor self-service portal + inspector

**Portal** (substrate at [`ops/goat-zenoh-portal/`](../../ops/goat-zenoh-portal/); deploys alongside the goat-zenoh Ansible role's Zenoh router): a web UI at the portal URL above lets you:

- Register a new topic (key pattern, format, schema ref, description) — UI form + API per the walkthrough above
- Upload a `.fds` descriptor per topic — schema viewer at `/schema.html?key=<topic>` lights up + inspector decodes live samples
- Edit format/schema/description for an existing topic
- Inspect the last value or last N values on any topic (yours or anyone else's)
- Live-stream new publishes matching a key expression in the browser

You'll get a portal API key at vendor registration; bring it as `Authorization: Bearer <key>` if you script against the API. The portal authenticates you to *write* metadata only; the Zenoh router doesn't care about the portal key — your publish ACL is your per-vendor X.509 cert URI-SAN + the vendor's write rule.

## What the operator sees (full disclosure)

The audit-logger plugin subscribes to `**` and captures every publish during the trial. For each event we record:

- The publisher's cert URI-SAN (which maps to your vendor identity)
- The topic key expression
- The payload size (not content, by default)
- A timestamp

This audit trail informs the learnings write-up at trial close. **If you want content captured for replay**, ask — we can enable storage on your prefix. If you DON'T want content captured at all, this trial probably isn't the right place for that data; the audit-logger captures metadata always, and all other vendors can read your content unfettered.

## Things to push us on

The whole point is the trial is too quiet without feedback. Specifically useful pushback:

- The `<vendor>/<system>/.../v<N>` shape doesn't fit the data I want to publish
- I want to use a `<system>` name that another vendor is already using
- The version tail (`v<N>`) feels unnecessary / feels under-specified
- The portal API/UI is missing an obvious operation
- The inspector renders my format badly or not at all
- A schema-declaration ergonomic problem
- A latency / partition / ack pattern that surprises you
- A replay use case the substrate makes harder than it should
- An onboarding step that took longer than you expected
- The wide-open-read posture is wrong for a data class I have in mind
- An ACL rule that bit you (you got allowed something you shouldn't have, or denied something you should have)

Any of those go in the feedback channel.

## Sunset

The trial fabric tears down on the date in the connection details table. Seven days before sunset, the operator publishes to `system/announcements`. If you have data you want to preserve past sunset, request a dump in the feedback channel — the operator can extract from RocksDB storage + the portal's topic registry before teardown.

After sunset there is no "shutdown grace period" — the fabric is destroyed in place. The audit log, schema-enforcer counter snapshots, and portal registry SQLite dump are archived for the trial-findings write-up; data payloads are not (unless you specifically requested a dump pre-sunset).

## What this is NOT

- Not a production service. Single point of failure; if the router dies, restart costs minutes.
- Not federated with the production goat-net Zenoh fabric. Data does not cross.
- Not read-isolated per vendor. Anyone with a fabric-CA-rooted cert can read every topic, including yours. Writes are vendor-scoped; reads are not.
- Not a long-term home for your data. Plan for the sunset.

## Cross-references

- [`../design/zenoh-trial-fabric.md`](../design/zenoh-trial-fabric.md) — the trial's design + the learning questions + portal architecture
- `../design/zenoh-data-fabric.md` — Zenoh as the project's universal interaction substrate
- `../design/nova-goatnet.md` — the nova goatnet the trial is co-deployed onto
- `../design/nova-x509-identity.md` — the X.509 identity substrate behind the per-vendor cert tarball
- [Eclipse Zenoh documentation](https://zenoh.io/docs/) — upstream user / developer docs
- `../design/zenoh-sandbox.md` — **never touched Zenoh before?** Start at the browser sandbox (mesh-internal — ask the operator for its URL): open it in two tabs and watch pub/sub/get/queryable work live, with no cert and no setup. It is an isolated throwaway router — separate from this trial fabric and carrying none of your data — so you can experiment freely before touching your real vendor prefix here.
