package attest

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/time-hygiene/internal/signer"
	"github.com/usareur-af/goatcore/ops/time-hygiene/internal/state"
)

func newSigner(t *testing.T) *signer.Signer {
	t.Helper()
	s, _, err := signer.Generate(filepath.Join(t.TempDir(), "key.pem"))
	if err != nil {
		t.Fatalf("signer.Generate: %v", err)
	}
	return s
}

func sampleState() state.NtpState {
	return state.NtpState{
		HostID:         "vega-mgmt-01",
		NtpClass:       state.ClassHealthy,
		NtpClassStr:    state.ClassHealthy.String(),
		SelectedSource: state.SourceGNSS,
		Stratum:        2,
		RefID:          "GPS",
		RmsOffsetNs:    187000,
		LastUpdatedUTC: time.Now().UTC(),
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	sgn := newSigner(t)
	s := sampleState()
	last := s.LastUpdatedUTC

	if err := Sign(&s, sgn, 5*time.Minute); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !s.Attested {
		t.Fatal("Attested = false after signing")
	}
	if s.AttestationAlg != Alg {
		t.Errorf("AttestationAlg = %q, want %q", s.AttestationAlg, Alg)
	}
	if s.Signature == "" || s.AttestationPubkey == "" {
		t.Fatal("signature/pubkey empty after signing")
	}
	if want := last.Add(5 * time.Minute); !s.ValidUntil.Equal(want) {
		t.Errorf("ValidUntil = %v, want %v", s.ValidUntil, want)
	}

	ok, err := Verify(s)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("Verify = false for a freshly-signed verdict")
	}
}

// The decisive test: a verdict that has round-tripped through JSON (the exact
// path the bus feed + the :9753/health consumer take) must still verify. This
// catches any monotonic-clock or RFC3339Nano precision drift between the
// signed bytes and the re-marshaled bytes.
func TestJSONRoundTripVerifies(t *testing.T) {
	sgn := newSigner(t)
	s := sampleState()
	if err := Sign(&s, sgn, 5*time.Minute); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got state.NtpState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	ok, err := Verify(got)
	if err != nil {
		t.Fatalf("Verify after round-trip: %v", err)
	}
	if !ok {
		t.Fatalf("Verify = false after JSON round-trip\n  json=%s", b)
	}
}

func TestVerifyDetectsTamper(t *testing.T) {
	sgn := newSigner(t)
	cases := []struct {
		name   string
		mutate func(*state.NtpState)
	}{
		{"class", func(s *state.NtpState) { s.NtpClassStr = "degraded" }},
		{"host", func(s *state.NtpState) { s.HostID = "evil-host" }},
		{"stratum", func(s *state.NtpState) { s.Stratum = 9 }},
		{"ref_id", func(s *state.NtpState) { s.RefID = "SPOOF" }},
		{"rms_offset", func(s *state.NtpState) { s.RmsOffsetNs = 999 }},
		{"source", func(s *state.NtpState) { s.SelectedSource = state.SourceOrphan }},
		{"valid_until", func(s *state.NtpState) { s.ValidUntil = s.ValidUntil.Add(time.Hour) }},
		{"last_updated", func(s *state.NtpState) { s.LastUpdatedUTC = s.LastUpdatedUTC.Add(time.Hour) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := sampleState()
			if err := Sign(&s, sgn, time.Minute); err != nil {
				t.Fatalf("Sign: %v", err)
			}
			tc.mutate(&s)
			ok, err := Verify(s)
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if ok {
				t.Errorf("Verify = true after tampering %s", tc.name)
			}
		})
	}
}

func TestUnattested(t *testing.T) {
	s := sampleState()
	if err := Sign(&s, nil, time.Minute); err != nil {
		t.Fatalf("Sign(nil): %v", err)
	}
	if s.Attested {
		t.Error("Attested = true with a nil signer")
	}
	if s.Signature != "" || s.AttestationPubkey != "" || s.AttestationAlg != "" {
		t.Error("signature fields set with a nil signer")
	}
	if s.ValidUntil.IsZero() {
		t.Error("ValidUntil should be set even when unattested")
	}
	ok, err := Verify(s)
	if err != nil {
		t.Errorf("Verify(unattested) returned error: %v", err)
	}
	if ok {
		t.Error("Verify(unattested) = true")
	}
}

func TestVerifyWrongKey(t *testing.T) {
	s := sampleState()
	if err := Sign(&s, newSigner(t), time.Minute); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	// Replace the embedded pubkey with a different valid key's public half.
	other := newSigner(t)
	otherState := sampleState()
	if err := Sign(&otherState, other, time.Minute); err != nil {
		t.Fatalf("Sign other: %v", err)
	}
	s.AttestationPubkey = otherState.AttestationPubkey
	ok, err := Verify(s)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Error("Verify = true with a swapped public key")
	}
}

func TestVerifyUnknownAlg(t *testing.T) {
	s := sampleState()
	if err := Sign(&s, newSigner(t), time.Minute); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	s.AttestationAlg = "ntp-attest-v99"
	if _, err := Verify(s); err == nil {
		t.Error("Verify accepted an unknown alg without error")
	}
}

func TestExpired(t *testing.T) {
	s := sampleState()
	s.LastUpdatedUTC = time.Now().UTC().Add(-10 * time.Minute)
	if err := Sign(&s, newSigner(t), 5*time.Minute); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	now := time.Now().UTC()
	if !Expired(s, now) {
		t.Error("Expired = false for a verdict whose window closed 5 minutes ago")
	}
	if Expired(s, s.ValidUntil.Add(-time.Second)) {
		t.Error("Expired = true one second before the window closes")
	}
}
