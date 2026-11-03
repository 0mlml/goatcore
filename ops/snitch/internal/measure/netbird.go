// Package measure implements network measurement probes.
package measure

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/snitch/internal/types"
)

// NetbirdSocketPath is the canonical path of the netbird daemon's gRPC
// socket. When this socket is missing, the netbird CLI enters an
// indefinite retry loop emitting WARNING log lines once per second; a
// snitch probe shelling out to the CLI in that state hangs the entire
// probe cycle. NetbirdStatus checks for the socket BEFORE invoking the
// CLI to fail fast in the absence-case.
const NetbirdSocketPath = "/var/run/netbird.sock"

// netbirdStatusHardTimeout caps the netbird subprocess call independent
// of any caller-supplied context. exec.CommandContext + cmd.Output()
// is not always reliable at killing child processes that fork
// grandchildren (the netbird CLI's gRPC retry routine spawns goroutines
// that reset internal connection state) — observed on Tegra L4T 5.15
// arm64 where the parent ctx's 30s deadline did not reliably terminate
// a hung netbird CLI. Hard ceiling here is the load-bearing safeguard.
const netbirdStatusHardTimeout = 3 * time.Second

// NetbirdStatus runs `netbird status --json` and parses the structured
// output. Returns a wrapped error in three failure modes:
//
//   - the netbird daemon socket is absent (F-098 fast-fail; saves 14+
//     min hang per cycle observed on hosts where netbird isn't enrolled)
//   - the subprocess takes longer than netbirdStatusHardTimeout (3s,
//     independent of caller context — defense against exec.CommandContext
//     not killing CLI grandchildren on Tegra arm64)
//   - the CLI returned non-zero or stdout doesn't parse as JSON
func NetbirdStatus(ctx context.Context) (*types.NetbirdStatusJSON, error) {
	// Fast-fail when the daemon socket is missing — saves the indefinite
	// gRPC retry loop the netbird CLI enters.
	if _, err := os.Stat(NetbirdSocketPath); err != nil {
		return nil, fmt.Errorf("netbird daemon socket %s missing (skipping CLI to avoid retry-loop hang): %w", NetbirdSocketPath, err)
	}

	// Hard timeout that supersedes any laxer caller ctx. We always pick
	// the EARLIER of the two deadlines (caller's ctx OR our hard cap).
	subCtx, cancel := context.WithTimeout(ctx, netbirdStatusHardTimeout)
	defer cancel()

	cmd := exec.CommandContext(subCtx, "netbird", "status", "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("netbird status --json: %w", err)
	}
	var status types.NetbirdStatusJSON
	if err := json.Unmarshal(out, &status); err != nil {
		return nil, fmt.Errorf("parse netbird status: %w", err)
	}
	return &status, nil
}

// PeersFromStatus extracts connected peer info from netbird status.
func PeersFromStatus(status *types.NetbirdStatusJSON) []PeerInfo {
	var peers []PeerInfo
	for _, p := range status.Peers.Details {
		ip := p.NetbirdIP
		// Strip CIDR suffix if present (e.g. "100.127.1.32/16" → "100.127.1.32")
		if idx := strings.IndexByte(ip, '/'); idx >= 0 {
			ip = ip[:idx]
		}
		peers = append(peers, PeerInfo{
			Name:      p.FQDN,
			IP:        ip,
			Connected: strings.EqualFold(p.Status, "Connected"),
			Direct:    strings.EqualFold(p.ConnType, "P2P"),
			ICELocal:  p.IceCandidateType.Local,
			ICERemote: p.IceCandidateType.Remote,
		})
	}
	return peers
}

// LocalPeerFromStatus extracts this peer's identity.
func LocalPeerFromStatus(status *types.NetbirdStatusJSON) (name, ip string) {
	local := status.LocalPeerState()
	ip = local.IP
	if idx := strings.IndexByte(ip, '/'); idx >= 0 {
		ip = ip[:idx]
	}
	name = local.FQDN
	return name, ip
}

// PeerInfo is a simplified peer record from netbird status.
type PeerInfo struct {
	Name      string
	IP        string
	Connected bool
	Direct    bool
	ICELocal  string // "host", "srflx", "relay"
	ICERemote string
}
