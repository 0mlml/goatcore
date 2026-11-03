package registry

// Topic-budget grant store + slot lifecycle classes — the registry home the
// group_vars interim records fold into (ADR 1075 follow-up named in
// zenoh-cardinality-budget-and-lazy-crawler.md §2; ADR 1104 for lifecycle).
//
// A topic-budget row is a per-owner allowance: the agreed cumulative
// distinct-stored-key ceiling under the owner's namespace, plus (layer 3,
// optional) the named channel prefixes the router ACL subdivides the owner's
// write grant into at the next render+restart. Owners are slot
// anchors OR infra store names (ops, attest_schema, ...) — the crawler's
// owner grain, so the column is free-text, not an FK.
//
// The crawler consumes these rows via the /api/budget-projection endpoint
// (the same JSON contract it previously read from the ansible-rendered
// projection file); every onboarded (assigned) slot additionally holds the
// automatic self-service block with no row needed.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// LifecycleClasses is the closed enum for slots.lifecycle_class (ADR 1104).
// Empty = unclassified, which every consumer treats as real (fail-safe:
// deletes nothing, notifies normally).
var LifecycleClasses = map[string]bool{"test": true, "notional": true, "real": true, "": true}

// TopicBudget is one per-owner allowance row.
type TopicBudget struct {
	Owner     string `db:"owner"      json:"owner"`
	Granted   int64  `db:"granted"    json:"granted"`
	Prefixes  string `db:"prefixes"   json:"-"` // JSON array, may be ""
	Note      string `db:"note"       json:"note"`
	UpdatedAt int64  `db:"updated_at" json:"updated_at"`
	UpdatedBy string `db:"updated_by" json:"updated_by"`
}

// PrefixList decodes the stored prefixes JSON ("" = no subdivision).
func (b *TopicBudget) PrefixList() []string {
	if b.Prefixes == "" {
		return nil
	}
	var ps []string
	if err := json.Unmarshal([]byte(b.Prefixes), &ps); err != nil {
		return nil
	}
	return ps
}

// UpsertTopicBudget records or replaces an owner's allowance. granted must be
// positive (a zero allowance is not a thing this surface expresses — remove
// the row instead; no row = the automatic block for onboarded slots, or
// unmeasured for anything else). prefixes may be nil (no subdivision).
func (r *Registry) UpsertTopicBudget(ctx context.Context, owner string, granted int64, prefixes []string, note, updatedBy string) error {
	if owner == "" {
		return errors.New("owner required")
	}
	if granted <= 0 {
		return fmt.Errorf("granted must be positive, got %d", granted)
	}
	pjson := ""
	if len(prefixes) > 0 {
		for _, p := range prefixes {
			if p == "" || p == "*" {
				return fmt.Errorf("prefix %q: must be a concrete channel name", p)
			}
		}
		raw, err := json.Marshal(prefixes)
		if err != nil {
			return err
		}
		pjson = string(raw)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO topic_budgets (owner, granted, prefixes, note, updated_at, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT(owner) DO UPDATE SET
		   granted = excluded.granted, prefixes = excluded.prefixes,
		   note = excluded.note, updated_at = excluded.updated_at,
		   updated_by = excluded.updated_by`,
		owner, granted, pjson, note, time.Now().Unix(), updatedBy)
	return err
}

// DeleteTopicBudget removes an owner's explicit allowance (it falls back to
// the automatic block if it is an onboarded slot, else unmeasured).
func (r *Registry) DeleteTopicBudget(ctx context.Context, owner string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM topic_budgets WHERE owner = $1`, owner)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTopicBudgets returns every allowance row, owner-ordered.
func (r *Registry) ListTopicBudgets(ctx context.Context) ([]TopicBudget, error) {
	var out []TopicBudget
	err := r.reader().SelectContext(ctx, &out, `SELECT * FROM topic_budgets ORDER BY owner`)
	return out, err
}

// AssignedSlotAnchors returns the anchors of every currently-assigned slot —
// the "onboarded slot" population that automatically holds the self-service
// block (allocation design §2 tier 1).
func (r *Registry) AssignedSlotAnchors(ctx context.Context) ([]string, error) {
	var out []string
	err := r.reader().SelectContext(ctx, &out,
		`SELECT slot_id FROM slots WHERE assigned_to IS NOT NULL ORDER BY slot_id`)
	return out, err
}

// SetSlotLifecycleClass stamps a slot's lifecycle class (ADR 1104:
// test | notional | real; "" clears back to unclassified-reads-as-real).
func (r *Registry) SetSlotLifecycleClass(ctx context.Context, slotID, class string) error {
	if !LifecycleClasses[class] {
		return fmt.Errorf("lifecycle_class %q: must be test|notional|real or empty", class)
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE slots SET lifecycle_class = $1 WHERE slot_id = $2`, class, slotID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SlotLifecycleClasses returns every slot carrying a non-empty lifecycle
// class, as anchor -> class (the ADR 1104 render input: absent = real).
func (r *Registry) SlotLifecycleClasses(ctx context.Context) (map[string]string, error) {
	rows, cancel, err := r.reader().QueryContext(ctx,
		`SELECT slot_id, lifecycle_class FROM slots
		  WHERE lifecycle_class IS NOT NULL AND lifecycle_class != ''`)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, class string
		if err := rows.Scan(&id, &class); err != nil {
			return nil, err
		}
		out[id] = class
	}
	return out, rows.Err()
}
