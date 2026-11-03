# ADR 1192 — The silent control plane (wg-cp0) is paused; goatnets enter through a public DNS name on every relay

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-09-28 |
| Deciders | operator |
| Repo scope | goat core (per-goatnet Terraform trees, `docs/design/wg-cp0-prod.md`, `docs/design/threat-model.md`) |
| Pauses | [ADR 0313](0313-silent-control-plane-outer-wg-wrap.md) (outer WireGuard wrap) and [ADR 0314](0314-no-public-dns-for-control-plane.md) (no public DNS for the control plane). Both stay on file as the design to re-apply; neither is superseded |
| Related | [ADR 0321](0321-wg-cp0-perimeter-not-acl.md) · [ADR 0322](0322-wg-cp0-declarative-substrate.md) · [ADR 0312](0312-dns-two-tier.md) · `wg-cp0-prod.md` · `threat-model.md` · `dns-service-discovery.md` · issue #1506 |

## TL;DR (for a layperson)

For a while the management side of the network sat behind a second, hidden
encrypted tunnel, so that someone without a signed enrolment bundle could not
even see it existed. That second tunnel is switched off for now, to keep the
system simpler to run. New devices instead find the network through an ordinary
public web address that points at every cloud relay at once, so losing one
relay does not close the door. The hidden-tunnel design is kept on file and can
be switched back on if a deployment needs it.

## Decision

1. **wg-cp0 is paused, not retired.** No goatnet runs it: the production goatnets set
   `wg_cp0_enabled = false` in their Terraform trees, and the servers it ran on were
   retired. The
   reason is simplicity: it was a second WireGuard tunnel wrapped around the
   NetBird tunnel, with its own bundle ceremony, key rotation and allow-list
   push, and its key rotation was fragile.
2. **The entry point is a public DNS name, answered by every relay.**
   `netbird.<zone>` has one A record per relay (`round_robin_dns_name = "netbird"`
   in the relay module). A device that
   cannot reach one relay retries another. The name leads to TLS on `:443`,
   rathole passes it through to the host site's management plane unread, and
   admission needs a short-lived setup key. No host site has a public address.
3. **DNSSEC is part of the entry-point design.** A goatnet zone is signed by
   declaring a `cloudflare_zone_dnssec` resource in its Terraform tree and
   registering the DS record at the registrar. A zone without it leaves a device
   trusting the enrolment name on TLS alone.
4. **Reactivation is a deliberate decision, not drift.** wg-cp0 comes back when
   a deployment needs the management plane to be invisible to scanning and
   passive collection, and that need outweighs the operating cost in item 1.
   The design, the address plan
   (`ops/ansible/inventory/wg-cp0-prod.ini`) and the relay modules in
   `terraform-prod`'s git history are what it is rebuilt from. Re-enabling it is
   a new ADR that lifts this pause.

## What this changes

- Documents that credit wg-cp0 or "no public DNS for management" as a control
  in force now say it is paused, and name the public entry point as the control
  in force: `threat-model.md`, `wg-cp0-prod.md`, `architecture-spine.md` row 12,
  `CLAUDE.md`.
- ADRs 0313 and 0314 keep status **Proposed**, with a line pointing here.
- Nothing changes on a live goatnet.

## Known gaps

| Gap | State | Closes when |
|---|---|---|
| goat-client still ships a `wg-cp0-only` mode | Built, unused on current goatnets | Kept for reactivation; no action |

## Alternatives

- **Keep wg-cp0 running.** Rejected for now: its operating cost (bundle
  ceremony, rotation, allow-list push) was high, and the relays'
  public `:443` has to exist anyway for enrolment.
- **Retire wg-cp0 permanently and delete the design.** Rejected: invisibility to
  scanning is worth having for some deployments, and rebuilding the design from
  nothing costs more than keeping it on file.
