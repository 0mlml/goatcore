//! The member-leaf `rotate` verb (frozen API, `docs/design/grant-crypto-sdk.md`;
//! semantics resolved in the prior-art survey §3, composes with ADR 1044 leaf re-issue).
//!
//! Rotation is **endpoint-enforced forward secrecy**: it happens entirely on the member's
//! own machine, and the platform is never involved (it holds no keys and can never
//! re-wrap). Two policies:
//!
//! - [`RotationPolicy::Destroy`] (the default): drop the retired private key — nothing is
//!   re-wrapped. The member loses the ability to re-fetch pre-rotation history from
//!   retained bundles, and *that loss is the forward secrecy*: a future theft of the new
//!   key reveals nothing sealed to the old one.
//! - [`RotationPolicy::KeepAccess`] (explicit opt-out): before dropping the old key,
//!   locally unwrap the member's still-live DEKs and re-wrap each to the new public key,
//!   on this machine. **This REDUCES forward secrecy** for those generations — a theft of
//!   the new key now reaches them.
//!
//! Every rotation emits a [`RotationReceipt`]: a structured, self-dating fact (never
//! time-relative prose) the caller persists to its ledger.

#[cfg(test)]
use p256::elliptic_curve::Generate;
use p256::{PublicKey, SecretKey};
use serde::Serialize;
use std::time::{SystemTime, UNIX_EPOCH};
use zeroize::Zeroize;

use crate::{secret_from_scalar, unwrap_dek, wrap_dek, Error, KeyBundle, WrappedDek};

/// How a rotation treats the retiring key's access to pre-rotation generations.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum RotationPolicy {
    /// Drop the retired key, re-wrap nothing (the stated default — full forward secrecy).
    #[default]
    Destroy,
    /// Re-wrap the member's still-live DEKs to the new key before dropping the old one —
    /// labeled as reducing forward secrecy for those generations.
    KeepAccess,
}

impl RotationPolicy {
    /// The stable, serialization-facing name (goes verbatim into the receipt).
    pub fn as_str(self) -> &'static str {
        match self {
            RotationPolicy::Destroy => "destroy",
            RotationPolicy::KeepAccess => "keep-access",
        }
    }
}

/// A member's id + private key material — unwrap-capable, never exposing the secret by
/// value. Constructed from the member's enrollment credential (a raw P-256 scalar).
pub struct Identity {
    /// The member id this identity unwraps slots for.
    pub member_id: String,
    secret: SecretKey,
}

impl Identity {
    /// Wrap an already-parsed secret key with its member id.
    pub fn new(member_id: impl Into<String>, secret: SecretKey) -> Self {
        Self {
            member_id: member_id.into(),
            secret,
        }
    }

    /// Construct from a raw 32-byte P-256 scalar (the enrollment credential form).
    /// `Malformed` if the scalar is not a valid key.
    pub fn from_scalar(member_id: impl Into<String>, scalar: &[u8; 32]) -> Result<Self, Error> {
        Ok(Self {
            member_id: member_id.into(),
            secret: secret_from_scalar(scalar)?,
        })
    }

    /// Borrow the secret key for an unwrap. Deliberately a borrow, never a by-value copy —
    /// the secret zeroizes on drop (`p256::SecretKey: ZeroizeOnDrop`) and never leaves the
    /// `Identity`. `pub` because the datapath/capi need it to drive the wire functions.
    pub fn secret(&self) -> &SecretKey {
        &self.secret
    }
}

/// One DEK re-wrapped to the new key under `KeepAccess`, for the caller to persist so the
/// member can keep opening that generation after the old key is gone.
#[derive(Debug, Serialize)]
pub struct ReWrappedDek {
    pub rs: String,
    pub gen: u64,
    pub wrapped: WrappedDek,
}

/// The structured, self-dating record of a rotation (serde-serializable). Carries counts,
/// not prose: how many pre-rotation generations became unreadable-from-the-platform, how
/// many were locally re-wrapped, and an absolute RFC3339 UTC timestamp.
#[derive(Debug, Serialize)]
pub struct RotationReceipt {
    pub member_id: String,
    pub policy: &'static str,
    /// Distinct `(rs, gen)` generations this member had a slot in among the visible
    /// bundles: after the old key is dropped these can no longer be re-fetched from those
    /// bundles (they were wrapped to the retiring key).
    pub now_unreadable_generations: u64,
    /// Distinct generations re-wrapped to the new key (`KeepAccess` only; `0` for `Destroy`).
    pub rewrapped_generations: u64,
    /// Absolute UTC instant of the rotation, RFC3339 (`YYYY-MM-DDTHH:MM:SSZ`).
    pub timestamp_rfc3339: String,
}

/// Rotate a member's leaf key. Consumes the old [`Identity`] (its secret is dropped — and
/// therefore zeroized — before returning) and returns the new identity, the receipt, and,
/// under `KeepAccess`, the DEKs re-wrapped to the new key for the caller to persist.
///
/// The platform is never involved: all unwrapping and re-wrapping is local, using the
/// existing wire functions. `Destroy` returns an empty re-wrap vector.
pub fn rotate(
    old: Identity,
    new_secret: SecretKey,
    policy: RotationPolicy,
    visible_bundles: &[KeyBundle],
) -> (Identity, RotationReceipt, Vec<ReWrappedDek>) {
    let member_id = old.member_id.clone();

    // Distinct (rs, gen) generations the member currently has a slot in — these become
    // un-re-fetchable once the old key is gone (they are wrapped to the retiring key).
    let mut generations: Vec<(String, u64)> = visible_bundles
        .iter()
        .filter(|b| b.members.contains_key(&member_id))
        .map(|b| (b.rs.clone(), b.gen))
        .collect();
    generations.sort();
    generations.dedup();
    let now_unreadable_generations = generations.len() as u64;

    let new_public: PublicKey = new_secret.public_key();
    let mut rewrapped: Vec<ReWrappedDek> = Vec::new();
    if policy == RotationPolicy::KeepAccess {
        // Member-side, local re-wrap: unwrap each still-live slot with the retiring key and
        // re-wrap it to the new public key. Reduces forward secrecy for these generations
        // (see the module doc). A malformed slot is skipped, not fatal.
        for bundle in visible_bundles {
            let Some(w) = bundle.members.get(&member_id) else {
                continue;
            };
            let Ok(mut dek) = unwrap_dek(w, old.secret()) else {
                continue;
            };
            if let Ok(wrapped) = wrap_dek(&dek, &new_public) {
                rewrapped.push(ReWrappedDek {
                    rs: bundle.rs.clone(),
                    gen: bundle.gen,
                    wrapped,
                });
            }
            dek.zeroize();
        }
    }

    // rewrapped_generations counts distinct (rs, gen) actually re-wrapped (a malformed slot
    // that failed to unwrap is excluded).
    let mut rw_generations: Vec<(&str, u64)> =
        rewrapped.iter().map(|r| (r.rs.as_str(), r.gen)).collect();
    rw_generations.sort();
    rw_generations.dedup();
    let rewrapped_generations = rw_generations.len() as u64;

    let receipt = RotationReceipt {
        member_id,
        policy: policy.as_str(),
        now_unreadable_generations,
        rewrapped_generations,
        timestamp_rfc3339: rfc3339_utc(SystemTime::now()),
    };

    // Drop the old identity: `p256::SecretKey` implements `ZeroizeOnDrop`
    // (elliptic-curve 0.13, `impl ZeroizeOnDrop for SecretKey`), so this wipes the retired
    // private scalar. No explicit zeroize is needed — verified against the crate.
    drop(old);

    let new_identity = Identity::new(receipt.member_id.clone(), new_secret);
    (new_identity, receipt, rewrapped)
}

/// Format a `SystemTime` as RFC3339 UTC (`YYYY-MM-DDTHH:MM:SSZ`), no dependency. Uses the
/// civil-from-days algorithm (Howard Hinnant, public domain). A pre-epoch time (should not
/// occur for a rotation) formats as the epoch.
fn rfc3339_utc(t: SystemTime) -> String {
    let secs = t
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let days = (secs / 86_400) as i64;
    let secs_of_day = secs % 86_400;
    let (hour, min, sec) = (
        secs_of_day / 3600,
        (secs_of_day % 3600) / 60,
        secs_of_day % 60,
    );

    // days since 1970-01-01 -> civil (y, m, d)
    let z = days + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = z - era * 146_097; // [0, 146096]
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146_096) / 365; // [0, 399]
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100); // [0, 365]
    let mp = (5 * doy + 2) / 153; // [0, 11]
    let d = doy - (153 * mp + 2) / 5 + 1; // [1, 31]
    let m = if mp < 10 { mp + 3 } else { mp - 9 }; // [1, 12]
    let year = if m <= 2 { y + 1 } else { y };

    format!(
        "{year:04}-{m:02}-{d:02}T{hour:02}:{min:02}:{sec:02}Z",
        year = year,
        m = m,
        d = d,
        hour = hour,
        min = min,
        sec = sec,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{new_bundle, new_dek, sign_bundle};
    use p256::ecdsa::SigningKey;
    use std::collections::BTreeMap;

    // Raw 32-byte scalar of a secret key (KeyBundle is not Clone, so tests borrow bundles).
    fn scalar_of(s: &SecretKey) -> [u8; 32] {
        let mut a = [0u8; 32];
        a.copy_from_slice(&s.to_bytes());
        a
    }

    fn bundle_for(rs: &str, gen: u64, member_id: &str, member_pub: p256::PublicKey) -> KeyBundle {
        let mut recipients = BTreeMap::new();
        recipients.insert(member_id.to_string(), member_pub);
        let dek = new_dek();
        let mut bundle = new_bundle(rs, gen, &dek, &recipients).unwrap();
        sign_bundle(&mut bundle, "key-service", &SigningKey::generate()).unwrap();
        bundle
    }

    #[test]
    fn destroy_loses_re_fetch() {
        // A member with a slot in one bundle rotates with Destroy. Afterwards the OLD key
        // can no longer unwrap that slot (prove by reconstructing the old identity and
        // attempting the unwrap succeeds first, then a fresh new key cannot open it).
        let old_secret = SecretKey::generate();
        let old_scalar = scalar_of(&old_secret);
        let member_id = "coalition-air/con";
        let bundle = bundle_for("coalition-air", 1, member_id, old_secret.public_key());

        // Baseline: the old key opens its slot.
        assert!(unwrap_dek(&bundle.members[member_id], &old_secret).is_ok());

        let old = Identity::from_scalar(member_id, &old_scalar).unwrap();
        let new_secret = SecretKey::generate();
        let (_new_id, receipt, rewrapped) = rotate(
            old,
            new_secret.clone(),
            RotationPolicy::Destroy,
            std::slice::from_ref(&bundle),
        );

        assert_eq!(receipt.policy, "destroy");
        assert_eq!(receipt.now_unreadable_generations, 1);
        assert_eq!(receipt.rewrapped_generations, 0);
        assert!(rewrapped.is_empty(), "destroy re-wraps nothing");

        // The NEW key has no slot in the old bundle — it cannot re-fetch the generation.
        assert!(
            unwrap_dek(&bundle.members[member_id], &new_secret).is_err(),
            "the new key cannot open a slot wrapped to the old key"
        );
    }

    #[test]
    fn keep_access_re_wraps_and_the_new_key_opens() {
        let old_secret = SecretKey::generate();
        let old_scalar = scalar_of(&old_secret);
        let member_id = "coalition-air/con";
        let b1 = bundle_for("coalition-air", 1, member_id, old_secret.public_key());
        let b2 = bundle_for("coalition-air", 2, member_id, old_secret.public_key());

        let old = Identity::from_scalar(member_id, &old_scalar).unwrap();
        let new_secret = SecretKey::generate();
        let (_new_id, receipt, rewrapped) = rotate(
            old,
            new_secret.clone(),
            RotationPolicy::KeepAccess,
            &[b1, b2],
        );

        assert_eq!(receipt.policy, "keep-access");
        assert_eq!(receipt.now_unreadable_generations, 2);
        assert_eq!(receipt.rewrapped_generations, 2);
        assert_eq!(rewrapped.len(), 2);

        // Each re-wrapped entry opens under the NEW key.
        for r in &rewrapped {
            let dek =
                unwrap_dek(&r.wrapped, &new_secret).expect("re-wrapped DEK opens under new key");
            assert_eq!(dek.len(), 32);
        }
    }

    #[test]
    fn receipt_counts_only_generations_with_a_slot() {
        let old_secret = SecretKey::generate();
        let old_scalar = scalar_of(&old_secret);
        let member_id = "coalition-air/con";
        // One bundle the member is in, one it is NOT in (a different member).
        let mine = bundle_for("coalition-air", 1, member_id, old_secret.public_key());
        let other_secret = SecretKey::generate();
        let theirs = bundle_for(
            "coalition-air",
            2,
            "someone/else",
            other_secret.public_key(),
        );

        let old = Identity::from_scalar(member_id, &old_scalar).unwrap();
        let new_secret = SecretKey::generate();
        let (_new_id, receipt, _rw) =
            rotate(old, new_secret, RotationPolicy::Destroy, &[mine, theirs]);
        assert_eq!(
            receipt.now_unreadable_generations, 1,
            "only the generation the member had a slot in counts"
        );
    }

    #[test]
    fn receipt_serde_shape_is_stable() {
        let receipt = RotationReceipt {
            member_id: "coalition-air/con".into(),
            policy: "destroy",
            now_unreadable_generations: 3,
            rewrapped_generations: 0,
            timestamp_rfc3339: "2026-08-01T12:34:56Z".into(),
        };
        let v: serde_json::Value = serde_json::to_value(&receipt).unwrap();
        assert_eq!(v["member_id"], "coalition-air/con");
        assert_eq!(v["policy"], "destroy");
        assert_eq!(v["now_unreadable_generations"], 3);
        assert_eq!(v["rewrapped_generations"], 0);
        assert_eq!(v["timestamp_rfc3339"], "2026-08-01T12:34:56Z");
    }

    #[test]
    fn rfc3339_formats_known_instants() {
        assert_eq!(rfc3339_utc(UNIX_EPOCH), "1970-01-01T00:00:00Z");
        // 1_700_000_000 = 2023-11-14T22:13:20Z (a known reference instant).
        assert_eq!(
            rfc3339_utc(UNIX_EPOCH + std::time::Duration::from_secs(1_700_000_000)),
            "2023-11-14T22:13:20Z"
        );
    }
}
