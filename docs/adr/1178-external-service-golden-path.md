# ADR 1178 — External services borrow a goatnet's identity plane from one declaration, and leave the same way

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-09-11 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | ADR 1016 (complexity budget), ADR 1073 (RBAC adherence, the `door:` vocabulary), ADR 1082 (name-fallback snapshots), ADR 1116 §9 (machine enrollment derives mesh groups), ADR 1162 (infra classes as independent axes), ADR 1031 (mesh-first revoke); F-197, F-215; docs/design/external-service-golden-path.md, docs/design/goat-map.md |

## TL;DR (for a layperson)

A goatnet can lend its sign-in to a service it does not run, for as long as an event lasts, by having the service's owner write one file and run one command. The same file and command take it away again. Nothing in goat is written for the service by name, so when it goes, nothing remembers it.

## Context

An external, non-core service is a workload goat does not build or deploy, running on its own host, that needs people to sign in with their goatnet identities and needs a name and a route on the goatnet's edge. goat-map was the first; a partner registry is the second and will be retired when the event it serves ends.

Before this record every such service cost a core engineer five places, and the same five to unpick: a Hydra client (eleven copy-pasted `*-oidc-client` roles, plus an inline list in a per-goatnet clients playbook for the ones that refused a role), a `{% if <service>_enabled %}` block in the shared Traefik template (twenty of them), a group_vars file with service-specific variable names, an `isitup_services` row, and a door record. Removal had no play. The registry's first PR (#707) paid all five, then went further and named the service inside the identity console, the welcome guide, the operator-key map, the onboard runner and the operator-onboard play, so that a teardown would have had to visit nine places nobody had listed.

The mesh half already had a path: an external host joins as a machine through custodian self-service enrollment, whose node class derives its groups and mints a single-use key (ADR 1116 §9), and the tier for somebody else's hardware serving our users is `EXTERNAL-INFRA`, reachable by every role group one-way (ADR 1162). Earlier external hosts and goat-map both joined that way. What was missing was the identity-plane half.

> **Correction, 2026-09-16:** the enrollment path described above does not exist. No node class mints `EXTERNAL-INFRA` (F-531). An external host is already a peer when its declaration names it; `ops/scripts/external-service-finish.py` moves it into the tier and records the move as a declared deviation. The decision below is unchanged.

## Decision

1. **One declaration per service per goatnet.** `ops/ansible/group_vars/<site>_hosts/external-service-<name>.yml` defines one mapping, `external_service_<name>`: the hostname label, the backend mesh URL, the health path, what the service's own fence decides (`door`, the ADR 1073 closed set), an optional goat-managed Hydra `client:` block, an optional `status:` row, an owner, and for a temporary service a `retire_after` date. The file is the whole goat-side footprint.
2. **One play, present or absent.** `ops/ansible/external-service.yml -e target=<site>_hosts -e service=<name> -e state=present|absent`. Present pre-flights the host (answers on its health path over the mesh; holds `EXTERNAL-INFRA`; its name is in the committed zone), upserts the Hydra client when declared, then runs the existing edge, zone and status-page plays. Absent, run after the owner sets `enabled: false` and before the file is deleted, deletes the client and re-runs the same three, which drop the route, SAN, record and row because the declaration is withdrawn. The play refuses `absent` while the declaration is still enabled: withdrawing a service is the owner's edit.
3. **Shared code reads declarations; it never names a service.** One loop in `templates/traefik-acme/mgmt-dynamic.yml.j2` renders every enabled declaration's SAN, router and backend. `ops/dns/generate-mesh-hosts.py` emits its record. `roles/isitup/templates/services.env.j2` derives its status row. A service whose name an `isitup_services` row already carries keeps that row and that record (first entry wins), which is how goat-map moved its edge onto a declaration without a DNS regeneration.
4. **A temporary service leaves no footprint outside its file, and CI enforces it.** `ops/ci/check-external-service-footprint.sh` fails when a declaration carrying `retire_after` has its name anywhere under `ops/`, `.github/` or `terraform/` except its own declaration files. A permanent service is reported, not gated. The identity console, roles, guides and onboarding plays therefore never learn a temporary service's name; the status page lists it, and the welcome guide already links the status page.
5. **The identity plane carries what a service authorizes on; no service asks for a per-service identity change.** A relying party gets a verified email and the affiliation claim `aff.id` (ADR 1043 Phase D, sandbox-first). Both are core work on their own schedule. A service that needs more than the token carries is asking for a core change, and that is the review moment.
6. **goat-map is the first declaration and the acceptance test.** Its three group_vars files became declarations on the day this landed; the Traefik template rendered structurally identical routers, services and certificate anchors on three goatnets before and after, and the DNS generator's check stayed green with no file regenerated.

7. **One declaration may carry several hostnames, and the partner may hold the TLS.** Amended 2026-09-14. `hostnames:` (default `[name]`) gives one service several labels — a vendor platform whose `api`, `audit` and `authentication` surfaces are one thing with one withdrawal, not N declarations with N status rows. `tls:` names who terminates: `edge` (the default, and every declaration written before this amendment) keeps goat's Traefik in the path, gated by the mesh-only allowlist, with the partner holding no key for the zone; `partner` points the names at the partner's own mesh address and renders no router, no backend and no certificate name, so the partner serves TLS with their own certificate. The cost of `partner`, and the reason it is not the default: goat's edge is no longer the gate, so reachability control lives on the partner's host and the status page reports only what their health endpoint says. goat never issues a certificate for a name a partner serves, which is how the credential question is avoided rather than answered.

8. **A partner that terminates proves control with DNS-01 delegation, not public exposure.** Amended 2026-09-17. A `tls: partner` declaration names `acme_delegate`, a hostname in a zone the partner controls; the play publishes one public `_acme-challenge.<label>.<site>.<zone>` CNAME to it per certificate name (each `<name>`, whose challenge also serves `*.<name>`, and each flat `<name>-<surface>`), owns those records by a comment tag, and removes them on absent or a move to edge. The partner's ACME client answers DNS-01 in its own zone, so it gets a publicly trusted certificate without its host ever serving the public internet, and renews unattended. Rejected: exposing the names publicly while HTTP-01 validates, because the host has no public address to validate against and every renewal would repeat the exposure. The service names stay in mesh DNS only; public DNS carries the delegation and nothing else (dns-service-discovery.md, tier 1). An optional `mesh_peer` names the host's NetBird peer, and the play refuses when the peer at `mesh_ip` has another name: records stay literal addresses (the CoreDNS `hosts` plugin and the signed name snapshot carry A records, not CNAMEs), and a NetBird address is stable for the life of the peer, so the check catches the one event that moves it — re-enrollment.

## What this does not do

- It does not host the fence. An off-box service runs its own oauth2-proxy or verifies tokens natively and is declared `door: <what that fence decides>`. goat's shared door role (`oauth2-proxy-surface`) stays for on-host surfaces: a host in `EXTERNAL-INFRA` is reachable by every role group directly, so a fence on the mgmt host in front of it would be a fence with a gap beside it.
- It does not put external services in the group-permissions matrix or the parity ratchet. The console's permissions page lists goat-built services; an external service's door is recorded in its declaration.
- It does not probe the service from the observability host. A blackbox target for it is one optional row in the obs group's `obs_blackbox_extra_targets`, on the obs host's own group, because Ansible group_vars cannot read across groups (the reason `check-blackbox-covers-registry.sh` exists).
- It does not provide a machine lane. A service whose API is called from code needs a client-credentials client or the caller's fabric bundle; the declaration reserves nothing for it yet.

## Alternatives considered

- **A generic `oidc-client` role parameterised per service.** Rejected: a role per service is what the eleven existing roles are, and one more generic role still leaves the template block, the DNS row and the door record as per-service edits. The unit of declaration has to be the whole service, not its client.
- **Keep per-service blocks, add a teardown checklist.** Rejected: a checklist is the nine-place footprint with a document beside it. F-197 and the three-weeks-unregistered microscope client are what a list that nobody re-reads produces.
- **Host every external service's fence on the mgmt host under `oauth2-proxy-surface`.** Rejected for off-box hosts in `EXTERNAL-INFRA`, above. Kept as the answer for a service that runs on the mgmt host, which is not external.

## Consequences

- A new external service on a goatnet costs its owner one file, one secret in the deploy environment and one play run; core engineering nothing. Removal costs one edit and one play run.
- The Traefik template gains one loop and loses three blocks today; the remaining twenty per-service blocks can migrate onto declarations as they are touched, each proven the same way goat-map was.
