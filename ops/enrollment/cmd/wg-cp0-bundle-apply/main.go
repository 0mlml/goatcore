// Command wg-cp0-bundle-apply is the device-side counterpart to
// bundle-create. It consumes a CA-signed CBOR bundle on the device,
// verifies the signature against the offline CA's PEM public key,
// checks expiry + activation deadline, validates the wg-cp0 keypair
// pairing invariant, and renders /etc/wireguard/wg-cp0.conf plus
// /etc/wireguard/wg-cp0-priv.key from the bundle's wg-cp0 fields.
//
// Block 22K. Closes the device-side half of F-053.
//
// Discipline:
//   - The bundle file MUST be mode 0600 throughout its lifetime on the
//     device. The Ansible role calling this tool is responsible for
//     `shred -u` immediately after this tool succeeds.
//   - The output privkey file is written with mode 0600 atomically
//     (write to .tmp + rename). The conf file is mode 0600 (it
//     references the privkey path by name; on Linux wg-quick reads
//     PrivateKey inline, so the conf itself contains the secret).
//   - The signature verification step is mandatory; this tool refuses
//     to render any output if Verify() fails.
//   - On hard failure (signature/expiry/pairing/keypair-mismatch), the
//     tool exits non-zero WITHOUT leaving partial output. Renaming
//     happens only after every check passes.
//
// Usage:
//
//   wg-cp0-bundle-apply \
//       --in /etc/goat/bundle.cbor \
//       --ca-pubkey /etc/goat/ca.pub \
//       --out-conf /etc/wireguard/wg-cp0.conf \
//       --out-privkey /etc/wireguard/wg-cp0-priv.key \
//       [--interface-name wg-cp0] \
//       [--persistent-keepalive 25]
//
// Exit codes:
//   0  bundle applied successfully
//   1  CLI usage error / file I/O
//   2  bundle signature/expiry/pairing/keypair-mismatch failure
//   3  bundle has no wg-cp0 fields (legacy / non-cp peer; nothing to apply)
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"golang.org/x/crypto/curve25519"

	"github.com/usareur-af/goatcore/ops/enrollment/bundle"
)

const (
	defaultInterfaceName       = "wg-cp0"
	defaultPersistentKeepalive = 25
	wgPubkeySize               = 32
)

func main() {
	in := flag.String("in", "", "Path to CBOR-encoded EnrollmentBundle (mode 0600 expected). Required.")
	caPubPath := flag.String("ca-pubkey", "", "Path to PEM-encoded CA trust anchor. Accepts either the raw SPKI PUBLIC KEY (ops/enrollment/public-keys/<ca-id>.pem) or the X.509 CERTIFICATE (ops/enrollment/ca/dev/root-cert.pem). Required.")
	outConf := flag.String("out-conf", "", "Output path for rendered wg-cp0.conf (mode 0600). Required.")
	outPrivkey := flag.String("out-privkey", "", "Output path for wg-cp0 privkey (base64, single line, mode 0600). Required.")
	interfaceName := flag.String("interface-name", defaultInterfaceName, "WireGuard interface name; informational, surfaces in conf comments.")
	persistentKeepalive := flag.Int("persistent-keepalive", defaultPersistentKeepalive, "PersistentKeepalive seconds for each cp-relay peer.")
	dryRun := flag.Bool("dry-run", false, "Validate the bundle and print the rendered conf to stdout without writing files. Useful for ceremony rehearsal.")
	flag.Parse()

	args := runArgs{
		InPath:              *in,
		CAPubPath:           *caPubPath,
		OutConfPath:         *outConf,
		OutPrivkeyPath:      *outPrivkey,
		InterfaceName:       *interfaceName,
		PersistentKeepalive: *persistentKeepalive,
		DryRun:              *dryRun,
		Now:                 time.Now().UTC(),
	}
	exitCode := run(args, os.Stdout, os.Stderr)
	os.Exit(exitCode)
}

type runArgs struct {
	InPath              string
	CAPubPath           string
	OutConfPath         string
	OutPrivkeyPath      string
	InterfaceName       string
	PersistentKeepalive int
	DryRun              bool
	Now                 time.Time
}

// run is the test-friendly entry point. Returns the exit code; emits
// human-readable messages to errOut and (on dry-run) the rendered conf
// to stdOut.
func run(args runArgs, stdOut, errOut *os.File) int {
	if err := validateArgs(args); err != nil {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: %v\n", err)
		return 1
	}

	caPub, err := loadCAPubkey(args.CAPubPath)
	if err != nil {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: load ca pubkey: %v\n", err)
		return 1
	}

	data, err := os.ReadFile(args.InPath)
	if err != nil {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: read bundle: %v\n", err)
		return 1
	}
	b, err := bundle.Unmarshal(data)
	if err != nil {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: parse bundle: %v\n", err)
		return 1
	}

	if err := b.Verify(caPub); err != nil {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: signature verify failed: %v\n", err)
		return 2
	}
	if err := b.CheckExpiry(args.Now); err != nil {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: %v\n", err)
		return 2
	}
	// Activation deadline is enforced by the mgmt-host validator on FIRST
	// netbird enrollment; for wg-cp0-bundle-apply we apply only on every
	// re-run (idempotent) so we do NOT enforce activation here. The
	// trade-off: a bundle past its activation deadline can still bring up
	// wg-cp0, which is correct because wg-cp0 is the long-lived control-
	// plane path that must come back up after reboots until the bundle
	// fully expires (CheckExpiry above).

	if err := b.CheckCPDeviceKeypair(); err != nil {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: %v\n", err)
		return 2
	}
	if len(b.CPDevicePubkey) == 0 {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: bundle has no wg-cp0 fields; nothing to apply (legacy or non-cp peer)\n")
		return 3
	}
	if b.CPDeviceAddress == "" {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: bundle has cp_device_pubkey but no cp_device_address\n")
		return 2
	}

	// Sanity: derive pubkey from privkey and confirm it matches.
	derived, err := curve25519.X25519(b.CPDevicePrivkey, curve25519.Basepoint)
	if err != nil {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: derive pubkey from privkey: %v\n", err)
		return 2
	}
	if !equalBytes(derived, b.CPDevicePubkey) {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: bundle keypair is broken (pubkey != derive(privkey))\n")
		return 2
	}

	cpRelays, err := selectCPRelays(b)
	if err != nil {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: %v\n", err)
		return 2
	}

	conf := renderConf(b, cpRelays, args.InterfaceName, args.PersistentKeepalive)
	privkey := base64.StdEncoding.EncodeToString(b.CPDevicePrivkey) + "\n"

	if args.DryRun {
		fmt.Fprintf(errOut, "DRY RUN — would write %s + %s\n\n", args.OutConfPath, args.OutPrivkeyPath)
		fmt.Fprintf(stdOut, "%s", conf)
		emitSummary(errOut, b, len(cpRelays), args.OutConfPath, args.OutPrivkeyPath, true)
		return 0
	}

	if err := atomicWrite(args.OutPrivkeyPath, []byte(privkey), 0o600); err != nil {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: write privkey: %v\n", err)
		return 1
	}
	if err := atomicWrite(args.OutConfPath, []byte(conf), 0o600); err != nil {
		fmt.Fprintf(errOut, "wg-cp0-bundle-apply: write conf: %v\n", err)
		return 1
	}

	emitSummary(errOut, b, len(cpRelays), args.OutConfPath, args.OutPrivkeyPath, false)
	return 0
}

func validateArgs(a runArgs) error {
	required := []struct {
		name string
		val  string
	}{
		{"--in", a.InPath},
		{"--ca-pubkey", a.CAPubPath},
		{"--out-conf", a.OutConfPath},
		{"--out-privkey", a.OutPrivkeyPath},
	}
	for _, r := range required {
		if r.val == "" {
			return fmt.Errorf("%s is required", r.name)
		}
	}
	if a.PersistentKeepalive < 0 {
		return fmt.Errorf("--persistent-keepalive must be non-negative")
	}
	return nil
}

func loadCAPubkey(path string) (*ecdsa.PublicKey, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in %s", path)
	}
	// Accept either the raw SPKI-encoded PUBLIC KEY PEM emitted by
	// `ca-init --gen-root-pubkey` (committed under
	// ops/enrollment/public-keys/<ca-id>.pem) OR the X.509 CERTIFICATE
	// PEM the same root is wrapped in for TLS use
	// (ops/enrollment/ca/dev/root-cert.pem, also the Traefik chain
	// anchor). Extracting the SubjectPublicKeyInfo from a CERTIFICATE
	// is cheap and lets operators point at either file without hitting
	// the "expected PUBLIC KEY block, got CERTIFICATE" footgun.
	switch block.Type {
	case "PUBLIC KEY":
		pub, err := parseSPKIECDSAP256(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse ECDSA P-256 pubkey: %w", err)
		}
		return pub, nil
	case "CERTIFICATE":
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse X.509 certificate: %w", err)
		}
		pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("certificate at %s does not carry an ECDSA public key (got %T)", path, cert.PublicKey)
		}
		if pub.Curve != elliptic.P256() {
			return nil, fmt.Errorf("certificate at %s carries ECDSA pubkey on wrong curve (got %s, want P-256)", path, pub.Curve.Params().Name)
		}
		return pub, nil
	default:
		return nil, fmt.Errorf("expected PUBLIC KEY or CERTIFICATE block at %s, got %q", path, block.Type)
	}
}

func selectCPRelays(b *bundle.EnrollmentBundle) ([]bundle.KnownEndpoint, error) {
	out := make([]bundle.KnownEndpoint, 0, len(b.KnownEndpoints))
	for _, ep := range b.KnownEndpoints {
		if ep.Kind != bundle.Kind("cp-relay") {
			continue
		}
		if len(ep.Pubkey) != wgPubkeySize {
			return nil, fmt.Errorf("cp-relay %q has wrong-size pubkey (%d, want %d)", ep.Addr, len(ep.Pubkey), wgPubkeySize)
		}
		if ep.MeshAddr == "" {
			return nil, fmt.Errorf("cp-relay %q missing mesh_addr (cannot derive AllowedIPs)", ep.Addr)
		}
		out = append(out, ep)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("bundle has no cp-relay endpoints — wg-cp0 cannot come up")
	}
	return out, nil
}

func renderConf(b *bundle.EnrollmentBundle, cpRelays []bundle.KnownEndpoint, ifname string, keepalive int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# /etc/wireguard/%s.conf\n", ifname)
	fmt.Fprintf(&sb, "# Rendered by wg-cp0-bundle-apply (Block 22K) from bundle %s.\n", shortDeviceID(b))
	fmt.Fprintf(&sb, "# DO NOT EDIT MANUALLY — re-running the role overwrites this file.\n")
	fmt.Fprintf(&sb, "#\n")
	fmt.Fprintf(&sb, "# Site:        %s\n", b.Site)
	fmt.Fprintf(&sb, "# Device:      %s\n", b.DeviceID)
	fmt.Fprintf(&sb, "# Bundle CA:   %s\n", b.CAID)
	fmt.Fprintf(&sb, "# Issued:      %s\n", b.IssuedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&sb, "# Expires:     %s\n", b.ExpiresAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&sb, "# cp-relays:   %d\n", len(cpRelays))
	fmt.Fprintf(&sb, "\n")
	fmt.Fprintf(&sb, "[Interface]\n")
	fmt.Fprintf(&sb, "PrivateKey = %s\n", base64.StdEncoding.EncodeToString(b.CPDevicePrivkey))
	fmt.Fprintf(&sb, "Address    = %s\n", b.CPDeviceAddress)
	fmt.Fprintf(&sb, "# Network host doesn't accept inbound connections — no ListenPort.\n")
	for _, r := range cpRelays {
		fmt.Fprintf(&sb, "\n[Peer]\n")
		fmt.Fprintf(&sb, "# %s\n", r.Addr)
		fmt.Fprintf(&sb, "PublicKey  = %s\n", base64.StdEncoding.EncodeToString(r.Pubkey))
		fmt.Fprintf(&sb, "Endpoint   = %s\n", r.Addr)
		// ADR 0321 — wg-cp0 perimeter, not ACL. Render the issuer-
		// supplied AllowedIPs when present (canonical: [MeshAddr/32,
		// WGCP0FleetSubnet] on every relay). When absent (older
		// bundles issued before ADR 0321), fall back to the canonical
		// two-entry shape so legacy bundles also produce a symmetric,
		// working mesh. The pre-ADR-0321 fallback of MeshAddr/32-only
		// was the asymmetric default that F-116 closes — never render
		// it again.
		if len(r.AllowedIPs) > 0 {
			fmt.Fprintf(&sb, "AllowedIPs = %s\n", strings.Join(r.AllowedIPs, ", "))
		} else {
			fmt.Fprintf(&sb, "AllowedIPs = %s/32, %s\n", r.MeshAddr, bundle.WGCP0FleetSubnet)
		}
		if keepalive > 0 {
			fmt.Fprintf(&sb, "PersistentKeepalive = %d\n", keepalive)
		}
	}
	return sb.String()
}

func shortDeviceID(b *bundle.EnrollmentBundle) string {
	if b.DeviceID == "" {
		return "<unknown>"
	}
	return b.DeviceID
}

func emitSummary(w *os.File, b *bundle.EnrollmentBundle, cpRelayCount int, confPath, privkeyPath string, dryRun bool) {
	prefix := "Applied"
	if dryRun {
		prefix = "Would apply"
	}
	fmt.Fprintf(w, "\n%s wg-cp0 bundle for %s (site=%s, ca_id=%s).\n", prefix, b.DeviceID, b.Site, b.CAID)
	fmt.Fprintf(w, "  cp_device_pubkey:    %s\n", base64.StdEncoding.EncodeToString(b.CPDevicePubkey))
	fmt.Fprintf(w, "  cp_device_address:   %s\n", b.CPDeviceAddress)
	fmt.Fprintf(w, "  cp-relays:           %d\n", cpRelayCount)
	fmt.Fprintf(w, "  expires_at:          %s\n", b.ExpiresAt.UTC().Format(time.RFC3339))
	if !dryRun {
		fmt.Fprintf(w, "  conf:                %s (mode 0600)\n", confPath)
		fmt.Fprintf(w, "  privkey:             %s (mode 0600)\n", privkeyPath)
		fmt.Fprintf(w, "\nNext steps:\n")
		switch runtime.GOOS {
		case "darwin":
			// Operator workstation path — Homebrew wg-quick search dir
			// differs from where Ansible-style apply writes (/etc/wireguard/),
			// so prefer the full-path bring-up form which is unambiguous.
			// Full per-OS notes live in docs/operations/wg-cp0-bundle-ceremony.md §6a.
			fmt.Fprintf(w, "  1. sudo wg-quick up %s   # full path (Homebrew search dir differs by arch)\n", confPath)
			fmt.Fprintf(w, "  2. sudo wg show wg-cp0  # confirm handshake on every cp-relay\n")
			fmt.Fprintf(w, "  3. rm -P <bundle.cbor>  # macOS rm -P overwrites before unlink (no shred(1) by default)\n")
		case "linux":
			fmt.Fprintf(w, "  1. systemctl restart wg-quick@<interface>\n")
			fmt.Fprintf(w, "  2. wg show <interface>  # confirm handshake on every cp-relay\n")
			fmt.Fprintf(w, "  3. shred -u <bundle.cbor>  # operator / role discipline\n")
		default:
			fmt.Fprintf(w, "  1. Bring up the wg-cp0 interface using your platform's WireGuard tooling.\n")
			fmt.Fprintf(w, "  2. wg show wg-cp0  # confirm handshake on every cp-relay\n")
			fmt.Fprintf(w, "  3. Securely remove <bundle.cbor> after applying.\n")
		}
	}
}
