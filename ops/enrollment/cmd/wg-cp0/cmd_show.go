// cmd_show.go — `wg-cp0 show` subcommand.
//
// Print one peer's full record including pubkey-history. With
// --include-live-state, append a "live state" section showing which
// reachable relays carry the peer's CurrentPubkey and that relay's
// last-handshake timestamp for this peer.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

func cmdShow(args []string, stdout, stderr io.Writer) int {
	accept := map[string]bool{
		"path":               true,
		"json":               true,
		"include-live-state": true,
	}
	f, pos, err := parseFlags(args, accept)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 show: %v\n", err)
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "wg-cp0 show: requires exactly one positional arg <peer-name>")
		return 2
	}
	name := pos[0]
	ps, err := peerset.Load(f.path)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 show: %v\n", err)
		return 1
	}

	var (
		peer   *peerset.Peer
		status string
	)
	if p, ok := ps.Lookup(name); ok {
		peer = p
		status = "active"
	} else if dp, ok := ps.LookupDeprecated(name); ok {
		peer = &dp.Peer
		status = fmt.Sprintf("deprecated (removed_at=%s reason=%q)",
			dp.RemovedAt.Format("2006-01-02T15:04:05Z"), dp.RemovedReason)
	} else {
		fmt.Fprintf(stderr, "wg-cp0 show: peer %q not found in active or deprecated set\n", name)
		return 1
	}

	var liveRows []showLiveRow
	if f.includeLiveState && status == "active" {
		live, _ := buildIOLayer(false)
		liveRows = readPeerLiveState(ps, peer.CurrentPubkey, live)
	}

	return emitPeer(peer, status, f.asJSON, liveRows, stdout)
}

// showLiveRow is one relay's view of the peer at show time.
type showLiveRow struct {
	Relay         string    `json:"relay"`
	Reachable     bool      `json:"reachable"`
	PubkeyPresent bool      `json:"pubkey_present"`
	LastHandshake time.Time `json:"last_handshake,omitempty"`
	Error         string    `json:"error,omitempty"`
}

func emitPeer(p *peerset.Peer, status string, asJSON bool, liveRows []showLiveRow, stdout io.Writer) int {
	if asJSON {
		type showWire struct {
			peerset.Peer
			Status    string        `json:"status"`
			LiveState []showLiveRow `json:"live_state,omitempty"`
		}
		out := showWire{Peer: *p, Status: status, LiveState: liveRows}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			fmt.Fprintf(stdout, "wg-cp0 show: encode output: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	fmt.Fprintf(stdout, "name:            %s\n", p.Name)
	fmt.Fprintf(stdout, "status:          %s\n", status)
	fmt.Fprintf(stdout, "wg_cp0_address:  %s\n", p.WGCP0Address)
	fmt.Fprintf(stdout, "current_pubkey:  %s\n", p.CurrentPubkey)
	fmt.Fprintf(stdout, "site:            %s\n", dash(p.Site))
	fmt.Fprintf(stdout, "role:            %s\n", dash(p.Role))
	if p.BundleExpiry != nil {
		fmt.Fprintf(stdout, "bundle_expiry:   %s\n", p.BundleExpiry.Format("2006-01-02T15:04:05Z"))
	}
	if len(p.PubkeyHistory) > 0 {
		fmt.Fprintf(stdout, "pubkey_history:\n")
		for _, h := range p.PubkeyHistory {
			fmt.Fprintf(stdout, "  - %s rotated_at=%s reason=%q\n",
				truncate(h.Pubkey, 22), h.RotatedAt.Format("2006-01-02T15:04:05Z"), h.Reason)
		}
	}
	if len(liveRows) > 0 {
		fmt.Fprintln(stdout, "live_state:")
		for _, r := range liveRows {
			switch {
			case !r.Reachable:
				fmt.Fprintf(stdout, "  - %-16s UNREACHABLE (%s)\n", r.Relay, r.Error)
			case r.PubkeyPresent && !r.LastHandshake.IsZero():
				fmt.Fprintf(stdout, "  - %-16s PRESENT  last_handshake=%s\n", r.Relay, r.LastHandshake.Format(time.RFC3339))
			case r.PubkeyPresent:
				fmt.Fprintf(stdout, "  - %-16s PRESENT  last_handshake=never\n", r.Relay)
			default:
				fmt.Fprintf(stdout, "  - %-16s ABSENT\n", r.Relay)
			}
		}
	}
	return 0
}

// readPeerLiveState returns one showLiveRow per declared relay,
// describing whether that relay currently carries the given pubkey
// + the LastHandshake the relay reports. Reads live state once via
// the injected LiveStateReader (so tests can substitute a rich
// MockLiveStateReader).
func readPeerLiveState(ps *peerset.PeerSet, pubkey string, live LiveStateReader) []showLiveRow {
	if live == nil {
		return nil
	}
	state, err := live.Read(context.Background(), ps.Relays)
	if err != nil {
		return nil
	}
	rows := make([]showLiveRow, 0, len(ps.Relays))
	for _, relay := range ps.Relays {
		row := showLiveRow{Relay: relay.Name}
		rlive, ok := state.PerRelay[relay.Name]
		if !ok {
			row.Reachable = false
			row.Error = "not in live-state snapshot"
			rows = append(rows, row)
			continue
		}
		row.Reachable = rlive.Reachable
		row.Error = rlive.UnreachableErr
		if rlive.Reachable {
			lp, present := rlive.Peers[pubkey]
			row.PubkeyPresent = present
			if present {
				row.LastHandshake = lp.LastHandshake
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Relay < rows[j].Relay })
	return rows
}
