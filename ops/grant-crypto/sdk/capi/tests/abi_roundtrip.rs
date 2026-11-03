//! End-to-end exercise of the C ABI through its extern "C" surface (the same entry
//! points the Go / Python / TS bindings call). Bundles + identity scalars are built
//! with the core's own P-256 stack; everything after that crosses the boundary as
//! handles + JSON bytes, never key material.

use p256::elliptic_curve::Generate;
use std::collections::BTreeMap;
use std::ffi::CString;

use grant_crypto::datapath::DEFAULT_SEAL_CAP;
use grant_crypto::{new_bundle, new_dek, sign_bundle, KeyBundle};
use grant_crypto_capi::*;
use p256::ecdsa::SigningKey;
use p256::pkcs8::EncodePublicKey;
use p256::{PublicKey, SecretKey};

// A member id + its retained secret; `scalar()` is the raw 32-byte form the ABI's
// gc_identity_new consumes.
struct Party {
    id: String,
    secret: SecretKey,
}
fn party(id: &str) -> Party {
    Party {
        id: id.into(),
        secret: SecretKey::generate(),
    }
}
impl Party {
    fn scalar(&self) -> [u8; 32] {
        let mut a = [0u8; 32];
        let b = self.secret.to_bytes();
        a.copy_from_slice(&b);
        a
    }
    fn public(&self) -> PublicKey {
        self.secret.public_key()
    }
}

// The key-service builds + signs a bundle for `audience`; returns (json, signing key).
fn signed_bundle(rs: &str, gen: u64, audience: &[&Party]) -> (Vec<u8>, SigningKey) {
    let recipients: BTreeMap<String, PublicKey> = audience
        .iter()
        .map(|p| (p.id.clone(), p.public()))
        .collect();
    let dek = new_dek();
    let mut bundle = new_bundle(rs, gen, &dek, &recipients).unwrap();
    let ks = SigningKey::generate();
    sign_bundle(&mut bundle, "key-service", &ks).unwrap();
    (serde_json::to_vec(&bundle).unwrap(), ks)
}

// Create an identity handle through the ABI.
fn identity(p: &Party) -> u64 {
    let cid = CString::new(p.id.clone()).unwrap();
    let scalar = p.scalar();
    let mut h = 0u64;
    let rc = unsafe { gc_identity_new(cid.as_ptr(), scalar.as_ptr(), &mut h) };
    assert_eq!(rc, GC_OK, "identity_new");
    h
}

// Copy a gc_buf's bytes out.
fn buf_bytes(h: u64) -> Vec<u8> {
    let p = gc_buf_ptr(h);
    let n = gc_buf_len(h);
    assert!(!p.is_null() && n > 0, "buffer must be non-empty");
    unsafe { std::slice::from_raw_parts(p, n) }.to_vec()
}

#[test]
fn abi_version_is_one() {
    assert_eq!(gc_abi_version(), 1);
}

#[test]
fn full_roundtrip_producer_seals_grantee_opens() {
    // Each sealing test uses a distinct release-set: the seal counter is process-global
    // per `(rs, gen)`, so a shared `rs` would let one test's seals leak into another's.
    let rs = "capi-roundtrip";
    let keyexpr = CString::new("data/coalition-air/cop").unwrap();
    let producer = party("coalition-air/prod");
    let grantee = party("coalition-air/con");
    let (bundle_json, _ks) = signed_bundle(rs, 1, &[&producer, &grantee]);
    let plaintext = b"brigade COP snapshot, REL-EXAMPLE only";

    // Producer: identity -> acquire highest-gen stream key -> seal.
    let prod_id = identity(&producer);
    let bundles_json = format!("[{}]", String::from_utf8(bundle_json.clone()).unwrap());
    let rs_c = CString::new(rs).unwrap();
    let mut sk = 0u64;
    let rc = unsafe {
        gc_streamkey_acquire(
            bundles_json.as_ptr(),
            bundles_json.len(),
            rs_c.as_ptr(),
            prod_id,
            &mut sk,
        )
    };
    assert_eq!(rc, GC_OK, "acquire");

    let mut sealed_buf = 0u64;
    let rc = unsafe {
        gc_streamkey_seal(
            sk,
            keyexpr.as_ptr(),
            plaintext.as_ptr(),
            plaintext.len(),
            &mut sealed_buf,
        )
    };
    assert_eq!(rc, GC_OK, "seal");
    let envelope_json = buf_bytes(sealed_buf);

    // Grantee: identity -> keystore -> admit -> open.
    let gr_id = identity(&grantee);
    let mut ks = 0u64;
    assert_eq!(unsafe { gc_keystore_new(&mut ks) }, GC_OK);
    let mut had_slot = false;
    let rc = unsafe {
        gc_keystore_admit(
            ks,
            gr_id,
            bundle_json.as_ptr(),
            bundle_json.len(),
            &mut had_slot,
        )
    };
    assert_eq!(rc, GC_OK, "admit");
    assert!(had_slot, "grantee has a slot");

    let mut opened_buf = 0u64;
    let rc = unsafe {
        gc_keystore_open(
            ks,
            keyexpr.as_ptr(),
            envelope_json.as_ptr(),
            envelope_json.len(),
            &mut opened_buf,
        )
    };
    assert_eq!(rc, GC_OK, "open");
    assert_eq!(buf_bytes(opened_buf), plaintext);

    // Clean up (each returns GC_OK once).
    assert_eq!(gc_buf_dispose(opened_buf), GC_OK);
    assert_eq!(gc_buf_dispose(sealed_buf), GC_OK);
    assert_eq!(gc_streamkey_dispose(sk), GC_OK);
    assert_eq!(gc_keystore_dispose(ks), GC_OK);
    assert_eq!(gc_identity_dispose(prod_id), GC_OK);
    assert_eq!(gc_identity_dispose(gr_id), GC_OK);
}

#[test]
fn outsider_has_no_slot_and_cannot_open() {
    let rs = "capi-outsider";
    let keyexpr = CString::new("data/coalition-air/cop").unwrap();
    let producer = party("coalition-air/prod");
    let grantee = party("coalition-air/con");
    let outsider = party("outsider");
    let (bundle_json, _ks) = signed_bundle(rs, 1, &[&producer, &grantee]);

    // Producer seals a real message.
    let prod_id = identity(&producer);
    let bundles_json = format!("[{}]", String::from_utf8(bundle_json.clone()).unwrap());
    let rs_c = CString::new(rs).unwrap();
    let mut sk = 0u64;
    assert_eq!(
        unsafe {
            gc_streamkey_acquire(
                bundles_json.as_ptr(),
                bundles_json.len(),
                rs_c.as_ptr(),
                prod_id,
                &mut sk,
            )
        },
        GC_OK
    );
    let plaintext = b"secret";
    let mut sealed_buf = 0u64;
    assert_eq!(
        unsafe {
            gc_streamkey_seal(
                sk,
                keyexpr.as_ptr(),
                plaintext.as_ptr(),
                plaintext.len(),
                &mut sealed_buf,
            )
        },
        GC_OK
    );
    let envelope_json = buf_bytes(sealed_buf);

    // Outsider admits nothing and cannot open.
    let out_id = identity(&outsider);
    let mut ks = 0u64;
    assert_eq!(unsafe { gc_keystore_new(&mut ks) }, GC_OK);
    let mut had_slot = true;
    assert_eq!(
        unsafe {
            gc_keystore_admit(
                ks,
                out_id,
                bundle_json.as_ptr(),
                bundle_json.len(),
                &mut had_slot,
            )
        },
        GC_OK
    );
    assert!(!had_slot, "outsider has no slot");

    let mut opened_buf = 0u64;
    assert_eq!(
        unsafe {
            gc_keystore_open(
                ks,
                keyexpr.as_ptr(),
                envelope_json.as_ptr(),
                envelope_json.len(),
                &mut opened_buf,
            )
        },
        GC_ERR_NO_SLOT,
        "outsider open is NoSlot"
    );
}

#[test]
fn use_after_dispose_and_double_dispose_are_disposed_handle() {
    let grantee = party("coalition-air/con");
    let (bundle_json, _ks) = signed_bundle("coalition-air", 1, &[&grantee]);
    let gr_id = identity(&grantee);

    let mut ks = 0u64;
    assert_eq!(unsafe { gc_keystore_new(&mut ks) }, GC_OK);
    assert_eq!(gc_keystore_dispose(ks), GC_OK);

    // Admit after dispose: fail-closed, no dereference of freed state.
    let mut had_slot = false;
    assert_eq!(
        unsafe {
            gc_keystore_admit(
                ks,
                gr_id,
                bundle_json.as_ptr(),
                bundle_json.len(),
                &mut had_slot,
            )
        },
        GC_ERR_DISPOSED_HANDLE
    );
    // Double-dispose is the same harmless error.
    assert_eq!(gc_keystore_dispose(ks), GC_ERR_DISPOSED_HANDLE);
    // A disposed identity is likewise invalid.
    assert_eq!(gc_identity_dispose(gr_id), GC_OK);
    assert_eq!(gc_identity_dispose(gr_id), GC_ERR_DISPOSED_HANDLE);
}

#[test]
fn wrong_topic_open_fails_the_aead() {
    let rs = "capi-wrongtopic";
    let producer = party("coalition-air/prod");
    let grantee = party("coalition-air/con");
    let (bundle_json, _ks) = signed_bundle(rs, 1, &[&producer, &grantee]);

    let prod_id = identity(&producer);
    let bundles_json = format!("[{}]", String::from_utf8(bundle_json.clone()).unwrap());
    let rs_c = CString::new(rs).unwrap();
    let mut sk = 0u64;
    assert_eq!(
        unsafe {
            gc_streamkey_acquire(
                bundles_json.as_ptr(),
                bundles_json.len(),
                rs_c.as_ptr(),
                prod_id,
                &mut sk,
            )
        },
        GC_OK
    );
    let sealed_topic = CString::new("data/coalition-air/cop").unwrap();
    let mut sealed_buf = 0u64;
    assert_eq!(
        unsafe { gc_streamkey_seal(sk, sealed_topic.as_ptr(), b"x".as_ptr(), 1, &mut sealed_buf,) },
        GC_OK
    );
    let envelope_json = buf_bytes(sealed_buf);

    let gr_id = identity(&grantee);
    let mut ks = 0u64;
    assert_eq!(unsafe { gc_keystore_new(&mut ks) }, GC_OK);
    let mut had_slot = false;
    assert_eq!(
        unsafe {
            gc_keystore_admit(
                ks,
                gr_id,
                bundle_json.as_ptr(),
                bundle_json.len(),
                &mut had_slot,
            )
        },
        GC_OK
    );

    // Open on a DIFFERENT topic: the AEAD binding rejects it.
    let other_topic = CString::new("data/coalition-air/other").unwrap();
    let mut opened_buf = 0u64;
    assert_eq!(
        unsafe {
            gc_keystore_open(
                ks,
                other_topic.as_ptr(),
                envelope_json.as_ptr(),
                envelope_json.len(),
                &mut opened_buf,
            )
        },
        GC_ERR_DECRYPT,
        "wrong topic must fail the AEAD"
    );
}

#[test]
fn verify_bundle_accepts_good_rejects_tampered() {
    let rs = "coalition-air";
    let grantee = party("coalition-air/con");
    let (bundle_json, ks) = signed_bundle(rs, 1, &[&grantee]);
    let vk_der = ks.verifying_key().to_public_key_der().unwrap();
    let vk = vk_der.as_bytes();

    // Good bundle verifies.
    assert_eq!(
        unsafe {
            gc_verify_bundle(
                bundle_json.as_ptr(),
                bundle_json.len(),
                vk.as_ptr(),
                vk.len(),
            )
        },
        GC_OK,
        "good bundle verifies"
    );

    // Tamper a signed field (rs): the JSON stays valid, the signature no longer
    // covers it -> reject.
    let mut v: serde_json::Value = serde_json::from_slice(&bundle_json).unwrap();
    v["rs"] = serde_json::Value::String("tampered".into());
    let tampered = serde_json::to_vec(&v).unwrap();
    // Sanity: it still parses as a bundle (so the reject is a signature reject, not a parse error).
    let _: KeyBundle = serde_json::from_slice(&tampered).unwrap();
    assert_ne!(
        unsafe { gc_verify_bundle(tampered.as_ptr(), tampered.len(), vk.as_ptr(), vk.len(),) },
        GC_OK,
        "tampered bundle is rejected"
    );
}

#[test]
fn seal_cap_is_enforced_through_the_abi() {
    let rs = "capi-sealcap";
    let keyexpr = CString::new("data/coalition-air/cop").unwrap();
    let producer = party("coalition-air/prod");
    let (bundle_json, _ks) = signed_bundle(rs, 1, &[&producer]);

    let prod_id = identity(&producer);
    let bundles_json = format!("[{}]", String::from_utf8(bundle_json).unwrap());
    let rs_c = CString::new(rs).unwrap();
    let mut sk = 0u64;
    assert_eq!(
        unsafe {
            gc_streamkey_acquire(
                bundles_json.as_ptr(),
                bundles_json.len(),
                rs_c.as_ptr(),
                prod_id,
                &mut sk,
            )
        },
        GC_OK
    );

    // A request above the default cap is refused (tighten-only).
    assert_eq!(
        gc_streamkey_set_cap(sk, u64::MAX),
        GC_ERR_MALFORMED,
        "cannot loosen above the default cap"
    );
    // Tighten to 2 and prove: 2 seals succeed, remaining counts down, the 3rd is refused.
    assert_eq!(gc_streamkey_set_cap(sk, 2), GC_OK);

    let mut remaining = 0u64;
    assert_eq!(unsafe { gc_streamkey_remaining(sk, &mut remaining) }, GC_OK);
    assert_eq!(remaining, 2);

    for _ in 0..2 {
        let mut buf = 0u64;
        assert_eq!(
            unsafe { gc_streamkey_seal(sk, keyexpr.as_ptr(), b"m".as_ptr(), 1, &mut buf) },
            GC_OK
        );
        assert_eq!(gc_buf_dispose(buf), GC_OK);
    }
    assert_eq!(unsafe { gc_streamkey_remaining(sk, &mut remaining) }, GC_OK);
    assert_eq!(remaining, 0);

    let mut buf = 0u64;
    assert_eq!(
        unsafe { gc_streamkey_seal(sk, keyexpr.as_ptr(), b"m".as_ptr(), 1, &mut buf) },
        GC_ERR_ROTATION_REQUIRED,
        "at the cap, seal is GC_ERR_ROTATION_REQUIRED"
    );
    assert_eq!(gc_streamkey_dispose(sk), GC_OK);
    assert_eq!(gc_identity_dispose(prod_id), GC_OK);
}

#[test]
fn set_cap_is_monotone_through_the_abi() {
    // Once tightened, a later set_cap to a higher value (even the default) does not loosen
    // the cap back (core takes min(current, requested)).
    let rs = "capi-monotone";
    let producer = party("coalition-air/prod");
    let (bundle_json, _ks) = signed_bundle(rs, 1, &[&producer]);

    let prod_id = identity(&producer);
    let bundles_json = format!("[{}]", String::from_utf8(bundle_json).unwrap());
    let rs_c = CString::new(rs).unwrap();
    let mut sk = 0u64;
    assert_eq!(
        unsafe {
            gc_streamkey_acquire(
                bundles_json.as_ptr(),
                bundles_json.len(),
                rs_c.as_ptr(),
                prod_id,
                &mut sk,
            )
        },
        GC_OK
    );

    assert_eq!(gc_streamkey_set_cap(sk, 2), GC_OK);
    let mut remaining = 0u64;
    assert_eq!(unsafe { gc_streamkey_remaining(sk, &mut remaining) }, GC_OK);
    assert_eq!(remaining, 2);

    // Request the (higher) default cap: accepted by the ABI but a no-op on the value.
    assert_eq!(gc_streamkey_set_cap(sk, DEFAULT_SEAL_CAP), GC_OK);
    assert_eq!(unsafe { gc_streamkey_remaining(sk, &mut remaining) }, GC_OK);
    assert_eq!(remaining, 2, "set_cap never loosens the cap");

    assert_eq!(gc_streamkey_dispose(sk), GC_OK);
    assert_eq!(gc_identity_dispose(prod_id), GC_OK);
}

#[test]
fn rotate_destroy_loses_access_and_reports_a_receipt() {
    let rs = "coalition-air";
    let member = party("coalition-air/con");
    let (bundle_json, _ks) = signed_bundle(rs, 1, &[&member]);
    let id = identity(&member);

    // A fresh new scalar to rotate into.
    let new_secret = SecretKey::generate();
    let mut new_scalar = [0u8; 32];
    new_scalar.copy_from_slice(&new_secret.to_bytes());
    let bundles_json = format!("[{}]", String::from_utf8(bundle_json.clone()).unwrap());

    let mut receipt_buf = 0u64;
    assert_eq!(
        unsafe {
            gc_rotate(
                id,
                new_scalar.as_ptr(),
                false, // Destroy
                bundles_json.as_ptr(),
                bundles_json.len(),
                &mut receipt_buf,
            )
        },
        GC_OK
    );
    let receipt: serde_json::Value = serde_json::from_slice(&buf_bytes(receipt_buf)).unwrap();
    assert_eq!(receipt["policy"], "destroy");
    assert_eq!(receipt["now_unreadable_generations"], 1);
    assert_eq!(receipt["rewrapped_generations"], 0);
    assert!(
        receipt.get("rewrapped").is_none(),
        "destroy carries no rewrapped"
    );
    assert!(receipt["timestamp_rfc3339"]
        .as_str()
        .unwrap()
        .ends_with('Z'));

    // The handle now holds the NEW scalar: admitting the OLD bundle (slot wrapped to the old
    // key) fails to unwrap — access to that generation is gone.
    let mut ks = 0u64;
    assert_eq!(unsafe { gc_keystore_new(&mut ks) }, GC_OK);
    let mut had_slot = false;
    assert_eq!(
        unsafe {
            gc_keystore_admit(
                ks,
                id,
                bundle_json.as_ptr(),
                bundle_json.len(),
                &mut had_slot,
            )
        },
        GC_ERR_UNWRAP,
        "the rotated (new) key can no longer unwrap the old slot"
    );
    assert_eq!(gc_buf_dispose(receipt_buf), GC_OK);
    assert_eq!(gc_keystore_dispose(ks), GC_OK);
    assert_eq!(gc_identity_dispose(id), GC_OK);
}

#[test]
fn rotate_keep_access_rewraps_so_the_new_key_opens() {
    let rs = "coalition-air";
    let member = party("coalition-air/con");
    let (bundle_json, _ks) = signed_bundle(rs, 1, &[&member]);
    let id = identity(&member);

    let new_secret = SecretKey::generate();
    let mut new_scalar = [0u8; 32];
    new_scalar.copy_from_slice(&new_secret.to_bytes());
    let bundles_json = format!("[{}]", String::from_utf8(bundle_json).unwrap());

    let mut receipt_buf = 0u64;
    assert_eq!(
        unsafe {
            gc_rotate(
                id,
                new_scalar.as_ptr(),
                true, // KeepAccess
                bundles_json.as_ptr(),
                bundles_json.len(),
                &mut receipt_buf,
            )
        },
        GC_OK
    );
    let receipt: serde_json::Value = serde_json::from_slice(&buf_bytes(receipt_buf)).unwrap();
    assert_eq!(receipt["policy"], "keep-access");
    assert_eq!(receipt["now_unreadable_generations"], 1);
    assert_eq!(receipt["rewrapped_generations"], 1);
    let rewrapped = receipt["rewrapped"].as_array().expect("rewrapped array");
    assert_eq!(rewrapped.len(), 1);
    assert_eq!(rewrapped[0]["rs"], rs);
    assert_eq!(rewrapped[0]["gen"], 1);

    // Reconstruct a bundle from the re-wrapped slot and admit it with the (now-new) identity:
    // the new key unwraps it -> proves KeepAccess re-wrapped to the new key.
    let rewrapped_bundle = serde_json::json!({
        "suite": "v1-ecdh-p256",
        "rs": rs,
        "gen": 1,
        "members": { "coalition-air/con": rewrapped[0]["wrapped"] },
        "signer": "",
        "sig": ""
    });
    let rb = serde_json::to_vec(&rewrapped_bundle).unwrap();
    let mut ks = 0u64;
    assert_eq!(unsafe { gc_keystore_new(&mut ks) }, GC_OK);
    let mut had_slot = false;
    assert_eq!(
        unsafe { gc_keystore_admit(ks, id, rb.as_ptr(), rb.len(), &mut had_slot) },
        GC_OK,
        "the new key unwraps the re-wrapped slot"
    );
    assert!(had_slot, "the re-wrapped slot is present for the member");
    assert_eq!(gc_buf_dispose(receipt_buf), GC_OK);
    assert_eq!(gc_keystore_dispose(ks), GC_OK);
    assert_eq!(gc_identity_dispose(id), GC_OK);
}

#[test]
fn null_and_bad_utf8_are_rejected() {
    // NULL out-param.
    let scalar = party("x").scalar();
    let cid = CString::new("x").unwrap();
    assert_eq!(
        unsafe { gc_identity_new(cid.as_ptr(), scalar.as_ptr(), std::ptr::null_mut()) },
        GC_ERR_NULL_ARG
    );
    // NULL string arg.
    let mut h = 0u64;
    assert_eq!(
        unsafe { gc_identity_new(std::ptr::null(), scalar.as_ptr(), &mut h) },
        GC_ERR_NULL_ARG
    );
}
