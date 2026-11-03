# Zenoh Dead-Letter Queue

> **⚠ Superseded — do not build (2026-06-09, [ADR 1022](../adr/1022-zenoh-denial-observability.md), [ADR 0510 status](../adr/0510-zenoh-dlq.md)).** A DLQ is the *wrong category* for our failure mode: Zenoh is a routing **fabric, not a broker**, with no native dead-letter (F-178), and admission rejection at the ACL is a produce-side condition, not a consumer-side delivery-attempt one. The canonical answer is the **expected-vs-actual write-denial monitor** + **producer-cooperation** — [ADR 1022](../adr/1022-zenoh-denial-observability.md). Producer-cooperation now has a native continuous-stream form ([ADR 1040](../adr/1040-zenoh-advanced-pubsub-liveliness-adoption.md) gap 5): owned producers publish via `AdvancedPublisher` (per-producer cache + sample-miss sequencing) so a catch-up subscriber recovers known-missed samples from the producer's own session — no queue, no broker; acked Query/Reply stays the explicit one-shot path (`goat pub --confirm`). This doc is retained for history + the *deferred-forensic* byte-capture option only; do not add new `@/dlq/**` topics, and read the brokerless map first ([`zenoh-brokerless-consequences.md`](zenoh-brokerless-consequences.md)).

**Scope:** the architectural pattern for handling samples that fail validation at any point in a Zenoh router's interceptor chain. Captures the sample + reason + metadata to a bounded, queryable DLQ topic instead of silently dropping.

**Status:** **Superseded by [ADR 1022](../adr/1022-zenoh-denial-observability.md)** (was "mainline"; the `ntp_degraded`/`no_subscribers` capture is now the expected-vs-actual monitor, not a DLQ topic).

**Why this exists:** a silently-dropped sample is indistinguishable from a never-produced sample at the subscriber side. In a safety-relevant system, that ambiguity is operationally dangerous. A DLQ preserves the evidence: what was produced, why it didn't reach the subscriber, when, and by which interceptor.

> **Not a broker DLQ — scope boundary.** Zenoh is **brokerless**: `put` is fire-and-forget, storages are caches not queues, and there is **no native dead-letter primitive** (F-178). So this is **not** a Kafka/SQS-style delivery-failure DLQ. It is a narrower, application-level capture of *validator-interceptor* failures (`ntp_degraded`, schema/cert rejects). The general "did my publish land?" question is answered by **producer-cooperation** (acked Query/Reply) + the **expected-vs-actual monitor** ([ADR 1022](../adr/1022-zenoh-denial-observability.md)), not a queue. This doc is one corner of the broader brokerless picture — see [`zenoh-brokerless-consequences.md`](zenoh-brokerless-consequences.md) for how this relates to the QoS, dynamic-ACL, and silent-denial work.

---

## Table of contents

1. [Topic scheme](#1-topic-scheme)
2. [Reason codes and retention](#2-reason-codes-and-retention)
3. [Payload wrapper](#3-payload-wrapper)
4. [Bounds and back-pressure](#4-bounds-and-back-pressure)
5. [Interceptor chain integration](#5-interceptor-chain-integration)
6. [`no_subscribers` as a special case](#6-no_subscribers-as-a-special-case)
7. [Consumers](#7-consumers)
8. [Security properties](#8-security-properties)
9. [Phasing](#9-phasing)
10. [References](#10-references)

---

## 1. Topic scheme

```
@/dlq/{reason}/{original-key-expression}
```

Examples:

- `@/dlq/ntp_degraded/sensor/acoustic/site-a/node-01`
- `@/dlq/schema_failure/track-correlation/v2/site-b`
- `@/dlq/cert_invalid/control/command-auth`
- `@/dlq/no_subscribers/health/mgmt-site-a-01`

Operators query `@/dlq/{reason}/**` to scope by failure class. Automated consumers subscribe to specific reasons they care about (e.g., replay workflows).

---

## 2. Reason codes and retention

Initial set (extensible). New interceptor types add new reason codes.

| Reason code | Triggered by | Default retention | Notes |
|---|---|---|---|
| `ntp_degraded` | Sample dropped because producer's `ntp_class ≠ healthy` and destination topic has drop-on-degraded policy | 24h | Candidate for replay when producer's NTP recovers |
| `schema_failure` | Payload failed schema validation interceptor | 7d | Debug-usable window; typically a producer bug |
| `cert_invalid` | Producer cert chain invalid or not trusted | 30d | Audit-relevant |
| `cert_expired` | Producer cert past validity window | 30d | Audit-relevant |
| `cert_revocation` | Producer cert on CRL | 30d | Audit-relevant; security signal |
| `no_subscribers` | Publication had no live or storage subscriber at time of publish | 1h | See §6 |
| `rate_limited` | Per-producer rate limit exceeded | 1h | Ops signal |
| `policy_deny` | ACL / policy interceptor denied transit | 30d | Audit-relevant |

Retention is per-reason. Configurable via router config; defaults above.

---

## 3. Payload wrapper

DLQ samples carry the original payload plus metadata:

```
{
  "original_key": "sensor/acoustic/site-a/node-01",
  "original_payload": <bytes>,
  "original_attachment": {
    "ntp_class": 2,
    "producer_epoch": 47
  },
  "original_producer_id": "site-a-sensor-07",
  "dlq_reason": "ntp_degraded",
  "dlq_timestamp": "2026-04-20T15:42:03.127Z",
  "interceptor_position": "ntp-policy-interceptor",
  "routing_path": ["site-a-router", "site-b-router"]
}
```

The wrapper preserves:

- **What** was published (original key, payload, attachment)
- **Who** published it (producer ID for attribution)
- **Why** it failed (reason code + which interceptor made the call)
- **When** it was intercepted
- **Where** it traveled before being caught (routing path — useful for cross-site DLQ analysis)

Subscribers unwrapping see the original sample plus reason metadata.

---

## 4. Bounds and back-pressure

DLQ storage is finite. Adversary-flood protection is required.

- **Per-site total cap** — default 1 GB, configurable. Oldest entries purged on overflow.
- **Per-producer rate limit** — default 100 DLQ entries/min per producer. Overflow drops without wrapping; a counter increments; alert fires.
- **Per-reason TTL** — see §2. Audit-relevant reasons retain longer.
- **Capacity alerts** — 50%, 80%, 100%. Feeds IOC watch (`federation-security.md` §O2).
- **Novel-reason alerts** — a DLQ reason code never seen before at a site is alert-worthy. Tooling signs a fingerprint on first occurrence.

---

## 5. Interceptor chain integration

The DLQ is implemented as a **single late interceptor in the chain**. Upstream interceptors don't drop — they tag the sample with `drop_intent=<reason>`. The DLQ interceptor reads the tag; if set, wraps + forwards to `@/dlq/...` and does NOT forward to the original destination. If not set, the sample passes through to output normally.

Benefits of centralized DLQ:

- Each validator interceptor is focused on one check — no per-interceptor DLQ plumbing.
- Reason codes and DLQ policy live in one place.
- Adding a new validator = set a new `drop_intent` code + add reason to §2 table. No DLQ code changes.

Example chain at a producer site:

```
sample in →
  NTP attestation interceptor (attach ntp_class + epoch) →
  Policy enforcement interceptor (may set drop_intent=policy_deny) →
  Schema validation interceptor (may set drop_intent=schema_failure) →
  Cert validation interceptor (may set drop_intent=cert_*) →
  Rate-limit interceptor (may set drop_intent=rate_limited) →
  NTP policy interceptor (per-topic; may set drop_intent=ntp_degraded) →
  DLQ interceptor (routes drop_intent → DLQ; otherwise passes) →
  Audit interceptor →
sample out
```

---

## 6. `no_subscribers` as a special case

Zenoh's native behavior: if no subscriber is connected for a key expression when a publication occurs, the sample is lost. There is no persistence unless a Zenoh storage plugin is configured.

**In goat deployments**, this creates a silent-failure class that our DLQ treats explicitly.

**Mechanism:** a `@/dlq/no_subscribers/**` storage plugin runs per site, listening for publications that:

1. Have no live subscriber in the router's routing table at publish time, AND
2. Have no storage plugin configured for that key expression

Such publications are forwarded to `@/dlq/no_subscribers/{original-key}` with the standard wrapper.

**Use cases this exposes:**

- Mis-configured subscribers (topic typo, subscription not started)
- Cross-site sub-pub where the subscriber site is unreachable
- Race conditions at startup (producer online before subscriber)
- Deprecated topics still being published

**Retention for `no_subscribers` is intentionally short** (1h default) — these are not per-se security-relevant; they're debugging signal. Long retention would accumulate noise.

---

## 7. Consumers

| Consumer | Pattern | Purpose |
|---|---|---|
| Operator CLI / dashboard | `zenoh get '@/dlq/{reason}/**'` | Ad-hoc forensic query |
| Automated replay workflow | Subscribe to specific reasons; opt-in per reason | On NTP recovery, replay recent `ntp_degraded` samples; on schema fix, replay `schema_failure`. **Replay is never automatic by default** — enabled per reason by operator decision. |
| Audit log integration | Subscribe to security-sensitive reasons | `cert_*`, `policy_deny` entries feed the tamper-evident audit log (`federation-security.md` S3, S12) |
| Prometheus exporter | Scrapes DLQ size + rate | Alert rules on capacity, rate spikes, novel reasons |

---

## 8. Security properties

- **Reverse-intelligence surface.** DLQ content reveals what data was being produced and what validation failed. An adversary who reads DLQ learns operationally relevant information. Access is restricted accordingly.
- **ACL on `@/dlq/**` read access** — operator role + audit service only. No general-purpose subscriber can read DLQ.
- **Write-only at runtime** — DLQ is populated by the router's interceptor. Operator roles cannot inject samples into DLQ, preventing a compromised operator from seeding false forensic evidence.
- **Audit-sensitive entries tamper-evident** — `cert_*` and `policy_deny` entries are immediately shipped off-host via the existing tamper-evident audit log pipeline, in parallel with DLQ retention. A compromised site cannot rewrite these by tampering with DLQ storage.
- **Replay guardrails** — automated replay is opt-in per reason. A replay-all-from-DLQ default would be a disaster vector (replay post-recovery floods consumers with historical data that subscribers didn't want).

---

## 9. Phasing

### Phase 2 rapid (~1 week)
- DLQ interceptor skeleton
- Bounded local storage plugin (`@/dlq/**`)
- Prometheus metrics (DLQ size by reason, rate by producer, capacity %)
- Initial reasons: `ntp_degraded`, `no_subscribers`
- Operator CLI examples

### Phase 3
- Remaining reason codes (`schema_*`, `cert_*`, `policy_*`) added as their validator interceptors come online
- Audit log integration for security-sensitive reasons
- Per-reason TTL policy enforcement
- Novel-reason alerting

### Phase 3+
- Automated replay workflows (opt-in per reason)
- Cross-site DLQ aggregation for forensic analysis
- Replay rehearsal drills (part of quarterly drill cadence)

---

## 10. References

- `docs/design/zenoh-data-fabric.md` — parent doc for the Zenoh data backbone
- [`docs/design/zenoh-ntp-integration.md`](zenoh-ntp-integration.md) — NTP hygiene integration (`ntp_degraded` is one DLQ reason)
- `docs/design/federation-security.md` — audit log pattern (S3, S12) and IOC watch (O2)
- [`docs/design/universal-ntp-hygiene.md`](universal-ntp-hygiene.md) — source of `ntp_degraded` reason semantics
- [Zenoh storage plugins](https://github.com/eclipse-zenoh/zenoh-backend-filesystem) — storage backend reference for DLQ persistence
