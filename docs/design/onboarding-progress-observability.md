# Onboarding progress observability

**Status:** built, not deployed — signals recorded, served, and rendered on the
portal's `/progress.html`.

The question this surface exists to answer is not "how far did this person get"
but **"where are they stuck, and what do I do about it"**. Those are different
questions, and the original four-level swimlane could only answer the first.

## The problem with a level ladder

`vendor_progress` records four sticky timestamps: L1 bundle minted, L2 mesh peer
seen, L3 hello-world published, L4 real publish. Every level is genuine evidence.
The trouble is what sits between them, and who performed each one.

L1 is something the **portal** did. A row resting at L1 might be a person who
never received their credentials, never fetched them, fetched them and never got
on the mesh, or got on the mesh and never published. Four states, four different
remedies, one indistinguishable timestamp. F-318 is that failure:
participants read as "L1, no peers" — indistinguishable from never having
started — when they had in fact collected credentials, downloaded bundles, and
enrolled peers, and were stuck at topic registration.

So the surface records the steps a **person** takes, and names the furthest one
with positive evidence behind it.

## Signals

| Signal | Source | Means |
|---|---|---|
| `l1_at` | `vendors.bundle_minted_at` | the portal minted a bundle |
| `bundle_downloaded_at` | `RecordBundleDownload`, on serve | the tarball left the portal, first time |
| `bundle_download_count` | same | how many fetches since |
| `l2_at` / `peer_count` | `netbirdpoll.Poll` | a NetBird peer attributed to this org |
| `registered_topics` | `topics` (unretired) | topics the org **declared** |
| `distinct_topics` | `vendor_topic_activity` | topics **observed** on the wire |
| `l3_at` | audit tailer | first hello-world publish |
| `l4_at` / `publish_count` | audit tailer | real (non-hello) publishes |

`stage` collapses these to one name: `registered` → `bundle-minted` →
`bundle-downloaded` → `mesh-enrolled` → `topics-registered` →
`hello-world-published` → `publishing`.

## Rules this surface follows

**Every stage asserts something that happened.** There is no
`bundle-not-downloaded` stage and no `never-published` stage. A nil timestamp
can mean the thing did not happen or that nothing was recording when it did —
rows predating a column carry exactly that ambiguity — and a name asserting the
negative would turn a gap in instrumentation into a claim about a person. The
reader sees an omitted field and draws their own conclusion, which is the honest
outcome.

**Declared and observed are separate columns, never merged.** They diverge in
both directions and the direction is diagnostic:

- declared > 0, observed 0 → an integration that was set up and never started
- observed > 0, declared 0 → a live publisher whose schemas the decode surfaces
  cannot render (panoscope/flashlight show bytes, not fields)

Summing them into one "topics" number destroys the only signal that tells those
apart.

**Peer attribution is evidence, not a record.** The join runs on the peer NAME
(`peer-<handle>[-<org_short_tag>]-<site>-<date>[-<hostname>]`) because setup-key
enrolment leaves `peer.user_id` empty. The recorded linkage is
`slots.identity_id`. A progress hint is the right strength for name-derived
evidence; nothing in an enforcement path may use this join.

**Candidate keys must cover both goatnet shapes.** Per-vendor goatnets key org
rows by a name that appears in the peer name; per-uuid slot-pool goatnets (ADR
1025) key them by a 32-hex anchor that never does. The matcher takes prefixes
**and** live slot handles in one longest-wins pass. Offering only one shape is
what made L2 structurally unreachable on ZAGROS — and it failed silently,
reporting an honest-looking zero rather than an error.

**Observability writes never fail the operation they observe.** The bundle
download records after the file is served, best-effort. Trading a working
onboarding for a status column is the wrong direction.

## The view

`/progress.html` leads with **Stage**, then a **Bundle** column that separates
minted from fetched, then the four level columns as raw evidence behind them,
then **Activity** as `N declared · M observed`.

Two gaps are named in the row itself rather than left for the reader to derive,
because each has a different remedy: *declared, never published* (an integration
that never started) and *publishing undeclared topics — decode surfaces cannot
render these*.

The summary line leads with how many orgs are **short of publishing** and where
they are sitting, because that is the number that prompts an action; the L1–L4
tallies stay behind it.

A missing bundle fetch renders as "no fetch recorded", never "never fetched",
and carries a tooltip saying downloads have only been recorded since
2026-08-12. The page states in prose that it cannot tell you whether anyone is
*receiving* data.

## Not built

- **Subscribe-side history.** *Live* subscription interest is already solved and
  is NOT this surface's job: the shared subscription-interest census (Tier A,
  live-fired on ZAGROS 2026-07-11) is consumed by panoscope, `goat top` and
  `goat who` — see
  [`subscriber-consumption-visibility.md`](subscriber-consumption-visibility.md).
  What that census cannot do is answer *after the fact*: it observes interest
  now, and nothing retains it, so "was this org receiving data last Tuesday"
  stays unanswerable. Retaining it is the open question, and it is
  brokerless-constrained — see
  [`zenoh-brokerless-consequences.md`](zenoh-brokerless-consequences.md).
  Tier B (per-subscriber throughput) is designed-not-built.
- **The console's own lifecycle state is in neither surface.** Proposed →
  approved → onboarded → offboarded, invites, and who sponsored whom live in
  `onboard.db` on the mgmt-host and feed no consolidated view. Answering "what
  happened to this person" still means reading that database directly.
- No stall alerting. Nothing notices that an org has sat at one stage for a
  week; today that requires someone to ask.

## Funnel vs COP — and the one join between them

This surface is the **funnel**: did onboarding complete, and where did it stop.
It deliberately does not answer "what is this identity doing now across every
layer" — that is `goat who` / panoscope `/who`
([ADR 1047](../adr/1047-goat-who-identity-cop.md)), which reads NetBird → Ory →
SPIRE/SPIFFE → Zenoh and was built precisely because identity-adjacent surfaces
had multiplied without one of them being the admin's glance.

Keeping them separate is right — they answer different questions — but leaving
them **disjoint** was not: an admin looking at the COP saw a quiet row and could
not tell someone who finished arriving and stopped from someone who never
finished arriving.

So there is exactly one join, and it runs one way: `stage` is carried on the
COP's existing bulk `GET /api/access/roster` call — no new endpoint, no N+1 —
and rendered as an ONBOARDING column with an `N never finished onboarding`
count in the summary. Constraints on it:

- **Keyed on the write anchor**, the one identifier both surfaces already agree
  on.
- **Display state only.** It builds no edge, grants nothing, and decides no
  verdict. A test asserts that adding a stage does not change write grants.
- **Degraded-read is non-fatal.** A progress failure emits a warn note and drops
  the column; the roster is still correct without it.
- **Empty means unknown.** A row whose stage the portal did not report renders
  `unknown`, and the `never finished onboarding` count excludes it. An
  undercount is honest; counting unknowns would invent the number.
## Cross-refs

- Findings: `F-318`
- ADRs: [`1025`](../adr/1025-zenoh-dynamic-tenant-authz-slot-pool.md) (slot pool),
  [`1039`](../adr/1039-slot-alias-display-layer.md) (alias is display-only)
- Peer-name grammar SoT: `ops/identity/peer-name-grammar.yaml`
