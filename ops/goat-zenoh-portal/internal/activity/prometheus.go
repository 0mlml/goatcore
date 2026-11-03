package activity

// The Prometheus-backed Source.
//
// It asks one question, fleet-wide, and it is the only question this package
// needs a metrics backend for:
//
//	count by (vendor) (goat_audit_samples_total)
//
// That counts DECLARED key patterns per org, the bounded form of this number.
// The audit-subscriber already labels by pattern rather than by concrete
// key, so the boundary is enforced upstream, by the component that actually sees
// the traffic.
//
// Deliberately NOT done here: a range query, a rate, or anything else that
// depends on retention. This is an instant query against a counter, so the
// answer does not change meaning when retention does. The one thing retention
// would buy — "publishes in the last N days" — is not a field the swimlane has.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// distinctPatternsQuery counts the declared key patterns each org has
// published under. Kept as a constant rather than built by string
// concatenation: it takes no caller input, so there is nothing to inject and
// nothing to get wrong per call.
const distinctPatternsQuery = `count by (vendor) (goat_audit_samples_total)`

// Prometheus reads activity counts from a Prometheus HTTP API.
type Prometheus struct {
	base   string
	client *http.Client
}

// NewPrometheus builds a Source against baseURL (for example
// "http://obs-prometheus:9090"). timeout bounds a single query; it should
// be well under the surface's own deadline, because an activity count is the
// least important thing on the swimlane and must never be what makes it slow.
func NewPrometheus(baseURL string, timeout time.Duration) (*Prometheus, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse prometheus url %q: %w", baseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("prometheus url %q needs an http or https scheme", baseURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("prometheus url %q has no host", baseURL)
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Prometheus{base: u.String(), client: &http.Client{Timeout: timeout}}, nil
}

func (p *Prometheus) Name() string { return "prometheus " + p.base }

// promResponse is the subset of the query API this needs.
type promResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func (p *Prometheus) ForOrgs(ctx context.Context, prefixes []string) (map[string]Summary, error) {
	res, err := p.instant(ctx, distinctPatternsQuery)
	if err != nil {
		// Returned, not swallowed into per-org "unavailable" summaries.
		// Deciding what a failure looks like on screen is the caller's job,
		// and a decorator that cannot see the failure cannot do its own — the
		// cache below needs the error to know when to serve a stale answer.
		return nil, err
	}

	out := map[string]Summary{}
	for _, r := range res.Data.Result {
		vendor := r.Metric["vendor"]
		if vendor == "" {
			continue
		}
		n, ok := sampleValue(r.Value)
		if !ok {
			continue
		}
		out[vendor] = Summary{DistinctTopics: n, Available: true}
	}
	return project(out, prefixes), nil
}

// instant runs one instant query and returns the decoded body.
func (p *Prometheus) instant(ctx context.Context, q string) (*promResponse, error) {
	endpoint := p.base + "/api/v1/query?" + url.Values{"query": {q}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned %s", resp.Status)
	}
	var out promResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode prometheus response: %w", err)
	}
	if out.Status != "success" {
		reason := out.Error
		if reason == "" {
			reason = out.Status
		}
		return nil, fmt.Errorf("prometheus query failed: %s", reason)
	}
	return &out, nil
}

// sampleValue pulls the numeric half out of Prometheus's [timestamp, "value"]
// pair. The value arrives as a STRING — decoding it as a float silently
// rounds large counters — so it is parsed here and only here.
func sampleValue(v []any) (int64, bool) {
	if len(v) != 2 {
		return 0, false
	}
	s, ok := v[1].(string)
	if !ok {
		return 0, false
	}
	// Counter values can carry an exponent or a fraction; both are legal and
	// neither is meaningful for a count of series, so round through float.
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	// Prometheus legitimately sends NaN and +Inf, and converting either to an
	// integer is UNDEFINED in Go — it yields 0 on arm64 and the minimum int64
	// on amd64, so the same metric would put a different number on the
	// swimlane depending on which architecture the portal happened to be
	// built for. A negative or oversized value is equally not a count of
	// series. All of them are rejected, which renders the field as unknown.
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > math.MaxInt64 {
		return 0, false
	}
	return int64(f), true
}
