# ADR 0410 — Go CLI for sovereign peer enrollment

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0411-ory-idp-mvp.md`](0411-ory-idp-mvp.md), `0412-spiffe-spire-workload-id.md`, [`0401-per-site-dex-oidc.md`](0401-per-site-dex-oidc.md), [`0308-offline-enrollment-cbor-bundles.md`](0308-offline-enrollment-cbor-bundles.md), [`1011-findings-register.md`](1011-findings-register.md), `../design/identity-architecture.md`, `../project/idp-strategy-spike-2026-04-25.md`, `../reports/2026-04-24-cgnat-validation-attempt-1-findings.md` |

> **Findings addressed** (per ADR 1011): F-007
> Modern netbird-mgmt images hash setup keys with `base64(sha256(UPPER(uuid)))`;
> DB-direct seeded plaintext rows can't authenticate.

## TL;DR (for a layperson)

The way computers join our private network is by getting a special
"setup key" from an operator. We have been creating those keys by
poking values directly into a database. That has been quietly
broken for months — the keys we created cannot actually let computers
join. We are fixing it by writing a small command-line tool that the
operator runs to create the keys properly. This tool removes any
dependency on a separate identity-management product for the join
step. It makes the network simpler, more sovereign, and more reliable.

## Context

On 2026-04-25, root-causing a stuck cloud validation environment
revealed that direct `INSERT` into the `setup_keys` table produces
rows that real netbird peers cannot authenticate against. The
modern netbird image hashes the presented setup key and compares
against a stored hash; our scripts were storing plaintext. The
same broken pattern was used in `bootstrap-hadev-netbird.sh`,
`soak-gen`, and the cgnat-validation playbook — meaning every
multi-host validation env we have run since the patched netbird
image landed has been failing silently on real-protocol peer
enrollment.

Two viable forward fixes:

1. **IdP-mediated bootstrap.** Configure netbird to validate JWTs
   from an OIDC identity provider; admin logs in via the IdP, gets
   a JWT, calls netbird's `/api/setup-keys` to create properly-formed
   setup keys.
2. **Sovereign code-path bootstrap.** Small Go CLI that calls
   netbird's own internal Go API (`types.NewAccount`, setup-key
   generation) directly, writes properly-formed rows. No IdP touched.

Option (2) is structurally cleaner: peer enrollment is a one-shot
operation that doesn't need an ongoing auth flow. Tying it to an
IdP couples our peer-enrollment availability to the IdP's
availability, expands blast radius, and complicates island-mode
recovery. The full reasoning is in
`../design/identity-architecture.md`.

## Decision

We ship a Go CLI named **`mgmt-host-bootstrap`** that runs in the
mgmt-host container and creates netbird accounts + setup keys
directly via netbird's internal Go API.

**Subcommands (Phase 1):**

- `create-account --owner <email> --site-name <name>`
  Creates a properly-formed account row (all required fields populated
  via netbird's `types.NewAccountWithId`).
- `create-setup-key --account <id> --role <bootstrap|admin|operator|peer> --ttl <duration>`
  Creates a setup key with the role-scoped TTL per
  [`../operations/setup-key-rotation.md`](../operations/setup-key-rotation.md).
  Returns the plaintext UUID for the operator to hand to the peer.
- `rotate-setup-key --id <sk_id>` — generates a new setup key
  preserving the same role / scope, revokes the old.
- `revoke-peer --id <peer_id>` — marks the peer revoked.
- `list-peers --account <id>` — operator visibility.
- `list-setup-keys --account <id>` — operator visibility.

**Implementation:**

- Go binary, ~1-2 days of focused work.
- Imports netbird's own `management/server/types` package for
  account + setup-key construction (the same code path the dashboard
  uses, just without the dashboard / OIDC layer in front).
- Connects to the netbird Postgres DB using the same DSN the
  netbird-mgmt container uses.
- Audit-logs every operation to the common audit pipeline (Block
  17D integrity tier).
- Ships in the mgmt-host container image (Chainguard-built where
  possible).

**Operator usage:**

```bash
docker exec mgmt-host-runtime mgmt-host-bootstrap create-account \
  --owner admin@example.org \
  --site-name alpha
```

**Coupling to netbird release cadence:** low. The Go API we depend on
(`types.NewAccountWithId`, setup-key hashing) is core netbird code;
breaking changes are visible in CI on every netbird version bump.

## Alternatives considered

- **IdP-mediated bootstrap (option 1 above).** Requires deploying an
  IdP product before peer enrollment can work. Couples peer-enrollment
  availability to IdP availability. Larger surface area for the same
  one-shot operation. Rejected as the primary path; the IdP is needed
  separately for human + service-account auth (ADR 0411) but doesn't
  need to be in the peer-enrollment critical path.
- **Continue DB-direct INSERT.** Doesn't work — that's the bug we
  discovered. Rejected.
- **Use netbird's existing `cmd/token` subcommand.** Manages proxy
  access tokens, not accounts or setup keys. Doesn't cover the use
  case. Rejected.
- **Wait for upstream netbird to add an admin CLI.** No timeline; we
  need this now to unblock validation envs and Phase 3 deployment.
  Rejected.

## Consequences

**Positive.** Peer enrollment is sovereign, scriptable, and IdP-free.
Validation envs work correctly. Production peer enrollment matches
the same path as validation envs. Blast radius of "IdP is down" no
longer includes "no new peers can be enrolled." The CLI is small
enough to audit fully.

**Negative.** We own a small Go binary. We are coupled to netbird's
internal Go API surface, not just its HTTP API — this surface is
less stable than the HTTP API. We need CI tests against new netbird
versions to catch breakage early. Operators who used to do
"create-setup-key via the dashboard" no longer have that flow until
the dashboard is wired into Ory in Phase 2.

## Assessor view (SCA-R)

The CLI is a sovereign control plane for peer-onboarding identity
material. It satisfies IA-3 (device identification and authentication)
and AC-2 (account management) by replacing an ad-hoc DB-write pattern
with a code-path-driven one that uses the same logic netbird's own
public API uses. Audit logging to the Block 17D integrity tier
satisfies AU-2 / AU-3. The setup-key role-scoped TTL strategy
([`../operations/setup-key-rotation.md`](../operations/setup-key-rotation.md))
is the compensating control for the long-lived nature of WireGuard
peer keys. Residual risk: operator on the mgmt-host shell with CLI
access is highly privileged; mitigated by Block 18B PAM (sudo
audit + session recording + JIT elevation) when that lands.

## Trigger conditions for revisit

- netbird upstream adds a stable admin CLI that covers the same
  operations — re-evaluate whether to converge on theirs.
- Operator count for mgmt-host-shell access exceeds 5 — may need
  to layer additional controls beyond PAM.
- A future netbird version restructures `management/server/types`
  in a way that makes our internal-API import pattern unsustainable
  — re-evaluate whether to switch to HTTP-API path with a sidecar
  IdP just-for-bootstrap.

---

*See `GLOSSARY.md` for terms and acronyms.*
