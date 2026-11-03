// Package bundle defines the enrollment bundle format and CBOR serialization.
//
// An EnrollmentBundle is a signed capability that authorizes one device to
// enroll into the mesh. It is issued by the offline CA at provisioning time
// and carried forward to the device; any reachable management server validates
// the signature and auto-provisions the peer.
//
// Block 79: hard cutover from Ed25519 to ECDSA P-256. The
// offline-CA root is ECDSA P-256; bundle signatures are ASN.1 DER
// ECDSA-Sig-Value envelopes (variable length, ~70-72 bytes typical) over
// SHA-256(canonical CBOR bytes). No backward compatibility with
// Ed25519-signed bundles — the rotation ceremony re-issues every active
// bundle.
//
// See docs/design/offline-enrollment.md for the full design.
package bundle

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// Version is the current bundle format version. Increment on incompatible
// changes to the wire schema; validators reject unknown versions.
const Version uint8 = 1

// WGCP0FleetSubnet is the wg-cp0 mesh subnet (per ADR 0319). Every
// bundle's relay peer blocks carry this as a catchall in AllowedIPs so
// that membership in wg-cp0 = full mesh reach at the WireGuard layer
// (ADR 0321). Fine-grained per-resource access control lives one
// altitude up at netbird-mesh ACL groups, Ory OIDC, mTLS, Spock per-DB,
// Zenoh key-expressions, and the egress proxy — NOT at WireGuard's
// AllowedIPs.
const WGCP0FleetSubnet = "198.18.0.0/24"

// Kind identifies the type of a KnownEndpoint.
type Kind string

const (
	KindRelay Kind = "relay"
	KindPeer  Kind = "peer"
	KindMgmt  Kind = "mgmt"
)

// KnownEndpoint is a bootstrap path a device will try in order when
// establishing initial connectivity.
type KnownEndpoint struct {
	Addr   string `cbor:"addr"`
	Pubkey []byte `cbor:"pubkey"`
	Kind   Kind   `cbor:"kind"`

	// MeshAddr is the peer's wg-cp0 mesh-side address (no CIDR; e.g.
	// "198.18.0.2"). Optional, populated only for Kind="cp-relay" — the
	// device-side wg-cp0 conf renderer derives AllowedIPs = MeshAddr/32
	// from this when AllowedIPs (below) is empty. Absent for other kinds
	// (relay/peer/mgmt) where the addressing model is different.
	// Block 22K / docs/design/offline-enrollment.md §11.2.
	MeshAddr string `cbor:"mesh_addr,omitempty"`

	// AllowedIPs is the list of CIDRs the device-side wg-cp0 conf
	// renderer writes into this endpoint's [Peer] block. Per ADR 0321
	// (wg-cp0 is a perimeter, not an ACL layer), every relay peer
	// block in every bundle carries the fleet subnet WGCP0FleetSubnet
	// (= 198.18.0.0/24, per ADR 0319) so that membership in wg-cp0 =
	// full mesh reach at the WireGuard layer.
	//
	// Canonical content for a Kind="cp-relay" endpoint is two entries:
	//   [MeshAddr/32, WGCP0FleetSubnet]
	// MeshAddr/32 ensures the relay's own mesh IP routes through this
	// peer block directly (e.g. for endpoint-switching latency
	// preference); the catchall covers every other peer reachable
	// through any relay.
	//
	// Issuer side (`bundle-create`): always populates AllowedIPs with
	// the canonical two-entry form on every relay endpoint. The
	// deprecated `--mesh-route-subnet` / `--first-relay-route-subnet`
	// operator flags remain as additive extras (extra CIDRs appended
	// after the canonical pair) for forward-compatibility with future
	// route extensions, but neither flag is required for the mesh to
	// work.
	//
	// Device side (`wg-cp0-bundle-apply`): renders AllowedIPs directly
	// when present; when absent (older bundles issued before ADR 0321),
	// falls back to [MeshAddr/32, WGCP0FleetSubnet] so legacy bundles
	// produce a symmetric, working mesh as well. The pre-ADR-0321
	// fallback of MeshAddr/32-only is dropped — the asymmetric default
	// is the failure mode ADR 0321 closes.
	//
	// Historical note (Block 22K.7, retired by ADR 0321): the earlier
	// `--first-relay-route-subnet` flag applied this list to the FIRST
	// relay only, which produced a fleet-wide per-bundle asymmetry —
	// peers couldn't reach each other when their bundles disagreed on
	// `/24` placement. The canonical-on-every-relay shape is
	// the durable fix.
	AllowedIPs []string `cbor:"allowed_ips,omitempty"`
}

// EnrollmentBundle is the signed capability.
//
// All fields except Signature participate in the signed payload. Signature is
// produced by the CA (YubiKey in production, age-encrypted file in dev) over
// the canonical CBOR encoding of the payload fields.
//
// Field-extension policy. Adding an OPTIONAL field (omitempty + zero value
// equivalent to "not present") is backwards-compatible at v1 — old bundles
// without the field still parse + verify, new validators reading old bundles
// see a zero value. INCOMPATIBLE changes (new required field, changed shape
// of existing field) require bumping Version.
//
// Block 61 (snitch v2) added SnitchProbeKey as such an optional
// field. Older bundles continue to parse with SnitchProbeKey == nil; the
// snitch-bundle ansible role's bundle-extract step gracefully reports
// "field absent" rather than failing.
//
// §11 fields (cp_device_pubkey, ntp_attestation_pubkey) named in
// docs/design/offline-enrollment.md §3 + §11 are owned by the Block 22
// wg-cp0 + NTP tracks respectively; they're additions to the same schema
// when those tracks land their wire-format work.
type EnrollmentBundle struct {
	Version            uint8           `cbor:"version"`
	DeviceID           string          `cbor:"device_id"`
	PeerPubkey         []byte          `cbor:"peer_pubkey"`
	ACLGroups          []string        `cbor:"acl_groups"`
	Site               string          `cbor:"site"`
	KnownEndpoints     []KnownEndpoint `cbor:"known_endpoints"`
	IssuedAt           time.Time       `cbor:"issued_at"`
	ActivationDeadline time.Time       `cbor:"activation_deadline"`
	ExpiresAt          time.Time       `cbor:"expires_at"`
	Nonce              []byte          `cbor:"nonce"`
	CAID               string          `cbor:"ca_id"`

	// SnitchProbeKey is the per-device snitch collector API key, 32 random
	// bytes generated at bundle issuance. SHA-256 of this is registered in
	// the snitch collector's api_keys table at issuance time so the device's
	// first probe POST authenticates without a separate online key-fetch.
	// Absent (nil) for bundles issued without a snitch deployment.
	// Block 61 / docs/design/offline-enrollment.md §11.6a.
	SnitchProbeKey []byte `cbor:"snitch_probe_key,omitempty"`

	// TLSTrustAnchor is the DER-encoded X.509 root cert of the internal
	// TLS CA (ADR 0407 / docs/design/internal-tls-ca.md). Peers add this
	// to their TLS root pool to verify internal-only HTTPS endpoints —
	// e.g. mgmt-API at https://198.18.0.1:443 behind wg-cp0.
	// Absent (nil) → bundle was issued without an internal TLS CA in use
	// (graceful: legacy dev/test bundles don't need this).
	// Block 22J / docs/design/offline-enrollment.md §3.
	TLSTrustAnchor []byte `cbor:"tls_trust_anchor,omitempty"`

	// CPDevicePubkey is the device's wg-cp0 (silent control plane outer
	// WireGuard wrap, ADR 0313) Curve25519 public key, 32 bytes. The
	// keypair is generated at bundle-issuance time on the air-gapped
	// workstation; the public half rides here, the private half rides
	// in CPDevicePrivkey below. Relays' wg-cp0 allowlists are populated
	// from this field at bundle-issuance time per offline-enrollment.md
	// §11.3 (Phase 2A HTTP pull / Phase 3 Spock-replicated).
	// Absent (nil) → bundle was issued without wg-cp0 in scope (legacy
	// pre-Block-22 bundles, or non-control-plane peers).
	// Block 22K / docs/design/offline-enrollment.md §11.2.
	CPDevicePubkey []byte `cbor:"cp_device_pubkey,omitempty"`

	// CPDevicePrivkey is the device's wg-cp0 Curve25519 private key, 32
	// bytes. Sensitive material — the bundle file is mode 0600 in transit
	// + at rest, and the device-side bundle-apply tool shreds the bundle
	// after extracting the privkey to /etc/wireguard/wg-cp0-priv.key
	// (mode 0600). Inside the CA signature so the (pubkey, privkey)
	// pair is integrity-bound by the offline CA, but secrecy depends on
	// transit + storage discipline (same trust model as SnitchProbeKey).
	// Absent (nil) → bundle was issued without wg-cp0 in scope. Always
	// paired with CPDevicePubkey: validators reject bundles where exactly
	// one of {CPDevicePubkey, CPDevicePrivkey} is present.
	// Block 22K / docs/design/offline-enrollment.md §11.2 amendment.
	CPDevicePrivkey []byte `cbor:"cp_device_privkey,omitempty"`

	// CPDeviceAddress is the device's wg-cp0 interface address as a CIDR
	// string (e.g. "198.18.0.6/24"). Optional; populated only when
	// CPDevicePubkey is set. The device-side wg-cp0-bundle-apply tool
	// writes this verbatim into the [Interface] Address line of the
	// rendered wg-cp0.conf. Carrying the CIDR (rather than separate IP
	// + subnet) keeps the bundle self-contained — no out-of-band subnet
	// assumption on the device side.
	// Block 22K / docs/design/offline-enrollment.md §11.2.
	CPDeviceAddress string `cbor:"cp_device_address,omitempty"`

	// InnerMeshSetup is the v0.2 extension carrying mgmt URL + setup
	// key (+ optional admin token + PSK) for the netbird inner mesh.
	// Populated when the issuing operator wants the device to run in
	// `netbird-only` or `combined` mode (per ADR 0840 Amendment
	// ). Absent for v0.1.x-shape bundles — the parser
	// treats absent as "wg-cp0-only-eligible only."
	//
	// The CBOR encoder omits the field when all sub-fields are zero
	// (omitempty on the struct value), so v1 bundles whose issuer did
	// not populate this field round-trip byte-identically through
	// new parser code. The wire-compat property is asserted in
	// TestV0_2FieldsOmitWhenEmpty so v0.1.x bundles in the wild
	// continue to verify after this rolls out.
	// Block 76 v0.2 / ADR 0840 Amendment .
	InnerMeshSetup InnerMeshSetup `cbor:"inner_mesh_setup,omitempty"`

	// MobileCert is the Block 80F per-device mTLS client cert + key
	// (PEM-bundled, leaf cert first, any intermediate chain, then
	// the private key block). Used when the device reaches mgmt +
	// signal + relay through the Block 80 / ADR 0843 public-mTLS
	// crutch tier in `netbird-only` mode. Absent for v0.1.x bundles
	// and for devices that reach the inner mesh directly without the
	// crutch.
	// Block 80F / ADR 0843.
	MobileCert []byte `cbor:"mobile_cert,omitempty"`

	// GoatCLIExtras is the goat-cli operator-profile material. Carries
	// portal + Zenoh-router endpoint pair plus the per-operator mTLS
	// cert/key/CA-roots PEM bundle so a single signed artifact lights
	// up every operator surface (portal browse, topic CRUD, pub/sub,
	// stethoscope, doctor). Inside the CA signature so endpoint set +
	// cert are integrity-bound — an attacker can't swap one endpoint
	// while keeping a valid cert. Absent for non-operator bundles
	// (device-enrollment + relay bundles); the goat-cli verifier
	// rejects with a "no goat-cli extras in bundle" hint if the
	// operator points `goat profile init` at one.
	// Block 84-ish / docs/design/goat-cli.md / ADR 1019.
	GoatCLIExtras GoatCLIExtras `cbor:"goat_cli_extras,omitempty"`

	// EnrollmentAnchor is the portal-registry slot anchor of the machine
	// this bundle was issued FROM (`bundle-create --from-machine`,
	// machine-mesh-alignment decision D6a). It ties the bundle — and
	// therefore the peer that joins on it — to the custody edge + born-as
	// node-class recorded at issuance, so a validator or the
	// provenance-guard reconcile can associate the joined peer with its
	// registry row without a portal round-trip. Absent (empty) for bundles
	// issued with hand-supplied --acl-groups (infra/site bundles) and for
	// all pre-D6a bundles — optional-field extension per the policy above,
	// so old bundles parse + verify unchanged.
	EnrollmentAnchor string `cbor:"enrollment_anchor,omitempty"`

	// NodeClass is the machine's BORN-AS node-class (ADR 1116 vocabulary)
	// as recorded in the portal registry at issuance. Pairs with
	// EnrollmentAnchor. The ACLGroups above are the DERIVATION of this
	// class — the field lets an offline consumer see WHY the groups are
	// what they are, and lets the reconcile check the derivation still
	// holds. Absent for non-registry-issued bundles.
	NodeClass string `cbor:"node_class,omitempty"`

	Signature []byte `cbor:"signature,omitempty"`
}

// InnerMeshSetup is the v0.2 nested-field shape carried inside
// EnrollmentBundle. The Go-side stable shape for the inner-mesh
// subsystem lives in goat-maintainer/goat-client's internal/innermesh and
// is derived from this via innermesh.FromBundle.
//
// Fields with zero values are omitted on the wire (the parent
// struct's omitempty drops the whole nested map when all sub-fields
// are zero). The inner-mesh eligibility test is
// (*EnrollmentBundle).HasInnerMesh — true when ManagementURL and
// SetupKey are both non-empty.
type InnerMeshSetup struct {
	// ManagementURL is the netbird management plane endpoint. Must
	// be https. Examples:
	//   https://mgmt.example.internal
	//   https://mgmt.example.internal:33073
	//   https://mgmt-crutch.example.public  (Block 80 crutch tier)
	ManagementURL string `cbor:"management_url,omitempty"`

	// SetupKey is the netbird setup-key string presented on first
	// registration.
	SetupKey string `cbor:"setup_key,omitempty"`

	// AdminAccessToken is the optional bearer token for the netbird
	// management API. Surface used by the diagnostics IPC method.
	AdminAccessToken string `cbor:"admin_access_token,omitempty"`

	// PreSharedKey is the optional WireGuard PSK passed through to
	// the inner-mesh userspace device. 32 bytes when set.
	PreSharedKey []byte `cbor:"pre_shared_key,omitempty"`

	// RosenpassEnabled is the per-goatnet PQC policy, stamped at mint
	// from the `rosenpass_enabled` group_var — the goat-client analogue
	// of the `--enable-rosenpass` enrollment flag. When true the device
	// brings its inner mesh up with Rosenpass (post-quantum) key exchange.
	// The CBOR tag MUST match the goat-client consumer
	// (internal/bundle.InnerMeshSetup) — this is the wire contract.
	RosenpassEnabled bool `cbor:"rosenpass_enabled,omitempty"`

	// RosenpassPermissive keeps connectivity to not-yet-enabled peers
	// during rollout. Only meaningful when RosenpassEnabled is true.
	RosenpassPermissive bool `cbor:"rosenpass_permissive,omitempty"`
}

// HasInnerMesh reports whether the bundle carries enough information
// to attempt the netbird inner mesh (`netbird-only` or `combined`
// modes). Tests the InnerMeshSetup nested struct rather than the
// presence of the CBOR field — both encodings (absent map / map with
// zero values) collapse to the same Go shape after Unmarshal, which
// is the property `omitempty` gives us.
func (b *EnrollmentBundle) HasInnerMesh() bool {
	return b.InnerMeshSetup.ManagementURL != "" && b.InnerMeshSetup.SetupKey != ""
}

// HasWgCp0 reports whether the bundle carries enough information to
// attempt the wg-cp0 outer tunnel (`wg-cp0-only` or `combined`
// modes). The CP device keypair must be present (32-byte halves) and
// the device address must be set.
func (b *EnrollmentBundle) HasWgCp0() bool {
	return len(b.CPDevicePubkey) == 32 &&
		len(b.CPDevicePrivkey) == 32 &&
		b.CPDeviceAddress != ""
}

// HasMobileCert reports whether the bundle carries a Block 80F
// per-device mTLS client cert + key. Required when the bundle's
// inner-mesh ManagementURL targets the Block 80 crutch tier.
func (b *EnrollmentBundle) HasMobileCert() bool {
	return len(b.MobileCert) > 0
}

// GoatCLIExtras is the nested-field shape carried inside
// EnrollmentBundle for goat-cli operator-profile material. Mirrors
// the InnerMeshSetup pattern: nested struct with all-omitempty
// sub-fields so the CBOR encoder drops the whole map when no
// goat-cli extras are populated.
//
// The eligibility test is (*EnrollmentBundle).HasGoatCLIExtras —
// true when every sub-field required to light up the operator
// surfaces is present (endpoints + cert + key + CA roots + token).
// A partial population (e.g. cert but no key) is a bundle-create
// programming error; callers should not see it on the wire.
type GoatCLIExtras struct {
	// PortalEndpoint is the operator-facing portal URL, e.g.
	// https://portal.nova.netbird.nova.example. Used by goat-cli
	// to mint requests against the portal REST API (vendor browse,
	// topic CRUD, audit tail).
	PortalEndpoint string `cbor:"portal_endpoint,omitempty"`

	// PortalToken is the opaque Bearer API key for the portal REST
	// API. Issued per operator (not per session); bcrypt'd in the
	// portal side, so this is the only place the cleartext exists.
	// Carried inside the CA signature so token swap requires a fresh
	// bundle re-sign by the CA, not just an in-flight tamper.
	PortalToken []byte `cbor:"portal_token,omitempty"`
	// RouterEndpoint is the Zenoh router locator the goat-cli pub/sub
	// + topic + doctor surfaces connect to. Form is the Zenoh wire
	// locator (e.g. tls/100.127.0.190:7447); goat-cli passes it
	// verbatim to the Zenoh session config.
	RouterEndpoint string `cbor:"router_endpoint,omitempty"`

	// OnboardEndpoint is the OPTIONAL onboarding-engine URL, e.g.
	// https://onboard.zagros.netbird.zagros.example. The onboarding engine
	// moved from the portal to goat-id-console
	// (docs/design/onboarding-extraction-plan.md), so `goat admin invite`/`queue`
	// target this endpoint for /api/onboard/* while the portal endpoint keeps
	// the cert-mint + whoami. Deliberately NOT part of HasGoatCLIExtras: a
	// goatnet that has not stood up the onboard route simply omits it, and the
	// CLI falls back to the portal endpoint (back-compat). goat-cli writes it to
	// the profile's `onboard_endpoint` on `profile init` when present.
	OnboardEndpoint string `cbor:"onboard_endpoint,omitempty"`

	// MTLSCert is the per-operator client cert (PEM, leaf-first if a
	// chain is present). Issued at operator-onboard time against the
	// trial-fabric ACL so the operator's URI-SAN matches the Zenoh
	// router's access-control map. Same cert is presented to both
	// the portal (mTLS layer) and the Zenoh router.
	MTLSCert []byte `cbor:"mtls_cert,omitempty"`

	// MTLSKey is the per-operator client private key (PEM). Sensitive
	// material — the bundle file is mode 0600 in transit + at rest;
	// goat-cli writes it to ~/.goat/contexts/<name>/mtls.key.pem
	// mode 0600 on `profile init`.
	MTLSKey []byte `cbor:"mtls_key,omitempty"`

	// CARoots is the PEM bundle of CA roots goat-cli uses to verify
	// the portal + Zenoh router server certs. One or more roots
	// concatenated; goat-cli loads them into a rustls RootCertStore
	// on profile init.
	CARoots []byte `cbor:"ca_roots,omitempty"`
}

// HasGoatCLIExtras reports whether the bundle carries enough
// operator-profile material to drive `goat profile init`. Tests the
// minimum useful set: both endpoints + cert + key + CA roots + token.
// A partial-population case (e.g. cert without key) returns false —
// `goat profile init` then surfaces a "no goat-cli extras in bundle"
// hint rather than half-installing a broken profile.
func (b *EnrollmentBundle) HasGoatCLIExtras() bool {
	x := b.GoatCLIExtras
	return x.PortalEndpoint != "" &&
		x.RouterEndpoint != "" &&
		len(x.PortalToken) > 0 &&
		len(x.MTLSCert) > 0 &&
		len(x.MTLSKey) > 0 &&
		len(x.CARoots) > 0
}

// canonicalEnc returns a CBOR encoder configured for deterministic output.
// Uses CTAP2 canonical form: shortest integer encoding, sorted map keys,
// definite-length containers. The same bundle always encodes to the same
// bytes — required for signing.
func canonicalEnc() (cbor.EncMode, error) {
	return cbor.CanonicalEncOptions().EncMode()
}

// Signable returns the byte sequence that the CA signs. It is the canonical
// CBOR encoding of the bundle with the Signature field cleared.
func (b *EnrollmentBundle) Signable() ([]byte, error) {
	em, err := canonicalEnc()
	if err != nil {
		return nil, fmt.Errorf("cbor canonical enc mode: %w", err)
	}
	unsigned := *b
	unsigned.Signature = nil
	return em.Marshal(&unsigned)
}

// Marshal returns the full canonical CBOR encoding including Signature.
func (b *EnrollmentBundle) Marshal() ([]byte, error) {
	em, err := canonicalEnc()
	if err != nil {
		return nil, fmt.Errorf("cbor canonical enc mode: %w", err)
	}
	return em.Marshal(b)
}

// Unmarshal parses a CBOR-encoded bundle. The caller must still Verify the
// signature and check expiry, activation deadline, and CRL before trusting.
func Unmarshal(data []byte) (*EnrollmentBundle, error) {
	var b EnrollmentBundle
	if err := cbor.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("cbor unmarshal: %w", err)
	}
	return &b, nil
}

// Sign produces a signature over the canonical payload using the given
// signing function and stores it on the bundle. The sign parameter abstracts
// over the CA backend (age-encrypted file in dev, YubiKey in production) so
// callers do not need to depend on a specific implementation here.
//
// Block 79: signatures are ASN.1 DER ECDSA-Sig-Value envelopes over
// SHA-256(canonical CBOR bytes). Length is variable; bounded sanity-check
// only ensures the value is non-empty and not absurdly large.
func (b *EnrollmentBundle) Sign(sign func([]byte) ([]byte, error)) error {
	payload, err := b.Signable()
	if err != nil {
		return fmt.Errorf("signable: %w", err)
	}
	sig, err := sign(payload)
	if err != nil {
		return fmt.Errorf("signer: %w", err)
	}
	if len(sig) == 0 || len(sig) > 256 {
		return fmt.Errorf("signature wrong size: got %d want 1-256 (ECDSA ASN.1 DER)", len(sig))
	}
	b.Signature = sig
	return nil
}

// Verify checks the bundle's ECDSA P-256 signature against a trusted public
// key. Does not check expiry, activation deadline, or the CRL — those are
// the caller's responsibility.
func (b *EnrollmentBundle) Verify(pub *ecdsa.PublicKey) error {
	if pub == nil {
		return errors.New("public key is nil")
	}
	if pub.Curve.Params().Name != "P-256" {
		return fmt.Errorf("public key curve is %s, want P-256", pub.Curve.Params().Name)
	}
	if len(b.Signature) == 0 {
		return errors.New("signature is empty")
	}
	payload, err := b.Signable()
	if err != nil {
		return fmt.Errorf("signable: %w", err)
	}
	digest := sha256.Sum256(payload)
	if !ecdsa.VerifyASN1(pub, digest[:], b.Signature) {
		return errors.New("signature invalid")
	}
	return nil
}

// ErrExpired is returned when ExpiresAt is in the past.
var ErrExpired = errors.New("bundle expired")

// ErrActivationDeadlinePassed is returned when first enrollment is requested
// after the activation deadline. Independent of ExpiresAt — a bundle can be
// past its activation deadline while still within its peer-record horizon.
var ErrActivationDeadlinePassed = errors.New("bundle activation deadline passed")

// CheckExpiry returns ErrExpired if now is after ExpiresAt.
func (b *EnrollmentBundle) CheckExpiry(now time.Time) error {
	if now.After(b.ExpiresAt) {
		return fmt.Errorf("%w: expires_at=%s now=%s", ErrExpired,
			b.ExpiresAt.UTC().Format(time.RFC3339),
			now.UTC().Format(time.RFC3339))
	}
	return nil
}

// CheckActivationDeadline returns ErrActivationDeadlinePassed if now is after
// ActivationDeadline. Validators call this on first-enrollment attempts to
// bound the bundle-in-flight theft window.
func (b *EnrollmentBundle) CheckActivationDeadline(now time.Time) error {
	if now.After(b.ActivationDeadline) {
		return fmt.Errorf("%w: activation_deadline=%s now=%s", ErrActivationDeadlinePassed,
			b.ActivationDeadline.UTC().Format(time.RFC3339),
			now.UTC().Format(time.RFC3339))
	}
	return nil
}

// ErrCPDeviceKeypairUnpaired is returned when exactly one of CPDevicePubkey
// or CPDevicePrivkey is present. Both fields must be present together (wg-cp0
// bundle) or both absent (legacy / non-control-plane peer).
var ErrCPDeviceKeypairUnpaired = errors.New("cp_device_pubkey/cp_device_privkey must be present together or both absent")

// CheckCPDeviceKeypair returns ErrCPDeviceKeypairUnpaired if exactly one
// of CPDevicePubkey or CPDevicePrivkey is present. Issuance + validation
// callers should invoke this before trusting the wg-cp0 fields.
func (b *EnrollmentBundle) CheckCPDeviceKeypair() error {
	pub := len(b.CPDevicePubkey) > 0
	priv := len(b.CPDevicePrivkey) > 0
	if pub != priv {
		return ErrCPDeviceKeypairUnpaired
	}
	return nil
}
