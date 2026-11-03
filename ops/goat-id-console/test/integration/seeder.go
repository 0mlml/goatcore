//go:build integration

package integration

// Seeding a REAL second factor.
//
// Kratos v1.3.1 admin identity-import cannot create one. Verified against a
// live v1.3.1 : posting credentials.totp to POST /admin/identities
// is rejected outright with `json: unknown field "totp"` — the import payload
// accepts password and oidc only. A genuine factor therefore has to be enrolled
// the way a human would, through the self-service settings flow.
//
// The whole sequence, proven end to end against a live Kratos before this file
// was written:
//
//  1. POST /admin/identities with credentials.password.config.password
//  2. GET  /self-service/login/api            -> flow id
//     POST /self-service/login?flow=<id>      -> session_token (aal1)
//  3. GET  /self-service/settings/api  (Bearer session_token)
//     -> ui.nodes[] contains a node whose attributes.id == "totp_secret_key";
//     the base32 secret is at attributes.text.context.secret
//  4. POST /self-service/settings?flow=<id>   {"method":"totp","totp_code":…}
//     -> state == "success"
//
// The resulting credential is `totp` with config.totp_url and NO credentials
// array, which is why HasSecondFactor counts totp by presence rather than by
// an enrolled-key count (see internal/kratos/client.go).
//
// RFC 6238 is implemented here rather than pulled from a module: it is ~15
// lines of stdlib, and a test-only dependency would otherwise land in the
// console's production module graph.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// TOTPIdentity is a seeded identity that carries a genuinely enrolled TOTP
// factor. Secret is the base32 seed Kratos generated, so callers can mint
// fresh codes for an aal2 login.
type TOTPIdentity struct {
	ID           string
	Email        string
	Password     string
	Secret       string
	SessionToken string
}

// TOTPCode returns a valid RFC 6238 code (SHA-1, 6 digits, 30s step) for the
// identity's secret at time t.
func (i *TOTPIdentity) TOTPCode(t time.Time) (string, error) { return totpCode(i.Secret, t) }

// codeForSubmission mints a TOTP code that will still be current when the
// server checks it. A code computed at the very end of a 30s step can expire
// in flight, leaving the suite dependent on Kratos's skew tolerance for a
// deterministic pass; waiting out the last two seconds costs nothing and
// removes the flake.
func codeForSubmission(secret string) (string, error) {
	if rem := 30 - (time.Now().Unix() % 30); rem <= 2 {
		time.Sleep(time.Duration(rem) * time.Second)
	}
	return totpCode(secret, time.Now())
}

func totpCode(secret string, t time.Time) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(strings.ReplaceAll(secret, " ", "")))
	if pad := len(s) % 8; pad != 0 {
		s += strings.Repeat("=", 8-pad)
	}
	key, err := base32.StdEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("decode totp secret: %w", err)
	}
	var ctr [8]byte
	binary.BigEndian.PutUint64(ctr[:], uint64(t.Unix())/30)
	mac := hmac.New(sha1.New, key)
	mac.Write(ctr[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000), nil
}

// doJSON issues a JSON request and decodes the response. A non-2xx is an error
// carrying the body, because Kratos explains its refusals there and a bare
// status code makes these flows very hard to debug.
func doJSON(ctx context.Context, method, url, bearer string, body, out interface{}) error {
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal %s %s: %w", method, url, err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return fmt.Errorf("new request %s %s: %w", method, url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("decode %s %s (status %d): %w", method, url, resp.StatusCode, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s: status %d: %s", method, url, resp.StatusCode, truncate(string(raw), 400))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("unmarshal %s %s: %w", method, url, err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// kratosFlow is the subset of a self-service flow this seeder reads.
type kratosFlow struct {
	ID    string `json:"id"`
	State string `json:"state"`
	UI    struct {
		Nodes []struct {
			Attributes struct {
				ID   string `json:"id"`
				Text struct {
					Context struct {
						Secret string `json:"secret"`
					} `json:"context"`
				} `json:"text"`
			} `json:"attributes"`
		} `json:"nodes"`
	} `json:"ui"`
}

type sessionEnvelope struct {
	SessionToken string `json:"session_token"`
	Session      struct {
		AAL                   string `json:"authenticator_assurance_level"`
		AuthenticationMethods []struct {
			Method      string    `json:"method"`
			AAL         string    `json:"aal"`
			CompletedAt time.Time `json:"completed_at"`
		} `json:"authentication_methods"`
	} `json:"session"`
}

// SeedTOTPIdentity creates an identity with a password and enrols a real TOTP
// factor on it, returning everything needed to drive an aal2 login later.
func SeedTOTPIdentity(ctx context.Context, adminURL, publicURL, email, password string) (*TOTPIdentity, error) {
	var created struct {
		ID string `json:"id"`
	}
	body := map[string]interface{}{
		"schema_id":   "default",
		"traits":      map[string]interface{}{"email": email},
		"credentials": map[string]interface{}{"password": map[string]interface{}{"config": map[string]interface{}{"password": password}}},
	}
	if err := doJSON(ctx, http.MethodPost, strings.TrimSuffix(adminURL, "/")+"/admin/identities", "", body, &created); err != nil {
		return nil, fmt.Errorf("create identity: %w", err)
	}

	id := &TOTPIdentity{ID: created.ID, Email: email, Password: password}
	token, err := LoginPassword(ctx, publicURL, email, password)
	if err != nil {
		return nil, err
	}
	id.SessionToken = token

	secret, err := EnrolTOTP(ctx, publicURL, token)
	if err != nil {
		return nil, err
	}
	id.Secret = secret
	return id, nil
}

// LoginPassword performs an API-flow password login and returns the session token.
func LoginPassword(ctx context.Context, publicURL, email, password string) (string, error) {
	base := strings.TrimSuffix(publicURL, "/")
	var flow kratosFlow
	if err := doJSON(ctx, http.MethodGet, base+"/self-service/login/api", "", nil, &flow); err != nil {
		return "", fmt.Errorf("init login flow: %w", err)
	}
	var env sessionEnvelope
	body := map[string]interface{}{"method": "password", "identifier": email, "password": password}
	if err := doJSON(ctx, http.MethodPost, base+"/self-service/login?flow="+flow.ID, "", body, &env); err != nil {
		return "", fmt.Errorf("submit password login: %w", err)
	}
	if env.SessionToken == "" {
		return "", fmt.Errorf("password login returned no session_token")
	}
	return env.SessionToken, nil
}

// EnrolTOTP drives the settings flow to enrol a genuine TOTP factor and
// returns the base32 secret Kratos generated.
func EnrolTOTP(ctx context.Context, publicURL, sessionToken string) (string, error) {
	base := strings.TrimSuffix(publicURL, "/")
	var flow kratosFlow
	if err := doJSON(ctx, http.MethodGet, base+"/self-service/settings/api", sessionToken, nil, &flow); err != nil {
		return "", fmt.Errorf("init settings flow: %w", err)
	}

	var secret string
	for _, n := range flow.UI.Nodes {
		if n.Attributes.ID == "totp_secret_key" {
			secret = n.Attributes.Text.Context.Secret
			break
		}
	}
	if secret == "" {
		return "", fmt.Errorf("settings flow %s exposed no totp_secret_key node — is selfservice.methods.totp.enabled true and a settings ui_url configured?", flow.ID)
	}

	code, err := codeForSubmission(secret)
	if err != nil {
		return "", err
	}
	var done kratosFlow
	body := map[string]interface{}{"method": "totp", "totp_code": code}
	if err := doJSON(ctx, http.MethodPost, base+"/self-service/settings?flow="+flow.ID, sessionToken, body, &done); err != nil {
		return "", fmt.Errorf("submit totp enrolment: %w", err)
	}
	if done.State != "success" {
		return "", fmt.Errorf("totp enrolment ended in state %q, want success", done.State)
	}
	return secret, nil
}

// StepUpAAL2 performs the refresh login leg that a step-up challenge sends an
// operator through, and reports the resulting AAL plus the per-method ledger
// the console reads to decide freshness.
func StepUpAAL2(ctx context.Context, publicURL string, id *TOTPIdentity) (string, []AuthMethod, error) {
	code, err := codeForSubmission(id.Secret)
	if err != nil {
		return "", nil, err
	}
	return stepUpWithCode(ctx, publicURL, id, code)
}

// stepUpWithCode is StepUpAAL2 with the code supplied, so a test can drive the
// same leg with a deliberately wrong one and prove Kratos refuses it.
func stepUpWithCode(ctx context.Context, publicURL string, id *TOTPIdentity, code string) (string, []AuthMethod, error) {
	base := strings.TrimSuffix(publicURL, "/")
	var flow kratosFlow
	if err := doJSON(ctx, http.MethodGet, base+"/self-service/login/api?aal=aal2&refresh=true", id.SessionToken, nil, &flow); err != nil {
		return "", nil, fmt.Errorf("init aal2 login flow: %w", err)
	}
	var env sessionEnvelope
	body := map[string]interface{}{"method": "totp", "totp_code": code}
	if err := doJSON(ctx, http.MethodPost, base+"/self-service/login?flow="+flow.ID, id.SessionToken, body, &env); err != nil {
		return "", nil, fmt.Errorf("submit aal2 totp: %w", err)
	}
	methods := make([]AuthMethod, 0, len(env.Session.AuthenticationMethods))
	for _, m := range env.Session.AuthenticationMethods {
		methods = append(methods, AuthMethod{Method: m.Method, AAL: m.AAL, CompletedAt: m.CompletedAt})
	}
	return env.Session.AAL, methods, nil
}

// AuthMethod mirrors one entry of the Kratos session's authentication_methods
// ledger — the signal the console reads live to determine AAL and freshness.
type AuthMethod struct {
	Method      string
	AAL         string
	CompletedAt time.Time
}
