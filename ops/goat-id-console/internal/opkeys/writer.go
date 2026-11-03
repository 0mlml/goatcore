// Package opkeys writes the on-host operator-key files the portal reads.
//
// The portal re-reads its key directory while running, so this package is the
// whole delivery mechanism: writing a file grants, removing one revokes, and
// the portal notices within one reload interval. Nothing restarts, and nobody
// edits an envrc on a laptop.
//
// The console is the record; the directory is the enforcement point. They can
// drift — a file left behind after a database restore, a write that failed
// while a row said active — so Reconcile exists to make the directory match
// the record and to say what it changed.
package opkeys

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Dir is a portal operator-key directory.
type Dir struct {
	Path string
	// Owner/Group are the numeric ids the portal container runs as (65532 for
	// the Chainguard base). A file the portal cannot read is a key that
	// silently does not work, which presents as "my key was approved and the
	// Portal still refuses me".
	UID, GID int
}

// Managed reports whether a directory is configured. An unconfigured Dir makes
// every operation a no-op rather than an error, so a goatnet that has not
// enabled the directory tier degrades to the old behavior instead of failing
// every approval.
func (d Dir) Managed() bool { return strings.TrimSpace(d.Path) != "" }

// fileFor is the per-handle path. One file per handle rather than one shared
// file: revoking is then a delete, which cannot half-succeed the way rewriting
// a shared file can.
func (d Dir) fileFor(handle string) string {
	return filepath.Join(d.Path, safeHandle(handle)+".key")
}

// safeHandle keeps a handle from escaping the directory. A handle arrives from
// a session, but a path built from any external string without this is one
// traversal away from writing somewhere it should not.
func safeHandle(h string) string {
	h = strings.TrimSpace(h)
	h = strings.ReplaceAll(h, "/", "_")
	h = strings.ReplaceAll(h, `\`, "_")
	h = strings.ReplaceAll(h, "..", "_")
	return h
}

// Write places a key. It writes a temp file and renames, so a portal reload
// that lands mid-write sees either the old file or the new one, never half of
// one — the portal skips `.tmp` names for the same reason.
func (d Dir) Write(handle, secret string) error {
	if !d.Managed() {
		return nil
	}
	if strings.TrimSpace(handle) == "" || strings.TrimSpace(secret) == "" {
		return fmt.Errorf("operator key file needs both a handle and a key")
	}
	if err := os.MkdirAll(d.Path, 0o750); err != nil {
		return fmt.Errorf("create operator-key dir: %w", err)
	}
	final := d.fileFor(handle)
	tmp := final + ".tmp"
	body := fmt.Sprintf("# managed by goat-id-console — edit through the console, not here\n%s:%s\n",
		safeHandle(handle), secret)
	if err := os.WriteFile(tmp, []byte(body), 0o640); err != nil {
		return fmt.Errorf("write operator-key temp file: %w", err)
	}
	if d.UID > 0 {
		// Best-effort: a console running unprivileged in a container may not be
		// able to chown, and a readable-by-group file is usually enough. A hard
		// failure here would block an approval over file ownership cosmetics.
		_ = os.Chown(tmp, d.UID, d.GID) //nolint:errcheck // group-readable mode is sufficient when container chown is unavailable
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("place operator-key file: %w", err)
	}
	return nil
}

// Remove revokes by deletion. A missing file is success: the desired end state
// is "this key is not accepted", and it already is not.
func (d Dir) Remove(handle string) error {
	if !d.Managed() {
		return nil
	}
	if err := os.Remove(d.fileFor(handle)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove operator-key file: %w", err)
	}
	return nil
}

// Handles lists the handles the directory currently grants, sorted.
func (d Dir) Handles() ([]string, error) {
	if !d.Managed() {
		return nil, nil
	}
	entries, err := os.ReadDir(d.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read operator-key dir: %w", err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".key") || strings.HasPrefix(n, ".") {
			continue
		}
		out = append(out, strings.TrimSuffix(n, ".key"))
	}
	sort.Strings(out)
	return out, nil
}

// ActiveLister is the slice of the store Reconcile needs. Kept narrow so the
// writer does not depend on the whole onboarding store.
//
// KnownHandles is what makes --apply safe. The directory also holds keys this
// console never issued: service keys a component's own ansible role writes
// (the onboard-runner, microscope) and human keys that predate the registry.
// Those have no row at all, and deleting them would be the console asserting
// ownership of credentials it did not create.
type ActiveLister interface {
	ActiveHandles(ctx context.Context, env string) ([]string, error)
	// KnownHandles is every handle this console has a row for, in ANY state.
	KnownHandles(ctx context.Context, env string) ([]string, error)
}

// Result is what a reconcile found, split three ways because the three cases
// call for different actions and lumping them together produced a live footgun.
type Result struct {
	// Stale: this console issued the key and has since revoked or rejected it,
	// but the file is still there. The portal accepts a key the record says was
	// taken away. This is the one --apply removes.
	Stale []string
	// Unmanaged: a file for a handle this console has no row for at all —
	// service keys written by their own ansible role, or keys that predate the
	// registry. Reported so nobody is surprised by them, never touched.
	Unmanaged []string
	// Missing: an active row with no file. That person holds a key the portal
	// refuses. Not repairable here: the secret is deliberately not retained,
	// and inventing one would hand somebody a credential nobody approved.
	Missing []string
}

// Reconcile compares the record against the directory the portal actually
// reads, and is deliberately conservative about what it will delete.
//
// The first version treated every file without an ACTIVE row as an unexplained
// grant and offered to remove it. Run against a real goatnet that would have
// deleted the onboard-runner's key and two human keys issued before this
// registry existed — none of which the console ever created. Ownership is the
// dividing line now: a handle this console has a row for is one it may enforce;
// a handle it has never heard of belongs to somebody else and is only reported.
func (d Dir) Reconcile(ctx context.Context, s ActiveLister, env string, apply bool) (Result, error) {
	var res Result
	if !d.Managed() {
		return res, nil
	}
	active, err := s.ActiveHandles(ctx, env)
	if err != nil {
		return res, err
	}
	known, err := s.KnownHandles(ctx, env)
	if err != nil {
		return res, err
	}
	isActive := map[string]bool{}
	for _, h := range active {
		isActive[safeHandle(h)] = true
	}
	isKnown := map[string]bool{}
	for _, h := range known {
		isKnown[safeHandle(h)] = true
	}
	have, err := d.Handles()
	if err != nil {
		return res, err
	}
	hasFile := map[string]bool{}
	for _, h := range have {
		hasFile[h] = true
		switch {
		case isActive[h]:
			// agrees
		case isKnown[h]:
			res.Stale = append(res.Stale, h)
		default:
			res.Unmanaged = append(res.Unmanaged, h)
		}
	}
	for h := range isActive {
		if !hasFile[h] {
			res.Missing = append(res.Missing, h)
		}
	}
	sort.Strings(res.Stale)
	sort.Strings(res.Unmanaged)
	sort.Strings(res.Missing)
	if apply {
		for _, h := range res.Stale {
			if rerr := d.Remove(h); rerr != nil {
				return res, rerr
			}
		}
	}
	return res, nil
}
