# ADR 1121 — `goat flow` resolves per-topic last-publish by merging every reachable observer, labelled

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-08-07 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | ADR 1019, ADR 1032, ADR 1034, ADR 1037, ADR 1038, ADR 1046, ADR 1047 |

## TL;DR (for a layperson)

An operator looking at our data fabric could not answer the two most basic
questions about a data feed: is anything coming through right now, and when
did something last come through? Worse, one screen displayed a counter that
was permanently stuck at zero, which read as "this feed has never carried
anything" when it actually meant "nobody is counting". We added a command,
`goat flow`, that asks every monitoring system we can reach and shows the
answer with a label saying which system said it. The important part is what
it does when nobody can answer: it says "no observer could answer" rather
than "no data", because those are different facts and confusing them is how
an operator concludes a working sensor is dead.

## Context

Nothing in `goat` answered "is data flowing on this topic" or "when did it
last flow" as a first-class field. The closest surface was
`goat replay '<topic>' --since 24h --count 1` — an event dump the operator
had to know to construct, which prints nothing when the window is guessed
too small, making "no output" ambiguous between "no events" and "wrong
window". `goat top` and `goat health` aggregate per traffic-class and
per-producer, never per-topic. `goat presence` deliberately answers "who is
alive", not "what is moving".

Three surfaces were actively misleading:

1. **`goat topic show` printed `publish_count`, always `0`.** The portal
   column (`ops/goat-zenoh-portal/internal/registry/schema.sql`) is created
   `NOT NULL DEFAULT 0` and no `UPDATE` statement anywhere in the portal
   writes it; the schema comment says so ("audit-logger integration is a
   follow-on; today these are 0/NULL"). An operator reads `0` as evidence.

2. **`goat ui` fetched per-topic freshness and discarded it.** panoscope has
   always sent `last_event_ms`; `cmd/ui.rs` parsed it, aggregated it across
   edges, and carried it into the picker's `LiveFacet` — and the detail pane
   rendered rate/pubs/subs only. It had zero render sites.

3. **`goat ui` showed a stale rate for quiet topics.** It summed `ema_eps`,
   which `EdgeState::record_event` only decays *when an event arrives*. A
   topic that published once an hour ago carries a permanently non-zero
   `ema_eps`. panoscope emits `projected_eps` alongside it precisely for
   this ("for read paths so a quiet edge fades over time"); the CLI ignored
   it, so a dead topic rendered as busy.

The three observers that can answer have **disjoint blind spots**, not a
quality ordering — which is the fact the whole design turns on:

| observer | cannot see | uniquely provides |
|---|---|---|
| panoscope | anything older than its configured audit-replay window; anything its token-bucket sampler dropped; and it never states where that window begins | rate, pub/sub counts |
| audit-tail | `volatile/**` (excluded by default, ADR 1032); anything past the portal's 7d presign clamp | durable, hashed, attributable timestamps that survive a panoscope restart |
| live window | anything outside its own window | works with ONLY the router reachable — the denied-edge case |

## Decision

**We separate engagement order from field resolution.** Conflating them is
what produced the misleading surfaces above.

- **Engagement is a cost decision** — which observers we bother to query. It
  is a fallback ladder. Default engages panoscope only; `--since` adds the
  audit tail; the live tier engages under `--live auto` only when every
  other engaged tier came back unreachable. So on the common path — any
  observer answering — `goat flow` opens no Zenoh session at all; the
  session appears only in the denied-edge case the live tier exists for.
- **Resolution is a truth decision** — given N answers, what we print. It is
  a **merge**: `last` is the **maximum across answering tiers, labelled with
  the tier that produced it**. Ties within a 2s skew tolerance break toward
  the more durable evidence, keeping the later timestamp.

Strict fallback on truth is rejected because it silently under-reports a
routine scenario: a topic flowed 2h ago, panoscope's sampler dropped it (or
it predates panoscope's replay window) → panoscope says nothing → the command
prints "no data" while the audit tail holds the row. Understating recency is the dangerous
direction; max-with-provenance cannot regress.

**Different fields resolve by different rules, deliberately.** For `last`,
durability wins ties. For `eps`, panoscope's real EMA beats a 3s live sample
count. Because the correct ordering differs per field, `Source::evidence_rank`
is a private method used only by the `last` resolution — **not** a blanket
`Ord`/`PartialOrd` impl, which would invite exactly the bug this ADR exists
to prevent.

**The honesty vocabulary is four states**, and the `Unknown`/`NoRecord`
split is the load-bearing part — the same distinction `goat health` draws
with "idle = UNCONFIRMED, not healthy" (ADR 1034):

- `flowing` — a tier has a last-publish within the freshness window.
- `idle` — a tier has a last-publish, older than the window.
- `no-record` — a tier that **can prove absence** looked over a **stated
  horizon** and found nothing. Rendered `none in 24h`.
- `unknown` — nothing could answer. **Not "no data" — "no observer".**

`Source::can_prove_absence()` is true only for the audit tail and the portal
registry: only they can look backwards over a **stated** horizon. panoscope
cannot — note this is *not* because its memory starts at boot (`minio_replay`
re-applies archived audit events at their original timestamps, so
`last_event_ms` survives a restart), but because it never puts the extent of
that memory on the wire, so its silence bounds nothing. A live window cannot
(silence is not absence). A tier in `Partial` state — a truncated audit
window — **forfeits** the ability for that run. Absence also requires
per-key coverage: the audit tail is `Ok` across its window while structurally
blind to `volatile/**`, so `covers(key)` gates the verdict alongside
`can_prove_absence()`.

Consequences that follow mechanically, each covered by a unit test:
default `goat flow` reads a silent topic as `unknown`, never `no-record`,
and the footer names `--since 24h` as the way to actually check; an
unreachable panoscope yields `unknown`, never `idle`; a truncated audit
window can never produce a `no-record` row. **The command never reports a
topic as having never published** — an absence verdict always names the
horizon it was checked over.

**Clock discipline.** Ages compute against the snapshot's `generated_at_ms`
— panoscope's *server* clock — not the operator's, whose NTP drift would
skew every age on the frame. This follows ADR 1037: we do not assume UTC
confidence. `last_event_ms == 0` maps to `None` at the parse boundary
(provably: `EdgeState::new` sets it to `now_ms` and is only reached inside
`record_event`, so a real observed edge always has `> 0`; zero means the row
was seeded from a node's publish list).

**`--watch` is a plain repaint loop, not a ratatui screen.** See
Alternatives.

**`--json` is one object** carrying `rows` plus a `sources` array with an
entry for **every** tier including skipped ones, so a script can distinguish
"the audit tail said no" from "the audit tail was never asked". A row whose
last-publish is unknown **omits the field entirely** — never `0`, never
`null`. Encoding the absence structurally is what stops this from becoming
the next `publish_count: 0`.

(The audit tier's truncation also stops being a bare stderr note and becomes
tier state the resolver acts on.)

**Exit code is `0` even when a tier is unreachable** — `goat flow` reports,
and a down observer is a reported fact (the `goat status` neutral-verdict
precedent). Scripts key on `sources[].state`. The one exception: when
*every* engaged tier is unreachable the command answered nothing at all, and
exits non-zero with the per-tier causes.

**`goat topic show` stops rendering `publish_count`** and prints a real
`last_publish` + `flow_state` resolved through the same code path (panoscope
+ registry only — one extra round-trip, not a fabric sweep). The `--json`
shape is unchanged: `publish_count` still serializes, so no consumer breaks.
`Topic` gains `last_seen_at: Option<i64>` with `skip_serializing_if`, so the
field is absent today and `topic ls --json` stays byte-identical.

## Alternatives considered

**Strict fallback (first observer that answers wins).** Simpler to explain
and cheaper. Rejected: the tiers have disjoint blind spots, so "first
answer" is not "best answer". Concretely, it prints "no data" for a topic
whose audit row exists but which panoscope's sampler dropped, or which
predates panoscope's replay window — routine on a busy fabric.

**panoscope-only.** By far the cheapest: `goat ui` already fetches the
snapshot, so this would have been a render fix. Rejected as the *sole*
source because it can never prove a
topic has not published — the `no-record` verdict, which is most of the
operational value, would be unreachable. panoscope remains the default
*engaged* tier, so we keep the cost profile without the epistemics.

**A new portal endpoint exposing `vendor_topic_activity` per-topic.** The
portal *already* maintains real per-topic `publish_count` + monotonic
`last_seen_at` in that table (`MarkTopicActivity`, fed by the audit tailer)
and aggregates it away to per-org rollups in `/api/progress`. A ~10-line Go
change would give the CLI an authoritative, bounded-horizon, zero-extra-
round-trip tier. Rejected **for this ADR only** on scope: it is a different
repo area with different owners. The client side is built to accept it — the
`portal-registry` tier exists and reports itself `skipped` until the column
is populated. See *Trigger conditions*.

**A full ratatui `--watch`.** Rejected on four grounds. (Consequently `--watch`
is **not** TTY-gated: gating it would forbid the very `| tee` this rejection
cites. `crate::tui::require_tty` is reserved for full-screen surfaces that read
input and would hang a script — `top`, `who`, `ui`, and the two `--tui` lenses.
The sibling line-oriented watchers `health --watch` and `presence --watch` are
ungated for the same reason. Under a pipe the loop appends each frame with a
separator instead of repainting in place.) The producer-task +
mpsc + `spawn_blocking` machinery in `tui/top.rs` exists to decouple a draw
loop from hundreds-per-second Zenoh arrivals; `flow` polls HTTP on a seconds
cadence and has nothing to decouple. One frame is ~N concurrent portal reads
plus a snapshot GET, so the honest cadence is 10–30s, at which an alternate
screen buys nothing. An alternate screen destroys the scrollback an operator
watching a topic go quiet actually wants, and makes the command impossible
to pipe (`goat flow --watch | tee`). And it would pull in the alternate-screen
lifecycle plus `StderrSilencer`, whose real implementation is unix-gated (it
has a no-op `cfg(not(unix))` arm), for no gain on a surface with no keybindings.

**A new `E_NO_OBSERVER` error code.** Rejected: every terminal failure is
already covered (`E_PROFILE_MISSING`, `E_PORTAL_UNREACHABLE`,
`E_TOKEN_INVALID`, `E_ROUTER_UNREACHABLE`), and an unreachable *observer* is
a degradation this command is specifically designed to report rather than
fail on. A single exit code cannot carry the per-tier granularity that
`sources[]` already provides. Purely additive later if a consumer wants it.

**Always engaging the live tier as corroboration.** Rejected as a default: a
session open costs a TLS handshake and a router transport slot, and
corroboration only helps when you already doubt panoscope. Available as
`--live always`.

## Consequences

**Positive.** The two questions an operator actually asks have a direct
answer for the first time. Three misleading surfaces are corrected — a
permanently-zero counter, a dropped freshness field, and a stale rate on
quiet topics — rather than left standing beside a new correct one. The
`unknown`/`no-record` distinction makes "we could not see" structurally
inexpressible as "there was nothing", at both the row level (absent JSON
field) and the frame level (empty stdout on failure). The pure resolver is
unit-testable without HTTP, Zenoh, or a clock, following the
`merge_roster`/`aggregate_live` precedent.

**Negative.** Four observers is more conceptual surface than one, and the
`sources` footer is a permanent tax on frame height — accepted because
removing it would restore exactly the ambiguity the command exists to end.
Default `goat flow` cannot produce `no-record`, which will surprise
operators who expect a bare invocation to be authoritative; mitigated by the
footer nudging toward `--since`, but it is a real papercut. `panoscope`'s
`total_events` is still not surfaced. The audit tier's cost scales with
window size and it prints its own truncation warning path separately from
the `Partial` state we model. We owe the future maintainer the portal-side
follow-on below; until it lands, the `portal-registry` tier is dead weight
that reports itself skipped on every run.

**Residual behavioural risk.** The 2s skew tolerance is a judgement call: two
observers of the same publish will not agree to the millisecond, but a
genuinely-newer publish inside 2s will be attributed to the more durable
observer rather than the faster one. This affects attribution only, never
the timestamp (the max is always kept), so no operator decision turns on it.

## Assessor view (SCA-R)

This is a read-only observability surface; it mints no credentials, changes
no ACL, and publishes nothing to the fabric. It consumes existing
authenticated paths only: the portal Bearer token (AC-3), portal-presigned
MinIO URLs for the audit tail (AU-9 — the operator laptop holds no MinIO
credentials, per ADR 1038), and an mTLS Zenoh session for the optional live
tier. The live tier uses a random ZID rather than the pinned slot identity
(the rationale is documented on `pubsub::open_session_readonly`), so it
earns no publisher provenance and cannot monopolise the
operator's slot.

Maps primarily to **AU-6 (audit review, analysis, and reporting)** and
**SI-4 (system monitoring)**. The load-bearing control property is the
refusal to represent absence-of-observation as absence-of-event: under
800-53 AU-6 an analyst conclusion drawn from a monitoring gap
misrepresented as a negative finding is an integrity defect, and the
`unknown`/`no-record` split plus the mandatory per-tier `sources` array is
the compensating control. No waiver is required. Residual risk: an operator
may still act on a stale `panoscope` reading if that service is wedged
rather than down — partially compensated by `generated_at_ms` being surfaced
as the frame's as-of, so a frozen snapshot clock is visible on the frame.
Consistent with the cATO pathway; no new external attack surface.

## Trigger conditions for revisit

- **The portal stamps `topics.last_seen_at`** (or exposes
  `vendor_topic_activity` per-topic). The `portal-registry` tier then
  becomes an authoritative bounded-horizon observer at zero extra
  round-trip, the audit tier should become opt-in-only rather than the sole
  absence-prover. Note `goat topic show` would still NOT gain `no-record`
  from this alone: the registry tier only holds rows that carry a stamp, so
  an unstamped topic is `unknown` there by construction — `topic show` would
  need to engage the audit tail to render an absence verdict.
- **panoscope exposes the extent of its replay window on the wire** (i.e. the
  effective floor of `minio_replay`, not merely its process start).
  `can_prove_absence()` could then become true for panoscope over that stated
  window, which would let default `goat flow` produce `no-record` without
  `--since` — removing the design's sharpest papercut. A process-start
  timestamp alone is NOT sufficient and would be actively misleading, since
  replay refills `last_event_ms` from before it.
- **The audit-tail retention changes from 7 days**, invalidating the horizon
  text and the clamp assumption.
- **A second command needs the layered resolver** (a health-domain arm, a
  stethoscope check). At that point `resolve_flow` and the tier types should
  move out of `cmd/flow.rs` into `panoscope-core`.
- **`volatile/**` becomes audit-visible**, which would remove a documented
  blind spot and change what a `no-record` verdict means for those keys.
