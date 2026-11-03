//! Rust production reference for grant-keyed payload encryption (suite `v1`).
//!
//! Implements the full wire format specified in `../../WIRE-FORMAT.md`:
//! - **read** (`verify_bundle`, `unwrap_dek`, `open_envelope`, `consume`);
//! - **write** (`new_dek`, `seal_envelope`, `wrap_dek`, `new_bundle`, `sign_bundle`).
//!
//! It is byte-compatible with the Go reference (`ops/grant-crypto`) — the same
//! additional-authenticated-data and signing-transcript layouts — proven by the
//! shared conformance vectors (`tests/conformance.rs`), and the write side round-
//! trips through the read side (`tests/roundtrip.rs`).
//!
//! Suite `v1-ecdh-p256`: AES-256-GCM payloads; DEK wrapped by ephemeral-static
//! ECDH P-256 → HKDF-SHA256 → AES-256-GCM; bundle signed ECDSA-P256/SHA-256.

use aes_gcm::aead::{Aead, Payload};
use aes_gcm::{AeadCore, Aes256Gcm, KeyInit, Nonce};
use base64::Engine;
use hkdf::Hkdf;
use p256::ecdsa::signature::{Signer, Verifier};
use p256::ecdsa::{Signature, SigningKey, VerifyingKey};
use p256::elliptic_curve::Generate;
use p256::{PublicKey, SecretKey};
use rand_core::{OsRng, RngCore};
use serde::{Deserialize, Serialize};
use sha2::Sha256;
use std::collections::BTreeMap;
use zeroize::Zeroize;

/// Producer + consumer data-path (Piece A): [`datapath::StreamKey`]/[`datapath::KeyStore`],
/// the typed [`datapath::Context`], and the enforced seal cap — the SDK-surface ergonomic
/// over the wire functions below.
pub mod datapath;

/// The member-leaf `rotate` verb (design §4): destroy-by-default forward secrecy, with an
/// explicit `KeepAccess` opt-out that reduces it.
pub mod rotate;

/// Suite `v2-x25519-mlkem768` — the post-quantum hybrid wrap (design §8, X-Wing).
pub mod pqc;

/// E1 — the key-distribution service core (state machine + assembly; design §3).
pub mod service;

/// The only implemented suite.
pub const SUITE_V1: &str = "v1-ecdh-p256";
/// Reserved post-quantum suite (not implemented here).
pub const SUITE_V2: &str = "v2-x25519-mlkem768";

const DEK_WRAP_INFO: &[u8] = b"grant-crypto/dek-wrap/v1";
const BUNDLE_SIG_DOMAIN: &[u8] = b"grant-crypto/bundle-sig/v1";
const NONCE_LEN: usize = 12;
const KEY_LEN: usize = 32;

/// A consumer error. Every variant is a *reject*; the distinction is advisory,
/// matching the Go reference's error taxonomy.
#[derive(Debug, PartialEq, Eq)]
pub enum Error {
    /// The named suite is not implemented (capability signal, not a key failure).
    UnsupportedSuite,
    /// AEAD authentication failed (wrong key/generation/release-set/topic, tamper).
    Decrypt,
    /// The wrapped DEK could not be recovered.
    Unwrap,
    /// The bundle signature did not verify.
    BadSignature,
    /// The member has no slot in the bundle (not in the audience).
    NoSlot,
    /// A field was malformed (bad base64, wrong length, bad key encoding).
    Malformed,
    /// The stream key hit its cumulative message-count cap and refuses to seal until
    /// rotation (the enforced nonce-safety cap, [`datapath::DEFAULT_SEAL_CAP`]). A local
    /// refusal — it changes nothing on the wire, so no conformance vector observes it — and
    /// distinct from `Decrypt`: it is a write-side reject, never a decrypt oracle.
    RotationRequired,
    /// The held stream key's generation has been superseded — a newer generation exists
    /// for this release set, so sealing under this one would keep publishing to an
    /// audience the owner has already changed. A local, write-side refusal in the
    /// same family as [`Error::RotationRequired`]: it changes nothing on the wire and no
    /// conformance vector observes it.
    Superseded,
    /// An audience change did not take effect on the fabric: the bundle read back still
    /// grants a slot to a member that should have lost one, or withholds a slot from one
    /// that should have gained it. Raised by [`datapath::confirm_audience`], which
    /// exists because a publish returning `Ok` proves only that a write was attempted —
    /// silent denial is this fabric's standing failure mode.
    AudienceNotApplied,
}

fn supported(suite: &str) -> bool {
    suite == SUITE_V1
}

// ---- wire structures (base64 byte fields decoded on access) ----

/// A sealed payload.
#[derive(Debug, Serialize, Deserialize)]
pub struct Envelope {
    pub suite: String,
    pub rs: String,
    pub gen: u64,
    #[serde(rename = "nonce")]
    nonce_b64: String,
    #[serde(rename = "ct")]
    ct_b64: String,
}

/// A DEK wrapped to one member.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WrappedDek {
    pub suite: String,
    #[serde(rename = "epk")]
    epk_b64: String,
    #[serde(rename = "nonce")]
    nonce_b64: String,
    #[serde(rename = "wct")]
    wct_b64: String,
}

/// The bundle published on `keys/<rs>/<gen>`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct KeyBundle {
    pub suite: String,
    pub rs: String,
    pub gen: u64,
    pub members: BTreeMap<String, WrappedDek>,
    #[serde(default)]
    pub signer: String,
    #[serde(default)]
    sig: String,
    /// The signer's SVID (PEM), carried so a member can validate it against the
    /// SPIRE root and obtain the verifying key — self-describing authenticity. It is
    /// deliberately NOT part of the signed transcript (`bundle_transcript`), so
    /// adding it does not change the signature or the cross-language test vectors.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub signer_svid: Option<String>,
}

fn b64(s: &str) -> Result<Vec<u8>, Error> {
    base64::engine::general_purpose::STANDARD
        .decode(s)
        .map_err(|_| Error::Malformed)
}

// ---- AAD / transcript layouts (must match the Go reference byte-for-byte) ----

fn put_lp(out: &mut Vec<u8>, p: &[u8]) {
    out.extend_from_slice(&(p.len() as u64).to_be_bytes());
    out.extend_from_slice(p);
}

/// `len8(suite)‖suite ‖ len8(rs)‖rs ‖ be64(gen) ‖ additional`
fn envelope_aad(suite: &str, rs: &str, gen: u64, additional: &[u8]) -> Vec<u8> {
    let mut b = Vec::new();
    put_lp(&mut b, suite.as_bytes());
    put_lp(&mut b, rs.as_bytes());
    b.extend_from_slice(&gen.to_be_bytes());
    b.extend_from_slice(additional);
    b
}

/// `len8(suite)‖suite ‖ ephemeralPub ‖ recipientPub` (the points are fixed-length).
fn wrap_aad(suite: &str, eph_pub: &[u8], recipient_pub: &[u8]) -> Vec<u8> {
    let mut b = Vec::new();
    put_lp(&mut b, suite.as_bytes());
    b.extend_from_slice(eph_pub);
    b.extend_from_slice(recipient_pub);
    b
}

/// The canonical bundle-signing transcript (excludes `sig`).
fn bundle_transcript(bundle: &KeyBundle) -> Result<Vec<u8>, Error> {
    let mut b = Vec::new();
    put_lp(&mut b, BUNDLE_SIG_DOMAIN);
    put_lp(&mut b, bundle.suite.as_bytes());
    put_lp(&mut b, bundle.rs.as_bytes());
    b.extend_from_slice(&bundle.gen.to_be_bytes());
    put_lp(&mut b, bundle.signer.as_bytes());
    b.extend_from_slice(&(bundle.members.len() as u64).to_be_bytes());
    // BTreeMap iterates in ascending key order — matches the Go sort.
    for (id, w) in &bundle.members {
        put_lp(&mut b, id.as_bytes());
        put_lp(&mut b, w.suite.as_bytes());
        put_lp(&mut b, &b64(&w.epk_b64)?);
        put_lp(&mut b, &b64(&w.nonce_b64)?);
        put_lp(&mut b, &b64(&w.wct_b64)?);
    }
    Ok(b)
}

// ---- operations ----

/// Verify the bundle signature against the trusted key-service verifying key.
pub fn verify_bundle(bundle: &KeyBundle, vk: &VerifyingKey) -> Result<(), Error> {
    let sig_bytes = b64(&bundle.sig)?;
    if sig_bytes.is_empty() {
        return Err(Error::BadSignature);
    }
    let sig = Signature::from_der(&sig_bytes).map_err(|_| Error::BadSignature)?;
    let transcript = bundle_transcript(bundle)?;
    vk.verify(&transcript, &sig)
        .map_err(|_| Error::BadSignature)
}

/// Recover the DEK from a member's wrapped entry using the member's secret key.
// `Nonce::from_slice` is deprecated only transitively (generic-array 0.14 →
// 1.x); the nonce length is checked above, so the conversion is infallible.
#[allow(deprecated)]
pub fn unwrap_dek(w: &WrappedDek, member: &SecretKey) -> Result<[u8; KEY_LEN], Error> {
    if !supported(&w.suite) {
        return Err(Error::UnsupportedSuite);
    }
    let epk = b64(&w.epk_b64)?;
    let nonce = b64(&w.nonce_b64)?;
    let wct = b64(&w.wct_b64)?;
    if nonce.len() != NONCE_LEN {
        return Err(Error::Unwrap);
    }
    let eph_pub = PublicKey::from_sec1_bytes(&epk).map_err(|_| Error::Unwrap)?;
    let shared = p256::ecdh::diffie_hellman(member.to_nonzero_scalar(), eph_pub.as_affine());
    let hk = Hkdf::<Sha256>::new(None, shared.raw_secret_bytes());
    let mut kek = [0u8; KEY_LEN];
    hk.expand(DEK_WRAP_INFO, &mut kek)
        .map_err(|_| Error::Unwrap)?;

    let recipient_pub = member.public_key().to_sec1_bytes(); // uncompressed, matches Go
    let aad = wrap_aad(&w.suite, &epk, &recipient_pub);
    let cipher = Aes256Gcm::new_from_slice(&kek).map_err(|_| Error::Unwrap)?;
    // The KEK is now copied into the AES round-key schedule (that schedule copy is part of
    // the stated zeroization ceiling — grant-crypto-sdk.md); wipe our stack copy.
    kek.zeroize();
    let mut pt = cipher
        .decrypt(
            Nonce::from_slice(&nonce),
            Payload {
                msg: &wct,
                aad: &aad,
            },
        )
        .map_err(|_| Error::Unwrap)?;
    if pt.len() != KEY_LEN {
        pt.zeroize();
        return Err(Error::Unwrap);
    }
    let mut dek = [0u8; KEY_LEN];
    dek.copy_from_slice(&pt);
    pt.zeroize();
    // Ceiling: `dek` is returned BY VALUE (the `[u8; 32]` signature is required by the
    // conformance/roundtrip layer — grant-crypto-sdk.md zeroization ceiling), so this move
    // leaves an uncovered caller copy; held DEKs are zeroized on drop by `datapath`.
    Ok(dek)
}

/// Open a sealed payload with the DEK, binding `additional` (the data topic).
#[allow(deprecated)] // see unwrap_dek: transitive generic-array deprecation only
pub fn open_envelope(
    dek: &[u8; KEY_LEN],
    env: &Envelope,
    additional: &[u8],
) -> Result<Vec<u8>, Error> {
    if !supported(&env.suite) {
        return Err(Error::UnsupportedSuite);
    }
    let nonce = b64(&env.nonce_b64)?;
    let ct = b64(&env.ct_b64)?;
    if nonce.len() != NONCE_LEN {
        return Err(Error::Decrypt);
    }
    let aad = envelope_aad(&env.suite, &env.rs, env.gen, additional);
    let cipher = Aes256Gcm::new_from_slice(dek).map_err(|_| Error::Decrypt)?;
    cipher
        .decrypt(
            Nonce::from_slice(&nonce),
            Payload {
                msg: &ct,
                aad: &aad,
            },
        )
        .map_err(|_| Error::Decrypt)
}

/// The full member consumer path, in the required order:
/// verify (if a trust key is given) → slot → unwrap → open.
pub fn consume(
    bundle: &KeyBundle,
    env: &Envelope,
    member_id: &str,
    member: &SecretKey,
    verify: Option<&VerifyingKey>,
    keyexpr: &str,
) -> Result<Vec<u8>, Error> {
    if let Some(vk) = verify {
        verify_bundle(bundle, vk)?;
    }
    let w = bundle.members.get(member_id).ok_or(Error::NoSlot)?;
    let dek = unwrap_dek(w, member)?;
    open_envelope(&dek, env, keyexpr.as_bytes())
}

// ---- write side (producer + key-service) ----

fn b64e(b: &[u8]) -> String {
    base64::engine::general_purpose::STANDARD.encode(b)
}

/// A fresh random 256-bit DEK from the system CSPRNG.
// Ceiling: returns the DEK BY VALUE — the `[u8; 32]` signature is required by the
// conformance/roundtrip layer (grant-crypto-sdk.md), so callers own an uncovered copy;
// the key service wraps-and-drops it and the datapath zeroizes held copies on drop.
pub fn new_dek() -> [u8; KEY_LEN] {
    let mut k = [0u8; KEY_LEN];
    OsRng.fill_bytes(&mut k);
    k
}

/// Seal a payload under `dek`, binding `(v1, rs, gen, additional)`.
#[allow(deprecated)] // transitive generic-array deprecation only
pub fn seal_envelope(
    dek: &[u8; KEY_LEN],
    rs: &str,
    gen: u64,
    plaintext: &[u8],
    additional: &[u8],
) -> Result<Envelope, Error> {
    let aad = envelope_aad(SUITE_V1, rs, gen, additional);
    let cipher = Aes256Gcm::new_from_slice(dek).map_err(|_| Error::Decrypt)?;
    let nonce = Aes256Gcm::generate_nonce(&mut OsRng);
    let ct = cipher
        .encrypt(
            &nonce,
            Payload {
                msg: plaintext,
                aad: &aad,
            },
        )
        .map_err(|_| Error::Decrypt)?;
    Ok(Envelope {
        suite: SUITE_V1.to_string(),
        rs: rs.to_string(),
        gen,
        nonce_b64: b64e(&nonce),
        ct_b64: b64e(&ct),
    })
}

/// Wrap `dek` to one recipient key (ephemeral-static ECDH → HKDF → AES-256-GCM),
/// with the suite and both public keys bound as AAD — byte-compatible with the
/// Go reference and the read side above.
#[allow(deprecated)]
pub fn wrap_dek(dek: &[u8; KEY_LEN], recipient: &PublicKey) -> Result<WrappedDek, Error> {
    let eph = SecretKey::try_generate().map_err(|_| Error::Unwrap)?;
    let shared = p256::ecdh::diffie_hellman(eph.to_nonzero_scalar(), recipient.as_affine());
    let hk = Hkdf::<Sha256>::new(None, shared.raw_secret_bytes());
    let mut kek = [0u8; KEY_LEN];
    hk.expand(DEK_WRAP_INFO, &mut kek)
        .map_err(|_| Error::Unwrap)?;

    let eph_pub = eph.public_key().to_sec1_bytes();
    let aad = wrap_aad(SUITE_V1, &eph_pub, &recipient.to_sec1_bytes());
    let cipher = Aes256Gcm::new_from_slice(&kek).map_err(|_| Error::Unwrap)?;
    // KEK now expanded into the AES round-key schedule (that copy is the stated ceiling —
    // grant-crypto-sdk.md); wipe our stack copy.
    kek.zeroize();
    let nonce = Aes256Gcm::generate_nonce(&mut OsRng);
    let wct = cipher
        .encrypt(
            &nonce,
            Payload {
                msg: &dek[..],
                aad: &aad,
            },
        )
        .map_err(|_| Error::Unwrap)?;
    Ok(WrappedDek {
        suite: SUITE_V1.to_string(),
        epk_b64: b64e(&eph_pub),
        nonce_b64: b64e(&nonce),
        wct_b64: b64e(&wct),
    })
}

/// Build a bundle by wrapping `dek` to each recipient in id order (fail-closed:
/// any wrap failure aborts the whole bundle).
pub fn new_bundle(
    rs: &str,
    gen: u64,
    dek: &[u8; KEY_LEN],
    recipients: &BTreeMap<String, PublicKey>,
) -> Result<KeyBundle, Error> {
    let mut members = BTreeMap::new();
    for (id, pk) in recipients {
        members.insert(id.clone(), wrap_dek(dek, pk)?);
    }
    Ok(KeyBundle {
        suite: SUITE_V1.to_string(),
        rs: rs.to_string(),
        gen,
        members,
        signer: String::new(),
        sig: String::new(),
        signer_svid: None,
    })
}

/// Attach the signer's SVID (PEM) to a bundle so members can obtain the verifying
/// key self-describingly. Set by the key-service after signing; not signed.
pub fn set_signer_svid(bundle: &mut KeyBundle, svid_pem: impl Into<String>) {
    bundle.signer_svid = Some(svid_pem.into());
}

/// Sign a bundle in place with the key-service key (ECDSA-P256/SHA-256, RFC6979).
pub fn sign_bundle(bundle: &mut KeyBundle, signer: &str, key: &SigningKey) -> Result<(), Error> {
    bundle.signer = signer.to_string();
    bundle.sig = String::new();
    let transcript = bundle_transcript(bundle)?;
    let sig: Signature = key.sign(&transcript);
    bundle.sig = b64e(sig.to_der().as_bytes());
    Ok(())
}

// ---- key reconstruction helpers (for the vectors' embedded material) ----

/// A P-256 secret key from a raw 32-byte scalar (the vectors' `priv_scalar_hex`).
pub fn secret_from_scalar(scalar: &[u8]) -> Result<SecretKey, Error> {
    SecretKey::from_slice(scalar).map_err(|_| Error::Malformed)
}

/// A verifying key from SPKI DER (the vectors' `pub_spki_b64`).
pub fn verifying_key_from_spki(der: &[u8]) -> Result<VerifyingKey, Error> {
    use p256::pkcs8::DecodePublicKey;
    let pk = PublicKey::from_public_key_der(der).map_err(|_| Error::Malformed)?;
    Ok(VerifyingKey::from(pk))
}
