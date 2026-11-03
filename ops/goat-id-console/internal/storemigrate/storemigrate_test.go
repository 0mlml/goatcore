package storemigrate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/orgstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
)

// onboardFixture covers every onboarding table, with NULLs in the nullable
// INTEGER and TEXT columns, 0/1 flags, JSON text, same-second ties, and text a
// naive dump would mangle (quotes, tab, newline, non-ASCII).
var onboardFixture = []string{
	`INSERT INTO onboard_proposals (id, created_at, sponsor, sponsor_name, env, handle, email, status, manifest_json, lint_json, decided_by, decided_at, note, escalated, complete, petitions, tier, setup_key, handset_key, retroactive, collab_activated, org_source, claimed_at)
	 VALUES ('prop-0001', 1800000000, 'jane-admin', 'Jane', 'zagros', 'pat-liaison', 'Pat@Example.test', 'onboarded', '{"identity":{"handle":"pat-liaison"}}', '{}', 'ops-admin', 1800000100, 'line one
line two	tabbed "quoted"', 1, 1, '[]', 'operator', 'SK-1', 'HK-1', 0, 1, 'sponsor', 1800000050)`,
	`INSERT INTO onboard_proposals (id, created_at, sponsor, env, handle, email, status, manifest_json, decided_by, decided_at, complete)
	 VALUES ('prop-0002', 1800000000, 'jane-admin', 'zagros', 'zoë-ünïcode', '', 'proposed', '{}', NULL, NULL, 0)`,
	`INSERT INTO onboard_invites (id, created_at, sponsor, env, token_hash, prebound_json, expires_at, status, consumed_at, proposal_id, candidate_email, sent_at, sent_via, revoked_at, archived_at)
	 VALUES ('inv-0001', 1799999000, 'jane-admin', 'zagros', 'aa11', '{"tier":"operator","org":"demoforge"}', 1800600000, 'submitted', 1800000000, 'prop-0001', 'pat@example.test', 1799999500, 'email', NULL, NULL)`,
	`INSERT INTO onboard_invites (id, created_at, sponsor, env, token_hash, expires_at, status, revoked_at)
	 VALUES ('inv-0002', 1799999000, 'jane-admin', 'zagros', 'bb22', 1800600000, 'revoked', 1799999900)`,
	`INSERT INTO ssh_keys (id, created_at, handle, key_type, key_body, fingerprint, status, expires_at)
	 VALUES ('sshk-0001', 1800000000, 'pat-liaison', 'ssh-ed25519', 'AAAAC3Nza', 'SHA256:x', 'approved', 0)`,
	`INSERT INTO operator_keys (id, created_at, handle, env, status, fingerprint, secret, decided_by, decided_at)
	 VALUES ('opk-0001', 1800000000, 'pat-liaison', 'zagros', 'active', 'sha256:abc', 'op_secret', 'ops-admin', 1800000001)`,
	`INSERT INTO onboard_actions (id, proposal_id, kind, status, requested_by, created_at, claimed_at, claimed_by, finished_at, result_json, params)
	 VALUES ('act-0002', 'prop-0001', 'reissue_handset', 'done', 'ops@example.test', 1800000200, 1800000201, 'runner-1', 1800000202, '{"ok":true}', '{}')`,
	`INSERT INTO onboard_actions (id, proposal_id, kind, status, created_at, params, target_env, target_handle)
	 VALUES ('act-0001', '', 'release_hold', 'queued', 1800000200, '{"peer_id":"p1","hold_entered_at":"2027-01-15T08:00:00Z"}', 'zagros', 'pat-liaison')`,
	`INSERT INTO probation_reactivation_requests (id, peer_id, peer_name, handle, hold_entered_at, requested_at, status)
	 VALUES ('react-0001', 'p1', 'pat-laptop', 'pat-liaison', 1800000000, 1800000300, 'open')`,
}

// orgFixture builds the org registry the way a long-lived file looks: orgs was
// created before sector/gov_branch/composition existed and gained them by
// ALTER TABLE, so its column order differs from a fresh file's. Parent edge ids
// have a gap, and sqlite_sequence is past the highest surviving id.
var orgFixture = []string{
	`CREATE TABLE orgs (
		id TEXT PRIMARY KEY, display_name TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '',
		nation TEXT NOT NULL DEFAULT '', echelon_hint TEXT NOT NULL DEFAULT '',
		provenance TEXT NOT NULL DEFAULT 'self-asserted', status TEXT NOT NULL DEFAULT 'active',
		created_by TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL)`,
	`ALTER TABLE orgs ADD COLUMN sector TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE orgs ADD COLUMN gov_branch TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE orgs ADD COLUMN composition TEXT NOT NULL DEFAULT ''`,
	orgSQLiteSchema,
	`INSERT INTO orgs (id, display_name, kind, nation, provenance, status, created_by, created_at, sector, gov_branch, composition)
	 VALUES ('usareur-af', 'USAREUR-AF', 'organic', 'USA', 'authoritative', 'active', 'ops', 1800000000, 'state', 'military', 'national')`,
	`INSERT INTO orgs (id, display_name, kind, nation, created_at) VALUES ('fortele-terestre', 'Forțele Terestre', 'partner', 'SVN', 1800000000)`,
	`INSERT INTO orgs (id, created_at, status) VALUES ('old-dup', 1800000001, 'absorbed')`,
	`INSERT INTO org_parent_edges (id, org_id, parent_id, valid_from, valid_to, recorded_at, actor, justification)
	 VALUES (1, 'fortele-terestre', 'usareur-af', 1800000000, 1800000500, 1800000000, 'ops', 'first')`,
	`INSERT INTO org_parent_edges (id, org_id, parent_id, valid_from, valid_to, recorded_at, actor)
	 VALUES (5, 'fortele-terestre', 'usareur-af', 1800000500, NULL, 1800000500, 'ops')`,
	`INSERT INTO org_parent_edges (id, org_id, parent_id, valid_from, recorded_at) VALUES (7, 'old-dup', 'usareur-af', 1800000600, 1800000600)`,
	`DELETE FROM org_parent_edges WHERE id = 7`,
	`INSERT INTO org_aliases (alias, org_id, kind, recorded_at, actor) VALUES ('old-dup', 'usareur-af', 'absorbed-id', 1800000700, 'ops')`,
	`INSERT INTO org_aliases (alias, org_id, kind, recorded_at) VALUES ('USAREUR', 'usareur-af', 'abbreviation', 1800000700)`,
	`INSERT INTO org_match_overrides (name_norm, org_id, recorded_at, actor) VALUES ('fortele terestre', 'usareur-af', 1800000800, 'ops')`,
}

func writeSQLite(ctx context.Context, t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := OpenSQLite(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }() //nolint:errcheck // test fixture
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("fixture %q: %v", firstLine(s), err)
		}
	}
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

func dumpSQLite(ctx context.Context, t *testing.T, path string, st Store) map[string][]byte {
	t.Helper()
	db, err := OpenSQLite(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }() //nolint:errcheck // test read
	out := map[string][]byte{}
	for _, tbl := range append(append([]string{}, st.Tables...), "sqlite_sequence") {
		if tbl == "sqlite_sequence" && st.Identity == nil {
			continue
		}
		d, n, err := Dump(ctx, db, SQLite, tbl)
		if err != nil {
			t.Fatalf("dump %s: %v", tbl, err)
		}
		if tbl != "sqlite_sequence" && n == 0 {
			t.Fatalf("fixture table %s is empty; the round trip would prove nothing about it", tbl)
		}
		out[tbl] = d
	}
	return out
}

// A TEXT column that SQLite stored with blob affinity (zagros's
// onboard_proposals.manifest_json) imports as text and compares
// equal; a blob that is not valid UTF-8 is still refused.
func TestImportReadsAValidUTF8BlobAsText(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	src := filepath.Join(dir, "onboard.db")
	writeSQLite(ctx, t, src, append([]string{Onboard.SQLiteSchema}, onboardFixture...)...)
	raw, err := OpenSQLite(src, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `UPDATE onboard_proposals SET manifest_json = CAST(manifest_json AS BLOB) WHERE rowid = (SELECT min(rowid) FROM onboard_proposals)`); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	var kind string
	if err := raw.GetContext(ctx, &kind, `SELECT typeof(manifest_json) FROM onboard_proposals WHERE rowid = (SELECT min(rowid) FROM onboard_proposals)`); err != nil || kind != "blob" {
		t.Fatalf("fixture row is %q, want blob (%v)", kind, err)
	}
	_ = raw.Close() //nolint:errcheck // test
	dsn := pgtest.DSN(t)
	reports, err := Import(ctx, Onboard, src, dsn)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	for _, r := range reports {
		if !r.OK() {
			t.Fatalf("import %s: %+v", r.Table, r)
		}
	}
	if _, err := canonical([]byte{0xff, 0xfe, 0x00}); err == nil {
		t.Fatal("canonical accepted a blob that is not UTF-8")
	}
}

// The round trip the rollback depends on: SQLite fixture -> import -> export
// gives table dumps byte-equal to the fixture's, for both stores.
func TestRoundTripSQLitePostgresSQLite(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		st      Store
		fixture []string
	}{
		{Onboard, append([]string{Onboard.SQLiteSchema}, onboardFixture...)},
		{Org, orgFixture},
	} {
		t.Run(tc.st.Name, func(t *testing.T) {
			src := filepath.Join(dir, tc.st.Name+"-src.db")
			back := filepath.Join(dir, tc.st.Name+"-back.db")
			dsn := pgtest.DSN(t)
			ctx := t.Context()
			writeSQLite(ctx, t, src, tc.fixture...)

			reports, err := Import(ctx, tc.st, src, dsn)
			if err != nil {
				t.Fatalf("import: %v", err)
			}
			if len(reports) != len(tc.st.Tables) {
				t.Fatalf("import reported %d tables, want %d", len(reports), len(tc.st.Tables))
			}
			for _, r := range reports {
				if !r.OK() || r.SQLiteRows == 0 {
					t.Fatalf("import %s: %+v", r.Table, r)
				}
			}

			if _, err := Export(ctx, tc.st, dsn, back); err != nil {
				t.Fatalf("export: %v", err)
			}
			want, got := dumpSQLite(ctx, t, src, tc.st), dumpSQLite(ctx, t, back, tc.st)
			for tbl, w := range want {
				if !bytes.Equal(w, got[tbl]) {
					t.Errorf("%s differs after the round trip\nfixture:\n%s\nexported:\n%s", tbl, w, got[tbl])
				}
			}

			// A second import into the same database refuses rather than
			// doubling rows; an export never overwrites a file.
			if _, err := Import(ctx, tc.st, src, dsn); err == nil || !strings.Contains(err.Error(), "already holds") {
				t.Fatalf("second import: want a refusal, got %v", err)
			}
			if _, err := Export(ctx, tc.st, dsn, back); err == nil || !strings.Contains(err.Error(), "already exists") {
				t.Fatalf("export over an existing file: want a refusal, got %v", err)
			}
		})
	}
}

// After an import the identity sequence continues past sqlite_sequence, the
// way AUTOINCREMENT would have: id 7 was issued and deleted, so the next is 8.
func TestImportCarriesIdentitySequence(t *testing.T) {
	src := filepath.Join(t.TempDir(), "orgs.db")
	writeSQLite(t.Context(), t, src, orgFixture...)
	dsn := pgtest.DSN(t)
	if _, err := Import(t.Context(), Org, src, dsn); err != nil {
		t.Fatalf("import: %v", err)
	}
	s, err := orgstore.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() }) //nolint:errcheck // test teardown
	if _, err := s.CreateOrg(orgstore.Org{ID: "new-unit", CreatedBy: "ops"}); err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	if err := s.SetParent("new-unit", "usareur-af", "ops", "after import"); err != nil {
		t.Fatalf("SetParent: %v", err)
	}
	pg, err := pgstore.Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Close() }) //nolint:errcheck // test teardown
	var maxID int64
	if err := pg.Get(&maxID, `SELECT MAX(id) FROM org_parent_edges`); err != nil {
		t.Fatal(err)
	}
	if maxID != 8 {
		t.Fatalf("next identity after import = %d, want 8 (sqlite_sequence was 7)", maxID)
	}
}

// A content difference with equal row counts is caught by the hash.
func TestCompareCatchesContentDifference(t *testing.T) {
	ctx := t.Context()
	src := filepath.Join(t.TempDir(), "onboard.db")
	writeSQLite(ctx, t, src, append([]string{Onboard.SQLiteSchema}, onboardFixture...)...)
	dsn := pgtest.DSN(t)
	if _, err := Import(ctx, Onboard, src, dsn); err != nil {
		t.Fatalf("import: %v", err)
	}
	pg, err := pgstore.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Close() }) //nolint:errcheck // test teardown
	if _, err := pg.ExecContext(ctx, `UPDATE onboard_proposals SET note = note || '.' WHERE id = 'prop-0001'`); err != nil {
		t.Fatal(err)
	}
	lite, err := OpenSQLite(src, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lite.Close() }) //nolint:errcheck // test teardown
	reports, err := compare(ctx, Onboard, lite, pg)
	if !errors.Is(err, ErrMismatch) {
		t.Fatalf("compare: want ErrMismatch, got %v", err)
	}
	for _, r := range reports {
		if r.Table == "onboard_proposals" && (r.OK() || r.SQLiteRows != r.PGRows) {
			t.Fatalf("onboard_proposals: want equal counts and different hashes, got %+v", r)
		}
		if r.Table != "onboard_proposals" && !r.OK() {
			t.Fatalf("%s: untouched table reported different: %+v", r.Table, r)
		}
	}
}

// A SQLite file missing a column (never opened by the current SQLite build)
// is refused with the columns named, not imported with defaults filled in.
func TestImportRefusesColumnDrift(t *testing.T) {
	src := filepath.Join(t.TempDir(), "orgs.db")
	writeSQLite(t.Context(), t, src, append([]string{`CREATE TABLE orgs (
		id TEXT PRIMARY KEY, display_name TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '',
		nation TEXT NOT NULL DEFAULT '', echelon_hint TEXT NOT NULL DEFAULT '',
		provenance TEXT NOT NULL DEFAULT 'self-asserted', status TEXT NOT NULL DEFAULT 'active',
		created_by TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL)`}, orgSQLiteSchema)...)
	_, err := Import(t.Context(), Org, src, pgtest.DSN(t))
	if err == nil || !strings.Contains(err.Error(), "columns differ") {
		t.Fatalf("want a column-drift refusal, got %v", err)
	}
}

// TestCheckpointLeavesASelfContainedFile: a WAL-mode file the console wrote is
// left in rollback-journal mode with every row in the file itself, so a 0444
// copy opens read-only on its own. A missing path is refused, never created.
func TestCheckpointLeavesASelfContainedFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "orgs.db")
	db, err := OpenSQLite(path, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`PRAGMA journal_mode=WAL`, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`, `INSERT INTO t (v) VALUES ('a'), ('b')`} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Checkpoint(ctx, path); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	for _, side := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + side); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s%s still present after Checkpoint (err=%v)", path, side, err)
		}
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	ro, err := OpenSQLite(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }() //nolint:errcheck // test handle
	var mode string
	if err := ro.GetContext(ctx, &mode, `PRAGMA journal_mode`); err != nil {
		t.Fatal(err)
	}
	if mode != "delete" {
		t.Errorf("journal_mode = %q, want delete", mode)
	}
	var n int
	if err := ro.GetContext(ctx, &n, `SELECT COUNT(*) FROM t`); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("rows = %d, want 2", n)
	}

	missing := filepath.Join(t.TempDir(), "absent.db")
	if err := Checkpoint(ctx, missing); err == nil {
		t.Error("Checkpoint of a missing file succeeded")
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Checkpoint created %s", missing)
	}
}
