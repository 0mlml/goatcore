// store-migrate copies the console's onboarding and org stores between their
// SQLite files and Postgres (ADR 1165 step A, docs/design/island-mode-multi-writer.md §5).
//
//	store-migrate import [--store onboard|org|all]   SQLite files -> empty Postgres databases
//	store-migrate export [--store onboard|org|all]   Postgres -> new SQLite files (rollback)
//	store-migrate dump   --store onboard|org (--sqlite PATH | --dsn DSN)
//	store-migrate checkpoint [--store onboard|org|all]   fold the WAL into each SQLite file
//
// Locations default to the environment the console uses: ONBOARD_DB_DSN and
// ORG_DB_DSN for Postgres, ONBOARD_DB_PATH and ORG_DB_PATH for the SQLite files
// (read here only; the console itself no longer opens SQLite). Flags override.
//
// import and export print, per table, the row count and content hash on each
// side, and exit 1 when any table differs; nothing is committed in that case.
// dump prints the canonical table dumps the hashes are taken over, for a
// byte-for-byte comparison during a rehearsal. checkpoint folds each SQLite
// file's write-ahead log into the file and leaves it in rollback-journal mode,
// so the file can be archived read-only on its own (the cutover runs it after
// stopping the console).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/storemigrate"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

type locations struct {
	sqlite map[string]*string
	dsn    map[string]*string
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet("store-migrate "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	which := fs.String("store", "all", "onboard | org | all")
	loc := locations{
		sqlite: map[string]*string{
			"onboard": fs.String("onboard-sqlite", os.Getenv("ONBOARD_DB_PATH"), "onboarding store SQLite file (default $ONBOARD_DB_PATH)"),
			"org":     fs.String("org-sqlite", os.Getenv("ORG_DB_PATH"), "org registry SQLite file (default $ORG_DB_PATH)"),
		},
		dsn: map[string]*string{
			"onboard": fs.String("onboard-dsn", os.Getenv("ONBOARD_DB_DSN"), "onboarding store Postgres DSN (default $ONBOARD_DB_DSN)"),
			"org":     fs.String("org-dsn", os.Getenv("ORG_DB_DSN"), "org registry Postgres DSN (default $ORG_DB_DSN)"),
		},
	}
	sqlitePath := fs.String("sqlite", "", "dump: the SQLite file to dump")
	dsn := fs.String("dsn", "", "dump: the Postgres DSN to dump")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	stores, err := selectStores(*which)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	switch cmd {
	case "import", "export":
		failed := false
		for _, st := range stores {
			src, dst := *loc.sqlite[st.Name], *loc.dsn[st.Name]
			if src == "" || dst == "" {
				fmt.Fprintf(stderr, "%s: need both a SQLite path and a Postgres DSN (flags --%s-sqlite/--%s-dsn or the environment)\n", st.Name, st.Name, st.Name)
				return 2
			}
			var reports []storemigrate.TableReport
			if cmd == "import" {
				reports, err = storemigrate.Import(ctx, st, src, dst)
			} else {
				reports, err = storemigrate.Export(ctx, st, dst, src)
			}
			printReports(stdout, st.Name, reports)
			if err != nil {
				fmt.Fprintf(stderr, "%s %s: %v\n", cmd, st.Name, err)
				failed = true
				if !errors.Is(err, storemigrate.ErrMismatch) {
					return 1
				}
			}
		}
		if failed {
			return 1
		}
		return 0
	case "checkpoint":
		for _, st := range stores {
			path := *loc.sqlite[st.Name]
			if path == "" {
				fmt.Fprintf(stderr, "%s: need a SQLite path (--%s-sqlite or the environment)\n", st.Name, st.Name)
				return 2
			}
			if err := storemigrate.Checkpoint(ctx, path); err != nil {
				fmt.Fprintf(stderr, "checkpoint %s: %v\n", st.Name, err)
				return 1
			}
			fmt.Fprintf(stdout, "%s\t%s\tcheckpointed, journal_mode=delete\n", st.Name, path)
		}
		return 0
	case "dump":
		if len(stores) != 1 || (*sqlitePath == "") == (*dsn == "") {
			fmt.Fprintln(stderr, "dump: name one --store and exactly one of --sqlite or --dsn")
			return 2
		}
		if err := dump(ctx, stdout, stores[0], *sqlitePath, *dsn); err != nil {
			fmt.Fprintf(stderr, "dump: %v\n", err)
			return 1
		}
		return 0
	default:
		usage(stderr)
		return 2
	}
}

func selectStores(which string) ([]storemigrate.Store, error) {
	switch which {
	case "all":
		return storemigrate.Stores, nil
	case "onboard":
		return []storemigrate.Store{storemigrate.Onboard}, nil
	case "org":
		return []storemigrate.Store{storemigrate.Org}, nil
	default:
		return nil, fmt.Errorf("--store must be onboard, org or all, not %q", which)
	}
}

func printReports(w io.Writer, store string, reports []storemigrate.TableReport) {
	if len(reports) == 0 {
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "store\ttable\tsqlite rows\tpostgres rows\tsqlite sha256\tpostgres sha256\tresult") //nolint:errcheck // report to stdout
	for _, r := range reports {
		result := "ok"
		if !r.OK() {
			result = "MISMATCH"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\t%s\t%s\n", store, r.Table, r.SQLiteRows, r.PGRows, r.SQLiteHash, r.PGHash, result) //nolint:errcheck // report to stdout
	}
	_ = tw.Flush() //nolint:errcheck // report to stdout
}

func dump(ctx context.Context, w io.Writer, st storemigrate.Store, sqlitePath, dsn string) error {
	if sqlitePath != "" {
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
	_, _ = fmt.Fprintln(w, `usage: store-migrate import|export [--store onboard|org|all] [--onboard-sqlite PATH] [--org-sqlite PATH] [--onboard-dsn DSN] [--org-dsn DSN]
       store-migrate dump --store onboard|org (--sqlite PATH | --dsn DSN)
       store-migrate checkpoint [--store onboard|org|all] [--onboard-sqlite PATH] [--org-sqlite PATH]`) //nolint:errcheck // usage to stderr
}
