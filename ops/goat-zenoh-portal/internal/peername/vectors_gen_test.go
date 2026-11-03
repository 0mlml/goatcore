// Code generated from ops/identity/peer-name-grammar.yaml by ops/identity/generate-peer-name.py. DO NOT EDIT.
//
// Peer-name grammar v1.1.0 — peer-<handle>[-<org_short_tag>]-<site>-<date>[-<hostname>]
//
// Under setup-key enrollment NetBird leaves peer.user_id empty, so the peer NAME is the
// only link from a mesh peer to the principal that owns it
// (docs/design/peer-identity-resolution.md, Option B). Edit the SoT and re-run the
// generator; ops/ci/check-peer-name-grammar-sync.sh fails the build on drift.
//
// A peer name is EVIDENCE about ownership, never a record of it — the recorded linkage
// is slots.identity_id, stamped at onboard. A consumer that presents ownership must say
// which of the two it used.

// The SoT's cross-language conformance vectors. The IDENTICAL cases are
// generated into the Rust implementation's test module, so Go and Rust are
// proven to agree on behaviour rather than assumed to.
package peername

import "testing"

func TestIsPeerNameVectors(t *testing.T) {
	cases := []struct {
		name string
		want bool
		why  string
	}{
		{"peer-maintainer-zagros-2026-06-10-mac", true, "the canonical form"},
		{"peer-", true, "prefix alone conforms; the remainder is simply empty"},
		{"thor-a", false, "an unmanaged peer carries no principal key"},
		{"Peer-maintainer-zagros-2026-06-10", false, "the prefix is case-sensitive"},
		{"", false, "empty is not a peer name"},
	}
	for _, c := range cases {
		if got := IsPeerName(c.name); got != c.want {
			t.Errorf("IsPeerName(%q) = %v, want %v — %s", c.name, got, c.want, c.why)
		}
	}
}

func TestRestVectors(t *testing.T) {
	cases := []struct {
		name   string
		want   string
		wantOK bool
		why    string
	}{
		{"peer-maintainer-zagros-2026-06-10-mac", "maintainer-zagros-2026-06-10-mac", true, "the keyed remainder"},
		{"peer-", "", true, "an empty remainder is present-but-empty, not absent"},
		{"thor-a", "", false, "absent for a non-conforming name"},
	}
	for _, c := range cases {
		got, ok := Rest(c.name)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("Rest(%q) = (%q, %v), want (%q, %v) — %s", c.name, got, ok, c.want, c.wantOK, c.why)
		}
	}
}

func TestMatchLongestKeyVectors(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want string
		why  string
	}{
		{"peer-aardwolf-nova-2026-05-21-laptop", []string{"aardwolf", "user52x"}, "aardwolf", "the ordinary org-prefix join"},
		{"peer-foobar-nova-2026-05-21-host", []string{"foo", "foobar"}, "foobar", "longest wins — `foo` must not shadow `foobar`"},
		{"peer-foobar-nova-2026-05-21-host", []string{"foo"}, "", "the separator anchor: `foo` does not match `foobar` at all"},
		{"peer-maintainer-smith-zagros-2026-06-10-mac", []string{"maintainer", "maintainer-smith"}, "maintainer-smith", "the org-mate mis-attribution class (hit live 2026-07-14) — the separator also occurs INSIDE a handle, so `maintainer-` does prefix `maintainer-smith-…`; longest-wins over the full candidate set is what keeps `maintainer` from claiming their peer"},
		{"peer-maintainer-smith-zagros-2026-06-10-mac", []string{"maintainer"}, "maintainer", "the stated limit of this matcher, pinned so nobody mistakes it for a cut selector: with the true owner ABSENT from the candidate set, a shorter handle does match. Anything that acts on a principal (a revoke cut) must use `belongs_to`, which anchors on handle AND site instead."},
		{"peer-maintainer-zagros-2026-06-10-mac", []string{"", "maintainer"}, "maintainer", "an empty key never matches, and never wins by being 'longest'"},
		{"thor-a", []string{"thor"}, "", "a non-conforming name is keyed by nothing, whatever it contains"},
		{"peer-maintainer-zagros-2026-06-10-mac", []string{}, "", "no candidates, no match — never a fabricated one"},
	}
	for _, c := range cases {
		if got := MatchLongestKey(c.name, c.keys); got != c.want {
			t.Errorf("MatchLongestKey(%q, %v) = %q, want %q — %s", c.name, c.keys, got, c.want, c.why)
		}
	}
}

func TestMintVectors(t *testing.T) {
	cases := []struct {
		handle, tag, site, date string
		want                    string
		why                     string
	}{
		{"maintainer", "", "nova", "2026-05-17", "peer-maintainer-nova-2026-05-17", "unaffiliated principal — the org segment is absent, not empty"},
		{"maintainer", "acme-corp", "nova", "2026-05-17", "peer-maintainer-acme-corp-nova-2026-05-17", "org-affiliated principal"},
		{"maintainer", "  acme  ", "zagros", "2026-06-10", "peer-maintainer-acme-zagros-2026-06-10", "a manifest's stray whitespace never reaches the wire name"},
		{"user371x", "user371x", "zagros", "2026-06-16", "peer-user371x-zagros-2026-06-16", "v1.1.0 — a tag EQUAL to the handle is omitted. This exact input minted peer-user371x-user371x-zagros-2026-06-16 on ZAGROS; the tag is forced to equal the handle on per-uuid goatnets and carries nothing."},
		{"maintainer", "  maintainer  ", "zagros", "2026-06-10", "peer-maintainer-zagros-2026-06-10", "the equality is tested AFTER trimming, or whitespace defeats the rule"},
	}
	for _, c := range cases {
		if got := Mint(c.handle, c.tag, c.site, c.date); got != c.want {
			t.Errorf("Mint(%q, %q, %q, %q) = %q, want %q — %s", c.handle, c.tag, c.site, c.date, got, c.want, c.why)
		}
	}
}

func TestBelongsToVectors(t *testing.T) {
	cases := []struct {
		name, handle, tag, site string
		want                    bool
		why                     string
	}{
		{"peer-maintainer-zagros-2026-06-10-mac", "maintainer", "", "zagros", true, "untagged form"},
		{"peer-maintainer-acme-zagros-2026-06-10-mac", "maintainer", "acme", "zagros", true, "tagged form"},
		{"peer-maintainer-acme-zagros-2026-06-10-mac", "maintainer", "", "zagros", false, "a manifest with no tag must not claim the tagged peer"},
		{"peer-user371x-user371x-zagros-2026-06-16-JL-2.local", "user371x", "user371x", "zagros", true, "MIGRATION — a peer minted BEFORE v1.1.0 carries the doubled segment and must still be claimed. This is the revoke selector: a miss here leaves a credential live that was supposed to be cut."},
		{"peer-user371x-zagros-2026-06-16-JL-2.local", "user371x", "user371x", "zagros", true, "the same principal minted AFTER v1.1.0 — both forms, one selector"},
		{"peer-maintainer-smith-zagros-2026-06-10-mac", "maintainer", "maintainer", "zagros", false, "the dual accept must not widen: `maintainer`+tag `maintainer` never claims the DIFFERENT principal `maintainer-smith`, whose name merely starts the same way"},
		{"peer-maintainer-smith-zagros-2026-06-10-mac", "maintainer", "", "zagros", false, "the org-mate mis-attribution class, on the revoke selector"},
		{"peer-maintainer-zagros-2026-06-10-mac", "maintainer", "", "nova", false, "site-scoped: a revoke on one goatnet never cuts a peer on another"},
		{"peer-maintainer-acme-zagros-2026-06-10-mac", "maintainer", "beta", "zagros", false, "the wrong tag is not a match"},
	}
	for _, c := range cases {
		if got := BelongsTo(c.name, c.handle, c.tag, c.site); got != c.want {
			t.Errorf("BelongsTo(%q, %q, %q, %q) = %v, want %v — %s", c.name, c.handle, c.tag, c.site, got, c.want, c.why)
		}
	}
}
