// Package attest signs and verifies ntp-health-agent verdicts (Block 12D,
// increment 2a). The signature lets any consumer verify "host X asserted
// verdict V, valid until T" without trusting the transport — the precondition
// for the cross-site peer consensus (universal-ntp-hygiene.md §5) and for the
// @/ntp-health bus feed published by the increment-2b sidecar.
//
// Signing covers a fixed, versioned subset of the verdict (see signingView):
// the host, the class verdict, the selected source + stratum + ref-id, the RMS
// offset, the verdict timestamp, and the validity window. Volatile / not-yet-
// active fields (drift model, system offset, freq, leap) are deliberately
// excluded — the signature attests the verdict and its time, the load-bearing
// claim for consensus. The Alg tag versions the covered set so a v2 can widen
// it without a silent signature mismatch.
package attest

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/usareur-af/goatcore/ops/time-hygiene/internal/canonical"
	"github.com/usareur-af/goatcore/ops/time-hygiene/internal/signer"
	"github.com/usareur-af/goatcore/ops/time-hygiene/internal/state"
)

// Alg versions the canonical signing view below. A consumer that does not
// recognize the alg must treat the attestation as unverifiable, not invalid.
const Alg = "ntp-attest-v1"

// signingView is the exact, ordered set of fields covered by the signature.
// Timestamps are RFC3339Nano UTC strings (not time.Time) so the byte form is
// reproducible on any verifier with no monotonic-clock or precision drift —
// the load-bearing determinism property the signature depends on.
type signingView struct {
	Alg            string `json:"alg"`
	HostID         string `json:"host_id"`
	NtpClassStr    string `json:"ntp_class_str"`
	SelectedSource string `json:"selected_source"`
	Stratum        uint16 `json:"stratum"`
	RefID          string `json:"ref_id"`
	RmsOffsetNs    int64  `json:"rms_offset_ns"`
	LastUpdatedUTC string `json:"last_updated_utc"`
	ValidUntil     string `json:"valid_until"`
}

func viewOf(s state.NtpState) signingView {
	return signingView{
		Alg:            Alg,
		HostID:         s.HostID,
		NtpClassStr:    s.NtpClassStr,
		SelectedSource: string(s.SelectedSource),
		Stratum:        s.Stratum,
		RefID:          s.RefID,
		RmsOffsetNs:    s.RmsOffsetNs,
		LastUpdatedUTC: s.LastUpdatedUTC.UTC().Format(time.RFC3339Nano),
		ValidUntil:     s.ValidUntil.UTC().Format(time.RFC3339Nano),
	}
}

// signingBytes is the deterministic byte form fed to Ed25519. Both Sign and
// Verify route through it so the produced and checked bytes can never diverge.
func signingBytes(s state.NtpState) ([]byte, error) {
	return canonical.Marshal(viewOf(s))
}

// Sign sets s.ValidUntil (= LastUpdatedUTC + validity) and, when sgn is
// non-nil, the Ed25519 Signature + AttestationPubkey + AttestationAlg, marking
// Attested. With a nil signer the verdict is served honestly unattested
// (Attested=false, no signature) — the unattested-publisher posture of
// universal-ntp-hygiene.md §2. ValidUntil is always set so the window is
// advisory even when unsigned.
func Sign(s *state.NtpState, sgn *signer.Signer, validity time.Duration) error {
	s.ValidUntil = s.LastUpdatedUTC.Add(validity).UTC()
	if sgn == nil {
		s.Attested = false
		s.AttestationAlg = ""
		s.AttestationPubkey = ""
		s.Signature = ""
		return nil
	}
	msg, err := signingBytes(*s)
	if err != nil {
		return err
	}
	s.Signature = base64.StdEncoding.EncodeToString(sgn.Sign(msg))
	s.AttestationPubkey = base64.StdEncoding.EncodeToString(sgn.PublicKey())
	s.AttestationAlg = Alg
	s.Attested = true
	return nil
}

// Verify reports whether s carries a signature that is valid over its own
// embedded public key. It does NOT establish trust in that key — the caller
// roots key authenticity in the offline-CA-signed enrollment bundle
// (universal-ntp-hygiene.md §4.5). Returns (false, nil) for an unattested
// verdict; an error only for a malformed attestation (bad alg / encoding).
func Verify(s state.NtpState) (bool, error) {
	if !s.Attested {
		return false, nil
	}
	if s.AttestationAlg != Alg {
		return false, fmt.Errorf("unrecognized attestation alg %q", s.AttestationAlg)
	}
	pub, err := base64.StdEncoding.DecodeString(s.AttestationPubkey)
	if err != nil {
		return false, fmt.Errorf("decode pubkey: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return false, fmt.Errorf("pubkey length %d, want %d", len(pub), ed25519.PublicKeySize)
	}
	sig, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil {
		return false, fmt.Errorf("decode signature: %w", err)
	}
	msg, err := signingBytes(s)
	if err != nil {
		return false, err
	}
	return ed25519.Verify(ed25519.PublicKey(pub), msg, sig), nil
}

// Expired reports whether the verdict's signed validity window has closed at
// now. A consumer must treat an expired attestation as stale even when the
// signature verifies — staleness, not just forgery, is a failure mode.
func Expired(s state.NtpState, now time.Time) bool {
	return now.After(s.ValidUntil)
}
