package api_test

// Identity-keyed rotation (goat#320): the console's half of `admin rotate`
// (ADR 1044). The console authenticates the admin (Ory through oauth2-proxy)
// and names the PERSON whose credential rotates — by Kratos identity UUID,
// never by anchor. The portal resolves identity → slot itself, exactly the
// collect trust boundary: the caller says who, never which anchor. These tests
// pin the properties credential-rotation.md rests on — re-issue IN PLACE (same
// anchor, fresh leaf + api_key, old key dead), refusal shapes that match
// collect's, the operator-bearer gate, and per-person audit attribution
// so a console rotate is never anonymous.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const rotateeIdentity = "7f2a9b1c-3d4e-4f5a-9b6c-8d7e6f5a4b3c"

// rotateByIdentity posts /api/orgs/rotate naming the person, optionally
// carrying the console's X-Goat-Operator-Actor delegated attribution.
func rotateByIdentity(t *testing.T, c *http.Client, baseURL, bearer, actorHeader string, payload any) *http.Response {
	t.Helper()
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", baseURL+"/api/orgs/rotate", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("Content-Type", "application/json")
	if actorHeader != "" {
		req.Header.Set("X-Goat-Operator-Actor", actorHeader)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

// The core contract: re-issue IN PLACE for the slot the identity owns. The
// anchor is unchanged, the fresh key authenticates as that same anchor, and
// the key the person held before is dead — rotation is a re-key, not a copy.
func TestRotateByIdentity_ReissuesInPlaceAndKillsTheOldKey(t *testing.T) {
	dir := t.TempDir()
	ts, _ := newSlotPoolServer(t, dir+"/config.json5")
	c := &http.Client{}

	onboard := doJSON(t, c, "POST", ts.URL+"/api/orgs", "dev-operator-key", map[string]any{
		"prefix": "fra-land-bde", "identity_id": rotateeIdentity,
	})
	if onboard.StatusCode != http.StatusCreated {
		t.Fatalf("onboard: status %d body=%s", onboard.StatusCode, mustReadBody(t, onboard))
	}
	ob := mustReadJSON(t, onboard)
	anchor, _ := ob["slot_id"].(string)
	oldKey, _ := ob["api_key"].(string)
	if anchor == "" || oldKey == "" {
		t.Fatalf("onboard returned no slot/key: %#v", ob)
	}

	resp := rotateByIdentity(t, c, ts.URL, "dev-operator-key", "",
		map[string]any{"identity_id": rotateeIdentity})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rotate: status %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	body := mustReadJSON(t, resp)

	if got, _ := body["slot_id"].(string); got != anchor {
		t.Errorf("slot_id = %q, want the SAME anchor %q — rotation must not re-anchor", got, anchor)
	}
	newKey, _ := body["api_key"].(string)
	if newKey == "" || newKey == oldKey {
		t.Fatalf("api_key = %q — rotation must mint a fresh key (old was %q)", newKey, oldKey)
	}
	for _, k := range []string{"bundle_url", "bundle_token", "uri_san"} {
		if v, _ := body[k].(string); v == "" {
			t.Errorf("%s missing from the rotation response", k)
		}
	}

	// The fresh key IS this anchor now…
	who := doJSON(t, c, "GET", ts.URL+"/api/whoami", newKey, nil)
	if who.StatusCode != http.StatusOK {
		t.Fatalf("whoami with the rotated key: status %d", who.StatusCode)
	}
	if got, _ := mustReadJSON(t, who)["vendor"].(string); got != anchor {
		t.Errorf("rotated key resolves to %q, want %q", got, anchor)
	}
	// …and the old key authenticates to nothing.
	whoOld := doJSON(t, c, "GET", ts.URL+"/api/whoami", oldKey, nil)
	if whoOld.StatusCode != http.StatusUnauthorized {
		t.Errorf("old key after rotation: status %d, want 401 — the re-key did not invalidate it",
			whoOld.StatusCode)
	}
}

// The caller names WHO, never WHICH anchor — same closed door as collect. A
// request that smuggles a slot/anchor field is refused outright, and the
// legitimate rotate that follows lands on the identity's own slot.
func TestRotateByIdentity_CallerCannotNameAnAnchor(t *testing.T) {
	dir := t.TempDir()
	ts, _ := newSlotPoolServer(t, dir+"/config.json5")
	c := &http.Client{}
	const otherIdentity = "11112222-3333-4444-5555-666677778888"

	mine := doJSON(t, c, "POST", ts.URL+"/api/orgs", "dev-operator-key",
		map[string]any{"prefix": "fra-land-bde", "identity_id": rotateeIdentity})
	if mine.StatusCode != http.StatusCreated {
		t.Fatalf("onboard mine: %d", mine.StatusCode)
	}
	myAnchor, _ := mustReadJSON(t, mine)["slot_id"].(string)

	theirs := doJSON(t, c, "POST", ts.URL+"/api/orgs", "dev-operator-key",
		map[string]any{"prefix": "isr-recon-det", "identity_id": otherIdentity})
	if theirs.StatusCode != http.StatusCreated {
		t.Fatalf("onboard theirs: %d", theirs.StatusCode)
	}
	theirAnchor, _ := mustReadJSON(t, theirs)["slot_id"].(string)

	steered := rotateByIdentity(t, c, ts.URL, "dev-operator-key", "", map[string]any{
		"identity_id": rotateeIdentity,
		"slot_id":     theirAnchor,
	})
	if steered.StatusCode != http.StatusBadRequest {
		t.Fatalf("request naming a slot: status %d, want 400 (body=%s)",
			steered.StatusCode, mustReadBody(t, steered))
	}

	honest := rotateByIdentity(t, c, ts.URL, "dev-operator-key", "",
		map[string]any{"identity_id": rotateeIdentity})
	if honest.StatusCode != http.StatusOK {
		t.Fatalf("rotate: status %d body=%s", honest.StatusCode, mustReadBody(t, honest))
	}
	if got, _ := mustReadJSON(t, honest)["slot_id"].(string); got != myAnchor {
		t.Fatalf("rotated %q, want the identity's own slot %q", got, myAnchor)
	}
}

// Nothing to rotate must read as nothing to rotate — and a malformed identity
// is refused at the door, matching collect's shapes exactly.
func TestRotateByIdentity_UnknownAndMalformedIdentitiesRefused(t *testing.T) {
	dir := t.TempDir()
	ts, _ := newSlotPoolServer(t, dir+"/config.json5")
	c := &http.Client{}

	resp := rotateByIdentity(t, c, ts.URL, "dev-operator-key", "",
		map[string]any{"identity_id": rotateeIdentity})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown identity: status %d, want 404 (body=%s)", resp.StatusCode, mustReadBody(t, resp))
	}
	for _, bad := range []string{"", "dana", "dana@example.test", "not-a-uuid"} {
		resp := rotateByIdentity(t, c, ts.URL, "dev-operator-key", "",
			map[string]any{"identity_id": bad})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("identity_id=%q: status %d, want 400", bad, resp.StatusCode)
		}
	}
}

// A slot with no collected credential has nothing to rotate. Minting here
// would be a first issue through the wrong door — it would burn the person's
// one-time collection (fabric-credential-collection.md) on a rotation nobody
// can hand to them. Refuse and name the right door instead.
func TestRotateByIdentity_RefusesAnUncollectedSlot(t *testing.T) {
	dir := t.TempDir()
	ts, reg := newSlotPoolServer(t, dir+"/config.json5")
	c := &http.Client{}
	seedUncollectedSlot(t, reg, "fra-land-bde", rotateeIdentity)

	resp := rotateByIdentity(t, c, ts.URL, "dev-operator-key", "",
		map[string]any{"identity_id": rotateeIdentity})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("rotate on an uncollected slot: status %d, want 409 (body=%s)",
			resp.StatusCode, mustReadBody(t, resp))
	}
	if code := errCode(t, resp); code != "not_minted" {
		t.Errorf("error code = %q, want not_minted", code)
	}

	// The refusal must not have minted anything: the slot still collects.
	slot, err := reg.SlotForIdentity(t.Context(), rotateeIdentity)
	if err != nil {
		t.Fatalf("SlotForIdentity: %v", err)
	}
	if slot.CertMintedAt != nil || slot.CollectedAt != nil {
		t.Error("refused rotation minted/collected the slot anyway")
	}
}

// Two live slots under one identity is broken state; rotating a coin-flip of
// the two re-keys somebody's working credential at random. Fail closed, same
// as collect.
func TestRotateByIdentity_DuplicateLinkageRefusedRatherThanGuessed(t *testing.T) {
	dir := t.TempDir()
	ts, reg := newSlotPoolServer(t, dir+"/config.json5")
	c := &http.Client{}
	seedUncollectedSlot(t, reg, "fra-land-bde", rotateeIdentity)
	seedUncollectedSlot(t, reg, "isr-recon-det", rotateeIdentity)

	resp := rotateByIdentity(t, c, ts.URL, "dev-operator-key", "",
		map[string]any{"identity_id": rotateeIdentity})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate linkage: status %d, want 409 (body=%s)", resp.StatusCode, mustReadBody(t, resp))
	}
	if code := errCode(t, resp); code != "ambiguous_identity" {
		t.Errorf("error code = %q, want ambiguous_identity", code)
	}
}

// Holding an operator key IS the trust boundary. No bearer and an
// ordinary org key are both refused — and the person's working credential is
// untouched by either refusal.
func TestRotateByIdentity_RequiresOperatorBearer(t *testing.T) {
	dir := t.TempDir()
	ts, _ := newSlotPoolServer(t, dir+"/config.json5")
	c := &http.Client{}

	onboard := doJSON(t, c, "POST", ts.URL+"/api/orgs", "dev-operator-key",
		map[string]any{"prefix": "fra-land-bde", "identity_id": rotateeIdentity})
	if onboard.StatusCode != http.StatusCreated {
		t.Fatalf("onboard: %d", onboard.StatusCode)
	}
	ob := mustReadJSON(t, onboard)
	anchor, _ := ob["slot_id"].(string)
	victimKey, _ := ob["api_key"].(string)

	if resp := rotateByIdentity(t, c, ts.URL, "", "",
		map[string]any{"identity_id": rotateeIdentity}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no bearer: status %d, want 401", resp.StatusCode)
	}

	// A second org's own legitimate key must not rotate ANYONE's credential —
	// that would let any fabric member re-key (deny service to) any other.
	second := doJSON(t, c, "POST", ts.URL+"/api/orgs", "dev-operator-key",
		map[string]any{"prefix": "isr-recon-det", "identity_id": "11112222-3333-4444-5555-666677778888"})
	if second.StatusCode != http.StatusCreated {
		t.Fatalf("second onboard: %d", second.StatusCode)
	}
	orgKey, _ := mustReadJSON(t, second)["api_key"].(string)
	if resp := rotateByIdentity(t, c, ts.URL, orgKey, "",
		map[string]any{"identity_id": rotateeIdentity}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("org key: status %d, want 403", resp.StatusCode)
	}

	// The victim's credential still works after both refusals.
	who := doJSON(t, c, "GET", ts.URL+"/api/whoami", victimKey, nil)
	if who.StatusCode != http.StatusOK {
		t.Fatalf("victim key after refused rotations: status %d — a refusal re-keyed it", who.StatusCode)
	}
	if got, _ := mustReadJSON(t, who)["vendor"].(string); got != anchor {
		t.Errorf("victim key resolves to %q, want %q", got, anchor)
	}
}

// A console rotate is never anonymous: the audit row carries the admin the
// console named via X-Goat-Operator-Actor, the
// same stream and action the CLI path writes — and never the api_key.
func TestRotateByIdentity_AuditRowNamesTheHumanAndCarriesNoSecret(t *testing.T) {
	dir := t.TempDir()
	ts, reg := newSlotPoolServer(t, dir+"/config.json5")
	c := &http.Client{}

	onboard := doJSON(t, c, "POST", ts.URL+"/api/orgs", "dev-operator-key",
		map[string]any{"prefix": "fra-land-bde", "identity_id": rotateeIdentity})
	if onboard.StatusCode != http.StatusCreated {
		t.Fatalf("onboard: %d", onboard.StatusCode)
	}
	anchor, _ := mustReadJSON(t, onboard)["slot_id"].(string)

	resp := rotateByIdentity(t, c, ts.URL, "dev-operator-key", "dana.admin@example.test",
		map[string]any{"identity_id": rotateeIdentity})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rotate: status %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	newKey, _ := mustReadJSON(t, resp)["api_key"].(string)

	events, err := reg.AuditEvents(t.Context(), "vendor-rotate", 10)
	if err != nil {
		t.Fatalf("AuditEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no vendor-rotate audit row — the console path must write the SAME stream as the CLI path")
	}
	ev := events[0]
	if ev.Actor != "dana.admin@example.test" {
		t.Errorf("audit actor = %q, want the named admin — a console rotate must not be anonymous", ev.Actor)
	}
	if ev.Target != anchor {
		t.Errorf("audit target = %q, want %q", ev.Target, anchor)
	}
	if newKey != "" && strings.Contains(ev.Details, newKey) {
		t.Error("audit details contain the fresh api_key — secrets must never land in the audit stream")
	}
}

// Slot-pool goatnets only, same refusal as collect: per-vendor mode has no
// person→slot linkage to resolve, so refuse plainly rather than half-work.
func TestRotateByIdentity_RefusedOnPerVendorGoatnets(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	resp := rotateByIdentity(t, c, ts.URL, "dev-operator-key", "",
		map[string]any{"identity_id": rotateeIdentity})
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("per-vendor goatnet: status %d, want 501 (body=%s)", resp.StatusCode, mustReadBody(t, resp))
	}
	if code := errCode(t, resp); code != "not_slot_mode" {
		t.Errorf("error code = %q, want not_slot_mode", code)
	}
}
