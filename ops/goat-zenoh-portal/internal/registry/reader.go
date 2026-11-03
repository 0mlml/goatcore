package registry

// The read path: every read-only method in this package goes through
// readOnly, which puts a deadline on each query. The deadline bounds how long
// one slow or stuck read can hold a pooled connection, so a bad query costs one
// request rather than the pool.

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

// readTimeout bounds one registry read. The slowest legitimate read measured
// in production was a 16 s scan of vendor_topic_activity; 30 s keeps that
// working on a bad day.
const readTimeout = 30 * time.Second

// readOnly is the registry's read path: the pool, with a deadline on every
// query. It exposes only contexted methods.
type readOnly struct {
	db  *sqlx.DB
	ttl time.Duration
}

// GetContext reads a single row under the read deadline.
func (rd readOnly) GetContext(ctx context.Context, dest any, query string, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, rd.ttl)
	defer cancel()
	return rd.db.GetContext(ctx, dest, query, args...)
}

// SelectContext reads a row set under the read deadline.
func (rd readOnly) SelectContext(ctx context.Context, dest any, query string, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, rd.ttl)
	defer cancel()
	return rd.db.SelectContext(ctx, dest, query, args...)
}

// QueryContext streams rows under the read deadline.
//
// It returns the cancel alongside the rows because the deadline has to outlive
// this call — cancelling here would kill the rows the caller is about to read.
// Callers defer BOTH, cancel first:
//
//	rows, cancel, err := r.reader().QueryContext(ctx, q, args...)
//	if err != nil { return nil, err }
//	defer cancel()
//	defer rows.Close()
func (rd readOnly) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, rd.ttl)
	rows, err := rd.db.QueryContext(ctx, query, args...)
	if err != nil {
		cancel()
		return nil, func() {}, err
	}
	return rows, cancel, nil
}
