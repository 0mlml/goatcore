# Portal-driven schema publishing

**One-liner.** Vendors registering a topic in the portal can upload the topic's schema body (a protobuf descriptor, a JSON Schema document, etc.). The portal stores it, lets anyone fetch it back, and makes the full registry browseable so consumers can find what types exist and decode the bytes on the wire.

**Related.**

- [`data-dictionary.md`](data-dictionary.md) §"Trial-fabric instantiation (Block 85)" — how this fits inside the canonical data-dictionary discipline.
- `zenoh-data-fabric.md` §"attest namespace" — the long-term on-wire namespace this work eventually targets.
- [`ADR 0506`](../adr/0506-fabric-schema-evolution.md) — versioning semantics.
- [`ADR 0512`](../adr/0512-rapid-adaptation-with-ai.md) — canonical data-dictionary ADR (Block 23 proper).

**Scope.** The trial fabric (Block 82A) first. Same shape carries to future site-fabrics, but the v1 implementation is explicitly a *trial-fabric expedient*. The long-term goat-net portal will revisit each decision below with greater rigor — that path is sketched in §"Long-term evolution" at the end.

---

## What ships in v1

Three things, all inside the existing portal at [`ops/goat-zenoh-portal/`](../../ops/goat-zenoh-portal/):

1. **Upload field on the Topics page.** Same form that already captures key pattern + format + `schema_ref` pointer now also accepts a file. Accepted format in v1: protobuf `FileDescriptorSet` (`.fds`) — what you get from `protoc --descriptor_set_out`. JSON Schema and AsyncAPI follow later.
2. **Server-side storage.** The portal saves the uploaded file to its local disk (next to the existing SQLite registry DB) and records a row in a new `topic_schemas` table tying the file to a topic + version.
3. **Fetch API.** Anyone authenticated to the portal can `GET /api/schemas/<vendor>/<system>/.../v<N>` and pull the file back. The Topics page renders a download link next to each topic that has a schema attached.

That's the whole substrate. The inspector picks up "real decode" as a follow-on consumer of the same fetch API.

### Schema viewer (PR-D, 2026-05-18)

The binary `.fds` is machine-friendly but unreadable to humans. PR-D adds a structured rendering of any uploaded descriptor — a Swagger-style page that lists every message, every field with type + cardinality + JSON-name, every enum with its values, and the originating `.proto` file with package + syntax.

- **Render endpoint:** `GET /api/schemas/render/{key...}[?version=v2]`. Walks the uploaded `FileDescriptorSet` via `google.golang.org/protobuf/reflect/protodesc` and returns flat JSON describing files + messages + enums. Backed by [`internal/decoder/render.go`](../../ops/goat-zenoh-portal/internal/decoder/render.go). Same Bearer-auth surface as the existing fetch endpoint.
- **Viewer page:** [`web/static/schema.html`](../../ops/goat-zenoh-portal/web/static/schema.html) + [`schema.js`](../../ops/goat-zenoh-portal/web/static/schema.js). Topics-page schema column now offers `view` + `↓` affordances. The viewer page supports a version picker, shareable URLs (`?key=<topic>&version=v2`), and clickable type-references that scroll to the target message or enum card.
- **Consumer-side use:** the same render endpoint is what an external consumer hits when it wants a structured, language-agnostic description of a topic's data dictionary — no protoc round-trip needed. A consumer that wants the raw bytes still uses `GET /api/schemas/<key>`.

The wire format on the topic stays whatever the producer is emitting (JSON / CBOR / protobuf). The `.fds` is a data-dictionary artifact, not a wire-format declaration. A producer that migrates to protobuf wire format can flip the topic format to `protobuf:<MsgName>` and the inspector's decoded-JSON path lights up automatically (the descriptor is already on the portal).

### Reconciliation against the synth-vendor harness (PR-D, 2026-05-18)

Canonical `.proto` sources for the seven topics that `ops/zenoh-trial-peers/synth-vendor` publishes are persisted at [`ops/zenoh-trial-peers/synth-vendor-schemas/`](https://github.com/usareur-af/goatstuff/tree/main/ops/zenoh-trial-peers/synth-vendor-schemas) with three reconciliation scripts:

- `build-fds.sh` — protoc → per-topic `.fds` under `out/` (gitignored).
- `upload-all.sh` — idempotent POST against the live portal; sha256-match skip, conflict surface, optional `--set-format` to flip topic format.
- `delete-ghost-topics.sh` — one-time cleanup of three stale topics a prior session created with invented key patterns (`caracal/tracks/aircraft/v1`, `community/weather/station/v1`, `pangolin/inspections/asset/v1`).

This pattern (per-trial-fabric `synth-vendor-schemas/` directory beside the producer, with a `topic-manifest.tsv` mapping topic → .proto → message-name → wire-format) is the worked example real vendors are pointed at when asked "what does a published data dictionary look like in the goat fabric?"

### Code examples — autogen + vendor overrides (PR-D, 2026-05-18)

The schema viewer's "Example code" section ships four per-language tabs (Rust / Go / Python / TypeScript) showing a working subscribe-and-decode snippet for the topic's primary message. Two paths:

- **Autogen** — portal-templated via [`internal/decoder/examples.go`](../../ops/goat-zenoh-portal/internal/decoder/examples.go) using Go's `text/template`. No network calls, no LLM, no SaaS. Templated against the descriptor + topic key. Each snippet carries an `Autogen` badge plus a section-level disclaimer that the code is "Notional, not validated" — the portal compiles nothing.
- **Vendor-provided** — per-(topic, language) overrides stored in the new `topic_code_examples` registry table via `PUT /api/vendors/{v}/topics/{id}/examples/{lang}`. The viewer's per-language "Provide custom" / "Edit override" buttons open an inline editor; saving replaces the autogen body for that language and flips the badge to a green `Vendor-provided`. A "Revert to autogen" action deletes the row and the autogen path takes over again. Cap: 64 KiB per row; languages constrained to the recognized set (rust/go/python/typescript/javascript/c/cpp/java/shell).

The fall-back order at render time is: vendor row for that language → autogen template → omitted (no template + no row). The viewer's section-level disclaimer is rendered unconditionally; per-tab badges distinguish what the operator is looking at.

### XML / XSD parity (PR-E, 2026-07-28)

The write side (topic format `xml` / `xml:<RootElement>` + `xsd` schema-document uploads) landed first; PR-E brings XSD topics to full parity with protobuf on the **read** surfaces, so an `xml:<Root>` topic with an uploaded `.xsd` is not a second-class citizen in the viewer:

- **Data dictionary.** [`internal/decoder/xsd_render.go`](../../ops/goat-zenoh-portal/internal/decoder/xsd_render.go) `RenderXSD` emits the *same* `RenderedSchema` JSON shape the protobuf renderer produces — top-level `<xs:element>` and named `<xs:complexType>` become "messages", child elements + attributes (prefixed `@`) become "fields", `minOccurs`/`maxOccurs` map into the `singular`/`optional`/`repeated` cardinality vocabulary the frontend already understands, and enumeration `simpleType`s become enums. The render handler dispatches on the persisted schema-body format (`schema_render.go`), so `schema.js` renders both with no format branch; a `source_lang` field only relabels the vocabulary (elements/types vs messages) and the source tab (`XSD source` vs `.proto source`).
- **Autogen code tabs.** [`internal/decoder/xsd_examples.go`](../../ops/goat-zenoh-portal/internal/decoder/xsd_examples.go) `RenderXSDCodeExamples` reuses the `CodeExample` struct and the same four languages, emitting subscribe-and-parse snippets (Rust `quick-xml`/serde, Go `encoding/xml`, Python `ElementTree`/`xmlschema`, TypeScript `fast-xml-parser`) keyed on the declared root element. Vendor-provided overrides work unchanged — `topic_code_examples` is language-keyed and format-agnostic.
- **Inspector decode.** The `GET /api/topics/decoded` gate widened to route `xml` / `xml:<Root>` topics into [`internal/decoder/xml_decode.go`](../../ops/goat-zenoh-portal/internal/decoder/xml_decode.go) `DecodeXML`, which projects the XML document to JSON (elements → keys, attributes → `@name`, repeated siblings → arrays) after checking the root against the declared element. XML is self-describing, so the decode path does **not** require an uploaded `.xsd`.

**Deliberate boundary:** `DecodeXML` is a structural XML→JSON projection plus a root-element check, not a full validating XSD parser — a complete instance validator is out of scope, and full XSD (substitution groups, cross-import extension chains, `redefine`) is rendered best-effort. This mirrors the protobuf path's own scope: the artifact is a data dictionary, not a wire-format enforcer.

### Topic-to-data-type cardinality (ADR 0516)

Recorded as policy: every topic carries exactly one message type (posture A); when a producer genuinely has tightly-related variants, it uses a protobuf `oneof` root (posture C); envelope-plus-opaque-payload (posture B) is rejected at the goat-net layer. The schema viewer renders `oneof` blocks correctly in-place, so posture C is supported by the existing code path with no new UI work. See [ADR 0516](../adr/0516-topic-to-data-type-cardinality.md) for the full rationale.

---

## The five decisions (locked in for v1)

Each decision below is **v1 / trial-fabric** and explicitly temporary. The reason it's the v1 answer is in **Why**; the long-term direction is in [§"Long-term evolution"](#long-term-evolution).

### 1. Where the schema file lives

**v1 decision.** Portal-local disk. The portal owns the durable copy; uploaded files sit alongside the existing SQLite registry under `/opt/goat-zenoh-portal/schemas/<vendor>/<system>/.../v<N>.fds`. Backed up the same way the rest of the portal state is.

**Why.** Simplest possible v1. The portal already has persistent disk; the existing SQLite registry is there. Adding a directory of files is a few lines. No need to validate Zenoh storage-manager config for `attest/**` keys, no need to think about Zenoh-side replication or retention.

### 2. Who signs the upload

**v1 decision.** The portal. When a vendor uploads a schema, the portal records the upload with the vendor's identity (from the existing auth flow) but the *durable artifact* is owned by the portal — no Zenoh publish happens in v1, so there's no on-wire identity question yet.

**Why.** Since the schema lives on the portal's disk (decision 1) rather than on the Zenoh fabric, the "who publishes to `attest/schema/...`" question is deferred until decision 1 changes. The audit trail still captures *which vendor uploaded* because the upload request is authenticated.

### 3. How versioning works in the UI

**v1 decision.** The vendor types the version number. When uploading, they fill in:

- A version label (`v1`, `v2`, …) — free-text, but the portal enforces the `v<N>` shape.
- A compatibility tag (`backward` / `forward` / `none`) — dropdown.

The portal stores both and surfaces them in the registry. No compatibility analysis happens on the portal side; the vendor's claim is taken at face value.

**Why.** Vendors are the only ones who know what changed schema-version to schema-version. Asking the portal to compare two `.fds` files and infer compatibility is a meaningful engineering problem that pays off only when there's enough schema-evolution volume to warrant it. Until then, vendor self-declaration is cheaper and correct-enough.

### 4. How a consumer fetches a schema

**v1 decision.** Portal proxy. Consumers call `GET /api/schemas/<vendor>/<system>/.../v<N>` on the portal. The portal looks up the file on its local disk and returns the bytes (with the same auth model as the rest of the portal API).

**Why.** Matches the existing portal auth + browse shape. Consumers already talk to the portal for the topic registry; serving the schema body alongside is the same surface. Avoids requiring every consumer to add a Zenoh client just to fetch a schema.

### 5. Which formats we accept first

**v1 decision.** Protobuf `FileDescriptorSet` (`.fds`) only.

**Why.** The portal already supports `protobuf:<MsgName>` as a format option in the Topics registry; `.fds` is the natural pairing — it's the compiled, self-contained descriptor that an inspector can use to decode protobuf bytes without having the original `.proto` source. JSON Schema, AsyncAPI fragments, and OpenAPI documents are all useful but each one adds a parsing + validation path. Doing one format end-to-end first is cheaper than doing three partially.

#### 5.1. Source-format upload (v1.1, 2026-05-24 per F-158)

The v1 decision above (`.fds`-only) was a deliberate trial-fabric expedient. The 2026-05-19 vendor-dogfood walk demonstrated that *vendors* installing `protoc` (~50 MB on macOS via Homebrew; non-trivial install on Windows without a package manager) was the longest single step in the OOB-bundle path. The portal is the natural place to centralize the compile step.

**v1.1 decision.** Accept `protobuf-source` as a second upload format. The handler reads the uploaded `.proto` source, compiles it in-process via [`github.com/bufbuild/protocompile`](https://pkg.go.dev/github.com/bufbuild/protocompile) (pure-Go, MIT), and persists the resulting `FileDescriptorSet` under the existing `protobuf-fds` on-disk layout. Decoder + render endpoints + schema viewer are unchanged downstream — the bytes downstream are byte-equivalent to what `protoc --descriptor_set_out=... --include_imports` would have written.

**Scope.** Single-file uploads with imports limited to the well-known types (`google/protobuf/*`). Vendors with multi-file source continue to use the `.fds` upload path; the trial pipeline's [`build-fds.sh`](https://github.com/usareur-af/goatstuff/blob/main/ops/zenoh-trial-peers/synth-vendor-schemas/build-fds.sh) remains the canonical multi-file recipe. Supporting multi-file uploads requires a contract for how the vendor delivers them (zip? multipart-with-multiple-files? tarball?) — deferred until a vendor pulls.

**Compiler choice.** Alternatives considered: (a) shell out to `protoc` from the portal container — adds protoc + the WKT proto-source tree to the image surface, contradicts the distroless posture; (b) vendor `libprotoc` via cgo — moves the compiler dependency into Go-native via a non-Go path; (c) hand-roll a `.proto` parser — substantial code for a narrow gain. `bufbuild/protocompile` is pure Go, no native deps, actively maintained by Buf, and emits descriptors directly so the assembly step lands in normal Go code without any build-time tooling on the portal side. MIT-licensed (consistent with [ADR 0111](../adr/0111-vendor-friendly-license-posture.md)).

**Persisted shape.** The stored row's `format` column remains `protobuf-fds` — the bytes on disk *are* a `FileDescriptorSet` after the compile step. The audit-log details JSON grows a `source_format` field (`"protobuf-fds"` or `"protobuf-source"`) so an operator can tell which path produced the stored bytes. The `schemaEntry` JSON API response shape is unchanged; clients see `format=protobuf-fds` either way.

**Failure-mode posture (per [ADR 0317](../adr/0317-per-service-failure-mode-declaration.md) discipline).** The new compile path is a user-input validation surface, not a dependency surface — it doesn't introduce any new external service the portal depends on. The compiler (`bufbuild/protocompile`) runs in-process with no shell-out, no temp files, no fork/exec; failure modes are fail-closed-on-user-input with structured 4xx responses:

| Failure | Response | Where it's caught |
|---|---|---|
| Body bytes exceed `schemastore.MaxFileSize` (1 MiB) at multipart-parse | HTTP 400, body `"too large"` | Go stdlib `http.Request.ParseMultipartForm` via `MaxBytesReader` |
| Body bytes exceed cap after stream-read | HTTP 413, body `"too large"` | `schemastore.MaxFileSize` post-read check |
| Source has a syntax error | HTTP 400, body `<file>:<line>:<col>: <msg>` | `protocompile.CompileError` |
| Source imports a path the WKT bundle doesn't supply | HTTP 400, body names the missing import | `protocompile.CompileError` |
| Compiled `FileDescriptorSet` exceeds size cap (pathological deep-nest case) | HTTP 413, body names compiled size + cap + source size | post-compile size check in handler |
| Compiled bytes round-trip through `proto.Unmarshal` and fail | HTTP 500, internal error | `protocompile.CompileSingleProto` sanity check |

The vendor-actionable cases (syntax / missing import / oversize) all carry the diagnostic into the body so the vendor sees the same shape they'd see from `protoc` locally — no retry, vendor edits + resubmits. Regression coverage in [`internal/protocompile/protocompile_test.go`](../../ops/goat-zenoh-portal/internal/protocompile/protocompile_test.go) (compile-level: valid + syntax error + unsupported import + multi-WKT import-walker + empty body) and [`internal/api/schemas_test.go`](../../ops/goat-zenoh-portal/internal/api/schemas_test.go) (HTTP-level: happy-path + syntax-error + unsupported-import + oversize-input).

---

## Long-term evolution

These v1 expedients work for the trial fabric. When the goat-net portal lands (post-Block 20 phoenix-native peer discipline, and at the latest as part of the canonical Block 23 schema-enforcer interceptor work), each decision evolves:

| Decision | v1 (trial) | Long-term (goat-net) |
|---|---|---|
| 1. Storage | Portal-local disk | Zenoh fabric at `attest/schema/<vendor>/<system>/.../v<N>` |
| 2. Signing identity | Portal (uploads authenticated, storage portal-owned) | Vendor — vendor's own Zenoh client publishes directly with their per-vendor cert |
| 3. Versioning | Vendor types `v<N>` + compatibility tag | Portal (or a sidecar) compares descriptor sets, auto-suggests version + compatibility |
| 4. Fetch API | Portal proxy at `/api/schemas/...` | Direct: consumers do `zenoh get attest/schema/...`; portal proxy remains as a browseability layer but isn't on the hot path |
| 5. Formats | `.fds` + single-file `.proto` (v1.1 per §5.1, server-side compile) | `.fds` + multi-file `.proto` source + JSON Schema + AsyncAPI + OpenAPI, with per-format validation hooks |

The on-wire namespace (`attest/schema/<topic-pattern>`) and the versioning semantics ([ADR 0506](../adr/0506-fabric-schema-evolution.md)) stay constant across both eras. What changes is *who publishes* and *where the durable copy lives*.

### The transition shape

When the long-term portal lands, the v1 portal-local files migrate to Zenoh by a one-time replay: the portal walks its `schemas/` directory and `zenoh put`s each file to the canonical `attest/schema/<vendor>/<system>/.../v<N>` key. The fetch API stays at the portal but becomes a proxy in front of Zenoh rather than reading from local disk. Vendors switch to publishing directly when their own tooling is ready; the portal upload path stays as a fallback for vendors that don't have publisher tooling yet.

No data is lost; the registry table keeps its `schema_ref` column with the same pointer string; the only thing that changes is what answers when something dereferences the pointer.

---

## Boundaries

**In scope:**

- The three v1 pieces (upload, store, fetch).
- The Topics page UI changes to expose them.
- A new `topic_schemas` table in the portal's SQLite schema.
- Inspector-side wiring to call the fetch API and decode with the descriptor (lands in the same change-set or a near-follow-on, but inside the existing inspector codebase).

**Out of scope (stays in Block 23 proper):**

- Runtime rejection of publishes to topics without a registered schema. That's the schema-enforcer interceptor — a Zenoh-side plugin sitting on the publish path. v1 here is registration + publication, not enforcement.
- Strict content validation (parsing actual payloads against the registered schema). The interceptor's `validate: strict` mode handles that later.
- A renderer that walks all schemas and produces a Swagger-like browse site. The Topics page is enough surface for the trial.

**Explicit non-goal:**

- Cross-vendor schema namespace governance. Two vendors uploading conflicting schemas under colliding keys is impossible by construction — the per-vendor URI-SAN scope already prevents one vendor writing under another's prefix.

---

## Topic lifecycle — retire vs delete (added 2026-05-26 per [ADR 0864](../adr/0864-portal-topic-retire-vs-delete.md))

`DELETE /api/vendors/{v}/topics/{id}` carries two semantics on one endpoint, selected by a single query parameter. The default is the audit-preserving move that has shipped since v1; the new opt-in surface is for vendors who need to free a name for reuse.

| Surface | DB effect | On-disk schemas | UNIQUE(vendor,key_pattern) slot | Audit row written |
|---|---|---|---|---|
| `DELETE .../topics/{id}` (default — **retire**) | `UPDATE topics SET retired_at = now` | preserved | **stays occupied** — re-create returns `ErrTopicExists` | `action: topic-retire` |
| `DELETE .../topics/{id}?purge=true` (**delete**) | `DELETE FROM topics WHERE id=?` (FK CASCADE wipes `topic_schemas` + `topic_code_examples`) | `<SchemaDir>/<topic_id>/` best-effort removed | **freed** — same `key_pattern` can be re-registered as a fresh row | `action: topic-delete` |

The Topics page renders both as sibling buttons per row with distinct `confirm()` copy:

- `retire` is the soft-delete tombstone — the row remains in the registry for audit; the API hides retired rows from the default list. Use when "we stopped using this topic but the audit trail should reflect that it existed."
- `delete` is the hard-delete — row gone, descriptor files gone, name available again. Use when "we minted this with a typo / wrong type / wrong cardinality and want to mint it again under the same name."

The audit_events row is written **before** the row deletion so the action is reconstructible from `audit_events` after the topic row is gone. The schema-on-disk cleanup is best-effort; orphan directories are harmless (next topic gets a fresh autoincrement id), but the call keeps the schema directory tidy.

`DELETE /api/vendors/{prefix}` (vendor-level cascade) already hard-removes retired topic rows via FK CASCADE — purge is the per-topic analogue with the same shape. The vendor-delete pre-check still refuses if any *active* topics remain.

This is a trial-fabric expedient. The long-term goat-net portal under Block 84 will recapitulate the same two-verb shape under the v2 Policy Engine (Block 17A.5) governance.

---

## Write-time format ↔ FDS consistency gate (added 2026-05-27)

`POST /api/vendors/{v}/topics/{id}/schemas` (schema upload) and `PATCH /api/vendors/{v}/topics/{id}` (format edit) both decode the relevant `.fds` body and refuse the write with `HTTP 422 format_mismatch` when the topic's declared `format: protobuf:<Name>` references a message that's not a top-level entry in the descriptor. Mirrors the read-side `schema-body` check at `crates/stethoscope-checks/src/checks/schema_body.rs` so the two sides agree byte-for-byte. The error body names the messages actually present in the `.fds` and suggests the most likely correction (PATCH format to the first present message, or rebuild the `.fds` with the declared message hoisted to top-level — nested messages don't match by convention).

Before the gate, the portal accepted any `(format, fds)` pair at write-time; the inconsistency only surfaced when stethoscope or a decoded-view subscriber tried to render. PATCH'ing format to a name not in an existing `.fds` is the same class of bug and is gated at the same point.

**The decoder joined the agreement on 2026-08-17.** "The two sides agree byte-for-byte" was true of the write gate and stethoscope but not of the server-side decoder, which resolved nested messages by both bare name and fully-qualified name. It predated the gate — the decode path landed 2026-05-17, the gate ten days later — so it was never a decision to allow nested, just a side left behind. It now refuses, naming the parent and repeating stethoscope's remediation (hoist it, or declare the parent).

Residual, stated because it is unverified rather than absent: a topic registered *before* 2026-05-27 could carry a nested format that decoded until now and errors after. Anything registered since cannot — the gate refuses it. No such topic appears in repo-declared state, and none was observed live, but a full live sweep of both goatnets was not run. The failure mode if one exists is a named error and an `UNDECODABLE` grade from the decode-health check, not silence.

`createTopic` (`POST /api/vendors/{v}/topics`) stays ungated for format: the topic can be registered before any `.fds` is uploaded, and the gate fires at upload-time. PATCH'ing format on a topic with no uploaded schema is also accepted — the rule is "if both sides are present, they must agree," not "format must always cite a known message."

---

## Open questions remaining

None that block v1. The decisions above are the gate to in-flight per the in-flight entry. Implementation can begin.

Two questions deferred to the long-term phase:

1. **Compatibility analysis algorithm.** When the portal auto-detects compatibility (v1 ships with vendor self-declaration), what's the comparison logic? Buf's `breaking` rule set is the obvious starting point for protobuf; less obvious for JSON Schema.
2. **Schema deprecation + retention.** Old `v<N>` versions accumulate. v1 keeps them all forever. The long-term portal needs a deprecation marker + retention policy.

Both are real, neither blocks the v1 shape — surfacing them here so they're not lost when long-term work picks up.
