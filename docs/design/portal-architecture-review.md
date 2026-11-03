# goat-zenoh-portal — architecture review + decomposition assessment (C1)

**Status:** DONE 2026-07-28 — reviewed against the live `main` codebase (`ops/goat-zenoh-portal`, ~34k Go LOC / 16 internal packages). **Recommendation: keep the portal as one deployable service; do NOT split it now.** The M2 key-distribution daemon (Phase E) **stays a separate process** and consumes the portal's resolver over its HTTP seam, which this review confirms is clean enough to depend on. **Phase E is unblocked.** The one real accretion — the 14k-LOC `api` package — is an *internal* maintainability concern (a same-service package split), not a service-decomposition concern, and does not gate anything.

**Gates:** Phase E (M2 machine-read encryption) — specifically E1, the key-distribution service. **Tee-up:** portal-architecture-review.md. **Bars applied:** ADR 1016 (complexity budget — minimal moving parts), ADR 0859/1060 (independent deploy/rollback), the `no_third_shim` single-grant-store invariant.

## 1. Responsibility inventory (from the code)

| Package | LOC | Role | Internal deps |
|---|---:|---|---|
| `api` | 14352 | **HTTP composition root** — all 31 handler files: org/vendor/registry CRUD, ADR 1075 grant/label mutation + drift, `/api/access/*` COP read verbs, `/api/schemas`, `/api/whoami`, sponsorship residue, audit read | **all 15 others** |
| `registry` | 5461 | **The single store** — orgs/producers/members/custody, the ADR 1074/1075 grant + label rows, aliases, audit events (SQLite) | none |
| `access` | 3560 | **Resolver + COP derivation** — data-roles resolver, release-sets, coalition derivation, `who`/`why`/`graph` — pure functions over passed-in data | **none** |
| `pubsubstate` | 2005 | pub/sub state-as-code reconcile | registry, schemastore |
| `decoder` | 1954 | protobuf/wire decode for the COP | none |
| `acl` | 1439 | Zenoh ACL assembly/admin | none |
| `certmint` | 1273 | vendor/org cert mint (offline-CA shell-out) | config |
| `audittail` | 1155 | MinIO audit-stream tailer → registry | registry |
| `config` | 900 | flags/env | none |
| `netbirdpoll` | 513 | NetBird peer poll → registry | registry |
| `protocompile`, `zenohproxy`, `schemastore`, `kratosadmin`, `notify`, `safego` | <400 ea | leaf utilities | ≤1 |

## 2. Coupling map (the honest picture)

The internal import graph is **a fan-out from one composition package, over otherwise-decoupled leaves:**

- **`api` imports every other package.** It is the god-package — but as an HTTP *composition* layer (handlers wiring domain packages to routes), not as tangled domain logic.
- **Every domain package is a leaf or near-leaf.** `access` imports **nothing** internal. `registry`, `decoder`, `acl`, `config`, `schemastore`, `certmint` import at most one utility. Only `audittail`/`netbirdpoll`/`pubsubstate` reach into `registry` (the store), which is correct.
- **`access` (the resolver) is a pure leaf** imported only by `api`. Its exported surface — `LoadDataRoster`, `OrgsFromRoles`, `MergeGrants`, the who/why/graph builders — takes registry data **as parameters**; it holds no store and no state. `api` reads from `registry` (`s.reg`) and feeds `access`.

**Consequence:** the "monolith" is skin-deep. The accretion lives in the *HTTP surface* (`api`), while the domain is already cleanly seamed. That inverts the usual decomposition calculus — there is no tangled core to tease apart.

## 3. Keep-vs-split, per candidate seam

| Seam | Verdict | Why |
|---|---|---|
| **Resolver** (`access`, `/api/access/data-roles`) | **Keep in portal** | Already a pure, zero-coupling leaf. Extracting it buys nothing — it is *already* the clean interface a consumer depends on. |
| **Grant/label store + mutation API** (`registry` + `api` handlers) | **Keep as one store** | `registry` is the single policy store; `access` is stateless so a second copy is structurally impossible (`no_third_shim` holds by construction). Splitting the mutation API into its own plane adds a deploy unit + a store-access path with **no forcing function** — a net loss against ADR 1016. |
| **Schema serving** (`schemastore`, 284 LOC) | **Keep** | A 284-LOC orthogonal leaf; extracting it is negative value. |
| **Key distribution** (Phase E / E1) | **Separate — confirmed** | Never in the portal (per grant-keyed-encryption.md §M2 boundary). Consumes the resolver **over HTTP** (`GET /api/access/data-roles` → audience/grant → key policy input). The resolver's purity (zero internal coupling, stateless) makes that HTTP contract a stable dependency: the daemon links no portal code and gets no second store copy. |

## 4. The gating decision (for Phase E)

**E1's key-distribution service is a standalone per-site daemon that reads audience/grant policy from the portal's resolver endpoint (`/api/access/data-roles`) over HTTP.** The seam is confirmed clean:

- the resolver is a pure leaf (imports nothing internal) — the daemon inherits none of the portal's mass;
- policy stays single-sourced — the daemon *reads* the resolver's derivation of the one `registry` grant store, it does not copy grants (`no_third_shim` preserved);
- the interface already exists and is served in production (B2.0/B2.2 live on ZAGROS), so E1 builds against a proven contract, not a speculative one.

No topology change is required to start E1. This review therefore **does not** produce an ADR (nothing moves); it settles the boundary as a design decision.

## 5. The one real finding — an internal `api` refactor (not gating)

`api` at 14.3k LOC across 31 handler files is the actual accretion. It is worth splitting **into sub-packages within the same service** — e.g. `api/grant`, `api/accesscop`, `api/registrycrud`, `api/schema`, `api/onboardresidue` — for reviewability and blast-radius clarity. This is:

- **not** a service decomposition (one binary, one deploy unit, one store — unchanged);
- **not** on Phase E's critical path (E1 depends on the *resolver*, which is already its own package);
- a maintainability follow-up, best done incrementally and opportunistically as each surface is next touched.

Filed as a low-priority hygiene item; it does not block M2.

## Constraints honored

- **`no_third_shim`** — one grant store (`registry`); the recommendation creates no second policy copy and no cross-plane sync job. E1 reads the resolver, never a grant mirror.
- **ADR 1016 complexity budget** — the recommendation *removes* the temptation to add planes without a forcing function; net moving parts unchanged.
- **ADR 0859/1060 deploy+rollback** — the portal stays one rollbackable unit; the vendor-surface + cert-identity CI guards are untouched.
- **`goat-id-console` extraction not relitigated** — that split already happened and is correct; this review leaves it be.
