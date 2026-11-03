#!/usr/bin/env bash
# Reproduction harness for the AAL2-step-up / OAuth2-login_challenge interaction.
#
# Drives a real Hydra-initiated login on the live goatnet with a synthetic
# identity that has BOTH a password and a TOTP second factor, and reports
# whether the Hydra login challenge is ever accepted.
#
# Two variants, run back to back, because they answer different questions:
#   A (kratos-direct)  — talks only to Kratos's public API. Isolates Kratos's
#                        own step-up behaviour from the selfservice-ui.
#   B (through the ui) — follows the browser chain via auth.<site>/login, which
#                        is what a human actually traverses.
#
# The verdict line each variant prints is the only thing that matters:
#   ACCEPTED  — Hydra reports the login request handled; the app is reached.
#   ORPHANED  — login succeeded, challenge never accepted; the browser lands on
#               the Ory welcome page instead of the app.
#
# Run ON the mgmt host: it needs mesh DNS plus the loopback-published Kratos
# (4434) and Hydra (4445) admin ports. Idempotent — the probe identity is
# deleted and recreated on each run, and deleted again at the end.
set -uo pipefail

SITE_AUTH="${SITE_AUTH:?set SITE_AUTH e.g. auth.zagros.netbird.zagros.example}"
SITE_OAUTH2="${SITE_OAUTH2:?set SITE_OAUTH2 e.g. oauth2.zagros.netbird.zagros.example}"
CLIENT_ID="${CLIENT_ID:-microscope}"
REDIRECT_URI="${REDIRECT_URI:?set REDIRECT_URI (a registered redirect for CLIENT_ID)}"
KRATOS_ADMIN="${KRATOS_ADMIN:-http://127.0.0.1:4434}"
HYDRA_ADMIN="${HYDRA_ADMIN:-http://127.0.0.1:4445}"

EMAIL="aal2-oidc-probe@invalid.test"
PASSWORD="Pr0be-Harness-Only-xQ7"
# Assigned by enrol_totp — Kratos's admin import accepts only password/oidc
# credentials ("json: unknown field \"totp\""), so the second factor has to be
# enrolled through the real settings flow like a human would.
TOTP_SECRET=""
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
IDENTITY_ID=""

say() { printf '\n=== %s\n' "$*"; }

# Hydra login challenges are ~1120 chars of base64 INCLUDING '=' padding, so a
# naive [A-Za-z0-9_-] sed truncates them and every admin lookup 404s. Parse
# query strings properly instead of pattern-matching URLs.
qparam() {  # qparam <url> <key>
  python3 - "$1" "$2" <<'PY'
import sys, urllib.parse
q = urllib.parse.parse_qs(urllib.parse.urlparse(sys.argv[1]).query)
print(q.get(sys.argv[2], [""])[0])
PY
}
chain_param() {  # chain_param <header-dump-file> <key> — first hop that carries it
  python3 - "$1" "$2" <<'PY'
import sys, urllib.parse
for line in open(sys.argv[1], errors="replace"):
    if line.lower().startswith("location:"):
        q = urllib.parse.parse_qs(urllib.parse.urlparse(line.split(":", 1)[1].strip()).query)
        if sys.argv[2] in q:
            print(q[sys.argv[2]][0]); break
else:
    print("")
PY
}
jqp() { python3 -c "import sys,json
try: d=json.load(sys.stdin)
except Exception: sys.exit(1)
$1"; }

totp_now() {
  python3 - "$TOTP_SECRET" <<'PY'
import base64, hmac, hashlib, struct, sys, time
secret = base64.b32decode(sys.argv[1], casefold=True)
mac = hmac.new(secret, struct.pack('>Q', int(time.time()) // 30), hashlib.sha1).digest()
off = mac[-1] & 0x0F
print("%06d" % ((struct.unpack('>I', mac[off:off+4])[0] & 0x7FFFFFFF) % 1000000))
PY
}

# --- probe identity ----------------------------------------------------------
setup_identity() {
  say "provisioning probe identity $EMAIL (password + TOTP)"
  local existing
  existing=$(curl -sS "$KRATOS_ADMIN/admin/identities?credentials_identifier=$(python3 -c \
      'import urllib.parse,sys;print(urllib.parse.quote(sys.argv[1]))' "$EMAIL")" \
      | jqp 'print(d[0]["id"] if d else "")')
  [ -n "$existing" ] && curl -sS -X DELETE "$KRATOS_ADMIN/admin/identities/$existing" >/dev/null

  python3 - "$EMAIL" "$PASSWORD" > "$WORK/identity.json" <<'PY'
import json, sys
email, password = sys.argv[1:3]
print(json.dumps({
    "schema_id": "default",
    "traits": {"email": email, "name": "AAL2 OIDC Probe"},
    "credentials": {"password": {"config": {"password": password}}},
}))
PY
  local resp
  resp=$(curl -sS -H 'Content-Type: application/json' -d @"$WORK/identity.json" "$KRATOS_ADMIN/admin/identities")
  IDENTITY_ID=$(printf '%s' "$resp" | jqp 'print(d.get("id",""))')
  if [ -z "$IDENTITY_ID" ]; then
    echo "  identity creation FAILED: $(printf '%s' "$resp" | head -c 400)"; return 1
  fi
  echo "  identity_id=$IDENTITY_ID"
  curl -sS "$KRATOS_ADMIN/admin/identities/$IDENTITY_ID" \
    | jqp 'print("  credentials:", sorted((d.get("credentials") or {}).keys()))'
}

teardown_identity() {
  [ -n "$IDENTITY_ID" ] || return 0
  curl -sS -X DELETE "$KRATOS_ADMIN/admin/identities/$IDENTITY_ID" >/dev/null
  say "probe identity deleted"
}

# Acceptance signal: when Kratos accepts the Hydra login request it returns a
# redirect back to /oauth2/auth carrying a `login_verifier`. The admin GET for
# the challenge keeps returning 200 after acceptance, so it is NOT a usable
# verdict — an earlier version of this harness read that as "still pending" and
# called a working flow ORPHANED.
accepted() { case "$1" in *login_verifier=*) return 0 ;; *) return 1 ;; esac; }

# --- flow helpers ------------------------------------------------------------
# Submit a login flow method. Sets LAST_LOCATION + LAST_STATUS.
post_flow() {
  local jar="$1" flowid="$2" method="$3" body csrf action
  body=$(curl -sS -b "$jar" "https://$SITE_AUTH/self-service/login/flows?id=$flowid")
  csrf=$(printf '%s' "$body" | jqp '
print(next((n["attributes"].get("value","") for n in d.get("ui",{}).get("nodes",[])
            if n.get("attributes",{}).get("name")=="csrf_token"), ""))')
  action=$(printf '%s' "$body" | jqp 'print(d.get("ui",{}).get("action",""))')
  local args=(-sS -o "$WORK/resp" -c "$jar" -b "$jar" -D "$WORK/hdr" -X POST "$action"
              -H 'Accept: application/json' --data-urlencode "csrf_token=$csrf")
  if [ "$method" = password ]; then
    args+=(--data-urlencode method=password --data-urlencode "identifier=$EMAIL" --data-urlencode "password=$PASSWORD")
  else
    args+=(--data-urlencode method=totp --data-urlencode "totp_code=$(totp_now)")
  fi
  LAST_STATUS=$(curl "${args[@]}" -w '%{http_code}')
  # With Accept: application/json Kratos returns the redirect target in the
  # body (redirect_browser_to) rather than a Location header; take either.
  LAST_LOCATION=$(sed -n 's/^[Ll]ocation: //p' "$WORK/hdr" | tr -d '\r' | tail -1)
  [ -n "$LAST_LOCATION" ] || LAST_LOCATION=$(jqp '
print(d.get("redirect_browser_to")
      or d.get("error",{}).get("details",{}).get("redirect_browser_to")
      or "(none)")' < "$WORK/resp" 2>/dev/null || echo "(none)")
}

session_aal() {
  curl -sS -b "$1" "https://$SITE_AUTH/sessions/whoami" | jqp '
print(d.get("authenticator_assurance_level") or ("REFUSED:" + d.get("error",{}).get("id","?")))' 2>/dev/null \
    || echo "(unparseable)"
}

flow_challenge() {
  curl -sS "https://$SITE_AUTH/self-service/login/flows?id=$1" | jqp '
c = d.get("oauth2_login_challenge") or d.get("oauth2_login_request",{}).get("challenge")
print((c[:16] + "…") if c else "NONE")' 2>/dev/null || echo "?"
}

# --- one variant -------------------------------------------------------------
run_variant() {
  # Declared separately: bash expands every word of a `local` command before
  # assigning any of them, so `jar="$WORK/jar.$label"` on the same line reads
  # $label while it is still unbound and trips `set -u`.
  local label="$1"
  local mode="$2"
  local jar="$WORK/jar.$label"
  rm -f "$jar"; : > "$jar"
  say "VARIANT $label ($mode)"

  # 1. Kick off authorize. Capture EVERY Location in the chain — the challenge
  #    appears on an intermediate hop and is gone by the final URL.
  local authorize="https://$SITE_OAUTH2/oauth2/auth?client_id=$CLIENT_ID&response_type=code&scope=openid&redirect_uri=$REDIRECT_URI&state=reprostate$RANDOM$RANDOM"
  curl -sS -o /dev/null -c "$jar" -b "$jar" -L --max-redirs 10 -D "$WORK/chain.$label" "$authorize" >/dev/null 2>&1
  local challenge
  challenge=$(chain_param "$WORK/chain.$label" login_challenge)
  if [ -z "$challenge" ]; then
    echo "  FAILED to obtain a login_challenge"
    grep -i '^location:' "$WORK/chain.$label" | cut -c1-120 | head -5
    return 1
  fi
  echo "  login_challenge=${challenge:0:16}…"

  # 2. Create the login flow — via Kratos directly, or via the ui-node.
  local flowurl flowid
  if [ "$mode" = direct ]; then
    flowurl="https://$SITE_AUTH/self-service/login/browser?login_challenge=$challenge"
  else
    flowurl="https://$SITE_AUTH/login?login_challenge=$challenge"
  fi
  flowid=$(qparam "$(curl -sS -o /dev/null -c "$jar" -b "$jar" -L --max-redirs 10 \
                      -w '%{url_effective}' "$flowurl")" flow)
  echo "  flow=$flowid  carries_challenge=$(flow_challenge "$flowid")"

  # 3. Password.
  post_flow "$jar" "$flowid" password
  local first_location="$LAST_LOCATION"

  # 3c. TWO-TAB variant. The operator's browser had a dozen tabs open. A second
  #     tab sitting on /welcome runs the ui-node's requireAuth middleware, which
  #     calls /sessions/whoami; with an aal1 session and whoami demanding aal2
  #     that 403s, and maybeInitiate2FA redirects THAT TAB into a brand-new
  #     ?aal=aal2 login flow built from scratch — no login_challenge, because
  #     the middleware constructs the URL itself rather than using Kratos's.
  #     Completing the second factor there satisfies the session while the OIDC
  #     tab's flow is left holding a challenge nobody will ever accept.
  if [ "$mode" = twotab ]; then
    local tabb
    tabb=$(curl -sS -o /dev/null -c "$jar" -b "$jar" -L --max-redirs 10 \
             -w '%{url_effective}' "https://$SITE_AUTH/welcome")
    echo "  tab B (/welcome) landed at: $(printf '%s' "$tabb" | cut -c1-95)"
    local bflow; bflow=$(qparam "$tabb" flow)
    if [ -n "$bflow" ]; then
      echo "  tab B was pushed into flow=$bflow  carries_challenge=$(flow_challenge "$bflow")"
      # The human finishes 2FA in whichever tab is in front of them — tab B.
      post_flow "$jar" "$bflow" totp
      echo "  tab B second factor: http=$LAST_STATUS session_aal=$(session_aal "$jar")"
      echo "                   tab B next=$(printf '%s' "$LAST_LOCATION" | cut -c1-95)"
    else
      echo "  tab B was NOT pushed into a step-up flow"
    fi
    # Now the original OIDC tab tries to continue from where Kratos left it.
    LAST_LOCATION="$first_location"
  fi

  # 3b. DOUBLE-SUBMIT variant: re-post the ORIGINAL password flow after the
  #     step-up redirect, which is what a browser does on a double-click, an
  #     autofill re-fire, or a back-then-forward. The session now exists, so
  #     Kratos answers session_already_available and sends the browser to
  #     default_browser_return_url — the auth root, i.e. the ui-node's welcome
  #     page — while the step-up flow is still in flight.
  if [ "$mode" = double ]; then
    post_flow "$jar" "$flowid" password
    echo "  re-post of the original flow: http=$LAST_STATUS"
    echo "                   next=$(printf '%s' "$LAST_LOCATION" | cut -c1-100)"
    # Follow it exactly as a browser would: land on the welcome page, let the
    # ui-node call /sessions/whoami, and follow whatever it decides.
    local wfinal
    wfinal=$(curl -sS -o /dev/null -c "$jar" -b "$jar" -L --max-redirs 10 \
               -w '%{url_effective}' "$LAST_LOCATION")
    echo "  welcome-page render landed at: $(printf '%s' "$wfinal" | cut -c1-100)"
    local wflow; wflow=$(qparam "$wfinal" flow)
    if [ -n "$wflow" ]; then
      echo "  welcome render handed us flow=$wflow  carries_challenge=$(flow_challenge "$wflow")"
      LAST_LOCATION="$wfinal"
    fi
    # Resume from whatever the browser is now looking at, not from the
    # challenge-bearing redirect Kratos originally issued.
    [ -n "$wflow" ] || LAST_LOCATION="$first_location"
  fi
  echo "  after password:  http=$LAST_STATUS  session_aal=$(session_aal "$jar")"
  echo "                   next=$LAST_LOCATION"

  # 4. Follow whatever Kratos handed back; submit the second factor if asked.
  local aal2flow=""
  case "$LAST_LOCATION" in
    *flow=*)    aal2flow=$(qparam "$LAST_LOCATION" flow) ;;
    *aal=aal2*) aal2flow=$(qparam "$(curl -sS -o /dev/null -c "$jar" -b "$jar" -L --max-redirs 10 \
                                      -w '%{url_effective}' "$LAST_LOCATION")" flow) ;;
  esac
  if [ -n "$aal2flow" ] && [ "$mode" = ui ]; then
    # THE HOP THAT MATTERS. A browser does not post to Kratos from thin air —
    # it first loads the ui-node page for the flow. That page calls
    # /sessions/whoami, which refuses a mid-step-up aal1 session, and the
    # ui-node then sends the browser to the bare redirect_browser_to from that
    # refusal — dropping the login_challenge Kratos had preserved.
    local uifinal
    uifinal=$(curl -sS -o /dev/null -c "$jar" -b "$jar" -L --max-redirs 10 \
                -w '%{url_effective}' "https://$SITE_AUTH/login?flow=$aal2flow")
    local uiflow; uiflow=$(qparam "$uifinal" flow)
    if [ -n "$uiflow" ] && [ "$uiflow" != "$aal2flow" ]; then
      echo "  ui render REPLACED the flow: $aal2flow -> $uiflow"
      aal2flow="$uiflow"
    else
      echo "  ui render kept flow $aal2flow"
    fi
  fi
  if [ -n "$aal2flow" ]; then
    echo "  aal2 flow=$aal2flow  carries_challenge=$(flow_challenge "$aal2flow")"
    post_flow "$jar" "$aal2flow" totp
    echo "  after totp:      http=$LAST_STATUS  session_aal=$(session_aal "$jar")"
    echo "                   next=$LAST_LOCATION"
  fi

  # 5. Verdict.
  if accepted "$LAST_LOCATION"; then
    echo "  VERDICT[$label]: ACCEPTED — Kratos returned a login_verifier; the app is reached"
  else
    echo "  VERDICT[$label]: ORPHANED — no login_verifier; browser lands at:"
    echo "                   $(printf '%s' "$LAST_LOCATION" | cut -c1-110)"
  fi
}

# Enrol the second factor the way a human does: log in with the password, open
# a settings flow, read the offered TOTP secret, and submit a code for it.
enrol_totp() {
  say "enrolling TOTP on the probe identity via the settings flow"
  local jar="$WORK/jar.enrol"; rm -f "$jar"; : > "$jar"

  local flowid
  flowid=$(qparam "$(curl -sS -o /dev/null -c "$jar" -b "$jar" -L --max-redirs 10 \
                      -w '%{url_effective}' "https://$SITE_AUTH/self-service/login/browser")" flow)
  post_flow "$jar" "$flowid" password
  echo "  password login: http=$LAST_STATUS session_aal=$(session_aal "$jar")"

  local sflow sbody
  sflow=$(qparam "$(curl -sS -o /dev/null -c "$jar" -b "$jar" -L --max-redirs 10 \
                     -w '%{url_effective}' "https://$SITE_AUTH/self-service/settings/browser")" flow)
  sbody=$(curl -sS -b "$jar" "https://$SITE_AUTH/self-service/settings/flows?id=$sflow")
  TOTP_SECRET=$(printf '%s' "$sbody" | jqp '
for n in d.get("ui",{}).get("nodes",[]):
    a = n.get("attributes",{})
    if a.get("id") == "totp_secret_key":
        print((a.get("text") or {}).get("text","")); break
else: print("")')
  if [ -z "$TOTP_SECRET" ]; then
    echo "  FAILED to obtain a TOTP secret from the settings flow"; return 1
  fi
  echo "  totp secret offered (${#TOTP_SECRET} chars)"

  local csrf action
  csrf=$(printf '%s' "$sbody" | jqp '
print(next((n["attributes"].get("value","") for n in d.get("ui",{}).get("nodes",[])
            if n.get("attributes",{}).get("name")=="csrf_token"), ""))')
  action=$(printf '%s' "$sbody" | jqp 'print(d.get("ui",{}).get("action",""))')
  curl -sS -o "$WORK/enrol.resp" -c "$jar" -b "$jar" -X POST "$action" -H 'Accept: application/json' \
    --data-urlencode "csrf_token=$csrf" --data-urlencode method=totp \
    --data-urlencode "totp_code=$(totp_now)" >/dev/null

  curl -sS "$KRATOS_ADMIN/admin/identities/$IDENTITY_ID" \
    | jqp 'print("  credentials now:", sorted((d.get("credentials") or {}).keys()))'
}

setup_identity || exit 1
enrol_totp || { teardown_identity; exit 1; }
run_variant kratos-direct direct
run_variant through-ui   ui
run_variant double-submit double
run_variant two-tab       twotab
teardown_identity
