#!/usr/bin/env bash
# Tier 2 assertions — require a running fabric (docker compose up).
# Five cases. The four denials are the point; the acceptance proves we did not
# simply break everything.
#
# HOW THIS ASSERTS, AND WHY IT CHANGED
#
# The previous version published with `docker run $ZENOH_IMAGE zenoh-put`. That
# binary does not exist: the eclipse/zenoh image ships ONLY /zenohd and two
# plugins. So every publish failed identically, and because each negative case
# asserted with grep -E 'error|refused|denied', "command not found" matched just
# as well as a real refusal. Every negative case passed unconditionally,
# including case 2 — the one that proves ADR 1157 decision 3. A harness that
# cannot fail is worse than no harness.
#
# Now sovereign B publishes through a REAL zenoh session: zenohd in CLIENT mode
# carrying B's certificate, driven over its loopback REST plugin. Two consequences
# shape every assertion below.
#
#   1. A REFUSED IDENTITY fails at the TLS handshake, so the publisher never
#      establishes a session. That is asserted POSITIVELY on the publisher's own
#      log, not on an empty result.
#   2. An ADMITTED identity writing outside its lane is denied at the ROUTER, and
#      per F-177 that denial is SILENT to the sender — REST still answers 200.
#      So it is asserted on the ROUTER log, which requires the ACL interceptor at
#      trace (the compose file sets it). Absence of data is never taken as proof.
set -uo pipefail
cd "$(dirname "$0")" || exit 1

Z=${ZENOH_IMAGE:?set ZENOH_IMAGE to a digest-pinned zenoh image (see README)}
NET=${FED_NET:-fedsmoke_fed}
PROJ=${COMPOSE_PROJECT_NAME:-fedsmoke}

pass=0; fail=0
ok(){ printf "  \033[32mPASS\033[0m  %s\n" "$1"; pass=$((pass+1)); }
no(){ printf "  \033[31mFAIL\033[0m  %s\n" "$1"; fail=$((fail+1)); }

PUBC="fedsmoke-pub-b"

# Start B's publisher with a chosen identity. Returns 0 if a SESSION was
# established, 1 if the handshake was refused. Leaves the container running so
# the caller can publish through it and read its log.
start_pub () { # $1 = cert base
  docker rm -f "$PUBC" >/dev/null 2>&1 || true
  docker run -d --name "$PUBC" --network "$NET" \
    -v "$PWD/conf:/conf:ro" -v "$PWD/pki:/pki:ro" \
    --entrypoint /zenohd "$Z" -c /conf/pub-b.json5 \
    --cfg "transport/link/tls/connect_certificate:\"/pki/$1.pem\"" \
    --cfg "transport/link/tls/connect_private_key:\"/pki/$1-key.pem\"" \
    >/dev/null 2>&1 || return 1
  # A zenohd in client mode that cannot connect EXITS ("Unable to connect to any
  # of [...]! Exiting..."), so container liveness is the signal. Grepping the
  # publisher log for connection words is unreliable: a SUCCESSFUL connect logs
  # nothing at info level, so absence of a success line looked identical to a
  # refusal and made the control fail while the session was actually fine.
  for _ in $(seq 1 16); do
    sleep 0.5
    st=$(docker inspect "$PUBC" --format '{{.State.Status}}' 2>/dev/null || echo gone)
    [ "$st" = "exited" ] && return 1
  done
  [ "$(docker inspect "$PUBC" --format '{{.State.Running}}' 2>/dev/null)" = "true" ] || return 1
  # Corroborate on the EDGE: a real session shows the peer's CN as an ACL
  # subject. This is the positive evidence that the handshake was accepted,
  # rather than an inference from the publisher merely still being alive.
  docker logs --since 60s "${PROJ}-edge-a-1" 2>&1 | grep -q "CertCommonName(" || return 1
  return 0
}

# Publish through the running publisher's loopback REST plugin. busybox has no
# curl; nc plus a hand-written request is dependency-free and enough for a PUT.
pub_via_rest () { # $1 = key expr, $2 = payload
  docker exec "$PUBC" /bin/busybox sh -c \
    "printf 'PUT /$1 HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Length: ${#2}\r\nConnection: close\r\n\r\n$2' \
     | /bin/busybox nc 127.0.0.1 8000" 2>&1
}

stop_pub () { docker rm -f "$PUBC" >/dev/null 2>&1 || true; }

# Read the ROUTER's own verdict. Requires the ACL interceptor at trace, which the
# compose file enables. Without it a denial leaves no trace at all and this would
# silently answer "no" — so the caller checks the trace is on first.
denied_in_router_log () { # $1 = container, $2 = key fragment
  docker logs --since 60s "$1" 2>&1 | grep -F 'is unauthorized to' | grep -qF "$2"
}

trace_is_on () { # $1 = container
  docker inspect "$1" --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null \
    | grep -q 'access_control=trace'
}

echo
if ! trace_is_on "${PROJ}-core-a-1" || ! trace_is_on "${PROJ}-edge-a-1"; then
  echo "  REFUSING TO RUN: the routers are not logging ACL decisions."
  echo "  Without RUST_LOG raising the ACL interceptor to trace, a denial leaves"
  echo "  no trace, and every negative case below would 'pass' on silence — which"
  echo "  is exactly the defect F-448 records. Bring the rig up with the compose"
  echo "  file in this directory."
  exit 2
fi

echo "1. The agreed peering identity is ADMITTED and may land data."
if start_pub b-peering; then
  out=$(pub_via_rest "from/svn/smoke/1" "smoke")
  if grep -qE 'HTTP/1\.[01] (200|204)' <<<"$out"; then
    if denied_in_router_log "${PROJ}-edge-a-1" "from/svn/smoke/1"; then
      no "session formed and REST accepted, but the EDGE denied the write"
    else
      ok "B's agreed identity established a session and wrote from/svn/**"
    fi
  else
    no "session formed but REST did not accept the put: $(head -1 <<<"$out")"
  fi
else
  no "B's agreed identity could not establish a session (this is the CONTROL — if it fails, the denials below prove nothing)"
fi
stop_pub

echo
# Cases 2 and 3 are NOT refused at the handshake. Every certificate B issues is
# signed by B's CA, the edge trusts B's CA, and TLS cannot see a Common Name policy
# -- so a forged CN establishes a TCP connection exactly like the agreed one.
# Asserting on the handshake would test the wrong layer and pass for the wrong
# reason.
#
# The real control is that the edge's ACL holds ONE subject (the agreed CN) and
# defaults to deny, so a cert the ACL does not name is admitted to the transport
# and authorized for NOTHING. THE EMPTINESS OF THE EDGE IS THE CONTROL, which is
# why check-edge-profile-isolation.sh exists and why an edge must never inherit a
# core profile. Assert on that: the forged subject must receive ZERO allows.
allows_for () { # $1 = CN
  docker logs "${PROJ}-edge-a-1" 2>&1 | grep -F 'is authorized to' \
    | grep -cF "CertCommonName($1)"
}

echo "2. THE F-424 CASE — B mints OUR router's CN. Must not gain our router's reach."
start_pub b-impersonate >/dev/null 2>&1 || true
pub_via_rest "from/svn/hijack" "smoke" >/dev/null 2>&1 || true
sleep 2
n=$(allows_for "goat-zenoh-router")
if [ "$n" -eq 0 ]; then
  ok "B's forged CN=goat-zenoh-router received ZERO authorizations on the edge"
else
  no "B's forged CN=goat-zenoh-router was AUTHORIZED $n time(s) — the edge names a subject it must not"
fi
stop_pub

echo
echo "3. A CN we never agreed to matches no subject, so default-deny applies."
start_pub b-unagreed >/dev/null 2>&1 || true
pub_via_rest "from/svn/unagreed" "smoke" >/dev/null 2>&1 || true
sleep 2
n=$(allows_for "peer-svn-unagreed")
if [ "$n" -eq 0 ]; then
  ok "unagreed CN received ZERO authorizations"
else
  no "unagreed CN was AUTHORIZED $n time(s)"
fi
stop_pub

echo
echo "4. An ADMITTED partner cannot write outside its landing area."
# Asserted on the ROUTER's own verdict over the WHOLE log, not --since: the ACL
# caches, so a decision can be logged once and never repeated. And with a
# positive control in the same breath -- a denial only means something if the
# same identity is demonstrably allowed somewhere.
start_pub b-peering >/dev/null 2>&1 || true
pub_via_rest "from/svn/ok"        "smoke" >/dev/null 2>&1 || true
pub_via_rest "ops/fabric/hijack"  "smoke" >/dev/null 2>&1 || true
sleep 3
verdict () { # $1 = key -> prints "authorized" | "unauthorized" | ""
  docker logs "${PROJ}-edge-a-1" 2>&1 \
    | grep -E "is (un)?authorized to Put \\(ingress\\) on $1\$" \
    | grep -oE "is (un)?authorized" | tail -1 | sed 's/^is //'
}
in_lane=$(verdict "from/svn/ok")
out_lane=$(verdict "ops/fabric/hijack")
if [ "$in_lane" = "authorized" ] && [ "$out_lane" = "unauthorized" ]; then
  ok "in-lane write ALLOWED and out-of-lane write DENIED by the edge (both observed)"
elif [ -z "$in_lane" ]; then
  no "no verdict recorded for the in-lane control — cannot conclude anything about the denial"
else
  no "in-lane=$in_lane out-of-lane=$out_lane (expected authorized / unauthorized)"
fi
stop_pub

echo "5. The edge holds no read of the core's fabric."
# The edge dials inward presenting OUR identity, so the question is what the CORE
# grants it. Counted rather than eyeballed: any read authorization at all is a
# failure, because an edge that can read the core is a partner-facing process
# with a view of our fabric.
core_reads=$(docker logs "${PROJ}-core-a-1" 2>&1 \
  | grep -cE "is authorized to (Declare Subscriber|Query|Declare Queryable|Declare/Undeclare Subscriber)")
edge_seen=$(docker logs "${PROJ}-core-a-1" 2>&1 | grep -c "CertCommonName(edge-a)")
if [ "$edge_seen" -eq 0 ]; then
  no "the core never saw the edge as a subject — cannot conclude it was denied"
elif [ "$core_reads" -eq 0 ]; then
  ok "the core granted the edge ZERO read authorizations ($edge_seen decisions seen)"
else
  no "the core granted the edge $core_reads read authorization(s)"
fi

echo
printf "  %d passed, %d failed\n" "$pass" "$fail"
[ "$fail" -eq 0 ] || exit 1
