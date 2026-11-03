//! Piece A live producer — seal a live stream to a release-set on a real router (mTLS).
//!
//! The write half of the data owner's capability, run against live ZAGROS. The producer is
//! a MEMBER of the release-set (design §3 DEK-handoff decision): it QUERIES `keys/<rs>/**`
//! off the router's keys storage, verifies each bundle's signer SVID against the SPIRE
//! root, unwraps ITS OWN slot to obtain the DEK, seals the message, and PUTs the ciphertext
//! (in-band `Envelope`) on `data/<rs>/<topic>`. Pair with the `consumer` example.
//!
//! The producer's unwrap key is its own SVID private key (PKCS#8 or SEC1 PEM) — the same
//! keypair whose leaf is in the audience under `member_id`.
//!
//! ```text
//! cargo run -p grant-keyd --example producer -- \
//!   tls/100.127.0.179:7447 \
//!   ~/.goat/contexts/zagros/mtls.cert.pem ~/.goat/contexts/zagros/mtls.key.pem \
//!   ~/.goat/contexts/zagros/ca-roots.pem \
//!   ./spire-ca.pem zagros.zagros.example \
//!   spiffe://zagros.zagros.example/grant-keyd \
//!   pilot spiffe://zagros.zagros.example/pilot/prod ./prod.svid.key.pem \
//!   data/pilot/cop "brigade COP snapshot"
//! ```

use std::path::Path;
use std::time::Duration;

use grant_crypto::KeyBundle;
use grant_keyd::datapath::{acquire_stream_key, encode_message, Context};
use grant_keyd::svid::{verify_bundle_self, SvidValidator, TrustBundle};
use p256::SecretKey;
use zenoh::{Config, Wait};

type BoxErr = Box<dyn std::error::Error + Send + Sync>;

fn load_secret(pem_path: &str) -> Result<SecretKey, BoxErr> {
    use p256::pkcs8::DecodePrivateKey;
    use p256::SecretKey as Sk;
    let pem = std::fs::read_to_string(pem_path)?;
    // SPIRE/spiffe-helper key.pem is usually PKCS#8; accept SEC1 as a fallback.
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
    if a.len() < 12 {
        return Err(
            "usage: producer <endpoint> <cert> <key> <router_ca> <spire_root> \
             <trust_domain> <key_service_spiffe_id> <release_set> <member_id> \
             <member_key_pem> <data_keyexpr> <message>"
                .into(),
        );
    }
    let (endpoint, cert, key, router_ca, spire_root, trust_domain, ks_id) =
        (&a[0], &a[1], &a[2], &a[3], &a[4], &a[5], &a[6]);
    let (rs, member_id, member_key_pem, data_keyexpr, message) =
        (&a[7], &a[8], &a[9], &a[10], &a[11]);

    let member_secret = load_secret(member_key_pem)?;
    let validator = SvidValidator::new(
        trust_domain.clone(),
        TrustBundle::load(Path::new(spire_root))?,
    );
    let session = open_session(endpoint, cert, key, router_ca)?;

    // Fetch the current key bundles for this release-set off the keys storage, keeping only
    // those whose signer SVID chains to the SPIRE root (fail-closed on the trust anchor).
    eprintln!("producer: GET keys/{rs}/** on {endpoint}…");
    let replies = session.get(format!("keys/{rs}/**")).wait()?;
    let mut bundles: Vec<KeyBundle> = Vec::new();
    while let Ok(Some(reply)) = replies.recv_timeout(Duration::from_secs(10)) {
        let Ok(sample) = reply.result() else { continue };
        let wire = sample.payload().to_bytes().to_vec();
        let bundle: KeyBundle = match serde_json::from_slice(&wire) {
            Ok(b) => b,
            Err(e) => {
                eprintln!("  skip {}: bad bundle: {e}", sample.key_expr().as_str());
                continue;
            }
        };
        match verify_bundle_self(&bundle, &validator, ks_id) {
            Ok(()) => {
                eprintln!("  bundle {} gen={} VERIFIED", bundle.rs, bundle.gen);
                bundles.push(bundle);
            }
            Err(e) => eprintln!("  reject {} gen={}: {e}", bundle.rs, bundle.gen),
        }
    }
    if bundles.is_empty() {
        return Err(format!(
            "no verified key bundle for release-set {rs:?} — is grant-keyd publishing, and is \
             {member_id:?} in the audience?"
        )
        .into());
    }

    // Producer-as-member: unwrap our own slot from the newest bundle to get the DEK.
    let mut sk = acquire_stream_key(rs, &bundles, member_id, &member_secret).map_err(|e| {
        format!("no slot for {member_id:?} in any bundle (producer not in the audience?): {e:?}")
    })?;
    eprintln!(
        "producer: acquired DEK for {rs} generation {} (as member {member_id})",
        sk.generation()
    );

    // Seal and publish the message in-band on the data topic.
    // seal_current, not seal: refuse if a newer generation is visible. This
    // example is one-shot so it would be safe either way — but it is the thing
    // people copy, and the pattern it demonstrates should be the correct one
    //.
    let env = sk
        .seal_current(Context(data_keyexpr), message.as_bytes(), &bundles)
        .map_err(|e| format!("seal: {e:?}"))?;
    let body = encode_message(&env).map_err(|e| format!("encode: {e:?}"))?;
    session.put(data_keyexpr.clone(), body).wait()?;
    println!(
        "PUBLISHED {}B sealed to {rs} gen {} on {data_keyexpr}",
        message.len(),
        sk.generation()
    );

    let _ = session.close().wait();
    Ok(())
}
