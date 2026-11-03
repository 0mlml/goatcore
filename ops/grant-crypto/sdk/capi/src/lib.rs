//! grant-crypto-capi — the Tier-1 C ABI (ADR 1106), the single audited FFI seam.
//!
//! The frozen contract is [`docs/design/grant-crypto-sdk.md`](../../../../docs/design/grant-crypto-sdk.md)
//! §"C ABI contract"; where this code and that doc conflict, the doc wins. Two
//! load-bearing properties, enforced here by construction:
//!
//! - **Opaque handles only.** Every live object (identity, keystore, stream key,
//!   output buffer) is a `u64` id into a process-global, `Mutex`-guarded registry.
//!   Key material NEVER crosses the boundary as bytes — only handles. Wire
//!   structures (bundle / envelope JSON) cross as bytes; plaintext crosses via a
//!   `gc_buf` (payload data, allowed).
//! - **Fail-closed lifecycle.** Handle ids are monotone and never recycled, so any
//!   use after `*_dispose` finds no entry and returns [`GC_ERR_DISPOSED_HANDLE`] —
//!   never a dereference of freed state; double-dispose is the same harmless error.
//!   Dispose zeroizes the entry's secret material (best-effort) via `Drop`.
//!
//! No algorithm/nonce/RNG appears in any signature (suite-agnostic by
//! construction); the producer stream-key path (highest-gen acquire, the enforced
//! seal cap) and the `rotate` verb are the core's `grant_crypto::datapath` /
//! `grant_crypto::rotate` types, held behind opaque handles. The consumer keystore is
//! likewise the core `grant_crypto::datapath::KeyStore` behind a handle — admit/open
//! delegate straight to it, so its zeroize-on-drop rides the core type (no bespoke DEK
//! map in this layer).
#![allow(clippy::missing_safety_doc)] // The ABI safety contract is documented centrally in grant-crypto-sdk.md.

use std::collections::HashMap;
use std::ffi::{c_char, CStr};
use std::sync::{Mutex, MutexGuard, OnceLock};

use grant_crypto::datapath::{
    acquire_stream_key, Context, KeyStore as CoreKeyStore, StreamKey as CoreStreamKey,
    DEFAULT_SEAL_CAP,
};
use grant_crypto::rotate::{rotate, Identity, RotationPolicy};
use grant_crypto::{
    secret_from_scalar, verify_bundle, verifying_key_from_spki, Envelope, Error as CoreError,
    KeyBundle,
};
use zeroize::Zeroize;

// ---- status codes (frozen; core 1:1 then ABI-layer) ----

/// Success.
pub const GC_OK: i32 = 0;
/// The named suite is not implemented (capability signal, pre-key).
pub const GC_ERR_UNSUPPORTED_SUITE: i32 = -1;
/// AEAD authentication failed (wrong key/gen/rs/topic, tamper).
pub const GC_ERR_DECRYPT: i32 = -2;
/// The wrapped DEK could not be recovered.
pub const GC_ERR_UNWRAP: i32 = -3;
/// The bundle signature did not verify.
pub const GC_ERR_BAD_SIGNATURE: i32 = -4;
/// The member has no slot / no held key (not in the audience).
pub const GC_ERR_NO_SLOT: i32 = -5;
/// A field was malformed (bad base64/length/key encoding, bad JSON).
pub const GC_ERR_MALFORMED: i32 = -6;
/// The stream key hit its message-count cap and refuses to seal until rotation (the
/// enforced nonce-safety cap). A write-side reject, never a decrypt oracle. Additive to
/// the core's six-variant taxonomy for the enforced cap; frozen at this value thereafter.
pub const GC_ERR_ROTATION_REQUIRED: i32 = -7;
/// A required pointer argument was NULL.
/// The held stream key's generation was superseded — a newer generation exists for this
/// release set. A write-side local refusal in the same family as
/// `GC_ERR_ROTATION_REQUIRED`; nothing on the wire observes it.
pub const GC_ERR_SUPERSEDED: i32 = -8;
/// An access change did not take effect: the bundle read back still grants a slot to a
/// member that should have lost one, or withholds one it should have gained.
pub const GC_ERR_AUDIENCE_NOT_APPLIED: i32 = -9;
pub const GC_ERR_NULL_ARG: i32 = -100;
/// A handle was already disposed, never issued, or of the wrong type.
pub const GC_ERR_DISPOSED_HANDLE: i32 = -101;
/// A string argument was not valid UTF-8.
pub const GC_ERR_BAD_UTF8: i32 = -102;
/// An internal invariant failed (e.g. serialization of a value we built).
pub const GC_ERR_INTERNAL: i32 = -103;

fn map_core_err(e: CoreError) -> i32 {
    match e {
        CoreError::UnsupportedSuite => GC_ERR_UNSUPPORTED_SUITE,
        CoreError::Decrypt => GC_ERR_DECRYPT,
        CoreError::Unwrap => GC_ERR_UNWRAP,
        CoreError::BadSignature => GC_ERR_BAD_SIGNATURE,
        CoreError::NoSlot => GC_ERR_NO_SLOT,
        CoreError::Malformed => GC_ERR_MALFORMED,
        CoreError::RotationRequired => GC_ERR_ROTATION_REQUIRED,
        CoreError::Superseded => GC_ERR_SUPERSEDED,
        CoreError::AudienceNotApplied => GC_ERR_AUDIENCE_NOT_APPLIED,
    }
}

/// The ABI version. Monotone integer; core + wrappers ship in lockstep from one
/// tag, so there is no cross-version ABI promise (SONAME-pinned).
#[no_mangle]
pub extern "C" fn gc_abi_version() -> u32 {
    1
}

/// A static, human-readable string for a status code (diagnostics only — never
/// an oracle; the caller must not branch on the text). The returned pointer is
/// static and must not be freed.
#[no_mangle]
pub extern "C" fn gc_error_message(code: i32) -> *const c_char {
    let s: &CStr = match code {
        GC_OK => c"ok",
        GC_ERR_UNSUPPORTED_SUITE => c"unsupported suite",
        GC_ERR_DECRYPT => c"decrypt failed",
        GC_ERR_UNWRAP => c"unwrap failed",
        GC_ERR_BAD_SIGNATURE => c"bad signature",
        GC_ERR_NO_SLOT => c"no slot",
        GC_ERR_MALFORMED => c"malformed input",
        GC_ERR_ROTATION_REQUIRED => c"rotation required (seal cap reached)",
        GC_ERR_NULL_ARG => c"null argument",
        GC_ERR_DISPOSED_HANDLE => c"disposed or invalid handle",
        GC_ERR_BAD_UTF8 => c"invalid utf-8",
        GC_ERR_INTERNAL => c"internal error",
        _ => c"unknown status",
    };
    s.as_ptr()
}

// ---- process-global handle registry ----

/// One managed object. Secret-bearing variants zeroize on `Drop`, so removal from
/// the registry (dispose) wipes key material best-effort.
enum Entry {
    Identity(IdentityEnt),
    KeyStore(KeyStoreEnt),
    StreamKey(StreamKeyEnt),
    Buffer(BufferEnt),
}

/// A member's id + private scalar. The scalar is stored raw and re-derived into a
/// `SecretKey` at each unwrap (so capi names no `p256` type); zeroized on dispose.
struct IdentityEnt {
    member_id: String,
    scalar: [u8; 32],
}
impl Drop for IdentityEnt {
    fn drop(&mut self) {
        self.scalar.zeroize();
    }
}

/// The consumer keystore — the core [`CoreKeyStore`], which owns the `(rs, gen)`-keyed
/// DEK map and zeroizes every held DEK on its own `Drop` (so this newtype needs no manual
/// `Drop`). Mirrors how [`StreamKeyEnt`] wraps the core stream key.
#[derive(Default)]
struct KeyStoreEnt(CoreKeyStore);

/// A producer's acquired stream key — the core [`CoreStreamKey`], which owns the DEK, the
/// seal counter, and the cap, and zeroizes the DEK on its own `Drop` (so this newtype needs
/// no manual `Drop`).
struct StreamKeyEnt(CoreStreamKey);

/// An output buffer carrying payload bytes (plaintext or a serialized envelope).
/// Contents zeroized on dispose — plaintext is sensitive even though it is allowed
/// to cross the boundary.
struct BufferEnt(Vec<u8>);
impl Drop for BufferEnt {
    fn drop(&mut self) {
        self.0.zeroize();
    }
}

#[derive(Default)]
struct Registry {
    next: u64,
    entries: HashMap<u64, Entry>,
}

impl Registry {
    /// Allocate a fresh, never-recycled id for `e`.
    fn alloc(&mut self, e: Entry) -> u64 {
        // ids start at 1 so a zeroed handle (0) is always invalid.
        self.next += 1;
        let id = self.next;
        self.entries.insert(id, e);
        id
    }
}

fn registry() -> MutexGuard<'static, Registry> {
    static REG: OnceLock<Mutex<Registry>> = OnceLock::new();
    REG.get_or_init(|| Mutex::new(Registry::default()))
        // Recover a poisoned lock rather than unwind across the FFI boundary (UB).
        .lock()
        .unwrap_or_else(|e| e.into_inner())
}

// ---- small unsafe helpers (null / utf8 / slice checks) ----

unsafe fn cstr<'a>(p: *const c_char) -> Result<&'a str, i32> {
    if p.is_null() {
        return Err(GC_ERR_NULL_ARG);
    }
    CStr::from_ptr(p).to_str().map_err(|_| GC_ERR_BAD_UTF8)
}

unsafe fn slice<'a>(p: *const u8, len: usize) -> Result<&'a [u8], i32> {
    if p.is_null() {
        return Err(GC_ERR_NULL_ARG);
    }
    Ok(std::slice::from_raw_parts(p, len))
}

fn parse_bundle(bytes: &[u8]) -> Result<KeyBundle, i32> {
    serde_json::from_slice(bytes).map_err(|_| GC_ERR_MALFORMED)
}

// ---- identity ----

/// Construct a member identity from its id and raw 32-byte P-256 scalar.
/// **`scalar32` is read as exactly 32 bytes — there is no length parameter; passing a
/// shorter buffer is an out-of-bounds read. Bindings MUST length-check before calling.**
/// Validates
/// the scalar (`GC_ERR_MALFORMED` if not a valid key). Writes the handle to `out`.
#[no_mangle]
pub unsafe extern "C" fn gc_identity_new(
    member_id: *const c_char,
    scalar32: *const u8,
    out: *mut u64,
) -> i32 {
    if out.is_null() {
        return GC_ERR_NULL_ARG;
    }
    let id = match cstr(member_id) {
        Ok(s) => s.to_string(),
        Err(code) => return code,
    };
    let sc = match slice(scalar32, 32) {
        Ok(s) => s,
        Err(code) => return code,
    };
    let mut scalar = [0u8; 32];
    scalar.copy_from_slice(sc);
    // Validate now so a bad credential fails at construction, not at admit.
    if secret_from_scalar(&scalar).is_err() {
        scalar.zeroize();
        return GC_ERR_MALFORMED;
    }
    let handle = registry().alloc(Entry::Identity(IdentityEnt {
        member_id: id,
        scalar,
    }));
    *out = handle;
    GC_OK
}

/// Dispose an identity handle (zeroizes the scalar). Double-dispose / wrong-type is
/// `GC_ERR_DISPOSED_HANDLE`.
#[no_mangle]
pub extern "C" fn gc_identity_dispose(h: u64) -> i32 {
    dispose_if(h, |e| matches!(e, Entry::Identity(_)))
}

// ---- keystore (consumer) ----

/// Create an empty keystore. Writes the handle to `out`.
#[no_mangle]
pub unsafe extern "C" fn gc_keystore_new(out: *mut u64) -> i32 {
    if out.is_null() {
        return GC_ERR_NULL_ARG;
    }
    let handle = registry().alloc(Entry::KeyStore(KeyStoreEnt::default()));
    *out = handle;
    GC_OK
}

/// Dispose a keystore handle (zeroizes all held DEKs).
#[no_mangle]
pub extern "C" fn gc_keystore_dispose(h: u64) -> i32 {
    dispose_if(h, |e| matches!(e, Entry::KeyStore(_)))
}

/// Admit a (trusted) bundle for the given identity: unwrap this member's slot and
/// retain the DEK for `(bundle.rs, bundle.gen)`. `out_had_slot` = whether the member
/// had a slot (a normal `false` for an outsider — nothing retained, not an error);
/// `Err` only on a malformed slot. The caller must have verified the bundle first
/// (`gc_verify_bundle`).
#[no_mangle]
pub unsafe extern "C" fn gc_keystore_admit(
    ks: u64,
    id: u64,
    bundle_json: *const u8,
    len: usize,
    out_had_slot: *mut bool,
) -> i32 {
    if out_had_slot.is_null() {
        return GC_ERR_NULL_ARG;
    }
    let bytes = match slice(bundle_json, len) {
        Ok(b) => b,
        Err(code) => return code,
    };
    let bundle = match parse_bundle(bytes) {
        Ok(b) => b,
        Err(code) => return code,
    };

    let mut reg = registry();
    // Copy the identity's material out first (releases the shared borrow before the
    // mutable keystore borrow); the local scalar is zeroized after use.
    let (member_id, mut scalar) = match reg.entries.get(&id) {
        Some(Entry::Identity(e)) => (e.member_id.clone(), e.scalar),
        _ => return GC_ERR_DISPOSED_HANDLE,
    };

    let result = (|| {
        let secret = match secret_from_scalar(&scalar) {
            Ok(s) => s,
            Err(e) => return map_core_err(e),
        };
        // Delegate to the core KeyStore: it does the no-slot (Ok(false)) / unwrap / retain
        // decision and owns the zeroize-on-drop of every held DEK.
        match reg.entries.get_mut(&ks) {
            Some(Entry::KeyStore(k)) => match k.0.admit(&bundle, &member_id, &secret) {
                Ok(had_slot) => {
                    *out_had_slot = had_slot;
                    GC_OK
                }
                Err(e) => map_core_err(e),
            },
            _ => GC_ERR_DISPOSED_HANDLE,
        }
    })();
    scalar.zeroize();
    result
}

/// Open a data message received on `keyexpr` (its in-band JSON body) with the held
/// DEK for the message's `(rs, gen)`. `GC_ERR_NO_SLOT` if none held. On success
/// writes a `gc_buf` handle carrying the plaintext to `out_buf`.
#[no_mangle]
pub unsafe extern "C" fn gc_keystore_open(
    ks: u64,
    keyexpr: *const c_char,
    envelope_json: *const u8,
    len: usize,
    out_buf: *mut u64,
) -> i32 {
    if out_buf.is_null() {
        return GC_ERR_NULL_ARG;
    }
    let ke = match cstr(keyexpr) {
        Ok(s) => s,
        Err(code) => return code,
    };
    let bytes = match slice(envelope_json, len) {
        Ok(b) => b,
        Err(code) => return code,
    };
    let env: Envelope = match serde_json::from_slice(bytes) {
        Ok(e) => e,
        Err(_) => return GC_ERR_MALFORMED,
    };

    let mut reg = registry();
    // Delegate the open to the core KeyStore (its DEK map, its AEAD). The owned Result
    // releases the borrow before we allocate the output buffer.
    let opened = match reg.entries.get(&ks) {
        Some(Entry::KeyStore(k)) => k.0.open(Context(ke), &env),
        _ => return GC_ERR_DISPOSED_HANDLE,
    };
    match opened {
        Ok(pt) => {
            let handle = reg.alloc(Entry::Buffer(BufferEnt(pt)));
            *out_buf = handle;
            GC_OK
        }
        Err(e) => map_core_err(e),
    }
}

// ---- stream key (producer) ----

/// Acquire the producer's stream key from a JSON array of bundles: pick the highest
/// generation of `rs` in which this identity has a slot and unwrap it.
/// `GC_ERR_NO_SLOT` if the producer isn't in any visible audience. Writes the handle
/// to `out`.
#[no_mangle]
pub unsafe extern "C" fn gc_streamkey_acquire(
    bundles_json: *const u8,
    len: usize,
    rs: *const c_char,
    id: u64,
    out: *mut u64,
) -> i32 {
    if out.is_null() {
        return GC_ERR_NULL_ARG;
    }
    let rs = match cstr(rs) {
        Ok(s) => s,
        Err(code) => return code,
    };
    let bytes = match slice(bundles_json, len) {
        Ok(b) => b,
        Err(code) => return code,
    };
    let bundles: Vec<KeyBundle> = match serde_json::from_slice(bytes) {
        Ok(b) => b,
        Err(_) => return GC_ERR_MALFORMED,
    };

    let mut reg = registry();
    let (member_id, mut scalar) = match reg.entries.get(&id) {
        Some(Entry::Identity(e)) => (e.member_id.clone(), e.scalar),
        _ => return GC_ERR_DISPOSED_HANDLE,
    };

    let result = (|| {
        let secret = match secret_from_scalar(&scalar) {
            Ok(s) => s,
            Err(e) => return map_core_err(e),
        };
        let sk = match acquire_stream_key(rs, &bundles, &member_id, &secret) {
            Ok(s) => s,
            Err(e) => return map_core_err(e),
        };
        let handle = reg.alloc(Entry::StreamKey(StreamKeyEnt(sk)));
        *out = handle;
        GC_OK
    })();
    scalar.zeroize();
    result
}

/// Tighten the seal cap of an acquired stream key. **Monotone**: the core sets
/// `cap = min(current_cap, requested)`, so a request can only lower the cap, never raise it
/// (a request at or above the current cap is a no-op). The ABI additionally refuses a
/// request **above** [`DEFAULT_SEAL_CAP`] with `GC_ERR_MALFORMED`. `GC_ERR_DISPOSED_HANDLE`
/// for a bad handle.
#[no_mangle]
pub extern "C" fn gc_streamkey_set_cap(sk: u64, cap: u64) -> i32 {
    if cap > DEFAULT_SEAL_CAP {
        return GC_ERR_MALFORMED;
    }
    let mut reg = registry();
    match reg.entries.get_mut(&sk) {
        Some(Entry::StreamKey(s)) => {
            s.0.set_cap(cap);
            GC_OK
        }
        _ => GC_ERR_DISPOSED_HANDLE,
    }
}

/// Write the number of messages that may still be sealed under this key before rotation is
/// required to `out` (the structured approach-to-cap fact). `GC_ERR_DISPOSED_HANDLE` for a
/// bad handle; `GC_ERR_NULL_ARG` for a NULL out-param.
#[no_mangle]
pub unsafe extern "C" fn gc_streamkey_remaining(sk: u64, out: *mut u64) -> i32 {
    if out.is_null() {
        return GC_ERR_NULL_ARG;
    }
    let reg = registry();
    match reg.entries.get(&sk) {
        Some(Entry::StreamKey(s)) => {
            *out = s.0.remaining();
            GC_OK
        }
        _ => GC_ERR_DISPOSED_HANDLE,
    }
}

/// Dispose a stream-key handle (zeroizes the DEK).
#[no_mangle]
pub extern "C" fn gc_streamkey_dispose(h: u64) -> i32 {
    dispose_if(h, |e| matches!(e, Entry::StreamKey(_)))
}

/// Seal `plaintext` for publication on `keyexpr` under the acquired stream key.
/// `keyexpr` is bound as AEAD additional data. On success writes a `gc_buf` handle
/// carrying the serialized `Envelope` (the in-band wire body) to `out_buf`.
#[no_mangle]
pub unsafe extern "C" fn gc_streamkey_seal(
    sk: u64,
    keyexpr: *const c_char,
    plaintext: *const u8,
    len: usize,
    out_buf: *mut u64,
) -> i32 {
    if out_buf.is_null() {
        return GC_ERR_NULL_ARG;
    }
    let ke = match cstr(keyexpr) {
        Ok(s) => s,
        Err(code) => return code,
    };
    let pt = match slice(plaintext, len) {
        Ok(b) => b,
        Err(code) => return code,
    };

    let mut reg = registry();
    // Seal through the core stream key (enforces the cap + advances the counter); the owned
    // Result releases the &mut borrow before we allocate the output buffer.
    let sealed = match reg.entries.get_mut(&sk) {
        Some(Entry::StreamKey(s)) => s.0.seal(Context(ke), pt),
        _ => return GC_ERR_DISPOSED_HANDLE,
    };
    let env = match sealed {
        Ok(e) => e,
        Err(e) => return map_core_err(e),
    };
    let body = match serde_json::to_vec(&env) {
        Ok(b) => b,
        Err(_) => return GC_ERR_INTERNAL,
    };
    let handle = reg.alloc(Entry::Buffer(BufferEnt(body)));
    *out_buf = handle;
    GC_OK
}

// ---- rotate (member-leaf forward-secrecy hygiene) ----

/// Rotate the identity handle's leaf key to `new_scalar32` (raw 32-byte P-256 scalar —
/// **read as exactly 32 bytes, no length parameter; bindings MUST length-check, a shorter
/// buffer is an out-of-bounds read**). On
/// success the handle **now holds the new scalar** (in-place swap; the old scalar is
/// zeroized). `keep_access = false` is the `Destroy` default (drop the old key, re-wrap
/// nothing — full forward secrecy); `keep_access = true` re-wraps the member's still-live
/// DEKs from `bundles_json` (a JSON array of bundles) to the new key locally, reducing
/// forward secrecy for those generations. Writes a `gc_buf` handle carrying the receipt JSON
/// to `out_receipt_json`; under `keep_access` the re-wrapped entries ride under a
/// `"rewrapped"` key of the same object. `GC_ERR_MALFORMED` for a bad scalar / bad JSON.
#[no_mangle]
pub unsafe extern "C" fn gc_rotate(
    identity: u64,
    new_scalar32: *const u8,
    keep_access: bool,
    bundles_json: *const u8,
    len: usize,
    out_receipt_json: *mut u64,
) -> i32 {
    if out_receipt_json.is_null() {
        return GC_ERR_NULL_ARG;
    }
    let new_sc = match slice(new_scalar32, 32) {
        Ok(s) => s,
        Err(code) => return code,
    };
    let bytes = match slice(bundles_json, len) {
        Ok(b) => b,
        Err(code) => return code,
    };
    let bundles: Vec<KeyBundle> = match serde_json::from_slice(bytes) {
        Ok(b) => b,
        Err(_) => return GC_ERR_MALFORMED,
    };
    let mut new_scalar = [0u8; 32];
    new_scalar.copy_from_slice(new_sc);

    let mut reg = registry();
    let (member_id, mut old_scalar) = match reg.entries.get(&identity) {
        Some(Entry::Identity(e)) => (e.member_id.clone(), e.scalar),
        _ => {
            new_scalar.zeroize();
            return GC_ERR_DISPOSED_HANDLE;
        }
    };

    // Build the old identity + the new secret from raw scalars (the wire functions run
    // entirely local; the platform is never involved).
    let old_id = match Identity::from_scalar(member_id, &old_scalar) {
        Ok(i) => i,
        Err(e) => {
            old_scalar.zeroize();
            new_scalar.zeroize();
            return map_core_err(e);
        }
    };
    let new_secret = match secret_from_scalar(&new_scalar) {
        Ok(s) => s,
        Err(e) => {
            old_scalar.zeroize();
            new_scalar.zeroize();
            return map_core_err(e);
        }
    };
    let policy = if keep_access {
        RotationPolicy::KeepAccess
    } else {
        RotationPolicy::Destroy
    };
    let (_new_identity, receipt, rewrapped) = rotate(old_id, new_secret, policy, &bundles);
    old_scalar.zeroize();

    // In-place swap: the handle now holds the new scalar; wipe the retired one.
    match reg.entries.get_mut(&identity) {
        Some(Entry::Identity(e)) => {
            e.scalar.zeroize();
            e.scalar = new_scalar;
        }
        _ => {
            new_scalar.zeroize();
            return GC_ERR_DISPOSED_HANDLE;
        }
    }
    new_scalar.zeroize(); // the local is a Copy of the stored scalar; wipe it too

    // Receipt (+ rewrapped entries under "rewrapped" when keep_access) as one JSON buffer.
    let mut value = match serde_json::to_value(&receipt) {
        Ok(v) => v,
        Err(_) => return GC_ERR_INTERNAL,
    };
    if keep_access {
        match serde_json::to_value(&rewrapped) {
            Ok(rw) => value["rewrapped"] = rw,
            Err(_) => return GC_ERR_INTERNAL,
        }
    }
    let body = match serde_json::to_vec(&value) {
        Ok(b) => b,
        Err(_) => return GC_ERR_INTERNAL,
    };
    let handle = reg.alloc(Entry::Buffer(BufferEnt(body)));
    *out_receipt_json = handle;
    GC_OK
}

// ---- bundle verification (no handle; pure) ----

/// Verify a bundle's signature against the key-service verifying key (SPKI DER).
/// `GC_OK` on success; `GC_ERR_BAD_SIGNATURE` / `GC_ERR_MALFORMED` otherwise.
#[no_mangle]
pub unsafe extern "C" fn gc_verify_bundle(
    bundle_json: *const u8,
    bundle_len: usize,
    vk_spki_der: *const u8,
    vk_len: usize,
) -> i32 {
    let bbytes = match slice(bundle_json, bundle_len) {
        Ok(b) => b,
        Err(code) => return code,
    };
    let vkbytes = match slice(vk_spki_der, vk_len) {
        Ok(b) => b,
        Err(code) => return code,
    };
    let bundle = match parse_bundle(bbytes) {
        Ok(b) => b,
        Err(code) => return code,
    };
    let vk = match verifying_key_from_spki(vkbytes) {
        Ok(k) => k,
        Err(e) => return map_core_err(e),
    };
    match verify_bundle(&bundle, &vk) {
        Ok(()) => GC_OK,
        Err(e) => map_core_err(e),
    }
}

// ---- output buffers (payload bytes) ----

/// Pointer to a buffer's bytes, valid until `gc_buf_dispose`. NULL for an invalid
/// handle.
#[no_mangle]
pub extern "C" fn gc_buf_ptr(h: u64) -> *const u8 {
    match registry().entries.get(&h) {
        Some(Entry::Buffer(b)) => b.0.as_ptr(),
        _ => std::ptr::null(),
    }
}

/// Length of a buffer's bytes. 0 for an invalid handle.
#[no_mangle]
pub extern "C" fn gc_buf_len(h: u64) -> usize {
    match registry().entries.get(&h) {
        Some(Entry::Buffer(b)) => b.0.len(),
        _ => 0,
    }
}

/// Dispose a buffer handle (zeroizes the contents).
#[no_mangle]
pub extern "C" fn gc_buf_dispose(h: u64) -> i32 {
    dispose_if(h, |e| matches!(e, Entry::Buffer(_)))
}

// ---- shared dispose ----

/// Remove `h` iff it is present and matches `is_kind`, dropping (and zeroizing) its
/// entry. A missing handle, an already-disposed handle, or a wrong-type handle all
/// return `GC_ERR_DISPOSED_HANDLE` — never a dereference of freed state.
fn dispose_if(h: u64, is_kind: impl Fn(&Entry) -> bool) -> i32 {
    let mut reg = registry();
    match reg.entries.get(&h) {
        Some(e) if is_kind(e) => {
            reg.entries.remove(&h);
            GC_OK
        }
        _ => GC_ERR_DISPOSED_HANDLE,
    }
}
