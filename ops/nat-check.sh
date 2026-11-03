#!/usr/bin/env bash
# nat-check.sh — Block 0A: Transport NAT characterization
#
# Determines NAT type for a given transport by querying multiple STUN servers
# and comparing the external IP:port mappings. Zero external dependencies —
# uses raw STUN protocol via Python3 stdlib (socket + struct).
#
# NAT type is a property of the TRANSPORT, not the mesh. Different transports
# (the satellite link, 5G, broadband, HaLow) will have different NAT behavior.
# Run once per transport to build a matrix that informs connectivity strategy.
#
# Result interpretation:
#   Same port across all servers     → Cone NAT (stunmesh-go viable)
#   Different port per server        → Symmetric NAT (relay required)
#
# Usage:
#   bash ops/nat-check.sh                             # auto-detect transport
#   bash ops/nat-check.sh --transport "star-shield"   # label this test run
#   bash ops/nat-check.sh --transport "verizon-5g"    # label for 5G test
#
# Run from any node behind the transport you want to characterize.
# Results append to docs/reference/transport-nat-matrix.md

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TRANSPORT_LABEL=""

# Matrix file location: use repo path if available, otherwise write next to script
if [[ -d "${SCRIPT_DIR}/../docs/reference" ]]; then
    MATRIX_FILE="${SCRIPT_DIR}/../docs/reference/transport-nat-matrix.md"
elif [[ -n "${GOAT_ROOT:-}" ]] && [[ -d "${GOAT_ROOT}/docs/reference" ]]; then
    MATRIX_FILE="${GOAT_ROOT}/docs/reference/transport-nat-matrix.md"
else
    MATRIX_FILE="${SCRIPT_DIR}/transport-nat-matrix.md"
fi

# --- Args ---
while [[ $# -gt 0 ]]; do
    case "$1" in
        --transport) TRANSPORT_LABEL="$2"; shift 2 ;;
        *) echo "Unknown arg: $1"; exit 1 ;;
    esac
done

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log()  { echo -e "${CYAN}[nat-check]${NC} $*"; }
ok()   { echo -e "${GREEN}[✓]${NC} $*"; }
warn() { echo -e "${YELLOW}[!]${NC} $*"; }
err()  { echo -e "${RED}[✗]${NC} $*"; }

# --- Auto-detect transport label ---
detect_transport() {
    if [[ -n "$TRANSPORT_LABEL" ]]; then
        return
    fi

    local gw_ip
    gw_ip=$(ip route show default 2>/dev/null | awk '{print $3; exit}' \
        || route -n get default 2>/dev/null | awk '/gateway:/{print $2; exit}') || true

    if [[ -n "${NAT_CHECK_GATEWAY_LABELS:-}" ]] && label=$(printf '%s\n' $NAT_CHECK_GATEWAY_LABELS | awk -F= -v g="$gw_ip" '$1==g{print $2; exit}') && [[ -n "$label" ]]; then
        # NAT_CHECK_GATEWAY_LABELS="192.168.1.1=star-shield 10.0.0.1=5g" maps a gateway to a transport label
        TRANSPORT_LABEL="$label"
        log "Auto-detected: $label (gateway $gw_ip)"
    else
        TRANSPORT_LABEL="unknown-${gw_ip:-no-gw}"
        warn "Could not auto-detect transport. Use --transport to label this run."
        warn "Detected gateway: ${gw_ip:-none}"
    fi
}

# --- Raw STUN Binding Request (no dependencies) ---
stun_query() {
    local server="$1"
    local port="$2"
    local source_port="$3"

    python3 - "$server" "$port" "$source_port" << 'PYEOF'
import socket, struct, os, sys, json

server = sys.argv[1]
port = int(sys.argv[2])
source_port = int(sys.argv[3])

# STUN Binding Request (RFC 5389)
# Header: type=0x0001 (Binding Request), length=0, magic=0x2112A442, txn_id=random
magic = 0x2112A442
txn_id = os.urandom(12)
header = struct.pack('!HHI', 0x0001, 0, magic) + txn_id

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.settimeout(3)
try:
    sock.bind(('0.0.0.0', source_port))
except OSError:
    # Port in use, pick a nearby one
    source_port = source_port + 1
    sock.bind(('0.0.0.0', source_port))

try:
    dest = (socket.gethostbyname(server), port)
    sock.sendto(header, dest)
    data, _ = sock.recvfrom(1024)
except (socket.timeout, OSError) as e:
    print(json.dumps({'server': f'{server}:{port}', 'error': str(e)}))
    sys.exit(0)
finally:
    sock.close()

# Parse response
if len(data) < 20:
    print(json.dumps({'server': f'{server}:{port}', 'error': 'response too short'}))
    sys.exit(0)

msg_type, msg_len, resp_magic = struct.unpack('!HHI', data[:8])

if msg_type != 0x0101:  # Not a Binding Response
    print(json.dumps({'server': f'{server}:{port}', 'error': f'unexpected type 0x{msg_type:04x}'}))
    sys.exit(0)

# Walk attributes looking for XOR-MAPPED-ADDRESS (0x0020) or MAPPED-ADDRESS (0x0001)
ext_ip = None
ext_port = None
attr_type_found = None
offset = 20

while offset < len(data):
    if offset + 4 > len(data):
        break
    attr_type, attr_len = struct.unpack('!HH', data[offset:offset+4])
    attr_data = data[offset+4:offset+4+attr_len]

    if attr_type == 0x0020 and len(attr_data) >= 8:  # XOR-MAPPED-ADDRESS
        family = attr_data[1]
        if family == 0x01:  # IPv4
            xport = struct.unpack('!H', attr_data[2:4])[0]
            xaddr = struct.unpack('!I', attr_data[4:8])[0]
            ext_port = xport ^ (magic >> 16)
            ext_ip_int = xaddr ^ magic
            ext_ip = socket.inet_ntoa(struct.pack('!I', ext_ip_int))
            attr_type_found = 'XOR-MAPPED-ADDRESS'
            break

    elif attr_type == 0x0001 and len(attr_data) >= 8:  # MAPPED-ADDRESS (fallback)
        family = attr_data[1]
        if family == 0x01:  # IPv4
            ext_port = struct.unpack('!H', attr_data[2:4])[0]
            ext_ip_int = struct.unpack('!I', attr_data[4:8])[0]
            ext_ip = socket.inet_ntoa(struct.pack('!I', ext_ip_int))
            attr_type_found = 'MAPPED-ADDRESS'
            # Don't break — prefer XOR-MAPPED-ADDRESS if it comes later

    # Attributes are padded to 4-byte boundaries
    offset += 4 + attr_len + (4 - attr_len % 4) % 4

if ext_ip and ext_port:
    print(json.dumps({
        'server': f'{server}:{port}',
        'external_ip': ext_ip,
        'external_port': ext_port,
        'attr': attr_type_found,
        'source_port': source_port
    }))
else:
    print(json.dumps({'server': f'{server}:{port}', 'error': 'no mapped address in response'}))
PYEOF
}

# --- Main ---
main() {
    log "Block 0A: Transport NAT Characterization"
    log "========================================="
    log "No external dependencies — raw STUN protocol via Python3 stdlib"
    echo

    detect_transport
    echo

    log "Transport: ${TRANSPORT_LABEL}"
    log "Host: $(hostname)"
    log "Interface: $(ip route show default 2>/dev/null | awk '{print $5; exit}' || route -n get default 2>/dev/null | awk '/interface:/{print $2; exit}' || echo 'unknown')"
    echo

    # Public STUN servers (diverse providers, different ports)
    declare -a STUN_SERVERS=(
        "stun.l.google.com:19302"
        "stun1.l.google.com:19302"
        "stun.cloudflare.com:3478"
        "stun2.l.google.com:19302"
    )

    # Use the SAME source port for all queries — this is the key to the test.
    # Cone NAT: same source port → same external port regardless of destination.
    # Symmetric NAT: same source port → DIFFERENT external port per destination.
    SOURCE_PORT=54320

    declare -a EXT_PORTS=()
    declare -a EXT_IPS=()
    declare -a SERVERS_OK=()
    local failures=0

    log "Sending STUN Binding Requests from source port ${SOURCE_PORT}..."
    echo

    for entry in "${STUN_SERVERS[@]}"; do
        server="${entry%%:*}"
        port="${entry##*:}"

        log "Querying ${server}:${port}..."
        result=$(stun_query "$server" "$port" "$SOURCE_PORT" 2>/dev/null) || result=""

        if [[ -z "$result" ]]; then
            warn "  FAILED — no output"
            failures=$((failures + 1))
            continue
        fi

        # Check for error
        error=$(echo "$result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('error',''))" 2>/dev/null) || error="parse error"
        if [[ -n "$error" ]]; then
            warn "  FAILED — ${error}"
            failures=$((failures + 1))
            continue
        fi

        ip=$(echo "$result" | python3 -c "import sys,json; print(json.load(sys.stdin)['external_ip'])")
        port_mapped=$(echo "$result" | python3 -c "import sys,json; print(json.load(sys.stdin)['external_port'])")

        ok "  External: ${ip}:${port_mapped}"

        EXT_PORTS+=("$port_mapped")
        EXT_IPS+=("$ip")
        SERVERS_OK+=("${server}:${port}")
    done

    # --- Analysis ---
    echo
    log "========================================="
    log "ANALYSIS"
    log "========================================="
    echo

    if [[ ${#EXT_PORTS[@]} -lt 2 ]]; then
        err "Need at least 2 successful STUN responses to characterize NAT."
        err "Got ${#EXT_PORTS[@]}. Check network connectivity and firewall rules."
        exit 1
    fi

    local unique_ports
    unique_ports=$(printf '%s\n' "${EXT_PORTS[@]}" | sort -u | wc -l | tr -d ' ')
    local unique_ips
    unique_ips=$(printf '%s\n' "${EXT_IPS[@]}" | sort -u | wc -l | tr -d ' ')

    log "Source port (local):   ${SOURCE_PORT}"
    log "Unique external IPs:   ${unique_ips} ($(printf '%s\n' "${EXT_IPS[@]}" | sort -u | tr '\n' ' '))"
    log "Unique external ports: ${unique_ports} ($(printf '%s\n' "${EXT_PORTS[@]}" | sort -u | tr '\n' ' '))"
    log "Servers responded:     ${#EXT_PORTS[@]} of ${#STUN_SERVERS[@]}"
    echo

    # Determine verdict
    local verdict=""
    local hole_punch=""

    if [[ "$unique_ports" -eq 1 ]]; then
        # Same external port for all destinations = Cone NAT
        if [[ "$unique_ips" -eq 1 ]]; then
            verdict="Cone NAT (consistent mapping)"
            hole_punch="yes"
            echo -e "${GREEN}VERDICT: Cone NAT${NC}"
            echo "  Same source port ${SOURCE_PORT} → same external port ${EXT_PORTS[0]} for all ${#EXT_PORTS[@]} servers."
            echo "  This transport preserves port mappings across destinations."
            echo "  STUN hole-punching (stunmesh-go) is viable on this transport."
        else
            verdict="Cone NAT (multiple external IPs)"
            hole_punch="yes"
            echo -e "${GREEN}VERDICT: Cone NAT (multi-IP)${NC}"
            echo "  Port is consistent but multiple external IPs seen."
            echo "  Likely load-balanced egress. Hole-punching may work but is less reliable."
        fi
    else
        # Different external ports = Symmetric NAT
        verdict="Symmetric NAT"
        hole_punch="no"
        echo -e "${RED}VERDICT: Symmetric NAT${NC}"
        echo "  Same source port ${SOURCE_PORT} → different external ports per destination:"
        for i in "${!SERVERS_OK[@]}"; do
            echo "    ${SERVERS_OK[$i]} → ${EXT_IPS[$i]}:${EXT_PORTS[$i]}"
        done
        echo "  This transport assigns a new external port for each destination."
        echo "  STUN hole-punching will NOT work. Relay required."
    fi

    echo
    log "========================================="
    echo

    # --- Append to transport matrix ---
    local timestamp
    timestamp=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
    local host
    host=$(hostname)
    local iface
    iface=$(ip route show default 2>/dev/null | awk '{print $5; exit}' || route -n get default 2>/dev/null | awk '/interface:/{print $2; exit}' || echo 'unknown')
    local ext_ip="${EXT_IPS[0]}"
    local port_list
    port_list=$(printf '%s,' "${EXT_PORTS[@]}")
    port_list="${port_list%,}"

    # Create matrix file with header if it doesn't exist
    if [[ ! -f "$MATRIX_FILE" ]]; then
        mkdir -p "$(dirname "$MATRIX_FILE")"
        cat > "$MATRIX_FILE" <<'HEADER_EOF'
# Transport NAT Matrix

NAT type is a property of the transport, not the mesh. Each row is a test run
from a specific node on a specific transport. This matrix informs which
connectivity strategy each transport supports.

**Connectivity strategies by NAT type:**

| NAT Type | stunmesh-go (direct P2P) | VPS relay | Pre-shared WG | LAN direct |
|---|---|---|---|---|
| Cone | yes | yes | yes | yes |
| Symmetric | no | yes | yes | yes |

A transport-aware mesh node should: detect its current transport, look up the
NAT type, and select the best available connectivity strategy. This is the
basis for a future transport mediator (see implementation plan).

## Test Results

| Date | Transport | Host | Interface | External IP | NAT Type | Mapped Ports (src:54320) | Hole Punch |
|---|---|---|---|---|---|---|---|
HEADER_EOF
    fi

    # Append this result row
    echo "| ${timestamp} | ${TRANSPORT_LABEL} | ${host} | ${iface} | ${ext_ip} | ${verdict} | ${port_list} | ${hole_punch} |" >> "$MATRIX_FILE"

    ok "Result appended to: ${MATRIX_FILE}"
    echo

    # --- Strategy recommendation ---
    log "STRATEGY for transport '${TRANSPORT_LABEL}':"
    if [[ "$hole_punch" == "yes" ]]; then
        echo "  Primary:     stunmesh-go direct P2P (STUN hole-punch)"
        echo "  Alternate:   VPS relay"
        echo "  Contingency: pre-shared WireGuard bootstrap"
    else
        echo "  Primary:     VPS relay (Symmetric NAT blocks hole-punching)"
        echo "  Alternate:   pre-shared WireGuard bootstrap"
        echo "  stunmesh-go: not viable on this transport"
    fi

    echo
    log "Run again on a different transport to build the matrix:"
    log "  bash nat-check.sh --transport \"verizon-5g\""
    log "  bash nat-check.sh --transport \"local-broadband\""
    log "  bash nat-check.sh --transport \"halow-mesh\""
}

main "$@"
