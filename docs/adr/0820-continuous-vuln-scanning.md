# ADR 0820 — Continuous vulnerability scanning — Trivy + grype + osquery drift

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | `0704-multi-scanner-regime.md`, [`0800-open-source-edr.md`](0800-open-source-edr.md) |

**Implementation status (2026-10-13):** The decision below describes the
target continuous-scanning design, not the deployed state. A non-blocking
Trivy advisory workflow currently scans the checkout and a curated set of
published images on relevant PRs, weekly, and on manual dispatch. Daily scans
of deployed images, per-image build gates, grype, and osquery config-drift are
not implemented. See ADR 0704 for CI details.

## TL;DR (for a layperson)

The target design adds continuous scanning to build-time scanning
(`0704`). Under this design, Trivy and grype scan
running images for newly-disclosed CVEs against currently-
deployed digests; osquery checks each host's actual state for
configuration drift. This is intended to catch problems that emerge after
build (newly-disclosed vulnerabilities, drift from baseline).

## Context

Build-time scanning catches known CVEs at build. New CVEs are
disclosed continuously. A system clean at build may be
vulnerable a week later. Continuous scanning closes the gap.

Block 18H in the implementation plan tracks rollout.

## Decision

Continuous vulnerability scanning, paired with Block 18G
runtime detection:

| Scanner | Frequency | Scope |
|---|---|---|
| **Trivy (continuous)** | Daily | Images currently deployed (manifest-based scan; no disk scan). |
| **grype (continuous)** | Daily | Same. |
| **osquery config drift** | Continuous (push) | Compares host state to baseline; alerts on drift. |

Findings ship to:

- **Alert routing**: Alertmanager
  ([`0815`](0815-observability-stack.md)) — high/critical
  CVE in deployed image is a paging alert.
- **Audit-integrity tier** ([`0811`](0811-audit-log-integrity.md))
  — every finding recorded for trend analysis.

## Alternatives considered

- **Build-time scanning only.** Misses post-build CVE
  disclosures. Rejected.
- **Cloud-based scanner SaaS.** Sovereignty issue.
  Rejected.
- **Single scanner.** Misses categories
  (`0704` reasoning
  applies). Rejected.

## Consequences

**Positive.** Newly-disclosed CVEs surface within 24 hours.
Configuration drift surfaces immediately. Findings feed
remediation pipeline.

**Negative.** Alert noise during CVE-disclosure storms.
Mitigated by deferred-list policy
([`0707`](0707-rust-toolchain-pinning.md) is the
pattern); high/critical pages, low/medium aggregates to
weekly review.

## Assessor view (SCA-R)

Continuous scanning serves RA-5 (vulnerability scanning —
continuous), SI-2 (flaw remediation — feeds the
remediation pipeline), CA-7 (continuous monitoring), and
CM-8 (component inventory drift detection).

## Trigger conditions for revisit

- Alert volume becomes unsustainable. Tune; do not weaken
  coverage.
- A scanner database becomes unreliable. Substitute.

---

*See `GLOSSARY.md` for terms and acronyms.*
