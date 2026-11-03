package api

import (
	"errors"
	"net/http"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// Key-only reissue (goat#1193): the lighter sibling of /rotate. rekey swaps
// only the portal api_key and leaves the mTLS cert untouched, so there is no
// bundle to download and no peer restart. UpdateVendorAPIKey writes only the
// key hash, so this works in both identity models with no slot allocation, no
// ACL render, and no router restart.

// rekeyResponse carries the fresh key and its anchor — no bundle_url/token,
// because rekey produces no bundle.
type rekeyResponse struct {
	Prefix      string `json:"prefix"`
	APIKey      string `json:"api_key"` // one-time return; bcrypt hash kept in DB
	NoteToShare string `json:"note_to_share"`
}

// rekeyVendor handles POST /api/orgs/{vendor}/rekey (operator-gated). It
// reissues only the portal api_key for an existing peer; the cert is untouched.
func (s *Server) rekeyVendor(w http.ResponseWriter, r *http.Request) {
	prefix := r.PathValue("vendor")
	if prefix == "" {
		writeError(w, http.StatusBadRequest, "decode", "missing org prefix")
		return
	}

	// The peer must already exist — rekey reissues a key, it does not create.
	if _, err := s.reg.GetVendor(r.Context(), prefix); errors.Is(err, registry.ErrNotFound) {
		writeError(w, http.StatusNotFound, "notfound",
			"no such peer; rekey reissues the portal key of an existing peer — use POST /api/vendors to onboard a new one")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	// Fresh portal token + hash; swap the hash in place. The cert, anchor,
	// alias, write namespace, and ACL are all untouched.
	apiKey, err := generateAPIKey(prefix)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "gen_key", err.Error())
		return
	}
	hash, digest, err := hashAPIKey(apiKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash", err.Error())
		return
	}
	if err := s.reg.UpdateVendorAPIKey(r.Context(), prefix, hash, digest); errors.Is(err, registry.ErrNotFound) {
		// The peer existed at the check above but the row is gone by the UPDATE
		// (deleted in the race between the two). That is still "no such peer",
		// not a server fault — surface it as 404, same as the existence check.
		writeError(w, http.StatusNotFound, "notfound",
			"no such peer; rekey reissues the portal key of an existing peer — use POST /api/vendors to onboard a new one")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "update_key", err.Error())
		return
	}

	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "vendor-rekey", prefix,
		`{"cert_touched":false}`)
	writeJSON(w, http.StatusOK, &rekeyResponse{
		Prefix: prefix,
		APIKey: apiKey,
		NoteToShare: "Portal key reissued in place — the mTLS cert, anchor, alias, write namespace, and ACL are all unchanged. " +
			"Hand over the new api_key; nothing to download and no peer restart needed (the cert was not touched, so publishing is unaffected). " +
			"The peer's existing fabric bundle still carries the OLD key, so paste this one into the profile — do not re-run `goat profile init` from that bundle.",
	})
}
