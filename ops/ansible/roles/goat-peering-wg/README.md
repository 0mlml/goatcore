# goat-peering-wg

One WireGuard tunnel between two edge hosts belonging to different sovereigns.
Neither host joins the other's mesh. The tunnel carries the far edge's fabric port
and the ports the deployment names, and the role's firewall drops the rest.

The driver runs this in two steps. `goat federate init` reads the interface's public
key with `wg show`, so the interface has to exist before our half is signed.

| Phase | Play | What exists after it |
|---|---|---|
| 2 | `peering-prepare.yml` (`tasks/prepare.yml`) | The interface and a private key that stays on this host. No address and no peer. |
| 11 | `peering-wg.yml` (`tasks/main.yml`) | The agreed address, the one peer, the firewall. Read back from the interface. |

Phase 11 runs only when the agreement's `transport.kind` is `wireguard`, and the role
refuses under any other kind. `goat-peering-edge` is the same step for
`ipsec-fips`.

## Where every value comes from

| Value | Source |
|---|---|
| Link addresses, MTU, who listens | the agreement, rendered by `goat federate apply` into `federation-<them>-transport.yml`. `parties[0]` holds the first address of the /31 or /30. |
| Far public key and endpoint | the counterparty package's `transport_offer`, the signed file phase 6 composed from |
| Interface name | `peering_wg_interface`, the same string as `[transport] interface` in `federation.toml` |
| Listen port, extra ports, FIPS waiver | this deployment's `group_vars/<group>/deployment.yml` |

The listening side configures no endpoint and waits. The other side sets the endpoint
and a keepalive.

## The FIPS posture

WireGuard's primitives are not FIPS-approved, so this role never makes the claim. The
claim sits in the Zenoh TLS session inside the tunnel
(`cross-sovereign-federation.md` §5.1).
Waiver: DBB-WVR-001.

| `peering_wg_fips_required` | `peering_wg_fips_waived_reason` | Outcome |
|---|---|---|
| true (default) | set | `proceed-waived`: configures, prints a deviation banner, records the reason |
| true | empty | `refuse`, having written the state file first |
| false | any | `proceed-no-claim`: configures and records that no claim is made |

## Firewall

`peering-wg.nft.j2` holds one table per interface, and it filters at prerouting because
the edge router's published port is a DNAT that never reaches the input chain.

- **From the tunnel:** established traffic, ICMP, and the listed TCP ports from the far
  link address to ours. Everything else is counted and dropped.
- **Onto the tunnel:** the same ports to the far link address. Everything else is
  counted and dropped.
- **The tunnel port:** answers only the address in the counterparty's offer, when that
  offer is a literal address.
- **MSS:** clamped to the route's MTU on forwarded traffic, because the edge's session
  is forwarded from a compose network.

## Cutting it

`systemctl stop goat-peering-wg@<interface>` removes the interface and the firewall
table. It is local, immediate, and needs nothing from the other side.

## State file

`/var/log/goat-peering-wg-<interface>-state.json` records the decision, both sovereign
ids, the far key, the link and what the kernel reports for FIPS. It is written before
the FIPS gate, so a refusal leaves its evidence on the host.

## Evidence

`ops/federation-e2e/run.sh` runs this role through the real driver between two
hermetic sovereigns in CI, and the proof is read back from the receiving side.
