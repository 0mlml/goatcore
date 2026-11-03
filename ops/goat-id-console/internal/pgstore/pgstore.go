// Package pgstore is the Postgres plumbing shared by the console's stores
// (onboardstore, orgstore): opening a pool through pgx's database/sql driver,
// a minimal versioned migration table, the additive-column helper, and the
// unique-violation test.
//
// ADR 1165 step A moves both stores from SQLite to Postgres with the tables
// unchanged, so this package deliberately carries no schema of its own: each
// store hands Migrate its version-1 DDL, which is the SQLite schema with the
// types mapped one to one (docs/design/island-mode-multi-writer.md §5).
package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/jmoiron/sqlx"
)

// UniqueViolation is the SQLSTATE Postgres returns for a duplicate key in a
// primary key or unique index.
const UniqueViolation = "23505"

// IsUniqueViolation reports whether err is a Postgres unique-key violation.
// It replaces the SQLite-era test of the error string for "UNIQUE".
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == UniqueViolation
}

// Open connects to dsn (a libpq URL or keyword string) and verifies the
// connection. It applies no schema; the caller runs Migrate.
func Open(ctx context.Context, dsn string) (*sqlx.DB, error) {
	if dsn == "" {
		return nil, errors.New("empty Postgres DSN")
	}
	db, err := sqlx.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close() //nolint:errcheck // the ping error is the one worth returning
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	return db, nil
}

// Migration is one numbered schema step. Version 1 is the schema the store had
// on SQLite; later versions are ADR 1165 step B.
type Migration struct {
	Version int
	SQL     string
}

// migrationLockKey serializes concurrent Migrate calls (two console replicas
// starting at once) on one database. Arbitrary but fixed.
const migrationLockKey int64 = 0x676f61745f6d6967 // "goat_mig"

// Migrate applies every migration whose version is not yet recorded in
// schema_migrations, in ascending order, each in its own transaction under a
// transaction-scoped advisory lock. Idempotent: a second call applies nothing.
func Migrate(ctx context.Context, db *sqlx.DB, migrations []Migration) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at BIGINT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	last := 0
	for _, m := range migrations {
		if m.Version <= last {
			return fmt.Errorf("migrations out of order: %d after %d", m.Version, last)
		}
		last = m.Version
		if err := applyOne(ctx, db, m); err != nil {
			return fmt.Errorf("migration %d: %w", m.Version, err)
		}
	}
	return nil
}

func applyOne(ctx context.Context, db *sqlx.DB, m Migration) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck // no-op after Commit
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	var applied bool
	if err := tx.GetContext(ctx, &applied,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, m.Version); err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	if applied {
		return nil
	}
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES ($1, $2)`,
		m.Version, time.Now().Unix()); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	return tx.Commit()
}

// SchemaVersion returns the highest applied migration, or 0 when none is.
func SchemaVersion(ctx context.Context, q sqlx.QueryerContext) (int, error) {
	var exists bool
	if err := sqlx.GetContext(ctx, q, &exists, `SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		 WHERE table_schema = current_schema() AND table_name = 'schema_migrations')`); err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	var v int
	if err := sqlx.GetContext(ctx, q, &v, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`); err != nil {
		return 0, err
	}
	return v, nil
}

// AddColumnIfMissing applies ALTER TABLE ... ADD COLUMN when the column is not
// already present in the current schema. It is the information_schema port of
// the SQLite-era PRAGMA table_info check. table, column and decl are
// compile-time constants from the store, never user input.
func AddColumnIfMissing(ctx context.Context, db sqlx.ExtContext, table, column, decl string) error {
	var n int
	if err := sqlx.GetContext(ctx, db, &n, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`, table, column); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, decl))
	return err
}

// RequireTables fails when any named table is missing from the current schema.
// Each store runs it after Migrate: schema_migrations is per database, so two
// stores pointed at one database would see the other's version 1 as their own
// and skip their DDL. This turns that misconfiguration into a startup error.
func RequireTables(ctx context.Context, q sqlx.QueryerContext, tables ...string) error {
	for _, t := range tables {
		var n int
		if err := sqlx.GetContext(ctx, q, &n, `SELECT COUNT(*) FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = $1`, t); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("table %s is missing although schema_migrations records the schema as applied: is this DSN pointing at another store's database?", t)
		}
	}
	return nil
}
