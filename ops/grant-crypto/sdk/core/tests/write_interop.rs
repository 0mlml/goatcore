//! Synthetic writer output consumed by the independent Go reference.
//! Explicitly run by tests/rustcrypto_wire_interop.sh; no Go dependency enters
//! this SDK workspace or its self-contained source tarball.
use base64::Engine;
use grant_crypto::{new_bundle, seal_envelope, sign_bundle};
use p256::pkcs8::{EncodePrivateKey, EncodePublicKey, LineEnding};
use p256::{ecdsa::SigningKey, SecretKey};
use std::collections::BTreeMap;

#[test]
#[ignore = "fixture producer for the repository's independent Go reader gate"]
fn export_go_fixture() {
    let output = std::env::var_os("GRANT_CRYPTO_WIRE_FIXTURE")
        .expect("fixture destination is required by the interoperability runner");
    // Public synthetic test scalars, independently reconstructed by Go.
    let member = SecretKey::from_slice(&[1u8; 32]).unwrap();
    let signer = SigningKey::from_slice(&[2u8; 32]).unwrap();
    let dek = [3u8; 32];
    let recipients = BTreeMap::from([("fixture-member".to_string(), member.public_key())]);
    let mut bundle = new_bundle("fixture-rs", 7, &dek, &recipients).unwrap();
    sign_bundle(&mut bundle, "fixture-signer", &signer).unwrap();
    let envelope = seal_envelope(
        &dek,
        "fixture-rs",
        7,
        b"RustCrypto wire fixture",
        b"data/fixture/topic",
    )
    .unwrap();
    let member_pem = member.to_pkcs8_pem(LineEnding::LF).unwrap();
    let signer_spki = signer.verifying_key().to_public_key_der().unwrap();
    let fixture = serde_json::json!({
        "bundle": bundle,
        "envelope": envelope,
        "member_pkcs8_pem": member_pem.as_str(),
        "signer_spki_der_b64": base64::engine::general_purpose::STANDARD.encode(signer_spki.as_bytes())
    });
    std::fs::write(output, serde_json::to_vec(&fixture).unwrap()).unwrap();
}
