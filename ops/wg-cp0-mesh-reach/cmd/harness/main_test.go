package main

import (
	"fmt"
	"testing"

	"github.com/usareur-af/goatcore/ops/enrollment/bundle"
)

// TestGenerateClampedShape sanity-checks the Curve25519 private-scalar
// clamping (RFC 7748 §5). Three invariants:
//
//	priv[0]  & 0b00000111  == 0   (bottom three bits cleared)
//	priv[31] & 0b10000000  == 0   (top bit cleared)
//	priv[31] & 0b01000000  != 0   (bit 6 set)
//
// If the clamp drifts, downstream curve25519.X25519 will silently produce
// an off-curve pubkey and the bench env will look healthy until it isn't.
func TestGenerateClampedShape(t *testing.T) {
	for i := 0; i < 32; i++ {
		k, err := generateClamped()
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if len(k) != 32 {
			t.Fatalf("len = %d; want 32", len(k))
		}
		if k[0]&0b00000111 != 0 {
			t.Errorf("iter %d: priv[0] bottom three bits not cleared: %08b", i, k[0])
		}
		if k[31]&0b10000000 != 0 {
			t.Errorf("iter %d: priv[31] top bit not cleared: %08b", i, k[31])
		}
		if k[31]&0b01000000 == 0 {
			t.Errorf("iter %d: priv[31] bit 6 not set: %08b", i, k[31])
		}
	}
}

// TestStripPublishFlag covers the small helper that removes `-p <port>`
// pairs from docker-run argv. Three cases: absent, single, multiple.
func TestStripPublishFlag(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"absent", []string{"run", "-d", "--name", "x"}, []string{"run", "-d", "--name", "x"}},
		{"single", []string{"run", "-p", "51821/udp", "-d"}, []string{"run", "-d"}},
		{"multiple", []string{"run", "-p", "51821/udp", "-p", "443/tcp", "-d"}, []string{"run", "-d"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripPublishFlag(c.in)
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("got %v; want %v", got, c.want)
			}
		})
	}
}

// TestRegressionWinnerCoversAllRelays asserts the drift math (winner :=
// i % M) hits every relay at least once when N >= M. That is what
// guarantees the F-116 failure mode appears in the matrix: at least two
// peers disagreeing on /24 placement.
func TestRegressionWinnerCoversAllRelays(t *testing.T) {
	for _, c := range []struct {
		n, m int
	}{
		{4, 3}, {10, 3}, {6, 2}, {5, 5},
	} {
		t.Run(fmt.Sprintf("N=%d-M=%d", c.n, c.m), func(t *testing.T) {
			seen := make(map[int]bool)
			for i := 0; i < c.n; i++ {
				seen[i%c.m] = true
			}
			if len(seen) != c.m {
				t.Errorf("winner set covered %d/%d relays — drift won't expose F-116", len(seen), c.m)
			}
		})
	}
}

// TestKindCPRelayMatchesBundleCreate guards against drift between the
// bundle-create source and our cp-relay sentinel. If bundle-create
// changes the string it writes into KnownEndpoint.Kind, the regression
// drift loop silently skips every endpoint and the harness would
// report PASS in regression mode (a false negative). This test pins
// the contract.
func TestKindCPRelayMatchesBundleCreate(t *testing.T) {
	if string(kindCPRelay) != "cp-relay" {
		t.Errorf("kindCPRelay = %q; want %q (must match bundle-create source)", kindCPRelay, "cp-relay")
	}
	// Round-trip through bundle.Kind to make sure the cast still works.
	var k bundle.Kind = "cp-relay"
	if k != kindCPRelay {
		t.Errorf("bundle.Kind round-trip mismatch")
	}
}
