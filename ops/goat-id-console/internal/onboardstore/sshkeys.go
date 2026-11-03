package onboardstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SSH-key lifecycle. Self-service SUBMIT, admin-gated INSTALL.
//
// The split is the security property, not ceremony. The console is reachable
// with an Ory session; if uploading a key authorized it, compromising that
// session would escalate straight to a root-capable shell on a production
// host. Today console access and host access are separate trust boundaries and
// this keeps them separate: a submission grants nothing until a human approves,
// and an approval is what the installer acts on.
const (
	SSHKeySubmitted = "submitted"
	SSHKeyApproved  = "approved"
	SSHKeyInstalled = "installed"
	SSHKeyRejected  = "rejected"
	SSHKeyRevoked   = "revoked"
)

// ErrSSHKeyBadFormat is a malformed or unsupported public key.
var ErrSSHKeyBadFormat = errors.New("not a valid SSH public key")

// ErrSSHKeyWrongState is returned when a transition does not apply to the row's
// current state — the caller should report a conflict, not a not-found.
var ErrSSHKeyWrongState = errors.New("ssh key is not in the required state")

// SSHKey is one submitted public key and its gate state.
type SSHKey struct {
	ID          string `db:"id"           json:"id"`
	CreatedAt   int64  `db:"created_at"   json:"created_at"`
	Handle      string `db:"handle"       json:"handle"`
	Email       string `db:"email"        json:"email,omitempty"`
	Env         string `db:"env"          json:"env,omitempty"`
	KeyType     string `db:"key_type"     json:"key_type"`
	KeyBody     string `db:"key_body"     json:"key_body"`
	Comment     string `db:"comment"      json:"comment,omitempty"`
	Fingerprint string `db:"fingerprint"  json:"fingerprint,omitempty"`
	Status      string `db:"status"       json:"status"`
	DecidedBy   string `db:"decided_by"   json:"decided_by,omitempty"`
	DecidedAt   int64  `db:"decided_at"   json:"decided_at,omitempty"`
	InstalledAt int64  `db:"installed_at" json:"installed_at,omitempty"`
	ExpiresAt   int64  `db:"expires_at"   json:"expires_at,omitempty"`
	Note        string `db:"note"         json:"note,omitempty"`
	// Installer skip annotation (display-only). Records why an approved key
	// was skipped at install time. State-neutral and cleared on install,
	// revoke and resubmit.
	SkipReason     string `db:"skip_reason"      json:"skip_reason,omitempty"`
	SkipReportedBy string `db:"skip_reported_by" json:"skip_reported_by,omitempty"`
	SkipReportedAt int64  `db:"skip_reported_at" json:"skip_reported_at,omitempty"`
}

// AuthorizedKeysLine renders the key as it belongs in authorized_keys.
func (k SSHKey) AuthorizedKeysLine() string {
	if k.Comment == "" {
		return k.KeyType + " " + k.KeyBody
	}
	return k.KeyType + " " + k.KeyBody + " " + k.Comment
}

// supportedKeyTypes is deliberately a closed set. `ssh-dss` (DSA) is excluded:
// it is 1024-bit, disabled in OpenSSH by default, and accepting it here would
// let someone install an unusable — or worse, weak — key and believe they had
// working access.
var supportedKeyTypes = map[string]bool{
	"ssh-ed25519":                        true,
	"sk-ssh-ed25519@openssh.com":         true, // oss-allow: key algorithm name
	"ssh-rsa":                            true,
	"ecdsa-sha2-nistp256":                true,
	"ecdsa-sha2-nistp384":                true,
	"ecdsa-sha2-nistp521":                true,
	"sk-ecdsa-sha2-nistp256@openssh.com": true, // oss-allow: key algorithm name
}

// ParseSSHPublicKey validates an authorized_keys-style line and splits it.
//
// Validation is real, not cosmetic: a bad key installed into authorized_keys is
// either dead weight or — if it is actually somebody else's — a grant to the
// wrong person. It rejects anything that is not <type> <base64-body> [comment],
// an unsupported type, or a body that is not decodable base64 whose embedded
// type string disagrees with the declared one (the classic paste-mangling and
// type-spoofing cases).
func ParseSSHPublicKey(raw string) (keyType, body, comment string, err error) {
	line := strings.TrimSpace(raw)
	// A PRIVATE key pasted by mistake is the one input we must never store.
	if strings.Contains(line, "PRIVATE KEY") {
		return "", "", "", fmt.Errorf("%w: that looks like a PRIVATE key — send only the .pub", ErrSSHKeyBadFormat)
	}
	if strings.ContainsAny(line, "\n\r") {
		return "", "", "", fmt.Errorf("%w: a public key is a single line", ErrSSHKeyBadFormat)
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", "", fmt.Errorf("%w: expected '<type> <base64> [comment]'", ErrSSHKeyBadFormat)
	}
	keyType, body = fields[0], fields[1]
	if len(fields) > 2 {
		comment = strings.Join(fields[2:], " ")
	}
	if !supportedKeyTypes[keyType] {
		return "", "", "", fmt.Errorf("%w: unsupported key type %q", ErrSSHKeyBadFormat, keyType)
	}
	decoded, derr := base64.StdEncoding.DecodeString(body)
	if derr != nil {
		return "", "", "", fmt.Errorf("%w: body is not valid base64 (a line-wrapped paste does this)", ErrSSHKeyBadFormat)
	}
	// The wire format embeds its own type string first; a mismatch means the
	// line was assembled by hand or tampered with.
	if len(decoded) < 4 {
		return "", "", "", fmt.Errorf("%w: body too short to be a key", ErrSSHKeyBadFormat)
	}
	n := int(decoded[0])<<24 | int(decoded[1])<<16 | int(decoded[2])<<8 | int(decoded[3])
	if n <= 0 || 4+n > len(decoded) || string(decoded[4:4+n]) != keyType {
		return "", "", "", fmt.Errorf("%w: declared type %q does not match the key body", ErrSSHKeyBadFormat, keyType)
	}
	return keyType, body, comment, nil
}

// SSHKeyFingerprint returns the OpenSSH SHA256 fingerprint of a key body —
// the same string sshd writes to the auth log on login ("SHA256:…"), which is
// what makes a shell login attributable to a person despite the shared account.
func SSHKeyFingerprint(body string) string {
	decoded, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(decoded)
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "=")
}

func newSSHKeyID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "sshk-" + hex.EncodeToString(b[:]), nil
}

// SubmitSSHKey records a key in the `submitted` state. Grants nothing.
//
// Re-submitting the same key for the same handle UPDATES the existing row
// rather than creating a second one, so an admin never has to reason about two
// rows meaning one grant. A re-submission of an already-installed key is a
// no-op on its state — it does not silently drop back to submitted and it does
// not re-grant.
//
// One state is different: a REJECTED row reopens as a fresh application
// (rejected -> submitted). Nothing else can ever move it — the queue lists
// only `submitted` and no admin transition starts from `rejected` — so keeping
// the state would leave the key waiting nowhere while the submitter is told it
// was recorded. Reopening is safe because it lands back
// BEFORE the gate: an admin still has to approve, same as any new submission.
// REVOKED stays sticky on purpose — a revoked key was cut deliberately,
// possibly as compromised, and must not be able to crawl back through
// self-service; the owner generates a new keypair instead.
func (s *Store) SubmitSSHKey(ctx context.Context, k *SSHKey, ttlDays int) (*SSHKey, error) {
	keyType, body, comment, err := ParseSSHPublicKey(k.KeyType + " " + k.KeyBody + " " + k.Comment)
	if err != nil {
		return nil, err
	}
	k.KeyType, k.KeyBody, k.Comment = keyType, body, comment
	k.Fingerprint = SSHKeyFingerprint(body)
	if strings.TrimSpace(k.Handle) == "" {
		return nil, errors.New("ssh key submission needs a handle")
	}
	if k.ID == "" {
		id, ierr := newSSHKeyID()
		if ierr != nil {
			return nil, ierr
		}
		k.ID = id
	}
	if k.CreatedAt == 0 {
		k.CreatedAt = time.Now().Unix()
	}
	if k.Status == "" {
		k.Status = SSHKeySubmitted
	}
	// Expiry is RECORDED here and reported by the lister; enforcement is a
	// separate, deliberately default-off sweep. Recording costs nothing;
	// silently expiring a shell during an incident is a real hazard, so
	// turning enforcement on stays an explicit operator choice.
	if ttlDays > 0 && k.ExpiresAt == 0 {
		k.ExpiresAt = time.Now().AddDate(0, 0, ttlDays).Unix()
	}

	if existing, gerr := s.SSHKeyByHandleAndBody(ctx, k.Handle, k.KeyBody); gerr == nil {
		if existing.Status == SSHKeyRejected {
			// Reopen as a fresh application: back to submitted, decision
			// cleared, created_at and expiry restamped so the queue treats it
			// as new. Guarded on the current state like every other
			// transition; losing the race (say, to a concurrent revoke) falls
			// through to the keep-state refresh below.
			res, uerr := s.db.ExecContext(ctx,
				`UPDATE ssh_keys SET status = $1, decided_by = '', decided_at = 0,
				        created_at = $2, expires_at = $3, comment = $4, email = $5, env = $6, note = $7
				 WHERE id = $8 AND status = $9`,
				SSHKeySubmitted, time.Now().Unix(), k.ExpiresAt,
				k.Comment, k.Email, k.Env, k.Note, existing.ID, SSHKeyRejected)
			if uerr != nil {
				return nil, fmt.Errorf("reopen rejected ssh key: %w", uerr)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return nil, fmt.Errorf("reopen rejected ssh key: count affected rows: %w", err)
			}
			if n == 1 {
				return s.SSHKeyByID(ctx, existing.ID)
			}
		}
		// Same person, same key: refresh the cosmetic fields, keep the state.
		// The skip annotation is deliberately NOT cleared here. Nothing about
		// the skip condition changed — the manifest is still missing, the role
		// still resolves to no host — so clearing it would erase the operator's
		// diagnostic on a no-op resubmit. A person who reads "not yet placed on
		// the hosts" and re-pastes the same key is the likeliest way to hit this,
		// and it would blank the reason for the admin triaging the queue next.
		// A CHANGED key body is a different row and carries no annotation anyway.
		_, uerr := s.db.ExecContext(ctx,
			`UPDATE ssh_keys SET comment = $1, email = $2, env = $3, note = $4
			 WHERE id = $5`,
			k.Comment, k.Email, k.Env, k.Note, existing.ID)
		if uerr != nil {
			return nil, fmt.Errorf("refresh ssh key: %w", uerr)
		}
		return s.SSHKeyByID(ctx, existing.ID)
	} else if !errors.Is(gerr, ErrNotFound) {
		return nil, gerr
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO ssh_keys (id, created_at, handle, email, env, key_type, key_body, comment,
		                       fingerprint, status, expires_at, note)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		k.ID, k.CreatedAt, k.Handle, k.Email, k.Env, k.KeyType, k.KeyBody, k.Comment,
		k.Fingerprint, k.Status, k.ExpiresAt, k.Note)
	if err != nil {
		return nil, fmt.Errorf("insert ssh key: %w", err)
	}
	return s.SSHKeyByID(ctx, k.ID)
}

// SSHKeyByID returns one key row, or ErrNotFound.
func (s *Store) SSHKeyByID(ctx context.Context, id string) (*SSHKey, error) {
	var k SSHKey
	err := s.db.GetContext(ctx, &k, `SELECT * FROM ssh_keys WHERE id = $1`, id)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// SSHKeyByHandleAndBody finds a person's existing row for a specific key.
func (s *Store) SSHKeyByHandleAndBody(ctx context.Context, handle, body string) (*SSHKey, error) {
	var k SSHKey
	err := s.db.GetContext(ctx, &k,
		`SELECT * FROM ssh_keys WHERE handle = $1 AND key_body = $2`, handle, body)
	if errors.Is(err, sqlErrNoRows()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// ListSSHKeys returns keys filtered by status ("" = all), newest first.
func (s *Store) ListSSHKeys(ctx context.Context, status string) ([]SSHKey, error) {
	var ks []SSHKey
	var err error
	if status == "" {
		err = s.db.SelectContext(ctx, &ks, `SELECT * FROM ssh_keys ORDER BY created_at DESC`)
	} else {
		err = s.db.SelectContext(ctx, &ks,
			`SELECT * FROM ssh_keys WHERE status = $1 ORDER BY created_at DESC`, status)
	}
	if err != nil {
		return nil, err
	}
	return ks, nil
}

// setSSHKeyState is the one guarded transition helper: it moves a row only from
// an expected state, so no caller can skip the gate by ordering calls cleverly.
func (s *Store) setSSHKeyState(ctx context.Context, id, from, to, actor string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE ssh_keys SET status = $1, decided_by = $2, decided_at = $3
		 WHERE id = $4 AND status = $5`,
		to, actor, time.Now().Unix(), id, from)
	if err != nil {
		return fmt.Errorf("ssh key %s -> %s: %w", from, to, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("ssh key %s -> %s: count affected rows: %w", from, to, err)
	}
	if n == 1 {
		return nil
	}
	if _, gerr := s.SSHKeyByID(ctx, id); gerr != nil {
		return gerr
	}
	return ErrSSHKeyWrongState
}

// ApproveSSHKey is THE GATE: submitted -> approved. Only an approved key is
// ever handed to the installer.
func (s *Store) ApproveSSHKey(ctx context.Context, id, actor string) error {
	return s.setSSHKeyState(ctx, id, SSHKeySubmitted, SSHKeyApproved, actor)
}

// RejectSSHKey declines a submission: submitted -> rejected.
func (s *Store) RejectSSHKey(ctx context.Context, id, actor string) error {
	return s.setSSHKeyState(ctx, id, SSHKeySubmitted, SSHKeyRejected, actor)
}

// MarkSSHKeyInstalled records that the installer actually placed the key:
// approved -> installed. Separate from approval so the console can show the
// difference between "an admin said yes" and "it is on the hosts" — the same
// sent-vs-used distinction the enrolled stage draws for mesh credentials.
func (s *Store) MarkSSHKeyInstalled(ctx context.Context, id, actor string) error {
	if err := s.setSSHKeyState(ctx, id, SSHKeyApproved, SSHKeyInstalled, actor); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE ssh_keys SET installed_at = $1, skip_reason = '', skip_reported_by = '', skip_reported_at = 0
		 WHERE id = $2`, time.Now().Unix(), id)
	return err
}

// RevokeSSHKey withdraws a key from any live state. Deliberately permissive
// about the starting state — revocation must never be the operation that is
// refused on a technicality — but it will not resurrect a rejected row.
func (s *Store) RevokeSSHKey(ctx context.Context, id, actor string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE ssh_keys SET status = $1, decided_by = $2, decided_at = $3,
			       skip_reason = '', skip_reported_by = '', skip_reported_at = 0
			 WHERE id = $4 AND status IN ($5, $6, $7)`,
		SSHKeyRevoked, actor, time.Now().Unix(), id,
		SSHKeySubmitted, SSHKeyApproved, SSHKeyInstalled)
	if err != nil {
		return fmt.Errorf("revoke ssh key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke ssh key: count affected rows: %w", err)
	}
	if n == 1 {
		return nil
	}
	if _, gerr := s.SSHKeyByID(ctx, id); gerr != nil {
		return gerr
	}
	return ErrSSHKeyWrongState
}

// RecordSSHKeySkip records that the installer skipped an approved key at
// install time, without changing its state. It is the display-only skip
// annotation used by the console queue; only applicable to approved rows.
func (s *Store) RecordSSHKeySkip(ctx context.Context, id, actor, reason string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE ssh_keys SET skip_reason = $1, skip_reported_by = $2, skip_reported_at = $3
			 WHERE id = $4 AND status = $5`,
		reason, actor, time.Now().Unix(), id, SSHKeyApproved)
	if err != nil {
		return fmt.Errorf("record ssh key skip: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("record ssh key skip: count affected rows: %w", err)
	}
	if n == 1 {
		return nil
	}
	if _, gerr := s.SSHKeyByID(ctx, id); gerr != nil {
		return gerr
	}
	return ErrSSHKeyWrongState
}
