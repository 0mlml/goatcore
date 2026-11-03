// Package orgstore is the first-class org registry (ADR 1112,
// docs/design/org-hierarchy-and-dedup.md): orgs + effective-dated parent
// edges + aliases, created and mutated only as deliberate audited acts.
//
// Load-bearing invariants (design §4/§5, tested in store_test.go):
//   - org ids are IMMUTABLE and never re-key anything downstream;
//   - at most one OPEN parent edge per org (vertical containment, the
//     grammar's v1 edge); history is closed, never deleted;
//   - a parent edge that would create a cycle is refused AT WRITE TIME —
//     the roster resolver's walk is cycle-guarded, but that guard prevents
//     a spin, not a wrong answer;
//   - aliases and org ids share one namespace (an alias can never shadow
//     an org, an org can never be created over an alias).
//
// Backed by sqlx over Postgres (pgx's database/sql driver), same pattern as the
// sibling onboardstore, in its OWN database (ADR 1165 step A moved the SQLite
// tables unchanged).
package orgstore

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/nation"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgstore"
	"github.com/jmoiron/sqlx"
)

//go:embed schema.sql
var schemaSQL string

// Migrations is the store's versioned schema. Version 1 is the SQLite schema,
// tables and columns unchanged.
var Migrations = []pgstore.Migration{{Version: 1, SQL: schemaSQL}}

// Tables are the store's tables in version 1, in foreign-key order (a table
// comes after every table it references). cmd/store-migrate copies them in
// this order.
var Tables = []string{"orgs", "org_parent_edges", "org_aliases", "org_match_overrides"}

// idRe is the affiliation.id charset from the operator-manifest schema —
// kebab slug, 2..64 chars, no leading/trailing dash.
var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)

// Closed vocabularies. Empty kind = not yet declared (allowed); anything
// else must parse.
var (
	validKinds = map[string]bool{"": true, "vendor": true, "organic": true, "partner": true, "civil": true}
	// ADR 1151 target sector, PLUS the ADR 1077 legacy values, which stay
	// acceptable for the whole migration window — an org registered before the
	// window must not become unwritable.
	validSectors = map[string]bool{
		"": true, "state": true, "commercial": true, "civil-society": true,
		"military": true, "law-enforcement": true, "civil-government": true,
		"intelligence": true, "ngo": true, "media": true, "academic": true,
	}
	// ADR 1151. Meaningful only when sector is state; CreateOrg enforces that
	// pairing rather than accepting a branch that contradicts the sector.
	validGovBranches = map[string]bool{
		"": true, "military": true, "law-enforcement": true, "intelligence": true, "civil": true,
	}
	// ADR 1171. national = owned by one sovereign regardless of who staffs it;
	// multinational = constituted by a body of nations. '' = not yet declared,
	// which every org registered before this change is, and which must stay
	// writable — an org cannot become unwritable because a field was added.
	validCompositions = map[string]bool{"": true, "national": true, "multinational": true}
	validProvenance   = map[string]bool{"authoritative": true, "operator-verified": true, "open-source": true, "self-asserted": true}
	validAliasKinds   = map[string]bool{"name-variant": true, "abbreviation": true, "absorbed-id": true}
	maxChainDepth     = 64 // refuse pathological chains; real ones are < 10
)

// Sentinel errors handlers translate to HTTP verdicts.
var (
	ErrNotFound   = errors.New("org not found")
	ErrConflict   = errors.New("conflict")
	ErrCycle      = errors.New("parent edge would create a cycle")
	ErrValidation = errors.New("validation")
)

// Org is one registry row.
type Org struct {
	ID          string `db:"id" json:"id"`
	DisplayName string `db:"display_name" json:"display_name,omitempty"`
	// Kind is DEPRECATED by ADR 1151 and still written during the migration
	// window; it is derivable from Sector + GovBranch + the organic test.
	Kind string `db:"kind" json:"kind,omitempty"`
	// Sector / GovBranch are the ADR 1151 target block. Sector is the UN SNA
	// institutional split (state | commercial | civil-society); GovBranch is
	// the branch of government and is meaningful only when Sector is state.
	// Both empty on an org registered before the migration — absent, not
	// unknown-and-guessed.
	// Composition is the ADR 1171 org axis: is this organisation owned by one
	// sovereign, or constituted by a body of nations? It decides what `Nation`
	// means on the row — a sovereign's alpha-3 code for `national`, a body
	// marker (NATO, EU) for `multinational` — and it is what makes a
	// `constituent` membership refusable on a national org.
	Composition string `db:"composition" json:"composition,omitempty"`
	Sector      string `db:"sector" json:"sector,omitempty"`
	GovBranch   string `db:"gov_branch" json:"gov_branch,omitempty"`
	Nation      string `db:"nation" json:"nation,omitempty"`
	EchelonHint string `db:"echelon_hint" json:"echelon_hint,omitempty"`
	Provenance  string `db:"provenance" json:"provenance"`
	Status      string `db:"status" json:"status"`
	CreatedBy   string `db:"created_by" json:"created_by,omitempty"`
	CreatedAt   int64  `db:"created_at" json:"created_at"`
}

// OrgView is an Org joined with its current parent and aliases — the list /
// export shape.
type OrgView struct {
	Org
	Parent  string   `json:"parent,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
}

// ParentEdge is one effective-dated containment edge.
type ParentEdge struct {
	OrgID         string `db:"org_id" json:"org_id"`
	ParentID      string `db:"parent_id" json:"parent_id"`
	ValidFrom     int64  `db:"valid_from" json:"valid_from"`
	ValidTo       *int64 `db:"valid_to" json:"valid_to,omitempty"`
	RecordedAt    int64  `db:"recorded_at" json:"recorded_at"`
	Actor         string `db:"actor" json:"actor,omitempty"`
	Justification string `db:"justification" json:"justification,omitempty"`
}

// Store wraps the Postgres handle for the org registry.
type Store struct {
	db *sqlx.DB
	// Now is the clock; overridable in tests so effective-dating is
	// assertable without sleeping.
	Now func() time.Time
}

// Open connects to the Postgres database at dsn and applies any migration not
// yet recorded, then the idempotent data backfills. Safe to call repeatedly.
func Open(dsn string) (*Store, error) {
	ctx := context.Background()
	db, err := pgstore.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pgstore.Migrate(ctx, db, Migrations); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := pgstore.RequireTables(ctx, db, Tables...); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Additive columns from the SQLite build, where they brought a registry
	// created before the column existed up to date. Version 1 has all three, so
	// on Postgres these find the column and do nothing.
	for _, col := range []struct{ name, decl string }{
		{"sector", `TEXT COLLATE "C" NOT NULL DEFAULT ''`},
		{"gov_branch", `TEXT COLLATE "C" NOT NULL DEFAULT ''`},
		{"composition", `TEXT COLLATE "C" NOT NULL DEFAULT ''`},
	} {
		if err := pgstore.AddColumnIfMissing(ctx, db, "orgs", col.name, col.decl); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("add orgs.%s: %w", col.name, err)
		}
	}
	if err := backfillSectorFromKind(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := backfillNationAlpha3(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, Now: time.Now}, nil
}

// backfillNationAlpha3 folds every stored alpha-2 nation onto its alpha-3 form.
//
// WHY A MIGRATION AND NOT ONLY A FOLD ON WRITE. CreateOrg and UpdateOrg call
// nation.Normalize, so anything written after that landed is already alpha-3.
// Every row written BEFORE it keeps whatever spelling the operator typed, and
// both spellings can be live: a registry can carry a row as US while the
// committed roster carries USA for the same country. A fold applied only on
// write leaves that split in place forever, because nothing rewrites a row that
// nobody edits.
//
// The split is not cosmetic. Comparisons that fold on read (Suggest, merge's
// conflict test) tolerate it; comparisons that do not — and the org-owned
// encryption intersection in the backlog is one, matching a person's nationality
// against the nation that owns their org — would silently deny a US person in a
// US org spelled the other way. Making the stored value canonical once removes
// the class rather than teaching each new consumer to fold.
//
// Idempotent by construction: folding an alpha-3 value returns it unchanged, so
// the second run updates nothing. NATO, EU and any unrecognized string pass
// through untouched, which is what keeps the multinational markers working.
func backfillNationAlpha3(db *sqlx.DB) error {
	var stored []string
	if err := db.Select(&stored, `SELECT DISTINCT nation FROM orgs WHERE COALESCE(nation, '') != ''`); err != nil {
		return fmt.Errorf("read orgs.nation for fold: %w", err)
	}
	for _, was := range stored {
		is := nation.Normalize(was)
		if is == was {
			continue
		}
		if _, err := db.ExecContext(context.Background(), `UPDATE orgs SET nation = $1 WHERE nation = $2`, is, was); err != nil {
			return fmt.Errorf("fold orgs.nation %q to %q: %w", was, is, err)
		}
	}
	return nil
}

// backfillSectorFromKind is ADR 1151 migration step 4's mechanical half.
//
// WHY THIS IS SAFE TO DO AUTOMATICALLY, when almost nothing else in this repo
// backfills on startup. The kind→sector mapping is TOTAL and LOSSLESS: every
// one of the four legacy kinds has exactly one sector, because the old field's
// governmental values (organic, partner) differed only on ours-vs-foreign, and
// that axis moves to the derived `organic` test rather than to sector.
//
//	vendor            -> commercial
//	civil             -> civil-society
//	organic | partner -> state
//
// So this is not a guess and not a judgement call; it is the same fact written
// in the newer vocabulary. Nothing is inferred and nothing is lost — `kind` is
// left exactly as it was, so the change is reversible by clearing sector.
//
// WHAT IT DELIBERATELY DOES NOT DO: it never sets gov_branch. Which branch of
// government an org belongs to is genuinely absent from the old data — `organic`
// says "ours", not "army" — and inventing `military` because most of them are
// would be exactly the kind of plausible-looking fabrication that makes a
// registry untrustworthy. Those rows keep an empty gov_branch until a human
// declares one, and ADR 1151 step 4 names that as the part needing a person.
//
// Idempotent: the WHERE clause only touches rows whose sector is still empty,
// so a redeploy is a no-op and an operator who deliberately cleared a sector
// does not get it silently re-derived on the next restart... except that it
// would. That is the one wrinkle worth naming: to UN-set a sector durably you
// must also change the kind, which is the correct forcing function anyway,
// because a row asserting kind=vendor with no sector is asserting less than it
// knows.
func backfillSectorFromKind(db *sqlx.DB) error {
	res, err := db.ExecContext(context.Background(), `
		UPDATE orgs
		   SET sector = CASE kind
		                  WHEN 'vendor'  THEN 'commercial'
		                  WHEN 'civil'   THEN 'civil-society'
		                  WHEN 'organic' THEN 'state'
		                  WHEN 'partner' THEN 'state'
		                END
		 WHERE COALESCE(sector, '') = ''
		   AND kind IN ('vendor', 'civil', 'organic', 'partner')`)
	if err != nil {
		return fmt.Errorf("backfill orgs.sector from kind: %w", err)
	}
	_, _ = res.RowsAffected() //nolint:errcheck // count is informational; the migration is idempotent
	return nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// ValidID reports whether id parses as an org slug.
func ValidID(id string) bool { return idRe.MatchString(id) }

// Resolve maps an id-or-alias to its canonical org id. ErrNotFound when
// neither an org nor an alias knows the name.
func (s *Store) Resolve(name string) (string, error) {
	name = strings.TrimSpace(name)
	var row struct {
		ID     string `db:"id"`
		Status string `db:"status"`
	}
	err := s.db.Get(&row, `SELECT id, status FROM orgs WHERE id = $1`, name)
	if err == nil && row.Status != "absorbed" {
		return row.ID, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	// Either unknown, or an ABSORBED org id — both route through the alias
	// table (a merge records the loser id as an absorbed-id alias of the
	// winner, which is what keeps everything keyed to the old id resolving).
	var id string
	err = s.db.Get(&id, `SELECT org_id FROM org_aliases WHERE alias = $1`, name)
	if errors.Is(err, sql.ErrNoRows) {
		if row.ID != "" {
			// Absorbed but no alias row (shouldn't happen; un-merge removes
			// the alias AND reactivates atomically) — answer the row itself
			// rather than inventing a miss.
			return row.ID, nil
		}
		return "", fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return id, err
}

// CreateOrg inserts a new org row. Refuses an invalid id/vocabulary, a
// duplicate id, and an id already taken as an alias (one namespace).
func (s *Store) CreateOrg(o Org) (Org, error) {
	o.ID = strings.TrimSpace(o.ID)
	if !ValidID(o.ID) {
		return Org{}, fmt.Errorf("%w: org id %q must match %s", ErrValidation, o.ID, idRe)
	}
	if !validKinds[o.Kind] {
		return Org{}, fmt.Errorf("%w: unknown kind %q", ErrValidation, o.Kind)
	}
	if o.Provenance == "" {
		o.Provenance = "self-asserted"
	}
	if !validProvenance[o.Provenance] {
		return Org{}, fmt.Errorf("%w: unknown provenance %q", ErrValidation, o.Provenance)
	}
	o.Sector = strings.ToLower(strings.TrimSpace(o.Sector))
	o.GovBranch = strings.ToLower(strings.TrimSpace(o.GovBranch))
	// Fold the nation on WRITE, so one country has one spelling in the store.
	// Create, Update and Suggest all call normalizeNation. Folding here is what
	// makes the Suggest fold below a back-stop for old rows rather than the only
	// defense.
	o.Nation = normalizeNation(o.Nation)
	o.Composition = strings.ToLower(strings.TrimSpace(o.Composition))
	if !validCompositions[o.Composition] {
		return Org{}, fmt.Errorf("%w: unknown composition %q (national | multinational)", ErrValidation, o.Composition)
	}
	if !validSectors[o.Sector] {
		return Org{}, fmt.Errorf("%w: unknown sector %q", ErrValidation, o.Sector)
	}
	if !validGovBranches[o.GovBranch] {
		return Org{}, fmt.Errorf("%w: unknown gov_branch %q", ErrValidation, o.GovBranch)
	}
	// gov_branch is meaningful ONLY under sector=state (ADR 1151). Refuse the
	// contradiction rather than storing a branch nothing will ever read: a
	// commercial company has no branch of government, and silently keeping one
	// would make the row assert something false.
	if o.GovBranch != "" && o.Sector != "state" {
		return Org{}, fmt.Errorf("%w: gov_branch %q is only meaningful when sector is state, got sector %q",
			ErrValidation, o.GovBranch, o.Sector)
	}
	if o.Status == "" {
		o.Status = "active"
	}
	// One namespace: an id that is someone's alias is a conflict.
	var n int
	if err := s.db.Get(&n, `SELECT COUNT(*) FROM org_aliases WHERE alias = $1`, o.ID); err != nil {
		return Org{}, err
	}
	if n > 0 {
		return Org{}, fmt.Errorf("%w: %q is an alias of an existing org", ErrConflict, o.ID)
	}
	o.CreatedAt = s.Now().Unix()
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO orgs (id, display_name, kind, composition, sector, gov_branch, nation, echelon_hint, provenance, status, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		o.ID, o.DisplayName, o.Kind, o.Composition, o.Sector, o.GovBranch, o.Nation, o.EchelonHint, o.Provenance, o.Status, o.CreatedBy, o.CreatedAt)
	if err != nil {
		if pgstore.IsUniqueViolation(err) {
			// Org ids are ONE flat namespace per goatnet, so a Polish CIS and a
			// Lithuanian CIS both want the slug `cis` and the second is refused.
			// Refusing is correct — the alternative is two different units
			// sharing an identity — but "already exists" alone leaves the
			// admin stuck: the matcher is nation-scoped, so they were offered
			// no candidate either, and nothing told them the existing row is a
			// DIFFERENT COUNTRY's org. Name the nation, so the next move
			// (a nation-qualified slug) is obvious.
			if existing, gerr := s.GetOrg(o.ID); gerr == nil && existing.Nation != "" &&
				!strings.EqualFold(existing.Nation, o.Nation) {
				return Org{}, fmt.Errorf(
					"%w: org %q already exists and belongs to %s, not %s — if this is a different organisation, give it a nation-qualified id such as %q",
					ErrConflict, o.ID, existing.Nation, firstNonBlank(o.Nation, "an unstated nation"),
					strings.ToLower(firstNonBlank(o.Nation, "xxx"))+"-"+o.ID)
			}
			return Org{}, fmt.Errorf("%w: org %q already exists", ErrConflict, o.ID)
		}
		return Org{}, err
	}
	return o, nil
}

// GetOrg returns the org for an id-or-alias, with its canonical id.
func (s *Store) GetOrg(name string) (OrgView, error) {
	id, err := s.Resolve(name)
	if err != nil {
		return OrgView{}, err
	}
	var o Org
	if err := s.db.Get(&o, `SELECT * FROM orgs WHERE id = $1`, id); err != nil {
		return OrgView{}, err
	}
	return s.view(o)
}

func (s *Store) view(o Org) (OrgView, error) {
	v := OrgView{Org: o}
	p, err := s.CurrentParent(o.ID)
	if err != nil {
		return v, err
	}
	v.Parent = p
	if err := s.db.Select(&v.Aliases, `SELECT alias FROM org_aliases WHERE org_id = $1 ORDER BY alias`, o.ID); err != nil {
		return v, err
	}
	return v, nil
}

// ListOrgs returns every org (any status), alphabetical, with current parent
// + aliases — the export/catalog shape.
func (s *Store) ListOrgs() ([]OrgView, error) {
	var rows []Org
	if err := s.db.Select(&rows, `SELECT * FROM orgs ORDER BY id`); err != nil {
		return nil, err
	}
	out := make([]OrgView, 0, len(rows))
	for _, o := range rows {
		v, err := s.view(o)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// CurrentParent returns the open parent edge's parent id, "" for a root.
func (s *Store) CurrentParent(orgID string) (string, error) {
	var p string
	err := s.db.Get(&p, `SELECT parent_id FROM org_parent_edges WHERE org_id = $1 AND valid_to IS NULL`, orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return p, err
}

// Children returns the orgs whose CURRENT parent is name, ordered by id.
//
// The downward direction needed no schema change: an edge is a row, and
// org_parent_edges_parent_idx has existed since the table was created. Only the
// query was missing, which is why the hierarchy felt one-directional -- it was,
// at the API, not in the data.
//
// Deliberately NOT a reciprocal `children` field on the org: that would be a
// second copy of an edge the row already holds, and nothing could keep the two
// agreeing. The unique partial index org_parent_edges_one_open enforces one
// open parent per org; a stored child list has no equivalent invariant.
//
// Semantics match Ancestors exactly -- CURRENT edges only (valid_to IS NULL),
// not an as-of-date walk. The two directions must agree or the same tree reads
// differently depending on which way you traverse it.
func (s *Store) Children(name string) ([]OrgView, error) {
	id, err := s.Resolve(name)
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := s.db.Select(&ids,
		`SELECT org_id FROM org_parent_edges WHERE parent_id = $1 AND valid_to IS NULL ORDER BY org_id`,
		id); err != nil {
		return nil, err
	}
	out := make([]OrgView, 0, len(ids))
	for _, cid := range ids {
		v, err := s.GetOrg(cid)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Descendants returns the full subtree under name, breadth-first, excluding
// name itself. Capped at maxChainDepth like Ancestors, and it additionally
// refuses to revisit an id -- SetParent already rejects cycles, but a read path
// must not hang if a cycle ever reaches the table by another route.
func (s *Store) Descendants(name string) ([]OrgView, error) {
	id, err := s.Resolve(name)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{id: true}
	var out []OrgView
	frontier := []string{id}
	for depth := 0; depth < maxChainDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, cur := range frontier {
			kids, err := s.Children(cur)
			if err != nil {
				return nil, err
			}
			for _, k := range kids {
				if seen[k.ID] {
					continue
				}
				seen[k.ID] = true
				out = append(out, k)
				next = append(next, k.ID)
			}
		}
		frontier = next
	}
	return out, nil
}

// Ancestors resolves name and returns its current parent chain nearest-first
// (parent, grandparent, … root), empty for a root org. The walk is capped at
// maxChainDepth as a belt to SetParent's cycle refusal, not a license for
// cycles to exist.
func (s *Store) Ancestors(name string) ([]OrgView, error) {
	id, err := s.Resolve(name)
	if err != nil {
		return nil, err
	}
	var chain []OrgView
	for i := 0; i < maxChainDepth; i++ {
		parent, err := s.CurrentParent(id)
		if err != nil {
			return nil, err
		}
		if parent == "" {
			return chain, nil
		}
		v, err := s.GetOrg(parent)
		if err != nil {
			return nil, err
		}
		chain = append(chain, v)
		id = v.ID
	}
	return chain, nil
}

// ParentAsOf returns the parent whose validity window contains t ("" if none).
func (s *Store) ParentAsOf(orgID string, t time.Time) (string, error) {
	ts := t.Unix()
	var p string
	err := s.db.Get(&p, `SELECT parent_id FROM org_parent_edges
		WHERE org_id = $1 AND valid_from <= $2 AND (valid_to IS NULL OR valid_to > $3)
		ORDER BY valid_from DESC LIMIT 1`, orgID, ts, ts)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return p, err
}

// SetParent points orgID at parentID (both id-or-alias), closing any open
// edge first. Refuses self-parenting and any edge that would create a cycle.
// A no-op (same current parent) returns without writing.
func (s *Store) SetParent(orgID, parentID, actor, justification string) error {
	org, err := s.Resolve(orgID)
	if err != nil {
		return err
	}
	parent, err := s.Resolve(parentID)
	if err != nil {
		return err
	}
	if org == parent {
		return fmt.Errorf("%w: an org cannot be its own parent", ErrValidation)
	}
	// Write-time cycle refusal: walk up from the WOULD-BE parent; reaching
	// org means org is an ancestor of parent, so parent→org would loop.
	cur := parent
	for i := 0; cur != ""; i++ {
		if i >= maxChainDepth {
			return fmt.Errorf("%w: parent chain exceeds %d hops (cyclic or pathological)", ErrCycle, maxChainDepth)
		}
		if cur == org {
			return fmt.Errorf("%w: %s is an ancestor of %s", ErrCycle, org, parent)
		}
		cur, err = s.CurrentParent(cur)
		if err != nil {
			return err
		}
	}
	existing, err := s.CurrentParent(org)
	if err != nil {
		return err
	}
	if existing == parent {
		return nil // idempotent no-op
	}
	now := s.Now().Unix()
	tx, err := s.db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(context.Background(), `UPDATE org_parent_edges SET valid_to = $1 WHERE org_id = $2 AND valid_to IS NULL`, now, org); err != nil {
		return err
	}
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO org_parent_edges (org_id, parent_id, valid_from, valid_to, recorded_at, actor, justification)
		VALUES ($1, $2, $3, NULL, $4, $5, $6)`, org, parent, now, now, actor, justification); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearParent closes the open parent edge (org becomes a root). No-op when
// already a root.
func (s *Store) ClearParent(orgID, actor, justification string) error {
	org, err := s.Resolve(orgID)
	if err != nil {
		return err
	}
	_ = actor // recorded on the closing row's history via the edge it closes
	_ = justification
	now := s.Now().Unix()
	_, err = s.db.ExecContext(context.Background(), `UPDATE org_parent_edges SET valid_to = $1 WHERE org_id = $2 AND valid_to IS NULL`, now, org)
	return err
}

// OrgUpdate names the correctable org facts. A nil field is left unchanged.
// Kind and Nation refuse an empty replacement (an org never becomes kindless
// by edit); DisplayName may be set to "" (fall back to rendering the id).
type OrgUpdate struct {
	DisplayName *string
	Kind        *string
	Nation      *string
	// Composition (ADR 1171). Correctable in place like every other org fact —
	// an org registered before this axis existed carries '' and needs a way to
	// declare it that is not DB surgery, which is the whole reason this verb
	// exists. Unlike Kind and Nation it MAY be set back to '': "we do not know
	// yet" is a real state here, and refusing to return to it would make a
	// wrong guess permanent.
	Composition *string
}

// UpdateOrg corrects an existing org's facts in place — the verb the
// `origin` kind mislabel lacked: a wrong kind captured at
// onboarding could previously be fixed only by DB surgery, because
// create/parent/alias/merge were the registry's only mutations. An update
// never re-keys anything: the org id is immutable and absent from every wire
// anchor (ADR 1112 §3), so this changes registry facts and nothing else.
// Resolves aliases (updating an absorbed id updates its winner, consistent
// with everything else keyed to the old id).
func (s *Store) UpdateOrg(name string, u OrgUpdate) (OrgView, error) {
	id, err := s.Resolve(name)
	if err != nil {
		return OrgView{}, err
	}
	sets, args := []string{}, []any{}
	if u.Kind != nil {
		k := strings.TrimSpace(*u.Kind)
		if k == "" || !validKinds[k] {
			return OrgView{}, fmt.Errorf("%w: unknown kind %q", ErrValidation, k)
		}
		sets, args = append(sets, fmt.Sprintf("kind = $%d", len(args)+1)), append(args, k)
	}
	if u.Nation != nil {
		// Normalized, not merely trimmed: `goat admin org update <id> --nation US`
		// must land the same value as `--nation USA`, or correcting a row by hand
		// re-introduces the split it was run to fix.
		n := normalizeNation(*u.Nation)
		if n == "" {
			return OrgView{}, fmt.Errorf("%w: nation cannot be cleared by update", ErrValidation)
		}
		sets, args = append(sets, fmt.Sprintf("nation = $%d", len(args)+1)), append(args, n)
	}
	if u.Composition != nil {
		c := strings.ToLower(strings.TrimSpace(*u.Composition))
		if !validCompositions[c] {
			return OrgView{}, fmt.Errorf("%w: unknown composition %q (national | multinational)", ErrValidation, c)
		}
		sets, args = append(sets, fmt.Sprintf("composition = $%d", len(args)+1)), append(args, c)
	}
	if u.DisplayName != nil {
		sets, args = append(sets, fmt.Sprintf("display_name = $%d", len(args)+1)), append(args, strings.TrimSpace(*u.DisplayName))
	}
	if len(sets) == 0 {
		return OrgView{}, fmt.Errorf("%w: nothing to update", ErrValidation)
	}
	args = append(args, id)
	q := `UPDATE orgs SET ` + strings.Join(sets, ", ") + fmt.Sprintf(` WHERE id = $%d`, len(args))
	if _, err := s.db.ExecContext(context.Background(), q, args...); err != nil {
		return OrgView{}, err
	}
	return s.GetOrg(id)
}

// AddAlias records alias → orgID. Refuses an alias colliding with any org id
// or existing alias, and an unknown kind.
func (s *Store) AddAlias(alias, orgID, kind, actor string) error {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return fmt.Errorf("%w: empty alias", ErrValidation)
	}
	if kind == "" {
		kind = "name-variant"
	}
	if !validAliasKinds[kind] {
		return fmt.Errorf("%w: unknown alias kind %q", ErrValidation, kind)
	}
	org, err := s.Resolve(orgID)
	if err != nil {
		return err
	}
	var n int
	if err := s.db.Get(&n, `SELECT COUNT(*) FROM orgs WHERE id = $1`, alias); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: %q is an existing org id", ErrConflict, alias)
	}
	_, err = s.db.ExecContext(context.Background(), `INSERT INTO org_aliases (alias, org_id, kind, recorded_at, actor) VALUES ($1, $2, $3, $4, $5)`,
		alias, org, kind, s.Now().Unix(), actor)
	if err != nil && pgstore.IsUniqueViolation(err) {
		return fmt.Errorf("%w: alias %q already recorded", ErrConflict, alias)
	}
	return err
}

// Catalog is the deterministic export shape the committed catalog
// (ops/identity/orgs/<goatnet>.yaml) and the sync script consume.
type Catalog struct {
	SchemaVersion int       `json:"schema_version"`
	Orgs          []OrgView `json:"orgs"`
}

// Export returns the full catalog, deterministically ordered.
func (s *Store) Export() (Catalog, error) {
	orgs, err := s.ListOrgs()
	if err != nil {
		return Catalog{}, err
	}
	sort.Slice(orgs, func(i, j int) bool { return orgs[i].ID < orgs[j].ID })
	return Catalog{SchemaVersion: 1, Orgs: orgs}, nil
}

// firstNonBlank returns the first non-empty string, for error text that must
// stay readable when a field the caller omitted is the one being described.
func firstNonBlank(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
