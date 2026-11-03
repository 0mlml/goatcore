package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The offboard leg (goat#372, final slice of goat#260): the console queues an
// admin-approved offboard as an `offboard-requested` proposal, the runner
// atomically claims it (offboard-requested → offboarding), executes the
// REVERSIBLE native revoke, and writes the terminal result back
// (offboarded | offboard-failed). Same engine as the CLI, not a second
// implementation. Tests derive from the issue spec +
// docs/design/credential-revocation-and-offboarding.md, not from the code.

// Spec A: an offboard-requested row is claimed, revoked, and finalized
// offboarded. Spec C: the approving admin rides the row into the revoke leg —
// that's who the audit attributes.
func TestRunOnce_OffboardRequested_RevokesAndWritesBack(t *testing.T) {
	f := newFakePortal()
	f.add(proposal{ID: "off-1", Env: "zagros", Handle: "jane", Email: "person-58@example.org",
		Status: "offboard-requested", OffboardRequestedBy: "adminbob"})
	r, _ := testRunner(t, f)

	var revoked []proposal
	r.revoke = func(_ context.Context, p proposal) error {
		revoked = append(revoked, p)
		return nil
	}

	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.offboardClaims) != 1 || f.offboardClaims[0] != "off-1" {
		t.Fatalf("offboard claims = %v, want [off-1]", f.offboardClaims)
	}
	if len(revoked) != 1 || revoked[0].Handle != "jane" {
		t.Fatalf("revoked = %+v, want jane", revoked)
	}
	if revoked[0].OffboardRequestedBy != "adminbob" {
		t.Errorf("revoke saw requested_by = %q, want adminbob", revoked[0].OffboardRequestedBy)
	}
	if f.results["off-1"] != "offboarded" {
		t.Errorf("result = %q, want offboarded", f.results["off-1"])
	}
}

// Two admins clicking at once resolves at the claim: the 409 loser walks away
// without revoking (the winner's revoke is the only cut).
func TestRunOnce_OffboardClaimLostSkipsRevoke(t *testing.T) {
	f := newFakePortal()
	f.add(proposal{ID: "off-1", Env: "zagros", Handle: "jane", Status: "offboard-requested"})
	f.offboardClaimFail["off-1"] = true
	r, _ := testRunner(t, f)
	revokes := 0
	r.revoke = func(context.Context, proposal) error { revokes++; return nil }

	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if revokes != 0 {
		t.Errorf("claim-lost revoked %d times, want 0", revokes)
	}
	if got := f.results["off-1"]; got != "" {
		t.Errorf("claim-lost wrote back %q, want nothing", got)
	}
}

// Partial failure stays loud and visible: a failed revoke is written back as
// offboard-failed with the error, never left silently claimed and never
// marked offboarded (the person still has access).
func TestRunOnce_OffboardRevokeFailureRecordsOffboardFailed(t *testing.T) {
	f := newFakePortal()
	f.add(proposal{ID: "off-1", Env: "zagros", Handle: "jane", Status: "offboard-requested"})
	r, _ := testRunner(t, f)
	r.revoke = func(context.Context, proposal) error { return errBoom }

	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.results["off-1"] != "offboard-failed" {
		t.Errorf("result = %q, want offboard-failed", f.results["off-1"])
	}
	if f.resultErrs["off-1"] == "" {
		t.Error("offboard-failed writeback carried no error text")
	}
}

// Dry-run must not claim, revoke, or write back — same contract as the
// onboarding leg.
func TestRunOnce_OffboardDryRunTouchesNothing(t *testing.T) {
	f := newFakePortal()
	f.add(proposal{ID: "off-1", Env: "zagros", Handle: "jane", Status: "offboard-requested"})
	r, _ := testRunner(t, f)
	r.dryRun = true
	revokes := 0
	r.revoke = func(context.Context, proposal) error { revokes++; return nil }

	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if revokes != 0 || len(f.offboardClaims) != 0 {
		t.Errorf("dry-run acted: revokes=%d claims=%v", revokes, f.offboardClaims)
	}
	if f.proposals["off-1"].Status != "offboard-requested" {
		t.Errorf("dry-run changed status to %q", f.proposals["off-1"].Status)
	}
}

// An env-scoped runner must not cut a principal on another goatnet — and must
// not let the request vanish silently either (it stays offboard-requested for
// that env's runner).
func TestRunOnce_OffboardOnlyEnvFilter(t *testing.T) {
	f := newFakePortal()
	f.add(proposal{ID: "off-nova", Env: "nova", Handle: "x", Status: "offboard-requested"})
	f.add(proposal{ID: "off-zagros", Env: "zagros", Handle: "y", Status: "offboard-requested"})
	r, _ := testRunner(t, f)
	r.onlyEnv = "zagros"
	var revoked []string
	r.revoke = func(_ context.Context, p proposal) error {
		revoked = append(revoked, p.Env)
		return nil
	}

	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(revoked) != 1 || revoked[0] != "zagros" {
		t.Errorf("only-env revoked %v, want [zagros]", revoked)
	}
	if f.proposals["off-nova"].Status != "offboard-requested" {
		t.Errorf("foreign-env row status = %q, want untouched offboard-requested", f.proposals["off-nova"].Status)
	}
}

// One poll services both queues: an approved onboard and a requested offboard
// in the same pass each reach their own terminal state.
func TestRunOnce_MixedQueueHandlesBothLegs(t *testing.T) {
	f := newFakePortal()
	f.add(proposal{ID: "on-1", Env: "zagros", Handle: "newguy", Email: "n@example.org", Status: "approved", ManifestJSON: testManifest})
	f.add(proposal{ID: "off-1", Env: "zagros", Handle: "oldguy", Status: "offboard-requested"})
	r, _ := testRunner(t, f)
	minted, revokes := 0, 0
	r.mint = func(context.Context, proposal, string) (mintResult, error) { minted++; return mintResult{}, nil }
	r.revoke = func(context.Context, proposal) error { revokes++; return nil }

	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if minted != 1 || revokes != 1 {
		t.Errorf("minted=%d revokes=%d, want 1 and 1", minted, revokes)
	}
	if f.results["on-1"] != "onboarded" || f.results["off-1"] != "offboarded" {
		t.Errorf("results = %v, want on-1 onboarded + off-1 offboarded", f.results)
	}
}

// Spec B: web offboard is the reversible kind ONLY. The exec'd command is the
// native revoke — and must never carry --purge (permanent delete stays
// CLI-only). --native is forced for the same reason the mint forces it: the
// runner cannot run Ansible, so a non-native-serviceable case must fail fast.
func TestRevokeArgs_ReversibleNeverPurge(t *testing.T) {
	args := revokeArgs(proposal{Handle: "jane", Env: "zagros"}, "/repo")
	joined := " " + strings.Join(args, " ") + " "
	for _, want := range []string{" admin ", " onboard ", " jane ", " --revoke ", " --native ", " --env zagros ", " --repo-root /repo "} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %v missing %q", args, strings.TrimSpace(want))
		}
	}
	if strings.Contains(joined, "--purge") {
		t.Fatalf("args %v carry --purge — web offboard is reversible ONLY", args)
	}
}

// The F-512 preflight (#963) guards this leg too: with the management API
// unreachable from the container, a queued offboard is NOT claimed — claiming
// would turn a deploy fault into an offboard-failed row an admin has to
// re-queue by hand. The row stays offboard-requested, where the next healthy
// cycle takes it unchanged.
func TestRunOnce_MgmtUnreachableClaimsNoOffboards(t *testing.T) {
	f := newFakePortal()
	f.add(proposal{ID: "off-1", Env: "zagros", Handle: "jane", Status: "offboard-requested"})
	r, _ := testRunner(t, f)
	revokes := 0
	r.revoke = func(context.Context, proposal) error { revokes++; return nil }
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close() // a closed port: the F-512 shape (connect refused)
	r.mgmtURL = dead.URL

	if err := r.runOnce(context.Background()); err == nil {
		t.Fatal("runOnce must report the preflight failure")
	}
	if revokes != 0 || len(f.offboardClaims) != 0 {
		t.Errorf("offboard leg acted with the management API unreachable: revokes=%d claims=%v", revokes, f.offboardClaims)
	}
	if got := f.proposals["off-1"].Status; got != "offboard-requested" {
		t.Errorf("row status = %q, want offboard-requested (untouched)", got)
	}
}

// An offboard-failed note must carry goat's terminal `error:` line, not the
// warning header that precedes it — the same output shape #963 fixed for the
// mint leg. Two real-length warning lines push the error past a head-300
// clip, so a head-based snippet records the wrong reason.
func TestExecRevoke_ErrorCarriesTerminalErrorLine(t *testing.T) {
	dir := t.TempDir()
	warn := "warning: operator-action audit emit failed (/home/runner/.goat/contexts/default/profile.toml missing router_endpoint - re-run goat profile init); recording to local fallback log"
	fake := "#!/bin/sh\n" +
		"echo '" + warn + "'\n" +
		"echo '" + warn + "'\n" +
		"echo 'error: netbird: NetBird GET /api/groups failed: network or parse error'\n" +
		"exit 1\n"
	goatBin := filepath.Join(dir, "goat")
	if err := os.WriteFile(goatBin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	r, _ := testRunner(t, newFakePortal())
	r.goatBin = goatBin

	err := r.execRevoke(context.Background(), proposal{ID: "off-1", Handle: "jane", Env: "zagros"})
	if err == nil {
		t.Fatal("exit 1 must surface as an error")
	}
	if !strings.Contains(err.Error(), "error: netbird: NetBird GET /api/groups failed") {
		t.Errorf("revoke error lost the terminal error line: %q", err.Error())
	}
	if strings.Contains(err.Error(), "warning:") {
		t.Errorf("revoke error carries the warning header instead of the reason: %q", err.Error())
	}
}

func TestOffboardClaimReportsRunningReissue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"reissue_running","message":"waiting for key reissue"}}`))
	}))
	defer srv.Close()
	c := newPortalClientWithHTTP(srv.URL, "fixture", srv.Client())
	won, err := c.offboardClaim(context.Background(), "fixture")
	if won || err == nil || !strings.Contains(err.Error(), "running reissue") {
		t.Fatalf("claim=%v, %v", won, err)
	}
}
