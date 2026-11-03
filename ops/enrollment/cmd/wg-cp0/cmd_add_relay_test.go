package main

// cmd_add_relay_test.go — coverage for the `wg-cp0 add-relay`
// subcommand. The add-relay flow is pure peerset mutation (no
// bundle-create invocation) which keeps the unit-test surface small
// and full-flow happy paths cheap.

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

// validPubkey returns a base64-encoded 32-byte zero pubkey, the
// shortest valid Curve25519 wire form for fixtures that need a
// well-shaped relay entry.
func validPubkey() string {
	return base64.StdEncoding.EncodeToString(make([]byte, 32))
}

// otherPubkey returns a second distinct valid pubkey (last byte set
// to 1 instead of 0) so duplicate-pubkey collision can be exercised.
func otherPubkey() string {
	raw := make([]byte, 32)
	raw[0] = 1
	return base64.StdEncoding.EncodeToString(raw)
}

// writeV2Fixture writes an empty v2 envelope to the given path; used
// as the seed state for tests that exercise add-relay against a clean
// JSON-of-record.
func writeV2Fixture(t *testing.T, path string, ps peerset.PeerSet) {
	t.Helper()
	if ps.SchemaVersion == 0 {
		ps.SchemaVersion = 2
	}
	if ps.Metadata.SchemaMinVersion == 0 {
		ps.Metadata.SchemaMinVersion = 2
	}
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCmdAddRelay_RequiresName(t *testing.T) {
	code, _, stderr := runArgs(t, "add-relay")
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "exactly one positional") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestCmdAddRelay_RequiresFlags(t *testing.T) {
	code, _, stderr := runArgs(t, "add-relay", "relay-rld")
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "--endpoint") || !strings.Contains(stderr, "--pubkey") || !strings.Contains(stderr, "--address") {
		t.Errorf("stderr should name every missing required flag: %q", stderr)
	}
}

func TestCmdAddRelay_RejectsBadPubkey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allow.json")
	writeV2Fixture(t, path, peerset.PeerSet{})
	code, _, stderr := runArgs(t, "add-relay", "relay-rld",
		"--endpoint", "1.2.3.4:51821",
		"--pubkey", "garbage",
		"--address", "198.18.0.4",
		"--path", path)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "--pubkey") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestCmdAddRelay_DryRunPreviewsAdd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allow.json")
	writeV2Fixture(t, path, peerset.PeerSet{})
	code, stdout, _ := runArgs(t, "add-relay", "relay-rld",
		"--endpoint", "relay-rld.netbird-prod.goat.example:51821",
		"--pubkey", validPubkey(),
		"--address", "198.18.0.4",
		"--site", "sjc",
		"--tier", "relay_servers",
		"--path", path)
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	for _, w := range []string{"PLAN — wg-cp0 add-relay relay-rld", "+ ADD", "relays[]: relay-rld",
		"endpoint=relay-rld.netbird-prod.goat.example:51821", "address=198.18.0.4", "Re-run with --apply"} {
		if !strings.Contains(stdout, w) {
			t.Errorf("stdout missing %q\nfull:\n%s", w, stdout)
		}
	}
	// State file must not have been mutated.
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "relay-rld") {
		t.Errorf("dry-run should not write to state file:\n%s", data)
	}
}

func TestCmdAddRelay_ApplyAddsRelay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allow.json")
	writeV2Fixture(t, path, peerset.PeerSet{})
	code, stdout, stderr := runArgs(t, "add-relay", "relay-rld",
		"--endpoint", "relay-rld.netbird-prod.goat.example:51821",
		"--pubkey", validPubkey(),
		"--address", "198.18.0.4",
		"--site", "sjc",
		"--tier", "relay_servers",
		"--path", path,
		"--apply")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Applied: add-relay relay-rld (added)") {
		t.Errorf("apply-completed line missing: %q", stdout)
	}
	if !strings.Contains(stdout, "wg-cp0 update-endpoints --all-peers --add-relay relay-rld") {
		t.Errorf("follow-up hint missing: %q", stdout)
	}
	// State file must reflect the add.
	ps, err := peerset.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := ps.Relay("relay-rld")
	if !ok {
		t.Fatal("relay-rld not in peerset after apply")
	}
	if r.Endpoint != "relay-rld.netbird-prod.goat.example:51821" {
		t.Errorf("endpoint = %q", r.Endpoint)
	}
}

func TestCmdAddRelay_IdempotentOnIdenticalReRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allow.json")
	writeV2Fixture(t, path, peerset.PeerSet{
		Relays: []peerset.Relay{{
			Name:          "relay-rld",
			Endpoint:      "x:51821",
			MeshAddress:   "198.18.0.4",
			CurrentPubkey: validPubkey(),
			Site:          "sjc",
			Tier:          "relay_servers",
		}},
	})
	code, stdout, _ := runArgs(t, "add-relay", "relay-rld",
		"--endpoint", "x:51821",
		"--pubkey", validPubkey(),
		"--address", "198.18.0.4",
		"--site", "sjc",
		"--tier", "relay_servers",
		"--path", path,
		"--apply")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stdout, "= KEEP") || !strings.Contains(stdout, "No changes required") {
		t.Errorf("expected no-op output: %q", stdout)
	}
}

func TestCmdAddRelay_RefusesOverwriteWithoutReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allow.json")
	writeV2Fixture(t, path, peerset.PeerSet{
		Relays: []peerset.Relay{{
			Name:          "relay-rld",
			Endpoint:      "old:51821",
			MeshAddress:   "198.18.0.4",
			CurrentPubkey: validPubkey(),
		}},
	})
	code, _, stderr := runArgs(t, "add-relay", "relay-rld",
		"--endpoint", "new:51821",
		"--pubkey", validPubkey(),
		"--address", "198.18.0.4",
		"--path", path,
		"--apply")
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "--replace") {
		t.Errorf("stderr should suggest --replace: %q", stderr)
	}
}

func TestCmdAddRelay_ReplaceOverridesByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allow.json")
	writeV2Fixture(t, path, peerset.PeerSet{
		Relays: []peerset.Relay{{
			Name:          "relay-rld",
			Endpoint:      "old:51821",
			MeshAddress:   "198.18.0.4",
			CurrentPubkey: validPubkey(),
		}},
	})
	code, stdout, stderr := runArgs(t, "add-relay", "relay-rld",
		"--endpoint", "new.example.com:51821",
		"--pubkey", otherPubkey(),
		"--address", "198.18.0.4",
		"--site", "sjc",
		"--path", path,
		"--apply", "--replace")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "~ UPDATE") {
		t.Errorf("stdout missing UPDATE glyph: %q", stdout)
	}
	if !strings.Contains(stdout, "endpoint: old:51821 → new.example.com:51821") {
		t.Errorf("stdout missing field diff: %q", stdout)
	}
	ps, _ := peerset.Load(path)
	r, _ := ps.Relay("relay-rld")
	if r.Endpoint != "new.example.com:51821" {
		t.Errorf("relay not updated; endpoint = %q", r.Endpoint)
	}
}

func TestCmdAddRelay_UnknownFlag(t *testing.T) {
	code, _, stderr := runArgs(t, "add-relay", "relay-rld", "--bogus", "x")
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "unknown flag") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestParseFlagsAddRelay_EqualsSyntax(t *testing.T) {
	accept := map[string]bool{"endpoint": true, "pubkey": true, "address": true, "apply": true}
	f, pos, err := parseFlagsAddRelay(
		[]string{"relay-rld", "--endpoint=x:1", "--pubkey=" + validPubkey(), "--address=198.18.0.4", "--apply"},
		accept,
	)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pos) != 1 || pos[0] != "relay-rld" {
		t.Errorf("pos = %v", pos)
	}
	if f.endpoint != "x:1" || !f.apply {
		t.Errorf("got %+v", f)
	}
}

func TestParseFlagsAddRelay_FlagAtEnd(t *testing.T) {
	accept := map[string]bool{"endpoint": true}
	if _, _, err := parseFlagsAddRelay([]string{"--endpoint"}, accept); err == nil {
		t.Errorf("expected error for value-less terminal flag")
	}
}

func TestClassifyRelayAdd_AllOps(t *testing.T) {
	ps := &peerset.PeerSet{Relays: []peerset.Relay{
		{Name: "existing", Endpoint: "a:1", MeshAddress: "198.18.0.2", CurrentPubkey: validPubkey()},
	}}
	// PlanAdd: new name.
	op, prior, err := classifyRelayAdd(ps, peerset.Relay{Name: "fresh", Endpoint: "b:1", MeshAddress: "198.18.0.3", CurrentPubkey: otherPubkey()}, false)
	if err != nil || op != PlanAdd || prior != nil {
		t.Errorf("PlanAdd path: op=%v prior=%v err=%v", op, prior, err)
	}
	// PlanKeep: identical fields.
	op, prior, err = classifyRelayAdd(ps, peerset.Relay{Name: "existing", Endpoint: "a:1", MeshAddress: "198.18.0.2", CurrentPubkey: validPubkey()}, false)
	if err != nil || op != PlanKeep || prior == nil {
		t.Errorf("PlanKeep path: op=%v err=%v", op, err)
	}
	// PlanUpdate refused without --replace.
	op, _, err = classifyRelayAdd(ps, peerset.Relay{Name: "existing", Endpoint: "different:1", MeshAddress: "198.18.0.2", CurrentPubkey: validPubkey()}, false)
	if err == nil || op != PlanUpdate {
		t.Errorf("refusal path: op=%v err=%v", op, err)
	}
	// PlanUpdate accepted with --replace.
	op, prior, err = classifyRelayAdd(ps, peerset.Relay{Name: "existing", Endpoint: "different:1", MeshAddress: "198.18.0.2", CurrentPubkey: validPubkey()}, true)
	if err != nil || op != PlanUpdate || prior == nil {
		t.Errorf("replace path: op=%v err=%v", op, err)
	}
}

func TestRelaysEqual(t *testing.T) {
	a := peerset.Relay{Name: "r", Site: "s", Tier: "t", Endpoint: "e", CurrentPubkey: "p", MeshAddress: "m"}
	if !relaysEqual(a, a) {
		t.Error("self-equality")
	}
	b := a
	b.Endpoint = "different"
	if relaysEqual(a, b) {
		t.Error("Endpoint diff should compare unequal")
	}
}

func TestFormatRelayDetail_OmitsEmptyFields(t *testing.T) {
	r := peerset.Relay{Endpoint: "x:1", MeshAddress: "198.18.0.2", CurrentPubkey: validPubkey()}
	got := formatRelayDetail(r)
	if strings.Contains(got, "site=") || strings.Contains(got, "tier=") {
		t.Errorf("empty optional fields should not appear: %q", got)
	}
}

func TestFormatRelayUpdateDetail_NilPriorFallsBack(t *testing.T) {
	r := peerset.Relay{Endpoint: "x:1", MeshAddress: "198.18.0.2", CurrentPubkey: validPubkey()}
	got := formatRelayUpdateDetail(nil, r)
	if !strings.Contains(got, "endpoint=x:1") {
		t.Errorf("got %q", got)
	}
}

func TestFormatRelayUpdateDetail_NoDiffSafetyNet(t *testing.T) {
	r := peerset.Relay{Name: "x", Endpoint: "x:1", MeshAddress: "198.18.0.2", CurrentPubkey: validPubkey()}
	if got := formatRelayUpdateDetail(&r, r); !strings.Contains(got, "no diff") {
		t.Errorf("safety net failed: %q", got)
	}
}

func TestEmptyDash(t *testing.T) {
	if emptyDash("") != "-" {
		t.Error("empty → -")
	}
	if emptyDash("x") != "x" {
		t.Error("non-empty unchanged")
	}
}

func TestOpLabel(t *testing.T) {
	for _, c := range []struct {
		op   PlanOp
		want string
	}{
		{PlanAdd, "added"}, {PlanUpdate, "updated"}, {PlanRemove, "removed"},
		{PlanKeep, "unchanged"}, {PlanReuseKeypair, "applied"},
	} {
		if got := opLabel(c.op); got != c.want {
			t.Errorf("opLabel(%v)=%q, want %q", c.op, got, c.want)
		}
	}
}
