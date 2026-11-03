package onboardstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Invite is one "presigned one-time link" for the three-stage delegated
// candidate onboarding (ADR 1062). A sponsor mints it (pre-binding the access
// level); a candidate opens the public link and fills only their own identity.
// On submit the token is consumed (single-use), a proposal is created, and the
// invite is dead. The plaintext token is never stored — only its SHA-256 hash.
type Invite struct {
	ID        string `db:"id"         json:"id"`
	CreatedAt int64  `db:"created_at" json:"created_at"`
	Sponsor   string `db:"sponsor"    json:"sponsor"`
	// SponsorName is the human-readable label for Sponsor (a participant
	// sponsor's Sponsor is its UUID org anchor — the machine value provenance
	// and sponsor-scoping match on; this is the portal display_name resolved
	// at mint). Display-only; '' on rows minted before the column existed.
	SponsorName  string  `db:"sponsor_name"  json:"sponsor_name,omitempty"`
	Env          string  `db:"env"           json:"env"`
	TokenHash    string  `db:"token_hash"    json:"-"`
	PreboundJSON string  `db:"prebound_json" json:"prebound_json"`
	ExpiresAt    int64   `db:"expires_at"    json:"expires_at"`
	Status       string  `db:"status"        json:"status"`
	ConsumedAt   *int64  `db:"consumed_at"   json:"consumed_at,omitempty"`
	ProposalID   *string `db:"proposal_id"   json:"proposal_id,omitempty"`
	Note         string  `db:"note"          json:"note"`
	// CandidateEmail is the address this invite was ADDRESSED to — typed at
	// mint, or on a later send from the invite page. '' means the link was
	// handed over out of band (or the row predates the column). It records the
	// intended recipient, never proof that they received it.
	CandidateEmail string `db:"candidate_email" json:"candidate_email,omitempty"`
	// SentAt / SentVia record when the console last released the link toward
	// CandidateEmail and by which route: "email" (the console sent it) or
	// "eml" (an .eml was composed for the operator's own mail client, so the
	// console never observed the send). Nil/'' = the console sent nothing.
	SentAt  *int64 `db:"sent_at"  json:"sent_at,omitempty"`
	SentVia string `db:"sent_via" json:"sent_via,omitempty"`
	// RevokedAt / ArchivedAt anchor the two questions the archive lane asks:
	// when did this die, and has it already been swept. RevokedAt is nil on
	// rows revoked before the column existed (StaleSince falls back to the
	// TTL for those); ArchivedAt is nil until the sweep moves the row.
	RevokedAt  *int64 `db:"revoked_at"  json:"revoked_at,omitempty"`
	ArchivedAt *int64 `db:"archived_at" json:"archived_at,omitempty"`
	// Escalated is computed (not stored): true when the pre-bound tier is an
	// escalated admin role, so lists/preview can flag it (ADR 1062 Amendment 1).
	Escalated bool `db:"-" json:"escalated"`
	// MetadataInvalid keeps list APIs available while making the affected row
	// fail closed: callers must not infer any access fields from it.
	MetadataInvalid bool `db:"-" json:"metadata_invalid,omitempty"`
}

// Prebound parses the invite's pre-bound access binding. Empty metadata is
// supported for legacy operator-minted invites; malformed non-empty metadata
// is an error because a zero-value SponsorMaxTier means unrestricted.
func (inv *Invite) Prebound() (Prebound, error) {
	var pb Prebound
	if inv.PreboundJSON != "" {
		if err := json.Unmarshal([]byte(inv.PreboundJSON), &pb); err != nil {
			return Prebound{}, fmt.Errorf("decode invite prebinding: %w", err)
		}
	}
	return pb, nil
}

// Invite lifecycle states.
const (
	InviteInvited   = "invited"
	InviteSubmitted = "submitted"
	InviteExpired   = "expired"
	InviteRevoked   = "revoked"
	// InviteArchived is cold storage for a link that is already dead — revoked,
	// or past its TTL — and has been dead long enough that it is no longer
	// something an operator is working on. It is a DISPLAY category, not a new
	// stage: an archived invite is exactly as unusable as it was the moment it
	// was revoked or expired, because ConsumeInvite only ever accepts 'invited'.
	// Archiving moves it off the working list so the list shows live work.
	InviteArchived = "archived"
)

// Prebound is the access binding a sponsor pins onto an invite. The candidate
// can never set these — capability rides the server-held invite, never the
// public form (ADR 1062 §2). Stored JSON-encoded so the shape can extend
// (echelon, node-class, parent-affiliation edge) without a schema migration.
type Prebound struct {
	Tier string `json:"tier,omitempty"`
	// Affiliation is affiliation.kind, DEPRECATED by ADR 1151 and no longer
	// offered on the mint form. Kept because invites minted before the change
	// carry it and must still resolve; kind is otherwise derived from Sector +
	// nation. Never re-introduce a control that sets this: a picker listing
	// `partner` beside `organic` is how the word gets read as an alliance
	// standing, which is the misuse ADR 1151 exists to end.
	Affiliation string `json:"affiliation,omitempty"`
	// Sector is the ADR 1151 target block value the sponsor may prebind
	// (state | commercial | civil-society). Empty = the candidate declares it.
	Sector string `json:"sector,omitempty"`
	// Org is the REGISTERED org id (ADR 1112) the sponsor pinned at mint, already
	// resolved through the alias table so it is canonical. It is the candidate's
	// recommended organization: the intake page shows it instead of asking them to
	// free-type a name, and assembly writes it straight to affiliation.id rather
	// than slugging whatever they typed, so one company does not become several
	// orgs. Empty = no recommendation; the candidate types a name and the
	// reviewer runs match-or-create as before. Advisory to the CANDIDATE (they may
	// still type a different employer, recorded as a petition), authoritative over
	// the SLUG (a typed name never silently mints a second org id for an invite
	// that named one).
	Org          string `json:"org,omitempty"`
	Site         string `json:"site,omitempty"`
	MachineQuota int    `json:"machine_quota,omitempty"`
	// Collab is the sponsor's per-individual decision to include this identity in
	// the cloud collaboration environment (Outline/filedrop/Zulip via the collab
	// Dex/Kanidm plane, separate from the mesh Ory). Prebound at mint; the candidate
	// does not set it. Stamped into the manifest as collab.enabled.
	Collab bool `json:"collab,omitempty"`
	// SponsorMaxTier is the SERVER-set ceiling on the tier this invite can
	// produce, copied from the minting sponsor's sponsorship annotation
	// (ADR 1071). '' = unrestricted (operator-minted). Never client-set:
	// the mint handler overwrites it from the resolved principal, and the
	// submit/preview paths cap the enrollee's tier choice against it.
	SponsorMaxTier string `json:"sponsor_max_tier,omitempty"`
}

// newInviteID returns an opaque random id ("inv-<16-hex>").
func newInviteID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "inv-" + hex.EncodeToString(b[:]), nil
}

// newInviteToken returns a 128-bit opaque one-time token ("oti_<32-hex>") and
// its SHA-256 hex hash. Only the hash is persisted.
func newInviteToken() (token, hash string, err error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	token = "oti_" + hex.EncodeToString(b[:])
	hash = hashInviteToken(token)
	return token, hash, nil
}

// hashInviteToken is the one-way lookup key for a presented token. A DB read
// never yields a usable link.
func hashInviteToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// MintInvite creates an invite, assigning id + created_at + a fresh one-time
// token, and returns the PLAINTEXT token (shown to the sponsor once, never
// stored). The caller supplies Sponsor, Env, ExpiresAt, PreboundJSON, Note.
func (s *Store) MintInvite(ctx context.Context, inv *Invite) (token string, err error) {
	if inv.ID == "" {
		id, e := newInviteID()
		if e != nil {
			return "", fmt.Errorf("invite id: %w", e)
		}
		inv.ID = id
	}
	if inv.CreatedAt == 0 {
		inv.CreatedAt = time.Now().Unix()
	}
	if inv.Status == "" {
		inv.Status = InviteInvited
	}
	if inv.PreboundJSON == "" {
		inv.PreboundJSON = "{}"
	}
	tok, hash, err := newInviteToken()
	if err != nil {
		return "", fmt.Errorf("invite token: %w", err)
	}
	inv.TokenHash = hash
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO onboard_invites (id, created_at, sponsor, sponsor_name, env, token_hash, prebound_json, expires_at, status, consumed_at, proposal_id, note, candidate_email, sent_at, sent_via)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		inv.ID, inv.CreatedAt, inv.Sponsor, inv.SponsorName, inv.Env, inv.TokenHash, inv.PreboundJSON, inv.ExpiresAt, inv.Status, inv.ConsumedAt, inv.ProposalID, inv.Note,
		strings.TrimSpace(inv.CandidateEmail), inv.SentAt, inv.SentVia)
	if err != nil {
		return "", fmt.Errorf("insert invite: %w", err)
	}
	return tok, nil
}

// GetInviteByToken looks an invite up by presented token. It returns the row
// regardless of status (the caller decides validity), or ErrNotFound. A row
// found past its TTL while still 'invited' is lazily marked 'expired' and
// returned with the updated status, so a stale link reads expired everywhere.
func (s *Store) GetInviteByToken(ctx context.Context, token string) (*Invite, error) {
	var inv Invite
	err := s.db.GetContext(ctx, &inv, `SELECT * FROM onboard_invites WHERE token_hash = $1`, hashInviteToken(token))
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.lazyExpire(ctx, &inv)
	return &inv, nil
}

// GetInvite returns one invite by id (never leaks the token), or ErrNotFound.
func (s *Store) GetInvite(ctx context.Context, id string) (*Invite, error) {
	var inv Invite
	err := s.db.GetContext(ctx, &inv, `SELECT * FROM onboard_invites WHERE id = $1`, id)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.lazyExpire(ctx, &inv)
	return &inv, nil
}

// ListInvites returns invites, optionally filtered by status (""=all), newest
// first. The token hash is never serialized (json:"-").
//
// TTL is folded in on the way out: a row still stored as 'invited' whose
// expires_at has passed is returned as 'expired', and the status filter is
// applied to that resolved value. Without this the list reports a dead link as
// open, the web page (which resolves TTL in its view layer) calls it expired,
// and the CLI's answer for the same row is the reassuring one.
func (s *Store) ListInvites(ctx context.Context, status string) ([]Invite, error) {
	var invs []Invite
	// Always pull the full set and filter after resolving TTL: an expired-by-TTL
	// row is stored as 'invited', so a SQL `WHERE status = 'expired'` would miss
	// it and `WHERE status = 'invited'` would wrongly include it.
	if err := s.db.SelectContext(ctx, &invs, `SELECT * FROM onboard_invites ORDER BY created_at DESC`); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	out := invs[:0]
	for _, inv := range invs {
		if inv.Status == InviteInvited && inv.ExpiresAt != 0 && now >= inv.ExpiresAt {
			inv.Status = InviteExpired
		}
		if status != "" && inv.Status != status {
			continue
		}
		out = append(out, inv)
	}
	return out, nil
}

// Delivery routes recorded in Invite.SentVia.
const (
	// SentViaEmail — the console itself sent the link (SES/SMTP accepted it).
	SentViaEmail = "email"
	// SentViaEML — the console composed an .eml the operator downloaded to send
	// from their own mail client. The console never observed the send, so this
	// records a hand-over, not a delivery.
	SentViaEML = "eml"
)

// AddressInvite records the address an invite is intended for WITHOUT claiming
// it was sent. Called when a sponsor types a candidate address (at mint, or
// before triggering address verification) so the record shows who the link was
// meant for even when delivery never happened. A blank address is a no-op — it
// never erases an address already on the row.
func (s *Store) AddressInvite(ctx context.Context, id, to string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE onboard_invites SET candidate_email = $1 WHERE id = $2`, to, id)
	if err != nil {
		return fmt.Errorf("address invite: %w", err)
	}
	return nil
}

// RecordInviteSend stamps who the link went to, when it left the console, and
// by which route (SentViaEmail | SentViaEML). A re-send overwrites: the row
// carries the LATEST hand-over, not a history — the per-send history lives in
// the audit trail (onboard_email_invite / onboard_email_download).
func (s *Store) RecordInviteSend(ctx context.Context, id, to, via string, at int64) error {
	to = strings.TrimSpace(to)
	if at == 0 {
		at = time.Now().Unix()
	}
	var err error
	if to == "" {
		_, err = s.db.ExecContext(ctx,
			`UPDATE onboard_invites SET sent_at = $1, sent_via = $2 WHERE id = $3`, at, via, id)
	} else {
		_, err = s.db.ExecContext(ctx,
			`UPDATE onboard_invites SET candidate_email = $1, sent_at = $2, sent_via = $3 WHERE id = $4`, to, at, via, id)
	}
	if err != nil {
		return fmt.Errorf("record invite send: %w", err)
	}
	return nil
}

// RevokeInvite kills an open invite before it is used (sponsor changed their
// mind, or the link leaked). A revoked link fails closed on consume. No-op on
// an already-submitted invite (that credential path is inert but recorded).
func (s *Store) RevokeInvite(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE onboard_invites SET status = $1, revoked_at = $2 WHERE id = $3 AND status = $4`,
		InviteRevoked, time.Now().Unix(), id, InviteInvited)
	if err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	n, raErr := res.RowsAffected()
	_ = raErr // The driver's RowsAffected error is best-effort; unknown row count falls through to the GetInvite check.
	if n == 0 {
		// Distinguish unknown id from not-open.
		if _, gErr := s.GetInvite(ctx, id); gErr != nil {
			return gErr
		}
		return ErrInviteConsumed
	}
	return nil
}

// ConsumeInvite validates a presented token and, if the invite is open,
// atomically: (1) assembles the intake record (pre-bound binding ⊕ the
// candidate's own submission via the caller's build func), (2) creates an inert
// proposal in the ADR 1061 queue, and (3) marks the invite 'submitted' +
// single-use-consumed with the new proposal id. Returns the created proposal.
//
// Errors: ErrNotFound (unknown token), ErrInviteExpired (past TTL),
// ErrInviteConsumed (already submitted/revoked). Single-use is enforced by a
// conditional UPDATE guarded on status='invited', so a concurrent double-submit
// resolves to exactly one proposal.
func (s *Store) ConsumeInvite(ctx context.Context, token string, build func(inv *Invite, pid string) (*Proposal, error)) (*Proposal, error) {
	inv, err := s.GetInviteByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	switch inv.Status {
	case InviteInvited:
		// open — proceed
	case InviteExpired:
		return nil, ErrInviteExpired
	default:
		return nil, ErrInviteConsumed
	}

	now := time.Now().Unix()
	pid, err := newProposalID()
	if err != nil {
		return nil, fmt.Errorf("proposal id: %w", err)
	}

	// Build the proposal BEFORE claiming, so a rejected submit (e.g. an
	// escalation-guard violation) leaves the link usable. The caller owns
	// assembly + the escalation rules (ADR 1062 Amendment 1); the store owns
	// the single-use atomicity below.
	p, err := build(inv, pid)
	if err != nil {
		return nil, err
	}
	p.ID = pid
	p.CreatedAt = now
	p.Sponsor = inv.Sponsor
	p.SponsorName = inv.SponsorName
	p.Env = inv.Env
	if p.Status == "" {
		p.Status = ProposalProposed
	}

	if err := prepareProposal(p); err != nil {
		return nil, err
	}

	// Claim the invite (single-use guard) and write the proposal in one
	// transaction. If another request already consumed it, RowsAffected is 0
	// and we refuse without minting a proposal; if the proposal write fails,
	// the rollback leaves the link usable. (On SQLite this was a claim, an
	// insert and a compensating un-claim, safe only because one connection
	// serialized every writer.)
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("claim invite: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx,
		`UPDATE onboard_invites SET status = $1, consumed_at = $2, proposal_id = $3 WHERE id = $4 AND status = $5`,
		InviteSubmitted, now, pid, inv.ID, InviteInvited)
	if err != nil {
		return nil, fmt.Errorf("claim invite: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("claim invite: count affected rows: %w", err)
	}
	if n == 0 {
		return nil, ErrInviteConsumed
	}
	if err := createProposal(ctx, tx, p); err != nil {
		return nil, fmt.Errorf("store proposal: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store proposal: commit: %w", err)
	}
	return p, nil
}

// lazyExpire flips an 'invited' row whose TTL has passed to 'expired' in place,
// so a stale link reads expired everywhere without a background sweep.
func (s *Store) lazyExpire(ctx context.Context, inv *Invite) {
	if inv.Status != InviteInvited || inv.ExpiresAt == 0 || time.Now().Unix() < inv.ExpiresAt {
		return
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE onboard_invites SET status = $1 WHERE id = $2 AND status = $3`,
		InviteExpired, inv.ID, InviteInvited); err == nil {
		inv.Status = InviteExpired
	}
}

// AssembleIntakeRecord builds the intake-time identity record (ADR 1062 §4):
// the sponsor-pre-bound access binding ⊕ the candidate's own submission, plus
// provenance (which invite / which sponsor). Returns the JSON record and the
// handle/email denormalized for the proposal queue (best-effort — a candidate
// need not know their final handle at sign-up; the grammar manifest is
// assembled later).
func AssembleIntakeRecord(inv *Invite, submission json.RawMessage) ([]byte, string, string, error) {
	var sub map[string]any
	if len(submission) == 0 {
		sub = map[string]any{}
	} else if err := json.Unmarshal(submission, &sub); err != nil {
		return nil, "", "", fmt.Errorf("candidate submission must be a JSON object: %w", err)
	}
	var prebound any
	if inv.PreboundJSON != "" {
		if err := json.Unmarshal([]byte(inv.PreboundJSON), &prebound); err != nil {
			return nil, "", "", fmt.Errorf("decode invite prebinding: %w", err)
		}
	}
	record := map[string]any{
		"schema_version":       "candidate-intake/v-draft",
		"invite_id":            inv.ID,
		"sponsor":              inv.Sponsor,
		"sponsor_name":         inv.SponsorName,
		"env":                  inv.Env,
		"prebound":             prebound,
		"candidate_submission": sub,
	}
	out, err := json.Marshal(record)
	if err != nil {
		return nil, "", "", fmt.Errorf("encode intake record: %w", err)
	}
	handle, _ := sub["handle"].(string)
	email, _ := sub["email"].(string)
	if handle == "" {
		handle = handleFromEmail(email)
	}
	return out, handle, email, nil
}

// handleFromEmail derives a best-effort placeholder handle from an email
// local-part (lowercased, non-[a-z0-9-] collapsed to '-') so the draft is
// listable. The real grammar handle is set when the manifest is assembled.
func handleFromEmail(email string) string {
	local, _, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return ""
	}
	local = strings.ToLower(local)
	var b strings.Builder
	for _, c := range local {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// DefaultInviteArchiveAfter is how long a dead invite stays on the working list
// before the sweep files it away. Seven days is short enough that the list
// reflects live work and long enough that an operator who revoked something on
// a Friday still sees it the following week.
const DefaultInviteArchiveAfter = 7 * 24 * time.Hour

// StaleSince answers "dead since when?" for one invite, which is the only input
// the archive sweep needs. It returns the moment the link stopped being usable,
// and whether that moment is known at all.
//
// The anchor differs by how the invite died, and that distinction matters:
//
//   - REVOKED: the revocation, not the TTL. A sponsor who revokes a 30-day link
//     on day one killed it on day one; dating its staleness from the TTL would
//     keep a link everyone knows is dead on the working list for a month.
//     Rows revoked before revoked_at existed fall back to the TTL — later than
//     the truth, so those linger a little rather than vanishing early.
//   - EXPIRED, whether stored as 'expired' or still stored 'invited' and past
//     its TTL (ListInvites resolves that lazily): the TTL itself.
//
// Everything else returns ok=false. An 'invited' row inside its TTL is live
// work; a 'submitted' row became a proposal and has its own lifecycle; an
// already-archived row is where it belongs. None of them are the sweep's
// business, and this is where that is decided rather than in the caller.
func StaleSince(inv Invite) (at int64, ok bool) {
	switch inv.Status {
	case InviteRevoked:
		if inv.RevokedAt != nil && *inv.RevokedAt > 0 {
			return *inv.RevokedAt, true
		}
		if inv.ExpiresAt > 0 {
			return inv.ExpiresAt, true
		}
		return 0, false
	case InviteExpired:
		if inv.ExpiresAt > 0 {
			return inv.ExpiresAt, true
		}
		return 0, false
	case InviteInvited:
		// Expired by TTL but not yet written back — the same row ListInvites
		// and inviteToView already show as "expired".
		if inv.ExpiresAt > 0 && time.Now().Unix() >= inv.ExpiresAt {
			return inv.ExpiresAt, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// ArchiveStaleInvites files away every invite that has been dead for at least
// `after`, and returns the rows it moved so the caller can audit them by id
// rather than reporting a bare count.
//
// It archives ONLY revoked and expired links. A live invite inside its TTL, a
// submitted one (now a proposal), and an already-archived one are all excluded
// by StaleSince rather than by a WHERE clause, so there is one definition of
// "dead" and the sweep and the UI cannot drift apart on it.
//
// Safe to run on a timer: each row's UPDATE is guarded on the status the sweep
// observed, so an invite revoked or consumed between the read and the write is
// left alone rather than overwritten. Idempotent — a second pass finds the rows
// already 'archived' and matches nothing.
//
// A zero or negative `after` means the caller did not configure a window; the
// default is used rather than archiving everything dead the instant it dies.
func (s *Store) ArchiveStaleInvites(ctx context.Context, after time.Duration) ([]Invite, error) {
	if after <= 0 {
		after = DefaultInviteArchiveAfter
	}
	var invs []Invite
	if err := s.db.SelectContext(ctx, &invs,
		`SELECT * FROM onboard_invites WHERE status IN ($1, $2, $3)`,
		InviteInvited, InviteExpired, InviteRevoked); err != nil {
		return nil, fmt.Errorf("archive stale invites: list: %w", err)
	}
	now := time.Now()
	cutoff := now.Add(-after).Unix()
	var moved []Invite
	for _, inv := range invs {
		at, ok := StaleSince(inv)
		if !ok || at > cutoff {
			continue
		}
		res, err := s.db.ExecContext(ctx,
			`UPDATE onboard_invites SET status = $1, archived_at = $2 WHERE id = $3 AND status = $4`,
			InviteArchived, now.Unix(), inv.ID, inv.Status)
		if err != nil {
			return moved, fmt.Errorf("archive stale invites: %s: %w", inv.ID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return moved, fmt.Errorf("archive stale invites: %s: count affected rows: %w", inv.ID, err)
		}
		if n == 1 {
			inv.Status = InviteArchived
			stamp := now.Unix()
			inv.ArchivedAt = &stamp
			moved = append(moved, inv)
		}
	}
	return moved, nil
}
