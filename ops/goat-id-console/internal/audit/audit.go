// Package audit implements the mutation audit-event emitter.
//
// v1 ships a stderr emitter + an in-memory ring buffer for the /audit
// UI page. The wire shape is locked in ADR 0860 so the v2 Zenoh-backed
// emitter is a drop-in swap behind the Emitter interface.
//
// Zenoh fan-out (TODO): ops/audit/id-console/<action>/v1, picked up
// by the Block 82S audit subscriber and landed in MinIO audit-integrity.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Actor struct {
	Email      string `json:"email"`
	IdentityID string `json:"identity_id,omitempty"`
	IdPKey     string `json:"idp_key"`
	AAL        string `json:"aal,omitempty"`
	SourceIP   string `json:"source_ip,omitempty"`
}

type Target struct {
	IdPKey     string `json:"idp_key,omitempty"`
	IdentityID string `json:"identity_id,omitempty"`
	Email      string `json:"email,omitempty"`
	// For invite-related actions, carries the invite token (first 8 chars
	// for traceability; full token would be a secret leak).
	InviteTokenPrefix string `json:"invite_token_prefix,omitempty"`
}

type Event struct {
	TS             time.Time              `json:"ts"`
	Actor          Actor                  `json:"actor"`
	Action         string                 `json:"action"`
	Target         Target                 `json:"target,omitempty"`
	Before         map[string]interface{} `json:"before,omitempty"`
	After          map[string]interface{} `json:"after,omitempty"`
	RequestID      string                 `json:"request_id"`
	StepUpVerified bool                   `json:"step_up_verified"`
	Result         string                 `json:"result"` // ok | refused | error
	ErrorMessage   string                 `json:"error_message,omitempty"`
}

// Emitter is the audit-event sink. v1 = stderr+ring; v2 = Postgres+Zenoh.
type Emitter interface {
	Emit(ctx context.Context, e Event)
	Recent(limit int) []Event
	// Durable reports whether events outlive the process, so the page can
	// say which it is instead of describing the architecture.
	Durable() bool
}

// StderrEmitter writes JSON-line audit events to slog and retains the
// last `cap` events in a ring buffer for the /audit page.
type StderrEmitter struct {
	mu     sync.RWMutex
	ring   []Event
	capN   int
	cursor int
}

func NewStderrEmitter(cap int) *StderrEmitter {
	if cap <= 0 {
		cap = 200
	}
	return &StderrEmitter{capN: cap, ring: make([]Event, 0, cap)}
}

func (s *StderrEmitter) Emit(ctx context.Context, e Event) {
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	// Log via slog so it lands in the container's structured log stream
	// alongside the rest of the service's output.
	payload, err := json.Marshal(e)
	if err != nil {
		slog.ErrorContext(ctx, "audit marshal failed",
			slog.String("audit", "true"),
			slog.String("action", e.Action),
			slog.String("result", e.Result),
			slog.String("actor_email", e.Actor.Email),
			slog.String("target_email", e.Target.Email),
			slog.String("idp", e.Actor.IdPKey),
			slog.String("request_id", e.RequestID),
			slog.String("marshal_error", err.Error()),
		)
	} else {
		slog.InfoContext(ctx, "audit",
			slog.String("audit", "true"),
			slog.String("action", e.Action),
			slog.String("result", e.Result),
			slog.String("actor_email", e.Actor.Email),
			slog.String("target_email", e.Target.Email),
			slog.String("idp", e.Actor.IdPKey),
			slog.String("request_id", e.RequestID),
			slog.String("event_json", string(payload)),
		)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ring) < s.capN {
		s.ring = append(s.ring, e)
	} else {
		s.ring[s.cursor] = e
		s.cursor = (s.cursor + 1) % s.capN
	}
}

func (s *StderrEmitter) Recent(limit int) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.ring)
	if limit > 0 && limit < n {
		n = limit
	}
	out := make([]Event, 0, n)
	// Walk newest-first. With a full ring, oldest is at cursor; newest is at cursor-1 (wrap).
	if len(s.ring) < s.capN {
		// not wrapped yet — newest is at end
		for i := len(s.ring) - 1; i >= 0 && len(out) < n; i-- {
			out = append(out, s.ring[i])
		}
		return out
	}
	// wrapped
	for k := 0; k < s.capN && len(out) < n; k++ {
		idx := (s.cursor - 1 - k + s.capN) % s.capN
		out = append(out, s.ring[idx])
	}
	return out
}

// FileEmitter is StderrEmitter plus an append-only JSONL file at a
// bind-mounted host path, so console-native audit (the Kratos mutations only
// this service performs) survives container redeploys — the
// lesson: the in-memory ring died with a mid-review redeploy and two admin
// approvals vanished from the page. This is the durable interim of ADR 0860
// ("v1.5"): no new backend service; the JSONL can be shipped into the
// existing MinIO audit tier if an evidence-grade need materializes.
type FileEmitter struct {
	*StderrEmitter
	fmu     sync.Mutex
	f       *os.File
	durable atomic.Bool
}

func NewFileEmitter(capN int, path string) (*FileEmitter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	e := &FileEmitter{StderrEmitter: NewStderrEmitter(capN), f: f}
	e.durable.Store(true)
	// Preload the ring from the tail of the file. Without this the file was
	// durable and the page was not: every redeploy showed "no audit events
	// yet in this run" over a file holding the whole history (
	// three console redeploys in one afternoon, page empty each time).
	e.preload(path)
	return e, nil
}

// preload fills the ring with the newest capN events from the JSONL file,
// oldest first so Recent() walks them newest-first. Unparseable lines are
// skipped; a file that cannot be read leaves the ring empty and the emitter
// still works.
func (e *FileEmitter) preload(path string) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > e.capN {
		lines = lines[len(lines)-e.capN:]
	}
	for _, ln := range lines {
		var ev Event
		if json.Unmarshal([]byte(ln), &ev) != nil || ev.TS.IsZero() {
			continue
		}
		e.mu.Lock()
		if len(e.ring) < e.capN {
			e.ring = append(e.ring, ev)
		} else {
			e.ring[e.cursor] = ev
			e.cursor = (e.cursor + 1) % e.capN
		}
		e.mu.Unlock()
	}
}

// Durable reports whether every event emitted by this process has been
// appended successfully. Once an event is lost, the emitter stays degraded:
// a later successful write cannot repair the gap in the audit history.
func (e *FileEmitter) Durable() bool   { return e.durable.Load() }
func (s *StderrEmitter) Durable() bool { return false }

func (e *FileEmitter) Emit(ctx context.Context, ev Event) {
	e.StderrEmitter.Emit(ctx, ev) // ring + structured log first (never lost to a file error)
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	line, err := json.Marshal(ev)
	if err != nil {
		e.durable.Store(false)
		slog.ErrorContext(ctx, "encode durable audit event", "error", err)
		return
	}
	e.fmu.Lock()
	defer e.fmu.Unlock()
	if _, err := e.f.Write(append(line, '\n')); err != nil {
		e.durable.Store(false)
		slog.ErrorContext(ctx, "write durable audit event", "error", err)
	}
}
