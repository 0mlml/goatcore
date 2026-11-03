package access

import "testing"

// Person→anchor attribution (identity fragility audit §1.2). The
// recorded linkage (slots.identity_id, arriving here as VendorIn.IdentityID) is
// ground truth; the org-tag join is the one name-parsing fallback, it must never
// override the recorded linkage, and every resolution must say which one
// answered — a name that happens to match is not an owner, and revocation
// targets owners.
//
// There is deliberately no alias↔handle join: goat-cli's rotate/revoke cascade
// refuses it as a mis-rotation, and the portal used to make it anyway, so the
// two components disagreed about ownership.

// linkageFixture: one person (kid-77 / dana@example.test, roster handle
// `dana-ops`, org short-tag `examplecorp`) and three candidate anchors — one
// carrying her recorded identity UUID, one whose alias equals her handle, one
// whose alias equals her org tag. Only the first is a recorded fact.
func linkageFixture(recordOn string) Sources {
	vendors := []VendorIn{
		{Prefix: "anchor-recorded", DisplayName: "recorded", WriteAnchor: "anchor-recorded", Alias: "dana-recorded"},
		{Prefix: "anchor-by-handle", DisplayName: "by handle", WriteAnchor: "anchor-by-handle", Alias: "dana-ops"},
		{Prefix: "anchor-by-tag", DisplayName: "by tag", WriteAnchor: "anchor-by-tag", Alias: "examplecorp"},
	}
	for i := range vendors {
		if vendors[i].Prefix == recordOn {
			vendors[i].IdentityID = "kid-77"
		}
	}
	return Sources{
		Vendors: vendors,
		Identities: []IdentityIn{
			{ID: "kid-77", Email: "dana@example.test", Name: "Dana", Vendor: "examplecorp"},
		},
		Roster: []RosterEntry{
			{Handle: "dana-ops", Type: "person", Org: "examplecorp", Status: "active",
				EmailHash: EmailHash("dana@example.test")},
		},
	}
}

func TestRecordedLinkageBeatsBothNameJoins(t *testing.T) {
	g := Assemble(linkageFixture("anchor-recorded"))
	pid := nodeID(KindPerson, "kid-77")

	if got := g.PersonAnchor[pid]; got != nodeID(KindVendor, "anchor-recorded") {
		t.Errorf("PersonAnchor = %q, want the recorded anchor (a name join won instead)", got)
	}
	if got := g.PersonAnchorSource[pid]; got != AnchorRecorded {
		t.Errorf("PersonAnchorSource = %q, want %q", got, AnchorRecorded)
	}
	if g.AnchorJoin.Recorded != 1 || g.AnchorJoin.Fallbacks() != 0 {
		t.Errorf("AnchorJoin = %+v, want 1 recorded and 0 fallbacks", g.AnchorJoin)
	}
	if got := g.Who("dana@example.test").AnchorLinkage; got != AnchorRecorded {
		t.Errorf("Who().AnchorLinkage = %q, want %q", got, AnchorRecorded)
	}
}

// The join-policy reconcile (fragility audit §1.2, fix-train item 8): a slot
// whose alias merely EQUALS someone's roster handle is not their slot. A slot
// alias spells the org short-tag (the onboard sets it from `zenoh.trial.vendor`
// and drift-checks it against `org.short_tag`), so a handle can only match by
// colliding with some other member's tag — which goat-cli's cascade refuses as
// a mis-rotation. The portal now refuses it too: with both candidates present
// and nothing recorded, the TAG anchor answers, never the handle-aliased one.
func TestHandleAliasedAnchorIsNeverJoinedToThatHandle(t *testing.T) {
	g := Assemble(linkageFixture(""))
	pid := nodeID(KindPerson, "kid-77")

	if got := g.PersonAnchor[pid]; got == nodeID(KindVendor, "anchor-by-handle") {
		t.Fatalf("PersonAnchor = %q — the portal took the alias↔handle join goat-cli refuses; "+
			"two components must not disagree about who owns a slot", got)
	}
	if got := g.PersonAnchor[pid]; got != nodeID(KindVendor, "anchor-by-tag") {
		t.Errorf("PersonAnchor = %q, want the org-tag match", got)
	}
	if got := g.PersonAnchorSource[pid]; got != AnchorTagJoin {
		t.Errorf("PersonAnchorSource = %q, want %q", got, AnchorTagJoin)
	}
	if g.AnchorJoin.Recorded != 0 || g.AnchorJoin.TagJoin != 1 || g.AnchorJoin.Fallbacks() != 1 {
		t.Errorf("AnchorJoin = %+v, want 0 recorded / 1 tag-join", g.AnchorJoin)
	}
	if got := g.Who("dana-ops").AnchorLinkage; got != AnchorTagJoin {
		t.Errorf("Who().AnchorLinkage = %q, want %q — a guess presented as a recorded owner is the mis-attribution class", got, AnchorTagJoin)
	}
}

// …and when the handle-aliased anchor is the ONLY candidate, the person reports
// no anchor at all. An honest miss beats a confident wrong owner: this is the
// row the recorded linkage (slots.identity_id) is there to fill.
func TestHandleAliasAloneLeavesThePersonUnanchored(t *testing.T) {
	src := linkageFixture("")
	src.Vendors = src.Vendors[1:2] // keep only anchor-by-handle
	g := Assemble(src)
	pid := nodeID(KindPerson, "kid-77")

	if got, ok := g.PersonAnchor[pid]; ok {
		t.Errorf("PersonAnchor = %q, want no anchor — the only candidate matched on a handle", got)
	}
	if g.AnchorJoin != (AnchorJoinStats{}) {
		t.Errorf("AnchorJoin = %+v, want all zero", g.AnchorJoin)
	}
	if got := g.Who("dana-ops").AnchorLinkage; got != "" {
		t.Errorf("AnchorLinkage = %q, want empty", got)
	}
}

// The org-tag join still answers on its own, and still says it is a name join.
func TestTagJoinAnswersAndSaysSo(t *testing.T) {
	src := linkageFixture("")
	src.Roster[0].Handle = "someone-else"
	g := Assemble(src)
	pid := nodeID(KindPerson, "kid-77")

	if got := g.PersonAnchor[pid]; got != nodeID(KindVendor, "anchor-by-tag") {
		t.Errorf("PersonAnchor = %q, want the org-tag match", got)
	}
	if got := g.PersonAnchorSource[pid]; got != AnchorTagJoin {
		t.Errorf("PersonAnchorSource = %q, want %q", got, AnchorTagJoin)
	}
	if g.AnchorJoin.TagJoin != 1 || g.AnchorJoin.Fallbacks() != 1 {
		t.Errorf("AnchorJoin = %+v, want 1 tag-join", g.AnchorJoin)
	}
}

// A person the graph cannot anchor at all reports no linkage rather than an
// empty-string source that reads like a recorded one.
func TestNoAnchorReportsNoLinkage(t *testing.T) {
	src := linkageFixture("")
	src.Vendors = nil
	g := Assemble(src)

	if got := g.Who("dana@example.test").AnchorLinkage; got != "" {
		t.Errorf("AnchorLinkage = %q, want empty for an unanchored person", got)
	}
	if g.AnchorJoin != (AnchorJoinStats{}) {
		t.Errorf("AnchorJoin = %+v, want all zero", g.AnchorJoin)
	}
}

// The roster (the bulk ownership surface `goat who` renders) carries the same
// label, so an operator scanning rows can see which ones are inferred.
func TestRosterRowCarriesAnchorLinkage(t *testing.T) {
	g := Assemble(linkageFixture("anchor-recorded"))
	var seen bool
	for _, row := range g.Roster().Rows {
		if row.Kind != KindPerson {
			continue
		}
		seen = true
		if row.AnchorLinkage != AnchorRecorded {
			t.Errorf("roster row %s AnchorLinkage = %q, want %q", row.ID, row.AnchorLinkage, AnchorRecorded)
		}
	}
	if !seen {
		t.Fatal("no person row in the roster")
	}
}
