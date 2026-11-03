# Onboarding tier ↔ identity-grammar role model (intake roadmap)

**Status:** **decisions accepted + implemented 2026-06-29** (#1 Option A, #2 yes, #3 ADMIN+DEVELOPER, #4 platform/build/IAM-only, #5 deferred) — see "Decisions" below. The intake form now ships Developer / Browse / **IAM Admin** / **Platform Admin** presets; the deeper RBAC *enforcement* plumbing (#1) and delegated machine-enrollment (#5) remain named follow-ons.
**Related:** [operator-onboard-intake.md](operator-onboard-intake.md) ([ADR 1061](../adr/1061-operator-onboard-intake-system.md)) · identity-grammar.md (ADR 1043) · [admin-operator-guide](../operations/admin-operator-guide.md).

## BLUF

The intake form offers three tier presets (Developer / Browse / Operator). Reviewing them against the ADR 1043 role grammar surfaced four things, only one of which is a genuine open question:

1. **The "Operator — backend admin" preset uses the wrong grammar role.** In the grammar, `OPERATOR` is the **tactical actor** (mission), and `ADMIN` is the **backend admin**. The preset sets `netbird.auto_groups: [OPERATOR]` — granting the *tactical* role, not backend admin. It should be `ADMIN`. (Bug — fix, not a decision.)
2. **"Backend admin should read/write Zenoh for test/troubleshoot" is answered by role composition.** The grammar's roles compose; its own worked example is *"Platform-eng lead = `ADMIN` + `DEVELOPER`."* `DEVELOPER` brings the Zenoh data slot, so a backend admin who tests the fabric is **`ADMIN` + `DEVELOPER`**, not a special case. (Resolved by the grammar.)
3. **There is no tier for an Identity & Access Administrator** — manage identity/access (NetBird + Ory + Zenoh ACL: provision/rotate/revoke/edit) + read security diagnostics, *without* full platform-admin and *without* developer fabric access. The grammar has `ADMIN` (full) and `MONITOR` (read-only obs) but nothing scoped between them. (The one **open ADR 1043 question** — §4.)
4. **There is no way to register a person *plus* their standing authority to enroll machines later, late-bound** (the delegated-enrollment-grant idea). (Separate capability — §5.)

## The grammar (reference — see identity-grammar.md §2, do not duplicate)

| Family | Role | Capability |
|---|---|---|
| Platform | `ADMIN` | full backend — mgmt server, ACLs, dashboards |
| | `MONITOR` | read-only platform observability (grafana/openobserve/minio); **no mgmt** |
| Build | `DEVELOPER` | Zenoh pub/sub + portal + dev web surfaces |
| Mission | `OPERATOR` | tactical actor — sensor/effector/C2/data-process |
| | `OBSERVER` | passive read of mission data + surfaces |

Roles are **orthogonal and composable**. A tier preset is a *bundle* of (role group(s) + Ory service groups + Zenoh access + out-of-band operator credentials).

## Proposed tier model

Each row = one intake-form preset. "OOB creds" = the operator-enablement ceremony the manifest can't grant (see §6).

| Tier (preset) | Grammar role(s) → `netbird.auto_groups` | `ory_site.groups` | Zenoh | OOB creds | Notes |
|---|---|---|---|---|---|
| **Developer** | `DEVELOPER` (+ `OBSERVER`) | dev web surfaces | own data slot (`<anchor>/**` r/w) | — | the common engineer tier (unchanged) |
| **Browse** | `OBSERVER` | read-only web surfaces | none | — | web surfaces only (unchanged) |
| **Platform Admin** *(was "Operator — backend admin")* | `ADMIN` (+ `DEVELOPER` for fabric test) | full consoles (`ADMINS`/…) | **via `DEVELOPER`** — own data slot for test pub/sub; `ops/**` control via the operator leaf | NetBird PAT · portal operator key · Kratos admin · `age` audit roster | true backend super-user; fix the grammar group `OPERATOR`→`ADMIN` |
| **IAM-Admin** *(new — §4 open)* | `MONITOR` (+ scoped IAM role?) | IAM consoles + diagnostic **read-only** (`GRAFANA-READONLY` / `prometheus-readonly` / `openobserve-readonly` / alertmanager) | none (no dev slot) | NetBird PAT · portal operator key · Kratos admin · `age` audit roster (scoped to IAM ops) | manage identity/access + security-monitoring read; **not** full platform, **not** developer |
| *(future)* **Tactical Operator / Observer** | `OPERATOR` / `OBSERVER` (mission) | — | mission surfaces | — | the genuine mission roles; the intake form models none today — flag whether intake should |

### §3 fix — Platform Admin Zenoh test access (resolved)

The backend admin's "read + write Zenoh for testing" decomposes cleanly:
- **Read** — already broad (flat community-read in the slot-pool model); subscribe-to-diagnose works today.
- **Test publish (data topics)** — `ADMIN` + **`DEVELOPER`** gives the admin their own data anchor (`<anchor>/**`) to publish probes; this is the grammar's own composition pattern, not a bespoke grant.
- **Control writes (`ops/**`)** — stay the operator Zenoh leaf (CN `operator-trial-peer`) from the operator bundle.
- *Optional:* a sanctioned `test/**` (or the existing isolated `loadtest/**`) namespace the operator leaf may write, if we want test traffic confined to a known prefix the audit/storage tiers treat specially. Secondary to the ADMIN+DEVELOPER answer.

## §4 — The open ADR 1043 question: how to represent IAM-Admin

IAM-Admin's capability = **manage identity/access** (the `goat admin onboard/rotate/revoke` machinery across NetBird + Ory + Zenoh ACL) + **read security diagnostics** (anomaly/flood/usage dashboards — a second set of eyes), explicitly **excluding** full platform-admin and developer fabric access. The grammar has no role between `MONITOR` (read-only) and `ADMIN` (full backend). Two ways to land it:

- **Option A — composed tier, no new role (recommended first).** IAM-Admin = `MONITOR` (the diagnostics-read half — already exists) + a **new narrow Ory service group** (`IAM-ADMIN`, gating the identity consoles: netbird-dashboard / id-admin-console / portal admin) + membership in the **operator roster** (the credentials that actually grant the mutate capability — NetBird PAT, portal operator key, Kratos admin, `age`). No grammar change; the IAM-mutate capability rides the *operator credentials + Ory group*, not a NetBird/cert role. Lighter, ships behind the existing grammar.
- **Option B — new role-family value** (e.g. `IAM-ADMIN` / `SECURITY-ADMIN`) in ADR 1043. Makes the capability first-class and cert-borne (URI-SAN `role=`), but is a real cutover cost (cert grammar + NetBird groups + Ory claims + the §8 gated migration), and the IAM-mutate capability is *operational credentials*, not something a Zenoh cert needs to assert. Hold this for if/when IAM-admin authority must be enforced *on the wire*.

**Recommendation:** Option A. Resolve via an **ADR 1043 note** (not a new role family) + an `ory_site.groups` `IAM-ADMIN` value, unless someone needs the IAM-admin capability to ride the cert. This also disentangles the existing tier: "Platform Admin" becomes honestly full-platform, "IAM-Admin" the narrower one.

## §5 — Delegated machine-enrollment grant (separate capability)

Register a **person** + their **standing authority + quota to bring machines/processes online later**, with per-machine specifics **bound late** (nobody knows the full inventory at registration). The mesh layer already does this (`netbird.usage_limit: N`); the gap is the *identity/data-fabric* layer (today one Zenoh identity per person; machines publishing on it are indistinguishable on the wire). Shape: an `enrollment_grant` on the person (quota + inherited affiliation/echelon/site) + a constrained late-bound self-service mint that records a **person→system custody edge** (custody, not ownership). Composes with the identity-descriptor standard (custody edges), the slot pool (on-demand allocation), and the SPIFFE/moon-pod self-enrollment ergonomics — does not duplicate them. **Specced and built** in machine-enrollment.md ([ADR 1096](../adr/1096-delegated-machine-enrollment.md)) — each machine now gets its own slot + cert, closing the wire-indistinguishability gap above.

## §6 — In-band (form) vs out-of-band (operator-enablement) split

The intake form can grant the **in-band** half of every tier: the grammar role groups, the Ory service groups, the Zenoh slot. It **cannot** grant the **operator credentials** that make Platform-Admin and IAM-Admin actually able to mutate identity — the NetBird API token, the portal operator key, Kratos admin access, and the `age` audit-roster entry are handed out per the [admin-operator-guide](../operations/admin-operator-guide.md), not minted by a manifest. So onboarding an admin tier = *form sets the groups + diagnostics access; then the operator-enablement ceremony grants the credentials.* The form should say so explicitly for those tiers (and ideally link the ceremony).

## Decisions (accepted 2026-06-29)

1. **§4 — IAM-Admin representation: Option A (composed), implemented.** A new Ory **`IAM-ADMIN`** service group (in `ops/permissions/group-permissions.yml` + the schema `ory_site.groups` enum, drift-guarded by `check-ory-groups-enum.sh`) gating the IAM consoles + diagnostics-read; mesh role stays `MONITOR`; the mutate capability rides operator-roster credentials. **No new grammar role-family.** *Follow-on (not done):* the L7 oauth2-proxy fences for netbird-dashboard / id-admin-console / portal currently allow only `ADMINS` — they must add `IAM-ADMIN` for the group to actually open those consoles (deploy-side; the group declares intent today).
2. **Tier rename: done.** "Operator — backend admin" → **"Platform Admin"** (`ADMIN`), freeing "Operator" for the tactical mission role.
3. **Platform-Admin fabric test: `ADMIN + DEVELOPER`, done** (the preset enables a Zenoh slot). The optional sanctioned `test/**` namespace on the operator leaf is **not** done — secondary.
4. **Mission tiers: scoped out.** The intake form stays platform/build/IAM (Developer / Browse / IAM Admin / Platform Admin). Tactical `OPERATOR` / mission-`OBSERVER` presets are **not** added — revisit if this form starts onboarding mission identities.
5. **Delegated machine-enrollment (§5): deferred** to its own spec — the *person* half is now specced in [three-stage-candidate-onboarding.md](three-stage-candidate-onboarding.md) (decisions locked, ready to build); the machine self-service mint is a downstream follow-on of that.

**Implemented in this change:** the four form presets above + the `IAM-ADMIN` Ory group (SoT + schema enum + embedded re-sync); both admin presets lint valid against the live linter. **Verification note:** the linter's `classify_shape` still reports `IAM-Admin` manifests as the `browse` *shape* (MONITOR isn't modeled there; it's display-only + shared with `validate.py`), so the form's verdict shows the **selected tier name** rather than the classify-shape. A shape-taxonomy update (Python + Go + parity) is optional future polish.

**Remaining follow-ons:** the #1 L7 fence wiring; the #5 delegated-enrollment spec; optionally the `test/**` namespace (#3) and the classify-shape polish.
