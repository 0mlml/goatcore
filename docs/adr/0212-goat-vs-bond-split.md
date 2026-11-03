# ADR 0212 — Scope split: goat (transport) vs goat-bond (WAN bonding)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0108-scope-discipline-own-the-transport.md`](0108-scope-discipline-own-the-transport.md), [`1005-monorepo-separation-map.md`](1005-monorepo-separation-map.md) |

## TL;DR (for a layperson)

Two distinct problems need to be solved at the network edge: how
to encrypt and route traffic between devices (the mesh), and how
to combine multiple unreliable WAN links (satellite, cellular,
partner WAN) into one reliable connection. We solve them in two
different repositories — goat and goat-bond —
because each is a serious engineering problem on its own and the
two have different release cadences, different technology stacks,
and different failure models.

## Context

Both goat and goat-bond run at the network
edge; both are operationally critical; both benefit from sharing
hardware and operator knowledge. The temptation is to fold them
into one repo / one product / one ATO. We resist this for several
reasons:

1. **Different problem domain.** Mesh transport (goat)
   is an identity-and-overlay problem. WAN bonding
   (goat-bond) is a multipath-routing-and-traffic-shaping
   problem. The skill sets, the testing infrastructure, and the
   upstream ecosystems do not overlap.
2. **Different release cadence.** Mesh transport changes
   quarterly with the cATO inheritance
   ([`0102`](0102-two-track-development.md)). WAN bonding
   changes more often, in response to per-link behavior.
3. **Different failure model.** A mesh transport bug affects
   peers' ability to talk; a WAN bonding bug affects link
   utilization. Coupling them means a fix to one requires
   re-validation of the other.
4. **MOSA (severability).** They are independently substitutable
   — goat could in principle run over a different
   bonding solution; goat-bond could in principle ride
   under a different transport.

## Decision

goat and goat-bond are *separate repositories*,
*separate release artifacts*, *separate ADR series*, and (when
authorized) *separate authorization boundaries*.

| Concern | goat | goat-bond |
|---|---|---|
| Layer | Identity-based mesh overlay (above the WAN) | Multi-link WAN bonding (below the mesh) |
| Repo | https://github.com/usareur-af/goatcore | goat-bond |
| Stack | NetBird + WireGuard + Spock + Zenoh | MLVPN + per-link policy |
| ADR series | This series, 0000-1099 | Separate, in goat-bond repo |
| Hardware | Mgmt-host + relay/bridge VPSes + boundary node + peers | Edge router with multiple WAN interfaces |

They share:

- **Operator team** — same people deploy and operate both.
- **CLAUDE.md cross-references** — each project's CLAUDE.md
  links the other.
- **Shared testbeds where useful** — but the testbeds run both
  side-by-side, not as a combined fixture.

They do *not* share:

- Code.
- ADR numbering.
- Release schedules.
- Authorization packages (when they exist).

## Alternatives considered

- **Single repo, two top-level directories.** Couples release
  cadence and CI pipelines; complicates per-repo authorization.
  Rejected.
- **Two repos but one ADR series.** Loses MOSA severability
  signal. Rejected.
- **One product, one authorization, both bundled.** Worst of
  both worlds — couples both technical and accreditation
  cycles. Rejected.

## Consequences

**Positive.** Each project has independent release cadence,
independent supply-chain attestation, independent ATO posture.
A change to one does not require re-validating the other.
Each ADR series stays manageable. MOSA severability is
demonstrated by construction.

**Negative.** Cross-repo changes require two PRs. Operator
documentation must explain the boundary clearly. Acceptable;
the operator knowledge transfers between the two anyway.

## Assessor view (SCA-R)

The two-repo split scopes each authorization boundary
narrowly. An assessor evaluating goat does not
need to evaluate goat-bond's bonding logic; the
interface between the two is the WAN-side network interface
of the mgmt-host (a standard interface). NIST SP 800-37
boundary definition is satisfied per repo.

The shared operator team is an SCA-R-relevant detail under
PS-2 (position categorization) and PS-7 (third-party personnel
security) — same operators, same
training.

## Trigger conditions for revisit

- The two projects develop genuinely shared code that becomes
  a third repo. Acceptable.
- A specific authorization vehicle requires bundled
  authorization. Concede only with explicit ADR documenting
  the bundle scope.
- Operator-team workload imbalance suggests merging would
  reduce overhead. Investigate; the technical boundary is
  the load-bearing reason for the split, not the operator
  count.

---

*See `GLOSSARY.md` for terms and acronyms.*
