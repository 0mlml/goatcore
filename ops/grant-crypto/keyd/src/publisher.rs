//! The native-Zenoh `BundlePublisher` — E1's write onto the fabric.
//!
//! `KeyService::rotate` hands a fully-assembled, signed bundle and its
//! `keys/<release-set>/<generation>` topic to a `BundlePublisher`; this is the
//! production impl, a thin wrapper over a live Zenoh session that PUTs the bytes
//! unchanged. Publishing is the ONLY thing that touches the fabric — the crypto is
//! already done and the bundle is self-authenticating (key-service signature +
//! the write-side ACL on `keys/**`), so the transport carries opaque bytes and the
//! router is never in the trust path (design §2, brokerless).
//!
//! Synchronous by construction: Zenoh 1.x's `.wait()` API runs the session without
//! an async runtime, matching the daemon's simple poll-and-publish loop.

use grant_crypto::service::{BundlePublisher, ServiceError};
use zenoh::{Session, Wait};

/// A `BundlePublisher` backed by a live Zenoh session.
pub struct ZenohBundlePublisher {
    session: Session,
}

impl ZenohBundlePublisher {
    /// Wrap an already-open session (the daemon shares one session for its lifetime).
    pub fn new(session: Session) -> Self {
        Self { session }
    }

    /// Open a session from a Zenoh config and wrap it. On the fleet this config
    /// carries the key-service's mTLS identity and the router endpoints; failure to
    /// connect is a `Publish` error (nothing was ever put).
    pub fn connect(config: zenoh::Config) -> Result<Self, ServiceError> {
        let session = zenoh::open(config)
            .wait()
            .map_err(|e| ServiceError::Publish(format!("open session: {e}")))?;
        Ok(Self::new(session))
    }

    /// The bundles the fabric's `keys/**` storage holds for `rs`, as raw
    /// payloads, one per generation. Used on a release-set's first sweep so a
    /// restarted daemon continues from what it published before. An empty
    /// answer is not an error: a new release-set has no bundles yet.
    pub fn stored(&self, rs: &str) -> Result<Vec<Vec<u8>>, ServiceError> {
        let replies = self
            .session
            .get(format!("keys/{rs}/*"))
            .wait()
            .map_err(|e| ServiceError::Publish(format!("get keys/{rs}/*: {e}")))?;
        let mut out = Vec::new();
        while let Ok(Some(reply)) = replies.recv_timeout(std::time::Duration::from_secs(10)) {
            if let Ok(sample) = reply.result() {
                out.push(sample.payload().to_bytes().to_vec());
            }
        }
        Ok(out)
    }

    /// Close the session, flushing any in-flight publications.
    pub fn close(self) -> Result<(), ServiceError> {
        self.session
            .close()
            .wait()
            .map_err(|e| ServiceError::Publish(format!("close session: {e}")))
    }
}

impl BundlePublisher for ZenohBundlePublisher {
    fn publish(&self, topic: &str, payload: &[u8]) -> Result<(), ServiceError> {
        self.session
            .put(topic.to_string(), payload.to_vec())
            .wait()
            .map_err(|e| ServiceError::Publish(format!("put {topic}: {e}")))
    }
}
