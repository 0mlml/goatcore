#!/usr/bin/env python3
# Negative/squat probe: with the jailbird vendor cert, attempt to PUBLISH to a
# prefix jailbird does NOT own. On the CURRENT fabric (egress+ingress ** write
# over-grant) this is expected to SUCCEED — demonstrating the ADR 1020 hole.
# After the ingress/egress split it should be DENIED.
import json, os, time, threading
import zenoh

ROUTER = os.environ.get("NOVA_ROUTER", "tls/zenoh.nova.netbird.nova.example:7447")
# Operator-local cert bundle dir, NOT committed. Override with NOVA_CERT_DIR / NOVA_VENDOR.
HERE = os.environ.get("NOVA_CERT_DIR", os.path.expanduser("~/.goat/state/jailbird-shared"))
VENDOR = os.environ.get("NOVA_VENDOR", "jailbird")  # the cert basename (the prefix it DOES own)
# A prefix the vendor does NOT own — clearly-foreign namespace.
SQUAT_KEY = os.environ.get("NOVA_SQUAT_KEY", "moon-pod-squat-test/intruder/ping")

def make_config():
    conf = zenoh.Config()
    conf.insert_json5("mode", '"client"')
    conf.insert_json5("connect/endpoints", json.dumps([ROUTER]))
    conf.insert_json5("transport/link/tls", json.dumps({
        "root_ca_certificate": os.path.join(HERE, "nova-ca-root.pem"),
        "connect_certificate": os.path.join(HERE, f"{VENDOR}-cert.pem"),
        "connect_private_key": os.path.join(HERE, f"{VENDOR}-key.pem"),
        "enable_mtls": True,
        "verify_name_on_connect": True,
    }))
    return conf

got = threading.Event()
received = {}
def on_sample(sample):
    received["key"] = str(sample.key_expr)
    got.set()

print(f"[squat] opening session (vendor=jailbird) to publish FOREIGN prefix {SQUAT_KEY}", flush=True)
session = zenoh.open(make_config())
sub = session.declare_subscriber("moon-pod-squat-test/**", on_sample)
time.sleep(1.0)
payload = f"SQUAT attempt {time.strftime('%H:%M:%S')}"
session.put(SQUAT_KEY, payload.encode())
print(f"[squat] PUT {SQUAT_KEY}", flush=True)
ok = got.wait(timeout=6.0)
if ok:
    print(f"[squat] RECEIVED back key={received['key']}", flush=True)
    print("[squat] RESULT: SQUAT SUCCEEDED — this is the ADR 1020 over-grant (current fabric). The ACL split will flip this to DENIED.", flush=True)
else:
    print("[squat] RESULT: squat produced no delivery within 6s — either already denied, or no route/storage for the foreign prefix.", flush=True)
sub.undeclare()
session.close()
