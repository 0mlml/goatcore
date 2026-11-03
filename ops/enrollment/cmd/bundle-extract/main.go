// Command bundle-extract reads a single field from a CBOR-encoded
// EnrollmentBundle and emits it on stdout in the requested encoding.
//
// Used by the snitch-bundle ansible role + first-boot scripts on
// bundle-onboarded peers to read per-device material from the
// offline-enrollment bundle without wiring the bundle parser into
// every consumer. Block 61 / docs/design/offline-enrollment.md §11.6a.
//
// Examples:
//
//   # Read the snitch probe API key as hex (32 bytes → 64 hex chars).
//   bundle-extract --field snitch_probe_key --hex /etc/goat/enrollment/enrollment.cbor
//
//   # Read the device ID as raw text.
//   bundle-extract --field device_id /etc/goat/enrollment/enrollment.cbor
//
//   # Read the peer pubkey as base64.
//   bundle-extract --field peer_pubkey --base64 enrollment.cbor
//
// Exit codes:
//   0  field present + emitted
//   1  bundle parse error
//   2  field absent or unknown field name
//   3  encoding error (output writing)
//
// Discipline:
//   - The CLI does NOT verify the bundle's signature. Verification is
//     the responsibility of the caller (mgmt-host on enrollment, or the
//     systemd oneshot on the device). bundle-extract is purely "read
//     this byte slice or string from the parsed bundle" — anything more
//     belongs in a separate verifier.
//   - We do not log the extracted value. Probe keys are sensitive;
//     callers redirect stdout to a mode-0600 env file and immediately
//     forget about it.

package main

import (
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/enrollment/bundle"
)

func main() {
	field := flag.String("field", "", "field name to extract (e.g. snitch_probe_key, device_id, peer_pubkey)")
	asHex := flag.Bool("hex", false, "encode bytes as lowercase hex")
	asBase64 := flag.Bool("base64", false, "encode bytes as standard base64 (no wrapping)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: bundle-extract --field <name> [--hex|--base64] <bundle.cbor>\n\n")
		fmt.Fprintf(os.Stderr, "Reads ONE field from a CBOR-encoded EnrollmentBundle and writes it on stdout.\n")
		fmt.Fprintf(os.Stderr, "Does NOT verify the signature — caller's responsibility.\n\n")
		fmt.Fprintf(os.Stderr, "Recognized fields:\n")
		for _, n := range knownFields() {
			fmt.Fprintf(os.Stderr, "  %s\n", n)
		}
		flag.PrintDefaults()
	}
	flag.Parse()

	if *field == "" {
		fmt.Fprintln(os.Stderr, "--field is required")
		os.Exit(2)
	}
	if *asHex && *asBase64 {
		fmt.Fprintln(os.Stderr, "--hex and --base64 are mutually exclusive")
		os.Exit(2)
	}
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	path := flag.Arg(0)

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
		os.Exit(1)
	}
	b, err := bundle.Unmarshal(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse %s: %v\n", path, err)
		os.Exit(1)
	}

	encoding := encodingNone
	switch {
	case *asHex:
		encoding = encodingHex
	case *asBase64:
		encoding = encodingBase64
	}

	if err := writeField(os.Stdout, b, *field, encoding); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		// Distinguish "field absent" from "I/O error".
		if strings.HasPrefix(err.Error(), "field ") {
			os.Exit(2)
		}
		os.Exit(3)
	}
}

type encodingMode int

const (
	encodingNone encodingMode = iota
	encodingHex
	encodingBase64
)

// knownFields lists the fields bundle-extract supports. Kept in sync
// with bundle.EnrollmentBundle by hand — adding a new field to the
// struct without listing it here just produces "field unknown" rather
// than a silent miss.
func knownFields() []string {
	return []string{
		"version",
		"device_id",
		"peer_pubkey",
		"acl_groups",
		"site",
		"issued_at",
		"activation_deadline",
		"expires_at",
		"nonce",
		"ca_id",
		"snitch_probe_key",
		"has_inner_mesh",
		"inner_mesh_mgmt_url",
		"enrollment_anchor",
		"node_class",
	}
}

// writeField emits the named field from b. Bytes encoded per encoding;
// strings emitted raw; integers + times emitted in their natural form.
// Returns an error starting with "field " when the field is absent or
// unrecognized; other errors are I/O.
func writeField(w io.Writer, b *bundle.EnrollmentBundle, name string, enc encodingMode) error {
	switch name {
	case "version":
		_, err := fmt.Fprintf(w, "%d", b.Version)
		return err
	case "device_id":
		return writeString(w, b.DeviceID)
	case "peer_pubkey":
		return writeBytes(w, b.PeerPubkey, enc, "peer_pubkey")
	case "acl_groups":
		_, err := fmt.Fprintln(w, strings.Join(b.ACLGroups, ","))
		return err
	case "site":
		return writeString(w, b.Site)
	case "issued_at":
		return writeTime(w, b.IssuedAt)
	case "activation_deadline":
		return writeTime(w, b.ActivationDeadline)
	case "expires_at":
		return writeTime(w, b.ExpiresAt)
	case "nonce":
		return writeBytes(w, b.Nonce, enc, "nonce")
	case "ca_id":
		return writeString(w, b.CAID)
	case "snitch_probe_key":
		return writeBytes(w, b.SnitchProbeKey, enc, "snitch_probe_key")
	case "has_inner_mesh":
		// Always "true" or "false" (exit 0) — callers branch on the value,
		// not the exit code. The setup-key itself is deliberately NOT
		// extractable; this surfaces combined-mode eligibility only.
		_, err := fmt.Fprintln(w, b.HasInnerMesh())
		return err
	case "inner_mesh_mgmt_url":
		return writeString(w, b.InnerMeshSetup.ManagementURL)
	case "enrollment_anchor":
		// D6a registry provenance: absent on pre-D6a and hand-grouped bundles
		// (writeString reports "field empty" gracefully, exit 2).
		return writeString(w, b.EnrollmentAnchor)
	case "node_class":
		return writeString(w, b.NodeClass)
	default:
		return fmt.Errorf("field unknown: %q (run with --help for list)", name)
	}
}

func writeString(w io.Writer, s string) error {
	if s == "" {
		return fmt.Errorf("field empty: string")
	}
	_, err := fmt.Fprintln(w, s)
	return err
}

func writeBytes(w io.Writer, b []byte, enc encodingMode, label string) error {
	if len(b) == 0 {
		return fmt.Errorf("field absent or empty: %s", label)
	}
	switch enc {
	case encodingHex:
		_, err := fmt.Fprintln(w, hex.EncodeToString(b))
		return err
	case encodingBase64:
		_, err := fmt.Fprintln(w, base64.StdEncoding.EncodeToString(b))
		return err
	default:
		_, err := w.Write(b)
		return err
	}
}

func writeTime(w io.Writer, t time.Time) error {
	if t.IsZero() {
		return fmt.Errorf("field empty: time")
	}
	_, err := fmt.Fprintln(w, t.UTC().Format(time.RFC3339))
	return err
}
