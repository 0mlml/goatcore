#!/usr/bin/env python3
"""zenoh-router-signals — delivery-stall signals the router only writes to its log.

Called by zenoh-health-probe.sh once per probe run. Emits one JSON object on
stdout; the probe folds it into its payload and its Prometheus textfile.

WHY THIS EXISTS . A router that cannot push to one
slow subscriber blocks the sending receive thread for `wait_before_close` and
then closes the transport; while it does, delivery from that source stops for
everyone, for as long as it lasts, while every existing signal (healthcheck,
REST, new-session open) stays green. The only
place the mechanism is visible is the router log:

  Unable to push non droppable network message to <zid>. Closing transport!

and the orphaned query replies that pile up behind it. No exported metric
counted either, so nothing alerted.

What it reads:

  * the router container's json-file log, TAILED BY BYTE OFFSET. `docker logs
    --since` rescans the file on every call (over a second per call on a large
    log); an offset kept in STATE_DIR reads only what was appended. Rotation is
    detected by inode change or shrink, and reading restarts at the new file's
    head. The first run ever records the end of file and counts nothing, so a
    deploy never replays a day of history into a counter.
  * the router's own `@/local/router` admin reply (passed in on stdin by the
    probe, which already fetched it), for the session census: sessions by
    `whatami`, and router/peer-mode sessions from remote addresses that are not
    on the declared allow list — the F-287 router-mode peer inventory.

Counters are cumulative and persisted in STATE_DIR so Prometheus `increase()`
works across probe runs; they reset only if STATE_DIR is wiped (Prometheus
handles a counter reset).

Honesty: every field is null when it could not be measured (log unreadable,
admin reply missing) and `log_tail_ok` says which. Never a fabricated zero.

Usage:
  zenoh-router-signals.py --log-path <json.log> --state-dir <dir> \
      [--allow-router-zids zid1,zid2] [--local-net 172.19.0.0/16] < admin.json
"""
import argparse
import ipaddress
import json
import os
import re
import sys
import time

CLOSE_RE = re.compile(r"Unable to push non droppable network message to ([0-9a-f]+)")
ORPHAN_RE = re.compile(
    r"Route reply: Query not found|Received ReplyData for unknown Query"
    r"|Received ResponseFinal for unknown Request"
)
# Bound one run's work. A burst larger than this is still counted next run.
MAX_READ_BYTES = 64 * 1024 * 1024
# How many recently-closed ZIDs to surface per run (a label, so keep it small).
MAX_RECENT_ZIDS = 5


def load_state(path):
    try:
        with open(path) as f:
            return json.load(f)
    except Exception:
        return {}


def save_state(path, state):
    tmp = path + ".tmp"
    with open(tmp, "w") as f:
        json.dump(state, f)
    os.replace(tmp, path)


def tail_log(log_path, state):
    """Read lines appended since the saved offset. Returns (lines, ok)."""
    try:
        st = os.stat(log_path)
    except OSError:
        return [], False
    inode, size = st.st_ino, st.st_size
    prev_inode = state.get("inode")
    offset = state.get("offset")
    if prev_inode is None or offset is None:
        # First run: anchor at end of file, count nothing.
        state["inode"], state["offset"] = inode, size
        return [], True
    if inode != prev_inode or size < offset:
        offset = 0  # rotated or truncated: the new file's head
    if size == offset:
        state["inode"], state["offset"] = inode, size
        return [], True
    to_read = min(size - offset, MAX_READ_BYTES)
    try:
        with open(log_path, "rb") as f:
            f.seek(offset)
            buf = f.read(to_read)
    except OSError:
        return [], False
    # Only consume whole lines; a partial trailing line is read next run.
    cut = buf.rfind(b"\n")
    if cut < 0:
        state["inode"], state["offset"] = inode, offset
        return [], True
    consumed = buf[: cut + 1]
    state["inode"], state["offset"] = inode, offset + len(consumed)
    return consumed.decode("utf-8", "replace").splitlines(), True


def log_text(line):
    """A docker json-file line carries the message in `log`; tolerate raw text."""
    try:
        return json.loads(line).get("log", "")
    except Exception:
        return line


def scan(lines):
    closes, orphans, zids = 0, 0, {}
    for line in lines:
        text = log_text(line)
        m = CLOSE_RE.search(text)
        if m:
            closes += 1
            zids[m.group(1)] = zids.get(m.group(1), 0) + 1
            continue
        if ORPHAN_RE.search(text):
            orphans += 1
    return closes, orphans, zids


def parse_allow(raw):
    """Split the allow list into (zids, networks).

    An entry is either a ZID or a mesh address / CIDR. BOTH are supported
    because a third-party router's ZID is NOT stable: a peer can present
    different ZIDs within one day,
    so a ZID-only list goes stale on the peer's next restart and the alert
    quietly returns. An address survives a restart; prefer it.
    """
    zids, nets = set(), []
    for item in (raw or "").split(","):
        item = item.strip()
        if not item:
            continue
        try:
            nets.append(ipaddress.ip_network(item, strict=False))
        except ValueError:
            zids.add(item)
    return zids, nets


def linkless_sessions(admin):
    """Sessions in the router's table that reference no network link, or None.

    A session whose teardown never finished stays in the table with an empty
    `links` list and still counts toward max_sessions. When the table fills,
    the router refuses every new transport and logs it only at trace level
    (#1922), so this count is the only early warning.
    """
    if isinstance(admin, list):
        admin = admin[0].get("value", {}) if admin else {}
    sessions = (admin or {}).get("sessions")
    if not isinstance(sessions, list):
        return None
    return sum(1 for s in sessions if not s.get("links"))


def session_census(admin, allow_zids, local_nets, allow_nets=()):
    """(sessions_by_whatami, remote_router_sessions) or (None, None)."""
    if isinstance(admin, list):
        admin = admin[0].get("value", {}) if admin else {}
    sessions = (admin or {}).get("sessions")
    if not isinstance(sessions, list):
        return None, None
    by_kind, remote_routers = {}, []
    for s in sessions:
        kind = s.get("whatami") or "unknown"
        by_kind[kind] = by_kind.get(kind, 0) + 1
        if kind not in ("router", "peer"):
            continue
        zid = s.get("peer", "")
        if zid in allow_zids:
            continue
        dst = ((s.get("links") or [{}])[0]).get("dst", "")
        host = re.sub(r"^[a-z]+/", "", dst).rsplit(":", 1)[0].strip("[]")
        try:
            ip = ipaddress.ip_address(host)
        except ValueError:
            remote_routers.append(zid)
            continue
        if ip.is_loopback or any(ip in n for n in local_nets):
            continue
        if any(ip in n for n in allow_nets):
            continue
        remote_routers.append(zid)
    return by_kind, remote_routers


def matches_everything(key: str) -> bool:
    """True when this declared key expression selects the WHOLE keyspace.

    `**` is the spelling everyone uses and the one the ACL refuses. It is not
    the only one: zenoh canonizes `**/*` to `*/**`, and `*/**` (or `*/*/**`, or
    a bare `*`) matches every key with at least one chunk — the whole fabric
    under a different name. No ACL rule can refuse those without also refusing
    ordinary keys, so the policy leans on this counter instead: the shape is
    reported per peer, and an unnamed peer declaring one is an alert, not a
    silent success.

    A key qualifies when every chunk is a wildcard. One literal chunk anywhere
    (`**/basemap_vendor/**`, `*/geo/track/v1/*`) makes it a filter, which is a
    legitimate subscription and NOT counted.
    """
    chunks = [c for c in key.split("/") if c]
    return bool(chunks) and all(c in ("*", "**") for c in chunks)


def fabric_wide_subscribers(census):
    """{zid: [whole-keyspace key exprs]} from the router's subscriber census.

    The census is `@/<router>/router/subscriber/<declared key expr>` with
    `value.clients` listing the sessions that declared it.
    """
    rows = census if isinstance(census, list) else [census]
    mark = "/router/subscriber/"
    found = {}
    seen_any = False
    for r in rows:
        if not isinstance(r, dict):
            continue
        key = r.get("key") or ""
        i = key.find(mark)
        if i < 0:
            continue
        seen_any = True
        declared = key[i + len(mark):]
        if not matches_everything(declared):
            continue
        for zid in (r.get("value") or {}).get("clients") or []:
            found.setdefault(zid, [])
            if declared not in found[zid]:
                found[zid].append(declared)
    return found if seen_any else None


def main(argv=None):
    ap = argparse.ArgumentParser()
    ap.add_argument("--log-path", default="")
    ap.add_argument("--state-dir", required=True)
    ap.add_argument("--allow-router-zids", default="",
                    help="declared router/peer-mode sessions: ZIDs and/or addresses/CIDRs")
    ap.add_argument("--local-net", action="append", default=[])
    ap.add_argument("--subscriber-census", default="",
                    help="file holding GET /@/*/router/subscriber/** (whole-keyspace detector)")
    a = ap.parse_args(argv)

    os.makedirs(a.state_dir, exist_ok=True)
    state_path = os.path.join(a.state_dir, "router-signals.json")
    state = load_state(state_path)

    out = {"log_tail_ok": False, "slow_consumer_closes_total": None,
           "orphan_replies_total": None, "recent_close_zids": None,
           "sessions_by_whatami": None, "remote_router_sessions": None,
           "linkless_sessions": None,
           "remote_router_zids": None, "fabric_wide_subscribers": None,
           "fabric_wide_shapes": None}

    if a.log_path:
        lines, ok = tail_log(a.log_path, state)
        if ok:
            closes, orphans, zids = scan(lines)
            state["closes_total"] = state.get("closes_total", 0) + closes
            state["orphans_total"] = state.get("orphans_total", 0) + orphans
            now = int(time.time())
            recent = state.get("recent_zids", {})
            for z in zids:
                recent[z] = now
            # Keep the most recently closed few; age out after an hour.
            recent = {z: t for z, t in recent.items() if now - t < 3600}
            recent = dict(sorted(recent.items(), key=lambda kv: -kv[1])[:MAX_RECENT_ZIDS])
            state["recent_zids"] = recent
            out.update(log_tail_ok=True,
                       slow_consumer_closes_total=state["closes_total"],
                       orphan_replies_total=state["orphans_total"],
                       recent_close_zids=recent)

    admin = None
    raw = sys.stdin.read() if not sys.stdin.isatty() else ""
    if raw.strip():
        try:
            admin = json.loads(raw)
        except Exception:
            admin = None
    allow, allow_nets = parse_allow(a.allow_router_zids)
    nets = []
    for n in a.local_net:
        try:
            nets.append(ipaddress.ip_network(n, strict=False))
        except ValueError:
            pass  # skip invalid CIDR
    by_kind, remote = session_census(admin, allow, nets, allow_nets)
    if by_kind is not None:
        out.update(sessions_by_whatami=by_kind,
                   remote_router_sessions=len(remote),
                   remote_router_zids=remote[:MAX_RECENT_ZIDS],
                   linkless_sessions=linkless_sessions(admin))

    # Whole-keyspace subscribers, from the subscriber census. The ACL refuses
    # the `**` spelling to anyone not named but cannot refuse the
    # equivalent `*/**`, so this counts the SHAPE wherever it appears and lets
    # an alert name the peer. Absent census file -> null, never zero.
    if a.subscriber_census:
        try:
            with open(a.subscriber_census, encoding="utf-8") as f:
                census = json.load(f)
        except Exception:
            census = None
        found = fabric_wide_subscribers(census) if census is not None else None
        if found is not None:
            shapes = sorted({k for keys in found.values() for k in keys})
            out.update(fabric_wide_subscribers=sorted(found), fabric_wide_shapes=shapes)

    save_state(state_path, state)
    json.dump(out, sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
