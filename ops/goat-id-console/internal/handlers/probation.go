package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/meshstate"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

const probationPetitionPath = "/__goat/probation/request"

const (
	probationPeerSnapshotTTL    = 30 * time.Second
	probationPeerSnapshotMaxAge = 2 * probationPeerSnapshotTTL
	probationPeerRetryDelay     = 5 * time.Second
)

// probationPeerSnapshot is immutable after publication. Keeping the lookup
// indexed by parsed address makes the entrypoint-wide ForwardAuth cost
// independent of fleet size. duplicateIP preserves the fail-closed behavior
// for an ambiguous address.
type probationPeerSnapshot struct {
	byIP        map[netip.Addr]meshstate.Peer
	duplicateIP map[netip.Addr]bool
	refreshedAt time.Time
}

func (p *probationPeerSnapshot) fresh(now time.Time) bool {
	return p != nil && !p.refreshedAt.IsZero() && now.Before(p.refreshedAt.Add(probationPeerSnapshotMaxAge))
}

type probationHoldKey struct {
	site   string
	peerID string
}

// probationHoldSnapshot is immutable after publication. FileInfo lets each
// request cheaply detect both append-in-place writes and atomic replacement;
// the indexed records keep classification independent of the ledger size.
// Callers still open the source before reuse so loss of readability fails
// closed even when the cached file identity and timestamps are unchanged.
type probationHoldSnapshot struct {
	info             os.FileInfo
	bySitePeer       map[probationHoldKey]meshstate.Records
	duplicatePeerIDs map[string]bool
}

type probationHold struct {
	PeerID        string
	PeerName      string
	Handle        string
	EnteredAt     time.Time
	HeldSince     string
	HeldAge       string
	PetitionToken string
	Requested     bool
}

type probationDisposition uint8

const (
	probationPass probationDisposition = iota
	probationDeny
	probationExplain
)

type reactivationRequestRow struct {
	onboardstore.ReactivationRequest
	HeldSince   string
	RequestedAt string
	ContextURL  string
}

type reactivationRequestsPage struct {
	Rows     []reactivationRequestRow
	Error    string
	Declined bool
}

type reactivationDeclinePage struct {
	Request   onboardstore.ReactivationRequest
	HeldSince string
}

func (s *Server) handleProbationAuth(w http.ResponseWriter, r *http.Request) {
	// The petition path must reach its handler even while the originating peer
	// is held. That handler independently repeats classification and validates a
	// token bound to the exact current hold.
	if forwardedPath(r.Header.Get("X-Forwarded-Uri")) == probationPetitionPath &&
		strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Method")), http.MethodPost) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	hold, decision := s.classifyProbation(r)
	switch decision {
	case probationPass:
		w.WriteHeader(http.StatusNoContent)
		return
	case probationDeny:
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}
	// A held peer gets exactly the self-contained explainer document. Assets,
	// APIs and other methods must not fall through to the requested service:
	// the probation ACL permits INFRA:443 only so this gate can explain the
	// hold, not as a residual application-access lane.
	method := strings.ToUpper(strings.TrimSpace(r.Header.Get("X-Forwarded-Method")))
	if (method != http.MethodGet && method != http.MethodHead) ||
		!strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}
	hold.PetitionToken = s.probationToken(hold)
	if s.Onboard != nil {
		requested, err := s.Onboard.HasOpenReactivationRequest(r.Context(), hold.PeerID, hold.EnteredAt)
		if err == nil {
			hold.Requested = requested
		}
	}
	s.writeProbation(w, r.Method, http.StatusLocked, hold)
}

func (s *Server) handleProbationRequest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	hold, decision := s.classifyProbation(r)
	if decision != probationExplain || !hmac.Equal([]byte(r.FormValue("token")), []byte(s.probationToken(hold))) {
		http.Error(w, "request unavailable", http.StatusForbidden)
		return
	}
	if s.Onboard == nil {
		http.Error(w, "request service unavailable", http.StatusServiceUnavailable)
		return
	}
	_, _, err := s.Onboard.RequestReactivation(r.Context(), hold.PeerID, hold.PeerName, hold.Handle, hold.EnteredAt)
	if err != nil {
		http.Error(w, "request service unavailable", http.StatusServiceUnavailable)
		return
	}
	hold.Requested = true
	s.writeProbation(w, r.Method, http.StatusOK, hold)
}

// handleProbationReady verifies the dependencies used by the running
// classifier, including its actual runtime URL/token pair. The Traefik play
// calls this loopback-only endpoint before installing the ForwardAuth gate.
func (s *Server) handleProbationReady(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(s.Cfg.NotifyGoatnet) == "" ||
		strings.TrimSpace(s.Cfg.HoldRecordsPath) == "" ||
		strings.TrimSpace(s.Cfg.NotifyMgmtURL) == "" ||
		strings.TrimSpace(s.Cfg.EnrollWatchToken) == "" {
		http.Error(w, "probation classifier unavailable", http.StatusServiceUnavailable)
		return
	}
	peers, err := s.loadProbationPeers(r.Context())
	if err != nil || len(peers) == 0 {
		http.Error(w, "probation classifier unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, err := loadProbationHoldSnapshot(strings.TrimSpace(s.Cfg.HoldRecordsPath)); err != nil {
		http.Error(w, "probation classifier unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeProbation(w http.ResponseWriter, method string, status int, hold probationHold) {
	t, err := s.Templates.Clone()
	if err != nil {
		http.Error(w, "page unavailable", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "probation", hold); err != nil {
		http.Error(w, "page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if method != http.MethodHead {
		if _, err := buf.WriteTo(w); err != nil {
			slog.Warn("probation response write failed", slog.String("err", err.Error()))
		}
	}
}

func forwardedPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

// classifyProbation returns an explainer only for one exact, current,
// internally consistent dormancy record. Known hold and ambiguous states are a
// generic denial; they never reveal whether a security hold exists.
func (s *Server) classifyProbation(r *http.Request) (probationHold, probationDisposition) {
	ip, meshSource, valid := forwardedSourceIP(r.Header.Get("X-Forwarded-For"))
	if !valid {
		return probationHold{}, probationDeny
	}
	if !meshSource {
		return probationHold{}, probationPass
	}
	if strings.TrimSpace(s.Cfg.NotifyGoatnet) == "" || strings.TrimSpace(s.Cfg.HoldRecordsPath) == "" {
		return probationHold{}, probationDeny
	}
	snapshot, err := s.cachedProbationPeerSnapshot(r.Context())
	if err != nil {
		return probationHold{}, probationDeny
	}
	hold, decision := s.classifyProbationWith(ip, snapshot)
	if decision != probationExplain || snapshot.fresh(time.Now()) {
		return hold, decision
	}
	// An IP address is not a durable member identity. Before disclosing a hold
	// or accepting its petition, force a current binding and classify again.
	snapshot, err = s.freshProbationPeerSnapshot(r.Context())
	if err != nil || !snapshot.fresh(time.Now()) {
		return probationHold{}, probationDeny
	}
	return s.classifyProbationWith(ip, snapshot)
}

func (s *Server) classifyProbationWith(ip netip.Addr, snapshot *probationPeerSnapshot) (probationHold, probationDisposition) {
	if snapshot.duplicateIP[ip] {
		return probationHold{}, probationDeny
	}
	peer, ok := snapshot.byIP[ip]
	if !ok {
		return probationHold{}, probationDeny
	}
	records, duplicate, err := s.probationRecordsFor(strings.TrimSpace(s.Cfg.HoldRecordsPath), s.Cfg.NotifyGoatnet, peer.ID)
	if err != nil {
		// The record is half of the trusted classification contract. Without it
		// we cannot distinguish an ordinary peer from a hold whose record was
		// written before its group move, so an unavailable source is unknown
		// state and must fail closed for every mesh peer.
		return probationHold{}, probationDeny
	}
	if duplicate {
		return probationHold{}, probationDeny
	}
	var record *meshstate.HoldRecord
	for i := range records.All {
		r := &records.All[i]
		if record != nil {
			return probationHold{}, probationDeny
		}
		record = r
	}
	if record == nil {
		if peerInHoldGroup(peer) {
			return probationHold{}, probationDeny
		}
		return probationHold{}, probationPass
	}
	// LastSeen is not displayed because an online member viewing this page has
	// a current value. It remains a consistency check on the live peer record.
	if record.Lane != meshstate.HoldKindDormancy || peer.LastSeen.IsZero() || peer.LastSeen.After(time.Now().UTC().Add(5*time.Minute)) {
		return probationHold{}, probationDeny
	}
	hv := meshstate.ResolveHolds(meshstate.HoldsInput{
		Site: s.Cfg.NotifyGoatnet, Records: records, Peers: []meshstate.Peer{peer}, Now: time.Now().UTC(),
	})
	for _, row := range hv.Rows {
		if row.PeerID != peer.ID || row.Kind != meshstate.HoldKindDormancy || row.State != meshstate.HoldStateHeld {
			continue
		}
		handle := ""
		if record.Handle != nil {
			handle = strings.TrimSpace(*record.Handle)
		}
		if row.DaysOnHold == "" {
			return probationHold{}, probationDeny
		}
		return probationHold{
			PeerID: peer.ID, PeerName: peer.Name, Handle: handle,
			EnteredAt: record.EnteredAt.UTC(),
			HeldSince: record.EnteredAt.UTC().Format("2 January 2006"),
			HeldAge:   probationHoldAge(record.EnteredAt, time.Now()),
		}, probationExplain
	}
	return probationHold{}, probationDeny
}

func probationHoldAge(enteredAt, now time.Time) string {
	age := now.Sub(enteredAt)
	if age < 24*time.Hour {
		return "today"
	}
	days := int(age / (24 * time.Hour))
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}

func forwardedSourceIP(raw string) (ip netip.Addr, mesh bool, valid bool) {
	// trustForwardHeader=false on Traefik makes this a single connection-derived
	// value. Refuse a chain instead of choosing an attacker-controlled member.
	if strings.Count(raw, ",") != 0 {
		return netip.Addr{}, false, false
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return netip.Addr{}, false, false
	}
	ip = ip.Unmap()
	meshPrefix := netip.MustParsePrefix("100.64.0.0/10")
	return ip, meshPrefix.Contains(ip), true
}

func peerInHoldGroup(peer meshstate.Peer) bool {
	for _, group := range peer.Groups {
		if meshstate.IsHoldGroup(group.Name) {
			return true
		}
	}
	return false
}

func (s *Server) probationRecordsFor(path, site, peerID string) (meshstate.Records, bool, error) {
	s.probationHoldMu.Lock()
	defer s.probationHoldMu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		return meshstate.Records{}, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return meshstate.Records{}, false, err
	}
	if s.probationHoldSnapshot == nil || !sameProbationHoldFile(s.probationHoldSnapshot.info, info) {
		snapshot, err := loadProbationHoldSnapshot(path)
		if err != nil {
			return meshstate.Records{}, false, err
		}
		s.probationHoldSnapshot = snapshot
	}
	key := probationHoldKey{site: strings.ToLower(strings.TrimSpace(site)), peerID: peerID}
	return s.probationHoldSnapshot.bySitePeer[key], s.probationHoldSnapshot.duplicatePeerIDs[peerID], nil
}

func sameProbationHoldFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func loadProbationHoldSnapshot(path string) (*probationHoldSnapshot, error) {
	// A writer either appends under the sidecar lock or atomically replaces the
	// path. Verify the path before and after parsing so a refresh never publishes
	// a mixture or mistakes an old inode for the replacement.
	for attempt := 0; attempt < 3; attempt++ {
		before, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		records, err := meshstate.LoadHoldRecords(path)
		if err != nil {
			return nil, err
		}
		after, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !sameProbationHoldFile(before, after) {
			continue
		}
		snapshot := &probationHoldSnapshot{
			info:             after,
			bySitePeer:       make(map[probationHoldKey]meshstate.Records),
			duplicatePeerIDs: records.DuplicatePeerIDs,
		}
		for _, record := range records.All {
			key := probationHoldKey{site: strings.ToLower(strings.TrimSpace(record.Site)), peerID: record.PeerID}
			indexed := snapshot.bySitePeer[key]
			indexed.All = append(indexed.All, record)
			snapshot.bySitePeer[key] = indexed
		}
		return snapshot, nil
	}
	return nil, fmt.Errorf("hold-records changed while being read")
}

func (s *Server) cachedProbationPeerSnapshot(ctx context.Context) (*probationPeerSnapshot, error) {
	for {
		now := time.Now()
		s.probationMu.Lock()
		if snapshot := s.probationSnapshot; snapshot != nil {
			if !s.probationRefreshing && !now.Before(s.probationRefreshAfter) && !now.Before(s.probationRetryAfter) {
				s.startProbationPeerRefreshLocked()
				// The stale snapshot lets this request return immediately; detach
				// cancellation while retaining context values for the bounded refresh.
				go s.refreshProbationPeerSnapshot(context.WithoutCancel(ctx))
			}
			s.probationMu.Unlock()
			return snapshot, nil
		}
		if s.probationRefreshing {
			done := s.probationRefreshDone
			s.probationMu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if s.probationRefreshErr != nil && now.Before(s.probationRetryAfter) {
			err := s.probationRefreshErr
			s.probationMu.Unlock()
			return nil, err
		}
		s.startProbationPeerRefreshLocked()
		s.probationMu.Unlock()
		s.refreshProbationPeerSnapshot(ctx)
	}
}

func (s *Server) freshProbationPeerSnapshot(ctx context.Context) (*probationPeerSnapshot, error) {
	for {
		now := time.Now()
		s.probationMu.Lock()
		if s.probationSnapshot.fresh(now) {
			snapshot := s.probationSnapshot
			s.probationMu.Unlock()
			return snapshot, nil
		}
		if s.probationRefreshing {
			done := s.probationRefreshDone
			s.probationMu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if s.probationRefreshErr != nil && now.Before(s.probationRetryAfter) {
			err := s.probationRefreshErr
			s.probationMu.Unlock()
			return nil, err
		}
		s.startProbationPeerRefreshLocked()
		s.probationMu.Unlock()
		s.refreshProbationPeerSnapshot(ctx)
	}
}

func (s *Server) startProbationPeerRefreshLocked() {
	s.probationRefreshing = true
	s.probationRefreshDone = make(chan struct{})
}

func (s *Server) refreshProbationPeerSnapshot(ctx context.Context) {
	peers, err := s.loadProbationPeers(ctx)
	if err == nil && len(peers) == 0 {
		err = errors.New("peer source returned no peers")
	}
	now := time.Now()
	var snapshot *probationPeerSnapshot
	if err == nil {
		snapshot = buildProbationPeerSnapshot(peers, now)
	}

	s.probationMu.Lock()
	if err == nil {
		s.probationSnapshot = snapshot
		s.probationRefreshAfter = now.Add(probationPeerSnapshotTTL)
		s.probationRetryAfter = time.Time{}
		s.probationRefreshErr = nil
	} else {
		// A last-known-good binding keeps ordinary traffic available through a
		// management outage. Explainer and petition paths require a fresh binding.
		s.probationRetryAfter = now.Add(probationPeerRetryDelay)
		s.probationRefreshErr = err
	}
	s.probationRefreshing = false
	close(s.probationRefreshDone)
	s.probationMu.Unlock()
	if err != nil {
		slog.Warn("probation peer snapshot refresh failed", slog.String("err", err.Error()))
	}
}

func (s *Server) loadProbationPeers(ctx context.Context) ([]meshstate.Peer, error) {
	if s.probationPeerSource != nil {
		return s.probationPeerSource(ctx)
	}
	return (&meshstate.Client{MgmtURL: s.Cfg.NotifyMgmtURL, Token: s.Cfg.EnrollWatchToken}).Peers(ctx)
}

func buildProbationPeerSnapshot(peers []meshstate.Peer, refreshedAt time.Time) *probationPeerSnapshot {
	snapshot := &probationPeerSnapshot{
		byIP:        make(map[netip.Addr]meshstate.Peer, len(peers)),
		duplicateIP: make(map[netip.Addr]bool),
		refreshedAt: refreshedAt,
	}
	for _, peer := range peers {
		ip, err := netip.ParseAddr(strings.TrimSpace(peer.IP))
		if err != nil {
			continue
		}
		ip = ip.Unmap()
		if _, exists := snapshot.byIP[ip]; exists {
			snapshot.duplicateIP[ip] = true
			continue
		}
		snapshot.byIP[ip] = peer
	}
	return snapshot
}

func (s *Server) probationToken(h probationHold) string {
	mac := hmac.New(sha256.New, []byte(s.csrfNonce))
	_, _ = fmt.Fprintf(mac, "%s\x00%d", h.PeerID, h.EnteredAt.Unix())
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) handleReactivationRequests(w http.ResponseWriter, r *http.Request) {
	page := &reactivationRequestsPage{Declined: s.reactivationDeclineConfirmed(r)}
	if s.Onboard == nil {
		page.Error = "request store is not configured"
	} else if requests, err := s.Onboard.ListOpenReactivationRequests(r.Context()); err != nil {
		page.Error = err.Error()
	} else {
		var reconcileErr error
		requests, reconcileErr = s.reconcileReactivationRequests(r, requests)
		if reconcileErr != nil {
			page.Error = "Some ended holds could not be reconciled; their requests remain open."
		}
		for _, req := range requests {
			q := url.Values{"filter": []string{req.PeerID}}
			page.Rows = append(page.Rows, reactivationRequestRow{
				ReactivationRequest: req,
				HeldSince:           time.Unix(req.HoldEnteredAt, 0).UTC().Format("2006-01-02 15:04 UTC"),
				RequestedAt:         time.Unix(req.RequestedAt, 0).UTC().Format("2006-01-02 15:04 UTC"),
				ContextURL:          "/holds?" + q.Encode(),
			})
		}
	}
	s.render(w, r, "reactivation_requests", &pageVars{Title: "Reactivation requests", ReactivationRequests: page})
}

func (s *Server) reactivationDeclineConfirmed(r *http.Request) bool {
	if s.Onboard == nil {
		return false
	}
	declinedID := strings.TrimSpace(r.URL.Query().Get("declined"))
	if declinedID == "" {
		return false
	}
	declined, err := s.Onboard.GetReactivationRequest(r.Context(), declinedID)
	actor := actorFromCtx(r.Context())
	return err == nil && declined.Status == onboardstore.ReactivationDeclined &&
		actor.Email != "" && strings.EqualFold(declined.DecidedBy, actor.Email)
}

// reconcileReactivationRequests removes a stale ask from the open queue only
// when the complete hold file and live mesh prove that exact hold has ended.
// A releasing record also requires every prior group to be restored. Any
// unavailable or partially-invalid source leaves the row open.
func (s *Server) reconcileReactivationRequests(r *http.Request, requests []onboardstore.ReactivationRequest) ([]onboardstore.ReactivationRequest, error) {
	ctx := r.Context()
	records, err := meshstate.LoadHoldRecords(strings.TrimSpace(s.Cfg.HoldRecordsPath))
	if err != nil {
		return requests, fmt.Errorf("load hold records for reconciliation: %w", err)
	}
	if records.Skipped > 0 {
		return requests, nil
	}
	cli := &meshstate.Client{MgmtURL: s.Cfg.NotifyMgmtURL, Token: s.Cfg.EnrollWatchToken}
	peers, err := cli.Peers(ctx)
	if err != nil {
		return requests, fmt.Errorf("load mesh peers for reconciliation: %w", err)
	}
	byPeer := map[string]meshstate.Peer{}
	duplicatePeer := map[string]bool{}
	for _, p := range peers {
		if _, exists := byPeer[p.ID]; exists {
			duplicatePeer[p.ID] = true
		}
		byPeer[p.ID] = p
	}
	kept := make([]onboardstore.ReactivationRequest, 0, len(requests))
	var reconcileErr error
	for _, req := range requests {
		var rec *meshstate.HoldRecord
		otherSite := false
		for i := range records.All {
			r := &records.All[i]
			if r.PeerID == req.PeerID && r.EnteredAt.Unix() == req.HoldEnteredAt {
				if sameGoatnet(r.Site, s.Cfg.NotifyGoatnet) {
					rec = r
				} else {
					otherSite = true
				}
			}
		}
		if otherSite {
			kept = append(kept, req)
			continue
		}
		ended := false
		peer, peerOK := byPeer[req.PeerID]
		if peerOK && !duplicatePeer[req.PeerID] {
			if rec != nil {
				ended = true
				for _, g := range peer.Groups {
					if strings.EqualFold(g.Name, rec.HoldGroup) || strings.Contains(strings.ToUpper(g.Name), "-PROBATION-") || strings.HasSuffix(strings.ToUpper(g.Name), "-QUARANTINED") {
						ended = false
					}
				}
				if ended && rec.Phase == "releasing" {
					for _, prior := range rec.PriorGroups {
						restored := false
						for _, g := range peer.Groups {
							if strings.EqualFold(g.Name, prior) {
								restored = true
								break
							}
						}
						if !restored {
							ended = false
							break
						}
					}
				}
			} else {
				// With the exact record gone, close only when the live peer is
				// also free of every hold group. A missing/ambiguous peer is not
				// evidence that a supported release completed.
				ended = true
				for _, g := range peer.Groups {
					upper := strings.ToUpper(g.Name)
					if strings.Contains(upper, "-PROBATION-") || strings.HasSuffix(upper, "-QUARANTINED") {
						ended = false
					}
				}
			}
		}
		if ended {
			const reconciler = "system (reconciled: hold ended)"
			closed, closeErr := s.Onboard.CloseReactivationRequest(ctx, req.PeerID, time.Unix(req.HoldEnteredAt, 0), reconciler)
			if closeErr != nil {
				slog.ErrorContext(ctx, "close reconciled reactivation request failed",
					slog.String("request_id", req.ID), slog.String("err", closeErr.Error()))
				kept = append(kept, req)
				if reconcileErr == nil {
					reconcileErr = fmt.Errorf("close reactivation request %s: %w", req.ID, closeErr)
				}
				continue
			}
			if closed {
				s.emitAudit(r, Actor{Email: "system"}, "reconcile_reactivation_request", nil, s.Cfg.NotifyGoatnet, nil,
					map[string]interface{}{"request_id": req.ID, "peer_id": req.PeerID, "hold_entered_at": time.Unix(req.HoldEnteredAt, 0).UTC().Format(time.RFC3339), "reason": "reconciled: hold ended"},
					false, "ok", "")
				continue
			}
		}
		kept = append(kept, req)
	}
	return kept, reconcileErr
}

func (s *Server) handleReactivationDeclinePreview(w http.ResponseWriter, r *http.Request) {
	if s.Onboard == nil {
		s.renderError(w, r, http.StatusServiceUnavailable, "request store is not configured")
		return
	}
	req, err := s.Onboard.GetReactivationRequest(r.Context(), r.PathValue("id"))
	if err != nil || req.Status != onboardstore.ReactivationOpen {
		s.renderError(w, r, http.StatusConflict, "This reactivation request is not open.")
		return
	}
	p := &reactivationDeclinePage{Request: req, HeldSince: time.Unix(req.HoldEnteredAt, 0).UTC().Format("2006-01-02 15:04 UTC")}
	s.render(w, r, "reactivation_decline", &pageVars{Title: "Decline reactivation request", ReactivationDecline: p})
}

func (s *Server) handleReactivationDecline(w http.ResponseWriter, r *http.Request) {
	if s.Onboard == nil {
		s.renderError(w, r, http.StatusServiceUnavailable, "request store is not configured")
		return
	}
	if err := s.validateCSRF(r); err != nil {
		s.renderError(w, r, http.StatusForbidden, err.Error())
		return
	}
	req, err := s.Onboard.GetReactivationRequest(r.Context(), r.PathValue("id"))
	if err != nil || req.Status != onboardstore.ReactivationOpen {
		s.renderError(w, r, http.StatusConflict, "This reactivation request is not open.")
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	if utf8.RuneCountInString(reason) > 500 {
		s.renderError(w, r, http.StatusBadRequest, "Decline reason is too long.")
		return
	}
	actor := actorFromCtx(r.Context())
	ok, err := s.Onboard.DeclineReactivationRequest(r.Context(), req.ID, actor.Email, reason)
	if err != nil || !ok {
		s.renderError(w, r, http.StatusConflict, "This reactivation request is not open.")
		return
	}
	s.emitAudit(r, actor, "decline_reactivation_request", nil, s.Cfg.NotifyGoatnet, nil,
		map[string]interface{}{"request_id": req.ID, "peer_id": req.PeerID, "hold_entered_at": time.Unix(req.HoldEnteredAt, 0).UTC().Format(time.RFC3339), "reason": reason}, false, "ok", "")
	q := url.Values{"declined": []string{req.ID}}
	http.Redirect(w, r, "/reactivation-requests?"+q.Encode(), http.StatusSeeOther)
}
