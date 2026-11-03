//! E1 end-to-end — the daemon stack over a live router.
//!
//! Drives the whole launch path the binary runs: an operator-declared audience
//! (`ConfiguredAudience`) whose keys come from a cert directory
//! (`CertDirRecipientKeys`), through the change-detecting `Daemon` and the native
//! `ZenohBundlePublisher`, over a real loopback router link — and confirms a
//! declared member opens the published bundle while an outsider cannot. The
//! difference from `transport.rs` (which drives the `KeyService` directly) is that
//! this exercises the config-shaped resolver + key-source layers the daemon adds.

use std::collections::BTreeMap;
use std::sync::mpsc;
use std::time::Duration;

use grant_crypto::service::KeyService;
use grant_crypto::{unwrap_dek, verify_bundle, KeyBundle};
use grant_keyd::daemon::{Daemon, SweepOutcome};
use grant_keyd::publisher::ZenohBundlePublisher;
use grant_keyd::recipient::CertDirRecipientKeys;
use grant_keyd::resolver::ConfiguredAudience;
use grant_keyd::svid::{SvidRecipientKeys, SvidValidator, TrustBundle};
use p256::ecdsa::SigningKey;
use p256::elliptic_curve::Generate;
use p256::pkcs8::{DecodePrivateKey, EncodePublicKey};
use p256::SecretKey;
use zenoh::{Config, Wait};

fn peer_listen(port: u16) -> Config {
    let mut c = Config::default();
    c.insert_json5("mode", r#""peer""#).unwrap();
    c.insert_json5("listen/endpoints", &format!(r#"["tcp/127.0.0.1:{port}"]"#))
        .unwrap();
    c.insert_json5("scouting/multicast/enabled", "false")
        .unwrap();
    c
}
fn peer_connect(port: u16) -> Config {
    let mut c = Config::default();
    c.insert_json5("mode", r#""peer""#).unwrap();
    c.insert_json5("connect/endpoints", &format!(r#"["tcp/127.0.0.1:{port}"]"#))
        .unwrap();
    c.insert_json5("listen/endpoints", "[]").unwrap();
    c.insert_json5("scouting/multicast/enabled", "false")
        .unwrap();
    c
}

#[test]
fn daemon_publishes_a_bundle_the_declared_member_opens() {
    let port: u16 = 47449;

    // Consumer session subscribed to the key namespace.
    let consumer = zenoh::open(peer_listen(port))
        .wait()
        .expect("open consumer");
    let (tx, rx) = mpsc::channel::<(String, Vec<u8>)>();
    let _sub = consumer
        .declare_subscriber("keys/**")
        .callback(move |sample| {
            let key = sample.key_expr().as_str().to_string();
            let payload = sample.payload().to_bytes().to_vec();
            let _ = tx.send((key, payload));
        })
        .wait()
        .expect("subscribe");

    // A cert directory: the declared member `coalition-air/a` (secret retained), and
    // an outsider that is NOT declared.
    let seed = hex::encode(&SecretKey::generate().to_bytes()[..8]);
    let dir = std::env::temp_dir().join(format!("keyd-e2e-{seed}"));
    std::fs::create_dir_all(dir.join("coalition-air")).unwrap();
    let a_secret = SecretKey::generate();
    std::fs::write(
        dir.join("coalition-air").join("a.pem"),
        a_secret
            .public_key()
            .to_public_key_pem(Default::default())
            .unwrap(),
    )
    .unwrap();
    let outsider = SecretKey::generate();

    // The operator-declared audience + the daemon.
    let mut declared = BTreeMap::new();
    declared.insert(
        "coalition-air".to_string(),
        vec!["coalition-air/a".to_string()],
    );
    let resolver = ConfiguredAudience::new(declared, CertDirRecipientKeys::new(&dir));

    let publisher = ZenohBundlePublisher::connect(peer_connect(port)).expect("connect publisher");
    std::thread::sleep(Duration::from_millis(1500));

    let svc = KeyService::new(SigningKey::generate(), "zagros-key-service");
    let mut daemon = Daemon::new(svc, resolver, publisher);
    let sets = vec!["coalition-air".to_string()];

    // First sweep rotates to gen 1 and publishes.
    let out = daemon.sweep(&sets);
    assert!(
        matches!(
            out[0],
            SweepOutcome::Rotated {
                generation: 1,
                members: 1,
                ..
            }
        ),
        "unexpected: {:?}",
        out
    );

    // The bundle arrives, is authentic, and the declared member opens it.
    let (topic, wire) = rx
        .recv_timeout(Duration::from_secs(5))
        .expect("bundle arrives over the router");
    assert_eq!(topic, "keys/coalition-air/1");
    let bundle: KeyBundle = serde_json::from_slice(&wire).expect("parse bundle");
    verify_bundle(&bundle, daemon.verifying_key()).expect("key-service signature");
    unwrap_dek(&bundle.members["coalition-air/a"], &a_secret).expect("declared member opens");
    // The outsider was never declared, and its key cannot unwrap a member's slot.
    assert!(
        unwrap_dek(&bundle.members["coalition-air/a"], &outsider).is_err(),
        "outsider must not open the bundle"
    );

    // A second sweep with the same declared audience does not rotate again.
    assert!(matches!(
        daemon.sweep(&sets)[0],
        SweepOutcome::Unchanged { .. }
    ));

    let _ = consumer.close().wait();
}

// The durable path: keys come from member SVIDs VALIDATED against the SPIRE root,
// not raw pubkey PEMs. Proves SvidRecipientKeys composes through the daemon over a
// live router.
#[test]
fn daemon_publishes_with_validated_svids() {
    use rcgen::{
        BasicConstraints, CertificateParams, ExtendedKeyUsagePurpose, IsCa, KeyPair, SanType,
    };
    let port: u16 = 47450;
    let domain = "zagros.zagros.example";

    // A CA standing in for the SPIRE root, and its bundle PEM.
    let ca_key = KeyPair::generate().unwrap();
    let mut ca_p = CertificateParams::new(vec![]).unwrap();
    ca_p.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
    let ca = ca_p.self_signed(&ca_key).unwrap();
    let bundle = TrustBundle::from_pem(&ca.pem()).unwrap();

    // The member's SVID (spiffe://<domain>/coalition-air/a), written to the cert dir;
    // retain its private key to open the bundle.
    let leaf_key = KeyPair::generate().unwrap();
    let member_secret = SecretKey::from_pkcs8_der(leaf_key.serialized_der()).unwrap();
    let mut leaf_p = CertificateParams::new(vec![]).unwrap();
    leaf_p.subject_alt_names = vec![SanType::URI(
        format!("spiffe://{domain}/coalition-air/a")
            .try_into()
            .unwrap(),
    )];
    leaf_p.extended_key_usages = vec![
        ExtendedKeyUsagePurpose::ClientAuth,
        ExtendedKeyUsagePurpose::ServerAuth,
    ];
    let leaf_pem = leaf_p.signed_by(&leaf_key, &ca, &ca_key).unwrap().pem();

    let seed = hex::encode(&SecretKey::generate().to_bytes()[..8]);
    let dir = std::env::temp_dir().join(format!("keyd-e2e-svid-{seed}"));
    std::fs::create_dir_all(dir.join("coalition-air")).unwrap();
    std::fs::write(dir.join("coalition-air").join("a.pem"), leaf_pem).unwrap();

    // Consumer + subscriber.
    let consumer = zenoh::open(peer_listen(port))
        .wait()
        .expect("open consumer");
    let (tx, rx) = mpsc::channel::<(String, Vec<u8>)>();
    let _sub = consumer
        .declare_subscriber("keys/**")
        .callback(move |s| {
            let _ = tx.send((
                s.key_expr().as_str().to_string(),
                s.payload().to_bytes().to_vec(),
            ));
        })
        .wait()
        .expect("subscribe");

    // The daemon over the validated-SVID source.
    let validator = SvidValidator::new(domain, bundle);
    let mut declared = std::collections::BTreeMap::new();
    declared.insert(
        "coalition-air".to_string(),
        vec!["coalition-air/a".to_string()],
    );
    let resolver = ConfiguredAudience::new(declared, SvidRecipientKeys::new(&dir, validator));
    let publisher = ZenohBundlePublisher::connect(peer_connect(port)).expect("connect");
    std::thread::sleep(Duration::from_millis(1500));
    let svc = KeyService::new(SigningKey::generate(), "zagros-key-service");
    let mut daemon = Daemon::new(svc, resolver, publisher);

    let out = daemon.sweep(&["coalition-air".to_string()]);
    assert!(
        matches!(
            out[0],
            SweepOutcome::Rotated {
                generation: 1,
                members: 1,
                ..
            }
        ),
        "unexpected: {:?}",
        out
    );

    let (topic, wire) = rx
        .recv_timeout(Duration::from_secs(5))
        .expect("bundle arrives");
    assert_eq!(topic, "keys/coalition-air/1");
    let b: KeyBundle = serde_json::from_slice(&wire).unwrap();
    verify_bundle(&b, daemon.verifying_key()).expect("authentic");
    unwrap_dek(&b.members["coalition-air/a"], &member_secret).expect("validated member opens");

    let _ = consumer.close().wait();
}
