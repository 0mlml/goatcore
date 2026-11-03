# goat-zenoh-portal — Vendor Activity admin page

**Added 2026-05-22.** Track `vendor-tag-traits-and-portal-roster`. Composes with `operator-onboard-utilities.md §83C.4` (vendor.short_tag manifest field + Ory traits.vendor) and `id-admin-console.md` §"Vendor affiliation surface" (the IdP-side view of the same data).

## Why this exists

The portal has three sources of "who did what" data that no surface previously joined:

1. **Vendor registry** ([`vendors` table](../../ops/goat-zenoh-portal/internal/registry/schema.sql)) — one row per vendor (Zenoh registration), carrying prefix + display_name + synthetic flag + approval timestamp.
2. **Per-vendor mutation counters** — `topics` / `topic_schemas` / `topic_code_examples` rows linked by `vendor_prefix` / `topic_id`. The portal already counts these for individual UI affordances (topic list, schema viewer) but never aggregated them.
3. **Operator audit trail** (`audit_events` table) — every mutation the portal mediates records actor + action + target.

When an operator asks "which vendor created this topic" or "what activity has Acme done this week", the answer required three SQL queries + a manual cross-reference. The Vendor Activity page is the single screen that answers those questions.

In parallel, the orchestrator-side change in 83C.4 populates the site-Kratos identity's `traits.vendor.short_tag` from the manifest. The portal can now JOIN vendor.prefix → identity.traits.vendor.short_tag via a live Kratos admin lookup, surfacing **the humans associated with each vendor** alongside the activity counters.

## What it shows

One row per registered vendor in the portal registry. Per row:

| Column | Source | Notes |
|---|---|---|
| Vendor | `vendors.display_name` + `prefix` | `synth` badge when `synthetic = 1` |
| Identities | Kratos `/admin/identities` filtered by `traits.vendor.short_tag == prefix` | List of email + name + state; gap-noted inline if Kratos unreachable or `--kratos-admin-url` unset |
| Topics | `COUNT(topics WHERE vendor_prefix=X)` split active vs retired | |
| Schemas | `COUNT(topic_schemas JOIN topics ON vendor_prefix=X)` | All-time |
| Code examples | `COUNT(topic_code_examples JOIN topics ON vendor_prefix=X)` | Vendor-supplied per-language code overrides |
| Audit (24h / total) | `audit_events WHERE actor=X OR target=X` | 24h cutoff + total + per-row "recent" link to drill into the latest 50 events |
| Last activity | `MAX(audit_events.occurred_at WHERE actor=X OR target=X)` | "Nm/h/d ago" |

The drill-down per-vendor view (`GET /api/admin/vendor-activity/{vendor}/events`) shows the most recent 50 audit-event rows for the named vendor, with actor / action / target / details columns. The same 50-row cap applies fleet-wide for v1.

## What it deliberately does NOT show — and why

**Wire-level publish/subscribe counts.** The audit-subscriber writes per-message NDJSON to MinIO `audit-integrity` with `{vendor, key_expr, ts, ...}` fields. Surfacing those as per-vendor counters here would require either (a) a heavy MinIO scan at page-render time (NDJSON across N day-partitioned objects), or (b) a separate index the portal builds incrementally. Both are real engineering, neither is necessary for v1's "is this vendor active" question — the portal-mediated audit log already says yes/no. **panoscope** is the live wire-level view today.

The page renders a yellow gap-note inline making this explicit so the operator isn't misled into thinking zero portal-audit-events means zero traffic.

**Per-individual action attribution.** Portal mutations today are gated by a single shared operator API key — the portal does not know which human pressed the button. Audit events record `actor: "operator"` (master-key calls) or `actor: "<vendor-prefix>"` (vendor-key calls), not the individual person. Surfacing "Alice from Acme created this topic" vs "Bob from Acme" requires operator-side OIDC against the portal admin API, which would change the auth model materially. Queued as a follow-on; the page notes the gap.

**In-place vendor-affiliation editing.** Read-only by design. Vendor affiliation set at orchestrator-mint time stays in lockstep across NetBird peer label + Zenoh URI-SAN + Kratos traits; in-place edits in the portal would diverge from those other surfaces. The right path is delete-and-re-mint via the orchestrator. Same rationale as the id-admin-console design doc §"Vendor affiliation surface."

## Architecture

```
                        ┌──────────────────┐
operator browser ─────▶ │ /admin/vendor-   │
                        │ activity.html    │
                        └────────┬─────────┘
                                 │ GET /api/admin/vendor-activity (Bearer op-key)
                                 ▼
                        ┌──────────────────────────────┐
                        │ portal handler               │
                        │ (operator-gated)             │
                        └────┬───────────────┬─────────┘
                             │               │
              SELECT v.* +   │               │ GET /admin/identities
              aggregate      │               ▼
              counts         │      ┌────────────────┐
                             ▼      │ Kratos admin   │
                       ┌─────────┐  │ (mesh-only)    │
                       │ portal  │  └────────┬───────┘
                       │ SQLite  │           │
                       └─────────┘           ▼
                                    filter by traits.vendor.short_tag
                                    bucket by prefix → identities[]
```

The Kratos lookup is **best-effort**: when the portal's `KratosAdminURL` is unset or the call fails, the page renders with empty identity columns + an inline yellow gap note. The aggregate counts come from the portal's own DB and always render.

## Endpoints (v1)

- `GET /api/admin/vendor-activity` — main aggregate. Operator-only. Returns one row per vendor with counts + identities + two gap-notes (identity-roster gap when Kratos unreachable; publish gap always-present).
- `GET /api/admin/vendor-activity/{vendor}/events` — most recent 50 audit events for a vendor (as actor OR target). Operator-only.

Both routes use the same `requireOperator` gate as the existing `/api/admin/snapshot` endpoint — Bearer token equality against `--operator-api-key` (no OIDC; that's the bigger lift queued for the per-individual attribution follow-on).

## Configuration

One new portal binary flag:

- `--kratos-admin-url` (env `PORTAL_KRATOS_ADMIN_URL`) — site-Kratos admin endpoint, e.g. `http://kratos:4434`. Optional; empty disables the identity-roster column. Kratos admin port has no bearer auth — network segmentation is the gate (per id-admin-console.md §Architecture), and the portal lives on the same docker network on nova-a.

No new tables; no new MinIO bucket; no new background job. All queries hit the existing portal SQLite + a live HTTP call to Kratos.

## Performance posture (v1)

- `ListVendorActivity` runs five correlated subqueries per vendor row. At ~5-15 vendors the query latency budget is well under 100ms in practice.
- Kratos `/admin/identities` is paginated client-side (250 per page, safety cap 5000). At ~30-100 identities, one or two pages.
- No caching layer — every page render does both calls. If the trial grows past a few hundred identities + a hundred vendors, a 30-second in-memory cache on the response is the obvious first knob.

## Long-term evolution

Three follow-ons gated on real demand:

1. **Per-individual action attribution.** Bolt OIDC onto the portal admin API (oauth2-proxy in front, same shape as id-admin-console). Add `actor_identity` columns to `topics` + `vendors`. Lights up "Alice (alice@acme.example) created this topic" rows.
2. **Wire-level publish counts.** Subscribe the portal to the audit-subscriber's MinIO output (or directly to a system-wide Zenoh audit topic), build a per-vendor message-count index keyed by day. Surfaces "Acme published 12,433 messages in the last 24h" inline.
3. **Vendor-affiliation editing (with audit).** Step-up + audit-emit pattern from id-admin-console. Synchronizes with the orchestrator-driven path so vendor.short_tag remains the single source of truth.

The first two are durable additions; the third is a behavior change that needs careful sequencing with the orchestrator. None block v1.
