// Slim protobuf decoder for `goat sub --decode`. Mirrors panoscope's
// decoder.rs pattern (fetch_fds + select-by-declared-format) without
// the cache surface — every invocation is interactive and short-
// lived, so the cache overhead doesn't pay for itself.
//
// Lookup path per sample:
//   1. find_topic_by_key(key) → topic.format ("protobuf:<MsgName>")
//   2. fetch_fds(key) → FileDescriptorSet bytes
//   3. select_message(fds, declared) → MessageDescriptor (honors
//      declared name; falls back to first-message-in-FDS when
//      undeclared or not present — matches panoscope behavior so
//      operators see the same decode result both places)
//   4. DynamicMessage::decode(descriptor, body) → message
//   5. serde_json::to_value(&msg) → JSON for stdout
//
// Cache shape: per-topic-key descriptor, populated on first sample,
// reused for subsequent samples of the same key in the same
// `goat sub` invocation. Sized small (HashMap) — `goat sub` against
// a wildcard expression sees N distinct topics, and the cache lives
// for the lifetime of the process.

use panoscope_core::portal::{FetchOutcome, PortalClient, PortalError};
use prost_reflect::{DescriptorPool, DynamicMessage, MessageDescriptor};
use std::collections::HashMap;

/// Per-key descriptor cache. Read-write from one task so no mutex
/// needed; `goat sub` consumes samples sequentially.
pub struct DecodeCache {
    portal: PortalClient,
    descriptors: HashMap<String, Option<MessageDescriptor>>,
    /// Short cooldown per topic after a `Transient` fetch failure. NOT
    /// caching a negative result at all (the fix for the worse permanent-
    /// negative-cache bug) means every single sample would otherwise re-hit
    /// the portal with two full round-trips for as long as it stays
    /// degraded — on a high-rate topic during a portal slowdown that
    /// stampedes the portal and stalls the whole `goat sub --decode` loop
    /// behind sequential retries. A few seconds of backoff avoids the
    /// stampede while still recovering promptly once the portal is healthy.
    transient_until: HashMap<String, std::time::Instant>,
}

/// How long to skip re-fetching after a Transient failure for a topic.
/// Short enough that decoding resumes promptly once the portal recovers;
/// long enough that a fast topic doesn't hammer a struggling portal once
/// per sample.
const TRANSIENT_BACKOFF: std::time::Duration = std::time::Duration::from_secs(3);

impl DecodeCache {
    pub fn new(portal: PortalClient) -> Self {
        Self {
            portal,
            descriptors: HashMap::new(),
            transient_until: HashMap::new(),
        }
    }

    /// Decode one sample. Returns the JSON-stringified decoded form on
    /// success, an operator-readable error string on failure (the
    /// per-sample form: "DECODE-ERR: <reason>" so the caller can
    /// render it inline next to the raw bytes).
    pub async fn decode(&mut self, topic_key: &str, body: &[u8]) -> String {
        let descriptor = match self.resolve(topic_key).await {
            Some(d) => d,
            None => return format!("DECODE-ERR: no schema for `{topic_key}` (portal disabled / no .fds uploaded)"),
        };
        match DynamicMessage::decode(descriptor.clone(), body) {
            Ok(msg) => match serde_json::to_string(&msg) {
                Ok(s) => s,
                Err(e) => format!("DECODE-ERR: json serialize: {e}"),
            },
            Err(e) => format!("DECODE-ERR: wire-format mismatch ({e}) — likely first-message-fallback hit the wrong message"),
        }
    }

    async fn resolve(&mut self, topic_key: &str) -> Option<MessageDescriptor> {
        if let Some(cached) = self.descriptors.get(topic_key) {
            return cached.clone();
        }
        if let Some(until) = self.transient_until.get(topic_key) {
            if std::time::Instant::now() < *until {
                return None;
            }
        }
        // Two roundtrips: topic row for declared format, then .fds bytes.
        let declared = match self.portal.find_topic_by_key(topic_key).await {
            Ok(Some(t)) => parse_protobuf_format(&t.format),
            Ok(None) | Err(PortalError::Disabled) | Err(PortalError::NotFound { .. }) => None,
            // A 401, a 5xx or a network blip: not fatal for decoding (the
            // descriptor is then selected without a declared format), but
            // not silent either.
            Err(e) => {
                tracing::debug!(
                    topic = topic_key, %e,
                    "topic lookup failed; selecting the descriptor without a declared format"
                );
                None
            }
        };
        let bytes = match self.portal.fetch_fds(topic_key).await {
            FetchOutcome::Ready(b) => b,
            // NoSchema/Disabled are terminal for this topic — caching `None`
            // is correct, they won't resolve differently on a retry within
            // this process. Transient is NOT terminal (it's a network blip,
            // per its own name) — caching it as a permanent negative used to
            // mean one bad request silently disabled decoding for the rest
            // of the `goat sub --decode` run. Don't cache it, so the next
            // sample on this topic gets a fresh attempt.
            FetchOutcome::NoSchema | FetchOutcome::Disabled => {
                self.descriptors.insert(topic_key.to_string(), None);
                return None;
            }
            FetchOutcome::Transient(reason) => {
                tracing::debug!(
                    topic = topic_key, %reason, backoff_secs = TRANSIENT_BACKOFF.as_secs(),
                    "schema fetch transient failure, backing off before retry"
                );
                self.transient_until
                    .insert(topic_key.to_string(), std::time::Instant::now() + TRANSIENT_BACKOFF);
                return None;
            }
        };
        let descriptor = select_message(&bytes, declared.as_deref());
        self.descriptors
            .insert(topic_key.to_string(), descriptor.clone());
        descriptor
    }
}

/// Extract `<Name>` from a topic's `format` field. Mirrors panoscope's
/// `parse_protobuf_format`. `protobuf:` (empty suffix) and non-protobuf
/// formats return None → caller falls back to first-message heuristic.
fn parse_protobuf_format(format: &str) -> Option<String> {
    let rest = format.strip_prefix("protobuf:")?;
    if rest.is_empty() {
        None
    } else {
        Some(rest.to_string())
    }
}

/// Pick the right MessageDescriptor from an FDS. Prefers the declared
/// name (fully-qualified, then simple-name walk), falls back to first
/// message when undeclared. Mirrors panoscope's `select_message`.
fn select_message(fds_bytes: &[u8], declared: Option<&str>) -> Option<MessageDescriptor> {
    let pool = DescriptorPool::decode(fds_bytes).ok()?;
    if let Some(name) = declared {
        if let Some(d) = pool.get_message_by_name(name) {
            return Some(d);
        }
        if let Some(d) = pool.all_messages().find(|m| m.name() == name) {
            return Some(d);
        }
    }
    // Bind first to a local so the all_messages() iterator (which
    // borrows `pool`) is dropped before pool drops. MessageDescriptor
    // is Arc-backed so it outlives pool on its own.
    let first = pool.all_messages().next();
    first
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_protobuf_format_extracts_name_or_none() {
        assert_eq!(
            parse_protobuf_format("protobuf:DenObservation"),
            Some("DenObservation".into())
        );
        assert_eq!(parse_protobuf_format("protobuf:"), None);
        assert_eq!(parse_protobuf_format("json"), None);
        assert_eq!(parse_protobuf_format(""), None);
    }

    #[test]
    fn select_message_returns_none_on_empty_fds() {
        // Empty bytes parse as an empty pool → no messages → None.
        assert!(select_message(&[], None).is_none());
    }
}
