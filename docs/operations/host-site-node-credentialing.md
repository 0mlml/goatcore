# Credentialing a host-site node, by role

**What this covers:** which secrets, certificates and shell access each of the
four host-site roles needs — core-a, core-b, obs-a, obs-b
(ADR 1141) — and which of them
must be **copied** from the existing site rather than generated.

**What this does not cover:** the per-node build sequence itself. That is
`node-addition.md` §2 (what every node needs, in order) and
§3 (founder-or-joiner). This page is the credential half of §2 step 4, expanded
per role, because the four roles do not need the same things and nothing else
in the tree says so.

**Status of the shape itself:** four-node is accepted and **not built on
hardware**. Read
[§"Readiness"](#readiness--measured-not-asserted) before planning around it.

---

## 1. The rule that governs everything below

A node joining an existing goatnet **copies** cluster-shared secrets and
**generates** per-node ones. Getting this backwards is not a typo — it produces
a node that looks healthy and cannot participate:

| Class | Behaviour if wrong | Examples |
|---|---|---|
| **Cluster-shared — copy** | generate a fresh value and the node silently fails to join: Spock will not replicate, NetBird cannot decrypt existing rows, Ory issues tokens the other nodes reject | `ZAGROS_NB_ENCKEY`, `ZAGROS_REPLICATOR_PASS`, `ZAGROS_RELAY_SECRET`, `ZAGROS_ORY_SHARED_NID`, `ZAGROS_KRATOS_CIPHER`, `ZAGROS_HYDRA_SYSTEM` |
| **Per-node — generate** | copy a value and two nodes share a credential; a compromise or rotation on one forces the other | `*_PG_PASS`, `*_BKP_KEY` |

`newsite-secrets.sh` generates values for a **new fabric**. Its own header
carries this warning. A node joining an existing mesh, Spock cluster or identity
cluster does not run it for the shared class.

---

## 2. Per-role credential requirements

Roles are from ADR 1141 §Decision.
"Copy" means from the site's existing active node.

### core-a — the active control plane

Runs netbird-mgmt, signal, Postgres/Spock, Ory (Kratos + Hydra), Traefik, the
Zenoh router, and the rest of the ADR 0826 mgmt inventory.

| Class | What it needs |
|---|---|
| Cluster-shared (copy) | all six from §1 |
| Per-node (generate) | Postgres password, pgBackRest key (`ZAGROS_BKP_KEY`) |
| Identity-plane service secrets | Kratos cookie + cipher, Hydra system + cookie, self-service UI cookie + CSRF |
| Per-surface OIDC + proxy pairs | dashboard, isitup, MinIO, microscope, netvis, panoscope, relay-health, consistency-console, id-admin-console — each a client secret **and** a cookie secret |
| Machine tokens | NetBird API token, NetBird reconcile token, portal operator API key, onboard-runner portal key, id-console portal key, onboard notify token, provenance-guard pair, decode-health portal key, enrollwatch token |
| Fabric | slot-pool salt, signal federation token, relay metrics bearer |
| Storage | MinIO root user + password |
| Third-party | Cloudflare API token, SES SMTP username/password + API secret, GHCR read token, PagerDuty key, Slack/ntfy webhooks |

They resolve at
play time from `.envrc.local.d/` per the credentials table in
`CLAUDE.md`; none are committed.

### core-b — the warm standby control plane

Same stack, dormant except the standby-safe classes in
[ADR 1111](../adr/1111-cutover-gated-service-class-for-standby-nodes.md) and
`ops/standby/service-classes.yaml`. Promotion is operator-fired — VRRP was
removed, so nothing promotes it automatically.

**It is a Spock cluster member and a full mgmt-stack node.** Dormant describes
which services answer, not which credentials it holds. It therefore needs:

- **every cluster-shared secret** — it replicates, so `ZAGROS_REPLICATOR_PASS`,
  `ZAGROS_NB_ENCKEY`, `ZAGROS_RELAY_SECRET` and `ZAGROS_ORY_SHARED_NID` are not
  optional
- **its own** Postgres password and pgBackRest key
- the identity-plane service secrets, so a promoted node can issue sessions

### obs-a — active observability and storage

Observability, storage, and the fleet-utility workloads from ADR 0826's
2026-05-06 amendment (in-theater Ansible bastion, apt and OCI pull-through
caches, in-fleet Forgejo, dev-CA signing host, operator IdP, local inference).

| Class | What it needs |
|---|---|
| Alerting | PagerDuty key, Slack webhook, ntfy webhook, dead-man webhook |
| Storage | MinIO root user + password, **if** MinIO lands here |
| Utility workloads | whatever the amendment's services carry — the caches, Forgejo, and the dev-CA signing host each have their own; none are declared on `zagros_cloud_obs_hosts` today |

### obs-b — the standby observability partner

Dormant per ADR 1111. Its one active job is being the **MinIO replication
target** — the property that restores ADR 0811's two-copy audit durability and
closes F-048.

A replication target is a receiver, not a second writer, which is why it is
standby-safe by `ops/standby/service-classes.yaml`'s own test. It still needs
**credentials the source can authenticate to it with** — a MinIO root pair at
minimum, plus whatever the replication user is.

---

## 3. Certificates

Separate from secrets and easy to miss, because the failure is silent-ish and
the damage is permanent.

Each site mints its leaves from a manifest:

```
ops/scripts/mint-site-certs.sh <site-id>
# default manifest: ops/enrollment/manifests/<site-id>-certs.yaml
```

The script is idempotent and mints only the gaps, so re-running it after adding
a node's leaves is the normal path.

**A new site has no manifest and the script fails closed.** Its own error text
says to start from `TEMPLATE-certs.yaml`, and says why the obvious shortcut is
wrong: URI SANs are freeze-critical identity (Class A,
`instance-variability-model.md` §1).
Copying `zagros-prod-certs.yaml` mints certificates that claim to **be**
zagros-prod. A cert is not re-labellable after issue.

---

## 4. Shell access is its own credential, and admin tier does not grant it

Being a platform admin on the goatnet does **not** confer the ability to build a
node. Those are different planes and it is worth stating because the tiers sound
like they should overlap:

| Capability | Where it comes from |
|---|---|
| Mesh access, web-surface admin | the onboarding tier (`PLATFORM-ADMIN` in the Ory group vocabulary, granting admin on id-admin-console, portal, netbird-dashboard, netbird-mgmt-api per `ops/permissions/group-permissions.yml`) |
| **Host shell** | [`ssh-key-self-service.md`](../design/ssh-key-self-service.md) — the person submits their own public key, an admin installs it. One door per device class; never two |
| **Cloud-plane actions** (instances, SSM, buckets) | AWS credentials via `aws-vault`, in the OS keychain, never in an env file |
| **The site's secrets** | `.envrc.local.d/`, gitignored, per the blast-radius split in `CLAUDE.md` |

A newly-minted platform admin has the first row and none of the others. Plan the
other three deliberately for anyone expected to build or operate a node.

---

## 5. Readiness — measured, not asserted

Run this before planning a build. It reports what a group's config surface still
owes, and works before `group_vars/<group>/` exists:

```
ops/scripts/standup-config-preflight.py <group> --scaffold-plan
```

---

## 6. Order of operations

Credential work interleaves with the build; it is not a phase you finish first.

1. **Before any play** — decide founder-or-joiner (`goatnet_founder`,
   `node-addition.md` §3). Every node added to an existing
   goatnet is a joiner. Set it in the group's `vars.yml`, never on the command
   line.
2. **Copy the cluster-shared secrets** from the existing active node into
   `.envrc.local.d/`. Generate only the per-node pair.
3. **Certificates** — manifest exists, CA exists, then `mint-site-certs.sh`.
4. **Config surface** — `standup-config-preflight.py <group> --scaffold-plan`,
   then port or declare every missing file.
5. **Host baseline + LUKS, narrowed.** Both playbooks default to the umbrella
   `mgmt_hosts` group — every mgmt host on every goatnet, production included.
   Since F-376
   they refuse an unnarrowed run, so a forgotten limit fails closed rather than
   reaching production. Pass `-e target=` or `--limit`.
6. **Shell access** for whoever operates the node, via the SSH key self-service
   flow — not as a side effect of their admin tier.
7. **Prove it.** `python3 ops/validation/run-acceptance.py --env <site>
   --all-eligible`, every applicable row PASS and never UNKNOWN. A probe that
   could not run is not a probe that passed.

---

## Related

- `node-addition.md` — the build sequence this page is the credential half of
- ADR 1141 — the four-node shape and role definitions
- [ADR 1111](../adr/1111-cutover-gated-service-class-for-standby-nodes.md) — what a standby may run while dormant
- [ADR 0811](../adr/0811-audit-log-integrity.md) + F-048 — the audit-durability property obs-b exists to restore
- [`ssh-key-self-service.md`](../design/ssh-key-self-service.md) — host shell, one door per device class
- `local-secrets-pattern.md` — where the values live on an operator laptop
