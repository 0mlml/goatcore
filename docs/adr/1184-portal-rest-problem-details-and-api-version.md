# ADR 1184 — The portal's REST errors are problem details, every response names its contract version, and reads revalidate

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-09-18 |
| Repo scope | goat core (`ops/goat-zenoh-portal`, `ops/ansible/roles/goat-zenoh-portal`) |
| Supersedes | — |
| Superseded by | — |
| Related | ADR 1052 (vendor to org rename, the deprecation pattern this follows), ADR 1133 (the stream router split the 429 page must not touch), F-371 (the portal's per-source limiters), F-207 (request id in the access log); `ops/goat-zenoh-portal/web/static/openapi.yaml` |

## TL;DR (for a layperson)

When the portal refuses a request it now answers in the standard shape the rest of the web uses for errors (RFC 9457), so a partner's tooling and a person reading a screen see the same fields, including a request id they can quote to us. Every answer says which version of the API it comes from, a browser can ask "has this changed?" instead of downloading a list again, and a request body that is too big is refused outright instead of being quietly cut. The old error shape is still inside every error, so nothing a partner built stops working. The alternative was to leave the portal's API document scoring below the bar we hold every vendor's document to.

## Context

The zagros portal serves `openapi.yaml`. Graded with the example-registry grader, the rubric every vendor's API document is scored against, it read 2.96 of 5. Half of the gap was the document (fixed in the preceding change: examples, operation ids, real response bodies, served as YAML). The other half was the wire: errors were `{"error":{"code","message"}}` with a plain-text 404 and 405 from the Go standard library, no response named an API version, no read carried a validator, and `readJSON` had no body cap. The bulk import capped its body with `io.LimitReader`, which truncates silently; a truncated file still parses, and in `?mode=replace` every topic missing from it is retired.

The document must not promise what the server does not do. So the score is earned in code, and the code has to stay at least as safe as it was. A security review of the plan (2026-09-17) set three constraints that shaped the decision below: the 429 page is never served by the portal; responses that carry a credential, a presigned URL or a person's details are never stored by a cache; and a 5xx never carries the fault's own text.

## Decision

- **Errors.** `writeError` keeps its signature and writes an RFC 9457 body, `Content-Type: application/problem+json; charset=utf-8`: `type` (`urn:goat:portal:problem:<code>`), `title` (the reason phrase), `status`, `detail`, `instance` (the request id, the same value as `X-Request-Id`), `code`, and the legacy `error: {code, message}` object, which the document marks deprecated. The standard library's 404 and 405 under `/api` are rewritten to the same shape by the access-log wrapper (`statusWriter`), which also implements `Flush` and `Unwrap` so the event stream is untouched. `detail` is at most 1024 runes.
- **5xx detail is fixed per code.** `serverFaultDetail` maps `db`, `store`, `zenoh`, `config` and `panic` to one sentence each; the message the handler passed is logged against the request id and never sent. The health 503 is `HealthProblem`: the problem members plus the health members, `reason` and `detail` both the constant `registry probe failed`, `health: unhealthy` beside the HTTP `status`.
- **Version.** `api.APIVersion` (`0.2.0`) is echoed as `Api-Version` on every `/api` response and is the binary's `version`. A request whose `Api-Version` names another major version is refused with `400 unsupported_api_version`; the refusal names the served version and never echoes the supplied value. The served document's `info.version` must equal it (`openapi_contract_test.go`).
- **Cache classes, chosen at route registration.** `revalidated()`: the documented reads carry `Cache-Control: private, no-cache`, `Vary: Authorization, Accept-Encoding` and a strong `ETag`; a matching `If-None-Match` is answered `304` (`conditionalMiddleware`, inside gzip; the gzip layer derives a `-gz` tag, RFC 9110 §8.8.1). The layer holds at most 1 MiB of a body to tag it; a larger body streams from that point without a validator, so a wide read costs the memory it did before. The schema download uses its stored digest as the tag through `http.ServeContent`. `noStore()`: `/api/whoami`, `/api/access/peer|peers|roster|data-roles|audience|recipient-cert`, `/api/audit/presign`, `/api/admin/snapshot*`, `/api/admin/vendor-activity*`, `/api/orgs/sponsorship`, `/api/lookup/person-presence`, the bundle routes, and every route whose response carries `api_key` or `mesh_setup_key` send `Cache-Control: no-store` and no validator. Every other route is unchanged.
- **Body caps.** Every JSON body, documented or not, reads through `http.MaxBytesReader` at 1 MiB (`readJSON`, `decodeJSON`) and a body past it is `413 payload_too_large`; the import and the peers batch use the same reader instead of `io.LimitReader`. `middleware()` puts a 2 MiB backstop on every `/api` request body, above the largest cap a route sets for itself, so a route that forgets its own cap can no longer read an unbounded body. `display_name` 200, `key_pattern` 512, `format` 32, `schema_ref` 512, `description` 2000 runes; an import holds at most 1000 topics and each is checked against the same caps. An empty topic PATCH is refused. The same numbers are the `maxLength` and `maxItems` values in the document.
- **Store faults are never client errors.** A registry write that fails validation answers to `registry.ErrInvalid` and is a `400 validate` that quotes the message; any other failure of the same call is a `500 db` whose detail is the fixed sentence, so a driver's message (constraint names, table names) never reaches a caller.
- **The 429 page.** A Traefik `errors` middleware on the portal's request router, listed after the mesh gate and before the two limiters, fetches `/429.json` from `goat-problem-pages`: a second container of the same image running `goat-zenoh-portal problem-pages`, no registry, no volumes, read-only root, CPU and memory capped. It is never the portal: an errors middleware fetches its page from the service directly, outside the router's limiters, so a page served by the portal would hand every shed request straight back to the backend. The fabric-wide entrypoint ceiling's 429 stays plain text and the document says so. The middleware also wraps the portal, so the portal must never produce a 429 of its own; the contract test holds that invariant over the route table.
- **Hygiene.** `X-Content-Type-Options: nosniff` on every response. The request id is 96 random bits under `rq-`, not boot time and a counter, since it now goes to every caller in a body. A schema download carries `Content-Disposition: attachment`. Every 4xx and 5xx is access-logged at Info, a success at Debug, so the `instance` a caller quotes meets a line in a production log; the `Api-Version` refusal is logged like any other. The portal UI's fetch wrapper (`app.js`) parses any `json` media type, so it reads the new bodies.

## Alternatives considered

- **Teach the grader the nested envelope.** Rejected: the grader is the bar every vendor meets; changing it for the portal's convenience moves the bar, and the flat shape is what a partner's HTTP client library already understands.
- **Replace the envelope instead of nesting it.** Rejected: six portal JS files, two upload scripts, the id-console mirror and the panoscope-core client read `error.code`. Additive now, removal in its own change after the deprecation window, the ADR 1052 pattern.
- **Serve the 429 page from a portal route.** Rejected on the security review: it defeats `goat-ratelimit` and `goat-inflight` as protection of the backend, and the caller's `Authorization` header is replayed to the page route.
- **No 429 page at all.** Kept as the fallback (`portal_problem_pages_enabled: false`): the document then reads 4.91 with R-10 and C-14 short of 5.
- **`Cache-Control: no-cache` on every GET.** Rejected: it writes presigned S3 URLs and PII into browser disk caches. Hence the two classes and an unchanged default.
- **Path versioning (`/api/v1/...`).** Rejected: every client and every bundle link would move for a change that carries no incompatibility; a header names the contract without renaming the routes.

## Consequences

**Positive.** One error shape on the wire, with a request id in the body. The portal's own document meets the bar vendors are held to, and the contract test plus the CI validity job keep it there. Fault text stays in the log. A truncated import can no longer retire topics. Browsers stop re-downloading unchanged lists.

**Negative.** Every error body grows by the duplicated legacy object until it is removed. `Vary: Authorization` makes shared caches useless for API reads, which is the intent. The problem-pages sidecar is one more container on every host that runs the portal, and the `errors` middleware is the first in `ops/ansible`; its header pass-through is verified on `zagros_dev` before `zagros`. A partner that pinned `Api-Version: 0.1.x` keeps working (same major); one that sends `1.x` is refused, which is new.

**Owed.** The legacy `error` object is removed in a later ADR, after the clients above have moved, no sooner than 2027-03. The remaining ~104 undocumented routes get the same document treatment in their own track; the middleware already applies to them.

## Assessor view (SCA-R)

Error handling now discloses less: 5xx bodies carry a fixed sentence and the fault text is confined to the log (SI-11, Error Handling). Request bodies and fields are bounded (SI-10, Information Input Validation), closing an unbounded-read path and a silent-truncation path that could retire records. Cache directives keep credentials, presigned URLs and PII out of client caches (SC-28 at the client edge; AC-3). The 429 page design preserves the rate and concurrency limiters as denial-of-service protection (SC-5). No new trust boundary is introduced; the sidecar holds no state and no credential. Residual risk: a cache that ignores `private, no-cache` could retain a documented read, which carries no credential; compensating control is the `no-store` class on everything that does.

## Trigger conditions for revisit

- A client is found to depend on the exact `application/json` type of an error response, or on `status` being a string on the health 503.
- The legacy `error` object's removal date arrives, or every known client has moved before it.
- The grader's rubric changes what R-10, R-11 or R-12 read.
- A second surface needs the errors middleware, at which point the sidecar becomes a shared fabric service rather than a portal-role container.
