// Turn a raw third-party error (Zenoh, the portal client, bare I/O) into a
// goat-authored error instead of letting the library's own Display text
// reach the user verbatim.
//
// Why this exists: when a local identity file such as `mtls.cert.pem` is
// missing, `goat sub` would otherwise print the raw zenoh/tokio error text:
// several lines including internal paths from the machine that BUILT the goat
// binary, and nothing about the actual cause.
//
// The fix has two parts, and the FIRST is the one that actually matters:
//
//  1. Before ever calling into Zenoh, check that the local identity files
//     (cert/key/CA-roots) exist and are readable — `Profile::require_mesh_ready`
//     in profile.rs. Most real-world "goat sub just gives me garbage" reports
//     are this, and it's cheap and 100% reliable to check directly rather than
//     trying to reverse-engineer it from Zenoh's opaque error text.
//  2. Only once the local files are confirmed fine does the classifier below
//     take over — at that point a failure is a genuine network/router/ACL
//     problem, and a best-effort text classification is good enough because
//     `goat doctor` remains available for a full diagnosis.
//
// The exit code is part of the classification. `exit_codes.rs` is a
// documented contract that CI scripts grep, so only the two shapes this
// module can actually recognise carry a specific code: reachability →
// `E_ROUTER_UNREACHABLE` (5), handshake/ACL → `E_CERT_EXPIRED` (6). Anything
// else stays a plain `anyhow::Error` (exit 1) — the message says what was
// seen, and never claims a cert problem it cannot see.

use crate::errors::GoatError;

/// The shapes a failed Zenoh operation can take once the local identity
/// files are known to be present and readable.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum Shape {
    /// TCP / DNS / timeout: the router was never reached.
    Reachability,
    /// The router answered and refused: TLS handshake, certificate, ACL.
    HandshakeOrAcl,
    /// Neither list matched. No cause is asserted.
    Unknown,
}

const REACHABILITY_NEEDLES: [&str; 11] = [
    "unable to connect",
    "connection refused",
    "connection reset",
    "broken pipe",
    "timed out",
    "timeout",
    "no route to host",
    "network is unreachable",
    "dns",
    "name resolution",
    "no such host",
];

// A narrower, positive match for the handshake/ACL/cert shape, rather than
// "everything that isn't reachability": a malformed key expression, an
// unusually-worded resource-exhaustion message, or a session-already-closed
// must not land in the cert/ACL bucket, which would be a confident-sounding
// WRONG diagnosis and would false-positive any script keying off exit code 6.
const HANDSHAKE_OR_ACL_NEEDLES: [&str; 9] = [
    "tls",
    "certificate",
    "cert",
    "x509",
    "handshake",
    "acl",
    "denied",
    "unauthorized",
    "permission",
];

pub(crate) fn shape_of(text: &str) -> Shape {
    let lower = text.to_lowercase();
    if REACHABILITY_NEEDLES
        .iter()
        .any(|needle| lower.contains(needle))
    {
        Shape::Reachability
    } else if HANDSHAKE_OR_ACL_NEEDLES
        .iter()
        .any(|needle| lower.contains(needle))
    {
        Shape::HandshakeOrAcl
    } else {
        Shape::Unknown
    }
}

/// Classify a failed Zenoh operation (session open, declare, put, get —
/// anything returning a `zenoh::Error` or a channel error) into the error
/// the command should return. Call this ONLY after
/// `Profile::require_mesh_ready()` has confirmed the local cert/key/CA-roots
/// files are present and readable, so the question is just:
/// reachability-shaped, handshake/ACL-shaped, or neither?
///
/// `endpoint` is the router endpoint the session was opened against
/// (`Profile::router_endpoint_label()` for callers that hold a profile);
/// `what` is a short verb phrase for the operation, e.g. `"open a fabric
/// session"` / `"declare a subscriber"` / `"publish"`.
///
/// Reachability → `GoatError::RouterUnreachable` (exit 5). Handshake/ACL →
/// `GoatError::CertExpired` (exit 6). Anything else → a plain
/// `anyhow::Error` (exit 1), because an unrecognised failure must not claim
/// a cert problem on a CI-grepped exit code.
pub fn classify_zenoh_err(e: impl std::fmt::Display, endpoint: &str, what: &str) -> anyhow::Error {
    let text = e.to_string();
    let raw = first_line(&text);
    match shape_of(&text) {
        Shape::Reachability => GoatError::RouterUnreachable {
            endpoint: endpoint.to_string(),
            hint: format!(
                "could not reach the router at {endpoint} to {what} — check network/mesh \
                 connectivity (`goat status`), or run `goat doctor` for a full diagnosis.\n\
                 (raw: {raw})"
            ),
        }
        .into(),
        // Local files are already confirmed present+readable by the caller,
        // so this is the router itself rejecting the session/operation: an
        // expired cert, a URI-SAN/ACL mismatch, or similar content-level
        // problem — not a missing-file problem.
        Shape::HandshakeOrAcl => GoatError::CertExpired {
            hint: format!(
                "the router at {endpoint} rejected the attempt to {what} — the cert may be \
                 expired (run `goat profile show`) or the router's ACL may not permit this \
                 identity (URI-SAN mismatch). Run `goat doctor` for a full diagnosis.\n\
                 (raw: {raw})"
            ),
        }
        .into(),
        Shape::Unknown => anyhow::anyhow!(
            "could not {what} via {endpoint}: {raw} — not clearly a connectivity or a \
             cert/ACL problem; run `goat doctor` for a full diagnosis"
        ),
    }
}

/// One-line form of [`classify_zenoh_err`] for a TUI status line or a
/// `goat doctor` hint: the same classification, the raw first line kept as
/// a breadcrumb, no `help:`/`code:` stanza and no line breaks.
pub fn classify_zenoh_err_line(e: impl std::fmt::Display, endpoint: &str, what: &str) -> String {
    let text = e.to_string();
    let raw = first_line(&text);
    match shape_of(&text) {
        Shape::Reachability => {
            format!("could not reach the router at {endpoint} to {what} (raw: {raw})")
        }
        Shape::HandshakeOrAcl => format!(
            "the router at {endpoint} rejected the attempt to {what}: cert expired or \
             ACL/URI-SAN mismatch (raw: {raw})"
        ),
        Shape::Unknown => format!(
            "could not {what} via {endpoint}: {raw} (not clearly a connectivity or a cert/ACL \
             problem)"
        ),
    }
}

/// Zenoh/tokio errors are frequently multi-line, and only the first line is
/// ever a human-relevant summary — the rest is internal source-location
/// noise (the exact thing this module exists to stop surfacing as the
/// PRIMARY message). Keep just the first line as a "raw" breadcrumb for
/// anyone who wants to grep further, and trim it so it doesn't itself run
/// on forever.
fn first_line(s: &str) -> String {
    let line = s.lines().next().unwrap_or(s).trim();
    if line.chars().count() > 200 {
        let truncated: String = line.chars().take(200).collect();
        format!("{truncated}…")
    } else {
        line.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const ENDPOINT: &str = "tls/zenoh.example:7447";

    fn code_of(e: &anyhow::Error) -> Option<&'static str> {
        e.downcast_ref::<GoatError>().map(|g| g.code())
    }

    #[test]
    fn classifies_connection_failures_as_router_unreachable() {
        let e = classify_zenoh_err(
            "Unable to connect to any of [tls/zenoh.example:7447]",
            ENDPOINT,
            "open a fabric session",
        );
        assert_eq!(code_of(&e), Some("E_ROUTER_UNREACHABLE"));
        match e.downcast_ref::<GoatError>() {
            Some(GoatError::RouterUnreachable { endpoint, hint }) => {
                assert_eq!(endpoint, ENDPOINT);
                assert!(hint.contains("(raw: Unable to connect"), "hint: {hint}");
            }
            other => panic!("expected RouterUnreachable, got {other:?}"),
        }
    }

    #[test]
    fn classifies_handshake_failures_as_cert_expired() {
        let e = classify_zenoh_err("tls handshake eof", ENDPOINT, "open a fabric session");
        assert_eq!(code_of(&e), Some("E_CERT_EXPIRED"));
    }

    #[test]
    fn unrecognized_failure_is_a_plain_error_and_keeps_the_raw_text() {
        // A malformed key expression is neither a reachability nor a
        // cert/ACL problem: no GoatError, so main.rs maps it to exit 1, and
        // the message must not assert either cause.
        let e = classify_zenoh_err(
            "invalid key expression: bad wildcard placement",
            ENDPOINT,
            "declare a subscriber",
        );
        assert!(e.downcast_ref::<GoatError>().is_none(), "got {e:?}");
        let text = e.to_string();
        assert!(
            text.contains("invalid key expression: bad wildcard placement"),
            "{text}"
        );
        assert!(!text.contains("cert may be expired"), "{text}");
        assert!(
            text.contains("not clearly a connectivity or a cert/ACL problem"),
            "{text}"
        );
    }

    #[test]
    fn closed_channel_recv_error_is_not_a_cert_problem() {
        // What a subscriber's recv_async returns once the session is gone.
        let e = classify_zenoh_err(
            "receiving on a closed channel",
            ENDPOINT,
            "receive a sample",
        );
        assert!(e.downcast_ref::<GoatError>().is_none(), "got {e:?}");
    }

    #[test]
    fn line_form_carries_endpoint_and_raw_text_for_every_shape() {
        let reach = classify_zenoh_err_line("connection refused", ENDPOINT, "open a session");
        assert!(
            reach.starts_with("could not reach the router at tls/zenoh.example:7447"),
            "{reach}"
        );
        assert!(reach.contains("(raw: connection refused)"), "{reach}");
        let acl = classify_zenoh_err_line("acl denied", ENDPOINT, "declare a subscriber");
        assert!(
            acl.contains("rejected the attempt to declare a subscriber"),
            "{acl}"
        );
        assert!(acl.contains("(raw: acl denied)"), "{acl}");
        let unknown = classify_zenoh_err_line(
            "receiving on a closed channel\nat src/x.rs",
            ENDPOINT,
            "receive a sample",
        );
        assert!(
            unknown.contains("receiving on a closed channel"),
            "{unknown}"
        );
        assert!(!unknown.contains("src/x.rs"), "{unknown}");
        assert!(!unknown.contains('\n'), "{unknown}");
    }

    #[test]
    fn first_line_drops_trailing_lines() {
        assert_eq!(first_line("first\nsecond\nthird"), "first");
    }

    #[test]
    fn first_line_truncates_long_single_lines() {
        let long = "x".repeat(500);
        let out = first_line(&long);
        assert!(out.ends_with('…'));
        assert!(out.chars().count() <= 201);
    }
}
