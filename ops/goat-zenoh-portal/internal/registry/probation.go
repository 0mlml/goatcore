package registry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgstore"
)

// probation.go is the store for the anomaly-probation list (ADR 1068 /
// docs/design/anomaly-probation-lifecycle.md): the holding area for things the
// COP surfaces that don't reconcile — a stray first, other anomaly kinds later.
// Held state lives here, deliberately separate from the declared registry (that
// is for things decided to belong; this is for undecided maybes). The store is
// non-load-bearing — nothing here is in an enforcement path; repair + purge are
// carried out through the existing onboard/intake + revoke paths, and this row
// only records the disposition + who + when.

// Probation is one held entity. Repair/purge are recorded as a disposition
// change, never an auto-action — a cure_by deadline only *flags*.
type Probation struct {
	ID          string `db:"id"           json:"id"`
	Ref         string `db:"ref"          json:"ref"`   // stable ref to the entity (identity id / anchor / recon key)
	Kind        string `db:"kind"         json:"kind"`  // identity | topic | peer | inbound
	Label       string `db:"label"        json:"label"` // human display, display-only
	Reason      string `db:"reason"       json:"reason"`
	DetectedAt  int64  `db:"detected_at"  json:"detected_at"`
	HeldAt      int64  `db:"held_at"      json:"held_at"`
	HeldBy      string `db:"held_by"      json:"held_by"`
	CureBy      int64  `db:"cure_by"      json:"cure_by"`
	Disposition string `db:"disposition"  json:"disposition"`
	DecidedAt   *int64 `db:"decided_at"   json:"decided_at,omitempty"`
	DecidedBy   string `db:"decided_by"   json:"decided_by"`
	Note        string `db:"note"         json:"note"`
	// Expired is computed (not stored): the cure_by deadline has passed while
	// the entry is still open. Surfaced so the COP can flag it — the deadline
	// never triggers an action on its own (ADR 1068: flags, never auto-deletes).
	Expired bool `db:"-" json:"expired"`
}

// Probation dispositions.
const (
	ProbationOpen     = "probation" // held, awaiting verify
	ProbationRepaired = "repaired"  // verified real+relevant → regularized (onboard/intake)
	ProbationPurged   = "purged"    // verified not-wanted → removed + cleansed (revoke --purge)
)

// Probation kinds. v1 implements `identity`; the rest are schema-reserved so
// the same list holds them later without a re-model (ADR 1068 §Generalization).
const (
	ProbationKindIdentity = "identity"
	ProbationKindTopic    = "topic"
	ProbationKindPeer     = "peer"
	ProbationKindInbound  = "inbound"
)

// DefaultCureWindow is the default hold length when a hold doesn't set cure_by.
// Operator-overridable per entry; a policy default (not a hard rule) so nothing
// lingers unbounded. 14 days matches the ADR 1068 open-question lean.
const DefaultCureWindow = 14 * 24 * time.Hour

// ErrProbationOpen means the ref already has an OPEN probation entry — a ref
// can't be double-held (the partial unique index enforces it).
var ErrProbationOpen = errors.New("entity is already on probation")

func newProbationID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "prob-" + hex.EncodeToString(b[:]), nil
}

// validProbationKind guards the kind enum. v1 only stores identity, but the
// column accepts the reserved kinds so a later phase needs no migration.
func validProbationKind(k string) bool {
	switch k {
	case ProbationKindIdentity, ProbationKindTopic, ProbationKindPeer, ProbationKindInbound:
		return true
	}
	return false
}

// HoldProbation enters an entity into probation. Assigns id / held_at /
// disposition / a default cure_by when unset. Returns ErrProbationOpen when the
// ref already has an open entry (no double-hold). held_by is the operator
// and must be set by the caller.
func (r *Registry) HoldProbation(ctx context.Context, p *Probation) error {
	if strings.TrimSpace(p.Ref) == "" {
		return fmt.Errorf("probation ref is required")
	}
	if strings.TrimSpace(p.HeldBy) == "" {
		return fmt.Errorf("probation held_by (operator) is required")
	}
	if p.Kind == "" {
		p.Kind = ProbationKindIdentity
	}
	if !validProbationKind(p.Kind) {
		return fmt.Errorf("invalid probation kind %q", p.Kind)
	}
	if p.ID == "" {
		id, err := newProbationID()
		if err != nil {
			return fmt.Errorf("probation id: %w", err)
		}
		p.ID = id
	}
	now := time.Now()
	if p.HeldAt == 0 {
		p.HeldAt = now.Unix()
	}
	if p.CureBy == 0 {
		p.CureBy = now.Add(DefaultCureWindow).Unix()
	}
	if p.Disposition == "" {
		p.Disposition = ProbationOpen
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO probation (id, ref, kind, label, reason, detected_at, held_at, held_by, cure_by, disposition, decided_at, decided_by, note)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		p.ID, p.Ref, p.Kind, p.Label, p.Reason, p.DetectedAt, p.HeldAt, p.HeldBy, p.CureBy, p.Disposition, p.DecidedAt, p.DecidedBy, p.Note)
	if err != nil {
		if pgstore.IsUniqueViolation(err) {
			return ErrProbationOpen
		}
		return fmt.Errorf("insert probation: %w", err)
	}
	return nil
}

// ListProbation returns probation rows. openOnly=true returns just the entries
// still awaiting a decision (disposition='probation'), newest hold first;
// openOnly=false returns the full history. Each row's Expired flag is computed
// against now.
func (r *Registry) ListProbation(ctx context.Context, openOnly bool) ([]Probation, error) {
	var ps []Probation
	var err error
	if openOnly {
		err = r.reader().SelectContext(ctx, &ps,
			`SELECT * FROM probation WHERE disposition = $1 ORDER BY held_at DESC`, ProbationOpen)
	} else {
		err = r.reader().SelectContext(ctx, &ps, `SELECT * FROM probation ORDER BY held_at DESC`)
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	for i := range ps {
		ps[i].Expired = ps[i].Disposition == ProbationOpen && ps[i].CureBy > 0 && ps[i].CureBy < now
	}
	return ps, nil
}

// GetProbation returns one entry by id, or ErrNotFound.
func (r *Registry) GetProbation(ctx context.Context, id string) (*Probation, error) {
	var p Probation
	err := r.reader().GetContext(ctx, &p, `SELECT * FROM probation WHERE id = $1`, id)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	p.Expired = p.Disposition == ProbationOpen && p.CureBy > 0 && p.CureBy < now
	return &p, nil
}

// DecideProbation records the terminal disposition (repaired | purged) for an
// OPEN entry, stamping the decider + timestamp + note. Guards on the current
// disposition so a decided entry can't be re-decided. The actual repair/purge
// happens through the existing onboard / revoke paths — this only records the
// outcome (ADR 1068: probation adds hold+verify+deadline, no enforcement).
// Returns ErrNotFound when id is unknown or already decided.
func (r *Registry) DecideProbation(ctx context.Context, id, disposition, decidedBy, note string) error {
	if disposition != ProbationRepaired && disposition != ProbationPurged {
		return fmt.Errorf("invalid probation disposition %q (want repaired|purged)", disposition)
	}
	if strings.TrimSpace(decidedBy) == "" {
		return fmt.Errorf("probation decided_by (operator) is required")
	}
	now := time.Now().Unix()
	res, err := r.db.ExecContext(ctx,
		`UPDATE probation SET disposition = $1, decided_at = $2, decided_by = $3, note = $4
		 WHERE id = $5 AND disposition = $6`,
		disposition, now, decidedBy, note, id, ProbationOpen)
	if err != nil {
		return fmt.Errorf("decide probation: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
