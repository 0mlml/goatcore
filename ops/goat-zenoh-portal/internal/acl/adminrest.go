package acl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// adminrest.go — live config reload via Zenoh admin-space REST.
//
// Resolves F-156: zenohd v1.9.0's SIGHUP handler is a no-op for both
// access_control and storage_manager reload. On-disk config edits don't
// take effect until process restart. Without live reload, every vendor
// mint requires an operator-driven router restart (a short outage + peer
// reconnects) just for the new vendor's per-vendor storage stanza
// to load. F-156's documented fix path #2 is what this file implements:
// the portal posts the new stanzas to the router's admin space at
// `@/<zid>/router/config/...`, which IS plumbed for live mutation when
// the router is configured with `adminspace.permissions.write: true`.
//
// Gotcha discovered during the spike: zenohd returns HTTP 200 for an
// admin-space PUT even when the permission gate refuses the change.
// The actual rejection lands as an ERROR in the router log only. So
// HTTP status is NOT proof of success — every PUT here is followed by
// a GET round-trip that asserts the change actually appeared in the
// running admin space. If the GET says no, we surface the failure to
// the caller so the mint flow can decide what to do (typical fallback:
// the on-disk write is still in place, so the next planned router
// restart picks up the change).
//
// Trust boundary: the router's REST plugin binds 0.0.0.0:8000 inside
// its container; the portal reaches it over the docker network. No
// off-host writer can hit :8000.

// AdminRESTClient is a thin client for the Zenoh router's admin-space
// REST endpoints. Decoupled from Renderer so we can unit-test it
// against an httptest.Server.
type AdminRESTClient struct {
	// BaseURL is the router's REST plugin endpoint, e.g.
	// "http://goat-zenoh-router:8000". No trailing slash.
	BaseURL string

	// HTTPClient is the http client (timeouts, transport). Caller may
	// override; zero value is a 5s-timeout default.
	HTTPClient *http.Client

	// zidCache caches the router ZID discovered via the admin space's
	// own key shape. ZID changes only at router process restart, so a
	// per-renderer cache is fine; first call discovers, subsequent
	// calls use the cached value.
	//
	// Not exported because callers shouldn't poke at it; the resolution
	// happens lazily inside the methods.
	zidCache string
}

func (c *AdminRESTClient) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 5 * time.Second}
}

// adminGet fetches an admin-space key and returns the decoded list of
// {key, value} entries. The Zenoh REST plugin returns a JSON array
// where each entry is {"key": "...", "value": ...}.
func (c *AdminRESTClient) adminGet(ctx context.Context, keyExpr string) ([]map[string]any, error) {
	u := c.BaseURL + "/" + strings.TrimLeft(keyExpr, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("admin GET %s: %w", keyExpr, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("admin GET %s: HTTP %d: %s", keyExpr, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var entries []map[string]any
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("admin GET %s: decode response: %w", keyExpr, err)
	}
	return entries, nil
}

// ResolveZID discovers the router's ZID by GET'ing a generic admin key
// and extracting it from the first returned key's `@/<zid>/...` prefix.
// Caches per-client.
func (c *AdminRESTClient) ResolveZID(ctx context.Context) (string, error) {
	if c.zidCache != "" {
		return c.zidCache, nil
	}
	entries, err := c.adminGet(ctx, "@/*/router")
	if err != nil {
		return "", fmt.Errorf("resolve zid: %w", err)
	}
	for _, e := range entries {
		k, _ := e["key"].(string)
		// Shape: @/<zid>/router
		if !strings.HasPrefix(k, "@/") {
			continue
		}
		rest := strings.TrimPrefix(k, "@/")
		idx := strings.Index(rest, "/")
		if idx <= 0 {
			continue
		}
		zid := rest[:idx]
		if zid != "" {
			c.zidCache = zid
			return zid, nil
		}
	}
	return "", errors.New("resolve zid: no @/<zid>/... key returned by admin space")
}

// AddStorage adds a per-vendor rocksdb storage stanza to the running
// router. The router's storage_manager plugin spawns the storage
// immediately if the admin-space write is accepted.
//
// On success the post-PUT GET round-trip confirms the storage appears
// under `@/<zid>/router/status/plugins/storage_manager/storages/<vendor>`.
// On failure (HTTP non-2xx OR successful HTTP with missing storage in
// the GET) returns a wrapped error so the caller can decide whether to
// fall back to a planned restart.
//
// dirPath is the rocksdb directory the storage backs onto, e.g.
// /var/lib/goat-zenoh/rocksdb/<vendor>. Caller is responsible for
// mkdir before this call; the rocksdb backend will not create the
// parent directory.
func (c *AdminRESTClient) AddStorage(ctx context.Context, vendor, dirPath string, ttlMs int64) error {
	if vendor == "" {
		return errors.New("admin add storage: vendor empty")
	}
	zid, err := c.ResolveZID(ctx)
	if err != nil {
		return err
	}
	cfg := map[string]any{
		"key_expr": vendor + "/**",
		"volume": map[string]any{
			"id":                  "rocksdb",
			"dir":                 dirPath,
			"on_closure":          "do_nothing",
			"create_db":           true,
			"db_recovery_enabled": false,
		},
		"garbage_collect_period": 86400,
		"time_to_live":           ttlMs,
	}
	body, _ := json.Marshal(cfg)

	putKey := "@/" + zid + "/router/config/plugins/storage_manager/storages/" + url.PathEscape(vendor)
	u := c.BaseURL + "/" + strings.TrimLeft(putKey, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("admin PUT storage %s: %w", vendor, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("admin PUT storage %s: HTTP %d: %s",
			vendor, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	// HTTP 200 alone isn't proof. Verify via GET. zenohd asynchronously
	// spawns the storage; allow a brief retry window.
	verifyKey := "@/" + zid + "/router/status/plugins/storage_manager/storages/" + vendor
	if err := c.waitForKey(ctx, verifyKey, 2*time.Second); err != nil {
		return fmt.Errorf("admin PUT storage %s: HTTP 200 but storage not present in admin space (likely adminspace.permissions.write=false): %w", vendor, err)
	}
	return nil
}

// RemoveStorage is the reverse: DELETE on the same admin path, with
// post-DELETE verification.
func (c *AdminRESTClient) RemoveStorage(ctx context.Context, vendor string) error {
	if vendor == "" {
		return errors.New("admin remove storage: vendor empty")
	}
	zid, err := c.ResolveZID(ctx)
	if err != nil {
		return err
	}
	delKey := "@/" + zid + "/router/config/plugins/storage_manager/storages/" + url.PathEscape(vendor)
	u := c.BaseURL + "/" + strings.TrimLeft(delKey, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("admin DELETE storage %s: %w", vendor, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("admin DELETE storage %s: HTTP %d: %s",
			vendor, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	// Verify via GET — storage should now be absent.
	verifyKey := "@/" + zid + "/router/status/plugins/storage_manager/storages/" + vendor
	if err := c.waitForKeyAbsent(ctx, verifyKey, 2*time.Second); err != nil {
		return fmt.Errorf("admin DELETE storage %s: HTTP 200 but storage still present in admin space: %w", vendor, err)
	}
	return nil
}

// waitForKey polls the admin space until the given key returns a
// non-empty entry list or the deadline expires.
func (c *AdminRESTClient) waitForKey(ctx context.Context, keyExpr string, deadline time.Duration) error {
	end := time.Now().Add(deadline)
	for {
		entries, err := c.adminGet(ctx, keyExpr)
		if err == nil && len(entries) > 0 {
			return nil
		}
		if time.Now().After(end) {
			if err != nil {
				return err
			}
			return fmt.Errorf("key %s not present in admin space after %s", keyExpr, deadline)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// waitForKeyAbsent is the reverse — for DELETE verification.
func (c *AdminRESTClient) waitForKeyAbsent(ctx context.Context, keyExpr string, deadline time.Duration) error {
	end := time.Now().Add(deadline)
	for {
		entries, err := c.adminGet(ctx, keyExpr)
		if err == nil && len(entries) == 0 {
			return nil
		}
		if time.Now().After(end) {
			if err != nil {
				return err
			}
			return fmt.Errorf("key %s still present in admin space after %s", keyExpr, deadline)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// liveAddVendor is the high-level helper called from Render(). It
// attempts admin-REST live-add of the storage stanza for `vendor`. If
// the router has admin write disabled (or any other rejection cause),
// the error is logged but does NOT fail Render — the on-disk write is
// still in place, so the next planned router restart picks up the
// change. This preserves the operator's prior expectation (mint
// succeeds, restart required to take effect) while delivering the new
// behavior (no restart needed) where the router config supports it.
//
// ACL rule live-add is intentionally NOT attempted in v1 — zenohd
// 1.9.0's behavior for `@/<zid>/router/config/access_control/...`
// writes is not yet empirically verified (the rules schema is more
// rigid than storage, and the access-control module may not hot-reload
// like storage_manager does). Until proven, ACL relies on the existing
// universal `trial-peer-r-all` rule which already grants every TLS peer
// the right to put / subscribe under their cert-rooted prefix, so the
// effective access posture is unchanged from pre-fix. See F-156 §
// "Architectural fix candidates" for the follow-on that tightens the
// universal rule once ACL live-add is proven.
func (r *Renderer) liveAddVendor(ctx context.Context, vendor string) {
	if r.AdminREST == nil {
		return
	}
	dirPath := r.StorageDirPrefix + "/" + vendor
	if err := r.AdminREST.AddStorage(ctx, vendor, dirPath, r.storageTTLMs()); err != nil {
		slog.Warn("acl render: live storage add failed; on-disk write is in place, router restart required to take effect",
			"vendor", vendor,
			"err", err,
		)
		return
	}
	slog.Info("acl render: live storage add succeeded",
		"vendor", vendor,
	)
}

// liveRemoveVendor is the symmetric helper for RemoveVendor.
func (r *Renderer) liveRemoveVendor(ctx context.Context, vendor string) {
	if r.AdminREST == nil {
		return
	}
	if err := r.AdminREST.RemoveStorage(ctx, vendor); err != nil {
		slog.Warn("acl remove: live storage delete failed; on-disk write is in place, router restart required to take effect",
			"vendor", vendor,
			"err", err,
		)
		return
	}
	slog.Info("acl remove: live storage delete succeeded",
		"vendor", vendor,
	)
}
