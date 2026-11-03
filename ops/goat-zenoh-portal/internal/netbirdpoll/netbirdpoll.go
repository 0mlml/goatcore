// Package netbirdpoll polls the netbird-mgmt API for peer rows and
// matches their names against the portal's vendor list to drive the
// L2 column of the vendor-progress swimlane (Block 88).
//
// AutoPeerName contract: operator-onboarded peers land with a `name` that
// follows the peer-name grammar (ops/identity/peer-name-grammar.yaml, generated
// into internal/peername). We only need the org-keyed leading segment to
// attribute a peer to a vendor row.
//
// The poller degrades to a no-op when MgmtURL or Token are empty so
// the rest of the portal continues to run unchanged (L2 column shows
// "—" for every vendor; no spurious errors).
package netbirdpoll

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/peername"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// accessCacheTTL is how long the synchronous access-COP path (PeersForAccess)
// serves the last peer snapshot without a live fetch. Set above the default 60s
// background poll interval so a healthy poller always satisfies the access path
// from cache — the access COP never blocks on netbird-mgmt. A var (not const)
// only so tests can exercise the stale path deterministically.
var accessCacheTTL = 90 * time.Second

// accessFetchTimeout bounds a cold/stale access-path fetch so a slow or
// unreachable netbird-mgmt costs seconds, not the full 15s client timeout, on
// every /api/access/* call (the degraded-mesh symptom).
const accessFetchTimeout = 5 * time.Second

// Client talks to a netbird-mgmt API. Constructed once at startup.
type Client struct {
	mgmtURL string
	token   string
	http    *http.Client

	// Peer snapshot cache. Every successful ListPeers stores here (so the
	// background poller warms it), and PeersForAccess reads it so the
	// synchronous access-COP path is decoupled from live netbird-mgmt latency.
	mu       sync.Mutex
	cached   []Peer
	cachedAt time.Time
}

// New returns a Client. mgmtURL must include scheme; trailing slashes
// are tolerated. token is the PAT (Bearer); empty disables the client.
func New(mgmtURL, token string) *Client {
	return &Client{
		mgmtURL: strings.TrimRight(mgmtURL, "/"),
		token:   token,
		http: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Enabled returns true when the client has the bits it needs to make a
// real request. Disabled clients silently no-op in Poll().
func (c *Client) Enabled() bool {
	return c != nil && c.mgmtURL != "" && c.token != ""
}

// Peer is the slim netbird-mgmt /api/peers shape we care about. NetBird's
// real payload has more fields; encoding/json ignores anything we don't
// reference. LastSeen is RFC3339 (the mgmt API's format); Connected is the
// live-reachability flag — both feed the access COP's last-activity dimension.
type Peer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	LastSeen  string `json:"last_seen"`
	Connected bool   `json:"connected"`
}

// LastSeenUnix parses the RFC3339 LastSeen into a unix timestamp, or 0 when
// it is absent / unparseable (treated as unknown, never a confident zero).
func (p Peer) LastSeenUnix() int64 {
	if p.LastSeen == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, p.LastSeen)
	if err != nil {
		return 0
	}
	return t.Unix()
}

// ListPeers returns the full peer roster. The mgmt API returns at most
// a few hundred entries per site, comfortably inside a single request.
func (c *Client) ListPeers(ctx context.Context) ([]Peer, error) {
	if !c.Enabled() {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.mgmtURL+"/api/peers", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("netbird mgmt GET /api/peers: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read peers body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("netbird mgmt /api/peers returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var peers []Peer
	if err := json.Unmarshal(body, &peers); err != nil {
		return nil, fmt.Errorf("parse peers JSON: %w", err)
	}
	c.store(peers)
	return peers, nil
}

// store caches the latest successful peer snapshot. Called from every
// successful ListPeers, so the background poller's periodic fetch keeps the
// access-COP cache warm.
func (c *Client) store(peers []Peer) {
	c.mu.Lock()
	c.cached, c.cachedAt = peers, time.Now()
	c.mu.Unlock()
}

func (c *Client) snapshot() ([]Peer, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cached, c.cachedAt
}

// PeersForAccess returns the peer roster for the synchronous access-COP path,
// bounded so a slow/unreachable netbird-mgmt never serializes its full latency
// onto every /api/access/* call (the degraded-mesh symptom this fixes). It
// serves the cache when fresh — kept warm by the background poller — and on a
// stale/cold cache tries one short-timeout fetch; if that fails it returns the
// last-known snapshot with a staleness warning rather than blocking or
// fabricating an empty roster. `enabled` is false only when no mgmt is
// configured (the honest "mesh layer unavailable" case, distinct from a fetch
// failure).
func (c *Client) PeersForAccess(ctx context.Context) (peers []Peer, warning string, enabled bool) {
	if !c.Enabled() {
		return nil, "", false
	}
	cached, at := c.snapshot()
	if !at.IsZero() && time.Since(at) < accessCacheTTL {
		return cached, "", true
	}
	fetchCtx, cancel := context.WithTimeout(ctx, accessFetchTimeout)
	defer cancel()
	if fresh, err := c.ListPeers(fetchCtx); err == nil {
		return fresh, "", true
	}
	if len(cached) > 0 {
		age := time.Since(at).Round(time.Second)
		return cached, fmt.Sprintf(
			"netbird mgmt slow/unreachable — mesh posture is stale (peer snapshot %s old)", age), true
	}
	return nil, "netbird mgmt slow/unreachable — mesh posture omitted this call (peers + on-mesh edges unavailable)", true
}

// Poll fetches the current peer list, attributes each peer to an org row, and
// updates vendor_progress (MarkL2). Returns the number of vendors whose row
// was touched.
//
// Attribution keys on BOTH forms the peer-name grammar can carry: the org
// prefix (per-vendor goatnets, where the prefix IS the org name and appears in
// the name) and the person handle (slot-pool goatnets, where the org row is
// keyed by a 32-hex anchor that appears in no peer name). Offering only
// prefixes made L2 structurally unreachable on every per-uuid goatnet — the
// column read "no peers" for people who were demonstrably enrolled.
func (c *Client) Poll(ctx context.Context, reg *registry.Registry) (int, error) {
	if !c.Enabled() {
		return 0, nil
	}
	peers, err := c.ListPeers(ctx)
	if err != nil {
		return 0, err
	}
	vendors, err := reg.ListVendors(ctx)
	if err != nil {
		return 0, fmt.Errorf("list vendors: %w", err)
	}
	// Handle → anchor for live slots. Empty on per-vendor goatnets, which is
	// not an error: prefix-keyed matching alone is correct there.
	handleAnchors, err := reg.LiveHandleAnchors(ctx)
	if err != nil {
		return 0, fmt.Errorf("live handle anchors: %w", err)
	}

	counts := make(map[string]int64, len(vendors))
	for _, v := range vendors {
		counts[v.Prefix] = 0
	}
	keys := candidateKeys(vendors, handleAnchors)
	for _, p := range peers {
		v := matchVendor(p.Name, keys)
		if v == "" {
			continue
		}
		// A handle can outlive its org row (slot assigned, org row deleted).
		// Counting it would resurrect a prefix MarkL2 then inserts against a
		// vendor that does not exist, so skip rather than invent the row.
		if _, ok := counts[v]; !ok {
			continue
		}
		counts[v]++
	}

	touched := 0
	for prefix, n := range counts {
		if err := reg.MarkL2(ctx, prefix, n); err != nil {
			return touched, fmt.Errorf("mark l2 %s: %w", prefix, err)
		}
		touched++
	}
	return touched, nil
}

// candidateKeys builds the key → org-prefix map the peer-name join matches
// against: every org prefix (mapping to itself) plus every live slot handle
// (mapping to its anchor). Both are offered to ONE longest-wins match rather
// than tried in sequence, so a handle that is a substring of another key
// cannot shadow it — the same rule that keeps "foo" off "peer-foobar-…".
//
// A handle that collides with an org prefix loses: the prefix mapping is
// written second. That collision is not reachable today (prefixes are 32-hex,
// handles are human), and resolving it toward the org row is the conservative
// direction if it ever becomes reachable.
func candidateKeys(vendors []registry.Vendor, handleAnchors map[string]string) map[string]string {
	keys := make(map[string]string, len(vendors)+len(handleAnchors))
	for handle, anchor := range handleAnchors {
		keys[handle] = anchor
	}
	for _, v := range vendors {
		keys[v.Prefix] = v.Prefix
	}
	return keys
}

// matchVendor returns the org prefix that owns the peer name, or "" when no
// candidate matches. The grammar (and the longest-wins rule that keeps "foo"
// from claiming a "foobar" peer) lives in internal/peername, generated from
// ops/identity/peer-name-grammar.yaml — this is one of four Go consumers that
// each carried their own copy before the identity fragility audit.
//
// The match is EVIDENCE of ownership derived from a name, never the recorded
// linkage (that is slots.identity_id). L2 is a progress hint, so name-derived
// evidence is the right strength here; an enforcement path must not use it.
func matchVendor(peerName string, keys map[string]string) string {
	candidates := make([]string, 0, len(keys))
	for k := range keys {
		candidates = append(candidates, k)
	}
	return keys[peername.MatchLongestKey(peerName, candidates)]
}

// Run launches a goroutine that calls Poll on each tick. Returns
// immediately; the goroutine exits when ctx is canceled. Errors are
// logged via slog at Warn — the swimlane degrades gracefully.
func (c *Client) Run(ctx context.Context, reg *registry.Registry, interval time.Duration) {
	if !c.Enabled() {
		slog.Info("netbird poller disabled (no mgmt URL or token); L2 column will show '—' for every vendor")
		return
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}
	go func() {
		// First poll on a short delay so startup logs settle.
		first := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			first.Stop()
			return
		case <-first.C:
		}
		c.tick(ctx, reg)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.tick(ctx, reg)
			}
		}
	}()
}

func (c *Client) tick(ctx context.Context, reg *registry.Registry) {
	pollCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	n, err := c.Poll(pollCtx, reg)
	if err != nil {
		slog.Warn("netbird poll failed", "err", err)
		return
	}
	slog.Debug("netbird poll ok", "vendors_updated", n)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
