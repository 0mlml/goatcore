package meshstate

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var holdsNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func holdsKnown(handles ...string) map[string]struct{} {
	m := map[string]struct{}{}
	for _, h := range handles {
		m[h] = struct{}{}
	}
	return m
}

func holdsPeer(id, name string, lastSeen time.Time, groups ...string) Peer {
	p := Peer{ID: id, Name: name, LastSeen: lastSeen, Groups: []Group{{ID: "g-all", Name: "All"}}}
	for _, g := range groups {
		p.Groups = append(p.Groups, Group{ID: "g-" + g, Name: g})
	}
	return p
}

func holdsRecord(lane, id, name, group string, entered time.Time, prior ...string) HoldRecord {
	return HoldRecord{Version: 1, Lane: lane, Site: "zagros", PeerID: id, PeerName: name, EnteredAt: entered, HoldGroup: group, PriorGroups: prior, Actor: "sweep"}
}

func rowByPeer(t *testing.T, hv HoldsView, id string) HoldRow {
	t.Helper()
	for _, r := range hv.Rows {
		if r.PeerID == id {
			return r
		}
	}
	t.Fatalf("no row for peer %s in %+v", id, hv.Rows)
	return HoldRow{}
}

// Every State the page can show, each from its own input shape, with Kind
// graded separately: a security hold with a stale record renders both.
func TestResolveHolds_EveryStateIsItsOwnRow(t *testing.T) {
	d := func(days int) time.Time { return holdsNow.Add(-time.Duration(days) * 24 * time.Hour) }
	records := Records{
		All: []HoldRecord{
			holdsRecord("dormancy", "p-held", "peer-ann-zagros-2026-09-01-laptop", "ZAGROS-PROBATION-DEVELOPER", d(10), "ZAGROS-DEVELOPER"),
			holdsRecord("security", "p-stale", "peer-bob-zagros-2026-09-01-desktop", "ZAGROS-QUARANTINED", d(5), "ZAGROS-OBSERVER"),
			holdsRecord("dormancy", "p-incons", "peer-cat-zagros-2026-09-01-laptop", "ZAGROS-PROBATION-OBSERVER", d(3), "ZAGROS-OBSERVER"),
			holdsRecord("dormancy", "p-orphan", "peer-dan-zagros-2026-09-01-laptop", "ZAGROS-PROBATION-OBSERVER", d(20), "ZAGROS-OBSERVER"),
			func() HoldRecord {
				r := holdsRecord("dormancy", "p-rel", "peer-eve-zagros-2026-09-01-laptop", "ZAGROS-PROBATION-DEVELOPER", d(2), "ZAGROS-DEVELOPER")
				r.Phase = "releasing"
				return r
			}(),
		},
		DuplicatePeerIDs: map[string]bool{},
	}
	peers := []Peer{
		holdsPeer("p-held", "peer-ann-zagros-2026-09-01-laptop", d(30), "ZAGROS-PROBATION-DEVELOPER"),
		holdsPeer("p-stale", "peer-bob-zagros-2026-09-01-desktop", d(1), "ZAGROS-OBSERVER"),
		holdsPeer("p-incons", "peer-cat-zagros-2026-09-01-laptop", d(4), "ZAGROS-PROBATION-OBSERVER", "ZAGROS-OBSERVER"),
		holdsPeer("p-rel", "peer-eve-zagros-2026-09-01-laptop", d(2), "ZAGROS-PROBATION-DEVELOPER", "ZAGROS-DEVELOPER"),
		holdsPeer("p-norec", "peer-fay-zagros-2026-09-01-laptop", d(0), "ZAGROS-QUARANTINED"),
		holdsPeer("p-free", "peer-gus-zagros-2026-09-01-laptop", d(0), "ZAGROS-DEVELOPER"),
	}
	hv := ResolveHolds(HoldsInput{
		Site: "zagros", Known: holdsKnown("ann", "bob", "cat", "dan", "eve", "fay", "gus"),
		Records: records, Peers: peers, Now: holdsNow,
	})

	if !hv.RecordsAvailable || !hv.MeshAvailable {
		t.Fatalf("availability: %+v", hv)
	}
	if hv.PeersChecked != 6 {
		t.Errorf("PeersChecked = %d, want 6", hv.PeersChecked)
	}
	if len(hv.Rows) != 6 {
		t.Fatalf("rows = %d, want 6 (five records + one no-record hold-group peer; the free peer is not a row): %+v", len(hv.Rows), hv.Rows)
	}

	want := map[string][3]string{ // peer → kind, state, handle
		"p-held":   {HoldKindDormancy, HoldStateHeld, "ann"},
		"p-stale":  {HoldKindSecurity, HoldStateStale, "bob"},
		"p-incons": {HoldKindDormancy, HoldStateInconsistent, "cat"},
		"p-orphan": {HoldKindDormancy, HoldStateOrphan, "dan"},
		"p-rel":    {HoldKindDormancy, HoldStateReleasing, "eve"},
		"p-norec":  {HoldKindSecurity, HoldStateHeld, "fay"},
	}
	for id, w := range want {
		r := rowByPeer(t, hv, id)
		if r.Kind != w[0] || r.State != w[1] || r.Handle != w[2] {
			t.Errorf("%s: kind=%q state=%q handle=%q, want %v", id, r.Kind, r.State, r.Handle, w)
		}
	}
	// The wording is pinned: the badge text is what an admin reads.
	for state, label := range map[string]string{
		HoldStateHeld: "held", HoldStateReleasing: "release in progress", HoldStateStale: "stale record",
		HoldStateInconsistent: "inconsistent, investigate", HoldStateOrphan: "orphan record",
	} {
		if HoldStateLabel[state] != label {
			t.Errorf("label for %s = %q, want %q", state, HoldStateLabel[state], label)
		}
	}
	if !strings.Contains(rowByPeer(t, hv, "p-incons").Detail, "ZAGROS-OBSERVER") {
		t.Errorf("inconsistent row must name the extra group: %q", rowByPeer(t, hv, "p-incons").Detail)
	}
	// One decimal, the member page's format, so the two surfaces agree.
	if got := rowByPeer(t, hv, "p-held"); got.DaysOnHold != "10.0" || got.InactiveDays != "30.0" {
		t.Errorf("held row days: on hold %q inactive %q, want 10.0 / 30.0", got.DaysOnHold, got.InactiveDays)
	}
	if got := rowByPeer(t, hv, "p-norec"); got.HasRecord || got.DaysOnHold != "" || got.InactiveDays != "0.0" {
		t.Errorf("no-record row: %+v", got)
	}
	// Kind and State are two facts: Dormancy/Security partition the rows
	// by kind (4 + 2 = 6), Attention counts by state (stale, inconsistent,
	// orphan = 3) and overlaps them. The stale security record is one
	// Security AND one Attention, so "Security" finds it.
	if hv.Total != 6 || hv.Dormancy != 4 || hv.Security != 2 || hv.Attention != 3 || hv.Unknown != 0 {
		t.Errorf("counts total=%d dormancy=%d security=%d attention=%d unknown=%d, want 6/4/2/3/0", hv.Total, hv.Dormancy, hv.Security, hv.Attention, hv.Unknown)
	}
	stale := rowByPeer(t, hv, "p-stale")
	if stale.KindName() != "security" || !stale.Attention() || stale.BandName() != "attention" {
		t.Errorf("stale security row: kind=%q attention=%v band=%q; want security / true / attention", stale.KindName(), stale.Attention(), stale.BandName())
	}
}

// A hold placed twelve hours ago is half a day on both surfaces: the member
// page prints DaysOnHold with one decimal, and this page must not round it
// to "0".
func TestResolveHolds_DaysOnHoldMatchesMemberPage(t *testing.T) {
	entered := holdsNow.Add(-12 * time.Hour)
	rec := HoldRecord{Version: 1, Lane: HoldKindDormancy, Site: "zagros", PeerID: "p-1", PeerName: "peer-ann-zagros-2026-09-01-laptop",
		EnteredAt: entered, HoldGroup: "ZAGROS-PROBATION-DEVELOPER", PriorGroups: []string{"ZAGROS-DEVELOPER"}, Actor: "sweep"}
	peers := []Peer{holdsPeer("p-1", "peer-ann-zagros-2026-09-01-laptop", entered, "ZAGROS-PROBATION-DEVELOPER")}
	in := HoldsInput{Site: "zagros", Known: holdsKnown("ann"), Records: Records{All: []HoldRecord{rec}}, Peers: peers, Now: holdsNow}
	hv := ResolveHolds(in)
	av := Resolve(ResolveInput{Site: "zagros", Handle: "ann", Known: in.Known, Records: in.Records, Peers: peers, Now: holdsNow})
	if len(hv.Rows) != 1 || len(av.Peers) != 1 {
		t.Fatalf("rows: holds=%d member=%d", len(hv.Rows), len(av.Peers))
	}
	if hv.Rows[0].DaysOnHold != "0.5" || av.Peers[0].DaysOnHold != hv.Rows[0].DaysOnHold {
		t.Errorf("DaysOnHold: holds page %q, member page %q; want both 0.5", hv.Rows[0].DaysOnHold, av.Peers[0].DaysOnHold)
	}
}

// Default order: attention first, then security, then dormancy; newest hold
// first within a band; one member's several devices adjacent even when
// another member's hold falls between their dates.
func TestResolveHolds_DefaultOrderIsPinned(t *testing.T) {
	d := func(days int) time.Time { return holdsNow.Add(-time.Duration(days) * 24 * time.Hour) }
	records := Records{
		All: []HoldRecord{
			holdsRecord("dormancy", "p-old", "peer-zed-zagros-2026-09-01-a", "ZAGROS-PROBATION-DEVELOPER", d(40), "ZAGROS-DEVELOPER"),
			holdsRecord("dormancy", "p-two-a", "peer-amy-zagros-2026-09-01-a", "ZAGROS-PROBATION-DEVELOPER", d(9), "ZAGROS-DEVELOPER"),
			holdsRecord("dormancy", "p-mid", "peer-ben-zagros-2026-09-01-a", "ZAGROS-PROBATION-DEVELOPER", d(6), "ZAGROS-DEVELOPER"),
			holdsRecord("dormancy", "p-two-b", "peer-amy-zagros-2026-09-01-b", "ZAGROS-PROBATION-DEVELOPER", d(1), "ZAGROS-DEVELOPER"),
			holdsRecord("security", "p-sec-old", "peer-cal-zagros-2026-09-01-a", "ZAGROS-QUARANTINED", d(30), "ZAGROS-OBSERVER"),
			holdsRecord("security", "p-sec-new", "peer-dee-zagros-2026-09-01-a", "ZAGROS-QUARANTINED", d(2), "ZAGROS-OBSERVER"),
			holdsRecord("dormancy", "p-stale", "peer-eli-zagros-2026-09-01-a", "ZAGROS-PROBATION-DEVELOPER", d(50), "ZAGROS-DEVELOPER"),
		},
		DuplicatePeerIDs: map[string]bool{},
	}
	peers := []Peer{
		holdsPeer("p-old", "peer-zed-zagros-2026-09-01-a", d(1), "ZAGROS-PROBATION-DEVELOPER"),
		holdsPeer("p-two-a", "peer-amy-zagros-2026-09-01-a", d(1), "ZAGROS-PROBATION-DEVELOPER"),
		holdsPeer("p-mid", "peer-ben-zagros-2026-09-01-a", d(1), "ZAGROS-PROBATION-DEVELOPER"),
		holdsPeer("p-two-b", "peer-amy-zagros-2026-09-01-b", d(1), "ZAGROS-PROBATION-DEVELOPER"),
		holdsPeer("p-sec-old", "peer-cal-zagros-2026-09-01-a", d(1), "ZAGROS-QUARANTINED"),
		holdsPeer("p-sec-new", "peer-dee-zagros-2026-09-01-a", d(1), "ZAGROS-QUARANTINED"),
		holdsPeer("p-stale", "peer-eli-zagros-2026-09-01-a", d(1), "ZAGROS-DEVELOPER"),
		holdsPeer("p-norec", "peer-fox-zagros-2026-09-01-a", d(1), "ZAGROS-QUARANTINED"),
	}
	hv := ResolveHolds(HoldsInput{
		Site: "zagros", Known: holdsKnown("zed", "amy", "ben", "cal", "dee", "eli", "fox"),
		Records: records, Peers: peers, Now: holdsNow,
	})
	var got []string
	for _, r := range hv.Rows {
		got = append(got, r.PeerID)
	}
	want := []string{
		"p-stale",                           // attention band
		"p-sec-new", "p-sec-old", "p-norec", // security: newest first; no-record (no date) last
		"p-two-b", "p-two-a", // dormancy: amy's newest is 1 d, so amy first, her two devices adjacent
		"p-mid", "p-old",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// A member's row in another band must not promote their rows in this
// band: jane's orphan record (attention) is newer than bob's dormancy
// hold, but in the dormancy band bob's newer hold still comes first.
func TestResolveHolds_NewestIsPerBand(t *testing.T) {
	d := func(days int) time.Time { return holdsNow.Add(-time.Duration(days) * 24 * time.Hour) }
	records := Records{
		All: []HoldRecord{
			holdsRecord("dormancy", "p-jane", "peer-jane-zagros-2026-09-01-a", "ZAGROS-PROBATION-DEVELOPER", d(50), "ZAGROS-DEVELOPER"),
			holdsRecord("dormancy", "p-jane-gone", "peer-jane-zagros-2026-08-01-old", "ZAGROS-PROBATION-DEVELOPER", d(1), "ZAGROS-DEVELOPER"),
			holdsRecord("dormancy", "p-bob", "peer-bob-zagros-2026-09-01-a", "ZAGROS-PROBATION-DEVELOPER", d(20), "ZAGROS-DEVELOPER"),
		},
		DuplicatePeerIDs: map[string]bool{},
	}
	peers := []Peer{
		holdsPeer("p-jane", "peer-jane-zagros-2026-09-01-a", d(1), "ZAGROS-PROBATION-DEVELOPER"),
		holdsPeer("p-bob", "peer-bob-zagros-2026-09-01-a", d(1), "ZAGROS-PROBATION-DEVELOPER"),
	}
	hv := ResolveHolds(HoldsInput{Site: "zagros", Known: holdsKnown("jane", "bob"), Records: records, Peers: peers, Now: holdsNow})
	var got []string
	for _, r := range hv.Rows {
		got = append(got, r.PeerID)
	}
	if strings.Join(got, ",") != "p-jane-gone,p-bob,p-jane" {
		t.Fatalf("order = %v, want [p-jane-gone p-bob p-jane]", got)
	}
}

// Without the mesh the records still render: kind, held since and prior
// groups, no live grading, Inactive blank; the page says which upstream
// failed.
func TestResolveHolds_RecordsAlone(t *testing.T) {
	r := holdsRecord("security", "p-1", "peer-ann-zagros-2026-09-01-laptop", "ZAGROS-QUARANTINED", holdsNow.Add(-48*time.Hour), "ZAGROS-OBSERVER")
	r.Reason = "credential exposure suspected"
	hv := ResolveHolds(HoldsInput{
		Site: "zagros", Known: holdsKnown("ann"),
		Records:  Records{All: []HoldRecord{r}, DuplicatePeerIDs: map[string]bool{}},
		PeersErr: errors.New("HTTP 502"), Now: holdsNow,
	})
	if hv.MeshAvailable || hv.MeshError != "HTTP 502" || !hv.RecordsAvailable {
		t.Fatalf("availability: %+v", hv)
	}
	if len(hv.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(hv.Rows))
	}
	row := hv.Rows[0]
	// No live peer to own ⇒ not attributed, as on the member page; the
	// device name still carries the handle.
	if row.Kind != HoldKindSecurity || row.State != HoldStateHeld || row.Handle != "" || row.InactiveDays != "" || row.DaysOnHold != "2.0" || row.Reason == "" {
		t.Errorf("records-alone row: %+v", row)
	}
	if hv.Security != 1 || hv.Total != 1 {
		t.Errorf("counts: %+v", hv)
	}
}

// Without the record file the mesh still yields the hold-group peers, kind
// unknown rather than guessed; with neither upstream there are no rows and
// both errors are carried.
func TestResolveHolds_MeshAloneAndNeither(t *testing.T) {
	peers := []Peer{holdsPeer("p-1", "peer-ann-zagros-2026-09-01-laptop", holdsNow, "ZAGROS-PROBATION-DEVELOPER")}
	hv := ResolveHolds(HoldsInput{
		Site: "zagros", Known: holdsKnown("ann"),
		RecordsErr: errors.New("open: no such file"), Peers: peers, Now: holdsNow,
	})
	if hv.RecordsAvailable || !hv.MeshAvailable || len(hv.Rows) != 1 {
		t.Fatalf("mesh-alone: %+v", hv)
	}
	if hv.Rows[0].Kind != "" || hv.Rows[0].State != HoldStateHeld || HoldKindLabel[hv.Rows[0].Kind] != "kind unknown" {
		t.Errorf("mesh-alone row: %+v", hv.Rows[0])
	}
	if hv.Security != 0 || hv.Unknown != 1 || hv.Rows[0].BandName() != "unknown" {
		t.Errorf("a kind-unknown hold must count as unknown, never as security: %+v", hv)
	}
	// An inconsistent no-record peer is kind unknown too when the file is
	// unreadable, on both surfaces.
	mixed := []Peer{holdsPeer("p-2", "peer-ann-zagros-2026-09-01-b", holdsNow, "ZAGROS-PROBATION-DEVELOPER", "ZAGROS-DEVELOPER")}
	hv2 := ResolveHolds(HoldsInput{Site: "zagros", Known: holdsKnown("ann"), RecordsErr: errors.New("open"), Peers: mixed, Now: holdsNow})
	av2 := Resolve(ResolveInput{Handle: "ann", Site: "zagros", Known: holdsKnown("ann"), RecordsErr: errors.New("open"), Peers: mixed, Now: holdsNow})
	if len(hv2.Rows) != 1 || hv2.Rows[0].Kind != "" || hv2.Rows[0].State != HoldStateInconsistent {
		t.Errorf("mesh-alone inconsistent row: %+v", hv2.Rows)
	}
	if len(av2.Peers) != 1 || av2.Peers[0].Kind != hv2.Rows[0].Kind || av2.Peers[0].State != hv2.Rows[0].State {
		t.Errorf("member page disagrees: %+v", av2.Peers)
	}

	none := ResolveHolds(HoldsInput{Site: "zagros", RecordsErr: errors.New("r"), PeersErr: errors.New("m"), Now: holdsNow})
	if none.RecordsAvailable || none.MeshAvailable || len(none.Rows) != 0 || none.RecordsError != "r" || none.MeshError != "m" {
		t.Errorf("neither: %+v", none)
	}
}

// Attribution follows the roster rule (longest known handle owns the name)
// and is withheld entirely when the roster is partial; an orphan record and
// a peer nobody owns stay as unattributed rows, never dropped.
func TestResolveHolds_AttributionRules(t *testing.T) {
	peers := []Peer{
		holdsPeer("p-dave2", "peer-dave-2-usareur-af-zagros-2026-08-04", holdsNow, "ZAGROS-QUARANTINED"),
		holdsPeer("p-nobody", "peer-xyz-zagros-2026-08-04", holdsNow, "ZAGROS-QUARANTINED"),
	}
	rec := holdsRecord("dormancy", "p-gone", "peer-dave-zagros-2026-08-01-old", "ZAGROS-PROBATION-DEVELOPER", holdsNow.Add(-24*time.Hour), "ZAGROS-DEVELOPER")
	in := HoldsInput{
		Site: "zagros", Known: holdsKnown("dave", "dave-2"),
		Records: Records{All: []HoldRecord{rec}, DuplicatePeerIDs: map[string]bool{}},
		Peers:   peers, Now: holdsNow,
	}
	hv := ResolveHolds(in)
	if len(hv.Rows) != 3 {
		t.Fatalf("rows = %d, want 3: %+v", len(hv.Rows), hv.Rows)
	}
	if r := rowByPeer(t, hv, "p-dave2"); r.Handle != "dave-2" {
		t.Errorf("dave-2's peer credited to %q", r.Handle)
	}
	if r := rowByPeer(t, hv, "p-nobody"); r.Handle != "" || r.State != HoldStateHeld || r.Kind != HoldKindSecurity {
		t.Errorf("unowned hold-group peer: %+v", r)
	}
	if r := rowByPeer(t, hv, "p-gone"); r.Handle != "dave" || r.State != HoldStateOrphan {
		t.Errorf("orphan record: %+v", r)
	}

	in.KnownErr = errors.New("list handles: store closed")
	hv = ResolveHolds(in)
	if hv.KnownHandlesError == "" {
		t.Fatal("partial roster not surfaced")
	}
	for _, r := range hv.Rows {
		if r.Handle != "" {
			t.Errorf("row %s attributed to %q with a partial roster", r.PeerID, r.Handle)
		}
	}
	if len(hv.Rows) != 3 {
		t.Errorf("partial roster dropped rows: %d", len(hv.Rows))
	}
}

// One row per held PEER: a duplicated record line is flagged on one row
// and adds no second — the FIRST line's kind wins, as on the member page
// and in the sweep, so every surface shows the same badge; a record for
// another site is not this page's.
func TestResolveHolds_OneRowPerPeer(t *testing.T) {
	a := holdsRecord("dormancy", "p-1", "peer-ann-zagros-2026-09-01-laptop", "ZAGROS-PROBATION-DEVELOPER", holdsNow.Add(-24*time.Hour), "ZAGROS-DEVELOPER")
	b := a
	b.Lane = "security"
	b.HoldGroup = "ZAGROS-QUARANTINED"
	other := holdsRecord("dormancy", "p-9", "peer-ann-zagros-prod-2026-09-01-laptop", "ZAGROS-PROD-PROBATION-DEVELOPER", holdsNow, "ZAGROS-PROD-DEVELOPER")
	other.Site = "zagros-prod"
	hv := ResolveHolds(HoldsInput{
		Site: "zagros", Known: holdsKnown("ann"),
		Records: Records{All: []HoldRecord{a, b, other}, DuplicatePeerIDs: map[string]bool{"p-1": true}},
		Peers:   []Peer{holdsPeer("p-1", a.PeerName, holdsNow, "ZAGROS-PROBATION-DEVELOPER")}, Now: holdsNow,
	})
	if len(hv.Rows) != 1 || hv.Rows[0].State != HoldStateInconsistent || !strings.Contains(hv.Rows[0].Detail, "duplicate") {
		t.Fatalf("rows: %+v", hv.Rows)
	}
	av := Resolve(ResolveInput{Handle: "ann", Site: "zagros", Known: holdsKnown("ann"),
		Records: Records{All: []HoldRecord{a, b}, DuplicatePeerIDs: map[string]bool{"p-1": true}},
		Peers:   []Peer{holdsPeer("p-1", a.PeerName, holdsNow, "ZAGROS-PROBATION-DEVELOPER")}, Now: holdsNow})
	if len(av.Peers) != 1 || av.Peers[0].Kind != hv.Rows[0].Kind || av.Peers[0].State != hv.Rows[0].State || hv.Rows[0].Kind != HoldKindDormancy {
		t.Fatalf("duplicate lanes: member page kind=%q state=%q, holds page kind=%q state=%q; want the first line (dormancy) on both",
			av.Peers[0].Kind, av.Peers[0].State, hv.Rows[0].Kind, hv.Rows[0].State)
	}
}

// A hold group under another prefix (an earlier site id, or another site on
// a shared management host) is still a hold, and the row says whose prefix
// it is under.
func TestResolveHolds_ForeignPrefixIsNamed(t *testing.T) {
	peers := []Peer{holdsPeer("p-1", "peer-ann-zagros-2026-09-01-laptop", holdsNow, "ZAGROS-PROD-QUARANTINED")}
	hv := ResolveHolds(HoldsInput{Site: "zagros", Known: holdsKnown("ann"), Records: Records{DuplicatePeerIDs: map[string]bool{}}, Peers: peers, Now: holdsNow})
	if len(hv.Rows) != 1 || strings.Count(hv.Rows[0].Detail, "not under this goatnet's prefix ZAGROS-") != 1 {
		t.Fatalf("want the note exactly once; rows: %+v", hv.Rows)
	}
}

// The same note on a record-backed row: a peer held under another site's
// prefix is explained the same way whether or not a record exists, and
// the note joins a non-empty Detail rather than replacing it.
func TestResolveHolds_ForeignPrefixIsNamedOnRecordRows(t *testing.T) {
	r := holdsRecord("security", "p-1", "peer-ann-zagros-2026-09-01-laptop", "ZAGROS-PROD-QUARANTINED", holdsNow.Add(-24*time.Hour))
	for _, tc := range []struct {
		name  string
		peers []Peer
		state string
		live  bool
	}{
		{"held", []Peer{holdsPeer("p-1", r.PeerName, holdsNow, "ZAGROS-PROD-QUARANTINED")}, HoldStateHeld, true},
		{"stale", []Peer{holdsPeer("p-1", r.PeerName, holdsNow, "ZAGROS-DEVELOPER")}, HoldStateStale, true},
		{"orphan", nil, HoldStateOrphan, false},
	} {
		hv := ResolveHolds(HoldsInput{Site: "zagros", Known: holdsKnown("ann"), Records: Records{All: []HoldRecord{r}, DuplicatePeerIDs: map[string]bool{}}, Peers: tc.peers, Now: holdsNow})
		if len(hv.Rows) != 1 || hv.Rows[0].State != tc.state || hv.Rows[0].Live != tc.live || strings.Count(hv.Rows[0].Detail, "hold group ZAGROS-PROD-QUARANTINED is not under this goatnet's prefix ZAGROS-") != 1 {
			t.Fatalf("%s: want state %s live=%v and the note exactly once; rows: %+v", tc.name, tc.state, tc.live, hv.Rows)
		}
		if strings.HasPrefix(hv.Rows[0].Detail, ";") || strings.Contains(hv.Rows[0].Detail, ";;") {
			t.Fatalf("%s: detail joined badly: %q", tc.name, hv.Rows[0].Detail)
		}
	}
}

// Live follows the mesh listing, not the graded state: a duplicated orphan
// record is graded inconsistent, yet its peer is not on the mesh, so Live
// stays false and the page must not say "never seen" of it.
func TestResolveHolds_LiveFollowsMeshListingNotState(t *testing.T) {
	r := holdsRecord("security", "p-1", "peer-ann-zagros-2026-09-01-laptop", "ZAGROS-QUARANTINED", holdsNow.Add(-24*time.Hour))
	hv := ResolveHolds(HoldsInput{Site: "zagros", Known: holdsKnown("ann"), Records: Records{All: []HoldRecord{r, r}, DuplicatePeerIDs: map[string]bool{"p-1": true}}, Peers: []Peer{}, Now: holdsNow})
	if len(hv.Rows) != 1 || hv.Rows[0].State != HoldStateInconsistent || hv.Rows[0].Live || hv.Rows[0].InactiveDays != "" {
		t.Fatalf("rows: %+v", hv.Rows)
	}
	never := holdsPeer("p-1", r.PeerName, time.Time{}, "ZAGROS-QUARANTINED")
	hv = ResolveHolds(HoldsInput{Site: "zagros", Known: holdsKnown("ann"), Records: Records{All: []HoldRecord{r}, DuplicatePeerIDs: map[string]bool{}}, Peers: []Peer{never}, Now: holdsNow})
	if len(hv.Rows) != 1 || !hv.Rows[0].Live || hv.Rows[0].InactiveDays != "" {
		t.Fatalf("a listed peer the mesh never saw must be Live with no InactiveDays; rows: %+v", hv.Rows)
	}
}

// The member page and the Holds page grade one peer the same way: for each
// live shape, Resolve's PeerView.Kind/State equal ResolveHolds' row.
func TestResolveHolds_AgreesWithMemberPage(t *testing.T) {
	d := func(days int) time.Time { return holdsNow.Add(-time.Duration(days) * 24 * time.Hour) }
	rel := holdsRecord("dormancy", "p-rel", "peer-ann-zagros-2026-09-01-c", "ZAGROS-PROBATION-DEVELOPER", d(2), "ZAGROS-DEVELOPER")
	rel.Phase = "releasing"
	records := Records{
		All: []HoldRecord{
			holdsRecord("dormancy", "p-held", "peer-ann-zagros-2026-09-01-a", "ZAGROS-PROBATION-DEVELOPER", d(10), "ZAGROS-DEVELOPER"),
			holdsRecord("security", "p-stale", "peer-ann-zagros-2026-09-01-b", "ZAGROS-QUARANTINED", d(5), "ZAGROS-OBSERVER"),
			rel,
			holdsRecord("dormancy", "p-orphan", "peer-ann-zagros-2026-09-01-d", "ZAGROS-PROBATION-OBSERVER", d(20), "ZAGROS-OBSERVER"),
			holdsRecord("dormancy", "p-incons", "peer-ann-zagros-2026-09-01-e", "ZAGROS-PROBATION-OBSERVER", d(3), "ZAGROS-OBSERVER"),
			// Duplicated record lines, one for a live peer and one for a
			// peer the mesh does not list: both surfaces grade them.
			holdsRecord("dormancy", "p-dup", "peer-ann-zagros-2026-09-01-h", "ZAGROS-PROBATION-OBSERVER", d(4), "ZAGROS-OBSERVER"),
			holdsRecord("dormancy", "p-dup", "peer-ann-zagros-2026-09-01-h", "ZAGROS-PROBATION-OBSERVER", d(4), "ZAGROS-OBSERVER"),
			holdsRecord("security", "p-duporphan", "peer-ann-zagros-2026-09-01-i", "ZAGROS-QUARANTINED", d(6), "ZAGROS-OBSERVER"),
			holdsRecord("security", "p-duporphan", "peer-ann-zagros-2026-09-01-i", "ZAGROS-QUARANTINED", d(6), "ZAGROS-OBSERVER"),
		},
		DuplicatePeerIDs: map[string]bool{"p-dup": true, "p-duporphan": true},
	}
	peers := []Peer{
		holdsPeer("p-held", "peer-ann-zagros-2026-09-01-a", d(1), "ZAGROS-PROBATION-DEVELOPER"),
		holdsPeer("p-stale", "peer-ann-zagros-2026-09-01-b", d(1), "ZAGROS-OBSERVER"),
		holdsPeer("p-rel", "peer-ann-zagros-2026-09-01-c", d(1), "ZAGROS-PROBATION-DEVELOPER", "ZAGROS-DEVELOPER"),
		holdsPeer("p-incons", "peer-ann-zagros-2026-09-01-e", d(1), "ZAGROS-PROBATION-OBSERVER", "ZAGROS-OBSERVER"),
		holdsPeer("p-norec", "peer-ann-zagros-2026-09-01-f", d(1), "ZAGROS-QUARANTINED"),
		holdsPeer("p-two", "peer-ann-zagros-2026-09-01-g", d(1), "ZAGROS-QUARANTINED", "ZAGROS-PROBATION-DEVELOPER"),
		holdsPeer("p-dup", "peer-ann-zagros-2026-09-01-h", d(1), "ZAGROS-PROBATION-OBSERVER"),
	}
	known := holdsKnown("ann")
	hv := ResolveHolds(HoldsInput{Site: "zagros", Known: known, Records: records, Peers: peers, Now: holdsNow})
	av := Resolve(ResolveInput{Handle: "ann", Site: "zagros", Known: known, Records: records, Peers: peers, Now: holdsNow})
	if len(av.Peers) != len(hv.Rows) {
		t.Fatalf("member page %d peers, holds page %d rows", len(av.Peers), len(hv.Rows))
	}
	for _, pv := range av.Peers {
		row := rowByPeer(t, hv, pv.ID)
		if pv.Kind != row.Kind || pv.State != row.State {
			t.Errorf("%s: member page kind=%q state=%q, holds page kind=%q state=%q", pv.ID, pv.Kind, pv.State, row.Kind, row.State)
		}
		if pv.State == "" {
			t.Errorf("%s: member page carries no shared State for a held peer (verdict %q)", pv.ID, pv.Verdict)
		}
	}
	if r := rowByPeer(t, hv, "p-duporphan"); r.State != HoldStateInconsistent {
		t.Errorf("duplicated orphan record: holds page state %q, want inconsistent", r.State)
	}
}
