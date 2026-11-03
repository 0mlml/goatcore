package orgstore

import (
	"errors"
	"testing"
	"time"
)

func TestMergeAliasAbsorption(t *testing.T) {
	s := open(t)
	base := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	clock := base
	s.Now = func() time.Time { return clock }

	mk(t, s, "org125x")
	mk(t, s, "org123x")
	mk(t, s, "org124x-dup") // the duplicate to absorb
	mk(t, s, "org124x-dup-child")
	if err := s.AddAlias("fortele-aeriene", "org124x-dup", "name-variant", "op"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetParent("org124x-dup", "org125x", "op", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetParent("org124x-dup-child", "org124x-dup", "op", ""); err != nil {
		t.Fatal(err)
	}

	// Fail-closed: no acknowledgment, no merge.
	if err := s.MergeOrg("org124x-dup", "org123x", "op", "dup", false); !errors.Is(err, ErrValidation) {
		t.Fatalf("unacknowledged merge: want ErrValidation, got %v", err)
	}

	clock = base.Add(time.Hour)
	if err := s.MergeOrg("org124x-dup", "org123x", "op", "duplicate of the air force", true); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// The loser id resolves to the winner — nothing keyed to it breaks.
	if id, err := s.Resolve("org124x-dup"); err != nil || id != "org123x" {
		t.Fatalf("loser resolve: %q err %v", id, err)
	}
	// The loser's pre-merge alias re-pointed to the winner.
	if id, _ := s.Resolve("fortele-aeriene"); id != "org123x" {
		t.Fatalf("alias re-point: %q", id)
	}
	// The loser's child re-parented to the winner, effective-dated.
	if p, _ := s.CurrentParent("org124x-dup-child"); p != "org123x" {
		t.Fatalf("child re-point: %q", p)
	}
	if p, _ := s.ParentAsOf("org124x-dup-child", base.Add(30*time.Minute)); p != "org124x-dup" {
		t.Fatalf("child history lost: as-of pre-merge = %q want org124x-dup", p)
	}
	// The loser row is absorbed, not gone; absorbed orgs leave Suggest.
	v, err := s.GetOrg("org124x-dup") // resolves to winner
	if err != nil || v.ID != "org123x" {
		t.Fatalf("get by loser id: %+v err %v", v, err)
	}
	cs, _ := s.Suggest("org124x dup", "", 5)
	for _, c := range cs {
		if c.ID == "org124x-dup" {
			t.Fatalf("absorbed org still suggested: %+v", cs)
		}
	}
	// Re-merge refused; merging something INTO an absorbed org resolves to
	// the winner and self-refuses.
	if err := s.MergeOrg("org124x-dup", "org123x", "op", "", true); !errors.Is(err, ErrValidation) {
		t.Fatalf("re-merge: want ErrValidation (already resolves), got %v", err)
	}

	// Un-merge: the alias row closes, the row reactivates; edges stay as
	// history (deliberately not auto-reverted).
	if err := s.UnmergeOrg("org124x-dup", "op"); err != nil {
		t.Fatalf("unmerge: %v", err)
	}
	if id, _ := s.Resolve("org124x-dup"); id != "org124x-dup" {
		t.Fatalf("post-unmerge resolve: %q want itself", id)
	}
	v, _ = s.GetOrg("org124x-dup")
	if v.Status != "active" {
		t.Fatalf("post-unmerge status: %s", v.Status)
	}
	// Un-merging a non-absorbed org is refused.
	if err := s.UnmergeOrg("org123x", "op"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unmerge active org: want ErrValidation, got %v", err)
	}
	if err := s.UnmergeOrg("never-existed", "op"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unmerge unknown: want ErrNotFound, got %v", err)
	}
}
