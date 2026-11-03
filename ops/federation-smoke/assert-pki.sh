#!/usr/bin/env bash
# Tier 1 assertions — provable with openssl alone, no containers, no fabric.
# These establish the PKI facts the whole design rests on.
set -uo pipefail
cd "$(dirname "$0")" || exit 1
P=pki; pass=0; fail=0
ok()  { printf "  \033[32mPASS\033[0m  %s\n" "$1"; pass=$((pass+1)); }
no()  { printf "  \033[31mFAIL\033[0m  %s\n" "$1"; fail=$((fail+1)); }
chk() { # $1 desc, $2 expect(0|1), shift 2 = command
  local d="$1" e="$2"; shift 2
  "$@" >/dev/null 2>&1; local r=$?
  { [ "$e" = 0 ] && [ $r -eq 0 ]; } || { [ "$e" = 1 ] && [ $r -ne 0 ]; } && ok "$d" || no "$d (rc=$r, wanted $([ "$e" = 0 ] && echo success || echo failure))"
}

echo
echo "A. Each sovereign's own leaves verify against its own root — the baseline."
chk "A's router verifies under A's CA"            0 openssl verify -CAfile $P/a-ca.pem $P/a-router.pem
chk "B's peering leaf verifies under B's CA"      0 openssl verify -CAfile $P/b-ca.pem $P/b-peering.pem

echo
echo "B. Separate trust stores — the design. A partner leaf is refused outright."
chk "B's peering leaf REFUSED under A's CA alone" 1 openssl verify -CAfile $P/a-ca.pem $P/b-peering.pem
chk "B's impersonation REFUSED under A's CA alone" 1 openssl verify -CAfile $P/a-ca.pem $P/b-impersonate.pem

echo
echo "C. ONE concatenated trust store — the F-424 condition. This is the finding."
cat $P/a-ca.pem $P/b-ca.pem > $P/concat-ca.pem
chk "B's impersonation is CHAIN-VALID under the concat" 0 openssl verify -CAfile $P/concat-ca.pem $P/b-impersonate.pem
imp_cn=$(openssl x509 -in $P/b-impersonate.pem -noout -subject | sed 's/.*CN *= *//')
our_cn=$(openssl x509 -in $P/a-router.pem      -noout -subject | sed 's/.*CN *= *//')
if [ "$imp_cn" = "$our_cn" ]; then
  ok "…and its CN is identical to ours: '$imp_cn'"
else
  no "CN mismatch: '$imp_cn' vs '$our_cn'"
fi
imp_iss=$(openssl x509 -in $P/b-impersonate.pem -noout -issuer | sed 's/.*CN *= *//')
our_iss=$(openssl x509 -in $P/a-router.pem      -noout -issuer | sed 's/.*CN *= *//')
if [ "$imp_iss" != "$our_iss" ]; then
  ok "…while the ISSUER differs ('$imp_iss' vs '$our_iss') — the field the ACL cannot read"
else
  no "issuers unexpectedly equal"
fi

echo
printf "  %d passed, %d failed\n\n" "$pass" "$fail"
if [ $fail -eq 0 ]; then
  cat <<'TXT'
  WHAT THIS PROVES
  Two certificates with the SAME Common Name, from DIFFERENT authorities, are
  both chain-valid once a router concatenates both roots into one trust store.
  Zenoh's ACL matches on Common Name and cannot read the issuer, so at that
  point the two are indistinguishable to authorization.
  Separate trust stores — one process per partner — refuse the foreign leaf at
  the handshake, before authorization is ever consulted. That is the design.
TXT
fi
if [ "$fail" -eq 0 ]; then exit 0; else exit 1; fi
