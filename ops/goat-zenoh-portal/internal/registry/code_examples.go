package registry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TopicCodeExample is one vendor-provided code snippet attached to a
// topic for a specific language. Block 85 PR-D follow-on; when a row
// exists for a (topic_id, language) pair, the schema viewer renders it
// with a "Vendor-provided" badge and suppresses the autogen
// "notional, not validated" disclaimer for that language.
type TopicCodeExample struct {
	ID        int64  `db:"id"          json:"id"`
	TopicID   int64  `db:"topic_id"    json:"topic_id"`
	Language  string `db:"language"    json:"language"`
	Code      string `db:"code"        json:"code"`
	UpdatedAt int64  `db:"updated_at"  json:"updated_at"`
	UpdatedBy string `db:"updated_by"  json:"updated_by"`
}

// MaxCodeExampleBytes is the per-row size cap, enforced at the API
// layer. Vendor snippets are documentation-grade — small. A few
// kilobytes is comfortably above any realistic subscribe-and-decode
// example; 64 KiB is the hard ceiling.
const MaxCodeExampleBytes = 64 * 1024

// allowedExampleLanguages is the canonical set the schema viewer
// renders. Mirrors decoder.exampleTemplates plus a few additional
// languages a vendor might choose to provide. The portal stores
// whatever the vendor uploads; this set is a sanity guard against
// typos ("ts" vs "typescript") and prevents arbitrary new languages
// from sneaking into the registry without a viewer-side surface.
var allowedExampleLanguages = map[string]bool{
	"rust":       true,
	"go":         true,
	"python":     true,
	"typescript": true,
	"javascript": true,
	"c":          true,
	"cpp":        true,
	"java":       true,
	"shell":      true,
}

// ValidExampleLanguage returns nil iff lang is in the recognized set.
// The set is intentionally narrow; new entries land via PR + test.
func ValidExampleLanguage(lang string) error {
	if !allowedExampleLanguages[strings.ToLower(lang)] {
		return fmt.Errorf("language %q: not in the supported set (rust, go, python, typescript, javascript, c, cpp, java, shell)", lang)
	}
	return nil
}

// UpsertCodeExample inserts a new row or replaces the existing one for
// the (topic_id, language) pair. UPSERT semantics let a vendor "save"
// repeatedly from the editor without the API caller having to
// distinguish create vs update.
func (r *Registry) UpsertCodeExample(ctx context.Context, e *TopicCodeExample) (int64, error) {
	if err := ValidExampleLanguage(e.Language); err != nil {
		return 0, err
	}
	if e.Code == "" {
		return 0, errors.New("UpsertCodeExample: code body is empty")
	}
	if len(e.Code) > MaxCodeExampleBytes {
		return 0, fmt.Errorf("UpsertCodeExample: code exceeds %d bytes", MaxCodeExampleBytes)
	}
	e.UpdatedAt = time.Now().Unix()
	e.Language = strings.ToLower(e.Language)
	var id int64
	err := r.db.GetContext(ctx, &id,
		`INSERT INTO topic_code_examples (topic_id, language, code, updated_at, updated_by)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT(topic_id, language) DO UPDATE SET
		   code        = excluded.code,
		   updated_at  = excluded.updated_at,
		   updated_by  = excluded.updated_by
		 RETURNING id`,
		e.TopicID, e.Language, e.Code, e.UpdatedAt, e.UpdatedBy)
	if err != nil {
		return 0, fmt.Errorf("upsert code example: %w", err)
	}
	if id != 0 {
		e.ID = id
	}
	return id, nil
}

// GetCodeExample returns the row for (topic_id, language) or
// ErrNotFound.
func (r *Registry) GetCodeExample(ctx context.Context, topicID int64, language string) (*TopicCodeExample, error) {
	var e TopicCodeExample
	err := r.reader().GetContext(ctx, &e,
		`SELECT * FROM topic_code_examples WHERE topic_id = $1 AND language = $2`,
		topicID, strings.ToLower(language))
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListCodeExamplesByTopic returns every vendor-uploaded example for a
// topic — used by the schema-render endpoint to overlay autogen with
// vendor rows.
func (r *Registry) ListCodeExamplesByTopic(ctx context.Context, topicID int64) ([]TopicCodeExample, error) {
	var es []TopicCodeExample
	err := r.reader().SelectContext(ctx, &es,
		`SELECT * FROM topic_code_examples WHERE topic_id = $1 ORDER BY language ASC`,
		topicID)
	return es, err
}

// DeleteCodeExample removes the row for (topic_id, language). Returns
// ErrNotFound when nothing matches so the caller can surface a 404.
func (r *Registry) DeleteCodeExample(ctx context.Context, topicID int64, language string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM topic_code_examples WHERE topic_id = $1 AND language = $2`,
		topicID, strings.ToLower(language))
	if err != nil {
		return fmt.Errorf("delete code example: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
