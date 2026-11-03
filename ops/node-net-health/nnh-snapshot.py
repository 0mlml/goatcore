#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# nnh-snapshot — the node-net-health collector (docs/design/node-net-health.md).
#
# Run by node-net-health-snapshot.timer (hourly + on-demand). Snapshots the
# node's two connection planes + classifies EVERY socket, then writes ONE
# JSON snapshot atomically for the goat-node-net-health publisher to grade
# and put on ops/net/health/<node>:
#
#   - mesh plane:  `netbird status --json` (passive; F-098 socket guard) —
#     per-peer handshake age, conn type, rx/tx byte deltas vs the previous
#     snapshot (the real gap is carried as snapshot_gap_secs).
#   - classifier:  `ss -tunapH` inventory (listening + established, TCP/UDP;
#     loopback-only excluded) into the four buckets of the closed graph:
#     mesh | fabric | baseline | undeclared. Baseline = the node-class egress
#     contract rendered by the role at /etc/node-net-health/baseline.json —
#     fail-closed: no matching rule -> undeclared.
#   - undeclared:  first/last-seen persisted across snapshots in the state
#     dir; each NEWLY seen observation appends one NDJSON line to
#     undeclared.ndjson for goat-logship (OCSF Network Activity 4001).
#
# Observation, not enforcement (Block 17 egress-proxy owns enforcement).
# Failure posture is honest-by-absence: on error the previous snapshot ages
# past the publisher's staleness window and the reading degrades to
# `unmetered` — never a false green.
#
# BANDWIDTH ATTRIBUTION (ADR 1128, --fast mode). The same two taps that answer
# "what is this node connected to" also answer "how much of the pipe is each
# kind of work using", so this collector carries both rather than growing a
# second daemon beside it. `--fast` runs on its own short timer and does the
# byte accounting only; the hourly full run is unchanged in shape and cadence,
# so the ADR 1036 `net` envelope and its grading are untouched.
#
#   TWO PLANES, NEVER SUMMED. A Zenoh message crossing the mesh is counted
#   twice on this host at this instant: once as WireGuard UDP on the physical
#   NIC, once as TCP :7447 inside the tunnel. Adding them inflates exactly the
#   number an operator uses to judge headroom. So: the TRANSPORT plane
#   (per-peer WireGuard, p2p vs relayed) carries the capacity question, the
#   WORKLOAD plane (in-tunnel sockets, by category) explains it, and they are
#   emitted as separate metric families that the dashboard never adds.
#
#   Byte counters are cumulative-per-socket, so rates come from deltas against
#   the previous sample, floored at 0 (a socket closing and its 4-tuple being
#   reused looks like a counter reset — same trick the mesh plane already uses
#   for peer counters). Deltas accumulate into a persisted running total which
#   is what gets EXPOSED, because a textfile exporting a delta would break
#   rate().
#
#   Coverage is honest, not total: kernel socket byte counters are a TCP
#   facility. Non-WireGuard UDP (chiefly DNS and NTP) is reported under the
#   `not-measured` category rather than as zero, and short-lived HTTP that opens
#   and closes between samples is invisible here BY DESIGN — that traffic is
#   attributed from the Traefik access log instead (the two taps are
#   complementary, see docs/design/mesh-bandwidth-attribution.md §4).

import argparse
import ipaddress
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time

STATE_DIR = os.environ.get("NNH_STATE_DIR", "/var/lib/node-net-health")
BASELINE = os.environ.get("NNH_BASELINE", "/etc/node-net-health/baseline.json")
SNAPSHOT = os.path.join(STATE_DIR, "snapshot.json")
PREV_MESH = os.path.join(STATE_DIR, "mesh-prev.json")
UNDECLARED_STATE = os.path.join(STATE_DIR, "undeclared-state.json")
UNDECLARED_TAIL = os.path.join(STATE_DIR, "undeclared.ndjson")

# --- bandwidth attribution (ADR 1128) ---------------------------------------
# Separate state files from the inventory ones: --fast runs every minute and
# the hourly full run must not race it for the same file.
PREV_SOCK = os.path.join(STATE_DIR, "sock-bytes-prev.json")
PREV_MESH_FAST = os.path.join(STATE_DIR, "mesh-prev-fast.json")
COUNTERS = os.path.join(STATE_DIR, "attribution-counters.json")
# The node-exporter role provisions this directory (roles/node-exporter
# defaults: node_exporter_textfile_host_dir). Writing here rather than opening
# a port keeps this off the "declared target with nothing behind it" path that
# F-289 was.
TEXTFILE = os.environ.get(
    "NNH_TEXTFILE", "/var/lib/node_exporter/textfile/goat_net_attribution.prom")
# Per-peer workload attribution: peers x categories x directions. Fine at
# goatnet scale (~90 peers -> ~3.4k series/host), NOT fine at the ADR 1122
# 10k-peer envelope. Toggle, with the budget stated rather than discovered.
PEER_ATTRIBUTION = os.environ.get("NNH_PEER_ATTRIBUTION", "1") == "1"
# LOCAL ports whose bytes the Traefik access log already accounts for at finer
# resolution. Sockets accepted here are categorised `http-delegated` and the
# dashboard excludes that row from the workload stack, so the socket plane and
# the HTTP plane partition instead of overlapping. See categorize().
HTTP_DELEGATED_PORTS = {int(p) for p in
                        os.environ.get("NNH_HTTP_DELEGATED_PORTS", "80,443").split(",")}
# Sockets that ARE the mesh transport rather than work riding on it. Their
# bytes are the transport plane's, seen a layer down; counting them in the
# workload plane double-counts the whole mesh. Named by baseline-rule and by
# comm so a goatnet can extend either without a code change.
TRANSPORT_UNDERLAY_RULES = set(
    filter(None, os.environ.get("NNH_UNDERLAY_RULES", "rathole-relays").split(",")))
TRANSPORT_UNDERLAY_COMMS = set(
    filter(None, os.environ.get("NNH_UNDERLAY_COMMS", "rathole,netbird,wireguard").split(",")))
MESH_CIDRS = [ipaddress.ip_network(c) for c in
              os.environ.get("NNH_MESH_CIDRS", "100.64.0.0/10").split(",")]
# Docker bridge space: traffic with BOTH ends here (or a listener bound here)
# is host-internal plumbing, not part of the node's connection graph.
INTERNAL_CIDRS = [ipaddress.ip_network(c) for c in
                  os.environ.get("NNH_INTERNAL_CIDRS", "172.16.0.0/12").split(",")]
# Live-graph states only — time-wait/fin-wait/syn-* are dead or in-flight
# teardown, not connections the node holds.
LIVE_STATES = {"listen", "estab"}
# Known non-live labels printed by ss; an unknown state is missing evidence.
# https://github.com/iproute2/iproute2/blob/v6.1.0/misc/ss.c (sock_state_print)
IGNORED_SOCKET_STATES = {"syn-sent", "syn-recv", "fin-wait-1", "fin-wait-2",
                         "time-wait", "close-wait", "last-ack", "closing"}
FABRIC_PORTS = {int(p) for p in os.environ.get("NNH_FABRIC_PORTS", "7447").split(",")}
FABRIC_COMMS = set(os.environ.get("NNH_FABRIC_COMMS", "zenohd").split(","))
# WireGuard rekey window x3 (docs/design/node-net-health.md staleness rule).
STALE_HANDSHAKE_S = int(os.environ.get("NNH_STALE_HANDSHAKE_S", "360"))


def now_ms():
    return int(time.time() * 1000)


def load_json(path, default):
    try:
        with open(path) as f:
            return json.load(f)
    except Exception:
        return default


def atomic_write(path, obj):
    d = os.path.dirname(path)
    fd, tmp = tempfile.mkstemp(dir=d, prefix=".nnh.")
    try:
        with os.fdopen(fd, "w") as f:
            json.dump(obj, f, separators=(",", ":"))
        os.chmod(tmp, 0o644)
        os.replace(tmp, path)
    except Exception:
        try:
            os.unlink(tmp)
        except OSError:
            pass  # best-effort temp cleanup
        raise


# --------------------------- mesh plane -------------------------------------

def handshake_age_ms(stamp, now_s):
    if not stamp:
        return -1
    try:
        # RFC3339, e.g. 2026-07-14T09:00:00Z or with fractional seconds/offset.
        import datetime
        t = datetime.datetime.fromisoformat(stamp.replace("Z", "+00:00"))
        return max(0, int((now_s - t.timestamp()) * 1000))
    except Exception:
        return -1


def mesh_plane(prev_path=PREV_MESH):
    """netbird status --json -> per-peer rows + rx/tx deltas vs prev snapshot.

    `prev_path` is a parameter because --fast and the hourly full run keep
    SEPARATE baselines: they run on different timers, and sharing one file
    would make each run's delta depend on whichever ran last.
    """
    if not os.path.exists("/var/run/netbird.sock"):     # F-098 guard
        return None, 0
    try:
        out = subprocess.run(["netbird", "status", "--json"], capture_output=True,
                             timeout=15, check=True).stdout
        st = json.loads(out)
    except Exception:
        return None, 0
    prev = load_json(prev_path, {})
    now_s = time.time()
    gap_s = int(now_s - prev.get("ts", now_s)) if prev.get("ts") else 0
    prev_peers = prev.get("peers", {})
    peers, new_prev = [], {}
    for p in st.get("peers", {}).get("details", []):
        fqdn = p.get("fqdn", "")
        rx, tx = int(p.get("transferReceived", 0)), int(p.get("transferSent", 0))
        pv = prev_peers.get(fqdn, {})
        # counters reset on peer/daemon restart -> delta floors at 0, honest
        rx_d = max(0, rx - pv.get("rx", rx))
        tx_d = max(0, tx - pv.get("tx", tx))
        connected = str(p.get("status", "")).lower() == "connected"
        age = handshake_age_ms(p.get("lastWireguardHandshake", ""), now_s)
        peers.append({
            "peer_fqdn": fqdn,
            "peer_ip": str(p.get("netbirdIp", "")).split("/")[0],
            "connected": connected,
            "conn_type": p.get("connectionType", "") if connected else "",
            "handshake_age_ms": age,
            "active": bool(connected and 0 <= age <= STALE_HANDSHAKE_S * 1000),
            "rx_bytes_delta": rx_d if gap_s else 0,
            "tx_bytes_delta": tx_d if gap_s else 0,
            "latency_ms": int(p.get("latency", 0)) / 1.0e6,   # ns -> ms; 0 = unmeasured
        })
        new_prev[fqdn] = {"rx": rx, "tx": tx}
    atomic_write(prev_path, {"ts": now_s, "peers": new_prev})
    return {"fqdn": st.get("fqdn", ""), "peers": peers}, gap_s


# --------------------------- socket inventory -------------------------------

SS_LINE = re.compile(r"^(?P<proto>tcp|udp)\s+(?P<state>\S+)\s+\S+\s+\S+\s+"
                     r"(?P<local>\S+)\s+(?P<remote>\S+)\s*(?P<proc>.*)$")
PID_RE = re.compile(r'\(\("(?P<comm>[^"]+)",pid=(?P<pid>\d+)')


def split_addr(a):
    """'1.2.3.4:80' / '[::1]:80' / '*:22' -> (ip, port)."""
    host, _, port = a.rpartition(":")
    host = host.strip("[]")
    try:
        return host, int(port)
    except ValueError:
        return host, 0


def ip_of(host):
    try:
        ip = ipaddress.ip_address(host)
    except ValueError:
        return None
    # ss prints v4-mapped IPv6 ("::ffff:172.17.0.1") for dual-stack sockets —
    # unmap so the CIDR classification sees the real v4 address.
    return getattr(ip, "ipv4_mapped", None) or ip


def is_loopback(host):
    ip = ip_of(host.split("%")[0])
    return ip is not None and ip.is_loopback


def in_net(host, nets):
    ip = ip_of(host.split("%")[0])
    return ip is not None and any(ip in n for n in nets if ip.version == n.version)


def in_mesh(host):
    return in_net(host, MESH_CIDRS)


def no_remote(host, port):
    """A listener's 'remote': '*', '0.0.0.0:0', '[::]:0'."""
    return port == 0 and (host in ("*", "") or (ip_of(host.split("%")[0]) or ipaddress.ip_address("1.1.1.1")).is_unspecified)


def container_map():
    """pid -> container name via docker ps + /proc/<pid>/cgroup (best-effort)."""
    ids = {}
    docker = shutil.which("docker")
    if docker:
        try:
            out = subprocess.run([docker, "ps", "--format", "{{.ID}} {{.Names}}"],
                                 capture_output=True, timeout=10, check=True).stdout
            for line in out.decode().splitlines():
                cid, _, name = line.partition(" ")
                if cid:
                    ids[cid] = name.strip()
        except Exception:
            pass  # best-effort container parse
    return ids


def container_of(pid, ids, cache):
    if pid in cache:
        return cache[pid]
    name = ""
    try:
        with open(f"/proc/{pid}/cgroup") as f:
            cg = f.read()
        m = re.search(r"docker[/-]([0-9a-f]{12,64})", cg)
        if m:
            full = m.group(1)
            for cid, cname in ids.items():
                if full.startswith(cid):
                    name = cname
                    break
            name = name or full[:12]
    except Exception:
        pass  # best-effort name resolution
    cache[pid] = name
    return name


def host_netns():
    """The inode of this process's network namespace, or "" if unreadable."""
    try:
        return os.readlink("/proc/self/ns/net")
    except OSError:
        return ""


class InventoryError(RuntimeError):
    """Required socket inventory could not be observed completely."""


def netns_targets(required=False):
    """[(container_name, pid)] — one entry per DISTINCT container netns.

    F-331: `ss` reports the namespace it runs in, so a collector that only ever
    runs on the host sees none of the sockets belonging to containers with
    their own netns. Coverage then stays low with BOTH catch-all buckets flat
    zero — the bytes never arrive to be categorised at all, which is why adding
    category rules to the egress contract cannot move the number.

    Deduped by namespace inode, and containers sharing the HOST namespace are
    excluded: their sockets are already in the host scan, and enumerating them
    again would double every byte they carry. That is the failure mode to fear
    here — under-reporting is visible as low coverage, over-reporting is
    invisible and inflates exactly the number used to judge headroom.
    """
    docker = shutil.which("docker")
    if not docker:
        return []
    if not required and not shutil.which("nsenter"):
        return []
    try:
        out = subprocess.run(
            [docker, "ps", "--format", "{{.ID}}"],
            capture_output=True, timeout=10, check=True).stdout.decode()
        ids = [i for i in out.split() if i]
        if not ids:
            return []
        if not shutil.which("nsenter"):
            raise InventoryError("container namespace discovery failed: nsenter unavailable")
        out = subprocess.run(
            [docker, "inspect", "--format", "{{.Name}} {{.State.Pid}}", *ids],
            capture_output=True, timeout=15, check=True).stdout.decode()
    except (OSError, subprocess.SubprocessError, UnicodeError, InventoryError) as e:
        if required:
            raise InventoryError(f"container namespace discovery failed: {e}") from e
        return []

    host_ns, seen, targets = host_netns(), set(), []
    if required and not host_ns:
        raise InventoryError("host namespace identity unreadable during container discovery")
    if required and len(out.splitlines()) != len(ids):
        raise InventoryError("container namespace inspection returned incomplete results")
    for line in out.splitlines():
        name, _, pid_s = line.strip().rpartition(" ")
        name = name.strip().lstrip("/")
        try:
            pid = int(pid_s)
        except ValueError:
            if required:
                raise InventoryError("container namespace inspection returned an invalid PID") from None
            continue
        if pid <= 0:                       # created but not running
            continue
        try:
            ns = os.readlink(f"/proc/{pid}/ns/net")
        except OSError as e:
            if required:
                raise InventoryError(f"container namespace identity unreadable for PID {pid}: {e}") from e
            continue
        if not ns or ns == host_ns or ns in seen:
            continue                       # host-networked, or a shared netns
        seen.add(ns)
        targets.append((name, pid))
    return targets


def run_ss(args, pid=None, timeout=15, *, required=False):
    """Run ss; required inventory failures raise, optional counters stay best effort.

    One parser, one command shape: entering the namespace and re-running the
    same `ss` keeps a single reading of what a socket is, rather than adding a
    second /proc/net/tcp parser that could disagree with the first.
    """
    cmd = ["ss", *args] if pid is None else ["nsenter", "-t", str(pid), "-n", "ss", *args]
    try:
        return subprocess.run(cmd, capture_output=True, timeout=timeout,
                              check=True).stdout.decode(errors="replace")
    except (OSError, subprocess.SubprocessError) as e:
        if required:
            scope = "host" if pid is None else f"container PID {pid}"
            raise InventoryError(f"socket scan failed for {scope}: {e}") from e
        return ""


# Set by sockets() so the textfile can report enumeration honestly rather than
# letting an unreadable namespace look like an idle one.
NETNS_STATS = {"targets": 0, "read": 0}


def parse_ss_rows(out, ids, cache, netns, netns_container="", *, required=False):
    """Parse `ss -tunapH` output into rows, tagged with the netns they came from.

    `netns` is "host" or a container name. It is carried on every row because
    the byte-counter join keys on (local, remote) and container address space
    is NOT unique — two containers on the default bridge can both hold
    172.17.0.2:443, and merging their counters on that key would silently add
    one container's traffic to another's.
    """
    rows = []
    in_container = netns != "host"
    for line in out.splitlines():
        if not line.strip():
            continue
        m = SS_LINE.match(line.strip())
        if not m:
            if required:
                raise InventoryError(f"unrecognized socket inventory row in {netns}")
            continue
        state = m.group("state").lower().replace("unconn", "listen")
        if state not in LIVE_STATES:
            if required and state not in IGNORED_SOCKET_STATES:
                raise InventoryError(f"unrecognized socket state in {netns}")
            continue                       # dead / teardown states aren't the graph
        lhost, lport = split_addr(m.group("local"))
        rhost, rport = split_addr(m.group("remote"))
        listener = state == "listen" or no_remote(rhost, rport)
        # Intra-node plumbing, not the node's connection graph:
        #   - loopback-bound listeners + loopback<->loopback flows
        if is_loopback(lhost) and (listener or is_loopback(rhost)):
            continue
        #   - docker-bridge-internal traffic (both ends on the bridge)
        if in_net(lhost, INTERNAL_CIDRS) and in_net(rhost, INTERNAL_CIDRS):
            continue
        #   - a bridge-bound LISTENER is plumbing when seen from the host,
        #     where that address belongs to the bridge. Seen from INSIDE the
        #     container it is the container's own address, and the listener is
        #     the actual service — :443 on traefik, :7447 on the zenoh router.
        #     Applying the host rule there would drop exactly the sockets this
        #     enumeration exists to find.
        if not in_container and listener and in_net(lhost, INTERNAL_CIDRS):
            continue
        pm = PID_RE.search(m.group("proc") or "")
        pid = int(pm.group("pid")) if pm else 0
        comm = pm.group("comm") if pm else ""
        # Inside a netns, pids are the container's own and /proc/<pid>/cgroup
        # resolution is unnecessary — we know which container we entered.
        container = netns_container if in_container else (
            container_of(pid, ids, cache) if pid else "")
        rows.append({
            "proto": m.group("proto"),
            "state": state,
            "local": (lhost, lport),
            "remote": (rhost, rport),
            "pid": pid,
            "comm": comm,
            "container": container,
            "netns": netns,
        })
    return rows


def sockets():
    """Every socket on this node — host namespace AND each container's own.

    Before F-331 this ran `ss` once, on the host, and therefore could not see
    any socket belonging to a container with its own network namespace. The
    categoriser and the egress-contract rules are unchanged; they were never
    the problem. The bytes simply never arrived.
    """
    ids, cache = container_map(), {}
    rows = parse_ss_rows(run_ss(["-tunapH"], required=True), ids, cache, "host", required=True)

    targets = netns_targets(required=True)
    NETNS_STATS["targets"], NETNS_STATS["read"] = len(targets), 0
    for name, pid in targets:
        out = run_ss(["-tunapH"], pid=pid, required=True)
        NETNS_STATS["read"] += 1
        rows.extend(parse_ss_rows(out, ids, cache, name, netns_container=name, required=True))
    return rows


# --------------------------- classification ---------------------------------

def rule_matches(rule, s):
    """Every field the rule specifies must match (AND); see node-net-health.md."""
    if rule.get("proto") and rule["proto"] != s["proto"]:
        return False
    if rule.get("remote_ports") and s["remote"][1] not in rule["remote_ports"]:
        return False
    if rule.get("local_ports") and s["local"][1] not in rule["local_ports"]:
        return False
    if rule.get("remote_cidrs"):
        ip = ip_of(s["remote"][0])
        if ip is None:
            return False
        nets = [ipaddress.ip_network(c) for c in rule["remote_cidrs"]]
        if not any(ip in n for n in nets if ip.version == n.version):
            return False
    if rule.get("comm"):
        if not any(s["comm"].startswith(c.rstrip("*")) if c.endswith("*") else s["comm"] == c
                   for c in rule["comm"]):
            return False
    if rule.get("container"):
        if not any(s["container"].startswith(c) for c in rule["container"]):
            return False
    return True


def classify(rows, rules):
    """mesh -> fabric -> baseline -> undeclared (first match wins)."""
    out = []
    for s in rows:
        bucket, rule_name = "undeclared", ""
        if in_mesh(s["local"][0]) or in_mesh(s["remote"][0]) or s["comm"] == "netbird":
            bucket = "mesh"
        elif (s["local"][1] in FABRIC_PORTS or s["remote"][1] in FABRIC_PORTS
              or s["comm"] in FABRIC_COMMS):
            bucket = "fabric"
        else:
            for r in rules:
                if rule_matches(r, s):
                    bucket, rule_name = "baseline", r.get("name", "?")
                    break
        out.append({
            "bucket": bucket,
            "proto": s["proto"],
            "state": s["state"],
            "local_addr": f'{s["local"][0]}:{s["local"][1]}',
            "remote_addr": "" if s["remote"][0] in ("*", "") else f'{s["remote"][0]}:{s["remote"][1]}',
            "pid": s["pid"],
            "comm": s["comm"],
            "container": s["container"],
            # Carried so the byte-counter join can key on it. Dropping it here
            # would make every container socket look like a host socket to the
            # join, which is where the counters would cross-attribute.
            "netns": s.get("netns", "host"),
            "baseline_rule": rule_name,
            "first_seen_unix_ms": 0,
            "last_seen_unix_ms": 0,
        })
    return out


def track_undeclared(conns):
    """Persist first/last-seen per undeclared key; NDJSON-append NEW sightings."""
    state = load_json(UNDECLARED_STATE, {})
    now = now_ms()
    fresh_lines = []
    for c in conns:
        if c["bucket"] != "undeclared":
            continue
        key = f'{c["remote_addr"] or c["local_addr"]}|{c["comm"]}|{c["container"]}'
        ent = state.get(key)
        if ent is None:
            ent = {"first_seen_unix_ms": now}
            fresh_lines.append({
                "ts_unix_ms": now,
                "event": "net_undeclared_conn",
                "node": os.uname().nodename,
                **{k: c[k] for k in ("proto", "state", "local_addr", "remote_addr",
                                     "pid", "comm", "container")},
            })
        ent["last_seen_unix_ms"] = now
        state[key] = ent
        c["first_seen_unix_ms"] = ent["first_seen_unix_ms"]
        c["last_seen_unix_ms"] = now
    if fresh_lines:
        with open(UNDECLARED_TAIL, "ab+") as f:
            # A failed write may leave a partial final record. Preserve those
            # bytes, but start retries on a new line so they remain parseable.
            f.seek(0, os.SEEK_END)
            if f.tell():
                f.seek(-1, os.SEEK_END)
                if f.read(1) != b"\n":
                    print("nnh-snapshot: separating incomplete undeclared event tail", file=sys.stderr)
                    f.write(b"\n")
            for line in fresh_lines:
                f.write((json.dumps(line, separators=(",", ":")) + "\n").encode("utf-8"))
        # Owner only: the tail names undeclared remote peers. Its reader
        # (goat-logship's Vector) runs as root, so nothing else needs to read it.
        os.chmod(UNDECLARED_TAIL, 0o600)
    # Only suppress repeats after their required output has closed successfully.
    # A checkpoint failure can cause duplicates on retry; an event must not vanish.
    atomic_write(UNDECLARED_STATE, state)
    return sum(1 for c in conns if c["bucket"] == "undeclared")


# --------------------- bandwidth attribution (ADR 1128) ----------------------
#
# Everything below is the --fast path. It shares the socket inventory and the
# mesh plane with the hourly run and adds three things: TCP byte counters, a
# WORKLOAD category orthogonal to the security bucket, and a Prometheus
# textfile.

# `ss -i` hangs its per-socket info on a CONTINUATION line beginning with
# whitespace, so the parse is stateful: a socket line establishes the key, the
# following indented line carries its counters.
#
# NOT SS_LINE. That regex expects a leading Netid column, which ss prints only
# when MORE THAN ONE family is requested (`-tu`). `ss -t` alone starts at
# State, so reusing SS_LINE here would silently match nothing.
SS_I_LINE = re.compile(r"^(?P<state>\S+)\s+\d+\s+\d+\s+(?P<local>\S+)\s+(?P<remote>\S+)")
# Separate searches, not one combined pattern: iproute2 emits sent before
# received today, and a pattern that encodes that ordering breaks silently if
# it ever changes.
SS_SENT = re.compile(r"\bbytes_sent:(\d+)\b")
SS_RECV = re.compile(r"\bbytes_received:(\d+)\b")


def tcp_byte_counters(raw=None, netns="host"):
    """`ss -tinpH` -> {(netns,local,remote): {sent, recv}} cumulative-since-open.

    TCP only — `-i` carries no counters for UDP, which is why non-WireGuard UDP
    is reported as `not-measured` rather than as zero. `raw` is a seam for
    tests; production passes nothing and the command is run.

    KEYED BY NAMESPACE. Container address space is not unique: two
    containers on the default bridge can both hold 172.17.0.2:443, so a
    (local, remote) key merged across namespaces would add one container's
    bytes to another's. The namespace is part of the socket's identity here.
    """
    if raw is None:
        raw = run_ss(["-tinpH"], timeout=20)
        if not raw:
            return {}
    counters, key = {}, None
    for line in raw.splitlines():
        if not line.strip():
            continue
        if line[0].isspace():                       # continuation: the counters
            if key is None:
                continue
            sent, recv = SS_SENT.search(line), SS_RECV.search(line)
            # A socket that has moved nothing in one direction omits that field
            # entirely rather than printing 0; absent means 0 bytes so far.
            if sent or recv:
                counters[key] = {"sent": int(sent.group(1)) if sent else 0,
                                 "recv": int(recv.group(1)) if recv else 0}
            continue
        m = SS_I_LINE.match(line)                   # a new socket: set the key
        key = None
        if not m or m.group("state").lower() != "estab":
            continue                                # listeners move no bytes
        lhost, lport = split_addr(m.group("local"))
        rhost, rport = split_addr(m.group("remote"))
        key = (netns, f"{lhost}:{lport}", f"{rhost}:{rport}")
    return counters


def tcp_byte_counters_all():
    """Byte counters from the host namespace and every container namespace."""
    counters = tcp_byte_counters()
    for name, pid in netns_targets():
        raw = run_ss(["-tinpH"], pid=pid, timeout=20)
        if raw:
            counters.update(tcp_byte_counters(raw=raw, netns=name))
    return counters


def categorize(conn, rules):
    """Workload category for one socket — ADR 1128 grammar.

    Precedence mirrors classify(): the STRUCTURAL categories are decided in
    code because they are properties of the substrate, and everything else
    comes from the baseline registry's optional `category:` field so the map
    stays in group_vars beside the egress contract it annotates.
    """
    # THE SECOND DOUBLE-COUNT, one layer below the two-plane one. Traefik's
    # browser-facing sockets are visible to `ss` AND every byte on them is
    # already in the access log, where the HTTP plane breaks it out by surface
    # category. Counting both would inflate the workload plane by the whole of
    # web traffic.
    #
    # So they get their own category and the dashboard's workload stack is
    # `category != http-delegated` PLUS the HTTP plane — a clean partition
    # rather than an overlap. The row is still emitted, on purpose: comparing
    # it against the HTTP plane's total is a free cross-check that the two taps
    # agree.
    #
    # Keyed on LOCAL port, which is what distinguishes an ACCEPTED connection
    # on :443 from an outbound image pull TO a remote :443 (that one has an
    # ephemeral local port and stays image-pull).
    if conn["proto"] == "tcp" and split_addr(conn["local_addr"])[1] in HTTP_DELEGATED_PORTS:
        return "http-delegated"

    # THE THIRD DOUBLE-COUNT. Sockets that ARE the mesh transport — the
    # rathole relay tunnels, the netbird control connection — carry the very
    # bytes the TRANSPORT plane already counts as mesh-relayed/mesh-p2p, one
    # layer down. They can be the largest "workload" figure on a host, and
    # every byte of it is the tunnel carrying the mesh, not work riding on it.
    #
    # They get their own category and the dashboard excludes it from the
    # workload stack, exactly as with http-delegated: partition, never overlap.
    # Emitted rather than dropped so the overlap stays measurable.
    if conn["baseline_rule"] in TRANSPORT_UNDERLAY_RULES or conn["comm"] in TRANSPORT_UNDERLAY_COMMS:
        return "transport-underlay"

    # Fabric is decided on the PORT/COMM, not on the security bucket. classify()
    # deliberately tests mesh BEFORE fabric — correct for a risk inventory,
    # where "is this on the mesh" is the first question — but it means a zenoh
    # session to a mesh peer lands in bucket `mesh` and would never reach a
    # `bucket == "fabric"` test here, and would report zero fabric bytes.
    if (conn["proto"] == "tcp"
            and (split_addr(conn["local_addr"])[1] in FABRIC_PORTS
                 or split_addr(conn["remote_addr"] or "*:0")[1] in FABRIC_PORTS
                 or conn["comm"] in FABRIC_COMMS)):
        return "fabric"
    if conn["bucket"] == "fabric":
        return "fabric"
    if conn["bucket"] == "undeclared":
        return "undeclared"
    if conn["bucket"] == "baseline":
        for r in rules:
            if r.get("name") == conn["baseline_rule"]:
                # A declared rule with no category is a map that has fallen
                # behind its contract — visible, not silently bucketed.
                return r.get("category") or "other-declared"
        return "other-declared"
    # bucket == mesh: in-tunnel traffic. The port/comm rules in the registry
    # still apply (a Postgres session to a peer is replication, not "mesh"),
    # so consult them before falling back.
    for r in rules:
        if r.get("category") and rule_matches(r, {
                "proto": conn["proto"],
                "local": split_addr(conn["local_addr"]),
                "remote": split_addr(conn["remote_addr"] or "*:0"),
                "comm": conn["comm"],
                "container": conn["container"]}):
            return r["category"]
    return "other-declared"


def bump(counters, family, labels, delta):
    """Accumulate a delta into the persisted running total.

    What gets EXPOSED is the running total, not the delta: a textfile that
    published a per-interval delta would make rate() nonsense. Losing the state
    file resets a counter to 0, which Prometheus already handles as a counter
    reset.
    """
    if delta <= 0:
        return
    key = family + "|" + "|".join(labels)
    counters[key] = counters.get(key, 0) + int(delta)


def attribution_pass(rules):
    """One --fast sample: both planes, deltas folded into the running totals."""
    # A required scan must succeed before mesh_plane advances its checkpoint;
    # otherwise a failed sample consumes transport deltas that never get saved.
    conns = classify(sockets(), rules)
    counters = load_json(COUNTERS, {})
    gauges = []   # (name, labels, value) — point-in-time, not accumulated

    # -- transport plane: per-peer WireGuard, p2p vs relayed -----------------
    mesh, gap_s = mesh_plane(PREV_MESH_FAST)
    peers_seen = 0
    if mesh:
        for p in mesh["peers"]:
            if not p["connected"]:
                continue
            peers_seen += 1
            # conn_type is netbird's own word ("P2P"/"Relayed"); map it onto the
            # grammar rather than leaking the vendor spelling into metrics.
            cls = "mesh-relayed" if p["conn_type"].lower().startswith("relay") \
                else "mesh-p2p"
            bump(counters, "transport", [cls, "rx"], p["rx_bytes_delta"])
            bump(counters, "transport", [cls, "tx"], p["tx_bytes_delta"])
            if PEER_ATTRIBUTION and p["peer_fqdn"]:
                bump(counters, "peer", [p["peer_fqdn"], cls, "rx"], p["rx_bytes_delta"])
                bump(counters, "peer", [p["peer_fqdn"], cls, "tx"], p["tx_bytes_delta"])
            # Per-peer RTT, split by path class. This is the p2p-vs-relayed
            # PERFORMANCE signal, and netbird already measures it for its own
            # connection management — the fast pass was reading the field and
            # discarding it.
            #
            # A gauge, not a counter, so it does not go through bump().
            # `measured` is emitted ALONGSIDE because netbird reports 0 for
            # peers it has not timed (often most of them), and a 0 here
            # would render as an instantaneous link. Absence of a measurement
            # is not a measurement of zero.
            if p["peer_fqdn"]:
                gauges.append(("goat_net_peer_latency_ms",
                               {"peer": p["peer_fqdn"], "class": cls},
                               round(p["latency_ms"], 3)))
                gauges.append(("goat_net_peer_latency_measured",
                               {"peer": p["peer_fqdn"], "class": cls},
                               1 if p["latency_ms"] > 0 else 0))

    # -- workload plane: in-tunnel sockets, by category ----------------------
    live = tcp_byte_counters_all()
    prev = load_json(PREV_SOCK, {})
    now_sock = {}
    for c in conns:
        if c["proto"] != "tcp" or not c["remote_addr"]:
            continue
        # The namespace is part of the key: without it two containers sharing
        # a bridge address would share a counter, and the previous-sample map
        # would attribute one's traffic to the other.
        ns = c.get("netns", "host")
        skey = f'{ns}|{c["local_addr"]}|{c["remote_addr"]}'
        cur = live.get((ns, c["local_addr"], c["remote_addr"]))
        if cur is None:
            continue
        now_sock[skey] = cur
        pv = prev.get(skey) or {}
        # Floor at 0: a reused 4-tuple restarts the counter, and a smaller
        # value is a reset, never negative traffic.
        d_tx = max(0, cur["sent"] - pv.get("sent", cur["sent"]))
        d_rx = max(0, cur["recv"] - pv.get("recv", cur["recv"]))
        cat = categorize(c, rules)
        # NETNS IS A LABEL, because it is the difference between a number that
        # can be compared to the NIC and one that cannot.
        #
        # This collector scans the host netns AND every container netns (see
        # netns_targets). The physical-NIC recording rule that netview divides
        # by counts PHYSICAL interfaces only -- its device filter drops veth.*,
        # docker.*, br-.*. So container-netns bytes have no carrier in the
        # denominator, and a "share of the pipe" built from the unlabelled sum
        # can exceed 100%: veth and bridge traffic can be several times the
        # physical NIC, so the numerator draws on a pool larger than what
        # divides it.
        #
        # Labelled, a consumer can ask the question it actually means:
        #   share of the physical pipe -> netns="host"
        #   total work on this node    -> no selector
        ns_label = "host" if ns == "host" else "container"
        bump(counters, "workload", [cat, "rx", ns_label], d_rx)
        bump(counters, "workload", [cat, "tx", ns_label], d_tx)
        # The remote of an in-tunnel socket IS the peer mesh IP — which is what
        # makes "who is driving THIS category" answerable at all.
        rhost = c["remote_addr"].rpartition(":")[0].strip("[]")
        if PEER_ATTRIBUTION and in_mesh(rhost):
            bump(counters, "workload_peer", [cat, rhost, "rx"], d_rx)
            bump(counters, "workload_peer", [cat, rhost, "tx"], d_tx)
    atomic_write(PREV_SOCK, now_sock)
    atomic_write(COUNTERS, counters)
    write_textfile(counters, gap_s, peers_seen, len(now_sock), mesh is not None,
                   gauges)
    return counters, gap_s


PROM_ESC = str.maketrans({"\\": "\\\\", '"': '\\"', "\n": " "})


def prom_line(name, labels, value):
    if not labels:
        return f"{name} {value}\n"
    lbl = ",".join(f'{k}="{str(v).translate(PROM_ESC)}"' for k, v in labels.items())
    return f"{name}{{{lbl}}} {value}\n"


def write_textfile(counters, gap_s, peers_seen, socks, mesh_ok, gauges=None):
    """Render the running totals for the node-exporter textfile collector.

    Written atomically into the collector dir — node-exporter reads whatever is
    there on each scrape, so a torn file would surface as a scrape error.

    Deliberately carries NO host-identifying label. The scrape config supplies
    `host`, and a textfile that also emitted one would collide: Prometheus
    renames the conflicting series label to `exported_host`, which would break
    every `by (host)` join in the recording rules without erroring anywhere.
    """
    out = []
    out.append("# HELP goat_net_transport_bytes_total Physical-pipe bytes by "
               "transport class. ADR 1128 TRANSPORT plane — do NOT add to the "
               "workload plane, they are the same bytes at two layers.\n")
    out.append("# TYPE goat_net_transport_bytes_total counter\n")
    out.append("# HELP goat_net_workload_bytes_total In-tunnel bytes by workload "
               "category. ADR 1128 WORKLOAD plane.\n")
    out.append("# TYPE goat_net_workload_bytes_total counter\n")
    out.append("# HELP goat_net_peer_bytes_total Physical-pipe bytes per mesh peer.\n")
    out.append("# TYPE goat_net_peer_bytes_total counter\n")
    out.append("# HELP goat_net_workload_peer_bytes_total In-tunnel bytes per "
               "category per peer — the category drill-down.\n")
    out.append("# TYPE goat_net_workload_peer_bytes_total counter\n")
    for key, val in sorted(counters.items()):
        parts = key.split("|")
        fam, rest = parts[0], parts[1:]
        if fam == "transport" and len(rest) == 2:
            out.append(prom_line("goat_net_transport_bytes_total",
                                 {"class": rest[0], "direction": rest[1]}, val))
        elif fam == "workload" and len(rest) == 3:
            out.append(prom_line("goat_net_workload_bytes_total",
                                 {"category": rest[0], "direction": rest[1],
                                  "netns": rest[2]}, val))
        elif fam == "workload" and len(rest) == 2:
            # Pre-label counters carried forward from an older state file. Emit
            # them as netns="unknown" rather than dropping them, so the counter
            # does not appear to reset on upgrade.
            out.append(prom_line("goat_net_workload_bytes_total",
                                 {"category": rest[0], "direction": rest[1],
                                  "netns": "unknown"}, val))
        elif fam == "peer" and len(rest) == 3:
            out.append(prom_line("goat_net_peer_bytes_total",
                                 {"peer": rest[0], "class": rest[1],
                                  "direction": rest[2]}, val))
        elif fam == "workload_peer" and len(rest) == 3:
            out.append(prom_line("goat_net_workload_peer_bytes_total",
                                 {"category": rest[0], "peer": rest[1],
                                  "direction": rest[2]}, val))
    # Point-in-time gauges (per-peer RTT and its measured flag).
    if gauges:
        out.append("# HELP goat_net_peer_latency_ms Per-peer round-trip time as "
                   "netbird measures it, split by path class. Meaningless unless "
                   "the matching goat_net_peer_latency_measured is 1.\n")
        out.append("# TYPE goat_net_peer_latency_ms gauge\n")
        out.append("# HELP goat_net_peer_latency_measured 1 when netbird has "
                   "actually timed this peer. 0 means NOT MEASURED — the latency "
                   "reads 0 and must not be rendered as a fast link.\n")
        out.append("# TYPE goat_net_peer_latency_measured gauge\n")
        for name, labels, val in gauges:
            out.append(prom_line(name, labels, val))

    # Honesty metrics — the dashboard grades its own coverage from these rather
    # than assuming the sample it is drawing is fresh and complete.
    out.append("# HELP goat_net_attribution_sample_gap_seconds Real interval "
               "covered by the last sample. 0 = first sample, no delta taken.\n")
    out.append("# TYPE goat_net_attribution_sample_gap_seconds gauge\n")
    out.append(prom_line("goat_net_attribution_sample_gap_seconds", {}, gap_s))
    out.append("# HELP goat_net_attribution_up Collector completed a sample.\n")
    out.append("# TYPE goat_net_attribution_up gauge\n")
    out.append(prom_line("goat_net_attribution_up", {}, 1))
    out.append("# HELP goat_net_attribution_mesh_plane_up netbird status was "
               "readable. 0 = transport plane is BLIND, not idle.\n")
    out.append("# TYPE goat_net_attribution_mesh_plane_up gauge\n")
    out.append(prom_line("goat_net_attribution_mesh_plane_up", {},
                         1 if mesh_ok else 0))
    out.append("# HELP goat_net_attribution_peers_sampled Connected peers in the sample.\n")
    out.append("# TYPE goat_net_attribution_peers_sampled gauge\n")
    out.append(prom_line("goat_net_attribution_peers_sampled", {}, peers_seen))
    out.append("# HELP goat_net_attribution_sockets_sampled TCP sockets with "
               "readable byte counters.\n")
    out.append("# TYPE goat_net_attribution_sockets_sampled gauge\n")
    out.append(prom_line("goat_net_attribution_sockets_sampled", {}, socks))
    # F-331. A coverage RATIO cannot distinguish "seen and uncategorised" from
    # "not seen at all", and for a year it was silently the second: sockets in
    # container namespaces never reached the categoriser, so the catch-all
    # buckets read flat zero while coverage read 7.2%. These two say how much
    # of the node was actually enumerated, so a low ratio can be read as
    # blindness rather than as idleness — and if targets > read, the sample is
    # incomplete and no ratio computed from it is honest.
    out.append("# HELP goat_net_attribution_netns_targets Container network "
               "namespaces found on this node (0 = none, or docker/nsenter absent).\n")
    out.append("# TYPE goat_net_attribution_netns_targets gauge\n")
    out.append(prom_line("goat_net_attribution_netns_targets", {},
                         NETNS_STATS.get("targets", 0)))
    out.append("# HELP goat_net_attribution_netns_read Container namespaces "
               "successfully enumerated. Below targets means the sample is BLIND "
               "for the difference, not that those containers were idle.\n")
    out.append("# TYPE goat_net_attribution_netns_read gauge\n")
    out.append(prom_line("goat_net_attribution_netns_read", {},
                         NETNS_STATS.get("read", 0)))

    d = os.path.dirname(TEXTFILE)
    try:
        os.makedirs(d, exist_ok=True)
        fd, tmp = tempfile.mkstemp(dir=d, prefix=".nnh-attr.")
        with os.fdopen(fd, "w") as f:
            f.write("".join(out))
        os.chmod(tmp, 0o644)
        os.replace(tmp, TEXTFILE)
    except Exception as e:
        # A missing textfile dir means node-exporter is not on this host; that
        # is a deployment fact, not a collector failure. Say so and carry on —
        # the Zenoh envelope does not depend on it.
        print(f"nnh-snapshot: textfile not written ({e})", file=sys.stderr)


def main():
    ap = argparse.ArgumentParser(description="node-net-health collector")
    ap.add_argument("--fast", action="store_true",
                    help="bandwidth attribution only (ADR 1128); skips the "
                         "inventory, the undeclared tail and the snapshot write")
    args = ap.parse_args()

    os.makedirs(STATE_DIR, exist_ok=True)
    baseline = load_json(BASELINE, {})
    rules = baseline.get("rules", [])

    if args.fast:
        try:
            counters, gap_s = attribution_pass(rules)
        except InventoryError as e:
            print(f"nnh-snapshot --fast: attribution not refreshed ({e})", file=sys.stderr)
            return 1
        print(f"nnh-snapshot --fast: {len(counters)} series, gap={gap_s}s",
              file=sys.stderr)
        return 0

    try:
        conns = classify(sockets(), rules)
    except InventoryError as e:
        print(f"nnh-snapshot: inventory not refreshed ({e})", file=sys.stderr)
        return 1
    mesh, gap_s = mesh_plane()
    undeclared = track_undeclared(conns)
    snap = {
        "generated_unix_ms": now_ms(),
        "node": os.uname().nodename,
        "node_class": baseline.get("node_class", "unknown"),
        "mesh": mesh,                       # None -> publisher grades honestly
        "snapshot_gap_secs": max(0, gap_s),
        "conns": conns,
        "undeclared_count": undeclared,
    }
    atomic_write(SNAPSHOT, snap)
    print(f"nnh-snapshot: {len(conns)} conns, {undeclared} undeclared, "
          f"mesh={'ok' if mesh else 'absent'}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
