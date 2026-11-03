#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Print the CA-ID and fingerprint of each enrollment CA root PEM.

goat-cli compiles in no goatnet CA root — an operator adds the root for
whichever goatnet they are joining with `goat ca add`, and that command prints a
fingerprint to check against one obtained out of band. This is what produces the
value to check against: the list an operator publishes, and the rows on the
get.<site>/ca-roots/ index page.

The fingerprint is SHA-256 over the SEC1 uncompressed public point, matching
`goat ca ls` / `goat ca add` byte for byte. Over the KEY, not the file, so a PEM
that gains or loses an RFC-1421 header still fingerprints the same — two people
comparing values are comparing the key.

    ops/enrollment/ca-root-fingerprint.py ops/enrollment/public-keys/*.pem
    ops/enrollment/ca-root-fingerprint.py --json ops/enrollment/public-keys/*.pem

No third-party imports: this runs on mgmt hosts and operator laptops alike, and
a trust-verification tool should not need a package installed to answer.
"""

import argparse
import base64
import hashlib
import json
import re
import sys

# A P-256 SubjectPublicKeyInfo is a fixed 91-byte DER whose final 65 bytes are
# the uncompressed point (0x04 || X || Y). Both facts are asserted below rather
# than assumed — a key of another curve must be refused loudly, not fingerprinted
# from the wrong bytes and silently compared against something else.
P256_SPKI_LEN = 91
SEC1_UNCOMPRESSED_LEN = 65

PEM_BODY = re.compile(
    r"-----BEGIN PUBLIC KEY-----(.*?)-----END PUBLIC KEY-----", re.S
)
CA_ID_HEADER = re.compile(r"^CA-ID:\s*(\S+)\s*$", re.M)
# RFC 1421 encapsulated headers (CA-ID:, Created-At:, Note:) sit inside the
# BEGIN/END block and are not base64 — strip them before decoding.
HEADER_LINE = re.compile(r"^[A-Za-z][A-Za-z0-9-]*:.*$", re.M)


class BadRoot(Exception):
    pass


def parse(path):
    """Return (ca_id, fingerprint) for one CA root PEM."""
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as source_file:
            text = source_file.read()
    except OSError as e:
        raise BadRoot(f"cannot read: {e}")

    if "PRIVATE KEY" in text:
        raise BadRoot("contains a PRIVATE KEY — a CA root is the public half")

    m = PEM_BODY.search(text)
    if not m:
        raise BadRoot("no `-----BEGIN PUBLIC KEY-----` block")

    b64 = HEADER_LINE.sub("", m.group(1))
    try:
        der = base64.b64decode("".join(b64.split()), validate=True)
    except Exception as e:
        raise BadRoot(f"base64 decode failed: {e}")

    if len(der) != P256_SPKI_LEN:
        raise BadRoot(
            f"not a P-256 public key ({len(der)}-byte SPKI, expected {P256_SPKI_LEN}); "
            "fingerprinting another curve here would produce a value that does not "
            "match `goat ca add`"
        )
    point = der[-SEC1_UNCOMPRESSED_LEN:]
    if point[0] != 0x04:
        raise BadRoot("public point is not SEC1-uncompressed (no 0x04 prefix)")

    # The issuer writes CA-ID:; `goat ca add` prefers it over the filename, so
    # renaming a file cannot rename a CA. Same precedence here.
    hdr = CA_ID_HEADER.search(text)
    if hdr:
        ca_id = hdr.group(1)
    else:
        stem = path.rsplit("/", 1)[-1]
        ca_id = stem[:-4] if stem.endswith(".pem") else stem

    return ca_id, "SHA256:" + hashlib.sha256(point).hexdigest()


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("paths", nargs="+", metavar="ROOT.pem")
    ap.add_argument("--json", action="store_true", help="emit JSON")
    args = ap.parse_args(argv)

    rows, bad = [], []
    for p in args.paths:
        try:
            ca_id, fp = parse(p)
            rows.append({"path": p, "ca_id": ca_id, "fingerprint": fp})
        except BadRoot as e:
            bad.append({"path": p, "error": str(e)})

    if args.json:
        print(json.dumps({"roots": rows, "errors": bad}, indent=2))
    else:
        width = max((len(r["ca_id"]) for r in rows), default=0)
        for r in rows:
            print(f"{r['ca_id']:<{width}}  {r['fingerprint']}  {r['path']}")
        for b in bad:
            print(f"{b['path']}: {b['error']}", file=sys.stderr)

    # A file that cannot be fingerprinted is a file that will not be trusted;
    # say so with the exit code rather than leaving a caller to scan stderr.
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
