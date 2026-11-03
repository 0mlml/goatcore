//! Block 50A.4 production-readiness hardening — fuzz the wire-format parsers.
//!
//! Each `#[test]` here drives one of the public parse functions with arbitrary
//! input (proptest-generated random byte slices, length 0..=4096) and asserts
//! that the function does not panic. Returning `Err` on malformed input is the
//! expected behavior; panicking on adversary-chosen bytes is a real bug. This
//! is not a substitute for cargo-fuzz / libFuzzer (deeper coverage, requires
//! nightly Rust), but proptest delivers ~80% of the value on stable + cheap
//! enough to run in CI on every PR.
//!
//! Coverage of the public parse surface today:
//!   - Auth::parse, AuthResponse::parse, Hello::parse, HelloResponse::parse
//!   - Transport::parse (the bulk-payload hot path)
//!   - parse_close, parse_health_check
//!   - parse_peer_list
//!   - read_msg_type
//!
//! Per-fixture corpus tests are not duplicated here — those live in the
//! existing conformance harness at goat-relay-protocol/src/bin/conformance.rs.

use goat_relay_protocol::messages::{
    auth::{Auth, AuthResponse, Hello, HelloResponse},
    close::{parse_close, parse_health_check},
    payload::Transport,
    peer_state::parse_peer_list,
    read_msg_type,
};
use proptest::prelude::*;

const MAX_FUZZ_LEN: usize = 4096;

proptest! {
    #![proptest_config(ProptestConfig {
        // 1024 cases per parser strikes a balance between coverage and CI time
        // (whole suite runs in well under 10s). Bumping to 10k+ is appropriate
        // for the scheduled long-fuzz cron once that lands.
        cases: 1024,
        ..ProptestConfig::default()
    })]

    #[test]
    fn fuzz_auth_parse_never_panics(input in prop::collection::vec(any::<u8>(), 0..=MAX_FUZZ_LEN)) {
        let _ = Auth::parse(&input);
    }

    #[test]
    fn fuzz_auth_response_parse_never_panics(input in prop::collection::vec(any::<u8>(), 0..=MAX_FUZZ_LEN)) {
        let _ = AuthResponse::parse(&input);
    }

    #[test]
    fn fuzz_hello_parse_never_panics(input in prop::collection::vec(any::<u8>(), 0..=MAX_FUZZ_LEN)) {
        let _ = Hello::parse(&input);
    }

    #[test]
    fn fuzz_hello_response_parse_never_panics(input in prop::collection::vec(any::<u8>(), 0..=MAX_FUZZ_LEN)) {
        let _ = HelloResponse::parse(&input);
    }

    #[test]
    fn fuzz_transport_parse_never_panics(input in prop::collection::vec(any::<u8>(), 0..=MAX_FUZZ_LEN)) {
        let _ = Transport::parse(&input);
    }

    #[test]
    fn fuzz_close_parse_never_panics(input in prop::collection::vec(any::<u8>(), 0..=MAX_FUZZ_LEN)) {
        let _ = parse_close(&input);
    }

    #[test]
    fn fuzz_health_check_parse_never_panics(input in prop::collection::vec(any::<u8>(), 0..=MAX_FUZZ_LEN)) {
        let _ = parse_health_check(&input);
    }

    #[test]
    fn fuzz_peer_list_parse_never_panics(input in prop::collection::vec(any::<u8>(), 0..=MAX_FUZZ_LEN)) {
        let _ = parse_peer_list(&input);
    }

    #[test]
    fn fuzz_read_msg_type_never_panics(input in prop::collection::vec(any::<u8>(), 0..=MAX_FUZZ_LEN)) {
        let _ = read_msg_type(&input);
    }
}
