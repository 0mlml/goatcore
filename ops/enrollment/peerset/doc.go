// Package peerset is the accessor library for the wg-cp0 peer-set
// declarative source-of-truth file (today: ops/enrollment/wg-cp0-bundle-allowlist.json).
//
// Block 22R.1 / ADR 0322 / docs/design/wg-cp0-declarative-substrate.md §2.
// This package is the only path that reads or writes the JSON-of-record;
// every wg-cp0 ceremony tool (bundle-create, future wg-cp0 orchestrator,
// Ansible plays) passes through it.
//
// Schema v2 envelope:
//
//	{
//	  "schema_version": 2,
//	  "peers":             [...],   // active peers
//	  "relays":            [...],   // wg-cp0 relay inventory
//	  "deprecated_peers":  [...],   // soft-deleted; never hard-removed
//	  "active_forwarder":  {...},   // optional Block 22P tier selection
//	  "metadata":          {...}    // write-time provenance
//	}
//
// Auto-migration from v1 (today's flat array) to v2 runs on Load when
// the document is missing schema_version. The migrated v2 envelope is
// produced in-memory; the file on disk is not rewritten until the
// caller invokes Save.
//
// Removal semantics. Remove never deletes a peer; the row moves to
// deprecated_peers[] with a recorded reason and timestamp. Resurrect is
// the explicit inverse. This is the substrate-level closure of the
// silent-fleet-amputation failure mode; the orchestrator surface
// builds on top of this guarantee.
package peerset
