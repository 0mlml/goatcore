package meshstate

import (
	"strings"
	"testing"
	"time"
)

// These cases pin the console to the sweep's answer on live shapes the
// sweep refuses (ops/scripts/lifecycle-sweep.py, --lane release): the
// page must never offer a re-run the engine then refuses, and never
// call a peer "not on hold" that the sweep treats as held.

func resolveOnePeer(t *testing.T, site string, rec *HoldRecord, groups ...string) (AccessView, PeerView) {
	t.Helper()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	name := "peer-alice-" + site + "-2026-09-01"
	peerGroups := []Group{{Name: "All"}}
	for _, g := range groups {
		peerGroups = append(peerGroups, Group{Name: g})
	}
	recs := Records{DuplicatePeerIDs: map[string]bool{}}
	if rec != nil {
		recs.All = []HoldRecord{*rec}
	}
	av := Resolve(ResolveInput{Handle: "alice", Site: site, Known: map[string]struct{}{"alice": {}},
		Records: recs, Peers: []Peer{{ID: "p-alice", Name: name, Groups: peerGroups}}, Now: now})
	if len(av.Peers) != 1 {
		t.Fatalf("got %d peer rows, want 1: %+v", len(av.Peers), av.Peers)
	}
	return av, av.Peers[0]
}

func releasingRecord(lane, holdGroup string, prior ...string) *HoldRecord {
	return &HoldRecord{Version: 1, Lane: lane, Site: "zagros", PeerID: "p-alice", PeerName: "peer-alice-zagros-2026-09-01",
		EnteredAt: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC), HoldGroup: holdGroup, PriorGroups: prior,
		Actor: "sweep", Phase: "releasing"}
}

func TestResolve_ReleasingRecordTheSweepRefusesIsInconsistent(t *testing.T) {
	cases := map[string]struct {
		rec    *HoldRecord
		groups []string
		want   string
	}{
		// lifecycle-sweep.py: "also in hold group(s) ... a release would
		// restore role groups under a live containment".
		"containment on top of a half-done release": {
			rec:    releasingRecord("dormancy", "ZAGROS-PROBATION-DEVELOPER", "ZAGROS-DEVELOPER"),
			groups: []string{"ZAGROS-PROBATION-DEVELOPER", "ZAGROS-QUARANTINED", "ZAGROS-DEVELOPER"},
			want:   "ZAGROS-QUARANTINED",
		},
		"containment after the hold group came off": {
			rec:    releasingRecord("dormancy", "ZAGROS-PROBATION-DEVELOPER", "ZAGROS-DEVELOPER"),
			groups: []string{"ZAGROS-QUARANTINED", "ZAGROS-DEVELOPER"},
			want:   "ZAGROS-QUARANTINED",
		},
		// lifecycle-sweep.py: "a release is in progress but the peer also
		// holds ..., which its record never listed".
		"group the record never listed": {
			rec:    releasingRecord("dormancy", "ZAGROS-PROBATION-DEVELOPER", "ZAGROS-DEVELOPER"),
			groups: []string{"ZAGROS-PROBATION-DEVELOPER", "ZAGROS-DEVELOPER", "ZAGROS-SENSOR"},
			want:   "ZAGROS-SENSOR",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			av, pv := resolveOnePeer(t, "zagros", tc.rec, tc.groups...)
			if pv.Verdict != "inconsistent" || !av.Inconsistent {
				t.Fatalf("verdict = %q (Inconsistent=%v), want inconsistent", pv.Verdict, av.Inconsistent)
			}
			if !strings.Contains(pv.VerdictDetail, tc.want) || !strings.Contains(pv.VerdictDetail, "refuses") {
				t.Fatalf("detail = %q, want it to name %s and the refusal", pv.VerdictDetail, tc.want)
			}
			if strings.Contains(pv.VerdictDetail, "--lane release") {
				t.Fatalf("detail = %q offers the re-run the sweep refuses", pv.VerdictDetail)
			}
		})
	}
}

// The sweep refuses every security record whatever its phase (ADR
// 1091), so the page names the containment's close-out, not the sweep.
func TestResolve_ReleasingSecurityRecordIsNotSentToTheSweep(t *testing.T) {
	av, pv := resolveOnePeer(t, "zagros", releasingRecord("security", "ZAGROS-QUARANTINED", "ZAGROS-OBSERVER"),
		"ZAGROS-QUARANTINED", "ZAGROS-OBSERVER")
	if pv.Verdict != "releasing" || av.Inconsistent {
		t.Fatalf("verdict = %q (Inconsistent=%v), want releasing", pv.Verdict, av.Inconsistent)
	}
	if strings.Contains(pv.VerdictDetail, "lifecycle-sweep.py") || !strings.Contains(pv.VerdictDetail, "ADR 1091") {
		t.Fatalf("detail = %q, want the ADR 1091 close-out and no sweep command", pv.VerdictDetail)
	}
}

// A hold group under an earlier site id is still a hold to the sweep
// (hold_groups_of matches any -PROBATION- or -QUARANTINED group).
func TestResolve_HoldGroupUnderAnEarlierSiteIDIsStillAHold(t *testing.T) {
	for _, g := range []string{"ZAGROS-QUARANTINED", "ZAGROS-PROBATION-DEVELOPER", "zagros-quarantined"} {
		t.Run(g, func(t *testing.T) {
			_, pv := resolveOnePeer(t, "zagros-dev", nil, g)
			if pv.Verdict != "on_hold_no_record" || !strings.Contains(pv.VerdictDetail, g) {
				t.Fatalf("verdict = %q detail = %q, want on_hold_no_record naming %s", pv.Verdict, pv.VerdictDetail, g)
			}
		})
	}
	av, pv := resolveOnePeer(t, "zagros-dev", nil, "ZAGROS-DEV-PROBATION-DEVELOPER", "ZAGROS-QUARANTINED")
	if pv.Verdict != "inconsistent" || !av.Inconsistent || !strings.Contains(pv.VerdictDetail, "more than one hold group") {
		t.Fatalf("verdict = %q detail = %q, want two hold groups inconsistent", pv.Verdict, pv.VerdictDetail)
	}
}
