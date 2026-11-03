package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestGCERoundTrip drives the subcommand functions in-process (they return errors
// rather than calling os.Exit — only main does that) so CI covers the crypto CLI
// without the Zenoh/Python transport harness. It mirrors the on-fabric proof:
// mint two members, seal to one, then confirm the member opens and every
// unauthorized path is denied.
func TestGCERoundTrip(t *testing.T) {
	dir := t.TempDir()
	const (
		rs      = "coalition-air"
		gen     = "7"
		keyexpr = "data/coalition-air/cop"
	)
	plaintext := []byte("brigade COP snapshot, releasable to REL-EXAMPLE only")

	mustRun(t, genMember, "member", dir)
	mustRun(t, genMember, "outsider", dir)

	ptPath := filepath.Join(dir, "pt.txt")
	if err := os.WriteFile(ptPath, plaintext, 0o644); err != nil {
		t.Fatal(err)
	}

	sealedPath := filepath.Join(dir, "sealed.json")
	mustRun(t, seal,
		"--rs", rs, "--gen", gen, "--keyexpr", keyexpr,
		"--in", ptPath, "--member", "member="+filepath.Join(dir, "member.crt.pem"),
		"--out", sealedPath)

	// Split the sealed artifact the way the transport harness does (bundle and
	// envelope travel on separate Zenoh topics).
	bundlePath, envPath := split(t, sealedPath, dir)

	openArgs := func(id, keyName, ke, outName string) []string {
		return []string{
			"--bundle", bundlePath, "--envelope", envPath,
			"--id", id, "--key", filepath.Join(dir, keyName),
			"--keyexpr", ke, "--out", filepath.Join(dir, outName),
		}
	}

	// 1. The member opens it.
	if err := open(openArgs("member", "member.key.pem", keyexpr, "out.txt")); err != nil {
		t.Fatalf("member open: unexpected error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("recovered plaintext = %q, want %q", got, plaintext)
	}

	// 2. An outsider (no slot in the bundle) is denied.
	if err := open(openArgs("outsider", "outsider.key.pem", keyexpr, "no.txt")); !errors.Is(err, errDenied) {
		t.Fatalf("outsider open: want errDenied, got %v", err)
	}

	// 3. The right member on the wrong topic is denied (keyexpr is AAD-bound).
	if err := open(openArgs("member", "member.key.pem", "data/other/topic", "no.txt")); !errors.Is(err, errDenied) {
		t.Fatalf("wrong-keyexpr open: want errDenied, got %v", err)
	}

	// 4. The right member with a re-labeled generation is denied (gen is authenticated).
	envWrongGen := reGen(t, envPath, dir, 8)
	wrongGenArgs := openArgs("member", "member.key.pem", keyexpr, "no.txt")
	wrongGenArgs[3] = envWrongGen // replace --envelope value
	if err := open(wrongGenArgs); !errors.Is(err, errDenied) {
		t.Fatalf("wrong-generation open: want errDenied, got %v", err)
	}
}

func mustRun(t *testing.T, fn func([]string) error, args ...string) {
	t.Helper()
	if err := fn(args); err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
}

// split writes the sealed artifact's envelope and bundle to separate files,
// exactly as the transport harness sends them on separate topics.
func split(t *testing.T, sealedPath, dir string) (bundlePath, envPath string) {
	t.Helper()
	b, err := os.ReadFile(sealedPath)
	if err != nil {
		t.Fatal(err)
	}
	var s sealed
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	bundlePath = filepath.Join(dir, "bundle.json")
	envPath = filepath.Join(dir, "env.json")
	writeJSON(t, bundlePath, s.Bundle)
	writeJSON(t, envPath, s.Envelope)
	return bundlePath, envPath
}

// reGen writes a copy of the envelope with a tampered generation number.
func reGen(t *testing.T, envPath, dir string, gen int) string {
	t.Helper()
	b, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["gen"] = gen
	out := filepath.Join(dir, "env-wronggen.json")
	writeJSON(t, out, m)
	return out
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
