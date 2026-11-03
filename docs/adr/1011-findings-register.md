# ADR 1011 — Findings register: durable home for "things we discovered" that aren't decisions

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-26 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`1008-soak-testing-discipline.md`](1008-soak-testing-discipline.md), [`1007-wave-style-validation.md`](1007-wave-style-validation.md), [`0005-decision-log-vs-design-doc.md`](0005-decision-log-vs-design-doc.md), [`0001-adr-template.md`](0001-adr-template.md) |

## TL;DR (for a layperson)

When we run a soak test, a wave validation, or a hands-on spike,
sometimes we discover something durable about how an external system
behaves — a quirk of Postgres-Spock replication, a multi-tenant filter
inside Kratos, a race window in Hydra's auth-code lifecycle. These
**findings** are different from **decisions** (which are ADRs) and
different from **bugs in our own code** (which the commit message
captures). They're observations about external systems we have to
engineer around. Today they live scattered inside ADR narratives,
decision-review tables, and soak-driver header comments — easy to
miss, easy to relearn the hard way. This ADR establishes a single
durable home for them.

## Context

Five days of soak testing across single-node Ory, 2-node Spock-
replicated Ory, CGNAT validation envs, and Wave 3 hadev surfaced a
recurring pattern: every soak produces ~1–3 observations about
external-system behavior that future operators need to know but that
don't fit cleanly into any existing artifact:

- **Not an ADR** — they're not decisions, they're inputs to decisions.
- **Not a design doc** — they're individual properties, not
  architecture narratives.
- **Not a commit message** — those rot once the diff is reviewed.
- **Not a decision-review assumption row** — those are time-bounded
  to one decision moment; the underlying observation outlives them.
- **Not a soak-driver comment** — locked inside one driver, invisible
  to anyone reading another.

Examples surfaced during the 2026-04-21 → 2026-04-25 window:

- Kratos creates its `networks` row at first boot of `serve`, and
  uses that row's UUID as the `nid` filter; two parallel Kratos
  instances each generate their own and the result is
  application-layer-visible asymmetry even though the database layer
  replicates correctly.
- Spock 5's default conflict resolution is last-write-wins per row,
  not per column — concurrent UPDATEs from different nodes silently
  drop the loser's column-set.
- `spock.repset_add_all_tables` rejects tables that lack a PRIMARY
  KEY when the repset is configured to replicate UPDATE/DELETE
  (because there's no row identity to use for the conflict resolver).
- The postgres-spock image's `init/*.sql` scripts trigger a
  post-init server restart that briefly fails subsequent
  connections with "the database system is shutting down."
- Modern netbird-mgmt images hash the presented setup-key with
  SHA-256-of-uppercase-base64; DB-direct seeding with the plaintext
  key produces a row that no real peer can authenticate against.

Each of these is a **finding** — durable, externally-caused, must-
engineer-around. Each is currently buried somewhere — the kratos
nid one is now in ADR 1009, the LWW one is in ADR 1010, the
`repset_add_all_tables` one is a comment inside the soak driver, the
postgres-spock init-restart is in a half-line of a different soak
driver, the netbird-mgmt setup-key one is in a 2026-04-24 findings
report. Searchability is per-incident; a future operator hitting the
same external behavior wouldn't find prior knowledge unless they
already know to look.

## Decision

**Findings register at `docs/findings/`** with the same lightweight
shape as the ADR series:

- `docs/findings/INDEX.md` — live status board, append-only.
- `docs/findings/TEMPLATE.md` — required sections + ID convention.
- `docs/findings/F-<NNN>-<slug>.md` — one file per finding.

**Stable IDs.** `F-NNN` (zero-padded to three digits, monotonically
incrementing). Once issued, the ID is forever; a finding is never
deleted, only marked `Obsolete` (component removed) or `Addressed by
ADR X` (still true, engineered around).

**Required sections** (per `TEMPLATE.md`):

- **One-line summary** (the title)
- **Trigger** — how the finding surfaced (which soak, which spike,
  which incident, which operator-hands-on session)
- **Observation** — what's true about the external system, in concrete
  terms, with a reproduction recipe if possible
- **Implication** — what this means for our system; what we have to
  do to engineer around it
- **Status** — `Active` (still true), `Addressed by ADR-XXXX` (the
  ADR that engineers around it), `Obsolete` (the component was
  removed or the upstream behavior changed)
- **Cross-refs** — ADRs, design docs, decision reviews, soak fields
  that act on the finding

**When to write a finding (criteria).** A finding is **required** when:

1. The observation is about **external system behavior** — upstream
   software (Kratos, Hydra, Spock, Postgres, netbird), a managed
   service (AWS, Cloudflare), a third-party library, or a cross-
   platform tooling difference (BSD vs GNU date, Linux vs macOS
   docker port-forwarding) — and
2. We have to **engineer around it** — change configuration, add a
   workaround, add monitoring, adopt a discipline — and
3. **Future-us would not learn this from reading our own code** — the
   workaround is in our code, but the *why* (the external behavior
   that motivated it) isn't.

A finding is **NOT** the right artifact for:

- **Bugs in our own code we fixed.** "I wrote `set -euo pipefail`
  thinking pipefail would help; it killed the loop on first transient
  failure" → commit message captures it.
- **Typos / off-by-one / API-shape mistakes.** "I sent `name` instead
  of `traits.name`" → fix and move on.
- **Incomplete implementations we then completed.** "I forgot to add
  the spock extension to kratos+hydra DBs in the init script" → just
  a TODO that's now done.
- **Decisions we made.** Those are ADRs.
- **Architecture narratives.** Those are design docs.
- **Time-bounded snapshots of state.** Those are reports under
  `docs/reports/`.

The discriminator is: **does future-me need to know this even if our
code is correct?** Yes → finding. No → not a finding.

**Cross-linking discipline.** Every finding addressed by an ADR is
linked from the ADR's "Related" row + a "Findings addressed" callout.
Every finding cited by a soak driver is linked in the driver's
header comment by ID. Every decision-review assumption row that
depends on a finding cites the finding ID. Bidirectional links keep
the register usable.

## Alternatives considered

- **Per-ADR appendix sections.** Findings often span multiple ADRs
  (the kratos `nid` finding informs both ADR 1009 wiring and ADR 1010
  operating pattern); duplicating the observation into both ADRs
  invites drift. Rejected.
- **Decision-review docs as the findings register.** Decision reviews
  are time-bounded snapshots scoped to one decision (Ory vs
  alternatives, Spock vs alternatives). Findings outlive the
  decision moment and may inform multiple future reviews. Rejected.
- **Pure operational journal / append-only log.** Lower friction
  but loses structure — the next operator can't search "what do we
  know about Kratos multi-tenancy?" without reading every entry.
  Rejected.
- **Promote findings into structured soak-evidence JSONL fields.**
  Works only for soak-derived findings; doesn't cover findings from
  spikes, hands-on operator sessions, incident post-mortems, or
  upstream documentation reads. Rejected as the only home; the soak
  evidence remains the *quantification* of a finding's surface, not
  the finding itself.
- **Wiki / external doc hosting (Notion, Confluence).** Loses the
  in-repo property. Rejected — same reasoning as ADRs being in-repo.

## Consequences

**Positive.**

- Single search surface for "what do we know about external system X."
- ADRs become smaller (no need to relitigate the underlying
  observation in every ADR that acts on it; cite the finding ID).
- Decision reviews become more honest (assumption rows cite findings
  by ID, making the evidence trail explicit).
- Soak drivers become more readable (no need for paragraph-long
  comments explaining motivations; cite the finding ID).
- New contributors have a fast onramp to "what's been discovered."
- Lossy-knowledge risk drops materially: an observation captured as
  a finding has 3+ links from soak driver, ADR, and design doc, vs
  1 link buried in an ADR narrative.

**Negative.**

- One more doc class to maintain. Cost: small (the format is
  intentionally lightweight, much smaller than ADRs).
- Risk of drift between findings register and the ADRs that reference
  them. Mitigation: bidirectional links + the same review discipline
  ADRs already have.
- Risk of overproduction (writing findings for things that are
  actually own-code bugs). Mitigation: the criteria above plus the
  "future-me needs to know this even if our code is correct" test.

## Assessor view (SCA-R)

The findings register supports CA-7 (continuous monitoring — the
register is the durable home for "what we've learned about the
behavior of the components we depend on"), SA-11 (developer security
testing and evaluation — soak-derived findings ARE the evaluation
artifacts), and CM-3 (configuration change control — findings inform
which upstream-version bumps require additional regression coverage).

## Trigger conditions for revisit

- Findings count exceeds ~50 and the per-finding markdown overhead
  becomes friction → consider a structured frontmatter format the
  index can render programmatically.
- A finding-class repeatedly recurs (e.g., "every Postgres minor
  upgrade surfaces 1–2 new findings") → spin out a sub-register
  scoped to that class with a per-version table.
- We acquire a vendor support contract for an upstream component →
  the findings register becomes the input to vendor support tickets.

---

*See `GLOSSARY.md` for terms and acronyms.*
