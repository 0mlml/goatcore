# Continuous architectural chaos — every documented invariant becomes a probe

## Conclusion

A `chaos/` peer class whose only job is to encode every load-bearing
architectural invariant claimed in CLAUDE.md, ADRs, and design docs as
a runnable assertion, then probe those assertions continuously against
the live fabric on a schedule. Drift between "what the docs say is
true" and "what is true" becomes a finding (F-NNN entry)
instead of something a human has to notice during the next adversarial
review.

Chaos is the **post-deployment** test discipline that completes the
trio with **soak** (correctness under realistic time + load —
ADR [`1008`](../adr/1008-soak-testing-discipline.md)) and **load**
(measured ceilings — ADR [`1012`](../adr/1012-load-testing-discipline.md)).
Soak validates correctness over time. Load validates limits. Chaos
validates that the *invariants we claim* still hold in production.

Decision: ADR [`1013`](../adr/1013-continuous-architectural-chaos.md).
Implementation: `../project/implementation-plan.md` Block 38.

## Why a third post-deployment discipline

goat has unusually high invariant-density per LOC. CLAUDE.md alone
makes load-bearing claims about: silent control plane, schema-enforcer
default-deny, Spock LWW per-row, peer-key attestation, audit
tamper-evidence, default-deny egress, phoenix-native peer
self-attestation, mandatory data-dictionary declaration, time-quality
floors per topic, and ~40 more across ADRs and design docs. Every one
of these is a place where reality could silently drift from the doc
without anybody noticing until the next adversarial review.

The UserPromptSubmit hook already nags about doc-vs-code drift at
authoring time. Chaos closes the loop: drift becomes machine-verifiable
in production. The hook prevents drift entering; chaos catches drift
that entered another way (vendor change, library upgrade, ops
remediation, partial rollback).

The project also has a stated comparative-posture commitment
(`../compliance/comparative-posture.md`)
that meaningful claims rest on operational evidence, not lab-ideal
assertions. We hold others to that. We have to hold ourselves to it.
Continuous probes against documented invariants are how we earn the
evidence.

## What a probe looks like

A probe is a small Go function:

```go
// ops/chaos/probes/silent_control_plane.go (sketch)
type SilentControlPlaneProbe struct{}

func (p *SilentControlPlaneProbe) ID() string             { return "silent-control-plane-listener-silent" }
func (p *SilentControlPlaneProbe) InvariantDoc() string   { return "docs/adr/0313-silent-control-plane-outer-wg-wrap.md" }
func (p *SilentControlPlaneProbe) Severity() Severity     { return SeverityCritical }
func (p *SilentControlPlaneProbe) DefaultInterval() time.Duration { return 5 * time.Minute }

func (p *SilentControlPlaneProbe) Run(ctx context.Context, env Env) ProbeResult {
    // Attempt TLS handshake to wg-cp0 from a non-mesh vantage.
    // Assertion: connection times out (no TLS response).
    for _, relay := range env.Relays {
        ok, evidence := tryTLSFromOutsideMesh(ctx, relay.PublicIP, 51820)
        if ok {
            return ProbeResult{
                Passed:        false,
                EvidencePath:  evidence,
                FindingDraft:  draftFinding("silent-cp-listener-not-silent", relay.ID),
            }
        }
    }
    return ProbeResult{Passed: true}
}
```

Probe shape is uniform:

| Field | Purpose |
|---|---|
| `ID()` | Stable string for probe-result correlation across runs |
| `InvariantDoc()` | Path to the ADR / design doc claiming the invariant — chaos peer publishes this in result attestation |
| `Severity()` | `Critical` (paging) / `High` (24h SLA) / `Medium` (weekly review) / `Informational` |
| `DefaultInterval()` | How often the probe runs by default; per-environment override allowed |
| `Run()` | Returns `passed`, optional `evidence_path` (in `docs/reports/chaos/<date>-<probe_id>.jsonl`), optional `finding_draft` |

Probe results are emitted on:

- `chaos/result/<probe_id>` — Zenoh topic for live consumers (dashboards, Tipper anomaly detection on the chaos pass-rate signal itself)
- OTLP wide event with attribute `dbb.chaos.probe_id`, `dbb.chaos.passed`, `dbb.chaos.severity`, `dbb.chaos.invariant_doc` — for cross-correlation with deploys / config changes
- Audit-integrity ledger entry — for evidentiary durability

Failed probes also draft a candidate finding entry at
`docs/findings/F-DRAFT-<probe_id>.md` and notify via the
Block 17A.5 policy engine
([`egress-proxy.md`](egress-proxy.md) §6) which already has Matrix +
email + isitup adapters.

## Invariant catalog (initial wave)

The first set of probes covers the highest-stakes invariants. All
trace to specific docs.

### From ADR series

| Probe ID | Invariant | Source | Method |
|---|---|---|---|
| `silent-control-plane-listener-silent` | The mgmt-plane TCP listeners answer no pre-auth bytes from non-mesh vantages. | ADR [`0313`](../adr/0313-silent-control-plane-outer-wg-wrap.md) | nmap + custom probe from outside-mesh EC2 instance |
| `no-public-dns-mgmt` | No public DNS A record resolves the mgmt-plane endpoint. | ADR [`0314`](../adr/0314-no-public-dns-for-control-plane.md) | dig against multiple resolvers |
| `peer-key-attestation-enforced` | Every active mesh peer's WireGuard pubkey traces to a valid offline-CA-signed bundle. | ADR [`0315`](../adr/0315-peer-key-attestation-via-offline-ca.md) | mgmt DB peer-list × bundle registry join |
| `setup-key-ttl-bounded` | No active setup key has TTL > its role's bound. | ADR [`0307`](../adr/0307-setup-key-rotation.md) | mgmt DB query |
| `bridge-cidr-default-deny` | Every bridge VPS forwarder has at least one CIDR allowlist entry; no `0.0.0.0/0`. | ADR [`0309`](../adr/0309-bridge-cidr-allowlist.md) | terraform state + AWS SG describe |
| `audit-tamper-detected-on-tamper` | A deliberate tamper of a synthetic audit-archive line is detected within one ledger cycle. | ADR [`0811`](../adr/0811-audit-log-integrity.md) | write synthetic line to non-Object-Locked replica → run integrity check → assert detection |
| `egress-tier1-default-deny` | A peer attempting direct egress to a known un-allowlisted CIDR is blocked at Tier 1. | ADR [`0806`](../adr/0806-egress-proxy-cascade.md) | spawn ephemeral peer in test ACL group, attempt curl to test CIDR, assert nftables drop |
| `wasm-fuel-bounded` | A pathological WASM module exhausting fuel terminates cleanly without OOM. | ADR [`0513`](../adr/0513-wasm-conditioning-sandbox.md) | submit fuel-burner module, observe Wasmtime termination |
| `time-quality-floor-enforced` | Emission to a topic with `minimum_source: nts_verified` is rejected if envelope source is `self_clocking`. | ADR [`0514`](../adr/0514-time-quality-attestation.md) (this set) | publish synthetic emission with degraded envelope, assert DLQ |

### From design / discipline docs

| Probe ID | Invariant | Source | Method |
|---|---|---|---|
| `data-dictionary-undeclared-rejected` | Publishing to an undeclared `attest/schema/<topic>` is rejected by the schema-enforcer. | [`data-dictionary.md`](data-dictionary.md) | publish to synthetic undeclared topic; assert rejection + audit entry |
| `phoenix-native-peers-attest` | Every entry in `attest/queryable/` has a corresponding offline-CA-signed `attest/identity/`. | [`phoenix-architecture.md`](phoenix-architecture.md) | Zenoh query + signature verification |
| `release-namespace-default-deny` | Publishing to `release/<unconfigured-partner>/...` is rejected at the bridge. | [`mission-partner-sharing.md`](mission-partner-sharing.md) | configure synthetic `release/test-partner-no-config/...`, attempt publish, assert deny |
| `spock-lww-per-row` | Conflicting writes on two Spock nodes resolve LWW per-row, not per-table. | F-003, `pg-spock-security.md` | seed conflict on adjacent rows, assert independent resolution |
| `no-orm-in-go-sources` | No new Go source file imports an ORM library. | ADR [`0413`](../adr/0413-no-orm-in-our-go-code.md) | grep across repo for forbidden imports; CI mirror |

### From CLAUDE.md (as canonical pointers — invariants live in the linked sources)

| Probe ID | Invariant | Method |
|---|---|---|
| `wg-cp0-allowlist-up-to-date` | Every device in the mgmt-plane peer registry is in `wg-cp0` allowlist within 60s of registration. | mgmt DB × wg-cp0 config diff |
| `signal-server-federation-healthy` | Every site's signal-server reaches every other site's within 30s of probe. | synthetic signal-RPC across all site pairs |
| `pgbackrest-stanza-fresh` | Each site's pgBackRest stanza has a successful backup within last 24h. | pgBackRest info query |

### From Block 21D.1 first-party 21D OIDC client wire-up checklist (added 2026-05-08)

The eight-item checklist in `identity-architecture.md` §11.1a becomes a probe set so future first-party consumers (tuwunel per Block 70, Scout UI per Block 23, etc.) inherit the runtime guarantees by construction. Five probes — items (1)+(2) collapse into `oidc-client-config-correct`, items (5)-(8) each get their own probe; items (3)+(4) are static-config invariants covered by the [`netbird-dashboard` role's structural test](../../ops/ansible/roles/netbird-dashboard/tests/test-structure.py) and the [Codex rule](../../ops/ci/check-hydra-client-invariants.py), so the chaos probe layer skips them.

| Probe ID | Invariant | Source | Method |
|---|---|---|---|
| `hydra-firstparty-client-skip-consent-and-jwt` | Every Hydra client tagged first-party in mgmt-host config has both `skip_consent: true` and `access_token_strategy: "jwt"`. | F-077 + F-078 (Block 21D.1 wire-up checklist items 1+2) | Per-site Hydra `/admin/clients` GET against the loopback admin port; assert both fields on each tagged client. Drift triggers an F-NNN draft auto-emit. |
| `mgmt-jwks-includes-access-token-kid` | Every site's netbird-mgmt cached JWKS contains the `kid` Hydra is currently using to sign access tokens for first-party clients. | F-078 (Block 21D.1 wire-up checklist item 5) | Per-site: GET `https://oauth2.<site>.../.well-known/jwks.json`, GET `http://management-a:80/api/jwks-debug` (debug endpoint to expose cached kids — to be added on first probe-driven need; until then probe asserts via synthetic-token-issuance round-trip). |
| `mgmt-user-pii-encrypted` | Every row in `users` table has email + name as base64-decodable ciphertext (decode succeeds; decrypt with `DataStoreEncryptionKey` succeeds). | F-079 (Block 21D.1 wire-up checklist item 6) | Per-site: SQL query reading `users.email` + `users.name`; assert each is base64-valid AND decrypts under the per-site key. Plaintext seeds (the F-079 trigger) fail the base64 step. |
| `dashboard-spa-renders-from-root` | Browsing to `https://dashboard.<site>.../` from a goat-net peer with the dev-CA root trusted reaches a hydrated React tree (non-empty body) within 15s. | F-080 (Block 21D.1 wire-up checklist + the durable Traefik `dashboard-root-to-access-control` `redirectRegex` middleware that survives every dashboard container churn) | Headless browser drives the full OIDC dance using a synthetic Kratos identity; asserts non-empty `document.body.innerText` after auth. Catches regressions of the `/` → `/access-control` Traefik middleware (e.g. middleware accidentally dropped from a per-site `traefik_services` group_var, role default mutated, file-watcher not picking up rendered config) AND any future netbirdio/dashboard upstream change that re-breaks the post-callback render. |
| `mgmt-stack-cert-multisan` | Each site's TLS cert presented by Traefik for any of the four mgmt-stack hostnames (`dashboard\|mgmt\|oauth2\|auth.<site>...`) has SANs covering all four. | Block 21D.1 wire-up checklist item 7 | Per-site: `openssl s_client -servername <each-host>:<port>` × 4; assert returned cert has SAN list covering all four hostnames. Per-host cert split (the pre-dogfood pattern) fails this. |

The catalog is not closed. New probes land in the same PR as the
invariants they validate (mirroring the ADR-with-implementation
discipline).

## Probe authoring rules

A probe is **valid** when all of:

1. **Cites a specific source.** `InvariantDoc()` returns a real path to a real claim. No probe without a doc; no doc-claim should go unprobed where mechanically feasible.
2. **Operationally non-destructive.** Probes do not delete state, do not hold locks longer than ~1s, do not exhaust shared resources. The chaos peer is read-mostly; write probes (synthetic-emission style) write to dedicated test namespaces / SGs / DBs.
3. **Self-cleaning.** Every write probe cleans up its synthetic state on success or failure. The framework provides a per-probe scratch namespace (`chaos/scratch/<probe_id>/<run_id>/...`) with auto-TTL.
4. **Bounded runtime.** Hard timeout per probe; default 30s. Probes exceeding their timeout are considered failed (timeout is itself a finding — invariant claim implicitly includes "this is checkable in <30s").
5. **Reproducible evidence.** Failed probes write evidence at `docs/reports/chaos/<date>-<probe_id>.jsonl` so a human reviewer can reproduce the failure.
6. **Findings-register-shaped output.** Failed probes draft a finding entry following F-NNN template — saves the human ~10 minutes per finding.

## Composition with other systems

| System | How it composes |
|---|---|
| Findings register | Failed probes draft finding entries; human reviewer promotes to permanent F-NNN. |
| ADR [`1008`](../adr/1008-soak-testing-discipline.md) (soak) | Chaos runs concurrently with soaks during canonical-tier soak windows. Chaos failures during soak are first-class soak-verdict inputs. |
| ADR [`1012`](../adr/1012-load-testing-discipline.md) (load) | Load tests probe ceilings; chaos probes invariants. They share a JSONL evidence shape and a validation-state tracker. |
| ADR [`0805`](../adr/0805-policy-engine-active-passive.md) (policy engine) | Critical-severity chaos failures notify via the policy engine's email + Matrix + isitup adapters. |
| ADR [`0822`](../adr/0822-wide-event-observability-otlp-openobserve.md) (wide events) | Probe results emit OTLP attributes; Tipper can detect anomalous regressions in the chaos pass-rate signal itself. |
| Block 32 [`engineering-health-assessment.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/engineering-health-assessment.md) | "Invariant-fulfillment rate" becomes a quarterly scorecard metric: % of documented invariants with active probes × % passing over the quarter. |
| Vendor handoff plan | Vendor-supplied components must pass the relevant chaos probes before procurement closes. New chaos probes are part of the seven-artifact handoff bundle. |
| Block 37 [`fabric-replay.md`](fabric-replay.md) | Chaos probes can run against replayed historical state to confirm invariants held in past windows. |
| [Phoenix-native peer pattern](phoenix-architecture.md) | Chaos peer is itself phoenix-native: rebuilds clean from supply chain, declares its capabilities, attests its identity. |

## What this is not

- **Not a security scanner.** Probes test architectural invariants, not generic CVEs. CVE scanning is Block 18H.
- **Not a CI replacement.** Probes run against the live fabric continuously, not against pre-merge code.
- **Not a kill switch.** Probes report; the policy engine + humans decide responses. The chaos peer has no authority to remediate.
- **Not exhaustive.** Some invariants are not mechanically probable (e.g., human-process invariants like "every PR is reviewed by CODEOWNERS" — that is a CI policy, not a chaos probe). Probe coverage is a percentage; the goal is high-and-rising, not 100%.
- **Not a substitute for adversarial review.** Quarterly red-team is still scheduled. Chaos is the floor; adversarial review is the ceiling.

## Sequencing

| Phase | Scope | Duration |
|---|---|---|
| **38A** | Probe framework Go binary + 5 probes (silent CP, no-pub-DNS, schema-enforcer, audit-tamper, setup-key TTL) + result emission to OTLP | ~1 wk |
| **38B** | Wide-event integration + audit-integrity ledger entries + policy-engine notification adapter | 3 days |
| **38C** | Findings-register draft generation | 2 days |
| **38D** | First-wave catalog ship — ~15 probes covering 0300-series mesh + 0500-series fabric + 0800-series operations invariants | ~1 wk |
| **38E** | Per-probe scheduling tuning + per-environment override (some probes only meaningful at production sites) | 3 days |
| **38F** | Engineering-health scorecard integration — quarterly probe-coverage + pass-rate metric | 2 days (after Block 32 lands the scorecard primitives) |
| **38G** | Continuous probe expansion — invariant catalog growth as ADRs land | ongoing |

Each phase is independently shippable. 38A delivers operator value (5 invariants now actively probed) before the catalog grows.

## Probe density target

Initial Phase 3 target: **80% of CLAUDE.md-quoted load-bearing
invariants have at least one probe.** The 20% remainder are either
not mechanically probable (process invariants, sponsor-facing claims)
or pending sequencing (probes whose preconditions are not yet
operational — peer-key attestation probe waits on Block 22H landing).

Phase 4 target: 95%, with the residual being explicitly catalogued as
"not mechanically probable, alternative discipline applies (red-team /
process audit / human review)."

## Why Go for the chaos peer

Chaos is request-shaped, not data-path-shaped. Probes fire on a
schedule, hit HTTP / DB / SSH / AWS APIs / Zenoh, return verdicts.
Broad library needs (postgres, ssh, AWS SDK, Prometheus client,
Zenoh-go). Iteration speed matters because the probe library will
grow steadily. No hot-path bytes; no F-017 regime. Per the refined
language-selection rule in
`tech-strategy.md` §"Language Selection" / ADR
[`0112`](../adr/0112-tech-strategy-workload-shape-language-boundary.md),
this is squarely Go territory.

The probes themselves call into whatever language-shaped tooling
makes sense (a Rust probe binary if a probe needs to attach to a
running interceptor, etc.) — Go is the orchestrator language, not a
constraint on what probes can do.

## Open questions

- **Probe authoring cadence.** The catalog grows naturally as ADRs land. Whether to make "ship a probe with the ADR" a hard discipline (CI gate) or a soft expectation (review-time nudge) is a Phase 3 decision. Soft to start; promote to hard if drift accumulates.
- **Probe runner location.** v1 runs the chaos peer from a per-site mgmt-host. Some probes (silent-control-plane from outside-mesh) require an off-mesh vantage — they run from a dedicated EC2 micro-instance per cloud region. The mix of vantages and the orchestration between them is a 38E refinement.
- **False-positive rate.** A probe that fails intermittently due to legitimate transient state (e.g., during a controlled failover) creates noise. Mitigations: per-probe `cool_down` after detected legitimate transitions, per-probe failure-streak threshold before paging, manual silencing windows. v1 has the streak threshold; the rest accumulate as needed.

## Related artefacts

- ADR [`1013`](../adr/1013-continuous-architectural-chaos.md) — the decision
- ADR [`1008`](../adr/1008-soak-testing-discipline.md) — sibling discipline (correctness)
- ADR [`1012`](../adr/1012-load-testing-discipline.md) — sibling discipline (limits)
- ADR [`1011`](../adr/1011-findings-register.md) — where probe failures become durable
- Findings register INDEX — F-NNN entries
- [`engineering-health-assessment.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/engineering-health-assessment.md) — scorecard metric integration
- [`fabric-replay.md`](fabric-replay.md) — chaos can probe replayed historical state

---

*See `../adr/GLOSSARY.md` for terms and acronyms.*
