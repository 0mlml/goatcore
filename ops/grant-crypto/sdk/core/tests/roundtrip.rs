//! Write-side round trips: the Rust producer/key-service functions compose with
//! the Rust read side. Because seal/wrap/sign reuse the same AAD and
//! signing-transcript builders the conformance vectors already proved
//! byte-identical to Go, artifacts produced here are wire-compatible with Go too.

use grant_crypto::*;
use p256::ecdsa::SigningKey;
use p256::elliptic_curve::Generate;
use p256::SecretKey;
use std::collections::BTreeMap;

#[test]
fn envelope_roundtrip() {
    let dek = new_dek();
    let env = seal_envelope(&dek, "rs", 7, b"hello", b"data/rs/cop").unwrap();
    assert_eq!(open_envelope(&dek, &env, b"data/rs/cop").unwrap(), b"hello");
    // wrong topic (AAD binding) and wrong key both fail.
    assert!(open_envelope(&dek, &env, b"data/rs/OTHER").is_err());
    assert!(open_envelope(&new_dek(), &env, b"data/rs/cop").is_err());
}

#[test]
fn wrap_roundtrip() {
    let dek = new_dek();
    let member = SecretKey::generate();
    let w = wrap_dek(&dek, &member.public_key()).unwrap();
    assert_eq!(unwrap_dek(&w, &member).unwrap(), dek);
    // a different recipient cannot unwrap.
    assert!(unwrap_dek(&w, &SecretKey::generate()).is_err());
}

#[test]
fn bundle_sign_verify_and_full_consume() {
    let dek = new_dek();
    let member = SecretKey::generate();
    let ks = SigningKey::generate();

    let mut recips = BTreeMap::new();
    recips.insert("member".to_string(), member.public_key());
    let mut bundle = new_bundle("rs", 7, &dek, &recips).unwrap();
    sign_bundle(&mut bundle, "key-service", &ks).unwrap();

    // verify: correct key ok, a different key rejected.
    verify_bundle(&bundle, ks.verifying_key()).unwrap();
    assert!(verify_bundle(&bundle, SigningKey::generate().verifying_key()).is_err());

    // full member consume path over Rust-produced artifacts.
    let env = seal_envelope(&dek, "rs", 7, b"payload", b"data/rs/cop").unwrap();
    let pt = consume(
        &bundle,
        &env,
        "member",
        &member,
        Some(ks.verifying_key()),
        "data/rs/cop",
    )
    .unwrap();
    assert_eq!(pt, b"payload");

    // an outsider with no slot is denied.
    let outsider = SecretKey::generate();
    assert!(consume(
        &bundle,
        &env,
        "outsider",
        &outsider,
        Some(ks.verifying_key()),
        "data/rs/cop"
    )
    .is_err());
}
