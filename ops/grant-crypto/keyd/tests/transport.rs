//! E1 on-fabric transport proof — the native-Rust production path.
//!
//! The Go+Python E0 proof (../transport-proof) showed the crypto composes with
//! a live Zenoh wire when the transport is Python. This proves the SAME thing on
//! the path E1 actually ships: a Rust `KeyService` mints + signs a bundle and its
//! `ZenohBundlePublisher` PUTs it over a real router link; a second session reads
//! it back and the machine-plane read cut holds — enforced by the crypto, not the
//! router.
//!
//! Two peer sessions over loopback TCP, multicast off (mirrors the Go harness).
//! Run: `cargo test -p grant-keyd --test transport` with a router-capable build.

use std::sync::mpsc;
use std::time::Duration;

use grant_crypto::service::{AudienceResolver, KeyService, Member, ServiceError};
use grant_crypto::{unwrap_dek, verify_bundle, KeyBundle};
use grant_keyd::publisher::ZenohBundlePublisher;
use p256::ecdsa::SigningKey;
use p256::elliptic_curve::Generate;
use p256::SecretKey;
use zenoh::{Config, Wait};

// A member plus its retained secret, so the test can attempt to open what the
// service wrapped. The `member` is what the resolver hands the service.
struct Recipient {
    secret: SecretKey,
    member: Member,
}
fn recipient(id: &str) -> Recipient {
    let secret = SecretKey::generate();
    let member = Member {
        id: id.into(),
        key: secret.public_key(),
    };
    Recipient { secret, member }
}

struct FixedAudience(Vec<Member>);
impl AudienceResolver for FixedAudience {
    fn resolve(&self, _rs: &str) -> Result<Vec<Member>, ServiceError> {
        Ok(self.0.clone())
    }
}

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
fn bundle_survives_a_live_router_and_the_read_cut_holds() {
    // A fixed loopback port in the ephemeral range; multicast is off so nothing
    // escapes the link.
    let port: u16 = 47447;

    // Consumer side: a session that subscribes to the key namespace and captures
    // the raw bytes exactly as they leave the router.
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
        .expect("declare subscriber");

    // Producer side: the key-service's own session + native publisher.
    let publisher =
        ZenohBundlePublisher::connect(peer_connect(port)).expect("connect publisher session");

    // Give the two peers a moment to establish the TCP link before publishing.
    std::thread::sleep(Duration::from_millis(1500));

    // Audience: two members in the release-set, plus an outsider that is NOT.
    let a = recipient("coalition-air/a");
    let b = recipient("coalition-air/b");
    let outsider = recipient("outsider");
    let resolver = FixedAudience(vec![a.member.clone(), b.member.clone()]);

    let mut svc = KeyService::new(SigningKey::generate(), "key-service");
    let rotation = svc
        .rotate("coalition-air", &resolver, &publisher)
        .expect("rotate + publish");
    assert_eq!(rotation.generation, 1);
    assert_eq!(rotation.members, 2);

    // (1) The bundle arrives over the live router on the expected key topic.
    let (topic, wire) = rx
        .recv_timeout(Duration::from_secs(5))
        .expect("bundle did not arrive over the router");
    assert_eq!(topic, "keys/coalition-air/1");

    // (2) It deserializes and is authenticated by the key-service signature. This
    //     is the integrity proof: the signature covers the canonical transcript, so
    //     a single corrupted byte anywhere in transit would fail verification here.
    let bundle: KeyBundle = serde_json::from_slice(&wire).expect("parse bundle off the wire");
    verify_bundle(&bundle, svc.verifying_key()).expect("bundle authenticity");

    // (3) The authorized members recover the DEK off the wire bytes; both get the
    //     same key (the AEAD-wrapped slots also survived intact).
    let dek_a = unwrap_dek(&bundle.members["coalition-air/a"], &a.secret).expect("member a opens");
    let dek_b = unwrap_dek(&bundle.members["coalition-air/b"], &b.secret).expect("member b opens");
    assert_eq!(dek_a, dek_b, "one DEK for the release-set generation");

    // (4) The outsider has no slot — the read cut, enforced by the crypto. The
    //     router delivered the identical bytes to the session; only members open.
    assert!(
        !bundle.members.contains_key("outsider"),
        "outsider must have no slot"
    );
    // And even handed a member's slot, the outsider's key cannot unwrap it.
    assert!(
        unwrap_dek(&bundle.members["coalition-air/a"], &outsider.secret).is_err(),
        "outsider key must not unwrap a member slot"
    );

    let _ = publisher.close();
    let _ = consumer.close().wait();
}
