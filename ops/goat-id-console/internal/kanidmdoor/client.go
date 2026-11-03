// Package kanidmdoor mints credential-setup codes on a Kanidm "door" — the
// sign-on planes that are SEPARATE from the mesh Ory/Kratos identity plane:
// the public dashboard door (idm.goat.example) and the collaboration door
// (idm.collab.goat.example).
//
// Why the console needs this at all. A setup code is single-use and lives 24
// hours, and that ceiling is not ours to change: Kanidm 1.5.0 clamps the
// credential-update intent TTL to 86400s and floors it at 300s, measured
// (a 7-day request came back as 24.00 h exactly). There is no
// account-policy knob for it, and there is no user-initiated "forgot password"
// on Kanidm either — /ui/forgot is a 404, and `send-reset-token` answers 404 on
// this server version. So a person whose code lapsed had exactly one route back
// in: email an operator and wait.
//
// This package removes that wait. A person already signed in to the mesh
// console proves who they are with the identity they already hold, and mints
// their OWN code on demand. The 24-hour ceiling then stops mattering, because
// a fresh code is five seconds away rather than a day of correspondence.
//
// Two rules the caller must keep, both learned expensively:
//
//   - NEVER hand back a URL with the code in it. A tokenised reset link is
//     spent in transit by mail scanners AND jams the reset page's
//     submit button, because the form appends a second `token` parameter and
//     Kanidm answers 400 while htmx declines to redraw. Return the page
//     address and the code separately, as this package does.
//   - NEVER resolve the account from user-supplied input. Bind on the SIGNED-IN
//     session's email only.
package kanidmdoor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Plane is one Kanidm door.
type Plane string

const (
	PlanePublic Plane = "public"
	PlaneCollab Plane = "collab"
)

// codeShape is what a real Kanidm credential-setup code looks like: four
// dash-separated groups of five alphanumerics.
//
// Validating the SHAPE rather than the HTTP status is load-bearing. Kanidm
// answers an unknown account with HTTP 200 and the bare JSON string
// "nomatchingentries", and answers a refused api-token request with 403 and the
// bare string "accessdenied". A caller that checks only the status hands the
// word "nomatchingentries" to a person as their setup code.
var codeShape = regexp.MustCompile(`^[A-Za-z0-9]{5}(-[A-Za-z0-9]{5}){3}$`)

// Client talks to one Kanidm door as idm_admin.
type Client struct {
	BaseURL string
	AdminPW string
	HTTP    *http.Client
	nowFn   func() time.Time // test seam
}

// New builds a client for one door. An empty base or password yields a client
// whose calls fail with a reader-facing sentence rather than a nil panic — the
// console must start on goatnets that have no collab plane at all.
func New(baseURL, adminPW string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		AdminPW: strings.TrimSpace(adminPW),
		HTTP:    &http.Client{Timeout: 20 * time.Second},
		nowFn:   time.Now,
	}
}

// Configured reports whether this door is wired up on this goatnet.
func (c *Client) Configured() bool {
	return c != nil && c.BaseURL != "" && c.AdminPW != ""
}

// Account is what the door knows about a person.
type Account struct {
	Handle        string // the sign-in name; what they type at the door
	Email         string
	HasCredential bool // they have already completed setup at least once
}

// Code is a freshly minted credential-setup code.
type Code struct {
	Handle    string
	Code      string
	ResetPage string    // BARE page address — never carries the code
	ExpiresAt time.Time // the expiry the SERVER chose, not the one we asked for
}

// FindByEmail returns the account whose `mail` attribute matches email, or nil
// when the door has no account for that address.
//
// Matching on email is the entire authorization model for self-service: the
// caller has already established, via the mesh session, that the requester owns
// that address. It follows that the caller MUST pass the session's email and
// never a form field.
func (c *Client) FindByEmail(ctx context.Context, email string) (*Account, error) {
	if !c.Configured() {
		return nil, errors.New("this goatnet has no such sign-on door configured")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, errors.New("no address to look up")
	}
	tok, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	status, body, err := c.call(ctx, http.MethodGet, "/v1/person", tok, nil)
	if err != nil || status != http.StatusOK {
		return nil, errors.New("the sign-on door did not answer, so nothing was changed — try again shortly")
	}
	var raw []struct {
		Attrs map[string][]string `json:"attrs"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil, errors.New("the sign-on door gave an answer this console could not read")
	}
	first := func(m map[string][]string, k string) string {
		if v := m[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	for _, r := range raw {
		if strings.ToLower(strings.TrimSpace(first(r.Attrs, "mail"))) != email {
			continue
		}
		_, hasPrimary := r.Attrs["primary_credential"]
		return &Account{
			Handle:        first(r.Attrs, "name"),
			Email:         first(r.Attrs, "mail"),
			HasCredential: hasPrimary || len(r.Attrs["passkeys"]) > 0 || len(r.Attrs["attested_passkeys"]) > 0,
		}, nil
	}
	return nil, nil
}

// MintCode issues a fresh credential-setup code for handle.
//
// ttl is what we ASK for; the returned ExpiresAt is what the server GRANTED,
// read from the response's expiry_time field. Those differ in practice — asking
// for a week yields 24 hours — and telling somebody a deadline the server did
// not agree to is how they come back to a dead code with no explanation.
func (c *Client) MintCode(ctx context.Context, handle string, ttl time.Duration) (*Code, error) {
	if !c.Configured() {
		return nil, errors.New("this goatnet has no such sign-on door configured")
	}
	if strings.TrimSpace(handle) == "" {
		return nil, errors.New("no account to issue a code for")
	}
	tok, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	secs := int(ttl.Seconds())
	if secs <= 0 {
		secs = 86400
	}
	_, body, err := c.call(ctx, http.MethodGet,
		fmt.Sprintf("/v1/person/%s/_credential/_update_intent/%d", handle, secs), tok, nil)
	if err != nil {
		return nil, errors.New("the sign-on door did not answer, so no code was issued — try again shortly")
	}
	var out struct {
		Token      string `json:"token"`
		ExpiryTime int64  `json:"expiry_time"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, errors.New("the sign-on door returned an unreadable response, so no code can be shown")
	}
	if !codeShape.MatchString(out.Token) {
		// Do not echo the body: on refusal it is a bare string like
		// "nomatchingentries", and putting that on a page reads as a code.
		return nil, errors.New("the sign-on door refused to issue a code for this account — an admin needs to look at it")
	}
	exp := time.Unix(out.ExpiryTime, 0).UTC()
	if out.ExpiryTime == 0 {
		exp = c.nowFn().UTC().Add(time.Duration(secs) * time.Second)
	}
	return &Code{
		Handle:    handle,
		Code:      out.Token,
		ResetPage: c.BaseURL + "/ui/reset",
		ExpiresAt: exp,
	}, nil
}

// token runs Kanidm's three-step idm_admin password auth.
func (c *Client) token(ctx context.Context) (string, error) {
	sid, _, err := c.authStep(ctx, "", map[string]any{"step": map[string]any{"init": "idm_admin"}})
	if err != nil || sid == "" {
		return "", errors.New("this console could not sign in to the sign-on door")
	}
	if _, _, err := c.authStep(ctx, sid, map[string]any{"step": map[string]any{"begin": "password"}}); err != nil {
		return "", errors.New("this console could not sign in to the sign-on door")
	}
	_, body, err := c.authStep(ctx, sid, map[string]any{"step": map[string]any{"cred": map[string]any{"password": c.AdminPW}}})
	if err != nil {
		return "", errors.New("this console could not sign in to the sign-on door")
	}
	var out struct {
		State struct {
			Success string `json:"success"`
		} `json:"state"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.State.Success == "" {
		return "", errors.New("this console could not sign in to the sign-on door")
	}
	return out.State.Success, nil
}

func (c *Client) authStep(ctx context.Context, sid string, payload any) (string, string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return sid, "", fmt.Errorf("encode authentication step: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/auth", strings.NewReader(string(b)))
	if err != nil {
		return sid, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if sid != "" {
		req.Header.Set("x-kanidm-auth-session-id", sid)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return sid, "", err
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return sid, "", fmt.Errorf("read authentication response: %w", err)
	}
	newSid := resp.Header.Get("x-kanidm-auth-session-id")
	if newSid == "" {
		newSid = sid
	}
	if resp.StatusCode >= 400 {
		return newSid, string(rb), fmt.Errorf("auth step HTTP %d", resp.StatusCode)
	}
	return newSid, string(rb), nil
}

func (c *Client) call(ctx context.Context, method, path, token string, body io.Reader) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", fmt.Errorf("read %s response: %w", path, err)
	}
	return resp.StatusCode, string(rb), nil
}
