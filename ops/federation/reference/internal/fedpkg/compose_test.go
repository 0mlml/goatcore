package fedpkg

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The conformance vectors are normative but not exhaustive. Five derivation
// rules in §7 are not distinguished by any fixture, because the values that
// would separate a correct implementation from a plausible wrong one happen to
// coincide there:
//
//   - the fixed transport preference order — no fixture has an intersection
//     with more than one member, so preferring wireguard passes every vector
//   - transport.mtu as the minimum — parties[0] holds the smaller value in both
//     transport-bearing fixtures, so "always take parties[0]'s" passes
//   - expires as the earlier — parties[1] holds the earlier date everywhere, so
//     "always take parties[1]'s" passes
//   - both listeners resolving to parties[0] — no fixture has two listeners
//   - fabric_fips_image carried verbatim per party — every fixture carries true
//     on both sides, so swapping or combining them passes
//
// These tests are this implementation's own, written against the specification
// text. They are not a substitute for a vector: a second implementation still
// has nothing to check itself against on these five rules, which is a gap in
// the vectors rather than in either implementation.

// testSeeds are the committed vector seeds. They are public, worthless, and
// exist only so that anyone can reproduce the signatures. Never bind one to a
// sovereign_id.
func testSeeds(t *testing.T) map[string]ed25519.PrivateKey {
	t.Helper()
	out := map[string]ed25519.PrivateKey{}
	paths, _ := filepath.Glob(filepath.Join(vectorsDir, "testkeys", "*.seed.hex"))
	if len(paths) == 0 {
		t.Fatal("no test seeds found")
	}
	for _, p := range paths {
		sid := strings.TrimSuffix(filepath.Base(p), ".seed.hex")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		seed, err := decodeHex(strings.TrimSpace(string(b)))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		out[sid] = ed25519.NewKeyFromSeed(seed)
	}
	return out
}

func decodeHex(s string) ([]byte, error) {
	out := make([]byte, len(s)/2)
	for i := range out {
		var v int
		for j := 0; j < 2; j++ {
			c := s[i*2+j]
			switch {
			case c >= '0' && c <= '9':
				v = v<<4 | int(c-'0')
			case c >= 'a' && c <= 'f':
				v = v<<4 | int(c-'a'+10)
			default:
				return nil, errBadHex
			}
		}
		out[i] = byte(v)
	}
	return out, nil
}

var errBadHex = &hexError{}

type hexError struct{}

func (*hexError) Error() string { return "not lowercase hex" }

// pkgOpts is the smallest set of knobs these tests need. Anything not named
// here is filled with a valid, unremarkable value, so each test varies exactly
// the thing it is about.
type pkgOpts struct {
	sid       string
	expires   string
	transport []string // capabilities.transports
	fips      bool
	offer     bool // carry transport_offer
	listener  bool
	mtu       string
	subnet    string
	data      bool // carry data_offer and cut
	release   []string
	hz        string
}

func buildPackage(t *testing.T, o pkgOpts) *Package {
	t.Helper()
	if o.expires == "" {
		o.expires = "2026-12-05"
	}
	if o.transport == nil {
		o.transport = []string{"ipsec-fips"}
	}
	if o.mtu == "" {
		o.mtu = "1400"
	}
	if o.subnet == "" {
		o.subnet = "169.254.10.0/31"
	}
	if o.hz == "" {
		o.hz = "100"
	}
	if o.release == nil {
		o.release = []string{"release/weather/**"}
	}

	m := map[string]any{
		"spec":         SpecVersion,
		"sovereign_id": o.sid,
		"issued":       "2026-05-01",
		"expires":      o.expires,
		"identity": map[string]any{
			"trust_domain":   "spiffe://" + o.sid + ".example",
			"ca_fingerprint": "sha256:" + strings.Repeat(o.sid[len(o.sid)-1:], 64),
		},
		"capabilities": map[string]any{
			"transports":        toAny(o.transport),
			"fabric_fips_image": o.fips,
		},
	}
	if o.offer {
		m["transport_offer"] = map[string]any{
			"endpoint":             "_fed._udp." + o.sid + ".example",
			"listener":             o.listener,
			"measured_mtu":         json.Number(o.mtu),
			"proposed_link_subnet": o.subnet,
			"public_key":           "IyMPpu2aeZCSOARoJYe7VXyT+FDRzUwmsBx+9vTJ+uE=",
		}
	}
	if o.data {
		m["data_offer"] = map[string]any{
			"accept_bytes_per_message": json.Number("1048576"),
			"accept_distinct_keys":     json.Number("5000"),
			"accept_hz":                json.Number(o.hz),
			"release":                  toAny(o.release),
		}
		m["cut"] = map[string]any{"contact": "fed-ops@" + o.sid + ".example"}
	}

	p := &Package{Raw: m}
	signing, err := p.SigningBytes()
	if err != nil {
		t.Fatal(err)
	}
	priv, ok := testSeeds(t)[o.sid]
	if !ok {
		t.Fatalf("no seed for %q", o.sid)
	}
	m["signature"] = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, signing))
	if f := p.structure(); len(f) > 0 {
		t.Fatalf("the fixture builder produced an invalid package: %v", f)
	}
	return p
}

func composeOK(t *testing.T, a, b *Package) map[string]any {
	t.Helper()
	now, err := ParseDate("2026-09-05")
	if err != nil {
		t.Fatal(err)
	}
	ag, f := Compose(a, b, testKeyring(t), now)
	if len(f) > 0 {
		t.Fatalf("refused: %v", Codes(f))
	}
	return ag.Raw
}

func transportOf(t *testing.T, ag map[string]any) map[string]any {
	t.Helper()
	m, ok := ag["transport"].(map[string]any)
	if !ok {
		t.Fatal("the agreement carries no transport member")
	}
	return m
}

// TestTransportPreferenceOrderIsFixed pins §7's fixed order. The order is part
// of the specification, not a local setting: a local preference would break
// determinism, and determinism is the entire mechanism.
func TestTransportPreferenceOrderIsFixed(t *testing.T) {
	both := []string{"ipsec-fips", "wireguard"}
	// Declared in the opposite order by each party, to prove that neither
	// party's ordering of its own list has any effect.
	a := buildPackage(t, pkgOpts{sid: "xaa", transport: both, offer: true, listener: true})
	b := buildPackage(t, pkgOpts{sid: "xab", transport: []string{"wireguard", "ipsec-fips"}, offer: true})

	if got := transportOf(t, composeOK(t, a, b))["kind"]; got != "wireguard" {
		t.Errorf("kind is %q; with both transports in the intersection the fixed order selects wireguard", got)
	}
}

// TestTransportMTUIsTheMinimum pins §7: never an average, never a default, and
// never simply parties[0]'s.
func TestTransportMTUIsTheMinimum(t *testing.T) {
	// The smaller value on parties[1], which is the arrangement no vector has.
	a := buildPackage(t, pkgOpts{sid: "xaa", offer: true, listener: true, mtu: "1420"})
	b := buildPackage(t, pkgOpts{sid: "xab", offer: true, mtu: "1380"})

	if got := transportOf(t, composeOK(t, a, b))["mtu"]; got != json.Number("1380") {
		t.Errorf("mtu is %v, want 1380 — the minimum, which here belongs to parties[1]", got)
	}
}

// TestExpiresIsTheEarlier pins §7: a composed link inherits the shorter term,
// so it has one, and neither party's term is silently extended by the other's.
func TestExpiresIsTheEarlier(t *testing.T) {
	// The earlier date on parties[0], which is the arrangement no vector has.
	a := buildPackage(t, pkgOpts{sid: "xaa", offer: true, listener: true, expires: "2026-10-01"})
	b := buildPackage(t, pkgOpts{sid: "xab", offer: true, expires: "2026-12-31"})

	if got := composeOK(t, a, b)["expires"]; got != "2026-10-01" {
		t.Errorf("expires is %v, want 2026-10-01 — the earlier, which here belongs to parties[0]", got)
	}
}

// TestBothListenersResolveToParties0 pins §7's tie-break. Position decides, so
// both sides agree without negotiating.
func TestBothListenersResolveToParties0(t *testing.T) {
	a := buildPackage(t, pkgOpts{sid: "xaa", offer: true, listener: true})
	b := buildPackage(t, pkgOpts{sid: "xab", offer: true, listener: true})

	if got := transportOf(t, composeOK(t, a, b))["listener"]; got != "xaa" {
		t.Errorf("listener is %v, want xaa — both declared listener: true, and position decides", got)
	}
	// And the argument order must not change it.
	if got := transportOf(t, composeOK(t, b, a))["listener"]; got != "xaa" {
		t.Errorf("swapping the arguments made the listener %v", got)
	}
}

// TestExactlyOneListenerWinsRegardlessOfPosition pins the other half: when
// parties[1] is the only listener, it is named, not parties[0].
func TestExactlyOneListenerWinsRegardlessOfPosition(t *testing.T) {
	a := buildPackage(t, pkgOpts{sid: "xaa", offer: true, listener: false})
	b := buildPackage(t, pkgOpts{sid: "xab", offer: true, listener: true})

	if got := transportOf(t, composeOK(t, a, b))["listener"]; got != "xab" {
		t.Errorf("listener is %v, want xab", got)
	}
}

// TestFabricFIPSImageIsCarriedPerParty pins §7's reason for deleting the
// combined boolean: both false and one-of-each are different situations, and a
// single value cannot tell them apart. What to do about a difference is a local
// decision, so the agreement carries the facts and stays identical on both
// sides while the two parties reach their own conclusions from it.
func TestFabricFIPSImageIsCarriedPerParty(t *testing.T) {
	a := buildPackage(t, pkgOpts{sid: "xaa", offer: true, listener: true, fips: true})
	b := buildPackage(t, pkgOpts{sid: "xab", offer: true, fips: false})

	ag := composeOK(t, a, b)
	parties, ok := ag["parties"].([]any)
	if !ok || len(parties) != 2 {
		t.Fatalf("parties is %v", ag["parties"])
	}
	p0 := parties[0].(map[string]any)
	p1 := parties[1].(map[string]any)
	if p0["fabric_fips_image"] != true {
		t.Errorf("parties[0].fabric_fips_image is %v, want true", p0["fabric_fips_image"])
	}
	if p1["fabric_fips_image"] != false {
		t.Errorf("parties[1].fabric_fips_image is %v, want false", p1["fabric_fips_image"])
	}
}

// TestAgreementCarriesNoDeletedMembers states the revision as a test. Free text
// inside a digest guarantees that two conforming implementations disagree: a
// sentence composed by an implementation is the one thing that will not be
// byte-identical on both sides.
func TestAgreementCarriesNoDeletedMembers(t *testing.T) {
	a := buildPackage(t, pkgOpts{sid: "xaa", offer: true, listener: true, data: true, fips: true})
	b := buildPackage(t, pkgOpts{sid: "xab", offer: true, data: true, fips: false})

	ag := composeOK(t, a, b)
	for _, gone := range []string{"decisions", "fips", "initiator", "reachability", "tier"} {
		if _, present := ag[gone]; present {
			t.Errorf("the agreement still carries %q, which the specification deletes", gone)
		}
	}
	tr := transportOf(t, ag)
	for _, gone := range []string{"initiator", "reachability"} {
		if _, present := tr[gone]; present {
			t.Errorf("transport still carries %q", gone)
		}
	}
	// §8.1: no other member is defined.
	for _, m := range []struct {
		name string
		obj  map[string]any
		want []string
	}{
		{"the agreement", ag, []string{"expires", "flows", "parties", "spec", "transport"}},
		{"transport", tr, []string{"kind", "link_subnet", "listener", "mtu"}},
	} {
		allowed := map[string]bool{}
		for _, k := range m.want {
			allowed[k] = true
		}
		for k := range m.obj {
			if !allowed[k] {
				t.Errorf("%s carries undefined member %q", m.name, k)
			}
		}
	}
}

// TestOneDirectionalFlow pins the half of §7's flow rule the vectors cannot
// reach: a party that receives and publishes nothing. Only the direction into
// it is emitted, and flows still holds exactly one entry.
func TestOneDirectionalFlow(t *testing.T) {
	a := buildPackage(t, pkgOpts{sid: "xaa", offer: true, listener: true, data: true})
	b := buildPackage(t, pkgOpts{sid: "xab", offer: true, data: true})
	// xab receives and publishes nothing.
	delete(b.Raw["data_offer"].(map[string]any), "release")
	resign(t, b)

	ag := composeOK(t, a, b)
	fl, _ := ag["flows"].([]any)
	if len(fl) != 1 {
		t.Fatalf("flows holds %d entries, want 1", len(fl))
	}
	only := fl[0].(map[string]any)
	if only["from"] != "xaa" || only["to"] != "xab" {
		t.Errorf("the emitted flow is %v→%v, want xaa→xab", only["from"], only["to"])
	}
	if only["publish_under"] != "from/xaa/**" {
		t.Errorf("publish_under is %v", only["publish_under"])
	}
}

// TestFlowsIsEmptyNotAbsent pins §8.1: flows is always present and may be an
// empty array. A path that carries nothing is a valid agreement, not an error.
func TestFlowsIsEmptyNotAbsent(t *testing.T) {
	a := buildPackage(t, pkgOpts{sid: "xaa", offer: true, listener: true})
	b := buildPackage(t, pkgOpts{sid: "xab", offer: true})

	b2, err := Canonical(composeOK(t, a, b))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b2), `"flows":[]`) {
		t.Errorf("flows is not an empty array: %s", b2)
	}
}

// TestReleaseSetIsSortedAndDeduplicated pins §4's rule for arrays that
// represent sets, which is applied before signing and again on the way into a
// flow.
func TestReleaseSetIsSortedAndDeduplicated(t *testing.T) {
	a := buildPackage(t, pkgOpts{sid: "xaa", data: true,
		release: []string{"release/weather/**", "release/imagery/**", "release/weather/**"}})
	b := buildPackage(t, pkgOpts{sid: "xab", data: true})

	ag := composeOK(t, a, b)
	fl := ag["flows"].([]any)[0].(map[string]any)
	got := fl["release"].([]any)
	want := []string{"release/imagery/**", "release/weather/**"}
	if len(got) != len(want) {
		t.Fatalf("release is %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("release[%d] is %v, want %v", i, got[i], want[i])
		}
	}
}

// TestIncompleteBlockIsRefusedNeverDefaulted pins §3. A silent default is how a
// link ends up with no rate limit.
func TestIncompleteBlockIsRefusedNeverDefaulted(t *testing.T) {
	now, _ := ParseDate("2026-09-05")
	cases := []struct {
		name string
		bust func(m map[string]any)
		want string
	}{
		{"a partial receive half — one ceiling missing", func(m map[string]any) {
			delete(m["data_offer"].(map[string]any), "accept_hz")
		}, "malformed.xab.data_offer.receive_half"},
		{"a partial receive half — two ceilings missing", func(m map[string]any) {
			d := m["data_offer"].(map[string]any)
			delete(d, "accept_distinct_keys")
			delete(d, "accept_hz")
		}, "malformed.xab.data_offer.receive_half"},
		{"data_offer carrying neither half", func(m map[string]any) {
			m["data_offer"] = map[string]any{}
		}, "malformed.xab.data_offer.empty"},
		{"data_offer with no cut alongside it", func(m map[string]any) {
			delete(m, "cut")
		}, "malformed.xab.cut"},
		{"an empty release set", func(m map[string]any) {
			m["data_offer"].(map[string]any)["release"] = []any{}
		}, "malformed.xab.data_offer.release"},
		{"transport_offer without a measured mtu", func(m map[string]any) {
			delete(m["transport_offer"].(map[string]any), "measured_mtu")
		}, "malformed.xab.transport_offer.measured_mtu"},
		{"transport_offer without a listener declaration", func(m map[string]any) {
			delete(m["transport_offer"].(map[string]any), "listener")
		}, "malformed.xab.transport_offer.listener"},
		{"a null at depth", func(m map[string]any) {
			m["transport_offer"].(map[string]any)["measured_mtu"] = nil
		}, "malformed.xab.null.transport_offer.measured_mtu"},
		{"a null element inside a set-array", func(m map[string]any) {
			m["data_offer"].(map[string]any)["release"] = []any{"release/x/**", nil}
		}, "malformed.xab.null.data_offer.release[1]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := buildPackage(t, pkgOpts{sid: "xaa", offer: true, listener: true, data: true})
			b := buildPackage(t, pkgOpts{sid: "xab", offer: true, data: true})
			c.bust(b.Raw)
			resign(t, b)

			ag, f := Compose(a, b, testKeyring(t), now)
			if ag != nil {
				t.Fatal("composed an incomplete block instead of refusing it")
			}
			if len(f) == 0 || Codes(f)[0] != c.want {
				t.Fatalf("refused under %v, want %s", Codes(f), c.want)
			}
		})
	}
}

// TestPeeringNameAndPrefixAreDerivedNotRead pins §3.1 and §3.2. A package
// declaring either is not consulted: a value that can only be derived cannot be
// disagreed about, so a declaration adds a way to be refused and no way to be
// right.
// TestIntegerBoundsAreEnforcedOnAPackage pins §4's range. A value outside it is
// refused in a package and in an agreement alike.
func TestIntegerBoundsAreEnforcedOnAPackage(t *testing.T) {
	p := buildPackage(t, pkgOpts{sid: "xab", offer: true, data: true})
	p.Raw["data_offer"].(map[string]any)["accept_hz"] = json.Number("9007199254740992")

	codes := Codes(p.structure())
	if len(codes) != 1 || codes[0] != "data_offer.accept_hz" {
		t.Fatalf("structure reported %v, want one data_offer.accept_hz", codes)
	}
}

// TestAnOutOfRangeIntegerIsMalformedNotUnverifiable pins WHERE the refusal
// happens. Such a package has no canonical form, so its signing input cannot be
// produced and a signature check would report it as an unverifiable signature —
// the right verdict from the wrong place, naming a rule the package does not
// break. The malformed gate runs before §6 step 0 precisely so that it does not.
func TestAnOutOfRangeIntegerIsMalformedNotUnverifiable(t *testing.T) {
	now, _ := ParseDate("2026-09-05")
	a := buildPackage(t, pkgOpts{sid: "xaa", offer: true, listener: true, data: true})
	b := buildPackage(t, pkgOpts{sid: "xab", offer: true, data: true})
	b.Raw["data_offer"].(map[string]any)["accept_hz"] = json.Number("9007199254740992")

	if _, err := b.SigningBytes(); err == nil {
		t.Fatal("an out-of-range integer produced a signing input")
	}
	ag, f := Compose(a, b, testKeyring(t), now)
	if ag != nil {
		t.Fatal("composed a package whose canonical form cannot be produced")
	}
	if len(f) != 1 || f[0].Code != "malformed.xab.data_offer.accept_hz" {
		t.Fatalf("refused under %v, want one malformed.xab.data_offer.accept_hz", Codes(f))
	}
	if !strings.Contains(f[0].Msg, "accept_hz") || !strings.Contains(f[0].Msg, "2^53") {
		t.Errorf("the refusal does not name the member and the range: %s", f[0].Msg)
	}
}

func TestPeeringNameAndPrefixAreDerivedNotRead(t *testing.T) {
	a := buildPackage(t, pkgOpts{sid: "xaa", data: true})
	b := buildPackage(t, pkgOpts{sid: "xab", data: true})
	// A package from an implementation still carrying the old declarations,
	// both wrong. Neither may reach the agreement.
	a.Raw["identity"].(map[string]any)["peering_cn"] = "fed.wrong.name.99"
	a.Raw["data_offer"].(map[string]any)["publish_under"] = "from/wrong/**"
	resign(t, a)

	ag := composeOK(t, a, b)
	p0 := ag["parties"].([]any)[0].(map[string]any)
	if p0["peering_cn"] != "fed.xaa.xab.01" {
		t.Errorf("peering_cn is %v, want the derived fed.xaa.xab.01", p0["peering_cn"])
	}
	fl := ag["flows"].([]any)[0].(map[string]any)
	if fl["publish_under"] != "from/xaa/**" {
		t.Errorf("publish_under is %v, want the derived from/xaa/**", fl["publish_under"])
	}
}

func resign(t *testing.T, p *Package) {
	t.Helper()
	delete(p.Raw, "signature")
	signing, err := p.SigningBytes()
	if err != nil {
		t.Fatal(err)
	}
	priv, ok := testSeeds(t)[p.SovereignID()]
	if !ok {
		t.Fatalf("no seed for %q", p.SovereignID())
	}
	p.Raw["signature"] = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, signing))
}

// TestLintNamesTheRuleNotATier states what §3 replaced. There is no tier and no
// ladder, so a package cannot claim a level and then contradict its own claim.
func TestLintNamesTheRuleNotATier(t *testing.T) {
	now, _ := ParseDate("2026-09-05")
	p := buildPackage(t, pkgOpts{sid: "xaa", offer: true, listener: true, data: true})
	if f := p.Lint(now); len(f) > 0 {
		t.Fatalf("a conforming package did not lint clean: %v", f)
	}
	// A package carrying the deleted member is not refused for carrying it:
	// unknown members survive into the canonical form, and the signature covers
	// them. It is simply not consulted.
	p.Raw["tier"] = "exchanging"
	resign(t, p)
	if f := p.Lint(now); len(f) > 0 {
		t.Errorf("a package carrying an unknown member was refused: %v", f)
	}

	expired := buildPackage(t, pkgOpts{sid: "xab", expires: "2026-08-01"})
	codes := Codes(expired.Lint(now))
	if len(codes) != 1 || codes[0] != "expires.past" {
		t.Errorf("lint of an expired package reported %v, want one expires.past", codes)
	}
}

func TestComposeRefusesAnExpiredPackageAtTheBoundary(t *testing.T) {
	now, _ := ParseDate("2026-09-05")
	a := buildPackage(t, pkgOpts{sid: "xaa", expires: "2026-09-05"})
	b := buildPackage(t, pkgOpts{sid: "xab", expires: "2026-09-05"})
	if _, f := Compose(a, b, testKeyring(t), now); len(f) > 0 {
		t.Errorf("a package expiring on the evaluation date is not before it, so it must not refuse: %v", Codes(f))
	}

	c := buildPackage(t, pkgOpts{sid: "xab", expires: "2026-09-04"})
	if ag, f := Compose(a, c, testKeyring(t), now); ag != nil || len(f) == 0 || Codes(f)[0] != RefuseExpired {
		t.Errorf("a package expiring the day before must refuse, got %v", Codes(f))
	}
}

// TestComposeRefusesAVersionItDoesNotImplement covers the half of §6 step 1 no
// fixture carries: two packages that agree with each other and not with this
// implementation. Composing them as this version would produce an agreement
// neither party's tooling wrote.
func TestComposeRefusesAVersionItDoesNotImplement(t *testing.T) {
	now, _ := ParseDate("2026-09-05")
	a := buildPackage(t, pkgOpts{sid: "xaa", data: true})
	b := buildPackage(t, pkgOpts{sid: "xab", data: true})
	for _, p := range []*Package{a, b} {
		p.Raw["spec"] = "federation-package/v2"
		resign(t, p)
	}
	ag, f := Compose(a, b, testKeyring(t), now)
	if ag != nil {
		t.Fatal("composed two v2 packages as v1")
	}
	if len(f) != 1 || f[0].Code != RefuseVersionMismatch {
		t.Fatalf("refused under %v, want one %s", Codes(f), RefuseVersionMismatch)
	}
}

// TestComposeRefusesAMissingSpecMember is the same rule at its other edge: two
// packages carrying no spec member at all agree with each other on nothing.
func TestComposeRefusesAMissingSpecMember(t *testing.T) {
	now, _ := ParseDate("2026-09-05")
	a := buildPackage(t, pkgOpts{sid: "xaa", data: true})
	b := buildPackage(t, pkgOpts{sid: "xab", data: true})
	for _, p := range []*Package{a, b} {
		delete(p.Raw, "spec")
		resign(t, p)
	}
	if ag, f := Compose(a, b, testKeyring(t), now); ag != nil || len(f) != 1 || f[0].Code != RefuseVersionMismatch {
		t.Fatalf("composed packages with no spec member, or refused under %v", Codes(f))
	}
}

// TestPreferenceFixtureStillSeparatesTheRules guards the property that makes
// compose-transport-outside-preference-ignored worth having.
//
// That fixture exists because "first entry of the sorted intersection" cannot be
// separated from §7's order using only the two preference-order transports:
// whichever way the order runs, some fixture has the two rules coincide. Under
// the pre-2026-09-06 order (ipsec-fips first) they coincided everywhere, since
// ipsec-fips also sorts first. Reversing the order separated them in
// compose-preference-and-tiebreak but made "first entry of the counterparty's
// declared list" the rule that now agrees there by accident. This fixture
// separates the sorted rule on its own terms rather than by an accident a
// reordering can flip: both parties declare quic, which is shared, sorts first,
// and is not in the preference order at all.
//
// If someone later edits that fixture to a shared out-of-preference name that
// sorts AFTER wireguard, the two rules would agree again and the fixture would
// silently stop testing anything while still passing. This asserts the
// separation itself, on the committed bytes.
func TestPreferenceFixtureStillSeparatesTheRules(t *testing.T) {
	dir := filepath.Join(vectorsDir, "compose-transport-outside-preference-ignored")

	raw, err := os.ReadFile(filepath.Join(dir, "package-xaa.canonical.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	declared := p.Transports()
	if len(declared) < 2 {
		t.Fatalf("the fixture declares %v; it needs a shared entry outside the preference order to separate the rules", declared)
	}

	// What §7 selects: the first preference-order entry present.
	var byPreference string
	for _, pref := range TransportPreference {
		for _, d := range declared {
			if d == pref {
				byPreference = pref
			}
		}
		if byPreference != "" {
			break
		}
	}
	// What the wrong rule selects: the first of the sorted intersection.
	bySort := declared[0]

	if byPreference == "" {
		t.Fatal("the fixture shares no preference-order transport, so §6 step 4(a) would refuse it")
	}
	if byPreference == bySort {
		t.Fatalf("both rules select %q, so this fixture no longer separates them: it needs a shared entry outside the preference order that sorts first", byPreference)
	}

	agreement, err := os.ReadFile(filepath.Join(dir, "agreement.canonical.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(agreement, []byte(`"kind":"`+byPreference+`"`)) {
		t.Errorf("the committed agreement does not carry kind %q", byPreference)
	}
	if bytes.Contains(agreement, []byte(`"kind":"`+bySort+`"`)) {
		t.Errorf("the committed agreement carries %q, which is the sorted-intersection answer and is not a value §8.1 permits", bySort)
	}
}
