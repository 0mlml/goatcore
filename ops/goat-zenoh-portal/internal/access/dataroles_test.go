package access

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// dataRosterFixture writes a roster.json exercising a three-deep vertical org
// chain (division → brigade → battalion), a separate root vendor org, and a
// person who belongs to two orgs — enough to cover nested transitive roll-up,
// custody, and multi-org union.
//
// Email-hashes are opaque here (the resolver joins on the hash, not the email);
// the fixture uses readable stand-ins.
func dataRosterFixture(t *testing.T) *DataRoster {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "roster.json")
	body := `{
      "generated_at": "2026-07-09T00:00:00Z",
      "source_digest": "deadbeef",
      "orgs": [
        {"id": "division", "kind": "organic", "nation": "USA", "parent": null, "members": [
          {"handle": "commander", "type": "person", "state": "active", "vendor_tag": null, "email_hash": "hash-commander"}
        ]},
        {"id": "brigade", "kind": "organic", "nation": "USA", "parent": "division", "members": [
          {"handle": "alice", "type": "person", "state": "active", "vendor_tag": "brigade-alice", "email_hash": "hash-alice"},
          {"handle": "dave", "type": "person", "state": "active", "vendor_tag": "brigade-dave", "email_hash": "hash-dave"}
        ]},
        {"id": "battalion", "kind": "organic", "nation": "USA", "parent": "brigade", "members": [
          {"handle": "bob", "type": "person", "state": "active", "vendor_tag": "battalion-bob", "email_hash": "hash-bob"}
        ]},
        {"id": "vendorco", "kind": "vendor", "nation": null, "parent": null, "members": [
          {"handle": "carol", "type": "person", "state": "active", "vendor_tag": "vendorco-carol", "email_hash": "hash-carol"},
          {"handle": "dave", "type": "person", "state": "active", "vendor_tag": null, "email_hash": "hash-dave"}
        ]}
      ],
      "members": []
    }`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	dr, err := LoadDataRoster(path)
	if err != nil {
		t.Fatalf("LoadDataRoster: %v", err)
	}
	return dr
}

func TestRolesForPerson(t *testing.T) {
	dr := dataRosterFixture(t)
	cases := []struct {
		name      string
		emailHash string
		wantRoles []string
		wantProds []string
	}{
		{
			// Division commander (no own producer): view of the org, and the
			// producer set rolls up transitively through brigade AND battalion.
			name:      "nested transitive rollup from the top org",
			emailHash: "hash-commander",
			wantRoles: []string{"data.view.org:division"},
			wantProds: []string{"battalion-bob", "brigade-alice", "brigade-dave"},
		},
		{
			// Brigade member with a producer: org view + custody of own producer;
			// producer set = brigade's own producers + the descendant battalion.
			name:      "org member with custody, one level of descendants",
			emailHash: "hash-alice",
			wantRoles: []string{"data.view.org:brigade", "data.view.producer:brigade-alice"},
			wantProds: []string{"battalion-bob", "brigade-alice", "brigade-dave"},
		},
		{
			// Leaf-org member: no descendants, so the producer set is just its own.
			name:      "leaf org member",
			emailHash: "hash-bob",
			wantRoles: []string{"data.view.org:battalion", "data.view.producer:battalion-bob"},
			wantProds: []string{"battalion-bob"},
		},
		{
			// Multi-org person: union of both orgs' view + rollups; custody only
			// for the producer they actually carry (brigade-dave), not vendorco.
			name:      "multi-org membership is a union",
			emailHash: "hash-dave",
			wantRoles: []string{"data.view.org:brigade", "data.view.org:vendorco", "data.view.producer:brigade-dave"},
			wantProds: []string{"battalion-bob", "brigade-alice", "brigade-dave", "vendorco-carol"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dr.RolesForPerson(c.emailHash)
			if !reflect.DeepEqual(got.Roles, c.wantRoles) {
				t.Errorf("roles = %v, want %v", got.Roles, c.wantRoles)
			}
			if !reflect.DeepEqual(got.Producers, c.wantProds) {
				t.Errorf("producers = %v, want %v", got.Producers, c.wantProds)
			}
		})
	}
}

func TestRolesForPerson_UnknownIsEmptyNotDenied(t *testing.T) {
	dr := dataRosterFixture(t)
	got := dr.RolesForPerson("hash-nobody")
	if len(got.Roles) != 0 || len(got.Producers) != 0 {
		t.Fatalf("unknown principal should derive nothing, got %+v", got)
	}
	if len(got.Notes) == 0 {
		t.Fatalf("unknown principal should carry an honest note, got none")
	}
}

func TestRolesForAnchor(t *testing.T) {
	dr := dataRosterFixture(t)
	cases := []struct {
		name      string
		anchor    string
		wantRoles []string
		wantProds []string
		wantNote  bool
	}{
		{
			// Anchor under a leaf org: write + self-view by construction, plus the
			// owning org view; producer set is just its own (leaf).
			name:      "anchor in a leaf org",
			anchor:    "battalion-bob",
			wantRoles: []string{"data.view.org:battalion", "data.view.producer:battalion-bob", "data.write.producer:battalion-bob"},
			wantProds: []string{"battalion-bob"},
		},
		{
			// Anchor under a mid org: owning-org view expands down to the descendant.
			name:      "anchor in an org with descendants",
			anchor:    "brigade-alice",
			wantRoles: []string{"data.view.org:brigade", "data.view.producer:brigade-alice", "data.write.producer:brigade-alice"},
			wantProds: []string{"battalion-bob", "brigade-alice", "brigade-dave"},
		},
		{
			// Anchor the roster doesn't place under any org: still gets its
			// by-construction roles + a note, no org roll-up.
			name:      "anchor with no declared owning org",
			anchor:    "ghost-x",
			wantRoles: []string{"data.view.producer:ghost-x", "data.write.producer:ghost-x"},
			wantProds: []string{"ghost-x"},
			wantNote:  true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dr.RolesForAnchor(c.anchor)
			if !reflect.DeepEqual(got.Roles, c.wantRoles) {
				t.Errorf("roles = %v, want %v", got.Roles, c.wantRoles)
			}
			if !reflect.DeepEqual(got.Producers, c.wantProds) {
				t.Errorf("producers = %v, want %v", got.Producers, c.wantProds)
			}
			if (len(got.Notes) > 0) != c.wantNote {
				t.Errorf("note presence = %v, want %v (notes: %v)", len(got.Notes) > 0, c.wantNote, got.Notes)
			}
		})
	}
}

func TestKnownProducer(t *testing.T) {
	dr := dataRosterFixture(t)
	if !dr.KnownProducer("battalion-bob") {
		t.Errorf("battalion-bob should be a known producer")
	}
	if dr.KnownProducer("ghost-x") {
		t.Errorf("ghost-x is not declared and must not be known")
	}
}

func TestLoadDataRoster_Errors(t *testing.T) {
	if _, err := LoadDataRoster(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Errorf("missing roster must fail closed, got nil error")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDataRoster(bad); err == nil {
		t.Errorf("malformed roster must fail closed, got nil error")
	}
}

// TestMachineAnchorsForPerson covers the WS4 person→machine projection: a
// principal grant to a person must reach the machine anchor(s) that person
// carries; a person with none is a browser-only reader (empty).
func TestMachineAnchorsForPerson(t *testing.T) {
	dr := dataRosterFixture(t)
	// dave carries a producer under brigade (brigade-dave) but none under vendorco.
	got := dr.MachineAnchorsForPerson("hash-dave")
	if !reflect.DeepEqual(got, []string{"brigade-dave"}) {
		t.Errorf("dave anchors = %v, want [brigade-dave]", got)
	}
	// commander has no vendor_tag anywhere → browser-only, no anchor.
	if got := dr.MachineAnchorsForPerson("hash-commander"); len(got) != 0 {
		t.Errorf("commander anchors = %v, want empty (browser-only)", got)
	}
	// an unknown identity resolves to nothing.
	if got := dr.MachineAnchorsForPerson("hash-nobody"); len(got) != 0 {
		t.Errorf("unknown anchors = %v, want empty", got)
	}
}

// TestOffboardedMemberDropsFromAudience covers WS4 offboarding freshness: a
// member marked inactive in the roster contributes no producer anchor, so it is
// not a wrap target once the roster reflects the offboarding.
func TestOffboardedMemberDropsFromAudience(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roster.json")
	body := `{
      "generated_at": "2026-07-30T00:00:00Z",
      "source_digest": "cafe",
      "orgs": [
        {"id": "unit", "kind": "organic", "nation": "USA", "parent": null, "members": [
          {"handle": "stay", "type": "person", "state": "active", "vendor_tag": "unit-stay", "email_hash": "h-stay"},
          {"handle": "gone", "type": "person", "state": "revoked", "vendor_tag": "unit-gone", "email_hash": "h-gone"}
        ]}
      ],
      "members": []
    }`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	dr, err := LoadDataRoster(path)
	if err != nil {
		t.Fatalf("LoadDataRoster: %v", err)
	}
	got := dr.ProducersUnderOrg("unit")
	if !reflect.DeepEqual(got, []string{"unit-stay"}) {
		t.Errorf("unit producers = %v, want [unit-stay] (revoked member dropped)", got)
	}
	if dr.KnownProducer("unit-gone") {
		t.Error("revoked member's anchor unit-gone is still a known producer — offboarding did not cut it")
	}
	// the offboarded person resolves to no anchor either.
	if got := dr.MachineAnchorsForPerson("h-gone"); len(got) != 0 {
		t.Errorf("offboarded person anchors = %v, want empty", got)
	}
}
