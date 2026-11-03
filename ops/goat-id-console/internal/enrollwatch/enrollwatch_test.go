package enrollwatch

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/audit"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
)

// fakeStoreHolder is a non-nil *Store used only to exercise the Enabled()
// input checks; no method on it is called.
var fakeStoreHolder onboardstore.Store

// staticJSONRoundTripper is a minimal RoundTripper that returns a fixed JSON
// body with HTTP 200 for any request. Used by tests to drive Watcher.tick
// without binding a real listener, which is disallowed in some environments.
type staticJSONRoundTripper struct {
	body string
}

func (rt staticJSONRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(rt.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// The matcher is the whole correctness surface of this package: everything
// else is polling and logging. It must answer "did THIS person use THEIR key"
// without ever crediting a different person's enrollment.
func TestUsedByHandle(t *testing.T) {
	keys := []setupKey{
		// Used, belongs to alice → alice is enrolled.
		{Name: "peer-alice-usareur-af-zagros-prod-2026-08-04", UsedTimes: 1},
		// Minted for bob, never used → bob is NOT enrolled.
		{Name: "peer-bob-usareur-af-zagros-prod-2026-08-04", UsedTimes: 0},
		// Used and later revoked → still proof carol did enroll.
		{Name: "peer-carol-zagros-prod-2026-08-04", UsedTimes: 2, Revoked: true},
		// `dave-2` is a DIFFERENT person whose handle extends `dave` by a
		// hyphenated segment, and their key IS used. `dave` has no key at all.
		// Under the old `peer-<handle>-` match, dave inherited this.
		{Name: "peer-dave-2-zagros-prod-2026-08-04", UsedTimes: 1},
	}

	known := map[string]struct{}{
		"alice": {}, "bob": {}, "carol": {}, "dave": {}, "dave-2": {},
	}

	for _, tc := range []struct {
		name   string
		handle string
		want   bool
		why    string
	}{
		{"used key", "alice", true, "a used key is the enrollment signal"},
		{"minted but unused", "bob", false, "an unused key means the package never landed"},
		{"used then revoked", "carol", true, "revocation must not walk the badge backwards"},
		{"no key at all", "dave", false,
			"dave has NO key; dave-2's used key must not be read as dave's"},
		// The prefix guard: `peer-<handle>-` must not match a LONGER handle that
		// merely starts with the same letters, or one person's enrollment would
		// silently mark another person enrolled.
		{"handle is a prefix of another", "alic", false, "alic must not inherit alice's key"},
		// ...nor may a handle match as a mid-name substring.
		{"handle appears mid-name", "usareur-af", false, "an org tag is not a person"},
		// The `alic`/`alice` row above fences a prefix with NO separator. This row
		// fences a handle extended by a HYPHENATED segment: without the goatnet anchor,
		// `peer-ada-2-<goatnet>-…` starts with `peer-ada-`, so the shorter handle would
		// inherit the longer one's used key. Anchoring the prefix on the goatnet closes
		// it, because the longer handle's extra segment sits BEFORE the goatnet.
		{"handle extended by a hyphenated segment", "dave-2", true,
			"dave-2 owns this key and must be matched"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := usedByHandle(keys, tc.handle, known); got != tc.want {
				t.Errorf("usedByHandle(%q) = %v, want %v — %s", tc.handle, got, tc.want, tc.why)
			}
		})
	}
}

// A watcher missing any of its inputs must report itself disabled rather than
// half-running: a watcher that polls with no token would log an auth failure
// every tick forever, and one with no store has nothing to write.
func TestEnabledRequiresEveryInput(t *testing.T) {
	var nilWatcher *Watcher
	if nilWatcher.Enabled() {
		t.Error("nil watcher must not report enabled")
	}
	if (&Watcher{MgmtURL: "https://x", Token: "t"}).Enabled() {
		t.Error("watcher with no store must not report enabled")
	}
	if (&Watcher{Store: &fakeStoreHolder, Token: "t"}).Enabled() {
		t.Error("watcher with no mgmt URL must not report enabled")
	}
	if (&Watcher{Store: &fakeStoreHolder, MgmtURL: "https://x"}).Enabled() {
		t.Error("watcher with no token must not report enabled — default-off is the contract")
	}
	if !(&Watcher{Store: &fakeStoreHolder, MgmtURL: "https://x", Token: "t"}).Enabled() {
		t.Error("fully configured watcher must report enabled")
	}
}

// The flip is the one lifecycle transition nobody decides — so the audit row
// is the only record it happened at all. It must carry the before/after states
// and attribute the watcher, not a person.
func TestTickAuditsTheFlip(t *testing.T) {
	store, err := onboardstore.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		_ = store.Close() // best-effort close in tests; open failure already fatal
	}()
	ctx := context.Background()
	p := &onboardstore.Proposal{ID: "p-1", Sponsor: "s@example.org", Env: "zagros", Handle: "alice",
		Email: "alice@example.org", Status: onboardstore.ProposalOnboarded, ManifestJSON: "{}", Complete: true}
	if err := store.CreateProposal(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.SetProposalResult(ctx, p.ID, onboardstore.ProposalOnboarded, "", "", "", "", "", false); err != nil {
		t.Fatalf("onboarded: %v", err)
	}

	client := &http.Client{Transport: staticJSONRoundTripper{
		body: `[{"name":"peer-alice-zagros-2026-08-11","used_times":1}]`,
	}}

	em := audit.NewStderrEmitter(10)
	w := &Watcher{Store: store, MgmtURL: "https://netbird.example", Token: "t", Client: client,
		Log: slog.Default(), Audit: em}
	w.tick(ctx)

	evs := em.Recent(10)
	if len(evs) != 1 || evs[0].Action != "onboard_enrolled" {
		t.Fatalf("want one onboard_enrolled event, got %+v", evs)
	}
	if evs[0].Actor.Email != "enroll-watch" {
		t.Fatalf("actor = %q, want enroll-watch (observed, not decided)", evs[0].Actor.Email)
	}
	if evs[0].After["status"] != onboardstore.ProposalEnrolled || evs[0].Target.Email != "alice@example.org" {
		t.Fatalf("event does not describe the flip: %+v", evs[0])
	}

	// A second pass has nothing to flip, so it must not re-announce one.
	w.tick(ctx)
	if got := len(em.Recent(10)); got != 1 {
		t.Fatalf("re-tick emitted %d events, want 1 — the flip is one-way", got)
	}
}

// Peer presence is the second line of evidence, because the first has a blind
// spot: somebody who joined on a key that was later replaced, or one named
// outside the grammar, holds a peer while every key bearing their handle still
// reads unredeemed, and nothing else would ever move that row.
func TestPeerByHandle(t *testing.T) {
	peers := []peer{
		{Name: "peer-alice-zagros-2026-06-10-Alices-MacBook.local"},
		{Name: "goat-tak-01"}, // infrastructure, attributable to nobody
		{Name: "peer-bob-2-zagros-2026-06-10-thing"},
	}
	if !peerByHandle(peers, "alice", map[string]struct{}{"alice": {}, "bob": {}, "bob-2": {}}) {
		t.Error("alice owns a peer and was not matched")
	}
	// A shorter handle must not inherit a longer one's peer — the same
	// near-miss the key matcher is fenced against.
	if peerByHandle(peers, "bob", map[string]struct{}{"alice": {}, "bob": {}, "bob-2": {}}) {
		t.Error("`bob` matched `bob-2`'s peer: the prefix must include the separator")
	}
	if peerByHandle(peers, "carol", map[string]struct{}{"alice": {}, "bob": {}, "bob-2": {}}) {
		t.Error("carol owns nothing and was matched")
	}
	// A name carrying no grammar attributes to nobody rather than to everybody.
	if peerByHandle(peers, "goat-tak-01", map[string]struct{}{"alice": {}, "bob": {}, "bob-2": {}}) {
		t.Error("an off-grammar peer name must not attribute")
	}
}
