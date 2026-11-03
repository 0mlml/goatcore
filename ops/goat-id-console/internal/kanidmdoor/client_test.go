package kanidmdoor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// doorStub is a Kanidm-shaped server: the three-step auth dance, a person list,
// and an intent mint whose response body the test controls.
func doorStub(t *testing.T, persons []map[string][]string, intent string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-kanidm-auth-session-id", "sid-1")
		_, _ = w.Write([]byte(`{"state":{"success":"bearer-1"}}`))
	})
	mux.HandleFunc("/v1/person", func(w http.ResponseWriter, r *http.Request) {
		out := []map[string]any{}
		for _, p := range persons {
			out = append(out, map[string]any{"attrs": p})
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/v1/person/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(intent))
	})
	return httptest.NewServer(mux)
}

func TestFindByEmailMatchesOnMailAttribute(t *testing.T) {
	srv := doorStub(t, []map[string][]string{
		{"name": {"someone-else"}, "mail": {"other@example.org"}},
		{"name": {"user167x"}, "mail": {"Jean@Example.ORG"}},
	}, `{}`)
	defer srv.Close()
	c := New(srv.URL, "pw")

	// Case-insensitive: Kanidm stores what it was given, the session gives
	// whatever the IdP mapped, and a case mismatch must not read as "no account".
	got, err := c.FindByEmail(context.Background(), "jean@example.org")
	if err != nil || got == nil {
		t.Fatalf("expected a match, got %v err=%v", got, err)
	}
	if got.Handle != "user167x" {
		t.Fatalf("matched the wrong account: %q", got.Handle)
	}
	if got.HasCredential {
		t.Fatal("no primary_credential in attrs, so HasCredential must be false")
	}
}

func TestFindByEmailAbsentIsNotAnError(t *testing.T) {
	srv := doorStub(t, []map[string][]string{{"name": {"a"}, "mail": {"a@x.test"}}}, `{}`)
	defer srv.Close()
	got, err := New(srv.URL, "pw").FindByEmail(context.Background(), "nobody@x.test")
	if err != nil {
		t.Fatalf("absent account must not be an error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for an absent account, got %+v", got)
	}
}

func TestFindByEmailDetectsExistingCredential(t *testing.T) {
	srv := doorStub(t, []map[string][]string{
		{"name": {"has-passkey"}, "mail": {"p@x.test"}, "passkeys": {"blob"}},
	}, `{}`)
	defer srv.Close()
	got, _ := New(srv.URL, "pw").FindByEmail(context.Background(), "p@x.test")
	if got == nil || !got.HasCredential {
		t.Fatal("a passkey counts as a credential — this is what makes the page say reset rather than set")
	}
}

func TestMintCodeUsesServerExpiryNotTheRequestedOne(t *testing.T) {
	// The whole point: Kanidm clamps to 24h and tells you so in expiry_time.
	// Asking for a week and reporting a week to the reader is the bug.
	exp := time.Now().Add(24 * time.Hour).Unix()
	srv := doorStub(t, nil, `{"token":"aB3dE-fG7hI-jK1lM-nO5pQ","expiry_time":`+itoa(exp)+`}`)
	defer srv.Close()

	got, err := New(srv.URL, "pw").MintCode(context.Background(), "someone", 168*time.Hour)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if d := time.Until(got.ExpiresAt); d > 25*time.Hour {
		t.Fatalf("reported an expiry the server did not grant: %v away", d)
	}
	if strings.Contains(got.ResetPage, "token") {
		t.Fatalf("the reset page must be BARE — a tokenised URL is spent by scanners "+
			"and jams the submit button; got %q", got.ResetPage)
	}
	if got.Code == "" || strings.Contains(got.ResetPage, got.Code) {
		t.Fatal("the code must never appear inside the page address")
	}
}

func TestMintCodeRefusesBareStringRefusals(t *testing.T) {
	// Kanidm answers an unknown account with HTTP 200 and the bare JSON string
	// "nomatchingentries". A caller that trusts the status hands that word to a
	// person as their setup code.
	for _, body := range []string{`"nomatchingentries"`, `"accessdenied"`, `null`, ``} {
		srv := doorStub(t, nil, body)
		got, err := New(srv.URL, "pw").MintCode(context.Background(), "ghost", time.Hour)
		srv.Close()
		if err == nil {
			t.Fatalf("body %q must be refused, got code %+v", body, got)
		}
		if got != nil {
			t.Fatalf("body %q must yield no code", body)
		}
	}
}

func TestUnconfiguredDoorFailsSoftly(t *testing.T) {
	c := New("", "")
	if c.Configured() {
		t.Fatal("an empty door must not report configured")
	}
	if _, err := c.FindByEmail(context.Background(), "a@b.test"); err == nil {
		t.Fatal("an unconfigured door must return an error, not panic or succeed")
	}
	if _, err := c.MintCode(context.Background(), "a", time.Hour); err == nil {
		t.Fatal("an unconfigured door must refuse to mint")
	}
}

func itoa(v int64) string {
	out := ""
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		out = string(rune('0'+v%10)) + out
		v /= 10
	}
	if out == "" {
		out = "0"
	}
	if neg {
		out = "-" + out
	}
	return out
}
