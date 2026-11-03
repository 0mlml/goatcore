# Node topologies & replication mechanisms — the five patterns, kept distinct

**Why this doc exists:** these five patterns look similar and have been repeatedly
conflated — especially *which replication mechanism belongs to which* (Spock vs PG
streaming vs VRRP). Getting it wrong sows architectural incorrectness. This is the
single canonical reference. **Read this before designing or deploying any second
node.** Mechanism claims are cited to their source doc.

## The five patterns at a glance

| # | Pattern | Location / network | Node state | **DB replication** | **Failover / activation** | **Cross-node data** |
|---|---|---|---|---|---|---|
| **1** | Intra-site active-passive | **same** site / LAN | B **stopped**, hot-standby | **PG streaming** (WAL ship, single-writer → standby) | **VRRP / floating VIP**, automatic | n/a (same LAN) |
| **2** | **Cross-site emergency STANDBY** | **different** site / network | **dormant**, promote-on-catastrophe | **Spock** multi-master (single-writer discipline) | **manual promotion** (relay-dispatch swap + writer flip + `pubsub-apply`), **not VRRP** | **passive async** (pubsub-state mirror + MinIO replication; Zenoh repopulates on promote) |
| **3** | Cross-site **ACTIVE** federated site (multi-site goatnet) | **different** site | **active**, serves its own peers | **Spock** (federation-plane state only) | n/a — both live | **Zenoh syncs**; **NO cross-site WG P2P** (site plane is local) — *data/app level not yet validated* |
| **4** | Moon pod | **dual-homed** (partner LAN + goatnet) | active edge bridge | **none** (not a goatnet) | n/a | goatnet + local Zenoh router; **release-bridge** conditions every crossing |
| **5** | Sovereign separate network | independent | independent | its **own** (or is another goatnet) | own | own |

## The anti-conflation rules (memorize these)

1. **Spock = cross-site only.** Multi-master logical replication over 50–200 ms WAN.
   Used by #2 and #3. *(ha-multi-site.md §2.1 table: "Spock multi-master inter-site".)*
2. **PG streaming = intra-site only.** WAL shipping, one primary → hot standby whose
   containers are **stopped until VIP failover**. Used by #1 only. *(ha-multi-site.md
   §2.1: "Postgres streaming replication intra-site (A primary → B hot standby)".)*
3. **VRRP / floating VIP = intra-site failover only.** The VIP is "the identity of the
   active router"; whichever node holds it is active. #1 only — **never** cross-site.
   *(ha-failover.md §VIP.)*
4. **Cross-site data = Zenoh, never cross-site WireGuard P2P.** The *site plane*
   (netbird P2P, local services) needs no cross-site coordination; only the small
   *federation plane* coordinates across sites. Peers do not P2P across sites; data
   crosses via Zenoh. #3 (and #2 replicates its fabric via Zenoh state-as-code, not
   P2P). *(ADR 0216 — federation plane vs site plane; boundary = coordination
   requirement, not location.)*
5. **Signal federation = cross-site signal matchmaking.** #2 (loose, standby) and #3
   (active). Distinct from intra-site, where a single signal + VIP failover suffices.
6. **Moon pod = minimal dual-homed bridge, not a full node.** One host, one bundle,
   two networks; release-bridge conditions data in/out. #4. *(goat-moon-pod.md.)*

## #2 vs #3 — same mechanisms, opposite activation

#2 and #3 **share** the cross-site toolkit (Spock + signal federation + Zenoh
cross-site data). The difference is **activation**:
- **#2 STANDBY** — the standby site is **dormant**: deployed, kept in parity by async passive
  reads, serving nothing, promoted only if the primary site suffers a catastrophic (network /
  site) failure the intra-site backup can't solve.
- **#3 ACTIVE site** — the remote site **serves its own peers** and provides
  cross-site functionality live; both sites are active.

Do not implement #2 as active-active (that re-creates the F-265 class of problem and
the double-side-effect hazards). Do not implement #3 as dormant.

## Replication mechanism per layer (for #2 and #3)

| Layer | Mechanism | Status |
|---|---|---|
| netbird mgmt + Ory (kratos/hydra) | **Spock** multi-master, single-writer discipline (ADR 1084) | **live/proven** |
| Signal (ephemeral) | **signal federation** (custom server); #2 = loose/standby, promote-on-failover | proven |
| Zenoh **structure** (slots/topics/schemas/ACL) | **pub/sub state-as-code** (Block 86): portal → per-site Forgejo `pubsub-state` git repo + MinIO snapshot; rebuild with `pubsub-apply` | built; use for standby rebuild |
| Zenoh **retained audit payloads** | **MinIO async replication** (ADR 0216: bulk audit bodies → per-site object store) | MinIO built-in |
| Zenoh **live fabric data** | **repopulates from peers on promotion** (Zenoh is mostly live pub/sub) | inherent |
| Apps (stateless) | deployed **dormant**; read replicated PG/Zenoh; started on promotion | standard |

**Explicitly NOT used for #2 Zenoh replication:** rsync of a live rocksdb store dir
(torn/inconsistent snapshots) and cross-site Zenoh **router** peering (ADR 1020 is
*Proposed / designed-not-built*, and a dormant standby doesn't need it).

## Split-brain (relevant to #1 and #2 promotion)

Two-primary divergence is a known failure mode with existing tooling:
`site-split-brain-detect.yml` + `site-split-brain-recovery.yml`. Any
promotion (#1 VIP or #2 cross-site) needs a guarantee the old primary is truly out
before the new one goes active — a **manual operator gate** today; fencing/quorum
only if ever automated.

Related: ha-failover.md (#1), ha-multi-site.md
+ [ADR 0216](../adr/0216-goat-net-federation-and-site-planes.md) (#2/#3),
[ADR 1084](../adr/1084-block-64r-mgmt-repset-and-two-node-federation.md) (Spock
write-routing), ADR 1107
(ZAGROS loose federation), [pubsub-state-as-code.md](pubsub-state-as-code.md) (Zenoh
structure), [goat-moon-pod.md](https://github.com/usareur-af/goatstuff/blob/main/docs/design/goat-moon-pod.md) (#4),
partner-standalone-goatnet.md (#5),
zagros-two-node-signal-bringup-and-netbird-perf-learnings.md
(the zagros-b #2 build).
