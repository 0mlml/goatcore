# Joining a goatnet from behind a corporate firewall

| | |
|---|---|
| Audience | A partner's network or security team writing egress rules; the sponsor or admin answering them |
| Reader-facing copy | The onboarding guide topic `?topic=firewall` on each goatnet's guide host, which prints that goatnet's own addresses |
| Source of the facts | The goatnet's `relay_addresses`, `stun_hosts` and management URL group_vars (the same values rendered into `management.json`) |

A device inside a corporate network or cloud VPC joins a goatnet only if its egress firewall allows **outbound** connections to the goatnet's management server and relays. An IP allowlist of those addresses is the right request. A client X.509 certificate does not substitute for it.

## What to ask the network team for

Outbound rules only, from the device's network to the goatnet's management server and relays. Nothing on a goatnet connects inward to a partner network, so no inbound rule is needed.

| Destination | Port | Protocol | Carries | Needed |
|---|---|---|---|---|
| Management server | 443 | TCP | Enrollment, then a long-lived gRPC (HTTP/2) stream; signaling rides the same connection | Required |
| Every relay | Relay port (33073 on current goatnets) | TCP | Relay traffic, WebSocket over TLS | Required |
| Every relay | Relay port | UDP | Relay traffic, QUIC; faster than the TCP relay | Strongly recommended |
| STUN hosts (the relays on current goatnets) | 3478 | UDP | Public-address discovery so devices can try a direct path | Recommended |
| Other participants' public addresses | Various | UDP | Direct WireGuard device-to-device | Optional; cannot be listed |
| `pkgs.netbird.io`, `netbird.io` | 443 | TCP | Client install only; CDN-served, allow by FQDN | Install time |

- **Working minimum:** TCP 443 and TCP on the relay port. Every connection then goes through a relay. It works, at lower throughput.
- **Allow every relay, not a subset.** A client keeps one home relay but opens a connection to another relay whenever the peer it wants is homed there (`shared/relay/client/manager.go` in the NetBird fork). A partial list breaks some peer pairs and not others.
- **Direct connections need open UDP.** They require outbound UDP to addresses that change, so most corporate policies will not allow them. Traffic falls back to relays without intervention.

## Where the addresses come from

Each goatnet's guide prints its own list at `https://guide.<goatnet base>/public/onboarding-guide?topic=firewall`:
- one row per hostname, with ports, taken from config;
- the public addresses each name resolves to, looked up when the page is requested and cached for 10 minutes;
- a de-duplicated `/32` list ready to paste into a rule.

The page drops private, CGNAT and loopback answers, and marks a name that did not resolve rather than guessing.

On current goatnets the management name resolves round-robin to the relays. The relays pass `:443` through to the management host (see relay-ingress-dispatch), so the address list is just the relay addresses.

From outside the goatnet, the same facts are:

```bash
dig +short netbird.<goatnet domain>
dig +short relay-<id>.<goatnet domain>     # one per relay in relay_addresses
```

Relay addresses are cloud static IPs that survive instance replacement but change if a relay is destroyed and rebuilt. Re-check before re-using an old firewall request.

## Firewall settings that decide whether it works

1. **Exempt the addresses from TLS inspection.** The management connection is a long-lived HTTP/2 stream, and the relay is WebSocket on a non-standard port. An inspecting proxy commonly breaks both. The client does not pin certificates: it trusts the system store (`client/grpc/dialer.go`, `shared/relay/client/dialer/ws/ws.go`). Inspection could still work with the corporate CA installed and HTTP/2 passed end-to-end, but that combination has not been tested. QUIC and STUN are UDP and cannot be inspected.
2. **No short idle timeout on TCP 443 to the management server.** The stream is held open and quiet by design; Traefik runs this entrypoint with read/write/idle timeouts of 0 for that reason. A firewall that reaps it causes repeated reconnects.
3. **Public DNS resolution** of the management and relay hostnames. The client dials by name.

## Why a client certificate does not replace the allowlist

Nothing at a goatnet's network edge checks a client certificate on the peer join path, so a certificate gives a firewall nothing to match on.
- **Joining:** a device joins with a one-time NetBird setup key and a WireGuard key pair.
- **Signing in:** the person signs in to Ory, which is reachable only inside the tunnel.
- **Server certificates:** the servers present publicly trusted Let's Encrypt certificates.
- **Other certificates:** the zenoh mTLS certificates travel inside WireGuard. The one client-certificate edge, the cross-environment `zenoh-door`, is server-to-server and not a partner path.

The allowlist only lets traffic out of the partner's network. Access on the goatnet is by identity (see mesh-acl), never by source address.

A partner that needs one fixed source address on both sides, rather than a device joining, wants router-to-router federation (see cross-sovereign-federation). That is a different integration.

## Verify before enrolling

From a host inside the restricted network:

```bash
dig +short netbird.<goatnet domain>          # expect public addresses
nc -zv <relay address> 443                   # each relay
nc -zv <relay address> 33073                 # each relay
# after enrolling
netbird status -d
```

| `netbird status -d` shows | Means |
|---|---|
| Management and Signal `Connected`, peers `P2P` or `Relayed` | Working. Both peer states are healthy |
| Every peer `Relayed` | UDP is blocked; works, slower |
| Management `Connected`, peers stuck `Connecting` | Relay port blocked or inspected |
| Management `Disconnected` | TCP 443 or DNS blocked, or TLS inspection on 443 |

The relay-health dashboard (`relay-health.<site>.<zone>`) shows whether the relays themselves are up.

## For sponsors and admins

The guide is mesh-only, so a person blocked by their firewall cannot open it. Send them the `?topic=firewall` page as a PDF, printed from the browser, before their setup key's 7-day clock starts. Most requests arrive as "can you give us an IP range, or will an X.509 cert do?"; the page answers both.

## Maintaining this

- The guide topic lives in `ops/goat-id-console/internal/welcome/guide_topics.go` (`"firewall"`). Its address table comes from `egress.go`, and the handler resolves names in `internal/handlers/guide_egress.go`.
- The console reads `NOTIFY_RELAY_ADDRESSES` and `NOTIFY_STUN_HOSTS`, which `roles/id-admin-console/templates/id-admin-console.env.j2` renders from `relay_addresses` and `stun_hosts`. Changing a goatnet's relays changes the page on the next console deploy; there is no second copy to update.
- Port or protocol changes to the relay (e.g. a relay on 443) must update this document and the topic's "Ports and protocols" section together.
