// Package enrollwatch observes whether an onboarded person actually USED the
// mesh credential minted for them, and flips their proposal `onboarded` →
// `enrolled`.
//
// Why this exists. Every other transition in the onboarding state machine is a
// decision somebody made: a sponsor vouched, an admin approved, a runner
// minted. `onboarded` therefore means "we minted credentials and emailed them"
// — it says nothing about whether the package landed, whether the person could
// follow it, or whether the setup key ever got used. Operators kept asking a
// question the console could not answer: did this actually work for them?
//
// The answer is observable rather than reportable. NetBird counts uses on the
// setup key the runner minted, so a key with `used_times > 0` is proof the
// person ran `netbird up` with the credential we sent. That is the signal this
// watcher polls, and `enrolled` is where it writes the answer.
//
// Deliberate properties:
//
//   - DEFAULT OFF. No mgmt URL or no token ⇒ the watcher never starts, and
//     every proposal stays at `onboarded` exactly as before. A goatnet that
//     hasn't wired the token loses a nicety, never correctness.
//   - READ-ONLY against NetBird. It lists setup keys; it never mints, revokes
//     or edits. The blast radius of a bad token is a wrong badge.
//   - ONE-WAY. `MarkEnrolled` only matches rows currently `onboarded`, so the
//     watcher can never resurrect a rejected row or overwrite a later state.
//   - SILENT ON FAILURE IS NOT ALLOWED. A poll that cannot reach NetBird logs
//     at warn and changes nothing; it must never be mistaken for "nobody has
//     enrolled", which is the honesty rule the fabric-health surface follows.
//
// Matching is by setup-key NAME, not value: the NetBird API returns a key's
// value only at creation, and the name is deterministic — the peer-name grammar
// (ops/identity/peer-name-grammar.yaml) is `peer-<handle>-<goatnet>-YYYY-MM-DD`.
//
// The prefix includes the GOATNET, and that is not cosmetic. `peer-<handle>-`
// alone is NOT unambiguous: a handle that is a prefix of another handle inherits
// its artifacts, so `ada` would match `ada-2`'s key.
package enrollwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/audit"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/meshstate"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// setupKey is the subset of NetBird's setup-key shape this watcher reads.
type setupKey struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UsedTimes int    `json:"used_times"`
	Revoked   bool   `json:"revoked"`
}

// Watcher polls a NetBird management API for setup-key usage.
type Watcher struct {
	Store    *onboardstore.Store
	MgmtURL  string        // e.g. https://netbird.example.net
	Token    string        // NetBird PAT; empty ⇒ disabled
	Interval time.Duration // poll period
	Client   *http.Client
	Log      *slog.Logger
	// Audit receives the onboarded → enrolled flip. Optional (nil = log only),
	// but without it the ONE observed transition in the lifecycle is the only
	// state change with no row in /audit: the queue moves and nothing records
	// who moved it or when.
	Audit audit.Emitter
}

// Enabled reports whether the watcher has everything it needs to run. A
// disabled watcher is a supported configuration, not an error.
func (w *Watcher) Enabled() bool {
	return w != nil && w.Store != nil && w.MgmtURL != "" && w.Token != ""
}

// Run polls until ctx is canceled. Returns immediately when disabled, so the
// caller can start it unconditionally.
func (w *Watcher) Run(ctx context.Context) {
	if !w.Enabled() {
		return
	}
	if w.Interval <= 0 {
		w.Interval = 5 * time.Minute
	}
	if w.Client == nil {
		w.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if w.Log == nil {
		w.Log = slog.Default()
	}
	w.Log.Info("enrollment watcher started",
		slog.String("mgmt_url", w.MgmtURL),
		slog.Duration("interval", w.Interval))

	t := time.NewTicker(w.Interval)
	defer t.Stop()
	// Tick once up front so a restart reconciles immediately rather than after
	// a full interval of showing stale state.
	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

// tick performs one reconciliation pass. Errors are logged, never fatal — a
// watcher that dies on a transient API blip would silently stop answering the
// question it exists to answer.
func (w *Watcher) tick(ctx context.Context) {
	pending, err := w.Store.ListProposals(ctx, onboardstore.ProposalOnboarded)
	if err != nil {
		w.Log.Warn("enrollment watch: list onboarded proposals failed", slog.Any("error", err))
		return
	}
	if len(pending) == 0 {
		return
	}
	keys, err := w.fetchSetupKeys(ctx)
	if err != nil {
		// Loud, and explicitly NOT "nobody enrolled": the rows keep their
		// current state and an operator can see why the answer didn't move.
		w.Log.Warn("enrollment watch: NetBird setup-key list failed; enrollment state UNMEASURED this tick",
			slog.Any("error", err))
		return
	}
	// Peers are a SUPPLEMENT, so a failure here is not fatal to the tick: the
	// key signal still works and losing the second one must not stop the first.
	// It is logged, because silently dropping half the evidence would make the
	// remaining answer look more complete than it is.
	peers, perr := w.fetchPeers(ctx)
	if perr != nil {
		w.Log.Warn("enrollment watch: NetBird peer list failed; enrolment will be judged on "+
			"setup-key usage alone this tick, so somebody who joined on a replaced or "+
			"off-grammar key stays UNMEASURED rather than enrolled",
			slog.Any("error", perr))
	}
	// Every handle the console knows, not just the pending ones: the handle that
	// steals an artifact is usually NOT in the pending set (it already enrolled),
	// so building this from `pending` alone would leave the ambiguity in place.
	known, kerr := meshstate.KnownHandles(ctx, w.Store, w.Log)
	if kerr != nil {
		w.Log.Warn("enrollment watch: full handle roster unavailable; a name may be credited to a handle that merely prefixes it this tick",
			slog.Any("error", kerr))
	}
	for _, p := range pending {
		if p.Handle == "" {
			continue
		}
		viaKey := usedByHandle(keys, p.Handle, known)
		viaPeer := perr == nil && peerByHandle(peers, p.Handle, known)
		if !viaKey && !viaPeer {
			continue
		}
		evidence := "setup-key-used"
		if !viaKey {
			// Worth naming in the log and the audit: this row moved on peer
			// presence alone, which means the key we minted never recorded a use
			// and the person got on by some other route.
			evidence = "peer-present-without-key-use"
		}
		ok, err := w.Store.MarkEnrolled(ctx, p.ID)
		if err != nil {
			w.Log.Warn("enrollment watch: mark enrolled failed",
				slog.String("id", p.ID), slog.String("handle", p.Handle), slog.Any("error", err))
			continue
		}
		if ok {
			w.Log.Info("enrolled: the person is on the mesh",
				slog.String("id", p.ID), slog.String("handle", p.Handle),
				slog.String("env", p.Env), slog.String("evidence", evidence))
			w.auditEnrolled(ctx, p, evidence)
		}
	}
}

// auditEnrolled records the flip. The actor is the watcher itself, not a
// person: nobody decided this — the watcher OBSERVED that the credential was
// used, and the audit row must not read as an operator action.
func (w *Watcher) auditEnrolled(ctx context.Context, p onboardstore.Proposal, evidence string) {
	if w.Audit == nil {
		return
	}
	w.Audit.Emit(ctx, audit.Event{
		Actor:  audit.Actor{Email: "enroll-watch"},
		Action: "onboard_enrolled",
		Target: audit.Target{Email: p.Email},
		Before: map[string]interface{}{"status": onboardstore.ProposalOnboarded},
		After: map[string]interface{}{"status": onboardstore.ProposalEnrolled,
			"handle": p.Handle, "env": p.Env, "proposal_id": p.ID,
			// WHICH signal moved this row. `peer-present-without-key-use` means
			// the person is on the mesh but the key we minted never recorded a
			// use — they joined by some other route, and that is worth being
			// able to find in the audit rather than inferring later.
			"evidence": evidence},
		Result: "ok",
	})
}

// usedByHandle reports whether any setup key belonging to this handle has been
// used at least once. Revoked keys still count: a key that was used and later
// revoked is proof the person did enroll, and forgetting that would walk the
// badge backwards.
func usedByHandle(keys []setupKey, handle string, known map[string]struct{}) bool {
	for _, k := range keys {
		if k.UsedTimes > 0 && meshstate.OwnsName(k.Name, handle, known) {
			return true
		}
	}
	return false
}

// peer is the subset of NetBird's peer shape this watcher reads.
type peer struct {
	Name string `json:"name"`
}

// peerByHandle reports whether the person owns a peer.
//
// THE SECOND LINE OF EVIDENCE, because the first one has a blind spot.
// `used_times` is a property of the KEY we minted; somebody who joined on a key
// that was later replaced, or on one named outside the peer-name grammar, holds
// a peer while every key bearing their handle still reads unredeemed, and
// nothing else would ever move that row.
//
// Same prefix rule as the key match, and the same limit: a peer whose name does
// not carry the grammar cannot be attributed here, so this ADDS evidence and
// never subtracts it. A handle with neither signal stays where it is.
func peerByHandle(peers []peer, handle string, known map[string]struct{}) bool {
	for _, p := range peers {
		if meshstate.OwnsName(p.Name, handle, known) {
			return true
		}
	}
	return false
}

func (w *Watcher) fetchPeers(ctx context.Context) ([]peer, error) {
	url := strings.TrimSuffix(w.MgmtURL, "/") + "/api/peers"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+w.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := w.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	var peers []peer
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		return nil, fmt.Errorf("decode peers: %w", err)
	}
	return peers, nil
}

func (w *Watcher) fetchSetupKeys(ctx context.Context) ([]setupKey, error) {
	url := strings.TrimSuffix(w.MgmtURL, "/") + "/api/setup-keys"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+w.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := w.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	var keys []setupKey
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		return nil, fmt.Errorf("decode setup keys: %w", err)
	}
	return keys, nil
}
