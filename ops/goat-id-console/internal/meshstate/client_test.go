package meshstate

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// staticJSONRoundTripper returns a fixed JSON body with HTTP 200 for
// any request. Mirrors the pattern used by enrollwatch tests.

type staticJSONRoundTripper struct {
	body   string
	status int
}

func (rt staticJSONRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	status := rt.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(rt.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// timeoutRoundTripper waits for the request context to be done and
// returns its error, exercising the per-call timeout plumbing without
// sleeping in real time.
type timeoutRoundTripper struct{}

func (timeoutRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestClientPeers_SuccessShape(t *testing.T) {
	cli := &Client{
		MgmtURL: "https://netbird.example.test",
		Token:   "t",
		HTTP: &http.Client{Transport: staticJSONRoundTripper{
			body: `[{"id":"p-1","name":"peer-alice-zagros-2026-09-01","connected":true,
                      "last_seen":"2026-09-10T10:00:00Z","groups":[{"id":"g1","name":"All"}]}]`,
		}},
	}
	peers, err := cli.Peers(context.Background())
	if err != nil {
		t.Fatalf("Peers error = %v", err)
	}
	if len(peers) != 1 || peers[0].ID != "p-1" {
		t.Fatalf("Peers = %+v, want one peer with ID p-1", peers)
	}
}

func TestClientPeers_UnreadableLastSeenKeepsEveryPeer(t *testing.T) {
	cli := &Client{
		MgmtURL: "https://netbird.example.test",
		Token:   "t",
		HTTP: &http.Client{Transport: staticJSONRoundTripper{
			body: `[
				{"id":"p-good","name":"good","last_seen":"2026-09-10T10:00:00Z","groups":[{"id":"g1","name":"All"}]},
				{"id":"p-empty","name":"empty","last_seen":"","groups":[{"id":"g2","name":"ZAGROS-QUARANTINED"}]},
				{"id":"p-bad","name":"bad","last_seen":"not-a-time","groups":[{"id":"g3","name":"ZAGROS-PROBATION-DEVELOPER"}]},
				{"id":"p-missing","name":"missing","groups":[]}
			]`,
		}},
	}
	peers, err := cli.Peers(context.Background())
	if err != nil {
		t.Fatalf("Peers error = %v", err)
	}
	if len(peers) != 4 {
		t.Fatalf("Peers = %+v, want all four peers", peers)
	}
	if peers[0].LastSeen.IsZero() || peers[0].LastSeenUnreadable {
		t.Errorf("valid timestamp = %+v, want parsed and readable", peers[0])
	}
	for _, i := range []int{1, 2} {
		if !peers[i].LastSeen.IsZero() || !peers[i].LastSeenUnreadable {
			t.Errorf("unreadable timestamp peer = %+v, want zero time and unreadable flag", peers[i])
		}
	}
	if !peers[3].LastSeen.IsZero() || peers[3].LastSeenUnreadable {
		t.Errorf("missing timestamp peer = %+v, want zero time without unreadable flag", peers[3])
	}
	if got := peers[2].Groups; len(got) != 1 || got[0].Name != "ZAGROS-PROBATION-DEVELOPER" {
		t.Errorf("bad timestamp peer groups = %+v, want preserved group", got)
	}
}

func TestClientPeers_Non200Error(t *testing.T) {
	cli := &Client{
		MgmtURL: "https://netbird.example.test",
		Token:   "t",
		HTTP: &http.Client{Transport: staticJSONRoundTripper{
			status: http.StatusForbidden,
			body:   "forbidden",
		}},
	}
	_, err := cli.Peers(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("Peers error = %v, want HTTP 403", err)
	}
}

func TestClientPeers_TimeoutHonoursContext(t *testing.T) {
	cli := &Client{
		MgmtURL: "https://netbird.example.test",
		Token:   "t",
		HTTP:    &http.Client{Transport: timeoutRoundTripper{}},
	}
	// Canceled parent context forces the per-call timeout path
	// to exit immediately instead of sleeping 5 seconds.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := cli.Peers(ctx)
	if err == nil {
		t.Fatalf("Peers error = nil, want context error")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("Peers took too long under canceled context")
	}
}

func TestClientSetupKeys_SuccessShape(t *testing.T) {
	cli := &Client{
		MgmtURL: "https://netbird.example.test",
		Token:   "t",
		HTTP: &http.Client{Transport: staticJSONRoundTripper{
			body: `[{"id":"k-1","name":"peer-alice-zagros-2026-09-01",
                      "used_times":1,"revoked":false,"valid":true,
                      "expires":"2026-10-01T00:00:00Z","state":"active"}]`,
		}},
	}
	keys, err := cli.SetupKeys(context.Background())
	if err != nil {
		t.Fatalf("SetupKeys error = %v", err)
	}
	if len(keys) != 1 || keys[0].ID != "k-1" || keys[0].Expires.IsZero() {
		t.Fatalf("SetupKeys = %+v, want one key with ID k-1 and non-zero Expires", keys)
	}
}

func TestClientSetupKeys_Non200Error(t *testing.T) {
	cli := &Client{
		MgmtURL: "https://netbird.example.test",
		Token:   "t",
		HTTP: &http.Client{Transport: staticJSONRoundTripper{
			status: http.StatusBadGateway,
			body:   "upstream error",
		}},
	}
	_, err := cli.SetupKeys(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("SetupKeys error = %v, want HTTP 502", err)
	}
}

func TestClientSetupKeys_TimeoutHonoursContext(t *testing.T) {
	cli := &Client{
		MgmtURL: "https://netbird.example.test",
		Token:   "t",
		HTTP:    &http.Client{Transport: timeoutRoundTripper{}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := cli.SetupKeys(ctx)
	if err == nil {
		t.Fatalf("SetupKeys error = nil, want context error")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("SetupKeys took too long under canceled context")
	}
}

func TestClientPeers_EmptyToken(t *testing.T) {
	cli := &Client{
		MgmtURL: "https://netbird.example.test",
		Token:   "  \t",
	}
	_, err := cli.Peers(context.Background())
	if err == nil || !strings.Contains(err.Error(), "token is empty") {
		t.Fatalf("Peers error = %v, want empty token error", err)
	}
}

func TestClientPeers_DecodeError(t *testing.T) {
	cli := &Client{
		MgmtURL: "https://netbird.example.test",
		Token:   "t",
		HTTP: &http.Client{Transport: staticJSONRoundTripper{
			body: `[{"id":"p-1", "name":`, // malformed JSON
		}},
	}
	_, err := cli.Peers(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode peers") {
		t.Fatalf("Peers error = %v, want decode peers error", err)
	}
}

func TestClientSetupKeys_EmptyToken(t *testing.T) {
	cli := &Client{
		MgmtURL: "https://netbird.example.test",
		Token:   "  \t",
	}
	_, err := cli.SetupKeys(context.Background())
	if err == nil || !strings.Contains(err.Error(), "token is empty") {
		t.Fatalf("SetupKeys error = %v, want empty token error", err)
	}
}

func TestClientSetupKeys_DecodeError(t *testing.T) {
	cli := &Client{
		MgmtURL: "https://netbird.example.test",
		Token:   "t",
		HTTP: &http.Client{Transport: staticJSONRoundTripper{
			body: `[{"id":"k-1", "name":`, // malformed JSON
		}},
	}
	_, err := cli.SetupKeys(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode setup keys") {
		t.Fatalf("SetupKeys error = %v, want decode setup keys error", err)
	}
}

// The PAT rides every request, and Go keeps Authorization on a
// same-host redirect, so the client refuses to follow any redirect:
// the 3xx is reported as an error and the target never sees the token.
func TestClient_RefusesRedirects(t *testing.T) {
	var tokenAtTarget atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/peers", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	mux.HandleFunc("/api/setup-keys", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			tokenAtTarget.Store(true)
		}
		_, _ = w.Write([]byte("[]"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, withHTTP := range []bool{false, true} {
		cli := &Client{MgmtURL: srv.URL, Token: "t"}
		if withHTTP {
			cli.HTTP = srv.Client()
		}
		if _, err := cli.Peers(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
			t.Fatalf("Peers (HTTP set: %v) err = %v, want HTTP 302", withHTTP, err)
		}
		if _, err := cli.SetupKeys(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
			t.Fatalf("SetupKeys (HTTP set: %v) err = %v, want HTTP 302", withHTTP, err)
		}
	}
	if tokenAtTarget.Load() {
		t.Fatal("the redirect target received the token")
	}
}
