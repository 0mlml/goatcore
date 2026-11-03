// Command gce (grant-crypto envelope) is the crypto half of the E0/M2.0 on-fabric
// transport proof (see ../../transport-proof/). It exercises the envelope package
// exactly as a producer and a member cert-process would:
//
//	gce gen-member <id> <dir>                       # a P-256 test SVID (stands in for SPIRE)
//	gce seal   --rs .. --gen .. --keyexpr .. \      # producer: mint DEK, seal payload,
//	           --in payload --member id=cert.pem..   #   wrap the DEK to each member
//	gce open   --bundle b.json --envelope e.json \  # member: unwrap its slot, open payload
//	           --id .. --key k.pem --keyexpr ..       #   exit 0 + plaintext, or exit 3 DENIED
//
// The Zenoh transport itself lives in the Python harness, not here: there is no
// maintained Go Zenoh client (the same gap that rest-pub-bridge, ADR 1023, exists
// to close), so the harness carries these JSON artifacts over a live router and
// shells back to this CLI for the crypto. E1 (the key-distribution service) will
// embed the envelope package directly rather than shell to this CLI — gce is a
// proof/operator tool, not a production seam.
//
// Standard library only, matching the envelope package it drives.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/usareur-af/goatcore/ops/grant-crypto/envelope"
)

// exitDenied is the process exit code for any crypto failure on `open` — wrong
// recipient, no key slot, wrong generation/release-set, tampered ciphertext. The
// harness asserts on it to prove the outsider is shut out. Kept distinct from the
// usage/IO error code (1) so "denied" is never confused with "the tool broke".
const exitDenied = 3

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: gce <gen-member|seal|open> [flags]")
	}
	var err error
	switch os.Args[1] {
	case "gen-member":
		err = genMember(os.Args[2:])
	case "seal":
		err = seal(os.Args[2:])
	case "open":
		err = open(os.Args[2:])
	default:
		fatalf("unknown subcommand %q (want gen-member|seal|open)", os.Args[1])
	}
	if err != nil {
		// `open` maps crypto denial to exitDenied via a sentinel; everything else
		// is a plain error (bad flags, missing file) at exit 1.
		if errors.Is(err, errDenied) {
			fmt.Fprintln(os.Stderr, "DENIED: "+err.Error())
			os.Exit(exitDenied)
		}
		fatalf("%v", err)
	}
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "gce: "+format+"\n", a...)
	os.Exit(1)
}

// errDenied wraps any `open` failure that must present as a read denial.
var errDenied = errors.New("cannot decrypt (wrong recipient, wrong generation, or tampered)")

func denied(reason string) error { return fmt.Errorf("%s: %w", reason, errDenied) }

// ---- gen-member: a P-256 leaf standing in for a SPIRE-issued SVID ----

// genMember writes <dir>/<id>.crt.pem and <dir>/<id>.key.pem: a self-signed
// P-256 leaf whose public key is the wrap target. SPIRE issues ECDSA/P-256 SVIDs
// (ADR 0412); for the crypto proof only the key type matters, so a self-signed
// leaf is a faithful stand-in — RecipientFromCert reads the public key alone.
func genMember(args []string) error {
	fs := newFlagSet("gen-member <id> <dir>")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: gce gen-member <id> <dir>")
	}
	id, dir := fs.Arg(0), fs.Arg(1)

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generating key: %w", err)
	}
	// A minimal, clearly-labeled test leaf. Deterministic fields except the key;
	// NotBefore is fixed in the past so the cert is valid without wall-clock games.
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: id},
		NotBefore:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), // a proof leaf, never trusted for time
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("creating certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("marshaling key: %w", err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if err := writePEM(filepath.Join(dir, id+".crt.pem"), "CERTIFICATE", der); err != nil {
		return err
	}
	if err := writePEM(filepath.Join(dir, id+".key.pem"), "PRIVATE KEY", keyDER); err != nil {
		return err
	}
	fmt.Printf("wrote %s/%s.crt.pem %s/%s.key.pem\n", dir, id, dir, id)
	return nil
}

// ---- seal: the producer half — mint DEK, seal payload, wrap to each member ----

// sealed is the combined artifact `seal` emits: the on-the-wire envelope and the
// keys/<rs>/<gen> bundle. The transport harness splits these onto two Zenoh
// topics; keeping them in one JSON keeps the CLI contract single.
type sealed struct {
	Envelope envelope.Envelope  `json:"envelope"`
	Bundle   envelope.KeyBundle `json:"bundle"`
	// Topic is where the bundle belongs (keys/<rs>/<gen>); echoed so the harness
	// and the CLI can never drift on the path shape.
	Topic string `json:"bundle_topic"`
}

func seal(args []string) error {
	fs := newFlagSet("seal --rs <rs> --gen <n> [--keyexpr <ke>] [--in <file>] --member id=cert.pem [--member ...] [--out <file>]")
	rs := fs.String("rs", "", "release-set name (audience unit)")
	gen := fs.Uint64("gen", 0, "DEK generation for this release-set")
	keyexpr := fs.String("keyexpr", "", "data key-expression, bound as AEAD additional data (topic-binding)")
	in := fs.String("in", "", "plaintext file (default: stdin)")
	out := fs.String("out", "", "write the sealed JSON here (default: stdout)")
	var members memberFlag
	fs.Var(&members, "member", "recipient as id=path/to/cert.pem (repeatable)")
	signKey := fs.String("sign-key", "", "key-service private key PEM (PKCS8 EC) to sign the bundle; omit to leave it unsigned")
	signer := fs.String("signer", "", "signer identity recorded in the signed bundle (required with --sign-key)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rs == "" {
		return errors.New("seal: --rs is required")
	}
	if len(members) == 0 {
		return errors.New("seal: at least one --member id=cert.pem is required")
	}

	plaintext, err := readInput(*in)
	if err != nil {
		return err
	}

	recipients := make(map[string]envelope.RecipientKey, len(members))
	for id, certPath := range members {
		cert, err := loadCert(certPath)
		if err != nil {
			return fmt.Errorf("member %q: %w", id, err)
		}
		pub, err := envelope.RecipientFromCert(cert)
		if err != nil {
			return fmt.Errorf("member %q: %w", id, err)
		}
		recipients[id] = envelope.RecipientKey{Pub: pub}
	}

	dek, err := envelope.NewDEK()
	if err != nil {
		return err
	}
	g := envelope.Generation(*gen)
	env, err := envelope.Seal(dek, *rs, g, plaintext, []byte(*keyexpr))
	if err != nil {
		return err
	}
	bundle, err := envelope.NewBundle(*rs, g, dek, recipients)
	if err != nil {
		return err
	}
	// Sign the bundle with the key-service identity (decision §7) when a key is
	// supplied; otherwise leave it unsigned (the earlier E0 proof posture).
	if *signKey != "" {
		if *signer == "" {
			return errors.New("seal: --signer is required with --sign-key")
		}
		priv, err := loadECDSAKey(*signKey)
		if err != nil {
			return fmt.Errorf("sign-key: %w", err)
		}
		if err := envelope.SignBundle(&bundle, *signer, priv); err != nil {
			return err
		}
	}

	blob, err := json.MarshalIndent(sealed{
		Envelope: env,
		Bundle:   bundle,
		Topic:    envelope.BundleTopic(*rs, g),
	}, "", "  ")
	if err != nil {
		return err
	}
	return writeOutput(*out, append(blob, '\n'))
}

// ---- open: a member unwraps its slot and opens the payload; outsider is denied ----

func open(args []string) error {
	fs := newFlagSet("open --bundle <b.json> --envelope <e.json> --id <memberid> --key <k.pem> [--keyexpr <ke>]")
	bundlePath := fs.String("bundle", "", "keys/<rs>/<gen> bundle JSON")
	envPath := fs.String("envelope", "", "envelope JSON (rs/gen/nonce/ciphertext)")
	id := fs.String("id", "", "this member's id (its slot in the bundle)")
	keyPath := fs.String("key", "", "this member's private key PEM (PKCS8 EC)")
	keyexpr := fs.String("keyexpr", "", "data key-expression, must match seal (topic-binding)")
	verifyCert := fs.String("verify-cert", "", "key-service cert PEM to verify the bundle signature; when set, an unsigned/mis-signed bundle is DENIED")
	out := fs.String("out", "", "write recovered plaintext here (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *bundlePath == "" || *envPath == "" || *id == "" || *keyPath == "" {
		return errors.New("open: --bundle, --envelope, --id and --key are all required")
	}

	bundleBytes, err := os.ReadFile(*bundlePath)
	if err != nil {
		return err
	}
	bundle, err := envelope.ParseBundle(bundleBytes)
	if err != nil {
		return err
	}
	// Authenticity gate (decision §7): when a key-service cert is supplied, the
	// bundle signature must verify before anything in it is trusted or unwrapped.
	if *verifyCert != "" {
		cert, err := loadCert(*verifyCert)
		if err != nil {
			return err
		}
		pub, err := envelope.SignerPubFromCert(cert)
		if err != nil {
			return err
		}
		if err := envelope.VerifyBundle(bundle, pub); err != nil {
			return denied("bundle signature")
		}
	}
	envBytes, err := os.ReadFile(*envPath)
	if err != nil {
		return err
	}
	var env envelope.Envelope
	if err := json.Unmarshal(envBytes, &env); err != nil {
		return fmt.Errorf("parsing envelope: %w", err)
	}

	// No slot for this id ⇒ not in the audience. This is the ordinary revoked /
	// never-authorized path: the bundle simply omits them.
	w, ok := bundle.Members[*id]
	if !ok {
		return denied(fmt.Sprintf("no key slot for member %q in bundle %s/%d", *id, bundle.ReleaseSet, bundle.Generation))
	}

	priv, err := loadECDHKey(*keyPath)
	if err != nil {
		return err
	}
	dek, err := envelope.UnwrapDEK(w, priv)
	if err != nil {
		// Slot present but the key doesn't open it (wrong key material).
		return denied("unwrap failed")
	}
	pt, err := envelope.Open(dek, env, []byte(*keyexpr))
	if err != nil {
		// The DEK unwrapped but the payload won't open: wrong generation/release-set
		// (both are authenticated), a keyexpr mismatch, or tampering.
		return denied("payload open failed")
	}
	return writeOutput(*out, pt)
}
