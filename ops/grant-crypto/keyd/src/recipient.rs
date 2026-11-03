//! The member→recipient-key provider — the audience's *key* half (the F-267 gap).
//!
//! [`crate::directory::DataRolesClient`] resolves member *identities*; wrapping the
//! DEK needs each member's **recipient public key**. The portal serves no such
//! key: `GET /api/access/data-roles` returns ids/producers only, and no
//! endpoint serves a member's SVID leaf key. So the source is a seam.
//!
//! [`RecipientKeys`] is that seam. [`CertDirRecipientKeys`] is the **launch
//! workaround**: an operator-provisioned directory of per-member PEM files,
//! the E0 `gce` model promoted to a daemon config path. It reads `<dir>/<id>.pem`
//! and extracts a **P-256** public key — matching `envelope.RecipientFromCert`'s
//! EC-only rule (SPIRE issues ECDSA P-256 SVIDs; a non-EC leaf is rejected by the
//! parse). The durable source (the member-key directory carrying each member's
//! SVID EC key alongside its `v2` hybrid KeyPackage) implements the same trait.
//!
//! Each PEM may be either a **certificate** (the SVID leaf — the P-256 key is taken
//! from its SubjectPublicKeyInfo) or a **raw public key** (the form the durable
//! directory serves). Both resolve to the same `p256::PublicKey`.

use std::path::{Path, PathBuf};

use p256::pkcs8::DecodePublicKey;
use p256::PublicKey;
use x509_cert::der::{DecodePem, Encode};
use x509_cert::Certificate;

use grant_crypto::service::ServiceError;

/// Resolves a member id to its recipient public key. A failure is fail-closed —
/// the member is dropped from the rotation rather than wrapped to a wrong key.
pub trait RecipientKeys {
    fn key(&self, member_id: &str) -> Result<PublicKey, ServiceError>;
}

/// So the daemon can hold whichever source it selected at startup (validated SVID
/// vs the cert-dir stopgap) behind one type.
impl RecipientKeys for Box<dyn RecipientKeys> {
    fn key(&self, member_id: &str) -> Result<PublicKey, ServiceError> {
        (**self).key(member_id)
    }
}

/// Resolve `<dir>/<member_id>.pem`, refusing any member id that could escape `dir`
/// (traversal, absolute, `.`/`..`, OS-separator or NUL in a component). Shared by
/// the cert-dir key sources.
/// The SPIFFE id a member id stands for.
///
/// A bare member id is ours: `weather/a` means `spiffe://<our domain>/weather/a`.
/// A member id written as a FULL SPIFFE URI names an identity in another trust
/// domain and is used verbatim — that is how a federated partner is put in an
/// audience (ADR 1169 decision 2). Writing the whole id is deliberate: a
/// foreign member should not be distinguishable from a local one only by a
/// config field somewhere else in the file.
pub(crate) fn expected_spiffe_id(local_trust_domain: &str, member_id: &str) -> String {
    if member_id.starts_with("spiffe://") {
        member_id.to_string()
    } else {
        format!("spiffe://{local_trust_domain}/{member_id}")
    }
}

/// Where a member's PEM lives under the certificate directory.
///
/// A local member is `<dir>/<member-id>.pem`. A federated member, whose id is a
/// full SPIFFE URI, is `<dir>/federated/<trust-domain>/<path>.pem` — the URI
/// itself cannot be a path (`spiffe://` contains an empty component and is
/// refused below), and putting foreign identities under their own directory
/// makes "whose certificates are these" answerable with `ls`.
pub(crate) fn member_pem_path(dir: &Path, member_id: &str) -> Result<PathBuf, ServiceError> {
    if member_id.is_empty() {
        return Err(ServiceError::Crypto("recipient: empty member id".into()));
    }
    if let Some(rest) = member_id.strip_prefix("spiffe://") {
        let (domain, path) = rest.split_once('/').ok_or_else(|| {
            ServiceError::Crypto(format!(
                "recipient: federated member id {member_id:?} has no path after the trust domain"
            ))
        })?;
        if domain.is_empty() || path.is_empty() {
            return Err(ServiceError::Crypto(format!(
                "recipient: federated member id {member_id:?} is malformed"
            )));
        }
        // Re-enter the safe walk below with the components we extracted, so a
        // federated id gets exactly the same traversal guards as a local one.
        return member_pem_path(&dir.join("federated"), &format!("{domain}/{path}"));
    }
    let mut path = dir.to_path_buf();
    for component in member_id.split('/') {
        if component.is_empty()
            || component == "."
            || component == ".."
            || component.contains('\\')
            || component.contains('\0')
        {
            return Err(ServiceError::Crypto(format!(
                "recipient: unsafe member id {member_id:?}"
            )));
        }
        path.push(component);
    }
    Ok(path.with_extension("pem"))
}

/// Tries each validated source in order; the first success wins. For a fleet
/// whose members split across identity planes — SPIRE workloads (SVID source)
/// and portal-minted orgs/humans/machines (portal-CA source). Still fail-closed:
/// a member resolves only if some *validated* source vouches for it, and the
/// error carries every source's refusal so a mis-provisioned member is
/// diagnosable.
pub struct ChainedRecipientKeys {
    sources: Vec<Box<dyn RecipientKeys>>,
}

impl ChainedRecipientKeys {
    pub fn new(sources: Vec<Box<dyn RecipientKeys>>) -> Self {
        Self { sources }
    }
}

impl RecipientKeys for ChainedRecipientKeys {
    fn key(&self, member_id: &str) -> Result<PublicKey, ServiceError> {
        let mut refusals = Vec::new();
        for source in &self.sources {
            match source.key(member_id) {
                Ok(k) => return Ok(k),
                Err(e) => refusals.push(format!("{e:?}")),
            }
        }
        Err(ServiceError::Crypto(format!(
            "recipient {member_id:?}: no validated source resolved it: [{}]",
            refusals.join("; ")
        )))
    }
}

/// Recipient keys from an operator-provisioned directory of `<member-id>.pem`
/// files. A member id may be hierarchical (`coalition-air/a`)
/// and maps to nested dirs; path traversal is refused.
pub struct CertDirRecipientKeys {
    dir: PathBuf,
}

impl CertDirRecipientKeys {
    pub fn new(dir: impl Into<PathBuf>) -> Self {
        Self { dir: dir.into() }
    }

    /// Resolve `<dir>/<member_id>.pem`, refusing anything that could escape `dir`.
    fn path_for(&self, member_id: &str) -> Result<PathBuf, ServiceError> {
        member_pem_path(&self.dir, member_id)
    }
}

/// Parse a P-256 public key from a PEM that is either a certificate (take its
/// SubjectPublicKeyInfo) or a raw public key. Non-P-256 keys are rejected here —
/// the EC-only guarantee.
fn p256_from_pem(pem: &str, whence: &Path) -> Result<PublicKey, ServiceError> {
    let fail = |m: String| ServiceError::Crypto(format!("recipient {}: {m}", whence.display()));
    if pem.contains("BEGIN CERTIFICATE") {
        let cert =
            Certificate::from_pem(pem.as_bytes()).map_err(|e| fail(format!("parse cert: {e}")))?;
        let spki_der = cert
            .tbs_certificate
            .subject_public_key_info
            .to_der()
            .map_err(|e| fail(format!("encode spki: {e}")))?;
        PublicKey::from_public_key_der(&spki_der)
            .map_err(|_| fail("leaf key is not P-256 (EC-only)".into()))
    } else {
        PublicKey::from_public_key_pem(pem)
            .map_err(|_| fail("not a P-256 public-key PEM (EC-only)".into()))
    }
}

impl RecipientKeys for CertDirRecipientKeys {
    fn key(&self, member_id: &str) -> Result<PublicKey, ServiceError> {
        let path = self.path_for(member_id)?;
        let pem = std::fs::read_to_string(&path).map_err(|e| {
            ServiceError::Crypto(format!("recipient {}: read: {e}", path.display()))
        })?;
        p256_from_pem(&pem, &path)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use p256::elliptic_curve::Generate;
    use p256::pkcs8::EncodePublicKey;
    use p256::SecretKey;

    fn tmpdir() -> PathBuf {
        // A unique dir under the OS temp root, seeded from a fresh keypair's bytes
        // (Date/rand are constrained in some contexts; a keypair is always fresh).
        let seed = hex::encode(&SecretKey::generate().to_bytes()[..8]);
        let dir = std::env::temp_dir().join(format!("keyd-recip-{seed}"));
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn resolves_a_raw_public_key_pem_and_it_wraps() {
        let dir = tmpdir();
        let secret = SecretKey::generate();
        let pem = secret
            .public_key()
            .to_public_key_pem(Default::default())
            .unwrap();
        std::fs::write(dir.join("member-a.pem"), pem).unwrap();

        let provider = CertDirRecipientKeys::new(&dir);
        let key = provider.key("member-a").expect("resolve");
        assert_eq!(key, secret.public_key());

        // The resolved key is a usable recipient: wrap a DEK to it, unwrap with the
        // secret, get the same DEK back — end-to-end through the crypto core.
        let dek = grant_crypto::new_dek();
        let wrapped = grant_crypto::wrap_dek(&dek, &key).unwrap();
        assert_eq!(grant_crypto::unwrap_dek(&wrapped, &secret).unwrap(), dek);
    }

    #[test]
    fn resolves_a_p256_certificate_pem() {
        let dir = tmpdir();
        let ck = rcgen::generate_simple_self_signed(vec!["member-b".to_string()]).unwrap();
        std::fs::write(dir.join("member-b.pem"), ck.cert.pem()).unwrap();

        let provider = CertDirRecipientKeys::new(&dir);
        let key = provider.key("member-b").expect("resolve cert");

        // The key extracted from the cert's SPKI equals the cert's own public key.
        let expected = PublicKey::from_public_key_der(ck.key_pair.public_key_der().as_slice())
            .expect("rcgen key is P-256");
        assert_eq!(key, expected);
    }

    #[test]
    fn hierarchical_ids_nest_and_traversal_is_refused() {
        let dir = tmpdir();
        let secret = SecretKey::generate();
        let pem = secret
            .public_key()
            .to_public_key_pem(Default::default())
            .unwrap();
        std::fs::create_dir_all(dir.join("coalition-air")).unwrap();
        std::fs::write(dir.join("coalition-air").join("a.pem"), pem).unwrap();

        let provider = CertDirRecipientKeys::new(&dir);
        assert_eq!(
            provider.key("coalition-air/a").unwrap(),
            secret.public_key()
        );

        for bad in ["../secret", "a/../../etc/passwd", "", "a/..", "/abs"] {
            assert!(provider.key(bad).is_err(), "must refuse unsafe id {bad:?}");
        }
    }

    #[test]
    fn missing_and_non_p256_fail_closed() {
        let dir = tmpdir();
        let provider = CertDirRecipientKeys::new(&dir);
        assert!(provider.key("nobody").is_err(), "missing file fails");

        std::fs::write(
            dir.join("garbage.pem"),
            "-----BEGIN PUBLIC KEY-----\nnope\n-----END PUBLIC KEY-----\n",
        )
        .unwrap();
        assert!(provider.key("garbage").is_err(), "junk PEM fails");
    }
}
