package registry

import (
	"context"
	"testing"
)

func TestLabelsValidate(t *testing.T) {
	cases := []struct {
		name    string
		l       Labels
		wantErr bool
	}{
		{"all unlabeled", Labels{}, false},
		{"valid full", Labels{Fidelity: "real", Context: "operational", Access: "controlled", Justification: "PII"}, false},
		{"community needs no justification", Labels{Access: "community"}, false},
		{"org needs justification", Labels{Access: "org"}, true},
		{"controlled needs justification", Labels{Access: "controlled"}, true},
		{"org with justification", Labels{Access: "org", Justification: "unit only"}, false},
		{"bad fidelity", Labels{Fidelity: "fake"}, true},
		{"bad context", Labels{Context: "training"}, true},
		{"bad access", Labels{Access: "secret"}, true},
		{"case-insensitive value", Labels{Fidelity: "REAL"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.l.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate()=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestEffectiveFidelity(t *testing.T) {
	if got := EffectiveFidelity("real", true); got != "real" {
		t.Fatalf("explicit lane wins: %q", got)
	}
	if got := EffectiveFidelity("", true); got != "notional" {
		t.Fatalf("synthetic bridges to notional: %q", got)
	}
	if got := EffectiveFidelity("", false); got != "" {
		t.Fatalf("unlabeled non-synthetic stays unlabeled: %q", got)
	}
}

func TestEffectiveAccess(t *testing.T) {
	if got := EffectiveAccess("controlled", "community"); got != "controlled" {
		t.Fatalf("explicit access wins: %q", got)
	}
	if got := EffectiveAccess("", "org"); got != "org" {
		t.Fatalf("goatnet default applied: %q", got)
	}
	if got := EffectiveAccess("", ""); got != AccessCommunity {
		t.Fatalf("empty default fails safe-open to community: %q", got)
	}
}

func TestSetVendorLabelsAndDrift(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()
	for _, p := range []string{"acme", "beta", "gamma"} {
		if err := reg.CreateVendor(ctx, &Vendor{Prefix: p, APIKeyHash: "x", DisplayName: p}); err != nil {
			t.Fatalf("create vendor %s: %v", p, err)
		}
	}
	// acme restricts (with justification), beta stays community, gamma unlabeled.
	if err := reg.SetVendorLabels(ctx, "acme", Labels{Fidelity: "real", Access: "controlled", Justification: "sensitive"}); err != nil {
		t.Fatalf("set labels acme: %v", err)
	}
	if err := reg.SetVendorLabels(ctx, "beta", Labels{Access: "community"}); err != nil {
		t.Fatalf("set labels beta: %v", err)
	}
	// Restriction without justification is refused (the gate).
	if err := reg.SetVendorLabels(ctx, "gamma", Labels{Access: "org"}); err == nil {
		t.Fatal("expected justification-gate refusal")
	}
	// Unknown producer.
	if err := reg.SetVendorLabels(ctx, "nope", Labels{Access: "community"}); err == nil {
		t.Fatal("expected ErrNotFound for unknown producer")
	}
	v, _ := reg.GetVendor(ctx, "acme")
	if v.LabelAccess != "controlled" || v.LabelJustification != "sensitive" {
		t.Fatalf("labels not persisted: %+v", v)
	}
	vendors, _ := reg.ListVendors(ctx)
	drift := ComputeLabelDrift(vendors, "community")
	// acme=controlled (restricted); beta=community; gamma unlabeled->community default.
	if drift.Producers != 3 || drift.Restricted != 1 || drift.Community != 2 {
		t.Fatalf("drift counts off: %+v", drift)
	}
	if drift.RestrictedShare < 0.33 || drift.RestrictedShare > 0.34 {
		t.Fatalf("restricted share %v not ~1/3", drift.RestrictedShare)
	}
}

func TestSyntheticMigratesToNotionalFidelity(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()
	if err := reg.CreateVendor(ctx, &Vendor{Prefix: "synth1", APIKeyHash: "x", DisplayName: "s", Synthetic: true}); err != nil {
		t.Fatalf("create synthetic vendor: %v", err)
	}
	// The additive migration runs at Open(); reopen the same DB path to trigger it
	// on the already-synthetic row. Use EffectiveFidelity directly for the live row.
	v, _ := reg.GetVendor(ctx, "synth1")
	if got := EffectiveFidelity(v.LabelFidelity, v.Synthetic); got != "notional" {
		t.Fatalf("synthetic vendor should read as notional fidelity, got %q", got)
	}
}
