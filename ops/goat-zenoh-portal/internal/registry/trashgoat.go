package registry

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgstore"
)

// Trashgoat lifecycle states. Stored as TEXT in the state column;
// kept narrow + opinionated so the runner doesn't need to negotiate.
const (
	TrashgoatStateDraft   = "draft"
	TrashgoatStateActive  = "active"
	TrashgoatStatePaused  = "paused"
	TrashgoatStateRetired = "retired"
)

// TrashgoatSpec matches the trashgoat_specs table. JSON tags drive the
// API surface. payload_spec_json + cadence_spec_json carry the JSON-
// encoded generator + cadence configs verbatim; the API layer shape-
// validates them on write, but the portal does NOT interpret them at
// runtime (that is the runner's job).
type TrashgoatSpec struct {
	ID              int64  `db:"id"                  json:"id"`
	Slug            string `db:"slug"                json:"slug"`
	Description     string `db:"description"         json:"description"`
	TopicPattern    string `db:"topic_pattern"       json:"topic_pattern"`
	PayloadSpecJSON string `db:"payload_spec_json"   json:"payload_spec"`
	CadenceSpecJSON string `db:"cadence_spec_json"   json:"cadence_spec"`
	State           string `db:"state"               json:"state"`
	Notes           string `db:"notes"               json:"notes"`
	CreatedAt       int64  `db:"created_at"          json:"created_at"`
	CreatedBy       string `db:"created_by"          json:"created_by"`
	LastActivatedAt *int64 `db:"last_activated_at"   json:"last_activated_at,omitempty"`
	LastPausedAt    *int64 `db:"last_paused_at"      json:"last_paused_at,omitempty"`
	LastCleansedAt  *int64 `db:"last_cleansed_at"    json:"last_cleansed_at,omitempty"`
	RetiredAt       *int64 `db:"retired_at"          json:"retired_at,omitempty"`
}

// trashgoatSlugRe matches the allowed slug syntax: lowercase ASCII +
// digits + '-', 1-48 chars, no leading or trailing '-'. Slug shows up
// as the second segment of the published key (trashgoat/<slug>/...);
// keep it identifier-shaped so it round-trips into key expressions.
var trashgoatSlugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,46}[a-z0-9])?$`)

// ValidTrashgoatSlug returns nil when the slug is acceptable.
func ValidTrashgoatSlug(s string) error {
	if !trashgoatSlugRe.MatchString(s) {
		return fmt.Errorf("slug %q: must be 1-48 chars [a-z0-9-], no leading/trailing '-'", s)
	}
	return nil
}

// validateTrashgoatTopicPattern enforces the trashgoat/<slug>/... shape
// at the registry layer in addition to the API-layer caller. Templates
// are allowed in interior segments (e.g. trashgoat/burst/v1/{uuid}); the
// runner resolves them at publish time.
func validateTrashgoatTopicPattern(slug, topicPattern string) error {
	if topicPattern == "" {
		return errors.New("topic_pattern must not be empty")
	}
	if len(topicPattern) > 512 {
		return errors.New("topic_pattern too long (>512 chars)")
	}
	parts := strings.Split(topicPattern, "/")
	if len(parts) < 3 {
		return fmt.Errorf("topic_pattern %q: must be at least trashgoat/<slug>/<...>", topicPattern)
	}
	if parts[0] != "trashgoat" {
		return fmt.Errorf("topic_pattern %q: first segment must be 'trashgoat', got %q", topicPattern, parts[0])
	}
	if parts[1] != slug {
		return fmt.Errorf("topic_pattern %q: second segment must equal slug %q, got %q", topicPattern, slug, parts[1])
	}
	for _, p := range parts {
		if p == "" {
			return fmt.Errorf("topic_pattern %q: empty segment", topicPattern)
		}
	}
	return nil
}

// CreateTrashgoatSpec inserts a new spec row in draft state. Returns
// ErrSpecExists when the slug collides. The caller is expected to have
// already shape-validated payload_spec + cadence_spec at the API
// boundary; registry stores both as TEXT verbatim.
func (r *Registry) CreateTrashgoatSpec(ctx context.Context, spec *TrashgoatSpec) (int64, error) {
	if err := ValidTrashgoatSlug(spec.Slug); err != nil {
		return 0, err
	}
	if err := validateTrashgoatTopicPattern(spec.Slug, spec.TopicPattern); err != nil {
		return 0, err
	}
	if spec.PayloadSpecJSON == "" {
		return 0, errors.New("payload_spec must not be empty")
	}
	if spec.CadenceSpecJSON == "" {
		return 0, errors.New("cadence_spec must not be empty")
	}
	spec.State = TrashgoatStateDraft
	spec.CreatedAt = time.Now().Unix()
	if spec.CreatedBy == "" {
		spec.CreatedBy = "operator"
	}
	var id int64
	err := r.db.GetContext(ctx, &id, `
		INSERT INTO trashgoat_specs (
			slug, description, topic_pattern, payload_spec_json,
			cadence_spec_json, state, notes, created_at, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id
	`,
		spec.Slug, spec.Description, spec.TopicPattern,
		spec.PayloadSpecJSON, spec.CadenceSpecJSON,
		spec.State, spec.Notes, spec.CreatedAt, spec.CreatedBy)
	if err != nil {
		if pgstore.IsUniqueViolation(err) {
			return 0, ErrSpecExists
		}
		return 0, fmt.Errorf("insert trashgoat spec: %w", err)
	}
	spec.ID = id
	return id, nil
}

// GetTrashgoatSpec returns a spec by id, or ErrNotFound.
func (r *Registry) GetTrashgoatSpec(ctx context.Context, id int64) (*TrashgoatSpec, error) {
	var s TrashgoatSpec
	err := r.reader().GetContext(ctx, &s, `SELECT * FROM trashgoat_specs WHERE id = $1`, id)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetTrashgoatSpecBySlug returns a spec by slug, or ErrNotFound.
func (r *Registry) GetTrashgoatSpecBySlug(ctx context.Context, slug string) (*TrashgoatSpec, error) {
	var s TrashgoatSpec
	err := r.reader().GetContext(ctx, &s, `SELECT * FROM trashgoat_specs WHERE slug = $1`, slug)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListTrashgoatSpecs returns every spec, newest-first by created_at.
func (r *Registry) ListTrashgoatSpecs(ctx context.Context) ([]TrashgoatSpec, error) {
	var specs []TrashgoatSpec
	err := r.reader().SelectContext(ctx, &specs,
		`SELECT * FROM trashgoat_specs ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	return specs, nil
}

// UpdateTrashgoatSpec patches description / topic_pattern /
// payload_spec_json / cadence_spec_json / notes. Allowed only on draft
// specs — once a spec has been activated, the spec definition is
// immutable (the operator must retire + re-author for a different
// shape). Returns ErrSpecImmutable when the state is not draft.
//
// notes is the one field that may be mutated regardless of state; we
// fold it into the same call to keep one round-trip but the API layer
// is welcome to expose a notes-only PATCH on top.
func (r *Registry) UpdateTrashgoatSpec(ctx context.Context, spec *TrashgoatSpec) error {
	cur, err := r.GetTrashgoatSpec(ctx, spec.ID)
	if err != nil {
		return err
	}
	if cur.State != TrashgoatStateDraft {
		return ErrSpecImmutable
	}
	if err := validateTrashgoatTopicPattern(cur.Slug, spec.TopicPattern); err != nil {
		return err
	}
	if spec.PayloadSpecJSON == "" {
		return errors.New("payload_spec must not be empty")
	}
	if spec.CadenceSpecJSON == "" {
		return errors.New("cadence_spec must not be empty")
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE trashgoat_specs SET
			description       = $1,
			topic_pattern     = $2,
			payload_spec_json = $3,
			cadence_spec_json = $4,
			notes             = $5
		WHERE id = $6
	`, spec.Description, spec.TopicPattern, spec.PayloadSpecJSON, spec.CadenceSpecJSON, spec.Notes, spec.ID)
	return err
}

// DeleteTrashgoatSpec removes a retired spec. Returns ErrSpecNotRetired
// when the spec is in any other state; the audit trail is preserved
// for retired specs until the operator explicitly prunes them.
func (r *Registry) DeleteTrashgoatSpec(ctx context.Context, id int64) error {
	cur, err := r.GetTrashgoatSpec(ctx, id)
	if err != nil {
		return err
	}
	if cur.State != TrashgoatStateRetired {
		return ErrSpecNotRetired
	}
	_, err = r.db.ExecContext(ctx, `DELETE FROM trashgoat_specs WHERE id = $1`, id)
	return err
}

// Lifecycle helpers. Each enforces the valid transition graph and
// stamps the appropriate lifecycle timestamp. The state graph is:
//
//	draft   → active
//	active  → paused
//	paused  → active   (resume)
//	active  → retired
//	paused  → retired
//
// retired is terminal. Any other requested transition returns
// ErrInvalidTransition.

// ActivateTrashgoatSpec moves draft|paused → active and stamps
// last_activated_at.
func (r *Registry) ActivateTrashgoatSpec(ctx context.Context, id int64) error {
	cur, err := r.GetTrashgoatSpec(ctx, id)
	if err != nil {
		return err
	}
	if cur.State != TrashgoatStateDraft && cur.State != TrashgoatStatePaused {
		return fmt.Errorf("%w: activate from %s not allowed", ErrInvalidTransition, cur.State)
	}
	now := time.Now().Unix()
	_, err = r.db.ExecContext(ctx,
		`UPDATE trashgoat_specs SET state = $1, last_activated_at = $2 WHERE id = $3`,
		TrashgoatStateActive, now, id)
	return err
}

// PauseTrashgoatSpec moves active → paused and stamps last_paused_at.
func (r *Registry) PauseTrashgoatSpec(ctx context.Context, id int64) error {
	cur, err := r.GetTrashgoatSpec(ctx, id)
	if err != nil {
		return err
	}
	if cur.State != TrashgoatStateActive {
		return fmt.Errorf("%w: pause from %s not allowed", ErrInvalidTransition, cur.State)
	}
	now := time.Now().Unix()
	_, err = r.db.ExecContext(ctx,
		`UPDATE trashgoat_specs SET state = $1, last_paused_at = $2 WHERE id = $3`,
		TrashgoatStatePaused, now, id)
	return err
}

// RetireTrashgoatSpec moves draft|active|paused → retired and stamps
// retired_at. Already-retired is a no-op (idempotent).
func (r *Registry) RetireTrashgoatSpec(ctx context.Context, id int64) error {
	cur, err := r.GetTrashgoatSpec(ctx, id)
	if err != nil {
		return err
	}
	if cur.State == TrashgoatStateRetired {
		return nil
	}
	if cur.State != TrashgoatStateDraft && cur.State != TrashgoatStateActive && cur.State != TrashgoatStatePaused {
		return fmt.Errorf("%w: retire from %s not allowed", ErrInvalidTransition, cur.State)
	}
	now := time.Now().Unix()
	_, err = r.db.ExecContext(ctx,
		`UPDATE trashgoat_specs SET state = $1, retired_at = $2 WHERE id = $3`,
		TrashgoatStateRetired, now, id)
	return err
}

// RecordTrashgoatCleanse stamps last_cleansed_at WITHOUT changing state.
// Cleanse is orthogonal to the lifecycle — issues z_delete against every
// key the spec has published; the spec keeps running if it was active.
// Allowed from any state except draft (nothing to cleanse if it never
// ran).
func (r *Registry) RecordTrashgoatCleanse(ctx context.Context, id int64) error {
	cur, err := r.GetTrashgoatSpec(ctx, id)
	if err != nil {
		return err
	}
	if cur.State == TrashgoatStateDraft {
		return fmt.Errorf("%w: cleanse from draft not allowed (nothing published)", ErrInvalidTransition)
	}
	now := time.Now().Unix()
	_, err = r.db.ExecContext(ctx,
		`UPDATE trashgoat_specs SET last_cleansed_at = $1 WHERE id = $2`, now, id)
	return err
}
