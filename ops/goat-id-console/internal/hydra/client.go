// Package hydra is a tiny Hydra admin client for "what services has this
// identity granted consent to, and when" — the page-level proxy for
// "services accessed." Per the ADR 0860 design, the v1 console renders
// these as consent-grant timestamps (lower bound), not last-request
// timestamps (which would need oauth2-proxy log ingestion we don't have).
package hydra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	AdminURL string
	HTTP     *http.Client
}

func NewClient(adminURL string) *Client {
	return &Client{
		AdminURL: adminURL,
		HTTP:     &http.Client{Timeout: 10 * time.Second},
	}
}

// ConsentSession is the subset of Hydra's consent session shape we render.
type ConsentSession struct {
	ConsentRequest           ConsentRequest `json:"consent_request"`
	GrantScope               []string       `json:"grant_scope"`
	GrantAccessTokenAudience []string       `json:"grant_access_token_audience"`
	HandledAt                time.Time      `json:"handled_at"`
	RememberFor              int            `json:"remember_for"`
	ExpiresAt                ExpiresAt      `json:"expires_at"`
}

type ConsentRequest struct {
	Challenge string  `json:"challenge"`
	Client    Client_ `json:"client"`
	Subject   string  `json:"subject"`
	Skip      bool    `json:"skip"`
}

type Client_ struct {
	ClientID   string `json:"client_id"`
	ClientName string `json:"client_name"`
}

type ExpiresAt struct {
	AccessToken   time.Time `json:"access_token"`
	IDToken       time.Time `json:"id_token"`
	AuthorizeCode time.Time `json:"authorize_code"`
	RefreshToken  time.Time `json:"refresh_token"`
}

// ListConsentSessions returns the per-subject list of OIDC clients
// (= services) the identity has granted consent to, with timestamps.
func (c *Client) ListConsentSessions(ctx context.Context, subject string) ([]ConsentSession, error) {
	u := fmt.Sprintf("%s/admin/oauth2/auth/sessions/consent?subject=%s",
		c.AdminURL, url.QueryEscape(subject))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		// Hydra returns 404 when there are no consent sessions for the subject.
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		body := truncate(raw, 200)
		if readErr != nil {
			if strings.TrimSpace(body) != "" {
				body = fmt.Sprintf("%s (body read error: %v)", body, readErr)
			} else {
				body = fmt.Sprintf("body read error: %v", readErr)
			}
		}
		return nil, fmt.Errorf("hydra GET %s: %d: %s", u, resp.StatusCode, body)
	}
	// readErr first: a 2xx whose body was truncated or reset can yield zero
	// bytes, and the len==0 arm would otherwise report that transport failure
	// as the benign "no consent sessions" answer.
	if readErr != nil {
		return nil, fmt.Errorf("read hydra response body: %w", readErr)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var sessions []ConsentSession
	if err := json.Unmarshal(raw, &sessions); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return sessions, nil
}

func truncate(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
