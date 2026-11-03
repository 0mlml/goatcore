#!/usr/bin/env python3
"""Federation round-trip responder — runs on the RECEIVING sovereign.

It answers probes that arrived through the peering. It can only see a probe if
the partner's own router delivered it, so the acknowledgement is evidence the
sample transited THIS side, not merely that it left the other one.

Rides on the two grants the agreement already makes: we write under our own wire
name, they receive it. No additional access is required, and none is asked for.
"""
import json, time, base64, hashlib, urllib.request, sys

BASE      = "http://127.0.0.1:8000"
THEIR_WIRE= sys.argv[1]          # e.g. from/<them>   — where their probes land
OUR_WIRE  = sys.argv[2]          # e.g. from/xaa    — where our ack goes
seen = set()

def get(k):
    try:
        with urllib.request.urlopen(f"{BASE}/{k}", timeout=5) as r:
            return json.loads(r.read().decode())
    except Exception:
        return []

def put(k, v):
    urllib.request.urlopen(
        urllib.request.Request(f"{BASE}/{k}", data=v.encode(), method="PUT"), timeout=5)

while True:
    for row in get(f"{THEIR_WIRE}/fedcheck/**"):
        key = row.get("key", "")
        if key in seen:
            continue
        seen.add(key)
        raw = base64.b64decode(row.get("value", "")) if row.get("value") else b""
        nonce = key.rsplit("/", 1)[-1]
        # The ack names what we saw and where we saw it. The digest is over the
        # payload as RECEIVED, so a mangled sample produces a mismatched ack
        # rather than a silent success.
        ack = json.dumps({
            "nonce":        nonce,
            "observed_key": key,
            "sha256":       hashlib.sha256(raw).hexdigest(),
            "observed_at":  time.time(),
            "responder":    OUR_WIRE,
        }, sort_keys=True)
        put(f"{OUR_WIRE}/fedcheck-ack/{nonce}", ack)
    time.sleep(0.2)
