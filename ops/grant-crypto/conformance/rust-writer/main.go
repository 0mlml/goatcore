// The independent Go reference verifies and opens upgraded Rust writer output.
package main

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"

	"github.com/usareur-af/goatcore/ops/grant-crypto/envelope"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	if len(os.Args) != 1 {
		panic("expected the synthetic fixture on stdin")
	}
	data, err := io.ReadAll(os.Stdin)
	must(err)
	var fixture struct {
		Bundle      envelope.KeyBundle `json:"bundle"`
		Envelope    envelope.Envelope  `json:"envelope"`
		MemberPKCS8 string             `json:"member_pkcs8_pem"`
		SignerSPKI  []byte             `json:"signer_spki_der_b64"`
	}
	must(json.Unmarshal(data, &fixture))
	expectedSigner, err := ecdh.P256().NewPrivateKey(bytes.Repeat([]byte{2}, 32))
	must(err)
	public, err := x509.ParsePKIXPublicKey(fixture.SignerSPKI)
	must(err)
	signer, ok := public.(*ecdsa.PublicKey)
	if !ok {
		panic("Go did not recover an ECDSA SPKI public key")
	}
	signerECDH, err := signer.ECDH()
	must(err)
	if !signerECDH.Equal(expectedSigner.PublicKey()) {
		panic("Go did not recover the expected SPKI public key")
	}
	block, _ := pem.Decode([]byte(fixture.MemberPKCS8))
	if block == nil {
		panic("Rust omitted the synthetic PKCS8 key")
	}
	private, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	must(err)
	if key, ok := private.(*ecdsa.PrivateKey); !ok || !bytes.Equal(key.D.FillBytes(make([]byte, 32)), bytes.Repeat([]byte{1}, 32)) {
		panic("Go did not recover the expected PKCS8 private key")
	}
	must(envelope.VerifyBundle(fixture.Bundle, signer))
	member, err := ecdh.P256().NewPrivateKey(bytes.Repeat([]byte{1}, 32))
	must(err)
	wrapped, ok := fixture.Bundle.Members["fixture-member"]
	if !ok {
		panic("Rust writer omitted the member")
	}
	dek, err := envelope.UnwrapDEK(wrapped, member)
	must(err)
	if !bytes.Equal(dek[:], bytes.Repeat([]byte{3}, 32)) {
		panic("Go recovered a different DEK")
	}
	plaintext, err := envelope.Open(dek, fixture.Envelope, []byte("data/fixture/topic"))
	must(err)
	if string(plaintext) != "RustCrypto wire fixture" {
		panic("Go recovered different plaintext")
	}
	if _, err = envelope.Open(dek, fixture.Envelope, []byte("data/fixture/other")); err == nil {
		panic("Go accepted the wrong topic")
	}
	outsider, err := ecdh.P256().NewPrivateKey(bytes.Repeat([]byte{4}, 32))
	must(err)
	if _, err = envelope.UnwrapDEK(wrapped, outsider); err == nil {
		panic("Go accepted an outsider")
	}
	tampered := fixture.Bundle
	tampered.Sig = append([]byte(nil), tampered.Sig...)
	tampered.Sig[0] ^= 1
	if err = envelope.VerifyBundle(tampered, signer); err == nil {
		panic("Go accepted a changed signature")
	}
	tagged := fixture.Envelope
	tagged.Generation++
	if _, err = envelope.Open(dek, tagged, []byte("data/fixture/topic")); err == nil {
		panic("Go accepted a changed generation")
	}
	fmt.Println("PASS: independent Go reads Rust PKCS8/SPKI, verifies the signature, unwraps the DEK, opens the payload, and rejects changed context/outsider/signature")
}
