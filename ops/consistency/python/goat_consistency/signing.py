# SPDX-License-Identifier: Apache-2.0
#
# signing.py — verify-at-read for the IntentV1 detached signature (CF-5).
#
# The scheme is ADR 1082's, reused verbatim: <intent.json>.sig is
# base64(ASN.1 DER ECDSA-Sig-Value, P-256) over SHA-256 of the EXACT file
# bytes, minted by ops/enrollment/cmd/intent-sign with the per-goatnet offline
# CA. The verification key is NEVER taken on the artifact's word alone: the
# artifact's ca_id must resolve to a committed SPKI at
# ops/enrollment/public-keys/<ca_id>.pem — the same trust root the name
# snapshots and enrollment bundles already use, and the same rule
# ops/ci/check-name-snapshot-sig.sh enforces for them.
#
# Verification shells out to openssl, exactly as that CI gate does, rather
# than growing a Python crypto dependency: every context this package runs in
# (CI runners, the ansible control node, an operator laptop) carries openssl,
# and verification works from the committed tree alone with no network —
# the island-mode requirement (ADR 0206).
#
# FAIL-CLOSED (ADR 1109 P14): every outcome that is not `verified` — a
# missing signature, an unknown CA, an invalid signature, or a context that
# genuinely cannot run openssl — is a REFUSAL. A consumer must not grade,
# render or stamp an intent body from any of those states; the honest output
# is the refusal and its reason, never the body. `unavailable` exists as a
# distinct state so that "this machine cannot verify" is said out loud
# instead of being reported as tampering — but it refuses all the same.
#
# Canon: docs/design/consistency-foundation.md §3.4 (ADR 1110 D1).

import base64
import collections
import json
import os
import shutil
import subprocess
import tempfile

from .paths import PUBKEY_DIR, rel

SIGNER_HINT = ("re-sign with ops/enrollment/cmd/intent-sign "
               "(cd ops/enrollment && go run ./cmd/intent-sign "
               "--intent <path> --root-key <ca.key.age> --age-identity <keys.txt>) "
               "and commit intent.json and intent.json.sig together")

# state: "verified" | "failed" | "unavailable". Anything but "verified"
# REFUSES; detail says why in one sentence a consumer can print verbatim.
Verification = collections.namedtuple("Verification", ["state", "detail"])


class IntentVerifyError(Exception):
    """An intent artifact could not be verified. Raised by verify-at-read
    consumers; the message is the honest refusal, fit to print."""


def clean_spki_pem(path):
    """The base64 body of the PUBLIC KEY block, rebuilt as a header-free PEM.

    public-keys PEMs carry RFC-1421 headers (CA-ID:, Created-At:, Note:)
    inside the block; openssl refuses headers on PUBLIC KEY blocks, so the
    clean PEM is rebuilt from the base64 body before verifying — the same
    dance check-name-snapshot-sig.sh does."""
    body, inside = [], False
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line == "-----BEGIN PUBLIC KEY-----":
                inside = True
            elif line == "-----END PUBLIC KEY-----":
                break
            elif inside and line and ":" not in line:
                body.append(line)
    if not body:
        return None
    return "-----BEGIN PUBLIC KEY-----\n%s\n-----END PUBLIC KEY-----\n" % "\n".join(body)


def verify_file(path, doc=None, pubkey_dir=None, raw=None):
    """Verify the detached signature over the EXACT bytes at `path`.

    Returns a Verification. `doc` is the already-parsed document when the
    caller has one (only its ca_id is read — the key is still resolved from
    the committed SPKI dir, never from the artifact); `raw` is the file's
    bytes when the caller already read them, so the bytes verified are the
    bytes used; `pubkey_dir` overrides ops/enrollment/public-keys for tests."""
    pubkey_dir = pubkey_dir or PUBKEY_DIR
    if raw is None:
        try:
            with open(path, "rb") as f:
                raw = f.read()
        except OSError as e:
            return Verification("failed", "cannot read %s: %s" % (path, e))

    sig_path = path + ".sig"
    if not os.path.exists(sig_path):
        return Verification(
            "failed",
            "%s has no detached signature at %s — an unsigned intent is refused, "
            "never graded (%s)" % (rel(path), rel(sig_path), SIGNER_HINT))

    if doc is None:
        try:
            doc = json.loads(raw)
        except ValueError as e:
            return Verification("failed", "%s is not JSON: %s" % (rel(path), e))
    ca_id = doc.get("ca_id") if isinstance(doc, dict) else None
    if not ca_id or not isinstance(ca_id, str) or "/" in ca_id or os.sep in ca_id:
        return Verification(
            "failed",
            "%s declares no usable ca_id — the verification key is resolved from "
            "the committed SPKI set by CA id, so an artifact naming none (or a "
            "path-shaped one) cannot be verified" % rel(path))

    pubkey_path = os.path.join(pubkey_dir, ca_id + ".pem")
    if not os.path.exists(pubkey_path):
        return Verification(
            "failed",
            "%s declares ca_id=%s but %s is not committed — the key is never "
            "taken on the artifact's word alone" % (rel(path), ca_id, rel(pubkey_path)))
    pem = clean_spki_pem(pubkey_path)
    if not pem:
        return Verification("failed", "%s carries no PUBLIC KEY block" % rel(pubkey_path))

    openssl = shutil.which("openssl")
    if not openssl:
        return Verification(
            "unavailable",
            "cannot verify %s: openssl is not on PATH in this context. Refusing "
            "to treat an unverifiable intent as verified (fail closed)" % rel(path))

    try:
        with open(sig_path) as f:
            sig_der = base64.b64decode(f.read().strip(), validate=True)
    except (OSError, ValueError) as e:
        return Verification("failed", "%s is not base64: %s" % (rel(sig_path), e))

    tmpdir = tempfile.mkdtemp(prefix="intent-verify-")
    try:
        tmp_pub = os.path.join(tmpdir, "spki.pem")
        tmp_sig = os.path.join(tmpdir, "sig.der")
        tmp_body = os.path.join(tmpdir, "body")
        with open(tmp_pub, "w") as f:
            f.write(pem)
        with open(tmp_sig, "wb") as f:
            f.write(sig_der)
        # The body is verified from a private copy of the bytes ALREADY READ,
        # not re-read from `path`: verify-then-reread would let the file change
        # between the check and the use.
        with open(tmp_body, "wb") as f:
            f.write(raw)
        try:
            proc = subprocess.run(
                [openssl, "dgst", "-sha256", "-verify", tmp_pub, "-signature", tmp_sig,
                 tmp_body],
                capture_output=True, timeout=30, check=False)
        except (OSError, subprocess.SubprocessError) as e:
            return Verification(
                "unavailable",
                "cannot verify %s: openssl did not run (%s). Refusing to treat an "
                "unverifiable intent as verified (fail closed)" % (rel(path), e))
        if proc.returncode != 0:
            return Verification(
                "failed",
                "%s FAILED signature verification against %s — the file changed "
                "after signing, or the signature is not this artifact's. Refusing "
                "to grade against an intent nobody signed (%s)"
                % (rel(path), rel(pubkey_path), SIGNER_HINT))
    finally:
        shutil.rmtree(tmpdir, ignore_errors=True)

    return Verification("verified",
                        "signature valid (ca %s, %s)" % (ca_id, rel(pubkey_path)))
