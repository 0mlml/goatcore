# ADR 1150 — Serving more than one alliance or coalition from one goatnet: the expansion path, design only

- **Status:** Accepted — **plans only, deliberately not built.** Operator direction 2026-09-01: *"check that we can handle that problem if and when it comes up, but not preemptively optimize and over engineer against a problem that is not at hand."* This ADR exists to record that we are **not foreclosed** and what the path is, so the next person does not rediscover it or, worse, assume the constraint is structural.
- **Date:** 2026-09-01
- **Deciders:** operator
- **Related:** [ADR 1077](1077-affiliation-coalition-standing.md) (standing) · [ADR 1148](1148-one-name-per-audience-the-circulated-model-is-canonical.md) (alliance and coalition defined) · ADR 1043 (identity grammar) · `identity-grammar.md`

## TL;DR

One goatnet serves one alliance framework today. If one ever had to serve two, **no data migration would be required** — standing is derived, not stored. The change would be a query change plus one genuinely open design question about publishing. Nothing here is built and nothing should be built until a goatnet actually needs it.

## What "more than one" would mean

Two distinct cases, and they are not the same problem:

| Case | Example | Difficulty |
|---|---|---|
| **Two alliances** | a goatnet serving both NATO and a regional treaty body, where an org may be a member of one and external to the other | the harder case — standing becomes non-scalar |
| **Two coalitions** | one alliance, but two separately activated operational groupings drawing on overlapping participants | easier — a coalition is already an activated grouping, not a property of an org |

The second is closer to a release-set question than an identity question. The first is what this ADR is about.

## Why we are not foreclosed — measured, 2026-09-01

Three properties of the current design make a second framework additive rather than disruptive. All were checked, not assumed.

### 1. The registry is already keyed by framework id

`ops/identity/coalition-frameworks.yaml` holds `frameworks:` as a **map**, currently `{nato: …}`, with a sibling `default_framework: nato`. A `default_` key only makes sense where more than one is anticipated. Adding a second framework is a data addition to an existing shape — no schema change.

### 2. Standing is stored nowhere

**No org carries a `standing` (or legacy `coalition`) value**, and no manifest seeds one. It is derived at read time from `nation` + the active framework, effective-dated against the member's `member_since`.

This is the load-bearing fact. Because standing is *computed*, an org being a member of one alliance and external to another needs **no new field and no re-tagging**. The answer is already a function of (nation, framework, as-of); it simply is not being asked twice.

### 3. The single-framework assumption is config, not data

It lives in one place: the portal's `--coalition-framework` / `PORTAL_COALITION_FRAMEWORK` selection, which picks one active framework and passes it into the derivation. That is a deployment choice, not a commitment baked into any record.

## The expansion path, if it were needed

Sketched to establish feasibility, not to be followed as a plan. Steps 1–3 are mechanical; step 4 is the real work.

1. **Add the second framework** to `coalition-frameworks.yaml` — members, `member_since` dates, member markers. Additive.
2. **Make the active framework a list** rather than a scalar in portal config. The derivation already takes a framework argument; it would be called once per active framework.
3. **Make standing a per-framework answer** at every surface that reveals it — `{nato: member, <other>: partner}` rather than a bare `member`. This is the API-shape change, and it is the reason to do this only when needed: it alters a response contract, so it is ADR-1052-method work (additive first, dual-read, flip later).
4. **Decide what a dual-standing org publishes into.** This is the open question and it should **not** be answered speculatively. If an org is an ally under framework A and external under framework B, publishing to `own/**` reaches A's members by read-all — but B's members are on the same fabric. The candidate answers each carry consequences worth arguing about at the time:
   - per-framework namespaces, which multiplies the namespace tree;
   - a single fabric with grant-keyed payload encryption ([ADR 1094](1094-grant-keyed-payload-encryption.md)) doing the separation, which keeps routing simple and moves the burden to key distribution;
   - or separate goatnets, which is the honest answer if the two alliances genuinely must not see each other's traffic.

## Consequences

- Recorded that the constraint is **config-level, not structural**, so nobody plans around a wall that is not there.
- Recorded that standing being derived is what buys the option. **If standing is ever made a stored field, this expansion path closes** and a migration is created where none exists today. That is the one decision that would foreclose it, and it should not be taken casually.
- Nothing is built. No code, no schema, no config. The `frameworks` map keeps one entry.
- Step 4 is deliberately left open. Answering it now would be designing against a hypothetical deployment whose constraints we do not know.
