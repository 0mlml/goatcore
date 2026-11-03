// cmd_test.go — unit tests for pure functions in mgmt-host-bootstrap.
//
// These cover the deterministic shape transformations that decode netbird's
// GORM-tag expectations into raw values. Each test corresponds to one of the
// nine critical invariants documented in README.md — drift here is the
// primary whack-a-mole failure mode.
//
// Integration tests (real netbird-mgmt + Postgres + peer enrollment) live
// in cmd_integration_test.go behind a build tag.

package main

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"strings"
	"testing"
)

// TestHashSetupKey verifies the base64(sha256(UPPER(uuid))) pattern netbird's
// GenerateSetupKey computes. Source: management/server/types/setupkey.go:155-187.
//
// Drift here means future netbird versions store the key differently and our
// CLI is producing rows that fail authentication. CI gates pin bumps on this.
func TestHashSetupKey(t *testing.T) {
	cases := []struct {
		plaintext string
		want      string
	}{
		// Lower-case input — must be uppercased before hashing.
		{
			plaintext: "7e12158f-d411-4d3a-b50c-1fb5ad3de97d",
			want:      "qJ1EVyA4FTrxATmZ0OLvrCVRlFSU5Cv12XXf35kBcXc=",
		},
		// Upper-case input — same result.
		{
			plaintext: "7E12158F-D411-4D3A-B50C-1FB5AD3DE97D",
			want:      "qJ1EVyA4FTrxATmZ0OLvrCVRlFSU5Cv12XXf35kBcXc=",
		},
		// Mixed-case input.
		{
			plaintext: "AaAa1111-bBbB-2222-cCcC-3333dDdD4444",
			want:      "VqWVAWtNo5iqGTua9Fb4z+FzuLeghnu6pFjQJlfNib4=",
		},
		// Site B's setup key from the cgnat-validation run.
		{
			plaintext: "f38d6440-8af6-4b3d-979e-4b7497fe35d1",
			want:      "IMbMA6VxAaDRlN/+S8RCxV9dbyyYH2Flil3Co6zGDoc=",
		},
	}
	for _, tc := range cases {
		t.Run(tc.plaintext, func(t *testing.T) {
			got := hashSetupKey(tc.plaintext)
			if got != tc.want {
				t.Errorf("hashSetupKey(%q) = %q, want %q", tc.plaintext, got, tc.want)
			}
			// Sanity: base64-decoded should be exactly 32 bytes (sha256).
			raw, err := base64.StdEncoding.DecodeString(got)
			if err != nil {
				t.Fatalf("base64 decode of result failed: %v", err)
			}
			if len(raw) != 32 {
				t.Errorf("decoded hash length = %d, want 32", len(raw))
			}
		})
	}
}

// TestHiddenKey verifies the masked-display format that goes in
// setup_keys.key_secret. Source: setupkey.go:111-117.
//
// Important: this column is NOT a validation field. It exists for the
// dashboard to show users what their key looked like. Drift here doesn't
// break enrollment but breaks dashboard rendering for cross-product UIs.
func TestHiddenKey(t *testing.T) {
	cases := []struct {
		plaintext string
		want      string
	}{
		{plaintext: "AaAa1111-bbbb-2222-cccc-3333dddd4444", want: "AAAA1****"},
		{plaintext: "7e12158f-d411-4d3a-b50c-1fb5ad3de97d", want: "7E121****"},
		{plaintext: "F38D6440-8AF6-4B3D-979E-4B7497FE35D1", want: "F38D6****"},
		{plaintext: "abc", want: "ABC****"}, // shorter than 5 falls through
	}
	for _, tc := range cases {
		t.Run(tc.plaintext, func(t *testing.T) {
			got := hiddenKey(tc.plaintext)
			if got != tc.want {
				t.Errorf("hiddenKey(%q) = %q, want %q", tc.plaintext, got, tc.want)
			}
		})
	}
}

// TestNetworkNetJSON verifies the JSON shape of accounts.network_net.
// Source: management/server/types/network.go:113 (`Net net.IPNet`
// with `gorm:"serializer:json"`).
//
// Format: {"IP":"<dotted>","Mask":"<base64>"} — IP as quoted string
// (Go's net.IP.MarshalJSON), Mask as base64 of the 4-byte mask
// (default []byte marshaling).
//
// This was *the* surprise that took an hour of cloud iteration to find —
// see the attempt-1 findings doc §1.3 (corrected).
func TestNetworkNetJSON(t *testing.T) {
	cases := []struct {
		cidr   string
		wantIP string
		// Mask is round-tripped to defend against base64 representation
		// drift on different Go versions.
		wantMaskBytes []byte
	}{
		{cidr: "100.64.0.0/16", wantIP: "100.64.0.0", wantMaskBytes: []byte{0xff, 0xff, 0, 0}},
		{cidr: "100.65.0.0/16", wantIP: "100.65.0.0", wantMaskBytes: []byte{0xff, 0xff, 0, 0}},
		{cidr: "10.0.0.0/8", wantIP: "10.0.0.0", wantMaskBytes: []byte{0xff, 0, 0, 0}},
		{cidr: "192.168.1.0/24", wantIP: "192.168.1.0", wantMaskBytes: []byte{0xff, 0xff, 0xff, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.cidr, func(t *testing.T) {
			s, err := networkNetJSON(tc.cidr)
			if err != nil {
				t.Fatalf("networkNetJSON(%q) returned error: %v", tc.cidr, err)
			}
			// Round-trip through net.IPNet must yield the same CIDR.
			var got net.IPNet
			if err := json.Unmarshal([]byte(s), &got); err != nil {
				t.Fatalf("output %q does not unmarshal as net.IPNet: %v", s, err)
			}
			if got.IP.String() != tc.wantIP {
				t.Errorf("IP = %q, want %q", got.IP.String(), tc.wantIP)
			}
			if !maskEqual(got.Mask, tc.wantMaskBytes) {
				t.Errorf("Mask bytes = %v, want %v", []byte(got.Mask), tc.wantMaskBytes)
			}
			// Sanity: the JSON should contain a quoted IP string (not base64).
			// netbird's net.IP.UnmarshalJSON rejects base64 with
			// "invalid IP address: ZEAAAA==" — we explicitly avoid that.
			if !strings.Contains(s, `"IP":"`) {
				t.Errorf("output should encode IP as quoted string; got %q", s)
			}
			if !strings.Contains(s, `"Mask":"`) {
				t.Errorf("output should encode Mask as quoted base64; got %q", s)
			}
		})
	}
}

func maskEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestNetworkNetJSON_BadCIDR rejects invalid CIDR input cleanly.
func TestNetworkNetJSON_BadCIDR(t *testing.T) {
	if _, err := networkNetJSON("not a cidr"); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
	if _, err := networkNetJSON(""); err == nil {
		t.Fatal("expected error for empty input")
	}
}

// TestGenerateAccountID checks the structural form of generated IDs.
// Pattern: acc_<site-name>_<8-hex-of-uuid>.
func TestGenerateAccountID(t *testing.T) {
	id := generateAccountID("vega")
	if !strings.HasPrefix(id, "acc_vega_") {
		t.Errorf("generated ID %q should start with acc_vega_", id)
	}
	suffix := strings.TrimPrefix(id, "acc_vega_")
	if len(suffix) != 8 {
		t.Errorf("suffix %q length = %d, want 8", suffix, len(suffix))
	}
	for _, c := range suffix {
		// Hex char (0-9, a-f) — uuid.New() returns lowercase.
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("suffix %q contains non-hex char %c", suffix, c)
		}
	}
	// Two calls must yield different IDs.
	if id == generateAccountID("vega") {
		t.Error("two calls returned the same ID")
	}
}
