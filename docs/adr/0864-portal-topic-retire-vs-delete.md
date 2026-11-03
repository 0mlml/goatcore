# ADR 0864 — portal topic retire vs delete

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-26 |
| Repo scope | goat core (goat-zenoh-portal) |
| Supersedes | — |
| Superseded by | — |
| Related | ADR 0844 (Zenoh deployment topology + ACL v1), ADR 0856 (partner-peering capability mint), ADR 0413 (no ORM, raw SQL), ADR 0859 (easy rollback), Block 85 (portal-driven schema publishing) |

## TL;DR (for a layperson)

The trial portal lets vendors register topic names on the data fabric and upload the data definitions ("schemas") that go with them. From day one, the "delete topic" button has been a *retire* — it marks the row as gone but keeps it in the database so the audit trail shows it once existed. That's the right default for production. For an actively-iterating vendor on a trial fabric, it's also a failure mode: a vendor "deleted" topics they minted with typos, the API said success, but the name was still reserved and they couldn't mint a corrected version under the same name. They were forced to keep changing the name with each attempt. We are keeping retire as the default (audit matters) and adding a clear, opt-in *delete* (real removal) on the same endpoint via a query parameter, with a separate UI button and a distinct confirm dialog. Now there is one button labelled "retire" for "I'm done with this topic, preserve history" and one labelled "delete" for "I made a mistake, free the name so I can use it again." Both are vendor-self-serve; no operator ceremony needed.

## Context

The portal's topic CRUD endpoint pair shipped in Block 85:

- `POST /api/vendors/{v}/topics` — create a topic row keyed by `(vendor_prefix, key_pattern)`. UNIQUE constraint at the DB layer ([`registry/schema.sql:78`](../../ops/goat-zenoh-portal/internal/registry/schema.sql)).
- `DELETE /api/vendors/{v}/topics/{id}` — wired to a handler named `retireTopic` in [`internal/api/handlers.go`](../../ops/goat-zenoh-portal/internal/api/handlers.go). The handler issues `UPDATE topics SET retired_at = now`; the row stays, the UNIQUE slot stays occupied, the API returns `204 No Content`.

The default-list filter (`WHERE retired_at IS NULL`) hides retired rows, so from the vendor's vantage the topic is "gone." But the next `POST` attempt with the same `key_pattern` returns `ErrTopicExists` because the UNIQUE constraint still holds.

A vendor iterating on its first taxonomy hits this quickly: each retired typo stays reserved, so every retry needs a new name and new code, and retired rows accumulate far faster than active ones. There is no vendor-facing path to free a name once retired — the only existing escape is operator-only `DELETE /api/vendors/{prefix}` (whole-vendor cascade per F-146 follow-on + [the consolidate-legacy-vendors.sh script](https://github.com/usareur-af/goatstuff/blob/main/ops/zenoh-trial-peers/consolidate-legacy-vendors.sh)), which nukes everything the vendor owns including active rows.

The retire-only posture is defensible in production where audit-preservation is load-bearing. It is not defensible on a trial fabric where:

- Vendors are actively shaping their first topic taxonomy and minting wrong names is the norm, not the exception.
- The audit trail is not a compliance artifact — `audit_events` is the source of record, and the topic-row tombstone duplicates that signal in a place that creates a forward footgun.
- No downstream consumer depends on retired-row presence to render history (the UI default-hides them; `audit_events` is what feeds the activity admin).
- Operator support load is real — every "purge these for me please" message costs both sides time and obscures the genuine vendor errors we want to surface.

We considered the cleaner-REST move of flipping `DELETE` to hard-delete and adding `POST .../retire` for the soft-delete. Rejected: the existing Topics page button is labelled "retire" and calls `DELETE`. Existing vendor scripts use `DELETE`. Any silent semantic change turns "I retired this for audit" into "I purged this for reuse" — a regression in the opposite direction from the one we want to fix.

## Decision

`DELETE /api/vendors/{vendor}/topics/{id}` keeps its default semantics. We add one query parameter:

- `DELETE /api/vendors/{vendor}/topics/{id}` — retire (default; unchanged). `retired_at = now`; row stays; slot stays occupied; audit `topic-retire`.
- `DELETE /api/vendors/{vendor}/topics/{id}?purge=true` — delete (new). `DELETE FROM topics WHERE id=?`; FK CASCADE wipes `topic_schemas` + `topic_code_examples`; on-disk schema directory under `<SchemaDir>/<topic_id>/` best-effort removed; slot freed; audit `topic-delete`.

Both verbs are vendor-self-serve under the same `scopedHandler` middleware that already gates the retire path. The audit event is written **before** the row delete so the action is reconstructible from `audit_events` after the topic row is gone.

On the UI, the topics page renders both as sibling buttons per row with distinct `confirm()` copy:

- `retire` — "soft-delete: tombstone the row for audit. The key_pattern stays reserved; re-registering the same name is refused."
- `delete` — "hard-delete: row is gone, schema files removed, the key_pattern is free for reuse. Permanent. No undo."

The registry layer gets a new sibling to `RetireTopic`:

```go
func (r *Registry) DeleteTopic(ctx context.Context, id int64) error {
    // DELETE FROM topics WHERE id = ?; FK CASCADE handles topic_schemas + topic_code_examples.
    // Returns ErrNotFound if the row is already gone.
}
```

`schemastore.Store` gets `RemoveTopicDir(topicID int64) error` for the on-disk cleanup.

## Consequences

**Good.**

- A vendor's iteration loop is unblocked without operator intervention. Same pattern works for every vendor.
- The intent-vs-mechanism gap goes away — "retire" and "delete" are two different buttons with two different verbs and two different confirm dialogs.
- Audit trail is *more* reliable than before. The old retire-via-retired_at path wrote a `topic-retire` audit row; the new purge path writes a `topic-delete` audit row before destroying the topics row. `audit_events` is now the source of record for both actions, instead of being one of two sources that could disagree.
- No breaking change. Existing bare `DELETE` calls (from the topics-page UI, from vendor scripts, from the bulk-import retire flow at [`bulk.go:232`](../../ops/goat-zenoh-portal/internal/api/bulk.go)) keep retire semantics verbatim. The `?purge=true` surface is purely additive.
- Symmetric with vendor-level cascade: `DELETE /api/vendors/{prefix}` already hard-removes retired topic rows via FK CASCADE; per-topic purge is the same shape at row scope.

**Cost.**

- One extra confirm step for the destructive path. Acceptable — the default still preserves audit; the purge button is for an explicit "I want the name back" intent.
- Schema-file cleanup is best-effort. Failure leaves an orphan directory under `<SchemaDir>/<topic_id>/` that no future topic id will reuse (autoincrement), so it's a dead-weight footprint that ops can sweep periodically. Not load-bearing.
- One more code path to keep an eye on in tests + reviews. Covered by `TestTopicDeleteVsRetire` ([`internal/api/topic_delete_test.go`](../../ops/goat-zenoh-portal/internal/api/topic_delete_test.go)) and extended `TestTopicCRUD` ([`internal/registry/registry_test.go`](../../ops/goat-zenoh-portal/internal/registry/registry_test.go)).

**Trial vs production posture.**

This is a trial-fabric expedient. The shape lifts cleanly into the long-term goat-net portal (Block 84 partner-peering portal) — same two verbs, same audit-trail discipline. When the v2 Policy Engine (Block 17A.5) governs the surface, the purge verb gains an additional approval-ladder gate per [ADR 0856](0856-partner-peering-capability-mint.md) (capability mint requires explicit operator approval; capability burn — i.e. purge — should likely too). For trial, vendor-self-serve is correct: vendors are minting their own taxonomy and they should be able to fix their own typos.

## Alternatives

- **Flip DELETE to hard-delete; add POST .../retire for soft-delete.** Rejected for the regression-in-opposite-direction reason above. Cleaner REST but worse forward compatibility with the existing vendor-script + UI surface.
- **Keep retire-only; expose a vendor-callable `POST /api/vendors/{v}/topics/{id}/purge`.** Equivalent functionally; rejected because two separate paths for "remove this topic" creates more surface for misuse than one path with one well-named flag. The `?purge=true` query param sits right next to `DELETE` semantically.
- **Keep the status quo; add a one-shot operator script to bulk-purge retired rows.** Rejected — punts the problem to operator ceremony, which does not scale with actively-iterating vendors.
- **Mark the row as "purged" in a third state and physically delete on a sweep.** Rejected — adds state without buying anything; the audit_events row is already the durable record.

