// main_test.go — Block 50E.12 production-readiness hardening.
//
// Stands up the test scaffold for admit-audit. The package previously
// had zero `*_test.go` files, which left Block 50A.2's
// goleak.VerifyTestMain promise unrealizable for this binary even
// though admit-audit was named in 50A.2's verdict criteria.
//
// Today's coverage is intentionally minimal — three pure functions
// (envDefault, containsCode, isUndefinedColumn). The point is the
// substrate, not breadth. Future tests for the auditor's polling /
// rolling-window / dedup paths bolt on top of this skeleton without
// having to introduce the testing infra at the same time.
//
// goleak guard catches any goroutine the future tests might leak;
// today's pure-function suite has no concurrency.

package main

import (
	"errors"
	"os"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestEnvDefault(t *testing.T) {
	const key = "ADMIT_AUDIT_TEST_KEY_R23ZXQ"
	t.Run("env_unset_returns_default", func(t *testing.T) {
		_ = os.Unsetenv(key)
		if got := envDefault(key, "fallback"); got != "fallback" {
			t.Fatalf("envDefault(unset) = %q, want %q", got, "fallback")
		}
	})
	t.Run("env_set_returns_env", func(t *testing.T) {
		t.Setenv(key, "from-env")
		if got := envDefault(key, "fallback"); got != "from-env" {
			t.Fatalf("envDefault(set) = %q, want %q", got, "from-env")
		}
	})
	t.Run("env_empty_returns_default", func(t *testing.T) {
		// envDefault treats "" as unset (per impl). This locks that contract.
		t.Setenv(key, "")
		if got := envDefault(key, "fallback"); got != "fallback" {
			t.Fatalf("envDefault(empty) = %q, want %q", got, "fallback")
		}
	})
}

func TestContainsCode(t *testing.T) {
	cases := []struct {
		name string
		s    string
		code string
		want bool
	}{
		{"found_at_start", "42703 column undefined", "42703", true},
		{"found_in_middle", "pq: error 42703 in query", "42703", true},
		{"found_at_end", "boom 42703", "42703", true},
		{"not_found", "pq: error 23505 unique violation", "42703", false},
		{"empty_string", "", "42703", false},
		{"code_longer_than_string", "abc", "42703", false},
		{"empty_code_always_matches", "anything", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := containsCode(tc.s, tc.code); got != tc.want {
				t.Fatalf("containsCode(%q, %q) = %v, want %v", tc.s, tc.code, got, tc.want)
			}
		})
	}
}

func TestIsUndefinedColumn(t *testing.T) {
	t.Run("nil_err_is_false", func(t *testing.T) {
		if isUndefinedColumn(nil) {
			t.Fatal("isUndefinedColumn(nil) = true, want false")
		}
	})
	t.Run("non_42703_is_false", func(t *testing.T) {
		err := errors.New("pq: error 23505 unique violation")
		if isUndefinedColumn(err) {
			t.Fatal("isUndefinedColumn(unique-violation) = true, want false")
		}
	})
	t.Run("42703_is_true", func(t *testing.T) {
		err := errors.New("pq: column \"created_at\" does not exist (SQLSTATE 42703)")
		if !isUndefinedColumn(err) {
			t.Fatal("isUndefinedColumn(42703) = false, want true")
		}
	})
}
