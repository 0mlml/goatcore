package fedpkg

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// The conformance vectors are normative. Every value in them was derived by
// hand from the specification and no file there was produced by running this
// code, which is the property that makes them a test of it rather than a
// description of it. If this implementation and a vector disagree, this
// implementation is wrong.
const vectorsDir = "../../../vectors"

// composingVectors produce an agreement. refusalVectors produce a refusal and
// nothing else, under the rule named here — a fixture that refuses for the
// wrong reason is a passing test hiding a broken implementation, and it is the
// specific way this kind of suite goes quietly wrong.
var (
	composingVectors = []string{
		"compose-collation-utf16",
		"compose-crossed-extremes",
		"compose-expiry-boundary",
		"compose-full",
		"compose-listener-second-party",
		"compose-no-flows",
		"compose-no-transport",
		"compose-one-way",
		"compose-party-order-not-trust-domain",
		"compose-preference-and-tiebreak",
		"compose-transport-one-sided",
		"compose-transport-outside-preference-ignored",
		"compose-unsorted-release",
	}

	// Ten fixtures for the seven rules of §6. Three rules carry two apiece,
	// because in each an implementation can pass one and fail the other: the
	// spec pin is literal rather than an equality test between the packages;
	// transport compatibility is tested against §7's preference order rather
	// than the raw intersection; and expiry is checked on BOTH packages, which
	// one fixture can only ever half-prove.
	refusalVectors = map[string]string{
		"refuse-bad-signature":                RefuseSignature,
		"refuse-version-mismatch":             RefuseVersionMismatch,
		"refuse-both-v2":                      RefuseVersionMismatch,
		"refuse-same-sovereign-id":            RefuseSameSovereignID,
		"refuse-same-trust-domain":            RefuseSameTrustDomain,
		"refuse-expired":                      RefuseExpired,
		"refuse-expired-first-party":          RefuseExpired,
		"refuse-no-transport-overlap":         RefuseNoTransportOverlap,
		"refuse-transport-outside-preference": RefuseNoTransportOverlap,
		"refuse-neither-listener":             RefuseNeitherListener,
	}

	// refuse-bad-signature carries the untampered signing input on purpose, so
	// that the failure is inspectable: signing input and package disagree by
	// exactly one token. It is the one place where recomputing the signing
	// input from the package must not reproduce the committed file.
	tamperedPackage = filepath.Join("refuse-bad-signature", "package-xaa")
)

func evaluationDate(t *testing.T) time.Time {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(vectorsDir, "now.txt"))
	if err != nil {
		t.Fatalf("reading now.txt: %v", err)
	}
	// now is a vector input, not a wall clock: run at this date or the expiry
	// fixtures change verdict as time passes.
	d, err := ParseDate(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("now.txt: %v", err)
	}
	return d
}

// testKeyring is the binding §5.1 requires, supplied here from the committed
// test keys. Those keys are public, worthless, and exist only to reproduce
// these vectors.
func testKeyring(t *testing.T) Keyring {
	t.Helper()
	k := Keyring{}
	paths, err := filepath.Glob(filepath.Join(vectorsDir, "testkeys", "*.public.hex"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no test keys found: %v", err)
	}
	for _, p := range paths {
		sid := strings.TrimSuffix(filepath.Base(p), ".public.hex")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		pub, err := ParsePublicKey(string(b))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		k[sid] = pub
	}
	return k
}

func vectorDirs(t *testing.T) []string {
	t.Helper()
	var out []string
	for d := range refusalVectors {
		out = append(out, d)
	}
	out = append(out, composingVectors...)
	sort.Strings(out)
	return out
}

// packagePair returns the two packages of a vector directory, in filename
// order. Composition is symmetric, so the order is immaterial to the outcome.
func packagePair(t *testing.T, dir string) (paths []string, pkgs []*Package) {
	t.Helper()
	all, err := filepath.Glob(filepath.Join(vectorsDir, dir, "package-*.canonical.json"))
	if err != nil {
		t.Fatalf("globbing %s: %v", dir, err)
	}
	for _, p := range all {
		if strings.Contains(filepath.Base(p), ".signing-input.") {
			continue
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) != 2 {
		t.Fatalf("%s holds %d packages, want 2: %v", dir, len(paths), paths)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		pkg, err := Load(b)
		if err != nil {
			t.Fatalf("loading %s: %v", p, err)
		}
		pkgs = append(pkgs, pkg)
	}
	return paths, pkgs
}

// TestVectorBytesHaveNoTrailingNewline pins the property the vectors state
// about themselves: a canonical form is bytes, not a text file. A tool that
// appends a newline produces a different digest, which is exactly the class of
// failure these vectors exist to catch.
func TestVectorBytesHaveNoTrailingNewline(t *testing.T) {
	for _, dir := range vectorDirs(t) {
		matches, _ := filepath.Glob(filepath.Join(vectorsDir, dir, "*.canonical.json"))
		for _, p := range matches {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("reading %s: %v", p, err)
			}
			if bytes.HasSuffix(b, []byte("\n")) {
				t.Errorf("%s ends with a newline; a canonical form is bytes, not a text file", p)
			}
		}
	}
}

// TestVectorPackagesAreCanonical checks this canonicaliser against every
// committed package, byte for byte. These bytes were written by hand from §4.
func TestVectorPackagesAreCanonical(t *testing.T) {
	for _, dir := range vectorDirs(t) {
		paths, pkgs := packagePair(t, dir)
		for i, p := range paths {
			want, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("reading %s: %v", p, err)
			}
			got, err := pkgs[i].CanonicalBytes()
			if err != nil {
				t.Fatalf("%s: canonical form: %v", p, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s: canonical form differs\n got %s\nwant %s", p, got, want)
			}
		}
	}
}

// TestVectorSigningInputs checks §5's signing input — the canonical form with
// the signature member removed — against the committed bytes.
func TestVectorSigningInputs(t *testing.T) {
	for _, dir := range vectorDirs(t) {
		paths, pkgs := packagePair(t, dir)
		for i, p := range paths {
			stem := strings.TrimSuffix(p, ".canonical.json")
			want, err := os.ReadFile(stem + ".signing-input.canonical.json")
			if err != nil {
				t.Fatalf("reading signing input for %s: %v", p, err)
			}
			got, err := pkgs[i].SigningBytes()
			if err != nil {
				t.Fatalf("%s: signing bytes: %v", p, err)
			}
			tampered := strings.HasSuffix(stem, tamperedPackage)
			switch {
			case tampered && bytes.Equal(got, want):
				t.Errorf("%s: the tampered package reproduces the committed signing input; the fixture is meant to differ from it by exactly one token", p)
			case !tampered && !bytes.Equal(got, want):
				t.Errorf("%s: signing input differs\n got %s\nwant %s", p, got, want)
			}
		}
	}
}

// TestVectorSignatures verifies each package under the key bound to the
// sovereign_id it declares, which is what §6 step 0 does.
func TestVectorSignatures(t *testing.T) {
	keys := testKeyring(t)
	for _, dir := range vectorDirs(t) {
		paths, pkgs := packagePair(t, dir)
		for i, p := range paths {
			pkg := pkgs[i]
			pub, ok := keys[pkg.SovereignID()]
			if !ok {
				t.Fatalf("%s: no test key for %q", p, pkg.SovereignID())
			}
			err := pkg.VerifySignature(pub)
			tampered := strings.HasSuffix(strings.TrimSuffix(p, ".canonical.json"), tamperedPackage)
			if tampered {
				if err == nil {
					t.Errorf("%s: signature verifies, but the fixture exists because it must not", p)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s: %v", p, err)
			}
		}
	}
}

// TestTamperedPackageSignatureCoversTheEarlierBytes states what
// refuse-bad-signature actually pins: the signature is valid over the bytes it
// was made on and invalid over the bytes now in the package. The tampered value
// is a rate limit, so a composer reading it before checking the signature would
// have accepted a limit the counterparty never signed.
func TestTamperedPackageSignatureCoversTheEarlierBytes(t *testing.T) {
	keys := testKeyring(t)
	stem := filepath.Join(vectorsDir, tamperedPackage)

	raw, err := os.ReadFile(stem + ".canonical.json")
	if err != nil {
		t.Fatalf("%v", err)
	}
	pkg, err := Load(raw)
	if err != nil {
		t.Fatalf("%v", err)
	}
	untampered, err := os.ReadFile(stem + ".signing-input.canonical.json")
	if err != nil {
		t.Fatalf("%v", err)
	}
	sig, err := signatureBytes(pkg)
	if err != nil {
		t.Fatalf("%v", err)
	}
	pub := keys[pkg.SovereignID()]

	if !ed25519.Verify(pub, untampered, sig) {
		t.Error("the signature does not verify over the committed untampered signing input; the fixture is not what its REFUSAL.md describes")
	}
	now, err := pkg.SigningBytes()
	if err != nil {
		t.Fatalf("%v", err)
	}
	if ed25519.Verify(pub, now, sig) {
		t.Error("the signature verifies over the tampered package's own signing input, which would make the fixture untestable")
	}
}

func signatureBytes(p *Package) ([]byte, error) {
	s, _ := p.Raw["signature"].(string)
	return base64.StdEncoding.DecodeString(s)
}

// TestComposeVectors is the whole point: compose the pair, compare the
// canonical bytes byte-for-byte, compare the digest string.
func TestComposeVectors(t *testing.T) {
	keys := testKeyring(t)
	now := evaluationDate(t)

	for _, dir := range composingVectors {
		t.Run(dir, func(t *testing.T) {
			_, pkgs := packagePair(t, dir)
			ag, findings := Compose(pkgs[0], pkgs[1], keys, now)
			if len(findings) > 0 {
				t.Fatalf("refused, but this vector composes: %v", Codes(findings))
			}
			if ag == nil {
				t.Fatal("no agreement and no findings")
			}

			got, err := ag.Canonical()
			if err != nil {
				t.Fatalf("canonical form: %v", err)
			}
			want, err := os.ReadFile(filepath.Join(vectorsDir, dir, "agreement.canonical.json"))
			if err != nil {
				t.Fatalf("%v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("agreement differs\n got %s\nwant %s", got, want)
			}

			gotDigest, err := ag.Digest()
			if err != nil {
				t.Fatalf("digest: %v", err)
			}
			wantDigest, err := os.ReadFile(filepath.Join(vectorsDir, dir, "agreement.digest.txt"))
			if err != nil {
				t.Fatalf("%v", err)
			}
			if w := strings.TrimSpace(string(wantDigest)); gotDigest != w {
				t.Errorf("digest\n got %s\nwant %s", gotDigest, w)
			}
			// The digest is over the committed bytes as well as over ours, so a
			// vector whose digest file drifted from its agreement is caught here
			// rather than being silently satisfied by our own output.
			if d := DigestBytes(want); d != strings.TrimSpace(string(wantDigest)) {
				t.Errorf("the committed digest does not match the committed agreement bytes: %s", d)
			}
		})
	}
}

// TestComposeIsSymmetric is §2 stated as a test. An agreement has no "us" and
// no "them": swapping the arguments must not change one byte.
func TestComposeIsSymmetric(t *testing.T) {
	keys := testKeyring(t)
	now := evaluationDate(t)

	for _, dir := range composingVectors {
		_, pkgs := packagePair(t, dir)
		ab, f1 := Compose(pkgs[0], pkgs[1], keys, now)
		ba, f2 := Compose(pkgs[1], pkgs[0], keys, now)
		if len(f1) > 0 || len(f2) > 0 {
			t.Fatalf("%s: refused: %v %v", dir, Codes(f1), Codes(f2))
		}
		x, err := ab.Canonical()
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		y, err := ba.Canonical()
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if !bytes.Equal(x, y) {
			t.Errorf("%s: composition has a perspective\n a,b %s\n b,a %s", dir, x, y)
		}
	}
}

// TestRefusalVectors asserts each fixture is refused and names the rule that
// refused it. It also asserts that no other rule fired: every fixture is built
// so that only its own rule can, and a second finding means a rule is firing on
// a package that does not break it.
func TestRefusalVectors(t *testing.T) {
	keys := testKeyring(t)
	now := evaluationDate(t)

	dirs := make([]string, 0, len(refusalVectors))
	for d := range refusalVectors {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		want := refusalVectors[dir]
		t.Run(dir, func(t *testing.T) {
			_, pkgs := packagePair(t, dir)
			ag, findings := Compose(pkgs[0], pkgs[1], keys, now)
			if ag != nil {
				b, _ := ag.Canonical()
				t.Fatalf("composed instead of refusing; a refusal produces no agreement and no digest\n%s", b)
			}
			if len(findings) == 0 {
				t.Fatal("neither an agreement nor a refusal")
			}
			for _, f := range findings {
				if f.Code != want {
					t.Errorf("refused under %q, want %q — a fixture that refuses for the wrong reason is a passing test hiding a broken implementation\n  %s", f.Code, want, f)
				}
			}
			// Symmetric too: a refusal must not depend on argument order.
			if _, rev := Compose(pkgs[1], pkgs[0], keys, now); len(rev) == 0 || Codes(rev)[0] != want {
				t.Errorf("swapping the arguments changes the refusal: %v", Codes(rev))
			}
		})
	}
}

// TestComposeRefusesAnUnboundKey covers the half of step 0 no fixture can carry:
// a package whose sovereign_id no key is bound to. The binding is an input,
// established over an independently authenticated channel (§5.1), so its
// absence is a refusal and never a lookup.
func TestComposeRefusesAnUnboundKey(t *testing.T) {
	now := evaluationDate(t)
	_, pkgs := packagePair(t, "compose-full")

	keys := testKeyring(t)
	delete(keys, pkgs[1].SovereignID())

	ag, findings := Compose(pkgs[0], pkgs[1], keys, now)
	if ag != nil {
		t.Fatal("composed a package no key is bound to")
	}
	if len(findings) != 1 || findings[0].Code != RefuseSignature {
		t.Fatalf("want one %s finding, got %v", RefuseSignature, Codes(findings))
	}
}

// TestVectorPackagesLintClean checks that every package in every fixture is
// well-formed on its own. Each refusal fixture is built so that only its own
// pair rule fires, so a structural finding here means this implementation is
// inventing a requirement the specification does not state.
func TestVectorPackagesLintClean(t *testing.T) {
	for _, dir := range vectorDirs(t) {
		paths, pkgs := packagePair(t, dir)
		for i, p := range paths {
			for _, f := range pkgs[i].structure() {
				t.Errorf("%s: %s", p, f)
			}
		}
	}
}

// The malformed kind: single packages, one file each, no counterparty, no
// agreement, no digest. It exists because §4's rules fire on inputs a
// well-formed pair cannot contain — a pair's packages come from a conforming
// serialiser, so no directory of pairs can carry an empty number token, a
// duplicate member name or an unpaired surrogate, however many pairs anyone
// writes.
//
// stage records WHERE the refusal must happen, which is the assertion that
// matters. Both stages are before §6 step 0; neither is a composition refusal.
type malformedStage int

const (
	// Refused while reading the bytes: not valid JSON, or valid JSON syntax
	// denoting something that is not a value this format can carry.
	refusedAtParse malformedStage = iota
	// Parsed, then refused by the structural rules of §3 and §4.
	refusedAtStructure
)

type malformedCase struct {
	stage    malformedStage
	code     string // refusedAtStructure: the finding, beyond the unsigned-ness every file here shares
	mentions string // a term the refusal must name, so it cannot pass for the wrong reason
}

// Every file in malformed/ is unsigned by design — the README says so, and it
// keeps the refusal attributable to the named rule rather than to a failed
// verification. So the assertion is: the findings, setting aside that shared
// unsigned-ness, are exactly the one rule the file is named for.
var malformedVectors = map[string]malformedCase{
	"duplicate-member.json":    {refusedAtParse, "", "duplicate"},
	"empty-number-token.json":  {refusedAtParse, "", ""},
	"leading-zero-number.json": {refusedAtParse, "", ""},
	"unpaired-surrogate.json":  {refusedAtParse, "", "surrogate"},
	"empty-release.json":       {refusedAtStructure, "data_offer.release", "empty"},
	"integer-above-range.json": {refusedAtStructure, "data_offer.accept_distinct_keys", "2^53"},
	"negative-zero.json":       {refusedAtStructure, "data_offer.accept_hz", ""},
	"non-integer-number.json":  {refusedAtStructure, "transport_offer.measured_mtu", "integers only"},
	"null-member.json":         {refusedAtStructure, "null.transport_offer.measured_mtu", "null"},
	"no-signature.json":        {refusedAtStructure, "signature", "required"},
}

func malformedPath(name string) string {
	return filepath.Join(vectorsDir, "malformed", name)
}

// TestMalformedVectors reads each file as BYTES and refuses it. The bytes are
// never round-tripped through this implementation's encoder to build the input:
// four of these carry tokens no conforming serialiser emits, and two are not
// valid JSON at all, so anything this code produced would not be the fixture.
func TestMalformedVectors(t *testing.T) {
	now := evaluationDate(t)

	names := make([]string, 0, len(malformedVectors))
	for n := range malformedVectors {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		c := malformedVectors[name]
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(malformedPath(name))
			if err != nil {
				t.Fatalf("%v", err)
			}

			pkg, loadErr := Load(raw)

			if c.stage == refusedAtParse {
				if loadErr == nil {
					t.Fatalf("parsed; this input is refused while reading the bytes, and accepting it means a later stage is working on a document that was never signed")
				}
				if c.mentions != "" && !strings.Contains(loadErr.Error(), c.mentions) {
					t.Errorf("the refusal does not name %q: %v", c.mentions, loadErr)
				}
				return
			}

			if loadErr != nil {
				t.Fatalf("refused while parsing, but this file is well-formed JSON and its defect is structural: %v", loadErr)
			}

			findings := pkg.Lint(now)
			if len(findings) == 0 {
				t.Fatal("accepted; an implementation that accepts any of these and produces a canonical form has failed, whatever bytes it produced")
			}

			// Set aside the unsigned-ness every file in this directory shares.
			// What remains must be exactly the rule the file is named for —
			// asserting only "it was refused" would pass an implementation that
			// noticed nothing but the missing signature.
			var others []string
			var sawSignature bool
			for _, f := range findings {
				if f.Code == "signature" {
					sawSignature = true
					continue
				}
				others = append(others, f.Code)
			}

			if c.code == "signature" {
				if !sawSignature {
					t.Fatalf("did not report the absent signature; findings were %v", Codes(findings))
				}
				if len(others) > 0 {
					t.Errorf("this file's only defect is the absent member, but it also reported %v", others)
				}
			} else {
				if len(others) != 1 || others[0] != c.code {
					t.Fatalf("reported %v beyond the shared unsigned-ness, want exactly [%s]", others, c.code)
				}
			}

			if c.mentions != "" {
				var msg string
				for _, f := range findings {
					if f.Code == c.code {
						msg = f.Msg
					}
				}
				if !strings.Contains(msg, c.mentions) {
					t.Errorf("the finding does not name %q: %s", c.mentions, msg)
				}
			}
		})
	}
}

// TestMalformedIsNeverAComposedRefusal is the location assertion the vectors
// README asks for: an implementation reporting one of these as a §6 refusal has
// the right verdict from the wrong place, and names a rule the package does not
// break.
//
// The malformed kind carries no counterparty on purpose, so the valid half here
// is borrowed rather than paired — this is a check on where the verdict comes
// from, not a fixture. The four parse-stage files never reach composition at
// all, which is that assertion in its strongest form.
func TestMalformedIsNeverAComposedRefusal(t *testing.T) {
	now := evaluationDate(t)
	keys := testKeyring(t)

	valid, err := os.ReadFile(filepath.Join(vectorsDir, "compose-full", "package-xab.canonical.json"))
	if err != nil {
		t.Fatal(err)
	}
	counterparty, err := Load(valid)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(malformedVectors))
	for n := range malformedVectors {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(malformedPath(name))
			if err != nil {
				t.Fatal(err)
			}
			pkg, err := Load(raw)
			if err != nil {
				return // refused before anything could be composed with it
			}
			ag, findings := Compose(pkg, counterparty, keys, now)
			if ag != nil {
				t.Fatal("composed a malformed package")
			}
			if len(findings) == 0 {
				t.Fatal("neither an agreement nor a refusal")
			}
			for _, f := range findings {
				if strings.HasPrefix(f.Code, "compose.") {
					t.Errorf("reported as a composition refusal (%s); a malformed package is refused before §6 begins and is not among its seven rules\n  %s", f.Code, f)
				}
				if !strings.HasPrefix(f.Code, "malformed.") {
					t.Errorf("finding %q is neither a malformed finding nor a composition refusal", f.Code)
				}
			}
		})
	}
}

// TestMalformedFilesAreUnsigned pins the property the whole kind rests on. If a
// signature were added to one, its refusal could become attributable to a failed
// verification rather than to the rule it is named for, and TestMalformedVectors
// would be setting aside a finding that no longer exists.
func TestMalformedFilesAreUnsigned(t *testing.T) {
	for name := range malformedVectors {
		raw, err := os.ReadFile(malformedPath(name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(`"signature"`)) {
			t.Errorf("%s carries a signature member; the kind is unsigned throughout by design", name)
		}
	}
}

// TestNonIntegerNumberSurvivesParsingAsItsOwnToken states what that file needs
// from a reader: the token as WRITTEN. A parser handing back a float64 has
// already lost the answer, and "the value is exactly 1380, so accept it" is a
// defensible reading of "integers only" that this rule exists to exclude.
func TestNonIntegerNumberSurvivesParsingAsItsOwnToken(t *testing.T) {
	raw, err := os.ReadFile(malformedPath("non-integer-number.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("1380.0")) {
		t.Fatal("the fixture no longer carries the token this test is about")
	}
	p, err := Load(raw)
	if err != nil {
		t.Fatalf("the file is well-formed JSON and must parse: %v", err)
	}
	n, ok := p.sub("transport_offer")["measured_mtu"].(json.Number)
	if !ok {
		t.Fatalf("measured_mtu did not arrive as a json.Number, so the token as written is unavailable")
	}
	if n.String() != "1380.0" {
		t.Errorf("the token arrived as %q, not %q", n.String(), "1380.0")
	}
	if _, err := p.CanonicalBytes(); err == nil {
		t.Error("the package canonicalised; RFC 8785 has no serialisation of this file")
	}
}
