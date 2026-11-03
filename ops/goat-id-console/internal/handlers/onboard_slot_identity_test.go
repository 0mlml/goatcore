package handlers

import "testing"

// The anchor is the portal's slot id and the key PUT /api/slots/{anchor}/identity
// writes against. Extracting the wrong substring would stamp the linkage onto
// SOMEONE ELSE'S slot, so this is pinned tightly.
func TestSlotAnchorFromBundlePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/api/org-bundle/4f250e30ac723662f668cbe941f4b0b6/832b6815336e872ea3d23c64ae18ca32.tar.gz",
			"4f250e30ac723662f668cbe941f4b0b6"},
		{"https://portal.zagros.netbird.zagros.example/api/org-bundle/abc123/tok.tar.gz", "abc123"},
		// No fabric leg — nothing to bind, and the caller must no-op rather than
		// invent an anchor.
		{"", ""},
		{"/api/vendor-bundle/abc123/tok.tar.gz", ""},
		{"/some/other/path", ""},
	}
	for _, c := range cases {
		if got := slotAnchorFromBundlePath(c.in); got != c.want {
			t.Errorf("slotAnchorFromBundlePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The stated anchor wins over the derived one, and a deferred onboard — a real
// slot with no bundle — still gets its linkage stamped. Before the runner
// carried `slot_anchor`, the bundle-path derivation returned "" for exactly that
// case and the stamp was skipped in silence, leaving the person owning a slot the
// registry could not attribute to them and a collection page that would answer
// "no slot recorded for you" forever.
func TestResolveSlotAnchor_PrefersTheStatedAnchor(t *testing.T) {
	const stated = "8aa78aa40cc102ecaa5bd5c1f5d02e15"
	const derivedAnchor = "4f250e30ac723662f668cbe941f4b0b6"
	bundle := "/api/org-bundle/" + derivedAnchor + "/tok.tar.gz"

	cases := []struct {
		name         string
		statedAnchor string
		bundlePath   string
		want         string
		wantDerived  bool
	}{
		{"stated anchor wins over the derivable one", stated, bundle, stated, false},
		{"deferred onboard: a real slot with no bundle at all", stated, "", stated, false},
		{"legacy writeback: falls back to the bundle path", "", bundle, derivedAnchor, true},
		{"genuinely fabric-less onboard: nothing to stamp", "", "", "", false},
		{"whitespace-only anchor is not an anchor", "   ", bundle, derivedAnchor, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, gotDerived := resolveSlotAnchor(c.statedAnchor, c.bundlePath)
			if got != c.want || gotDerived != c.wantDerived {
				t.Errorf("resolveSlotAnchor(%q, %q) = (%q, %v), want (%q, %v)",
					c.statedAnchor, c.bundlePath, got, gotDerived, c.want, c.wantDerived)
			}
		})
	}
}
