package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeKeyFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func mustStore(t *testing.T, env map[string]string, dir string) *OperatorKeyStore {
	t.Helper()
	s, err := NewOperatorKeyStore(env, dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s
}

// The point of the whole exercise: a key that appears while the portal is
// running is accepted without a restart, and one that disappears stops working.
// Issuing used to mean an envrc edit plus a redeploy.
func TestOperatorKeyStore_grantsAndRevokesWithoutRestart(t *testing.T) {
	dir := t.TempDir()
	s := mustStore(t, nil, dir)
	if got := s.Match([]byte("op_new")); got != "" {
		t.Fatalf("key not issued yet, got actor %q", got)
	}

	writeKeyFile(t, dir, "ada.key", "ada:op_new\n")
	if _, err := s.Reload(); err != nil {
		t.Fatalf("reload after grant: %v", err)
	}
	if got := s.Match([]byte("op_new")); got != "ada" {
		t.Errorf("granted key must resolve to its handle, got %q", got)
	}

	// Revocation is removal. If this did not take effect, revoking would mean
	// restarting the portal — the exact operation this store exists to avoid.
	if err := os.Remove(filepath.Join(dir, "ada.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reload(); err != nil {
		t.Fatalf("reload after revoke: %v", err)
	}
	if got := s.Match([]byte("op_new")); got != "" {
		t.Errorf("revoked key still accepted as %q", got)
	}
}

// A directory that cannot be READ must not log everyone out. Losing a bind
// mount for a moment is an availability event; turning it into a fleet-wide
// deauthentication would be an outage.
func TestOperatorKeyStore_unreadableDirKeepsLastGoodSet(t *testing.T) {
	dir := t.TempDir()
	writeKeyFile(t, dir, "ada.key", "ada:op_live\n")
	s := mustStore(t, nil, dir)

	// A file that cannot be read is the realistic failure — a directory chmod
	// would not be portable as a test when running as root.
	bad := writeKeyFile(t, dir, "broken.key", "not-a-pair-at-all\n")
	if _, err := s.Reload(); err == nil {
		t.Fatal("a malformed key file must be reported, not silently skipped")
	}
	if got := s.Match([]byte("op_live")); got != "ada" {
		t.Errorf("a failed reload must keep the previous keys working, got %q", got)
	}
	if s.LastError() == nil {
		t.Error("a stale set must be able to say WHY it is stale")
	}

	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reload(); err != nil {
		t.Fatalf("recovery reload: %v", err)
	}
	if s.LastError() != nil {
		t.Errorf("a good reload must clear the stale marker, got %v", s.LastError())
	}
}

// Env keys are the deployment's intent and the break-glass tier. Nothing
// written into the directory may redefine or remove one.
func TestOperatorKeyStore_envWinsAndCannotBeRevokedByTheDirectory(t *testing.T) {
	dir := t.TempDir()
	writeKeyFile(t, dir, "collide.key", "ada:op_from_dir\nbob:op_bob\n")
	s := mustStore(t, map[string]string{"ada": "op_from_env"}, dir)

	if got := s.Match([]byte("op_from_env")); got != "ada" {
		t.Errorf("env key must win, got %q", got)
	}
	if got := s.Match([]byte("op_from_dir")); got != "" {
		t.Errorf("directory must not redefine a handle the deployment pinned, got %q", got)
	}
	if got := s.Match([]byte("op_bob")); got != "bob" {
		t.Errorf("a handle the env does not claim must still come from the directory, got %q", got)
	}

	// Emptying the directory revokes bob and leaves ada untouched.
	if err := os.Remove(filepath.Join(dir, "collide.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := s.Match([]byte("op_from_env")); got != "ada" {
		t.Error("an empty directory must not revoke the env tier")
	}
	if got := s.Match([]byte("op_bob")); got != "" {
		t.Errorf("directory handle should be gone, got %q", got)
	}
}

// A writer that creates a temp file and renames it into place is doing the
// right thing. Reading its half-written temp file and hard-failing the whole
// reload would punish correct behaviour.
func TestOperatorKeyStore_ignoresPartialWrites(t *testing.T) {
	dir := t.TempDir()
	writeKeyFile(t, dir, "ada.key", "ada:op_live\n")
	writeKeyFile(t, dir, "ada.key.tmp", "ada:op_half")
	writeKeyFile(t, dir, ".hidden.swp", "garbage-not-a-pair")
	s := mustStore(t, nil, dir)
	if got := s.Match([]byte("op_live")); got != "ada" {
		t.Errorf("real key must load alongside temp files, got %q", got)
	}
}

// Watch is what makes the whole thing live. It must report WHO changed, since
// "the operator key set changed" is not an actionable log line.
func TestOperatorKeyStore_watchReportsWhoGainedAndLost(t *testing.T) {
	dir := t.TempDir()
	writeKeyFile(t, dir, "bob.key", "bob:op_bob\n")
	s := mustStore(t, nil, dir)

	type change struct{ added, removed []string }
	ch := make(chan change, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Watch(ctx, 10*time.Millisecond, func(added, removed []string) {
		ch <- change{added, removed}
	}, nil)

	writeKeyFile(t, dir, "ada.key", "ada:op_ada\n")
	if err := os.Remove(filepath.Join(dir, "bob.key")); err != nil {
		t.Fatal(err)
	}

	select {
	case c := <-ch:
		if strings.Join(c.added, ",") != "ada" {
			t.Errorf("added = %v, want [ada]", c.added)
		}
		if strings.Join(c.removed, ",") != "bob" {
			t.Errorf("removed = %v, want [bob]", c.removed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch never reported the change — the directory is not live")
	}

	if got := s.Match([]byte("op_ada")); got != "ada" {
		t.Errorf("watch must install the new set, got %q", got)
	}
	if got := s.Match([]byte("op_bob")); got != "" {
		t.Errorf("watch must honour removal, got %q", got)
	}
}

// Handles feeds logs. A store that will print its own secrets eventually prints
// them somewhere durable.
func TestOperatorKeyStore_handlesNeverExposeKeys(t *testing.T) {
	dir := t.TempDir()
	writeKeyFile(t, dir, "ada.key", "ada:op_secret_value\n")
	s := mustStore(t, map[string]string{"env-op": "op_env_secret"}, dir)
	for _, h := range s.Handles() {
		if strings.Contains(h, "op_") {
			t.Errorf("Handles() leaked a key: %q", h)
		}
	}
	if got, want := len(s.Handles()), 2; got != want {
		t.Errorf("Handles() = %d entries, want %d", got, want)
	}
}

// A directory key for a handle the env tier also pins never takes effect, so
// revoking it in the console changes nothing. The store must report the
// collision instead of resolving it silently, and must not change which key
// wins: removing an env pin is a deployment decision, not the store's.
func TestOperatorKeyStore_reportsHandlesShadowedByEnv(t *testing.T) {
	dir := t.TempDir()
	writeKeyFile(t, dir, "ada.key", "ada:op_dir_ada\n")
	writeKeyFile(t, dir, "bob.key", "bob:op_dir_bob\n")
	s := mustStore(t, map[string]string{"ada": "op_env_ada", "pinned": "op_env_pinned"}, dir)

	if got := strings.Join(s.Shadowed(), ","); got != "ada" {
		t.Fatalf("Shadowed() = %q, want ada", got)
	}
	if got := s.Match([]byte("op_env_ada")); got != "ada" {
		t.Errorf("env key must still win the collision, got %q", got)
	}
	if got := s.Match([]byte("op_dir_ada")); got != "" {
		t.Errorf("shadowed directory key must stay refused, got %q", got)
	}
	for _, h := range s.Shadowed() {
		if strings.Contains(h, "op_") {
			t.Errorf("Shadowed() leaked a key: %q", h)
		}
	}

	if err := os.Remove(filepath.Join(dir, "ada.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := s.Shadowed(); len(got) != 0 {
		t.Errorf("Shadowed() after removing the directory key = %v, want none", got)
	}
}

// The live tier is reloaded by Watch, not only by Reload. A console that issues
// a key for an env-pinned handle while the portal runs must show up as shadowed
// even though the accepted handle set did not change.
func TestOperatorKeyStore_watchUpdatesShadowed(t *testing.T) {
	dir := t.TempDir()
	s := mustStore(t, map[string]string{"ada": "op_env_ada"}, dir)
	if got := s.Shadowed(); len(got) != 0 {
		t.Fatalf("Shadowed() with an empty directory = %v, want none", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Watch(ctx, 10*time.Millisecond, nil, nil)

	writeKeyFile(t, dir, "ada.key", "ada:op_dir_ada\n")
	deadline := time.Now().Add(3 * time.Second)
	for strings.Join(s.Shadowed(), ",") != "ada" {
		if time.Now().After(deadline) {
			t.Fatalf("watch never reported the shadowed handle, Shadowed() = %v", s.Shadowed())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A malformed key line is still a credential. The parse error reaches the
// boot and reload logs, so it must locate the line without quoting it.
func TestOperatorKeyStore_malformedLineErrorNeverQuotesTheKey(t *testing.T) {
	for name, body := range map[string]string{
		"extra colon":  "ada:op_secret_value:trailing\n",
		"empty handle": ":op_secret_value\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeKeyFile(t, dir, "ada.key", "# comment\n"+body)
			_, err := NewOperatorKeyStore(nil, dir)
			if err == nil {
				t.Fatal("NewOperatorKeyStore accepted a malformed line")
			}
			if strings.Contains(err.Error(), "op_secret_value") {
				t.Errorf("error leaked the key: %v", err)
			}
			if !strings.Contains(err.Error(), "line 2") {
				t.Errorf("error does not locate the line: %v", err)
			}
		})
	}
}
