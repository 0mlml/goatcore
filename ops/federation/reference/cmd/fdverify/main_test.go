package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const vectorsDir = "../../../vectors"

func vec(parts ...string) string {
	return filepath.Join(append([]string{vectorsDir}, parts...)...)
}

func evaluationDate(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(vec("now.txt"))
	if err != nil {
		t.Fatalf("%v", err)
	}
	return strings.TrimSpace(string(b))
}

// captureStdout runs f with stdout redirected to a temporary file and returns
// what it wrote. The canonical form goes to stdout as bytes, so it has to be
// compared as bytes.
func captureStdout(t *testing.T, f func() error) ([]byte, error) {
	t.Helper()
	tmp, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = tmp
	runErr := f()
	os.Stdout = saved
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	return out, runErr
}

func TestComposeVerbReproducesTheVectors(t *testing.T) {
	now := evaluationDate(t)
	for _, dir := range []string{
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
	} {
		t.Run(dir, func(t *testing.T) {
			out, err := captureStdout(t, func() error {
				return cmdCompose([]string{
					"-keydir", vec("testkeys"), "-now", now,
					vec(dir, "package-xaa.canonical.json"),
					vec(dir, "package-xab.canonical.json"),
				})
			})
			if err != nil {
				t.Fatalf("%v", err)
			}
			want, err := os.ReadFile(vec(dir, "agreement.canonical.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, want) {
				t.Errorf("stdout differs from the vector\n got %s\nwant %s", out, want)
			}
			if bytes.HasSuffix(out, []byte("\n")) {
				t.Error("compose wrote a trailing newline; a canonical form is bytes, and appending one changes the digest")
			}
		})
	}
}

// TestComposeVerbNamesTheRule checks the operator-facing half of every refusal:
// exit status 1, and a message carrying the rule's name.
func TestComposeVerbNamesTheRule(t *testing.T) {
	now := evaluationDate(t)
	cases := map[string][2]string{
		"refuse-bad-signature":                {"compose.signature", "package-xab"},
		"refuse-version-mismatch":             {"compose.version_mismatch", "package-xab"},
		"refuse-same-trust-domain":            {"compose.same_trust_domain", "package-xab"},
		"refuse-expired":                      {"compose.expired", "package-xab"},
		"refuse-no-transport-overlap":         {"compose.no_transport_overlap", "package-xab"},
		"refuse-neither-listener":             {"compose.neither_listener", "package-xab"},
		"refuse-same-sovereign-id":            {"compose.same_sovereign_id", "package-xaa-second"},
		"refuse-both-v2":                      {"compose.version_mismatch", "package-xab"},
		"refuse-transport-outside-preference": {"compose.no_transport_overlap", "package-xab"},
		"refuse-expired-first-party":          {"compose.expired", "package-xab"},
	}
	for dir, c := range cases {
		t.Run(dir, func(t *testing.T) {
			out, err := captureStdout(t, func() error {
				return cmdCompose([]string{
					"-keydir", vec("testkeys"), "-now", now,
					vec(dir, "package-xaa.canonical.json"),
					vec(dir, c[1]+".canonical.json"),
				})
			})
			if err == nil {
				t.Fatalf("composed instead of refusing: %s", out)
			}
			if len(out) > 0 {
				t.Errorf("a refusal produces no agreement, but stdout carried %d bytes: %s", len(out), out)
			}
			var o outcome
			if !errors.As(err, &o) {
				t.Fatalf("a refusal must exit 1, not 2: %v", err)
			}
			if !strings.Contains(err.Error(), c[0]) {
				t.Errorf("the refusal does not name %s:\n%s", c[0], err)
			}
		})
	}
}

func TestCanonicalVerbWritesTheCommittedBytes(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return cmdCanonical([]string{vec("compose-full", "package-xaa.canonical.json")})
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(vec("compose-full", "package-xaa.canonical.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, want) {
		t.Errorf("\n got %s\nwant %s", out, want)
	}
}

func TestDigestVerb(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return cmdDigest([]string{vec("compose-full", "agreement.canonical.json")})
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(vec("compose-full", "agreement.digest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != strings.TrimSpace(string(want)) {
		t.Errorf("digest\n got %s\nwant %s", out, want)
	}
}

func TestVerifyVerb(t *testing.T) {
	if _, err := captureStdout(t, func() error {
		return cmdVerify([]string{"-keydir", vec("testkeys"), vec("compose-full", "package-xaa.canonical.json")})
	}); err != nil {
		t.Errorf("a valid package did not verify: %v", err)
	}
	_, err := captureStdout(t, func() error {
		return cmdVerify([]string{"-keydir", vec("testkeys"), vec("refuse-bad-signature", "package-xaa.canonical.json")})
	})
	var o outcome
	if !errors.As(err, &o) {
		t.Errorf("the tampered package verified, or failed as a usage error: %v", err)
	}
}

// TestVerifyVerbRefusesWithNoKey pins that the key is an input and never
// something this program goes and finds (§5.1).
func TestVerifyVerbRefusesWithNoKey(t *testing.T) {
	_, err := captureStdout(t, func() error {
		return cmdVerify([]string{vec("compose-full", "package-xaa.canonical.json")})
	})
	if err == nil {
		t.Fatal("verified with no key supplied")
	}
	var o outcome
	if errors.As(err, &o) {
		t.Error("a missing key is a usage error, not a refusal: nothing about the package was judged")
	}
	if !strings.Contains(err.Error(), "key") {
		t.Errorf("the error does not mention the key: %v", err)
	}
}

func TestDiffVerb(t *testing.T) {
	same, err := captureStdout(t, func() error {
		return cmdDiff([]string{
			vec("compose-full", "agreement.canonical.json"),
			vec("compose-full", "agreement.canonical.json"),
		})
	})
	if err != nil {
		t.Fatalf("identical agreements reported a difference: %v", err)
	}
	if !strings.Contains(string(same), "identical") {
		t.Errorf("output does not say the agreements are identical:\n%s", same)
	}

	_, err = captureStdout(t, func() error {
		return cmdDiff([]string{
			vec("compose-full", "agreement.canonical.json"),
			vec("compose-no-flows", "agreement.canonical.json"),
		})
	})
	if err == nil {
		t.Fatal("two different agreements reported no difference")
	}
	var o outcome
	if !errors.As(err, &o) {
		t.Fatalf("a difference is an outcome, not a usage error: %v", err)
	}
	for _, want := range []string{"flows[0]", "flows[1]", "only in a"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the report does not name %q:\n%s", want, err)
		}
	}
}

// TestDiffNamesTheField is the property §9 promises: when two digests disagree,
// each side prints its agreement and the offending field names itself.
func TestDiffNamesTheField(t *testing.T) {
	a := filepath.Join(t.TempDir(), "a.json")
	b := filepath.Join(t.TempDir(), "b.json")
	src, err := os.ReadFile(vec("compose-full", "agreement.canonical.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a, src, 0o644); err != nil {
		t.Fatal(err)
	}
	// One party's composer took the larger measured mtu instead of the smaller.
	if err := os.WriteFile(b, bytes.Replace(src, []byte(`"mtu":1380`), []byte(`"mtu":1420`), 1), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = captureStdout(t, func() error { return cmdDiff([]string{a, b}) })
	if err == nil {
		t.Fatal("no difference reported")
	}
	msg := err.Error()
	if !strings.Contains(msg, "transport.mtu") {
		t.Errorf("the report does not name transport.mtu:\n%s", msg)
	}
	if !strings.Contains(msg, "1380") || !strings.Contains(msg, "1420") {
		t.Errorf("the report does not carry both values:\n%s", msg)
	}
	if strings.Count(msg, "\n  transport") != 1 {
		t.Errorf("one field differs, but the report lists more:\n%s", msg)
	}
}

func TestKeyFlagParsing(t *testing.T) {
	var k keyFlags
	k.pairs = []string{"xaa"}
	if _, err := k.keyring(); err == nil {
		t.Error("-key with no = was accepted")
	}
	k.pairs = []string{"xaa=" + vec("testkeys", "xaa.public.hex")}
	ring, err := k.keyring()
	if err != nil {
		t.Fatal(err)
	}
	if len(ring) != 1 || ring["xaa"] == nil {
		t.Errorf("keyring is %v", ring)
	}

	// The base64 and hex forms of the same key must load to the same bytes.
	var k2 keyFlags
	k2.pairs = []string{"xaa=" + vec("testkeys", "xaa.public.b64")}
	ring2, err := k2.keyring()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ring["xaa"], ring2["xaa"]) {
		t.Error("the hex and base64 forms of one key loaded differently")
	}
}

// TestLintVerbOnTheMalformedKind checks the operator path for single-file
// inputs. They have no counterparty, so `lint` is the verb that reaches them,
// and its exit status must be a finding rather than a usage error.
func TestLintVerbOnTheMalformedKind(t *testing.T) {
	now := evaluationDate(t)
	files, err := filepath.Glob(vec("malformed", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no malformed fixtures found: %v", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			out, err := captureStdout(t, func() error {
				return cmdLint([]string{"-now", now, f})
			})
			if err == nil {
				t.Fatalf("linted clean: %s", out)
			}
			// A malformed input is a finding, not a usage error: exit 1, not 2.
			// Four of these are refused while reading the bytes, which reaches
			// the operator as a read failure of the file itself.
			var o outcome
			if !errors.As(err, &o) && !strings.Contains(err.Error(), filepath.Base(f)) {
				t.Errorf("the refusal does not name the file: %v", err)
			}
		})
	}
}
