// Package activity answers "how much has this org published, and to how many
// topics" without the registry having to record it.
//
// WHY THIS PACKAGE EXISTS. A registry table written twice per observed message
// grows by one row per distinct key expression ever seen on the wire, far past
// the declared topic count, and drives an unbounded write-ahead log on the
// host's disk for a few fields on one operator screen.
//
// The counters already exist, correctly bounded, on the same host: the
// audit-subscriber exports goat_audit_samples_total labelled by the DECLARED
// key pattern. This package reads those instead of keeping a third copy.
//
// THE SEAM. Source is an interface on purpose. The registry is scheduled to
// move from SQLite to Postgres, and when it does the question "where do
// activity counts come from" should be answerable by writing one more Source
// rather than by touching the swimlane, the tailer or the registry. Everything
// outside this package depends on Summary and Source, never on Prometheus.
//
// HONESTY. A Source that cannot answer says so — it returns Unavailable with a
// reason rather than zeros. A zero publish count and an absent publish count
// mean opposite things to an operator deciding whether an onboarding has
// stalled, and the surface that conflated them is the one this replaces.
package activity

import (
	"context"
	"fmt"
	"time"
)

// Summary is one org's published-activity rollup.
//
// It deliberately carries no per-topic detail. Per-topic rows keyed on the
// concrete key expression are unbounded by publisher behaviour, which is the
// defect this package exists to remove; a caller that wants per-topic numbers
// should query the metrics system directly rather than have the portal
// materialise them.
type Summary struct {
	// DistinctTopics is how many declared key patterns this org has published
	// under. Bounded by the org's declared namespaces, NOT by how many
	// distinct keys it has used — those can differ by orders of magnitude, and the
	// pattern count is the one an operator actually wants.
	DistinctTopics int64
	// Available is false when the source could not answer. The fields above
	// are then meaningless and must be rendered as unknown, never as zero.
	Available bool
	// Reason explains an unavailable answer in terms an operator can act on.
	Reason string
}

// Source answers the activity question for a set of orgs.
//
// Implementations must be safe for concurrent use and must not block longer
// than the context allows — the swimlane is an interactive surface.
//
// An implementation RETURNS its errors rather than rendering them as
// unavailable summaries. Only the caller knows whether a failure should blank
// a column, serve a stale answer or be retried, and a decorator that cannot
// see the error cannot make that choice either.
type Source interface {
	// ForOrgs returns a Summary per requested org prefix. Orgs with no
	// recorded activity are present with Available=true and zero counts; that
	// is a real answer ("nothing published"), distinct from an unavailable one.
	ForOrgs(ctx context.Context, prefixes []string) (map[string]Summary, error)
	// Name identifies the source in logs and on the surface, so an operator
	// reading a number knows where it came from.
	Name() string
}

// Unavailable builds the Summary a Source returns when it cannot answer.
func Unavailable(reason string) Summary {
	return Summary{Available: false, Reason: reason}
}

// unavailableFor is the whole-set form of Unavailable.
func unavailableFor(prefixes []string, reason string) map[string]Summary {
	out := make(map[string]Summary, len(prefixes))
	for _, p := range prefixes {
		out[p] = Unavailable(reason)
	}
	return out
}

// Disabled is the Source used when no metrics backend is configured.
//
// It is not a failure state and not a stub to be filled in later: a goatnet
// that runs no Prometheus still runs a portal, and the honest answer there is
// "this portal cannot tell you", returned quickly and uniformly. Returning
// zeros instead would put a fabricated number on an operator screen.
type Disabled struct{}

func (Disabled) Name() string { return "disabled" }

func (Disabled) ForOrgs(_ context.Context, prefixes []string) (map[string]Summary, error) {
	return unavailableFor(prefixes, "no metrics source configured for this goatnet"), nil
}

// cacheEntry is one memoised answer.
type cacheEntry struct {
	at   time.Time
	data map[string]Summary
}

// Cached wraps a Source with a short time-to-live, so a surface refreshed by
// several operators at once does not multiply into upstream queries. The TTL is
// short because the number is an activity reading, not a fact of record.
type Cached struct {
	inner Source
	ttl   time.Duration
	now   func() time.Time

	mu    chanMutex
	entry *cacheEntry
}

// chanMutex is a mutex that respects context cancellation, so a slow upstream
// cannot make callers queue past their own deadlines.
type chanMutex chan struct{}

func newChanMutex() chanMutex {
	c := make(chanMutex, 1)
	c <- struct{}{}
	return c
}

func (m chanMutex) lock(ctx context.Context) error {
	select {
	case <-m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m chanMutex) unlock() { m <- struct{}{} }

// NewCached wraps src with a ttl-bounded memo. A ttl of zero disables caching.
func NewCached(src Source, ttl time.Duration) *Cached {
	return &Cached{inner: src, ttl: ttl, now: time.Now, mu: newChanMutex()}
}

func (c *Cached) Name() string { return c.inner.Name() }

func (c *Cached) ForOrgs(ctx context.Context, prefixes []string) (map[string]Summary, error) {
	if c.ttl <= 0 {
		return c.inner.ForOrgs(ctx, prefixes)
	}
	if err := c.mu.lock(ctx); err != nil {
		return nil, fmt.Errorf("activity cache: %w", err)
	}
	defer c.mu.unlock()

	if c.entry != nil && c.now().Sub(c.entry.at) < c.ttl {
		return project(c.entry.data, prefixes), nil
	}
	// The upstream is asked for EVERY org, not just the requested ones: one
	// query for the whole fleet costs the same as one for a single org, and
	// caching the whole answer makes the per-org swimlane free.
	data, err := c.inner.ForOrgs(ctx, nil)
	if err != nil {
		// Serve a stale answer rather than nothing when one exists — an
		// activity count minutes out of date is more useful than a blank
		// column, and the surface is not a source of record.
		if c.entry != nil {
			return project(c.entry.data, prefixes), nil
		}
		return nil, err
	}
	c.entry = &cacheEntry{at: c.now(), data: data}
	return project(data, prefixes), nil
}

// project narrows a whole-fleet answer to the requested orgs. An org the
// source did not mention has no recorded activity, which is a real answer.
func project(data map[string]Summary, prefixes []string) map[string]Summary {
	if prefixes == nil {
		return data
	}
	out := make(map[string]Summary, len(prefixes))
	for _, p := range prefixes {
		if s, ok := data[p]; ok {
			out[p] = s
			continue
		}
		out[p] = Summary{Available: true}
	}
	return out
}
