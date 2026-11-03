# ops/lifecycle — the hold record

The contract between the thing that parks a member's mesh access and the
things that show or undo it. Canon:
[`membership-lifecycle-and-transfer.md`](../../docs/design/membership-lifecycle-and-transfer.md)
§"Hold record"; ADR 1136 D1 (clarified), ADR 1089, ADR 1091.

| File | What |
| --- | --- |
| `schema/hold-record.v1.schema.json` | One record = one held peer: lane, site, peer, when, hold group, **prior groups** (the restore content), actor. |
| `fixtures/hold-records.jsonl` | Two synthetic records, one per lane; the security one carries `reason` + `actor` so both lanes render side by side on the console's Holds page. Console work develops and tests against this file. |
| `validate-hold-records.py` | Validates a file (CLI) and the fixture (pytest). Run by `tests/lifecycle-hold-record.bats`. |

On a mgmt host the file lives where the `peerview` role says
(`lifecycle_hold_records`, default `/var/lib/peerview/hold-records.jsonl`);
the path reaches the sweep as `LIFECYCLE_HOLD_RECORDS` in
`/etc/goat/lifecycle-sweep.env`. Writers: `ops/scripts/lifecycle-sweep.py`
(dormancy lane) and, once built, the console's quarantine action (security
lane). Readers: the console and the sweep's `--lane release`.

Rules every reader relies on:

- **`prior_groups` is the restore content.** A release puts back exactly that
  list, never more. The hold group's name is only the target.
- **`lane: dormancy` ⇔ a `<SITE>-PROBATION-<role>` group; `lane: security` ⇔
  `<SITE>-QUARANTINED`.** The schema refuses the cross.
- **A peer in a hold group with no record is a security hold.** Fail toward
  the lane with no self-service exit.
- **One line per held peer.** A release removes the line; a re-hold replaces it
  under the same lock by dropping any existing line for that `peer_id` before
  writing the new one. The file never carries more than one line per held peer
  at a time.
- **One writer at a time, atomic replace.** Every writer takes an exclusive
  `flock` on a sidecar lock file (`<path>.lock`) for the whole
  read-modify-write and uses the record helper to open the file only after
  that lock is held. A hold appends under the lock; a release writes the
  surviving lines to a temp file in the same directory and atomically
  replaces the original (`os.replace`) under the same lock. A reader never
  sees a half-written file; concurrent writers that honour the lock contract
  never lose each other's record, even when one opened the path before
  another rewrote it.
- **A bad line is skipped, never fatal, and always counted.** A line that is
  not a JSON object, is not `v: 1`, or lacks a field the v1 schema marks
  required is a bad line; blank lines are ignored and not counted. Every
  reader skips a bad line, keeps reading, never acts on it, and reports how
  many it skipped: the sweep in its report (`HOLD RECORDS … bad line(s)
  skipped`, `hold_records` in `--json`) and on stderr when it writes; the
  console on the Access section ("N bad hold-record line(s) skipped", with
  line numbers). No reader refuses the whole file because of one bad line.
  Writers carry bad lines through every rewrite unchanged so
  `validate-hold-records.py` can name them. A held peer whose line is bad
  therefore has no record and reads as a security hold until the line is
  repaired; the count is what tells an operator to look.
- **A stranded hold is finished by the next hold run; inconsistent state
  that needs hands fails it; a group-choice refusal does neither.** The
  sweep writes the ledger row and the record, adds the peer to the hold
  group, verifies, then removes the prior groups, so a crash can leave a
  peer in its hold group *and* in prior groups, with its record (intent is
  written before the first group change, so a hold-group PUT whose response
  was lost is still a stranded hold the next run finishes, never a peer
  with no record). A held peer holds its hold group and nothing else, so *any* other
  group beside a hold group (a role, `SENSOR`, `INFRA`, `PARTNER` — not only
  the canonical roles), or more than one hold group at once, is inconsistent
  live state and is listed under `refused` whether or not the peer is
  connected (a stranded peer that came back online is dual-homed with full
  role access, and must not vanish from the roster). The next `--apply --lane quarantine` recognises a stranded
  hold (the peer is in its record's hold group, and every other group it
  holds is in the record's `prior_groups`) and completes the removal; the
  record stays because the peer stays held. A peer in a hold group and a
  role group with no record, or holding a group the record never listed, is
  left alone, printed `REFUSED … INCONSISTENT, needs hands`, and makes the
  run exit 1. A peer the group-choice rule could not place (two roles, no
  role, a role with no mirror) is the intended fail-closed outcome, not a
  fault: it is printed `REFUSED … left in its groups` and does not change
  the exit code, so a red apply run always means a stuck peer. The
  report-only run (what the hourly timer executes) always exits 0 and lists
  every refusal under `refused`: its guard treats a non-zero exit as "a
  source could not be read" and would stop publishing the roster that shows
  them.
- **A stale record has an exit through release.** When `--lane release
  --peer` finds a record whose hold group the peer no longer holds (released
  out of band), it removes the stale line, says so (`STALE RECORD`), touches
  no group and exits 0. The record never outlives the hold it describes once
  an operator has looked.
- **Every group write re-reads the group first.** NetBird's group update is
  a whole-list replace with no If-Match. A PUT built from a list read
  earlier in the run would silently revert whatever another writer (the
  console release, an admin) did to that group in between — dropping
  *their* held peer out of its hold group, or putting it back into its role
  group. So the sweep GETs the group immediately before every PUT, for the
  hold, the removal, the resume and the release alike.
- **`--limit` is one budget, counted in attempts.** A failed hold spent its
  slot; a group-choice refusal touches nothing and never spends one, so it
  is always printed and counted, and an inconsistent peer behind a queue of
  such refusals is still reached. Inconsistent peers the budget did not
  reach are reported as `NOT TOUCHED THIS RUN`.
- **Release decides on the peer as it is now.** Like the hold, release
  re-reads the peer before its checks, so a containment added since the
  start-of-run list is seen before any role group is restored. When a
  `releasing` record's peer is already out of its hold group, the prior
  groups are restored (idempotently) before the record is deleted.
- **A release marks its intent before it acts.** Half-way through a release
  (prior groups restored, hold group not yet removed) the peer's live shape
  is the same as half-way through a hold. The releaser therefore rewrites
  the record with `phase: releasing` under the lock BEFORE the first group
  write. A `releasing` record is a release to finish (`--lane release
  --peer <id>` again resumes it), never a stranded hold to complete: the
  hold run refuses it and points at release. `phase` is optional in the
  schema; absent means held, and any value other than `releasing` makes the
  line a bad line to the sweep, as the schema's enum does for the validator.
- **An orphan record has an exit.** The report counts records whose peer is
  no longer on the mesh (deleted in the NetBird UI, or an old enrolment's
  id) and names them; `--apply --lane release --peer <that id>` removes the
  line and prints `ORPHAN RECORD`; an orphan `security` line is refused. A
  sweep-driven revoke removes the record itself. The report also names
  `stale` records — a live peer that is not in its record's hold group (a
  hold that failed at the add, or an out-of-band release) — so they stay
  visible after the peer reconnects; the same release command removes them.
- **The sweep acts on `dormancy` records only, in every lane.** The
  stranded-hold resume and release refuse a `security` record; the revoke
  lane skips a peer that holds one or sits in `<SITE>-QUARANTINED` (ADR
  1091: a containment is closed out by its own process, never deleted by a
  dormancy tool); the orphan exit refuses a `security` line.
- **Lane and group must agree for a line to be a record — to the sweep,
  the validator and the console.** A line whose `lane` and `hold_group`
  disagree (`dormancy` with `QUARANTINED`), a `lane` outside the enum, or a
  `phase` other than `releasing` is a bad line to all three readers. The
  sweep (`_is_bad_hold_record`) and the console (`parseHoldRecordLine` in
  `ops/goat-id-console/internal/meshstate`) implement the same rule and
  are pinned to the same answer by
  `fixtures/hold-records-bad-lines.jsonl`: both test suites read it and
  must count 2 records and 10 bad lines. Change the rule in both, and move
  the numbers in both. **Known residual, stated rather than hidden:** the
  console, like the validator, also rejects an `entered_at` that is not
  RFC 3339, a wrong-typed `site` / `lane` / `handle` / `actor`, and a line
  over 1 MiB; the sweep loads those. Such a line is a record to the sweep
  and a skipped line to the console, whose page then shows that peer as
  no-record (a security hold) with the skipped-line banner; the validator
  names the line. The sweep writes none of these shapes itself. A release
  refuses when a prior group no longer exists rather than restoring less
  than the record says and deleting it.
- **`handle` is recorded only when the name says it unambiguously, and
  nobody matches on it.** Handle and short_tag may both contain hyphens;
  without the registry the sweep cannot split `peer-jane-doe-acme-zagros-…`.
  The part before the site is recorded when it has no hyphen; otherwise
  `handle` is `null`. The console attributes a record to a member by
  `peer_id` against that member's own peers (an orphan record, whose id
  is no live peer, by `peer_name` under the same roster-disambiguated
  ownership rule); `handle` is display only, so a hand-edited line cannot
  put one member's hold on another member's page.
- **Release acts on this peer's record only.** A record is matched by
  `peer_id`. A match by `peer_name` is accepted for one case only — the peer
  re-enrolled under a new id, so the record's own `peer_id` is no longer a
  live peer — and only when no other live peer shares the name. A name shared
  by two live peers is refused ("name the peer by id"); a record whose
  `peer_id` is another live peer is never acted on or deleted on behalf of
  this one. The tool cannot tell a re-enrolment from a different device
  that took the same name: an operator who releases a peer whose record
  matched by name is attesting that it is the same device re-enrolled.
- **A `security` record is never released by the sweep.** `--lane release`
  refuses a record whose `lane` is `security` (ADR 1091: the exit is the
  close-out of an investigation), with no flag to override. It also refuses
  a dormancy record when the peer is in *any other* hold group beside the
  record's (`QUARANTINED` placed on top of `PROBATION-*`): NetBird ACLs are
  additive, so restoring role groups would hand a contained peer its full
  reach.
- **A hold re-checks the peer before it acts.** The candidate list comes
  from one read; before the add, the sweep re-reads the peer and re-runs the
  group-choice rule. If the answer changed (an admin added a containment or
  a second role since), the hold is refused and nothing is recorded — a
  containment is never written into `prior_groups` and removed.
- **Reading never needs write access.** The report-only run counts records
  without taking the sidecar lock: writers replace the file atomically, so a
  reader always sees a whole file, and a non-root hand run of the report
  must not die for want of write access to the directory. An unreadable file
  is reported as such, not as zero.
- **Releasing a peer that has no record is an attestation, not a default.**
  With no record, `--lane release` can fall back to the sweep's own ledger
  for a peer in `<SITE>-QUARANTINED`. Ledger rows are this peer's by id; a
  row matched by name is used only when no id row exists, the row's id is
  not a live peer, and no other live peer shares the name, and the refusal
  prints the row's peer id. That group is also the security
  containment (ADR 1091), and a stale dormancy ledger row from an earlier
  episode cannot tell the two apart. The fallback therefore refuses, prints
  the ledger row it would act on, and acts only when the operator passes
  `--acknowledge-quarantined-is-dormancy`, attesting that this hold is a
  pre-change dormancy hold. A peer with a record never needs the flag.
  Membership is checked before the attestation: a peer that is not in
  `<SITE>-QUARANTINED` has nothing the fallback could release and gets
  "nothing to release", exit 0 — so a second release run after a successful
  one is a no-op even though the sweep's append-only ledger still carries
  the hold's row.
