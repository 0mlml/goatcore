#!/usr/bin/env bash
# netbird-boot-selfheal.sh — self-heal a wedged NetBird client on a mesh host.
#
# WHY THIS EXISTS:
#   The stock netbird.service waits only on `network.target`, which does NOT
#   mean the network is usable — it fires before routes/DNS to the management
#   and relay endpoints are actually up. A netbird that starts into that window
#   sees "management: no route to host", and its internal engine can wedge (ICE
#   agents never initialize, the signal outbound channel backs up): few peer
#   tunnels ever establish and it never recovers by itself. The host stays up
#   but peers cannot reach it. One `systemctl restart netbird`, once the network
#   is ready, clears it. See docs/design/netbird-boot-selfheal.md.
#
# WHAT IT DOES:
#   Runs on a timer. Reads `netbird status`. If the CONTROL PLANE is up
#   (management + signal Connected) but the connected-peer count is stuck below
#   a floor for several consecutive checks, it bounces netbird ONCE (rate
#   limited, capped per boot). It never restarts when the control plane is down
#   (that's a real upstream outage a restart can't fix) — so it only acts on
#   the exact "control-plane-healthy-but-peers-wedged" signature.
#
#   Also emits a node_exporter-style textfile metric so the wedge (and the
#   heal) are VISIBLE off-host, because a wedged host self-reports healthy
#   while no peer can reach it.
#
# The companion `10-network-online.conf` drop-in (installed by the same role)
# is the root-cause mitigation: it makes netbird wait for network-online.target
# so the racy start is far less likely in the first place. This script is the
# backstop for when it wedges anyway.
#
# DEPENDENCY-FREE: parses plain `netbird status` text (no jq/python). Config is
# injected via EnvironmentFile (see the role). Idempotent; safe to run often.
set -u

# ---- config (env, with safe defaults) --------------------------------------
NB_BIN="${NB_BIN:-netbird}"
MIN_PEERS="${MIN_PEERS:-15}"                 # wedge floor: set above the count a wedged client reaches
SUSTAINED_CHECKS="${SUSTAINED_CHECKS:-4}"    # consecutive low samples before acting
COOLDOWN_S="${COOLDOWN_S:-1800}"             # min seconds between restarts (30 min)
MAX_RESTARTS_PER_BOOT="${MAX_RESTARTS_PER_BOOT:-3}"  # give up + escalate past this

# The budget above resets on a new boot_id. That is right for the boot race and
# wrong for a long-lived node: a node that spends all its restarts on one boot
# would silently disarm the watchdog until the next reboot. An escalation that
# only writes a textfile metric reaches no human on a host nothing scrapes.
#
# So the budget also DECAYS: if the last restart is older than this window, the
# count resets. Anti-thrash is preserved (3 inside the window still caps and
# escalates — if three restarts in a day did not fix it, a fourth will not),
# while a node that is saved once a month is not permanently disarmed by its
# own successes. 0 disables the decay (per-boot only, previous behaviour).
RESTART_BUDGET_WINDOW_S="${RESTART_BUDGET_WINDOW_S:-86400}"
STATE_DIR="${STATE_DIR:-/var/lib/netbird-boot-selfheal}"
METRICS_FILE="${METRICS_FILE:-${STATE_DIR}/metrics.prom}"
RESTART_CMD="${RESTART_CMD:-systemctl restart netbird}"

# ---- second wedge signature: control-plane-DOWN-but-endpoint-REACHABLE ------
# The original signature above only fires when mgmt+signal read Connected, and
# it deliberately refuses to act when the control plane is down, on the
# reasoning that a down control plane is a real upstream outage a restart
# cannot fix.
#
# One circumstance breaks that premise: the client reads `Management:
# Disconnected` / `Signal: Disconnected` while its interfaces have working
# egress and can TCP-reach the management endpoint, because the daemon holds a
# stale "keepalive ping failed to receive ACK" / read-timeout. A restart clears
# it, and without this arm the watchdog exits at the guard above.
#
# So: when the control plane reads down but the endpoint is demonstrably
# reachable at the network layer, the daemon is stuck, not the upstream. That is
# a restartable condition, and it is distinguishable with one TCP connect.
#
# OPT-IN (default off): this adds a new reason to restart netbird, so a group
# enables it deliberately.
CP_WEDGE_ENABLED="${CP_WEDGE_ENABLED:-0}"
# Endpoint to probe. Empty = derive from `netbird status --json`
# (.management.url). If it cannot be derived, the signature is skipped entirely
# — fail-safe: an unknown endpoint must never justify a restart.
MGMT_PROBE_URL="${MGMT_PROBE_URL:-}"
PROBE_TIMEOUT_S="${PROBE_TIMEOUT_S:-5}"

# ---- third wedge signature: EVERYTHING READS HEALTHY BUT NO DATA MOVES ------
# Signatures 1 and 2 both key off the CONTROL plane — one needs it up with
# peers low, the other needs it down. Neither can see this shape:
# `Management: Connected`, `Signal: Connected`, a normal peer count, every peer
# listing `Status: Connected, Connection type: P2P` — and the data plane dead in
# both directions, with nothing in the journal. A restart clears it.
#
# So: probe the DATA plane. Success on ANY target means the tunnel carries
# traffic and we do nothing. Only when EVERY target fails, sustained, while the
# control plane is up and peers are at/above the floor, is the daemon lying.
#
# TCP ONLY, NEVER PING. Two independent reasons: F-067 documents a transport
# state where TCP forwarding fails per-client while ICMP keeps working, so ping
# reports healthy through a real outage; and BREAKGLASS hosts are granted
# `tcp 22` outbound ONLY (ADR 1114), so an ICMP probe from one reads broken
# forever and would restart netbird every cooldown.
#
# TARGETS ARE EXPLICIT AND NEVER DERIVED. A guessed target that the ACL does
# not permit is indistinguishable from a wedge, and would bounce netbird on a
# perfectly healthy host. Empty list = signature skipped entirely.
# Format: space-separated `host:port`, e.g. "100.127.0.221:22 100.127.0.206:22".
# Give it MORE THAN ONE so a single peer rebooting is not read as a wedge.
#
# DEFAULT OFF.
DATA_WEDGE_ENABLED="${DATA_WEDGE_ENABLED:-0}"
DATA_PROBE_TARGETS="${DATA_PROBE_TARGETS:-}"

mkdir -p "${STATE_DIR}"
now="$(date +%s)"

low_file="${STATE_DIR}/low_streak"
last_restart_file="${STATE_DIR}/last_restart"
restart_count_file="${STATE_DIR}/restart_count"
boot_id_file="${STATE_DIR}/boot_id"
# Cross-boot restart ledger. One TSV line per restart this
# watchdog issues — `unixtime<TAB>boot_id<TAB>connected<TAB>total` — appended,
# NOT reset per boot (unlike restart_count), so the dashboard can show the
# TIMELINE of self-heals across reboots, not just this boot's count. Bounded to
# the last LEDGER_KEEP events so it can't grow unbounded.
ledger_file="${STATE_DIR}/restart-ledger.tsv"
# Streak for the second signature, tracked separately from low_streak so the two
# wedge shapes can never borrow each other's evidence.
cp_down_file="${STATE_DIR}/cp_down_streak"
# Streak for the third signature, likewise separate: "everything reads healthy
# and no data moves" must never inherit evidence from either control-plane arm.
data_dead_file="${STATE_DIR}/data_dead_streak"
LEDGER_KEEP="${LEDGER_KEEP:-200}"

read_int() { local f="$1" d="$2"; if [ -r "$f" ]; then cat "$f" 2>/dev/null | tr -cd '0-9' | head -c 18; else printf '%s' "$d"; fi; }
low_streak="$(read_int "$low_file" 0)";           low_streak="${low_streak:-0}"
last_restart="$(read_int "$last_restart_file" 0)"; last_restart="${last_restart:-0}"
restart_count="$(read_int "$restart_count_file" 0)"; restart_count="${restart_count:-0}"

# Per-boot reset: a fresh boot_id means restart budget resets.
cur_boot_id="$(cat /proc/sys/kernel/random/boot_id 2>/dev/null || echo unknown)"
prev_boot_id="$(cat "$boot_id_file" 2>/dev/null || echo none)"
if [ "$cur_boot_id" != "$prev_boot_id" ]; then
  echo "$cur_boot_id" > "$boot_id_file"
  restart_count=0; echo 0 > "$restart_count_file"
  low_streak=0;    echo 0 > "$low_file"
  echo 0 > "$data_dead_file"
fi

# Budget decay (see RESTART_BUDGET_WINDOW_S). Same boot, but the last restart is
# old enough that this is a new incident rather than thrashing — give the
# watchdog its budget back instead of leaving it permanently disarmed.
if [ "$RESTART_BUDGET_WINDOW_S" -gt 0 ] 2>/dev/null \
   && [ "$restart_count" -gt 0 ] \
   && [ "$last_restart" -gt 0 ] \
   && [ $(( now - last_restart )) -ge "$RESTART_BUDGET_WINDOW_S" ]; then
  echo "netbird-boot-selfheal: last restart was $(( (now - last_restart) / 3600 ))h ago (>= ${RESTART_BUDGET_WINDOW_S}s window); resetting restart budget from ${restart_count} so the watchdog is armed for a new incident." >&2
  restart_count=0; echo 0 > "$restart_count_file"
fi

# Derive "host port" from a URL like https://host:443 (no jq — this script is
# deliberately dependency-free). Prints nothing if it cannot parse.
derive_mgmt_hostport() {
  local url="${1:-}" hostport host port
  [ -n "$url" ] || return 1
  hostport="${url#*://}"      # strip scheme
  hostport="${hostport%%/*}"  # strip any path
  case "$hostport" in
    *:*) host="${hostport%:*}"; port="${hostport##*:}" ;;
    *)   host="$hostport";      port=443 ;;
  esac
  case "$host" in ""|*" "*) return 1 ;; esac
  case "$port" in ""|*[!0-9]*) return 1 ;; esac
  printf '%s %s\n' "$host" "$port"
}

# 0 = a TCP connection to host/port completed inside PROBE_TIMEOUT_S.
mgmt_endpoint_reachable() {
  timeout "$PROBE_TIMEOUT_S" bash -c "exec 3<>/dev/tcp/$1/$2" 2>/dev/null
}

# 0 = at least ONE configured data-plane target accepted a TCP connection over
# the mesh. ANY success is enough: this asks "does the tunnel carry traffic at
# all", not "is every peer up". Malformed entries are skipped, not fatal —
# a typo must never be able to manufacture a wedge verdict.
data_plane_reachable() {
  local target host port
  for target in $DATA_PROBE_TARGETS; do
    host="${target%:*}"; port="${target##*:}"
    case "$host" in ""|*" "*) continue ;; esac
    case "$port" in ""|*[!0-9]*) continue ;; esac
    if timeout "$PROBE_TIMEOUT_S" bash -c "exec 3<>/dev/tcp/${host}/${port}" 2>/dev/null; then
      return 0
    fi
  done
  return 1
}

emit_metrics() {
  # $1 mgmt $2 signal $3 connected $4 total $5 wedged $6 low_streak
  # $7 cp_wedged (optional) $8 cp_down_streak (optional)
  # $9 data_wedged (optional) $10 data_dead_streak (optional)
  local tmp="${METRICS_FILE}.$$"
  {
    echo "# HELP netbird_selfheal_mgmt_connected NetBird management control-plane up (1) or down (0)."
    echo "# TYPE netbird_selfheal_mgmt_connected gauge"
    echo "netbird_selfheal_mgmt_connected $1"
    echo "# HELP netbird_selfheal_signal_connected NetBird signal control-plane up (1) or down (0)."
    echo "# TYPE netbird_selfheal_signal_connected gauge"
    echo "netbird_selfheal_signal_connected $2"
    echo "# HELP netbird_selfheal_peers_connected Currently-connected mesh peer tunnels."
    echo "# TYPE netbird_selfheal_peers_connected gauge"
    echo "netbird_selfheal_peers_connected $3"
    echo "# HELP netbird_selfheal_peers_total Peers this host knows about."
    echo "# TYPE netbird_selfheal_peers_total gauge"
    echo "netbird_selfheal_peers_total $4"
    echo "# HELP netbird_selfheal_min_peers Configured wedge floor."
    echo "# TYPE netbird_selfheal_min_peers gauge"
    echo "netbird_selfheal_min_peers ${MIN_PEERS}"
    echo "# HELP netbird_selfheal_wedged 1 = control plane up but peer count sustained-low (wedge)."
    echo "# TYPE netbird_selfheal_wedged gauge"
    echo "netbird_selfheal_wedged $5"
    echo "# HELP netbird_selfheal_low_streak Consecutive low-peer samples."
    echo "# TYPE netbird_selfheal_low_streak gauge"
    echo "netbird_selfheal_low_streak $6"
    echo "# HELP netbird_selfheal_cp_wedged 1 = control plane reads DOWN while its endpoint is TCP-reachable (stuck daemon, not an upstream outage)."
    echo "# TYPE netbird_selfheal_cp_wedged gauge"
    echo "netbird_selfheal_cp_wedged ${7:-0}"
    echo "# HELP netbird_selfheal_cp_down_streak Consecutive control-plane-down-but-endpoint-reachable samples."
    echo "# TYPE netbird_selfheal_cp_down_streak gauge"
    echo "netbird_selfheal_cp_down_streak ${8:-0}"
    echo "# HELP netbird_selfheal_data_wedged 1 = control plane up and peers connected, yet no data-plane target answered a TCP connect (daemon reporting health it does not have)."
    echo "# TYPE netbird_selfheal_data_wedged gauge"
    echo "netbird_selfheal_data_wedged ${9:-0}"
    echo "# HELP netbird_selfheal_data_dead_streak Consecutive samples where every data-plane target refused a TCP connect while everything else read healthy."
    echo "# TYPE netbird_selfheal_data_dead_streak gauge"
    echo "netbird_selfheal_data_dead_streak ${10:-0}"
    echo "# HELP netbird_selfheal_restart_total netbird restarts this selfheal has triggered since boot."
    echo "# TYPE netbird_selfheal_restart_total counter"
    echo "netbird_selfheal_restart_total ${restart_count}"
    echo "# HELP netbird_selfheal_last_restart_timestamp Unix time of the last selfheal restart (0 = never this boot)."
    echo "# TYPE netbird_selfheal_last_restart_timestamp gauge"
    echo "netbird_selfheal_last_restart_timestamp ${last_restart}"
    echo "# HELP netbird_selfheal_last_run_timestamp Unix time of the last selfheal check."
    echo "# TYPE netbird_selfheal_last_run_timestamp gauge"
    echo "netbird_selfheal_last_run_timestamp ${now}"
  } > "$tmp" 2>/dev/null
  if ! mv -f "$tmp" "$METRICS_FILE" 2>/dev/null; then rm -f "$tmp" 2>/dev/null; fi
}

# ---- read status -----------------------------------------------------------
status="$("$NB_BIN" status 2>/dev/null)"
if [ -z "$status" ]; then
  # daemon not answering (still starting, or down) — can't act, report dark.
  emit_metrics 0 0 0 0 0 "$low_streak"
  echo "netbird-boot-selfheal: netbird status unavailable; no action." >&2
  exit 0
fi

mgmt_up=0;   echo "$status" | grep -qE '^Management: Connected'  && mgmt_up=1
signal_up=0; echo "$status" | grep -qE '^Signal: Connected'      && signal_up=1

# "Peers count: 32/64 Connected"
peer_line="$(echo "$status" | grep -E '^Peers count:' | head -1)"
connected="$(echo "$peer_line" | sed -nE 's/^Peers count: *([0-9]+)\/([0-9]+).*/\1/p')"
total="$(echo "$peer_line"     | sed -nE 's/^Peers count: *([0-9]+)\/([0-9]+).*/\2/p')"
connected="${connected:-0}"; total="${total:-0}"

# ---- decide ----------------------------------------------------------------
if [ "$mgmt_up" != 1 ] || [ "$signal_up" != 1 ]; then
  # Control plane down. Signatures 1 (peers-low) and 3 (data-plane) both
  # require control-plane-UP, so their streaks reset here unconditionally.
  low_streak=0; echo 0 > "$low_file"
  echo 0 > "$data_dead_file"

  if [ "$CP_WEDGE_ENABLED" != 1 ]; then
    # Original behaviour: assume a genuine upstream outage; a restart can't fix
    # that and could thrash.
    echo 0 > "$cp_down_file"
    emit_metrics "$mgmt_up" "$signal_up" "$connected" "$total" 0 0 0 0
    exit 0
  fi

  # Signature 2: is the endpoint actually reachable? If it is, the daemon is
  # stuck rather than the upstream being down.
  probe_url="$MGMT_PROBE_URL"
  if [ -z "$probe_url" ]; then
    probe_url="$("$NB_BIN" status --json 2>/dev/null \
      | sed -nE 's/.*"management"[[:space:]]*:[[:space:]]*\{[^}]*"url"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' | head -1)"
  fi
  hostport="$(derive_mgmt_hostport "$probe_url" || true)"

  if [ -z "$hostport" ]; then
    # Fail-safe: endpoint unknown, so reachability is unknown, so we must not
    # restart on this signature.
    echo 0 > "$cp_down_file"
    emit_metrics "$mgmt_up" "$signal_up" "$connected" "$total" 0 0 0 0
    echo "netbird-boot-selfheal: control plane down but management endpoint could not be derived; not acting." >&2
    exit 0
  fi

  # shellcheck disable=SC2086
  if ! mgmt_endpoint_reachable $hostport; then
    # Endpoint genuinely unreachable — a real outage or a real network fault.
    # This is the case the original guard was right about.
    echo 0 > "$cp_down_file"
    emit_metrics "$mgmt_up" "$signal_up" "$connected" "$total" 0 0 0 0
    echo "netbird-boot-selfheal: control plane down AND ${hostport% *} unreachable — upstream/network problem, not a stuck daemon; not acting." >&2
    exit 0
  fi

  cp_down_streak="$(read_int "$cp_down_file" 0)"; cp_down_streak="${cp_down_streak:-0}"
  cp_down_streak=$(( cp_down_streak + 1 ))
  echo "$cp_down_streak" > "$cp_down_file"
  emit_metrics "$mgmt_up" "$signal_up" "$connected" "$total" 0 0 1 "$cp_down_streak"

  if [ "$cp_down_streak" -lt "$SUSTAINED_CHECKS" ]; then
    echo "netbird-boot-selfheal: control plane down but ${hostport% *} REACHABLE, streak ${cp_down_streak}/${SUSTAINED_CHECKS}; watching." >&2
    exit 0
  fi
  if [ "$restart_count" -ge "$MAX_RESTARTS_PER_BOOT" ]; then
    echo "netbird-boot-selfheal: ALREADY restarted ${restart_count}x this boot and control plane still down with a reachable endpoint — escalating to human, no further restarts." >&2
    exit 0
  fi
  if [ $(( now - last_restart )) -lt "$COOLDOWN_S" ]; then
    echo "netbird-boot-selfheal: control-plane wedge but within ${COOLDOWN_S}s cooldown; waiting." >&2
    exit 0
  fi

  echo "netbird-boot-selfheal: CONTROL-PLANE WEDGE CONFIRMED — mgmt=${mgmt_up} signal=${signal_up} while ${hostport% *} is TCP-reachable, for ${cp_down_streak} checks. Restarting netbird (restart #$(( restart_count + 1 )) this boot)." >&2
  if $RESTART_CMD; then
    restart_count=$(( restart_count + 1 ))
    echo "$restart_count" > "$restart_count_file"
    echo "$now" > "$last_restart_file"
    last_restart="$now"
    echo 0 > "$cp_down_file"
    { printf '%s\t%s\t%s\t%s\tcp-wedge\n' "$now" "$cur_boot_id" "$connected" "$total" >> "$ledger_file"; } 2>/dev/null
    if [ -f "$ledger_file" ]; then
      tail -n "$LEDGER_KEEP" "$ledger_file" > "${ledger_file}.tmp" 2>/dev/null \
        && mv -f "${ledger_file}.tmp" "$ledger_file" 2>/dev/null || rm -f "${ledger_file}.tmp" 2>/dev/null
    fi
    emit_metrics "$mgmt_up" "$signal_up" "$connected" "$total" 0 0 1 0
    echo "netbird-boot-selfheal: netbird restart issued OK; will re-evaluate next tick." >&2
  else
    echo "netbird-boot-selfheal: '${RESTART_CMD}' FAILED (rc=$?); leaving state, will retry next tick." >&2
  fi
  exit 0
fi

# Control plane is up — signature 2's streak resets.
echo 0 > "$cp_down_file"

if [ "$connected" -ge "$MIN_PEERS" ]; then
  # Everything the control plane can tell us says healthy. Signature 1's
  # streak resets here regardless.
  low_streak=0; echo 0 > "$low_file"

  if [ "$DATA_WEDGE_ENABLED" != 1 ] || [ -z "$DATA_PROBE_TARGETS" ]; then
    # Signature 3 off, or no targets configured. Unconfigured means we cannot
    # speak about the data plane — report healthy on what we DID measure and
    # reset the streak, never infer a wedge from a probe we did not run.
    echo 0 > "$data_dead_file"
    emit_metrics "$mgmt_up" "$signal_up" "$connected" "$total" 0 0 0 0 0 0
    exit 0
  fi

  if data_plane_reachable; then
    # Control plane up, peers up, and traffic actually crosses the tunnel.
    echo 0 > "$data_dead_file"
    emit_metrics "$mgmt_up" "$signal_up" "$connected" "$total" 0 0 0 0 0 0
    exit 0
  fi

  # Signature 3: the daemon claims health and moves no data.
  data_dead_streak="$(read_int "$data_dead_file" 0)"; data_dead_streak="${data_dead_streak:-0}"
  data_dead_streak=$(( data_dead_streak + 1 ))
  echo "$data_dead_streak" > "$data_dead_file"
  emit_metrics "$mgmt_up" "$signal_up" "$connected" "$total" 0 0 0 0 1 "$data_dead_streak"

  if [ "$data_dead_streak" -lt "$SUSTAINED_CHECKS" ]; then
    echo "netbird-boot-selfheal: control plane up and ${connected}/${total} peers connected, but NO data-plane target answered; streak ${data_dead_streak}/${SUSTAINED_CHECKS}; watching." >&2
    exit 0
  fi
  if [ "$restart_count" -ge "$MAX_RESTARTS_PER_BOOT" ]; then
    echo "netbird-boot-selfheal: ALREADY restarted ${restart_count}x this boot and the data plane is still dead while everything reads healthy — escalating to human, no further restarts." >&2
    exit 0
  fi
  if [ $(( now - last_restart )) -lt "$COOLDOWN_S" ]; then
    echo "netbird-boot-selfheal: data-plane wedge but within ${COOLDOWN_S}s cooldown; waiting." >&2
    exit 0
  fi

  echo "netbird-boot-selfheal: DATA-PLANE WEDGE CONFIRMED — mgmt+signal Connected and ${connected}/${total} peers report connected, yet none of [${DATA_PROBE_TARGETS}] accepted a TCP connection for ${data_dead_streak} checks. Restarting netbird (restart #$(( restart_count + 1 )) this boot)." >&2
  if $RESTART_CMD; then
    restart_count=$(( restart_count + 1 ))
    echo "$restart_count" > "$restart_count_file"
    echo "$now" > "$last_restart_file"
    last_restart="$now"
    echo 0 > "$data_dead_file"
    { printf '%s\t%s\t%s\t%s\tdata-wedge\n' "$now" "$cur_boot_id" "$connected" "$total" >> "$ledger_file"; } 2>/dev/null
    if [ -f "$ledger_file" ]; then
      tail -n "$LEDGER_KEEP" "$ledger_file" > "${ledger_file}.tmp" 2>/dev/null \
        && mv -f "${ledger_file}.tmp" "$ledger_file" 2>/dev/null || rm -f "${ledger_file}.tmp" 2>/dev/null
    fi
    emit_metrics "$mgmt_up" "$signal_up" "$connected" "$total" 0 0 0 0 1 0
    echo "netbird-boot-selfheal: netbird restart issued OK; will re-evaluate next tick." >&2
  else
    echo "netbird-boot-selfheal: '${RESTART_CMD}' FAILED (rc=$?); leaving state, will retry next tick." >&2
  fi
  exit 0
fi

# Peers below floor — signature 3's streak resets; signature 1 owns this path.
echo 0 > "$data_dead_file"

# Control plane UP but peers below floor — candidate wedge.
low_streak=$(( low_streak + 1 ))
echo "$low_streak" > "$low_file"
emit_metrics "$mgmt_up" "$signal_up" "$connected" "$total" 1 "$low_streak"

if [ "$low_streak" -lt "$SUSTAINED_CHECKS" ]; then
  echo "netbird-boot-selfheal: peers ${connected}/${total} < ${MIN_PEERS}, streak ${low_streak}/${SUSTAINED_CHECKS} (control plane up); watching." >&2
  exit 0
fi

if [ "$restart_count" -ge "$MAX_RESTARTS_PER_BOOT" ]; then
  echo "netbird-boot-selfheal: ALREADY restarted ${restart_count}x this boot and still wedged (${connected}/${total}). NOT the boot race — escalating to human, no further restarts." >&2
  exit 0
fi

if [ $(( now - last_restart )) -lt "$COOLDOWN_S" ]; then
  echo "netbird-boot-selfheal: wedged (${connected}/${total}) but within ${COOLDOWN_S}s cooldown since last restart; waiting." >&2
  exit 0
fi

# ---- act -------------------------------------------------------------------
echo "netbird-boot-selfheal: WEDGE CONFIRMED — control plane up (mgmt+signal Connected) but only ${connected}/${total} peers for ${low_streak} checks. Restarting netbird (restart #$(( restart_count + 1 )) this boot)." >&2
if $RESTART_CMD; then
  restart_count=$(( restart_count + 1 ))
  echo "$restart_count" > "$restart_count_file"
  echo "$now" > "$last_restart_file"
  last_restart="$now"
  low_streak=0; echo 0 > "$low_file"
  # Record this restart in the cross-boot ledger + bound it.
  { printf '%s\t%s\t%s\t%s\n' "$now" "$cur_boot_id" "$connected" "$total" >> "$ledger_file"; } 2>/dev/null
  if [ -f "$ledger_file" ]; then
    tail -n "$LEDGER_KEEP" "$ledger_file" > "${ledger_file}.tmp" 2>/dev/null \
      && mv -f "${ledger_file}.tmp" "$ledger_file" 2>/dev/null || rm -f "${ledger_file}.tmp" 2>/dev/null
  fi
  emit_metrics "$mgmt_up" "$signal_up" "$connected" "$total" 1 0
  echo "netbird-boot-selfheal: netbird restart issued OK; will re-evaluate next tick." >&2
else
  echo "netbird-boot-selfheal: '${RESTART_CMD}' FAILED (rc=$?); leaving state, will retry next tick." >&2
fi
exit 0
