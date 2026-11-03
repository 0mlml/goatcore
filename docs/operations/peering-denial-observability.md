# Peering denial observability — runbook

> **60-second summary:** When the ACL at a sovereign peering edge refuses a partner, nobody is
> told — the partner's `put` is fire-and-forget so it gets no error, and at the default log level
> our router prints nothing (F-177).
> "The partner is being refused" and "the partner never sent anything" therefore look identical.
> This is the `peering` domain of the [environment-health surface](../design/environment-health-surface.md):
> the edge's own ACL decision log is collected on a timer into monotonic counters and self-published
> as `EnvHealthV1{domain:"peering"}` on `ops/peering/health/<edge>`. **Refused** reads `red` or
> `amber`, **never attempted** reads `idle`, and **cannot see** reads `unmetered` — never green.

| | |
|---|---|
| Status | Built; not deployed. Gate **G-6** of cross-sovereign-federation-plan (stage S4). |
| Components | `ops/peering-denials/pd-snapshot.py` (collector) · `ops/zenoh-trial-peers/peering-health/` (publisher) · `ops/scripts/edge-prefix-denials.sh` (the ad-hoc twin) |
| Contract | [`environment-health-surface.md`](../design/environment-health-surface.md) ([ADR 1036](../adr/1036-environment-health-surface.md)); schema `ops/zenoh-trial-peers/time-health/proto/env_health_v1.proto`, `PeeringDetailV1`, oneof arm 16 |
| Related | F-177 (silent at both ends) · F-448 (the measured denial tally) · [ADR 1022](../adr/1022-zenoh-denial-observability.md) (the fleet-wide expected-vs-actual monitor this complements) · [ADR 1157](../adr/1157-cross-sovereign-federation-edge-terminated.md) |

---

## 1. Why a gate and not a nicety

Continuous monitoring cannot assert an enforcement it cannot see. Until a refusal at the peering
edge is observable, every statement about partner access enforcement is unevidenced — the ACL may
be working perfectly and we have no way to say so, and it may have been misconfigured into
refusing everything and we have no way to notice. That is why no partner carries data under an
authorization before this exists.

The specific defect is that **absence carries no information today**. A `--since` grep over the
edge's log looks the same whether a partner was refused a thousand times or never connected. The
surface below exists to make those two states render differently, and to make the second one — a
partner being refused — carry a count that rises while it is happening.

## 2. Where it appears

| Altitude | Where | What you see |
|---|---|---|
| Bus | `ops/peering/health/<edge>` | `EnvHealthV1{domain:"peering"}`, C0 control priority, one beat per publish interval (default 30 s) |
| CLI | `goat health --domain peering` | the colour, the grounding `n`, and the denial rows |
| Roll-up | `goat-peering-health --mode rollup` | a passive subscriber of `ops/peering/health/**` writing one JSON for a dashboard |

Reveal-only. Each edge self-publishes; the roll-up only listens. **Nothing queries the router on
demand** — client demand must never amplify into fabric load ([ADR 1123](../adr/1123-client-demand-must-not-amplify.md)).

## 3. The five states, and what you do at 03:00

The colour answers one question: *is the peering edge refusing this partner, and can we tell?*

| Colour | What it means | What it is NOT | Do this |
|---|---|---|---|
| **`unmetered`** | We cannot see. Either the ACL interceptor is not at trace on that edge, or the collector's snapshot is stale/missing, or the log carried decision lines the parser could not read and none it could. | Not "no denials". An empty denial list under a blind observer is evidence of nothing. | Read `observer_note` — it names which of the three. Trace off → set `goat_zenoh_edge_acl_trace: true` for that edge and re-deploy it. Stale snapshot → `systemctl status peering-denials-snapshot.timer` on the edge host. Unparsed lines → §7. |
| **`idle`** | The observer is working and there were **no ACL decisions at all** in the window — nothing allowed, nothing denied. Nobody asked this edge for anything. | Not a failure. A partner is allowed to be quiet, and absence is not illness ([ADR 1036](../adr/1036-environment-health-surface.md) §3.4). | Nothing, if the partner is expected to be quiet. If the partner says it is sending: the traffic is **not reaching this edge**. Look at the IPsec SA and the listener, not at the prefix. |
| **`green`** | Decisions observed, none denied. | — | Nothing. |
| **`amber`** | Denials observed **and** allows observed: part of what the partner sends lands and part is refused. The classic prefix/lane mismatch — an admitted identity writing outside its lane. Also amber when some decision lines did not parse, because an incomplete reading must not read clean. | Not "half broken transport". The session is fine; the naming is not. | Read the `key_expr` of the denial rows. That is the prefix the partner is **actually** using. Compare it to the `from/<sov>/**` in the agreement. If they differ this is a naming mismatch — tell the partner the wire name, do not touch the tunnel. |
| **`red`** | Denials observed and **zero allows**: the partner is being refused wholesale and nothing is getting through. | Not a network fault, and this is the whole point — it is the state two organisations currently spend a day misreading as one. | Same first move as amber: read `key_expr`. Then check `cert_common_name` against the one subject this edge's ACL names. A blank CN on every row means only the cached log path fired — see §5. If the CN is right and the key is right, the ACL profile on this edge is wrong; do **not** widen it to make the red go away (see §6). |

`idle` and `red` are the pair that must never render the same. They differ on one measured fact —
whether any decision was denied — read from the same log, in the same window, as the allow side.

## 4. Turning it on

Four things, in order. None of them is on by default.

1. **The observer.** `goat_zenoh_edge_acl_trace: true` for that edge, then re-deploy it. This
   raises **only** `zenoh::net::routing::interceptor::access_control` to trace; everything else
   stays at its normal level. Honest cost: allows log at the same level, so this is one line per
   ACL decision on that link. It is bounded on a peering edge in a way it would not be on a core
   router — one partner, and the edge's ingress rate and size limits cap the traffic — but it is
   not free. `ops/ansible/roles/goat-zenoh-edge/defaults/main.yml` carries the same warning.
2. **The collector.** `ops/peering-denials/pd-snapshot.py --edge <container> --partner-id <sov>`
   on a systemd timer on the edge host. It reads with `docker logs --timestamps --since <cursor>`
   and stores the cursor, so counters are exact across its own restarts rather than inflated by
   re-reads. Writes `/run/peering-denials/snapshot.json` atomically; state in
   `/var/lib/goat/peering-denials/state.json`.
3. **The publisher.** `goat-peering-health` as an mTLS Zenoh client peer with the snapshot
   bind-mounted read-only, `--edge <edge>`.
4. **The ACL grant.** The publisher writes `ops/peering/health/<edge>`, and that write is itself
   subject to the ACL on the router it publishes to — a missing rule means the health readings are
   silently denied, which is F-177 eating its own tail. Grant the publisher's identity write on
   `ops/peering/health/**` on the **core** router (never on the peering edge), the same shape as
   the `infra-w-net-health` rule.

**Deploy order matters:** grant first, then publish. A publisher started before its grant produces
exactly the silence this surface exists to remove.

## 5. Does a repeated denial actually produce a repeated observation?

**Yes, and this contradicts what F-177
and F-448
currently say.** Both record the ACL decision as "logged once and never repeated". Read against the
pinned zenoh `=1.9.0` source, the cache short-circuits the **evaluation**, not the **log**:

| Path | Source | Line printed |
|---|---|---|
| Evaluated (first decision for a key expression on that session) | `access_control.rs` `action()`, ~:998 | `<zid> on <subject> is unauthorized to <msg> on <key_expr>` |
| **Cached** (every subsequent message) | `access_control.rs` `cached_result_or_action()`, ~:84 and ~:393 | `Using cached result: <zid> is unauthorized to <msg> on <key_expr>` |

`filter_message` runs per network message and calls `cached_result_or_action` per message, so a
partner refused a thousand times produces a thousand lines — one evaluated, 999 cached. The
earlier reading is most likely an artifact of the F-448 measurement rather than of the code: that
run drove one REST `PUT` per key and then filtered with `--since 30s`, so a time-boxed grep found
nothing on a quiet link.

**This is source-verified, not live-verified.** No rig was run against it here. So the surface does
not take it on trust: `cached_denial_lines` counts the post-cache lines specifically, and
`repeats_observed` is set only when some row actually fired more than once in a window. If a
deployment ever shows `cached_denial_lines: 0` while a partner is demonstrably still publishing,
the source reading is wrong and the counter below is what you fall back on.

**The fallback is already there either way.** `total_count` per row and `deny_total` per edge are
monotonic across windows: even in a world where the log went quiet, a total that has risen is a
record that refusals happened. A rate an operator can watch rise is the minimum viable evidence,
and it is carried regardless of which reading of the caching behaviour turns out to hold.

Two limits of the cached path, stated because they show up in the output:

- **It carries no ACL subject** — only the ZID. Rows are therefore keyed on
  (session, key expression, action, direction) and the CN is backfilled from whichever line carried
  it. A row whose CN is blank was seen **only** on the cached path.
- **It carries the same key expression**, which is the field that actually diagnoses the problem,
  so a blank CN degrades attribution, never the diagnosis.

## 6. What must not be done about a red

Do not widen the edge's ACL to clear it. F-448
measured why: a peering edge is safe **because it names exactly one subject and defaults to deny**,
not because it inspects anything. A forged CN connects exactly like an agreed one — TLS has no
opinion about common names, and the ACL cannot see which CA issued a cert
(F-424).
Add one subject to an edge and an impersonator inherits it immediately. A red here is a naming
mismatch to be fixed by naming, or a real refusal that should stay refused.

## 7. What this still cannot observe

Stated plainly, because a monitoring surface that overstates its coverage is worse than none.

- **Anything, while the trace switch is off.** That is a detected, named state (`unmetered` with a
  note), not a silent gap — but it is a gap.
- **Denied bytes.** The payload of a refused sample is dropped inside the router and never reaches
  a subscriber. Capturing it would need a custom interceptor, i.e. a forked `zenohd`, which
  [ADR 0858](../adr/0858-zenoh-image-upstream-pivot.md) closes and [ADR 1022](../adr/1022-zenoh-denial-observability.md)
  declines on category grounds. The key expression, subject, action and direction are captured; the
  bytes are not.
- **A refusal the partner never attempted.** If the partner's client gives up before sending, there
  is no decision to log. `idle` is honest about this: it says nothing was asked, not that nothing
  was wrong.
- **Anything before the container's log retention** or before the collector's first run
  (`first_run: true` on that snapshot).
- **Denials at the partner's own edge.** This observes our side. The partner's refusals of *our*
  egress appear here only as `(egress)` decisions our edge itself made.
- **A direct denial counter from zenoh.** The `access_control` module does maintain one, but it is
  behind the `stats` cargo feature, which the upstream `eclipse/zenoh` image is not built with
  (F-177). Counting log
  lines is the available signal, not the preferred one — which is why an unparsed line is counted
  and downgrades the reading rather than being dropped.

## 8. Asking the same question by hand

`ops/scripts/edge-prefix-denials.sh <edge-container> [since]` is the terminal twin of this surface:
same log, same two shapes, no timer and no bus. Use it during a new peering's bringup, which is
exactly when a prefix mismatch happens. It refuses to run when the observer is off, and it
distinguishes "no denials" from "no decisions at all" for the same reason the domain does.

## 9. Tests

| Case | Fixture | Asserted in |
|---|---|---|
| A single denial beside accepted traffic | `ops/peering-denials/fixtures/single-denial.log` | collector + publisher — reads `amber` |
| A repeated denial, no allows | `repeated-denial.log` | reads `red`, `repeats_observed` true |
| **A cached denial** (1 evaluated + 11 cached) | `cached-denial.log` | reads as **12**, not 1; `cached_denial_lines` = 11 |
| Traffic present, nothing denied | `no-denials-traffic.log` | reads `green` |
| No traffic at all | `no-traffic.log` | reads `idle`, and `assert_ne!` against the `red` case |
| Observer off / container gone | (synthesised in the collector test) | reads `unmetered`, note names the switch |
| Log shape unrecognised | `unrecognised-shape.log` | reads `unmetered`, lines counted not dropped |

Run: `cd ops/peering-denials && python3 -m unittest discover`, and
`cd ops/zenoh-trial-peers/peering-health && cargo test`.
