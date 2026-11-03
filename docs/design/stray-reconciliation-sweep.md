# Stray reconciliation + safe sweep

**Problem.** The Data-Access COP ([data-access-cop.md](data-access-cop.md)) reconciles the live
layers (NetBird, Ory, Zenoh) against the declared roster and flags anything
live-but-undeclared as a **stray** (`state=out_of_band`). Operators need to
*clean out* strays — but a naive "cut all strays" is dangerous, because a
"stray" today conflates two very different things:

- **True strays** — genuine offboarding residue or an unmanaged peer enrollment
  never tracked. Safe to cut (after corroboration).
- **False strays** — the entity is legitimately declared, but the
  reconciliation *key* didn't match (a peer named off the `peer-<vendor>-…`
  convention, or an Ory `traits.vendor.short_tag` that doesn't line up with a
  declared org), or the portal is serving a stale `roster.json`. Cutting these
  breaks working identities.

So the safety of any sweep rides on **distinguishing the two before anything is
cut**. That is what this design adds.

## What counts as admitted: the recorded slot first, the roster second

A login is **not** a stray when either of these holds:

1. **It holds a live slot the portal recorded to it at onboarding.** The onboard
   path stamps the person's Kratos identity UUID on the slot it allocates
   (`slots.identity_id`, `onboardSlotSystem` in
   `ops/goat-zenoh-portal/internal/api/handlers.go`), and revocation frees the
   slot (`RevokeSlot` / `RevokeAssignedSlot` in
   `internal/registry/slots.go`). A live slot recorded to this exact identity is
   therefore the portal's own current record that the login was admitted. It
   needs no rebuild step, so a person is known the moment they are onboarded.
2. **It matches a roster entry** (`docs/operators/<goatnet>/roster.json`) by
   email hash, vendor tag or `aff` claim, in that order.

The recorded slot decides the state when it exists: `active`, or `cut_off` when
Ory marks the identity inactive. It also outranks a roster `revoked`, because
revocation frees the slot: a live slot means the access is live, and `cut_off`
would say otherwise.

**Why the recorded slot comes first.** The roster is a hand-regenerated copy of
the onboarding decisions (`ops/scripts/sync-onboarded-manifests.py`, then
`ops/operators/gen-roster.py`, then a PR and a portal redeploy). Every onboarding
since the last rebuild used to reconcile as a stray.

**Their devices too.** The slot alias is the onboarding handle, and the
onboarding names the person's NetBird peers `peer-<handle>-<site>-<date>-…`. The
graph adds each recorded slot's handle to the peer-custody candidate set
alongside the roster handles, so a person the roster does not know yet still
reports their own `on_mesh` / `last_seen`. Longest-wins runs over the combined
set, so a short handle cannot claim a longer one's peer.

**Duplicate roster emails.** A re-onboarded person carries a revoked entry and a
new entry under the same email. The email-hash index keeps the non-revoked one
whichever order the roster lists them in, instead of whichever it read last.

The roster stays load-bearing for everyone onboarded before the linkage was
stamped and for the other consumers of
`roster.json` (affiliation, data roles, the audience preview). The recorded-slot
rule only removes the roster's lag from the stray verdict.

Tests: `ops/goat-zenoh-portal/internal/access/recorded_admission_test.go`.

## Built: the reconcile reason (classify at the source)

Every reason below applies only to a login with **no recorded slot and no roster
match** (see above). The access graph attaches a **`ReconReason`** to every `out_of_band` node
(`internal/access/graph.go`), so a stray self-identifies *why* it didn't
reconcile rather than looking identical to every other stray:

| reason | meaning | disposition |
|---|---|---|
| `untracked` | the live entity carries **no** org/tenant key at all (peer name off-convention, or an Ory identity with no vendor tag) | **cut candidate** (after corroboration) — never enrolled through the tracked path |
| `no-declared-member` | the org **is** declared, but this specific member is absent from its roster | **cut candidate** — classic offboarding residue |
| `unregistered-key` | the entity **does** carry an org key, but it matches no declared org | **VERIFY — do not blind-cut.** Naming drift / a typo'd tag (fix the key) OR an offboarded org. The false-positive highlight class. |

Classification is computed in `Assemble` from data already in the graph — the
identity's vendor tag vs the declared-org index, and the peer name's convention
prefix (`classifyStrayIdentity` / `classifyStrayPeer`). It rides the node →
`WhoResult.recon_reason` and the bulk `RosterRow.recon_reason`, and is rendered
by `goat who`:

- **STATE column** — a verify-class stray (`unregistered-key`) renders `stray?`
  in amber, never a clean `stray`, so it can't be swept as a cut candidate at a
  glance.
- **Detail grid `RECON` row** — names the class + one line of guidance (cut
  candidate vs verify-first).
- **Remediation block** — for a verify-first stray the drill-down prepends a
  `⚠ verify first` caution above the copy-ready `goat admin` commands.

`goat who` stays a pure read-only consumer ([ADR 1047](../adr/1047-goat-who-identity-cop.md));
it reveals + classifies, it never cuts.

## The safe sweep (collect → gate → classify → confirm → cut)

A one-shot "clean out the strays" run is a five-step pipeline with a fail-safe
at every step. The classification (step 3) and the read-only collector
(steps 2–3, `goat access strays`) are **built**; the freshness gate, the
corroboration, and the actual cut (`--apply`) are the next increment.

**`goat access strays` (built)** — collects every stray from the bulk roster,
freezes the set, and buckets it by `recon_reason` into a **CUT CANDIDATES** list
(`untracked` / `no-declared-member`) and a **VERIFY FIRST** list
(`unregistered-key` + anything unclassified), with each entry's last-seen age as
a corroboration hint. The mesh column only reports a device the portal could
attribute (`on-mesh`); an unattributed row prints `device unattributed`, and
`--json` carries `on_mesh` / `connected` / `last_seen_unix` as `null` with
`mesh_attribution` alongside, because the portal did not look. A bare `false`
and `0` there would read as "never enrolled a device". Read-only — it prints the lists and the `goat admin
onboard --revoke <id>` command, and never cuts. `--json` emits the frozen
bucketed set for scripting. **Fail-safe proven live:** run against a portal that
doesn't yet emit `recon_reason`, every stray lands in VERIFY FIRST as
`unclassified` — zero cut candidates, so an un-upgraded portal can never produce
a blind cut.

1. **Freshness gate first.** Confirm the portal is serving the current
   `roster.json` (`ops/operators/gen-roster.py --env <goatnet> --check`) before
   trusting a stray that carries no recorded slot. People onboarded through the
   portal no longer depend on this (see "What counts as admitted"); a stale
   roster can still mis-flag people onboarded before the slot linkage was
   stamped. If freshness can't be confirmed, treat those as suspect.
2. **Collect + freeze.** Snapshot the full stray set (`goat who --once`, filter
   to the stray state) and freeze it, so triage runs against a fixed list, not a
   moving target.
3. **Auto-bucket by `recon_reason`** (built): `untracked` + `no-declared-member`
   → cut-candidate list; `unregistered-key` → a separate **fix-the-key** list
   (reconcile, don't cut); anything unrecognized → human.
4. **Corroborate the cut bucket.** Promote a stray to "safe to cut" only on
   **multiple** agreeing signals — e.g. `no-declared-record` **and** stale
   `last_seen` **and** no live cert — never the single reason alone.
5. **Cut, reversibly, one at a time.** Through the audited chokepoint
   (`goat admin onboard --revoke`, mesh-first, reversible per
   [ADR 1031](../adr/1031-credential-revocation-mesh-first-offboarding.md)) —
   never a bulk blind cut. **Dry-run by default** (print the list + reasons),
   `--apply` to execute, mirroring the `ops/scripts/worktree-reap.sh` safety
   contract.

**Invariant:** the sweep only ever *proposes*; a stray is cut only when multiple
signals agree and a human confirmed the list. False positives are routed to a
fix-the-key list, not a grave.

### Scope note — peers vs persons

`Roster()` rows are org/person spines; mesh peers roll up to their vendor and
aren't standalone roster rows, so the reason today surfaces on stray **persons**
(the visible stray case in `goat who`). Surfacing unmanaged **peers** as their
own sweep rows (they're `out_of_band` peer nodes with a reason already computed)
is part of the sweep-tool increment.

### Reconcile key — broadened past the vendor tag ([F-234], fix built)

**Fixed (ZAGROS deploy pending):** reconciliation now matches a live Ory identity
to its declared member by an `email_hash` (SHA-256 of the lowercased email,
emitted per member by `gen-roster.py`) **before** the vendor-tag fallback — so
tagless organic/partner people reconcile and pick up affiliation instead of
being mis-flagged as `untracked` strays. The join is Go-side; a mismatch fails
closed to the vendor-tag path (never a wrong match). The original limitation,
for context:

### Known limitation (now fixed) — the reconcile key was vendor-tag-centric ([F-234])

A live-fire on ZAGROS (portal built from this branch) proved the classification
works but exposed that reconciliation keyed **only** on `traits.vendor.short_tag`.
ZAGROS's population is mostly organic/partner *people* who legitimately have no
vendor tag ([ADR 1043]), so they're declared-but-unmatchable and reconcile to
`untracked` — inflating the stray set with real people and blocking affiliation attachment (nothing on ZAGROS shows
affiliation). The corroboration step holds them from any cut (all
`last-seen unknown`), so **no real person is at cut risk**, but the sweep and the
affiliation column are nova-a-accurate / ZAGROS-limited until the key is broadened
(a sanitization-safe email-hash person key in `roster.json`, portal-side handle
derivation, or the Phase-D affiliation claim). This is the load-bearing next
step for making the sweep trustworthy on ZAGROS.

[F-234]: ../findings/F-234-vendor-centric-reconcile-key-misflags-organic-people-as-strays.md
[ADR 1043]: ../adr/1043-identity-grammar-role-affiliation-echelon.md
