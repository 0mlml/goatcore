# Zenoh trial fabric — single-instance learning deployment

| Field | Value |
|---|---|
| Status | **Live on nova-a; first real publishers + verified wire-flow 2026-05-17; upstream-image pivot landed 2026-05-18.** `goat-zenoh-router` Healthy on `<mesh-ip>:7447` (mTLS) + `:8000` (REST). Initial substrate landed 2026-05-15; first publishers landed 2026-05-17 under track `portal-cert-mint-real` after walking three sequenced unblocks: (1) hand-minted per-vendor leaves replaced portal `--cert-sign-fake` placeholders; (2) F-135 flipped ACL flow direction from `["egress"]`-only to `["egress", "ingress"]` so client→router PUTs are admitted; (3) portal flipped to real `cert-sign` invocation against on-host CA bundle + age identity. Wire-level smoke is PASSING — independent Python `eclipse-zenoh==1.4.0` subscriber receives flowing samples from synth-tracks-demo / synth-power-demo within seconds. The 2026-05-18 ADR 0858 step 5 pivot replaced the in-tree zenoh image build with upstream `eclipse/zenoh:1.9.0` (digest-pinned) + bind-mounted upstream `libzenoh_backend_rocksdb.so` (per F-139) — closes the F-125 / F-136 plugin-ABI / libstdc++ class of failure by construction. Initial substrate-gap log: F-124 + F-125. |
| Scope | Single Zenoh router on a single goat-host, in a third trust domain separate from the legacy dev/test mesh and the usareur-af production mesh. Co-deployment target is the nova goatnet (`nova-goatnet.md`, Block 82) — Zenoh router co-resident with netbird-mgmt + signal-server on nova-a. |
| Related | [ADR 0500](../adr/0500-zenoh-data-fabric.md), [ADR 0858](../adr/0858-zenoh-image-upstream-pivot.md) (image posture; supersedes ADR 0708), [ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md), [ADR 0512](../adr/0512-rapid-adaptation-with-ai.md), `zenoh-data-fabric.md`, [`zenoh-trial-onboarding.md`](../operations/zenoh-trial-onboarding.md), `nova-goatnet.md`, `nova-x509-identity.md` |

## What this is

A deliberately small Zenoh deployment for a community of producers and consumers to use as a real pub/sub bus while we learn substrate behaviors at small scale. Single Zenoh router, single host, single trust domain, time-bounded. The trial fabric is its own independent thing — **not** a peer of the production goat-net Zenoh fabric, **not** federated to anything.

The architectural through-line is the same as production: **Zenoh runs inside a NetBird WireGuard P2P mesh.** Community members enroll as NetBird peers on the trial mesh and reach the Zenoh router via the mesh, with the same identity / encryption / ACL model the production fabric uses — just configured with trial-appropriate (relaxed) dial settings.

The trial deliberately previews two ergonomics ideas that the v1 production contract leaves vague: **(a)** a vendor-scoped namespace where each registered vendor owns a top-level prefix and self-services everything below it, and **(b)** a thin vendor self-service portal + message inspector UI that lets producers register topics, declare their serialization format, and observe what's flowing without operator handholding. Both are trial-only; lessons inform whether the production fabric absorbs them.

## What this is NOT

- **Not part of production goat-net.** No federation, no Spock peering, no shared identity, no shared offline CA.
- **Not subject to the [ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md) v1 contract verbatim.** This trial deliberately relaxes specific dials (ACL granularity, cert URI-SAN encoding, schema enforcement) for learning purposes. The contract's architectural decisions (Zenoh inside NetBird, schema-neutral substrate, audit-logger captures everything) still hold.
- **Not multi-router, not multi-site.** Single point of failure accepted. If the router dies, restart costs minutes.
- **Not the place where production lessons get applied.** Lessons inform v1 dial-tightening in a separate change. The trial is a learning instance.

## Scope of this prep — what THIS session produces

Three Zenoh-side artifacts:

1. This design doc — defines the trial-fabric shape and the learning questions
2. [`docs/operations/zenoh-trial-onboarding.md`](../operations/zenoh-trial-onboarding.md) — community-facing packet template (connection details filled in at deploy time)
3. A one-line pointer in `CLAUDE.md` under the Tactical Data Fabric Pilot bullet

Out of scope for THIS session (the other session's job):

- Hardware selection, AWS account / on-prem decision, terraform module
- Identity domain / fresh dev offline CA setup
- DNS zone / domain name decision
- Ansible inventory + group_vars + bootstrap orchestration for the host
- netbird-mgmt + signal-server setup on the host
- wg-cp0 posture for the trial mesh
- Community-onboarding workflow (setup-key issuance flow, named member list, OOB distribution channel)
- Operator-facing bringup runbook (will reference this doc when the deploy session authors it)

## Zenoh deployment shape

Single Zenoh router co-resident with netbird-mgmt + signal-server on one goat-host. Router binds the host's NetBird mesh IP (single interface) per the v1 architectural through-line. Image is upstream `eclipse/zenoh:1.9.0` pinned by content digest per [ADR 0858](../adr/0858-zenoh-image-upstream-pivot.md) (supersedes ADR 0708); the rocksdb storage backend `.so` is downloaded from the matching upstream `zenoh-backend-rocksdb` 1.9.0 release and bind-mounted via the role's host plugin dir (see F-139). Multi-arch (arm64 + amd64).

Plugins enabled at trial:

| Plugin | Purpose | Notes |
|---|---|---|
| audit-logger | Subscribes to `**`; captures every publish for the learning trail | Existing Rust plugin in `zenoh-plugins/audit-logger/` |
| rest | HTTP status + introspection endpoint | Operator-side; not exposed to community |
| storage_manager + RocksDB | Replay storage for the trial namespace | Lets community subscribers catch up on missed history; we want to see whether anyone actually uses this organically |
| schema-enforcer (audit mode) | Stamps publishes with `schema_status: registered|binary-declared|undeclared`; counts but does not reject | Per [ADR 0512](../adr/0512-rapid-adaptation-with-ai.md) rollout curve |

## Trial-mode dial settings (relaxed vs v1 contract)

Each row is a deliberate relaxation of the [ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md) v1 contract for trial purposes. The architectural pattern (Zenoh inside NetBird) stays; the policy settings on top of that pattern are dialed for learning.

| Dial | Trial setting | v1 production setting | Reason for relaxation |
|---|---|---|---|
| ACL posture | **Wide-open reads, vendor-scoped writes.** Default-deny floor; universal `query` + `declare_subscriber` allow for any mTLS peer (chain validation gates); per-vendor write rule (`put`/`delete` on `<vendor>/**`) added by operator at vendor registration, keyed off the leaf CN `<vendor>-trial-peer`. | Default-deny + per-prefix allowlist with C2-only writes on `cmd/`/`task/`; reads also ACL-scoped per data class | Trial-scope simplification: vendors share everything they publish (community-of-trust). Production tightens read-ACL once data classes get sensitivity grades. Vendor registration stays the only operator bottleneck on writes. |
| Identity model | **Per-vendor X.509 leaf with `CN=<vendor>-trial-peer` + `urn:goat-nova:vendor=<v>` URI-SAN** under a nova-specific offline-CA. See `nova-x509-identity.md`. CN drives the Zenoh ACL (Zenoh 1.x access_control has no URI-SAN matcher); URI-SAN is forward-compat for ADR 0844 production posture. | Per-peer cert with URI-SAN encoding site + role + echelon | Tightened from the prior "shared role-cert" sketch 2026-05-14: per-vendor cert is cheap, the operator-time delta vs shared-cert is negligible, and it lets the Zenoh ACL enforce per-vendor write scope **at the wire** rather than only at the portal layer. Identity axis revised from URI-SAN to CN 2026-05-15; trial learns whether further per-peer granularity is worth carrying to production. |
| Namespace | `<vendor>/<system>/<producer-discretion-arbitrary-depth>/v<N>` — vendor prefix registered by operator (one-line allowlist); everything below `<vendor>/` is producer self-service; arbitrary depth; optional trailing `v<N>` version tag encouraged. `ops/` reserved for the operator/control namespace (ADR 0844 / ADR 1035 — `system/` was the deprecated predecessor); `attest/schema/` reserved for schema declarations. | 12 reserved top-level prefixes (`data/`, `task/`, `ack/`, `telemetry/`, …) with namespace governance per data class | Vendor-coherent + producer-free; we observe whether vendor-scoped prefixes are the right shape for production rollout or whether class-keyed prefixes (the v1 default) read better in practice |
| Schema enforcement | Audit-mode only (captures + counts, never rejects). Producers strongly encouraged to declare format + schema at portal registration time. | Audit-mode at v1, enforce-mode in Phase 3 | Same posture as v1, deliberately not advanced to enforce-mode |
| Cross-site federation | None | Federated read on event / detection / entity / alert; site-scoped write on cmd / task | Trial is single-site by definition |
| Storage / replay | RocksDB backend enabled; 7-day retention on every registered `<vendor>/**` | RocksDB enabled at Phase 3 per v1 contract | Same; surface replay early so community can try it. Last-message inspector backend rides this. |
| Vendor self-service | Topic registry + inspector UI (substrate landed at [`ops/goat-zenoh-portal/`](../../ops/goat-zenoh-portal/); producers can also use API/CLI directly) | Operator-mediated namespace governance per v1 contract | Tries the ergonomics question: does a simple portal reduce operator burden enough to inform production tooling? |
| Sunset | Fixed date; teardown at sunset | None (production) | Trial scope |

## Namespace convention (the only thing the community needs to know)

**Shape:** `<vendor>/<system>/<producer-discretion-arbitrary-depth>/v<N>`

Where:

- **`<vendor>`** — operator-registered prefix. Examples: `example-vendor`, `org-1`, `goat`, `community` (catchall for trial participants without a real org affiliation). One vendor = one ACL allowlist entry; vendors can't write into other vendors' subtrees. Vendor registration is the bounded operator step (one-line config push, seconds); after that, everything below is producer-free.
- **`<system>`** — producer's choice. System codename, function name, integration name, whatever fits. The producer invents this; no operator approval. Examples: `tracks`, `sensors`, `cot`, `snitch`, `demo-test`.
- **`<producer-discretion>`** — arbitrary depth, fully producer's call. Sub-class, instance ID, region, mission, whatever shape the data wants. Examples: `entity/track`, `power/site-3`, `probe-report`, `foo/bar/baz`.
- **`v<N>`** — optional but **encouraged** trailing version tag (`v1`, `v2`, …). Lets producers version-evolve message shapes without breaking subscribers on the old version. Consumers subscribe to a specific `v<N>` or to `<topic>/v*` if they accept any version.

**Examples:**

```text
example-vendor/tracks/entity/track/v1
org-1/sensors/power/site-3/v2
org-1/cot/track/v1
goat/snitch/probe-report/v1
community/demo-test/foo/bar/v1
```

**Reserved prefixes** (operator-owned, vendors cannot publish under):

- **`ops/announcements`** — operator publishes status updates; everyone subscribes
- **`ops/audit-stream`** — audit-logger's own emission stream; operator-only
- *(`ops/` is the canonical operator/control namespace per ADR 0844 / ADR 1035; the deprecated `system/` predecessor is retained transitionally — see F-183)*
- `ops/fabric/cardinality/<site>` + `ops/fabric/census/<site>` — the single lazy crawler's rollup + adminspace census mirror (zenoh-cardinality-budget-and-lazy-crawler.md §4; registered per ADR 1035 §2). Consumers read these instead of walking the router.
- **`attest/schema/<vendor>/<system>/<...>/v<N>`** — schema declarations per topic pattern (encouraged for every registered topic; surfaced by the inspector for decode)

**Trust model — writes scoped, reads wide-open:** the vendor prefix is the trust boundary for **writes only**. A `community/...` producer cannot publish to `example-vendor/...`. **Reads, however, are wide-open**: any holder of a nova-CA-rooted leaf cert can subscribe to or query any topic on the trial fabric — including topics they don't own and topics they didn't register. This is a deliberate trial-scope simplification (community-of-trust; everyone publishing is implicitly opting into community visibility); production v1 tightens reads per data-class sensitivity. ACL is enforced at the Zenoh router via default-deny + universal-read allow (subject: any mTLS link) + per-vendor write allow keyed off the leaf **Common Name** (`<vendor>-trial-peer`). Earlier sketch keyed off the leaf URI-SAN; Zenoh 1.9.0's `access_control` schema has no URI-SAN matcher field — only `cert_common_names` (plus `interfaces`/`usernames`/`link_protocols`/`zids`). Discovery surfaced 2026-05-15 at first-fire push-through under track `goat-zenoh-first-deploy` (folded into F-124 item 7). URI-SAN is still minted by Track A for forward-compat with [ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md) production posture; the trial-fabric ACL just doesn't consult it.

## What we want to learn (the actual value of the trial)

Each question below maps to a v1-contract dial. We carry the trial 30-60 days, capture the audit log + REST endpoint metrics + portal usage data, then write up the findings + propose dial adjustments to the production contract in a separate change. **If we can't answer at least 5 of these 9 questions by sunset, the trial was too quiet to inform v1.**

1. **Vendor-scoped namespace coherence** — does the `<vendor>/<system>/.../v<N>` shape hold up? Do vendors stay coherent inside their prefix, or do producers stray into neighboring vendor subtrees (caught by ACL) or invent ambiguous `<system>` segments that collide with another vendor's `<system>`? Does any second-segment pattern emerge (per-data-class? per-function?) that suggests v1 should reserve top-level prefixes by data class (today's `data/<class>/<site>/<id>`) instead of by vendor?
2. **Version-tail uptake** — do producers actually use the optional `v<N>` trailing version tag? At what rate? Do subscribers subscribe to specific versions, to `v*`, or to versionless topics? Tells us whether v1 should make the version tail mandatory, encouraged, or skip it entirely.
3. **Schema + format discipline under low pressure** — when the enforcer is in audit-mode (counts but doesn't reject), do producers declare schemas at portal registration? At what rate? What % of publishes are `undeclared` after week 4? Do the declared formats cluster around a small set (JSON / CBOR / Protobuf) or sprawl?
4. **Pub/sub access patterns** — is the substrate used heavily for fanout (one publisher, many subscribers), point-to-point queryable, long-lived subscriptions, or transient pub/sub? Tells us whether the v1 contract's audit-logger and storage assumptions are right-sized.
5. **Portal usability** — does the vendor self-service portal actually reduce operator burden? Operator-fired vendor registrations / topic registrations / format declarations vs portal-fired equivalents. Friction points reported by vendors. Whether the inspector UI is used enough to justify production investment.
6. **Operational load on the operator** — token rotation, peer revocation, support requests, mesh issues — how often does the operator need to do something? Informs the staffing model for the production fabric at scale.
7. **Latency / partition / ack-timeout observations** — do the v1 ADR's defaults (sub-second target, expire-on-partition, observational-ack-timeout) match what trial users actually experience and care about? If trial users complain or surprise us, the defaults get revisited.
8. **Replay usage** — does Zenoh's storage/replay get used organically, or do consumers reach for it only when prompted? Informs whether replay should be on-by-default or opt-in in v1.
9. **Load shape** — measured publishes/sec, peak fanout, byte throughput on the single router. Gives the production scalability planning real numbers instead of armchair guesses.

## Vendor self-service portal + inspector

A thin web service that lets vendors register topics, declare their message format, and inspect what's flowing — without operator handholding. **Substrate landed 2026-05-14 at [`ops/goat-zenoh-portal/`](../../ops/goat-zenoh-portal/) (PR #50, squash-merged at `0d77d3f1`); deploys via [`ops/ansible/roles/goat-zenoh-portal/`](../../ops/ansible/roles/goat-zenoh-portal/).** **Not blocking for first deploy**: vendors can use raw Zenoh clients (CLI / SDK) against the router, and operators can edit the ACL allowlist by hand for the first ~few vendors. The portal flips on at first nova-a deploy.

### Access model — what each credential authorizes

The Zenoh router (port 7447) and the portal (HTTPS) are both mesh-internal only — a peer must be enrolled in the nova netbird mesh to reach either at the TCP layer. Past that, credentials gate behavior independently per surface:

| Credential | Zenoh router | Portal |
|---|---|---|
| Mesh tunnel only (no cert, no token) | No TLS handshake — connection refused | Read-only browse (`/vendor.html`, `/schema.html`, inspector UI); read-only `GET /api/...` queries |
| Vendor X.509 cert tarball | mTLS handshake succeeds; CN `<vendor>-trial-peer` gates publish to `<vendor>/**`; subscribe + get on `**` per universal-read rule | (none — Zenoh cert is not portal auth) |
| Vendor portal API key | (none — portal key is not router auth) | Vendor-scoped mutations: register topics under `<vendor>`, upload schema bodies, PATCH per-topic metadata, PUT per-language code examples |
| Operator API key (`PORTAL_OPERATOR_API_KEY`) | (none) | Operator-scoped mutations: `POST /api/vendors`, `DELETE /api/vendors/{prefix}` (tears down four marker stanzas + per-vendor RocksDB dir + SIGHUPs router — symmetric to mint), `PATCH /api/vendors/{prefix}` (fix display_name) |

The vendor cert tarball and vendor portal API key are issued together at vendor mint time (single `POST /api/vendors` call returns both). The operator API key is a constant-time-compare against the env-supplied `PORTAL_OPERATOR_API_KEY` value. The Zenoh router does not consult either portal key; the portal does not consult the Zenoh cert. Two surfaces, two auth namespaces.

### Architecture

```text
                        +------------------------+
                        |  Trial Zenoh router    |
                        |   (zenohd + plugins)   |
                        |  - audit-logger        |
                        |  - storage_manager     |
                        |  - REST plugin :8000   | ◀──┐
                        |  - access_control      |    │ "get last value" reads
                        +-----------▲------------+    │
                                    │ ACL config       │
                                    │ reload (SIGHUP)  │
                        +-----------┴------------+    │
                        |  vendor-portal (Go)    |────┘
                        |  - REST /api/*         |
                        |  - static UI /         |
                        |  - SQLite (registry)   |
                        +-----------▲------------+
                                    │
                          HTTPS via Traefik (vendor cert)
                                    │
                  +-----------------+----------------+
                  │ Vendor browser  │   curl / CI     │
                  │ - register      │   - automation  │
                  │ - inspect       │                 │
                  +-----------------+-----------------+
```

The portal is a single Go binary + a static-asset HTML/JS frontend. It owns a small SQLite registry of vendors, topics, formats, and schema refs. It renders the zenohd `access_control` config from the registry and SIGHUP-reloads the router on change. The inspector frontend reads "last value" per topic through the existing Zenoh REST plugin (`GET http://zenoh-host:8000/<key-expr>`) — Zenoh's storage_manager + RocksDB backend already retains last-N values per topic, so no new storage substrate.

### API surface (REST)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/vendors` | List registered vendors (public to portal users) |
| `POST` | `/api/vendors` | Register a new vendor (operator-only; mints the producer API key) |
| `GET` | `/api/vendors/<vendor>/topics` | List topics this vendor has registered |
| `POST` | `/api/vendors/<vendor>/topics` | Register a new topic under this vendor (vendor-auth) |
| `GET` | `/api/vendors/<vendor>/topics/<id>` | Topic details: key pattern, declared format, schema ref, last-seen-at, publish-count |
| `PATCH` | `/api/vendors/<vendor>/topics/<id>` | Update format / schema ref (vendor-auth) |
| `DELETE` | `/api/vendors/<vendor>/topics/<id>` | Retire a topic (vendor-auth; doesn't ACL-deny, just retires from registry) |
| `GET` | `/api/topics/<key-expr>/latest` | Inspector: last value(s) at this key expression (proxies through Zenoh REST plugin) |
| `GET` | `/api/topics/<key-expr>/recent?n=20` | Inspector: last N values (storage_manager-backed) |
| `GET` | `/api/topics/<key-expr>/stream` | Inspector: SSE stream of new publishes matching key expression |

Topic registration payload (POST `/api/vendors/<vendor>/topics`):

```json
{
  "key_pattern": "example-vendor/tracks/entity/track/v1",
  "format": "json",
  "schema_ref": "attest/schema/example-vendor/tracks/entity/track/v1",
  "description": "Entity track stream — see the vendor schema",
  "publish_to_storage": true
}
```

On topic registration, the portal:

1. Validates `key_pattern` starts with `<vendor>/<system>/...` for the authenticated vendor
2. Writes the registry row
3. Optionally publishes the schema reference to `attest/schema/<key_pattern>` on the vendor's behalf (if `schema_ref` provided)
4. Re-renders zenohd's `access_control` config to ensure `<vendor>/**` is in the per-vendor producer's allow list
5. SIGHUPs zenohd

### UI screens

Three primary screens, all behind vendor login (vendor API key as bearer; operator login is a superset):

1. **Vendor home** (`/`) — "Welcome, `<vendor>`. You have N topics registered. Last publish 12s ago. Inspect a topic →"
2. **Vendor topics** (`/vendors/<vendor>/topics`) — table of registered topics, with create/edit/retire. Filter by `<system>`. Per-row link to the inspector.
3. **Topic inspector** (`/topics/?key=<key-expr>`) — split pane:
   - Left: registered topics matching the key expression (collapsible tree by `<vendor>` → `<system>` → `<...>`)
   - Right: last value rendered using the topic's declared format, with timestamp + publisher peer-ID
   - Bottom: live-stream toggle (SSE) — new publishes append as they arrive

The inspector dispatches decode by the registered format (see next section). If a topic is unregistered or has no format declared, the inspector displays raw bytes + a hex view + a "this topic is undeclared" banner.

### Message format + serde libraries

The trial supports a small, well-defined set of message formats. The portal stores the declared format per topic; the inspector dispatches the decoder by that format. Producers pick from this list at registration time.

| Format | When to pick | Inspector decode |
|---|---|---|
| `json` | Default for human-readable; structured data; debugging friendly | Native `JSON.parse` + pretty render |
| `cbor` | Compact binary; structured; common in IoT / sensor flows | `cbor-x` JS library; decode to JSON, pretty render |
| `protobuf:<msg-name>` | High-performance binary; producers upload `.proto` to portal; consumers fetch on-demand | `protobuf.js` runtime; load `.proto` from `attest/schema/...`, decode named message, pretty render |
| `msgpack` | Compact binary; structured; popular in some ecosystems | `@msgpack/msgpack` JS library; decode to JSON |
| `text` | Plaintext (logs, free-form events); UTF-8 | Direct render with line-break preservation |
| `raw` | Opaque bytes; "I don't want the inspector to interpret" | Hex view + length only |

**Schema reference convention:** when the format requires an out-of-band schema (Protobuf especially), the producer publishes the schema source to `attest/schema/<vendor>/<system>/.../v<N>` (e.g., the `.proto` file content). The inspector fetches the schema at decode time and caches it. This rides the existing schema-declaration discipline rather than introducing a separate schema registry.

**Versioning:** the `v<N>` tail in the topic key expression is the version. Subscribers consume `<topic>/v1` for the v1 wire shape; producers publishing `v2` use a new topic, not an in-place evolution. The portal surfaces version sibling-topics in the inspector tree so a vendor can spot stale `v1` subscribers at a glance.

### Trust model

**Two layers — Zenoh ACL governs publish/subscribe at the wire; portal authz governs metadata operations:**

| Operation | Surface | Who is allowed | Enforcement |
|---|---|---|---|
| Read / subscribe any topic | Zenoh wire | Anyone who completed mTLS handshake (= anyone with a nova-CA-rooted leaf cert) | Zenoh ACL — universal `query` + `declare_subscriber` allow on `**` with subject `link_protocols: ["tls"]` (chain validation by the transport's `root_ca_certificate` is the structural gate) |
| Publish to a registered vendor prefix | Zenoh wire | Holder of the per-vendor cert (`CN=<vendor>-trial-peer`) | Zenoh ACL — per-vendor `put`/`delete` allow on `<vendor>/**`, subject matcher `cert_common_names: ["<vendor>-trial-peer"]`; one rule per registered vendor |
| Register a new vendor | Portal API | Operator only | Portal authz |
| Register / edit / retire a topic under `<vendor>/...` | Portal API | The vendor's API-key holder, OR operator | Portal authz — vendor scoped to its own prefix; cannot touch another vendor's topics |
| Edit topic metadata (format, schema ref, description) | Portal API | Same as register | Portal authz |
| Inspect / view any topic | Portal UI | Anyone with a nova-CA-rooted leaf cert (anonymous to portal, but on the trial fabric) | Read-through to Zenoh; matches the wide-open read posture |
| View / edit `ops/*` topics or audit stream | Portal UI + Zenoh wire | Operator only | Portal authz + ACL operator subject |

**Per the operator directive: a vendor can create, edit, and publish to their own topics. Anyone on the trial fabric can read everything.** Topic creation is structurally just "publish to a key under your prefix" — Zenoh has no separate create operation — but the portal layer attaches registration metadata (format, schema ref, description) for the inspector to use, and prevents other vendors from publishing to your prefix at the ACL layer.

Vendor API keys are not Zenoh credentials — they authenticate to the portal only. Publishing to Zenoh uses the **per-vendor X.509 leaf** described at `nova-x509-identity.md`; the ACL's per-vendor write rule keys off the cert's **Common Name** (`<vendor>-trial-peer`) and structurally prevents one vendor from publishing into another vendor's prefix at the wire layer. The portal handles *registration + metadata + inspection*; the cert handles *authentication + per-vendor write scope*. (Earlier sketch had a shared role-cert with vendor scope at the portal-API layer only; tightened to per-vendor cert 2026-05-14 per `nova-x509-identity.md` §"Why this exists now." Identity axis revised from leaf URI-SAN to leaf CN 2026-05-15 — Zenoh 1.x access_control schema has no URI-SAN matcher; CN is the wire-effective shape. See F-124 item 7.)

### Open design decisions for the build session

Resolved during the 2026-05-14 build session ([`ops/goat-zenoh-portal/`](../../ops/goat-zenoh-portal/), track `nova-vendor-portal`):

1. **Backend language** — **Go.** Matches the rest of the substrate, request-shaped per [ADR 0112](../adr/0112-tech-strategy-workload-shape-language-boundary.md), single static binary, simple SQLite. Rust is overkill for this surface.
2. **Vendor registration approval** — **operator-approved by default.** `portal_operator_approval_required: true` in the Ansible role. The operator can flip per-vendor via `auto_approve: true` in the POST body (e.g. when batch-onboarding a cohort). Re-evaluate at week 1 against actual friction.
3. **Where does the portal run** — **same host as zenohd (nova-a), in its own container, fronted by Traefik.** Mesh-only; no public DNS. See [`ops/ansible/roles/goat-zenoh-portal/`](../../ops/ansible/roles/goat-zenoh-portal/).
4. **Where do the static frontend assets live** — **embedded in the Go binary** via `web/web.go` + `embed.FS` so it's a single-artifact deploy. No separate Nginx.
5. **How does the portal reload zenohd's ACL** — **write the JSON5 config file, then run an operator-supplied shell command** (default: `docker exec zenohd kill -HUP 1`). `--acl-reload-fake` skips reload for dev/test. If zenohd doesn't hot-reload cleanly we fall back to `docker compose restart zenoh` — same shape, different command, no portal change needed.
6. **Authentication for vendor login** — **portal-issued API key in the `Authorization: Bearer` header.** SHA-256-pre-hashed + bcrypt at default cost (vendor tokens are >72 bytes so direct bcrypt would silently truncate). Operator API key is a constant-time compare against the env-supplied value. Future OIDC federation against Ory Kratos is out of scope for trial.
7. **Inspector stream** — **SSE.** Simpler than WebSocket, works through Traefik without special config. The portal poll-interval is 1s against the zenohd REST plugin (zenohd doesn't expose native streaming today); end-to-end latency is poll-interval-bounded. Acceptable at trial scope.

### Build sequencing

This is **not blocking** for the deploy session. Sequence:

1. **Deploy session lands the namespace + ACL contract** — zenohd up with default-deny + 1-2 hand-edited vendor allowlist entries (`community/**` for the operator's own testing). First vendors onboard via direct operator coordination.
2. **First few vendors publish via raw Zenoh CLI/SDK**, manually registering schemas at `attest/schema/...` per the namespace convention. Validates the contract holds under real publish patterns.
3. **Portal substrate landed 2026-05-14** at [`ops/goat-zenoh-portal/`](../../ops/goat-zenoh-portal/) (track `nova-vendor-portal`, PR #50 squash-merged at `0d77d3f1`); deploys via [`ops/ansible/roles/goat-zenoh-portal/`](../../ops/ansible/roles/goat-zenoh-portal/). All seven open design decisions above resolved during the build session. The cert-sign URI-SAN substrate extension also landed 2026-05-14 (Block 82A), so the portal's vendor-mint shell-out is unblocked from the cert side; the `--acl-reload-fake` switch stays on until first nova-a deploy stands up the zenohd container, then flips to real via group_vars overrides.
4. **Cutover** — once portal lands, operator migrates the existing manual registrations into the registry. New vendors onboard via portal.

If the trial sunsets before the portal builds, the namespace + ACL contract still informed v1; the portal lessons are deferred to a future trial-fabric instance.

## What the deploy session inherits from this prep

When the next session brings up the host, they should be able to read this design doc + the onboarding packet template + the v1 ADR and know:

- What Zenoh image to deploy (already built; tag pinned per ADR 0708's amendment)
- What plugins to enable (named above — `audit-logger`, `rest`, `storage_manager` + RocksDB, `schema-enforcer` audit-mode, `access_control`)
- What ACL config to render (default-deny + per-vendor `<vendor>/**` allowlist rule — concrete config snippet below; start with 1-2 hand-edited vendor entries for first-cohort registration before the portal lands)
- What namespace prefixes to reserve operator-side (`ops/`, `attest/schema/`)
- What to put in the onboarding packet (template at [`zenoh-trial-onboarding.md`](../operations/zenoh-trial-onboarding.md))
- What to capture as evidence (audit-logger output → archived at sunset; REST endpoint metrics → snapshot weekly; portal usage data once portal lands)

Concrete ACL config sketch for the deploy session to render (exact JSON5 syntax for Zenoh 1.9.0 confirmed at deploy time):

```json5
{
  enabled: true,
  default_permission: "deny",
  rules: [
    // Universal read — any nova-CA-rooted peer can subscribe to or get any topic.
    // Wide-open reads per the trial's community-of-trust posture.
    { id: "trial-peer-r-all",
      messages: ["declare_subscriber", "get"], flows: ["egress"], permission: "allow",
      key_exprs: ["**"] },
    // Per-vendor writes — operator (or portal, once it lands) adds one allow rule per registered vendor.
    // Writes scoped to the vendor's own prefix; cannot publish into another vendor's subtree.
    { id: "trial-peer-w-community",
      messages: ["put", "delete"], flows: ["egress"], permission: "allow",
      key_exprs: ["community/**"] },
    { id: "trial-peer-w-goat",
      messages: ["put", "delete"], flows: ["egress"], permission: "allow",
      key_exprs: ["goat/**"] },
    // Schema declarations under attest/schema/<vendor>/... — vendors write their own schema docs.
    // Portal enforces the second-segment-matches-vendor invariant at registration; ACL allows the broader pattern.
    { id: "trial-peer-w-attest-schema",
      messages: ["put", "delete"], flows: ["egress"], permission: "allow",
      key_exprs: ["attest/schema/**"] },
    // Operator writes to system topics (announcements, etc.)
    { id: "operator-w-system",
      messages: ["put", "delete"], flows: ["egress"], permission: "allow",
      key_exprs: ["ops/**"] },
  ],
  // Per-vendor subjects keyed off the leaf Common Name — Zenoh 1.x `access_control` schema does NOT
  // support a URI-SAN matcher field; only `cert_common_names` (plural list, exact match) +
  // `interfaces` / `usernames` / `link_protocols` / `zids`. Resolved at first-fire push-through
  // 2026-05-15 against Zenoh 1.9.0 — see F-124 item 7. URI-SAN is still minted by Track A for
  // forward-compat with ADR 0844 production posture but is not consulted by the trial-fabric ACL.
  // access_control schema at deploy time — the shape below is illustrative.
  subjects: [
    { id: "trial-peer-read",  link_protocols:     ["tls"] },                          // any mTLS peer (chain validation by root_ca_certificate gates)
    { id: "vendor-community", cert_common_names:  ["community-trial-peer"] },
    { id: "vendor-goat",      cert_common_names:  ["goat-trial-peer"] },
    { id: "operator",         cert_common_names:  ["operator-trial-peer"] },
  ],
  policies: [
    { rules: ["trial-peer-r-all"], subjects: ["trial-peer-read"] },
    { rules: ["trial-peer-w-community", "trial-peer-w-attest-schema"], subjects: ["vendor-community"] },
    { rules: ["trial-peer-w-goat", "trial-peer-w-attest-schema"], subjects: ["vendor-goat"] },
    { rules: ["trial-peer-r-all", "operator-w-system"], subjects: ["operator"] },
  ],
}
```

**Adding a vendor:** as of 2026-05-20 the portal's `POST /api/vendors` does the whole sequence end-to-end with no operator-side editing — (a) mints the per-vendor leaf via `cert-sign --cn "<vendor>-trial-peer" --uri-san "urn:goat-nova:vendor=<v>"` per `nova-x509-identity.md` (CN drives the ACL; URI-SAN is forward-compat for ADR 0844), (b) writes four marker-block stanzas to the live `/etc/goat-zenoh/config.json5` — one `trial-peer-w-<vendor>` write rule + one `vendor-<vendor>` subject + one policy + one `<vendor>: { key_expr: "<vendor>/**", ... }` storage entry under `storage_manager.storages`, (c) provisions the per-vendor rocksdb dir on the host so the storage backend can spawn, (d) sends SIGHUP to the zenoh-router container via the Docker Engine API over the host's mounted socket. The universal read rule (`trial-peer-r-all`) and its subject (`trial-peer-read`) are set once at deploy and never edited per-vendor. `ops/ansible/roles/goat-zenoh/files/add-vendor.sh` is the equivalent operator-fired escape hatch — same four marker-block writes, same SIGHUP, callable from the host when the portal path is unavailable. Both paths use the rule-id `trial-peer-w-<vendor>` as the idempotency anchor so a re-run is a no-op. Closes the original substrate gaps captured in F-DOG-15 (per-vendor storage not auto-provisioned) and F-145 (old portal renderer used the deprecated `cert_uri_san_*` shape).

**Removing a vendor:** symmetric counterpart to "Adding a vendor" (83F). The portal's `DELETE /api/vendors/{prefix}` does the whole sequence end-to-end — (a) refuses with 409 if the vendor still owns non-retired topics (registry-row preflight; operator must retire those first via `DELETE /api/vendors/{v}/topics/{id}`); (b) removes the registry row + cascade-cleans schema + code-example rows; (c) strips the same four marker-block stanzas the create path wrote — `trial-peer-w-<vendor>` rule + `vendor-<vendor>` subject + the policy + the `<vendor>:` storage entry — anchored on the vendor-named identifiers so no neighbor stanza is touched; (d) SIGHUPs the router; (e) best-effort `rm -rf` of the per-vendor rocksdb dir (failure here is logged but doesn't fail the request — wire-layer revocation is in effect either way; operator can `rm -rf` manually). `ops/ansible/roles/goat-zenoh/files/delete-vendor.sh` is the operator-fired escape hatch (sibling of `add-vendor.sh`, same shape/exit-code conventions). Both paths use regex-based stanza removal pinned by `internal/acl/acl_test.go` so the two stay in lockstep with the create-path emitter. Order matters: registry-row delete first, then wire-layer — so a half-failure (wire-layer revoke fails after registry-row gone) can be reconciled by re-running DELETE (idempotent on both sides). Closes 83F.

**Audit subscribe:** the audit-logger plugin subscribes to `**` from inside the zenohd process — it doesn't pass through the wire-level ACL the same way external `trial-peer` subjects do. The plugin's subscription is mediated by the plugin loader, not the `access_control` plugin. No separate `audit` subject needed; if the audit-logger ever externalizes (cross-process via a Unix socket etc.), revisit and add an `audit` subject with `**` declare_subscriber rights.

Schema matcher names confirmed against running Zenoh 1.9.0 at first-fire push-through 2026-05-15: only `cert_common_names` (plus `interfaces` / `usernames` / `link_protocols` / `zids`) are supported — no URI-SAN matcher exists. The shape above is the wire-effective form post-discovery; F-124 item 7 captures the discovery process.

## Partner-peering facilitation surface (trial scope)

A four-PR substrate inside the existing goat-zenoh-portal that makes it
possible for a Zenoh-first-timer running their own router to peer with
the trial fabric without DM'ing the operator for every config value.
Leans into the spirit of `partner-peering-portal.md`
+ [ADR 0856](../adr/0856-partner-peering-capability-mint.md) but **does
not instantiate them** — no approval ladder, no `release/<partner>/...`
namespace, no YubiKey-PIV mint ceremony. Trial scope only.

Mental model: a partner-with-their-own-router is a different bundle
flavor of the existing vendor-mint flow. Same `POST /api/vendors`, same
offline-CA URI-SAN, same per-prefix ACL allow row — the only difference
is the contents of the generated bundle (a `zenohd.json5` instead of a
Python publisher snippet).

### Surfaces

- **Nav** &mdash; portal top nav was 8 flat links; refactored into four
  grouped `<details>` dropdowns (Catalog / Inspect / Peering / Reference)
  + Home so the new section has somewhere to live without crowding.
- **Our Config (PR-2)** &mdash; `GET /api/fabric/config` (no auth)
  returns a JSON self-description: router endpoint, mTLS posture, root-
  CA fingerprint + bundle URL, supported Zenoh version, reserved top-
  level prefixes, currently-taken vendor prefixes (live registry query),
  URI-SAN prefix, registration endpoint shapes, recommended + supported
  wire formats, and the two onboarding flavors (publisher / router-peer).
  The human-readable render at `/peering/our-config.html` fetches the
  same JSON and renders it card-style. Public root-CA bundle at
  `GET /peering/root-ca.pem` so a partner can pin our trust anchor
  before mint (chicken-and-egg without it).
- **Guide (PR-3, #357 merged)** &mdash; `/peering/guide.html`, first-
  time-Zenoh explainer + publisher-vs-router-peer contrast. 90-second
  Zenoh intro, decision table, Flavor A walkthrough (Publisher), Flavor
  B walkthrough (Router-peer), what's-not-in-trial, getting help.
  Feedback channel fetched live from `GET /api/fabric/config`.
- **zenohd-config generator (PR-4)** &mdash; `GET /api/fabric/zenohd-config?prefix=<x>`,
  copy-paste partner-side router config in json5 with our endpoint,
  mTLS pinning, cert-path placeholders, router-mode connect to our
  endpoint. Companion page `/peering/zenohd-config.html` wraps the
  endpoint with prefix-input + Generate button + copy/download +
  prefix-status feedback (available / reserved / taken-existing-vendor).
  Reserved-prefix collision returns 409; taken-existing-vendor is
  informational (200 + `X-Prefix-Status` header) since the mint flow
  elsewhere is the authoritative gate on prefix allocation.
- **Workbench (PR-5)** &mdash; `POST /api/peering/lint` + companion
  `/peering/workbench.html` interactive page. Two-pane layout: paste a
  `zenohd.json5` draft on the left, structured findings stream on the
  right with 300 ms debounced re-lint as the user types. Findings carry
  severity (error / warn / info), JSONPointer-ish field path, message,
  and a doc anchor back into the Guide / Our Config / Router Config
  pages. Rules: mode must be `router`; `connect.endpoints` must contain
  our published router endpoint (scheme-tolerant host:port match);
  `enable_mtls` + `verify_name_on_connect` both true; all five cert paths
  set; ACL `key_exprs` cover the user's intended prefix when supplied;
  Zenoh 1.9.0 version-pin advisory (info). A `Load template` button
  proxies `GET /api/fabric/zenohd-config?prefix=<intended>` into the
  paste box so first-time users see a known-good config to edit rather
  than a blank screen. Backend has no new dependencies &mdash; a 150-line
  in-package JSON5 preprocessor (line + block comments, trailing commas,
  unquoted identifier keys, single-quoted strings) normalizes to strict
  JSON before `encoding/json` parses into `map[string]any`; hand-coded
  semantic checks against the tree produce findings. The narrow domain
  (~6 rules) doesn't justify a generic JSON Schema validator we don't
  otherwise need. Same no-auth posture as `/api/fabric/zenohd-config`:
  pasted content carries no secrets and a partner shouldn't need a
  vendor row to validate their pre-mint draft.

### Explicitly NOT in trial scope

- Operator approval ladder ([ADR 0856 D3](../adr/0856-partner-peering-capability-mint.md)
  YubiKey-PIV ceremony &mdash; production-only)
- `release/<partner>/...` namespace separation ([ADR 0821](../adr/0821-mission-partner-sharing-pattern.md))
- Reciprocity / bidirectional peering
- Standalone compatibility-check + request-lifecycle endpoints

### Lift path to production

When Block 84
lights up against a real partner relationship, the read-only half
(`/peering/our-config.html` + `GET /api/fabric/config` + the public
root-CA bundle endpoint) lifts untouched &mdash; that "this is the
fabric you're peering with" surface is identical between trial and
production. The Peering nav slot is already positioned for the
approval-ladder + request-lifecycle pages.

## Sunset + lessons folding

- **Sunset date** — set explicitly at deploy time (recommended 60 days from go-live; trial-specific override OK)
- **Pre-sunset warning** — 7 days before sunset, operator publishes to `ops/announcements`; community members back up their data if any
- **Teardown** — `terraform destroy` (if cloud) or host repurpose (if on-prem), archive the audit log + schema-enforcer counter snapshot + portal registry SQLite dump
- **Lessons fold** — within 2 weeks of sunset, the operator writes `docs/reports/<date>-zenoh-trial-findings.md` covering the 9 questions above, then opens a follow-on PR proposing any dial adjustments to the v1 ADR 0844 contract (vendor-vs-class top-level prefix, version-tail mandatory/optional/dropped, portal worth productizing)

## Cross-references

- [ADR 0844](../adr/0844-zenoh-deployment-topology-and-acl-v1.md) — v1 production contract; the trial is a deliberate relaxation of the dials within that contract
- [ADR 0500](../adr/0500-zenoh-data-fabric.md) — Zenoh-as-data-fabric anchor
- [ADR 0708](../adr/0708-build-zenoh-from-source.md) — image build-from-source pattern
- [ADR 0512](../adr/0512-rapid-adaptation-with-ai.md) — data-dictionary discipline + schema-enforcer rollout curve (trial stays at audit-mode)
- `zenoh-data-fabric.md` — canonical Zenoh design doc; this trial design is a separate file because trial scope is bounded and lessons fold back into the canonical doc rather than living there
- [`zenoh-trial-onboarding.md`](../operations/zenoh-trial-onboarding.md) — community-facing packet template
- `nova-goatnet.md` — co-deployment target; trial-fabric Zenoh router lives on nova-a alongside netbird-mgmt + signal-server
