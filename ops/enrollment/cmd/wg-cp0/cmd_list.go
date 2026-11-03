// cmd_list.go — `wg-cp0 list` subcommand.
//
// Read-only enumeration of the active peer set. Supports site + role
// filters, --include-deprecated for the soft-delete tier, and
// --include-live-state to surface the per-peer last-handshake
// timestamp from whichever reachable relay has the freshest reading.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

func cmdList(args []string, stdout, stderr io.Writer) int {
	accept := map[string]bool{
		"path":               true,
		"site":               true,
		"role":               true,
		"include-deprecated": true,
		"include-live-state": true,
		"json":               true,
	}
	f, pos, err := parseFlags(args, accept)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 list: %v\n", err)
		return 2
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "wg-cp0 list: unexpected positional %v\n", pos)
		return 2
	}
	ps, err := peerset.Load(f.path)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 list: %v\n", err)
		return 1
	}
	rows := filterPeers(ps.Peers, f.site, f.role)

	// When --include-live-state, read the live state once + index by
	// pubkey for per-peer freshest-handshake lookup.
	var freshest map[string]time.Time
	if f.includeLiveState {
		live, _ := buildIOLayer(false)
		freshest = readFreshestHandshakes(ps, live)
	}

	if f.asJSON {
		type listRow struct {
			peerset.Peer
			LastHandshake *time.Time `json:"last_handshake,omitempty"`
		}
		out := make([]listRow, 0, len(rows))
		for _, p := range rows {
			row := listRow{Peer: p}
			if freshest != nil {
				if ts, ok := freshest[p.CurrentPubkey]; ok && !ts.IsZero() {
					t := ts
					row.LastHandshake = &t
				}
			}
			out = append(out, row)
		}
		payload := struct {
			SchemaVersion int                      `json:"schema_version"`
			Peers         []listRow                `json:"peers"`
			Deprecated    []peerset.DeprecatedPeer `json:"deprecated_peers,omitempty"`
		}{ps.SchemaVersion, out, nil}
		if f.includeDeprecated {
			payload.Deprecated = ps.DeprecatedPeers
		}
		b, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "wg-cp0 list: encode output: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(b))
		return 0
	}

	// Text output: existing header + an optional LAST_HANDSHAKE column
	// when --include-live-state.
	header := fmt.Sprintf("%-32s  %-16s  %-12s  %-12s  %s", "NAME", "WG_CP0_ADDRESS", "SITE", "ROLE", "CURRENT_PUBKEY")
	if f.includeLiveState {
		header += "  LAST_HANDSHAKE"
	}
	fmt.Fprintln(stdout, header)
	for _, p := range rows {
		line := fmt.Sprintf("%-32s  %-16s  %-12s  %-12s  %s",
			p.Name, p.WGCP0Address, dash(p.Site), dash(p.Role), truncate(p.CurrentPubkey, 22))
		if f.includeLiveState {
			ts, ok := freshest[p.CurrentPubkey]
			switch {
			case !ok:
				line += "  -"
			case ts.IsZero():
				line += "  never"
			default:
				line += "  " + ts.Format(time.RFC3339)
			}
		}
		fmt.Fprintln(stdout, line)
	}
	if f.includeDeprecated && len(ps.DeprecatedPeers) > 0 {
		fmt.Fprintln(stdout)
		fmt.Fprintf(stdout, "DEPRECATED PEERS (%d):\n", len(ps.DeprecatedPeers))
		for _, dp := range ps.DeprecatedPeers {
			fmt.Fprintf(stdout, "  - %-32s removed_at=%s reason=%q\n",
				dp.Name, dp.RemovedAt.Format("2006-01-02T15:04:05Z"), dp.RemovedReason)
		}
	}
	return 0
}

// readFreshestHandshakes returns a map of CurrentPubkey →
// freshest-known LastHandshake from any reachable relay. Missing
// entries (peer not present on any reachable relay) are absent;
// zero-timestamp values (peer present but never handshaked) are
// represented as time.Time{} (the caller distinguishes "absent"
// from "zero"). The reader is injected so tests can substitute a
// rich MockLiveStateReader without going through buildIOLayer.
func readFreshestHandshakes(ps *peerset.PeerSet, live LiveStateReader) map[string]time.Time {
	if live == nil {
		return nil
	}
	state, err := live.Read(context.Background(), ps.Relays)
	if err != nil {
		return nil
	}
	freshest := map[string]time.Time{}
	for _, relay := range ps.Relays {
		rlive, ok := state.PerRelay[relay.Name]
		if !ok || !rlive.Reachable {
			continue
		}
		for pubkey, lp := range rlive.Peers {
			cur, seen := freshest[pubkey]
			if !seen || lp.LastHandshake.After(cur) {
				freshest[pubkey] = lp.LastHandshake
			}
		}
	}
	return freshest
}
