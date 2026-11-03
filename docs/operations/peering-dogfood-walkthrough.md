# Peering dogfood walkthrough — skeleton

**Status:** Ready to fire. Pre-populated for live-fire ceremony with one external Zenoh-first-timer participant.

Companion to `vendor-dogfood-walkthrough.md` + `external-vendor-dogfood-walkthrough.md`. Different scope: those walks validated the **single-publisher** flow (Flavor A — partner runs `first-publisher.py` only). This walk validates the **router-peer** flow (Flavor B — partner runs their own `zenohd` and federates with ours), which the 2026-05-21 portal-peering-section substrate added.

## Audience

One participant who:

- Knows enough programming to read a Python or shell snippet
- Has **never used Zenoh** before
- Has **never operated a `zenohd` router** before
- Is willing to be shoulder-surfed for ~60-90 minutes

Decline anyone who's already used Zenoh productively — they bring fluency that contaminates the validation. The whole point is finding what the substrate assumes about prior knowledge.

## Pre-flight (operator)

- [ ] Portal live on the trial goatnet at `sha-59e417a` or newer (F-157 mitigation must be in place)
- [ ] Participant's machine on the netbird mesh
- [ ] Participant can reach `https://portal.example/` (curl-check that it returns 200)
- [ ] Operator has a vendor prefix ready to mint (pick one not in current `taken_vendor_prefixes`)
- [ ] Operator workstation has `PORTAL_OPERATOR_API_KEY` loaded
- [ ] Recording: screen-share is fine; encourage participant to think aloud
- [ ] Stopwatch / log: timestamp each "what is this?" / "this is broken" / fumble

## Constraint on the operator during the walk

**No coaching for >10 minutes.** If the participant is stuck for less than 10 minutes, watch them work it out — every workaround they invent is signal about the substrate's failure modes. After 10 minutes of true stuck, give the minimum hint to unblock, log what the hint was, continue.

If the participant asks a clarifying question that has an answer **on the portal pages already** (Our Config / Guide / Router Config), point them at the page and observe whether they find the answer themselves. If they don't, that's a signal the page's information architecture has a gap.

## The walk

### Phase 0 — Cold open (~5 min)

Hand the participant exactly one URL: `https://portal.example/peering/guide.html`.

Tell them: *"This is a Zenoh pub/sub fabric. Your goal is to publish one message we can both see on the inspector. Take as long as you need."*

Do not point them at any other resource. Do not tell them what Flavor to pick. Do not tell them an operator has to mint them a vendor row. They should discover all of that from the guide.

**Observe:**

- [ ] Do they read the "What Zenoh is" section, or scroll past?
- [ ] Do they pick Flavor A or Flavor B from the decision table?
- [ ] If Flavor B, do they actually need it (do they have downstream infrastructure?), or did they pick it because it sounded more capable?
- [ ] At what point do they ask the operator something? What did they ask?

### Phase 1 — Mint + bundle (~15 min)

Participant asks for a vendor row. Operator runs `POST /api/vendors` with the participant's chosen prefix.

**Observe:**

- [ ] Did they pick a prefix that's already taken? (Did they check Our Config first?)
- [ ] Did they understand the bundle is one-time? Did they capture it on first try?
- [ ] Do they understand which file is which (cert / key / root)?

### Phase 2A — Flavor A (Publisher) path

If they picked Flavor A:

- [ ] `pip install eclipse-zenoh` — works first try?
- [ ] `python3 first-publisher.py` — runs first try?
- [ ] What error did they hit, if any? How long to recover?

### Phase 2B — Flavor B (Router-peer) path

If they picked Flavor B:

- [ ] Did they navigate to Router Config from the guide, or get there by themselves?
- [ ] Punch in prefix, click Generate — config appears as expected?
- [ ] Copy or download the `zenohd.json5` — which did they pick?
- [ ] `docker pull eclipse/zenoh:1.9.0` — works?
- [ ] `docker run ...` with bind-mounts — did they get the mount paths right? Where did they fumble?
- [ ] Did they understand "your downstream stack connects to `127.0.0.1:7447`, not to ours"? How did they discover this?

### Phase 3 — Verify (~10 min)

Participant should see their message on Inspector or Panoscope.

**Observe:**

- [ ] How long did "publish" → "see it on inspector" take?
- [ ] Did they look at Inspector first or Panoscope first?
- [ ] Could they find their topic in the Inspector list?

### Phase 4 — Debrief (~15 min)

Open-ended questions:

1. What was the single most confusing thing?
2. Where did you waste the most time?
3. Was there any moment where you nearly gave up?
4. Was there any moment where it felt easier than expected?
5. If you were the operator handing this to the next person, what would you change first?

## Findings register

| # | Surfaced at | Description | Severity (S/M/L) | Fix | Status |
|---|---|---|---|---|---|
| F-DOG-P-01 | | | | | |
| F-DOG-P-02 | | | | | |
| F-DOG-P-03 | | | | | |

(Fill in during the walk. Following the F-DOG-{1..16} convention from prior walks; the `-P-` infix scopes to this peering walk.)

## After the walk

- [ ] Update this file in place with timestamps + observations + findings
- [ ] Open one PR per finding (or one bundled PR for trivial cosmetic fixes)
- [ ] Update [`zenoh-trial-fabric.md`](../design/zenoh-trial-fabric.md) §"Partner-peering facilitation surface" with the validation result + any scope changes the walk surfaced
- [ ] Promote `block-84-trial-scope-partner-peering-facilitation` in the trial-fabric tracking from "live-fire validation pending" to "live-fire validated YYYY-MM-DD with [participant role]"
- [ ] Decide whether the validation has shaken the substrate enough to start the lift to production Block 84 (the read-only half), or whether another walk against a different audience is needed first

## What success looks like

The walk succeeds if the participant publishes a message we both see, regardless of how many findings they surface. The walk **also** succeeds if they bail out — that surfaces "this substrate is not for this audience" which is also useful data.

The walk fails if the operator coaches around problems instead of letting them surface as findings, or if the recording / timestamping is too loose to reconstruct what happened where.

## Cross-refs

- `docs/operations/vendor-dogfood-walkthrough.md` — 2026-05-19 walk (Flavor A only; surfaced F-DOG-1 through F-DOG-9 + F-143)
- `docs/operations/external-vendor-dogfood-walkthrough.md` — 2026-05-19 walk with Windows vendor (surfaced F-DOG-10 through F-DOG-16)
- [`docs/design/zenoh-trial-fabric.md`](../design/zenoh-trial-fabric.md) §"Partner-peering facilitation surface" — the substrate being validated
