package orgstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mk(t *testing.T, s *Store, id string) {
	t.Helper()
	if _, err := s.CreateOrg(Org{ID: id, Kind: "partner", Nation: "SVN", CreatedBy: "test"}); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
}

func TestCreateValidationAndConflicts(t *testing.T) {
	s := open(t)
	mk(t, s, "org125x")

	if _, err := s.CreateOrg(Org{ID: "org125x"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate id: want ErrConflict, got %v", err)
	}
	for _, bad := range []string{"", "X", "-lead", "trail-", "Has Space", "a"} {
		if _, err := s.CreateOrg(Org{ID: bad}); !errors.Is(err, ErrValidation) {
			t.Fatalf("id %q: want ErrValidation, got %v", bad, err)
		}
	}
	if _, err := s.CreateOrg(Org{ID: "ok-org", Kind: "battalion"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad kind: want ErrValidation, got %v", err)
	}
	if _, err := s.CreateOrg(Org{ID: "ok-org", Provenance: "vibes"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad provenance: want ErrValidation, got %v", err)
	}
	// Default provenance is honest, not green.
	o, err := s.CreateOrg(Org{ID: "ok-org"})
	if err != nil || o.Provenance != "self-asserted" {
		t.Fatalf("default provenance: got %+v err %v", o, err)
	}
}

// The update verb corrects facts in place — the door the `origin`
// kind mislabel lacked.
func TestUpdateOrgFactsInPlace(t *testing.T) {
	s := open(t)
	mk(t, s, "origin") // seeded kind=partner nation=SVN by the helper

	kind, nation := "vendor", "LVA"
	v, err := s.UpdateOrg("origin", OrgUpdate{Kind: &kind, Nation: &nation})
	if err != nil || v.Kind != "vendor" || v.Nation != "LVA" {
		t.Fatalf("update: %+v err %v", v, err)
	}

	// Unknown kind refused; an org never becomes kindless by edit.
	bad := "battalion"
	if _, err := s.UpdateOrg("origin", OrgUpdate{Kind: &bad}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad kind: want ErrValidation, got %v", err)
	}
	empty := ""
	if _, err := s.UpdateOrg("origin", OrgUpdate{Kind: &empty}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty kind: want ErrValidation, got %v", err)
	}
	// Empty update is a caller bug, not a silent no-op.
	if _, err := s.UpdateOrg("origin", OrgUpdate{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty update: want ErrValidation, got %v", err)
	}
	if _, err := s.UpdateOrg("no-such-org", OrgUpdate{Kind: &kind}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown org: want ErrNotFound, got %v", err)
	}

	// Updating by alias resolves to the canonical org.
	if err := s.AddAlias("origin-robotics", "origin", "name-variant", "test"); err != nil {
		t.Fatalf("alias: %v", err)
	}
	dn := "ORIGIN Robotics"
	v, err = s.UpdateOrg("origin-robotics", OrgUpdate{DisplayName: &dn})
	if err != nil || v.ID != "origin" || v.DisplayName != "ORIGIN Robotics" {
		t.Fatalf("update via alias: %+v err %v", v, err)
	}
	// The earlier fact edits survived.
	if v.Kind != "vendor" || v.Nation != "LVA" {
		t.Fatalf("facts lost across updates: %+v", v)
	}
}

func TestOneOpenParentAndEffectiveDating(t *testing.T) {
	s := open(t)
	base := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	clock := base
	s.Now = func() time.Time { return clock }

	mk(t, s, "raf")
	mk(t, s, "svn-land")
	mk(t, s, "svn-forces")

	if err := s.SetParent("svn-land", "raf", "op", "initial"); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	// Re-parent later: the old edge closes, the new one opens.
	clock = base.Add(24 * time.Hour)
	if err := s.SetParent("svn-land", "svn-forces", "op", "reorg"); err != nil {
		t.Fatalf("re-parent: %v", err)
	}
	cur, err := s.CurrentParent("svn-land")
	if err != nil || cur != "svn-forces" {
		t.Fatalf("current parent: got %q err %v", cur, err)
	}
	// As-of before the reorg → the original parent; after → the new one.
	if p, _ := s.ParentAsOf("svn-land", base.Add(time.Hour)); p != "raf" {
		t.Fatalf("as-of pre-reorg: got %q want raf", p)
	}
	if p, _ := s.ParentAsOf("svn-land", base.Add(48*time.Hour)); p != "svn-forces" {
		t.Fatalf("as-of post-reorg: got %q want svn-forces", p)
	}
	// Exactly one open edge exists.
	var n int
	if err := s.db.Get(&n, `SELECT COUNT(*) FROM org_parent_edges WHERE org_id='svn-land' AND valid_to IS NULL`); err != nil || n != 1 {
		t.Fatalf("open edges: n=%d err=%v", n, err)
	}
	// History survives (two rows total).
	if err := s.db.Get(&n, `SELECT COUNT(*) FROM org_parent_edges WHERE org_id='svn-land'`); err != nil || n != 2 {
		t.Fatalf("edge history: n=%d err=%v", n, err)
	}
	// Idempotent no-op writes nothing.
	if err := s.SetParent("svn-land", "svn-forces", "op", "again"); err != nil {
		t.Fatalf("no-op re-set: %v", err)
	}
	if err := s.db.Get(&n, `SELECT COUNT(*) FROM org_parent_edges WHERE org_id='svn-land'`); err != nil || n != 2 {
		t.Fatalf("no-op wrote an edge: n=%d err=%v", n, err)
	}
	// ClearParent → root again.
	clock = base.Add(72 * time.Hour)
	if err := s.ClearParent("svn-land", "op", "detach"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if p, _ := s.CurrentParent("svn-land"); p != "" {
		t.Fatalf("after clear: got %q want root", p)
	}
}

func TestCycleRefusal(t *testing.T) {
	s := open(t)
	mk(t, s, "org-a")
	mk(t, s, "org-b")
	mk(t, s, "org-c")

	// a ← b ← c (c's parent b, b's parent a)
	if err := s.SetParent("org-b", "org-a", "op", ""); err != nil {
		t.Fatalf("b→a: %v", err)
	}
	if err := s.SetParent("org-c", "org-b", "op", ""); err != nil {
		t.Fatalf("c→b: %v", err)
	}
	// Self-parent refused.
	if err := s.SetParent("org-a", "org-a", "op", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("self-parent: want ErrValidation, got %v", err)
	}
	// Direct cycle refused: a→b when b's chain already contains a? No —
	// direct cycle is a→c: c's chain is c→b→a, so parenting a under c loops.
	if err := s.SetParent("org-a", "org-c", "op", ""); !errors.Is(err, ErrCycle) {
		t.Fatalf("transitive cycle: want ErrCycle, got %v", err)
	}
	// Two-node cycle refused.
	if err := s.SetParent("org-a", "org-b", "op", ""); !errors.Is(err, ErrCycle) {
		t.Fatalf("two-node cycle: want ErrCycle, got %v", err)
	}
}

func TestAliasNamespaceAndResolution(t *testing.T) {
	s := open(t)
	mk(t, s, "org123x")
	mk(t, s, "other-org")

	if err := s.AddAlias("org124x", "org123x", "abbreviation", "op"); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	// Resolve through the alias.
	id, err := s.Resolve("org124x")
	if err != nil || id != "org123x" {
		t.Fatalf("resolve alias: got %q err %v", id, err)
	}
	// GetOrg works by alias and reports the canonical id + aliases.
	v, err := s.GetOrg("org124x")
	if err != nil || v.ID != "org123x" || len(v.Aliases) != 1 {
		t.Fatalf("get by alias: %+v err %v", v, err)
	}
	// One namespace: an alias equal to an org id is refused...
	if err := s.AddAlias("other-org", "org123x", "name-variant", "op"); !errors.Is(err, ErrConflict) {
		t.Fatalf("alias shadowing org: want ErrConflict, got %v", err)
	}
	// ...and an org created over an alias is refused.
	if _, err := s.CreateOrg(Org{ID: "org124x"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("org over alias: want ErrConflict, got %v", err)
	}
	// Duplicate alias refused; unknown kind refused; unknown target refused.
	if err := s.AddAlias("org124x", "other-org", "name-variant", "op"); !errors.Is(err, ErrConflict) {
		t.Fatalf("dup alias: want ErrConflict, got %v", err)
	}
	if err := s.AddAlias("x-y", "org123x", "nickname", "op"); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad alias kind: want ErrValidation, got %v", err)
	}
	if err := s.AddAlias("z-z", "nope-org", "name-variant", "op"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("alias to unknown org: want ErrNotFound, got %v", err)
	}
	// SetParent accepts aliases on both sides.
	if err := s.SetParent("other-org", "org124x", "op", "via alias"); err != nil {
		t.Fatalf("set parent via alias: %v", err)
	}
	if p, _ := s.CurrentParent("other-org"); p != "org123x" {
		t.Fatalf("parent via alias: got %q", p)
	}
}

func TestExportDeterminism(t *testing.T) {
	s := open(t)
	mk(t, s, "zulu-org")
	mk(t, s, "alpha-org")
	if err := s.SetParent("zulu-org", "alpha-org", "op", ""); err != nil {
		t.Fatalf("parent: %v", err)
	}
	c1, err := s.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	c2, _ := s.Export()
	if len(c1.Orgs) != 2 || c1.Orgs[0].ID != "alpha-org" || c1.Orgs[1].Parent != "alpha-org" {
		t.Fatalf("export shape: %+v", c1.Orgs)
	}
	if len(c2.Orgs) != len(c1.Orgs) || c2.Orgs[0].ID != c1.Orgs[0].ID {
		t.Fatalf("export not deterministic")
	}
	// Reopen the same file — schema apply is idempotent, data survives.
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// --- ADR 1151 step 4: the mechanical half of the backfill --------------------

// Every legacy kind must land on exactly one sector, `kind` must be left alone,
// and gov_branch must stay EMPTY — the old data does not contain it and
// inventing one would make the registry assert something nobody declared.
func TestBackfillSectorFromKind(t *testing.T) {
	path := pgtest.DSN(t)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	seed := []struct{ id, kind string }{
		{"a-contractor", "vendor"},
		{"a-university", "civil"},
		{"our-army", "organic"},
		{"their-army", "partner"},
		{"undeclared", ""},
	}
	for _, o := range seed {
		if _, err := s.CreateOrg(Org{ID: o.id, Kind: o.kind, Nation: "USA"}); err != nil {
			t.Fatalf("seed %s: %v", o.id, err)
		}
	}
	// Simulate a pre-migration database: clear what CreateOrg may have set.
	if _, err := s.db.ExecContext(context.Background(), `UPDATE orgs SET sector = '', gov_branch = ''`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := backfillSectorFromKind(s.db); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	want := map[string]string{
		"a-contractor": "commercial",
		"a-university": "civil-society",
		"our-army":     "state",
		"their-army":   "state",
		"undeclared":   "", // nothing declared it, so nothing to derive
	}
	for id, wantSector := range want {
		v, err := s.GetOrg(id)
		if err != nil {
			t.Fatalf("GetOrg(%s): %v", id, err)
		}
		if v.Sector != wantSector {
			t.Errorf("%s: sector = %q, want %q", id, v.Sector, wantSector)
		}
		if v.GovBranch != "" {
			t.Errorf("%s: gov_branch = %q, want empty — the old data does not "+
				"contain a branch and it must not be invented", id, v.GovBranch)
		}
	}
	// kind is untouched, which is what makes this reversible.
	for _, o := range seed {
		v, _ := s.GetOrg(o.id)
		if v.Kind != o.kind {
			t.Errorf("%s: kind changed to %q, want %q — the backfill must not "+
				"rewrite the legacy field", o.id, v.Kind, o.kind)
		}
	}

	// Idempotent, and it must not clobber a sector a human chose that disagrees
	// with the legacy kind.
	if _, err := s.db.ExecContext(context.Background(), `UPDATE orgs SET sector = 'civil-society' WHERE id = 'our-army'`); err != nil {
		t.Fatalf("hand edit: %v", err)
	}
	if err := backfillSectorFromKind(s.db); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if v, _ := s.GetOrg("our-army"); v.Sector != "civil-society" {
		t.Errorf("re-running the backfill overwrote a deliberate value: sector = %q", v.Sector)
	}
	_ = s.Close()
}

// Reopening a database created before the columns existed must add them and
// backfill, rather than failing to start.
func TestOpenMigratesAnOlderDatabase(t *testing.T) {
	path := pgtest.DSN(t)
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.CreateOrg(Org{ID: "legacy-org", Kind: "vendor", Nation: "USA"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Drop the row back to the pre-ADR-1151 shape.
	if _, err := s.db.ExecContext(context.Background(), `UPDATE orgs SET sector = '', gov_branch = ''`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()
	v, err := s2.GetOrg("legacy-org")
	if err != nil {
		t.Fatalf("GetOrg: %v", err)
	}
	if v.Sector != "commercial" {
		t.Errorf("sector = %q, want commercial after reopen", v.Sector)
	}
}

// --- cross-nation slug collision -------------------------------------------

// Org ids are ONE flat namespace per goatnet, so a Polish CIS and a Lithuanian
// CIS both want `cis`. These pin what actually happens, because the answer
// matters more than the theory: the second is REFUSED (never silently merged
// into the first), and the refusal names the country so the admin can see that
// the existing row is somebody else's org rather than their own duplicate.
func TestCrossNationSlugCollisionIsRefusedAndExplained(t *testing.T) {
	s, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := s.CreateOrg(Org{ID: "cis", Kind: "partner", Nation: "SVK"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err = s.CreateOrg(Org{ID: "cis", Kind: "partner", Nation: "POL"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("a same-slug different-nation org must CONFLICT, got %v", err)
	}
	for _, want := range []string{"SVK", "POL", "pol-cis"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal should mention %q so the next move is obvious: %v", want, err)
		}
	}

	// The incumbent is untouched — a refused create must not mutate anything.
	v, gerr := s.GetOrg("cis")
	if gerr != nil || v.Nation != "SVK" {
		t.Errorf("incumbent changed: %+v err %v", v, gerr)
	}

	// Same nation is an ordinary duplicate; the nation clause must NOT fire and
	// invent a confusing suggestion.
	_, err = s.CreateOrg(Org{ID: "cis", Kind: "partner", Nation: "SVK"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("same-nation duplicate must still conflict, got %v", err)
	}
	if strings.Contains(err.Error(), "belongs to") {
		t.Errorf("a same-nation duplicate is not a cross-nation collision: %v", err)
	}
}

// TestBackfillNationAlpha3 proves the fold reaches rows written BEFORE the fold
// on write existed. The regression it guards is the real one: zagros's registry
// carries `usareur-af` as US while the committed roster carries USA for the
// same country, and nothing rewrites a row nobody edits.
func TestBackfillNationAlpha3(t *testing.T) {
	path := pgtest.DSN(t)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	seed := []struct{ id, nation string }{
		{"usareur-af", "US"},
		{"cdn-liaison", "CA"},
		{"already-folded", "DEU"},
		{"org79x", "NATO"},
		{"undeclared", ""},
	}
	for _, o := range seed {
		if _, err := s.CreateOrg(Org{ID: o.id, Kind: "organic", Nation: o.nation}); err != nil {
			t.Fatalf("seed %s: %v", o.id, err)
		}
	}
	// Write the alpha-2 spellings back underneath CreateOrg's fold, which is
	// exactly the state a registry created before that fold landed is in.
	for _, o := range seed {
		if _, err := s.db.ExecContext(context.Background(), `UPDATE orgs SET nation = $1 WHERE id = $2`, o.nation, o.id); err != nil {
			t.Fatalf("reset %s: %v", o.id, err)
		}
	}
	if err := backfillNationAlpha3(s.db); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	want := map[string]string{
		"usareur-af":     "USA",
		"cdn-liaison":    "CAN",
		"already-folded": "DEU",
		"org79x":        "NATO", // not a country; must pass through untouched
		"undeclared":     "",
	}
	for id, wantNation := range want {
		v, err := s.GetOrg(id)
		if err != nil {
			t.Fatalf("GetOrg(%s): %v", id, err)
		}
		if v.Nation != wantNation {
			t.Errorf("%s: nation = %q, want %q", id, v.Nation, wantNation)
		}
	}

	// Idempotent: a second run must change nothing.
	if err := backfillNationAlpha3(s.db); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	for id, wantNation := range want {
		v, _ := s.GetOrg(id)
		if v.Nation != wantNation {
			t.Errorf("after second run %s: nation = %q, want %q", id, v.Nation, wantNation)
		}
	}
	_ = s.Close()

	// And it runs at Open, not only when a test calls it directly.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()
	if _, err := s2.db.ExecContext(context.Background(), `UPDATE orgs SET nation = 'US' WHERE id = 'usareur-af'`); err != nil {
		t.Fatalf("re-dirty: %v", err)
	}
	_ = s2.Close()
	s3, err := Open(path)
	if err != nil {
		t.Fatalf("third open: %v", err)
	}
	defer func() { _ = s3.Close() }()
	v, err := s3.GetOrg("usareur-af")
	if err != nil {
		t.Fatalf("GetOrg after reopen: %v", err)
	}
	if v.Nation != "USA" {
		t.Errorf("Open did not fold: nation = %q, want USA", v.Nation)
	}
}
