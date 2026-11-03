// Command relay-probe is the typed replacement for the relay-health prober +
// self-publish bash: one static, dependency-free binary with two
// subcommands sharing ONE Deep struct, so the wire shape can't drift across
// files. Stdlib only — cross-compiles to a single binary that drops onto any
// relay with no toolchain (more portable than the old bash + python3 + ss + curl
// chain). ADR 0112 Go/CLI lane; the vehicle for the ADR-1055 per-peer stats.
//
//	relay-probe publish   # runs ON a relay: self-publish host metrics (ADR 1058)
//	relay-probe probe     # (planned) blackbox-probe the relay fleet -> JSON
//
// Config is via env (see publishConfigFromEnv) so it drops into the existing
// systemd EnvironmentFile unchanged.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "publish":
		os.Exit(runPublish())
	case "probe":
		os.Exit(runProbe(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "relay-probe: unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `relay-probe — relay-health prober + self-publish (one binary, one shape)

usage:
  relay-probe publish    self-publish this relay's host metrics to the ingest(s)
  relay-probe probe      blackbox-probe the relay fleet -> relay-health.json

publish env: RELAY_NAME, RELAY_METRICS_INGEST_URL, RELAY_METRICS_BEARER_TOKEN
  optional: RELAY_METRICS_INGEST_URL2 (+ _TOKEN2), RELAY_METRICS_INGEST_HOST,
            GOAT_RELAY_CONTAINER, HIST_KEEP, STATE_DIR, PEERS_WINDOW_MIN

probe flags: --conf --out --vantage --tunnel-endpoint --ingest-dir
  (--deep SSM + --alert-sink/--emit-only are deferred; use relay-health-probe.sh)
`)
}
