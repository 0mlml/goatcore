// idempotency_test.go — pure-function tests for the Block 50B.8 idempotency
// helpers. The DB-touching path (runIdempotent) is exercised in the
// integration test (cmd_integration_test.go).

package main

import (
	"strings"
	"testing"
)

// TestIdempotencyKeyFor verifies determinism + sensitivity to input
// reordering. Same inputs → same key; different inputs → different key.
func TestIdempotencyKeyFor(t *testing.T) {
	a := idempotencyKeyFor("create-account", "alice@example.com", "vega", "100.64.0.0/16", "vega.local")
	b := idempotencyKeyFor("create-account", "alice@example.com", "vega", "100.64.0.0/16", "vega.local")
	if a != b {
		t.Errorf("same inputs produced different keys:\n  %s\n  %s", a, b)
	}

	c := idempotencyKeyFor("create-account", "bob@example.com", "vega", "100.64.0.0/16", "vega.local")
	if a == c {
		t.Errorf("different owner produced same key: %s", a)
	}

	// Reordered inputs should yield different key — important so two
	// commands with similar argument sets don't collide.
	d := idempotencyKeyFor("create-account", "vega", "alice@example.com", "100.64.0.0/16", "vega.local")
	if a == d {
		t.Errorf("reordered inputs produced same key: %s", a)
	}

	// Key shape: "<command>:<hex-sha256>". Useful for human inspection of
	// the bootstrap_idempotency table.
	if !strings.HasPrefix(a, "create-account:") {
		t.Errorf("key %q missing command prefix", a)
	}
	suffix := strings.TrimPrefix(a, "create-account:")
	if len(suffix) != 64 {
		t.Errorf("hex suffix length = %d, want 64", len(suffix))
	}
}

// TestResolveIdempotencyKey covers the explicit-vs-derived branch.
func TestResolveIdempotencyKey(t *testing.T) {
	derived := resolveIdempotencyKey("", "create-account", "alice", "vega")
	if !strings.HasPrefix(derived, "create-account:") {
		t.Errorf("derived key did not get command prefix: %q", derived)
	}
	explicit := resolveIdempotencyKey("operator-supplied-key", "create-account", "alice", "vega")
	if explicit != "operator-supplied-key" {
		t.Errorf("explicit key not preserved: %q", explicit)
	}
}

// TestAdvisoryLockKey verifies determinism + non-negative output (Postgres
// pg_advisory_xact_lock(int8) accepts negatives but we mask the sign bit so
// keys land in a single half of the lock space).
func TestAdvisoryLockKey(t *testing.T) {
	k1 := advisoryLockKey("foo")
	k2 := advisoryLockKey("foo")
	if k1 != k2 {
		t.Errorf("non-deterministic: %d != %d", k1, k2)
	}
	if k1 < 0 {
		t.Errorf("advisoryLockKey returned negative %d (sign bit not masked)", k1)
	}
	if advisoryLockKey("foo") == advisoryLockKey("bar") {
		t.Error("expected different inputs to map to different lock keys")
	}
}

// TestIsNoRows guards against the test pattern silently breaking if a
// future database/sql change reshapes the sentinel string.
func TestIsNoRows(t *testing.T) {
	cases := []struct {
		errStr string
		want   bool
	}{
		{"sql: no rows in result set", true},
		{"some other error", false},
		{"", false},
	}
	for _, c := range cases {
		got := isNoRows(testErr(c.errStr))
		if got != c.want {
			t.Errorf("isNoRows(%q) = %v, want %v", c.errStr, got, c.want)
		}
	}
	if isNoRows(nil) {
		t.Error("isNoRows(nil) should be false")
	}
}

type testErr string

func (e testErr) Error() string { return string(e) }
