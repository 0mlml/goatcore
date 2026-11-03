#!/usr/bin/env bash
# diagnose-relay-rla-ssm.sh — F-057 / Block 22K.8 diagnostic checklist
#
# Operator runs this on relay-rla AFTER opening SSH temporarily via
# terraform-prod ssh_cidr_blocks. Output is a single text bundle that
# captures everything needed to diagnose the ssm-agent ConnectionLost.
#
# Usage (operator's laptop):
#
#   1. Open SSH temporarily for the operator's IP:
#      cd terraform-prod
#      # Edit terraform-prod/main.tf or terraform-prod/goat-relay-prod.tf:
#      # In module.relay_rla block, set ssh_cidr_blocks = ["<operator-ip>/32"]
#      aws-vault exec usareur-af --no-session -- terraform apply
#
#   2. Push this script + run it:
#      OP_IP=<your IP>
#      scp ops/scripts/diagnose-relay-rla-ssm.sh ubuntu@<relay-rla public IP>:/tmp/
#      ssh ubuntu@<relay-rla public IP> 'sudo bash /tmp/diagnose-relay-rla-ssm.sh' | tee \
#         ~/.goat/state/F-057-diagnosis-$(date -u +%Y-%m-%dT%H%M%SZ).txt
#
#   3. Close SSH:
#      Revert ssh_cidr_blocks = [] + terraform apply
#
# Required: bash, journalctl, ip, ss, curl, dig, systemctl, wg-tools
#
# Exit codes:
#   0 — diagnostic completed (regardless of findings)
#   2 — critical step failed (e.g. journalctl unavailable)

set -euo pipefail

# Colorize? Default no — output is meant to be captured to a file.
SECTION() { printf '\n=== %s ===\n' "$1"; }
CHECK()   { printf '\n--- %s ---\n' "$1"; }
RUN()     { echo "+ $*"; "$@" 2>&1 || echo "(rc=$?)"; }

SECTION "F-057 diagnostic — relay-rla ssm-agent ConnectionLost"
date -u +"timestamp: %Y-%m-%dT%H:%M:%SZ"
echo "host: $(hostname)"
echo "uptime: $(uptime -p)"
RUN cat /etc/os-release

SECTION "ssm-agent service state + recent logs"
CHECK "service status"
RUN systemctl status amazon-ssm-agent --no-pager -l
CHECK "last 200 lines of journalctl -u amazon-ssm-agent"
RUN journalctl -u amazon-ssm-agent --no-pager -n 200
CHECK "ssm-agent error log"
if [ -f /var/log/amazon/ssm/errors.log ]; then
  RUN tail -100 /var/log/amazon/ssm/errors.log
else
  echo "(no /var/log/amazon/ssm/errors.log)"
fi

SECTION "Network state"
CHECK "interfaces"
RUN ip -br addr show
CHECK "routes (main table)"
RUN ip route show
CHECK "route to IMDS (169.254.169.254)"
RUN ip route get 169.254.169.254
CHECK "route to wg-cp0 mesh address (.1)"
RUN ip route get 198.18.0.1
CHECK "default route + ens5"
RUN ip route show default
RUN ip link show ens5

SECTION "wg-cp0 state"
CHECK "wg show wg-cp0"
RUN wg show wg-cp0
CHECK "wg-cp0 service"
RUN systemctl status wg-quick@wg-cp0 --no-pager -l
CHECK "wg-cp0 conf"
RUN cat /etc/wireguard/wg-cp0.conf

SECTION "Firewall + sysctl"
CHECK "nft list ruleset"
RUN nft list ruleset
CHECK "iptables -L FORWARD"
RUN iptables -L FORWARD -n -v
CHECK "iptables -L OUTPUT"
RUN iptables -L OUTPUT -n -v
CHECK "iptables -t nat -L"
RUN iptables -t nat -L -n -v
CHECK "ip_forward + rp_filter sysctls"
RUN sysctl net.ipv4.ip_forward net.ipv4.conf.all.rp_filter net.ipv4.conf.default.rp_filter net.ipv4.conf.ens5.rp_filter
CHECK "ip rule"
RUN ip rule show

SECTION "IMDSv2 + AWS endpoint reachability"
CHECK "IMDSv2 PUT for token (5s timeout)"
TOKEN=$(curl -fsS --max-time 5 -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 60" 2>&1 || echo "FAIL")
echo "token=$TOKEN"
if [ "$TOKEN" != "FAIL" ] && [ -n "$TOKEN" ]; then
  CHECK "IMDS GET instance-id"
  RUN curl -fsS --max-time 5 -H "X-aws-ec2-metadata-token: $TOKEN" "http://169.254.169.254/latest/meta-data/instance-id"
  CHECK "IMDS GET instance-profile credentials"
  RUN curl -fsS --max-time 5 -H "X-aws-ec2-metadata-token: $TOKEN" "http://169.254.169.254/latest/meta-data/iam/security-credentials/"
fi
CHECK "DNS resolution for ssm.eu-central-1.amazonaws.com"
RUN dig +short +time=2 ssm.eu-central-1.amazonaws.com
CHECK "TCP 443 reachability to ssm.eu-central-1.amazonaws.com"
RUN timeout 5 bash -c 'cat < /dev/tcp/ssm.eu-central-1.amazonaws.com/443 || true'
CHECK "TCP 443 reachability to ec2messages.eu-central-1.amazonaws.com"
RUN timeout 5 bash -c 'cat < /dev/tcp/ec2messages.eu-central-1.amazonaws.com/443 || true'
CHECK "TCP 443 reachability to ssmmessages.eu-central-1.amazonaws.com"
RUN timeout 5 bash -c 'cat < /dev/tcp/ssmmessages.eu-central-1.amazonaws.com/443 || true'

SECTION "Clock state (TLS handshake fails on bad clock)"
RUN timedatectl status
RUN chronyc tracking 2>&1 || systemctl status chrony --no-pager -l 2>&1 || echo "(no chrony)"

SECTION "Conntrack snapshot for AWS-internal flows"
RUN conntrack -L 2>&1 | grep -E "169\.254\.|amazonaws|443" | head -20

SECTION "Done"
echo "Capture this entire output for F-057 diagnosis. Recommended next steps:"
echo "  - If IMDS PUT failed: hop-limit issue (curl PUT failing typically means metadata-options have HttpPutResponseHopLimit < 2 OR network namespace is intercepting). Check: aws ec2 describe-instances --instance-id <fra> --query 'Reservations[].Instances[].MetadataOptions'"
echo "  - If IMDS PUT succeeded but ssm-agent still fails: ssm-agent specific (re-install + restart, or check /var/lib/amazon/ssm/Vault for stale credential cache)."
echo "  - If TCP 443 to ssm.eu-central-1 fails: SG egress / NACL / route issue."
echo "  - If clock is off: NTP fix first; AWS rejects requests with skew > 5 min."
echo "  - If nft FORWARD policy is DROP: previously-deployed rule landed; not from wg-cp0-server role per CLAUDE.md but worth verifying."
