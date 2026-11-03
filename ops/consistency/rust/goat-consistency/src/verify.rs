// SPDX-License-Identifier: Apache-2.0
//
// verify.rs — verify-at-read for the IntentV1 detached signature (CF-5).
//
// The scheme is ADR 1082's, reused verbatim: <intent.json>.sig is
// base64(ASN.1 DER ECDSA-Sig-Value, P-256) over SHA-256 of the EXACT file
// bytes, minted by ops/enrollment/cmd/intent-sign with the per-goatnet
// offline CA. The verification key is NEVER taken on the artifact's word
// alone: the artifact's ca_id must resolve to an SPKI at
// <pubkey_dir>/<ca_id>.pem — the committed ops/enrollment/public-keys set,
// deployed beside the artifact by the consistency-health role, the same
// trust root goat-cli embeds for name snapshots and enrollment bundles
// (crates/goat-cli/src/bundle.rs).
//
// FAIL-CLOSED (ADR 1109 P14): every error string returned here is a REFUSAL
// a reader prints verbatim instead of grading. There is no "skip
// verification" path — a context that cannot verify says so and refuses.
//
// Canon: docs/design/consistency-foundation.md §3.4 (ADR 1110 D1).

use base64::Engine as _;
use p256::ecdsa::signature::Verifier as _;
use p256::ecdsa::{Signature, VerifyingKey};
use p256::pkcs8::DecodePublicKey as _;
use serde_json::Value;
use std::path::Path;

/// The base64 body of the PUBLIC KEY block, with RFC-1421 headers stripped.
///
/// public-keys PEMs carry headers (CA-ID:, Created-At:, Note:) inside the
/// block; strict RFC 7468 parsers refuse them (the same reason
/// crates/goat-cli/src/bundle.rs uses the lenient `pem` crate), so the DER is
/// rebuilt from the base64 body — the dance ops/ci/check-name-snapshot-sig.sh
/// does with python.
pub fn spki_der_from_pem(text: &str) -> Result<Vec<u8>, String> {
    let mut body = String::new();
    let mut inside = false;
    for line in text.lines() {
        let line = line.trim();
        if line == "-----BEGIN PUBLIC KEY-----" {
            inside = true;
        } else if line == "-----END PUBLIC KEY-----" {
            break;
        } else if inside && !line.is_empty() && !line.contains(':') {
            body.push_str(line);
        }
    }
    if body.is_empty() {
        return Err("no PUBLIC KEY block".to_string());
    }
    base64::engine::general_purpose::STANDARD
        .decode(&body)
        .map_err(|e| format!("PUBLIC KEY body is not base64: {e}"))
}

/// Verify a detached signature (base64 of DER ECDSA P-256 over SHA-256 of
/// `bytes`) against an SPKI DER public key. Ok(()) or the refusal.
pub fn verify_detached(bytes: &[u8], sig_text: &str, spki_der: &[u8]) -> Result<(), String> {
    let key = VerifyingKey::from_public_key_der(spki_der)
        .map_err(|e| format!("declared public key is not ECDSA P-256 SPKI: {e}"))?;
    let der = base64::engine::general_purpose::STANDARD
        .decode(sig_text.trim())
        .map_err(|e| format!("signature is not base64: {e}"))?;
    let sig = Signature::from_der(&der).map_err(|e| format!("signature is not DER: {e}"))?;
    key.verify(bytes, &sig)
        .map_err(|_| "signature does not verify over these bytes".to_string())
}

/// Verify an intent artifact's detached signature: `bytes` are the EXACT
/// bytes the caller read from `path` (verified-then-used, never re-read),
/// `doc` is their parse (only `ca_id` is consulted), and `pubkey_dir` is the
/// trusted SPKI set. Ok(()) or the refusal a reader prints instead of a
/// grade.
pub fn verify_intent(
    path: &Path,
    bytes: &[u8],
    doc: &Value,
    pubkey_dir: &Path,
) -> Result<(), String> {
    let sig_path = path.with_file_name(format!(
        "{}.sig",
        path.file_name().and_then(|n| n.to_str()).unwrap_or("intent.json")
    ));
    let sig_text = std::fs::read_to_string(&sig_path).map_err(|_| {
        format!(
            "{} has no readable detached signature at {} — an unsigned intent is \
             refused, never graded (sign with ops/enrollment/cmd/intent-sign and \
             redeploy both files)",
            path.display(),
            sig_path.display()
        )
    })?;

    let ca_id = doc.get("ca_id").and_then(|v| v.as_str()).unwrap_or("");
    if ca_id.is_empty() || ca_id.contains('/') || ca_id.contains('\\') {
        return Err(format!(
            "{} declares no usable ca_id — the verification key is resolved from \
             the deployed SPKI set by CA id, so an artifact naming none (or a \
             path-shaped one) cannot be verified",
            path.display()
        ));
    }
    let pubkey_path = pubkey_dir.join(format!("{ca_id}.pem"));
    let pem_text = std::fs::read_to_string(&pubkey_path).map_err(|_| {
        format!(
            "{} declares ca_id={ca_id} but {} is not present — the key is never \
             taken on the artifact's word alone; deploy ops/enrollment/public-keys \
             beside the artifact",
            path.display(),
            pubkey_path.display()
        )
    })?;
    let spki = spki_der_from_pem(&pem_text)
        .map_err(|e| format!("{}: {e}", pubkey_path.display()))?;

    verify_detached(bytes, &sig_text, &spki).map_err(|e| {
        format!(
            "{} FAILED signature verification against {} ({e}) — the file changed \
             after signing, or the signature is not this artifact's. Refusing to \
             grade against an intent nobody signed",
            path.display(),
            pubkey_path.display()
        )
    })
}

/// Read, verify and integrity-check a committed IntentV1 generation, in one
/// call — the read path BOTH long-running readers use (the CF-3 rollup meter
/// and the CF-6 console), lifted here so neither carries its own copy.
///
/// P14 fail-closed: an unverifiable artifact is REFUSED, never rendered as
/// fact. Three layers, checked in order on EVERY read: the detached offline-CA
/// signature over the exact file bytes (WHO produced them; key resolved from
/// `pubkey_dir/<ca_id>.pem`, never from the artifact), then `content_hash`
/// self-consistency (the body is what it claims about itself). The third layer
/// — the regenerate-diff against the tree — is CI's, repo-side. The bytes
/// verified are the bytes parsed: no re-read between check and use.
///
/// Returns `(Some(doc), "")` on success or `(None, refusal)` — the refusal is
/// printed/rendered verbatim in place of a grade.
pub fn load_verified(path: &Path, pubkey_dir: &Path) -> (Option<Value>, String) {
    let bytes = match std::fs::read(path) {
        Ok(b) => b,
        Err(e) => return (None, format!("cannot read {}: {e}", path.display())),
    };
    let doc: Value = match serde_json::from_slice(&bytes) {
        Ok(v) => v,
        Err(e) => return (None, format!("{} is not JSON: {e}", path.display())),
    };
    if let Err(refusal) = verify_intent(path, &bytes, &doc, pubkey_dir) {
        return (None, refusal);
    }
    if !crate::intent::self_consistent(&doc) {
        return (
            None,
            format!(
                "{} FAILED its integrity check — its content_hash is not the hash of its own body. \
                 Refusing to grade against an intent nobody can trust.",
                path.display()
            ),
        );
    }
    (Some(doc), String::new())
}

#[cfg(test)]
mod tests {
    use super::*;
    use p256::ecdsa::{signature::Signer as _, SigningKey};
    use p256::pkcs8::EncodePublicKey as _;

    /// A deterministic throwaway P-256 key — a fixed nonzero scalar, so the
    /// test needs no RNG dependency. Test-only; never key material.
    pub(crate) fn scratch_key() -> SigningKey {
        let mut scalar = [0u8; 32];
        scalar[31] = 7;
        SigningKey::from_slice(&scalar).expect("nonzero scalar in range")
    }

    pub(crate) fn sign_detached(key: &SigningKey, bytes: &[u8]) -> String {
        let sig: Signature = key.sign(bytes);
        base64::engine::general_purpose::STANDARD.encode(sig.to_der().as_bytes()) + "\n"
    }

    /// The committed SPKI format, headers inside the block and all.
    pub(crate) fn headered_pem(key: &SigningKey, ca_id: &str) -> String {
        let spki = key
            .verifying_key()
            .to_public_key_der()
            .expect("spki der")
            .as_bytes()
            .to_vec();
        let b64 = base64::engine::general_purpose::STANDARD.encode(spki);
        format!(
            "-----BEGIN PUBLIC KEY-----\nCA-ID: {ca_id}\nNote: test key\n\n{b64}\n-----END PUBLIC KEY-----\n"
        )
    }

    #[test]
    fn signed_bytes_verify_and_tampered_bytes_refuse() {
        let key = scratch_key();
        let body = b"{\n  \"ca_id\": \"test-ca\"\n}\n";
        let sig = sign_detached(&key, body);
        let spki = spki_der_from_pem(&headered_pem(&key, "test-ca")).expect("clean pem");

        assert!(verify_detached(body, &sig, &spki).is_ok());
        let refusal = verify_detached(b"{\n  \"ca_id\": \"test-cb\"\n}\n", &sig, &spki)
            .expect_err("tampered bytes must refuse");
        assert!(refusal.contains("does not verify"));
        // Reverted, the same signature is good again — the tamper, not the
        // artifact, was the problem.
        assert!(verify_detached(body, &sig, &spki).is_ok());
    }

    #[test]
    fn garbage_signatures_refuse_rather_than_panic() {
        let key = scratch_key();
        let spki = spki_der_from_pem(&headered_pem(&key, "test-ca")).expect("clean pem");
        assert!(verify_detached(b"x", "not base64", &spki).is_err());
        assert!(verify_detached(b"x", "AAAA", &spki).is_err());
        assert!(spki_der_from_pem("no pem here").is_err());
    }

    /// Cross-stack: both committed artifacts (signed by the Go signer with
    /// the real offline CAs) verify through this Rust path against the
    /// committed public keys. The corpus already proves both languages hash
    /// identically; this proves they TRUST identically.
    #[test]
    fn committed_artifacts_verify_against_committed_public_keys() {
        let consistency = Path::new(env!("CARGO_MANIFEST_DIR")).join("../..");
        let pubkey_dir = consistency.join("../enrollment/public-keys");
        let intent_dir = consistency.join("intent");
        let mut seen = 0;
        for entry in std::fs::read_dir(&intent_dir).expect("ops/consistency/intent exists") {
            let path = entry.expect("readable entry").path().join("intent.json");
            if !path.exists() {
                continue;
            }
            let bytes = std::fs::read(&path).expect("readable intent");
            let doc: Value = serde_json::from_slice(&bytes).expect("intent is JSON");
            verify_intent(&path, &bytes, &doc, &pubkey_dir)
                .unwrap_or_else(|e| panic!("committed artifact must verify: {e}"));
            seen += 1;
        }
        assert!(seen >= 2, "both governed goatnets' artifacts are committed and signed");
    }
}
