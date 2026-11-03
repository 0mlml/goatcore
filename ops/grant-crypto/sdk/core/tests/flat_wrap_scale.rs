//! M2.4 — what flat DEK wrapping actually costs as an audience grows.
//!
//! `grant-keyed-encryption.md` §5 decided to build flat wrapping and named the
//! key tree (LKH / MLS TreeKEM) as the upgrade "past low-thousands or under heavy
//! churn". That decision rested on estimates — ~80 KB and ~50 ms at low-thousands,
//! 0.5–0.8 MB at 10,000. This measures them, so the threshold is a number someone
//! took rather than a number someone guessed.
//!
//! What is being measured, and what is not:
//!
//! - **Rekey cost is O(N)** — one ECDH wrap per member, and it is the only part of
//!   the system that scales with audience size. That is what these cases time.
//! - **The data path is indifferent to audience size** — a message is sealed once
//!   under the DEK regardless of how many members hold it, so it does not appear
//!   here at all.
//! - **The read side is O(1)** — a member looks up its own slot and unwraps only
//!   that one. `read_side_does_not_scale_with_audience` is the assertion, and it
//!   compares work at two audience sizes rather than trusting the claim.
//!
//! Keypair generation is setup, not the operation under test, and is timed
//! separately so it cannot be mistaken for wrap cost.
//!
//! The bounded sweep runs in CI. The full sweep to 10,000 is `#[ignore]`d — it is
//! the measurement run, not a gate:
//!
//! ```text
//! cargo test --manifest-path sdk/core/Cargo.toml --test flat_wrap_scale -- --ignored --nocapture
//! ```

use p256::elliptic_curve::Generate;
use std::collections::BTreeMap;
use std::time::{Duration, Instant};

use grant_crypto::{new_bundle, new_dek, sign_bundle, unwrap_dek, KeyBundle};
use p256::ecdsa::SigningKey;
use p256::{PublicKey, SecretKey};

/// One audience size's result.
struct Row {
    n: usize,
    keygen: Duration,
    wrap: Duration,
    sign: Duration,
    wire_bytes: usize,
}

impl Row {
    /// Marginal wire cost per member — the number that decides whether a bundle
    /// fits a constrained link, and the one that is stable across machines.
    fn bytes_per_member(&self) -> f64 {
        self.wire_bytes as f64 / self.n as f64
    }
}

/// Member ids are the fabric's write-anchors — 32 hex characters (a slot uuid).
/// Using a short synthetic id would understate the wire size, since the id is
/// stored per slot.
fn member_id(i: usize) -> String {
    format!("{i:032x}")
}

fn measure(n: usize) -> (Row, Vec<SecretKey>) {
    let t = Instant::now();
    let secrets: Vec<SecretKey> = (0..n).map(|_| SecretKey::generate()).collect();
    let keygen = t.elapsed();

    let recipients: BTreeMap<String, PublicKey> = secrets
        .iter()
        .enumerate()
        .map(|(i, s)| (member_id(i), s.public_key()))
        .collect();

    let dek = new_dek();
    let t = Instant::now();
    let mut bundle = new_bundle("scale", 1, &dek, &recipients).expect("bundle");
    let wrap = t.elapsed();

    let signer = SigningKey::generate();
    let t = Instant::now();
    sign_bundle(&mut bundle, "key-service", &signer).expect("sign");
    let sign = t.elapsed();

    // The on-wire form is exactly what the publisher PUTs: serde_json bytes.
    let wire_bytes = serde_json::to_vec(&bundle).expect("serialize").len();

    (
        Row {
            n,
            keygen,
            wrap,
            sign,
            wire_bytes,
        },
        secrets,
    )
}

fn print_table(rows: &[Row]) {
    println!();
    println!(
        "{:>7}  {:>10}  {:>10}  {:>9}  {:>12}  {:>9}",
        "members", "keygen", "wrap", "sign", "wire bytes", "B/member"
    );
    for r in rows {
        println!(
            "{:>7}  {:>10.1?}  {:>10.1?}  {:>9.1?}  {:>12}  {:>9.1}",
            r.n,
            r.keygen,
            r.wrap,
            r.sign,
            r.wire_bytes,
            r.bytes_per_member()
        );
    }
    println!();
}

#[test]
fn flat_wrap_cost_is_linear_and_bounded() {
    let sizes = [1usize, 10, 100, 1_000];
    let rows: Vec<Row> = sizes.iter().map(|&n| measure(n).0).collect();
    print_table(&rows);

    let last = rows.last().unwrap();

    // 1. Per-member wire cost is a fixed shape (suite tag, ephemeral pubkey,
    //    nonce, ciphertext, all base64, plus the 32-char id), so it must not
    //    drift with audience size. A change here means the slot encoding changed
    //    and every size estimate downstream is stale.
    let per_member = last.bytes_per_member();
    assert!(
        (200.0..=400.0).contains(&per_member),
        "per-member wire cost {per_member:.1} B is outside the recorded band — \
         the slot encoding changed; re-measure and update M-115 and §5"
    );

    // 2. Linearity. Quadratic wrapping would be the failure that matters, and it
    //    would show as a ~100x step between 100 and 1000 rather than ~10x. The
    //    bound is deliberately loose (a shared CI runner is noisy); it is here to
    //    catch a complexity regression, not to police milliseconds.
    let at_100 = rows[2].wrap.as_secs_f64();
    let at_1000 = rows[3].wrap.as_secs_f64();
    if at_100 > 0.0 {
        let ratio = at_1000 / at_100;
        assert!(
            ratio < 40.0,
            "wrap time grew {ratio:.1}x from 100 to 1000 members — expected ~10x for O(N); \
             above ~100x means the wrap became quadratic"
        );
    }

    // 3. A 1,000-member rekey stays well inside a poll interval (30s by default).
    //    If this ever fails the daemon can no longer rotate within one sweep and
    //    the key tree stops being optional.
    //
    //    The bound is profile-dependent because P-256 ECDH is roughly 18x slower
    //    unoptimized, and this suite runs debug in CI while the fleet runs the
    //    release image. Measured on an Apple-Silicon laptop: 358ms
    //    release, 6.4s debug, at 1,000 members. Both bounds sit far enough above
    //    those to survive a loaded runner and still catch a real regression.
    let bound = if cfg!(debug_assertions) {
        Duration::from_secs(30)
    } else {
        Duration::from_secs(2)
    };
    assert!(
        last.wrap < bound,
        "wrapping to 1,000 members took {:?} (bound {bound:?}) — a rekey must fit \
         comfortably inside a poll interval",
        last.wrap
    );
}

#[test]
fn read_side_does_not_scale_with_audience() {
    // The claim under test: a member unwraps only its own slot, so a 1,000-member
    // audience costs a reader exactly what a 10-member one does. Compared as a
    // ratio between two audiences rather than asserted, and each unwrap is
    // repeated so a single scheduling hiccup cannot decide the result.
    fn unwrap_cost(n: usize) -> Duration {
        let (_, secrets) = measure(n);
        let recipients: BTreeMap<String, PublicKey> = secrets
            .iter()
            .enumerate()
            .map(|(i, s)| (member_id(i), s.public_key()))
            .collect();
        let dek = new_dek();
        let bundle: KeyBundle = new_bundle("scale", 1, &dek, &recipients).expect("bundle");

        // Read the LAST member's slot — the worst case for any implementation
        // that scans instead of looking up.
        let id = member_id(n - 1);
        let secret = &secrets[n - 1];
        let reps = 50;
        let t = Instant::now();
        for _ in 0..reps {
            let opened = unwrap_dek(&bundle.members[&id], secret).expect("unwrap");
            assert_eq!(opened, dek);
        }
        t.elapsed() / reps
    }

    let small = unwrap_cost(10);
    let large = unwrap_cost(1_000);
    println!("unwrap: 10-member audience {small:?}, 1000-member audience {large:?}");

    let ratio = large.as_secs_f64() / small.as_secs_f64();
    assert!(
        ratio < 3.0,
        "unwrapping one slot cost {ratio:.2}x more in a 1000-member audience than in a \
         10-member one — the read side is supposed to be O(1) in audience size"
    );
}

/// The measurement run behind M-115. Not a gate: it takes tens of seconds,
/// dominated by generating 10,000 keypairs, which is setup rather than the
/// operation being measured.
#[test]
#[ignore = "measurement run; see M-115"]
fn full_sweep_to_the_scope_ceiling() {
    let sizes = [1usize, 10, 100, 1_000, 5_000, 10_000];
    let rows: Vec<Row> = sizes.iter().map(|&n| measure(n).0).collect();
    print_table(&rows);

    let ten_k = rows.last().unwrap();
    println!(
        "10,000 members: bundle {:.2} MB on the wire, wrapped in {:?}",
        ten_k.wire_bytes as f64 / 1_048_576.0,
        ten_k.wrap
    );
}
