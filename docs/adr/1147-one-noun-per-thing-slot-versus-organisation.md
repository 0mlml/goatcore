# ADR 1147 — One noun per thing: the registry row is a *slot*, "org" is reserved for the organisation

- **Status:** Proposed
- **Date:** 2026-08-27 (context rewritten 2026-08-31 after tracing the rename history)
- **Deciders:** operator
- **Amends:** [ADR 1052](1052-vendor-to-org-machine-rename.md) — corrects the target noun of that rename. Everything else in 1052 stands, including its migration method, which this reuses.
- **Related:** [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (per-uuid slot pool — the change that made the row a slot) · [ADR 1112](1112-org-hierarchy-and-dedup.md) (the organisation entity — built, enabled per goatnet, adoption incomplete) · [ADR 1102](1102-organization-owned-data-sharing.md) + [ADR 1105](1105-pubsub-org-admin-co-publish-write-grants.md) (org-keyed authority that assumes orgs are real) · [`portal-lexicon.md`](../design/portal-lexicon.md) (the display layer, and its reach) · F-389 (the evidence)

## TL;DR

"Org" names two different things: the registry row, which is really one identity's **slot**, and the **organisation** an operator means when they say the word. Only the first is deployed. Give each thing its own noun rather than build a second one under the same name — and fix the operator-facing damage now, ahead of the wire rename.

## Context

### The word went wrong fifteen days before anyone renamed anything

| When | What happened | Was "the row is a company" true? |
|---|---|---|
| PR #50 | `/api/vendors` ships — one row per **vendor company**, one shared cert, `CN=<vendor>-trial-peer` | **Yes.** The word was correct. |
| [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md), 2026-06-05 | Per-uuid slot pool: one cert per publisher, `CN = slot id`. The row becomes **one identity's anchor** | **No** — and nothing was renamed. |
| [ADR 1052](1052-vendor-to-org-machine-rename.md), 2026-06-20 | `vendors` → `orgs` across the wire, 106 route references | **Still no** — a company word swapped for a company word. |

ADR 1052 did not create the mismatch. It **inherited a row that had stopped being a company fifteen days earlier**, and gave it a second company-shaped name. Its own Related line even calls ADR 1025 "*the anchor model the prefix names*" — the ADR knew the row was an anchor and named the route `orgs` anyway.

### The noun was reviewed. The review asked the wrong question.

`vendor-to-org-machine-rename.md:64` records the deliberation:

> The noun is `org` (the schema/registry noun), **not** "entity" — and the name must be coordinated with the identity-grammar `affiliation` block … so we don't introduce a third competing noun.

The question asked was *"org or entity — which word?"*. The question never asked was *"is this thing an organisation at all?"*. And the answer — deliberately align the row's noun with the `affiliation` block — is precisely what welded the two senses together, because `affiliation.id` **is** the containing organisation. Avoiding a third noun by merging into the second is what produced the collision.

### The migration was executed well; only the noun was wrong

ADR 1052 ran four phases with dual-emit, a client-read flip, a `legacy_vendor_surface_hits` counter, live-fire measurements, and a gate that correctly **refused** removal when it caught a partner bridge still on the old route (F-233). That method is sound and this ADR reuses it. The defect is the target word, not the machinery.

### The code still holds three names for one row

| Layer | Name | Cite |
|---|---|---|
| struct | `Vendor` | `internal/registry/registry.go:31` |
| handler | `listVendors` | `internal/api/handlers.go:98` |
| route | `/api/orgs` — derived by **string-replacing** the vendor route | `internal/api/server.go:501` |
| the value the row actually carries | `slot.SlotID` | — |

The substrate has been naming the right word the whole time, in the field the row is assigned from.

### The consequences are live

On a production goatnet, registry rows outnumber the real organisations they stand for, because grouping is string equality on a label an enrollee types by hand. An operator asked `goat-id-console` to move an identity between orgs and was told the org "did not exist" — true, and useless, because no orgs exist there. `goat admin org tree` and `org update` return bare `404` while appearing in `--help`.

**Operator direction: we should not have two things called org.** This ADR records which one moves.

## Decision

**The deployed registry row is a `slot`. The noun `org` is reserved for the organisation entity of [ADR 1112](1112-org-hierarchy-and-dedup.md) and is not used for anything else.**

| Thing | Noun | What it is | Status |
|---|---|---|---|
| Registry row keyed by anchor | **`slot`** | one identity's tenancy: anchor = cert CN = write namespace root | deployed; renamed by this ADR |
| The organisation | **`org`** | a real unit with members, a parent, aliases, and a merge verb | ADR 1112 — built; enabled per goatnet, and not yet read by the roster |

`slot` is not new vocabulary. It is the word already used wherever the thing is discussed precisely — slot pool, slot anchor, slot alias, `AllocateSlot`, pre-cut slot blocks. The registry row is the one place calling it something else.

### Order of work — repair first, adoption next, wire rename last

Corrected 2026-08-31. An earlier revision of this ADR treated the organisation entity as
unbuilt and made "decide ADR 1112" the pivot. That was wrong: **ADR 1112 shipped on
2026-08-04 (`#2095`) and is enabled per goatnet.** The gap is adoption, not
construction — see that ADR's corrected status line.

1. **Flip the display lexicon off `org`** on the four surfaces that read it — `portal_lex_org`
   is consumed by `goat-zenoh-portal`, `panoscope`, `flashlight`, `stethoscope`. It is free
   text and derives its own plural and title-case forms (`config.go:476-479`: plural
   `<Org>s`, title = first-letter-upper), so any lowercase singular noun is a legal value.
   One group_var per goatnet, display-only, reversible.

   **The display word is `slot` too — one noun, everywhere.** Operator decision
   2026-08-31. `portal-lexicon.md` permits the human word to differ from the machine word,
   but permission is not a reason: this ADR exists to stop one thing having two names, and
   a portal that says one word while `goat admin` and the code say another rebuilds exactly
   the translation gap that produced the original defect.

   `participant` was considered and rejected. It is the circulated explainer's word (20 uses
   against 0 for `slot`), and it is not *wrong* — that document ties it to the credential
   ("a property of the credential", "each chosen participant's public key"), so it denotes
   the same thing a slot does. That is precisely why it loses: it is a **second** word for
   what `slot` already names, not a more accurate one.

   **The one real cost, stated honestly.** Roughly six stethoscope verdict strings become
   repetitive — `registration.rs:114` renders *"Slot 'X' has 3 slot-anchored topics
   (declared under slot-pool anchors)"*. Every use there points at the same slot-pool
   concept, so this is clumsy prose, not ambiguity; reword those strings. An earlier
   revision of this ADR called it a three-way collision, which was an over-claim.

   **Two hazards were cleared before this flip, both live defects in their own right**
   (F-413):
   ten indefinite-article sites that tokenised `a`/`an` against the noun (nine of which
   render *"a org"* today), and the single site where a lexicon token sat in a **comparison**
   rather than in rendered text.

2. **Fix `goat-id-console` separately — it does *not* read the lexicon.** Verified: no
   `portal_lex_org` reference anywhere under `ops/goat-id-console/`. The console is where the
   operator error happened, so the cheap display fix does not reach it.
3. **Turn on the duplicate-label check that already exists.** Not a build:
   `/api/org-registry/suggest` and the match-or-create panel (`org_match_review.go`) ship in
   the image production already runs. Set `id_admin_console_org_match_enabled: true`. The
   schema is `CREATE TABLE IF NOT EXISTS`, applied by `orgstore.Open()` — idempotent, no
   migration runner, no downtime. **Scope honestly: this fixes the *intake* duplicate check
   and does not fix the 404s** — see (4).
4. **Fix the 404, which is a routing gap, not a disabled feature.** An earlier revision of
   this ADR said the honest message was "present but disabled here". That was wrong, and the
   code refutes it: `cmd/console/main.go:117` wires `srv.Orgs` **unconditionally**, all nine
   `/api/org-registry/*` routes are registered with no flag guard
   (`handlers.go:369-377`), and `apiOrgList` consults only the nil-store check, never
   `orgMatchActive` (`org_registry.go:58-60`). With the flag off, `org tree` returns **200
   and an empty list**. `ORG_MATCH_ENABLED` gates matching only — six handlers, all in
   `org_match_review.go`.

   Two real defects produce what the operator saw:

   a. **Reachability.** The single router for that host is
      `` Host(`onboard.<site>.<fqdn>`) && PathPrefix(`/api/onboard/`) ``
      (`ops/ansible/templates/traefik-acme/mgmt-dynamic.yml.j2:747` — the only such rule in
      the tree). `/api/org-registry/*` matches nothing, so **Traefik** answers a bare
      `404 page not found` before the console is reached. `POST /api/users/groups` and
      `/api/users/state` sit outside that prefix too. Fix is one line: widen the prefix.
      `sync-org-catalog.py` is unaffected — it builds from a free-form `$CONSOLE_URL`.
   b. **The CLI guesses.** `crates/goat-cli/src/cmd/admin.rs:1425-1435` maps *any* 404 to
      the sentence "org matching is not enabled on this goatnet (ORG_MATCH_ENABLED off)".
      The server already returns a typed `org_match_disabled` error, but
      `PortalError::Http` (`portal.rs:39-40`) carries only a status code and the non-2xx
      arms discard the body. So the operator is shown the CLI's interpretation of a number.
      **On a goatnet where the flag is ON, that sentence is simply false.** Fix
      is to carry the server's `code`/`message` and render it.
5. **Reconcile the existing fragments** — several ids that each name the same unit. The
   edit point is **not** `roster.json`, which declares itself *"GENERATED … do not
   hand-edit"*. Use `orgstore/merge.go`, which makes a loser id a permanent alias so
   anything keyed to it keeps resolving, then re-run `sync-org-catalog.py`.
6. **Cut the roster over to the store, and wire the sync.** The load-bearing item.
   `gen-roster.py` defaulted to `--org-source manifests` at the time of writing — a per-person aggregation of
   hand-typed `affiliation.id` — and **no caller anywhere pins `store`**; (true when written; the 1112 cutover landed 2026-09-07 and `store` is now the default)
   `sync-org-catalog.py` has **0 references** in CI, Ansible or any timer. Until that
   changes, the org store is authoritative by design and unread in practice, and a merge
   performed in (5) reaches no enforcement path.

   **This is safe to land before any curation exists.** `apply_org_catalog()` is an
   *overlay*, not a replacement: the manifest aggregation remains the base and the catalog
   overrides only the org facts it actually carries. The function's own contract
   (`gen-roster.py:468`) states it directly — *"An empty catalog is a no-op by
   construction: the parity gate holds because every branch below requires a catalog org
   row."* So the cutover can be pinned first and curation applied incrementally afterwards,
   with no flag day and no seed tool. An empty or partial catalog degrades to exactly
   today's output.

   **Order matters, because a MISSING catalog is not the same as an empty one.**
   `apply_org_catalog()` treats an *empty* catalog as a no-op, but
   `load_org_catalog()` (`gen-roster.py:501-508`) **`sys.exit`s** when the catalog *file*
   is absent — deliberately, *"fail loud, not silently flat"*. Only one goatnet has a catalog
   today, so pinning `--org-source store` first would break Phase 6.1b of
   `operator-onboard.yml` on every other goatnet. Sequence: run
   `sync-org-catalog.py --env <goatnet>` and commit `ops/identity/orgs/<goatnet>.yaml`
   **before or in the same change as** the pin. Do not hand-stub the file; it carries a
   `DO NOT hand-edit` banner.

   **Two upstream defects must be fixed first, or the guard lies.** Both verified by
   execution, not inspection:
   - `sync-org-catalog.py:39-44` builds its per-env variable as `<ENV>_CONSOLE_...`, so
     `--env zagros-prod` looks for `ZAGROS-PROD_CONSOLE_ONBOARD_KEY` — **not a valid shell
     identifier**. Normalise `-`→`_`, or use the bare `CONSOLE_URL` / `CONSOLE_ONBOARD_KEY`
     fallback.
   - The script contains **zero** `try`/`except`, so an unreachable console raises
     `URLError` and exits **1** — the same code `--check` uses for drift. **A dead console
     is indistinguishable from a drifted catalog**, which inverts the guard: an unreadable
     source would be reported as the very condition the guard exists to catch. The
     live-state-guard contract reserves exit **2** for "source could not be read, never
     taken as clean"; this must return 2.

   **Where each half runs.** The staleness check cannot live in Actions, and the reason is
   the credential, not reachability: the export needs an `ONBOARD_OPERATOR_API_KEYS` bearer,
   which is operator-equivalent, and Actions secrets are readable by any workflow on a
   same-repo branch. It belongs on a mgmt-host timer per ADR 1130 /
   `live-state-guard-scheduling.md` §4 — which also notes this would be the **third** such
   guard and triggers its own "fold at three" review. What Actions *can* do: wire the
   already-written, never-referenced `ops/operators/test_gen_roster_orgstore.py` (its two
   siblings are wired; this one has zero CI references), and add a static validator for
   `ops/identity/orgs/*.yaml` — parses, unique ids, every `parent` resolves in-file.

   *Note:* `apiOrgExport` gates only on a nil store, **not** on `ORG_MATCH_ENABLED`
   (`org_registry.go:72-84`), so the sync works on a goatnet with matching off.

   **Done means drift-gated, not merely pinned.** The store reaches the portal through a
   four-hop generated pipeline, so a pinned `--org-source store` whose catalog is never
   re-synced goes stale while every run still reports success — the failure shape of
   F-367,
   where a dashboard graded a correct node against a nine-day-old artifact and blamed the
   node. This item is not complete until the sync is scheduled and its drift check fails
   loudly. Per the live-state guard convention the scheduled half belongs on a mgmt-host
   timer, not in Actions, because it needs a reachable console.
7. **Rename the wire `/api/orgs` → `/api/slots`.** The correct end state, **deferred, not
   refused** — see gating below.

Items 1–4 are small and independent. Item 5 needs a person who knows which units are the
same. **Item 6 is what makes 3 and 5 mean anything.** Item 7 carries all the wire risk and
none of the urgency.

### What item 7 is gated on

- **Item 6 first.** Renaming the row's wire surface before the organisation half is actually
  read would tidy vocabulary while the two senses are still tangled in practice.
- **Removal is the risky phase, and ADR 1052 is the evidence.** Additive dual-read is
  routine; ADR 1052's own Phase 4 wire removal has been blocked by F-233 since 2026-06-29
  and is still blocked. Plan for the new route to be added and the old one to persist
  indefinitely.
- **The fallback if item 6 is declined.** If the organisation entity is never going to be
  read by an enforcement path, the honest resolution inverts: keep calling the row `org`,
  state in the design docs that an "org" is a per-identity label and not a group, and drop
  the unbuildable verbs. That is a worse system and a cheaper one, and it should be chosen
  deliberately rather than reached by default.

### The cert is untouched

`certmint.go` freezes a per-goatnet URI-SAN prefix — a goatnet mints `urn:goat:<goatnet>:org=<anchor>`. Those strings are Class-A, integration-pinned identity. **Already-issued certs are not re-minted and the URI-SAN scheme is out of scope**, exactly as ADR 1052 held the earlier `urn:goat-<goatnet>:vendor=` scheme out of its own rename. A frozen string may disagree with the current noun; that is the cost of freezing it, and it is the correct trade.

## Alternatives considered

**Build ADR 1112 and keep both nouns, disambiguated by context.** Rejected: this is the status quo plus more surface. The confusion is already producing operator errors and, in one recorded case, a wrong access recommendation.

**Rename the ADR 1112 entity instead** — `unit`, `affiliation-group`. Rejected: `org` is the word people use for what 1112 describes, it is already the word in `affiliation.id`, and `data.govern.org` (ADR 1102) is org-keyed authority that assumes the ordinary meaning. Moving the noun away from the concept that deserves it optimises for the code that got there first.

**Do nothing and document the ambiguity.** Rejected by operator direction, and it does not hold: ADRs 1102 and 1105 grant authority keyed on "org", so the word has to resolve to one thing before that authority means anything.

## Consequences

- Operator-facing confusion is addressed by items 1–4 **without** waiting on a wire migration, which is the change from the previous revision of this ADR.
- The console needs its own fix regardless of the lexicon, because it is not lexicon-driven. That was assumed and is false.
- Design docs, CLI help and console strings converge on one meaning per noun; `goat admin org *` becomes the organisation verb set and stops being the slot verb set.
- Frozen URI-SANs keep a legacy noun. Accepted — the same accommodation ADR 1052 made.
- Until 1112 ships, the only honest thing `org` can name is a label.
