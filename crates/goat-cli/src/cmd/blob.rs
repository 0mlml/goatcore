// `goat blob send|get` — Phase G.5 volatile/blob large-file transfer.
//
// `volatile/<peer>/blob/**` (ADR 1032) is the non-persisted, chunked,
// manifest-verified class for moving a bounded large artifact peer-to-peer.
// Nothing under `volatile/` reaches rocksdb or the audit bucket, so there is
// no catch-up (docs/design/zenoh-volatile-class.md §5): the receiver must be
// listening before the sender publishes — this is a *streamed* transfer, not
// store-and-fetch.
//
// The chunk/reassemble/sha256 dance was hand-rolled in every publisher (the
// "12 MB published clean but `get` returns nothing" trap). This makes it a
// uniform, CLI-enforced contract: chunks of <= 1.5 MiB BLOCK-published under
// `<base>/chunk/<NNNNNNNN>`, a `<base>/manifest` carrying {chunks,size,sha256},
// and a 100 MiB reassembled ceiling that refuses loudly above the cap.

use clap::Subcommand;
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::fmt::Write as _;
use std::path::PathBuf;
use std::time::Duration;
use tokio::time::timeout;
use zenoh::qos::CongestionControl;

/// Per-chunk payload size. 1.5 MB matches the documented publisher reference
/// (docs/design/zenoh-volatile-class.md §8) so a Rust receiver interops with a
/// Python sender and vice versa. Stays well under the stock-Zenoh 16 MiB
/// single-sample ceiling (pubsub-best-practices.md §5).
const CHUNK_SIZE: usize = 1_500_000;

/// Reassembled-artifact ceiling (ADR 1032). Above this an artifact does not
/// belong on the bus — publish an external-store pointer instead (Pattern G).
const MAX_BLOB_BYTES: usize = 100 * 1024 * 1024;

#[derive(Subcommand, Debug)]
pub enum BlobCmd {
    /// Send a file as a chunked volatile blob.
    ///
    /// Splits the file into <= 1.5 MiB chunks, BLOCK-publishes each under
    /// `<to>/chunk/<NNNNNNNN>`, then publishes a `<to>/manifest` with the
    /// chunk count, byte size, and sha256. The receiver must already be
    /// running `goat blob get` — volatile keys are not retained, so a send
    /// with no live receiver is silently lost.
    Send {
        /// File to send.
        file: PathBuf,
        /// Base volatile key, e.g. `volatile/<peer>/blob/<artifact-id>`.
        #[arg(long)]
        to: String,
        /// Print what would be published without opening a session.
        #[arg(long)]
        dry_run: bool,
    },
    /// Receive a chunked volatile blob into a file.
    ///
    /// Subscribes to `<key>/**`, collects the chunks + manifest as they
    /// stream in, reassembles in order, and verifies the sha256 before
    /// writing. Start this BEFORE the sender publishes: volatile keys have no
    /// catch-up.
    Get {
        /// Base volatile key the sender published to.
        key: String,
        /// Output file path.
        #[arg(short = 'o', long)]
        output: PathBuf,
        /// Give up if the transfer doesn't complete within this many seconds.
        #[arg(long, default_value = "120")]
        timeout_secs: u64,
    },
}

/// The small JSON manifest published last, after every chunk. Field names +
/// shape match the publisher reference (zenoh-volatile-class.md §8) for
/// cross-language interop.
#[derive(serde::Serialize, serde::Deserialize, Debug)]
struct Manifest {
    chunks: usize,
    size: usize,
    sha256: String,
}

pub async fn run(ctx: &super::Ctx, op: BlobCmd) -> anyhow::Result<()> {
    match op {
        BlobCmd::Send { file, to, dry_run } => send(ctx, file, &to, dry_run).await,
        BlobCmd::Get {
            key,
            output,
            timeout_secs,
        } => get(ctx, &key, output, timeout_secs).await,
    }
}

async fn send(ctx: &super::Ctx, file: PathBuf, to: &str, dry_run: bool) -> anyhow::Result<()> {
    // Resolve the peer alias inside a `volatile/<peer>/...` key (2nd segment),
    // failing closed on an unknown alias. `shown` is the human form for status.
    let reg = crate::alias::load_aliases(&ctx.profile).await;
    let base = crate::alias::resolve_key_volatile_aware(&reg, to.trim())
        .map_err(|e| anyhow::anyhow!("{e}"))?;
    let shown = crate::alias::display_key_volatile_aware(&reg, ctx.uuid, &base);

    if !base.starts_with("volatile/") {
        eprintln!(
            "note: `{shown}` is not under `volatile/` — blob transfer is designed for the \
             non-persisted `volatile/<peer>/blob/**` class; sending anyway."
        );
    }

    let payload = tokio::fs::read(&file)
        .await
        .map_err(|e| anyhow::anyhow!("read {}: {e}", file.display()))?;
    let size = payload.len();
    if size == 0 {
        anyhow::bail!("{} is empty — nothing to send", file.display());
    }
    if size > MAX_BLOB_BYTES {
        anyhow::bail!(
            "{} is {:.1} MiB — over the {} MiB volatile/blob ceiling. A file this large does not \
             belong on the bus: publish an external-store pointer instead (pubsub-best-practices.md \
             Pattern G).",
            file.display(),
            size as f64 / 1_048_576.0,
            MAX_BLOB_BYTES / 1_048_576
        );
    }
    let n_chunks = size.div_ceil(CHUNK_SIZE);
    let sha = hex_sha256(&payload);

    if dry_run {
        eprintln!("dry-run: would publish a volatile blob to base `{shown}`");
        eprintln!("  {n_chunks} chunk(s) of <= {CHUNK_SIZE} bytes → `{shown}/chunk/00000000..` (Block)");
        eprintln!(
            "  manifest `{shown}/manifest` = {{\"chunks\":{n_chunks},\"size\":{size},\"sha256\":\"{sha}\"}}"
        );
        eprintln!("  (start `goat blob get {shown} -o <file>` on the receiver first — no catch-up)");
        return Ok(());
    }

    let session = super::pubsub::open_session_for_transfer(&ctx.profile).await?;
    for (i, chunk) in payload.chunks(CHUNK_SIZE).enumerate() {
        let key = format!("{base}/chunk/{i:08}");
        session
            .put(&key, chunk.to_vec())
            .congestion_control(CongestionControl::Block)
            .await
            .map_err(|e| anyhow::anyhow!("put chunk {i} on `{key}`: {e}"))?;
    }
    let manifest = serde_json::to_vec(&Manifest {
        chunks: n_chunks,
        size,
        sha256: sha.clone(),
    })?;
    let manifest_key = format!("{base}/manifest");
    session
        .put(&manifest_key, manifest)
        .congestion_control(CongestionControl::Block)
        .await
        .map_err(|e| anyhow::anyhow!("put manifest on `{manifest_key}`: {e}"))?;
    let _ = session.close().await;

    eprintln!(
        "sent {size} bytes ({n_chunks} chunk(s), sha256 {}…) to `{shown}`",
        &sha[..12]
    );
    Ok(())
}

async fn get(
    ctx: &super::Ctx,
    key: &str,
    output: PathBuf,
    timeout_secs: u64,
) -> anyhow::Result<()> {
    let reg = crate::alias::load_aliases(&ctx.profile).await;
    let base = crate::alias::resolve_key_volatile_aware(&reg, key.trim())
        .map_err(|e| anyhow::anyhow!("{e}"))?;
    let shown = crate::alias::display_key_volatile_aware(&reg, ctx.uuid, &base);

    let session = super::pubsub::open_session_for_transfer(&ctx.profile).await?;
    let pattern = format!("{base}/**");
    let subscriber = session
        .declare_subscriber(pattern.clone())
        .await
        .map_err(|e| anyhow::anyhow!("declare subscriber on `{pattern}`: {e}"))?;
    eprintln!("listening for blob on `{shown}` (sender must publish now; Ctrl-C to stop)…");

    let chunk_prefix = format!("{base}/chunk/");
    let manifest_key = format!("{base}/manifest");
    let mut chunks: HashMap<usize, Vec<u8>> = HashMap::new();
    let mut manifest: Option<Manifest> = None;

    let collected = timeout(Duration::from_secs(timeout_secs), async {
        loop {
            let sample = subscriber
                .recv_async()
                .await
                .map_err(|e| anyhow::anyhow!("subscriber recv error: {e}"))?;
            let k = sample.key_expr().as_str().to_string();
            let body = sample.payload().to_bytes().to_vec();
            if k == manifest_key {
                manifest = Some(
                    serde_json::from_slice(&body)
                        .map_err(|e| anyhow::anyhow!("parse manifest on `{k}`: {e}"))?,
                );
            } else if let Some(idx_s) = k.strip_prefix(&chunk_prefix) {
                if let Ok(idx) = idx_s.parse::<usize>() {
                    chunks.insert(idx, body);
                }
            }
            // Done once we have the manifest AND every chunk it names. The
            // manifest is published last, so by the time it lands the chunks
            // are usually already in hand.
            if let Some(m) = &manifest {
                if chunks.len() >= m.chunks {
                    return Ok::<(), anyhow::Error>(());
                }
            }
        }
    })
    .await;

    let _ = session.close().await;

    match collected {
        Err(_) => {
            let have_manifest = manifest.is_some();
            let want = manifest
                .as_ref()
                .map(|m| m.chunks.to_string())
                .unwrap_or_else(|| "?".into());
            anyhow::bail!(
                "timed out after {timeout_secs}s — got {}/{} chunk(s){}. volatile/blob has no \
                 catch-up: start `goat blob get` BEFORE the sender publishes.",
                chunks.len(),
                want,
                if have_manifest {
                    ""
                } else {
                    ", and no manifest"
                }
            );
        }
        Ok(Err(e)) => return Err(e),
        Ok(Ok(())) => {}
    }

    let m = manifest.expect("manifest present on success");
    let mut out = Vec::with_capacity(m.size);
    for i in 0..m.chunks {
        let chunk = chunks
            .get(&i)
            .ok_or_else(|| anyhow::anyhow!("missing chunk {i} of {}", m.chunks))?;
        out.extend_from_slice(chunk);
    }
    if out.len() != m.size {
        anyhow::bail!(
            "reassembled {} bytes but the manifest declared {} — incomplete transfer, not written",
            out.len(),
            m.size
        );
    }
    let got_sha = hex_sha256(&out);
    if got_sha != m.sha256 {
        anyhow::bail!(
            "sha256 mismatch — reassembled {got_sha}, manifest says {} — corrupt transfer, not written",
            m.sha256
        );
    }
    tokio::fs::write(&output, &out)
        .await
        .map_err(|e| anyhow::anyhow!("write {}: {e}", output.display()))?;
    eprintln!(
        "received {} bytes ({} chunk(s)) → {} (sha256 ✓ {}…)",
        m.size,
        m.chunks,
        output.display(),
        &m.sha256[..12]
    );
    Ok(())
}

/// Lowercase-hex sha256 of a buffer. Matches the manifest's `sha256` field
/// (the same `hashlib.sha256(payload).hexdigest()` the publisher reference
/// emits), so the verify is a plain string compare.
fn hex_sha256(data: &[u8]) -> String {
    let digest = Sha256::digest(data);
    let mut s = String::with_capacity(64);
    for b in digest {
        let _ = write!(s, "{b:02x}");
    }
    s
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sha256_is_lowercase_hex_of_known_vector() {
        // sha256("") well-known vector.
        assert_eq!(
            hex_sha256(b""),
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        );
    }

    #[test]
    fn chunk_count_matches_div_ceil() {
        // Exactly on the boundary and one over.
        let exact = vec![0u8; CHUNK_SIZE * 2];
        assert_eq!(exact.len().div_ceil(CHUNK_SIZE), 2);
        let over = vec![0u8; CHUNK_SIZE * 2 + 1];
        assert_eq!(over.len().div_ceil(CHUNK_SIZE), 3);
    }

    #[test]
    fn manifest_roundtrips_with_reference_field_names() {
        // The wire shape must stay {chunks,size,sha256} for cross-language
        // interop with the documented Python publisher reference.
        let m = Manifest {
            chunks: 3,
            size: 4_500_001,
            sha256: "abc123".into(),
        };
        let json = serde_json::to_string(&m).unwrap();
        assert_eq!(json, r#"{"chunks":3,"size":4500001,"sha256":"abc123"}"#);
        let back: Manifest = serde_json::from_str(&json).unwrap();
        assert_eq!(back.chunks, 3);
        assert_eq!(back.size, 4_500_001);
    }

    #[test]
    fn reassembly_in_index_order_reconstructs_payload() {
        // Out-of-order arrival (HashMap) must reassemble by index, not receipt.
        let mut chunks: HashMap<usize, Vec<u8>> = HashMap::new();
        chunks.insert(2, b"ccc".to_vec());
        chunks.insert(0, b"aaa".to_vec());
        chunks.insert(1, b"bbb".to_vec());
        let mut out = Vec::new();
        for i in 0..3 {
            out.extend_from_slice(&chunks[&i]);
        }
        assert_eq!(out, b"aaabbbccc");
    }
}
