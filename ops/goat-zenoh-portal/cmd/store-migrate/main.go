// store-migrate copies the portal registry between its SQLite file and
// Postgres (ADR 1165 step A, docs/design/island-mode-multi-writer.md §5).
//
//	store-migrate import       SQLite file -> empty Postgres database
//	store-migrate export       Postgres -> new SQLite file (rollback)
//	store-migrate dump sqlite|postgres
//	store-migrate checkpoint   fold the WAL into the SQLite file
//
// Locations default to the environment the portal uses: PORTAL_DB_DSN for
// Postgres and PORTAL_DB for the SQLite file (read here only; the portal itself
// no longer opens SQLite). --dsn and --sqlite override.
//
// import and export print, per table, the row count and content hash on each
// side, and exit 1 when any table differs; nothing is committed in that case.
// The report is whitespace-separated columns: store, table, sqlite rows,
// postgres rows, sqlite sha256, postgres sha256, result (ok | MISMATCH).
// dump prints the canonical table dumps the hashes are taken over, for a
// byte-for-byte comparison during a rehearsal. checkpoint folds the SQLite
// file's write-ahead log into the file and leaves it in rollback-journal mode,
// so the file can be archived read-only on its own (the cutover runs it after
// stopping the portal).
//
// Schema files under the portal's schema directory and bundles are files on
// disk keyed by ids this copy keeps unchanged; nothing here touches them.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgstore"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/storemigrate"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	cmd, rest := args[0], args[1:]
	side := ""
	if cmd == "dump" && len(rest) > 0 && (rest[0] == "sqlite" || rest[0] == "postgres") {
		side, rest = rest[0], rest[1:]
	}
	fs := flag.NewFlagSet("store-migrate "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	sqlitePath := fs.String("sqlite", os.Getenv("PORTAL_DB"), "registry SQLite file (default $PORTAL_DB)")
	dsn := fs.String("dsn", os.Getenv("PORTAL_DB_DSN"), "registry Postgres DSN (default $PORTAL_DB_DSN)")
	out := fs.String("out", "", "prune: the pruned copy to write (must not exist)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	st := storemigrate.Registry

	switch cmd {
	case "import", "export":
		if *sqlitePath == "" || *dsn == "" {
			fmt.Fprintln(stderr, "need both a SQLite path and a Postgres DSN (--sqlite/--dsn, or PORTAL_DB/PORTAL_DB_DSN)")
			return 2
		}
		var reports []storemigrate.TableReport
		var dropped []storemigrate.Dropped
		var err error
		if cmd == "import" {
			reports, dropped, err = storemigrate.Import(ctx, st, *sqlitePath, *dsn)
		} else {
			reports, err = storemigrate.Export(ctx, st, *dsn, *sqlitePath)
		}
		printReports(stdout, st.Name, reports)
		printDropped(stdout, dropped)
		if err != nil {
			fmt.Fprintf(stderr, "%s %s: %v\n", cmd, st.Name, err)
			return 1
		}
		return 0
	case "prune":
		if *sqlitePath == "" || *out == "" {
			fmt.Fprintln(stderr, "prune: need --sqlite and --out")
			return 2
		}
		dropped, err := storemigrate.Prune(ctx, *sqlitePath, *out)
		printDropped(stdout, dropped)
		if err != nil {
			fmt.Fprintf(stderr, "prune %s: %v\n", st.Name, err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\t%s\tpruned copy, %d row(s) removed\n", st.Name, *out, len(dropped))
		return 0
	case "checkpoint":
		if *sqlitePath == "" {
			fmt.Fprintln(stderr, "need a SQLite path (--sqlite or PORTAL_DB)")
			return 2
		}
		if err := storemigrate.Checkpoint(ctx, *sqlitePath); err != nil {
			fmt.Fprintf(stderr, "checkpoint %s: %v\n", st.Name, err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\t%s\tcheckpointed, journal_mode=delete\n", st.Name, *sqlitePath)
		return 0
	case "dump":
		if side == "" {
			fmt.Fprintln(stderr, "dump: name the side: store-migrate dump sqlite|postgres")
			return 2
		}
		if err := dump(ctx, stdout, st, side, *sqlitePath, *dsn); err != nil {
			fmt.Fprintf(stderr, "dump: %v\n", err)
			return 1
		}
		return 0
	default:
		usage(stderr)
		return 2
	}
}

// printDropped lists the rows the declared cascades removed. Each line starts
// with "#" and "dropped", so a reader of the report table (the cutover's count
// check reads column two as the table name) skips it.
func printDropped(w io.Writer, dropped []storemigrate.Dropped) {
	for _, d := range dropped {
		_, _ = fmt.Fprintf(w, "# dropped %s rowid=%d (its %s row is gone; ON DELETE CASCADE, F-593): %s\n", d.Table, d.RowID, d.Parent, d.Row) //nolint:errcheck // report to stdout
	}
}

func printReports(w io.Writer, store string, reports []storemigrate.TableReport) {
	if len(reports) == 0 {
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "store\ttable\tsqlite_rows\tpostgres_rows\tsqlite_sha256\tpostgres_sha256\tresult") //nolint:errcheck // report to stdout
	for _, r := range reports {
		result := "ok"
		if !r.OK() {
			result = "MISMATCH"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\t%s\t%s\n", store, r.Table, r.SQLiteRows, r.PGRows, r.SQLiteHash, r.PGHash, result) //nolint:errcheck // report to stdout
	}
	_ = tw.Flush() //nolint:errcheck // report to stdout
}

func dump(ctx context.Context, w io.Writer, st storemigrate.Store, side, sqlitePath, dsn string) error {
	if side == "sqlite" {
		if sqlitePath == "" {
			return fmt.Errorf("need a SQLite path (--sqlite or PORTAL_DB)")
		}
		db, err := storemigrate.OpenSQLite(sqlitePath, true)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }() //nolint:errcheck // read-only handle
		for _, t := range st.Tables {
			d, _, err := storemigrate.Dump(ctx, db, storemigrate.SQLite, t)
			if err != nil {
				return fmt.Errorf("%s: %w", t, err)
			}
			if _, err := w.Write(d); err != nil {
				return err
			}
		}
		return nil
	}
	if dsn == "" {
		return fmt.Errorf("need a Postgres DSN (--dsn or PORTAL_DB_DSN)")
	}
	db, err := pgstore.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }() //nolint:errcheck // read-only use
	for _, t := range st.Tables {
		d, _, err := storemigrate.Dump(ctx, db, storemigrate.Postgres, t)
		if err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
		if _, err := w.Write(d); err != nil {
			return err
		}
	}
	return nil
}

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `usage: store-migrate import|export [--sqlite PATH] [--dsn DSN]
       store-migrate dump sqlite|postgres [--sqlite PATH] [--dsn DSN]
       store-migrate checkpoint [--sqlite PATH]
       store-migrate prune --sqlite PATH --out PATH   (the copy import reads: declared cascades applied)`) //nolint:errcheck // usage to stderr
}
