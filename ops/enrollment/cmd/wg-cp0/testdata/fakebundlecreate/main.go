// Command fakebundlecreate is a test-only stand-in for the real
// bundle-create CLI. The wg-cp0 orchestrator's apply path (Block
// 22R.2 Session C) shells out to bundle-create via os/exec; the
// test suite needs to exercise that path end-to-end without the
// real binary's signing-material requirements.
//
// This binary parses the subset of flags the Session-C apply path
// passes and:
//
//   1. Writes a placeholder bundle.cbor at the --out path (so the
//      orchestrator's "bundle file landed" expectation holds).
//   2. Reads the --update-allowlist JSON-of-record, finds the
//      peer matching --device-id, and bumps its bundle_expiry by
//      --ttl-days * 24 h (so the orchestrator's post-apply
//      verification of the BundleExpiry field passes).
//   3. Logs every invocation to $GOAT_FAKE_BUNDLE_CREATE_LOG (if
//      set) so tests can assert on the per-call argv.
//
// The fake is built once per test run via `go build` and pointed
// at by the WG_CP0_BUNDLE_CREATE_BIN env var. It deliberately does
// NOT replicate bundle-create's cryptography — the orchestrator
// integration test is checking ITS contract with bundle-create, not
// bundle-create's internal correctness.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	site := flag.String("site", "", "")
	deviceID := flag.String("device-id", "", "")
	wgCP0Address := flag.String("wg-cp0-address", "", "")
	relaysPath := flag.String("wg-cp0-relays", "", "")
	aclGroups := flag.String("acl-groups", "", "")
	caID := flag.String("ca-id", "", "")
	mode := flag.String("mode", "dev", "")
	caKey := flag.String("ca-key", "", "")
	ageIdentity := flag.String("age-identity", "", "")
	tlsTrustAnchor := flag.String("tls-trust-anchor", "", "")
	updateAllowlist := flag.String("update-allowlist", "", "")
	outPath := flag.String("out", "", "")
	ttlDays := flag.Int("ttl-days", 90, "")
	reuseKeypairFrom := flag.String("reuse-keypair-from", "", "")

	flag.Parse()

	// Log the invocation if configured. Used by tests to assert the
	// orchestrator passed the expected flags.
	if logPath := os.Getenv("GOAT_FAKE_BUNDLE_CREATE_LOG"); logPath != "" {
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = io.WriteString(f, strings.Join(os.Args, " ")+"\n")
			_ = f.Close()
		}
	}

	// Required-flag enforcement — mirror enough of bundle-create's
	// validation that a malformed orchestrator call fails the test.
	if *site == "" || *deviceID == "" || *wgCP0Address == "" ||
		*relaysPath == "" || *aclGroups == "" || *caID == "" || *outPath == "" {
		_, _ = fmt.Fprintf(os.Stderr, "fakebundlecreate: missing required flag\n")
		os.Exit(2)
	}
	if *reuseKeypairFrom == "" {
		// Session C's contract: every metadata operation passes
		// --reuse-keypair-from. A missing flag is a test signal that
		// the orchestrator regressed.
		_, _ = fmt.Fprintf(os.Stderr, "fakebundlecreate: --reuse-keypair-from is required (Session-C contract)\n")
		os.Exit(2)
	}
	if *reuseKeypairFrom != *deviceID {
		_, _ = fmt.Fprintf(os.Stderr, "fakebundlecreate: --reuse-keypair-from (%s) must match --device-id (%s)\n",
			*reuseKeypairFrom, *deviceID)
		os.Exit(2)
	}
	if *mode == "dev" && (*caKey == "" || *ageIdentity == "") {
		_, _ = fmt.Fprintf(os.Stderr, "fakebundlecreate: dev mode needs --ca-key + --age-identity\n")
		os.Exit(2)
	}
	if _, err := os.Stat(*relaysPath); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fakebundlecreate: --wg-cp0-relays: %v\n", err)
		os.Exit(2)
	}

	_ = tlsTrustAnchor // accept the flag; not used by the fake

	// Write a placeholder bundle.cbor.
	if err := os.WriteFile(*outPath, []byte("FAKE-BUNDLE-"+*deviceID+"-ttl="+strconv.Itoa(*ttlDays)), 0o600); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fakebundlecreate: write %s: %v\n", *outPath, err)
		os.Exit(1)
	}

	// Update the allowlist's peer entry to reflect the new TTL.
	if *updateAllowlist != "" {
		if err := updatePeerExpiry(*updateAllowlist, *deviceID, time.Duration(*ttlDays)*24*time.Hour); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "fakebundlecreate: update allowlist: %v\n", err)
			os.Exit(1)
		}
	}

	_, _ = fmt.Fprintf(os.Stderr, "fakebundlecreate: ok (device=%s ttl_days=%d out=%s)\n", *deviceID, *ttlDays, *outPath)
}

// updatePeerExpiry advances the named peer's bundle_expiry to now +
// extend. Matches the orchestrator's post-apply verification window
// (within 24 h of the requested horizon).
func updatePeerExpiry(path, deviceID string, extend time.Duration) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	peersAny, ok := raw["peers"].([]any)
	if !ok {
		return fmt.Errorf("peers[] missing or wrong shape")
	}
	now := time.Now().UTC()
	newExpiry := now.Add(extend).Format(time.RFC3339)
	found := false
	for i, pAny := range peersAny {
		p, ok := pAny.(map[string]any)
		if !ok {
			continue
		}
		if p["name"] == deviceID {
			p["bundle_expiry"] = newExpiry
			peersAny[i] = p
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("peer %q not in allowlist", deviceID)
	}
	raw["peers"] = peersAny
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}
