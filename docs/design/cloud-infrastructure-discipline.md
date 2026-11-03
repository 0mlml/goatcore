# Cloud infrastructure discipline — the bar a cloud host meets before it serves goatnet users

| Field | Value |
|---|---|
| Status | Written 2026-09-11; the bar in §2 is canon per [ADR 1179](../adr/1179-cloud-infrastructure-discipline.md). The adoption path in §5 is designed, not yet exercised on a host |
| Canon | [ADR 1179](../adr/1179-cloud-infrastructure-discipline.md); builds on [ADR 0219](../adr/0219-multi-repo-iac-state-separation-coordinated-compliance.md) |
| Related | cloud-environments.md (the accounts and trees), instance-variability-model.md (what may differ per instance), credential-inventory.md + local-secrets-pattern (secrets), [universal-deploy-rollback.md](universal-deploy-rollback.md) (rollback ledger), [external-service-golden-path.md](external-service-golden-path.md) (how a host on foreign infrastructure borrows the goatnet's identity) |

## 1. Why this page exists

The goat estate manages identity, access, secrets, releases and rollbacks with the same care: one source of truth, fail-closed defaults, a recorded way in and a recorded way out. Cloud infrastructure has the same machinery, and most of the estate uses it. Some hosts do not, and the gap only shows when somebody else has to touch them.

The short version: **a cloud host that serves goatnet users is managed the way a goatnet is managed, or it does not serve goatnet users.**

## 2. The bar

Every cloud host, bucket, function or managed service that goatnet users depend on meets all of the following before it takes traffic. Each row names the mechanism goat already uses, so meeting the bar is a matter of using what exists.

| # | Requirement | The mechanism that already exists | Fails when |
|---|---|---|---|
| 1 | **The account is the organisation's.** AWS: one of the organisation's accounts, per the blast-radius split. GCP or Azure: a project or subscription owned by the organisation, named in `cloud-environments.md`, with at least two people holding owner | cloud-environments.md §1, the two-account model | The project exists only under one person's login, or its name appears nowhere in the estate |
| 2 | **It is declared in a Terraform tree in a repository**, with its own state bucket named `<account>-<repo-slug>-tf-state` and the shared lock table | [ADR 0219](../adr/0219-multi-repo-iac-state-separation-coordinated-compliance.md); the per-repo trees under `terraform-*/`; the GCP equivalents in ADR 0219's portability table (GCS bucket with versioning, `default_labels`) | A resource exists that `terraform plan` does not know about; a console click made it |
| 3 | **It carries the compliance tags and is in the IaC registry.** The tree consumes `modules/aws/compliance-tags` and `modules/aws/iac-registry` from the organisation's infrastructure repository, or their GCP/Azure equivalents | cloud-environments.md §3; the compliance dashboard cross-references the registry against what is live | A live resource has no registry entry, so the scanner reports it as unmanaged, or never sees it |
| 4 | **Two people can get in without each other.** No SSH on port 22 from the internet; access through the cloud's session manager or over the mesh, with keys or roles for at least two named people | CLAUDE.md "VPS shell access": SSM for relays and bridges, mesh IP for mesh peers; `host-site-node-credentialing.md` for site hosts | Deploy or recovery needs one specific person's laptop |
| 5 | **Secrets follow the documented pattern.** Nothing in committed or generated files; runtime values from the environment or a secret store; AWS keys in the OS keychain through `aws-vault`, never in a file | local-secrets-pattern; `check-no-identity-spillage.sh`; the pre-commit live-secret guard | A `.env` on one laptop is the only copy of a credential the service needs |
| 6 | **Its backups land where the organisation can read them.** A bucket in the organisation's account, with a restore that a second person has run | The pgBackRest and audit cold-storage patterns; `docs/standby.md`-style rehearsal | The backup bucket is in the same unowned project as the host, or only its author has run a restore |
| 7 | **It is in the estate catalog** with an owner team and a risk category | the organisation's catalog (`catalog/owners.yaml`; `docs/REPO-CATALOG.md`) | Nobody can answer "who owns this" without asking around |
| 8 | **What it varies per instance is declared.** Anything that differs per goatnet or site is Class A, B or C in the instance model, with a fail-closed default | instance-variability-model.md §1 and §3 | A second instance inherits the first one's name, bucket or token by silence |
| 9 | **It has a recorded way out.** Teardown is `terraform destroy` on its tree plus a written list of what that leaves (DNS, buckets with retention, identities); if it deploys images, they go through the rollback ledger | [universal-deploy-rollback.md](universal-deploy-rollback.md); [external-service-golden-path.md](external-service-golden-path.md) for the goatnet-side withdrawal | Removing it means somebody remembering five places |
| 10 | **If it borrows the goatnet's identity plane, it does so through the external-service path**, as one declaration, and its host is a mesh peer in `EXTERNAL-INFRA` | [external-service-golden-path.md](external-service-golden-path.md), [ADR 1178](../adr/1178-external-service-golden-path.md) | A Hydra client, a route or a DNS name exists for it outside a declaration |

A host that meets rows 1 to 9 is managed. A host that also meets row 10 may serve goatnet users under goatnet sign-in.

## 3. Do

- **Start from a tree that exists.** Copy the shape of a goatnet `terraform-<goatnet>/` tree (AWS) or, for GCP, `hello-galaxy-gcp-service` — the reference host built to this bar (a `gcs` backend, a shielded `google_compute_instance` provisioned by a `startup-script`, firewall rules as resources, a separate data disk with snapshots, a versioned backup bucket, two-owner IAM, the GCP registry entry). Google's Debian image has no cloud-init; the guest agent runs `startup-script` metadata, which that repo learned by booting the machine. A new goatnet's tree is generated, not copied, by the standup tooling; a single host follows the same conventions by hand.
- **Bootstrap state before the first apply.** `terraform-prod/bootstrap.sh` shows the one-time, idempotent shape: state bucket, versioning, the shared lock table if absent.
- **Register before serving.** The iac-registry entry and the catalog row are part of the first PR, not a follow-up. A follow-up is how the compliance dashboard ends up with a hole shaped like a live host.
- **Run every cloud command through `aws-vault exec <profile> --no-session --`.** Session tokens are refused by the account policy; a bare `aws` call with keys in the environment is the shape the guards exist to catch.
- **Use the mesh for administration where the host is a mesh peer.** Port 22 stays closed to the internet; the mesh is the outer gate, the same as for every site host.
- **Put the second person in before the first user.** A second owner on the account, a second key on the host, a second restore of the backups. This is the difference between a service and a hobby.
- **Write the teardown in the same PR as the build.** For a temporary service, put the retirement date in the declaration; the guard and the play refuse a service past its date.
- **Name the cloud and the account in the design doc.** "One cloud host" is not a location. `cloud-environments.md` has the table; add the row.

## 4. Do not

- **Do not provision by console click.** A resource that Terraform does not know about cannot be planned, reviewed, tagged, registered, rolled back or destroyed by anyone but the person who clicked. Import it or replace it.
- **Do not use a personal cloud project or account, even for a trial.** The trial becomes the service. Ownership of a GCP project or an AWS account cannot be transferred cheaply; the organisation's account can hold a trial as easily as a personal one.
- **Do not deploy from one laptop's SSH key as the only path.** "Deploy as yourself" is right for attribution and wrong as the sole door. A second named person's key, or session-manager access, goes on the host on day one.
- **Do not keep the only copy of a secret in a local env file.** The pattern has a home for every class of secret; a value that exists only on one machine is a value the organisation does not have.
- **Do not back a service up to a bucket the organisation cannot read.** A backup nobody else can restore protects the author, not the service.
- **Do not wire a temporary service into permanent code.** The external-service path exists so a service that lives for one event leaves nothing behind; the footprint guard refuses its name outside its declaration.
- **Do not treat "it is not AWS" as an exemption.** ADR 0219 maps every invariant to GCP and Azure: per-repo state object, shared lock, published tag contract, central registry. The GCP reverse-proxy project already follows it.
- **Do not describe a host without its coordinates.** A design doc that says "one cloud host" and names no provider, project, region or tree is a host nobody else can find, which is the same as a host nobody else can fix.

## 5. Bringing an existing host up to the bar

For a host that is already serving and does not meet §2, in this order. Each step is reversible until the last.

1. **Name it.** Add the provider, project or account, region and owner to `cloud-environments.md` and a catalog row to goat-hq. This costs nothing and ends the "where does it run" question.
2. **Give a second person access.** A second owner on the project or account; a second SSH key or session-manager role on the host. Until this is done, every other step depends on one person's availability.
3. **Move or share the backups.** Point the backup job at a bucket in the organisation's account, or grant the organisation read on the existing one, and have the second person run one restore.
4. **Import into Terraform.** Create the tree with its own state bucket and the shared lock, then `terraform import` the instance, disks, firewall rules and bucket. The first `plan` after import must be empty; a non-empty plan is the list of what the console and the code disagree on, resolved before anything else changes.
5. **Tag and register.** Add the compliance-tags and iac-registry modules (or their GCP equivalents). The compliance dashboard now sees the host.
6. **Move the account if it is personal.** For GCP, a project can be moved between organisations by an owner of both; for a host in an account that cannot be moved, the tree from step 4 is what makes rebuilding it in the organisation's account a `terraform apply` rather than a week.
7. **Record the way out.** The teardown list, and for a temporary service the retirement date in its declaration.


## 6. What this does not say

- It does not forbid Google Cloud or Azure. It requires the same shape on them, and ADR 0219 already wrote down the mapping.
- It does not require every experiment to be Terraformed. It requires it before goatnet users depend on it. An experiment that never serves users answers to nobody; the moment it takes traffic, it answers to this page.
- It does not move existing compliant trees. The `terraform-*/` trees, the relay fleet, the cloud host site and the reverse-proxy project already meet the bar; this page describes what they do so the next host does the same.
