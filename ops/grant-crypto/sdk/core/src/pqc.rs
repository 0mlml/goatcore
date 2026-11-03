//! Suite `v2-x25519-mlkem768` — the post-quantum path (design §8).
//!
//! The DEK wrap is a **hybrid KEM** following **X-Wing**
//! (`draft-connolly-cfrg-xwing-kem`, the reviewed standard shipped by Apple):
//! an X25519 ephemeral-static exchange **and** an ML-KEM-768 encapsulation, whose
//! shared secrets are combined by X-Wing's fixed **SHA3-256 combiner** (with the
//! X-Wing label and the exact input ordering `ss_M ‖ ss_X ‖ ct_X ‖ pk_X`).
//! Confidentiality holds if *either* KEM is unbroken — the harvest-now,
//! decrypt-later defence. Adopted per external cryptographic review.
//!
//! Implemented directly over the `ml-kem` + `x25519-dalek` crates because the
//! `x-wing` crate requires rand_core 0.10, which conflicts with the aes-gcm/p256
//! stack here (rand_core 0.6); the construction is byte-identical to X-Wing, and
//! we migrate to the crate once that dependency settles.
//!
//! Status: inert. Needs a hybrid-recipient PKI extension (a KeyPackage directory
//! of X25519 + ML-KEM keys, SVID-signed) to bind to real identities.

use aes_gcm::aead::{Aead, Payload};
use aes_gcm::{AeadCore, Aes256Gcm, KeyInit, Nonce};
use hkdf::Hkdf;
use ml_kem::kem::{Decapsulate, Encapsulate};
use ml_kem::{EncodedSizeUser, KemCore, MlKem768};
use rand_core::OsRng;
use sha2::Sha256;
use sha3::{Digest, Sha3_256};
use x25519_dalek::{EphemeralSecret, PublicKey as XPublic, StaticSecret};

use crate::Error;

/// Suite id for the X-Wing hybrid post-quantum wrap.
pub const SUITE_V2: &str = "v2-x25519-mlkem768";
const WRAP_INFO_V2: &[u8] = b"grant-crypto/dek-wrap/v2";
const KEY_LEN: usize = 32;
const NONCE_LEN: usize = 12;
const X25519_LEN: usize = 32;

// X-Wing label: the six bytes of "\.//^\" (draft-connolly-cfrg-xwing-kem §5.3).
const XWING_LABEL: &[u8; 6] = &[0x5c, 0x2e, 0x2f, 0x2f, 0x5e, 0x5c];

type MlEk = <MlKem768 as KemCore>::EncapsulationKey;
type MlDk = <MlKem768 as KemCore>::DecapsulationKey;

/// A recipient's hybrid public key: an X25519 point plus an ML-KEM-768
/// encapsulation key. Published in a KeyPackage (the v2 PKI extension).
pub struct HybridPublic {
    pub x25519: [u8; X25519_LEN],
    pub mlkem_ek: Vec<u8>,
}

/// A recipient's hybrid secret key, retaining the public materials so the X-Wing
/// combiner transcript can be reconstructed at unwrap time.
pub struct HybridSecret {
    x25519: StaticSecret,
    mlkem_dk: MlDk,
    x25519_pub: [u8; X25519_LEN],
    mlkem_ek: Vec<u8>,
}

impl HybridSecret {
    /// Generate a fresh hybrid recipient keypair.
    pub fn generate() -> Self {
        let x = StaticSecret::random_from_rng(OsRng);
        let x_pub = XPublic::from(&x).to_bytes();
        let (dk, ek) = MlKem768::generate(&mut OsRng);
        HybridSecret {
            x25519: x,
            mlkem_dk: dk,
            x25519_pub: x_pub,
            mlkem_ek: ek.as_bytes().to_vec(),
        }
    }
    /// The recipient's public half, to hand to a producer / key-service.
    pub fn public(&self) -> HybridPublic {
        HybridPublic {
            x25519: self.x25519_pub,
            mlkem_ek: self.mlkem_ek.clone(),
        }
    }
}

impl HybridPublic {
    /// The KeyPackage bytes to publish: `x25519 (32) ‖ mlkem_ek`.
    pub fn to_bytes(&self) -> Vec<u8> {
        let mut b = Vec::with_capacity(X25519_LEN + self.mlkem_ek.len());
        b.extend_from_slice(&self.x25519);
        b.extend_from_slice(&self.mlkem_ek);
        b
    }
    /// Parse a published KeyPackage.
    pub fn from_bytes(b: &[u8]) -> Result<Self, Error> {
        if b.len() <= X25519_LEN {
            return Err(Error::Malformed);
        }
        let mut x = [0u8; X25519_LEN];
        x.copy_from_slice(&b[..X25519_LEN]);
        Ok(HybridPublic {
            x25519: x,
            mlkem_ek: b[X25519_LEN..].to_vec(),
        })
    }
}

/// The hybrid-wrapped DEK for suite v2.
pub struct WrappedDekV2 {
    pub epk_x25519: [u8; X25519_LEN],
    pub mlkem_ct: Vec<u8>,
    pub nonce: Vec<u8>,
    pub wct: Vec<u8>,
}

// X-Wing combiner: SHA3-256(label ‖ ss_M ‖ ss_X ‖ ct_X ‖ pk_X) → the X-Wing
// shared secret; then HKDF-SHA256 to a KEK (domain separation for this use).
fn kek(ss_m: &[u8], ss_x: &[u8], ct_x: &[u8], pk_x: &[u8]) -> Result<[u8; KEY_LEN], Error> {
    let mut h = Sha3_256::new();
    h.update(XWING_LABEL);
    h.update(ss_m);
    h.update(ss_x);
    h.update(ct_x);
    h.update(pk_x);
    let xwing_ss = h.finalize();

    let hk = Hkdf::<Sha256>::new(None, &xwing_ss);
    let mut k = [0u8; KEY_LEN];
    hk.expand(WRAP_INFO_V2, &mut k).map_err(|_| Error::Unwrap)?;
    Ok(k)
}

/// Wrap `dek` to a hybrid recipient (X-Wing over X25519 + ML-KEM-768).
#[allow(deprecated)] // transitive generic-array deprecation only
pub fn wrap_dek_v2(dek: &[u8; KEY_LEN], recipient: &HybridPublic) -> Result<WrappedDekV2, Error> {
    // classical leg
    let eph = EphemeralSecret::random_from_rng(OsRng);
    let eph_pub = XPublic::from(&eph).to_bytes();
    let ss_x = eph.diffie_hellman(&XPublic::from(recipient.x25519));

    // post-quantum leg
    let ek_arr = ml_kem::Encoded::<MlEk>::try_from(recipient.mlkem_ek.as_slice())
        .map_err(|_| Error::Malformed)?;
    let ek = MlEk::from_bytes(&ek_arr);
    let (ct, ss_m) = ek.encapsulate(&mut OsRng).map_err(|_| Error::Unwrap)?;

    let k = kek(&ss_m, ss_x.as_bytes(), &eph_pub, &recipient.x25519)?;

    let cipher = Aes256Gcm::new_from_slice(&k).map_err(|_| Error::Unwrap)?;
    let nonce = Aes256Gcm::generate_nonce(&mut OsRng);
    let wct = cipher
        .encrypt(
            &nonce,
            Payload {
                msg: &dek[..],
                aad: SUITE_V2.as_bytes(),
            },
        )
        .map_err(|_| Error::Unwrap)?;
    Ok(WrappedDekV2 {
        epk_x25519: eph_pub,
        mlkem_ct: ct.as_slice().to_vec(),
        nonce: nonce.to_vec(),
        wct,
    })
}

/// Recover the DEK from an X-Wing-wrapped entry using the recipient's secret key.
#[allow(deprecated)]
pub fn unwrap_dek_v2(w: &WrappedDekV2, recipient: &HybridSecret) -> Result<[u8; KEY_LEN], Error> {
    if w.nonce.len() != NONCE_LEN {
        return Err(Error::Unwrap);
    }
    // classical leg
    let ss_x = recipient
        .x25519
        .diffie_hellman(&XPublic::from(w.epk_x25519));

    // post-quantum leg
    let ct_arr = ml_kem::Ciphertext::<MlKem768>::try_from(w.mlkem_ct.as_slice())
        .map_err(|_| Error::Malformed)?;
    let ss_m = recipient
        .mlkem_dk
        .decapsulate(&ct_arr)
        .map_err(|_| Error::Unwrap)?;

    let k = kek(&ss_m, ss_x.as_bytes(), &w.epk_x25519, &recipient.x25519_pub)?;

    let cipher = Aes256Gcm::new_from_slice(&k).map_err(|_| Error::Unwrap)?;
    let pt = cipher
        .decrypt(
            Nonce::from_slice(&w.nonce),
            Payload {
                msg: &w.wct,
                aad: SUITE_V2.as_bytes(),
            },
        )
        .map_err(|_| Error::Unwrap)?;
    if pt.len() != KEY_LEN {
        return Err(Error::Unwrap);
    }
    let mut dek = [0u8; KEY_LEN];
    dek.copy_from_slice(&pt);
    Ok(dek)
}
