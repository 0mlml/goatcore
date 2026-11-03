package handlers

// Slot→identity binding (ADR 1051, the registry-side linkage).
//
// A portal slot records `identity_id`: the Kratos identity UUID of the person
// the slot was onboarded for. It is what makes "which slot belongs to this
// person" answerable directly rather than inferred. The portal has always
// supported it — it validates the UUID, stamps it, exposes an operator-gated
// PUT to set it later, and logs a WARNING when a slot is onboarded without one:
//
//	"slot onboarded with no recorded identity linkage — ownership for this slot
//	 will be derived by name join (alias↔handle), the fallback the linkage
//	 exists to retire"
//
// That warning has been firing because the SENDING side never filled it in: the
// Rust portal client's NewVendor has no identity_id field, so the native mint
// cannot pass one. On the ZAGROS gateway that left 105 of 197 live person slots
// with no linkage, and both machine slots.
//
// Why the console stamps it rather than the mint. The native mint provisions in
// a deliberate order — NetBird, then Zenoh, then Ory LAST, because the Ory leg
// is the one allowed to fail without failing the onboard (the mint reports
// "partial"). The Kratos identity therefore does not exist yet when the slot is
// created, and reordering to fix that would turn a degraded outcome into a total
// one. So the linkage is written after the fact, from the console, which by then
// knows both halves: it owns the Kratos identity and receives the slot anchor in
// the runner's result writeback.
//
// Best-effort by contract. A failure here costs an inferred-by-name-join
// ownership lookup, exactly the status quo — it must never fail an onboard that
// otherwise succeeded.

import (
	"bytes"
	"context"
	"encoding/json"

	"log/slog"
	"net/http"
	"regexp"
	"strings"
)

// slotAnchorRe pulls the slot anchor out of the runner-reported bundle path
// (/api/org-bundle/<anchor>/<token>.tar.gz). The anchor is the portal's slot id,
// which is what PUT /api/slots/{anchor}/identity keys on.
var slotAnchorRe = regexp.MustCompile(`/api/org-bundle/([A-Za-z0-9]+)/`)

// slotAnchorFromBundlePath returns the slot anchor embedded in a bundle path, or
// "" when the path is absent or shaped differently (in which case there is
// nothing to stamp and the caller no-ops rather than guessing).
func slotAnchorFromBundlePath(bundlePath string) string {
	m := slotAnchorRe.FindStringSubmatch(bundlePath)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

// resolveSlotAnchor picks the slot anchor to stamp the linkage against, and
// reports whether it had to fall back to deriving one from the bundle path.
//
// Precedence is the point: the anchor the MINT STATED beats one re-derived by
// parsing an artifact path. The fallback exists only for a writeback that
// predates `slot_anchor`, and it cannot serve a slot that has no bundle — which
// is precisely what a deferred (mint-on-collect) onboard produces.
func resolveSlotAnchor(statedAnchor, bundlePath string) (anchor string, derived bool) {
	if a := strings.TrimSpace(statedAnchor); a != "" {
		return a, false
	}
	if a := slotAnchorFromBundlePath(bundlePath); a != "" {
		return a, true
	}
	return "", false
}

// stampSlotIdentity records the person's Kratos identity on their portal slot.
// Best-effort: every failure is logged and swallowed.
//
// statedAnchor is the anchor the MINT reported (runner writeback `slot_anchor`).
// It is preferred over bundlePath, which is only a fallback for a writeback that
// predates the field. Deriving the anchor by regexing a tarball path was always
// the fragile form — it is the same "re-derive ownership by parsing names"
// pattern the linkage exists to retire — and it becomes wrong, not merely ugly,
// once a slot can legitimately have no bundle: mint-on-collect defers the
// credential, so a deferred onboard has a real slot and no tarball, and the
// derivation would read that as "no fabric leg, nothing to link" and skip in
// silence. The person would then own a slot the registry cannot attribute to
// them, and their collection page would answer "no slot recorded for you"
// forever (docs/design/fabric-credential-collection.md).
//
// It is a no-op when the portal is unreachable-by-config, when NEITHER an anchor
// nor a bundle path is available (a genuinely fabric-less onboard), or when the
// identity cannot be resolved — never an error to the caller.
func (s *Server) stampSlotIdentity(ctx context.Context, handle, email, statedAnchor, bundlePath string) {
	base := strings.TrimRight(strings.TrimSpace(s.Cfg.PortalInternalURL), "/")
	token := strings.TrimSpace(s.Cfg.PortalLookupToken)
	if base == "" || token == "" {
		return
	}
	anchor, derived := resolveSlotAnchor(statedAnchor, bundlePath)
	if derived {
		slog.Info("slot identity linkage: anchor derived from the bundle path (writeback carried no slot_anchor)",
			"handle", handle, "anchor", anchor)
	}
	if anchor == "" {
		// Genuinely no slot: a login-only / mesh-only onboard. Distinguished from
		// "we could not work out the anchor" by the runner now stating it — a
		// fabric onboard that reaches here with neither is a wiring fault, so say
		// so rather than returning quietly as if there were nothing to do.
		if strings.TrimSpace(bundlePath) != "" {
			slog.Warn("slot identity linkage SKIPPED — a bundle exists but no anchor could be resolved from it "+
				"and the writeback carried no slot_anchor; this slot will have no recorded owner",
				"handle", handle, "bundle_path", bundlePath)
		}
		return
	}
	identityID := s.kratosIdentityIDForEmail(ctx, email)
	if identityID == "" {
		slog.Warn("slot identity linkage skipped — no Kratos identity resolved for this onboard; "+
			"slot ownership stays derived by name join",
			"handle", handle, "email", email, "anchor", anchor)
		return
	}

	body, err := json.Marshal(map[string]string{"identity_id": identityID})
	_ = err // map[string]string is always JSON-marshalable; encode failure is not expected here.
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		base+"/api/slots/"+anchor+"/identity", bytes.NewReader(body))
	if err != nil {
		slog.Warn("slot identity linkage: build request failed", "err", err, "anchor", anchor)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := onboardHTTP.Do(req)
	if err != nil {
		slog.Warn("slot identity linkage: portal unreachable", "err", err, "anchor", anchor, "handle", handle)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("slot identity linkage: portal refused",
			"status", resp.StatusCode, "anchor", anchor, "handle", handle)
		return
	}
	slog.Info("slot identity linkage recorded", "handle", handle, "anchor", anchor, "identity_id", identityID)
}

// kratosIdentityIDForEmail resolves the tenant Kratos identity UUID for an
// email. "" on any miss — the caller degrades rather than guessing, because a
// WRONG binding is worse than none: it would assert that one person owns
// another's namespace, and ownership is what revocation targets.
func (s *Server) kratosIdentityIDForEmail(ctx context.Context, email string) string {
	cli := s.Kratos["tenant"]
	email = strings.TrimSpace(email)
	if cli == nil || email == "" {
		return ""
	}
	ids, err := cli.ListIdentities(ctx)
	if err != nil {
		slog.Warn("slot identity linkage: list identities failed", "err", err)
		return ""
	}
	var found string
	for _, i := range ids {
		if !strings.EqualFold(strings.TrimSpace(i.Email()), email) {
			continue
		}
		if found != "" && found != i.ID {
			// Two identities share this address. Refuse rather than pick one:
			// binding the wrong one mis-assigns namespace ownership.
			slog.Warn("slot identity linkage: email matches MORE THAN ONE identity — refusing to guess",
				"email", email, "first", found, "second", i.ID)
			return ""
		}
		found = i.ID
	}
	return found
}
