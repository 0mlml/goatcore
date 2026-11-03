# ADR 1052 — Vendor → org: rename the machine surfaces, back-compat-first

| | |
|---|---|
| Status | Proposed (2026-06-20). Designed-not-built; sequenced migration. |
| Related | `vendor-to-org-machine-rename.md` (the plan), [`portal-lexicon.md`](../design/portal-lexicon.md) (display layer + the "separate breaking-change track" this resolves), ADR 1043 (the `affiliation.vendor` taxonomy enum — *not* renamed), [ADR 1044](1044-credential-rotation-in-place-reissue.md) (legacy-cert re-mint path), [ADR 1039](1039-slot-alias-display-layer.md) / [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (the anchor model the prefix names). |

## Plain-language summary

"Vendor" is early-sandbox wording. We already render "org" everywhere a human reads (the display lexicon), and just fixed the user-facing error strings that bypassed it. But "vendor" is still the *machine* name in places that travel on the wire and to disk: the `/api/vendors` HTTP routes, ~13 JSON field keys, the bulk export/import YAML `vendor:` field, a schema-source enum value, and identifiers across the portal and every client. This ADR commits to renaming those to "org" — but as a **coordinated, back-compat-first migration**, because they are a live contract with integration-pinned clients and on-disk exports. We add the `org` surface alongside `vendor`, let clients move at their own pace, flip defaults, and remove `vendor` only after telemetry shows nothing uses it.

## Context

The portal exposes a vendor-keyed API and on-disk format consumed by `goat-cli`, panoscope, and flashlight, and by exported files. The display vocabulary is already org-agnostic via the lexicon, and the cert URI-SAN is already `org=` on the production goatnet. The remaining "vendor" is purely the machine contract — 106 route references, 13 wire keys, the bulk YAML field, ~1,400 identifier hits across the Rust clients. `portal-lexicon.md` explicitly parked this as "a separate, breaking API/schema change… its own tracked effort." This ADR is that effort.

A blast-rename is rejected: renaming a wire surface in one PR breaks every client that hasn't shipped the matching change at the same instant, and the production goatnet is live.

## Decision

Rename the machine surfaces vendor → org, via the **additive-first, remove-last** sequence in the design doc:

1. **Phase 1 — additive back-compat (non-breaking):** the portal serves `/api/orgs*` aliases, dual-reads `org`/`vendor` request keys, dual-emits both response keys, accepts both enum values, documents `org` (deprecates `vendor`) in openapi, and adds (a) a CI ratchet forbidding *new* `vendor` machine surfaces and (b) a counter of `vendor`-surface use.
2. **Phase 2 — clients adopt `org`** (any order; dual portal keeps un-migrated clients working); internal Go/Rust identifiers rename here (`extract_vendor` → `extract_org`, etc.).
3. **Phase 3 — flip defaults** to emit `org` primary.
4. **Phase 4 — remove `vendor`**, gated on the Phase-1 counter reading zero over a measured window. The wire removal (4a/4b routes+keys) stays counter-gated (blocked by F-233, a client still on `/api/vendors`). The **Ory identity-trait leg (4c: `traits.vendor.{short_tag,display_name}` → `traits.org.{...}`) is split out and NOT counter-gated** — it is a data-at-rest migration decoupled from the wire so a portal rollback never unwinds an Ory write. **Deployed + live on the production goatnet** (schema `org` block + `ory_site_vendor_trait_compat` toggle, both Go readers prefer-org, orchestrator writes `traits.org`, retroactive+reversible backfill `ops/identity/backfill-vendor-to-org-trait.py`): every identity backfilled `traits.vendor`→`traits.org`, org-only schema, settings UI shows "Org affiliation" only. Done retroactively for all identities (the original "forward-only" note is superseded).

**Explicitly not renamed:** the cert URI-SAN (freeze-critical, integration-pinned, already `org=` on the production goatnet); legacy sandbox certs (re-mint = rotation, ADR 1044); and the `affiliation.vendor` *taxonomy enum value* (a kind of affiliation in the identity grammar, a different concept from the participant/org noun).

## Consequences

- **No flag day, no broken clients** — the invariant "never remove a surface a live client still uses" is enforced by the telemetry gate; sandbox and production goatnets both work at every phase.
- **The leak stops recurring** — the additive-only CI ratchet is the durable guard the de-vendor work lacked (which is why "vendor" kept reappearing).
- **Multi-PR, multi-repo, paced** — one PR per phase/client; the wire-flip and removal phases are deploy-coordinated, not rushed.
- **A deprecation window must actually be waited out** — Phase 4 removal cannot precede client adoption; the counter, not a calendar, is the gate.

## Alternatives considered

- **Blast-rename in one PR.** Rejected — breaks integration-pinned live clients + exported files instantly.
- **Display-only (status quo).** Rejected — the user asked for the machine surfaces themselves to stop being vendor-centric; display-only is what we already have.
- **Leave it forever.** Rejected — "vendor" keeps leaking back into new code precisely because the machine name normalizes it; the rename + CI ratchet removes the root.
