#!/usr/bin/env python3
"""E0/M2.0 on-fabric transport proof for grant-keyed encryption.

Carries the grant-crypto envelope over a LIVE Zenoh transport and confirms the
crypto composes with the wire end to end. There is no maintained Go Zenoh client
(the gap rest-pub-bridge / ADR 1023 exists to close), so the crypto is the Go
`gce` CLI and the transport is this Python harness — the same Go-crypto +
native-Zenoh split E1 will run in production (Go key-service publishing via
rest-pub-bridge; native cert-processes consuming).

What it proves, on a real router link (two peers, loopback TCP, multicast off):

  1. The producer's sealed payload and the keys/<rs>/<gen> bundle survive a real
     Zenoh round-trip byte-for-byte.
  2. Both generation-tag carriers work: the BUNDLE rides in-band JSON (the path a
     Go publisher via rest-pub-bridge must use — the bridge is body-only), and the
     DATA payload rides the Zenoh ATTACHMENT (payload bytes untouched — the path a
     native producer can use). See grant-keyed-encryption.md §7.
  3. The authorized member unwraps its slot and opens the payload (exit 0).
  4. An outsider with no slot in the bundle is denied (exit 3) — the machine-plane
     read cut, enforced by the crypto, not the router.

Usage:  harness.py [workdir]   (GCE=/path/to/gce overrides the binary; default ./gce)
Exit 0 = proof passed; nonzero = a check failed.
"""
import base64
import json
import os
import subprocess
import sys
import tempfile
import threading
import time

import zenoh

RS = "coalition-air"
GEN = 7
DATA_KEYEXPR = f"data/{RS}/cop"          # AAD-bound at seal; the member must match it
BUNDLE_KEYEXPR = f"keys/{RS}/{GEN}"
ENDPOINT = "tcp/127.0.0.1:17447"

GCE = os.environ.get("GCE", "./gce")
PLAINTEXT = b"brigade COP snapshot, releasable to REL-EXAMPLE only"


def run_gce(*args, check=True):
    """Invoke the gce CLI; return (returncode, stdout_bytes)."""
    p = subprocess.run([GCE, *args], capture_output=True)
    if check and p.returncode != 0:
        sys.stderr.write(p.stderr.decode(errors="replace"))
        raise SystemExit(f"gce {args[0]} failed (exit {p.returncode})")
    return p.returncode, p.stdout


def main():
    work = sys.argv[1] if len(sys.argv) > 1 else tempfile.mkdtemp(prefix="gce-e0-")
    os.makedirs(work, exist_ok=True)
    j = lambda name: os.path.join(work, name)

    # --- producer side (Go crypto) --------------------------------------------
    run_gce("gen-member", "member", work)
    run_gce("gen-member", "outsider", work)
    run_gce("gen-member", "keyservice", work)   # the bundle-signing identity
    with open(j("pt.txt"), "wb") as f:
        f.write(PLAINTEXT)
    run_gce("seal", "--rs", RS, "--gen", str(GEN), "--keyexpr", DATA_KEYEXPR,
            "--in", j("pt.txt"), "--member", "member=" + j("member.crt.pem"),
            "--sign-key", j("keyservice.key.pem"), "--signer", "key-service",
            "--out", j("sealed.json"))
    with open(j("sealed.json")) as source_file:
        sealed = json.load(source_file)
    assert sealed["bundle_topic"] == BUNDLE_KEYEXPR, sealed["bundle_topic"]
    env = sealed["envelope"]                    # {suite, rs, gen, nonce(b64), ct(b64)}
    bundle_bytes = json.dumps(sealed["bundle"], separators=(",", ":")).encode()
    ciphertext = base64.b64decode(env["ct"])    # the payload bytes that ride the wire
    nonce_b64 = env["nonce"]
    suite = env["suite"]                        # crypto-agility id — must ride along

    # --- live Zenoh transport --------------------------------------------------
    received = {}
    done = threading.Event()

    def on_sample(sample):
        key = str(sample.key_expr)
        payload = bytes(sample.payload)
        att = bytes(sample.attachment) if sample.attachment is not None else None
        received[key] = (payload, att)
        if BUNDLE_KEYEXPR in received and DATA_KEYEXPR in received:
            done.set()

    sub = zenoh.open(zenoh.Config.from_json5(json.dumps({
        "mode": "peer",
        "listen": {"endpoints": [ENDPOINT]},
        "scouting": {"multicast": {"enabled": False}},
    })))
    sub.declare_subscriber("keys/**", on_sample)
    sub.declare_subscriber("data/**", on_sample)
    time.sleep(0.5)

    pub = zenoh.open(zenoh.Config.from_json5(json.dumps({
        "mode": "peer",
        "connect": {"endpoints": [ENDPOINT]},
        "scouting": {"multicast": {"enabled": False}},
    })))
    time.sleep(1.0)  # let the two peers link before publishing

    # Bundle: in-band JSON (the Go-via-rest-pub-bridge carrier — body only).
    pub.put(BUNDLE_KEYEXPR, bundle_bytes)
    # Data payload: ciphertext as the body, {rs,gen,nonce} as the Zenoh ATTACHMENT
    # (payload format untouched — the native-producer carrier, §7 lean).
    attach = json.dumps({"suite": suite, "rs": RS, "gen": GEN, "nonce": nonce_b64}).encode()
    pub.put(DATA_KEYEXPR, ciphertext, attachment=attach)

    ok = done.wait(timeout=10)
    pub.close()
    sub.close()
    if not ok:
        raise SystemExit(f"FAIL: transport timeout; received only {list(received)}")

    # --- transport integrity ---------------------------------------------------
    got_bundle, _ = received[BUNDLE_KEYEXPR]
    got_ct, got_att = received[DATA_KEYEXPR]
    if got_bundle != bundle_bytes:
        raise SystemExit("FAIL: bundle bytes mutated in transit")
    if got_ct != ciphertext:
        raise SystemExit("FAIL: ciphertext mutated in transit")
    if got_att is None:
        raise SystemExit("FAIL: attachment carrier dropped by transport")
    meta = json.loads(got_att)

    # --- reassemble the envelope from body + attachment (the attachment model) --
    recv_env = {"suite": meta["suite"], "rs": meta["rs"], "gen": meta["gen"],
                "nonce": meta["nonce"], "ct": base64.b64encode(got_ct).decode()}
    with open(j("recv-env.json"), "w") as source_file:
        json.dump(recv_env, source_file)
    with open(j("recv-bundle.json"), "wb") as f:
        f.write(got_bundle)

    # --- consumer side (Go crypto), on the RECEIVED artifacts ------------------
    # Member verifies the bundle signature against the key-service cert, then opens.
    rc, out = run_gce("open", "--bundle", j("recv-bundle.json"),
                      "--envelope", j("recv-env.json"), "--id", "member",
                      "--key", j("member.key.pem"), "--keyexpr", DATA_KEYEXPR,
                      "--verify-cert", j("keyservice.crt.pem"),
                      check=False)
    if rc != 0:
        raise SystemExit(f"FAIL: member could not verify+open received payload (exit {rc})")
    if out != PLAINTEXT:
        raise SystemExit(f"FAIL: member recovered {out!r}, want {PLAINTEXT!r}")

    # Outsider (no slot in the bundle) is denied even with a valid signature.
    rc, _ = run_gce("open", "--bundle", j("recv-bundle.json"),
                    "--envelope", j("recv-env.json"), "--id", "outsider",
                    "--key", j("outsider.key.pem"), "--keyexpr", DATA_KEYEXPR,
                    "--verify-cert", j("keyservice.crt.pem"),
                    check=False)
    if rc != 3:
        raise SystemExit(f"FAIL: outsider not denied as expected (exit {rc}, want 3)")

    # Authenticity: the member rejects a bundle whose signer is NOT the trusted
    # key-service (here, verifying against the wrong cert) — the forged-bundle case.
    rc, _ = run_gce("open", "--bundle", j("recv-bundle.json"),
                    "--envelope", j("recv-env.json"), "--id", "member",
                    "--key", j("member.key.pem"), "--keyexpr", DATA_KEYEXPR,
                    "--verify-cert", j("outsider.crt.pem"),
                    check=False)
    if rc != 3:
        raise SystemExit(f"FAIL: wrong-signer bundle not denied as expected (exit {rc}, want 3)")

    print("PASS: signed bundle + envelope carried over live Zenoh; member "
          f"verified the signature and decrypted {len(PLAINTEXT)}B on the received "
          "artifacts; outsider denied and a wrong-signer bundle denied (exit 3); "
          "attachment + in-band carriers both survived.")


if __name__ == "__main__":
    main()
