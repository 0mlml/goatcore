#!/usr/bin/env bash
# Tier 1b — the mTLS HANDSHAKE, with no container runtime and no fabric.
#
# assert-pki.sh proves chain validation with `openssl verify`. This goes one
# step closer to the real thing: it stands up an actual TLS server with a
# client-certificate requirement, exactly as a router's listener does, and
# asserts which peers get through the handshake.
#
# That matters because the handshake is where the design's isolation is
# enforced. If a foreign certificate is refused HERE, it never reaches
# authorization at all, and the ACL's inability to see the issuer stops being
# reachable.
set -uo pipefail
cd "$(dirname "$0")" || exit 1
P=pki; pass=0; fail=0
ok(){ printf "  \033[32mPASS\033[0m  %s\n" "$1"; pass=$((pass+1)); }
no(){ printf "  \033[31mFAIL\033[0m  %s\n" "$1"; fail=$((fail+1)); }

port=$(( ( RANDOM % 20000 ) + 30000 ))

# $1 = the CA the listener accepts clients from
# $2 = the client cert basename
# Returns 0 only if the session actually CARRIED DATA end to end.
#
# Two harness traps this avoids, both found the hard way:
#
#   1. Do NOT key on s_client's exit code. In TLS 1.3 the server's rejection of
#      a client certificate reaches the client asynchronously, so s_client can
#      exit 0 on a session the server already refused. We assert on DATA FLOW —
#      the server emits a token and the client either reads it or does not.
#
#   2. `s_server -Verify N` alone LOGS a client-certificate failure and then
#      CONTINUES: its verify callback returns 1. Without -verify_return_error a
#      wrong-CA client sails through and the test silently proves nothing.
#      This is a property of the openssl test tool, not of TLS and not of the
#      fabric — a real listener uses a strict verifier.
handshake () {
  local ca="$1" client="$2" p=$((port++)) tok="FEDERATION-OK-$RANDOM"
  ( printf '%s\n' "$tok"; sleep 2 ) | openssl s_server -quiet -accept "$p" -naccept 1 \
      -cert "$P/a-router.pem" -key "$P/a-router-key.pem" \
      -CAfile "$P/$ca" -Verify 1 -verify_return_error >/dev/null 2>&1 &
  local srv=$!
  sleep 0.8
  local got
  got=$( ( sleep 1.5 ) | openssl s_client -quiet -connect "127.0.0.1:$p" \
      -cert "$P/$client.pem" -key "$P/$client-key.pem" \
      -CAfile "$P/a-ca.pem" 2>/dev/null | tr -d '\r' )
  kill "$srv" 2>/dev/null; wait "$srv" 2>/dev/null
  grep -q "$tok" <<<"$got"
}

echo
echo "A listener trusting ONLY sovereign A's CA — the core router's posture."
handshake a-ca.pem a-operator && ok "our own operator completes the handshake" \
                              || no "our own operator was refused — the harness is wrong"
handshake a-ca.pem b-peering     && no "partner leaf COMPLETED the handshake — isolation broken" \
                                 || ok "partner leaf refused at the handshake"
handshake a-ca.pem b-impersonate && no "partner's CN=goat-zenoh-router COMPLETED — isolation broken" \
                                 || ok "partner's CN=goat-zenoh-router refused at the handshake"

echo
echo "A listener trusting ONLY sovereign B's CA — the edge router's posture."
handshake b-ca.pem b-peering && ok "the agreed partner identity completes the handshake" \
                             || no "the agreed partner identity was refused"
handshake b-ca.pem a-operator && no "OUR operator completed against the partner listener" \
                              || ok "our own operator refused there — the edge is not a way in"

echo
echo "The failure mode the design exists to prevent: BOTH CAs in one store."
cat $P/a-ca.pem $P/b-ca.pem > $P/concat-ca.pem
handshake concat-ca.pem b-impersonate \
  && ok "with both roots trusted, the impersonation COMPLETES — F-424, demonstrated" \
  || no "expected the impersonation to succeed under a concatenated store"

echo
printf "  %d passed, %d failed\n\n" "$pass" "$fail"
[ $fail -eq 0 ] && cat <<'TXT'
  WHAT THIS ADDS OVER assert-pki.sh
  Not just that the certificate chain validates, but that a real TLS listener
  requiring a client certificate ACCEPTS or REFUSES it. The isolation is
  enforced at the handshake, before authorization is consulted at all — which
  is why one trust store per partner is the control, and why the ACL's
  blindness to the issuer is survivable.
TXT
if [ "$fail" -eq 0 ]; then exit 0; else exit 1; fi
