# ADR 1126 — The declared ACL asserts things about itself, and they are re-checked when nothing is being written

- **Status:** Accepted
- **Date:** 2026-08-13
- **Deciders:** platform
- **Related:** ADR 1124 (scope-qualified destinations), ADR 1116 (gear node
  classes; the setup key is the enforcement point), ADR 1091 (quarantine
  containment), ADR 1092 (moon sovereignty), ADR 1089 (probation lifecycle),
  ADR 1114 (breakglass), ADR 1110 (consistency foundation), ADR 1123 (client
  demand must not amplify), F-322 (retracted — the method failure that
  motivates this), netvis.md,
  mesh-acl.md,
  netvis-three-views-plan.md §4 phase 2

## Context

`ops/ansible/netbird_acl_matrix.yml` says what is **permitted**. What that has
to **mean** — a quarantined peer reaches nothing lateral, nothing initiates
toward a moon, the breakglass is reachable — is written in `description:`
fields and ADR prose. None of it is checkable.

Two events can break those meanings, and only one of them is a write.

**A policy change.** Someone edits the matrix. A pull request exists, a diff
exists, and a gate can run.

**A membership change.** A peer gains or loses a group. No file is edited, no
review happens, nothing is committed — and an assertion that was true at merge
is now false. This is the change this substrate actually produces: ADR 1116 §9
puts enforcement on the setup key, `goat admin members move` mutates membership
directly, and the transfer engine strips and restores groups as a matter of
routine.

**No write-time gate of any design fires on the second one, because nothing is
being written.** Tailscale — the closest product to our substrate, and the one
whose policy file is the most-praised artifact in the category — has
[#8324](https://github.com/tailscale/tailscale/issues/8324) open since 2023 for
exactly this: their ACL tests are re-checked on policy writes, so removing a
tag can silently violate a passing test. Removing a tag succeeds.

Two more gaps, both open across the whole category:

- **You cannot check your work without shipping it.** Tailscale
  [#4070](https://github.com/tailscale/tailscale/issues/4070), open since 2022.
- **Nobody can assert that a path SHOULD exist.** AWS Network Access
  Analyzer's scope language has `MatchPaths` and `ExcludePaths` and no
  `MustExistPaths`, so a missing required path yields zero findings —
  byte-identical to compliant. Our own breakglass host would be green in AWS's
  flagship product.

And the local motivation, which is a method failure rather than a defect.
F-322 claimed the zagros-prod breakglass was reachable by nothing. It was
**retracted**: the claim was manufactured by diffing live policies against a
stale scratch file from a different goatnet, and every group-name lookup
missed. The output was indistinguishable from a real negative result. No amount
of care prevents that recurring — a hand-run audit is a method with no memory.
An assertion has one.

## Decision

**The declaration carries assertions about itself, in the same file as the
rules, and they are evaluated by one implementation from three places — one of
which runs when nothing is being written.**

### 1. The assertions live in `netbird_acl_matrix.yml`

A new top-level `netbird_acl_tests:` key, keyed by scope, using **Tailscale's
field names** (`src`, `proto`, `accept`, `deny`) so anyone who has used theirs
reads ours cold.

```yaml
netbird_acl_tests:
  zagros:
    - name: "a quarantined peer keeps only the logged web path and DNS (ADR 1091)"
      src: QUARANTINED
      proto: any
      deny: ["DEVELOPER:*", "P2P:*", "INFRA:22", "INFRA:7447"]
```

Additive: no rule changes, and the cutover playbook and
`check-netbird-acl-parity.py` read only the keys they name.

Targets are `SUFFIX:PORT` against this scope's prefix, or `SCOPE:SUFFIX:PORT`
for another (`HOST:` = an exercise's host site, ADR 1124). `*` is a legal port
meaning **any**, which turns the assertion into one about the pair.

**Assertions restate intent that is already written down** — a `description:`
above them, or an ADR. An assertion that is somebody's fresh opinion is a
second source of truth wearing a test's clothes.

### 2. Two separately-named checks, never one with a direction flag

| field | check | means |
|---|---|---|
| `accept` | **existence** | every `src` MUST reach every target |
| `deny` | **isolation** | no `src` may reach any target |

Batfish shipped the general version, it produced semantic ambiguity, and they
split it: *"checking if a service endpoint is reachable from its intended
client locations is a separate query from checking if a service cannot be
reached."* Forward Networks names the same pair **existence** and
**isolation**; those are the two words in the code.

`accept` is the capability we did not have. Until now the surface could only
observe what reaches; it could not assert what should.

### 3. The quantifier is ALL-TO-ALL and it is in the artifact

"Set A should reach set B" has four readings and Batfish reports the ambiguity
causing real misuse. Every source must reach every destination. The string is
carried in every report rather than in a doc somebody has to find.

### 4. Search for the violation; empty is the pass

The evaluator enumerates the pairs that break the assertion. That inversion
turns a spot check into a proof over the bounded space and yields a **named
counterexample** instead of a bare `false` — and beside it, **the nearest rule
that almost holds**. Batfish added contrast deliberately: it is what localises
the fault. "Nothing reaches the breakglass" is a fact; "and this rule reaches
it on 443" is a diagnosis.

### 5. Three verdicts, and `UNDETERMINED` caps the roll-up

`SATISFIED` / `VIOLATED` / `UNDETERMINED`, plus `BASELINED` for a recorded
known violation. Undetermined carries a **closed reason enum** split by whose
fault it is:

| declaration fault | unread source |
|---|---|
| `unknown_group`, `malformed_target`, `unknown_protocol`, `empty_selector` | `groups_unread`, `policies_unread` |

A typo must never be graded as a broken mesh — AWS has a whole class of codes
for that distinction. And exposing the third state is **not enough**: Batfish's
own open bug is parse failures causing later false positives, so one
undecidable assertion caps the whole report and the gate exits non-zero on it.

### 6. Baseline, then ratchet

A row carrying `baseline:` is a violation already known and already decided: it
still runs, is still reported, and does not fail the gate. Batfish §5.2 — a
day-one audit of an aged network *"becomes a low-priority, background
project"*, while *"we see a greater willingness to go fix new errors
immediately."* When a baselined row starts passing, every surface says so
loudly, because a baseline nobody deletes is a baseline that has become
permission.

### 7. One evaluator, three consumers

`crates/panoscope-core/src/aclcheck.rs` evaluates a flat `Model` — a permit set
over group names. Two things build one, and neither decides reachability:

- **declared** — from the two ACL YAMLs, with `netbird_exercises` expanded
  exactly as `netbird-exercise-expand.yml` expands it. No network.
- **live** — from the `netvis::Doc` the matrix already draws from.

| consumer | source | when |
|---|---|---|
| `cargo test -p panoscope-core` | declared | the PR gate |
| `goat acl test [scope]` | declared | a human, before pushing — no network, no context, no credentials |
| netvis `/assertions` + `/api/assertions` | **live** | every read |
| `netvis-assertions.timer` on the mgmt host | **live** | every 15 min, whether or not anyone is looking |

Three copies of "does this rule permit that" is three chances to disagree about
one goatnet. There is one.

The timer is the answer to the membership-change gap: it fails the systemd
unit, so `systemctl --failed` is the alarm and `journalctl -u
netvis-assertions` is the evidence. It costs no extra management-API reads
beyond the ones the page already makes (ADR 1123).

## Consequences

- **39 assertions across `zagros`, `zagros-prod` and `site-one` hold against the
  declaration** as of 2026-08-13. `zagros-prod` runs `zagros`'s assertions by YAML
  alias, for the same reason it runs its rules by alias: zagros is its
  integration environment, so a property proven there must be the property
  asserted here or the validation does not transfer.
- **A scope with no assertions is named, never omitted.** `nova` and the
  five unstamped exercise scopes are listed as *not checked*. Nothing declared
  is not a pass; it is a scope nothing is watching.
- **An assertion that checks zero pairs is a CI failure.** A row that cannot
  fail is not a control.
- **The declaration is passed INTO the evaluator, never compiled into it.**
  `aclcheck` is a library, and an `include_str!` there forced every image
  linking `panoscope-core` — microscope, stethoscope, panoscope, flashlight,
  none of which read an ACL assertion — to ship `ops/ansible/*.yml` in order
  to compile. Four image builds failed on exactly that before the boundary was
  moved. `netvis/src/doctrine.rs` and `goat-cli/src/cmd/acl.rs` own the
  `include_str!`; netvis's Dockerfile already carried both files for
  `group_purpose`.
- **The first run of the ratchet corrected its own author.** A baseline was
  written claiming zagros's `BREAKGLASS` group was referenced by no rule; the
  gate reported the baseline as passing on its first execution, because ADR
  1114's two rules are in fact declared. The baseline was deleted.
- **`Realization` gained an `Unknown` variant** on the way, because the
  assertion work required an honest answer to "was membership read". With the
  peer list unread every edge previously came out `Dormant`, which asserts both
  endpoint groups have members. See netvis.md.
- **The cross-scope source grammar does not exist yet.** ADR 1124's downward
  rule sources `HOST:PLATFORM-ADMIN`, and an assertion's `src` resolves against
  its own scope only, so that rule is unasserted. Recorded as a gap in
  netvis.md §Open rather than stubbed — a placeholder row would be an assertion
  that cannot be written truthfully.

## Alternatives

- **A Python checker beside the existing parity script.** Rejected: it would
  be a second implementation of reachability, and two implementations is how
  two surfaces come to disagree about the same goatnet.
- **A solver (BDD / SMT).** Rejected. Those exist because a data plane is a
  fixed point over routing protocols. A NetBird policy is group-to-group with
  ports — set intersection over bounded membership. The lineage is cited and
  none of it is imported.
- **A day-one audit that must be cleared before the gate arms.** Rejected per
  Batfish §5.2; the baseline-then-ratchet shape is what gets new errors fixed.
- **Assertions in their own file.** Rejected: they are statements about the
  rules, and separating them is how the two drift.
