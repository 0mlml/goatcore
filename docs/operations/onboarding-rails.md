# Onboarding rails — there is one correct way, and going around it breaks people

**Status:** written immediately after the author did the wrong thing and had to undo it.
This document exists because the wrong path is *easy, fast, and looks like it worked*.

**Two kinds of principal, two rails, one rule.** People are below; machines have the
same rule and a different mint — [§ Machines have their own rails](#machines-have-their-own-rails-and-the-same-rule).
Read the half you need; the failure mode is identical and so is the answer.

## The rule, for people

**Every human principal is created by the three-stage candidate flow — never by hand,
and never by the retired enroll leg.**

**There is ONE live front door** (ADR 1062). It is the id-admin console
(`admin.auth.<site>.<zone>`), reachable from the CLI as `goat admin invite`:

```bash
goat admin invite --env <goatnet> …          # the console mints a one-time invite
# the candidate accepts in the console; a sponsor vouches; an admin admits
# onboard-runner claims the proposal and mints every surface
ops/scripts/sync-onboarded-manifests.py --env <goatnet> …   # the roster catches up
```

**`ops/operators/<env>/<handle>.yml` is an OUTPUT, not an input.** Under this flow the
runner mints first and `sync-onboarded-manifests.py` writes the manifest *afterwards*,
so the declared roster agrees with what actually happened. Every generated file says so
in its own first line.

Addressed invites are refused before creation or delivery when the email already
has an active onboarding record. The error names the existing record and its
stage. Later sends and email exports check the current recipient again. Use the
existing record for recovery; an archived record requires an admin restore.

> ### `goat admin onboard` is the retired leg. Do not reach for it.
>
> Its enroll half is retired; only its **revoke** leg remains live. A successful run is not
> permission.

**Anything else is a breaking action.** Specifically, do NOT:

- POST to the Kratos admin API to create an identity
- INSERT into the NetBird database (`ops/ci/check-netbird-user-sql.sh` already refuses this
  for the `users` table — the same reasoning applies to every other principal row)
- mint a setup key by hand and mail it to someone
- create a portal org row, or a slot, outside the portal's own API

## Why — a principal is a set of facts across FOUR planes

Onboarding is not "make a login." One principal simultaneously needs:

1. **Ory (Kratos)** — the identity, its traits, its groups, its credentials
2. **NetBird mesh** — a setup key with the right `auto_groups`, so the peer lands in the
   correct ACL groups when it enrols
3. **Zenoh fabric** — where applicable, a portal-minted per-principal cert whose URI-SAN
   carries the frozen per-goatnet identity strings
4. **The delivered bundle** — the artifact that tells the human what they have and how to
   use it, and which is the ONLY record of several one-time values

Hand-creating any one of those produces a principal that exists on one plane and not the
others. That state is worse than no principal at all, because it *looks* provisioned.

## The rail the person can step off, on their own machine

Everything above is about an operator going around the orchestrator. There is one
equivalent on the joiner's side, and it has the same signature — *easy, fast, and it
looks like it worked*.

**Installing the mesh client inside the Windows Subsystem for Linux enrols WSL, not
Windows.** WSL runs in a lightweight VM with its own network stack, so a client inside it
does not carry the host's traffic. The peer registers, reports `Connected`, and appears
healthy on the admin dashboard — while the person's Windows browser cannot open a single
mesh service. **An operator-visible success that is a user-visible failure**, which is why
it survives triage: whoever is helping looks at the dashboard, sees a healthy peer, and
concludes the problem is elsewhere.

It costs twice. The enrolment also spends a device off the person's setup key, so the
native Windows client is then refused with `couldn't add peer: setup key is invalid` —
and that error reads like a credential problem rather than the consequence of installing
in the wrong place.

| | |
|---|---|
| **Tell** | Peer `os` is a Linux distribution and `kernel_version` contains `microsoft-standard-WSL2`, usually on a Windows-shaped hostname |
| **Ask the person** | Open a mesh-only service in your normal Windows browser. Fails while `netbird status` inside WSL says Connected → this is it |
| **Fix** | Install the **native Windows client**; delete the WSL peer; confirm the key has a device left, or issue a fresh one |

Why it keeps happening: the enrolment instructions group macOS, Linux and WSL together,
because for those three the commands genuinely are the same. They are the same *command*
and a different *machine*. The joiner-facing surfaces now say so directly — the welcome
mail, the bundle, `onboarding.md` and the training modules — and the
Windows section is placed ahead of the Linux one so a Windows reader meets the right
instructions first.

## The gate that catches it, and what it means when it fires

`operator-onboard.yml` **Phase 5.5 — Bundle-completeness gate** refuses to write a bundle
when a surface the manifest enables did not provision:

```
Bundle is INCOMPLETE for enabled surface(s): Ory login.
A surface the manifest enables did not provision, so the bundle ... would be delivered
half-broken. Do NOT deliver it.
```

**If you see this, do not work around it.** It is doing its job. The most common cause is
exactly the mistake this document exists to prevent: *the principal already partially exists*
because somebody created it out-of-band, so the orchestrator's create step no-ops and the
bundle would ship missing that surface.

**A failed mint has a supported retry — use it instead of touching the database.**
`failed` used to be terminal in both the console and the API, so the only way
forward was a hand-written `UPDATE` against the console's SQLite store. That is
the out-of-band surgery this document exists to forbid, and the author of this
document did it because nothing else existed. Now:

- console: a **Retry provisioning** button on a failed candidate;
- API: `POST /api/onboard/proposals/{id}/retry` (operator principals only).

Retry re-runs the **mint**, never a decision: both human gates were already
passed to reach `failed`, and the store admits only rows currently in that
state — so it cannot resurrect a rejected candidate, interrupt a mint in
flight, or repeat a completed one. It is audited as `onboard_retry_proposal`.
Read the failure note first: if the cause is still present, the retry fails
the same way.

Restore and retry validate the stored provisioning manifest before requeueing.
An incomplete historical record, including an empty `{}` manifest, is refused
without changing its state or credentials. No supported repair path exists yet,
so do not hand-edit the record; raise it as an issue.

Welcome email, resend, and download need a mesh enrollment key unless the stored
manifest says `netbird.enabled: false`. A manifest that is empty, unreadable, or
has no `netbird` object is treated as needing a key, so a keyless package is held
and a package that carries a key is delivered. To supply a key, send it as
`setup_key` with a welcome resend. The console saves it even when email is not
configured, and the download then carries it.

**For revocation, the remedy is OFFBOARD, not hand-editing `onboard.db`.**
An already-onboarded proposal used to have no on-rails way to be cut: the only
way to free the duplicate-email guard or stop the console treating it as live
was a hand-written `UPDATE` against the onboarding store. Now:

- console: a **Mark offboarded** action on an onboarded, enrolled, or archived candidate;
- API: `POST /api/onboard/proposals/{id}/offboard` (operator principals only).

Offboard marks the proposal `offboarded` and records the operator who did it,
without deleting the row or its sponsor/approver tier. Leaving the note field
empty preserves the note already on the row (an archived row's archive
reason); typing a note overwrites it. A fresh invite for the
same email is then accepted instead of refused by the duplicate-email guard,
and the console's audit still shows who brought the person on and who later
cut them.

**Repair path:** clear the partial server-side state for that principal (revoke, and verify
the NetBird peer / Kratos identity / portal org row are actually gone), then re-run the
orchestrator so every enabled surface mints together in one render.

## A standup, as a worked example of the wrong way

(omitted)

If you need a roster to *exist* before people are onboarded — a legitimate need, e.g. to plan
groups — write the manifests (`ops/operators/<env>/<handle>.yml`) and validate them. That is
the supported way to have a roster on paper without half-creating anyone.

## Machines have their own rails, and the same rule

**Every machine principal is minted by the portal, against its custodian's standing
allowance — never by a hand-assigned joining key.**

The machine failure has the same shape as the human one: hand-picked `auto_groups` on
one-off keys. Reusability is not the vector; **who chose the groups** is.

Four doors, all driving the same derivation. Detail, and which class to pick, in
[`gear-enrollment-sop.md`](gear-enrollment-sop.md) — this page is only the rule:

| Door | Use it for |
|---|---|
| `goat machine enroll <handle> --node-class <CLASS> --autonomy <…>` | the normal enrolment, custodian self-service against their quota |
| `goat machine mesh-key <handle>` | the join window lapsed before the box joined — quota-neutral re-mint from the born-as class |
| `goat machine enroll --bulk <file>` | N machines, one custodian, one act — N one-off keys, never one reusable key |
| `bundle-create --from-machine <anchor>` | the box is offline at issuance; groups still derived from the registry, not typed |

The allowance itself is one operator act, once per custodian:
`goat machine grant <custodian> --quota N --affiliation <unit>`. Grant it at onboarding.
A custodian sitting at zero when the gear arrives is the condition that produces
everything this section forbids.

**The exit is a door too.** `goat machine revoke <handle>` cuts the peer and any unconsumed
key first, then releases the identity, and returns the quota unit. Deleting a peer in the
NetBird console instead leaves the registry saying the machine is live and the custodian's
allowance still spent — the same one-plane-not-the-other state the human half describes,
pointing the other way. (Needs goat-cli ≥ 0.4.40 and a portal carrying the endpoint; an
older portal answers 405 and the CLI says so.)

**Anything else is a breaking action.** Specifically, do NOT:

- mint a NetBird setup key and choose its `auto_groups` by hand for gear
- issue a **reusable** key to gear — banned outright, including under outage
- pick a class by what the box *is* rather than what it *does*; a box doing two jobs is
  enrolled twice, once per job
- hand-type a gear-class group into `bundle-create --acl-groups` (refused against the
  taxonomy; infra and site groups stay hand-supplied)
- leave a joined peer on its factory hostname — the peer name IS the join key to its
  custody record

### Why — the same shape as the human case

A hand-assigned key produces a mesh peer and nothing else. It skips, silently:

1. **The machine's own fabric identity** — its Zenoh cert and slot, stamped with
   node-class and autonomy. Without it the machine publishes as its custodian and is
   indistinguishable on the wire from every other machine they own.
2. **The custody edge** — the person→system `enrolled_by` record. There is no mint
   event to record it at, so nothing ever says who brought this box on.
3. **The derived groups** — `MeshGroupsForMachine` turns the class into exactly the
   groups that class may hold. A human picking them is that shape.

As with a hand-made identity, the result *looks* provisioned. It is on the mesh, it
passes traffic, and the gap only surfaces when somebody asks who owns it or why it can
reach something.

### Break-glass, and what it costs

If the portal is down during a gear standup: an admin hand-mints a **one-off, single-use**
key carrying exactly the groups the class derives (read them from a prior enroll response
or from `ops/ansible/netbird_acl_taxonomy.yml`), and retro-stamps the machine with
`PUT /api/slots/{anchor}/custody` at next portal availability. Reusable keys stay banned.

The guard below reports that peer until the stamp lands. **That is the desired behaviour,
not a failure** — the exception is visible for as long as it is real.

### The gate that catches it

`ops/ci/check-node-class-membership.sh`, provenance stage. It joins live NetBird
membership to the portal registry and grades every peer holding a gear-class group as
`aligned` / `pending-reconcile` / `violation` / `unknown`. `unknown` is never counted as
aligned — a portal it cannot read is reported, not assumed.

```
NB_API_URL=… NB_API_TOKEN=… PORTAL_URL=… PORTAL_OPERATOR_KEY=… \
  PROVENANCE_ENFORCE=1 ops/ci/check-node-class-membership.sh
```

It is **not** in PR CI and cannot be:
it needs live mesh credentials, and a check that passes because it could not see the
truth is worse than no check.

**Read a violation carefully before treating it as a side door.** The join key is the
peer NAME, so a peer renamed off its handle grades as a violation with its paperwork
perfectly intact. When any registry handle matches no peer at all, the guard names those
handles under `LIKELY CAUSE — name/handle mismatch, not a side door`. Rename the peer;
never allowlist it. Only a violation with no unmatched handle is a real side door.

Scheduling the guard is proposed in
`machine-provenance-guard-scheduling.md`
(ADR 1130).

## Adding a new goatnet to the rails

`operator-onboard.yml` carries several per-site maps near the top of the file. A new goatnet
needs an entry in **each** of them, or the run fails partway with an unhelpful templating
error (`object of type 'dict' has no attribute '<site>'`):

| Map | What it carries |
|---|---|
| `netbird_site_env_var_map` | which env var holds that site's NetBird PAT |
| `netbird_site_url_map` | the site's mgmt-API base URL |
| `ory_site_kratos_admin_host_map` | SSH delegate for that site's Kratos admin API |
| `ory_site_zone_map` | the site's public DNS zone |
| `services_host_suffix_map` | `<service>.<suffix>` for mesh service names |
| `goat_cli_ca_id_map` / `goat_cli_ca_key_path_map` | **that goatnet's OWN CA** — never another's |

**And four more the runner needs, which now come from CONFIG rather than a
release.** They were hand-copied Rust match arms in
`crates/goat-cli/src/cmd/admin.rs`, which is why a new goatnet could not onboard
anyone until a `goat-cli` release shipped. `goat` now prefers whatever
the active context's `profile.toml` declares and falls back to that table, and
the `onboard-runner` role renders the values from the group_vars below — so a
correctly-configured goatnet needs no CLI change at all:

| Table | What it carries | Source of truth it mirrors |
|---|---|---|
| `native_router_endpoint` | the goatnet's Zenoh router endpoint | `portal_router_endpoint` |
| `native_netbird_config` | mgmt URL, PAT env var, ACL group prefix, rosenpass | `public_mgmt_fqdn`, `netbird_acl_taxonomy`, `rosenpass_enabled` |
| `native_ory_config` | SSH host + user, auth URL | `mesh_ip_a`, `site_dns_zone` |

Every value already exists in that goatnet's `group_vars`, and the role's
defaults derive each one from it, so the normal case is **nothing to do**.

Add a goatnet to the built-in table only when it must work from a laptop with no
profile for it — and then add it to `NATIVE_GOATNETS` in the same file, because
the guard test reads that const. That const used to carry its own separate list,
which is how it passed green while all three tables were missing a goatnet.

**Two runner facts the role now handles, listed so a failure is recognisable:**
the runner container cannot resolve the goatnet's public mgmt FQDN (it resolves
on the host to `127.0.0.1`, which inside a container is the container), so the
role renders `extra_hosts` pointing it at `host-gateway`, the host, where traefik
publishes :443. Never at a docker bridge IP (the runner log and the proposal note
both clip the error; the unclipped line is in the container's
`~/.goat/audit/operator-actions.log`). And `ROUTER_ENDPOINT`
must reach the runner or the per-person operator-action audit falls back to a log
file inside the container. Both are asserted by `run-acceptance.py`
(`onboard-runner-reaches-mgmt-api`, `onboard-runner-audit-reaches-the-fabric`);
run the suite against a goatnet after every runner converge, not only at standup.

**The management-API path fails closed at four layers.** A wrong
address cannot land silently again, because each layer that can see the path
refuses on it:

| Layer | Where | What it does when the address is wrong |
|---|---|---|
| CI | `ops/ci/check-no-docker-bridge-ip-pins.sh` (repo-guards) | refuses a literal docker-bridge address in any shared deploy artifact |
| Deploy | `roles/onboard-runner/tasks/main.yml`, after bring-up | curls `https://<public_mgmt_fqdn>/api/groups` from inside the container; anything but 200/401/403 fails the play and names the mapped address. Every converge path runs the role: ansible, `deploy-component.sh`, fastlane, `rc.sh` |
| Runtime | the runner, every poll with approved work | probes the same URL before claiming; unreachable means nothing is claimed, proposals stay `approved`, one ERROR line names the URL. A claim never turns a deploy fault into a `failed` row |
| Health | the container healthcheck (`onboard_runner_healthcheck_enabled`) | carries the same probe; `unhealthy` within three minutes, and `IdentityPlaneServiceUnhealthy` fires |

A release candidate for the runner or its role must pass
`onboard-runner-reaches-mgmt-api` on dev (`ops/validation/flow-map.yaml`,
flow `onboarding-review-and-mint`). A mint that still fails records goat's
terminal `error:` line in the proposal note, not the warnings before it.

That last row is load-bearing: a new environment must get its own CA id and key path.

**The onboarding host must be a mesh peer of the target goatnet** — the Kratos admin API is
bound to localhost on the mgmt host and is reached by SSH over the mesh. Enrol the operator
machine on that goatnet first (`netbird profile add <goatnet>` keeps profiles switchable).

## A federated sovereign partner is NOT on these rails, and that is the point

Everything above is for a **principal inside our planes** — a person or a machine whose identity
we mint, in our Ory, on our mesh, against our certificate authority. The rails exist because
stepping off them leaves someone half-provisioned across four planes.

A **federated sovereign partner is none of those things**, deliberately. They run their own
environment, their own certificate authority and their own identity plane. There is nothing for us
to mint, no proposal to claim, and no surface for `onboard-runner` to create. Putting one through
the candidate pipeline does not produce a degraded partner — it produces a principal in *our*
planes wearing a partner's name, which is the opposite of what was agreed.

**Ask one question to tell them apart:** *must they own their own identity plane and certificate
authority?*

| Answer | What they are | What you do |
|---|---|---|
| No | A **pod** — a node inside our mesh, on a key we issue, holding our certificates | These rails, unchanged. [ADR 1092](../adr/1092-moon-pod-peer-classes.md) |
| Yes | A **federated sovereign** | Not these rails. `cross-sovereign-federation.md` |

**A pod is the default and federation is the exception.** Most partners are pods and should be.
Reach for federation only when owning those planes is the point of the arrangement rather than an
implementation detail — an ally standing up an environment they own is the case it was built for.

The federated path has its own bootstrap, and it deliberately shares nothing with these rails: an
out-of-band exchange of two values, a signed half from each side composing to one digest both
compute independently, one router process and one trust store per partner, and a name **derived**
from the pair of sovereign ids rather than issued by us. Nobody is enrolled, because there is
nobody of ours to enrol.

> **The failure to watch for.** Someone reads "onboard the partner", finds this page because it is
> the canonical one, and runs the three-stage pipeline against a sovereign counterparty. It will
> appear to work. What it produces is an account in our Ory and a certificate from our authority —
> exactly the dependency the partner federated in order not to have.

## Related

- `cross-sovereign-federation.md` — the case that is deliberately not on these rails.
- `federating-across-sovereign-boundaries.md` — the same material for a partner or an accreditor.

- `operator-onboard.md` — the runbook for a single onboard
- [`admin-operator-guide.md`](admin-operator-guide.md) — operator-facing guide
- [`identity-access-and-onboarding-reference.md`](../design/identity-access-and-onboarding-reference.md) —
  cross-plane ground truth; read before inferring how the planes relate
- [`three-stage-candidate-onboarding.md`](../design/three-stage-candidate-onboarding.md) — ADR 1062 flow
- [`operator-onboarding-collision-safety.md`](../design/operator-onboarding-collision-safety.md) —
  ADR 1048, email-keyed pre-mint collision check

Machine half:

- [`gear-enrollment-sop.md`](gear-enrollment-sop.md) — doing it: which class, what each
  refusal means, and the two-halves procedure when gear beats the site's groups
- `machine-enrollment.md` — ADR 1096 design canon; the
  mint's mesh half, the four doors, the revoke exit
- `machine-provenance-guard-scheduling.md` —
  ADR 1130 (proposed), running the guard on a schedule instead of by memory
- ADR 1116 — the gear-class vocabulary the
  mint and the mesh taxonomy share
