//! Keeping grant-keyd's bundles verifiable across SVID renewal and restart.
//!
//! A bundle carries the SVID it was signed under, and a reader refuses it once
//! that SVID expires. A SPIRE SVID lives about an hour and spiffe-helper renews
//! it on disk, so the daemon re-reads its signing key and SVID every sweep and,
//! when they change, re-signs every bundle it holds ([`KeyService::republish`]).
//!
//! A restarted daemon holds nothing, so on a release-set's first sweep it reads
//! that set's bundles back from `keys/**` storage and adopts the ones it can
//! authenticate as its own ([`adoptable`]). Generations then continue above the
//! stored ones instead of starting again at 1, and the adopted bundles are
//! re-signed with the other retained ones.
//!
//! [`KeyService::republish`]: grant_crypto::service::KeyService::republish

use std::path::PathBuf;

use grant_crypto::KeyBundle;
use p256::ecdsa::SigningKey;
use p256::pkcs8::{DecodePrivateKey, DecodePublicKey, EncodePublicKey};
use x509_cert::der::{DecodePem, Encode};
use x509_cert::Certificate;

use crate::svid::{verify_own_stored_bundle, SvidValidator};

/// The signing key and SVID files spiffe-helper keeps current.
pub struct SignerFiles {
    key: PathBuf,
    svid: Option<PathBuf>,
    last: Option<(String, Option<String>)>,
}

impl SignerFiles {
    pub fn new(key: PathBuf, svid: Option<PathBuf>) -> Self {
        Self {
            key,
            svid,
            last: None,
        }
    }

    /// Read the key and SVID. Returns them when either differs from the last
    /// pair returned (the first call always returns them), `Ok(None)` when
    /// nothing changed.
    ///
    /// spiffe-helper writes the certificate and the key as two files, so a read
    /// can land between the two writes. A key that does not match the SVID's
    /// public key is reported as an error and NOT adopted; the next sweep reads
    /// the finished pair.
    pub fn reload_if_changed(&mut self) -> Result<Option<(SigningKey, Option<String>)>, String> {
        let key_pem = std::fs::read_to_string(&self.key)
            .map_err(|e| format!("signing key {}: {e}", self.key.display()))?;
        let svid_pem = match &self.svid {
            Some(p) => Some(
                std::fs::read_to_string(p)
                    .map_err(|e| format!("signer SVID {}: {e}", p.display()))?,
            ),
            None => None,
        };
        let pair = (key_pem, svid_pem);
        if self.last.as_ref() == Some(&pair) {
            return Ok(None);
        }
        let key = SigningKey::from_pkcs8_pem(&pair.0).map_err(|e| {
            format!(
                "signing key {}: not a PKCS#8 P-256 key: {e}",
                self.key.display()
            )
        })?;
        if let Some(svid) = &pair.1 {
            if !key_matches_cert(&key, svid)? {
                return Err("signing key does not match the SVID's public key \
                            (files mid-renewal?); keeping the current signer"
                    .into());
            }
        }
        let svid = pair.1.clone();
        self.last = Some(pair);
        Ok(Some((key, svid)))
    }
}

fn key_matches_cert(key: &SigningKey, cert_pem: &str) -> Result<bool, String> {
    let cert =
        Certificate::from_pem(cert_pem.as_bytes()).map_err(|e| format!("parse SVID: {e}"))?;
    let spki = cert
        .tbs_certificate
        .subject_public_key_info
        .to_der()
        .map_err(|e| format!("encode SVID key: {e}"))?;
    let cert_key = p256::PublicKey::from_public_key_der(&spki)
        .map_err(|_| "SVID key is not P-256".to_string())?;
    let ours = key
        .verifying_key()
        .to_public_key_der()
        .map_err(|e| format!("encode signing key: {e}"))?;
    let theirs = cert_key
        .to_public_key_der()
        .map_err(|e| format!("encode SVID key: {e}"))?;
    Ok(ours.as_bytes() == theirs.as_bytes())
}

/// The bundles among `payloads` (read back from `keys/<rs>/*`) that grant-keyd
/// itself published for `rs`: they parse, name `rs`, carry an SVID for
/// `key_service_spiffe_id` that chained to the trust root when it was issued,
/// and verify under it. Returns the adoptable bundles and how many were refused.
pub fn adoptable(
    payloads: &[Vec<u8>],
    rs: &str,
    validator: &SvidValidator,
    key_service_spiffe_id: &str,
) -> (Vec<KeyBundle>, usize) {
    let mut ok = Vec::new();
    let mut refused = 0;
    for p in payloads {
        match serde_json::from_slice::<KeyBundle>(p) {
            Ok(b)
                if b.rs == rs
                    && verify_own_stored_bundle(&b, validator, key_service_spiffe_id).is_ok() =>
            {
                ok.push(b)
            }
            _ => refused += 1,
        }
    }
    (ok, refused)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::svid::TrustBundle;
    use rcgen::{
        BasicConstraints, CertificateParams, ExtendedKeyUsagePurpose, IsCa, KeyPair, SanType,
    };
    use std::collections::BTreeMap;

    const ID: &str = "spiffe://zagros.zagros.example/grant-keyd";

    struct Ca {
        cert: rcgen::Certificate,
        key: KeyPair,
    }
    fn ca() -> Ca {
        let key = KeyPair::generate().unwrap();
        let mut p = CertificateParams::new(vec![]).unwrap();
        p.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
        let cert = p.self_signed(&key).unwrap();
        Ca { cert, key }
    }
    // An SVID for `ID` that expired in 2021, with its key as PKCS#8 PEM.
    fn expired_svid(ca: &Ca) -> (String, String) {
        use time::OffsetDateTime;
        let key = KeyPair::generate().unwrap();
        let mut p = CertificateParams::new(vec![]).unwrap();
        p.subject_alt_names = vec![SanType::URI(ID.try_into().unwrap())];
        p.extended_key_usages = vec![ExtendedKeyUsagePurpose::ClientAuth];
        p.not_before = OffsetDateTime::from_unix_timestamp(1_577_836_800).unwrap();
        p.not_after = OffsetDateTime::from_unix_timestamp(1_609_459_200).unwrap();
        let cert = p.signed_by(&key, &ca.cert, &ca.key).unwrap().pem();
        (cert, key.serialize_pem())
    }
    fn validator(ca: &Ca) -> SvidValidator {
        SvidValidator::new(
            "zagros.zagros.example",
            TrustBundle::from_pem(&ca.cert.pem()).unwrap(),
        )
    }
    fn bundle(rs: &str, gen: u64, key_pem: &str, svid: &str) -> Vec<u8> {
        let member = p256::SecretKey::from_pkcs8_pem(key_pem)
            .unwrap()
            .public_key();
        let mut b = grant_crypto::new_bundle(
            rs,
            gen,
            &grant_crypto::new_dek(),
            &BTreeMap::from([("m".to_string(), member)]),
        )
        .unwrap();
        grant_crypto::sign_bundle(
            &mut b,
            "grant-keyd",
            &SigningKey::from_pkcs8_pem(key_pem).unwrap(),
        )
        .unwrap();
        grant_crypto::set_signer_svid(&mut b, svid);
        serde_json::to_vec(&b).unwrap()
    }

    #[test]
    fn its_own_stored_bundles_are_adopted_even_after_their_svid_expired() {
        let ca = ca();
        let (svid, key) = expired_svid(&ca);
        let stored = vec![bundle("rs", 1, &key, &svid), bundle("rs", 2, &key, &svid)];
        let (found, refused) = adoptable(&stored, "rs", &validator(&ca), ID);
        assert_eq!(found.iter().map(|b| b.gen).collect::<Vec<_>>(), vec![1, 2]);
        assert_eq!(refused, 0);
    }

    #[test]
    fn a_bundle_from_another_root_another_set_or_garbage_is_refused() {
        let ours = ca();
        let attacker = ca();
        let (svid, key) = expired_svid(&attacker);
        let (own_svid, own_key) = expired_svid(&ours);
        let stored = vec![
            bundle("rs", 3, &key, &svid),
            bundle("other", 1, &own_key, &own_svid),
            b"not a bundle".to_vec(),
        ];
        let (found, refused) = adoptable(&stored, "rs", &validator(&ours), ID);
        assert!(found.is_empty());
        assert_eq!(refused, 3);
    }

    #[test]
    fn reload_reports_a_change_once_and_refuses_a_half_renewed_pair() {
        let dir = std::env::temp_dir().join(format!("grant-keyd-signer-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let (key_p, svid_p) = (dir.join("key.pem"), dir.join("cert.pem"));
        let ca = ca();
        let (svid1, key1) = expired_svid(&ca);
        let (svid2, key2) = expired_svid(&ca);
        std::fs::write(&key_p, &key1).unwrap();
        std::fs::write(&svid_p, &svid1).unwrap();
        let mut files = SignerFiles::new(key_p.clone(), Some(svid_p.clone()));
        assert!(
            files.reload_if_changed().unwrap().is_some(),
            "first read returns the pair"
        );
        assert!(files.reload_if_changed().unwrap().is_none(), "unchanged");

        // Certificate renewed, key not yet written: refused, nothing adopted.
        std::fs::write(&svid_p, &svid2).unwrap();
        assert!(files.reload_if_changed().is_err());
        // Both written: the renewed pair is returned.
        std::fs::write(&key_p, &key2).unwrap();
        let (_, svid) = files.reload_if_changed().unwrap().expect("renewed");
        assert_eq!(svid.as_deref(), Some(svid2.as_str()));
        std::fs::remove_dir_all(&dir).ok();
    }
}
