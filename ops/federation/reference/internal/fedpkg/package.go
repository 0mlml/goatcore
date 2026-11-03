package fedpkg

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	SpecVersion   = "federation-package/v1"
	AgreementSpec = "federation-agreement/v1"
	DateLayout    = "2006-01-02"
)

// TransportPreference is fixed by the specification, not by local policy (§7).
// A local preference order would make composition non-deterministic, which
// would break the digest comparison the whole design rests on.
//
// WireGuard is first, decided. It is what we run, what our
// waiver covers, and what we can cut unilaterally; transport_offer.public_key
// is WireGuard-shaped and has no defined IPsec derivation, so ranking
// IPsec first preferred a transport this format cannot describe an offer for.
// A party that needs IPsec gets it by declaring ipsec-fips and not wireguard,
// which makes the intersection decide and never reaches this order — so the
// exception costs a declaration, not a change here.
var TransportPreference = []string{"wireguard", "ipsec-fips"}

var (
	// §3: 2–16 characters, lowercase a-z0-9-. A full stop cannot occur, which
	// is what lets a peering name parse by splitting on one (§3.1).
	sovereignRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,14}[a-z0-9])?$`)
	dateRe        = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	fingerprintRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

	// §5: Ed25519 is exactly 64 bytes, base64 with the standard alphabet of
	// RFC 4648 §4, padded. 64 bytes is 21 whole three-byte groups and one byte
	// over, so the encoding is always 88 characters ending in TWO padding
	// characters. Not the URL-safe alphabet: a value containing - or _ is
	// malformed, not something to be decoded a second way.
	signatureRe = regexp.MustCompile(`^[A-Za-z0-9+/]{86}==$`)

	// §3: a name an operator can misread as a wildcard in an access rule is a
	// defect in the name, not in the operator.
	wildcardWords = map[string]bool{"any": true, "all": true, "none": true}
)

// Finding is one problem with a package or a pair of them. Refusal is an
// outcome of the process working, not an error condition, so findings are
// returned as data rather than raised.
type Finding struct {
	Code string `json:"code"`
	Msg  string `json:"message"`
}

func (f Finding) String() string { return f.Code + ": " + f.Msg }

// Codes returns just the rule names, which is what a test asserts on: a fixture
// that refuses for the wrong reason is a passing test hiding a broken
// implementation.
func Codes(f []Finding) []string {
	out := make([]string, 0, len(f))
	for _, x := range f {
		out = append(out, x.Code)
	}
	return out
}

// Package is one party's half, kept as a decoded map so that unknown members
// survive canonicalisation intact. A typed struct would silently drop members
// added by a newer minor revision and change the digest.
type Package struct {
	Raw map[string]any
}

func Load(data []byte) (*Package, error) {
	m, err := DecodeJSON(data)
	if err != nil {
		return nil, err
	}
	return &Package{Raw: m}, nil
}

func (p *Package) str(path ...string) string {
	cur := any(p.Raw)
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[k]
	}
	s, _ := cur.(string)
	return s
}

func (p *Package) sub(k string) map[string]any {
	m, _ := p.Raw[k].(map[string]any)
	return m
}

// has reports whether a top-level block is present as an object. Presence is
// the declaration (§3): a package carrying transport_offer is willing to
// connect, a package carrying data_offer is willing to exchange.
func (p *Package) has(k string) bool { return p.sub(k) != nil }

func (p *Package) SovereignID() string     { return p.str("sovereign_id") }
func (p *Package) TrustDomain() string     { return p.str("identity", "trust_domain") }
func (p *Package) CAFingerprint() string   { return p.str("identity", "ca_fingerprint") }
func (p *Package) Spec() string            { return p.str("spec") }
func (p *Package) Expires() string         { return p.str("expires") }
func (p *Package) HasTransportOffer() bool { return p.has("transport_offer") }
func (p *Package) HasDataOffer() bool      { return p.has("data_offer") }

// FabricFIPSImage is carried verbatim, per party, into the agreement (§7).
// Both false and one-of-each are different situations and a single combined
// boolean cannot tell them apart.
func (p *Package) FabricFIPSImage() (bool, bool) {
	c := p.sub("capabilities")
	if c == nil {
		return false, false
	}
	b, ok := c["fabric_fips_image"].(bool)
	return b, ok
}

// Transports is capabilities.transports, sorted and de-duplicated (§4).
func (p *Package) Transports() []string { return sortedStrings(p.sub("capabilities")["transports"]) }

// Listener is transport_offer.listener. The second result is false when the
// package carries no transport offer, or the member is not a boolean.
func (p *Package) Listener() (bool, bool) {
	t := p.sub("transport_offer")
	if t == nil {
		return false, false
	}
	b, ok := t["listener"].(bool)
	return b, ok
}

func (p *Package) offerNum(block, k string) (json.Number, bool) {
	b := p.sub(block)
	if b == nil {
		return "", false
	}
	return num(b[k])
}

func (p *Package) MeasuredMTU() (json.Number, bool) {
	return p.offerNum("transport_offer", "measured_mtu")
}

func (p *Package) ProposedLinkSubnet() (string, bool) {
	t := p.sub("transport_offer")
	if t == nil {
		return "", false
	}
	s, ok := t["proposed_link_subnet"].(string)
	return s, ok
}

// ReleaseSet is data_offer.release, sorted and de-duplicated (§4). The second
// result is false when the party declares none, which is one half of the flow
// rule in §7.
func (p *Package) ReleaseSet() ([]string, bool) {
	d := p.sub("data_offer")
	if d == nil {
		return nil, false
	}
	arr, ok := d["release"].([]any)
	if !ok || len(arr) == 0 {
		return nil, false
	}
	return sortedStrings(arr), true
}

// acceptKeys are the three ceilings a receiver declares. All three or none: a
// blank limit is not a sensible default, it is no limit at all.
var acceptKeys = []string{"accept_bytes_per_message", "accept_distinct_keys", "accept_hz"}

// Envelope is the receiver's three declared values (§7). The second result is
// false unless all three are present, which is the other half of the flow rule.
func (p *Package) Envelope() (map[string]json.Number, bool) {
	d := p.sub("data_offer")
	if d == nil {
		return nil, false
	}
	out := make(map[string]json.Number, len(acceptKeys))
	for _, k := range acceptKeys {
		n, ok := num(d[k])
		if !ok {
			return nil, false
		}
		out[k] = n
	}
	return out, true
}

// DerivedCN is the peering name required by §3.1. It is computed, never
// chosen, so the two parties cannot disagree about it and nobody can mistype it
// into a refusal that is silent at both ends. A package does not declare it.
func DerivedCN(issuer, counterparty string) string {
	return fmt.Sprintf("fed.%s.%s.01", issuer, counterparty)
}

// DerivedPrefix is the publishing prefix required by §3.2. One symmetric rule:
// every party publishes across the link under its own name. A package does not
// declare it.
func DerivedPrefix(sovereign string) string {
	return fmt.Sprintf("from/%s/**", sovereign)
}

// Lint checks a package against itself: the members that are always required,
// and the internal completeness of whatever optional blocks are present.
//
// There is no tier and no ladder (§3). Presence is the declaration, so there is
// nothing to claim and therefore nothing that can disagree with a claim. What a
// pair can do is derived from what both packages carry (§7).
func (p *Package) Lint(now time.Time) []Finding {
	f := p.structure()

	if got := p.Spec(); got != SpecVersion {
		f = append(f, Finding{"spec", fmt.Sprintf("spec must be %q, got %q", SpecVersion, got)})
	}
	if exp := p.Expires(); dateRe.MatchString(exp) {
		if t, err := time.Parse(DateLayout, exp); err == nil && t.Before(now) {
			f = append(f, Finding{"expires.past", fmt.Sprintf("package expired on %s", exp)})
		}
	}
	if iss, exp := p.str("issued"), p.Expires(); dateRe.MatchString(iss) && dateRe.MatchString(exp) && exp < iss {
		f = append(f, Finding{"issued.after_expires", fmt.Sprintf("issued %s is after expires %s", iss, exp)})
	}
	sortFindings(f)
	return f
}

// structure is everything about a package that composition depends on: the
// always-required members, and the completeness of each optional block that is
// present. It deliberately excludes the value of spec and the expiry date,
// because those are pair rules in §6 and must refuse under their own names.
//
// A block present but incomplete is refused, never defaulted (§3) — silent
// defaults are how a link ends up with no rate limit.
func (p *Package) structure() []Finding {
	var f []Finding
	add := func(code, msg string, a ...any) {
		f = append(f, Finding{code, fmt.Sprintf(msg, a...)})
	}

	// A null is an error at any depth (§4). It is refused and never repaired,
	// because the signature covers the null: a receiver that quietly dropped
	// "cut": null would verify one document and compose a different one, and
	// both parties would then be normalising on their own judgement, which is
	// how one pair of files yields two digests.
	//
	// It runs first and alone. A null reaching the checks below would be
	// reported as an absent member, and a null is not a shorthand for absence.
	var nulls []string
	findNulls(p.Raw, "", &nulls)
	if len(nulls) > 0 {
		for _, path := range nulls {
			add("null."+path, "%s is null; a member is either present with a value or absent, and a null is not a shorthand for absence (§4)", path)
		}
		sortFindings(f)
		return f
	}

	sid := p.SovereignID()
	switch {
	case !sovereignRe.MatchString(sid):
		add("sovereign_id", "%q must be 2-16 characters of lowercase a-z0-9-, not starting or ending with a hyphen (§3)", sid)
	case wildcardWords[sid]:
		add("sovereign_id.wildcard", "%q reads as a wildcard in an access rule; a name an operator can misread is a defect in the name (§3)", sid)
	}

	for _, k := range []string{"issued", "expires"} {
		v := p.str(k)
		if v == "" {
			add(k, "%s is required (§3)", k)
			continue
		}
		if !dateRe.MatchString(v) {
			add(k, "%s must be YYYY-MM-DD with no time and no zone, got %q (§4)", k, v)
			continue
		}
		if _, err := time.Parse(DateLayout, v); err != nil {
			add(k, "%s is %q, which is not a date", k, v)
		}
	}

	// identity — always required, both members.
	if id := p.sub("identity"); id == nil {
		add("identity", "identity is required (§3)")
	} else {
		if td, ok := id["trust_domain"].(string); !ok || td == "" {
			add("identity.trust_domain", "identity.trust_domain is required and must be a non-empty string (§8.1)")
		}
		fp, ok := id["ca_fingerprint"].(string)
		switch {
		case !ok || fp == "":
			add("identity.ca_fingerprint", "identity.ca_fingerprint is required (§8.1)")
		case !fingerprintRe.MatchString(fp):
			add("identity.ca_fingerprint", "%q must be sha256: followed by 64 lowercase hex characters (§8.1)", fp)
		}
	}

	// capabilities — always required. transports arms §6 step 4(a);
	// fabric_fips_image is carried per party into every agreement (§8.1), so
	// both must be present in every package.
	if caps := p.sub("capabilities"); caps == nil {
		add("capabilities", "capabilities is required (§3)")
	} else {
		if err := checkStringSet(caps["transports"]); err != nil {
			add("capabilities.transports", "capabilities.transports %s", err)
		}
		if _, ok := caps["fabric_fips_image"].(bool); !ok {
			add("capabilities.fabric_fips_image", "capabilities.fabric_fips_image is required and must be a boolean; it is carried verbatim per party into the agreement (§7)")
		}
	}

	// transport_offer — optional as a block, complete once present (§3).
	if p.HasTransportOffer() {
		t := p.sub("transport_offer")
		for _, k := range []string{"endpoint", "public_key", "proposed_link_subnet"} {
			if s, ok := t[k].(string); !ok || s == "" {
				add("transport_offer."+k, "transport_offer.%s is required once transport_offer is present and must be a non-empty string (§3)", k)
			}
		}
		if _, ok := t["listener"].(bool); !ok {
			add("transport_offer.listener", "transport_offer.listener is required once transport_offer is present and must be a boolean (§3, §7)")
		}
		if n, ok := num(t["measured_mtu"]); !ok {
			add("transport_offer.measured_mtu", "transport_offer.measured_mtu is required and must be measured, never assumed (§3, §7)")
		} else if err := checkInteger("transport_offer.measured_mtu", n); err != nil {
			add("transport_offer.measured_mtu", "%v", err)
		}
	}

	// data_offer carries two separable declarations, and each is complete once
	// present, independently of the other (§3). Send is release; receive is the
	// three accept_* ceilings. At least one half must be there.
	//
	// One-way federation is a real case, not a degenerate one. Requiring all
	// four together would force a receive-only partner to invent a release set
	// it will never publish under — and a package carrying a fiction written to
	// satisfy a validator is worse than one honest half, because the fiction is
	// later read as an intention.
	if p.HasDataOffer() {
		d := p.sub("data_offer")

		declared := 0
		for _, k := range acceptKeys {
			if _, present := d[k]; present {
				declared++
			}
		}
		switch {
		case declared == len(acceptKeys):
			for _, k := range acceptKeys {
				n, ok := num(d[k])
				if !ok {
					add("data_offer."+k, "data_offer.%s must be an integer (§4)", k)
					continue
				}
				if err := checkInteger("data_offer."+k, n); err != nil {
					add("data_offer."+k, "%v", err)
				}
			}
		case declared > 0:
			// A partial receive half is refused: a limit that is not stated is
			// a limit that does not exist. That is §7's rule that a flow is
			// never emitted with a limit it does not have, seen from the other
			// end.
			var missing []string
			for _, k := range acceptKeys {
				if _, present := d[k]; !present {
					missing = append(missing, k)
				}
			}
			add("data_offer.receive_half", "the receive half of data_offer is partial: %s missing. All three accept_* values or none — a limit that is not stated is a limit that does not exist (§3)", strings.Join(missing, ", "))
		}

		_, hasSend := d["release"]
		if hasSend {
			if err := checkStringSet(d["release"]); err != nil {
				add("data_offer.release", "data_offer.release %s; there is no wildcard form of a release set (§7)", err)
			}
		}
		if !hasSend && declared == 0 {
			add("data_offer.empty", "data_offer carries neither half: it must declare release, or all three accept_* values, or both (§3)")
		}

		// A peering with no agreed cut procedure gets severed badly, at speed,
		// by someone guessing (§3). Required whichever halves data_offer carries.
		if c := p.sub("cut"); c == nil {
			add("cut", "data_offer requires cut alongside it, whichever halves it carries (§3)")
		}
	}

	// cut — optional as a block, complete once present (§3).
	if c := p.sub("cut"); c != nil {
		if s, ok := c["contact"].(string); !ok || s == "" {
			add("cut.contact", "cut.contact is required once cut is present and must be a non-empty string (§3)")
		}
	}

	// signature — the encoding, not the cryptography (§5). A wrong-shaped value
	// is refused here, before verification is attempted, so that it reads as a
	// malformed package rather than as a bad signature. Those are two different
	// findings that would otherwise look alike.
	raw, present := p.Raw["signature"]
	if !present {
		add("signature", "signature is required (§3); an unsigned package authenticates nothing (§5)")
	} else if err := checkSignatureEncoding(raw); err != nil {
		add("signature", "%v", err)
	}

	sortFindings(f)
	return f
}

func checkStringSet(v any) error {
	arr, ok := v.([]any)
	if !ok {
		return fmt.Errorf("is required and must be an array of strings")
	}
	if len(arr) == 0 {
		return fmt.Errorf("is empty, and an empty array is not a declaration: a half is said by omitting it, never by carrying nothing (§3)")
	}
	for _, e := range arr {
		if _, ok := e.(string); !ok {
			return fmt.Errorf("must contain strings only, got %T", e)
		}
	}
	return nil
}

func checkInteger(name string, n json.Number) error {
	_, err := canonicalInteger(n, name)
	return err
}

func sortFindings(f []Finding) {
	sort.SliceStable(f, func(i, j int) bool { return f[i].Code < f[j].Code })
}

// findNulls records the path of every null in a decoded document, at any depth.
// Paths are reported in a stable order so a finding list does not depend on Go's
// map iteration.
func findNulls(v any, path string, out *[]string) {
	switch t := v.(type) {
	case nil:
		at := path
		if at == "" {
			at = "(the document)"
		}
		*out = append(*out, at)
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return LessUTF16(keys[i], keys[j]) })
		for _, k := range keys {
			child := k
			if path != "" {
				child = path + "." + k
			}
			findNulls(t[k], child, out)
		}
	case []any:
		for i, e := range t {
			findNulls(e, fmt.Sprintf("%s[%d]", path, i), out)
		}
	}
}

// checkSignatureEncoding enforces §5's encoding rules on the signature member.
//
// Ed25519 is exactly 64 bytes. 64 is 21 whole three-byte groups with one byte
// left over, so the base64 is always 88 characters ending in two padding
// characters — never one. Decoding is strict: the standard alphabet only, no
// whitespace, no line breaks, and a final quantum whose unused bits are zero,
// which is checked by re-encoding and comparing.
func checkSignatureEncoding(raw any) error {
	s, ok := raw.(string)
	if !ok {
		return fmt.Errorf("signature must be a string (§5)")
	}
	if len(s) != 88 {
		return fmt.Errorf("signature is %d characters; an Ed25519 signature is exactly 64 bytes, which is exactly 88 base64 characters (§5)", len(s))
	}
	if !signatureRe.MatchString(s) {
		return fmt.Errorf("signature is not base64 in the standard alphabet of RFC 4648 §4 with the padding 64 bytes requires: A-Z a-z 0-9 + / and a trailing \"==\". The URL-safe alphabet is not an alternative encoding here (§5)")
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("signature is not valid base64: %w (§5)", err)
	}
	if len(b) != ed25519.SignatureSize {
		return fmt.Errorf("signature decodes to %d bytes, not %d (§5)", len(b), ed25519.SignatureSize)
	}
	// Rejects a final quantum whose unused bits are not zero, which Go's
	// decoder accepts and which would let one signature have two spellings.
	if base64.StdEncoding.EncodeToString(b) != s {
		return fmt.Errorf("signature is not the canonical base64 of its own bytes: the unused bits of the final quantum are not zero (§5)")
	}
	return nil
}

// SigningBytes is the canonical form with the signature member removed (§5).
func (p *Package) SigningBytes() ([]byte, error) {
	clone := make(map[string]any, len(p.Raw))
	for k, v := range p.Raw {
		if k != "signature" {
			clone[k] = v
		}
	}
	return Canonical(clone)
}

// CanonicalBytes is the canonical form of the complete signed package.
func (p *Package) CanonicalBytes() ([]byte, error) { return Canonical(p.Raw) }

// VerifySignature checks the package against a public key supplied by the
// caller.
//
// The key is a parameter, never something this code fetches and never taken
// from the package: a package carrying its own key authenticates nothing. It is
// the key bound to that party's sovereign_id over a channel authenticated
// independently of the relationship being established (§5.1). A directory is a
// cache of that binding, never its source — a key learned only from a directory
// is a key learned from whoever runs the directory.
func (p *Package) VerifySignature(pub ed25519.PublicKey) error {
	sig, _ := p.Raw["signature"].(string)
	if sig == "" {
		return fmt.Errorf("package carries no signature")
	}
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("signature is not valid base64: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("the key bound to %q is %d bytes, not an Ed25519 public key", p.SovereignID(), len(pub))
	}
	msg, err := p.SigningBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, msg, raw) {
		return fmt.Errorf("signature does not verify under the key bound to %q", p.SovereignID())
	}
	return nil
}

// Digest is the value the two parties compare (§9). The channel carrying it
// does not need to be secure: altering it in transit causes a visible mismatch,
// and causing a false match would require forging a signature.
func Digest(v any) (string, error) {
	b, err := Canonical(v)
	if err != nil {
		return "", err
	}
	return DigestBytes(b), nil
}

// DigestBytes is sha256 over already-canonical bytes.
func DigestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// sortedStrings sorts and de-duplicates an array that represents a set (§4).
func sortedStrings(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	seen := map[string]bool{}
	for _, e := range arr {
		if s, ok := e.(string); ok && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return LessUTF16(out[i], out[j]) })
	return out
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func num(v any) (json.Number, bool) {
	n, ok := v.(json.Number)
	return n, ok
}

// ParseDate reads one of this format's dates: YYYY-MM-DD, no times, no zones,
// no clock skew between the parties (§4).
func ParseDate(s string) (time.Time, error) {
	if !dateRe.MatchString(s) {
		return time.Time{}, fmt.Errorf("%q is not YYYY-MM-DD", s)
	}
	return time.Parse(DateLayout, s)
}

// ParsePublicKey reads an Ed25519 public key in hex or base64, which are the
// two forms the conformance vectors ship.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	s = strings.TrimSpace(s)
	if b, err := hex.DecodeString(s); err == nil && len(b) == ed25519.PublicKeySize {
		return ed25519.PublicKey(b), nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("key is neither hex nor base64: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("key is %d bytes, not %d", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}
