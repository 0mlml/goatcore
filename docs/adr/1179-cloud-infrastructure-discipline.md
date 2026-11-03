# ADR 1179 — A cloud host serves goatnet users only when it is managed the way a goatnet is

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-09-11 |
| Repo scope | goat core; applies to every repository in the operating organisations that deploys something goatnet users depend on |
| Supersedes | — |
| Superseded by | — |
| Related | ADR 0219 (per-repo IaC state, coordinated compliance), ADR 1178 (external-service golden path), ADR 1060 (deploy-rollback ledger), ADR 1016 (complexity budget); docs/design/cloud-infrastructure-discipline.md, docs/design/cloud-environments.md, docs/design/instance-variability-model.md |

## TL;DR (for a layperson)

If people on a goatnet rely on a server in the cloud, that server has to be written down as code in a repository, live in an account the organisation owns, be reachable by at least two people, keep its secrets and backups where the organisation can get them, and have a written way to remove it. Those are the same rules the goatnets themselves follow. A server that does not meet them can be an experiment; it cannot be a service.

## Context

goat manages identity, secrets, releases and rollbacks through one source of truth, fail-closed defaults and a recorded path in and out. Its own cloud footprint follows the same discipline: two AWS accounts split by blast radius, one Terraform tree per goatnet with its own state bucket and a shared lock, compliance tags and an IaC registry that a scanner cross-references against live resources, no SSH from the internet, secrets in the documented pattern, a rollback ledger for anything that deploys images (ADR 0219, ADR 1060, cloud-environments.md).

That discipline is written down for goat's trees and nowhere else. A goatnet-facing service can therefore end up on one hand-provisioned virtual machine in a prototype cloud project, with no infrastructure-as-code in any repository, deployed over one person's SSH login, backed up inside the same project, and absent from the IaC registry, the estate catalog and every goat document — and nothing says that is below the bar, because the bar is only implied (cloud-environments.md §6a).

## Decision

1. **The bar.** A cloud host, bucket, function or managed service that goatnet users depend on meets the ten requirements in `docs/design/cloud-infrastructure-discipline.md` §2 before it takes traffic: organisation-owned account; a Terraform tree with per-repo state and the shared lock; compliance tags and an IaC registry entry; two-person access with no internet SSH; secrets in the documented pattern; backups the organisation can restore; a catalog row; declared per-instance variability; a recorded teardown and, where it deploys images, the rollback ledger; and, where it borrows the goatnet's identity plane, the external-service declaration.
2. **Provider is not an exemption.** ADR 0219's portability table maps every invariant to GCP and Azure. A GCP host uses a GCS state bucket with versioning, `default_labels` and the registry equivalent, as the reverse-proxy project already does.
3. **Experiments are exempt until they serve.** A host nobody depends on answers to nobody. The moment goatnet users depend on it, it answers to this record, and the adoption path in the design doc §5 is how it gets there without downtime: name it, add a second person, share the backups, import into Terraform, tag and register, move the account if personal, record the way out.
4. **The design doc names the coordinates.** Any design document for a cloud-hosted service names the provider, the account or project, the region and the Terraform tree. "One cloud host" is not a location.
5. **Review asks for the bar.** A PR that adds a goatnet-facing cloud service is reviewed against §2; a row it does not meet is either met in the PR or recorded as an open item with an owner and a date, never left implicit.

## Alternatives considered

- **Leave it to judgement.** Without a written bar, careful engineers still produce services nobody else can manage.
- **Require AWS only.** Rejected: the estate already has a compliant GCP project, and partners bring their own providers. The shape is what matters, and ADR 0219 already made it portable.
- **A CI check that scans for unmanaged hosts.** Wanted, not yet possible: the scanner can only cross-reference accounts it has credentials for, and an unmanaged host usually sits in an account it cannot see. Rows 1 and 3 of the bar are what make such a check meaningful; it follows once every account is named.

## Consequences

- One page now says what was implied. New cloud services cost their authors a Terraform tree, a registry entry and a second person before the first user, which is a day of work against months of somebody else being unable to touch it.
- `cloud-environments.md` gains rows for non-AWS projects as they are named; the estate catalog gains an owner for each.
