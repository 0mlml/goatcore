// Command wg-cp0 is the operator-facing orchestrator for the wg-cp0
// declarative substrate (ADR 0322 / Block 22R).
//
// 22R.1 shipped the read-only surface (validate, list, show).
//
// 22R.2 lands the mutating surface across three parallel sessions, all
// merged here:
//
//   - Session A: reconcile + diff + the shared apply pipeline
//     (applier.go, live_state.go, mutations.go, diff_formatter.go,
//     audit.go) that the lifecycle + metadata subcommands compose
//     against. Track wg-cp0-orchestrator-reconcile-core.
//   - Session B: per-peer lifecycle subcommands
//     (onboard / rotate / remove / resurrect). Track
//     wg-cp0-orchestrator-peer-lifecycle.
//   - Session C: additive-metadata subcommands + signed-release
//     pipeline (add-relay / update-endpoints / update-ttl). Track
//     wg-cp0-orchestrator-metadata-release.
//
// All three sessions' subcommands compose with bundle-create's
// --reuse-keypair-from flag (per ADR 0322 Property 4). With all three
// merged, the operator surface §4.1 in
// docs/design/wg-cp0-declarative-substrate.md is complete and the
// four-tool ceremony deprecation window (22R.4) can open.
//
// Subcommand registration is alphabetical (per the Session A / B / C
// coordination contract — append in alphabetical order to minimize
// merge conflicts).
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

const usage = `wg-cp0 — declarative substrate orchestrator (ADR 0322 / Block 22R)

usage:

read-only (22R.1):
  wg-cp0 validate [--path <p>]
  wg-cp0 list     [--path <p>] [--site <s>] [--role <r>] [--include-deprecated] [--json]
  wg-cp0 show     <peer-name> [--path <p>] [--json]

reconcile-core (22R.2, Session A):
  wg-cp0 diff       [--site <s>] [--no-add] [--remove] [--path <p>] [--json]
  wg-cp0 reconcile  [--site <s>] [--apply] [--no-add] [--remove] [--path <p>] [--json]

per-peer lifecycle (22R.2, Session B):
  wg-cp0 onboard  <peer-name> --site <s> --role <r> --wg-cp0-address <ip>/24 [--bundle-out <p>] [--apply]
  wg-cp0 rotate   <peer-name> --reason "<text>" [--bundle-out <p>] [--apply]
  wg-cp0 remove   <peer-name> --reason "<text>" [--apply]
  wg-cp0 resurrect <peer-name> [--apply]

additive-metadata (22R.2, Session C):
  wg-cp0 add-relay         <relay-name> --endpoint <host:port> --pubkey <b64> --address <ip>
                           [--site <s>] [--tier <t>] [--apply] [--replace] [--path <p>]
  wg-cp0 update-endpoints  <peer-name>|--all-peers
                           [--add-relay <name>] [--remove-relay <name>]
                           [--set-active-forwarder <name>]
                           [--apply] [--out-dir <path>] [--path <p>]
  wg-cp0 update-ttl        <peer-name>|--all-peers --extend <duration>
                           [--apply] [--force] [--out-dir <path>] [--path <p>]

  default --path is ops/enrollment/wg-cp0-bundle-allowlist.json
  All mutating subcommands are dry-run-by-default; --apply commits.
`

const defaultPath = "ops/enrollment/wg-cp0-bundle-allowlist.json"

func main() {
	code := run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

// run is the testable entrypoint. Returns the exit code the binary
// would emit. Writing to the supplied io.Writers lets tests capture
// output without depending on os.Stdout/Stderr.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		if _, err := fmt.Fprint(stderr, usage); err != nil {
			return 1
		}
		return 2
	}
	cmd, rest := args[0], args[1:]
	// Subcommand dispatch — alphabetical to minimize merge conflicts
	// across Sessions A / B / C, all merged here.
	//   A owns: diff, reconcile (+ list, show, validate from 22R.1
	//           promoted to live-state-aware bodies)
	//   B owns: onboard, remove, resurrect, rotate
	//   C owns: add-relay, update-endpoints, update-ttl
	switch cmd {
	case "add-relay":
		return cmdAddRelay(rest, stdout, stderr)
	case "diff":
		return cmdDiff(rest, stdout, stderr)
	case "list":
		return cmdList(rest, stdout, stderr)
	case "onboard":
		return cmdOnboard(rest, stdout, stderr)
	case "reconcile":
		return cmdReconcile(rest, stdout, stderr)
	case "remove":
		return cmdRemove(rest, stdout, stderr)
	case "resurrect":
		return cmdResurrect(rest, stdout, stderr)
	case "rotate":
		return cmdRotate(rest, stdout, stderr)
	case "show":
		return cmdShow(rest, stdout, stderr)
	case "update-endpoints":
		return cmdUpdateEndpoints(rest, stdout, stderr)
	case "update-ttl":
		return cmdUpdateTTL(rest, stdout, stderr)
	case "validate":
		return cmdValidate(rest, stdout, stderr)
	case "-h", "--help", "help":
		if _, err := fmt.Fprint(stdout, usage); err != nil {
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "wg-cp0: unknown subcommand %q\n%s", cmd, usage)
		return 2
	}
}

type filters struct {
	path              string
	site              string
	role              string
	includeDeprecated bool
	asJSON            bool
	includeLiveState  bool
}

// parseFlags consumes the recognized flags from the tail of a
// subcommand's args; the function name is internal to keep main.go a
// single readable file. Unknown flags return an error.
func parseFlags(args []string, accept map[string]bool) (filters, []string, error) {
	f := filters{path: defaultPath}
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") && !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		var val string
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			val = name[eq+1:]
			name = name[:eq]
		} else if name != "include-deprecated" && name != "json" && name != "include-live-state" {
			if i+1 >= len(args) {
				return f, nil, fmt.Errorf("flag --%s requires a value", name)
			}
			val = args[i+1]
			i++
		}
		if !accept[name] {
			return f, nil, fmt.Errorf("unknown flag --%s", name)
		}
		switch name {
		case "path":
			f.path = val
		case "site":
			f.site = val
		case "role":
			f.role = val
		case "include-deprecated":
			f.includeDeprecated = true
		case "json":
			f.asJSON = true
		case "include-live-state":
			f.includeLiveState = true
		}
	}
	return f, positional, nil
}

func filterPeers(in []peerset.Peer, site, role string) []peerset.Peer {
	if site == "" && role == "" {
		return in
	}
	out := make([]peerset.Peer, 0, len(in))
	for _, p := range in {
		if site != "" && p.Site != site {
			continue
		}
		if role != "" && p.Role != role {
			continue
		}
		out = append(out, p)
	}
	return out
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return s[:n-1] + "…"
}
