// Package pgtest gives each test its own empty Postgres schema.
//
// Tests that touch a store need a real Postgres. TEST_PG_DSN names one (a URL,
// e.g. postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable);
// CI provides it from a postgres:16 service container. When it is unset the
// test is skipped with a message saying so, so `go test ./...` still runs the
// rest of the module on a laptop without a database.
package pgtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

// EnvDSN is the variable that names the test server.
const EnvDSN = "TEST_PG_DSN"

// DSN creates a fresh schema on the TEST_PG_DSN server, registers its removal
// with t.Cleanup, and returns a DSN whose search_path is that schema, so every
// table a store creates lands there and two tests never share a row.
func DSN(t testing.TB) string {
	t.Helper()
	base := os.Getenv(EnvDSN)
	if base == "" {
		t.Skipf("%s is not set: this test needs a Postgres server (CI runs it against a postgres:16 service container)", EnvDSN)
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatalf("%s must be a postgres:// URL: %q", EnvDSN, base)
	}
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	schema := "t_" + hex.EncodeToString(b[:])

	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open %s: %v", EnvDSN, err)
	}
	ctx := t.Context()
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		_ = admin.Close() //nolint:errcheck // the create error is the one worth reporting
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		// t.Context is already canceled when cleanups run.
		if _, err := admin.ExecContext(context.WithoutCancel(ctx), `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Logf("drop test schema %s: %v", schema, err)
		}
		_ = admin.Close() //nolint:errcheck // test teardown
	})

	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
