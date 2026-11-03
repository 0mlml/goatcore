//! Producer + consumer data-path (Piece A) — the visible capability.
//!
//! E1/M2.1 distributes the keys; this is what a data owner's stream and a grantee's
//! reader actually *do* with them. It is the SDK-surface ergonomic over the low-level
//! wire functions in [`crate`] (`seal_envelope`/`open_envelope`/`unwrap_dek`): those
//! stay byte-identical and are the wire layer; the types here add the misuse-resistant
//! shape the frozen API freezes (`docs/design/grant-crypto-sdk.md`) — a typed
//! [`Context`], held-DEK zeroization, and the enforced message-count cap. Two halves:
//!
//! **Producer (write side).** The producer is a MEMBER of the release-set — the
//! DEK-handoff decision (design §3): rather than a new secret channel or a handoff RPC
//! from grant-keyd, the producer's own SVID goes into the audience, so it fetches
//! `keys/<rs>/**`, unwraps ITS OWN slot to obtain the DEK ([`acquire_stream_key`]), and
//! seals each message under it ([`StreamKey::seal`]). This reuses the entire existing
//! bundle/wrap path — no change to grant-keyd beyond ensuring the producer id is in the
//! audience.
//!
//! **Consumer (read side).** A member keeps the bundles it can open ([`KeyStore::admit`],
//! fetched from the `keys/**` storage on join, then live) and, per data message, opens its
//! slot's DEK ([`KeyStore::open`]). A non-member has no slot → the read is denied by the
//! crypto, not the router (the E0 outsider-denied result, now as running libraries).
//!
//! **Carrier — in-band (the self-describing `Envelope` as the message body).** A data
//! message is `serde_json(Envelope)`; the `Envelope` already carries `suite`/`rs`/`gen`/
//! nonce/ciphertext, so it needs no companion tag. This is the carrier the `KeyBundle`
//! itself uses and it rides the stable Zenoh PUT the fleet already builds. (Design §7
//! decision 1 also allows a *native attachment* carrier — raw ciphertext body + a
//! `{rs,gen,nonce}` attachment — proven in the Python E0 proof; in Rust the write-side
//! attachment API is behind zenoh's gated `internal` feature, so the native-attachment
//! carrier is deferred rather than forcing that feature onto the fleet data path.)
//!
//! This module is pure (no Zenoh): the transport shell that fetches bundles and
//! subscribes streams lives in `grant-keyd`'s `producer`/`consumer` examples, so the
//! crypto decisions are unit-tested in isolation the same way `service` is.

#[cfg(test)]
use p256::elliptic_curve::Generate;
use std::collections::{BTreeMap, HashMap};
use std::sync::{Mutex, MutexGuard, OnceLock};

use p256::SecretKey;
use zeroize::Zeroize;

use crate::{open_envelope, seal_envelope, unwrap_dek, Envelope, Error, KeyBundle};

/// Process-global cumulative seal counts, keyed by `(release-set, generation)`. This is
/// the anti-reset guarantee: because the count lives here — not in the [`StreamKey`] —
/// re-acquiring the same generation cannot silently reset the nonce budget on the same
/// DEK. [`acquire_stream_key`] seeds a new key's `seals` from this map, and every
/// successful [`StreamKey::seal`] writes through to it.
///
/// Honest ceiling: the guarantee is **per-process**. Across a restart (or a second
/// process) this map is empty again; the systemic protection there is the key service
/// publishing a new generation (rotation), which re-keys the DEK so a fresh 96-bit-nonce
/// budget is legitimately safe. Concurrent producers on the exact same `(rs, gen)` in one
/// process may each observe a slightly stale local count between the check and the
/// write-through, so the effective cap is approximate under that (unusual) concurrency —
/// never loose enough to matter against the `2⁸` margin below the birthday bound.
fn seal_counts() -> MutexGuard<'static, HashMap<(String, u64), u64>> {
    static COUNTS: OnceLock<Mutex<HashMap<(String, u64), u64>>> = OnceLock::new();
    COUNTS
        .get_or_init(|| Mutex::new(HashMap::new()))
        // Recover a poisoned lock rather than panic — a seal that unwound mid-write is
        // rare, and the count is monotone, so the recovered map is still safe to use
        // (mirrors the capi registry's poison-recovery).
        .lock()
        .unwrap_or_else(|e| e.into_inner())
}

/// The required typed AAD binding on seal and open: the data topic / key expression a
/// message is sealed to and can only be opened on. Mandatory — there is no context-free
/// overload — so a caller cannot accidentally seal without binding the topic. The
/// low-level [`crate::seal_envelope`]/[`crate::open_envelope`] free functions remain the
/// context-as-`&[u8]` wire layer; `Context` is the ergonomic that makes the binding
/// unforgettable at the SDK surface.
pub struct Context<'a>(pub &'a str);

/// Cumulative message-count cap per stream key, below the AES-256-GCM random-nonce
/// birthday bound. v1 seals with a fresh random 96-bit nonce under a shared per-generation
/// DEK; the risk is a nonce *collision*, which after `q` seals has probability
/// ≈ `q² / 2⁹⁷`. NIST SP 800-38D-style guidance keeps `q ≤ 2³²` (collision ≈ 2⁻³³). We
/// default an order of magnitude tighter — `2²⁴ = 16_777_216` seals, a **256× (2⁸)**
/// margin below `2³²`, giving collision probability ≈ `(2²⁴)² / 2⁹⁷ = 2⁻⁴⁹` at the cap.
/// At the cap [`StreamKey::seal`] refuses with [`Error::RotationRequired`] (a local
/// refusal — nothing on the wire) so the producer rotates rather than risking nonce reuse.
pub const DEFAULT_SEAL_CAP: u64 = 1 << 24;

/// Serialize a sealed [`Envelope`] for the in-band carrier (the Zenoh message body).
pub fn encode_message(env: &Envelope) -> Result<Vec<u8>, Error> {
    serde_json::to_vec(env).map_err(|_| Error::Malformed)
}

/// Parse a received data message body back into an [`Envelope`].
pub fn decode_message(body: &[u8]) -> Result<Envelope, Error> {
    serde_json::from_slice(body).map_err(|_| Error::Malformed)
}

// ---- producer (write side) ----

/// The producer's current stream key: the DEK for generation `gen` of release-set `rs`,
/// obtained by unwrapping the producer's own bundle slot. Never carries anyone else's key
/// material; drop it and the producer can no longer seal until it re-acquires. The held
/// DEK is zeroized on drop (best-effort, per the crate ceiling).
pub struct StreamKey {
    rs: String,
    gen: u64,
    dek: [u8; 32],
    /// Cumulative messages sealed under this `(rs, gen)` in this process — seeded at
    /// [`acquire_stream_key`] from the process-global [`seal_counts`] map and advanced on
    /// every successful [`StreamKey::seal`], so re-acquiring the same generation cannot
    /// reset the budget. Compared against `cap` to enforce the nonce-safety rotation cap.
    seals: u64,
    /// The cap this key refuses to seal past (defaults to [`DEFAULT_SEAL_CAP`]; tightenable).
    cap: u64,
}

impl Drop for StreamKey {
    fn drop(&mut self) {
        self.dek.zeroize();
    }
}

impl StreamKey {
    /// The release-set this key seals for.
    pub fn release_set(&self) -> &str {
        &self.rs
    }

    /// The generation this key seals under (stamped into every message).
    pub fn generation(&self) -> u64 {
        self.gen
    }

    /// Tighten the seal cap (builder form). **Monotone: `cap = min(current_cap, requested)`**,
    /// so a caller may only lower the cap, never raise it — and since it starts at
    /// [`DEFAULT_SEAL_CAP`], never above the default. A request at or above the current cap
    /// is a no-op.
    pub fn with_cap(mut self, cap: u64) -> Self {
        self.cap = self.cap.min(cap);
        self
    }

    /// Tighten the seal cap in place. **Monotone: `cap = min(current_cap, requested)`** —
    /// tighten-only, never loosen (a request at or above the current cap is a no-op). The
    /// mutable sibling of [`with_cap`](Self::with_cap) for a handle-held key (the C ABI's
    /// `gc_streamkey_set_cap`).
    pub fn set_cap(&mut self, cap: u64) {
        self.cap = self.cap.min(cap);
    }

    /// Messages that may still be sealed under this key before rotation is required — the
    /// caller surfaces this as a structured fact well before refusal.
    pub fn remaining(&self) -> u64 {
        self.cap.saturating_sub(self.seals)
    }

    /// Seal `plaintext` for publication on the topic named by `ctx`, returning the
    /// [`Envelope`] to publish (as the in-band body via [`encode_message`]). `ctx` is bound
    /// as AEAD additional data, so a sealed message can only be opened on the exact topic it
    /// was sealed for (a cross-topic replay fails cleanly). Refuses with
    /// [`Error::RotationRequired`] once [`remaining`](Self::remaining) reaches zero — the
    /// enforced nonce-safety cap (see [`DEFAULT_SEAL_CAP`]).
    pub fn seal(&mut self, ctx: Context, plaintext: &[u8]) -> Result<Envelope, Error> {
        if self.seals >= self.cap {
            return Err(Error::RotationRequired);
        }
        let env = seal_envelope(&self.dek, &self.rs, self.gen, plaintext, ctx.0.as_bytes())?;
        // Write through to the process-global cumulative counter so a later re-acquire of
        // this same `(rs, gen)` inherits the count and cannot reset the budget. The global
        // value is the source of truth; the local `seals` mirrors it after each seal.
        let mut counts = seal_counts();
        let n = counts.entry((self.rs.clone(), self.gen)).or_insert(0);
        *n += 1;
        self.seals = *n;
        Ok(env)
    }

    /// Seal only if this key's generation is still the newest one the producer can see —
    /// the sealing entry point a long-lived producer should use.
    ///
    /// [`seal`](Self::seal) alone cannot notice that the owner changed the audience: it
    /// holds a DEK and keeps using it, so a member removed from the release set keeps
    /// reading every message published afterwards. This checks first and refuses
    /// with [`Error::Superseded`] when a newer generation exists, which is the caller's
    /// signal to re-acquire — the same shape as the [`Error::RotationRequired`] it already
    /// has to handle at the nonce cap.
    ///
    /// `bundles` is whatever the producer currently sees on `keys/<rs>/**`; it is already
    /// subscribed there to have acquired a key at all, so this adds no dependency and no
    /// online requirement. Bundles for other release sets are ignored.
    pub fn seal_current(
        &mut self,
        ctx: Context,
        plaintext: &[u8],
        bundles: &[KeyBundle],
    ) -> Result<Envelope, Error> {
        self.assert_current(bundles)?;
        self.seal(ctx, plaintext)
    }

    /// The newest generation of this key's release set among `bundles`, or this key's own
    /// generation if none is newer. Read-only; does not consider slot membership, because
    /// a producer dropped from its own audience must still learn it was superseded.
    pub fn newest_visible(&self, bundles: &[KeyBundle]) -> u64 {
        bundles
            .iter()
            .filter(|b| b.rs == self.rs)
            .map(|b| b.gen)
            .max()
            .unwrap_or(self.gen)
            .max(self.gen)
    }

    /// `Ok(())` while this key is the newest generation visible, [`Error::Superseded`]
    /// once it is not.
    pub fn assert_current(&self, bundles: &[KeyBundle]) -> Result<(), Error> {
        if self.newest_visible(bundles) > self.gen {
            return Err(Error::Superseded);
        }
        Ok(())
    }
}

/// What a publisher expects to be true of the audience after changing access — the input
/// to [`confirm_audience`].
///
/// Stated as member ids rather than a count, because a count cannot distinguish "the
/// member I removed is gone" from "somebody else left in the same window".
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct AudienceExpectation {
    /// Member ids that must NOT hold a slot in the newest generation.
    pub excluded: Vec<String>,
    /// Member ids that must hold one.
    pub included: Vec<String>,
}

/// What [`confirm_audience`] observed, for the caller to log or attach to an audit record.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AudienceConfirmation {
    /// The generation the check was satisfied against.
    pub generation: u64,
    /// Member ids holding a slot in it.
    pub members: Vec<String>,
}

/// Confirm that an access change actually took effect on the fabric, by reading the
/// published bundle back and checking who holds a slot in it.
///
/// **Why this exists rather than trusting the publish.** A revoke succeeds in the portal,
/// the key service resolves the smaller audience, mints a new generation and writes it —
/// and every one of those steps can return `Ok` while the write is silently dropped at the
/// router, which is this fabric's standing failure mode. Absence of an error on
/// the writing side is not evidence of anything; only reading the far side is. So a
/// publisher that has changed access is expected to call this and treat
/// [`Error::AudienceNotApplied`] as "the change has not landed", not as a transient.
///
/// `bundles` is the set read back from `keys/<rs>/**` after the change. The newest
/// generation is the one checked, since that is what subsequent messages seal under.
/// Fail-closed: [`Error::NoSlot`] when no bundle for `rs` is visible at all, because
/// seeing nothing is not the same as seeing the change applied.
pub fn confirm_audience(
    rs: &str,
    bundles: &[KeyBundle],
    expect: &AudienceExpectation,
) -> Result<AudienceConfirmation, Error> {
    let newest = bundles
        .iter()
        .filter(|b| b.rs == rs)
        .max_by_key(|b| b.gen)
        .ok_or(Error::NoSlot)?;

    for id in &expect.excluded {
        if newest.members.contains_key(id) {
            return Err(Error::AudienceNotApplied);
        }
    }
    for id in &expect.included {
        if !newest.members.contains_key(id) {
            return Err(Error::AudienceNotApplied);
        }
    }

    let mut members: Vec<String> = newest.members.keys().cloned().collect();
    members.sort();
    Ok(AudienceConfirmation {
        generation: newest.gen,
        members,
    })
}

/// Acquire the producer's stream key from the bundles it can see: pick the **highest**
/// generation of `rs` in which `member_id` has a slot and unwrap it. Fail-closed with
/// [`Error::NoSlot`] if no visible bundle carries a slot for this producer — it is not yet
/// in the audience, or no bundle has been published — so a producer never seals under a
/// generation it cannot itself open. The returned key carries the default seal cap
/// ([`DEFAULT_SEAL_CAP`]); tighten it with [`StreamKey::with_cap`].
///
/// The new key's seal count is **seeded from the process-global cumulative counter** for
/// this `(rs, gen)` (see [`seal_counts`]): re-acquiring a generation already sealed under
/// in this process inherits its count, so the nonce budget cannot be reset by re-acquiring.
pub fn acquire_stream_key(
    rs: &str,
    bundles: &[KeyBundle],
    member_id: &str,
    member_secret: &SecretKey,
) -> Result<StreamKey, Error> {
    let chosen = bundles
        .iter()
        .filter(|b| b.rs == rs && b.members.contains_key(member_id))
        .max_by_key(|b| b.gen)
        .ok_or(Error::NoSlot)?;
    let dek = unwrap_dek(&chosen.members[member_id], member_secret)?;
    let seals = seal_counts()
        .get(&(rs.to_string(), chosen.gen))
        .copied()
        .unwrap_or(0);
    Ok(StreamKey {
        rs: rs.to_string(),
        gen: chosen.gen,
        dek,
        seals,
        cap: DEFAULT_SEAL_CAP,
    })
}

// ---- consumer (read side) ----

/// A member's held keys: the DEK it unwrapped from each bundle it was admitted, indexed by
/// `(release-set, generation)`. A data message is openable iff its generation's key is
/// held — so a revoked member simply stops admitting new generations and cannot open them
/// (revocation = rotation, design §4), while still opening the older generations it holds.
/// All held DEKs are zeroized on drop (best-effort, per the crate ceiling).
#[derive(Default)]
pub struct KeyStore {
    deks: BTreeMap<(String, u64), [u8; 32]>,
}

impl Drop for KeyStore {
    fn drop(&mut self) {
        for dek in self.deks.values_mut() {
            dek.zeroize();
        }
    }
}

impl KeyStore {
    pub fn new() -> Self {
        Self::default()
    }

    /// Admit a bundle for this member: unwrap its slot and retain the DEK for
    /// `(bundle.rs, bundle.gen)`. Returns `Ok(true)` if the member had a slot (key
    /// retained), `Ok(false)` if it had none (an outsider for this generation — nothing
    /// retained, not an error), and `Err` only on a malformed slot. The caller is expected
    /// to have verified the bundle's signer first (`keyd::svid::verify_bundle_self`); this
    /// method assumes an already-trusted bundle.
    pub fn admit(
        &mut self,
        bundle: &KeyBundle,
        member_id: &str,
        member_secret: &SecretKey,
    ) -> Result<bool, Error> {
        match bundle.members.get(member_id) {
            None => Ok(false),
            Some(w) => {
                let dek = unwrap_dek(w, member_secret)?;
                self.deks.insert((bundle.rs.clone(), bundle.gen), dek);
                Ok(true)
            }
        }
    }

    /// Open a data message received on the topic named by `ctx`, decoded from its in-band
    /// body into `env`. Fail-closed with [`Error::NoSlot`] if no key is held for the
    /// message's `(rs, gen)` — the read cut, enforced by the crypto. `ctx` must name the
    /// topic the message arrived on (it is AEAD-bound).
    pub fn open(&self, ctx: Context, env: &Envelope) -> Result<Vec<u8>, Error> {
        let dek = self
            .deks
            .get(&(env.rs.clone(), env.gen))
            .ok_or(Error::NoSlot)?;
        open_envelope(dek, env, ctx.0.as_bytes())
    }

    /// Whether a key is held for `(rs, gen)` (diagnostics / catch-up decisions).
    pub fn holds(&self, rs: &str, gen: u64) -> bool {
        self.deks.contains_key(&(rs.to_string(), gen))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{new_bundle, new_dek, sign_bundle};
    use p256::ecdsa::SigningKey;

    // A member id + its retained secret (so the test can both wrap to it and open).
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

    // The key-service builds + signs a bundle for `audience` under a fresh DEK. Returns the
    // signed bundle (the producer/consumer only ever see this, never the raw DEK).
    fn published_bundle(rs: &str, gen: u64, audience: &[&Party]) -> KeyBundle {
        let recipients: BTreeMap<String, p256::PublicKey> = audience
            .iter()
            .map(|p| (p.id.clone(), p.secret.public_key()))
            .collect();
        let dek = new_dek();
        let mut bundle = new_bundle(rs, gen, &dek, &recipients).unwrap();
        sign_bundle(&mut bundle, "key-service", &SigningKey::generate()).unwrap();
        bundle
    }

    // ---- F-498: the producer must notice the audience changed ----

    #[test]
    fn a_superseded_generation_refuses_to_seal() {
        // The defect this closes: grant-keyd mints a generation that drops a member, and
        // the producer keeps sealing under the old one, which the dropped member can open.
        let rs = "f498-superseded";
        let producer = party("prod");
        let leaving = party("leaving");
        let gen1 = published_bundle(rs, 1, &[&producer, &leaving]);

        let mut sk = acquire_stream_key(
            rs,
            std::slice::from_ref(&gen1),
            &producer.id,
            &producer.secret,
        )
        .unwrap();
        // While gen 1 is the newest, sealing is fine either way.
        assert!(sk.assert_current(std::slice::from_ref(&gen1)).is_ok());
        assert!(sk
            .seal_current(Context("d/1"), b"before", std::slice::from_ref(&gen1))
            .is_ok());

        // The owner revokes: a new generation appears WITHOUT the leaving member.
        let gen2 = published_bundle(rs, 2, &[&producer]);
        let visible = [gen1, gen2];

        assert_eq!(sk.assert_current(&visible), Err(Error::Superseded));
        assert_eq!(
            sk.seal_current(Context("d/1"), b"after", &visible)
                .unwrap_err(),
            Error::Superseded,
            "sealing under a superseded generation is what let a revoked member keep reading"
        );
        // The bare seal still works — that is exactly why seal_current exists.
        assert!(sk.seal(Context("d/1"), b"after").is_ok());
    }

    #[test]
    fn supersession_is_by_generation_not_by_membership() {
        // A producer dropped from its OWN audience must still learn it was superseded,
        // otherwise the one party that can stop publishing never finds out.
        let rs = "f498-dropped-producer";
        let producer = party("prod");
        let other = party("other");
        let gen1 = published_bundle(rs, 1, &[&producer]);
        let mut sk = acquire_stream_key(
            rs,
            std::slice::from_ref(&gen1),
            &producer.id,
            &producer.secret,
        )
        .unwrap();

        // gen 2 excludes the producer itself.
        let gen2 = published_bundle(rs, 2, &[&other]);
        let visible = [gen1, gen2];
        assert_eq!(
            sk.seal_current(Context("d/1"), b"x", &visible).unwrap_err(),
            Error::Superseded
        );
    }

    #[test]
    fn another_release_sets_generations_do_not_supersede_this_one() {
        let rs = "f498-scoped";
        let producer = party("prod");
        let mine = published_bundle(rs, 1, &[&producer]);
        let theirs = published_bundle("someone-else", 9, &[&producer]);
        let mut sk = acquire_stream_key(
            rs,
            std::slice::from_ref(&mine),
            &producer.id,
            &producer.secret,
        )
        .unwrap();
        let visible = [mine, theirs];
        assert!(sk.seal_current(Context("d/1"), b"x", &visible).is_ok());
    }

    // ---- F-498: an access change must be CONFIRMED, not assumed ----

    #[test]
    fn confirm_audience_reads_the_change_back() {
        // The operator's rule: a publisher that changes access validates that the change
        // took effect. A publish returning Ok proves only that a write was attempted.
        let rs = "f498-confirm";
        let producer = party("prod");
        let staying = party("staying");
        let leaving = party("leaving");

        let gen1 = published_bundle(rs, 1, &[&producer, &staying, &leaving]);
        let expect = AudienceExpectation {
            excluded: vec![leaving.id.clone()],
            included: vec![producer.id.clone(), staying.id.clone()],
        };

        // The revoke has NOT taken effect: the newest bundle still carries the slot.
        assert_eq!(
            confirm_audience(rs, std::slice::from_ref(&gen1), &expect),
            Err(Error::AudienceNotApplied)
        );

        // It lands. Now the check passes, and reports what it saw.
        let gen2 = published_bundle(rs, 2, &[&producer, &staying]);
        let visible = [gen1, gen2];
        let got = confirm_audience(rs, &visible, &expect).expect("the change is applied");
        assert_eq!(got.generation, 2);
        assert_eq!(got.members, vec![producer.id.clone(), staying.id.clone()]);
    }

    #[test]
    fn confirm_audience_catches_a_member_that_should_have_been_added() {
        let rs = "f498-confirm-add";
        let producer = party("prod");
        let joining = party("joining");
        let gen1 = published_bundle(rs, 1, &[&producer]);
        let expect = AudienceExpectation {
            excluded: vec![],
            included: vec![joining.id.clone()],
        };
        assert_eq!(
            confirm_audience(rs, std::slice::from_ref(&gen1), &expect),
            Err(Error::AudienceNotApplied)
        );
    }

    #[test]
    fn confirm_audience_fails_closed_when_it_can_see_nothing() {
        // Seeing no bundle is not the same as seeing the change applied.
        let expect = AudienceExpectation {
            excluded: vec!["anyone".into()],
            included: vec![],
        };
        assert_eq!(
            confirm_audience("f498-absent", &[], &expect),
            Err(Error::NoSlot)
        );
    }

    #[test]
    fn producer_as_member_seals_and_a_grantee_member_opens() {
        // Each sealing test uses a distinct release-set: the seal counter is process-global
        // per `(rs, gen)`, so a shared `rs` would let one test's seals leak into another's.
        let rs = "seal-producer-member";
        let keyexpr = "data/coalition-air/cop";
        let producer = party("coalition-air/prod");
        let grantee = party("coalition-air/con");
        // The audience includes BOTH the producer (so it can pull its DEK) and the grantee.
        let bundle = published_bundle(rs, 1, &[&producer, &grantee]);

        // Producer acquires its key from the bundle it fetched and seals a message; the
        // message is carried in-band exactly as it would ride the wire.
        let mut sk = acquire_stream_key(
            rs,
            std::slice::from_ref(&bundle),
            &producer.id,
            &producer.secret,
        )
        .expect("producer is in the audience");
        assert_eq!(sk.generation(), 1);
        let plaintext = b"brigade COP snapshot, releasable to REL-EXAMPLE only";
        let wire = encode_message(&sk.seal(Context(keyexpr), plaintext).expect("seal")).unwrap();

        // Grantee admits the same bundle, decodes the received body, and opens it.
        let mut store = KeyStore::new();
        assert!(store
            .admit(&bundle, &grantee.id, &grantee.secret)
            .expect("admit"));
        let env = decode_message(&wire).unwrap();
        assert_eq!(env.gen, 1);
        assert_eq!(env.rs, rs);
        let opened = store.open(Context(keyexpr), &env).expect("grantee opens");
        assert_eq!(opened, plaintext);
    }

    #[test]
    fn an_outsider_cannot_open_the_stream() {
        let rs = "seal-outsider";
        let keyexpr = "data/coalition-air/cop";
        let producer = party("coalition-air/prod");
        let grantee = party("coalition-air/con");
        let outsider = party("outsider");
        let bundle = published_bundle(rs, 1, &[&producer, &grantee]);

        let mut sk = acquire_stream_key(
            rs,
            std::slice::from_ref(&bundle),
            &producer.id,
            &producer.secret,
        )
        .unwrap();
        let env = sk.seal(Context(keyexpr), b"secret").unwrap();

        // The outsider has no slot: admit retains nothing, and open is denied.
        let mut store = KeyStore::new();
        assert!(!store
            .admit(&bundle, &outsider.id, &outsider.secret)
            .expect("admit returns false, not error"));
        assert_eq!(store.open(Context(keyexpr), &env), Err(Error::NoSlot));
    }

    #[test]
    fn a_producer_not_in_the_audience_gets_no_key() {
        let rs = "coalition-air";
        let grantee = party("coalition-air/con");
        let would_be_producer = party("coalition-air/prod");
        // Bundle audience is the grantee only — the producer has no slot.
        let bundle = published_bundle(rs, 1, &[&grantee]);
        assert_eq!(
            acquire_stream_key(
                rs,
                std::slice::from_ref(&bundle),
                &would_be_producer.id,
                &would_be_producer.secret
            )
            .map(|_| ()),
            Err(Error::NoSlot)
        );
    }

    #[test]
    fn acquire_picks_the_highest_generation() {
        let rs = "coalition-air";
        let producer = party("coalition-air/prod");
        let g1 = published_bundle(rs, 1, &[&producer]);
        let g3 = published_bundle(rs, 3, &[&producer]);
        let g2 = published_bundle(rs, 2, &[&producer]);
        let sk = acquire_stream_key(rs, &[g1, g3, g2], &producer.id, &producer.secret).unwrap();
        assert_eq!(sk.generation(), 3, "the newest generation wins");
    }

    #[test]
    fn a_message_cannot_be_opened_on_a_different_topic() {
        // AEAD binds the keyexpr: a message sealed for one topic must not open on another
        // even by an authorized member (guards a mis-routed / replayed publication).
        let rs = "seal-topic-bind";
        let producer = party("coalition-air/prod");
        let grantee = party("coalition-air/con");
        let bundle = published_bundle(rs, 1, &[&producer, &grantee]);
        let mut sk = acquire_stream_key(
            rs,
            std::slice::from_ref(&bundle),
            &producer.id,
            &producer.secret,
        )
        .unwrap();
        let env = sk.seal(Context("data/coalition-air/cop"), b"x").unwrap();

        let mut store = KeyStore::new();
        store.admit(&bundle, &grantee.id, &grantee.secret).unwrap();
        assert_eq!(
            store.open(Context("data/coalition-air/other"), &env),
            Err(Error::Decrypt),
            "wrong topic must fail the AEAD"
        );
    }

    #[test]
    fn a_revoked_member_keeps_old_generations_but_not_new_ones() {
        // gen 1 audience = {producer, member}; gen 2 revokes member. The member holds gen 1
        // (opens gen-1 traffic forever) but never admits gen 2 (no slot) → cannot open it.
        let rs = "seal-revoked";
        let keyexpr = "data/coalition-air/cop";
        let producer = party("coalition-air/prod");
        let member = party("coalition-air/con");
        let g1 = published_bundle(rs, 1, &[&producer, &member]);
        let g2 = published_bundle(rs, 2, &[&producer]); // member revoked

        let mut store = KeyStore::new();
        assert!(store.admit(&g1, &member.id, &member.secret).unwrap());
        assert!(!store.admit(&g2, &member.id, &member.secret).unwrap()); // no slot in gen 2

        // A gen-1 message still opens.
        let mut sk1 = acquire_stream_key(
            rs,
            std::slice::from_ref(&g1),
            &producer.id,
            &producer.secret,
        )
        .unwrap();
        let e1 = sk1.seal(Context(keyexpr), b"old").unwrap();
        assert_eq!(store.open(Context(keyexpr), &e1).unwrap(), b"old");

        // A gen-2 message does not — the member holds no gen-2 key.
        let mut sk2 = acquire_stream_key(
            rs,
            std::slice::from_ref(&g2),
            &producer.id,
            &producer.secret,
        )
        .unwrap();
        let e2 = sk2.seal(Context(keyexpr), b"new").unwrap();
        assert_eq!(store.open(Context(keyexpr), &e2), Err(Error::NoSlot));
    }

    #[test]
    fn seal_cap_is_enforced_and_remaining_counts_down() {
        // A tiny cap proves the enforced nonce-safety rotation cap: exactly `cap` seals
        // succeed, `remaining` counts down to zero, and the next seal is RotationRequired.
        let rs = "seal-cap-enforced";
        let keyexpr = "data/coalition-air/cop";
        let producer = party("coalition-air/prod");
        let bundle = published_bundle(rs, 1, &[&producer]);
        let mut sk = acquire_stream_key(
            rs,
            std::slice::from_ref(&bundle),
            &producer.id,
            &producer.secret,
        )
        .unwrap()
        .with_cap(3);

        assert_eq!(sk.remaining(), 3);
        for i in 0..3 {
            assert_eq!(sk.remaining(), 3 - i);
            sk.seal(Context(keyexpr), b"m").expect("under the cap");
        }
        assert_eq!(sk.remaining(), 0);
        assert_eq!(
            sk.seal(Context(keyexpr), b"one too many").unwrap_err(),
            Error::RotationRequired,
            "at the cap, seal refuses until rotation"
        );
    }

    #[test]
    fn with_cap_clamps_to_the_default_and_cannot_loosen() {
        let rs = "seal-cap-clamp";
        let producer = party("coalition-air/prod");
        let bundle = published_bundle(rs, 1, &[&producer]);
        // Asking for more than the default is clamped down to the default, never up.
        let sk = acquire_stream_key(
            rs,
            std::slice::from_ref(&bundle),
            &producer.id,
            &producer.secret,
        )
        .unwrap()
        .with_cap(DEFAULT_SEAL_CAP * 1000);
        assert_eq!(sk.remaining(), DEFAULT_SEAL_CAP);
    }

    #[test]
    fn set_cap_is_monotone_and_cannot_loosen() {
        // set_cap / with_cap take min(current, requested): once tightened, a later request
        // for a higher cap (even the default) does not loosen it back.
        let rs = "seal-cap-monotone";
        let keyexpr = "data/coalition-air/cop";
        let producer = party("coalition-air/prod");
        let bundle = published_bundle(rs, 1, &[&producer]);
        let mut sk = acquire_stream_key(
            rs,
            std::slice::from_ref(&bundle),
            &producer.id,
            &producer.secret,
        )
        .unwrap();
        sk.set_cap(2);
        assert_eq!(sk.remaining(), 2);
        sk.set_cap(DEFAULT_SEAL_CAP); // a request to loosen back to the default
        assert_eq!(sk.remaining(), 2, "set_cap never loosens the cap");
        // And the tightened cap still bites.
        sk.seal(Context(keyexpr), b"a").unwrap();
        sk.seal(Context(keyexpr), b"b").unwrap();
        assert_eq!(
            sk.seal(Context(keyexpr), b"c").unwrap_err(),
            Error::RotationRequired
        );
    }

    #[test]
    fn seal_count_carries_over_across_reacquire_of_the_same_generation() {
        // The security property (F1): re-acquiring the SAME generation must not reset the
        // nonce budget on the same DEK. First key exhausts a tightened cap; a second key
        // for the same (rs, gen) inherits the exhausted count and is refused immediately.
        let rs = "seal-carryover-same-gen";
        let keyexpr = "data/coalition-air/cop";
        let producer = party("coalition-air/prod");
        let bundle = published_bundle(rs, 1, &[&producer]);

        let mut sk1 = acquire_stream_key(
            rs,
            std::slice::from_ref(&bundle),
            &producer.id,
            &producer.secret,
        )
        .unwrap()
        .with_cap(2);
        sk1.seal(Context(keyexpr), b"a").unwrap();
        sk1.seal(Context(keyexpr), b"b").unwrap();
        assert_eq!(
            sk1.seal(Context(keyexpr), b"c").unwrap_err(),
            Error::RotationRequired
        );
        drop(sk1);

        // Re-acquire the same generation: the cumulative count carried over.
        let mut sk2 = acquire_stream_key(
            rs,
            std::slice::from_ref(&bundle),
            &producer.id,
            &producer.secret,
        )
        .unwrap()
        .with_cap(2);
        assert_eq!(
            sk2.remaining(),
            0,
            "re-acquire inherits the exhausted budget"
        );
        assert_eq!(
            sk2.seal(Context(keyexpr), b"d").unwrap_err(),
            Error::RotationRequired,
            "the budget cannot be reset by re-acquiring the same generation"
        );
    }

    #[test]
    fn a_new_generation_starts_with_a_fresh_seal_budget() {
        // The flip side of carryover: a DIFFERENT generation is a different budget line —
        // rotation legitimately re-keys, so its count starts fresh.
        let rs = "seal-carryover-new-gen";
        let keyexpr = "data/coalition-air/cop";
        let producer = party("coalition-air/prod");
        let g1 = published_bundle(rs, 1, &[&producer]);
        let mut sk1 = acquire_stream_key(
            rs,
            std::slice::from_ref(&g1),
            &producer.id,
            &producer.secret,
        )
        .unwrap()
        .with_cap(1);
        sk1.seal(Context(keyexpr), b"a").unwrap();
        assert_eq!(
            sk1.seal(Context(keyexpr), b"b").unwrap_err(),
            Error::RotationRequired
        );
        drop(sk1);

        let g2 = published_bundle(rs, 2, &[&producer]);
        let mut sk2 = acquire_stream_key(
            rs,
            std::slice::from_ref(&g2),
            &producer.id,
            &producer.secret,
        )
        .unwrap();
        assert_eq!(
            sk2.remaining(),
            DEFAULT_SEAL_CAP,
            "a new generation starts fresh"
        );
        sk2.seal(Context(keyexpr), b"c")
            .expect("the new generation seals under a fresh budget");
    }
}
