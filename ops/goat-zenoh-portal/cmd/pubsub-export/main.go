// Command pubsub-export serializes the current goat-zenoh-portal state
// (vendors + topics + schemas + per-language code examples) to the
// state-of-record filesystem layout described in
// docs/design/pubsub-state-as-code.md.
//
// Typical use is from the Ansible role pubsub-state-repo or from the
// portal's POST /api/admin/snapshot endpoint (which calls the same
// underlying pubsubstate.Snapshot helper in-process). Standalone use:
//
//	pubsub-export --dsn "$PORTAL_DB_DSN" \
//	              --schema-dir /var/lib/goat-portal/schemas \
//	              --out-dir ./pubsub-state
//
// It only reads: it opens the registry with registry.OpenExisting, which
// applies no migration and refuses a database the portal has not migrated.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pubsubstate"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/schemastore"
)

const version = "0.1.0"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	dsn := flag.String("dsn", os.Getenv("PORTAL_DB_DSN"), "Postgres DSN of the portal registry (default $PORTAL_DB_DSN; required)")
	schemaDir := flag.String("schema-dir", "", "path to portal schemastore root (required)")
	outDir := flag.String("out-dir", "pubsub-state", "directory to write the state repo into")
	portalVersion := flag.String("portal-version", version, "portal binary version to record in MANIFEST")
	flag.Parse()

	if *dsn == "" || *schemaDir == "" {
		fmt.Fprintln(os.Stderr, "pubsub-export: --dsn (or PORTAL_DB_DSN) and --schema-dir are required")
		flag.Usage()
		os.Exit(2)
	}

	reg, err := registry.OpenExisting(*dsn)
	if err != nil {
		slog.Error("open registry", "err", err)
		os.Exit(1)
	}
	defer reg.Close()

	store, err := schemastore.New(*schemaDir)
	if err != nil {
		slog.Error("open schemastore", "err", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	state, err := pubsubstate.Snapshot(ctx, reg, store)
	if err != nil {
		slog.Error("snapshot", "err", err)
		os.Exit(1)
	}
	manifest, err := pubsubstate.WriteRepo(*outDir, state, *portalVersion, time.Now().Unix())
	if err != nil {
		slog.Error("write repo", "err", err)
		os.Exit(1)
	}
	slog.Info("exported",
		"out_dir", *outDir,
		"vendors", manifest.VendorCount,
		"topics", manifest.TopicCount,
		"schemas", manifest.SchemaCount,
		"examples", manifest.ExampleCount,
		"content_sha256", manifest.ContentSHA256,
	)
}
