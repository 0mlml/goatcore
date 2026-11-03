// Package zenohproxy proxies inspector requests through the zenohd REST
// plugin (`GET http://<router>:8000/<key-expr>`) and exposes a Server-Sent
// Events stream for live tail.
//
// Trial-fabric posture: zenohd's REST plugin is loopback-only on nova-a; the
// portal is the only thing in front of it that vendor browsers reach.
// Auth + scope are enforced by the portal API layer before requests
// hit the proxy. The proxy itself trusts its caller.
//
// Fake mode (--zenoh-rest-fake) returns canned responses so the portal can
// be built and tested before the Track A Zenoh router is up.
package zenohproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Proxy talks to a zenohd REST plugin (or returns fake responses).
type Proxy struct {
	baseURL string
	fake    bool
	client  *http.Client
}

// New returns a Proxy that talks to the given Zenoh REST endpoint. When
// fake is true, baseURL may be empty and responses are synthesized.
func New(baseURL string, fake bool) *Proxy {
	return &Proxy{
		baseURL: strings.TrimRight(baseURL, "/"),
		fake:    fake,
		client: &http.Client{
			// Per Block 50A.1 timeout discipline. Loopback-only call;
			// short tail acceptable. SSE upgrades to its own long-lived
			// connection via a separate request.
			Timeout: 10 * time.Second,
		},
	}
}

// Sample is a single key/value observation from the Zenoh REST plugin.
// Zenoh's REST shape varies by version; we keep what we surface in the
// inspector minimal and pass through the underlying JSON body as
// pre-parsed value for the format-aware decoder in the browser.
type Sample struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	Encoding  string          `json:"encoding,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	Source    string          `json:"source,omitempty"`
}

// Latest fetches the last value(s) at key. Empty result on miss; not an
// error. Caller bounds the key-expression to the operations they're
// allowed to query.
func (p *Proxy) Latest(ctx context.Context, keyExpr string) ([]Sample, error) {
	if p.fake {
		return fakeSamples(keyExpr, 1), nil
	}
	if err := validateKey(keyExpr); err != nil {
		return nil, err
	}
	u := p.baseURL + "/" + escapeKeyExpr(keyExpr)
	return p.doFetch(ctx, u)
}

// Put publishes a value to keyExpr via the zenohd REST plugin (HTTP PUT
// to <baseURL>/<keyExpr>). Used by the portal for fleet-wide control
// topics (ops/trashgoat/control/<id>/v1, etc.) where the portal
// authors a message on behalf of the operator without holding a
// per-vendor cert. contentType is sent as the request Content-Type
// header; empty string defaults to application/octet-stream.
//
// Fake mode is a no-op (returns nil) so unit tests can exercise the
// control-emit code path without a live router.
func (p *Proxy) Put(ctx context.Context, keyExpr string, body []byte, contentType string) error {
	if p.fake {
		return nil
	}
	if err := validateKey(keyExpr); err != nil {
		return err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	u := p.baseURL + "/" + escapeKeyExpr(keyExpr)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("zenoh REST PUT: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("zenoh REST PUT returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// Recent fetches up to n historical values for key. Backed by zenohd's
// storage_manager; only present when the topic registry's
// publish_to_storage is on for that topic. zenohd returns whatever it
// has; n is a hint, not a hard limit.
func (p *Proxy) Recent(ctx context.Context, keyExpr string, n int) ([]Sample, error) {
	if p.fake {
		return fakeSamples(keyExpr, n), nil
	}
	if err := validateKey(keyExpr); err != nil {
		return nil, err
	}
	// Zenoh REST plugin shape: GET /<key-expr>?_history=N returns up to N
	// samples backed by storage_manager. Confirmed-at-deploy-time syntax;
	// see ADR 0708 amendment for the bundled version.
	u := fmt.Sprintf("%s/%s?_history=%d", p.baseURL, escapeKeyExpr(keyExpr), n)
	return p.doFetch(ctx, u)
}

// StreamSSE long-polls the Zenoh REST plugin for new samples matching the
// key expression and writes each as a Server-Sent Event to w. Returns when
// ctx is canceled, w closes, or the upstream connection breaks.
//
// Zenoh's REST plugin does not expose native streaming today. We poll
// every interval (default 1s) and emit new samples as they arrive.
// Trade-off: simpler than a Zenoh subscriber sidecar at the cost of up to
// `interval` end-to-end latency. Acceptable for trial-scope inspection.
func (p *Proxy) StreamSSE(ctx context.Context, w http.ResponseWriter, keyExpr string, interval time.Duration) error {
	if err := ValidateStreamKey(keyExpr); err != nil {
		return err
	}
	if interval <= 0 {
		interval = time.Second
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("response writer does not support flushing — SSE requires http.Flusher")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable Nginx-style buffering at any intermediary
	flusher.Flush()

	// Track last observed timestamps so we don't re-emit. Trial-scope
	// dedup; we accept that a clock-skewed publisher can briefly re-emit.
	seen := make(map[string]string)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-heartbeat.C:
			// SSE comment line keeps the connection alive through
			// idle-killing proxies.
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-ticker.C:
			samples, err := p.Latest(ctx, keyExpr)
			if err != nil {
				// One bad poll is not fatal; SSE consumers tolerate gaps.
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", jsonEscape(err.Error()))
				flusher.Flush()
				continue
			}
			for _, s := range samples {
				ts := s.Timestamp
				if ts == "" {
					ts = fmt.Sprintf("%d", time.Now().UnixNano())
				}
				if seen[s.Key] == ts {
					continue
				}
				seen[s.Key] = ts
				payload, _ := json.Marshal(s)
				fmt.Fprintf(w, "event: sample\ndata: %s\n\n", payload)
				flusher.Flush()
			}
		}
	}
}

func (p *Proxy) doFetch(ctx context.Context, u string) ([]Sample, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zenoh REST GET: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read zenoh REST body: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("zenoh REST returned %d: %s", resp.StatusCode, string(body))
	}
	// Zenoh REST returns a JSON array of samples. Schema differs between
	// versions; for portability we read into a generic []map and translate
	// known fields. Unknown fields are passed through as raw JSON in Value.
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		// Tolerate a single-sample object as an alternative shape.
		var one map[string]json.RawMessage
		if err2 := json.Unmarshal(body, &one); err2 == nil {
			raw = []map[string]json.RawMessage{one}
		} else {
			return nil, fmt.Errorf("parse zenoh REST body: %w (body: %s)", err, string(body))
		}
	}
	out := make([]Sample, 0, len(raw))
	for _, m := range raw {
		s := Sample{}
		_ = json.Unmarshal(m["key"], &s.Key)
		_ = json.Unmarshal(m["encoding"], &s.Encoding)
		_ = json.Unmarshal(m["timestamp"], &s.Timestamp)
		_ = json.Unmarshal(m["source"], &s.Source)
		if v, ok := m["value"]; ok {
			s.Value = v
		}
		out = append(out, s)
	}
	return out, nil
}

// validateStreamKey applies the RECURRING-read bound on top of validateKey:
// a key expression whose FIRST chunk is (or contains) a wildcard matches
// across every namespace owner — a whole-tree walk. On the one-shot Latest
// path that is a single operator-triggered read and stays allowed; on the
// SSE path it repeats every poll interval (1 s) for as long as a browser tab
// stays open, which is exactly the broad-crawl-×-cardinality wedge factor the
// single-lazy-crawler strategy removes
// (docs/design/zenoh-cardinality-budget-and-lazy-crawler.md §4 — read the
// crawler's published rollup instead). Owner-scoped wildcards
// (`<owner>/**`) remain allowed: bounded by one owner, whose size the
// cardinality budget bounds in turn.
func ValidateStreamKey(k string) error {
	if err := validateKey(k); err != nil {
		return err
	}
	first, _, _ := strings.Cut(k, "/")
	if strings.ContainsAny(first, "*$") {
		return fmt.Errorf("streaming rejects cross-owner wildcards (first chunk %q): a `**`-wide stream re-walks the whole key tree every poll; scope the stream under one namespace owner, or read the crawler rollup on ops/fabric/cardinality/<site>", first)
	}
	return nil
}

// validateKey rejects key expressions that include characters we don't
// allow at proxy time (control chars, embedded URL-bad characters). Zenoh
// itself enforces its own grammar; this is portal-side defense.
func validateKey(k string) error {
	if k == "" {
		return errors.New("empty key expression")
	}
	if len(k) > 512 {
		return fmt.Errorf("key expression too long (>512 chars)")
	}
	for _, r := range k {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("key expression contains control character")
		}
	}
	return nil
}

// escapeKeyExpr URL-escapes path segments but preserves '/', '*', '**'
// because Zenoh REST uses them as part of the key-expression grammar.
func escapeKeyExpr(k string) string {
	parts := strings.Split(k, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// jsonEscape escapes a string for inclusion in an SSE data field.
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// fakeSamples builds canned samples for --zenoh-rest-fake. The body is a
// JSON envelope around the key expression itself so the inspector decoder
// has something to render. n caps the count.
func fakeSamples(keyExpr string, n int) []Sample {
	if n < 1 {
		n = 1
	}
	if n > 5 {
		n = 5
	}
	out := make([]Sample, 0, n)
	for i := 0; i < n; i++ {
		v := map[string]any{
			"_fake":   true,
			"key":     keyExpr,
			"seq":     i,
			"message": fmt.Sprintf("fake sample %d for %s (no Zenoh router wired)", i, keyExpr),
		}
		body, _ := json.Marshal(v)
		out = append(out, Sample{
			Key:       keyExpr,
			Value:     body,
			Encoding:  "application/json",
			Timestamp: fmt.Sprintf("%d", time.Now().UnixNano()-int64(i)*int64(time.Second)),
			Source:    "fake-zenohd",
		})
	}
	return out
}

// ensure bufio is referenced (used by SSE-line-split tests; keep import
// stable).
var _ = bufio.NewReader
