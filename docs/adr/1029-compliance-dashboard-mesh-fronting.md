# ADR 1029 — Mesh-front the cloud compliance dashboard (ALB cold-start → mesh-only)

**Status:** Proposed (2026-06-06)
**Repo scope:** goat core (goat-side mesh fronting: `compliance-proxy` role + `aws-sigv4-proxy` → locked Function URL, mesh-only Traefik + CoreDNS, `COMPLIANCE` Ory group via `goat admin onboard`). The cloud-side counterpart is recorded in the infra repo (the Function-URL front door + the per-env stage toggles).
**Related:** design `compliance-dashboard-mesh-fronting.md` (the full two-stage plan) · `operator-onboard-utilities.md` (`goat admin onboard` + the group model) · `instance-variability-model.md` (Class-A per-env values) · [ADR 0219](0219-multi-repo-iac-state-separation-coordinated-compliance.md) (the two-account / two-repo split)

## Plain-language summary

There is a dashboard, hosted in an AWS account, that lists every cloud resource the account runs. That makes it a map an attacker would love, so it should not be openly on the public internet. Its first stage sits behind a public load balancer (the ALB) with a login in front. We decided to eventually serve it **only from inside the goat mesh** — reachable just like our other internal tools — with the public load balancer removed. But the dashboard has to exist and be usable *before* the mesh does (it's foundational, stood up first), so we can't start it mesh-only. The decision: walk a **two-stage evolution per environment** — start on the ALB to escape that cold-start, then move it behind the mesh and retire the ALB once the mesh is up. This ADR records the goat-side half (the mesh front and how access is managed); the cloud-side half is recorded in the infra repo.

## Context

The compliance dashboard is a large AWS-native Lambda (in the infra repo) that reads live AWS + Cloudflare on every request — it is not portable to the fabric and stays in AWS. It is a high-value reconnaissance surface (it enumerates the cloud footprint). It must not remain openly public long-term.

Two facts constrain the design:

1. **Cold-start dilemma.** The service is foundational — it exists and must be usable before goatnet is stood up, and every new AWS environment brings it up first. So it cannot be *born* mesh-only.
2. **The dashboard Lambda cannot authenticate on its own.** Its handler only *reads* an identity supplied upstream. Login is performed by a self-hosted oauth2-proxy + Dex, and that oauth2-proxy is reachable only through the ALB. So the ALB is the public ingress for the *login*, not merely a router to the Lambda.

Together these mean there is **no secure "ALB-free but still public, pre-netbird" intermediate state**: removing the ALB before the mesh exists forces either re-inventing the public ingress (an ALB by another name) or pointing a public Function URL straight at the Lambda with no login (insecure).

## Decision

Front the cloud compliance dashboard **through the goat mesh**, as a **two-stage, replayable, per-environment evolution**:

- **Stage 1 — ALB cold-start.** The environment runs the dashboard on the ALB with oauth2-proxy/Dex login (cloud-side, infra repo). Usable immediately, no mesh required. Every new AWS environment starts here.
- **Stage 2 — mesh-only.** Once that environment's goatnet is up, the goat side stands up a `compliance-proxy`: a mesh-only Traefik route (the shared mesh-only middleware) + CoreDNS hostname (no public DNS) → an `aws-sigv4-proxy` sidecar that reaches the dashboard's **Function URL**, which is locked to `AWS_IAM` and invocable only by that mesh's proxy role. oauth2-proxy/Dex still performs login, now gating on a `COMPLIANCE` Ory group, so access is granted/revoked via `goat admin onboard <handle>`. The ALB is retired **by** this cutover — never before.

**One service per AWS account, fanned in to N meshes.** The service follows the account, not the mesh. Environments that share an account front the *same* Function URL; each mesh runs its own proxy with its own scoped invoke credential (per-mesh, independently revocable, audit-attributable). Different account ⇒ its own service.

## Alternatives considered

- **Cloudflare Access in front of the dashboard** — rejected: the stack deliberately moved *off* Cloudflare Access to self-hosted oauth2-proxy + Dex. We carry the self-hosted login across both stages.
- **Re-host the 22k-LOC AWS-native app on the fabric** — rejected: it reads live AWS every request; re-hosting means broad AWS read credentials on a fabric host + porting the whole runtime. Not worth it; it stays in AWS.
- **An "ALB-free, pre-netbird" standalone stage** — rejected as insecure (see Context): the ALB is the login's public ingress, so dropping it pre-mesh has no secure form.
- **One compliance service per mesh (1:1)** — rejected: wasteful and inconsistent (N dashboards rendering the same account). Fan-in to one service is cleaner.

## Consequences

- The goat side owns a small `compliance-proxy` role (reused per mesh, parameterized on Class-A values) + the `COMPLIANCE` group; access management folds into the existing `goat admin onboard` path.
- Until an environment reaches Stage 2, it remains on the ALB — that is correct, not debt.
- The ALB cutover is a deliberate, reversible step gated on the mesh proxy working; the self-hosted oauth2-proxy/Dex login is preserved across the cutover.
- The Function URL is never a public door — it is `AWS_IAM`-locked and only reachable through the mesh proxy.
