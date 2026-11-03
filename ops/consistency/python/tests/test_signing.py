#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# test_signing.py — CF-5 verify-at-read, pinned red-then-reverted.
#
# The §9 CF-5 gate in test form: an intent whose signature is missing, whose
# body was tampered with after signing, or whose declared CA is not committed
# REFUSES — it is never rendered or graded — and the same artifact verifies
# again the moment the tamper is reverted. The scheme under test is ADR 1082's,
# reused verbatim: base64(ASN.1 DER ECDSA P-256) over SHA-256 of the exact
# file bytes, key resolved from the committed SPKI at
# ops/enrollment/public-keys/<ca_id>.pem and NEVER from the artifact.
#
# Two layers are pinned:
#   1. the COMMITTED artifacts: both governed goatnets' intent.json + .sig
#      verify against the committed public keys — the cross-stack proof that
#      what the Go signer minted, the Python verifier accepts;
#   2. an EPHEMERAL CA (openssl-minted P-256 key, discarded at exit): sign a
#      scratch artifact, prove verified; tamper the body -> refuse; strip the
#      sig -> refuse; point at an uncommitted ca_id -> refuse; revert -> green.
#
# Plain python3 + asserts, no pytest — the ops/operators/test_*.py convention.
# Exit codes: 0 all pass, 1 one or more failed.

import base64
import json
import os
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CONSISTENCY = os.path.normpath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(CONSISTENCY, "python"))

from goat_consistency import intent as intent_mod, signing  # noqa: E402
from goat_consistency.paths import INTENT_DIR, intent_path  # noqa: E402
from goat_consistency.signing import IntentVerifyError  # noqa: E402

FAILURES = []


def check(what, actual, expected):
    if actual != expected:
        FAILURES.append("%s\n    expected: %r\n    actual:   %r" % (what, expected, actual))


def ok(what, condition):
    if not condition:
        FAILURES.append(what)


def governed_goatnets():
    return sorted(d for d in os.listdir(INTENT_DIR)
                  if os.path.isdir(os.path.join(INTENT_DIR, d)))


# ---------------------------------------------------------------------------
# Layer 1 — the committed artifacts verify (Go signer -> Python verifier)
# ---------------------------------------------------------------------------

def test_every_committed_artifact_verifies():
    if not os.path.isdir(INTENT_DIR):
        print("skip: no committed intent artifacts in this tree")
        return
    nets = governed_goatnets()
    ok("at least the two governed goatnets are committed", len(nets) >= 2)
    for g in nets:
        v = signing.verify_file(intent_path(g))
        check("committed %s intent verifies" % g, v.state, "verified")


def test_load_returns_only_a_verified_body():
    if not os.path.isdir(INTENT_DIR):
        print("skip: no committed intent artifacts in this tree")
        return
    for g in governed_goatnets():
        doc = intent_mod.load(g)
        check("load(%r) returns the goatnet's own document" % g, doc.get("goatnet"), g)


def test_load_refuses_an_absent_goatnet():
    try:
        intent_mod.load("not-a-goatnet")
    except IntentVerifyError:
        return
    FAILURES.append("load() invented an intent for an ungoverned goatnet")


# ---------------------------------------------------------------------------
# Layer 2 — red-then-reverted with an ephemeral CA
# ---------------------------------------------------------------------------

def _mint_scratch(tmp):
    """A scratch intent + sig + pubkey dir, signed by a throwaway P-256 key
    with the exact production encoding (DER sig, base64+newline, headered SPKI
    PEM like the committed public-keys files)."""
    ca_id = "test-scratch-ca"
    key = os.path.join(tmp, "key.pem")
    subprocess.run(["openssl", "ecparam", "-name", "prime256v1", "-genkey",
                    "-noout", "-out", key], check=True, capture_output=True)
    pub = subprocess.run(["openssl", "ec", "-in", key, "-pubout"],
                         check=True, capture_output=True).stdout.decode()

    pubdir = os.path.join(tmp, "public-keys")
    os.makedirs(pubdir)
    # Re-emit with RFC-1421 headers inside the block, the committed format the
    # cleaner must cope with.
    body = "".join(line for line in pub.splitlines(keepends=True)
                   if "-----" not in line)
    with open(os.path.join(pubdir, ca_id + ".pem"), "w") as f:
        f.write("-----BEGIN PUBLIC KEY-----\nCA-ID: %s\nNote: test key\n\n%s"
                "-----END PUBLIC KEY-----\n" % (ca_id, body))

    doc = {"format": "goat-intent-v1", "goatnet": "scratch", "ca_id": ca_id,
           "serial": 1}
    path = os.path.join(tmp, "intent.json")
    with open(path, "w") as f:
        f.write(json.dumps(doc, sort_keys=True, indent=2) + "\n")
    der = subprocess.run(["openssl", "dgst", "-sha256", "-sign", key, path],
                         check=True, capture_output=True).stdout
    with open(path + ".sig", "w") as f:
        f.write(base64.b64encode(der).decode() + "\n")
    return path, pubdir


def test_red_then_reverted():
    tmp = tempfile.mkdtemp(prefix="cf5-signing-")
    try:
        path, pubdir = _mint_scratch(tmp)

        v = signing.verify_file(path, pubkey_dir=pubdir)
        check("freshly signed artifact verifies", v.state, "verified")

        # RED 1 — tamper the body after signing.
        with open(path) as source_file:
            original = source_file.read()
        with open(path, "w") as f:
            f.write(original.replace('"serial": 1', '"serial": 2'))
        v = signing.verify_file(path, pubkey_dir=pubdir)
        check("tampered body refuses", v.state, "failed")
        ok("the refusal says the file changed after signing",
           "changed after signing" in v.detail)

        # REVERT — the same artifact goes green again.
        with open(path, "w") as f:
            f.write(original)
        v = signing.verify_file(path, pubkey_dir=pubdir)
        check("reverted body verifies again", v.state, "verified")

        # RED 2 — strip the signature entirely.
        os.rename(path + ".sig", path + ".sig.away")
        v = signing.verify_file(path, pubkey_dir=pubdir)
        check("missing signature refuses", v.state, "failed")
        ok("the refusal names the missing .sig", "no detached signature" in v.detail)
        os.rename(path + ".sig.away", path + ".sig")
        check("restored signature verifies again",
              signing.verify_file(path, pubkey_dir=pubdir).state, "verified")

        # RED 3 — the declared CA is not committed. The key is never taken on
        # the artifact's word alone, so an unknown ca_id cannot verify.
        empty = os.path.join(tmp, "no-keys")
        os.makedirs(empty)
        v = signing.verify_file(path, pubkey_dir=empty)
        check("unknown ca_id refuses", v.state, "failed")
        ok("the refusal names the uncommitted key", "not committed" in v.detail)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def test_garbage_sig_refuses():
    tmp = tempfile.mkdtemp(prefix="cf5-signing-")
    try:
        path, pubdir = _mint_scratch(tmp)
        with open(path + ".sig", "w") as f:
            f.write("not base64 at all\n")
        check("garbage signature refuses",
              signing.verify_file(path, pubkey_dir=pubdir).state, "failed")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def main():
    tests = [(n, f) for n, f in sorted(globals().items())
             if n.startswith("test_") and callable(f)]
    for name, fn in tests:
        fn()
    if FAILURES:
        print("FAIL (%d):" % len(FAILURES))
        for f in FAILURES:
            print("  - %s" % f)
        return 1
    print("ok: %d signing test(s) — an unverified intent REFUSES, and the "
          "refusal reverts clean" % len(tests))
    return 0


if __name__ == "__main__":
    sys.exit(main())
