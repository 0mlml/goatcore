package registry

// multipub_topics.go — the ADR 1105 multi-publisher topic store. The
// one-writer-per-<uuid>/**-root rule (ADR 1025) is inviolable; a multi-publisher topic
// is the ONLY place more than one writer is allowed, and it is a deliberately-
// created, marked, discoverable, org-scoped object in a distinct namespace:
//
//	multipub/<pool_handle>/<topic_id>/**
//
// The reserved `multipub/` segment announces "do not assume one writer here" and
// is never a single-owner root. `<pool_handle>` is a STABLE OPAQUE per-org
// handle (born-under, ADR 1051) so the org NAME is never frozen into a key (the
// mutable-ownership-in-a-freeze-critical-key anti-pattern, data-access-roles.md
// §1 / ADR 1043); the handle resolves to the org, the org to a display name via
// the alias layer (ADR 1039). The authorized publisher set lives in write grant
// rows (grants.action='write', producer = this key-base), not here.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// MultipubTopicPrefix is the reserved top-level key-expr class for
// multi-publisher topics. A co-publish grant target MUST live under it; a
// single-owner `<uuid>/**` root never does.
const MultipubTopicPrefix = "multipub"

// opaqueSeg matches one opaque key segment (a pool handle or topic id): lowercase
// hex/alnum, no separators — deliberately not a human name (names are aliases).
var opaqueSeg = regexp.MustCompile(`^[a-z0-9]{1,64}$`)

// MultipubTopicStatus values (closed set; deprecate-never-delete).
const (
	MultipubTopicActive  = "active"
	MultipubTopicRetired = "retired"
)

// ErrMultipubTopicInvalid signals a structurally invalid multi-publisher topic.
var ErrMultipubTopicInvalid = errors.New("invalid multi-publisher topic")

// OrgMultipubPool is one org's stable opaque multipub-pool handle (minted
// once).
type OrgMultipubPool struct {
	PoolHandle string `db:"pool_handle" json:"pool_handle"`
	Org        string `db:"org"         json:"org"`
	CreatedBy  string `db:"created_by"  json:"created_by"`
	CreatedAt  int64  `db:"created_at"  json:"created_at"`
}

// MultipubTopic is one deliberately-created multi-publisher topic. KeyBase() is
// the ACL/storage/subscription key-base `multipub/<pool_handle>/<topic_id>`.
type MultipubTopic struct {
	TopicID       string `db:"topic_id"      json:"topic_id"`
	Org           string `db:"org"           json:"org"`
	PoolHandle    string `db:"pool_handle"   json:"pool_handle"`
	DisplayName   string `db:"display_name"  json:"display_name"`
	Justification string `db:"justification" json:"justification"`
	Persist       bool   `db:"persist"       json:"persist"`
	Status        string `db:"status"        json:"status"`
	CreatedBy     string `db:"created_by"    json:"created_by"`
	CreatedAt     int64  `db:"created_at"    json:"created_at"`
}

// KeyBase returns the multi-publisher topic's key-expr base:
// multipub/<pool>/<topic>. The ACL render appends `/**`, the `volatile/`
// mirror, and the `/@adv/**` form.
func (s MultipubTopic) KeyBase() string {
	return MultipubTopicPrefix + "/" + s.PoolHandle + "/" + s.TopicID
}

// MultipubTopicKeyBase composes the key-base from a pool handle and topic id.
func MultipubTopicKeyBase(poolHandle, topicID string) string {
	return MultipubTopicPrefix + "/" + poolHandle + "/" + topicID
}

// IsMultipubTopicKeyBase reports whether a key-base is well-formed for the
// multipub class: exactly `multipub/<seg>/<seg>` with two non-empty opaque
// segments. The grant gate uses it to reject any target that is not a
// multipub-topic key (a single-owner `<uuid>/**` root, or anything else, fails
// here) before even a registry lookup — the mechanistic guardrail's first
// line.
func IsMultipubTopicKeyBase(keyBase string) bool {
	parts := strings.Split(strings.TrimSpace(keyBase), "/")
	if len(parts) != 3 || parts[0] != MultipubTopicPrefix {
		return false
	}
	for _, p := range parts[1:] {
		if !opaqueSeg.MatchString(p) {
			return false
		}
	}
	return true
}

// newOpaqueID returns a stable opaque id (16 hex chars) for a pool handle or a
// topic id — never derived from a mutable name, so a rename/reorg never changes
// a key. Prefixless (a bare hex segment) so the key stays `multipub/<h>/<t>`.
func newOpaqueID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// EnsureOrgMultipubPool returns the org's stable multipub-pool handle, minting
// it once on first use. The handle never changes; the `org` field is
// currently-is.
func (r *Registry) EnsureOrgMultipubPool(ctx context.Context, org, createdBy string) (*OrgMultipubPool, error) {
	org = strings.TrimSpace(org)
	if org == "" {
		return nil, fmt.Errorf("%w: org is required", ErrMultipubTopicInvalid)
	}
	var p OrgMultipubPool
	err := r.db.GetContext(ctx, &p, `SELECT * FROM org_multipub_pools WHERE org = $1`, org)
	if err == nil {
		return &p, nil
	}
	if !errors.Is(err, sqlErrNoRows()) {
		return nil, err
	}
	handle, err := newOpaqueID()
	if err != nil {
		return nil, fmt.Errorf("mint pool handle: %w", err)
	}
	p = OrgMultipubPool{PoolHandle: handle, Org: org, CreatedBy: createdBy, CreatedAt: nowUnix()}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO org_multipub_pools (pool_handle, org, created_by, created_at) VALUES ($1, $2, $3, $4)`,
		p.PoolHandle, p.Org, p.CreatedBy, p.CreatedAt)
	if err != nil {
		// A concurrent minter may have won the UNIQUE(org) race; re-read.
		if got, gerr := r.getOrgMultipubPool(ctx, org); gerr == nil {
			return got, nil
		}
		return nil, fmt.Errorf("insert org multipub pool: %w", err)
	}
	return &p, nil
}

func (r *Registry) getOrgMultipubPool(ctx context.Context, org string) (*OrgMultipubPool, error) {
	var p OrgMultipubPool
	err := r.reader().GetContext(ctx, &p, `SELECT * FROM org_multipub_pools WHERE org = $1`, org)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreateMultipubTopic mints a new multi-publisher topic for an org: ensures
// the org's pool handle, mints an opaque topic id, and inserts an active row.
// A display name is required (it is what a human discovers); the org is
// required.
func (r *Registry) CreateMultipubTopic(ctx context.Context, org, displayName, justification, createdBy string, persist bool) (*MultipubTopic, error) {
	org = strings.TrimSpace(org)
	displayName = strings.TrimSpace(displayName)
	if org == "" {
		return nil, fmt.Errorf("%w: org is required", ErrMultipubTopicInvalid)
	}
	if displayName == "" {
		return nil, fmt.Errorf("%w: display_name is required (it is what a human discovers)", ErrMultipubTopicInvalid)
	}
	pool, err := r.EnsureOrgMultipubPool(ctx, org, createdBy)
	if err != nil {
		return nil, err
	}
	topicID, err := newOpaqueID()
	if err != nil {
		return nil, fmt.Errorf("mint topic id: %w", err)
	}
	st := &MultipubTopic{
		TopicID:       topicID,
		Org:           org,
		PoolHandle:    pool.PoolHandle,
		DisplayName:   displayName,
		Justification: strings.TrimSpace(justification),
		Persist:       persist,
		Status:        MultipubTopicActive,
		CreatedBy:     createdBy,
		CreatedAt:     nowUnix(),
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO multipub_topics (topic_id, org, pool_handle, display_name, justification, persist, status, created_by, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		st.TopicID, st.Org, st.PoolHandle, st.DisplayName, st.Justification, boolInt(st.Persist), st.Status, st.CreatedBy, st.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert multi-publisher topic: %w", err)
	}
	return st, nil
}

// GetMultipubTopic returns one multi-publisher topic by id, or ErrNotFound.
func (r *Registry) GetMultipubTopic(ctx context.Context, topicID string) (*MultipubTopic, error) {
	var st MultipubTopic
	err := r.reader().GetContext(ctx, &st, `SELECT * FROM multipub_topics WHERE topic_id = $1`, strings.TrimSpace(topicID))
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// GetMultipubTopicByKeyBase resolves a `multipub/<pool>/<topic>` key-base to
// its row (ErrNotFound if the topic_id isn't registered, or the key isn't
// multipub-class). This is the guardrail the grant gate uses: an anchor root
// or unregistered key resolves to nothing, so it can never be a co-publish
// target.
func (r *Registry) GetMultipubTopicByKeyBase(ctx context.Context, keyBase string) (*MultipubTopic, error) {
	if !IsMultipubTopicKeyBase(keyBase) {
		return nil, ErrNotFound
	}
	parts := strings.Split(strings.TrimSpace(keyBase), "/")
	pool, topicID := parts[1], parts[2]
	st, err := r.GetMultipubTopic(ctx, topicID)
	if err != nil {
		return nil, err
	}
	// The pool must match the topic's own pool (a forged key naming a real
	// topic_id under a different pool must not resolve).
	if st.PoolHandle != pool {
		return nil, ErrNotFound
	}
	return st, nil
}

// ListMultipubTopics returns multi-publisher topics newest first, optionally
// narrowed to an org. The discovery read behind the catalog / `goat access
// co-publish list-topics` — every row carries its org (handle → name) +
// MULTI-PUBLISHER class by construction.
func (r *Registry) ListMultipubTopics(ctx context.Context, org string) ([]MultipubTopic, error) {
	q := `SELECT * FROM multipub_topics`
	var args []any
	if strings.TrimSpace(org) != "" {
		q += " WHERE org = $1"
		args = append(args, strings.TrimSpace(org))
	}
	q += " ORDER BY created_at DESC, topic_id DESC"
	out := []MultipubTopic{}
	if err := r.reader().SelectContext(ctx, &out, q, args...); err != nil {
		return nil, fmt.Errorf("list multi-publisher topics: %w", err)
	}
	return out, nil
}

// RetireMultipubTopic flips a multi-publisher topic to retired
// (deprecate-never-delete).
func (r *Registry) RetireMultipubTopic(ctx context.Context, topicID string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE multipub_topics SET status = $1 WHERE topic_id = $2`, MultipubTopicRetired, strings.TrimSpace(topicID))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
