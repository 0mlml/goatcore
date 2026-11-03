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

// TopicSchema matches the topic_schemas table. Represents one uploaded
// schema body for a registered topic (Block 85, design memo at
// docs/design/portal-schema-publishing.md).
type TopicSchema struct {
	ID            int64  `db:"id"            json:"id"`
	TopicID       int64  `db:"topic_id"      json:"topic_id"`
	Version       string `db:"version"       json:"version"`
	Compatibility string `db:"compatibility" json:"compatibility"`
	Format        string `db:"format"        json:"format"`
	FilePath      string `db:"file_path"     json:"-"` // relative path; not exposed
	SizeBytes     int64  `db:"size_bytes"    json:"size_bytes"`
	SHA256        string `db:"sha256"        json:"sha256"`
	UploadedAt    int64  `db:"uploaded_at"   json:"uploaded_at"`
	UploadedBy    string `db:"uploaded_by"   json:"uploaded_by"`
}

// schemaVersionRe matches the allowed version label: `v` followed by one
// or more digits.
var schemaVersionRe = regexp.MustCompile(`^v\d+$`)

// allowedSchemaFormats enumerates the schema-document formats accepted at
// upload time. v1.1 adds protobuf-source on top of the original
// protobuf-fds; the handler compiles source-format uploads in-process via
// the internal/protocompile wrapper and persists the resulting bytes
// under the protobuf-fds on-disk layout, so the decoder + render paths
// downstream see no difference between the two upload formats. v1.2 adds
// xsd — an XML Schema Definition document, the schema half of the
// xml:<root-element> topic format; persisted verbatim (it is itself XML)
// and validated for well-formedness + declared-root-element on ingress.
var allowedSchemaFormats = map[string]bool{
	"protobuf-fds":    true,
	"protobuf-source": true,
	"xsd":             true,
}

// allowedCompatibilities is the vendor-declared compatibility enum.
var allowedCompatibilities = map[string]bool{
	"backward": true,
	"forward":  true,
	"none":     true,
}

// ValidSchemaVersion returns nil iff label matches ^v\d+$.
func ValidSchemaVersion(label string) error {
	if !schemaVersionRe.MatchString(label) {
		return fmt.Errorf("schema version %q: must be v<N> (e.g. v1, v2)", label)
	}
	return nil
}

// ValidSchemaFormat returns nil iff format is one of the accepted set
// (protobuf-fds | protobuf-source | xsd). protobuf-source is compiled to a
// FileDescriptorSet on ingress and persisted as protobuf-fds; xsd is
// persisted verbatim.
func ValidSchemaFormat(format string) error {
	if !allowedSchemaFormats[format] {
		return fmt.Errorf("schema format %q: must be one of protobuf-fds | protobuf-source | xsd", format)
	}
	return nil
}

// ValidCompatibility returns nil iff compatibility is one of the enum.
func ValidCompatibility(c string) error {
	if !allowedCompatibilities[c] {
		return fmt.Errorf("compatibility %q: must be one of backward/forward/none", c)
	}
	return nil
}

// InsertSchema writes a new schema row. Caller is responsible for first
// persisting the file bytes to disk and supplying FilePath + SHA256 +
// SizeBytes. Returns ErrSchemaExists if (topic_id, version) collides.
func (r *Registry) InsertSchema(ctx context.Context, s *TopicSchema) (int64, error) {
	if err := ValidSchemaVersion(s.Version); err != nil {
		return 0, err
	}
	if err := ValidSchemaFormat(s.Format); err != nil {
		return 0, err
	}
	if err := ValidCompatibility(s.Compatibility); err != nil {
		return 0, err
	}
	if s.SHA256 == "" || s.FilePath == "" {
		return 0, errors.New("InsertSchema: file_path + sha256 must be set by caller")
	}
	s.UploadedAt = time.Now().Unix()
	var id int64
	err := r.db.GetContext(ctx, &id,
		`INSERT INTO topic_schemas (topic_id, version, compatibility, format, file_path, size_bytes, sha256, uploaded_at, uploaded_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		s.TopicID, s.Version, s.Compatibility, s.Format, s.FilePath, s.SizeBytes, s.SHA256, s.UploadedAt, s.UploadedBy)
	if err != nil {
		if pgstore.IsUniqueViolation(err) {
			return 0, ErrSchemaExists
		}
		return 0, fmt.Errorf("insert schema: %w", err)
	}
	s.ID = id
	return id, nil
}

// ListSchemasByTopic returns every schema row for a topic, newest version
// first (lexically — fine for v1 / v2 / v3 / ..., not for >v9 without
// zero-pad, but for trial scope this is acceptable).
func (r *Registry) ListSchemasByTopic(ctx context.Context, topicID int64) ([]TopicSchema, error) {
	var ss []TopicSchema
	err := r.reader().SelectContext(ctx, &ss,
		// id DESC is the deterministic tiebreak when two uploads share the
		// same second-resolution uploaded_at (the autoincrement rowid is
		// monotonic with insertion order); without it "latest" is ambiguous.
		`SELECT * FROM topic_schemas WHERE topic_id = $1 ORDER BY uploaded_at DESC, id DESC`,
		topicID)
	return ss, err
}

// GetSchemaByTopicVersion returns the schema row for (topic_id, version)
// or ErrNotFound.
func (r *Registry) GetSchemaByTopicVersion(ctx context.Context, topicID int64, version string) (*TopicSchema, error) {
	var s TopicSchema
	err := r.reader().GetContext(ctx, &s,
		`SELECT * FROM topic_schemas WHERE topic_id = $1 AND version = $2`,
		topicID, version)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetLatestSchemaForTopic returns the most-recently-uploaded schema for a
// topic, or ErrNotFound when the topic has none.
func (r *Registry) GetLatestSchemaForTopic(ctx context.Context, topicID int64) (*TopicSchema, error) {
	var s TopicSchema
	err := r.reader().GetContext(ctx, &s,
		// id DESC tiebreaks same-second uploads deterministically (see
		// ListSchemasByTopic) so "latest" is the truly newest row.
		`SELECT * FROM topic_schemas WHERE topic_id = $1 ORDER BY uploaded_at DESC, id DESC LIMIT 1`,
		topicID)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetTopicByVendorKey looks up a topic by (vendor_prefix, key_pattern) —
// convenience for the catch-all `/api/schemas/<key...>` fetch endpoint
// which receives a key-pattern, not an id.
//
// Keyexpr intersection (the wildcard-registration companion): when no topic
// row matches the key exactly, a registered pattern carrying `*` segments
// (validateTopic permits `*` only as a complete non-first segment) that
// intersects the concrete key resolves instead — so a per-entity key like
// adsb/aircraft/151e22/v1 finds the schema registered once as
// adsb/aircraft/*/v1. Retired patterns never match. When several patterns
// intersect, the most specific (fewest wildcard segments) wins; ties break
// on the oldest row (lowest id) for determinism.
func (r *Registry) GetTopicByVendorKey(ctx context.Context, vendor, key string) (*Topic, error) {
	var t Topic
	err := r.reader().GetContext(ctx, &t,
		`SELECT * FROM topics WHERE vendor_prefix = $1 AND key_pattern = $2`,
		vendor, key)
	if err == nil {
		return &t, nil
	}
	if !errors.Is(err, sqlErrNoRows()) {
		return nil, err
	}
	// Exact miss — try wildcard-pattern intersection. Only unretired rows
	// that actually carry a wildcard segment are candidates.
	var candidates []Topic
	if err := r.reader().SelectContext(ctx, &candidates,
		`SELECT * FROM topics
		  WHERE vendor_prefix = $1 AND retired_at IS NULL
		    AND key_pattern LIKE '%*%'
		  ORDER BY id`,
		vendor); err != nil {
		return nil, err
	}
	var best *Topic
	bestWild := -1
	for i := range candidates {
		wild, ok := patternIntersects(candidates[i].KeyPattern, key)
		if !ok {
			continue
		}
		if best == nil || wild < bestWild {
			best = &candidates[i]
			bestWild = wild
		}
	}
	if best == nil {
		return nil, ErrNotFound
	}
	return best, nil
}

// patternIntersects reports whether a registered key pattern (concrete
// segments plus `*` complete-segment wildcards) covers a concrete key, and
// how many wildcard segments it used — the specificity metric for choosing
// among several intersecting patterns. Segment counts must match exactly:
// `*` binds one segment, never zero or many (`**` is not registrable).
func patternIntersects(pattern, key string) (wildcards int, ok bool) {
	ps := strings.Split(pattern, "/")
	ks := strings.Split(key, "/")
	if len(ps) != len(ks) {
		return 0, false
	}
	for i := range ps {
		if ps[i] == "*" {
			if ks[i] == "" {
				return 0, false
			}
			wildcards++
			continue
		}
		if ps[i] != ks[i] {
			return 0, false
		}
	}
	return wildcards, true
}
