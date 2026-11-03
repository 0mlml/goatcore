# ADR 1132 — goat gym: training that points, drills, and records quirks

**Status:** Accepted (2026-08-14, ops) · **Design:** [goat-gym.md](https://github.com/usareur-af/goatstuff/blob/main/docs/design/goat-gym.md) · **Track:** `goat-gym`

## Context

We run 27 distinct hosted services across the goatnets and roughly 324 design
documents behind them. A new person — operator, developer, sponsor, partner,
custodian — arrives with a welcome package and a status page, and then spends
days rediscovering things that are already known.

Three separate problems hide behind the phrase "we need training", and
conflating them is why the obvious answers are wrong.

**Order.** A design document is written for a reader who already knows why they
are reading it. Nothing in the tree says *what to read first if you are a
sponsor*, and the reading list we do have (teammate-recommended-reading.md)
is one order for one audience — a technical generalist joining the project.

**Proof.** Reading is not knowing. There is no point at which someone can
demonstrate, to themselves, that they can do the thing. The first proof today
is a live mistake.

**Quirks.** This is the expensive one. Every surface has behaviour that is true,
load-bearing, and *invisible on first look*: a denied Zenoh publish returns
success to the publisher; an interrupted ansible run leaves a container
executing a stale inode while the re-run prints `changed=0`; a service
declared without a DNS record deploys up, healthy and unresolvable; a
fence wired to a header its own proxy never sets is indistinguishable from a
working fence. Those facts exist — in 336 findings, in ADRs, in
group_vars comments — but they are organised by *when we discovered them*, not
by *the surface you are about to touch*.

The obvious answer is a training wiki, and it is wrong here for a reason we have
already paid for: **two pages claiming one job means only one gets maintained**.
F-326 is that failure exactly — a console page and a guide page both claimed
"onboarding docs", the console page took 0 commits to the guide's 33 over three
weeks, and every newly-onboarded person was sent to the stale one, which was
also gated against them. A training surface that re-explains the fabric becomes
the second page, and the second page is the one that rots.

## Decision

**1. goat gym is a training service whose content is mostly POINTERS.**

Every module declares one of two modes, and the gate refuses anything else:

- `mode: reference` — the explanation exists. The module carries the pointer
  (path, section, and one line saying what the reader gets there) and does not
  restate it. The path must exist in the tree or CI fails.
- `mode: in-place` — nothing owns this explanation. The module carries its own
  prose *and* a `no_sot:` line stating what was searched for and not found —
  a claim a reviewer can falsify.

Today: 78 of 80 modules are pointers. Two are written in place — how to read the
status page (isitup has no design doc) and the end-to-end ACL change walk
(warrant.md holds the model, nothing holds the walk).

**2. What the gym adds is order, a drill, and quirks — never a second copy.**

Each module carries a **drill**: one command with a stated pass condition, and a
`safety:` of read-only / writes / admin. Each carries **quirks**: the invisible
behaviour, each with `evidence:` — a finding ID, an ADR, or a repo path. A quirk
with no evidence is folklore and CI refuses it. The registry currently carries
159 evidenced quirks.

**3. Coverage is measured against isitup, and reported rather than enforced.**

Modules join to live services by `surface:` ids taken from `isitup_services` in
group_vars. `check.py` prints how many live service ids have no module — today
28 of 28 are covered. Coverage is not a build failure: failing a build for
content nobody has written yet trains people to bypass the gate.
`--strict-coverage` exists for the day we hold that line.

**4. The gate is deliberately not path-filtered.**

A module breaks when a doc it points at is renamed — a change that touches
`docs/` and never touches `ops/gym/`. A `paths:` filter would run the gate on
every PR except the ones that can break it.

**5. It is served mesh-only and otherwise unfenced.**

`gym.<site>.<zone>`, the same posture as the onboarding guide. Nothing in it is
a credential and every drill is safe to fail. F-326's rule applies directly: a
"start here" link must be openable by the person you sent it to.

**6. The site is generated from the repo at deploy time, not from a release.**

Unlike the goat-cli docs site — whose content must match a tagged binary — the
gym's content *is* the repo. The role renders it on the control node from the
checked-out tree and copies it to the host. Consequence, stated plainly: the
`ansible-preflight` wrapper matters more here than anywhere, because deploying
from a stale trunk serves stale training, and stale training is worse than none
because it is believed.

## Alternatives considered

**A training wiki or handbook.** Rejected: it becomes the second page claiming
the job, and nothing structurally prevents it from drifting.

**Chapters in the goat-cli book.** The book is versioned with the binary and
translated into 30 languages under a currency gate. Training content that spans
ACLs, deploys and identity does not belong on the binary's release cadence, and
would put every prose change behind a 29-language translation campaign.

**Findings as the training surface.** Findings are organised by discovery date
and written for the person who already understands the system. They are the
right *evidence* store and the wrong *teaching* order — which is why the gym
cites them rather than replacing them.

**Enforcing coverage from the start.** Rejected for now: a red build for
unwritten content trains people to merge past a red check.

## Consequences

- A doc rename now breaks a build if a gym module points at it. That is the
  intended cost: the alternative is a dead pointer discovered by a newcomer.
- Quirks are recorded in two places — the finding (why we know) and the module
  (what to do about it). This is a deliberate exception to one-fact-one-home:
  the module carries the *claim and a citation*, never the analysis.
- The gym is additive. Removing `ops/gym/`, the workflow and the role leaves
  every surface it points at unchanged.


## Amendment — 2026-08-14, after first contact

The pointer-only model in D1 was **wrong for the audience** and is superseded in
part. Modules pointed at repository paths; most readers have no repository, so
the pages read as skeletons — a citation with the reader's time spent on it. The
operator's verdict on the first deployed pages was "sparse", and it was correct.

**A1 — a module teaches in place.** Summary, numbered steps with real commands,
and quirks that carry the instruction that follows from the fact (`so:`). The
gate refuses a written module with fewer than two steps, a step that says
nothing, a quirk with no `so:`, and a drill with no pass condition. The
anti-duplication rule is unchanged: the module holds the *use*, the project
document holds the *reasoning*.

**A2 — documents are hosted beside the gym.** Every cited document is rendered
into the site and linked by its own title. A document that cannot be published
is named on the module that cites it and never linked or pathed. This is the
step that makes "reference the source of truth" true for a reader who has no
repository, and it generalises: when the repository is open-sourced the whole
set can be hosted the same way.

**A3 — no repository path may reach a reader**, checked against the BUILT SITE
rather than the registry, because that is where the reader meets it. The hosted
documents are out of scope for that check: a path inside somebody's engineering
prose is context they wrote deliberately, and mangling it to satisfy a rule
about navigation would be the wrong trade.

**A4 — a publication gate stands in front of every page.** The gym is unfenced
on the mesh, so it refuses pages carrying an operator's name or handle, an email
address, a named place of operations, a mesh or private address, or a
credential-shaped string. Operator names are read at run time from the
gitignored manifests, never committed here. Because those manifests do not exist
in CI, the name check runs at DEPLOY with `--require-roster` and refuses rather
than passing silently — the first version of it found zero names and reported
every page publishable, which is a guard that cannot fail.

**A5 — `planned` modules get no page.** A thin page is worse than an honest
absence: the reader pays the click before finding out. Lanes show the gap by
name. 18 of 80 mapped modules are written today.

**A6 — Foundations: curated outside reading for what we do not own.** Fourteen
technologies (WireGuard, NetBird, STUN/TURN, Traefik, HAProxy/rathole, Zenoh,
Ory, PQC, Rosenpass, Postgres, Ansible, Terraform, aws-vault, containers), each
carrying what it is, how we use it, **where our use differs from what the
upstream document assumes**, curated readings with a reason and an honest time
estimate, and activities. The differences field is the reason the section exists;
the links are the cheap part. Link reachability is recorded with a date and is
NOT a gate — a build that fails when a third party reorganises their
documentation trains people to ignore it — while the shape gate does run in CI.
All modules are now written (80 of 80); `planned` remains a legal state for what
comes next.

**A7 — the gym is opt-in per goatnet, and zagros-prod deliberately does not host
it** (operator decision, 2026-08-15). The role defaults to off; a goatnet hosts
it by declaring `gym_enabled` in its own group_vars, and the play REFUSES rather
than skipping when pointed at a goatnet that has not opted in — a silent skip on
the wrong target is indistinguishable from a successful run. The absence on
zagros-prod is recorded in the group_vars parity exclusions under the repo's
absence-as-data rule, so it reads as a decision rather than a gap.

The reasoning, stated so it can be revisited rather than rediscovered: the gym
is unfenced on the mesh and hosts a library of internal project documents. Where
people are onboarded, that combination is the point — training must be reachable
by someone who has nothing yet. On a production goatnet the same combination
buys training value that belongs elsewhere at the cost of an unfenced surface
and an internal-document library in a narrower environment. Nova is not a
target today and no decision is recorded either way.

**A8 — Foundations covers 19 technologies**, adding CoreDNS, MinIO,
SPIFFE/SPIRE, Matrix and age to the original fourteen. 49 readings, all reached
and answered on 2026-08-15.

**A9 — an unhosted document is named, previewed and located, not apologised
for** (operator direction, 2026-08-15). Each carries its title in normal ink,
what it gives you, a scrubbed preview of one contiguous opening paragraph, and a
link to the repository it lives in with the caveat that access must be
requested. Where it is a goat core repository the page also states that goat is
being open-sourced and the public link will replace the private one — said once
per page, because repeated under every entry it read as noise.

This **reverses the standing rule** that repository URLs stay out of served
pages (they leak org, repo and file layout to viewers who have no access). The
reversal is scoped to this surface and the caveat is rendered every time, never
implied.

Previews are held to the same rules as any other gym text — the publication gate
AND the no-repository-paths rule — and walk paragraphs to find a clean one
rather than editing a quotation to fit. Where none qualifies the page says the
preview was withheld. Both rules caught real content on the first run: the gate
refused four pages whose previews quoted repository paths.
