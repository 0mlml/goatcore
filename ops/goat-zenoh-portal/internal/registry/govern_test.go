package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
)

func governReg(t *testing.T) *Registry {
	t.Helper()
	r, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestGovernAdmins_NominateListAndNarrow(t *testing.T) {
	r := governReg(t)
	ctx := context.Background()

	// No nominations anywhere → nothing narrowed.
	narrowed, err := r.GovernNarrowedOrgs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(narrowed) != 0 {
		t.Fatalf("fresh store must narrow nothing: %v", narrowed)
	}

	g := &GovernAdmin{Org: "unit", IdentityID: "carol-uuid", NominatedBy: "operator"}
	if err := r.NominateGovernAdmin(ctx, g); err != nil {
		t.Fatalf("nominate: %v", err)
	}
	if g.ID == "" || g.Status != GovernAdminActive {
		t.Fatalf("row not initialized: %+v", g)
	}

	// Duplicate active nomination refused.
	if err := r.NominateGovernAdmin(ctx, &GovernAdmin{Org: "unit", IdentityID: "carol-uuid", NominatedBy: "operator"}); !errors.Is(err, ErrGovernAdminExists) {
		t.Fatalf("duplicate must be refused, got %v", err)
	}

	narrowed, _ = r.GovernNarrowedOrgs(ctx)
	if !narrowed["unit"] {
		t.Fatalf("unit must be narrowed after its first nomination")
	}
	adminOf, _ := r.GovernAdminOrgs(ctx, "carol-uuid")
	if !adminOf["unit"] {
		t.Fatalf("carol must be an admin of unit")
	}
	if of, _ := r.GovernAdminOrgs(ctx, "nobody"); len(of) != 0 {
		t.Fatalf("a non-admin identity must have no admin orgs")
	}

	rows, err := r.ListGovernAdmins(ctx, "unit", true)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %v / %d rows", err, len(rows))
	}
}

func TestGovernAdmins_LastAdminRemovalRefused(t *testing.T) {
	r := governReg(t)
	ctx := context.Background()
	a := &GovernAdmin{Org: "unit", IdentityID: "carol-uuid", NominatedBy: "op"}
	if err := r.NominateGovernAdmin(ctx, a); err != nil {
		t.Fatal(err)
	}

	// Sole admin: removal refused — narrowing is monotonic, never fall-open.
	if err := r.RemoveGovernAdmin(ctx, a.ID, "op"); !errors.Is(err, ErrLastGovernAdmin) {
		t.Fatalf("last-admin removal must be refused, got %v", err)
	}

	// Nominate a second admin, then the first removal succeeds.
	b := &GovernAdmin{Org: "unit", IdentityID: "dave-uuid", NominatedBy: "op"}
	if err := r.NominateGovernAdmin(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveGovernAdmin(ctx, a.ID, "op"); err != nil {
		t.Fatalf("remove with a replacement present: %v", err)
	}
	got, _ := r.GetGovernAdmin(ctx, a.ID)
	if got.Status != GovernAdminRemoved || got.RemovedAt == nil || got.RemovedBy == nil {
		t.Fatalf("removal must end-date the row: %+v", got)
	}
	// Removing an already-removed row is a benign no-op.
	if err := r.RemoveGovernAdmin(ctx, a.ID, "op"); err != nil {
		t.Fatalf("idempotent removal: %v", err)
	}
	// The org stays narrowed (dave is still active); carol no longer admins.
	narrowed, _ := r.GovernNarrowedOrgs(ctx)
	if !narrowed["unit"] {
		t.Fatalf("org must stay narrowed while an admin remains")
	}
	if of, _ := r.GovernAdminOrgs(ctx, "carol-uuid"); of["unit"] {
		t.Fatalf("removed admin must lose the org")
	}
	// Unknown id → ErrNotFound.
	if err := r.RemoveGovernAdmin(ctx, "gadm-nope", "op"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}
