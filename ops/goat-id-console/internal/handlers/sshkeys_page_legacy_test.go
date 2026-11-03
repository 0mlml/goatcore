package handlers

import (
	"context"
	"fmt"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
)

func TestLegacySSHKeyHandleForEmail(t *testing.T) {
	st, err := onboardstore.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	// The legacy join is scoped to this console's goatnet (same trust scope
	// as the install-panel gate), so the server needs one configured.
	srv := &Server{Onboard: st, Cfg: &config.Config{NotifyGoatnet: "zagros"}}
	ctx := context.Background()

	keyIdx := 0

	// Helper to add keys
	addKey := func(handle, email, env, status string) {
		keyIdx++
		k, err := st.SubmitSSHKey(ctx, &onboardstore.SSHKey{
			Handle:  handle,
			Email:   email,
			Env:     env,
			KeyType: "ssh-ed25519",
			KeyBody: fmt.Sprintf("AAAAC3NzaC1lZDI1NTE5AAAAIFlqrvvrCRCKdT58z056yi4wpTZwpq9K/iGxL5H8c%03d", keyIdx),
		}, 90)
		if err != nil {
			t.Fatalf("SubmitSSHKey for %s: %v", handle, err)
		}
		if status == onboardstore.SSHKeyApproved || status == onboardstore.SSHKeyInstalled {
			err := st.ApproveSSHKey(ctx, k.ID, "system")
			if err != nil {
				t.Fatalf("ApproveSSHKey for %s: %v", handle, err)
			}
		}
		if status == onboardstore.SSHKeyInstalled {
			if err := st.MarkSSHKeyInstalled(ctx, k.ID, "system"); err != nil {
				t.Fatalf("MarkSSHKeyInstalled for %s: %v", handle, err)
			}
		}
	}

	addKey("admin-bob", "bob@example.com", "zagros", onboardstore.SSHKeyInstalled)
	addKey("admin-alice", "Alice@EXAMPLE.com", "zagros", onboardstore.SSHKeyInstalled)
	addKey("admin-eve1", "eve@example.com", "zagros", onboardstore.SSHKeyInstalled)
	addKey("admin-eve2", "eve@example.com", "zagros", onboardstore.SSHKeyInstalled)
	addKey("admin-sam", "sam@example.com", "zagros", onboardstore.SSHKeyInstalled)
	addKey("admin-sam", "sam@example.com", "zagros", onboardstore.SSHKeyInstalled) // duplicate handle
	addKey("admin-user123x", "user123x@example.com", "zagros", onboardstore.SSHKeyApproved)
	// Cross-goatnet + empty-env rows must never vouch for a handle here:
	// the store is env-agnostic, but the join's trust scope is this console's.
	addKey("admin-dave", "dave@example.com", "zagros-prod", onboardstore.SSHKeyInstalled)
	addKey("admin-erin", "erin@example.com", "", onboardstore.SSHKeyInstalled)
	// A cross-env row for an email that ALSO has an in-env row must not make
	// the mapping ambiguous — only the in-env row counts.
	addKey("admin-bob-prod", "bob@example.com", "zagros-prod", onboardstore.SSHKeyInstalled)

	cases := []struct {
		email string
		want  string
	}{
		{"bob@example.com", "admin-bob"},
		{" BOB@example.com ", "admin-bob"},
		{"alice@example.com", "admin-alice"},
		{"Alice@EXAMPLE.com", "admin-alice"},
		{"eve@example.com", ""},
		{"sam@example.com", "admin-sam"},
		{"user123x@example.com", ""},
		{"dave@example.com", ""},
		{"erin@example.com", ""},
		{"", ""},
		{"unknown@example.com", ""},
	}

	for _, tc := range cases {
		t.Run(tc.email, func(t *testing.T) {
			got, err := srv.legacySSHKeyHandleForEmail(ctx, tc.email)
			if err != nil {
				t.Fatalf("legacySSHKeyHandleForEmail(%q): %v", tc.email, err)
			}
			if got != tc.want {
				t.Errorf("legacySSHKeyHandleForEmail(%q) = %q; want %q", tc.email, got, tc.want)
			}
		})
	}

	// No configured goatnet → no legacy join at all (fail closed), even for
	// an email with an unambiguous installed row.
	t.Run("no goatnet configured", func(t *testing.T) {
		srvNoEnv := &Server{Onboard: st, Cfg: &config.Config{}}
		got, err := srvNoEnv.legacySSHKeyHandleForEmail(ctx, "bob@example.com")
		if err != nil {
			t.Fatalf("legacySSHKeyHandleForEmail with no goatnet: %v", err)
		}
		if got != "" {
			t.Errorf("legacySSHKeyHandleForEmail with no goatnet = %q; want \"\"", got)
		}
	})
}
