package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// The offboard leg (goat#372): poll offboard-requested proposals, claim
// atomically, run the reversible native revoke, write the terminal result
// back. Same engine as the CLI cut — the runner execs `goat admin onboard
// --revoke`, it does not reimplement it. See
// docs/design/credential-revocation-and-offboarding.md.

// processOffboard handles one queued offboard end to end. A failure is
// recorded back on the proposal (offboard-failed) rather than crashing the
// loop — the row stays loud until the cut actually lands.
func (r *runner) processOffboard(ctx context.Context, p proposal) {
	l := r.log.With("id", p.ID, "handle", p.Handle, "env", p.Env, "requested_by", p.OffboardRequestedBy)

	if r.dryRun {
		l.Info("dry-run: would offboard (no claim / revoke / writeback)")
		return
	}

	won, err := r.pc.offboardClaim(ctx, p.ID)
	if err != nil {
		l.Error("offboard claim failed", "err", err)
		return
	}
	if !won {
		l.Debug("offboard claim lost (already claimed / not requested) — skipping")
		return
	}
	l.Info("offboard claimed")

	revoke := r.revoke
	if revoke == nil {
		revoke = r.execRevoke
	}
	if err := revoke(ctx, p); err != nil {
		l.Error("revoke failed", "err", err)
		_ = r.pc.result(ctx, p.ID, "offboard-failed", truncErr(err.Error()))
		return
	}
	l.Info("revoked")

	if err := r.pc.result(ctx, p.ID, "offboarded", ""); err != nil {
		l.Error("writeback (offboarded) failed", "err", err)
	}
}

// revokeArgs builds the argv for the reversible native revoke. Never --purge:
// permanent delete stays CLI-only (goat#372 spec B). --native is forced for
// the same reason the mint forces it — the runner cannot run Ansible, so a
// non-native-serviceable case must fail fast with the CLI's own error.
func revokeArgs(p proposal, repoRoot string) []string {
	args := []string{"admin", "onboard", p.Handle, "--revoke", "--native"}
	if p.Env != "" {
		args = append(args, "--env", p.Env)
	}
	return append(args, "--repo-root", repoRoot)
}

// execRevoke runs the reversible native revoke, stamping GOAT_OPERATOR_HANDLE
// with the runner's ID — the same runner-executed/operator-approved
// attribution as the mint. The approving admin is recorded on the proposal
// row (offboard_requested_by) and in the console's own audit event.
func (r *runner) execRevoke(ctx context.Context, p proposal) error {
	args := revokeArgs(p, r.repoRoot)
	cmd := exec.CommandContext(ctx, r.goatBin, args...)
	cmd.Dir = r.repoRoot
	cmd.Env = append(os.Environ(), "GOAT_OPERATOR_HANDLE="+r.runnerID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", r.goatBin, strings.Join(args, " "), err, mintErrSnippet(out))
	}
	return nil
}
