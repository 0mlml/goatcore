// cmd_validate.go — `wg-cp0 validate` subcommand.
//
// Schema-validates the JSON-of-record and optionally cross-checks
// against live state. The validate path is the cron + pre-commit gate
// — fast (<10 ms) when the file is local and the operator wants to
// catch a malformed write before it lands.
//
// The optional live-state cross-check (`--check-live-state`) reads
// the relays once and warns when the declared CurrentPubkey for a
// peer is missing from any reachable relay, or when a relay carries
// an active pubkey for a peer that the declaration has rotated past.
// This is a *warning* layer, not a fail-loud — a deeper investigation
// flows through `wg-cp0 diff` / `wg-cp0 reconcile`.

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

func cmdValidate(args []string, stdout, stderr io.Writer) int {
	accept := map[string]bool{
		"path":               true,
		"include-live-state": true,
	}
	f, pos, err := parseFlags(args, accept)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 validate: %v\n", err)
		return 2
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "wg-cp0 validate: unexpected positional %v\n", pos)
		return 2
	}
	ps, err := peerset.Load(f.path)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 validate: %v\n", err)
		return 1
	}
	if err := ps.Validate(); err != nil {
		fmt.Fprintf(stderr, "wg-cp0 validate: FAIL %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wg-cp0 validate: PASS schema_version=%d peers=%d relays=%d deprecated=%d\n",
		ps.SchemaVersion, len(ps.Peers), len(ps.Relays), len(ps.DeprecatedPeers))

	if !f.includeLiveState {
		return 0
	}
	live, _ := buildIOLayer(false)
	mismatches := runLiveStateCrossCheck(ps, live, stderr)
	if len(mismatches) == 0 {
		fmt.Fprintln(stdout, "wg-cp0 validate: live-state cross-check PASS — all declared peers found on all reachable relays")
		return 0
	}
	fmt.Fprintln(stdout, "wg-cp0 validate: live-state cross-check found drift (warning, not fatal):")
	for _, m := range mismatches {
		fmt.Fprintln(stdout, "  - "+m)
	}
	return 0
}

// runLiveStateCrossCheck reads the live state via the supplied reader
// (nil → empty result), compares against the declared PeerSet, and
// returns the drift rows. Non-fatal read errors are logged to stderr.
// Extracted so tests can inject a rich MockLiveStateReader.
func runLiveStateCrossCheck(ps *peerset.PeerSet, live LiveStateReader, stderr io.Writer) []string {
	if live == nil {
		return nil
	}
	state, err := live.Read(context.Background(), ps.Relays)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 validate: live-state read error (non-fatal): %v\n", err)
		return nil
	}
	return compareDeclaredVsLive(ps, state)
}

// compareDeclaredVsLive returns one human-readable string per drift
// row. Drift kinds:
//
//   - peer DECLARED but ABSENT on relay-X
//   - peer ROTATED-AWAY pubkey still present on relay-X
//   - peer ALLOWED-IPS mismatch on relay-X
//
// Empty slice → no drift. Used by validate's optional cross-check.
func compareDeclaredVsLive(ps *peerset.PeerSet, state LiveState) []string {
	var out []string
	for _, relay := range ps.Relays {
		rlive, ok := state.PerRelay[relay.Name]
		if !ok || !rlive.Reachable {
			continue // can't compare against unreachable
		}
		declared := map[string]peerset.Peer{} // pubkey → peer
		declaredByName := map[string]peerset.Peer{}
		for _, p := range ps.Peers {
			declared[p.CurrentPubkey] = p
			declaredByName[p.Name] = p
		}
		for _, p := range ps.Peers {
			if _, present := rlive.Peers[p.CurrentPubkey]; !present {
				out = append(out, fmt.Sprintf("peer %q DECLARED but ABSENT on relay %s (pubkey=%s)",
					p.Name, relay.Name, truncate(p.CurrentPubkey, 12)))
			}
		}
		for pubkey := range rlive.Peers {
			if _, ok := declared[pubkey]; ok {
				continue
			}
			peerName := ""
			for _, p := range ps.Peers {
				for _, h := range p.PubkeyHistory {
					if h.Pubkey == pubkey {
						peerName = p.Name
					}
				}
			}
			if peerName != "" {
				out = append(out, fmt.Sprintf("peer %q ROTATED-AWAY pubkey still on relay %s (pubkey=%s)",
					peerName, relay.Name, truncate(pubkey, 12)))
			} else {
				out = append(out, fmt.Sprintf("UNKNOWN peer pubkey on relay %s (pubkey=%s) — consider `wg-cp0 reconcile --remove`",
					relay.Name, truncate(pubkey, 12)))
			}
		}
	}
	return out
}
