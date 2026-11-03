#!/usr/bin/env python3
# Bounded baseline probe of a live fabric (pre-ACL-split).
# Proves: (a) mTLS session opens, (b) authorized publish to own prefix works,
# (c) subscriber delivery works (round-trip). Exits non-zero on failure.
import json, os, sys, time, threading
import zenoh

ROUTER = os.environ.get("NOVA_ROUTER", "tls/zenoh.nova.netbird.nova.example:7447")
# Directory holding the vendor mTLS cert bundle (<vendor>-cert.pem / <vendor>-key.pem /
# <site>-ca-root.pem). Operator-local, NOT committed. Override with NOVA_CERT_DIR.
HERE = os.environ.get("NOVA_CERT_DIR", os.path.expanduser("~/.goat/state/jailbird-shared"))
PREFIX = os.environ.get("NOVA_VENDOR", "jailbird")   # the authorized vendor prefix + cert basename
KEY = f"{PREFIX}/moon-pod-baseline/ping"

def make_config():
    conf = zenoh.Config()
    conf.insert_json5("mode", '"client"')
    conf.insert_json5("connect/endpoints", json.dumps([ROUTER]))
    conf.insert_json5("transport/link/tls", json.dumps({
        "root_ca_certificate": os.path.join(HERE, "nova-ca-root.pem"),
        "connect_certificate": os.path.join(HERE, f"{PREFIX}-cert.pem"),
        "connect_private_key": os.path.join(HERE, f"{PREFIX}-key.pem"),
        "enable_mtls": True,
        "verify_name_on_connect": True,
    }))
    return conf

got = threading.Event()
received = {}

def on_sample(sample):
    received["key"] = str(sample.key_expr)
    received["payload"] = bytes(sample.payload).decode("utf-8", "replace")
    got.set()

print(f"[probe] opening mTLS session to {ROUTER}", flush=True)
session = zenoh.open(make_config())
print("[probe] session OPEN", flush=True)

sub = session.declare_subscriber(f"{PREFIX}/**", on_sample)
print(f"[probe] subscribed {PREFIX}/**", flush=True)
time.sleep(1.0)  # let the subscriber declaration propagate

payload = f"moon-pod baseline {time.strftime('%H:%M:%S')}"
session.put(KEY, payload.encode())
print(f"[probe] PUT {KEY} = {payload!r}", flush=True)

ok = got.wait(timeout=8.0)
if ok:
    print(f"[probe] RECEIVED  key={received['key']}  payload={received['payload']!r}", flush=True)
    print("[probe] RESULT: PASS — authorized publish + delivery round-trip works on live nova-a", flush=True)
    rc = 0
else:
    print("[probe] RESULT: FAIL — no sample received within 8s (publish or delivery broken)", flush=True)
    rc = 1

sub.undeclare()
session.close()
sys.exit(rc)
