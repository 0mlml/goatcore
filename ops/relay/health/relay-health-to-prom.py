#!/usr/bin/env python3
"""relay-health-to-prom.py — bridge the relay-health prober output into a
Prometheus textfile so Grafana renders the SAME relay signals as the custom
single-file dashboard.

Why this exists: blackbox-exporter can re-derive relay *reachability* and TCP
latency, but it cannot do the prober's app-layer signals — the real STUN
binding request (:3478), the multi-try enrollment ok-ratio over the rathole
hairpin, or the `--deep` SSM metrics (throughput / peers / cpu). So instead of
re-probing with blackbox, we reuse the prober's own measurements: it already
writes relay-health.json; this converts that JSON into `goat_relay_*` metrics
on stdout / a .prom file. Drop the .prom into a node-exporter
`--collector.textfile.directory` (nova-obs already runs that collector) and the
relay Grafana dashboard (uid relay-fleet-health) lights up at near-parity.

Honesty discipline carries over: `goat_relay_probe_timestamp_seconds` lets the
dashboard grey on staleness; deep-only series are simply absent (not zero) when
the prober ran fast-only, so panels read "no data" rather than a false zero.

Usage:
  ./relay-health-probe.sh --out relay-health.json
  ./relay-health-to-prom.py --in relay-health.json --out /var/lib/node_exporter/textfile/relay-health.prom
  # or pipe: ./relay-health-to-prom.py < relay-health.json > relay.prom

Atomic write (tmp + rename) so node-exporter never scrapes a half-written file.
"""
import argparse
import json
import os
import sys


def num(v):
    """Return a float for a JSON value, or None if it is null / unparseable /
    a sentinel ('?', '', -1 latency)."""
    if v is None:
        return None
    if isinstance(v, bool):
        return 1.0 if v else 0.0
    if isinstance(v, (int, float)):
        return float(v)
    s = str(v).strip()
    if s in ("", "?", "unknown", "null"):
        return None
    try:
        return float(s)
    except ValueError:
        return None


def metric(lines, name, value, labels=None, mtype="gauge", help_text=None):
    if value is None:
        return
    if name not in metric._seen:
        if help_text:
            lines.append(f"# HELP {name} {help_text}")
        lines.append(f"# TYPE {name} {mtype}")
        metric._seen.add(name)
    # Full precision: %g truncates a unix timestamp (~1.78e9) to 6 sig figs,
    # which would corrupt the staleness math. Print integral values as ints,
    # everything else via repr (round-trip-safe).
    vs = str(int(value)) if value == int(value) else repr(value)
    if labels:
        lbl = ",".join(f'{k}="{v}"' for k, v in labels.items())
        lines.append(f"{name}{{{lbl}}} {vs}")
    else:
        lines.append(f"{name} {vs}")


metric._seen = set()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--in", dest="inp", default="-")
    ap.add_argument("--out", dest="out", default="-")
    args = ap.parse_args()

    if args.inp != "-":
        with open(args.inp) as source_file:
            raw = source_file.read()
    else:
        raw = sys.stdin.read()
    data = json.loads(raw)

    lines = []
    metric._seen = set()

    tunnel = 1.0 if data.get("tunnel_endpoint") else 0.0
    metric(lines, "goat_relay_probe_timestamp_seconds", num(data.get("generated")),
           help_text="unix time the prober last wrote (staleness honesty)")
    metric(lines, "goat_relay_vantage_tunnel_endpoint", tunnel,
           help_text="1 = probed from the rathole tunnel endpoint (Thor); :443/STUN are informational")
    metric(lines, "goat_relay_deep", 1.0 if data.get("deep") else 0.0,
           help_text="1 = --deep run (SSM service-state + metrics present)")

    for r in data.get("relays", []):
        name = r.get("name", "?")
        lbl = {"relay": name, "region": r.get("region", ""),
               "goatnet": r.get("goatnet", "")}
        rl = {"relay": name}

        enroll = r.get("enroll", {}) or {}
        ok = num(enroll.get("ok")) or 0.0
        tries = num(enroll.get("tries")) or 0.0
        ratio = (ok / tries) if tries else None
        metric(lines, "goat_relay_enroll_ok_ratio", ratio, rl,
               help_text=":443 enrollment-tunnel TLS ok-ratio over N tries")
        metric(lines, "goat_relay_enroll_up", (1.0 if ok > 0 else 0.0), rl,
               help_text=":443 enrollment reachable at all (ok>0)")
        lat = num(r.get("lat_ms"))
        if lat is not None and lat >= 0:
            metric(lines, "goat_relay_enroll_tcp_latency_ms", lat, rl,
                   help_text=":443 min TCP-connect latency (ms)")

        rh = 1.0 if r.get("rathole_ctl") else 0.0
        metric(lines, "goat_relay_rathole_ctl_up", rh, rl,
               help_text=":2333 rathole control-channel reachable (the non-hairpin health driver)")
        metric(lines, "goat_relay_stun_ok", (1.0 if r.get("stun") else 0.0), rl,
               help_text=":3478 coturn STUN binding-request success")

        # vantage-aware rollup, mirroring the custom dashboard's logic:
        # tunnel-endpoint vantage -> rathole control is the health driver;
        # external vantage -> enrollment ok is the driver.
        up = rh if tunnel else (1.0 if ok > 0 else 0.0)
        metric(lines, "goat_relay_up", up, lbl,
               help_text="vantage-aware relay health (tunnel->rathole, external->enroll)")

        deep = r.get("deep") or {}
        if isinstance(deep, dict):
            for src, dst, help_t in [
                ("rx_bps", "goat_relay_rx_bps", "host NIC rx bytes/s (--deep)"),
                ("tx_bps", "goat_relay_tx_bps", "host NIC tx bytes/s (--deep)"),
                ("rx_pps", "goat_relay_rx_pps", "host NIC rx packets/s (--deep)"),
                ("tx_pps", "goat_relay_tx_pps", "host NIC tx packets/s (--deep)"),
                ("peers_5m", "goat_relay_peers_5m", "distinct PeerIds in 5m of relay logs (--deep)"),
                ("cpu_pct", "goat_relay_cpu_pct", "goat-relay container CPU % (--deep)"),
                ("mem_pct", "goat_relay_mem_pct", "goat-relay container mem % (--deep)"),
                ("heals_1h", "goat_relay_heals_1h", "rathole watchdog heals in 1h (--deep)"),
                ("relay_up", "goat_relay_container_up", "goat-relay container running (--deep)"),
                ("coturn_listen", "goat_relay_coturn_listen", "coturn :3478 listening (--deep)"),
            ]:
                metric(lines, dst, num(deep.get(src)), rl, help_text=help_t)
            # service-state strings -> 1/0 active
            for src, dst in [("rathole", "goat_relay_rathole_server_active"),
                             ("watchdog", "goat_relay_watchdog_active")]:
                v = deep.get(src)
                if v is not None and v != "unknown":
                    metric(lines, dst, 1.0 if v == "active" else 0.0, rl,
                           help_text=f"systemd {src} is active (--deep)")

            # F-269 — per-peer auth rejections in the 5m log window, by reason,
            # + a per-offender gauge that NAMES the peer egress IP. This is the
            # signal that would have pointed at the profile-drift single-peer
            # isolation in minutes (the relay logged every rejection; nothing
            # surfaced which peer). Zeros ARE emitted (a live 0 reads "no
            # rejections", not "no data"). ws_handoff_eof is a benign QUIC-race
            # reason — carried for context, not alerted on.
            ar = deep.get("auth_reject")
            if isinstance(ar, dict):
                for reason in ("expired_token", "ws_upgrade_timeout", "ws_handoff_eof"):
                    metric(lines, "goat_relay_auth_reject_5m", num(ar.get(reason)),
                           {"relay": name, "reason": reason},
                           help_text="relay auth/handshake rejections in the last 5m of goat-relay logs, by reason")
                for off in (ar.get("top_offenders") or []):
                    if isinstance(off, dict) and off.get("ip"):
                        metric(lines, "goat_relay_auth_reject_top_offender_5m", num(off.get("count")),
                               {"relay": name, "peer_ip": str(off["ip"])},
                               help_text="per-source expired-auth-token rejections in 5m — names the isolated peer egress IP")

            # F-275 — data-plane delivery: drop counters by reason, the QUIC-vs-WS
            # connection split, and the writer-death / eviction rates. These are
            # what make the cross-transport writer-wedge visible from outside the
            # relay. datagram_too_large is expected-benign in a trickle post-fix
            # (drop+count) and alertable on a spike; writer_death should stay ~0.
            dv = deep.get("delivery")
            if isinstance(dv, dict):
                dr = dv.get("drops") or {}
                for reason in ("peer_not_found", "writer_full", "writer_closed", "datagram_too_large"):
                    metric(lines, "goat_relay_drops_total", num(dr.get(reason)),
                           {"relay": name, "reason": reason},
                           help_text="lifetime relay data-plane frame drops by reason (ADR 1055 / F-275)")
                for src, dst, ht in [
                    ("writer_death", "goat_relay_writer_death_recent",
                     "QUIC writers that hit a connection-level failure in the window"),
                    ("evictions", "goat_relay_evictions_recent",
                     "peers evicted on dead-writer in the window"),
                    ("datagram_too_large", "goat_relay_datagram_too_large_recent",
                     "oversized cross-transport frames dropped in the window"),
                ]:
                    metric(lines, dst, num(dv.get(src)), rl, help_text=ht)
                for t in ("quic", "ws"):
                    metric(lines, "goat_relay_connections", num(dv.get("conns_" + t)),
                           {"relay": name, "transport": t},
                           help_text="active relay connections by transport")

    out = "\n".join(lines) + "\n"
    if args.out == "-":
        sys.stdout.write(out)
    else:
        tmp = args.out + ".tmp"
        with open(tmp, "w") as f:
            f.write(out)
        os.replace(tmp, args.out)


if __name__ == "__main__":
    main()
