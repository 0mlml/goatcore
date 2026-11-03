# ADR 1140 — TLS issuer of record: the ACME zone answers it, and Let's Encrypt is never authorization-bearing

- **Status:** Accepted
- **Date:** 2026-08-19
- **Deciders:** platform
- **Related:** ADR 0400 (sovereign per-site PKI), ADR 0406 (offline CA / YubiKey PIV),
  ADR 0407 (internal X.509 CA for service TLS), ADR 0314 (no public DNS for the control plane),
  ADR 0814 (Traefik + Caddy two-layer proxy), ADR 0843 (public mTLS crutch tier),
  ADR 1079 (zenoh PQ hybrid TLS),
  [internal-tls-ca.md](../design/internal-tls-ca.md),
  F-071,
  F-089,
  F-312

## Context

Nothing here is a new decision. Every rule below was already made — in ADR 0400,
ADR 0407 and the two playbooks — and none of it was written in one place. The
cost of that showed up twice on 2026-08-18, when two unrelated commits both
landed on the same fault line without either naming it: `6ff2b3adc` fixed a TLS
verify race on the internal-CA path, and `4a083a63b` corrected `cert-sign` flag
drift documented in `dashboard-deploy.yml`. Between them sits a question neither
had to answer and everyone eventually asks: **which certificate authority is
supposed to issue for this hostname?**

Four things make that genuinely hard to answer from the tree.

**1. Two playbooks configure the same component.**
[`traefik-acme.yml`](../../ops/ansible/traefik-acme.yml) renders a compose
overlay giving Traefik an ACME resolver named `le` (Let's Encrypt, Cloudflare
DNS-01). [`lan-traefik-routes.yml`](../../ops/ansible/lan-traefik-routes.yml)
delivers a sovereign-CA multi-SAN `mgmt-identity` leaf as a file cert and says
in its own header "internal-CA file certs, no ACME". Same Traefik, same `:443`,
same class of browser-facing hostname, two issuers. The second calls itself
"the LAN sibling of `traefik-acme.yml`" — which is correct, and is the only
place the relationship is stated.

**2. The canonical role points one way and the live goatnets point the other.**
`traefik-acme.yml`'s header records that ACME is deliberately an overlay rather
than an `acme_mode` switch inside `ops/ansible/roles/traefik/`, "which today is
wired for offline-CA leaf certs per ADR 0407 + Block 22J". So reading the role
tells you the fleet uses the sovereign CA; reading a live goatnet's mgmt host
tells you it uses Let's Encrypt. Both are true, on different hosts, and neither
source says so.

**3. `TRAEFIK DEFAULT CERT` is the shared failure symptom of both paths.**
On the ACME path it means the overlay was dropped by an explicit `-f` set
(F-312). On
the internal-CA path it means the file provider had not yet loaded the dynamic
TLS config when the probe ran (`6ff2b3adc`, observed twice on a fresh
standup). Identical string, unrelated causes, different issuers — so the symptom
tells you nothing about which CA was supposed to be there.

**4. A stale status line points away from the sovereign CA.**
F-071
still reads `Status: Active — operator-side workaround documented (use
Safari/Firefox)`. Read alone, it says our own CA cannot front a browser. That
has not been true since 2026-05-09: `cert-sign` defaults to `--key-type auto`,
which selects ECDSA-P256 for any leaf whose EKU includes `serverAuth`
([`ops/enrollment/cmd/cert-sign/main.go:324-340`](../../ops/enrollment/cmd/cert-sign/main.go),
F-089
Resolved), and Block 79A hard-replaced Ed25519 with ECDSA P-256 across the
enrollment stack. Chrome accepts a leaf minted today.

The trap underneath all four: the obvious discriminator — "is this hostname
reachable from the public internet?" — **is wrong**, and quietly so. DNS-01
proves control of a *zone* by writing a TXT record, not control of a host. A
mesh-only name like `dashboard.zagros.netbird.example.net` is inside a
Cloudflare zone we hold, so Let's Encrypt will happily issue for it even though
nothing outside the mesh can reach it. A goatnet on the ACME path carries **two** LE
certs for exactly this reason — a public anchor and a mesh-only anchor, in
`templates/traefik-acme/mgmt-dynamic.yml.j2`.

## Decision

**The issuer is determined by one question: is this hostname's parent zone a
Cloudflare zone we control, such that a DNS-01 TXT record can be written for
it?** Reachability is not the question. Neither is browser trust.

| Surface | Issuer | Mechanism |
|---|---|---|
| Any hostname whose parent zone is a Cloudflare zone we hold — public-reachable *or* mesh-only | **Let's Encrypt** | `traefik-acme.yml` → `certificatesresolvers.le`, DNS-01 |
| Hostnames with no Cloudflare-controlled parent zone — LAN-enrollment / island goatnets (`goat.lan`), and the wg-cp0 control-plane names ADR 0314 keeps out of public DNS by design | **Sovereign CA** | `lan-traefik-routes.yml` → `cert-sign` `mgmt-identity` leaf, Traefik file provider |
| Peer, operator, machine and workload identity; zenoh mTLS; the crutch tier's **client** certs | **Sovereign CA, always** | offline ceremony (ADR 0406 / 0407) |

Four corollaries, each of which someone has re-derived at least once:

- **Let's Encrypt is never authorization-bearing.** ADR 0400 already says it:
  a public CA is "used for service-facing TLS (Traefik DNS-01); not used for
  peer / operator identity." Nothing on any goatnet grants access because an LE
  certificate validated. An LE failure degrades to a browser warning; it never
  costs reach.
- **We do not add a second public CA.** Traefik does not fail over between
  `certificatesresolvers` — a second one buys a faster *manual* switch, not
  redundancy. And the shared failure domain is not Let's Encrypt: it is the
  Cloudflare DNS-01 token every goatnet renews through. A second CA does not
  touch that.
- **The key algorithm is not the discriminator.** ECDSA-P256 is already the
  default for `serverAuth` leaves. Do not reach for Let's Encrypt on the belief
  that browsers reject our own certificates.
- **One hostname, one issuer.** Do not mix. Where the ACME path applies,
  `tls.domains` must be declared exactly once per hostname across the dynamic
  config — overlapping SAN lists fire concurrent ACME orders that collide on the
  same Cloudflare TXT record (`81058`), and `Host()` rules must carry no IP
  literals, which LE rejects as `rejectedIdentifier`.

## Alternatives considered

- **Sovereign CA everywhere; drop Let's Encrypt.** Every browser reaching a
  hosted surface would need our root installed — including partner, candidate
  and assessor browsers we do not administer. Rejected: the trust-distribution
  cost lands on people outside our control.
- **Let's Encrypt everywhere; drop the internal CA for service TLS.** Requires
  a public zone for control-plane names, which ADR 0314 removes deliberately,
  and puts a WAN dependency in front of island-mode goatnets that have no
  Cloudflare zone at all. Rejected.
- **A second public CA as a standing fallback resolver.** No automatic failover,
  and it does not address the Cloudflare token. Rejected as a default; it stays
  available as a manual recovery step if the `le` resolver is ever unusable.

## Consequences

**Positive.** One question resolves the issuer for any hostname on the fleet.
The two playbooks read as siblings covering disjoint cases rather than as rival
approaches. `TRAEFIK DEFAULT CERT` becomes a two-branch diagnosis — check which
path the host is on *first*, then check F-312 or the reload race, not both.

**Negative, accepted.** Renewal on the ACME path needs WAN plus Cloudflare
reachability at renewal time. Certificates are 90 days and Traefik renews at 30
days remaining, so a site partitioned longer than roughly 60 days serves an
expired certificate on those hostnames. This is accepted because the affected
names are the ones anchored in a public zone; a partitioned site's public
surface is already unreachable from outside. Island-mode goatnets are on the
sovereign CA and are unaffected.

The 30-day runway only helps where something is watching.
`ops/compose/goat-obs/prometheus/rules/tls-cert-renewal.yml` alerts when renewal
*stops* rather than when a certificate is merely expiring. On a goatnet running
the `obs-host` role, the role's blackbox-exporter and `blackbox-https` job sit
behind `obs_blackbox_enabled`, and `tls-cert-renewal.yml` is appended to the
rule set by that same flag so the alert and the metric it needs can never ship
apart. Adding the rule file alone to a Prometheus with no blackbox job is wrong:
it can never satisfy the rule's `absent()` guard and would fire forever. A
goatnet declares one probe target per `probe_gate` vhost, and
`ops/ci/check-blackbox-covers-registry.sh` fails the build if the probe list and
the service registry drift. Coverage per goatnet is a row in
[`ops/pins/parity-capabilities.yaml`](../../ops/pins/parity-capabilities.yaml).

## What this ADR does not change

No certificate is reissued and no host is reconfigured as a result of it. Every
goatnet keeps the issuer it has today; this records which one that is and why.

## Trigger conditions for revisit

- Cloudflare stops being the DNS provider for a goatnet zone, or the DNS-01
  token model changes — the discriminator above is written in terms of it.
- A public CA offers a post-quantum leaf. None does today; the PQ path is hybrid
  key exchange (ADR 1079), not the certificate.
- A partitioned site needs its *public* hostnames to keep serving browser-trusted
  TLS through a partition longer than the renewal window.

---

*See `GLOSSARY.md` for terms and acronyms.*
