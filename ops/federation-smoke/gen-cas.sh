#!/usr/bin/env bash
# Mint two INDEPENDENT certificate authorities and the leaves each would issue.
# Sovereign A is "us"; sovereign B is the synthetic partner. They share nothing.
set -euo pipefail
cd "$(dirname "$0")"
OUT="pki"; rm -rf "$OUT"; mkdir -p "$OUT"

mkca () { # $1 = name
  openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
    -keyout "$OUT/$1-ca-key.pem" -out "$OUT/$1-ca.pem" -days 3650 \
    -subj "/O=Sovereign ${1^^}/OU=offline-ca/CN=Sovereign ${1^^} Root CA" 2>/dev/null
}

leaf () { # $1 = ca, $2 = CN, $3 = out basename, $4... = extra DNS SANs
  # The SAN matters as much as the CN. A router verifies the NAME it dialled
  # against the peer's SAN, so a server cert whose SAN omits the address the
  # client used is refused with BadCertificate even though the chain is valid.
  # (An edge dialling "core-a" fails if the core's cert carries only its CN.)
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
    -keyout "$OUT/$3-key.pem" -out "$OUT/$3.csr" -subj "/O=Sovereign ${1^^}/CN=$2" 2>/dev/null
  openssl x509 -req -in "$OUT/$3.csr" -CA "$OUT/$1-ca.pem" -CAkey "$OUT/$1-ca-key.pem" \
    -CAcreateserial -out "$OUT/$3.pem" -days 365 \
    -extfile <(printf 'basicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth,clientAuth\nsubjectAltName=%s\n' "$(san_list "$2" "${@:4}")") 2>/dev/null
  rm -f "$OUT/$3.csr"
}

san_list () { # build "DNS:a,DNS:b,..." from every name given
  local out="DNS:$1"; shift
  for n in "$@"; do out="$out,DNS:$n"; done
  printf '%s' "$out"
}

mkca a
mkca b

# --- Sovereign A (us) ---------------------------------------------------------
leaf a "goat-zenoh-router"        a-router  core-a   # our core; SAN must cover the name the edge dials
leaf a "operator-trial-peer"      a-operator    # a legitimate local client

# --- Sovereign B (the partner) ------------------------------------------------
leaf b "peer-svn"                 b-peering     # the AGREED peering CN
leaf b "goat-zenoh-router"        b-impersonate # THE ATTACK: B mints OUR router's CN
leaf b "peer-svn-unagreed"        b-unagreed    # a CN we never agreed to

# --- A's edge router: two identities, one per direction --------------------
leaf a "edge-a-partner-face"      a-edge  edge-a   # presented TO the partner; SAN covers the dialled name
leaf a "edge-a"                   a-edge-core   # presented INWARD to our core


echo "Two independent CAs minted:"
for c in a b; do
  printf "  sovereign %s  " "$c"
  openssl x509 -in "$OUT/$c-ca.pem" -noout -subject -fingerprint -sha256 \
    | tr '\n' ' ' | sed 's/subject=//; s/SHA256 Fingerprint=/fp=/' | cut -c1-118; echo
done
echo
echo "Note b-impersonate: sovereign B has issued a certificate bearing OUR router's"
echo "Common Name. Nothing prevents that — it is exactly the F-424 condition."
