// ratelimit.go — ADR 1081 Layer 2: per-sender token bucket on the unary
// Send path.
//
// The ZAGROS control-plane storm was a single-digit set of senders
// hammering Send() at a target peer that was offline: the server accepted
// 97.5 GB of offers it could deliver 8.1 GB of, its log flooded with
// relay-miss lines. A per-sender token bucket lets the server shed that
// honestly — it tells the abusive sender to back off (codes.ResourceExhausted
// + a RetryInfo retry hint) instead of silently drinking the load.
//
// Sizing rationale (see also the negative cache in negcache.go and the PR):
// the bucket is keyed on the SENDER peer key, not the recipient. A healthy
// mass-reconnect after a Thor reboot is many DIFFERENT peers each emitting a
// modest handful of offers (one per peer they want to reach) — each gets its
// own bucket, freshly full to `burst`, so mass reconnect does NOT throttle.
// A storm is a FEW senders emitting thousands; those are exactly the buckets
// that drain and start shedding. Per-sender bucketing is what separates the
// two cases.
//
// Default OFF: SenderRateLimitPerMin <= 0 makes allow() a no-op that always
// admits. Wired from the SIGNAL_SENDER_RATE_LIMIT / SIGNAL_SENDER_RATE_BURST
// env vars via cmd/signal-server + server.Config.

package server

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	// retryAfterBase is the retry-after hint returned on a sender's first
	// breach; it doubles per consecutive breach up to retryAfterCap.
	retryAfterBase = 5 * time.Second
	// retryAfterCap caps the retry-after hint. 5s<<4 = 80s, so the shift is
	// capped at 4 (the 5th consecutive breach and every one after → 80s).
	retryAfterCap = 80 * time.Second
)

// senderBucket is one token bucket, owned by a single sender peer key.
// Access is serialized by senderRateLimiter.mu (the buckets themselves carry
// no lock).
type senderBucket struct {
	tokens float64   // current tokens; consumed one per admitted Send
	last   time.Time // last refill timestamp
	// consecutiveBreaches counts breaches since the last admitted Send. Drives
	// the doubling retry-after hint; reset to 0 the moment a Send is admitted.
	consecutiveBreaches uint
}

// senderRateLimiter is a per-sender token bucket. Keyed on the sender's
// WireGuard public key (EncryptedMessage.Key). Only the unary Send path
// consumes tokens — that is the path the storm arrived on and the only path
// that can carry a retry hint back to the caller.
type senderRateLimiter struct {
	ratePerMin int // 0 or negative disables the limiter entirely
	burst      float64
	refillPerS float64 // tokens replenished per second = ratePerMin/60

	// now is the clock, injectable so tests exercise refill/breach without
	// wall-clock sleeps. Defaults to time.Now in the constructor.
	now func() time.Time

	mu      sync.Mutex
	buckets map[string]*senderBucket

	limited prometheus.Counter
}

// newSenderRateLimiter builds a limiter. ratePerMin <= 0 disables it (allow
// always admits); the metric is still registered so the series exists at zero
// on dashboards. burst <= 0 falls back to 2× ratePerMin (the documented
// default: rate 30 → burst 60), never below 1 when the limiter is enabled.
func newSenderRateLimiter(reg prometheus.Registerer, node string, ratePerMin, burst int) *senderRateLimiter {
	limited := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "signal_server_send_rate_limited_total",
		Help:        "Total unary Send RPCs rejected by the per-sender token bucket (ADR 1081 L2).",
		ConstLabels: prometheus.Labels{"node": node},
	})
	reg.MustRegister(limited)

	rl := &senderRateLimiter{
		ratePerMin: ratePerMin,
		now:        time.Now,
		buckets:    make(map[string]*senderBucket),
		limited:    limited,
	}
	if ratePerMin > 0 {
		if burst <= 0 {
			burst = 2 * ratePerMin
		}
		if burst < 1 {
			burst = 1
		}
		rl.burst = float64(burst)
		rl.refillPerS = float64(ratePerMin) / 60.0
	}
	return rl
}

// enabled reports whether the limiter is active.
func (rl *senderRateLimiter) enabled() bool { return rl != nil && rl.ratePerMin > 0 }

// allow charges one token to sender and reports whether the Send is admitted.
// On a breach it returns admit=false and a retry-after hint that doubles from
// retryAfterBase per consecutive breach, capped at retryAfterCap. A fresh
// sender starts with a full bucket (burst tokens), so a first-contact burst is
// never throttled. Disabled limiter always admits with a zero hint.
func (rl *senderRateLimiter) allow(sender string) (admit bool, retryAfter time.Duration) {
	if !rl.enabled() {
		return true, 0
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	b, ok := rl.buckets[sender]
	if !ok {
		// New sender: full bucket, one token consumed for this Send.
		rl.buckets[sender] = &senderBucket{tokens: rl.burst - 1, last: now}
		return true, 0
	}

	// Refill by elapsed wall time, capped at burst.
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * rl.refillPerS
		if b.tokens > rl.burst {
			b.tokens = rl.burst
		}
		b.last = now
	}

	if b.tokens >= 1 {
		b.tokens--
		b.consecutiveBreaches = 0
		return true, 0
	}

	b.consecutiveBreaches++
	rl.limited.Inc()
	return false, retryAfterForBreach(b.consecutiveBreaches)
}

// rateLimitError builds the gRPC error returned to a throttled sender. It is
// codes.ResourceExhausted (the standard gRPC "back off" signal) and carries a
// machine-readable retry hint two ways so any client can consume one: the
// standard errdetails.RetryInfo status detail (what a Google-API-style client
// reads) and a human-readable retry-after in the message. The signal proto
// has no native retry field, so RetryInfo is the closest idiomatic mechanism.
// The L1 client (separate track) reads RetryInfo to schedule its backoff.
func rateLimitError(retryAfter time.Duration) error {
	st := status.New(codes.ResourceExhausted,
		fmt.Sprintf("sender rate limit exceeded; retry after %s", retryAfter))
	withDetail, err := st.WithDetails(&errdetails.RetryInfo{
		RetryDelay: durationpb.New(retryAfter),
	})
	if err != nil {
		// Attaching details can only fail on marshal errors we don't expect;
		// fall back to the bare status so the caller still gets ResourceExhausted.
		return st.Err()
	}
	return withDetail.Err()
}

// retryAfterForBreach returns the retry-after hint for the n-th consecutive
// breach (1-based): retryAfterBase doubling each breach, capped at
// retryAfterCap. n=1→5s, 2→10s, 3→20s, 4→40s, 5+→80s.
func retryAfterForBreach(n uint) time.Duration {
	if n == 0 {
		n = 1
	}
	shift := n - 1
	if shift >= 4 { // retryAfterBase<<4 == retryAfterCap; clamp to avoid overflow
		return retryAfterCap
	}
	d := retryAfterBase << shift
	if d > retryAfterCap {
		return retryAfterCap
	}
	return d
}
