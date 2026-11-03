//! Round trip for the v2 hybrid post-quantum wrap (X-Wing = X25519 + ML-KEM-768).
//! Proves the construction composes: wrap to a hybrid recipient, unwrap recovers
//! the DEK, a different recipient cannot, and the KeyPackage public key
//! serializes. Nothing enforces on this.

use grant_crypto::pqc::{unwrap_dek_v2, wrap_dek_v2, HybridPublic, HybridSecret};

fn dek() -> [u8; 32] {
    grant_crypto::new_dek()
}

#[test]
fn xwing_wrap_roundtrip() {
    let d = dek();
    let recipient = HybridSecret::generate();
    let w = wrap_dek_v2(&d, &recipient.public()).unwrap();
    // X-Wing ciphertext = X25519 point (32) + ML-KEM-768 ciphertext (1088).
    assert_eq!(w.epk_x25519.len(), 32);
    assert_eq!(w.mlkem_ct.len(), 1088);
    assert_eq!(unwrap_dek_v2(&w, &recipient).unwrap(), d);
}

#[test]
fn xwing_wrong_recipient_rejected() {
    let d = dek();
    let recipient = HybridSecret::generate();
    let w = wrap_dek_v2(&d, &recipient.public()).unwrap();
    let other = HybridSecret::generate();
    assert!(unwrap_dek_v2(&w, &other).is_err());
}

#[test]
fn keypackage_pubkey_roundtrip() {
    // The recipient publishes its encapsulation key; a producer parses it back
    // and wraps to it — the KeyPackage-directory flow (the v2 PKI extension).
    let recipient = HybridSecret::generate();
    let bytes = recipient.public().to_bytes();
    let parsed = HybridPublic::from_bytes(&bytes).unwrap();

    let d = dek();
    let w = wrap_dek_v2(&d, &parsed).unwrap();
    assert_eq!(unwrap_dek_v2(&w, &recipient).unwrap(), d);
}
