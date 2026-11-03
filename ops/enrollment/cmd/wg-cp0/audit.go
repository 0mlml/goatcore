// audit.go — append-only JSONL audit-record emission for the wg-cp0
// declarative-substrate orchestrator.
//
// Exactly one AuditRecord row is appended per --apply invocation;
// dry-runs never write to the audit log. The default path is
// `ops/enrollment/wg-cp0-ceremony-audit.jsonl`. Phase 3+ extension is
// to emit each record as an event into the Block 17D / ADR 0811
// audit-integrity tier; for 22R.2 the JSONL file on the operator
// laptop (with git history as the secondary durability layer) is the
// authoritative store.
//
// The record carries (per ADR 0322 §4.3):
//
//   - timestamp + operator + host
//   - the full subcommand argv that fired
//   - the intent kind + a plan summary (counts + affected peers /
//     relays)
//   - per-step outcome including duration + error string for failed
//     steps
//   - a correlation_id stable across the plan + apply phases so a
//     future consumer can join dry-run preview emit (if any) with the
//     apply record
//
// Schema is deliberately conservative — every field is JSON-tagged
// and pointer-free so a future consumer can decode against a partial
// struct without nil-check ceremony.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultAuditPath is the on-disk JSONL audit log, sibling to the
// JSON-of-record so a single operator workdir holds both.
const DefaultAuditPath = "ops/enrollment/wg-cp0-ceremony-audit.jsonl"

// AuditRecord is one row appended on --apply. Field names match ADR
// 0322 §4.3.
type AuditRecord struct {
	// Timestamp is the wall-clock at Apply entry (not at Plan entry —
	// Plan does not emit).
	Timestamp time.Time `json:"ts"`

	// Operator is "<user>@<host>" — the operator-laptop identity.
	// Resolved from $USER + `git config user.email` (the same
	// signature the work CLI uses, so cross-substrate audit
	// reconciliation is a join on this field).
	Operator string `json:"operator"`

	// Host is the operator-laptop hostname (os.Hostname).
	Host string `json:"host"`

	// Command is the full argv concatenation. Captures every flag so
	// post-incident reconstruction can replay the exact invocation.
	Command string `json:"command"`

	// IntentKind matches Intent.Kind on the original request.
	IntentKind string `json:"intent_kind"`

	// CorrelationID is a per-Apply-invocation identifier.
	// "wg-cp0:<RFC3339-ts>:<rand-suffix>" today; the audit-integrity
	// tier (Phase 3+) will compute a stronger digest.
	CorrelationID string `json:"correlation_id"`

	// PlanSummary collapses the plan into countable + named rollups.
	PlanSummary PlanSummary `json:"plan_summary"`

	// Outcome is "ok" | "partial" | "fail".
	Outcome string `json:"outcome"`

	// PerStep mirrors the StepOutcome list from ApplyResult.PerStep.
	PerStep []StepOutcome `json:"per_step"`

	// Track is the work-CLI track this invocation rode under.
	// Populated from the WORK_TRACK env var when set; otherwise empty.
	Track string `json:"track,omitempty"`
}

// PlanSummary collapses an ApplyPlan into counts + affected names.
// Used by the audit record + by `wg-cp0 diff --json` for scripting.
type PlanSummary struct {
	Adds            int      `json:"adds"`
	Rotates         int      `json:"rotates"`
	Removes         int      `json:"removes"`
	EndpointUpdates int      `json:"endpoint_updates"`
	TTLUpdates      int      `json:"ttl_updates"`
	RelaysAdded     int      `json:"relays_added"`
	AffectedPeers   []string `json:"affected_peers"`
	AffectedRelays  []string `json:"affected_relays"`
}

// SummarizePlan computes the PlanSummary fields from a plan's
// StatePlan + LivePlan. Pure function so tests can assert without
// running the applier.
//
// Counts the union of state-plan changes (lifecycle subcommands —
// onboard/rotate/remove change the JSON-of-record) and live-plan
// changes (reconcile's adds/rotates/removes are wire-side; the
// declared PeerSet already carries the peer, the "add" is in the
// relay's wg-set state).
func SummarizePlan(p *ApplyPlan) PlanSummary {
	s := PlanSummary{}
	if p == nil {
		return s
	}
	peers := map[string]struct{}{}
	relays := map[string]struct{}{}
	if p.StatePlan != nil {
		s.Adds += len(p.StatePlan.Added)
		s.Rotates += len(p.StatePlan.Rotated)
		s.Removes += len(p.StatePlan.Removed)
		s.EndpointUpdates += len(p.StatePlan.EndpointsUpdated)
		s.TTLUpdates += len(p.StatePlan.TTLUpdated)
		s.RelaysAdded += len(p.StatePlan.RelaysAdded)
		for _, d := range p.StatePlan.Added {
			peers[d.Name] = struct{}{}
		}
		for _, d := range p.StatePlan.Removed {
			peers[d.Name] = struct{}{}
		}
		for _, d := range p.StatePlan.Rotated {
			peers[d.Name] = struct{}{}
		}
		for _, d := range p.StatePlan.EndpointsUpdated {
			peers[d.Name] = struct{}{}
		}
		for _, d := range p.StatePlan.TTLUpdated {
			peers[d.Name] = struct{}{}
		}
		for _, d := range p.StatePlan.RelaysAdded {
			relays[d.Name] = struct{}{}
		}
	}
	if p.LivePlan != nil {
		// Live-side counts. Reconcile's adds/rotates/removes hit the
		// wire only; the JSON-of-record is unchanged. Track per peer
		// name to avoid double-counting the same peer touched on
		// multiple relays — the operator cares "did peer X get added
		// (somewhere)?" + the per-relay breakdown lives in PerStep.
		liveAdded := map[string]struct{}{}
		liveRotated := map[string]struct{}{}
		liveRemoved := map[string]struct{}{}
		for _, rd := range p.LivePlan.PerRelay {
			if !rd.IsEmpty() {
				relays[rd.Relay] = struct{}{}
			}
			for _, op := range rd.WouldAdd {
				if op.PeerName != "" {
					liveAdded[op.PeerName] = struct{}{}
					peers[op.PeerName] = struct{}{}
				}
			}
			for _, op := range rd.WouldRotate {
				if op.PeerName != "" {
					liveRotated[op.PeerName] = struct{}{}
					peers[op.PeerName] = struct{}{}
				}
			}
			for _, op := range rd.WouldRemove {
				name := op.PeerName
				if name != "" {
					liveRemoved[name] = struct{}{}
					peers[name] = struct{}{}
				}
			}
		}
		s.Adds += len(liveAdded)
		s.Rotates += len(liveRotated)
		s.Removes += len(liveRemoved)
	}
	s.AffectedPeers = sortedKeys(peers)
	s.AffectedRelays = sortedKeys(relays)
	return s
}

// AuditEmitter writes one AuditRecord per call. The default impl
// appends a JSONL line to a file; tests substitute a Noop or a
// recording emitter.
type AuditEmitter interface {
	Emit(record AuditRecord) error
}

// FileAuditEmitter appends to a JSONL file. Concurrent writes from
// multiple goroutines (not expected in the current orchestrator but
// cheap to guarantee) are serialized via a mutex; the OS-level append
// flag handles cross-process safety.
type FileAuditEmitter struct {
	Path string
	mu   sync.Mutex
}

// NewFileAuditEmitter constructs an emitter against the given path,
// or DefaultAuditPath when empty.
func NewFileAuditEmitter(path string) *FileAuditEmitter {
	if path == "" {
		path = DefaultAuditPath
	}
	return &FileAuditEmitter{Path: path}
}

// Emit appends one JSON-encoded record + newline.
func (e *FileAuditEmitter) Emit(record AuditRecord) (err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(e.Path), 0o755); err != nil {
		return fmt.Errorf("wg-cp0 audit: ensure dir: %w", err)
	}
	f, err := os.OpenFile(e.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("wg-cp0 audit: open %s: %w", e.Path, err)
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("wg-cp0 audit: marshal: %w", err)
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("wg-cp0 audit: write: %w", err)
	}
	return nil
}

// NoopAuditEmitter discards every record. Used in unit tests that do
// not care about audit shape.
type NoopAuditEmitter struct{}

// Emit implements AuditEmitter.
func (NoopAuditEmitter) Emit(_ AuditRecord) error { return nil }

// RecordingAuditEmitter captures records in memory so a test can
// assert the audit shape after Apply.
type RecordingAuditEmitter struct {
	Records []AuditRecord
	Err     error
	mu      sync.Mutex
}

// Emit implements AuditEmitter.
func (r *RecordingAuditEmitter) Emit(record AuditRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Records = append(r.Records, record)
	return r.Err
}

// ResolveOperator returns "<user>@<git-email>" or the closest
// approximation we can derive. Empty $USER falls through to
// os/user.Current; missing git config falls through to "(no-email)".
// Errors that would block the orchestrator are intentionally NOT
// returned — the orchestrator should never block on audit-identity
// resolution.
func ResolveOperator() string {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME") // windows fallback
	}
	email := ""
	if cmd := exec.Command("git", "config", "user.email"); cmd != nil {
		if out, err := cmd.Output(); err == nil {
			email = strings.TrimSpace(string(out))
		}
	}
	switch {
	case user != "" && email != "":
		return user + "@" + email
	case email != "":
		return email
	case user != "":
		return user
	default:
		return "(unknown)"
	}
}

// ResolveTrack reads the WORK_TRACK env var (set by ops/work/work
// pickup) and returns it. Empty when not set.
func ResolveTrack() string {
	return os.Getenv("WORK_TRACK")
}

// sortedKeys returns the keys of m sorted lexicographically. Local
// helper so audit + summary outputs are deterministic.
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MakeCorrelationID returns a per-Apply identifier of the shape
// "wg-cp0:<RFC3339-ts>:<rand-suffix>". Pure function over the clock
// + a randomness source; tests inject both.
func MakeCorrelationID(now time.Time, rand io.Reader) string {
	ts := now.UTC().Format("20060102T150405Z")
	suffix := readShortHex(rand, 4) // 8 hex chars
	if suffix == "" {
		suffix = "00000000"
	}
	return "wg-cp0:" + ts + ":" + suffix
}

// readShortHex reads n bytes from r and returns them as a hex string,
// or "" on error. n must be small (<=16); large reads should use the
// stdlib hex encoder directly.
func readShortHex(r io.Reader, n int) string {
	if r == nil {
		return ""
	}
	if n <= 0 || n > 16 {
		return ""
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return ""
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 2*n)
	for i, b := range buf {
		out[2*i] = hex[b>>4]
		out[2*i+1] = hex[b&0x0f]
	}
	return string(out)
}

// ErrAuditPathEmpty is returned by NewFileAuditEmitter consumers when
// path resolution fails; exported so callers can errors.Is against it.
var ErrAuditPathEmpty = errors.New("wg-cp0 audit: empty path")
