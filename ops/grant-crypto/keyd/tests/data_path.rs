//! Piece A end-to-end over a live Zenoh wire — the native-Rust producer→consumer path.
//!
//! `tests/transport.rs` proved the key *bundle* survives a real router link. This proves
//! the *data* path on top of it: a key-service publishes a bundle whose audience is a
//! producer AND a grantee; the producer (a member) pulls its own DEK from that bundle,
//! seals a live message, and PUTs it on `data/<rs>/<topic>`; the grantee admits the bundle
//! and opens the received message; an OUTSIDER session receives the identical bytes and
//! cannot open them. This is the E0 harness result (`transport-proof/harness.py`), now on
//! the path E1 ships — native Rust, one process, over the wire.
//!
//! Two peer sessions over loopback TCP, multicast off. In-band carrier (the `Envelope` is
//! the message body). Run: `cargo test -p grant-keyd --test data_path`.

use std::sync::mpsc;
use std::time::Duration;

use grant_crypto::{new_bundle, new_dek, sign_bundle, verify_bundle};
use grant_keyd::datapath::{acquire_stream_key, decode_message, encode_message, Context, KeyStore};
use p256::ecdsa::SigningKey;
use p256::elliptic_curve::Generate;
use p256::{PublicKey, SecretKey};
use std::collections::BTreeMap;
use zenoh::{Config, Wait};

const RS: &str = "coalition-air";
const DATA_KEYEXPR: &str = "data/coalition-air/cop";
const PLAINTEXT: &[u8] = b"brigade COP snapshot, releasable to REL-EXAMPLE only";

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
fn data_stream_is_readable_by_a_member_and_denied_to_an_outsider_over_the_wire() {
    let port: u16 = 47451;

    // The producer, a grantee member, and an outsider each hold a keypair.
    let producer = party("coalition-air/prod");
    let grantee = party("coalition-air/con");
    let outsider = party("outsider");

    // Key-service builds + signs a bundle to {producer, grantee} under a fresh DEK.
    let ks_signer = SigningKey::generate();
    let recipients: BTreeMap<String, PublicKey> = [
        (producer.id.clone(), producer.secret.public_key()),
        (grantee.id.clone(), grantee.secret.public_key()),
    ]
    .into_iter()
    .collect();
    let dek = new_dek();
    let mut bundle = new_bundle(RS, 1, &dek, &recipients).expect("bundle");
    sign_bundle(&mut bundle, "key-service", &ks_signer).expect("sign");
    let bundle_wire = serde_json::to_vec(&bundle).unwrap();

    // Consumer side (the grantee's + outsider's vantage): one session that captures every
    // data message exactly as it leaves the router.
    let consumer_sess = zenoh::open(peer_listen(port))
        .wait()
        .expect("open consumer");
    let (tx, rx) = mpsc::channel::<(String, Vec<u8>)>();
    let _sub = consumer_sess
        .declare_subscriber("data/**")
        .callback(move |sample| {
            let _ = tx.send((
                sample.key_expr().as_str().to_string(),
                sample.payload().to_bytes().to_vec(),
            ));
        })
        .wait()
        .expect("subscribe data/**");

    // Producer side: its own session. It PUBLISHES the bundle (so a real deployment's
    // reader would fetch it from the keys storage) and then the sealed data message.
    let producer_sess = zenoh::open(peer_connect(port))
        .wait()
        .expect("open producer");
    std::thread::sleep(Duration::from_millis(1500)); // let the peers link

    // The producer publishes the key bundle (grant-keyd's job in production; here the
    // producer stands in so the test is one process) and acquires ITS OWN DEK from it.
    producer_sess
        .put("keys/coalition-air/1", bundle_wire)
        .wait()
        .expect("publish bundle");
    let mut sk = acquire_stream_key(
        RS,
        std::slice::from_ref(&bundle),
        &producer.id,
        &producer.secret,
    )
    .expect("producer pulls its own DEK (producer-as-member)");
    assert_eq!(sk.generation(), 1);

    // Seal a live message and PUT it on the data topic (in-band carrier).
    let env = sk.seal(Context(DATA_KEYEXPR), PLAINTEXT).expect("seal");
    producer_sess
        .put(DATA_KEYEXPR, encode_message(&env).unwrap())
        .wait()
        .expect("publish data");

    // (1) The message arrives over the live router on the data topic.
    let (topic, wire) = rx
        .recv_timeout(Duration::from_secs(5))
        .expect("data message did not arrive over the router");
    assert_eq!(topic, DATA_KEYEXPR);

    // The bundle a real consumer would have fetched is authenticated first.
    verify_bundle(&bundle, ks_signer.verifying_key()).expect("bundle authenticity");
    let received = decode_message(&wire).expect("decode received body");

    // (2) The grantee member admits the bundle and OPENS the received bytes.
    let mut member_store = KeyStore::new();
    assert!(member_store
        .admit(&bundle, &grantee.id, &grantee.secret)
        .expect("grantee admits its slot"));
    let opened = member_store
        .open(Context(&topic), &received)
        .expect("grantee opens the stream");
    assert_eq!(opened, PLAINTEXT, "member reads the plaintext");

    // (3) The outsider receives the SAME bytes and cannot open them — the read cut,
    //     enforced by the crypto, not the router.
    let mut outsider_store = KeyStore::new();
    assert!(
        !outsider_store
            .admit(&bundle, &outsider.id, &outsider.secret)
            .expect("admit is not an error for an outsider"),
        "outsider has no slot"
    );
    assert!(
        outsider_store.open(Context(&topic), &received).is_err(),
        "outsider must not open the stream"
    );

    let _ = producer_sess.close().wait();
    let _ = consumer_sess.close().wait();
}
