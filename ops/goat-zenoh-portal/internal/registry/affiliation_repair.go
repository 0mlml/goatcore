package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrAffiliationRepairInvalid = errors.New("invalid affiliation repair")
	ErrAffiliationRepairStale   = errors.New("affiliation repair preview is stale; preview again")
)

// AffiliationRepairRequest names exact stored source spellings. Only To is
// normalized; normalizing From would make some legacy rows impossible to select.
type AffiliationRepairRequest struct {
	From             []string `json:"from"`
	To               string   `json:"to"`
	Reason           string   `json:"reason"`
	Apply            bool     `json:"apply"`
	ExpectedRevision string   `json:"expected_revision,omitempty"`
}

// AffiliationRepairRow deliberately contains no credential or personnel fields.
type AffiliationRepairRow struct {
	Kind        string `db:"kind" json:"kind"`
	ID          string `db:"id" json:"id"`
	Affiliation string `db:"affiliation" json:"affiliation"`
}

type AffiliationRepairResult struct {
	From     []string               `json:"from"`
	To       string                 `json:"to"`
	Revision string                 `json:"revision"`
	Rows     []AffiliationRepairRow `json:"rows"`
	Grants   int                    `json:"grants"`
	Slots    int                    `json:"slots"`
	Applied  bool                   `json:"applied"`
	AuditID  int64                  `json:"audit_id,omitempty"`
}

func validateAffiliationRepair(req AffiliationRepairRequest) (AffiliationRepairRequest, error) {
	req.To = cleanAffiliation(req.To)
	req.Reason = strings.TrimSpace(req.Reason)
	if len(req.From) == 0 || foldAffiliation(req.To) == "" || req.Reason == "" {
		return req, fmt.Errorf("%w: from, to and reason are required", ErrAffiliationRepairInvalid)
	}
	seen := make(map[string]bool)
	for _, source := range req.From {
		if source == req.To || seen[source] || foldAffiliation(source) != foldAffiliation(req.To) {
			return req, fmt.Errorf("%w: sources must be distinct spellings of the target's comparison group", ErrAffiliationRepairInvalid)
		}
		seen[source] = true
	}
	req.From = append([]string(nil), req.From...)
	sort.Strings(req.From)
	if req.Apply {
		decoded, err := hex.DecodeString(req.ExpectedRevision)
		if err != nil || len(decoded) != sha256.Size || req.ExpectedRevision != strings.ToLower(req.ExpectedRevision) {
			return req, fmt.Errorf("%w: apply requires the preview's expected_revision", ErrAffiliationRepairInvalid)
		}
	} else if req.ExpectedRevision != "" {
		return req, fmt.Errorf("%w: expected_revision is only valid with apply", ErrAffiliationRepairInvalid)
	}
	return req, nil
}

type affiliationRepairReader interface {
	SelectContext(context.Context, any, string, ...any) error
}

func affiliationRepairSnapshot(ctx context.Context, reader affiliationRepairReader, req AffiliationRepairRequest) ([]AffiliationRepairRow, string, error) {
	var rows []AffiliationRepairRow
	if err := reader.SelectContext(ctx, &rows, `
		SELECT 'grant' AS kind, custodian AS id, affiliation FROM machine_grants WHERE affiliation != ''
		UNION ALL
		SELECT 'slot' AS kind, slot_id AS id, affiliation FROM slots WHERE affiliation IS NOT NULL AND affiliation != ''
		ORDER BY kind, id`); err != nil {
		return nil, "", fmt.Errorf("read affiliation repair group: %w", err)
	}
	group := make([]AffiliationRepairRow, 0)
	for _, row := range rows {
		if foldAffiliation(row.Affiliation) == foldAffiliation(req.To) {
			group = append(group, row)
		}
	}
	// Bind the exact mapping and the entire comparison group, including target
	// rows. A new enrollment or a newly introduced spelling invalidates preview.
	encoded, err := json.Marshal(struct {
		From []string               `json:"from"`
		To   string                 `json:"to"`
		Rows []AffiliationRepairRow `json:"rows"`
	}{req.From, req.To, group})
	if err != nil {
		return nil, "", fmt.Errorf("encode affiliation repair revision: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return group, hex.EncodeToString(sum[:]), nil
}

func selectAffiliationRepair(req AffiliationRepairRequest, group []AffiliationRepairRow, revision string) (*AffiliationRepairResult, error) {
	selected := make(map[string]int)
	for _, source := range req.From {
		selected[source] = 0
	}
	result := &AffiliationRepairResult{From: req.From, To: req.To, Revision: revision, Rows: make([]AffiliationRepairRow, 0)}
	for _, row := range group {
		if row.Affiliation == req.To {
			continue
		}
		if _, ok := selected[row.Affiliation]; !ok {
			return nil, fmt.Errorf("%w: comparison group also contains unselected spelling %q", ErrAffiliationRepairInvalid, row.Affiliation)
		}
		selected[row.Affiliation]++
		result.Rows = append(result.Rows, row)
		if row.Kind == "grant" {
			result.Grants++
		} else {
			result.Slots++
		}
	}
	for _, source := range req.From {
		if selected[source] == 0 {
			return nil, fmt.Errorf("%w: source spelling %q has no records", ErrAffiliationRepairInvalid, source)
		}
	}
	return result, nil
}

// RepairAffiliations previews or applies a deliberate same-organization repair.
// The API must enforce operator authorization. The snapshot, both updates and
// the audit share a transaction; a competing SQLite writer cannot change the
// snapshot underneath an application (a write conflict fails without a commit).
func (r *Registry) RepairAffiliations(ctx context.Context, req AffiliationRepairRequest, principal, actor string) (*AffiliationRepairResult, error) {
	req, err := validateAffiliationRepair(req)
	if err != nil {
		return nil, err
	}
	if req.Apply && (strings.TrimSpace(principal) == "" || strings.TrimSpace(actor) == "") {
		return nil, fmt.Errorf("%w: authenticated principal and actor are required", ErrAffiliationRepairInvalid)
	}
	if !req.Apply {
		group, revision, err := affiliationRepairSnapshot(ctx, r.reader(), req)
		if err != nil {
			return nil, err
		}
		return selectAffiliationRepair(req, group, revision)
	}
	tx, err := r.beginLocked(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin affiliation repair: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	group, revision, err := affiliationRepairSnapshot(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	if req.Apply && req.ExpectedRevision != revision {
		return nil, ErrAffiliationRepairStale
	}
	result, err := selectAffiliationRepair(req, group, revision)
	if err != nil {
		return nil, err
	}

	for _, row := range result.Rows {
		query := `UPDATE machine_grants SET affiliation = $1 WHERE custodian = $2 AND affiliation = $3`
		if row.Kind == "slot" {
			query = `UPDATE slots SET affiliation = $1 WHERE slot_id = $2 AND affiliation = $3`
		}
		updated, err := tx.ExecContext(ctx, query, req.To, row.ID, row.Affiliation)
		if err != nil {
			return nil, fmt.Errorf("update affiliation repair %s: %w", row.Kind, err)
		}
		n, err := updated.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("count affiliation repair rows: %w", err)
		}
		if n != 1 {
			return nil, ErrAffiliationRepairStale
		}
	}
	result.Applied = true
	details, err := json.Marshal(struct {
		Principal string `json:"principal"`
		Reason    string `json:"reason"`
		*AffiliationRepairResult
	}{principal, req.Reason, result})
	if err != nil {
		return nil, fmt.Errorf("encode affiliation repair audit: %w", err)
	}
	var auditID int64
	if err := tx.GetContext(ctx, &auditID, `INSERT INTO audit_events (occurred_at, actor, action, target, details) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		time.Now().Unix(), actor, "machine-affiliation-repair", req.To, string(details)); err != nil {
		return nil, fmt.Errorf("audit affiliation repair: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit affiliation repair: %w", err)
	}
	result.Applied, result.AuditID = true, auditID
	return result, nil
}
