// Command vectorgen produces the committed cross-language conformance test
// vectors for grant-crypto (see the conformance package + ../../WIRE-FORMAT.md).
//
//	go run ./cmd/vectorgen <outdir>     # e.g. envelope/testdata/vectors
//
// Run once and commit vectors.json. Regenerate only when the wire format changes
// — the committed bytes ARE the vectors, so re-running (which draws fresh
// randomness) changes them. The vectors are self-contained: throwaway test keys
// are embedded (private keys as P-256 scalars, public keys as SPKI), so no key
// files are written. The Go reference and the Rust production core both validate
// against the committed file.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/usareur-af/goatcore/ops/grant-crypto/conformance"
	"github.com/usareur-af/goatcore/ops/grant-crypto/envelope"
)

const (
	releaseSet = "coalition-air"
	generation = 7
	keyexpr    = "data/coalition-air/cop"
)

var plaintext = []byte("brigade COP snapshot, releasable to REL-EXAMPLE only")

func main() {
	if len(os.Args) != 2 {
		fatal("usage: vectorgen <outdir>")
	}
	outdir := os.Args[1]
	if err := os.MkdirAll(outdir, 0o750); err != nil {
		fatal("%v", err)
	}

	// Throwaway test identities. Only the P-256 key type matters to the crypto.
	member := genKey()
	outsider := genKey()
	keyservice := genKey()

	memberPub, err := member.ECDH()
	if err != nil {
		fatal("member ECDH: %v", err)
	}

	// Produce the base artifacts once.
	dek, err := envelope.NewDEK()
	if err != nil {
		fatal("dek: %v", err)
	}
	env, err := envelope.Seal(dek, releaseSet, generation, plaintext, []byte(keyexpr))
	if err != nil {
		fatal("seal: %v", err)
	}
	bundle, err := envelope.NewBundle(releaseSet, generation, dek, map[string]envelope.RecipientKey{
		"member": {Pub: memberPub.PublicKey()},
	})
	if err != nil {
		fatal("bundle: %v", err)
	}
	if err := envelope.SignBundle(&bundle, "key-service", keyservice); err != nil {
		fatal("sign: %v", err)
	}

	baseEnv := mustJSON(env)
	baseBundle := mustJSON(bundle)

	artifacts := map[string]conformance.Artifact{
		"base":            {Envelope: baseEnv, Bundle: baseBundle, Keyexpr: keyexpr},
		"env-wrong-gen":   {Envelope: mutateJSON(baseEnv, func(m map[string]any) { m["gen"] = generation + 1 }), Bundle: baseBundle, Keyexpr: keyexpr},
		"env-tampered-ct": {Envelope: mutateJSON(baseEnv, func(m map[string]any) { m["ct"] = flipB64(m["ct"].(string)) }), Bundle: baseBundle, Keyexpr: keyexpr},
		"env-v2-suite":    {Envelope: mutateJSON(baseEnv, func(m map[string]any) { m["suite"] = string(envelope.SuiteV2X25519MLKEM768) }), Bundle: baseBundle, Keyexpr: keyexpr},
		"bundle-tampered": {Envelope: baseEnv, Bundle: mutateBundleMemberCT(baseBundle, "member"), Keyexpr: keyexpr},
	}

	ptB64 := base64.StdEncoding.EncodeToString(plaintext)
	cases := []conformance.Case{
		{Name: "member verifies signature and opens", Artifact: "base", Member: "member", Verify: "keyservice",
			Expect: conformance.Expect{Result: "accept", PlaintextB64: ptB64}},
		{Name: "member opens without verifying (verify is the caller's choice)", Artifact: "base", Member: "member",
			Expect: conformance.Expect{Result: "accept", PlaintextB64: ptB64}},
		{Name: "outsider has no slot in the bundle", Artifact: "base", Member: "outsider", Verify: "keyservice",
			Expect: conformance.Expect{Result: "reject", Reason: "no-slot"}},
		{Name: "wrong signer: bundle verified against a non-key-service key", Artifact: "base", Member: "member", Verify: "outsider",
			Expect: conformance.Expect{Result: "reject", Reason: "bad-signature"}},
		{Name: "wrong key-expression (topic binding)", Artifact: "base", Member: "member", Verify: "keyservice", Keyexpr: "data/coalition-air/OTHER",
			Expect: conformance.Expect{Result: "reject", Reason: "open-failed"}},
		{Name: "wrong generation (authenticated)", Artifact: "env-wrong-gen", Member: "member", Verify: "keyservice",
			Expect: conformance.Expect{Result: "reject", Reason: "open-failed"}},
		{Name: "tampered ciphertext", Artifact: "env-tampered-ct", Member: "member", Verify: "keyservice",
			Expect: conformance.Expect{Result: "reject", Reason: "open-failed"}},
		{Name: "unsupported suite on the envelope", Artifact: "env-v2-suite", Member: "member", Verify: "keyservice",
			Expect: conformance.Expect{Result: "reject", Reason: "unsupported-suite"}},
		{Name: "tampered bundle (a member's wrapped key altered after signing)", Artifact: "bundle-tampered", Member: "member", Verify: "keyservice",
			Expect: conformance.Expect{Result: "reject", Reason: "bad-signature"}},
	}

	v := conformance.Vectors{
		Suite: string(envelope.SuiteV1ECDHP256),
		Note:  "grant-crypto decrypt-direction conformance vectors; throwaway embedded keys; consumer order = verify → slot → unwrap → open (see conformance package + WIRE-FORMAT.md)",
		Keys: map[string]conformance.KeyRef{
			"member":     {PrivScalarHex: scalarHex(member)},
			"outsider":   {PrivScalarHex: scalarHex(outsider), PubSpkiB64: spkiB64(outsider)},
			"keyservice": {PubSpkiB64: spkiB64(keyservice)},
		},
		Artifacts: artifacts,
		Cases:     cases,
	}
	blob, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatal("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outdir, "vectors.json"), append(blob, '\n'), 0o644); err != nil {
		fatal("write: %v", err)
	}
	fmt.Printf("wrote %s/vectors.json (%d cases, self-contained keys)\n", outdir, len(cases))
}

func genKey() *ecdsa.PrivateKey {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		fatal("key: %v", err)
	}
	return k
}

// scalarHex is the P-256 private scalar (the ECDH private bytes) as hex.
func scalarHex(priv *ecdsa.PrivateKey) string {
	ec, err := priv.ECDH()
	if err != nil {
		fatal("ECDH: %v", err)
	}
	return hex.EncodeToString(ec.Bytes())
}

// spkiB64 is the SPKI DER of the public key as base64 — public, safe to commit.
func spkiB64(priv *ecdsa.PrivateKey) string {
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		fatal("spki: %v", err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		fatal("marshal: %v", err)
	}
	return b
}

// mutateJSON parses raw as an object, applies fn, and re-marshals.
func mutateJSON(raw json.RawMessage, fn func(map[string]any)) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		fatal("mutate parse: %v", err)
	}
	fn(m)
	return mustJSON(m)
}

// mutateBundleMemberCT flips a byte of one member's wrapped ciphertext, leaving
// the (now stale) signature in place — the tampered-bundle case.
func mutateBundleMemberCT(raw json.RawMessage, member string) json.RawMessage {
	return mutateJSON(raw, func(m map[string]any) {
		members := m["members"].(map[string]any)
		w := members[member].(map[string]any)
		w["wct"] = flipB64(w["wct"].(string))
	})
}

// flipB64 decodes a std-base64 string, flips the first byte, and re-encodes.
func flipB64(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) == 0 {
		fatal("flipB64: bad input")
	}
	b[0] ^= 0xff
	return base64.StdEncoding.EncodeToString(b)
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "vectorgen: "+format+"\n", a...)
	os.Exit(1)
}
