# Egress Proxy — operations runbook

Day-to-day procedures for the Block 17 egress-control stack. Use this
runbook to request an allowlist change, trigger break-glass, or
investigate a blocked request.

**Design:** [`../design/egress-proxy.md`](../design/egress-proxy.md)
**Tracker:** `../project/egress-proxy-implementation.md`

---

## Table of contents

- [1. Quick reference](#1-quick-reference)
- [2. How to request a normal allowlist change](#2-how-to-request-a-normal-allowlist-change)
- [3. How to trigger break-glass](#3-how-to-trigger-break-glass)
- [4. How to investigate a blocked request](#4-how-to-investigate-a-blocked-request)
- [5. How to investigate an audit-integrity alert](#5-how-to-investigate-an-audit-integrity-alert)
- [6. What the audit logs do and do not contain](#6-what-the-audit-logs-do-and-do-not-contain)
- [7. Failure modes + recovery](#7-failure-modes--recovery)
- [8. Scheduled maintenance](#8-scheduled-maintenance)

---

## 1. Quick reference

| I want to... | Where to go |
|---|---|
| Add a normal allowlist entry | §2 — submit via Web UI at `policy.<site>.netbird.legacy.example` or `db egress-policy submit` CLI |
| Trigger fast-path (urgent / SLA-breach) | §3 — submit with `--priority urgent`; any two non-requestor operators approve |
| Submit a batch | §2.4 — same submit, multi-entry YAML payload |
| See why a request was denied (Squid/dante) | §4 — query Loki for the source IP |
| Check current allowlist | Web UI active-policy view, OR `db egress-policy list --tier 2` |
| See metrics dashboard | Grafana → "Egress Proxy" board |
| Renew an expired entry | §2 — same submit; expire-scanner opens auto-removal PR for replacement |
| See pending requests | isitup widget shows count per site; click → site queue |

Emergency local override (logged, auditable): on a mgmt-host as root,
`systemctl stop egress-firewall.service`. This is the escape-hatch of
last resort — breaks Island Mode posture, creates an incident, and
requires post-hoc documentation within 24h.

**The approval mechanism is the per-site policy engine, not GitHub.**
A partitioned site (no WAN) can still accept submissions and
approvals locally; Spock replicates them to peer sites on reconnect.

---

## 2. How to request a normal allowlist change

Normal path = role-holder reviews + approves within 24h SLA. Single
approval is sufficient — the role carries the formal responsibility.
Use this for any non-urgent allowlist change.

### 2.1 Submit (Web UI or CLI — pick whichever)

**Web UI:** browse to `policy.<site>.netbird.legacy.example`. Click
"New request." Fill in: tier, payload (sni / cidr / port / protocol),
reason, owner, expires, priority=normal. Submit.

**CLI:**

```
db egress-policy submit \
  --tier 2 \
  --sni "example-service.com" \
  --reason "Why this is needed (specific ops context, not 'for X')" \
  --owner @your-oidc-sub \
  --expires 2026-10-31 \
  --priority normal
```

Returns a request ID. Engine writes `egress_policy` row (state=pending),
notifies role-holders via email + Matrix + isitup widget.

### 2.2 What the role-holder sees

- Email to their per-site address (subject: `[policy] new request <id>`)
- Matrix message in `#policy-alerts:<site>.netbird.legacy.example`
- isitup widget pending-count increments
- Web UI queue shows the request at top

Click into the request → see details + diff preview + reason.
Approve / Reject / Comment buttons. Approval is server-side signed via
the role-holder's session OIDC token.

### 2.3 Tier-by-tier specifics

**Tier 2 (HTTP/HTTPS, most common):** SNI patterns; supports `*.host.com`.

**Tier 3 (SOCKS5):** rare; if the destination speaks HTTPS, use Tier 2.

**Tier 4 (direct CIDR — last resort):** must include explicit `expires:`
(no `review-annual`); must include ticket reference; must include
explanation of why neither Tier 2 nor Tier 3 works.

### 2.4 Batch submission

```
db egress-policy submit --batch policy-batch.yaml
```

`policy-batch.yaml` is a multi-entry document — one entry per allowlist
addition. Reviewer sees them as a per-row checklist on the same screen;
can approve/reject each individually or batch-approve all. Mixed
priorities (some normal, some urgent) supported.

### 2.5 SLA + auto-escalation

Timer starts at submission timestamp. Warning ping at T+22h. SLA breach
alert at T+24h, AND the request automatically becomes eligible for the
fast path (§3) — any two non-requestor operators can then approve it
to unstick the queue.

---

## 3. Fast-path approval (urgent or SLA-breached)

Fast path = two-of-N peer approval, immediate activation. Use for:

- Genuine urgency (incident response, partner delivery time-window)
- Requests that breached the 24h SLA on the normal path
- Any situation where the role-holder is unreachable and waiting is
  not viable

**When NOT to use.** Convenience. "Faster than waiting for the
role-holder." If the situation can wait, use the normal path so the
role-holder retains accountability.

### 3.1 Procedure

1. Submit with `--priority urgent` (or, if a normal-path request
   already breached SLA, just have two operators approve it — the
   `fast_path_eligible=true` flag is automatically set).
2. State the urgency in the `reason` field — what makes this
   time-bounded, what blocks waiting.
3. Any two distinct operators (≠ requestor) approve via Web UI or CLI.
4. On the second approval, engine activates the policy immediately.
   Ansible coordinated-upgrade picks it up within ~15 min.
5. **Within 7 days,** post-action review:
   - The role-holder retroactively confirms the change in the audit
     record, OR
   - An explicit "this should not have used the fast path" note is
     logged and a process-improvement issue opened.

Fast-path requests without a post-action review in 7 days trigger
`PolicyFastPathReviewOverdue` alert.

### 3.2 What "any two operators" means

Operator = OIDC `sub` claim with the configured operator group
membership. Roster is at `ops/policy-engine/approvers.yaml` per site,
plus cross-site approvers are explicitly allowed (an operator at one site
can approve another site's fast-path request — the audit record carries
`cross_site=true`).

Server-side enforcement: requestor cannot approve their own request;
two approvals must come from two distinct OIDC subjects. The Web UI
greys out the approve button if you're the requestor.

---

## 4. How to investigate a blocked request

### 4.1 Operator reports "my service can't reach X"

1. Get from the operator: source host, destination (SNI or IP),
   approximate timestamp, and which service is trying to reach it.
2. Open Grafana → "Egress Proxy" → "Recent Denies" panel. Filter by
   source mesh IP (peer name lookup is automatic).
3. You'll see one of:
   - **Tier 0/1 deny** — DNS or firewall blocked it. Check if the
     destination should be in a bypass list (observability, NTP,
     Netbird control plane) vs. proxy-routed. If proxy-routed, the
     app isn't using `HTTP_PROXY` — check the
     `egress-proxy-client` Ansible role.
   - **Tier 2 deny (Squid TCP_DENIED)** — SNI not in allowlist.
     Decide: is this a legitimate new destination (→ §2) or an
     unexpected one (→ §4.2)?
   - **Tier 3 deny** — same analysis for SOCKS5.
4. Query for context: `{app="squid"} |= "<source mesh IP>"` in Loki
   shows the full decision trail around the event.

### 4.2 Unexpected destination — possible compromise

If the source host is attempting to reach a destination that has no
operational reason to be allowed:

1. **Do not add it to the allowlist.** Do not assume the operator is
   right.
2. Check Alertmanager for `EgressUnexpectedDestination` firing for
   this source.
3. Cross-reference the source host's recent activity:
   - auth events (who logged in recently?)
   - sudo history
   - process tree (if accessible)
   - container restart / pull events
4. If the pattern suggests compromise, escalate per the incident-
   response runbook (Block 18 item — in progress). Do **not** silently
   block further or the attacker learns they've been detected.

---

## 5. How to investigate an audit-integrity alert

`EgressAuditIntegrityAlert` means the daily tamper-detection CI
found a chunk whose hash doesn't match the integrity ledger.

**Assume compromise until proven otherwise.**

1. Identify which chunk + which mgmt-host the mismatch was found on.
2. **Do not** log into the suspect mgmt-host via normal SSH — that
   creates new events on a possibly-hostile system and alerts the
   attacker.
3. From the peer mgmt-host (the one that holds the integrity copy),
   pull the original chunk from the MinIO bucket.
4. Compare to the on-disk Loki chunk on the suspect host via
   out-of-band channel (SSM session, not SSH — see CLAUDE.md "VPS
   Shell Access" for the SSM pattern, adapted for mgmt-host).
5. If the Loki chunk on the suspect host has been modified while the
   MinIO copy has not, you have a tamper event. Escalate per IR
   runbook.
6. If the MinIO copy has also been modified (Object Lock violation
   somehow), you have a fundamental integrity failure — the attacker
   has service-account creds to MinIO. Full site compromise
   assumption.

---

## 6. What the audit logs do and do not contain

Incident responders often ask for detail we don't have. Know the
boundaries before you promise an answer.

**What we log for HTTPS traffic (Tier 2):**

- Timestamp (millisecond precision)
- Source: mesh IP + resolved peer name
- Destination: SNI (from ClientHello), resolved IP, port
- Bytes up + bytes down
- Decision: TCP_TUNNEL / TCP_DENIED
- Rule ID (which allowlist entry matched, or "default-deny")
- Latency (ms)

**What we do NOT log for HTTPS (no-MITM trade-off — see design §2.2):**

- HTTP method (GET / POST / PUT)
- URL path
- Query parameters
- Request headers
- Request body
- Response body

**What we log for HTTP traffic (Tier 2, rare — most egress is HTTPS):**

Everything in the HTTPS list PLUS method, URL path (minus query by
default — configurable), and response code.

**What we log for SOCKS5 (Tier 3):**

Connection metadata only — timestamp, source peer, destination host +
port, bytes transferred, decision. No protocol-level content.

**What we log for firewall tiers (0, 1, 4):**

Per-packet at source+dest granularity with a rate-limit to prevent
log flooding. No payload.

If a compliance investigation requires method/path detail, that's the
trigger to revisit MITM (see design §9).

---

## 7. Failure modes + recovery

### 7.1 Squid down on Router-A; Router-B healthy

Keepalived failover is automatic. VIP moves to Router-B within ~3s.
Peers see a brief connection reset; apps retry and succeed.

Recovery: investigate why Squid died on Router-A (check
`journalctl -u docker`, `docker logs squid`). Restart. Keepalived
stays on B until manual `systemctl restart keepalived` on A reclaims
the VIP.

### 7.2 Squid down on both routers

Site loses HTTP/HTTPS egress. Mesh, observability, NTP, backups
continue via bypass list. **This is the design's fail-closed
behavior** — it's the correct outcome, not a bug.

Recovery: same as §7.1 but in parallel. If both Squid instances are
crashing on the same signal (e.g., bad allowlist), revert the last
allowlist PR on main — `coordinated-upgrade.yml` re-applies and both
Squid instances come back.

### 7.3 `EgressPolicyDrift` firing between sites

Sites have different rule-set hashes. Either a site missed a
coordinated-upgrade run, or someone edited config directly on a host
(local override — logged but possible).

Recovery:

1. Check `coordinated-upgrade.yml` status in Ansible AWX / logs.
2. Re-run `ansible-playbook -i inventory/hosts.ini egress-enforce.yml
   --limit <drifting-site>`.
3. If config was edited locally, audit-integrity will have captured
   it in the config-change event class. Open an incident — this is a
   bypass of the approval workflow.

### 7.4 MinIO (audit-integrity tier) down

Integrity writes pause. Loki continues to work normally. Alert
`EgressAuditIntegrityDown` fires. This is degraded — the site is
still secure (Tier 2 still enforcing) but the audit guarantee for the
outage window is weakened.

Recovery: restart MinIO. Once back up, backfill the ledger from
Loki (integrity CI has a `backfill` mode). Document the outage
window in the incident log.

### 7.5 Role-holder unavailable + urgent change needed

The 24h SLA exists exactly for this. If the role-holder is unreachable:

1. Use the fast path (§3) — any two non-requestor operators can
   approve immediately.
2. If two approvers are also unreachable: wait for the SLA breach
   (T+24h from submission) — auto-escalation makes any two operators
   eligible.
3. If both above fail and the situation is genuinely urgent:
   emergency local override (§1 quick reference, last row). This
   creates an incident that MUST be documented post-hoc.

The whole point of the dual-path design is that you never have to
edit iptables at 3am. The fast path IS the 3am answer; use it.

---

## 8. Scheduled maintenance

### 8.1 Quarterly DoH blocklist refresh

CI job pulls the current known-public-DoH IP list, generates a PR.
Review + merge like any allowlist change. No on-call work required.

### 8.2 Annual allowlist review

`expire-scanner.sh` opens a reminder PR for every entry with
`expires: review-annual` that hasn't been reviewed in the last year.
Owner responds to the PR with either "still needed" (re-dates the
review timer) or "no longer needed" (removal).

Unaddressed review-annual PRs after 30 days trigger
`EgressAllowlistReviewOverdue` alert.

### 8.3 Squid / dante / squid-exporter image updates

Standard Chainguard image-pin-bump workflow (see Block 13B). Image
pin PR → CI runs `ops/ci/check-pin-compat.sh` → two-of-N approval →
merge → coordinated-upgrade canary one site, 24h soak, then fleet.

### 8.4 Approver roster maintenance

When an operator joins or leaves: update
`ops/policy-engine/approvers.yaml` same day. Minimum 1 named
role-holder per site (preferred 2-3); cross-site approvers explicitly
allowed and useful for TZ coverage. The roster is itself two-of-N
protected (changes require approval through the same engine). Bootstrap
chicken-and-egg is handled by an initial-deployment override + audit
log entry on first import.

If coverage drops to a single named role-holder per site,
`PolicyApproverRosterUnderstaffed` alert fires and shoring up the
roster becomes priority. The fast path remains available so this is
degraded, not broken.

### 8.5 Spock conflict review (per-week cadence)

`PolicyConflictResolved` and `PolicyConflictDenied` alerts fire on any
post-partition merge anomaly. Weekly cadence: review the conflict log
in the Web UI; confirm CRDT resolution was correct; if it wasn't, open
a process-improvement issue. These should be rare in steady state.
