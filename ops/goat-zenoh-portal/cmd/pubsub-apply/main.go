// Command pubsub-apply reconciles a goat-zenoh-portal to the state
// described in a pub/sub state-of-record repo (Block 86). Idempotent;
// current-state-only (extras get retired / deleted).
//
// Authoritative design: docs/design/pubsub-state-as-code.md.
//
// Typical use:
//
//	# Apply local repo to local portal.
//	pubsub-apply --from ./pubsub-state \
//	             --portal-url http://127.0.0.1:8088 \
//	             --api-key $PORTAL_OPERATOR_API_KEY
//
//	# Dry-run (print plan, change nothing).
//	pubsub-apply --from ./pubsub-state --dry-run
//
//	# Narrow to one vendor.
//	pubsub-apply --from ./pubsub-state --scope notional
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pubsubstate"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	fromDir := flag.String("from", "pubsub-state", "directory containing the state repo to apply")
	portalURL := flag.String("portal-url", envOr("PORTAL_URL", "http://127.0.0.1:8088"), "portal base URL")
	apiKey := flag.String("api-key", os.Getenv("PORTAL_OPERATOR_API_KEY"), "operator API key (default: $PORTAL_OPERATOR_API_KEY)")
	scope := flag.String("scope", "", "narrow the reconcile to this vendor prefix (default: all vendors)")
	dryRun := flag.Bool("dry-run", false, "print the plan; do not mutate")
	autoApprove := flag.Bool("auto-approve", true, "mark newly-created vendors as approved (default true)")
	exitOnDriftSeconds := flag.Int("timeout", 120, "overall context timeout in seconds")
	flag.Parse()

	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "pubsub-apply: --api-key (or $PORTAL_OPERATOR_API_KEY) is required")
		os.Exit(2)
	}

	desired, err := pubsubstate.ReadRepo(*fromDir)
	if err != nil {
		slog.Error("read repo", "dir", *fromDir, "err", err)
		os.Exit(1)
	}
	slog.Info("loaded desired state", "vendors", len(desired))

	client, err := pubsubstate.NewClient(*portalURL, *apiKey)
	if err != nil {
		slog.Error("init client", "err", err)
		os.Exit(1)
	}
	if *dryRun {
		client.SetDryRun(func(method, path string) {
			fmt.Printf("would: %s %s\n", method, path)
		})
	}

	rec := &pubsubstate.Reconciler{
		Client:      client,
		Desired:     desired,
		VendorScope: *scope,
		AutoApprove: *autoApprove,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*exitOnDriftSeconds)*time.Second)
	defer cancel()

	plan, err := rec.Apply(ctx)
	if err != nil {
		slog.Error("apply failed", "err", err)
		os.Exit(1)
	}

	if plan.IsEmpty() {
		fmt.Println("no changes; portal already matches repo")
		return
	}
	for _, a := range plan.Actions {
		fmt.Printf("%s  vendor=%s  key=%s  ver=%s  lang=%s  — %s\n",
			a.Verb, a.VendorPrefix, a.KeyPattern, a.Version, a.Language, a.Reason)
	}
	if *dryRun {
		// In dry-run, exit 2 when the plan is non-empty so CI / scripts
		// can detect drift.
		os.Exit(2)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
