# SSH key self-service — submit your own, an admin installs it

**Status:** built; console pages, installer, and `goat admin ssh-key install`
verb landed (CLI + console panel).

Host shell access is a **separate grant** from a console login. This describes
how someone gets one without anybody mailing a public key around.

## The problem it fixes

Platform admins had console access but no shell on the mgmt hosts, and the
welcome package told them "your SSH key was added when you were onboarded." It
was not. Installing a key is a separate ceremony (`operator-tier-onboard.yml`)
that reads `manifest.operator.ssh_pubkey` and **skips silently when absent** —
so the people it skipped were told they had access they did not have.

Two things followed from that. Keys got passed around over chat and installed
by hand, which left no record for offboarding to act on. And nobody could tell,
from any surface, whether a given person actually had a shell.

## The shape

```
person submits  ->  admin approves  ->  installer writes  ->  console says installed
   /ssh-key          /onboarding/         operator-ssh-      status: installed
   (own key)          ssh-keys            key-install.yml
```

Four states, and the words are not interchangeable:

| state | means |
|---|---|
| `submitted` | recorded. **Grants nothing.** |
| `approved` | an admin **decided**. Still grants nothing. |
| `installed` | the key is in `authorized_keys` on real hosts. **Now they have a shell.** |
| `rejected` | declined. **Not terminal:** resubmitting the same key reopens it as a fresh application (back to `submitted`) |
| `revoked` | cut, and it **stays cut** — resubmitting changes nothing; generate a new key |

`approved` and `installed` stay distinct on purpose. Collapsing them is exactly
how the old welcome email came to assert access that did not exist.

**Resubmission updates the one row, it never mints a second.** Same handle +
same key body always lands on the existing record, so an admin never reasons
about two rows meaning one grant. State survives a resubmit (an installed key
must not quietly drop back to `submitted`) with one exception: a `rejected` row
reopens as a fresh application, because nothing else can ever move it — the
queue lists only `submitted`, so a sticky `rejected` left the submitter told
"recorded" while the key waited nowhere. Reopening lands
before the gate — an admin still decides. `revoked` is deliberately immune: a
revoked key was cut on purpose, possibly as compromised, and self-service must
not be able to bring it back. The submit banner reports the state the row
actually ended in, not the happy path.

## Why submitting cannot authorize

The console is reachable with an Ory session. If uploading a key installed it,
a stolen web session would escalate straight to a root-capable shell on a
production host — collapsing two trust boundaries that are separate on purpose.
So the upload records state and nothing else; the installer acts **only** on
rows an admin approved.

For the same reason the **handle comes from the session, never the form**. A
form-supplied handle would let anyone with a console session park their own
public key against someone else's account; the approval step would still run,
and would still hand a shell to the wrong person. An email with no onboarding
record is refused rather than guessed at from the local-part — a guessed handle
keys the grant to an identity that may not exist, or may belong to someone else.

The self-service pages are deliberately **not** behind `onboardGate`. The people
who most need them are ordinary members with no admin rights and no sponsorship
annotation; a gate they cannot pass would send them back to mailing a key
around, which is the path this closes. They are still authenticated, and can
only ever act on their own handle.

## Scope: a shell is not one privilege

`operator_ssh_scopes` names sets of hosts; `operator_ssh_role_scopes` says which
roles receive which.

```yaml
operator_ssh_scopes:
  host-nodes:      # the mgmt pair
    - { ssh_host: "{{ mesh_ip_a }}", ssh_user: "goat" }
    - { ssh_host: "{{ mesh_ip_b }}", ssh_user: "goat" }
  network-nodes: []          # NETWORK-class bearer gear (ADR 1113)

operator_ssh_role_scopes:
  PLATFORM-ADMIN: [host-nodes, network-nodes]
  NETWORK-ADMIN:  [network-nodes]
```

A site network admin (ADR 1113) enrolls bearer gear — routers, switches, radios
— and gets a shell on that gear and **not** on the mgmt pair. Delegating network
administration is not delegating the platform. Before scopes existed this was
unexpressible: one flat list meant everyone with a key got every host.

**Install is role-scoped; revoke is the union of every scope.** The asymmetry is
deliberate. A cut that swept only the role we currently believe someone holds
would strand keys across a role change. Over-removing costs a re-request;
under-removing leaves a live shell after offboarding.

**Fail closed and loud.** A key that resolves to no host — unmapped role, or
every mapped scope empty — is refused with the groups, the mapped roles and the
resolved scopes named. It is never accepted into a silent no-op, and it is never
marked `installed`. On zagros-prod `network-nodes` is empty today because no
NETWORK node is enrolled yet, so a network admin's key is currently refused with
that explanation rather than quietly doing nothing.

Resolution lives in **one file**, `ops/ansible/tasks/operator-ssh-scope.yml`,
included by both the onboarding path and the installer. Two copies would drift,
and the drift would be silent: one path granting a shell the other would not.

### Undecided

Whether a network admin gets a shell on **pods** and on sensor-gateway nodes
is not decided. Absent from `operator_ssh_role_scopes` means not granted. Do not
add either without recording the decision — a pod shell is a different blast
radius from a switch console.

## One door per device class

There are **two** independent ways to get a shell on a mesh peer, and they share
nothing:

| | NetBird SSH | `authorized_keys` (this document) |
|---|---|---|
| who is allowed | mesh identity + ACL + per-peer `ssh_enabled` | a public key in a file on the host |
| grant | enable the flag, be in a permitted group | submit → admin approve → installer |
| revoke | remove from the group / disable the flag | Phase R1.6 removes the key line |
| record | the ACL | the console `ssh_keys` table |

**Decision (2026-08-08): one mechanism per device class, never both.**

- **Bearer gear (NETWORK class) — NetBird SSH only.** Appliances are a poor fit
  for `authorized_keys`: the account varies by vendor, the filesystem may be
  ephemeral. Identity-based access revoked by group membership is both simpler and more
  auditable here. `network-nodes` in `operator_ssh_scopes` therefore stays
  **permanently empty** — that is a decision, not a gap.
- **Host nodes — `authorized_keys` only.** `ssh_enabled` stays **off**.

**Why not both: it makes revocation lie.** Enabling each path on the same device
gives it two grant surfaces and two revoke surfaces. Cutting someone's key would
leave their NetBird SSH access untouched while the console reports `revoked` —
reintroducing exactly the false-access claim this whole track exists to remove.
If a device ever genuinely needs both, the revoke path must cut both in one
operation before that is allowed.

## Attribution on a shared account

The hosts use a shared `goat` account, but logins are still attributable:
sshd logs `Accepted publickey ... SHA256:<fp>` at INFO, and the console stores
the same OpenSSH SHA256 fingerprint against the person's handle. Joining the two
answers "who logged in" without per-person unix accounts.

## The parser is the security boundary

A key that gets past it becomes a line in `authorized_keys` on a production
host, so it refuses: a **private** key pasted by mistake (the one input that
must never reach the DB or an audit log), a body whose embedded type disagrees
with the declared one (which would write a line sshd rejects while the person
believes they have access), `ssh-dss` and other weak types, line-wrapped pastes,
and short or undecodable bodies.

## Expiry

Recorded (default 90 days, `SSH_KEY_TTL_DAYS`) and reported. **Enforcement is
deliberately not built:** recording costs nothing, but a sweep that silently
expires a shell mid-incident is a hazard that should be an explicit choice.

## Running the installer

The normal path is the CLI verb:

```bash
# Recommended: preview-only (no writes)
goat admin ssh-key install --env zagros-prod --check

# Real run: install every approved key
goat admin ssh-key install --env zagros-prod --yes
```

The preview and the real run are two reads of the queue: the real run acts on
whatever is `approved` at execution time, so a key approved between preview
and confirmation is installed too. That is deliberate — install is a
whole-queue operation, and every row it acts on has individually passed the
approval gate — but it is why the confirmation prompt says so out loud.

`--env` selects the goatnet; the CLI resolves the console's onboard API base
from the active context's `onboard_endpoint` and your personal onboard API
key from a per-env operator variable (for example
`ZAGROS_PORTAL_OPERATOR_API_KEY` for `--env zagros`). The cross-goatnet defense
is a **recorded binding, not a naming convention**: when the active
profile.toml carries `goatnet = "<slug>"`, that binding must equal `--env`
and the context name is irrelevant; a profile pinned to one goatnet can
never run another's install regardless of what the context is called. For
profiles that predate the key, the CLI falls back to requiring the context
NAME to equal the `--env` slug, and the refusal names the remedy: add
`goatnet = "<env>"` to the context's profile.toml (only if its
`onboard_endpoint` really is that goatnet's console). Either way a mismatch
is refused before Ansible starts. The installer reads the key
from the `ONBOARD_API_KEY` environment variable and never places it on
`argv`. The CLI also passes the repo root as a JSON extra-var
(`-e {"onboard_runner_repo": "<repo root>"}` — the key=value form would
split on any whitespace in the path) so the play resolves operator
manifests from the local checkout (`ops/operators/<env>/<handle>.yml`) —
the play's default manifest root is the mgmt-host runner path, which exists
on no operator laptop. When run without `--yes`, the confirmation prompt
requires a terminal (piped invocations fail fast instead of hanging) and
Ctrl-C at the prompt aborts with nothing installed.

Real runs emit `sshkey.install` operator-action audit events
(started / ok / failed). `--check` previews deliberately emit none: a
preview changes no host and no console state — it is a read of the queue —
and auditing reads would bury the events that record grants.

For break-glass or direct Ansible runs on the mgmt host, the underlying
playbook can still be invoked manually:

```bash
cd ops/ansible
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES \
  ONBOARD_API_KEY="$YOUR_ONBOARD_API_KEY" \
  ../scripts/ansible-preflight.sh \
    -i inventory/mgmt-hosts-mesh.ini operator-ssh-key-install.yml \
    -e onboard_env=zagros-prod \
    -e onboard_api_base=<console url>
```

Add `-e ssh_key_install_check=true` to see what would be installed and change
nothing. There is no default console URL on purpose: the container-internal
`http://goat-id-console:8080` resolves only inside the mgmt host's docker
network, and a plausible-looking default could point a run at another goatnet's
console.

Keys whose operator has no manifest, whose role resolves to no host, or whose
recorded env is not this run's goatnet (including legacy rows with no recorded
env), are skipped **loudly** and stay `approved` for the next run. One
unresolvable row must not block the others, and it must never be reported as
installed. The env skip is the per-row half of the cross-goatnet defense: the
CLI verb makes a cross-goatnet *run* unexpressible, and the installer refuses
to place any *row* outside its recorded env — without letting one such row
abort a queue earlier rows already landed from.

The installer reports those skips back to the console over the onboard API
(`POST /api/onboard/ssh-keys/{id}/skip-report`), with a closed enum reason
(`"no_manifest"` for a missing operator manifest, `"no_scope"` for a role that
resolves to no host, `"env_mismatch"` for a row recorded for another goatnet). The optional context fields (`env`, `groups`) are
audit-only and shape-checked to the same structured-fields-only bar — slug and
role-token patterns, never prose. The queue row renders that as canonical wording with
timestamp and actor — e.g. `skipped 2026-08-13 UTC by alice — no operator
manifest`. The annotation is display-only: it never changes the key's state.

It is cleared on install and on revoke — both mean the annotation no longer
describes anything true. It is deliberately **kept** across an identical
resubmit: nothing about the skip condition changed, and the likeliest way to
reach that path is a person who read "not yet placed on the hosts", assumed
their submission failed, and re-pasted the same key. Clearing there would blank
the reason for whoever triages the queue next. A changed key body is a
different row and carries no annotation anyway. The queue also refuses to
render an annotation beside any status other than `approved`, so a clear that
fails cannot leave a stale reason next to an installed key.

A reason this console binary does not recognise renders as `reason not
recognized by this console` rather than as nothing. Image pins are per goatnet
([ADR 1120](../adr/1120-release-graduation-ladder.md)), so a newer installer reporting
a newer reason to an older console is expected rather than anomalous — and an
empty annotation would be byte-identical to "never skipped", which is the false
signal this feature exists to remove. The raw value is never echoed.

## Surfaces

| what | where |
|---|---|
| submit your own key | `GET/POST /ssh-key` |
| approval queue | `GET /onboarding/ssh-keys` (sponsor-gated, not admin-gated — see the door note below) |
| decisions | `POST /ssh-keys/{id}/{action}` |
| machine API | `/api/onboard/ssh-keys[/{id}/approve\|reject\|revoke\|installed\|skip-report]` |
| store + parser | `ops/goat-id-console/internal/onboardstore/sshkeys.go` |
| scope resolution | `ops/ansible/tasks/operator-ssh-scope.yml` |
| install | `ops/ansible/operator-ssh-key-install.yml` |
| revoke | `ops/ansible/operator-onboard.yml` Phase R1.6 |

## Legacy backfill

Before the console's ssh-key flow existed, host shells were granted by editing
`authorized_keys` directly and no rows existed in `ssh_keys`. A one-time
backfill records those legacy keys so the install panel gate has something to
reason about and offboarding acts on a row instead of rediscovering keys.

The backfill helper (`ops/scripts/ssh-key-backfill.sh`) reads an
`authorized_keys` file and, for each non-comment line, prompts an operator to
confirm three things:

- the handle to attach the key to (kebab-case, validated);
- the login email for that admin; and
- whether the key currently belongs to an installed admin or to someone
  already offboarded.

The script writes two artifacts:

- a `.pub` file per key containing the **full authorized_keys line**
  (type, body, comment) so later revoke passes can remove the exact line —
  filenames carry a per-run counter (`<handle>-legacy-<n>.pub`) so a holder
  of several legacy keys gets one file per key, not one overwriting the
  other; and
- a `ssh-key-backfill.plan` shell plan of `goat admin ssh-key submit` commands
  with `--email` set to the admin's login email and a `legacy-backfilled` note
  (or `legacy-backfilled (already offboarded)` for offboarded holders).

Lines carrying authorized_keys options (`from="…"`, `command="…"`, `no-pty`,
…) are recorded verbatim but flagged with a warning: the console's parser
accepts plain `<type> <body> [comment]` lines only, so the plan's submit for
such a row is refused until the operator strips the options from the `.pub`
file (recording them in `--note`).

Operators review and run the plan manually — submit, approve, and install for
still-active admins; submit then revoke for already-offboarded holders.
"Install" here is `goat admin ssh-key install` — there is no direct
mark-installed verb. **The installer has a prerequisite the backfill cannot
waive:** it resolves scope from the handle's operator manifest
(`ops/operators/<env>/<handle>.yml`), and a legacy admin who predates
`goat admin onboard` has none — their row is skipped `no_manifest` on every
run and stays `approved` until their onboarding is finished (or a manifest
is authored for them). The backfill helper probes for the manifest at
plan-generation time and writes a loud `PREREQUISITE` block into the plan
for every handle missing one, so the dependency is visible before anyone
runs the commands. Once the manifest exists, running the installer over a
legacy key that is already on the in-scope hosts is safe by construction:
the authorized_keys write is an idempotent line re-place, so nothing changes
on disk and the row is marked installed. The
`state=offboarded` cases are **record-only** entries that grant nothing until
rejected or revoked; they exist so GH #371's remove-or-show decision can act on
a row instead of a rediscovery.

On the console side, `sshKeyHandleFor` first resolves the signed-in login
email to an onboarding proposal row via `ActiveProposalForEmail`. When no
proposal exists, it falls back to a legacy join keyed by the same login email:
`legacySSHKeyHandleForEmail` scans installed ssh-key rows for an unambiguous
email→handle mapping. The join carries the same trust scope as the install
panel gate — only installed rows recorded for **this console's goatnet**
count (a row minted for another goatnet must not vouch for a handle here),
and with no goatnet configured the join is disabled outright. Ambiguous
mappings are refused rather than guessed.

The install panel gate is keyed on that same login email: admins whose session
email has at least one installed key row for this console's goatnet see the
panel; other admins see only the static hint text. After the backfill, that
gate is uniform for both new and legacy admins and every shell-holding key has
a row for revoke/offboard to act on.

## How people find these pages

The mesh onboarding guide links both, so nobody has to be told a URL. The links
are DERIVED from the admin-console base rather than configured per goatnet —
`sshkey` and `sshkeyqueue` resolve to `<console>/ssh-key` and
`<console>/onboarding/ssh-keys` in the guide's link catalog, because a goatnet
cannot have these pages without having the console. A goatnet with no console
renders the words "not configured on this goatnet" instead of a dead link.

The guide splits the two audiences the same way this design does. The platform
admin path says submit your public key and names the three states, because the
gap between APPROVED and INSTALLED is where somebody concludes the system is
broken when in fact the installer has not run. The identity-admin path gets the
queue, and says plainly that approving does not install and that revoking
belongs in the queue rather than in a hand-edit of `authorized_keys`.

### The console door is not the same on every goatnet

`/ssh-key` is written for any signed-in person, but reaching it means getting
through the console's own oauth2-proxy first, and that door is per goatnet:

| goatnet | `id_admin_console_oauth2_proxy_allowed_groups` | who reaches `/ssh-key` |
|---|---|---|
| zagros | `""` (authenticated-only, deliberate) | anyone who can sign in |
| zagros-prod | role default `PLATFORM-ADMIN,IAM-ADMIN` | admins only |

Verified from the deploy output on both, 2026-08-08.

So on zagros-prod today, an operator who is neither platform nor identity admin —
a delegated network admin under [ADR 1113](../adr/1113-network-node-class-and-delegated-network-admin.md),
for instance — cannot submit their own key, and self-service degrades to asking
an admin. That is a door decision on the console rather than anything in this
design, and it is recorded here so it is chosen rather than discovered. The
guide's wording does not assert either door: it says that if the console
refuses you, that is the console door and not your key.

## Related

- [ADR 1113](../adr/1113-network-node-class-and-delegated-network-admin.md) — NETWORK node class, delegated network admin
- credential-revocation-and-offboarding.md
- [identity-access-and-onboarding-reference.md](identity-access-and-onboarding-reference.md)
