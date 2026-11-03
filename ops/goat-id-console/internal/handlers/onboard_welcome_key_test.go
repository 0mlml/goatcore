package handlers

import (
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// needsSetupKey decides whether a welcome package is incomplete without a mesh
// enrollment key — the predicate behind holding the send. A mesh-enrolled person
// who gets a keyless package is stranded at step 1 with no recovery path, so a
// package must not go out before the mint produces the key.
func TestNeedsSetupKey(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		want     bool
	}{
		{"netbird enabled → key required",
			`{"netbird":{"enabled":true,"auto_groups":["OBSERVER"]}}`, true},
		{"netbird disabled → no key to wait for",
			`{"netbird":{"enabled":false},"ory_site":{"enabled":true}}`, false},
		// No netbird object cannot prove the person needs no key: historical
		// index rows ({}) delivered keyless packages this way (#2027).
		{"netbird absent → key required",
			`{"zenoh":{"enabled":true}}`, true},
		{"historical index row → key required", `{}`, true},
		{"null manifest → key required", `null`, true},
		{"malformed manifest → key required", `{not json`, true},
		{"empty manifest → key required", ``, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &onboardstore.Proposal{ManifestJSON: c.manifest}
			if got := needsSetupKey(p); got != c.want {
				t.Errorf("needsSetupKey(%s) = %v, want %v", c.manifest, got, c.want)
			}
		})
	}
}
