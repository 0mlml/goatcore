//! Relay-client authentication validator.
//!
//! Wire-compatible Rust port of netbird's relay timed-HMAC auth (**v2**),
//! extended with the **peer-ID-bound** variant per [ADR 1021]. The netbird
//! Go client sends an HMAC token in the Auth message's `auth_payload`; this
//! validator checks it before the relay registers the peer — closing the
//! relay-client auth gap ([F-176]) and, for peer-bound tokens, intra-mesh
//! relay-layer impersonation.
//!
//! [ADR 1021]: ../../../docs/adr/1021-relay-peer-id-bound-tokens.md
//! [F-176]: ../../../docs/findings/F-176-goat-relay-permissive-validator-open-relay.md
//!
//! # Token wire format
//!
//! `[algo:1][sig:32][payload]`, mirroring `shared/relay/auth/hmac/v2`:
//!
//! - `algo = 1` ([`AUTH_ALGO_HMAC_SHA256`]) — **unbound** (upstream netbird v2):
//!   `sig = HMAC-SHA256(key, payload)`.
//! - `algo = 2` ([`AUTH_ALGO_HMAC_SHA256_PEER_BOUND`]) — **peer-bound** (ADR 1021):
//!   `sig = HMAC-SHA256(key, peer_id_bytes(36) || payload)`. The `peer_id`
//!   is the claimed relay `PeerId` from the same Auth frame, so a token
//!   minted for peer A does not validate when presented as peer B.
//!
//! In both cases `payload` is the ASCII decimal unix expiry timestamp and
//! `key = SHA256(relay_secret)` (matching netbird's
//! `sha256.Sum256([]byte(AuthSecret))` in `relay/cmd/root.go`).
//!
//! # Migration policy
//!
//! [`TimedHmacValidator::with_policy`] takes `accept_unbound`:
//!
//! - `true` (default, [`Self::from_secret`]) — **dual-accept**: both algo
//!   `1` and algo `2` pass. This is the migration window: relays upgrade
//!   first and keep authenticating today's unbound-token-issuing
//!   management servers.
//! - `false` — **bound-only** (ADR 1021 Phase C): algo `1` is rejected;
//!   only peer-bound tokens pass. Flip this only once the whole fleet
//!   issues algo `2`.
//!
//! # Security scope
//!
//! Unbound timed-HMAC proves the client holds the per-site shared secret —
//! it closes the *external* open-relay / slot-hijack hole. Peer-ID
//! binding additionally closes *intra-mesh* impersonation: because peers do
//! not hold the secret (the management server issues their token), a peer
//! cannot mint a token for another peer's `peer_id`. Neither layer protects
//! relayed payloads — those stay end-to-end WireGuard-encrypted regardless.

use std::time::{SystemTime, UNIX_EPOCH};

use ring::{digest, hmac};

use goat_relay_protocol::error::{ProtocolError, Result};
use goat_relay_protocol::handshake::Validator;
use goat_relay_protocol::messages::{PeerId, PEER_ID_SIZE};

/// `AuthAlgo` byte for unbound HMAC-SHA256 — matches netbird v2
/// `AuthAlgoHMACSHA256 = 1` (`shared/relay/auth/hmac/v2/algo.go`).
const AUTH_ALGO_HMAC_SHA256: u8 = 1;

/// `AuthAlgo` byte for peer-ID-bound HMAC-SHA256 (ADR 1021). The signature
/// covers `peer_id_bytes(36) || payload` instead of `payload` alone.
const AUTH_ALGO_HMAC_SHA256_PEER_BOUND: u8 = 2;

/// HMAC-SHA256 signature size in bytes.
const SIG_SIZE: usize = 32;

/// Minimum payload length — 10 ASCII digits of a unix timestamp. Mirrors
/// netbird's `minLengthUnixTimestamp`.
const MIN_PAYLOAD_LEN: usize = 10;

/// Clock-skew grace (seconds) applied to the expiry check. A token
/// is rejected only once `now > expiry + grace`, so a relay host whose
/// clock runs up to this far ahead of the token minter/client does not
/// reject still-valid tokens near end-of-life. Mirrors the skew tolerance
/// the NTS/cert validators use; the fleet's clock-hygiene concerns
/// (chrony-presync, ntp-health) make a hard unslewed `now > expiry`
/// comparison a real source of intermittent near-expiry auth failures.
const CLOCK_SKEW_GRACE_SECS: i64 = 60;

/// Timed-HMAC relay-client validator, wire-compatible with netbird relay
/// auth v2 and the ADR 1021 peer-bound extension. Construct once per relay
/// process and share across all connection-handler tasks (it is
/// `Send + Sync` — the only state is immutable key material + a policy bool).
pub struct TimedHmacValidator {
    /// HMAC keys = `SHA256(raw shared secret)`, one per accepted secret.
    /// Normally one (the current secret); during a secret rotation it
    /// holds `[current, previous]` so tokens signed with either pass for
    /// the overlap window — without this, rotating the per-site
    /// relay secret instantly rejects every in-flight token until the
    /// whole fleet re-mints, an avoidable hard cutover. A token is valid
    /// if its MAC verifies against ANY held key.
    keys: Vec<hmac::Key>,
    /// When `true`, unbound (algo `1`) tokens are accepted alongside
    /// peer-bound (algo `2`) ones — the migration default. When `false`,
    /// only peer-bound tokens pass (ADR 1021 Phase C).
    accept_unbound: bool,
}

impl TimedHmacValidator {
    /// Build a validator from the raw per-site shared secret in the
    /// **dual-accept** migration default (accepts both unbound and
    /// peer-bound tokens). Equivalent to `with_policy(secret, true)`.
    ///
    /// The secret is SHA256-hashed to form the HMAC key, matching
    /// netbird's server-side `sha256.Sum256([]byte(AuthSecret))`. Pass the
    /// raw secret bytes here (not a pre-hash) for wire compatibility.
    #[must_use]
    pub fn from_secret(raw_secret: &[u8]) -> Self {
        Self::with_policy(raw_secret, true)
    }

    /// Build a validator with an explicit unbound-token policy.
    ///
    /// `accept_unbound = true` is the migration default (dual-accept);
    /// `false` rejects algo-`1` tokens, enforcing peer-bound-only
    /// (ADR 1021 Phase C). See the module docs.
    #[must_use]
    pub fn with_policy(raw_secret: &[u8], accept_unbound: bool) -> Self {
        Self {
            keys: vec![Self::derive_key(raw_secret)],
            accept_unbound,
        }
    }

    /// Build a validator that accepts tokens signed by EITHER the current
    /// or the previous per-site secret — the secret-rotation overlap
    /// window. Deploy with both during a rotation, then drop the
    /// previous once the fleet has re-minted on the current. `accept_unbound`
    /// carries the same dual-accept/bound-only policy as [`Self::with_policy`].
    #[must_use]
    pub fn with_rotation(current_secret: &[u8], previous_secret: &[u8], accept_unbound: bool) -> Self {
        Self {
            keys: vec![
                Self::derive_key(current_secret),
                Self::derive_key(previous_secret),
            ],
            accept_unbound,
        }
    }

    /// HMAC key = `SHA256(raw secret)`, matching netbird's
    /// `sha256.Sum256([]byte(AuthSecret))`.
    fn derive_key(raw_secret: &[u8]) -> hmac::Key {
        let hashed = digest::digest(&digest::SHA256, raw_secret);
        hmac::Key::new(hmac::HMAC_SHA256, hashed.as_ref())
    }

    /// Current unix time in seconds as `i64`, mirroring Go's
    /// `time.Now().Unix()`.
    fn now_unix() -> Result<i64> {
        let secs = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_err(|_| ProtocolError::Parse("system clock before unix epoch".into()))?
            .as_secs();
        i64::try_from(secs)
            .map_err(|_| ProtocolError::Parse("unix timestamp overflows i64".into()))
    }

    /// Constant-time MAC verification over `mac_msg` against `signature`,
    /// then parse `token_payload` as a unix expiry and reject if past.
    ///
    /// For unbound tokens `mac_msg == token_payload`; for peer-bound tokens
    /// `mac_msg == peer_id || token_payload`. The expiry is always read
    /// from `token_payload`.
    fn verify_mac_and_expiry(
        &self,
        mac_msg: &[u8],
        signature: &[u8],
        token_payload: &[u8],
    ) -> Result<()> {
        // ring recomputes the MAC over `mac_msg` and compares in constant
        // time per key (matches Go's `hmac.Equal`). The token is valid if
        // it verifies against ANY held key — normally just the current
        // secret, or {current, previous} during a rotation window.
        let mac_ok = self
            .keys
            .iter()
            .any(|key| hmac::verify(key, mac_msg, signature).is_ok());
        if !mac_ok {
            return Err(ProtocolError::Parse("invalid auth signature".into()));
        }

        let ts_str = core::str::from_utf8(token_payload)
            .map_err(|_| ProtocolError::Parse("auth token payload is not utf-8".into()))?;
        let expiry: i64 = ts_str.parse().map_err(|_| {
            ProtocolError::Parse("auth token payload is not an integer timestamp".into())
        })?;

        // Reject only once we are PAST expiry by more than the skew grace,
        // so a relay clock running slightly ahead of the minter does not
        // reject still-valid near-expiry tokens. saturating_add
        // guards a pathological i64::MAX expiry.
        if Self::now_unix()? > expiry.saturating_add(CLOCK_SKEW_GRACE_SECS) {
            return Err(ProtocolError::Parse("expired auth token".into()));
        }
        Ok(())
    }
}

impl Validator for TimedHmacValidator {
    fn validate(&self, peer_id: &PeerId, payload: &[u8]) -> Result<()> {
        // Unmarshal token: [algo:1][sig:32][payload:N].
        let Some((&algo, rest)) = payload.split_first() else {
            return Err(ProtocolError::Parse("empty auth token".into()));
        };
        if rest.len() < SIG_SIZE {
            return Err(ProtocolError::Parse(
                "auth token too short for HMAC-SHA256 signature".into(),
            ));
        }
        let (signature, token_payload) = rest.split_at(SIG_SIZE);
        if token_payload.len() < MIN_PAYLOAD_LEN {
            return Err(ProtocolError::Parse(
                "auth token payload shorter than a unix timestamp".into(),
            ));
        }

        match algo {
            AUTH_ALGO_HMAC_SHA256 => {
                if !self.accept_unbound {
                    return Err(ProtocolError::Parse(
                        "unbound auth token rejected: this relay requires peer-bound tokens".into(),
                    ));
                }
                self.verify_mac_and_expiry(token_payload, signature, token_payload)
            }
            AUTH_ALGO_HMAC_SHA256_PEER_BOUND => {
                // MAC covers peer_id || payload, binding the token to the
                // claimed relay identity.
                let mut mac_msg = Vec::with_capacity(PEER_ID_SIZE + token_payload.len());
                mac_msg.extend_from_slice(peer_id.as_ref());
                mac_msg.extend_from_slice(token_payload);
                self.verify_mac_and_expiry(&mac_msg, signature, token_payload)
            }
            other => Err(ProtocolError::Parse(format!(
                "unsupported auth algorithm: {other}"
            ))),
        }
    }
}

/// Generate an **unbound** (algo `1`) HMAC token for tests — the
/// client-side mirror of upstream netbird v2 `Generator.GenerateToken`,
/// with an explicit absolute `expiry_unix` so tests can mint expired
/// tokens. `sig = HMAC-SHA256(SHA256(secret), payload)`.
#[cfg(test)]
pub(crate) fn generate_test_token(raw_secret: &[u8], expiry_unix: i64) -> Vec<u8> {
    let hashed = digest::digest(&digest::SHA256, raw_secret);
    let key = hmac::Key::new(hmac::HMAC_SHA256, hashed.as_ref());
    let payload = expiry_unix.to_string().into_bytes();
    let sig = hmac::sign(&key, &payload);

    let mut token = Vec::with_capacity(1 + SIG_SIZE + payload.len());
    token.push(AUTH_ALGO_HMAC_SHA256);
    token.extend_from_slice(sig.as_ref());
    token.extend_from_slice(&payload);
    token
}

/// Generate a **peer-bound** (algo `2`) HMAC token for tests, bound to
/// `peer_id`. `sig = HMAC-SHA256(SHA256(secret), peer_id || payload)`.
/// This is the client-/management-side mirror of the ADR 1021 binding.
#[cfg(test)]
pub(crate) fn generate_test_token_bound(
    raw_secret: &[u8],
    peer_id: &PeerId,
    expiry_unix: i64,
) -> Vec<u8> {
    let hashed = digest::digest(&digest::SHA256, raw_secret);
    let key = hmac::Key::new(hmac::HMAC_SHA256, hashed.as_ref());
    let payload = expiry_unix.to_string().into_bytes();

    let mut mac_msg = Vec::with_capacity(PEER_ID_SIZE + payload.len());
    mac_msg.extend_from_slice(peer_id.as_ref());
    mac_msg.extend_from_slice(&payload);
    let sig = hmac::sign(&key, &mac_msg);

    let mut token = Vec::with_capacity(1 + SIG_SIZE + payload.len());
    token.push(AUTH_ALGO_HMAC_SHA256_PEER_BOUND);
    token.extend_from_slice(sig.as_ref());
    token.extend_from_slice(&payload);
    token
}

#[cfg(test)]
mod tests {
    use super::*;

    const SECRET: &[u8] = b"per-site-relay-shared-secret";

    fn far_future() -> i64 {
        TimedHmacValidator::now_unix().unwrap() + 3600
    }

    fn test_peer(byte: u8) -> PeerId {
        let mut b = [0u8; PEER_ID_SIZE];
        b[..4].copy_from_slice(b"sha-");
        b[4..].fill(byte);
        PeerId::from_bytes(b)
    }

    // --- unbound (algo 1) ---------------------------------------------------

    #[test]
    fn accepts_valid_unbound_token() {
        let v = TimedHmacValidator::from_secret(SECRET); // dual-accept
        let token = generate_test_token(SECRET, far_future());
        // peer_id is irrelevant for unbound tokens — any value passes.
        assert!(v.validate(&test_peer(0x11), &token).is_ok());
    }

    #[test]
    fn rejects_unbound_when_require_peer_bound() {
        let v = TimedHmacValidator::with_policy(SECRET, false); // bound-only
        let token = generate_test_token(SECRET, far_future());
        let err = v.validate(&test_peer(0x11), &token).unwrap_err();
        assert!(format!("{err}").contains("peer-bound"));
    }

    // --- peer-bound (algo 2) ------------------------------------------------

    #[test]
    fn accepts_valid_peer_bound_token() {
        let v = TimedHmacValidator::from_secret(SECRET);
        let peer = test_peer(0xAA);
        let token = generate_test_token_bound(SECRET, &peer, far_future());
        assert!(v.validate(&peer, &token).is_ok());
    }

    #[test]
    fn rejects_peer_bound_token_presented_as_wrong_peer() {
        // The core impersonation defense: a token minted for peer A must
        // NOT validate when the Auth frame claims peer B.
        let v = TimedHmacValidator::from_secret(SECRET);
        let peer_a = test_peer(0xAA);
        let peer_b = test_peer(0xBB);
        let token_for_a = generate_test_token_bound(SECRET, &peer_a, far_future());
        assert!(v.validate(&peer_a, &token_for_a).is_ok());
        assert!(v.validate(&peer_b, &token_for_a).is_err());
    }

    #[test]
    fn peer_bound_accepted_even_in_bound_only_mode() {
        let v = TimedHmacValidator::with_policy(SECRET, false); // bound-only
        let peer = test_peer(0xCC);
        let token = generate_test_token_bound(SECRET, &peer, far_future());
        assert!(v.validate(&peer, &token).is_ok());
    }

    #[test]
    fn rejects_peer_bound_wrong_secret() {
        let v = TimedHmacValidator::from_secret(SECRET);
        let peer = test_peer(0xAA);
        let token = generate_test_token_bound(b"a-different-secret", &peer, far_future());
        assert!(v.validate(&peer, &token).is_err());
    }

    #[test]
    fn rejects_expired_peer_bound_token() {
        let v = TimedHmacValidator::from_secret(SECRET);
        let peer = test_peer(0xAA);
        let token = generate_test_token_bound(
            SECRET,
            &peer,
            TimedHmacValidator::now_unix().unwrap() - 100,
        );
        let err = v.validate(&peer, &token).unwrap_err();
        assert!(format!("{err}").contains("expired"));
    }

    // --- clock skew + secret rotation ------------------------------

    #[test]
    fn accepts_token_within_clock_skew_grace() {
        // Token expired 30s ago; with a 60s skew grace it is still accepted
        // (simulates the relay clock running ahead of the minter).
        let v = TimedHmacValidator::from_secret(SECRET);
        let token =
            generate_test_token(SECRET, TimedHmacValidator::now_unix().unwrap() - 30);
        assert!(
            v.validate(&test_peer(0x11), &token).is_ok(),
            "token within the {CLOCK_SKEW_GRACE_SECS}s skew grace must be accepted"
        );
    }

    #[test]
    fn rejects_token_past_clock_skew_grace() {
        // Expired well beyond the grace → rejected.
        let v = TimedHmacValidator::from_secret(SECRET);
        let token = generate_test_token(
            SECRET,
            TimedHmacValidator::now_unix().unwrap() - (CLOCK_SKEW_GRACE_SECS + 60),
        );
        let err = v.validate(&test_peer(0x11), &token).unwrap_err();
        assert!(format!("{err}").contains("expired"));
    }

    #[test]
    fn with_rotation_accepts_either_secret() {
        // During a rotation the relay holds {current, previous}; tokens
        // signed by EITHER pass, an unrelated secret fails.
        let current = b"current-secret";
        let previous = b"previous-secret";
        let v = TimedHmacValidator::with_rotation(current, previous, true);
        let peer = test_peer(0x22);

        let tok_current = generate_test_token_bound(current, &peer, far_future());
        let tok_previous = generate_test_token_bound(previous, &peer, far_future());
        let tok_other = generate_test_token_bound(b"unrelated", &peer, far_future());

        assert!(v.validate(&peer, &tok_current).is_ok(), "current secret accepted");
        assert!(v.validate(&peer, &tok_previous).is_ok(), "previous secret accepted in window");
        assert!(v.validate(&peer, &tok_other).is_err(), "unrelated secret rejected");
    }

    // --- shared structural / tamper cases ----------------------------------

    #[test]
    fn rejects_wrong_secret_unbound() {
        let v = TimedHmacValidator::from_secret(SECRET);
        let token = generate_test_token(b"a-different-secret", far_future());
        assert!(v.validate(&test_peer(0x11), &token).is_err());
    }

    #[test]
    fn rejects_tampered_signature() {
        let v = TimedHmacValidator::from_secret(SECRET);
        let mut token = generate_test_token(SECRET, far_future());
        token[1] ^= 0xFF; // flip a signature byte
        assert!(v.validate(&test_peer(0x11), &token).is_err());
    }

    #[test]
    fn rejects_tampered_payload() {
        let v = TimedHmacValidator::from_secret(SECRET);
        let mut token = generate_test_token(SECRET, far_future());
        let last = token.len() - 1;
        token[last] = if token[last] == b'9' { b'8' } else { b'9' };
        assert!(v.validate(&test_peer(0x11), &token).is_err());
    }

    #[test]
    fn rejects_expired_unbound_token() {
        let v = TimedHmacValidator::from_secret(SECRET);
        let token = generate_test_token(SECRET, TimedHmacValidator::now_unix().unwrap() - 100);
        let err = v.validate(&test_peer(0x11), &token).unwrap_err();
        assert!(format!("{err}").contains("expired"));
    }

    #[test]
    fn rejects_unknown_algo() {
        let v = TimedHmacValidator::from_secret(SECRET);
        let mut token = generate_test_token(SECRET, far_future());
        token[0] = 0x03; // neither 1 nor 2
        assert!(v.validate(&test_peer(0x11), &token).is_err());
    }

    #[test]
    fn rejects_empty_token() {
        let v = TimedHmacValidator::from_secret(SECRET);
        assert!(v.validate(&test_peer(0x11), &[]).is_err());
    }

    #[test]
    fn rejects_too_short_for_signature() {
        let v = TimedHmacValidator::from_secret(SECRET);
        assert!(v
            .validate(&test_peer(0x11), &[AUTH_ALGO_HMAC_SHA256, 0x01, 0x02, 0x03])
            .is_err());
    }

    #[test]
    fn rejects_short_payload() {
        let v = TimedHmacValidator::from_secret(SECRET);
        let short_expiry = b"123"; // 3 bytes < MIN_PAYLOAD_LEN
        let hashed = digest::digest(&digest::SHA256, SECRET);
        let key = hmac::Key::new(hmac::HMAC_SHA256, hashed.as_ref());
        let sig = hmac::sign(&key, short_expiry);
        let mut token = vec![AUTH_ALGO_HMAC_SHA256];
        token.extend_from_slice(sig.as_ref());
        token.extend_from_slice(short_expiry);
        assert!(v.validate(&test_peer(0x11), &token).is_err());
    }
}
