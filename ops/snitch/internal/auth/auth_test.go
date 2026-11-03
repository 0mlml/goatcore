// auth_test.go — Block 50F.1 production-readiness hardening.
//
// Backfills tests for Block 50E.2 (URL-query auth path removal +
// Authorization-header-only) and Block 50E.3 (KeyCount + UPDATE
// last_used error surfacing). Both shipped with build + reasoning as
// the only verification; these tests close the gap.
//
// Pattern: testcontainers-go matching ops/mgmt-host-bootstrap's
// Block 50B.3 work. Each test owns its own isolated Postgres
// container; the container is torn down on test exit, panic, OR
// signal via t.Cleanup hooked to the testcontainers reaper.
//
// Run locally (~30s with image cached, ~2 min cold pull):
//
//	cd ops/snitch
//	go test -tags=integration -race -count=1 -v ./internal/auth/...
//
// Prereqs: docker daemon reachable; postgres:16-alpine cached.

//go:build integration

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const apiKeysSchema = `
CREATE TABLE IF NOT EXISTS api_keys (
	id BIGSERIAL PRIMARY KEY,
	key_hash TEXT NOT NULL UNIQUE,
	role TEXT NOT NULL CHECK(role IN ('admin','operator','viewer','probe')),
	label TEXT NOT NULL,
	created_at BIGINT NOT NULL,
	last_used BIGINT
);
`

// startPostgres spins up an isolated Postgres container and returns a
// connected pgxpool plus a cleanup that the test framework will call
// on exit/panic/signal.
func startPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_PASSWORD": "snitchtest",
			"POSTGRES_DB":       "snitch",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(45 * time.Second),
	}
	pgC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = pgC.Terminate(context.Background())
	})

	host, err := pgC.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := pgC.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("container port: %v", err)
	}
	dsn := "postgres://postgres:snitchtest@" + host + ":" + port.Port() + "/snitch?sslmode=disable"
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, apiKeysSchema); err != nil {
		t.Fatalf("create api_keys: %v", err)
	}
	return pool
}

// TestExtractKeyHeaderOnly is the regression test for Block 50E.2.
// The previous implementation accepted ?key=<plain> as a URL query
// fallback; that path leaks API keys into proxy logs / CDN caches /
// shell history. Header-only is the load-bearing invariant.
func TestExtractKeyHeaderOnly(t *testing.T) {
	t.Run("bearer_header_extracted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/whatever", nil)
		req.Header.Set("Authorization", "Bearer snitch_abc123")
		if got := extractKey(req); got != "snitch_abc123" {
			t.Fatalf("extractKey(bearer) = %q, want %q", got, "snitch_abc123")
		}
	})
	t.Run("non_bearer_authorization_rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/whatever", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		if got := extractKey(req); got != "" {
			t.Fatalf("extractKey(Basic) = %q, want empty", got)
		}
	})
	t.Run("query_string_key_rejected_50e2_regression", func(t *testing.T) {
		// Block 50E.2 — the URL-query path used to return the key.
		// Header-only now; this is a regression test.
		req := httptest.NewRequest(http.MethodGet, "/api/whatever?key=snitch_abc123", nil)
		if got := extractKey(req); got != "" {
			t.Fatalf("extractKey(?key=) = %q, want empty (URL-query auth removed)", got)
		}
	})
	t.Run("no_authorization_returns_empty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/whatever", nil)
		if got := extractKey(req); got != "" {
			t.Fatalf("extractKey(no auth) = %q, want empty", got)
		}
	})
}

// TestRequireRejectsURLQueryAuth is the end-to-end version of the
// 50E.2 regression: a real Require-protected handler must return 401
// when called with ?key=<valid>. Belt-and-braces alongside the
// extractKey unit test above — Require is the layer operators
// actually hit.
func TestRequireRejectsURLQueryAuth(t *testing.T) {
	pool := startPostgres(t)
	a := New(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	plain, err := a.CreateKey(ctx, RoleOperator, "test-operator")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	handler := a.Require(RoleOperator, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	t.Run("bearer_header_succeeds", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		req.Header.Set("Authorization", "Bearer "+plain)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (valid bearer)", resp.StatusCode)
		}
	})

	t.Run("query_string_returns_401", func(t *testing.T) {
		// Even with a VALID key in ?key=, Require must reject with 401
		// because the URL-query path was removed in Block 50E.2.
		resp, err := http.Get(srv.URL + "?key=" + plain) //nolint:noctx,bodyclose // test
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (URL-query auth removed)", resp.StatusCode)
		}
	})

	t.Run("missing_authorization_returns_401", func(t *testing.T) {
		resp, err := http.Get(srv.URL) //nolint:noctx,bodyclose // test
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("insufficient_role_returns_403", func(t *testing.T) {
		viewerKey, err := a.CreateKey(ctx, RoleViewer, "test-viewer")
		if err != nil {
			t.Fatalf("CreateKey: %v", err)
		}
		// Viewer can't reach an Operator-required endpoint.
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		req.Header.Set("Authorization", "Bearer "+viewerKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (viewer < operator)", resp.StatusCode)
		}
	})
}

// TestKeyCountAndEnsureDefaults exercises the Block 50E.3 fix: the
// previous KeyCount returned (int) and silently swallowed Scan errors
// to 0, racing EnsureDefaults to re-create default keys on transient
// DB failures. New signature returns (int, error); EnsureDefaults
// propagates.
func TestKeyCountAndEnsureDefaults(t *testing.T) {
	pool := startPostgres(t)
	a := New(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("count_zero_on_empty_table", func(t *testing.T) {
		n, err := a.KeyCount(ctx)
		if err != nil {
			t.Fatalf("KeyCount: %v", err)
		}
		if n != 0 {
			t.Fatalf("KeyCount = %d, want 0", n)
		}
	})

	t.Run("ensure_defaults_creates_two_keys_on_empty", func(t *testing.T) {
		adminKey, probeKey, err := a.EnsureDefaults(ctx)
		if err != nil {
			t.Fatalf("EnsureDefaults: %v", err)
		}
		if !strings.HasPrefix(adminKey, "snitch_") {
			t.Errorf("adminKey = %q, want snitch_ prefix", adminKey)
		}
		if !strings.HasPrefix(probeKey, "snitch_") {
			t.Errorf("probeKey = %q, want snitch_ prefix", probeKey)
		}
		// Verify count is now 2.
		n, err := a.KeyCount(ctx)
		if err != nil {
			t.Fatalf("KeyCount: %v", err)
		}
		if n != 2 {
			t.Fatalf("KeyCount after EnsureDefaults = %d, want 2", n)
		}
	})

	t.Run("ensure_defaults_noop_when_keys_exist", func(t *testing.T) {
		// Second call must be a no-op (returns empty strings + nil error).
		adminKey, probeKey, err := a.EnsureDefaults(ctx)
		if err != nil {
			t.Fatalf("EnsureDefaults (second call): %v", err)
		}
		if adminKey != "" || probeKey != "" {
			t.Fatalf("EnsureDefaults (existing keys) = (%q, %q), want both empty", adminKey, probeKey)
		}
	})

	t.Run("count_error_propagates_through_ensure_defaults", func(t *testing.T) {
		// Drop the table to simulate a transient DB error class. KeyCount
		// must return an error; EnsureDefaults must propagate (NOT
		// silently re-create defaults — that was the 50E.3 race).
		if _, err := pool.Exec(ctx, "DROP TABLE api_keys"); err != nil {
			t.Fatalf("drop: %v", err)
		}
		t.Cleanup(func() {
			// Restore for any later tests in this package.
			_, _ = pool.Exec(context.Background(), apiKeysSchema)
		})
		_, err := a.KeyCount(ctx)
		if err == nil {
			t.Fatal("KeyCount on missing table = nil, want error")
		}
		if !strings.Contains(err.Error(), "count api_keys") {
			t.Errorf("error = %q, want wrapping containing %q", err.Error(), "count api_keys")
		}
		_, _, err = a.EnsureDefaults(ctx)
		if err == nil {
			t.Fatal("EnsureDefaults on missing table = nil, want error (50E.3 race regression)")
		}
		if !strings.Contains(err.Error(), "ensure-defaults") {
			t.Errorf("error = %q, want wrapping containing %q", err.Error(), "ensure-defaults")
		}
	})
}

// TestValidateKeyExercise covers the happy + sad paths of ValidateKey
// to lock the role-extraction contract.
func TestValidateKeyExercise(t *testing.T) {
	pool := startPostgres(t)
	a := New(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	plain, err := a.CreateKey(ctx, RoleAdmin, "test-admin")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	t.Run("valid_key_returns_role", func(t *testing.T) {
		role, err := a.ValidateKey(ctx, plain)
		if err != nil {
			t.Fatalf("ValidateKey: %v", err)
		}
		if role != RoleAdmin {
			t.Fatalf("role = %q, want admin", role)
		}
	})

	t.Run("invalid_key_returns_error", func(t *testing.T) {
		_, err := a.ValidateKey(ctx, "snitch_not_a_real_key")
		if err == nil {
			t.Fatal("ValidateKey(invalid) = nil, want error")
		}
	})

	t.Run("empty_key_returns_error", func(t *testing.T) {
		_, err := a.ValidateKey(ctx, "")
		if err == nil {
			t.Fatal("ValidateKey(empty) = nil, want error")
		}
	})
}

// TestCreateKeyWithPlaintext covers the Block 61 path: bundle-create
// generates the per-device probe key as part of the offline-CA-rooted
// bundle ceremony and registers the bearer here. Idempotent on label —
// re-issuing for the same device replaces the prior key.
func TestCreateKeyWithPlaintext(t *testing.T) {
	pool := startPostgres(t)
	a := New(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("registers_supplied_plaintext_key_validatable", func(t *testing.T) {
		plain := "snitch_aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
		if err := a.CreateKeyWithPlaintext(ctx, RoleProbe, "device-A", plain); err != nil {
			t.Fatalf("CreateKeyWithPlaintext: %v", err)
		}
		role, err := a.ValidateKey(ctx, plain)
		if err != nil {
			t.Fatalf("ValidateKey: %v", err)
		}
		if role != RoleProbe {
			t.Fatalf("role = %q, want probe", role)
		}
	})

	t.Run("idempotent_on_label_replaces_prior_key", func(t *testing.T) {
		first := "snitch_1111111111111111111111111111111111111111111111111111111111111111"
		second := "snitch_2222222222222222222222222222222222222222222222222222222222222222"
		if err := a.CreateKeyWithPlaintext(ctx, RoleProbe, "device-rotate", first); err != nil {
			t.Fatalf("first register: %v", err)
		}
		if err := a.CreateKeyWithPlaintext(ctx, RoleProbe, "device-rotate", second); err != nil {
			t.Fatalf("second register: %v", err)
		}
		// Old key MUST NOT validate.
		if _, err := a.ValidateKey(ctx, first); err == nil {
			t.Fatal("first key still valid after rotation; idempotent-on-label contract violated")
		}
		// New key validates.
		role, err := a.ValidateKey(ctx, second)
		if err != nil {
			t.Fatalf("second key not valid: %v", err)
		}
		if role != RoleProbe {
			t.Fatalf("role = %q, want probe", role)
		}
		// Exactly one row remains for the label.
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE label = $1`, "device-rotate").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Errorf("rows for label = %d; want 1", n)
		}
	})

	t.Run("empty_plaintext_rejected", func(t *testing.T) {
		err := a.CreateKeyWithPlaintext(ctx, RoleProbe, "x", "")
		if err == nil {
			t.Fatal("expected error on empty plaintext")
		}
		if !strings.Contains(err.Error(), "plaintext") {
			t.Errorf("error %q does not mention 'plaintext'", err)
		}
	})

	t.Run("empty_label_rejected", func(t *testing.T) {
		err := a.CreateKeyWithPlaintext(ctx, RoleProbe, "", "snitch_x")
		if err == nil {
			t.Fatal("expected error on empty label")
		}
		if !strings.Contains(err.Error(), "label") {
			t.Errorf("error %q does not mention 'label'", err)
		}
	})
}
