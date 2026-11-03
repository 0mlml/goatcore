// Package notify posts candidate-facing onboarding stage notifications to the
// id-console, which owns the email channel (SES) — increment 3 of
// docs/design/onboarding-notifications.md. Fire-and-forget by contract: a
// send failure must never block or fail the onboarding transition it
// decorates. The console mails best-effort and both sides audit-log; the
// portal's transition audit stays the record of the state change itself.
package notify

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Event is the minimal payload — identity + stage only. The candidate-facing
// links (mgmt / downloads / portal) are console config: the sender of record
// owns the words and the URLs.
type Event struct {
	Event     string `json:"event"` // proposed | sponsor_approved | rejected | onboarded
	Email     string `json:"email"`
	Name      string `json:"name,omitempty"`
	Env       string `json:"env,omitempty"`
	Handle    string `json:"handle,omitempty"`
	Escalated bool   `json:"escalated,omitempty"`
	Reason    string `json:"reason,omitempty"`    // reviewer note on a rejection
	SetupKey  string `json:"setup_key,omitempty"` // onboarded only — the mesh enrollment key
}

// Notifier is safe to use when disabled — every method no-ops. Disabled when
// either the console URL or the shared token is unset (feature-flag off).
type Notifier struct {
	url   string
	token string
	http  *http.Client
}

// New builds a notifier for the console's server-to-server /api/notify
// endpoint. Empty url or token → disabled (the default everywhere the
// feature isn't wired).
func New(url, token string) *Notifier {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	token = strings.TrimSpace(token)
	if url == "" || token == "" {
		return &Notifier{}
	}
	return &Notifier{url: url, token: token, http: &http.Client{Timeout: 5 * time.Second}}
}

func (n *Notifier) Enabled() bool { return n != nil && n.url != "" }

// Candidate fires the notification in a goroutine and returns immediately —
// the calling transition handler never waits on the mail path.
func (n *Notifier) Candidate(ev Event) {
	if !n.Enabled() || strings.TrimSpace(ev.Email) == "" {
		return
	}
	go func() {
		body, _ := json.Marshal(ev)
		req, err := http.NewRequest(http.MethodPost, n.url+"/api/notify", bytes.NewReader(body))
		if err != nil {
			slog.Warn("candidate notify: build request", "event", ev.Event, "err", err)
			return
		}
		req.Header.Set("Authorization", "Bearer "+n.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := n.http.Do(req)
		if err != nil {
			slog.Warn("candidate notify failed", "event", ev.Event, "err", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			slog.Warn("candidate notify rejected", "event", ev.Event, "status", resp.StatusCode)
		}
	}()
}
