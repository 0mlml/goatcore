package access

import (
	"reflect"
	"strings"
	"testing"
)

func TestOrgsFromRoles(t *testing.T) {
	roles := []string{"data.view.org:org1", "data.view.producer:org1-producer", "data.view.org:acme"}
	got := OrgsFromRoles(roles)
	want := []string{"org1", "acme"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("OrgsFromRoles=%v want %v", got, want)
	}
}

func TestMergeGrantsPrincipal(t *testing.T) {
	base := DataRoleResult{Roles: []string{"data.view.org:acme"}, Producers: []string{"acme-sensor"}}
	grants := []GrantRow{
		{GranteeKind: "principal", GranteeID: "uuid-alice", Producer: "beta-radar"},
		{GranteeKind: "principal", GranteeID: "uuid-someone-else", Producer: "gamma-eo"},
	}
	got := MergeGrants(base, "uuid-alice", grants)
	if !contains(got.Roles, "data.view.producer:beta-radar") {
		t.Fatalf("alice's principal grant not applied: %v", got.Roles)
	}
	if contains(got.Roles, "data.view.producer:gamma-eo") {
		t.Fatalf("someone else's grant leaked to alice: %v", got.Roles)
	}
	if !contains(got.Producers, "beta-radar") || !contains(got.Producers, "acme-sensor") {
		t.Fatalf("producer set wrong: %v", got.Producers)
	}
	if len(got.Notes) == 0 {
		t.Fatal("expected an honesty note when a grant is applied")
	}
}

func TestMergeGrantsCoPublishWrite(t *testing.T) {
	// A co-publish (write) grant to alice for a multi-publisher topic folds into
	// her CoPublish membership — NOT a producer role, view or write — with a
	// note.
	multipubTopic := "multipub/aa11bb22/cc33dd44"
	base := DataRoleResult{Roles: []string{"data.write.producer:alice-anchor", "data.view.producer:alice-anchor"}, Producers: []string{"alice-anchor"}}
	grants := []GrantRow{
		{GranteeKind: "principal", GranteeID: "alice-anchor", Producer: multipubTopic, Action: "write"},
		{GranteeKind: "principal", GranteeID: "someone-else", Producer: "multipub/aa11bb22/eeeeffff", Action: "write"},
	}
	got := MergeGrants(base, "alice-anchor", grants)
	if !contains(got.CoPublish, multipubTopic) {
		t.Fatalf("co-publish membership not applied: %v", got.CoPublish)
	}
	if contains(got.CoPublish, "multipub/aa11bb22/eeeeffff") {
		t.Fatalf("someone else's co-publish grant leaked to alice: %v", got.CoPublish)
	}
	if contains(got.Roles, "data.write.producer:"+multipubTopic) || contains(got.Producers, multipubTopic) {
		t.Fatalf("a multipub-topic co-publish must NOT become a producer role/producer: roles=%v prods=%v", got.Roles, got.Producers)
	}
	var coNote bool
	for _, n := range got.Notes {
		if strings.Contains(n, "co-publish") {
			coNote = true
		}
	}
	if !coNote {
		t.Fatalf("expected a co-publish honesty note, got %v", got.Notes)
	}
}

// A write grant with an ORG grantee is malformed (the store refuses it); if one
// ever reaches the resolver it must be ignored, never widen an org to co-publish.
func TestMergeGrantsWriteToOrgIgnored(t *testing.T) {
	base := DataRoleResult{Roles: []string{"data.view.org:acme"}, Producers: []string{}}
	grants := []GrantRow{{GranteeKind: "org", GranteeID: "acme", Producer: "multipub/aa/bb", Action: "write"}}
	got := MergeGrants(base, "uuid-bob", grants)
	if contains(got.CoPublish, "multipub/aa/bb") {
		t.Fatalf("a write grant to an ORG grantee must be ignored, got %v", got.CoPublish)
	}
}

func TestMergeGrantsOrgExpansion(t *testing.T) {
	// A member of org acme gets an org-scoped grant to beta-radar.
	base := DataRoleResult{Roles: []string{"data.view.org:acme", "data.view.producer:acme-sensor"}, Producers: []string{"acme-sensor"}}
	grants := []GrantRow{
		{GranteeKind: "org", GranteeID: "acme", Producer: "beta-radar"},
		{GranteeKind: "org", GranteeID: "other-org", Producer: "gamma-eo"},
	}
	got := MergeGrants(base, "uuid-bob", grants)
	if !contains(got.Roles, "data.view.producer:beta-radar") {
		t.Fatalf("acme org grant not applied to a member: %v", got.Roles)
	}
	if contains(got.Roles, "data.view.producer:gamma-eo") {
		t.Fatalf("non-member org grant leaked: %v", got.Roles)
	}
}

func TestMergeGrantsNoMatchLeavesBaseIntact(t *testing.T) {
	base := DataRoleResult{Roles: []string{"data.view.org:acme"}, Producers: []string{"acme-sensor"}, Notes: []string{"seed note"}}
	grants := []GrantRow{{GranteeKind: "principal", GranteeID: "nobody", Producer: "x"}}
	got := MergeGrants(base, "uuid-carol", grants)
	if !reflect.DeepEqual(got.Roles, base.Roles) || !reflect.DeepEqual(got.Producers, base.Producers) {
		t.Fatalf("no-match merge altered base: %+v", got)
	}
	if len(got.Notes) != 1 || got.Notes[0] != "seed note" {
		t.Fatalf("no-match merge should not append an applied-grant note: %v", got.Notes)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
