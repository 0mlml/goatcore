package main

// Block 22R.1 — bundle-create's --update-allowlist surface now routes
// through ops/enrollment/peerset (the accessor library mandated by
// ADR 0322). This file is a thin call-site adapter; the prior in-line
// v1 array writer + atomic writer have moved into the peerset package
// so there is one (and only one) path that reads or writes the JSON.

import (
	"fmt"
	"time"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

// allowlistEntry preserves the bundle-create-side shape used by
// main.go. It maps onto a peerset.Peer at the API boundary so the rest
// of bundle-create (and the test harness) does not need to learn the
// v2 envelope shape.
type allowlistEntry struct {
	Name    string
	Pubkey  string
	Address string
}

// rotateAllowlist loads the peer-set at path, calls peerset.RotatePubkey
// for the named device, and saves the v2 envelope back. The new pubkey
// gets appended to pubkey_history[] with the operator-supplied reason.
// Block 22R.2 / ADR 0322 Property 4.
func rotateAllowlist(path, deviceID, newPubkey, reason string) error {
	if deviceID == "" || newPubkey == "" || reason == "" {
		return fmt.Errorf("rotate-allowlist: empty deviceID/pubkey/reason (%q/%q/%q)", deviceID, newPubkey, reason)
	}
	ps, err := peerset.Load(path)
	if err != nil {
		return fmt.Errorf("load peerset %s: %w", path, err)
	}
	if err := ps.RotatePubkey(deviceID, newPubkey, reason); err != nil {
		return fmt.Errorf("rotate peer %s: %w", deviceID, err)
	}
	if err := ps.Save(path); err != nil {
		return fmt.Errorf("save peerset %s: %w", path, err)
	}
	return nil
}

// upsertAllowlist loads the peer-set at path (auto-migrating v1 → v2
// in-memory on first read), Upserts the entry, and saves the v2 envelope
// back. Idempotent on re-run; collision against another active peer's
// pubkey or address surfaces as a fatal error (per peerset invariants)
// rather than being silently absorbed.
//
// Site + RoleByDeviceID stay on the bundle-create side because
// bundle-create knows them from its CLI flags; this adapter passes
// them through.
func upsertAllowlist(path string, entry allowlistEntry, site, role string, bundleExpiry time.Time) error {
	if entry.Name == "" || entry.Pubkey == "" || entry.Address == "" {
		return fmt.Errorf("incomplete allowlist entry: %+v", entry)
	}
	ps, err := peerset.Load(path)
	if err != nil {
		return fmt.Errorf("load peerset %s: %w", path, err)
	}
	peer := peerset.Peer{
		Name:          entry.Name,
		WGCP0Address:  entry.Address,
		CurrentPubkey: entry.Pubkey,
		Role:          role,
		Site:          site,
	}
	if !bundleExpiry.IsZero() {
		peer.BundleExpiry = &bundleExpiry
	}
	peer.Metadata.LastOperation = "bundle-create --update-allowlist"
	if err := ps.Upsert(peer); err != nil {
		return fmt.Errorf("upsert peer %s: %w", entry.Name, err)
	}
	if err := ps.Save(path); err != nil {
		return fmt.Errorf("save peerset %s: %w", path, err)
	}
	return nil
}
