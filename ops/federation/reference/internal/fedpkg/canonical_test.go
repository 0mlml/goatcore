package fedpkg

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
)

// TestCanonicalRefusesTheEmptyNumber is the regression for the defect this
// canonicaliser was rebuilt around. An empty json.Number satisfies "not a
// float" and was written as nothing at all, producing "mtu":, — bytes that
// parse nowhere, carrying a clean digest and a nil error. Both sides can agree
// on that, which makes it the worst failure this format can have (§4).
func TestCanonicalRefusesTheEmptyNumber(t *testing.T) {
	b, err := Canonical(map[string]any{"mtu": json.Number("")})
	if err == nil {
		t.Fatalf("no error, and it produced %q", b)
	}
	if b != nil {
		t.Errorf("output alongside an error: %q", b)
	}
	if !strings.Contains(err.Error(), "mtu") {
		t.Errorf("the error does not name the member at fault: %v", err)
	}
}

// TestCanonicalWritesNullFaithfully pins where §4's no-null rule lives. It is a
// package rule and an agreement rule, not a serialisation rule: a package
// carrying a null still has a canonical form and a signature over it, and a
// receiver must reproduce those bytes to check that signature before it can say
// anything about the package. The refusal happens in structural validation —
// see TestNullIsRefusedStructurally — and malformed/null-member.json is the fixture.
func TestCanonicalWritesNullFaithfully(t *testing.T) {
	b, err := Canonical(map[string]any{"measured_mtu": nil})
	if err != nil {
		t.Fatalf("a package carrying a null could not be canonicalised, so its signature could never be checked: %v", err)
	}
	if string(b) != `{"measured_mtu":null}` {
		t.Errorf("got %s", b)
	}
}

func TestCanonicalIntegerBounds(t *testing.T) {
	cases := []struct {
		in   string
		want string // empty means the value must be refused
	}{
		{"0", "0"},
		{"-0", ""}, // valid JSON, but JCS emits this value as 0, so the token is refused rather than normalised
		{"1380", "1380"},
		{"-1", "-1"},
		{"9007199254740991", "9007199254740991"}, // 2^53 − 1
		{"-9007199254740991", "-9007199254740991"}, // −(2^53 − 1)
		{"9007199254740992", ""},                   // one past the range every runtime represents exactly
		{"-9007199254740992", ""},
		{"1.0", ""}, // no floating-point numbers anywhere
		{"1e3", ""},
		{"1E3", ""},
		{"01", ""}, // not JSON at all; a lenient parser hands on a value whose canonical form is a different token
		{"+1", ""},
		{"-01", ""},
		{"", ""},
		{" 1", ""},
		{"0x10", ""},
		{"NaN", ""},
		{"Infinity", ""},
	}
	for _, c := range cases {
		got, err := Canonical(map[string]any{"n": json.Number(c.in)})
		if c.want == "" {
			if err == nil {
				t.Errorf("json.Number(%q) was accepted and produced %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("json.Number(%q): %v", c.in, err)
			continue
		}
		if want := `{"n":` + c.want + `}`; string(got) != want {
			t.Errorf("json.Number(%q) produced %s, want %s", c.in, got, want)
		}
	}
}

func TestCanonicalRefusesAFloat(t *testing.T) {
	if b, err := Canonical(map[string]any{"hz": 100.0}); err == nil {
		t.Fatalf("a float64 was accepted: %q", b)
	}
}

func TestCanonicalRefusesInvalidUTF8(t *testing.T) {
	bad := string([]byte{0xff, 0xfe})
	if b, err := Canonical(map[string]any{"k": bad}); err == nil {
		t.Errorf("invalid UTF-8 in a value was accepted: %q", b)
	}
	if b, err := Canonical(map[string]any{bad: "v"}); err == nil {
		t.Errorf("invalid UTF-8 in a member name was accepted: %q", b)
	}
}

// TestCanonicalOrdersByUTF16CodeUnit pins the one case where RFC 8785's
// ordering differs from a plain byte comparison. U+10000 sorts above U+FFFD by
// UTF-8 bytes and below it by UTF-16 code unit, because it encodes as the
// surrogate pair D800 DC00 and D800 is below FFFD.
func TestCanonicalOrdersByUTF16CodeUnit(t *testing.T) {
	hi := "\U00010000"
	lo := "�"
	if !(hi > lo) {
		t.Fatalf("the fixture assumes %q sorts above %q by bytes", hi, lo)
	}
	got, err := Canonical(map[string]any{hi: json.Number("1"), lo: json.Number("2")})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"` + hi + `":1,"` + lo + `":2}`
	if string(got) != want {
		t.Errorf("member order\n got %q\nwant %q", got, want)
	}
}

func TestCanonicalEscaping(t *testing.T) {
	in := "\"\\\b\f\n\r\t\u0000\u001f\u007f \u00e9"
	got, err := Canonical(map[string]any{"k": in})
	if err != nil {
		t.Fatal(err)
	}
	// The short forms where they exist, \u00xx in lowercase hex otherwise, and
	// no escaping at all above U+001F: the canonical form is UTF-8.
	want := `{"k":"\"\\\b\f\n\r\t\u0000\u001f` + "\u007f \u00e9" + `"}`
	if string(got) != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
}

// TestCanonicalIsDeterministic guards against a member order that follows Go's
// randomised map iteration rather than the sort.
func TestCanonicalIsDeterministic(t *testing.T) {
	m := map[string]any{}
	for _, k := range []string{"a", "z", "m", "B", "0", "aa", "A", "_", "é", "ß"} {
		m[k] = json.Number("1")
	}
	first, err := Canonical(m)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		again, err := Canonical(m)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("run %d differs\n %s\n %s", i, first, again)
		}
	}
}

func TestDecodeJSONRefusesDuplicateMembers(t *testing.T) {
	if _, err := DecodeJSON([]byte(`{"expires":"2026-11-20","expires":"2036-11-20"}`)); err == nil {
		t.Error("a duplicate member was resolved rather than refused; the two parties would compose two different agreements and each would believe its own")
	}
	if _, err := DecodeJSON([]byte(`{"a":{"b":1,"b":2}}`)); err == nil {
		t.Error("a duplicate member nested in an object was accepted")
	}
	if _, err := DecodeJSON([]byte(`{"a":1} {"b":2}`)); err == nil {
		t.Error("trailing content after the top-level value was accepted")
	}
	if _, err := DecodeJSON([]byte(`[1,2]`)); err == nil {
		t.Error("a top-level array was accepted as a package")
	}
}

// numberPool holds every shape of number token that has to be refused, so the
// property test spends its budget on the boundary rather than on random digits.
var numberPool = []string{
	"", " ", "1", "0", "-0", "-1", "01", "+1", "1.0", "1e3", "1E3", "0x10",
	"1_000", "-", ".", "NaN", "Infinity", "-Infinity", "1 ", "\t2",
	"9007199254740991", "9007199254740992", "-9007199254740991",
	"-9007199254740992", "99999999999999999999999999", "١٢",
	"1,000", "--1", "0.0000000000000000000001", "1e-3", "2e308",
}

var stringPool = []string{
	"", "a", "release/weather/**", "é", "\U0001F600", "\U00010000", "�",
	"line\nbreak", "quote\"", "back\\slash", "\u0000\u0001\u001f", "  ",
	string([]byte{0xff, 0xfe}), string([]byte{0xc3, 0x28}),
}

var keyPool = []string{
	"a", "b", "z", "A", "Z", "0", "_", "é", "\U00010000", "�", "",
	"mtu", "expires", "spec", string([]byte{0xff}),
}

func randLeaf(r *rand.Rand) any {
	switch r.Intn(6) {
	case 0:
		return nil
	case 1:
		return r.Intn(2) == 0
	case 2:
		return stringPool[r.Intn(len(stringPool))]
	case 3:
		return json.Number(numberPool[r.Intn(len(numberPool))])
	case 4:
		// A float never reaches the canonicaliser through DecodeJSON, but a
		// caller building a value by hand can hand it one.
		return float64(r.Intn(1000)) / 7
	default:
		return json.Number(strings.Repeat("9", 1+r.Intn(20)))
	}
}

func randValue(r *rand.Rand, depth int) any {
	if depth <= 0 || r.Intn(100) < 50 {
		return randLeaf(r)
	}
	if r.Intn(2) == 0 {
		a := make([]any, r.Intn(4))
		for i := range a {
			a[i] = randValue(r, depth-1)
		}
		return a
	}
	m := map[string]any{}
	for i := r.Intn(5); i > 0; i-- {
		m[keyPool[r.Intn(len(keyPool))]] = randValue(r, depth-1)
	}
	return m
}

// TestCanonicalNeverLies is the property the specification asks for by name: a
// canonicaliser must never return both a nil error and bytes that parse
// nowhere. Output that carries a clean digest and satisfies no parser is the
// one failure both parties can agree on (§4).
func TestCanonicalNeverLies(t *testing.T) {
	r := rand.New(rand.NewSource(20260905))
	errors, outputs := 0, 0
	for i := 0; i < 50000; i++ {
		v := randValue(r, 4)
		out, err := Canonical(v)
		if err != nil {
			errors++
			if out != nil {
				t.Fatalf("case %d: an error and %d bytes of output: %q", i, len(out), out)
			}
			continue
		}
		outputs++
		if !json.Valid(out) {
			t.Fatalf("case %d: nil error and output that is not JSON: %q\nfrom %#v", i, out, v)
		}
		// Canonicalising canonical bytes must be a no-op, or two conforming
		// implementations that round-trip a different number of times disagree.
		var back any
		dec := json.NewDecoder(bytes.NewReader(out))
		dec.UseNumber()
		if err := dec.Decode(&back); err != nil {
			t.Fatalf("case %d: output does not decode: %v (%q)", i, err, out)
		}
		again, err := Canonical(back)
		if err != nil {
			t.Fatalf("case %d: canonical output is not itself canonicalisable: %v (%q)", i, err, out)
		}
		if !bytes.Equal(out, again) {
			t.Fatalf("case %d: canonicalisation is not idempotent\n once %q\ntwice %q", i, out, again)
		}
	}
	// A generator that only ever produced refusals would satisfy the property
	// while testing nothing.
	if outputs < 1000 || errors < 1000 {
		t.Fatalf("the generator is one-sided: %d produced output, %d refused", outputs, errors)
	}
}

// TestDecodeJSONRefusesUnpairedSurrogates covers the rule no check after
// decoding can enforce. Go's decoder silently replaces an unpaired surrogate
// with U+FFFD, and the raw input and the decoded string are then both valid
// UTF-8 — so the substitution is invisible downstream. It changes the bytes and
// therefore the digest, with nothing reported and no field that differs: a
// decoder that replaces invalid input has composed a document that was never
// signed (§4).
func TestDecodeJSONRefusesUnpairedSurrogates(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool // true = must be refused
	}{
		{"lone high surrogate", `{"a":"x\ud800"}`, true},
		{"lone low surrogate", `{"a":"x\udc00"}`, true},
		{"high surrogate followed by a plain escape", `{"a":"\ud800\n"}`, true},
		{"high surrogate followed by an ordinary character", `{"a":"\ud800Z"}`, true},
		{"high surrogate at the very end", `{"a":"\ud800"}`, true},
		{"a valid pair", `{"a":"\ud840\udc00"}`, false},
		{"a valid pair mid-string", `{"a":"x\ud840\udc00y"}`, false},
		{"an escape that is not a surrogate", `{"a":"\u00e9"}`, false},
		{"an escaped backslash before a u", `{"a":"\\u0041"}`, false},
		{"a literal supplementary character", `{"a":"𐀀"}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeJSON([]byte(c.in))
			if c.want && err == nil {
				t.Errorf("accepted %s", c.in)
			}
			if !c.want && err != nil {
				t.Errorf("refused %s: %v", c.in, err)
			}
		})
	}
}

func TestDecodeJSONRefusesInvalidUTF8Input(t *testing.T) {
	bad := append([]byte(`{"a":"`), 0xff, 0xfe, '"', '}')
	if _, err := DecodeJSON(bad); err == nil {
		t.Error("accepted input that is not valid UTF-8")
	}
}
