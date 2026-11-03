#!/usr/bin/env python3
"""redact-relay-health.py — write the reduced-detail variant of the dashboard data.

Relay topology is recon-sensitive (ADR 1066). The gate decides WHO gets in; this
decides WHAT they see once inside. Admins get the full document; everyone else
gets this one.

WHY SERVER-SIDE AND NOT IN THE PAGE'S JAVASCRIPT
Hiding fields in the browser would be theatre. The dashboard is static nginx and
the page fetches relay-health.json wholesale, so anyone admitted could read the
hidden fields straight out of devtools or with one curl. Redaction only means
anything if the withheld values never leave the box, which means two documents
and a server-side choice between them.

WHAT IS WITHHELD — the fields that help someone ACT against the fleet:
  ip                 a relay's public address: the target itself
  placement.lat/lon  precise siting; the human-readable `place` stays
  deep.peers[].peer_id  per-peer WireGuard identity hex
  region/az inside placement    provider-internal siting detail
  instance_id        (never in this document, but stripped if it appears)

WHAT SURVIVES — everything the page exists to answer:
  name, region, goatnet, placement.place, status/enroll/rathole/dataplane
  booleans, latency, aggregate throughput, capacity, peer COUNTS.
`region` is deliberately kept: it is coarse (an AWS region spans a country or
more), it is what makes latency numbers interpretable, and the operator judged
it not worth losing. Aggregate counts stay for the same reason — "11 peers on
fra" is an operational fact; the 40-char identity of each is not.

The reduced view stays HONEST: nothing is faked or rounded to mislead. A field
that is withheld is ABSENT, not zeroed — a zero would read as a measurement.

Usage:  redact-relay-health.py <in.json> <out.json>
"""
import json
import sys

# Per-relay keys dropped outright.
DROP_RELAY = ("ip", "instance_id")
# Placement sub-keys dropped; `place` and `provider` survive.
DROP_PLACEMENT = ("lat", "lon", "az")
# Deep-block sub-keys dropped.
DROP_DEEP = ("peers",)


def redact_relay(r):
    out = {k: v for k, v in r.items() if k not in DROP_RELAY}

    p = r.get("placement")
    if isinstance(p, dict):
        out["placement"] = {k: v for k, v in p.items() if k not in DROP_PLACEMENT}

    d = r.get("deep")
    if isinstance(d, dict):
        nd = {k: v for k, v in d.items() if k not in DROP_DEEP}
        # Replace the per-peer LIST with its length. The count is operational;
        # the identities are not. Absent when the relay published no list at all,
        # so "idle relay" stays distinguishable from "no publisher" (the
        # distinction the full view relies on too).
        peers = d.get("peers")
        if isinstance(peers, list):
            nd["peer_count"] = len(peers)
        out["deep"] = nd

    return out


def main():
    if len(sys.argv) != 3:
        print(__doc__.strip().splitlines()[-1], file=sys.stderr)
        return 2
    src, dst = sys.argv[1], sys.argv[2]
    try:
        with open(src) as f:
            doc = json.load(f)
    except FileNotFoundError:
        # The prober has not run yet. Emit nothing rather than an empty document
        # that the page would render as a real "no relays" answer.
        return 0
    except json.JSONDecodeError as e:
        print(f"redact: {src} is not valid JSON ({e}); leaving previous output", file=sys.stderr)
        return 1

    doc["relays"] = [redact_relay(r) for r in (doc.get("relays") or [])]
    # Marked so the page can say so out loud rather than silently showing less.
    doc["view"] = "reduced"

    tmp = dst + ".tmp"
    with open(tmp, "w") as f:
        json.dump(doc, f)
    import os
    os.replace(tmp, dst)
    return 0


if __name__ == "__main__":
    sys.exit(main())
