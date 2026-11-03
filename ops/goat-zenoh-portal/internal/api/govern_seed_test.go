package api

import (
	"context"
	"os"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/access"
)

// The ADR 1102 cutover: existing orgs keep ALL current members (behavior-
// preserving, NOT retroactive-first-member); the migration runs exactly once;
// a NEW org onboarded afterward gets only its FIRST member seeded.
func TestGovernCutover_AllMembersThenFirstMember(t *testing.T) {
	srv, reg := governAdminServer(t) // org unit: persons carol, dave (+ machines)
	srv.cfg.GovernBootstrapOpen = false
	ctx := context.Background()

	// --- one-time all-members migration for the existing org --------------
	if err := srv.seedExistingOrgsAllMembers(ctx); err != nil {
		t.Fatalf("migration: %v", err)
	}
	for uuid, who := range map[string]string{"carol-uuid": "carol", "dave-uuid": "dave"} {
		if a, _ := reg.GovernAdminOrgs(ctx, uuid); !a["unit"] {
			t.Fatalf("all-members migration must seed %s as an admin of unit (keep all members)", who)
		}
	}
	// The machine-only org (partner) has no person to govern it — it is not
	// seeded (operator proxy stewardship is its recovery path).
	if a, _ := reg.GovernAdminOrgs(ctx, "radar"); a["partner"] {
		t.Fatalf("a machine must never be seeded as an admin")
	}

	// Marker set → migration is one-time and idempotent.
	if _, done, _ := reg.GovernMetaGet(ctx, governCutoverMarkerKey); !done {
		t.Fatalf("cutover completion marker must be set")
	}
	before, _ := reg.ListGovernAdmins(ctx, "unit", true)
	if err := srv.seedExistingOrgsAllMembers(ctx); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	after, _ := reg.ListGovernAdmins(ctx, "unit", true)
	if len(before) != len(after) {
		t.Fatalf("migration must be idempotent: %d → %d admins", len(before), len(after))
	}

	// --- a NEW org onboarded after cutover: first member only -------------
	newRoster := `{
      "generated_at": "2026-08-01T00:00:00Z",
      "orgs": [
        {"id": "unit", "parent": null, "members": [
          {"handle": "carol", "type": "person", "state": "active", "vendor_tag": null, "email_hash": "` + access.EmailHash("carol@example.org") + `"},
          {"handle": "dave",  "type": "person", "state": "active", "vendor_tag": null, "email_hash": "` + access.EmailHash("dave@example.org") + `"}
        ]},
        {"id": "newco", "parent": null, "members": [
          {"handle": "erin",  "type": "person", "state": "active", "vendor_tag": null, "email_hash": "` + access.EmailHash("erin@example.org") + `"},
          {"handle": "frank", "type": "person", "state": "active", "vendor_tag": null, "email_hash": "` + access.EmailHash("frank@example.org") + `"}
        ]}
      ],
      "members": []
    }`
	if err := os.WriteFile(srv.cfg.RosterPath, []byte(newRoster), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.humans = fakeHumans{byUUID: map[string]string{
		"carol-uuid": "carol@example.org", "dave-uuid": "dave@example.org",
		"erin-uuid": "erin@example.org", "frank-uuid": "frank@example.org",
	}}
	if err := srv.reconcileFirstMember(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if a, _ := reg.GovernAdminOrgs(ctx, "erin-uuid"); !a["newco"] {
		t.Fatalf("the new org's FIRST member (erin) must be seeded")
	}
	if a, _ := reg.GovernAdminOrgs(ctx, "frank-uuid"); a["newco"] {
		t.Fatalf("the new org's second member (frank) must NOT be auto-seeded (first-member-only going forward)")
	}
	// The existing org is untouched by the reconcile — it keeps BOTH members,
	// never reduced to just its first (the not-retroactive guarantee).
	if a, _ := reg.GovernAdminOrgs(ctx, "dave-uuid"); !a["unit"] {
		t.Fatalf("the reconcile must not reduce an existing org to its first member")
	}
}

// Under bootstrap-open the cutover is inert: no migration, no marker, no
// nominations — the kick-start posture stands until the operator flips closed.
func TestGovernCutover_InertUnderBootstrapOpen(t *testing.T) {
	srv, reg := governAdminServer(t) // GovernBootstrapOpen defaults true here
	ctx := context.Background()
	srv.RunGovernReconcile(ctx, 0)
	if a, _ := reg.GovernAdminOrgs(ctx, "carol-uuid"); a["unit"] {
		t.Fatalf("bootstrap-open must NOT seed any admin (seeding would narrow the org)")
	}
	if _, done, _ := reg.GovernMetaGet(ctx, governCutoverMarkerKey); done {
		t.Fatalf("bootstrap-open must not write the cutover marker")
	}
}
