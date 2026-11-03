# ADR 1049 — A live, local zero-trust identity sandbox: prove "all five layers or none" by experiment

**Status:** Proposed (track `trust-sandbox`; scaffolding in progress)
**Repo scope:** goat core — a new `ops/serve/trust-sandbox/` (Vite+Vue3 SPA + a Rust/axum orchestrator + a multi-service Docker Compose project) and its CI image workflow. Local-only; no Ansible role, no mesh surface in v1.
**Related:** [`trust-sandbox.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/trust-sandbox.md) (the design) · `zenoh-sandbox.md` + [`zenoh-sandbox-ux.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/zenoh-sandbox-ux.md) (the sibling whose shape + quality bar this matches) · `identity-architecture.md` (the four-domain model dramatized) · ADR 1043 (URI-SAN grammar shown) · [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (slot-pool ACL the sandbox `zenohd` mirrors) · [ADR 0857](0857-patched-selfservice-ui-for-groups-claim.md) (groups-claim forwarding) · ADR 0412 (SPIRE) · `dns-service-discovery.md` + [`phoenix-architecture.md`](../design/phoenix-architecture.md) (private-DNS addressing layer + burn-&-respawn) · F-177 + [ADR 1022](1022-zenoh-denial-observability.md) (silent denial)

## Plain-language summary

A disposable, fully local playground that lets a visitor *feel* how goat net's five
identity layers — NetBird, x509 leaf certs, SPIRE/SPIFFE, Ory, and Zenoh ACLs — compose
into zero trust, and proves *by experiment* that you cannot keep only some of them. It
also runs a real private mesh DNS, the *addressing* layer that deliberately carries no
trust — which is what lets a service be burned down and respawned in seconds behind a
stable name while identity is re-verified cryptographically every connection. It is the
identity sibling of the Zenoh learning sandbox: same warm/tacticool Vite+Vue3 experience,
same active+passive shape, same "it's real, not a cartoon" bar. The whole stack runs as
one `docker compose up` on a laptop and `docker compose down -v` wipes it.

## Context

- The Zenoh sandbox (`zenoh-sandbox.md`) is well-regarded
  precisely because it is *real* — you actually publish, the bytes actually move — and it
  is warm, explanatory, and disposable. The operator wants an equally-real sibling that
  teaches **identity and permission**, not pub/sub.
- The identity story is genuinely hard to convey: five independent layers each answer a
  different question, and the headline property ("you need all of them for zero trust")
  is invisible in any one screenshot. Asserting it convinces nobody; *doing* it does.
- Every layer is a real service that fits in an isolated, throwaway compose project — Ory
  (live browser OIDC), a throwaway CA minting real URI-SAN-grammar leaves, SPIRE issuing
  real short-TTL SVIDs, and an isolated `zenohd` with a real client-cert ACL that really
  allows or *silently* denies. So the demo can be live, not mocked.
- The one exception is NetBird: a browser cannot perform a WireGuard handshake. Running
  locally makes representing that layer honest by construction — there is no mesh to
  pretend you're on.

## Decision

1. **Build a sibling sandbox, reusing the proven shape.** Vite+Vue3, the shared
   warm/tacticool design tokens (dark+light), `@eclipse-zenoh/zenoh-ts@1.9.0` for the
   live fabric viz, static build → nginx. Match the quality bar; do not reinvent it.

2. **Full live stack, run locally.** One isolated Docker Compose project
   (`ops/serve/trust-sandbox/`) running the real components — Ory (Kratos+Hydra), SPIRE
   (server+agent), a throwaway CA, an isolated `zenohd` (remote_api + real client-cert
   ACL), a private CoreDNS, the web app, and a Rust orchestrator. Primary delivery is
   `docker compose up` → `localhost:8100`. No Traefik, no Ansible role in v1.

3. **A Rust orchestrator is the privileged broker.** It does what a browser cannot:
   owns a throwaway CA and mints real org leaf certs; is a SPIRE workload that fetches and
   rotates real SVIDs; opens **real native-zenoh mTLS sessions** with a chosen cert and
   attempts real PUTs; calls Ory to gate issuance authority on the operator's role claim;
   and returns a per-gate verdict trace + expected-vs-actual outcome. It is Rust because
   the native mTLS Zenoh client lives there (panoscope-core / goat-cli patterns). It is
   the analog of the remote_api plugin — what makes the demo real.

4. **The honesty matrix is load-bearing and surfaced in-UI.** Each layer is graded
   live-vs-represented. Ory / x509 / SPIRE / Zenoh ACL are **live**; NetBird/WireGuard is
   **represented** (toggle + real artifacts, no in-browser WG). The UI says so plainly —
   no false "you're enrolled" claims. Same discipline as fabric-health honesty colors.

5. **Teach two issuance paths, one decision point.** External orgs get a portal/offline-CA
   leaf (orchestrator-as-CA); workloads we run get a SPIRE SVID. Both terminate at the
   same `zenohd` ACL — the accurate reason SPIRE and the portal mint coexist.

6. **Private DNS is a distinct addressing layer that carries no trust.** A real CoreDNS
   (local analog of the mesh service-discovery tier, F-039 mode gotcha honored) resolves
   stable service names; the orchestrator resolves-then-verifies. A real **burn & respawn**
   re-points a name to a fresh endpoint/cert while every gate re-verifies — demonstrating
   that names launder no trust, that a stale/cached name can't impersonate a service, and
   that services can therefore be phoenix-like (faster patching, smaller blast radius). It
   is **live**, not represented.

7. **The payoff is a knockout experiment.** A persistent Zero-Trust Integrity panel;
   knocking out any layer makes a real end-to-end write fail (often *silently*, itself a
   lesson per F-177 / ADR 1022) and names the trust property just lost.

8. **Ephemeral + isolated by construction.** In-memory Ory DSN, tmpfs SPIRE data, no
   `zenohd` storage, in-memory orchestrator CA/session. Demo identities only; never a real
   roster or a real anchor. **No `connect:` endpoints anywhere** — the load-bearing
   invariant from the Zenoh sandbox; adding one stops it being a sandbox.

9. **Stays inside fleet contracts.** The `zenohd` reuses the fleet-pinned
   `eclipse/zenoh:1.9.0` image (passes `check-zenoh-pinning.sh`); the remote_api plugin is
   the same SHA-verified bind-mount the Zenoh sandbox uses; the groups-claim forwarding
   mirrors ADR 0857; the cert URI-SAN grammar is the ADR 1043 grammar verbatim.

## Consequences

**Positive.** A real, self-contained artifact that makes "zero trust needs all five
layers" experiential for engineers and stakeholders alike; runs anywhere Docker does, with
no mesh or credentials; reinforces the project's honesty discipline; and doubles as a
living reference implementation of how the five layers actually compose.

**Costs / risks.** Heavier than the Zenoh sandbox (a full identity stack vs two services) and introduces a
new Rust service. Mitigated by local-only scope (no deploy surface), dev-mode/in-memory
backends (no persistence to operate), and reuse of the proven web shape. Standing up Ory +
SPIRE + a cert-gated `zenohd` together in compose is the main integration effort; phased
PRs (design §"Build phases") de-risk it one layer at a time.

**The NetBird honesty caveat is permanent.** Gate ① is represented, not live, for as long
as it runs in a browser. A future stretch could enroll a throwaway peer at the orchestrator
level, but that is explicitly out of v1 scope and the UI must never overstate it.

## Alternatives considered

- **Deploy to a shared host like the Zenoh sandbox.** Rejected per the operator's call:
  local is a better fit for an ephemeral playground (structural wipe, no shared env to
  corrupt) and dodges nothing real — the only browser-impossible layer (NetBird) is
  represented either way.
- **A lighter v1 with canned cert/SVID/ACL scenarios (only Ory live).** Rejected — it
  would miss the entire point of matching the Zenoh sandbox's "it's real" magic; the
  silent-denial and rotation lessons only land when the bytes really move or really don't.
- **A static explainer doc / slide deck (cf. the QoS + time-hygiene explainers).** Useful
  and complementary, but it asserts rather than lets the visitor *do* the knockout
  experiment. This sandbox is the hands-on counterpart.
- **Go orchestrator.** Rejected — the native mTLS Zenoh client is Rust; reusing
  panoscope-core/goat-cli avoids a second client implementation.
