package peerset

import "time"

// SchemaVersion is the current envelope schema version. Increment on
// incompatible changes; readers reject documents whose schema_version
// exceeds the version they understand.
const SchemaVersion = 2

// PeerSet is the canonical in-memory representation of the wg-cp0
// peer-set artifact. Load parses a JSON file (v1 or v2) into this
// shape; Save serializes it back as a v2 envelope.
type PeerSet struct {
	SchemaVersion   int              `json:"schema_version"`
	Peers           []Peer           `json:"peers"`
	Relays          []Relay          `json:"relays"`
	DeprecatedPeers []DeprecatedPeer `json:"deprecated_peers"`
	ActiveForwarder *ActiveForwarder `json:"active_forwarder,omitempty"`
	Metadata        Metadata         `json:"metadata"`
}

// Peer is an active wg-cp0 client. Name is the stable identifier across
// pubkey rotations. CurrentPubkey is the live pubkey the WireGuard
// listener accepts; prior pubkeys (post-rotation) are appended to
// PubkeyHistory with rotated-at + reason for audit reconstruction.
type Peer struct {
	Name           string               `json:"name"`
	WGCP0Address   string               `json:"wg_cp0_address"`
	CurrentPubkey  string               `json:"current_pubkey"`
	PubkeyHistory  []PubkeyHistoryEntry `json:"pubkey_history,omitempty"`
	Role           string               `json:"role,omitempty"`
	Site           string               `json:"site,omitempty"`
	BundleExpiry   *time.Time           `json:"bundle_expiry,omitempty"`
	AllowedIPsExtra []string            `json:"allowed_ips_extra,omitempty"`
	Metadata       PeerMetadata         `json:"metadata,omitempty"`
}

// PubkeyHistoryEntry records one rotation event: the pubkey that was
// retired, when it was rotated out, and a free-form reason captured by
// the orchestrator (or by the operator at bundle-create time).
type PubkeyHistoryEntry struct {
	Pubkey    string    `json:"pubkey"`
	RotatedAt time.Time `json:"rotated_at"`
	Reason    string    `json:"reason"`
}

// PeerMetadata is per-peer write-time provenance. Created and last-updated
// timestamps are maintained automatically by mutation methods; LastOperation
// is a short tag (e.g. "upsert", "rotate", "resurrect") set by the caller
// site to make audit reading scannable.
type PeerMetadata struct {
	CreatedAt     time.Time `json:"created_at,omitempty"`
	LastUpdatedAt time.Time `json:"last_updated_at,omitempty"`
	LastOperation string    `json:"last_operation,omitempty"`
}

// Relay is a wg-cp0 listener (an EC2 relay VPS or a local-physical relay,
// per ADR 0316). Name is the stable identifier; Endpoint is the
// network-reachable host:port the device-side bundle dials; MeshAddress
// is the relay's own wg-cp0 mesh-side address.
type Relay struct {
	Name          string `json:"name"`
	Site          string `json:"site,omitempty"`
	Tier          string `json:"tier,omitempty"`
	Endpoint      string `json:"endpoint,omitempty"`
	CurrentPubkey string `json:"current_pubkey,omitempty"`
	MeshAddress   string `json:"mesh_address,omitempty"`
}

// DeprecatedPeer carries the full peer record at the time of removal
// plus the reason and timestamp. Resurrect restores the active Peer
// from this row.
type DeprecatedPeer struct {
	Peer
	RemovedAt     time.Time `json:"removed_at"`
	RemovedReason string    `json:"removed_reason"`
}

// ActiveForwarder names the currently-elected forwarder tier + host per
// Block 22P. Optional — absent means the inventory-side default applies.
type ActiveForwarder struct {
	Tier string `json:"tier"`
	Host string `json:"host"`
}

// Metadata is the envelope-level write-time provenance.
type Metadata struct {
	LastModifiedAt   time.Time `json:"last_modified_at,omitempty"`
	LastModifiedBy   string    `json:"last_modified_by,omitempty"`
	LastModifiedVia  string    `json:"last_modified_via,omitempty"`
	SchemaMinVersion int       `json:"schema_min_compatible_version,omitempty"`
}
