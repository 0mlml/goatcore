# ADR 1136 — Dormancy lifecycle: quarantine by inactivity, revoke only what can be re-onboarded

- **Status:** Accepted (clarified 2026-09-11 — D1's "quarantine" means the ADR 1089 `<SITE>-PROBATION-<role>` group, never ADR 1091's `QUARANTINED`; every hold writes a hold record, `ops/lifecycle/`, and #1040 makes the dormancy sweep the writer of that record; amended 2026-08-18 — **the never-enrolled lane ARCHIVES the proposal record instead of deleting it, and Restore re-mints in one action**; the purge age clock now reads the mint date stamped in the key name rather than NetBird's `updated_at`, which any touch reset, F-353 — **a key whose name carries no date is `unageable` and is never purged, and the sweep is deployed by the `peerview` role rather than placed by hand**; amended 2026-08-17 — gear revoke at 120 days; never-enrolled purge lane added; **D5 corrected — NetBird's inactivity switch is inert on a setup-key-enrolled fleet, both lanes run on our own evaluation**, F-347)
- **Date:** 2026-08-15
- **Deciders:** platform
- **Related:** [ADR 1068](1068-anomaly-probation-lifecycle.md) (probation — the
  holding state this reuses), [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md)
  (mesh-first, reversible until `--purge`), ADR 1116
  (the gear classes this keys on),
  F-338
  (what prompted it, and the measurement that bounds the benefit)

## Context

Every peer on a setup-key-enrolled goatnet carries NetBird's lifecycle switches **off**:

```
login_expiration_enabled=f  inactivity_expiration_enabled=f  ephemeral=f   every peer
```

So nothing ages out.

Two costs follow from peers that never leave. Every peer is in every other
peer's network map, so each dormant entry is management traffic and a
connection every client keeps attempting. And a peer that is never reaped is
never re-verified: it holds mesh membership indefinitely on the strength of an
enrollment nobody has revisited.

### What this does NOT fix, stated first

F-338 is the reason this came up, and the honest answer is that **dormancy
reaping is a partial mitigation of unproven size** for that problem. Measured
during the F-338 RCA: the offline population held **roughly constant** while
the large-message rate swung **by more than an order of magnitude**. The offline set bounds
*who* can be retried; it does not set the rate.

Adopt this for network-map cost and for membership hygiene, which it does
address directly. Do not adopt it expecting the signal load to fall
proportionally — that belongs to [ADR 1134](1134-rosenpass-key-distribution-by-digest.md)
and the ADR 1081 L1 breaker.

## Decision

### D1 — Quarantine is probation, not a new mechanism

A dormant peer enters the **ADR 1068 probation list**: recorded, left in place,
bounded by a deadline, resolved by repair or purge. Probation already has the
store, the operator-gated API, the `goat access probation` surface, and the
reversible-by-default posture. Dormancy becomes a new *trigger* into it, not a
parallel lifecycle.

Quarantine therefore means: **removed from the active network map, retained as a
record, restorable by a decision.** It is not a revoke and it is not a delete.

> **Clarified 2026-09-11 — the decision is unchanged; the word was ambiguous.**
> This section uses "quarantine" for the *state*, and ADR 1091 uses
> `QUARANTINED` for a *group* that is the security containment lane —
> admin-placed, never automatic, no explainer, no self-service exit. The sweep
> built from this ADR read the word and moved dormant peers into that group
> (#444). To remove the ambiguity: **a dormancy hold parks in the ADR 1089
> `<SITE>-PROBATION-<role>` group** (gear in `PROBATION-GEAR`, ADR 1116 §8);
> `QUARANTINED` is ADR 1091's alone. Every hold, either lane, writes a hold
> record whose `prior_groups` is the restore content
> (`ops/lifecycle/schema/hold-record.v1.schema.json`). The full rule set lives
> in [membership-lifecycle-and-transfer.md](../design/membership-lifecycle-and-transfer.md)
> §"Hold record"; the sweep correction is #1040.

### D2 — The thresholds differ by node class, because the failure modes differ

| Class | Quarantine | Revoke |
|---|---|---|
| Never enrolled — key issued, no peer ever created | — | **revoke the key, ARCHIVE the record** (amended 2026-08-18; was *purge*) |
| People (laptops, phones, workstations) | **14 days** inactive | **60 days** offline |
| Gear — `SENSOR`, `EFFECTOR`, tactical, bearer | **30 days** inactive | **120 days** offline |

A person who has not connected in 60 days has left, changed machine, or does not
need access; re-onboarding them is a portal flow that takes minutes and their
identity still exists.

Gear gets four months rather than two, because a sensor can be legitimately
disconnected for a long time by the mission and a month of silence is
unremarkable for a field device.

**AMENDED 2026-08-17, and the earlier reasoning is kept because the risk it
names is real.** This decision originally said gear is revoked **never
automatically**, on the grounds that re-onboarding requires reaching the device
— which is precisely what is not possible for something deployed and silent — so
an automatic revoke converts a temporary communications gap into a permanent
loss of the asset. The operator has decided that four months of silence is long
enough to act on, and that is the rule now.

The residual risk is unchanged by the longer threshold and is stated rather than
argued away: **a device revoked at 120 days must be physically or
administratively reached to come back.** What the design does to bound it:

- Revoke is the ADR 1031 **reversible** revoke, never `--purge` (D4). The
  registry record, the identity, and the attribution survive — re-onboarding is
  a re-enrolment, not a re-creation from nothing.
- A peer whose class **cannot be determined is treated as gear** (D3), so the
  longer clock is the default for anything ambiguous.
- The revoke lane is the one that runs report-only longest (D6). Four months is
  long enough that the first real revocations are months away; nothing is
  stranded by shipping this.

What it does NOT do is make an unreachable device reachable. If a mission
routinely parks gear beyond four months, the threshold is the wrong number for
that fleet and should be raised for it, not worked around at run time.

30 days rather than 14 for gear quarantine because a month of silence is
unremarkable for a field device and 14 days would make quarantine the normal
state, which destroys the signal.

### D2a — A key that was never redeemed is not a lifecycle, it is a record we should not hold

A handle can have a setup key minted and never enroll a peer: a person is
issued a credential and never joins the mesh, and the key stays valid and
unredeemed.

These are not dormant. Dormant implies a peer that connected and stopped, and
there is no peer — nothing was ever created, so no inactivity clock can start
and every threshold above is undefined for them. They sit outside the lifecycle
entirely, which is why the original version of this ADR did not mention them and
why nothing has ever counted them.

Holding them is a liability with no upside. Each is a live, valid credential
issued to somebody who did not use it, plus a personal record — name and email —
retained for an onboarding that never happened. The credential is the sharper
half: an unredeemed reusable key is an unattended way onto the mesh.

**Decision: purge, not quarantine.** Revoke the unredeemed key and delete the
proposal record. There is no peer to quarantine and no membership to suspend,
and retaining someone's details against a process they abandoned is the wrong
default. Re-onboarding is the ordinary portal flow.

> **AMENDED 2026-08-18 — the record is ARCHIVED, not deleted.** The paragraph
> above is kept as written because the reasoning it got wrong is instructive.
> "Re-onboarding is the ordinary portal flow" is true and was still the wrong
> conclusion: the ordinary flow is a *three-stage* flow with two human gates, so
> deleting the record priced re-enabling one person at two fresh vouches for
> somebody who had already been vouched for. That is a real cost paid by people
> other than whoever runs the sweep, and the predictable result is that the lane
> is never run with `--apply` at all, and unredeemed credentials and stale
> `onboarded` records accumulate with nothing ever tidying either.
>
> An irreversible action nobody dares take protects nothing. Cold storage costs
> the same tidiness and keeps the way back to a single admin action:
>
> - `archived` — a new terminal-ish state. The row is kept WHOLE: sponsor, both
>   gate approvers, tier, affiliation, manifest. The unredeemed key is still
>   revoked; only the record survives.
> - **Restore** returns the row to `approved`, which is the state the runner
>   claims from — so one click re-mints a fresh key and re-sends the welcome
>   package, with neither human gate repeated. Retaining the approvals is
>   precisely what makes that safe: nobody is re-approved, the original approval
>   is re-used.
> - Restore (and retry of a `failed` row) drops a `netbird.ttl_days` longer than
>   the role default when `netbird.ttl_reason` does not justify it. Rows approved
>   before the peer default fell from 90 days to 7 still ask for 90, which the
>   mint refuses, so without this every such restore lands in `failed`. Dropping
>   it only shortens the key to the default; a justified override is kept.
> - A restored person whose fabric slot survived the archive gets that slot
>   back: the deferred mint reuses a live slot under the same handle instead of
>   refusing it as taken (#1576), and their login's org and name follow the
>   restored manifest.
> - An archived row still answers the duplicate-email guard, so a shelved person
>   cannot acquire a second proposal row by re-submitting. Restore is the only
>   way back, which is what keeps one person to one record.
>
> Deletion remains available as a deliberate human action for the case archiving
> does not cover — a record that should not be retained at all. It is no longer
> what a sweep does by default.

Two guards on it, because purge is the one irreversible action here:

- **Only where no peer has EVER existed for that handle.** Not "no peer now" —
  a handle that enrolled and was later revoked has history and belongs in the
  dormancy lanes above, not here.
- **A grace period of 30 days from key issue**, so somebody onboarded this week
  is not purged before they have had a chance to act.
- **A key nothing can age is never purged** (added 2026-08-18). The grace period
  is measured from the mint date the peer-name grammar stamps into the key name;
  a key minted outside that grammar has no creation-anchored fact to measure, so
  the age is `None` and the irreversible lane fails closed. It is reported in its
  own `unageable` bucket rather than dropped, because an exemption nobody can see
  is indistinguishable from no such key existing.

### D3 — Class is read from the registry, never inferred from the name

The split keys on `node_class` (`ops/identity/grammar.yaml`), which is stamped
at enrollment and enforced by the setup key. It is NOT inferred from hostname,
OS, or naming convention — a device called `Mac` tells you nothing, and F-338's
remediation found peers whose names carry no attribution at all.

**A peer whose class cannot be determined is treated as GEAR** — quarantine, no
automatic revoke. Fail toward the reversible action. The cost of wrongly sparing
a laptop is one stale record; the cost of wrongly revoking a deployed sensor is
a truck roll or a lost asset.

### D4 — Revoke means the ADR 1031 reversible revoke, never `--purge`

Automatic dormancy revoke performs the **mesh-first, reversible** cut: the peer
loses mesh membership, the registry keeps the record, and the identity survives.
`--purge` — the irreversible step that cleanses the credential — stays a human
decision. Nothing automatic should reach it.

### D5 — Both tracks run on our own evaluation; NetBird's switch is inert here

> **Amended 2026-08-17 (F-347).**
> This decision originally called `inactivity_expiration_enabled` "the cheapest
> way to enforce the people track" and reserved our own evaluation for gear.
> **That switch cannot fire on this fleet at all**, so the people track is
> carried by our own evaluation too. The original text is preserved below the
> line because the reasoning it recorded is still sound — it was the fact about
> NetBird that was wrong.

`inactivity_expiration_enabled` is a per-peer NetBird switch with no notion of
node class, so it cannot express D2 on its own. That much was always true.
What was missed is that it cannot express D2's *people* half either, here:

```go
// management/server/peer/peer.go:290
func (p *Peer) SessionExpired(expiresIn time.Duration) (bool, time.Duration) {
	if !p.AddedWithSSOLogin() || !p.InactivityExpirationEnabled || p.Status.Connected {
		return false, 0
	}
	...
}
```

`AddedWithSSOLogin()` is `return p.UserID != ""`. On a setup-key-enrolled
fleet **every peer has an empty `user_id`**, because every one is
admitted by a setup key rather than an SSO login. `SessionExpired` therefore
returns `false` for every peer on the goatnet before it looks at any timer.
Turning the switch on fleet-wide would change nothing.

**Both tracks are therefore evaluated by `ops/scripts/lifecycle-sweep.py`**,
which reads the peer list, resolves class per D3, and applies the D2 thresholds
itself. Gear peers keep `inactivity_expiration_enabled` off, and so do people
peers — not as a policy choice but because the flag is inert either way, and a
flag that is set but does nothing is worse than one that is clear.

**Do not "simplify" this by deleting the person lane and setting the flag.**
That is the specific regression this amendment exists to prevent: the fleet
would go unreaped and the surface would show an empty quarantine list, which is
indistinguishable from a healthy fleet. If peers ever become SSO-attributed,
this can be revisited — but that is a much larger change than a flag.

`p.Status.Connected` in the guard above is correct and load-bearing for a
separate reason F-347 documents: `peer_status_last_seen` is written at session
start and end only, so for a *connected* peer it means "connected since". Every
consumer of that column needs the connected guard, and three now carry it
independently — NetBird's `SessionExpired`, `lifecycle-sweep.py`, and
`peer-dormancy-report.sh`.

Whatever runs the evaluation conforms to the
`live-state-guard-scheduling.md` §4 contract: a mgmt-host timer, never a
GitHub Actions cron, because it needs an admin-scoped NetBird credential and
push access must not confer NetBird admin.

**The timer needs a converge path, not just a host** (added 2026-08-18). It had
neither: `lifecycle-sweep.py` and both units were placed on the mgmt host by
hand, owned by no role, playbook or CI job. The host copy drifted from the repo
and kept running the broken age clock for hours after the fix merged, because
merging changed nothing on the box. It is now installed by the `peerview` role —
chosen because the sweep writes its roster into `peerview_state_dir`, which that
role already owns — and the role removes the hand-placed copy from
`/usr/local/bin`. The installed unit carries no `--apply` and no `--lane`: which
lane to run is an operator's decision, and a timer that could revoke is a timer
nobody dares leave enabled. A guard on an unmanaged timer ages out of date while
its output still looks authoritative, which is worse than no guard.

Two consequences of holding it to the §4 contract are worth stating, because both
look like omissions:

- **The token arrives in an EnvironmentFile, not from a container.** The
  hand-placed version scraped it out of the running `onboard-runner` with `sudo
  docker inspect`, which forced the unit to hold docker-socket access, defeated
  the confinement §4 asks for, and named a goatnet-specific variable that would
  read empty on any other goatnet. The role now renders the token from
  `group_vars` and **refuses to install the timer at all when it is unset** —
  an absent roster is visible, whereas a unit failing hourly gets muted and then
  ignored.
- **It publishes no metric by default.** The node_exporter textfile directory
  differs per goatnet, and a directory existing is not evidence a scraper reads
  it — writing a metric nobody scrapes is the F-335 fault. The support is there
  and the variable stays empty until a scraper is confirmed.

### D6 — Announce before the first reap, and make the list reviewable first

The first run of anything that removes membership runs in **report-only** mode
and publishes the roster it *would* act on. A lifecycle policy that silently
disconnects dozens of people on its first evaluation is indistinguishable from an
outage, and the affected peers are exactly the ones whose owners are least
likely to be watching.

## Alternatives rejected

- **One threshold for everything.** Simple, and it either strands field hardware
  at 60 days or lets laptops accumulate for months. The two populations have
  genuinely different physics.
- **Revoke gear on a longer timer (say 180 days).** Still strands the asset, just
  later, and the operator who could have intervened has forgotten the context by
  then. The problem with automatic revoke of unreachable hardware is not the
  duration.
- **`ephemeral` peers for gear.** NetBird's ephemeral flag deletes a peer shortly
  after disconnect. That is the exact opposite of what a field device needs.
- **Infer the class from the peer name.** The name is evidence, not a record
  (peer-name-grammar.yaml), and live peers can have names that carry no
  attribution at all.
- **Delete dormant peers outright to fix F-338.** Measured and rejected: the
  offline count held steady while the rate moved by more than an order of magnitude.

## Consequences

- **On a young goatnet this quarantines a handful of peers and revokes none**
  until peers pass 60 days. The policy is preventive; it does not clear a
  backlog where there is not one yet.
- Peers must carry a resolvable `node_class` for D2 to discriminate. Where it is
  absent the peer is treated as gear, so the policy degrades toward inaction
  rather than toward damage.
- Network-map size falls by the quarantined count, which is the benefit actually
  being bought.
- Re-onboarding after a people-track revoke is a portal flow; it is not free, and
  60 days is chosen to make it rare.
