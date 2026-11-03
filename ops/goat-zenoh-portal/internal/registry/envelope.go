// Per-actor throughput envelopes (ADR 1045 /
// docs/design/qos-throughput-envelopes.md).
//
// An envelope is an actor's *expected* traffic, declared as four ascending
// levels — min ≤ mean ≤ high ≤ max — on two axes (msgs/s and bytes/s). It is
// the per-actor expectation that fabric-health footprints are judged against:
// `goat top` joins a live footprint to its envelope and shows the producer as
// LOW / normal / HIGH / OVER its declared band.
//
// It is DECLARED governance/display metadata, never authorization — it does
// not touch the wire, the ACL, the cert, or the namespace (mirrors the
// slot-alias layer, ADR 1039). Declaring an envelope is a statement of intent,
// not permission to send that much; enforcement is a separate, later step.
//
// Keyed by the actor's write anchor (= the vendor prefix in per-vendor mode,
// = the slot_id in per-uuid-slot-pool mode — the same string the router ACL
// and fabric-health footprints key on), so it composes with both identity
// models with one table.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Envelope is one actor's declared throughput expectation. See package doc.
type Envelope struct {
	Anchor string `db:"anchor" json:"anchor"`

	// msgs/s axis.
	MinMsgsPerS  float64 `db:"min_msgs_per_s"  json:"min_msgs_per_s"`
	MeanMsgsPerS float64 `db:"mean_msgs_per_s" json:"mean_msgs_per_s"`
	HighMsgsPerS float64 `db:"high_msgs_per_s" json:"high_msgs_per_s"`
	MaxMsgsPerS  float64 `db:"max_msgs_per_s"  json:"max_msgs_per_s"`

	// bytes/s axis.
	MinBytesPerS  float64 `db:"min_bytes_per_s"  json:"min_bytes_per_s"`
	MeanBytesPerS float64 `db:"mean_bytes_per_s" json:"mean_bytes_per_s"`
	HighBytesPerS float64 `db:"high_bytes_per_s" json:"high_bytes_per_s"`
	MaxBytesPerS  float64 `db:"max_bytes_per_s"  json:"max_bytes_per_s"`

	// Source is "default" (the rough class-baseline placeholder assigned at
	// registration — ADR 1045 §3, "a placeholder to be corrected, not a
	// model") or "edited" (the registering party set the numbers). ClassKey
	// records which per-class baseline a default came from (provenance only).
	Source   string `db:"source"    json:"source"`
	ClassKey string `db:"class_key" json:"class_key"`

	CreatedAt int64 `db:"created_at" json:"created_at"`
	UpdatedAt int64 `db:"updated_at" json:"updated_at"`
}

// Validate enforces the band invariant on both axes: every level is
// non-negative and the four levels ascend min ≤ mean ≤ high ≤ max. An actor
// that is legitimately event-driven/bursty declares min = 0 (so correct
// silence never trips the LOW band downstream); min = 0 is valid.
func (e *Envelope) Validate() error {
	axes := []struct {
		name                 string
		min, mean, high, max float64
	}{
		{"msgs/s", e.MinMsgsPerS, e.MeanMsgsPerS, e.HighMsgsPerS, e.MaxMsgsPerS},
		{"bytes/s", e.MinBytesPerS, e.MeanBytesPerS, e.HighBytesPerS, e.MaxBytesPerS},
	}
	for _, a := range axes {
		if a.min < 0 || a.mean < 0 || a.high < 0 || a.max < 0 {
			return fmt.Errorf("envelope %s: levels must be non-negative", a.name)
		}
		if !(a.min <= a.mean && a.mean <= a.high && a.high <= a.max) {
			return fmt.Errorf("envelope %s: levels must ascend min ≤ mean ≤ high ≤ max (got %g/%g/%g/%g)",
				a.name, a.min, a.mean, a.high, a.max)
		}
	}
	return nil
}

// Baseline is the eight-number rough default for one QoS class.
type Baseline struct {
	MinMsgsPerS  float64 `json:"min_msgs_per_s"`
	MeanMsgsPerS float64 `json:"mean_msgs_per_s"`
	HighMsgsPerS float64 `json:"high_msgs_per_s"`
	MaxMsgsPerS  float64 `json:"max_msgs_per_s"`

	MinBytesPerS  float64 `json:"min_bytes_per_s"`
	MeanBytesPerS float64 `json:"mean_bytes_per_s"`
	HighBytesPerS float64 `json:"high_bytes_per_s"`
	MaxBytesPerS  float64 `json:"max_bytes_per_s"`
}

// BaselineSet maps a QoS class (C0..C3) to its rough default envelope.
type BaselineSet map[string]Baseline

// DefaultBaselineClass is the class used when no class is known for an actor at
// registration (the portal doesn't observe class at onboard time — ADR 1045
// §3). C2 (situational telemetry) is the common default. Overridable via the
// portal's --envelope-default-class / PORTAL_ENVELOPE_DEFAULT_CLASS.
const DefaultBaselineClass = "C2"

// DefaultBaselines is the built-in rough placeholder table (ADR 1045 §3). The
// numbers are DELIBERATELY APPROXIMATE — they exist so an envelope always has a
// value, and are placeholders the registering party is expected to correct, NOT
// a model. They scale a per-class shape (C2 situational lower than C3 bulk;
// C3 declares min = 0 because bulk is legitimately bursty/idle). Tunable
// without a rebuild via PORTAL_ENVELOPE_BASELINES (a JSON object of the same
// shape); see ParseBaselines.
func DefaultBaselines() BaselineSet {
	return BaselineSet{
		// class:  msgs/s {min mean high max}            bytes/s {min mean high max}
		"C0": {1, 2, 5, 20, 256, 512, 2_048, 8_192},
		"C1": {2, 10, 50, 200, 1_024, 8_192, 65_536, 262_144},
		"C2": {1, 5, 30, 120, 512, 4_096, 32_768, 131_072},
		"C3": {0, 20, 200, 1_000, 4_096, 262_144, 4_194_304, 16_777_216},
	}
}

// For returns the baseline for a class, falling back to the C2 baseline (then
// any) when the class is unknown — the table is never empty in practice.
func (bs BaselineSet) For(class string) Baseline {
	if b, ok := bs[strings.ToUpper(strings.TrimSpace(class))]; ok {
		return b
	}
	if b, ok := bs[DefaultBaselineClass]; ok {
		return b
	}
	for _, b := range bs {
		return b
	}
	return Baseline{}
}

// ParseBaselines parses the PORTAL_ENVELOPE_BASELINES JSON override into a
// BaselineSet, falling back to DefaultBaselines for empty/blank input. A
// non-empty-but-malformed value is a configuration error (fail fast at
// startup rather than silently mis-defaulting every actor).
func ParseBaselines(raw string) (BaselineSet, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultBaselines(), nil
	}
	var bs BaselineSet
	if err := json.Unmarshal([]byte(raw), &bs); err != nil {
		return nil, fmt.Errorf("parse envelope baselines JSON: %w", err)
	}
	if len(bs) == 0 {
		return DefaultBaselines(), nil
	}
	return bs, nil
}

// EnvelopeFromBaseline materializes a default ("source=default") envelope for
// an anchor from a class baseline.
func EnvelopeFromBaseline(anchor, class string, b Baseline, now int64) *Envelope {
	return &Envelope{
		Anchor:        anchor,
		MinMsgsPerS:   b.MinMsgsPerS,
		MeanMsgsPerS:  b.MeanMsgsPerS,
		HighMsgsPerS:  b.HighMsgsPerS,
		MaxMsgsPerS:   b.MaxMsgsPerS,
		MinBytesPerS:  b.MinBytesPerS,
		MeanBytesPerS: b.MeanBytesPerS,
		HighBytesPerS: b.HighBytesPerS,
		MaxBytesPerS:  b.MaxBytesPerS,
		Source:        "default",
		ClassKey:      strings.ToUpper(strings.TrimSpace(class)),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

const envelopeColumns = `anchor,
	min_msgs_per_s, mean_msgs_per_s, high_msgs_per_s, max_msgs_per_s,
	min_bytes_per_s, mean_bytes_per_s, high_bytes_per_s, max_bytes_per_s,
	source, class_key, created_at, updated_at`

// EnsureDefaultEnvelope assigns the rough class-baseline default for an anchor
// IFF no envelope exists for it yet (ON CONFLICT DO NOTHING) — an already-declared
// (possibly edited) envelope is never clobbered. Best-effort by contract: a
// failure here must NOT gate registration (ADR 1045 §3), so callers ignore the
// returned error after logging.
func (r *Registry) EnsureDefaultEnvelope(ctx context.Context, anchor, class string, bs BaselineSet) error {
	now := time.Now().Unix()
	e := EnvelopeFromBaseline(anchor, class, bs.For(class), now)
	_, err := r.db.NamedExecContext(ctx,
		`INSERT INTO envelopes (`+envelopeColumns+`)
		 VALUES (:anchor,
		 :min_msgs_per_s, :mean_msgs_per_s, :high_msgs_per_s, :max_msgs_per_s,
		 :min_bytes_per_s, :mean_bytes_per_s, :high_bytes_per_s, :max_bytes_per_s,
		 :source, :class_key, :created_at, :updated_at)
		 ON CONFLICT (anchor) DO NOTHING`,
		e)
	if err != nil {
		return fmt.Errorf("ensure default envelope %q: %w", anchor, err)
	}
	return nil
}

// UpsertEnvelope writes an edited envelope (source forced to "edited"),
// inserting or replacing the numbers while preserving created_at on update.
// Validates the band invariant first.
func (r *Registry) UpsertEnvelope(ctx context.Context, e *Envelope) error {
	if err := e.Validate(); err != nil {
		return err
	}
	now := time.Now().Unix()
	e.Source = "edited"
	e.UpdatedAt = now
	if e.CreatedAt == 0 {
		e.CreatedAt = now
	}
	_, err := r.db.NamedExecContext(ctx,
		`INSERT INTO envelopes (`+envelopeColumns+`)
		 VALUES (:anchor,
		 :min_msgs_per_s, :mean_msgs_per_s, :high_msgs_per_s, :max_msgs_per_s,
		 :min_bytes_per_s, :mean_bytes_per_s, :high_bytes_per_s, :max_bytes_per_s,
		 :source, :class_key, :created_at, :updated_at)
		 ON CONFLICT(anchor) DO UPDATE SET
		   min_msgs_per_s   = excluded.min_msgs_per_s,
		   mean_msgs_per_s  = excluded.mean_msgs_per_s,
		   high_msgs_per_s  = excluded.high_msgs_per_s,
		   max_msgs_per_s   = excluded.max_msgs_per_s,
		   min_bytes_per_s  = excluded.min_bytes_per_s,
		   mean_bytes_per_s = excluded.mean_bytes_per_s,
		   high_bytes_per_s = excluded.high_bytes_per_s,
		   max_bytes_per_s  = excluded.max_bytes_per_s,
		   source           = excluded.source,
		   class_key        = excluded.class_key,
		   updated_at       = excluded.updated_at`,
		e)
	if err != nil {
		return fmt.Errorf("upsert envelope %q: %w", e.Anchor, err)
	}
	return nil
}

// GetEnvelope returns the envelope for an anchor, or ErrNotFound.
func (r *Registry) GetEnvelope(ctx context.Context, anchor string) (*Envelope, error) {
	var e Envelope
	err := r.reader().GetContext(ctx, &e, `SELECT * FROM envelopes WHERE anchor = $1`, anchor)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListEnvelopes returns every declared envelope, ordered by anchor. The
// display layer (`goat top`) caches this and degrades to "no envelope" (an
// honest `—`, never a faked verdict) when the portal is unreachable.
func (r *Registry) ListEnvelopes(ctx context.Context) ([]Envelope, error) {
	out := []Envelope{}
	if err := r.reader().SelectContext(ctx, &out, `SELECT * FROM envelopes ORDER BY anchor`); err != nil {
		return nil, fmt.Errorf("list envelopes: %w", err)
	}
	return out, nil
}
