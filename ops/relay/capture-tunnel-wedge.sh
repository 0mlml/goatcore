#!/usr/bin/env bash
# capture-tunnel-wedge.sh — capture the clean, correlated evidence of a rathole
# management-tunnel wedge, the capture that F-594 could not get.
#
# WHY THIS EXISTS. relay-dub's tunnel to the zagros Thor flapped
# for ~26 h: the server logged 5,666 noise-handshake EOFs, the client
# established the control channel once and otherwise exited clean, and it
# recovered on its own. It was never root-caused, because the one diagnostic
# that would have cracked it — an strace of a SINGLE isolated client process
# plus a packet capture on BOTH ends of the exact handshake — was never taken
# cleanly: the client's `Restart=always`/`RestartSec=5s` kept a second process
# running alongside any manual strace, so every capture was polluted. This
# script takes that capture. Run it while the wedge is live (F-596
# RelayEnrollmentTunnelDown is the trigger). docs/findings/F-594.
#
# SAFETY. The only state it changes is stopping ONE rathole-client@<relay> unit
# on the Thor so a single process can be isolated, and it ALWAYS restarts it on
# exit (trap), including on error or Ctrl-C. The tunnel is already down when you
# run this, so the stop costs nothing. Everything else is read-only.
#
# USAGE
#   ops/relay/capture-tunnel-wedge.sh <relay> --thor <user@host:port> \
#       --instance <i-...> --region <aws-region> [--seconds 30]
#   e.g. ops/relay/capture-tunnel-wedge.sh dub \
#       --thor goat@127.0.0.1:12201 \
#       --instance i-00000d60adc214661 --region eu-west-1
#
# The Thor target is the door/mesh ssh endpoint (ops/scripts/env-door.sh
# <env> --ssh-target). The relay side goes through SSM under aws-vault, so run
# under `aws-vault exec usareur-af --no-session --` or have creds in the env.
set -uo pipefail

RELAY="${1:-}"; shift || true
THOR="" INST="" REGION="" SECONDS_CAP=30
while [ $# -gt 0 ]; do
  case "$1" in
    --thor)     THOR="$2"; shift 2 ;;
    --instance) INST="$2"; shift 2 ;;
    --region)   REGION="$2"; shift 2 ;;
    --seconds)  SECONDS_CAP="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done
[ -n "$RELAY" ] && [ -n "$THOR" ] && [ -n "$INST" ] && [ -n "$REGION" ] || {
  echo "usage: $0 <relay> --thor <user@host:port> --instance <i-...> --region <aws-region> [--seconds N]" >&2
  exit 2
}

TU="${THOR%@*}"; HP="${THOR#*@}"; TH="${HP%%:*}"; TP="${HP##*:}"
[ "$TP" = "$HP" ] && TP=22
SSH="ssh -o ConnectTimeout=12 -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p $TP $TU@$TH"
UNIT="rathole-client@${RELAY}"
OUT="$(mktemp -d "/tmp/tunnel-wedge-${RELAY}-$(date -u +%Y%m%dT%H%M%SZ)-XXXX")"
echo "capture dir: $OUT"

restore() {
  echo "restoring ${UNIT} on the Thor ..."
  $SSH "sudo systemctl start ${UNIT}" 2>/dev/null \
    && echo "restored: $($SSH "systemctl is-active ${UNIT}" 2>/dev/null)" \
    || echo "WARNING: could not confirm ${UNIT} restart — start it by hand: systemctl start ${UNIT}"
}
trap restore EXIT

echo "== 1/5  relay address for this client"
RELAY_IP="$($SSH "sudo grep -oE 'remote_addr = \"[0-9.]+' /etc/rathole/client-${RELAY}.toml" 2>/dev/null | grep -oE '[0-9.]+' | head -1)"
echo "relay ${RELAY} ip: ${RELAY_IP:-unknown}"

echo "== 2/5  isolate ONE client: stop the auto-restarting unit"
$SSH "sudo systemctl stop ${UNIT}" || { echo "could not stop ${UNIT}"; exit 3; }

echo "== 3/5  capture BOTH ends for ${SECONDS_CAP}s (NTP-synced clocks correlate)"
# relay side via SSM: tcpdump :2333 + server trace for the window
aws ssm send-command --region "$REGION" --instance-ids "$INST" --document-name AWS-RunShellScript \
  --parameters "{\"commands\":[\"timeout ${SECONDS_CAP} tcpdump -ni any 'tcp port 2333' -w /tmp/relay-2333.pcap 2>/dev/null & journalctl -u rathole-server -o short-iso --since now > /tmp/relay-server.log 2>&1 & sleep $((SECONDS_CAP+2)); pkill -f 'journalctl -u rathole-server' 2>/dev/null; echo captured\"],\"executionTimeout\":[\"$((SECONDS_CAP+60))\"]}" \
  --query Command.CommandId --output text > "$OUT/relay-cmd-id" 2>/dev/null \
  && echo "relay capture started (SSM $(cat "$OUT/relay-cmd-id"))" \
  || echo "WARNING: relay-side SSM capture did not start (check aws-vault creds); Thor side still captured"

# Thor side: tcpdump to the relay + ONE strace'd client, same window
$SSH "sudo sh -c 'timeout ${SECONDS_CAP} tcpdump -ni any \"host ${RELAY_IP} and tcp port 2333\" -w /tmp/thor-2333.pcap 2>/dev/null &
  RUST_LOG=trace timeout ${SECONDS_CAP} strace -f -tt -e trace=network,read,write,close /usr/local/bin/rathole -c /etc/rathole/client-${RELAY}.toml > /tmp/thor-client-strace.log 2>&1
  wait'" || true

echo "== 4/5  collect"
$SSH "sudo chmod a+r /tmp/thor-2333.pcap /tmp/thor-client-strace.log 2>/dev/null; true"
scp -q -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -P "$TP" "$TU@$TH:/tmp/thor-2333.pcap" "$OUT/" 2>/dev/null || true
scp -q -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -P "$TP" "$TU@$TH:/tmp/thor-client-strace.log" "$OUT/" 2>/dev/null || true
if [ -s "$OUT/relay-cmd-id" ]; then
  CID="$(cat "$OUT/relay-cmd-id")"
  for _ in $(seq 1 30); do
    st="$(aws ssm get-command-invocation --region "$REGION" --command-id "$CID" --instance-id "$INST" --query Status --output text 2>/dev/null || echo Pending)"
    case "$st" in Success|Failed|TimedOut) break;; esac; sleep 3
  done
  aws ssm send-command --region "$REGION" --instance-ids "$INST" --document-name AWS-RunShellScript \
    --parameters '{"commands":["base64 /tmp/relay-2333.pcap 2>/dev/null; echo ---SERVERLOG---; tail -400 /tmp/relay-server.log 2>/dev/null"]}' \
    --query Command.CommandId --output text > "$OUT/relay-pull-id" 2>/dev/null || true
  sleep 8
  aws ssm get-command-invocation --region "$REGION" --command-id "$(cat "$OUT/relay-pull-id")" --instance-id "$INST" \
    --query StandardOutputContent --output text 2>/dev/null > "$OUT/relay-pull.txt" || true
  awk '/---SERVERLOG---/{exit} {print}' "$OUT/relay-pull.txt" 2>/dev/null | base64 -d > "$OUT/relay-2333.pcap" 2>/dev/null || true
  awk 'f{print} /---SERVERLOG---/{f=1}' "$OUT/relay-pull.txt" 2>/dev/null > "$OUT/relay-server.log" || true
fi

echo "== 5/5  done. captured:"
ls -la "$OUT" | awk 'NR>1{print "  "$5, $9}'
cat <<EOF

Analyse (clocks correlate across both ends):
  Thor strace : $OUT/thor-client-strace.log   — the ISOLATED client: connect, write (noise msg1), what it reads, where it closes
  Thor pcap   : $OUT/thor-2333.pcap           — wireshark; follow the :2333 stream
  Relay pcap  : $OUT/relay-2333.pcap          — the same handshake from the relay's side
  Relay log   : $OUT/relay-server.log         — the server's view (noise EOF vs handshake timeout vs auth)

The open question: the client connects and the noise handshake does not
complete (server reads EOF) thousands of times, then recovers. This capture
shows, for ONE isolated attempt, which step fails and whether the server ever
answers msg2 — the thing the polluted strace could not.
EOF
