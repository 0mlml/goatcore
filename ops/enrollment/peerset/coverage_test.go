package peerset

import (
	"errors"
	"testing"
)

// TestRotatePubkey_EmptyNewPubkey covers the early validation branch
// (mutate.go RotatePubkey empty-newPubkey guard).
func TestRotatePubkey_EmptyNewPubkey(t *testing.T) {
	ps := emptyV2()
	_ = ps.Upsert(samplePeer("a", "P", "198.18.0.1"))
	err := ps.RotatePubkey("a", "", "drill")
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("err = %v, want ErrSchemaInvalid", err)
	}
}

// TestValidate_DirectShapeViolations constructs invalid PeerSets that
// bypass Upsert (which would reject them at insert) and ensures
// Validate catches each per-field shape error. Hits validate.go
// lines 34-42 (empty name / pubkey / address).
func TestValidate_DirectShapeViolations(t *testing.T) {
	cases := []struct {
		name string
		ps   *PeerSet
	}{
		{
			name: "empty peer name",
			ps: func() *PeerSet {
				p := emptyV2()
				p.Peers = []Peer{{Name: "", CurrentPubkey: "P", WGCP0Address: "A"}}
				return p
			}(),
		},
		{
			name: "empty current_pubkey",
			ps: func() *PeerSet {
				p := emptyV2()
				p.Peers = []Peer{{Name: "n", CurrentPubkey: "", WGCP0Address: "A"}}
				return p
			}(),
		},
		{
			name: "empty wg_cp0_address",
			ps: func() *PeerSet {
				p := emptyV2()
				p.Peers = []Peer{{Name: "n", CurrentPubkey: "P", WGCP0Address: ""}}
				return p
			}(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.ps.Validate(); !errors.Is(err, ErrSchemaInvalid) {
				t.Fatalf("err = %v, want ErrSchemaInvalid", err)
			}
		})
	}
}

// TestUpsert_NonReplacementPathExitsLoop ensures Upsert correctly
// continues past unrelated peers and exercises the loop-skip branch.
func TestUpsert_NonReplacementPathExitsLoop(t *testing.T) {
	ps := emptyV2()
	_ = ps.Upsert(samplePeer("first", "P1", "198.18.0.1"))
	if err := ps.Upsert(samplePeer("second", "P2", "198.18.0.2")); err != nil {
		t.Fatal(err)
	}
	if len(ps.Peers) != 2 {
		t.Fatalf("count = %d, want 2", len(ps.Peers))
	}
}

// TestRemove_SkipsUnmatchedPeer ensures Remove iterates past
// non-matching peers and reaches the target. This exercises the loop
// "continue" branch.
func TestRemove_SkipsUnmatchedPeer(t *testing.T) {
	ps := emptyV2()
	_ = ps.Upsert(samplePeer("first", "P1", "198.18.0.1"))
	_ = ps.Upsert(samplePeer("target", "P2", "198.18.0.2"))
	if err := ps.Remove("target", "drill"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ps.Lookup("target"); ok {
		t.Fatal("target still active")
	}
	if _, ok := ps.Lookup("first"); !ok {
		t.Fatal("unrelated peer disappeared")
	}
}
