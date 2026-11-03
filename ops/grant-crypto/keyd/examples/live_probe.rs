//! Live probe — read a grant-keyd DEK bundle off a real router (mTLS) and verify
//! it. Two modes:
//!   - default (subscribe): catch a bundle published *while the probe is live* —
//!     confirms the write is admitted (not silently ACL-denied) end to end.
//!   - `--get` (8th arg): QUERY `keys/**` as a late reader — confirms the router's
//!     `keys` storage RETAINED the bundle so a reader that arrived *after* the
//!     publish can still fetch the current generation (M2.2 history-key retention,
//!     grant-keyed-encryption.md §"History + late joiners"). Without the storage a
//!     get returns nothing (a plain PUT is fire-and-forget).
//!
//! Classical client — the PQC router down-negotiates to X25519 (zenoh-pqc-tls.md),
//! so no CryptoProvider install is needed.
//!
//! ```text
//! cargo run -p grant-keyd --example live_probe -- \
//!   tls/100.127.0.179:7447 \
//!   ~/.goat/contexts/zagros/mtls.cert.pem ~/.goat/contexts/zagros/mtls.key.pem \
//!   ~/.goat/contexts/zagros/ca-roots.pem \
//!   ./spire-ca.pem zagros.zagros.example \
//!   spiffe://zagros.zagros.example/grant-keyd \
//!   [--get]
//! ```

use std::path::Path;
use std::sync::mpsc;
use std::time::Duration;

use grant_crypto::KeyBundle;
use grant_keyd::svid::{verify_bundle_self, SvidValidator, TrustBundle};
use zenoh::{Config, Wait};

type BoxErr = Box<dyn std::error::Error + Send + Sync>;

fn main() -> Result<(), BoxErr> {
    let a: Vec<String> = std::env::args().skip(1).collect();
    if a.len() < 7 {
        return Err("usage: live_probe <endpoint> <cert> <key> <router_ca> <spire_root> <trust_domain> <key_service_spiffe_id>".into());
    }
    let (endpoint, cert, key, router_ca, spire_root, trust_domain, ks_id) =
        (&a[0], &a[1], &a[2], &a[3], &a[4], &a[5], &a[6]);
    let get_mode = a.get(7).map(|s| s == "--get").unwrap_or(false);

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

    let session = zenoh::open(c).wait()?;
    let validator = SvidValidator::new(
        trust_domain.clone(),
        TrustBundle::load(Path::new(spire_root))?,
    );

    // Deserialize, describe, and verify one received bundle against the SPIRE root.
    let report = |topic: &str, wire: &[u8]| {
        let bundle: KeyBundle = match serde_json::from_slice(wire) {
            Ok(b) => b,
            Err(e) => {
                println!("RECEIVED on {topic} but failed to deserialize: {e}");
                return;
            }
        };
        println!(
            "RECEIVED on {topic}: suite={} rs={} gen={} members={} signer={:?} self_describing={}",
            bundle.suite,
            bundle.rs,
            bundle.gen,
            bundle.members.len(),
            bundle.signer,
            bundle.signer_svid.is_some(),
        );
        match verify_bundle_self(&bundle, &validator, ks_id) {
            Ok(()) => println!(
                "VERIFIED: the bundle's signer SVID chains to the SPIRE root and its id matches {ks_id:?}"
            ),
            Err(e) => println!("VERIFY FAILED: {e}"),
        }
    };

    if get_mode {
        // Late-reader retention test: QUERY keys/** — only the `keys` storage can
        // answer, so a non-empty result proves the bundle was retained (a plain
        // PUT with no store would return nothing to a reader that arrived late).
        eprintln!("probe: GET keys/** on {endpoint} (late-reader retention test)…");
        let replies = session.get("keys/**").wait()?;
        let mut got = 0usize;
        // FIFO handler: recv_timeout -> ZResult<Option<Reply>>; Ok(None) = query
        // drained / no more replies within the window, so Some(..) drives the loop.
        while let Ok(Some(reply)) = replies.recv_timeout(Duration::from_secs(10)) {
            match reply.result() {
                Ok(sample) => {
                    got += 1;
                    let wire = sample.payload().to_bytes().to_vec();
                    report(sample.key_expr().as_str(), &wire);
                }
                Err(e) => eprintln!("reply error: {e:?}"),
            }
        }
        if got == 0 {
            println!(
                "NO bundle retained — the keys storage is missing/empty, so a late reader gets nothing."
            );
        } else {
            println!("RETENTION OK: {got} bundle(s) served to a late reader by the keys storage.");
        }
    } else {
        let (tx, rx) = mpsc::channel::<(String, Vec<u8>)>();
        let _sub = session
            .declare_subscriber("keys/**")
            .callback(move |s| {
                let _ = tx.send((
                    s.key_expr().as_str().to_string(),
                    s.payload().to_bytes().to_vec(),
                ));
            })
            .wait()?;
        eprintln!("probe: subscribed keys/** on {endpoint}; waiting 30s for a bundle…");

        match rx.recv_timeout(Duration::from_secs(30)) {
            Ok((topic, wire)) => report(&topic, &wire),
            Err(_) => {
                println!(
                    "NO bundle received in 30s — not published, denied by ACL, or wrong topic."
                )
            }
        }
    }
    let _ = session.close().wait();
    Ok(())
}
