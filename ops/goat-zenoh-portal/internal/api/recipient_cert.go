package api

// recipient_cert.go serves the by-anchor PUBLIC recipient-cert store — the
// distribution half of the U7 "publish my key" step (grant-keyed-encryption
// §"Recipient-key PKI", the F-267 residual). certmint drops every minted leaf's
// public cert at <RecipientKeysDir>/<anchor>.pem (rotation overwrites, ADR 1044);
// grant-keyd fetches it here and syncs it into its cert_dir, where the
// portal-CA-validated key source (keyd portal.rs) re-validates chain + CN +
// URI-SAN before ever wrapping to it. Serving the cert is DISTRIBUTION, not
// trust: the downstream validation is the guarantee, and the portal is the
// issuer of these leaves, so republishing their public half adds no authority.
//
// Auth mirrors the audience/release-sets reads: operator bearer or the shared
// service token (lookupAuthorized) — grant-keyd is an internal service consumer.
// Public certs only; the private key never exists outside the one-time
// onboarding tarball.

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// recipientAnchorRe is the flat write-anchor shape the mint paths produce (the
// data-access-roles.yaml producer-key grammar). Anything else is refused before
// it can touch the filesystem.
var recipientAnchorRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// handleRecipientCert serves GET /api/access/recipient-cert?anchor=<a>: the
// anchor's current public leaf cert PEM. 404 carries the honest remedy — an
// anchor minted before this store existed has no persisted cert until its leaf
// is rotated (in-place re-issue re-runs the same mint path, which now persists).
func (s *Server) handleRecipientCert(w http.ResponseWriter, r *http.Request) {
	if !s.lookupAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "auth", "operator bearer or service token required")
		return
	}
	if s.cfg.RecipientKeysDir == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"recipient-cert store not configured (recipient-keys-dir)")
		return
	}
	anchor := strings.TrimSpace(r.URL.Query().Get("anchor"))
	if anchor == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "anchor is required")
		return
	}
	if !recipientAnchorRe.MatchString(anchor) {
		writeError(w, http.StatusBadRequest, "bad_request", "anchor is not a valid producer key")
		return
	}
	pem, err := os.ReadFile(filepath.Join(s.cfg.RecipientKeysDir, anchor+".pem"))
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusNotFound, "not_found",
			"no persisted recipient cert for this anchor — its leaf predates the recipient-cert store; rotate it (in-place re-issue, ADR 1044) to publish the current public cert")
		return
	}
	if err != nil {
		// Do not echo err (it carries the store filesystem path).
		writeError(w, http.StatusInternalServerError, "store", "could not read the recipient cert")
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pem)
}
