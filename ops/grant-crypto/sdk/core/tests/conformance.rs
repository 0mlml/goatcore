//! Cross-language conformance: the Rust read side must reach the same accept/
//! reject decision (and plaintext) as the Go reference on every committed vector.
//! The vectors file is shared, not copied — the drift guard between the two
//! implementations. See ../../../conformance and ../../../WIRE-FORMAT.md.

use base64::Engine;
use grant_crypto::*;
use serde::Deserialize;
use std::collections::BTreeMap;

#[derive(Deserialize)]
struct Vectors {
    suite: String,
    keys: BTreeMap<String, KeyRef>,
    artifacts: BTreeMap<String, Artifact>,
    cases: Vec<Case>,
}

#[derive(Deserialize)]
struct KeyRef {
    #[serde(default)]
    priv_scalar_hex: String,
    #[serde(default)]
    pub_spki_b64: String,
}

#[derive(Deserialize)]
struct Artifact {
    envelope: serde_json::Value,
    bundle: serde_json::Value,
    keyexpr: String,
}

#[derive(Deserialize)]
struct Case {
    name: String,
    artifact: String,
    member: String,
    #[serde(default)]
    verify: String,
    #[serde(default)]
    keyexpr: String,
    expect: Expect,
}

#[derive(Deserialize)]
struct Expect {
    result: String,
    #[serde(default)]
    plaintext_b64: String,
    #[serde(default)]
    reason: String,
}

fn b64(s: &str) -> Vec<u8> {
    base64::engine::general_purpose::STANDARD
        .decode(s)
        .expect("base64")
}

#[test]
fn conformance_vectors() {
    let path = concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../../envelope/testdata/vectors/vectors.json"
    );
    let data = std::fs::read_to_string(path).expect("read vectors.json");
    let v: Vectors = serde_json::from_str(&data).expect("parse vectors");
    assert_eq!(v.suite, SUITE_V1, "vectors suite");
    assert!(!v.cases.is_empty(), "no cases");

    for c in &v.cases {
        let art = v
            .artifacts
            .get(&c.artifact)
            .unwrap_or_else(|| panic!("unknown artifact {}", c.artifact));
        let env: Envelope = serde_json::from_value(art.envelope.clone()).expect("parse envelope");
        let bundle: KeyBundle = serde_json::from_value(art.bundle.clone()).expect("parse bundle");
        let keyexpr = if c.keyexpr.is_empty() {
            art.keyexpr.as_str()
        } else {
            c.keyexpr.as_str()
        };

        let member_hex = &v.keys.get(&c.member).expect("member key").priv_scalar_hex;
        let member =
            secret_from_scalar(&hex::decode(member_hex).expect("hex")).expect("member secret");

        let vk = if c.verify.is_empty() {
            None
        } else {
            let der = b64(&v.keys.get(&c.verify).expect("verify key").pub_spki_b64);
            Some(verifying_key_from_spki(&der).expect("verify key"))
        };

        let res = consume(&bundle, &env, &c.member, &member, vk.as_ref(), keyexpr);

        match c.expect.result.as_str() {
            "accept" => {
                let pt = res
                    .unwrap_or_else(|e| panic!("case {:?}: expected accept, got {:?}", c.name, e));
                assert_eq!(
                    pt,
                    b64(&c.expect.plaintext_b64),
                    "case {:?}: plaintext",
                    c.name
                );
            }
            "reject" => {
                assert!(
                    res.is_err(),
                    "case {:?}: expected reject ({}), got accept",
                    c.name,
                    c.expect.reason
                );
            }
            other => panic!("case {:?}: unknown result {}", c.name, other),
        }
    }
}
