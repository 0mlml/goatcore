package api

// audit_presign.go is the server side of goat-cli Phase G.6 (ADR 1038): the
// operator reads the Object-Lock audit-tail via portal-minted presigned S3
// GET URLs, so goat replay / sub --since / narrate work without MinIO
// credentials on the laptop. The portal authorizes (operator Bearer token),
// signs short-TTL GET URLs for the audit objects covering the requested
// window, and returns the list; the bytes then flow laptop→MinIO directly so
// the portal is not in the data path. Revocation stays mesh-first (revoke the
// token → no new presigns); no standing object-store credential leaves the
// server. Read-only + presign-scoped, so it cannot weaken the Object-Lock
// write/retention guarantees (ADR 0811).

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/audittail"
)

const (
	// Presigned URLs live just long enough for a slow MinIO reply; a leaked
	// URL ages out fast.
	auditPresignTTL = 5 * time.Minute
	// Ceiling on objects (and therefore signed URLs) per call. A wider
	// window than this should be split client-side.
	auditPresignMaxObjects = 500
	// Hardest a single call may reach back, so one request can't try to
	// presign weeks of objects. The MinIO middle tier retains ~30d.
	auditPresignMaxWindow = 7 * 24 * time.Hour
)

type auditPresignObject struct {
	Key string `json:"key"`
	URL string `json:"url"`
}

type auditPresignResponse struct {
	Bucket     string               `json:"bucket"`
	Since      string               `json:"since"`
	Key        string               `json:"key,omitempty"`
	TTLSeconds int                  `json:"ttl_seconds"`
	Count      int                  `json:"count"`
	Truncated  bool                 `json:"truncated"`
	Objects    []auditPresignObject `json:"objects"`
}

// handleAuditPresign mints presigned GET URLs for the audit objects covering
// a time window. Operator-gated: the audit-tail is a fleet-wide history.
//
// Query params: `since` (a duration like `1h`/`90m`, an RFC3339 timestamp,
// or empty → last 1h) and an optional `key` (a Zenoh key expression echoed
// back for the client to filter on; the audit objects are NDJSON batches
// mixing many keys, so key filtering is necessarily client-side).
func (s *Server) handleAuditPresign(w http.ResponseWriter, r *http.Request) {
	pre := audittail.NewPresigner(audittail.Config{
		Endpoint:     s.cfg.AuditS3Endpoint,
		Bucket:       s.cfg.AuditS3Bucket,
		Region:       s.cfg.AuditS3Region,
		AccessKey:    s.cfg.AuditS3AccessKey,
		SecretKey:    s.cfg.AuditS3SecretKey,
		SubscriberID: s.cfg.AuditSubscriberID,
	})
	if !pre.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "audit",
			"audit-tail read is not configured on this portal (no MinIO endpoint/credentials)")
		return
	}

	sinceParam := strings.TrimSpace(r.URL.Query().Get("since"))
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "audit.presign", sinceParam, "")

	since, err := parseAuditSince(sinceParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, "decode", err.Error())
		return
	}

	keys, err := pre.ListSince(r.Context(), since, auditPresignMaxObjects+1)
	if err != nil {
		writeError(w, http.StatusBadGateway, "audit", "list audit objects: "+err.Error())
		return
	}
	truncated := false
	if len(keys) > auditPresignMaxObjects {
		keys = keys[:auditPresignMaxObjects]
		truncated = true
	}

	objects := make([]auditPresignObject, 0, len(keys))
	for _, k := range keys {
		u, err := pre.PresignGet(k, auditPresignTTL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "audit", "presign: "+err.Error())
			return
		}
		objects = append(objects, auditPresignObject{Key: k, URL: u})
	}

	writeJSON(w, http.StatusOK, auditPresignResponse{
		Bucket:     s.cfg.AuditS3Bucket,
		Since:      since.UTC().Format(time.RFC3339),
		Key:        strings.TrimSpace(r.URL.Query().Get("key")),
		TTLSeconds: int(auditPresignTTL.Seconds()),
		Count:      len(objects),
		Truncated:  truncated,
		Objects:    objects,
	})
}

// parseAuditSince accepts a relative duration (`1h`, `90m`, `2h30m`), an
// RFC3339 timestamp, or empty (→ 1h ago). The result is clamped to the
// [now-auditPresignMaxWindow, now] range so a single call can't try to
// presign weeks of objects.
func parseAuditSince(s string) (time.Time, error) {
	now := time.Now().UTC()
	floor := now.Add(-auditPresignMaxWindow)
	clamp := func(t time.Time) time.Time {
		if t.Before(floor) {
			return floor
		}
		if t.After(now) {
			return now
		}
		return t
	}
	if s == "" {
		return now.Add(-1 * time.Hour), nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d < 0 {
			d = -d
		}
		return clamp(now.Add(-d)), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return clamp(t.UTC()), nil
	}
	return time.Time{}, fmt.Errorf("invalid since %q (use a duration like 1h, or an RFC3339 timestamp)", s)
}
