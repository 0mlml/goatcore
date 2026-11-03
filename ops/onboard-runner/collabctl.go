package main

// Operator-facing collab account lifecycle: `onboard-runner -collab <op>`.
//
// The mesh revoke deliberately does NOT cut collab access
// (docs/design/collab-onboarding-provisioner.md §"Revocation is a separate
// decision"), so cutting it is an explicit, separate operator act. These
// subcommands are that act, plus the read-only surfaces an operator needs to
// decide: they reuse the same provider, credentials and config as the
// provisioning leg — no second client, no second credential path.
//
//	-collab status  -handle <h>   is there a collab account for this person?
//	-collab revoke  -handle <h>   disable it (idempotent; requires -yes)
//	-collab list                  every collab account (the inventory F-274 wants)
//	-collab reconcile             accounts × identity stores → the delta
//	                              (-json; -emit-backfill <path> writes the patch set)
//	-collab backfill -file <p>    apply an emitted patch set to the manifests
//	                              (dry-run without -yes)
//
// Exit codes: 0 success, 2 usage/config error, 3 operation failed. A reconcile
// that FINDS discrepancies still exits 0 — it is a report, and turning "found
// something" into "failed" would make the cron that runs it lie.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"
)

// collabOpts carries one operator collab invocation. A struct rather than a
// widening argument list — `backfill` and `reconcile` need the repo root and
// output knobs that `status`/`revoke` do not.
type collabOpts struct {
	op       string
	handle   string
	repoRoot string
	// emitBackfill: with `reconcile`, also write the backfill patch set here.
	emitBackfill string
	// file: with `backfill`, the patch set to apply.
	file    string
	jsonOut bool
	yes     bool
	timeout time.Duration
}

// runCollabOp executes an operator collab subcommand and returns a process exit
// code. Kept out of main()'s polling path entirely — this is a one-shot.
func runCollabOp(o collabOpts, log *slog.Logger) int {
	// `backfill` applies an already-emitted document to on-disk manifests: it
	// needs no provider, no credentials, and no network. Handle it before the
	// provider gate so the deploy pass can apply a reconcile taken elsewhere.
	if strings.ToLower(o.op) == "backfill" {
		return runCollabBackfill(o)
	}

	p := newCollabProviderFromEnv(o.timeout, log)
	if p == nil {
		fmt.Fprintln(os.Stderr, "collab provider not configured — set COLLAB_PROVISION_ENABLED=1 plus COLLAB_KANIDM_API_TOKEN (preferred) or COLLAB_KANIDM_IDM_ADMIN_PW")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout+30*time.Second)
	defer cancel()
	handle := o.handle

	switch strings.ToLower(o.op) {
	case "status":
		if handle == "" {
			fmt.Fprintln(os.Stderr, "-collab status needs -handle")
			return 2
		}
		ok, err := p.Exists(ctx, handle)
		if err != nil {
			fmt.Fprintf(os.Stderr, "collab status %s: %v\n", handle, err)
			return 3
		}
		if ok {
			fmt.Printf("%s: collab account PRESENT (provider %s)\n", handle, p.Name())
		} else {
			fmt.Printf("%s: no collab account (provider %s)\n", handle, p.Name())
		}
		return 0

	case "revoke":
		if handle == "" {
			fmt.Fprintln(os.Stderr, "-collab revoke needs -handle")
			return 2
		}
		ok, err := p.Exists(ctx, handle)
		if err != nil {
			fmt.Fprintf(os.Stderr, "collab revoke %s: pre-check failed: %v\n", handle, err)
			return 3
		}
		if !ok {
			fmt.Printf("%s: no collab account — nothing to revoke\n", handle)
			return 0
		}
		if !o.yes {
			fmt.Fprintf(os.Stderr, "refusing to revoke %s without -yes (this disables a real person's collab access; it is NOT implied by a mesh revoke)\n", handle)
			return 2
		}
		if err := p.Deprovision(ctx, handle); err != nil {
			fmt.Fprintf(os.Stderr, "collab revoke %s: %v\n", handle, err)
			return 3
		}
		fmt.Printf("%s: collab account revoked (provider %s)\n", handle, p.Name())
		return 0

	case "list":
		lister, ok := p.(interface {
			List(context.Context) ([]CollabAccount, error)
		})
		if !ok {
			fmt.Fprintf(os.Stderr, "provider %s cannot enumerate accounts\n", p.Name())
			return 2
		}
		accts, err := lister.List(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "collab list: %v\n", err)
			return 3
		}
		sort.Slice(accts, func(i, j int) bool { return accts[i].Handle < accts[j].Handle })
		for _, a := range accts {
			fmt.Printf("%s\t%s\t%s\n", a.Handle, a.Email, a.DisplayName)
		}
		fmt.Fprintf(os.Stderr, "%d collab accounts (provider %s)\n", len(accts), p.Name())
		return 0

	case "reconcile":
		lister, ok := p.(collabLister)
		if !ok {
			fmt.Fprintf(os.Stderr, "provider %s cannot enumerate accounts\n", p.Name())
			return 2
		}
		accts, err := lister.List(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "collab reconcile: list accounts: %v\n", err)
			return 3
		}
		manifests, warns, err := loadManifestRecords(o.repoRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "collab reconcile: read manifests: %v\n", err)
			return 3
		}
		roster, err := loadRosterRecords(o.repoRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "collab reconcile: read roster: %v\n", err)
			return 3
		}
		if len(manifests) == 0 && len(roster) == 0 {
			// Fail loudly rather than reporting every account as an orphan: an
			// empty identity side is a wrong-repo-root, not a finding.
			fmt.Fprintf(os.Stderr, "collab reconcile: no manifests and no roster under %s — wrong -repo-root? (refusing to report every account as unmatched)\n", o.repoRoot)
			return 2
		}
		now := nowUTC()
		rep := reconcileCollab(accts, manifests, roster, p.Name(), collabSourceLabel(p), now, warns)
		if o.jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(rep); err != nil {
				fmt.Fprintf(os.Stderr, "collab reconcile: encode json: %v\n", err)
				return 3
			}
		} else {
			writeReport(os.Stdout, rep)
		}
		if o.emitBackfill != "" {
			doc := buildBackfill(rep, now)
			if err := writeBackfill(o.emitBackfill, doc); err != nil {
				fmt.Fprintf(os.Stderr, "collab reconcile: write backfill %s: %v\n", o.emitBackfill, err)
				return 3
			}
			fmt.Fprintf(os.Stderr, "wrote %d backfill patch(es) to %s — apply with `-collab backfill -file %s -yes`\n",
				len(doc.Patches), o.emitBackfill, o.emitBackfill)
		}
		if n := rep.Counts[verdictRevokedLiveAccount]; n > 0 {
			fmt.Fprintf(os.Stderr, "%d account(s) belong to revoked/inactive identities — cutting them is a separate operator act: `-collab revoke -handle <h> -yes`\n", n)
		}
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown -collab op %q (want status|revoke|list|reconcile|backfill)\n", o.op)
		return 2
	}
}

// collabSourceLabel names what the accounts were read from, for the report
// header — the IdP base URL when the provider exposes one, else its slug.
func collabSourceLabel(p CollabProvider) string {
	if k, ok := p.(*kanidmProvider); ok {
		return k.base
	}
	return p.Name()
}

// runCollabBackfill applies an emitted patch set to the on-disk manifests. No
// provider, no network: it is the deploy-pass half of the durable record, split
// from the reconcile so looking never writes.
func runCollabBackfill(o collabOpts) int {
	if o.file == "" {
		fmt.Fprintln(os.Stderr, "-collab backfill needs -file <patch-set> (produced by `-collab reconcile -emit-backfill`)")
		return 2
	}
	doc, err := readBackfill(o.file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "collab backfill: %v\n", err)
		return 2
	}
	if !o.yes {
		fmt.Fprintf(os.Stderr, "dry-run (%d patch(es) from %s, observed %s) — re-run with -yes to write:\n",
			len(doc.Patches), o.file, doc.ObservedAt)
	}
	changed, unchanged, errs := applyBackfill(o.repoRoot, doc, o.yes, os.Stdout)
	verb := "would change"
	if o.yes {
		verb = "changed"
	}
	fmt.Fprintf(os.Stderr, "%s %d manifest(s); %d already current; %d error(s)\n", verb, changed, unchanged, len(errs))
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "  error: %s\n", e)
	}
	if len(errs) > 0 {
		return 3
	}
	return 0
}
