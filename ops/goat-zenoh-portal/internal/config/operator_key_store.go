package config

import (
	"context"
	"crypto/subtle"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// OperatorKeyStore holds the operator API keys the portal will accept, and —
// unlike the startup-only map it replaces — notices when the on-host key
// directory changes.
//
// WHY THIS EXISTS. Issuing a person an operator key used to mean editing a
// gitignored envrc on somebody's laptop and redeploying the portal. That is a
// bottleneck with a human in it, not a control: the governance that matters is
// an admin APPROVING the key, and approval does not require a restart. With
// the directory reloaded live, minting and revoking become ordinary operations
// that a console page (or `goat admin operator-key`) can perform, and the
// portal picks them up on its own.
//
// TWO TIERS, DELIBERATELY DIFFERENT.
//
//   - ENV keys (PORTAL_OPERATOR_API_KEYS) are frozen at startup. They are the
//     deploying laptop's intent, they are how the portal boots with a working
//     credential even if the directory is empty or unreadable, and nothing at
//     runtime can remove them. A break-glass key belongs here.
//   - DIRECTORY keys are live. Adding a file grants; removing it revokes. Env
//     wins on a handle collision, so a directory can never quietly redefine a
//     handle the deployment pinned. The collision is recorded (Shadowed): a
//     directory key the env tier overrides is one that revoking in the console
//     cannot revoke, and that must be visible rather than silent.
//
// FAILURE DIRECTION. A reload that cannot READ the directory keeps the last
// good set: a transient I/O error, a half-written file, or an unmounted volume
// must not log every operator out of a running goatnet. A reload that reads
// successfully and finds fewer keys DOES shrink the set — that is revocation,
// and treating it as an error would make revoking impossible. The distinction
// is "could not read" versus "read, and it says less".
type OperatorKeyStore struct {
	env map[string]string // immutable; parsed once from the environment
	dir string            // "" disables the live tier entirely

	mu     sync.RWMutex
	merged map[string]string // env + dir, env winning
	// lastErr is the most recent reload failure, retained so a health surface
	// can say "serving stale keys because X" rather than looking healthy while
	// silently ignoring the directory.
	lastErr  error
	loadedAt time.Time
	// shadowed lists the handles that have a directory key AND an env key.
	// The env key is the one accepted; the directory key is ignored, so
	// deleting its file through the console does not revoke the handle.
	shadowed []string
}

// NewOperatorKeyStore builds the store and performs the first load. A first
// load that fails to read the directory is a hard error: at startup there is no
// last-good set worth preserving, and booting with a silently smaller key set
// is how a portal comes up refusing the credentials its operators hold.
func NewOperatorKeyStore(env map[string]string, dir string) (*OperatorKeyStore, error) {
	s := &OperatorKeyStore{env: cloneKeys(env), dir: strings.TrimSpace(dir)}
	if _, err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// Match returns the handle whose key equals the presented token, or "".
//
// It compares against EVERY key before returning rather than short-circuiting,
// so the time taken does not reveal which handle matched — or that any did.
// The comparison itself is constant-time.
func (s *OperatorKeyStore) Match(token []byte) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	matched := ""
	for handle, key := range s.merged {
		if subtle.ConstantTimeCompare(token, []byte(key)) == 1 {
			matched = handle
		}
	}
	return matched
}

// Len reports how many keys are currently accepted.
func (s *OperatorKeyStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.merged)
}

// Handles returns the accepted handles, sorted. Never the keys — this feeds
// logs and health surfaces, and a store that can print its own secrets will
// eventually print them somewhere durable.
func (s *OperatorKeyStore) Handles() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.merged))
	for h := range s.merged {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// Shadowed returns, sorted, the handles whose directory key is overridden by
// an env key as of the last successful load. Handles only, never keys. A
// non-empty result means console revocation of those handles has no effect
// until the env pin is removed and the portal redeployed.
func (s *OperatorKeyStore) Shadowed() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.shadowed...)
}

// LastError reports the most recent reload failure, or nil. A non-nil value
// means the served set is STALE, not empty.
func (s *OperatorKeyStore) LastError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastErr
}

// Reload re-reads the directory and swaps in the new merged set. It reports
// whether the accepted handle set changed, which is what callers log — the
// interesting event is "somebody gained or lost access", not "the timer fired".
func (s *OperatorKeyStore) Reload() (changed bool, err error) {
	next := cloneKeys(s.env)
	shadowed, err := mergeOperatorKeysFromDirReportingShadowed(s.dir, next, s.env)
	if err != nil {
		s.mu.Lock()
		s.lastErr = err
		s.mu.Unlock()
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed = !sameKeys(s.merged, next)
	s.merged = next
	s.shadowed = shadowed
	s.lastErr = nil
	s.loadedAt = time.Now()
	return changed, nil
}

// Diff reports what a reload would change, as handle lists. Used for the log
// line so an operator reading it learns WHO gained or lost a key rather than
// only that a count moved.
func (s *OperatorKeyStore) diffAgainst(next map[string]string) (added, removed []string) {
	for h := range next {
		if _, ok := s.merged[h]; !ok {
			added = append(added, h)
		}
	}
	for h := range s.merged {
		if _, ok := next[h]; !ok {
			removed = append(removed, h)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// Watch reloads on an interval until ctx is done, calling onChange with the
// handles gained and lost. Polling rather than inotify: the directory is a
// bind-mounted host path, and filesystem-event delivery across a container
// boundary is exactly the kind of thing that works in development and silently
// stops working in production. A poll cannot miss an edit; it can only be late,
// and it is bounded late.
//
// Reload errors are reported through onError rather than being swallowed; the
// previous key set stays in force, so the portal keeps serving.
func (s *OperatorKeyStore) Watch(ctx context.Context, interval time.Duration, onChange func(added, removed []string), onError func(error)) {
	if s.dir == "" || interval <= 0 {
		return // no live tier to watch
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			next := cloneKeys(s.env)
			shadowed, err := mergeOperatorKeysFromDirReportingShadowed(s.dir, next, s.env)
			if err != nil {
				s.mu.Lock()
				s.lastErr = err
				s.mu.Unlock()
				if onError != nil {
					onError(err)
				}
				continue
			}
			s.mu.Lock()
			added, removed := s.diffAgainst(next)
			s.merged = next
			s.shadowed = shadowed
			s.lastErr = nil
			s.loadedAt = time.Now()
			s.mu.Unlock()
			if (len(added) > 0 || len(removed) > 0) && onChange != nil {
				onChange(added, removed)
			}
		}
	}
}

func cloneKeys(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sameKeys(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// mergeOperatorKeysFromDir reads every regular file in dir as operator keys
// ('handle:key' lines; '#' comments and blank lines ignored) and merges them
// INTO the given map. Env-derived keys already present WIN on a handle
// conflict. An empty dir path, or a missing dir, is a no-op — a portal with no
// keystore behaves exactly as one configured with env keys only.
//
// A malformed line is a hard error rather than a skipped line: a typo that
// silently dropped a key would present as an operator's credential mysteriously
// not working, which is a much worse afternoon than a loud config failure.
func mergeOperatorKeysFromDir(dir string, into map[string]string) error {
	_, err := mergeOperatorKeysFromDirReportingShadowed(dir, into, nil)
	return err
}

// mergeOperatorKeysFromDirReportingShadowed is mergeOperatorKeysFromDir that
// also returns, sorted and de-duplicated, the directory handles that collide
// with a handle in pinned (the env tier). Which key wins is unchanged: the one
// already in into.
func mergeOperatorKeysFromDirReportingShadowed(dir string, into, pinned map[string]string) ([]string, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read operator-keys dir %q: %w", dir, err)
	}
	shadowSet := map[string]struct{}{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// Skip the temp files an atomic writer leaves mid-rename. Reading one
		// would produce a truncated key and a hard parse error on a directory
		// that is merely being written to correctly.
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".tmp") {
			continue
		}
		path := filepath.Join(dir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read operator-keys file %q: %w", path, err)
		}
		// Errors name the file and line NUMBER, never the line: a malformed
		// line is still a handle:key pair, and the error reaches the logs.
		for lineIndex, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			idx := strings.IndexByte(line, ':')
			if idx < 0 || strings.IndexByte(line[idx+1:], ':') >= 0 {
				return nil, fmt.Errorf("operator-keys file %q line %d must be exactly one handle:key pair", path, lineIndex+1)
			}
			handle := strings.TrimSpace(line[:idx])
			key := strings.TrimSpace(line[idx+1:])
			if handle == "" || key == "" {
				return nil, fmt.Errorf("operator-keys file %q line %d has an empty handle or key", path, lineIndex+1)
			}
			if _, isPinned := pinned[handle]; isPinned {
				shadowSet[handle] = struct{}{}
			}
			if _, exists := into[handle]; !exists { // env wins
				into[handle] = key
			}
		}
	}
	if len(shadowSet) == 0 {
		return nil, nil
	}
	shadowed := make([]string, 0, len(shadowSet))
	for h := range shadowSet {
		shadowed = append(shadowed, h)
	}
	sort.Strings(shadowed)
	return shadowed, nil
}
