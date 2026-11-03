package onboardstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// A row approved before the peer key lifetime was cut from 90 days to 7
// carries netbird.ttl_days 90 and no ttl_reason. The mint refuses
// that, so restore and retry must hand the runner a manifest it will accept,
// or every such row goes archived -> approved -> failed and stops there.
const legacyManifest = `{"identity":{"handle":"legacy","email":"legacy@example.test","name":"Legacy","status":"active","added":"2026-06-01","added_by":"admin@example.test"},"netbird":{"auto_groups":["DEVELOPER"],"enabled":true,"role":"peer","sites":["zagros"],"ttl_days":90,"usage_limit":5},"ory_site":{"enabled":false},"ory_idp":{"enabled":false},"zenoh":{"enabled":false}}`

func netbirdOf(t *testing.T, manifest string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(manifest), &m); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	nb, _ := m["netbird"].(map[string]any)
	return nb
}

func TestDropUnjustifiedTTL(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantDrop bool
	}{
		{"legacy 90-day peer key, no reason", legacyManifest, true},
		{"reason too short to satisfy the mint", `{"netbird":{"role":"peer","ttl_days":90,"ttl_reason":"n/a"}}`, true},
		{"justified override is kept", `{"netbird":{"role":"peer","ttl_days":90,"ttl_reason":"issued ahead of the Rota movement; enrolled on arrival"}}`, false},
		{"at the default", `{"netbird":{"role":"peer","ttl_days":7}}`, false},
		{"shorter than the default", `{"netbird":{"role":"peer","ttl_days":3}}`, false},
		{"operator role allows 30", `{"netbird":{"role":"operator","ttl_days":30}}`, false},
		{"no ttl_days", `{"netbird":{"role":"peer"}}`, false},
		{"no netbird block", `{"identity":{"handle":"x"}}`, false},
		{"not JSON", `not json`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, changed := dropUnjustifiedTTL(c.in)
			if changed != c.wantDrop {
				t.Fatalf("changed = %v, want %v (out %s)", changed, c.wantDrop, out)
			}
			if !changed {
				if out != c.in {
					t.Fatalf("unchanged manifest was rewritten: %s", out)
				}
				return
			}
			nb := netbirdOf(t, out)
			if _, still := nb["ttl_days"]; still {
				t.Fatalf("ttl_days survived: %s", out)
			}
			// Everything else in the block is untouched, integers stay integers.
			if nb["role"] != "peer" {
				t.Fatalf("role lost: %s", out)
			}
		})
	}
	out, _ := dropUnjustifiedTTL(legacyManifest)
	if nb := netbirdOf(t, out); nb["usage_limit"] != float64(5) || nb["enabled"] != true {
		t.Fatalf("other netbird fields changed: %s", out)
	}
}

func TestRestoreAndRetryDropLegacyTTL(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	seed := func(id, status string) {
		t.Helper()
		p := &Proposal{ID: id, Sponsor: "s", Env: "zagros", Handle: id, Email: id + "@x", Status: ProposalProposed, ManifestJSON: legacyManifest}
		if err := st.CreateProposal(ctx, p); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
		if err := st.DecideProposal(ctx, id, status, "t", ""); err != nil {
			t.Fatalf("set status %s: %v", status, err)
		}
	}
	check := func(id string) {
		t.Helper()
		got, err := st.GetProposal(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != ProposalApproved {
			t.Fatalf("%s status = %s, want approved", id, got.Status)
		}
		if _, still := netbirdOf(t, got.ManifestJSON)["ttl_days"]; still {
			t.Fatalf("%s still asks for the legacy key lifetime: %s", id, got.ManifestJSON)
		}
	}

	seed("prop-archived", ProposalOnboarded)
	if ok, err := st.ArchiveProposal(ctx, "prop-archived", "key expired unredeemed"); err != nil || !ok {
		t.Fatalf("archive: ok=%v err=%v", ok, err)
	}
	if ok, err := st.RestoreProposal(ctx, "prop-archived", "admin@x"); err != nil || !ok {
		t.Fatalf("restore: ok=%v err=%v", ok, err)
	}
	check("prop-archived")

	seed("prop-failed", ProposalFailed)
	if err := st.RetryProposal(ctx, "prop-failed"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	check("prop-failed")

	// The guards are unchanged: restore of a non-archived row and retry of a
	// non-failed row still move nothing.
	if ok, err := st.RestoreProposal(ctx, "prop-failed", "admin@x"); err != nil || ok {
		t.Fatalf("restore of an approved row: ok=%v err=%v", ok, err)
	}
	if err := st.RetryProposal(ctx, "prop-archived"); !errors.Is(err, ErrProposalNotFailed) {
		t.Fatalf("retry of an approved row: err=%v, want ErrProposalNotFailed", err)
	}
	if err := st.RetryProposal(ctx, "prop-missing"); err == nil || errors.Is(err, ErrProposalNotFailed) {
		t.Fatalf("retry of a missing row: err=%v, want not-found", err)
	}
}
