//! Piece A live consumer — subscribe an encrypted stream and open it (mTLS, real router).
//!
//! The read half of the capability, run against live ZAGROS. It fetches the release-set's
//! key bundles off the `keys/**` storage, verifies each signer SVID against the SPIRE root,
//! admits the slots it can open, then subscribes `data/<rs>/**` and opens each message with
//! the held DEK. A member prints the plaintext; a NON-member prints DENIED — the read cut,
//! enforced by the crypto, not the router. Pair with the `producer` example.
//!
//! The consumer's unwrap key is its own SVID private key (PKCS#8 or SEC1 PEM), matching the
//! leaf in the audience under `member_id`.
//!
//! ```text
//! cargo run -p grant-keyd --example consumer -- \
//!   tls/100.127.0.179:7447 \
//!   ~/.goat/contexts/zagros/mtls.cert.pem ~/.goat/contexts/zagros/mtls.key.pem \
//!   ~/.goat/contexts/zagros/ca-roots.pem \
//!   ./spire-ca.pem zagros.zagros.example \
//!   spiffe://zagros.zagros.example/grant-keyd \
//!   pilot spiffe://zagros.zagros.example/pilot/con ./con.svid.key.pem \
//!   30
//! ```
//! The last arg is how many seconds to keep reading the stream (default 30).

use std::path::Path;
use std::sync::mpsc;
use std::time::Duration;

use grant_crypto::KeyBundle;
use grant_keyd::datapath::{decode_message, Context, KeyStore};
use grant_keyd::svid::{verify_bundle_self, SvidValidator, TrustBundle};
use p256::SecretKey;
use zenoh::{Config, Wait};

type BoxErr = Box<dyn std::error::Error + Send + Sync>;

fn load_secret(pem_path: &str) -> Result<SecretKey, BoxErr> {
    use p256::pkcs8::DecodePrivateKey;
    use p256::SecretKey as Sk;
    let pem = std::fs::read_to_string(pem_path)?;
    Sk::from_pkcs8_pem(&pem)
        .or_else(|_| Sk::from_sec1_pem(&pem))
        .map_err(|e| format!("load member key {pem_path}: {e}").into())
}

fn open_session(
    endpoint: &str,
    cert: &str,
    key: &str,
    router_ca: &str,
) -> Result<zenoh::Session, BoxErr> {
    let mut c = Config::default();
    // Client mode, no scouting: two peer-mode copies on one machine find each other
    // and answer each other's queries empty instead of reaching the router.
    c.insert_json5("mode", "\"client\"")?;
    c.insert_json5("scouting/multicast/enabled", "false")?;
    c.insert_json5("connect/endpoints", &format!("[{endpoint:?}]"))?;
    for (k, v) in [
        ("transport/link/tls/root_ca_certificate", router_ca),
        ("transport/link/tls/connect_certificate", cert),
        ("transport/link/tls/connect_private_key", key),
    ] {
        c.insert_json5(k, &format!("{v:?}"))?;
    }
    c.insert_json5("transport/link/tls/verify_name_on_connect", "false")?;
    c.insert_json5("transport/link/tls/enable_mtls", "true")?;
    zenoh::open(c).wait()
}

fn main() -> Result<(), BoxErr> {
    let a: Vec<String> = std::env::args().skip(1).collect();
    if a.len() < 10 {
        return Err(
            "usage: consumer <endpoint> <cert> <key> <router_ca> <spire_root> \
             <trust_domain> <key_service_spiffe_id> <release_set> <member_id> \
             <member_key_pem> [read_secs] [sub_keyexpr]"
                .into(),
        );
    }
    let (endpoint, cert, key, router_ca, spire_root, trust_domain, ks_id) =
        (&a[0], &a[1], &a[2], &a[3], &a[4], &a[5], &a[6]);
    let (rs, member_id, member_key_pem) = (&a[7], &a[8], &a[9]);
    let read_secs: u64 = a.get(10).and_then(|s| s.parse().ok()).unwrap_or(30);
    // Where the ciphertext lives. Default = the local-router test shape
    // (`data/<rs>/**`); on a live slot-pool goatnet a producer may only write
    // under ITS OWN anchor subtree (the router ACL admits CN = anchor for
    // `<anchor>/**` and nothing else — F-177 silent denial otherwise), and
    // since rs ≡ producer anchor the live shape is `<rs>/**`. Pass it
    // explicitly to match where the producer actually publishes.
    let sub_keyexpr = a
        .get(11)
        .cloned()
        .unwrap_or_else(|| format!("data/{rs}/**"));

    let member_secret = load_secret(member_key_pem)?;
    let validator = SvidValidator::new(
        trust_domain.clone(),
        TrustBundle::load(Path::new(spire_root))?,
    );
    let session = open_session(endpoint, cert, key, router_ca)?;

    // Fetch + verify + admit the release-set's current key bundles.
    eprintln!("consumer: GET keys/{rs}/** on {endpoint}…");
    let mut store = KeyStore::new();
    let mut admitted = 0usize;
    let replies = session.get(format!("keys/{rs}/**")).wait()?;
    while let Ok(Some(reply)) = replies.recv_timeout(Duration::from_secs(10)) {
        let Ok(sample) = reply.result() else { continue };
        let wire = sample.payload().to_bytes().to_vec();
        let bundle: KeyBundle = match serde_json::from_slice(&wire) {
            Ok(b) => b,
            Err(_) => continue,
        };
        if verify_bundle_self(&bundle, &validator, ks_id).is_err() {
            eprintln!(
                "  reject {} gen={}: signer not trusted",
                bundle.rs, bundle.gen
            );
            continue;
        }
        match store.admit(&bundle, member_id, &member_secret) {
            Ok(true) => {
                admitted += 1;
                eprintln!("  admitted {} gen={} (have a slot)", bundle.rs, bundle.gen);
            }
            Ok(false) => eprintln!(
                "  {} gen={}: no slot for {member_id}",
                bundle.rs, bundle.gen
            ),
            Err(e) => eprintln!("  {} gen={}: bad slot: {e:?}", bundle.rs, bundle.gen),
        }
    }
    if admitted == 0 {
        eprintln!(
            "NOTE: no bundle carried a slot for {member_id:?} — reads will be DENIED (this is the \
             expected outcome for a non-member/outsider)."
        );
    }

    // Consumption is query-history-THEN-subscribe. Plain Zenoh pub/sub only
    // delivers to subscribers already matched at PUT time, so a live-only
    // `declare_subscriber` misses anything published before its declaration
    // propagates to the router (the M-090 live-fire hit exactly this: a single
    // producer PUT raced the subscription and the subscriber saw 0). The fabric
    // retains data on the router's slot storage, so a startup GET pulls whatever
    // is already there; the subscribe then carries new samples live. This is the
    // durable real-consumer pattern (retention storage + live tail), not a
    // workaround — a plain live-only subscriber is only correct for a stream you
    // are guaranteed to have joined before the first PUT.
    let (tx, rx) = mpsc::channel::<(String, Vec<u8>)>();

    // (1) History: query the storage for anything already retained.
    let replies = session.get(sub_keyexpr.clone()).wait()?;
    let mut history = 0usize;
    while let Ok(reply) = replies.recv() {
        if let Ok(sample) = reply.result() {
            let _ = tx.send((
                sample.key_expr().as_str().to_string(),
                sample.payload().to_bytes().to_vec(),
            ));
            history += 1;
        }
    }
    eprintln!("consumer: {history} retained sample(s) from storage on {sub_keyexpr}");

    // (2) Live tail: subscribe for anything published from here on.
    let _sub = session
        .declare_subscriber(sub_keyexpr.clone())
        .callback(move |sample| {
            let _ = tx.send((
                sample.key_expr().as_str().to_string(),
                sample.payload().to_bytes().to_vec(),
            ));
        })
        .wait()?;
    eprintln!("consumer: subscribed {sub_keyexpr}; reading for {read_secs}s…");

    let deadline = Duration::from_secs(read_secs);
    let mut opened = 0usize;
    let mut denied = 0usize;
    while let Ok((topic, wire)) = rx.recv_timeout(deadline) {
        let env = match decode_message(&wire) {
            Ok(e) => e,
            Err(_) => {
                eprintln!("  {topic}: not a grant-crypto envelope (skipped)");
                continue;
            }
        };
        match store.open(Context(&topic), &env) {
            Ok(pt) => {
                opened += 1;
                match std::str::from_utf8(&pt) {
                    Ok(s) => println!("OPENED {topic} gen={}: {s}", env.gen),
                    Err(_) => println!("OPENED {topic} gen={}: {}B binary", env.gen, pt.len()),
                }
            }
            Err(e) => {
                denied += 1;
                println!("DENIED {topic} gen={}: {e:?}", env.gen);
            }
        }
    }
    println!("done: {opened} opened, {denied} denied over {read_secs}s.");

    let _ = session.close().wait();
    Ok(())
}
