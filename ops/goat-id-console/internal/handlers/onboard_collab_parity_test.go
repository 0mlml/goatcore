package handlers

import (
	"encoding/json"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// F-363: the web invite page ships a collab checkbox that is CHECKED by default,
// while the machine mint request had no collab field at all — so the same invite
// minted through `goat admin invite` silently excluded the person from the
// collaboration environment, with nothing in the response to say so. Two mint
// paths must pre-bind the same thing.
func TestMintInviteRequest_collabDefaultsOnLikeTheWebForm(t *testing.T) {
	var req mintInviteRequest
	if err := json.Unmarshal([]byte(`{"env":"zagros","tier":"developer"}`), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Collab != nil {
		t.Fatal("an absent collab key must decode to nil, not false")
	}
	pb := onboardstore.Prebound{Collab: req.Collab == nil || *req.Collab}
	if !pb.Collab {
		t.Error("omitting collab must inherit the web form's default (on), not silently exclude")
	}
}

func TestMintInviteRequest_collabCanBeTurnedOffExplicitly(t *testing.T) {
	var req mintInviteRequest
	if err := json.Unmarshal([]byte(`{"env":"zagros","collab":false}`), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Collab == nil || *req.Collab {
		t.Fatal("an explicit false must be distinguishable from absent")
	}
	pb := onboardstore.Prebound{Collab: req.Collab == nil || *req.Collab}
	if pb.Collab {
		t.Error("explicit collab:false must exclude")
	}
}
