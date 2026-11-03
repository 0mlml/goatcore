package meshstate

import (
	"context"
	"testing"
	"time"
)

// fixturePath is the hold-records fixture shipped with the
// lifecycle schema (story #1045).
const fixturePath = "../../../lifecycle/fixtures/hold-records.jsonl"

func TestLoadHoldRecords_Fixture(t *testing.T) {
	recs, err := LoadHoldRecords(fixturePath)
	if err != nil {
		t.Fatalf("LoadHoldRecords(%q) error = %v", fixturePath, err)
	}
	if len(recs.All) != 2 {
		t.Fatalf("got %d records, want 2", len(recs.All))
	}
	r0 := recs.All[0]
	if r0.Lane != "dormancy" || r0.Site != "zagros" {
		t.Fatalf("first record lane/site = %q/%q, want dormancy/zagros", r0.Lane, r0.Site)
	}
	if len(r0.PriorGroups) == 0 {
		t.Fatalf("first record has no prior_groups; want at least one")
	}
}

func TestResolve_OnHoldFromFixture(t *testing.T) {
	recs, err := LoadHoldRecords(fixturePath)
	if err != nil {
		t.Fatalf("LoadHoldRecords(%q) error = %v", fixturePath, err)
	}
	if len(recs.All) != 2 {
		t.Fatalf("got %d records, want 2", len(recs.All))
	}

	// Synthetic peers that place each fixture member in their
	// hold group and in no prior group: the RECORD is the restore
	// content and the live groups carry only the hold lane.
	peers := []Peer{}
	known := map[string]struct{}{}
	for _, r := range recs.All {
		handle := r.HandleString()
		if handle == "" {
			t.Fatalf("fixture record for peer %s has empty handle", r.PeerID)
		}
		known[handle] = struct{}{}
		peers = append(peers, Peer{
			ID:        r.PeerID,
			Name:      r.PeerName,
			Connected: false,
			// Only the hold group and the implicit All group.
			Groups: []Group{{Name: r.HoldGroup}, {Name: "All"}},
		})
	}

	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

	for _, r := range recs.All {
		handle := r.HandleString()
		t.Run(handle, func(t *testing.T) {
			av := Resolve(ResolveInput{
				Handle:     handle,
				Site:       "zagros",
				Known:      known,
				Records:    recs,
				RecordsErr: nil,
				Peers:      peers,
				PeersErr:   nil,
				Keys:       nil,
				KeysErr:    nil,
				Now:        now,
			})
			if av.Inconsistent {
				t.Fatalf("AccessView.Inconsistent = true (%s), want false", av.InconsistentDetail)
			}
			if len(av.Peers) == 0 {
				t.Fatalf("no peers in AccessView for handle %q", handle)
			}
			var pv *PeerView
			for i := range av.Peers {
				if av.Peers[i].ID == r.PeerID {
					pv = &av.Peers[i]
					break
				}
			}
			if pv == nil {
				t.Fatalf("no PeerView for peer_id %s", r.PeerID)
			}
			if pv.Verdict != "on_hold" {
				t.Fatalf("peer verdict = %q, want on_hold", pv.Verdict)
			}
			if pv.Lane != r.Lane {
				t.Fatalf("peer lane = %q, want %q", pv.Lane, r.Lane)
			}
			if pv.HoldGroup != r.HoldGroup {
				t.Fatalf("hold_group = %q, want %q", pv.HoldGroup, r.HoldGroup)
			}
			if len(pv.PriorGroups) != len(r.PriorGroups) {
				t.Fatalf("prior_groups len = %d, want %d", len(pv.PriorGroups), len(r.PriorGroups))
			}
		})
	}
}

func TestResolve_TruthTableRows(t *testing.T) {
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	known := map[string]struct{}{"alice": {}}

	baseRec := HoldRecord{
		Version:     1,
		Lane:        "dormancy",
		Site:        "zagros",
		PeerID:      "peer-1",
		PeerName:    "peer-alice-zagros-2026-09-01-laptop",
		Handle:      ptr("alice"),
		EnteredAt:   time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
		HoldGroup:   "ZAGROS-PROBATION-DEVELOPER",
		PriorGroups: []string{"ZAGROS-DEVELOPER"},
		Actor:       "sweep",
	}

	t.Run("record+peer+hold+no_prior → on_hold", func(t *testing.T) {
		in := ResolveInput{
			Handle: "alice",
			Site:   "zagros",
			Known:  known,
			Records: Records{
				All:              []HoldRecord{baseRec},
				DuplicatePeerIDs: map[string]bool{},
			},
			Peers: []Peer{{
				ID:   baseRec.PeerID,
				Name: baseRec.PeerName,
				Groups: []Group{
					{Name: baseRec.HoldGroup},
					{Name: "All"},
				},
			}},
			Now: now,
		}
		av := Resolve(in)
		if len(av.Peers) != 1 {
			t.Fatalf("len(Peers) = %d, want 1", len(av.Peers))
		}
		if av.Peers[0].Verdict != "on_hold" {
			t.Fatalf("verdict = %q, want on_hold", av.Peers[0].Verdict)
		}
	})

	t.Run("record+peer+hold+prior → inconsistent", func(t *testing.T) {
		in := ResolveInput{
			Handle: "alice",
			Site:   "zagros",
			Known:  known,
			Records: Records{
				All:              []HoldRecord{baseRec},
				DuplicatePeerIDs: map[string]bool{},
			},
			Peers: []Peer{{
				ID:   baseRec.PeerID,
				Name: baseRec.PeerName,
				Groups: []Group{
					{Name: baseRec.HoldGroup},
					{Name: baseRec.PriorGroups[0]},
				},
			}},
			Now: now,
		}
		av := Resolve(in)
		if !av.Inconsistent {
			t.Fatalf("Inconsistent = false, want true")
		}
		if len(av.Peers) != 1 || av.Peers[0].Verdict != "inconsistent" {
			t.Fatalf("peer verdict = %q, want inconsistent", av.Peers[0].Verdict)
		}
	})

	t.Run("record+peer+no_hold → inconsistent", func(t *testing.T) {
		in := ResolveInput{
			Handle: "alice",
			Site:   "zagros",
			Known:  known,
			Records: Records{
				All:              []HoldRecord{baseRec},
				DuplicatePeerIDs: map[string]bool{},
			},
			Peers: []Peer{{
				ID:     baseRec.PeerID,
				Name:   baseRec.PeerName,
				Groups: []Group{{Name: baseRec.PriorGroups[0]}},
			}},
			Now: now,
		}
		av := Resolve(in)
		if !av.Inconsistent {
			t.Fatalf("Inconsistent = false, want true")
		}
		if av.Peers[0].Verdict != "inconsistent" {
			t.Fatalf("peer verdict = %q, want inconsistent", av.Peers[0].Verdict)
		}
	})

	t.Run("record+no_peer → inconsistent", func(t *testing.T) {
		in := ResolveInput{
			Handle: "alice",
			Site:   "zagros",
			Known:  known,
			Records: Records{
				All:              []HoldRecord{baseRec},
				DuplicatePeerIDs: map[string]bool{},
			},
			Peers: nil,
			Now:   now,
		}
		av := Resolve(in)
		if !av.Inconsistent {
			t.Fatalf("Inconsistent = false, want true")
		}
		if len(av.Peers) != 1 || av.Peers[0].Verdict != "inconsistent" {
			t.Fatalf("peer verdict = %q, want inconsistent", av.Peers[0].Verdict)
		}
	})

	t.Run("duplicate records → inconsistent", func(t *testing.T) {
		dupRecs := Records{
			All: []HoldRecord{baseRec, baseRec},
			DuplicatePeerIDs: map[string]bool{
				baseRec.PeerID: true,
			},
		}
		in := ResolveInput{
			Handle:  "alice",
			Site:    "zagros",
			Known:   known,
			Records: dupRecs,
			Peers: []Peer{{
				ID:     baseRec.PeerID,
				Name:   baseRec.PeerName,
				Groups: []Group{{Name: baseRec.HoldGroup}},
			}},
			Now: now,
		}
		av := Resolve(in)
		if !av.Inconsistent {
			t.Fatalf("Inconsistent = false, want true")
		}
		if len(av.Peers) == 0 || av.Peers[0].Verdict != "inconsistent" {
			t.Fatalf("peer verdict = %q, want inconsistent", av.Peers[0].Verdict)
		}
	})

	t.Run("no record + hold group → on_hold_no_record", func(t *testing.T) {
		in := ResolveInput{
			Handle: "alice",
			Site:   "zagros",
			Known:  known,
			Records: Records{
				All:              nil,
				DuplicatePeerIDs: map[string]bool{},
			},
			Peers: []Peer{{
				ID:     baseRec.PeerID,
				Name:   baseRec.PeerName,
				Groups: []Group{{Name: baseRec.HoldGroup}},
			}},
			Now: now,
		}
		av := Resolve(in)
		if len(av.Peers) != 1 {
			t.Fatalf("len(Peers) = %d, want 1", len(av.Peers))
		}
		if av.Peers[0].Verdict != "on_hold_no_record" {
			t.Fatalf("verdict = %q, want on_hold_no_record", av.Peers[0].Verdict)
		}
	})

	t.Run("no record + no hold group → not_on_hold", func(t *testing.T) {
		in := ResolveInput{
			Handle: "alice",
			Site:   "zagros",
			Known:  known,
			Records: Records{
				All:              nil,
				DuplicatePeerIDs: map[string]bool{},
			},
			Peers: []Peer{{
				ID:     baseRec.PeerID,
				Name:   baseRec.PeerName,
				Groups: []Group{{Name: "ZAGROS-DEVELOPER"}},
			}},
			Now: now,
		}
		av := Resolve(in)
		if len(av.Peers) != 1 {
			t.Fatalf("len(Peers) = %d, want 1", len(av.Peers))
		}
		if av.Peers[0].Verdict != "not_on_hold" {
			t.Fatalf("verdict = %q, want not_on_hold", av.Peers[0].Verdict)
		}
	})

	t.Run("records unavailable → mesh renders, no healthy verdict", func(t *testing.T) {
		in := ResolveInput{
			Handle:     "alice",
			Site:       "zagros",
			Known:      known,
			RecordsErr: context.DeadlineExceeded,
			Peers: []Peer{{
				ID:     baseRec.PeerID,
				Name:   baseRec.PeerName,
				Groups: []Group{{Name: "ZAGROS-DEVELOPER"}},
			}},
			Now: now,
		}
		av := Resolve(in)
		if av.RecordsAvailable {
			t.Fatalf("RecordsAvailable = true, want false")
		}
		if !av.MeshAvailable {
			t.Fatalf("MeshAvailable = false, want true")
		}
		if len(av.Peers) != 1 {
			t.Fatalf("len(Peers) = %d, want 1", len(av.Peers))
		}
		if av.Peers[0].Verdict == "not_on_hold" {
			t.Fatalf("verdict = not_on_hold with records unavailable; want degraded state")
		}
	})

	t.Run("records unavailable + hold group → in_hold_group", func(t *testing.T) {
		in := ResolveInput{
			Handle:     "alice",
			Site:       "zagros",
			Known:      known,
			RecordsErr: context.DeadlineExceeded,
			Peers: []Peer{{
				ID:   baseRec.PeerID,
				Name: baseRec.PeerName,
				Groups: []Group{{
					Name: baseRec.HoldGroup,
				}},
			}},
			Now: now,
		}
		av := Resolve(in)
		if av.RecordsAvailable {
			t.Fatalf("RecordsAvailable = true, want false")
		}
		if !av.MeshAvailable {
			t.Fatalf("MeshAvailable = false, want true")
		}
		if len(av.Peers) != 1 {
			t.Fatalf("len(Peers) = %d, want 1", len(av.Peers))
		}
		pv := av.Peers[0]
		if pv.Verdict != "in_hold_group" {
			t.Fatalf("verdict = %q, want in_hold_group", pv.Verdict)
		}
		wantDetail := "peer in hold group " + baseRec.HoldGroup + "; hold record unavailable, lane unknown"
		if pv.VerdictDetail != wantDetail {
			t.Fatalf("VerdictDetail = %q, want %q", pv.VerdictDetail, wantDetail)
		}
	})

	t.Run("mesh unavailable → no record is attributed by name alone", func(t *testing.T) {
		recs := Records{
			All:              []HoldRecord{baseRec},
			DuplicatePeerIDs: map[string]bool{},
		}
		in := ResolveInput{
			Handle:     "alice",
			Site:       "zagros",
			Known:      known,
			Records:    recs,
			RecordsErr: nil,
			PeersErr:   context.DeadlineExceeded,
			Now:        now,
		}
		av := Resolve(in)
		if !av.RecordsAvailable {
			t.Fatalf("RecordsAvailable = false, want true")
		}
		if av.MeshAvailable {
			t.Fatalf("MeshAvailable = true, want false")
		}
		// The record's handle matches this member, but with no peer
		// list there is nothing to own the record against, and a hold
		// credited by name alone is the F-358 mistake; the section
		// reads "mesh state unavailable" instead.
		if len(av.Peers) != 0 {
			t.Fatalf("len(Peers) = %d, want 0: %+v", len(av.Peers), av.Peers)
		}
		if av.Inconsistent {
			t.Fatalf("Inconsistent = true, want false")
		}
	})

	t.Run("duplicate records isolated per peer", func(t *testing.T) {
		peerA := baseRec
		peerA.PeerID = "peer-A"
		peerA.PeerName = "peer-alice-A-zagros"
		peerB := baseRec
		peerB.PeerID = "peer-B"
		peerB.PeerName = "peer-alice-B-zagros"

		dupRecs := Records{
			All: []HoldRecord{peerA, peerA, peerB},
			DuplicatePeerIDs: map[string]bool{
				peerA.PeerID: true,
			},
		}
		in := ResolveInput{
			Handle:  "alice",
			Site:    "zagros",
			Known:   known,
			Records: dupRecs,
			Peers: []Peer{
				{
					ID:   peerA.PeerID,
					Name: peerA.PeerName,
					Groups: []Group{{
						Name: peerA.HoldGroup,
					}},
				},
				{
					ID:   peerB.PeerID,
					Name: peerB.PeerName,
					Groups: []Group{{
						Name: peerB.HoldGroup,
					}},
				},
			},
			Now: now,
		}
		av := Resolve(in)
		if !av.Inconsistent {
			t.Fatalf("Inconsistent = false, want true")
		}
		var aVerdict, bVerdict string
		for _, pv := range av.Peers {
			switch pv.ID {
			case peerA.PeerID:
				if aVerdict == "" {
					aVerdict = pv.Verdict
				}
			case peerB.PeerID:
				if bVerdict == "" {
					bVerdict = pv.Verdict
				}
			}
		}
		if aVerdict != "inconsistent" {
			t.Fatalf("peer A verdict = %q, want inconsistent", aVerdict)
		}
		if bVerdict != "on_hold" {
			t.Fatalf("peer B verdict = %q, want on_hold", bVerdict)
		}
	})
}

func ptr(s string) *string { return &s }
