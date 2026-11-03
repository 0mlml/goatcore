#!/usr/bin/env python3
"""Minimal zenoh mTLS client for the slot-pool isolation proof.

  iso_client.py sub  <cert> <key> <ca> <endpoint> <seconds>
  iso_client.py pub  <cert> <key> <ca> <endpoint> <keyexpr>...

sub: subscribe `**`, print RECV <key> for each sample for <seconds>.
pub: put each <keyexpr> (payload "x"), print PUT <key>.
The router's per-slot ACL decides which puts are admitted; a denied put
is silent at the publisher and simply never reaches the subscriber.
"""
import json
import sys
import time

import zenoh

mode, cert, key, ca, endpoint = sys.argv[1:6]
conf = zenoh.Config.from_json5(json.dumps({
    "mode": "client",
    "connect": {"endpoints": [endpoint]},
    "transport": {"link": {"tls": {
        "root_ca_certificate": ca,
        "connect_certificate": cert,
        "connect_private_key": key,
        "enable_mtls": True,
    }}},
}))

session = zenoh.open(conf)
try:
    if mode == "sub":
        def on_sample(sample):
            print("RECV " + str(sample.key_expr), flush=True)
        session.declare_subscriber("**", on_sample)
        print("SUB-READY", flush=True)
        time.sleep(float(sys.argv[6]))
    elif mode == "pub":
        time.sleep(0.5)  # let the session settle
        for ke in sys.argv[6:]:
            session.put(ke, b"x")
            print("PUT " + ke, flush=True)
        time.sleep(1.0)
    else:
        print("unknown mode", file=sys.stderr)
        sys.exit(2)
finally:
    session.close()
