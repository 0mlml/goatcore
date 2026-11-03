package peerset

import (
	"encoding/json"
	"fmt"
)

// v1Entry mirrors today's flat-array shape (one row per active client)
// written by bundle-create --update-allowlist prior to schema v2. We
// keep the struct definition narrowly scoped to this file so the rest
// of the package never touches the legacy shape.
type v1Entry struct {
	Name    string `json:"name"`
	Pubkey  string `json:"pubkey"`
	Address string `json:"address"`
}

// migrateV1 parses a v1 flat array and returns the equivalent v2
// envelope. The translation preserves name + current_pubkey +
// wg_cp0_address one-to-one; no field that v1 carries is dropped, and
// no v2-only field is fabricated. Auto-migration runs on Load so a
// v1-shaped operator file is usable without an explicit migration
// step, and the file remains v1 on disk until the caller invokes Save.
func migrateV1(data []byte) (*PeerSet, error) {
	var entries []v1Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse v1 array: %w", err)
	}
	ps := emptyV2()
	for i, e := range entries {
		if e.Name == "" {
			return nil, fmt.Errorf("entry %d: empty name", i)
		}
		if e.Pubkey == "" {
			return nil, fmt.Errorf("entry %d (%s): empty pubkey", i, e.Name)
		}
		if e.Address == "" {
			return nil, fmt.Errorf("entry %d (%s): empty address", i, e.Name)
		}
		ps.Peers = append(ps.Peers, Peer{
			Name:          e.Name,
			WGCP0Address:  e.Address,
			CurrentPubkey: e.Pubkey,
		})
	}
	return ps, nil
}
