package main

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildInnerMeshSetup_NoFlags returns the zero value when no inner-mesh
// flags are set. CBOR omitempty will drop the field on the wire so v0.1.x
// bundles round-trip byte-identically through new parser code.
func TestBuildInnerMeshSetup_NoFlags(t *testing.T) {
	got, err := buildInnerMeshSetup(runArgs{})
	if err != nil {
		t.Fatalf("buildInnerMeshSetup: %v", err)
	}
	if got.ManagementURL != "" || got.SetupKey != "" ||
		got.AdminAccessToken != "" || len(got.PreSharedKey) != 0 {
		t.Errorf("expected zero value, got %+v", got)
	}
}

// TestBuildInnerMeshSetup_HappyPath_All — every flag populated, including
// a base64 PSK that decodes to exactly 32 bytes.
func TestBuildInnerMeshSetup_HappyPath_All(t *testing.T) {
	psk := make([]byte, 32)
	for i := range psk {
		psk[i] = byte(i)
	}
	pskB64 := base64.StdEncoding.EncodeToString(psk)
	got, err := buildInnerMeshSetup(runArgs{
		InnerMeshMgmtURL:    "https://mgmt.example.internal",
		InnerMeshSetupKey:   "abcd-efgh-ijkl",
		InnerMeshAdminToken: "Bearer-xyz",
		InnerMeshPSKB64:     pskB64,
	})
	if err != nil {
		t.Fatalf("buildInnerMeshSetup: %v", err)
	}
	if got.ManagementURL != "https://mgmt.example.internal" {
		t.Errorf("mgmt URL=%q", got.ManagementURL)
	}
	if got.SetupKey != "abcd-efgh-ijkl" {
		t.Errorf("setup key=%q", got.SetupKey)
	}
	if got.AdminAccessToken != "Bearer-xyz" {
		t.Errorf("admin token=%q", got.AdminAccessToken)
	}
	if len(got.PreSharedKey) != 32 {
		t.Errorf("PSK len=%d want 32", len(got.PreSharedKey))
	}
	for i := range psk {
		if got.PreSharedKey[i] != psk[i] {
			t.Errorf("PSK[%d]=%d want %d", i, got.PreSharedKey[i], psk[i])
			break
		}
	}
}

// TestBuildInnerMeshSetup_HappyPath_Minimal — only the required pair is
// set. Optional fields stay zero.
func TestBuildInnerMeshSetup_HappyPath_Minimal(t *testing.T) {
	got, err := buildInnerMeshSetup(runArgs{
		InnerMeshMgmtURL:  "https://mgmt.example.internal",
		InnerMeshSetupKey: "abcd-efgh-ijkl",
	})
	if err != nil {
		t.Fatalf("buildInnerMeshSetup: %v", err)
	}
	if got.AdminAccessToken != "" || len(got.PreSharedKey) != 0 {
		t.Errorf("expected optional fields empty, got %+v", got)
	}
}

// TestBuildInnerMeshSetup_RequiresMgmtURL — setting any other flag without
// mgmt-url fails.
func TestBuildInnerMeshSetup_RequiresMgmtURL(t *testing.T) {
	_, err := buildInnerMeshSetup(runArgs{
		InnerMeshSetupKey: "abcd-efgh-ijkl",
	})
	if err == nil || !strings.Contains(err.Error(), "inner-mesh-mgmt-url is required") {
		t.Errorf("expected mgmt-url required error, got %v", err)
	}
}

// TestBuildInnerMeshSetup_RequiresSetupKey — setting any other flag
// without setup-key fails.
func TestBuildInnerMeshSetup_RequiresSetupKey(t *testing.T) {
	_, err := buildInnerMeshSetup(runArgs{
		InnerMeshMgmtURL: "https://mgmt.example.internal",
	})
	if err == nil || !strings.Contains(err.Error(), "inner-mesh-setup-key is required") {
		t.Errorf("expected setup-key required error, got %v", err)
	}
}

// TestBuildInnerMeshSetup_RejectsNonHTTPS — http:// mgmt URL is refused.
func TestBuildInnerMeshSetup_RejectsNonHTTPS(t *testing.T) {
	_, err := buildInnerMeshSetup(runArgs{
		InnerMeshMgmtURL:  "http://mgmt.example.internal",
		InnerMeshSetupKey: "abcd-efgh-ijkl",
	})
	if err == nil || !strings.Contains(err.Error(), "must be https") {
		t.Errorf("expected https-required error, got %v", err)
	}
}

// TestBuildInnerMeshSetup_RejectsBadPSKLength — base64 that decodes to
// !=32 bytes is refused.
func TestBuildInnerMeshSetup_RejectsBadPSKLength(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	_, err := buildInnerMeshSetup(runArgs{
		InnerMeshMgmtURL:  "https://mgmt.example.internal",
		InnerMeshSetupKey: "abcd-efgh-ijkl",
		InnerMeshPSKB64:   short,
	})
	if err == nil || !strings.Contains(err.Error(), "must decode to 32 bytes") {
		t.Errorf("expected PSK length error, got %v", err)
	}
}

// TestBuildInnerMeshSetup_RejectsBadPSKBase64 — non-base64 PSK is refused
// with a wrapped decode error.
func TestBuildInnerMeshSetup_RejectsBadPSKBase64(t *testing.T) {
	_, err := buildInnerMeshSetup(runArgs{
		InnerMeshMgmtURL:  "https://mgmt.example.internal",
		InnerMeshSetupKey: "abcd-efgh-ijkl",
		InnerMeshPSKB64:   "!!!not-base64!!!",
	})
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("expected PSK decode error, got %v", err)
	}
}

// TestBuildInnerMeshSetup_RosenpassOn — enabling Rosenpass stamps
// RosenpassEnabled and (permissive defaulting true) RosenpassPermissive.
func TestBuildInnerMeshSetup_RosenpassOn(t *testing.T) {
	got, err := buildInnerMeshSetup(runArgs{
		InnerMeshMgmtURL:             "https://mgmt.example.internal",
		InnerMeshSetupKey:            "abcd-efgh-ijkl",
		InnerMeshEnableRosenpass:     true,
		InnerMeshRosenpassPermissive: true,
	})
	if err != nil {
		t.Fatalf("buildInnerMeshSetup: %v", err)
	}
	if !got.RosenpassEnabled || !got.RosenpassPermissive {
		t.Errorf("enabled=%t permissive=%t, want true/true", got.RosenpassEnabled, got.RosenpassPermissive)
	}
}

// TestBuildInnerMeshSetup_RosenpassStrict — enabling with permissive=false
// stamps RosenpassEnabled but leaves RosenpassPermissive false (enforce).
func TestBuildInnerMeshSetup_RosenpassStrict(t *testing.T) {
	got, err := buildInnerMeshSetup(runArgs{
		InnerMeshMgmtURL:             "https://mgmt.example.internal",
		InnerMeshSetupKey:            "abcd-efgh-ijkl",
		InnerMeshEnableRosenpass:     true,
		InnerMeshRosenpassPermissive: false,
	})
	if err != nil {
		t.Fatalf("buildInnerMeshSetup: %v", err)
	}
	if !got.RosenpassEnabled || got.RosenpassPermissive {
		t.Errorf("enabled=%t permissive=%t, want true/false", got.RosenpassEnabled, got.RosenpassPermissive)
	}
}

// TestBuildInnerMeshSetup_RosenpassOffLeavesFieldsZero — the default (no
// enable flag) leaves both rosenpass fields false so CBOR omitempty drops
// them and a non-PQC bundle stays byte-clean.
func TestBuildInnerMeshSetup_RosenpassOffLeavesFieldsZero(t *testing.T) {
	got, err := buildInnerMeshSetup(runArgs{
		InnerMeshMgmtURL:             "https://mgmt.example.internal",
		InnerMeshSetupKey:            "abcd-efgh-ijkl",
		InnerMeshRosenpassPermissive: true, // default; must NOT stamp without enable
	})
	if err != nil {
		t.Fatalf("buildInnerMeshSetup: %v", err)
	}
	if got.RosenpassEnabled || got.RosenpassPermissive {
		t.Errorf("enabled=%t permissive=%t, want false/false", got.RosenpassEnabled, got.RosenpassPermissive)
	}
}

// TestBuildInnerMeshSetup_RosenpassRequiresMgmt — enabling Rosenpass alone
// (no mgmt-url) fails loudly rather than silently dropping the intent,
// because the enable flag participates in the anySet gate.
func TestBuildInnerMeshSetup_RosenpassRequiresMgmt(t *testing.T) {
	_, err := buildInnerMeshSetup(runArgs{
		InnerMeshEnableRosenpass: true,
	})
	if err == nil || !strings.Contains(err.Error(), "inner-mesh-mgmt-url is required") {
		t.Errorf("expected mgmt-url required error, got %v", err)
	}
}

// TestLoadMobileCertIfSet_Empty — no path means no cert.
func TestLoadMobileCertIfSet_Empty(t *testing.T) {
	got, err := loadMobileCertIfSet("")
	if err != nil {
		t.Fatalf("loadMobileCertIfSet: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil cert for empty path, got %d bytes", len(got))
	}
}

// TestLoadMobileCertIfSet_RejectsCertOnly — leaf cert without a key block
// is rejected (the device-side parser needs both).
func TestLoadMobileCertIfSet_RejectsCertOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cert-only.pem")
	if err := os.WriteFile(path, []byte(certOnlyPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadMobileCertIfSet(path)
	if err == nil || !strings.Contains(err.Error(), "no PRIVATE KEY block") {
		t.Errorf("expected key-required error, got %v", err)
	}
}

// TestLoadMobileCertIfSet_RejectsKeyOnly — private key without a cert
// block is rejected.
func TestLoadMobileCertIfSet_RejectsKeyOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key-only.pem")
	if err := os.WriteFile(path, []byte(keyOnlyPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadMobileCertIfSet(path)
	if err == nil || !strings.Contains(err.Error(), "no CERTIFICATE block") {
		t.Errorf("expected cert-required error, got %v", err)
	}
}

// TestLoadMobileCertIfSet_AcceptsLeafPlusKey — both blocks present
// (PRIVATE KEY type accepted).
func TestLoadMobileCertIfSet_AcceptsLeafPlusKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.pem")
	if err := os.WriteFile(path, []byte(certOnlyPEM+keyOnlyPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadMobileCertIfSet(path)
	if err != nil {
		t.Fatalf("loadMobileCertIfSet: %v", err)
	}
	if len(got) == 0 {
		t.Errorf("expected non-empty bytes")
	}
}

// TestLoadMobileCertIfSet_FileNotFound returns the os.IsNotExist error
// wrapped so the bundle-create caller can surface it.
func TestLoadMobileCertIfSet_FileNotFound(t *testing.T) {
	_, err := loadMobileCertIfSet("/nonexistent/path/to/cert.pem")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !os.IsNotExist(err) && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.IsNotExist, got %v", err)
	}
}

// Synthetic PEM fixtures — these are not valid certs cryptographically;
// the helper only checks block presence + type, not the body.
const certOnlyPEM = `-----BEGIN CERTIFICATE-----
MIIBdjCCAR2gAwIBAgIUYzPlAceCXqJUUUUUUUUUUUUUUUUwCgYIKoZIzj0EAwIw
-----END CERTIFICATE-----
`

const keyOnlyPEM = `-----BEGIN PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgxxxxxxxxxxxxxxxx
-----END PRIVATE KEY-----
`
