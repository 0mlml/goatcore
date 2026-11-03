package peerset

import "errors"

// Sentinel errors returned by the library. All distinguishable failure
// modes are reachable via errors.Is so callers (bundle-create, the
// future wg-cp0 orchestrator, Ansible-callable helpers) can switch on
// the kind without parsing strings.
var (
	// ErrPeerNotFound is returned by Lookup / LookupByPubkey /
	// Remove / Resurrect when no peer matches the requested name or
	// pubkey in the relevant section (Remove looks in active peers;
	// Resurrect looks in deprecated_peers[]).
	ErrPeerNotFound = errors.New("peerset: peer not found")

	// ErrDuplicateName is returned by Upsert when the caller passes a
	// peer whose Name is empty, or by Resurrect when the active peer
	// set already carries a peer of that name (operator must rotate
	// or remove the active entry first). Bare-name-collision on Upsert
	// is the *intended* path — Upsert replaces by name; it is not an
	// error there.
	ErrDuplicateName = errors.New("peerset: duplicate peer name")

	// ErrDuplicatePubkey is returned by Upsert / Resurrect when the
	// incoming peer's CurrentPubkey matches another active peer's
	// CurrentPubkey. Pubkeys must be globally unique because relay
	// allowlists are keyed by pubkey; two peers cannot share one.
	ErrDuplicatePubkey = errors.New("peerset: duplicate pubkey")

	// ErrDuplicateAddress is returned by Upsert / Resurrect when the
	// incoming peer's WGCP0Address collides with another active peer's
	// address. Two peers cannot claim the same /32 on the mesh.
	ErrDuplicateAddress = errors.New("peerset: duplicate wg_cp0_address")

	// ErrSchemaInvalid is returned by Load + Validate when the document
	// (or the in-memory PeerSet) violates a schema invariant — unknown
	// schema_version, missing required fields, empty name, malformed
	// timestamp, etc.
	ErrSchemaInvalid = errors.New("peerset: schema invariant violated")

	// ErrMigrationFailed is returned by Load when a v1 → v2
	// auto-migration cannot complete (e.g. the v1 document is itself
	// malformed). The underlying parse error is wrapped.
	ErrMigrationFailed = errors.New("peerset: v1 to v2 migration failed")
)
