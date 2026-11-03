// Package fedpkg implements the federation package specification, version 1.
//
// The specification is the contract; this is one implementation of it. Any
// other implementation that follows docs/design/federation-package-spec.md must
// produce byte-identical canonical output and therefore identical digests.
//
// Section references in this package are to that document.
package fedpkg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// The closed range every mainstream runtime represents exactly, ±(2^53 − 1)
// (§4). A larger integer loses precision in a runtime holding numbers as
// IEEE-754 doubles and re-serialises to different bytes there: a digest
// mismatch with no wrong value anywhere to find.
const (
	MaxSafeInteger = 9007199254740991
	MinSafeInteger = -9007199254740991
)

var (
	maxSafe = big.NewInt(MaxSafeInteger)
	minSafe = big.NewInt(MinSafeInteger)

	// A JSON integer already in its canonical decimal form. Anything else is
	// refused rather than repaired, because a canonicaliser that repairs input
	// is one two implementations can disagree about (§4).
	//
	// Note what this excludes besides the obvious. "01" and "+1" are not JSON
	// at all, and a lenient parser that accepts them hands on a value whose
	// canonical form is a different token. "-0" IS valid JSON, and JCS emits it
	// as "0" — so a package carrying it is not in canonical form, and an
	// implementation that passes the token through produces different bytes for
	// the same value. All three are refused, not normalised: normalising is the
	// repair this rule exists to forbid.
	integerRe = regexp.MustCompile(`^(?:0|-?[1-9][0-9]*)$`)
)

// Canonical serialises a decoded JSON value per RFC 8785 (JCS).
//
// Numbers are carried as json.Number and never through a float, which removes
// the single largest source of cross-implementation digest disagreement.
//
// The contract this function must never break: it returns either an error or
// bytes that parse as JSON. It never returns both a nil error and output that
// parses nowhere — output carrying a clean digest that no parser accepts is the
// worst failure this format can have, because both sides can agree on it (§4).
func Canonical(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := writeCanonical(&b, v, ""); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// path is the JSON pointer-ish location of v, so an error names where the
// problem is rather than only what it is.
func writeCanonical(b *bytes.Buffer, v any, path string) error {
	at := func() string {
		if path == "" {
			return "the top-level value"
		}
		return path
	}
	switch t := v.(type) {
	case nil:
		// A JSON null is written faithfully. §4 forbids nulls, but it forbids
		// them as a PACKAGE rule and an AGREEMENT rule, not as a serialisation
		// rule — and the difference is load-bearing. A package carrying a null
		// still has a canonical form and a signature over it, and a receiver
		// has to reproduce those bytes to check that signature before it can
		// say anything about the package at all. malformed/null-member.json is
		// exactly that fixture: byte-exact canonical form, valid signature,
		// refused by structural validation (see Package.structure).
		//
		// The hole this used to guard against is a member with NO value, which
		// is json.Number("") and is still refused below.
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		if err := writeJSONString(b, t, at()); err != nil {
			return err
		}
	case json.Number:
		s, err := canonicalInteger(t, at())
		if err != nil {
			return err
		}
		b.WriteString(s)
	case float64:
		return fmt.Errorf("%s is a float; decode with DecodeJSON so numbers arrive as json.Number (§4 permits integers only)", at())
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, e, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		// RFC 8785 orders members by their UTF-16 code units.
		sort.Slice(keys, func(i, j int) bool { return LessUTF16(keys[i], keys[j]) })
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			kp := k
			if path != "" {
				kp = path + "." + k
			}
			if err := writeJSONString(b, k, "the member name at "+kp); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := writeCanonical(b, t[k], kp); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("%s has type %T, which this canonical form cannot represent", at(), v)
	}
	return nil
}

// canonicalInteger returns the canonical decimal form of n, or an error.
//
// The empty json.Number is the specific defect this guards: it satisfies "not a
// float" and would otherwise be written as nothing at all, producing "mtu":,
// with a nil error and a clean digest.
func canonicalInteger(n json.Number, at string) (string, error) {
	s := n.String()
	if s == "" {
		return "", fmt.Errorf("%s carries a number with no value; there is no empty token (§4)", at)
	}
	if strings.ContainsAny(s, ".eE") {
		return "", fmt.Errorf("%s is %q: the specification permits integers only, in decimal (§4)", at, s)
	}
	if !integerRe.MatchString(s) {
		return "", fmt.Errorf("%s is %q, which is not a JSON integer in canonical decimal form (§4)", at, s)
	}
	i, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return "", fmt.Errorf("%s is %q, which is not a JSON integer (§4)", at, s)
	}
	if i.Cmp(maxSafe) > 0 || i.Cmp(minSafe) < 0 {
		return "", fmt.Errorf("%s is %s, outside the closed range ±(2^53−1) that every mainstream runtime represents exactly (§4)", at, s)
	}
	// Every token the regexp admits is already canonical, so this is identity.
	// It is written as the big.Int's own rendering rather than as the input so
	// that the value emitted is provably the value range-checked.
	return i.String(), nil
}

// LessUTF16 compares two strings by UTF-16 code unit, one code unit at a time,
// the shorter string first where one is a prefix of the other.
//
// This is the ONE comparison the specification uses (§4, "One ordering,
// everywhere"): object member names, set-array elements, and the party sort all
// use it. Reusing a single comparator is the point — an implementer must build
// it anyway for RFC 8785 member names, and a second comparator is one that can
// differ from the first in a way nothing tests.
//
// It is not Go's default. sort.Strings compares bytes, and byte-wise UTF-8
// order is code-point order, which differs from this on exactly one thing: a
// character above the basic multilingual plane encodes as a surrogate pair
// beginning D800-DBFF, so UTF-16 places U+20000 BEFORE U+FF21 where a
// code-point comparison places it after. compose-collation-utf16 is the fixture.
func LessUTF16(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

func writeJSONString(b *bytes.Buffer, s, at string) error {
	// JCS output is UTF-8. Ranging over invalid UTF-8 silently substitutes
	// U+FFFD, which would change the bytes and therefore the digest without
	// anything having reported a problem.
	if !utf8.ValidString(s) {
		return fmt.Errorf("%s is not valid UTF-8, and the canonical form is UTF-8 (§4)", at)
	}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return nil
}

// DecodeJSON parses one JSON object with numbers preserved exactly.
//
// It refuses a duplicate member name rather than resolving it. Go's decoder
// keeps the last occurrence and another implementation may keep the first; a
// package carrying two "expires" members would then compose into two different
// agreements, and each side would believe its own.
func DecodeJSON(data []byte) (map[string]any, error) {
	// §4: text is Unicode, and an escape denoting no character is an error.
	// This runs on the raw bytes because it cannot run anywhere else: Go's
	// decoder silently replaces an unpaired surrogate with U+FFFD, and both the
	// raw input and the decoded string are then valid UTF-8, so no check after
	// decoding can see what happened. Substitution is the worst available
	// behaviour — silent, and it changes the bytes and therefore the digest,
	// with nothing reported on either side and no field that differs. A decoder
	// that replaces invalid input has composed a document that was never signed.
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("the input is not valid UTF-8, and text in this format is Unicode (§4)")
	}
	if err := checkSurrogateEscapes(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing content after the top-level JSON value")
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("a package or an agreement is a JSON object, got %T", v)
	}
	return m, nil
}

// checkSurrogateEscapes refuses a \uXXXX escape that denotes no Unicode scalar
// value: a high surrogate not immediately followed by a low one, or a low
// surrogate standing alone. Such an escape is valid JSON syntax and has no
// UTF-8 encoding at all (§4).
//
// It walks the raw text tracking string context, because an escape only means
// anything inside a string, and a backslash may itself be escaped.
func checkSurrogateEscapes(data []byte) error {
	inString := false
	for i := 0; i < len(data); i++ {
		switch {
		case !inString:
			if data[i] == '"' {
				inString = true
			}
		case data[i] == '"':
			inString = false
		case data[i] == '\\':
			if i+1 >= len(data) {
				return fmt.Errorf("the input ends in an unterminated escape")
			}
			if data[i+1] != 'u' {
				i++ // an ordinary two-character escape
				continue
			}
			hi, ok := hex4(data, i+2)
			if !ok {
				return fmt.Errorf("a \\u escape at byte %d is not followed by four hex digits", i)
			}
			switch {
			case hi >= 0xD800 && hi <= 0xDBFF:
				lo, ok := 0, false
				if i+11 < len(data) && data[i+6] == '\\' && data[i+7] == 'u' {
					lo, ok = hex4(data, i+8)
				}
				if !ok || lo < 0xDC00 || lo > 0xDFFF {
					return fmt.Errorf("the escape \\u%04X at byte %d is a high surrogate with no low surrogate following; it denotes no character and has no UTF-8 encoding, and must never be replaced with U+FFFD (§4)", hi, i)
				}
				i += 11 // consume both escapes of the pair
			case hi >= 0xDC00 && hi <= 0xDFFF:
				return fmt.Errorf("the escape \\u%04X at byte %d is a low surrogate standing alone; it denotes no character and has no UTF-8 encoding, and must never be replaced with U+FFFD (§4)", hi, i)
			default:
				i += 5 // consume this escape
			}
		}
	}
	return nil
}

func hex4(data []byte, at int) (int, bool) {
	if at+4 > len(data) {
		return 0, false
	}
	v := 0
	for j := 0; j < 4; j++ {
		c := data[at+j]
		switch {
		case c >= '0' && c <= '9':
			v = v<<4 | int(c-'0')
		case c >= 'a' && c <= 'f':
			v = v<<4 | int(c-'a'+10)
		case c >= 'A' && c <= 'F':
			v = v<<4 | int(c-'A'+10)
		default:
			return 0, false
		}
	}
	return v, true
}

func decodeValue(dec *json.Decoder) (any, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	d, ok := t.(json.Delim)
	if !ok {
		// nil, bool, string or json.Number.
		return t, nil
	}
	switch d {
	case '{':
		m := map[string]any{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, err
			}
			k, ok := kt.(string)
			if !ok {
				return nil, fmt.Errorf("object member name is not a string")
			}
			if _, dup := m[k]; dup {
				return nil, fmt.Errorf("duplicate member %q: two implementations may resolve it differently, so it is refused rather than resolved", k)
			}
			v, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			m[k] = v
		}
		if _, err := dec.Token(); err != nil { // closing brace
			return nil, err
		}
		return m, nil
	case '[':
		a := []any{}
		for dec.More() {
			v, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		if _, err := dec.Token(); err != nil { // closing bracket
			return nil, err
		}
		return a, nil
	}
	return nil, fmt.Errorf("unexpected delimiter %v", d)
}
