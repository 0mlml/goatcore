package fedpkg

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// The seven ways to refuse (§6). There are exactly seven, and the conformance
// vectors carry one fixture for each. A refusal names its rule: a fixture that
// refuses for the wrong reason is a passing test hiding a broken implementation.
const (
	RefuseSignature          = "compose.signature"            // step 0
	RefuseVersionMismatch    = "compose.version_mismatch"     // step 1
	RefuseSameSovereignID    = "compose.same_sovereign_id"    // step 2
	RefuseSameTrustDomain    = "compose.same_trust_domain"    // step 2
	RefuseExpired            = "compose.expired"              // step 3
	RefuseNoTransportOverlap = "compose.no_transport_overlap" // step 4(a)
	RefuseNeitherListener    = "compose.neither_listener"     // step 4(b)
)

// Keyring binds a sovereign_id to the Ed25519 public key that party signs with.
//
// It is an input to composition (§5.1). The binding is made once per party over
// a channel authenticated independently of the relationship being established —
// a meeting, a call to a voice you recognise, a signed instrument through an
// established diplomatic channel. A directory may cache it and may carry a
// rotation; it is never the source, because a key learned only from a directory
// is a key learned from whoever runs the directory.
type Keyring map[string]ed25519.PublicKey

// Agreement is the composed result. It has no perspective (§2): the two halves
// are sorted by sovereign_id into positions, and every derived value is
// expressed by position. Both parties therefore compute byte-identical output,
// which is what makes the digest comparison possible at all.
type Agreement struct {
	Raw map[string]any
}

// Canonical is the agreement's canonical form (§4).
func (a *Agreement) Canonical() ([]byte, error) { return Canonical(a.Raw) }

// Digest is the value the two parties compare (§9).
func (a *Agreement) Digest() (string, error) { return Digest(a.Raw) }

// Compose merges two packages per §6. Refusal is an outcome of the mechanism
// working, not an error condition, so it is returned as findings. A refusal
// produces no agreement: the first result is nil, never a partial one.
func Compose(a, b *Package, keys Keyring, now time.Time) (*Agreement, []Finding) {
	var f []Finding
	add := func(code, msg string, v ...any) {
		f = append(f, Finding{code, fmt.Sprintf(msg, v...)})
	}

	// Before any of §6's steps: a malformed package is refused, and this is not
	// one of the seven ways composition refuses (§6). A malformed package is a
	// property of ONE package — a missing member, a null at any depth, a
	// non-integer number, an empty set-array, a partial data_offer half, a
	// signature of the wrong shape — rather than a verdict on a pair.
	//
	// It has to come before step 0, not after, because some of these packages
	// cannot be canonicalised at all: "measured_mtu": 1380.0 has no canonical
	// form, so the signing input cannot be produced and the signature check
	// would report it as an unverifiable signature. That is the right verdict
	// from the wrong place, and it names a rule the package does not break.
	//
	// The value of spec is deliberately NOT checked here. It is a literal pin
	// at step 1 (§6), so that a mismatched pair and a both-on-another-version
	// pair produce the same named composition refusal.
	for _, p := range []*Package{a, b} {
		for _, x := range p.structure() {
			add("malformed."+p.SovereignID()+"."+x.Code, "%s", x.Msg)
		}
	}
	if len(f) > 0 {
		return nil, f
	}

	// Step 0. Nothing below means anything on an unauthenticated package, so
	// this is first and there is no partial result before it.
	for _, p := range []*Package{a, b} {
		sid := p.SovereignID()
		pub, ok := keys[sid]
		if !ok {
			add(RefuseSignature, "no key is bound to %q; the binding is an input to composition, established over an independently authenticated channel, and is never fetched from the package or from a directory (§5.1)", sid)
			continue
		}
		if err := p.VerifySignature(pub); err != nil {
			add(RefuseSignature, "%s: %v", sid, err)
		}
	}
	if len(f) > 0 {
		return nil, f
	}

	// Step 1. Never compose across versions: two implementations producing two
	// digests is a false alarm, and a false alarm here destroys confidence in
	// the mechanism permanently. It runs before anything is read out of either
	// package, so a package of another version is refused for being another
	// version rather than for failing this version's structural rules.
	sa, sb := a.Spec(), b.Spec()
	if sa != sb {
		add(RefuseVersionMismatch, "refusing to compose %q with %q", sa, sb)
		return nil, f
	}
	// Two packages may agree on a version this implementation does not
	// implement. Composing them as if they were this version would produce an
	// agreement neither party's tooling wrote, so the same rule refuses.
	if sa != SpecVersion {
		add(RefuseVersionMismatch, "both packages carry %q; this implements %q, and the spec version is the migration path precisely so that a party cannot be led into accepting another version's rules (§5)", sa, SpecVersion)
		return nil, f
	}

	// Step 2. Two parties must be distinguishable at every layer.
	if a.SovereignID() == b.SovereignID() {
		add(RefuseSameSovereignID, "both packages name the party %q; the two ends would derive the same peering name and publish under the same prefix, and the access rules that are the entire enforcement mechanism could not tell them apart", a.SovereignID())
	}
	if a.TrustDomain() == b.TrustDomain() {
		add(RefuseSameTrustDomain, "both packages declare the trust domain %q; the names would compose cleanly and one authority would issue the workload identities behind both", a.TrustDomain())
	}
	if len(f) > 0 {
		return nil, f
	}

	// Step 3. The earlier-of-two rule in §7 gives a live link its term; it does
	// not resurrect a dead package.
	for _, p := range []*Package{a, b} {
		t, err := ParseDate(p.Expires())
		if err != nil {
			add(RefuseExpired, "%s: %v", p.SovereignID(), err)
			continue
		}
		if t.Before(now) {
			add(RefuseExpired, "%s's package expired on %s, and now is %s", p.SovereignID(), p.Expires(), now.Format(DateLayout))
		}
	}
	if len(f) > 0 {
		return nil, f
	}

	// Position, not perspective: sort by sovereign_id, ascending, byte-wise.
	p0, p1 := a, b
	if LessUTF16(b.SovereignID(), a.SovereignID()) {
		p0, p1 = b, a
	}

	// Step 4, and the transport derivation of §7. Both apply only when both
	// packages carry transport_offer: when they do not, no link was asked for
	// and there is nothing to fail.
	bothOffer := p0.HasTransportOffer() && p1.HasTransportOffer()
	var transport map[string]any
	if bothOffer {
		t, refusals := negotiateTransport(p0, p1)
		if len(refusals) > 0 {
			return nil, refusals
		}
		transport = t
	}

	// Steps 5 to 7.
	ag := map[string]any{
		"spec":    AgreementSpec,
		"expires": earlier(p0.Expires(), p1.Expires()),
		"parties": []any{party(p0, p1), party(p1, p0)},
		"flows":   flows(p0, p1),
	}
	if transport != nil {
		ag["transport"] = transport
	}

	// Nothing may be emitted without a value, and nothing in §8.1 may be null
	// (§4). Every member above is taken through an accessor that reports
	// absence, and every package was structurally validated before any of them
	// ran — so a hole here is a defect in this function. It is caught rather
	// than serialised, because an agreement carrying a null is output no other
	// implementation will reproduce.
	var holes []string
	findNulls(ag, "", &holes)
	if len(holes) > 0 {
		add("compose.internal", "the composed agreement carries a null at %s, which is a defect in this implementation: §8.1 defines no nullable member", strings.Join(holes, ", "))
		return nil, f
	}
	if _, err := Canonical(ag); err != nil {
		add("compose.internal", "the composed agreement is not serialisable, which is a defect in this implementation: %v", err)
		return nil, f
	}
	return &Agreement{Raw: ag}, nil
}

// negotiateTransport derives the transport member, or refuses under §6 step 4.
// It is reached only when both packages carry transport_offer.
func negotiateTransport(p0, p1 *Package) (map[string]any, []Finding) {
	t0, t1 := p0.Transports(), p1.Transports()

	// (a) Intersect, then the fixed preference order of §7. The order is part
	// of the specification, not a local setting: a local preference would break
	// determinism, and determinism is the whole mechanism.
	inBoth := map[string]bool{}
	for _, x := range t0 {
		for _, y := range t1 {
			if x == y {
				inBoth[x] = true
			}
		}
	}
	kind := ""
	for _, pref := range TransportPreference {
		if inBoth[pref] {
			kind = pref
			break
		}
	}
	if kind == "" {
		return nil, []Finding{{RefuseNoTransportOverlap, fmt.Sprintf(
			"%s offers %v and %s offers %v; both asked for a link and no transport is supported by both",
			p0.SovereignID(), t0, p1.SovereignID(), t1)}}
	}

	// (b) Exactly one true, that party. Both true, parties[0] — position
	// decides, so neither side has to negotiate. Neither true, refuse. The
	// member holds the party that listens and is named for what it holds.
	l0, ok0 := p0.Listener()
	l1, ok1 := p1.Listener()
	if !ok0 || !ok1 {
		// Unreachable on a structurally valid package; kept so that a future
		// caller bypassing the structure check cannot reach the emit path.
		return nil, []Finding{{RefuseNeitherListener, "transport_offer.listener is not a boolean on both sides"}}
	}
	var listener string
	switch {
	case l0:
		listener = p0.SovereignID()
	case l1:
		listener = p1.SovereignID()
	default:
		return nil, []Finding{{RefuseNeitherListener, fmt.Sprintf(
			"neither %s nor %s will accept a connection; each has said it will dial and there is no link to build",
			p0.SovereignID(), p1.SovereignID())}}
	}

	m0, _ := p0.MeasuredMTU()
	m1, _ := p1.MeasuredMTU()
	subnet, _ := p0.ProposedLinkSubnet()

	return map[string]any{
		"kind": kind,
		// parties[0]'s proposal. Position decides, so both sides agree without
		// negotiating.
		"link_subnet": subnet,
		"listener":    listener,
		// The smaller of the two measured values. Never an average, never a
		// default.
		"mtu": minNumber(m0, m1),
	}, nil
}

// party is one entry of parties[n]. peering_cn is derived from the two
// sovereign_id values (§3.1) and appears only here: a value that can only be
// derived cannot be disagreed about, and a name that is never written cannot be
// mistyped into a refusal that is silent at both ends.
func party(self, other *Package) map[string]any {
	fips, _ := self.FabricFIPSImage()
	return map[string]any{
		"ca_fingerprint":    self.CAFingerprint(),
		"fabric_fips_image": fips,
		"peering_cn":        DerivedCN(self.SovereignID(), other.SovereignID()),
		"sovereign_id":      self.SovereignID(),
		"trust_domain":      self.TrustDomain(),
	}
}

// flows emits one entry per direction that is fully specified, ordered so that
// the flow originating from parties[0] comes first (§7, §8.1). A pair where
// neither direction is complete yields an empty array, which is a valid
// agreement — a path that carries nothing — and must not be reported as an
// error.
func flows(p0, p1 *Package) []any {
	out := make([]any, 0, 2)
	for _, d := range [][2]*Package{{p0, p1}, {p1, p0}} {
		if fl, ok := flow(d[0], d[1]); ok {
			out = append(out, fl)
		}
	}
	return out
}

// flow is direction from→to. It is emitted only if the originator declares a
// release set and the receiver declares all three ceilings. A direction missing
// either half is not emitted: a flow is never emitted with a limit it does not
// have, and a zero standing in for "not declared" is worse than an absent flow
// — it is a real limit both sides will agree on and neither party chose.
func flow(from, to *Package) (map[string]any, bool) {
	release, ok := from.ReleaseSet()
	if !ok {
		return nil, false
	}
	env, ok := to.Envelope()
	if !ok {
		return nil, false
	}
	return map[string]any{
		// The receiver's declared figures. You cannot be obliged to accept more
		// than you declared, and the sender cannot raise your ceiling.
		"envelope": map[string]any{
			"bytes_per_message": env["accept_bytes_per_message"],
			"distinct_keys":     env["accept_distinct_keys"],
			"hz":                env["accept_hz"],
		},
		"from": from.SovereignID(),
		// Derived, never negotiated (§3.2).
		"publish_under": DerivedPrefix(from.SovereignID()),
		// Carried verbatim, sorted and de-duplicated. Never merged, never
		// widened; there is no wildcard form of a release set.
		"release": toAny(release),
		"to":      to.SovereignID(),
	}, true
}

// earlier returns the earlier of two YYYY-MM-DD dates. A composed link inherits
// the shorter term, so it has one, and neither party's term is silently
// extended by the other's (§7).
func earlier(a, b string) string {
	if b < a {
		return b
	}
	return a
}

func minNumber(a, b json.Number) json.Number {
	ia, oka := new(big.Int).SetString(a.String(), 10)
	ib, okb := new(big.Int).SetString(b.String(), 10)
	if oka && okb && ib.Cmp(ia) < 0 {
		return b
	}
	return a
}
