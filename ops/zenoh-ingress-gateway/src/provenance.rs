use anyhow::{Context, Result};
use serde::Serialize;
use std::fs::{File, OpenOptions};
use std::io::Write;
use std::sync::Mutex;
use tracing::info;

/// One provenance record per admitted sample. This is the goat-side attribution that ADR 1020
/// requires: it binds the *verified* peering identity to the claimed original key, independent
/// of (and complementary to) the partner pod's own local audit-sink.
#[derive(Serialize, Debug)]
pub struct Provenance<'a> {
    /// RFC3339 receive timestamp (when the gateway got the sample).
    pub received_at: String,
    /// Verified identity of the peered entity (the only identity we can authenticate).
    pub peering_entity: &'a str,
    /// Stable id for this peering relationship/session.
    pub peering_id: &'a str,
    /// Key exactly as the peer presented it.
    pub original_key: &'a str,
    /// Key the gateway re-published under in the core fabric.
    pub admitted_key: &'a str,
    /// "quarantine" or "passthrough".
    pub admit_mode: &'a str,
    /// Payload size in bytes (payload itself is not duplicated here — the pod-side audit-sink and
    /// the core fabric storage hold the data; this is attribution metadata).
    pub payload_len: usize,
}

/// Append-only NDJSON provenance writer. Optional file sink plus always-on structured tracing.
pub struct ProvenanceLog {
    file: Option<Mutex<File>>,
}

impl ProvenanceLog {
    pub fn new(path: Option<&str>) -> Result<Self> {
        let file = match path {
            Some(p) => {
                let f = OpenOptions::new()
                    .create(true)
                    .append(true)
                    .open(p)
                    .with_context(|| format!("opening provenance ndjson at {p}"))?;
                Some(Mutex::new(f))
            }
            None => None,
        };
        Ok(Self { file })
    }

    /// Record one admitted sample. Emits a structured tracing line always, plus an NDJSON line
    /// if a file sink is configured. Provenance write failures are surfaced, not swallowed.
    pub fn record(&self, p: &Provenance) -> Result<()> {
        info!(
            peering_entity = p.peering_entity,
            peering_id = p.peering_id,
            original_key = p.original_key,
            admitted_key = p.admitted_key,
            admit_mode = p.admit_mode,
            payload_len = p.payload_len,
            "admitted peered sample"
        );
        if let Some(m) = &self.file {
            let line = serde_json::to_string(p).context("serialize provenance")?;
            let mut f = m.lock().expect("provenance file mutex poisoned");
            writeln!(f, "{line}").context("append provenance ndjson")?;
        }
        Ok(())
    }
}
