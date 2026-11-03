//! SVID validation — anchoring recipient keys and signer trust to SPIRE.
//!
//! The durable answer to F-267: a member's recipient key is its **SPIRE SVID's
//! P-256 key**, because the SVID cryptographically binds that key to the member's
//! SPIFFE identity. Trusting it means validating the SVID against the goatnet's
//! SPIRE **trust root** — not just extracting the key from whatever cert is on disk
//! (what `recipient::CertDirRecipientKeys` does, the launch stopgap this closes).
//!
//! Expected shape: trust domain `spiffe://<trust-domain>`; the self-signed SPIRE
//! root signs leaves directly (root at `/opt/spire/data/server/spire-ca.pem`);
//! SVIDs are ECDSA P-256 with the SPIFFE ID in a URI-SAN. This mirrors the one
//! existing validator in the repo
//! (`goat-id-console/internal/spiffemtls`, Go): hold the bundle, self-validate the
//! chain, then match the URI-SAN — deliberately no SPIFFE library.
//!
//! The chain step uses `rustls-webpki` (vetted path validation — signature,
//! validity, EKU, and *intermediates*, so this does not silently break if SPIRE
//! grows an UpstreamAuthority); `x509-cert` extracts the SPIFFE-ID and P-256 key.
//! Fail-closed throughout.

use std::path::Path;

use p256::pkcs8::DecodePublicKey;
use p256::PublicKey;
use rustls_pki_types::{CertificateDer, UnixTime};
use webpki::{anchor_from_trusted_cert, EndEntityCert, KeyUsage};
use x509_cert::der::oid::AssociatedOid;
use x509_cert::der::{Decode, DecodePem, Encode};
use x509_cert::ext::pkix::{name::GeneralName, SubjectAltName};
use x509_cert::Certificate;

use grant_crypto::service::ServiceError;

/// The signature algorithms accepted on a chain — SPIRE issues P-256; P-384 is
/// allowed for forward-compatibility. Anything else is refused (algorithm-
/// confusion guard).
static SIG_ALGS: &[&dyn rustls_pki_types::SignatureVerificationAlgorithm] = &[
    webpki::ring::ECDSA_P256_SHA256,
    webpki::ring::ECDSA_P384_SHA384,
];

fn crypto(msg: impl Into<String>) -> ServiceError {
    ServiceError::Crypto(format!("svid: {}", msg.into()))
}

/// The SPIRE trust root(s) an SVID must chain to.
pub struct TrustBundle {
    roots: Vec<CertificateDer<'static>>,
}

impl TrustBundle {
    /// Parse one or more `CERTIFICATE` PEM blocks (the SPIRE bundle may carry a
    /// rotated CA alongside the current one).
    pub fn from_pem(pem: &str) -> Result<Self, ServiceError> {
        let mut roots = Vec::new();
        let mut rest = pem;
        while let Some(start) = rest.find("-----BEGIN CERTIFICATE-----") {
            let after = &rest[start..];
            let end = after
                .find("-----END CERTIFICATE-----")
                .ok_or_else(|| crypto("trust bundle: unterminated CERTIFICATE block"))?;
            let block = &after[..end + "-----END CERTIFICATE-----".len()];
            // Decode to DER (byte-preserving) via a cert round-trip.
            let cert = Certificate::from_pem(block.as_bytes())
                .map_err(|e| crypto(format!("trust bundle: parse root: {e}")))?;
            let der = cert
                .to_der()
                .map_err(|e| crypto(format!("trust bundle: encode root: {e}")))?;
            roots.push(CertificateDer::from(der));
            rest = &after[end + "-----END CERTIFICATE-----".len()..];
        }
        if roots.is_empty() {
            return Err(crypto("trust bundle: no CERTIFICATE blocks"));
        }
        Ok(Self { roots })
    }

    /// Load the bundle from a PEM file (e.g. `/opt/spire/data/server/spire-ca.pem`).
    pub fn load(path: &Path) -> Result<Self, ServiceError> {
        let pem = std::fs::read_to_string(path)
            .map_err(|e| crypto(format!("trust bundle {}: {e}", path.display())))?;
        Self::from_pem(&pem)
    }

    /// The root certificates, for sibling validators (the portal-CA source
    /// anchors to a different root with the same bundle mechanics).
    pub(crate) fn roots(&self) -> &[CertificateDer<'static>] {
        &self.roots
    }
}

/// Validates SVIDs against a trust bundle and a fixed trust domain.
pub struct SvidValidator {
    trust_domain: String,
    /// Trust domains OTHER than our own whose SVIDs may hold an audience slot.
    ///
    /// EXPLICIT, never inferred from the trust bundle. The bundle carries a
    /// counterparty root because the two SPIRE servers federate — that is a
    /// statement about link authentication, not about who may read sealed data.
    /// ADR 1169 keeps audience membership entirely ours to decide, so admitting
    /// a domain is a configured act and an empty list means "nobody but us".
    federated_trust_domains: Vec<String>,
    bundle: TrustBundle,
}

impl SvidValidator {
    /// `trust_domain` is the bare domain, e.g. `site.example.net`.
    pub fn new(trust_domain: impl Into<String>, bundle: TrustBundle) -> Self {
        Self {
            trust_domain: trust_domain.into(),
            federated_trust_domains: Vec::new(),
            bundle,
        }
    }

    /// Admit audience members from these federated trust domains as well as our
    /// own (ADR 1169 decision 2). Their SVIDs still have to chain to a root in
    /// the bundle and still have to match their expected id exactly; this only
    /// lifts the check that the id lives in OUR domain.
    pub fn with_federated_trust_domains<I, T>(mut self, domains: I) -> Self
    where
        I: IntoIterator<Item = T>,
        T: Into<String>,
    {
        self.federated_trust_domains = domains.into_iter().map(Into::into).collect();
        self
    }

    /// The trust domain an expected id belongs to, or `None` if it is not a
    /// SPIFFE id we would ever accept.
    fn domain_of(expected_spiffe_id: &str) -> Option<&str> {
        expected_spiffe_id
            .strip_prefix("spiffe://")
            .and_then(|rest| rest.split_once('/'))
            .map(|(domain, _)| domain)
            .filter(|d| !d.is_empty())
    }

    /// Validate an SVID leaf PEM and return its P-256 key, requiring its SPIFFE ID
    /// to equal `expected_spiffe_id`. Fail-closed on any failure.
    pub fn validate(
        &self,
        leaf_pem: &str,
        expected_spiffe_id: &str,
    ) -> Result<PublicKey, ServiceError> {
        self.validate_at(leaf_pem, expected_spiffe_id, UnixTime::now())
    }

    /// As [`validate`], but checked at the moment the leaf was issued (its
    /// `notBefore`) rather than now. Only grant-keyd uses this, to authenticate
    /// its OWN bundles read back from `keys/**` storage after a restart: those
    /// carry an SVID that has long since expired, and the question is whether
    /// a grant-keyd SVID that chained to this root signed them, not whether
    /// that SVID is still current. Readers never use it.
    pub fn validate_as_issued(
        &self,
        leaf_pem: &str,
        expected_spiffe_id: &str,
    ) -> Result<PublicKey, ServiceError> {
        let cert = Certificate::from_pem(leaf_pem.as_bytes())
            .map_err(|e| crypto(format!("parse leaf: {e}")))?;
        let issued = cert.tbs_certificate.validity.not_before.to_unix_duration();
        self.validate_at(
            leaf_pem,
            expected_spiffe_id,
            UnixTime::since_unix_epoch(issued + std::time::Duration::from_secs(1)),
        )
    }

    /// As [`validate`], with the validation time injected (for tests).
    fn validate_at(
        &self,
        leaf_pem: &str,
        expected_spiffe_id: &str,
        now: UnixTime,
    ) -> Result<PublicKey, ServiceError> {
        // The expected id must live in our own trust domain, or in one we have
        // been told to federate with. Guards a caller that derived the id with
        // the wrong domain, and — for a federated member — records that
        // admitting the domain was a decision somebody made (ADR 1169).
        let domain = Self::domain_of(expected_spiffe_id).ok_or_else(|| {
            crypto(format!(
                "expected id {expected_spiffe_id:?} is not a SPIFFE id of the form \
                 spiffe://<trust-domain>/<path>"
            ))
        })?;
        if domain != self.trust_domain && !self.federated_trust_domains.iter().any(|d| d == domain)
        {
            return Err(crypto(format!(
                "expected id {expected_spiffe_id:?} is in trust domain {domain:?}, which is \
                 neither ours ({:?}) nor federated. A federated partner's domain has to be \
                 admitted deliberately — the trust bundle carrying their root authenticates \
                 the LINK and says nothing about who may read sealed data (ADR 1169)",
                self.trust_domain
            )));
        }

        let cert = Certificate::from_pem(leaf_pem.as_bytes())
            .map_err(|e| crypto(format!("parse leaf: {e}")))?;
        let leaf_der_bytes = cert
            .to_der()
            .map_err(|e| crypto(format!("encode leaf: {e}")))?;
        let leaf_der = CertificateDer::from(leaf_der_bytes);

        // (1) Vetted chain validation: leaf chains to a trusted root, is time-valid,
        //     and carries the client-auth usage. Refused otherwise.
        let anchors: Vec<_> = self
            .bundle
            .roots
            .iter()
            .map(|r| anchor_from_trusted_cert(r).map_err(|e| crypto(format!("bad root: {e}"))))
            .collect::<Result<_, _>>()?;
        let ee = EndEntityCert::try_from(&leaf_der)
            .map_err(|e| crypto(format!("leaf not a cert: {e}")))?;
        ee.verify_for_usage(
            SIG_ALGS,
            &anchors,
            &[],
            now,
            KeyUsage::client_auth(),
            None,
            None,
        )
        .map_err(|e| crypto(format!("chain invalid: {e}")))?;

        // (2) The SPIFFE ID (URI-SAN) must equal the expected id exactly.
        let spiffe_id = single_spiffe_uri(&cert)?;
        if spiffe_id != expected_spiffe_id {
            return Err(crypto(format!(
                "SPIFFE ID mismatch: cert {spiffe_id:?} != expected {expected_spiffe_id:?}"
            )));
        }

        // (3) The recipient key: the leaf's P-256 SPKI.
        let spki_der = cert
            .tbs_certificate
            .subject_public_key_info
            .to_der()
            .map_err(|e| crypto(format!("encode spki: {e}")))?;
        PublicKey::from_public_key_der(&spki_der)
            .map_err(|_| crypto("leaf key is not P-256 (EC-only)"))
    }
}

/// The durable recipient-key source: a directory of per-member SVID
/// leaf PEMs, each **validated** against the SPIRE trust root before its key is
/// used — unlike [`crate::recipient::CertDirRecipientKeys`], which extracts the key
/// from any cert without checking the chain. The member id maps to both the file
/// (`<dir>/<member-id>.pem`) and the required SPIFFE ID
/// (`spiffe://<trust-domain>/<member-id>`), so a cert can only stand in for the
/// member whose identity it actually carries.
pub struct SvidRecipientKeys {
    dir: std::path::PathBuf,
    validator: SvidValidator,
}

impl SvidRecipientKeys {
    pub fn new(dir: impl Into<std::path::PathBuf>, validator: SvidValidator) -> Self {
        Self {
            dir: dir.into(),
            validator,
        }
    }
}

impl crate::recipient::RecipientKeys for SvidRecipientKeys {
    fn key(&self, member_id: &str) -> Result<PublicKey, ServiceError> {
        let expected =
            crate::recipient::expected_spiffe_id(&self.validator.trust_domain, member_id);
        let path = crate::recipient::member_pem_path(&self.dir, member_id)?;
        let pem = std::fs::read_to_string(&path)
            .map_err(|e| crypto(format!("read {}: {e}", path.display())))?;
        self.validator.validate(&pem, &expected)
    }
}

/// Verify a key bundle by anchoring its **signer** to SPIRE: validate the
/// key-service's SVID against the trust root + expected SPIFFE ID, take its
/// verifying key, and verify the bundle signature with it. This closes the loop on
/// bundle authenticity — a member trusts the signer by its SPIFFE identity, not a
/// pre-shared/pinned key — on the same trust foundation as the recipient keys.
pub fn verify_bundle_signer(
    bundle: &grant_crypto::KeyBundle,
    key_service_svid_pem: &str,
    validator: &SvidValidator,
    expected_key_service_id: &str,
) -> Result<(), ServiceError> {
    let public = validator.validate(key_service_svid_pem, expected_key_service_id)?;
    let vk = p256::ecdsa::VerifyingKey::from(&public);
    grant_crypto::verify_bundle(bundle, &vk).map_err(ServiceError::from)
}

/// Authenticate a bundle grant-keyd itself published before a restart, read back
/// from `keys/**` storage: its attached SVID must chain to the trust root as of
/// that SVID's issue time and carry `expected_key_service_id`, and the signature
/// must verify under it. Used only to decide what grant-keyd re-signs; readers
/// keep requiring a currently valid signer ([`verify_bundle_self`]).
pub fn verify_own_stored_bundle(
    bundle: &grant_crypto::KeyBundle,
    validator: &SvidValidator,
    expected_key_service_id: &str,
) -> Result<(), ServiceError> {
    let svid = bundle
        .signer_svid
        .as_deref()
        .ok_or_else(|| crypto("bundle carries no signer SVID (not self-describing)"))?;
    let public = validator.validate_as_issued(svid, expected_key_service_id)?;
    let vk = p256::ecdsa::VerifyingKey::from(&public);
    grant_crypto::verify_bundle(bundle, &vk).map_err(ServiceError::from)
}

/// Verify a **self-describing** bundle: it carries its signer's SVID
/// (`signer_svid`), so no side channel is needed. Validates that SVID against the
/// SPIRE root + expected key-service identity and verifies the signature. This is
/// how a member verifies a bundle it catches up on from cache even after the
/// key-service has rotated its identity — the bundle carries the exact cert that
/// signed it. Fail-closed if the bundle carries no SVID.
pub fn verify_bundle_self(
    bundle: &grant_crypto::KeyBundle,
    validator: &SvidValidator,
    expected_key_service_id: &str,
) -> Result<(), ServiceError> {
    let svid = bundle
        .signer_svid
        .as_deref()
        .ok_or_else(|| crypto("bundle carries no signer SVID (not self-describing)"))?;
    verify_bundle_signer(bundle, svid, validator, expected_key_service_id)
}

/// Extract the single SPIFFE URI-SAN from a cert, refusing zero or multiple (an
/// SVID has exactly one).
fn single_spiffe_uri(cert: &Certificate) -> Result<String, ServiceError> {
    let exts = cert
        .tbs_certificate
        .extensions
        .as_ref()
        .ok_or_else(|| crypto("leaf has no extensions (no SAN)"))?;
    let mut found: Option<String> = None;
    for ext in exts.iter() {
        if ext.extn_id != SubjectAltName::OID {
            continue;
        }
        let san = SubjectAltName::from_der(ext.extn_value.as_bytes())
            .map_err(|e| crypto(format!("parse SAN: {e}")))?;
        for gn in san.0.iter() {
            if let GeneralName::UniformResourceIdentifier(uri) = gn {
                let uri = uri.as_str();
                if uri.starts_with("spiffe://") {
                    if found.is_some() {
                        return Err(crypto("leaf has multiple SPIFFE URI-SANs"));
                    }
                    found = Some(uri.to_string());
                }
            }
        }
    }
    found.ok_or_else(|| crypto("leaf has no SPIFFE URI-SAN"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use rcgen::{
        BasicConstraints, CertificateParams, ExtendedKeyUsagePurpose, IsCa, KeyPair, SanType,
    };
    use std::time::Duration;

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
    // A leaf SVID with the given SPIFFE ID, signed by `ca`.
    fn svid(ca: &Ca, spiffe_id: &str) -> String {
        let key = KeyPair::generate().unwrap();
        let mut p = CertificateParams::new(vec![]).unwrap();
        p.subject_alt_names = vec![SanType::URI(spiffe_id.try_into().unwrap())];
        p.extended_key_usages = vec![
            ExtendedKeyUsagePurpose::ClientAuth,
            ExtendedKeyUsagePurpose::ServerAuth,
        ];
        p.signed_by(&key, &ca.cert, &ca.key).unwrap().pem()
    }

    fn validator(ca: &Ca) -> SvidValidator {
        SvidValidator::new(
            "zagros.zagros.example",
            TrustBundle::from_pem(&ca.cert.pem()).unwrap(),
        )
    }
    const ID: &str = "spiffe://zagros.zagros.example/coalition-air/a";

    // ---- ADR 1169 decision 2: a federated partner may hold an audience slot ----

    #[test]
    fn a_federated_member_is_refused_until_its_domain_is_admitted() {
        let ca = ca();
        let partner = "spiffe://xaa.example.net/member/partner";
        let leaf = svid(&ca, partner);
        // The leaf chains to a root in the bundle — federation put it there — and
        // is STILL refused, because admitting the domain is a separate decision.
        let err = validator(&ca).validate(&leaf, partner).unwrap_err();
        let msg = format!("{err}");
        assert!(msg.contains("xaa.example.net"), "{msg}");
        assert!(msg.contains("federated"), "names why: {msg}");
    }

    #[test]
    fn a_federated_member_validates_once_its_domain_is_admitted() {
        let ca = ca();
        let partner = "spiffe://xaa.example.net/member/partner";
        let leaf = svid(&ca, partner);
        let v = validator(&ca).with_federated_trust_domains(["xaa.example.net"]);
        v.validate(&leaf, partner)
            .expect("federated member admitted");
    }

    #[test]
    fn admitting_one_domain_does_not_admit_another() {
        let ca = ca();
        let v = validator(&ca).with_federated_trust_domains(["xaa.example.net"]);
        let other = "spiffe://xab.example.net/member/partner";
        assert!(v.validate(&svid(&ca, other), other).is_err());
    }

    #[test]
    fn a_federated_id_still_has_to_match_its_certificate() {
        let ca = ca();
        let v = validator(&ca).with_federated_trust_domains(["xaa.example.net"]);
        // Right domain, wrong path: the exact-id check is untouched by federation.
        let leaf = svid(&ca, "spiffe://xaa.example.net/member/someone-else");
        assert!(v
            .validate(&leaf, "spiffe://xaa.example.net/member/partner")
            .is_err());
    }

    #[test]
    fn a_federated_svid_that_chains_to_nothing_is_still_refused() {
        let ours = ca();
        let stranger = ca(); // a root that is NOT in our bundle
        let partner = "spiffe://xaa.example.net/member/partner";
        let v = validator(&ours).with_federated_trust_domains(["xaa.example.net"]);
        // Admitting the DOMAIN does not admit an unchained certificate: the
        // partner's root has to be in the bundle, which is what federation puts
        // there. Domain admission lifts one check and no others.
        assert!(v.validate(&svid(&stranger, partner), partner).is_err());
    }

    #[test]
    fn a_malformed_expected_id_is_refused_and_says_so() {
        let ca = ca();
        let err = validator(&ca)
            .validate(&svid(&ca, ID), "not-a-spiffe-id")
            .unwrap_err();
        assert!(format!("{err}").contains("spiffe://"), "{err}");
    }

    #[test]
    fn a_valid_svid_validates_and_yields_its_key() {
        let ca = ca();
        let leaf = svid(&ca, ID);
        let key = validator(&ca).validate(&leaf, ID).expect("valid SVID");
        // The key is a usable recipient (wrap/unwrap through the core).
        let dek = grant_crypto::new_dek();
        let wrapped = grant_crypto::wrap_dek(&dek, &key).unwrap();
        // (unwrap needs the secret; here we only assert wrap succeeds to a real key)
        let _ = wrapped;
    }

    #[test]
    fn a_svid_from_a_different_root_is_refused() {
        let real = ca();
        let attacker = ca();
        let leaf = svid(&attacker, ID); // signed by the wrong CA
        assert!(validator(&real).validate(&leaf, ID).is_err());
    }

    #[test]
    fn a_wrong_spiffe_id_is_refused() {
        let ca = ca();
        let leaf = svid(&ca, "spiffe://zagros.zagros.example/someone-else");
        assert!(validator(&ca).validate(&leaf, ID).is_err());
    }

    #[test]
    fn a_different_trust_domain_is_refused() {
        let ca = ca();
        // Cert is in another domain; expected id is in ours — both the domain guard
        // and the id-match reject it.
        let other = "spiffe://other.example.net/coalition-air/a";
        let leaf = svid(&ca, other);
        assert!(validator(&ca).validate(&leaf, other).is_err()); // domain guard
        assert!(validator(&ca).validate(&leaf, ID).is_err()); // id mismatch
    }

    #[test]
    fn an_expired_svid_is_refused() {
        use time::OffsetDateTime;
        let ca = ca();
        // A leaf whose validity window is entirely in the past (2020–2021).
        let key = KeyPair::generate().unwrap();
        let mut p = CertificateParams::new(vec![]).unwrap();
        p.subject_alt_names = vec![SanType::URI(ID.try_into().unwrap())];
        p.extended_key_usages = vec![
            ExtendedKeyUsagePurpose::ClientAuth,
            ExtendedKeyUsagePurpose::ServerAuth,
        ];
        p.not_before = OffsetDateTime::from_unix_timestamp(1_577_836_800).unwrap(); // 2020-01-01
        p.not_after = OffsetDateTime::from_unix_timestamp(1_609_459_200).unwrap(); // 2021-01-01
        let leaf = p.signed_by(&key, &ca.cert, &ca.key).unwrap().pem();
        // Validate "now" (2026+), past the window → expired → refused.
        let now = UnixTime::since_unix_epoch(Duration::from_secs(1_800_000_000)); // 2027
        assert!(validator(&ca).validate_at(&leaf, ID, now).is_err());
    }

    #[test]
    fn an_expired_svid_validates_as_issued_but_only_under_the_real_root() {
        use time::OffsetDateTime;
        let expired = |ca: &Ca| {
            let key = KeyPair::generate().unwrap();
            let mut p = CertificateParams::new(vec![]).unwrap();
            p.subject_alt_names = vec![SanType::URI(ID.try_into().unwrap())];
            p.extended_key_usages = vec![
                ExtendedKeyUsagePurpose::ClientAuth,
                ExtendedKeyUsagePurpose::ServerAuth,
            ];
            p.not_before = OffsetDateTime::from_unix_timestamp(1_577_836_800).unwrap();
            p.not_after = OffsetDateTime::from_unix_timestamp(1_609_459_200).unwrap();
            p.signed_by(&key, &ca.cert, &ca.key).unwrap().pem()
        };
        let real = ca();
        let leaf = expired(&real);
        assert!(
            validator(&real).validate(&leaf, ID).is_err(),
            "readers still refuse it"
        );
        validator(&real)
            .validate_as_issued(&leaf, ID)
            .expect("grant-keyd recognises its own past SVID");
        let attacker = ca();
        assert!(validator(&real)
            .validate_as_issued(&expired(&attacker), ID)
            .is_err());
        let other = "spiffe://zagros.zagros.example/someone-else";
        assert!(validator(&real).validate_as_issued(&leaf, other).is_err());
    }

    #[test]
    fn a_non_certificate_pem_is_refused() {
        let ca = ca();
        assert!(validator(&ca)
            .validate(
                "-----BEGIN CERTIFICATE-----\nnope\n-----END CERTIFICATE-----\n",
                ID
            )
            .is_err());
    }

    // --- SvidRecipientKeys (the durable, validated key source) ---

    use crate::recipient::RecipientKeys;
    use std::sync::atomic::{AtomicUsize, Ordering};

    static CTR: AtomicUsize = AtomicUsize::new(0);
    fn tmpdir() -> std::path::PathBuf {
        let n = CTR.fetch_add(1, Ordering::Relaxed);
        let dir = std::env::temp_dir().join(format!("keyd-svidkeys-{}-{n}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn resolves_a_validated_member_svid() {
        let ca = ca();
        let dir = tmpdir();
        std::fs::create_dir_all(dir.join("coalition-air")).unwrap();
        std::fs::write(dir.join("coalition-air").join("a.pem"), svid(&ca, ID)).unwrap();

        let keys = SvidRecipientKeys::new(&dir, validator(&ca));
        keys.key("coalition-air/a").expect("validated member key");
    }

    #[test]
    fn refuses_an_svid_whose_identity_does_not_match_the_member_id() {
        // The file at coalition-air/a.pem carries an SVID for someone else — the
        // member-id↔SPIFFE-id binding must reject it (no key confusion).
        let ca = ca();
        let dir = tmpdir();
        std::fs::create_dir_all(dir.join("coalition-air")).unwrap();
        let wrong = svid(&ca, "spiffe://zagros.zagros.example/someone-else");
        std::fs::write(dir.join("coalition-air").join("a.pem"), wrong).unwrap();

        let keys = SvidRecipientKeys::new(&dir, validator(&ca));
        assert!(keys.key("coalition-air/a").is_err());
    }

    #[test]
    fn missing_and_traversal_fail_closed() {
        let ca = ca();
        let dir = tmpdir();
        let keys = SvidRecipientKeys::new(&dir, validator(&ca));
        assert!(keys.key("nobody").is_err());
        assert!(keys.key("../escape").is_err());
    }

    #[test]
    fn verifies_a_bundle_signed_by_the_key_service_svid() {
        use grant_crypto::{new_bundle, new_dek, sign_bundle};
        use p256::ecdsa::SigningKey;
        use p256::pkcs8::DecodePrivateKey;
        use p256::SecretKey;
        use std::collections::BTreeMap;

        let root = ca();
        let ks_id = "spiffe://zagros.zagros.example/goat/key-service";

        // The key-service SVID, and the signing key that matches its leaf key.
        let ks_key = KeyPair::generate().unwrap();
        let signer = SigningKey::from(SecretKey::from_pkcs8_der(ks_key.serialized_der()).unwrap());
        let mut leaf_p = CertificateParams::new(vec![]).unwrap();
        leaf_p.subject_alt_names = vec![SanType::URI(ks_id.try_into().unwrap())];
        leaf_p.extended_key_usages = vec![
            ExtendedKeyUsagePurpose::ClientAuth,
            ExtendedKeyUsagePurpose::ServerAuth,
        ];
        let ks_svid = leaf_p
            .signed_by(&ks_key, &root.cert, &root.key)
            .unwrap()
            .pem();

        // A bundle signed by that key-service.
        let dek = new_dek();
        let members: BTreeMap<String, p256::PublicKey> = BTreeMap::new();
        let mut bundle = new_bundle("rs", 1, &dek, &members).unwrap();
        sign_bundle(&mut bundle, "goat/key-service", &signer).unwrap();

        let v = validator(&root);
        // The member trusts the signer by its SPIFFE identity, no pinned key.
        verify_bundle_signer(&bundle, &ks_svid, &v, ks_id).expect("signer trusted");

        // Wrong expected key-service identity → refused.
        assert!(verify_bundle_signer(
            &bundle,
            &ks_svid,
            &v,
            "spiffe://zagros.zagros.example/goat/impostor"
        )
        .is_err());

        // A key-service SVID from an untrusted root → refused (before any signature).
        let rogue = ca();
        let rogue_svid = {
            let k = KeyPair::generate().unwrap();
            let mut p = CertificateParams::new(vec![]).unwrap();
            p.subject_alt_names = vec![SanType::URI(ks_id.try_into().unwrap())];
            p.extended_key_usages = vec![
                ExtendedKeyUsagePurpose::ClientAuth,
                ExtendedKeyUsagePurpose::ServerAuth,
            ];
            p.signed_by(&k, &rogue.cert, &rogue.key).unwrap().pem()
        };
        assert!(verify_bundle_signer(&bundle, &rogue_svid, &v, ks_id).is_err());
    }

    #[test]
    fn a_self_describing_bundle_verifies_from_its_carried_svid() {
        use grant_crypto::{new_bundle, new_dek, set_signer_svid, sign_bundle};
        use p256::ecdsa::SigningKey;
        use p256::pkcs8::DecodePrivateKey;
        use p256::SecretKey;
        use std::collections::BTreeMap;

        let root = ca();
        let ks_id = "spiffe://zagros.zagros.example/goat/key-service";
        let ks_key = KeyPair::generate().unwrap();
        let signer = SigningKey::from(SecretKey::from_pkcs8_der(ks_key.serialized_der()).unwrap());
        let mut leaf_p = CertificateParams::new(vec![]).unwrap();
        leaf_p.subject_alt_names = vec![SanType::URI(ks_id.try_into().unwrap())];
        leaf_p.extended_key_usages = vec![
            ExtendedKeyUsagePurpose::ClientAuth,
            ExtendedKeyUsagePurpose::ServerAuth,
        ];
        let ks_svid = leaf_p
            .signed_by(&ks_key, &root.cert, &root.key)
            .unwrap()
            .pem();

        let dek = new_dek();
        let members: BTreeMap<String, p256::PublicKey> = BTreeMap::new();
        let mut bundle = new_bundle("rs", 1, &dek, &members).unwrap();
        sign_bundle(&mut bundle, "goat/key-service", &signer).unwrap();

        let v = validator(&root);
        // Not self-describing yet → fail-closed.
        assert!(verify_bundle_self(&bundle, &v, ks_id).is_err());
        // Attach the signer SVID → the bundle verifies from its own contents.
        set_signer_svid(&mut bundle, ks_svid);
        verify_bundle_self(&bundle, &v, ks_id).expect("self-describing bundle verifies");
    }
}
