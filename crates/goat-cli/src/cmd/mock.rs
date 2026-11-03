// `goat mock <key>` — synthetic publisher. Operator gets a single
// command for "publish something fake so I can validate plumbing"
// without writing throwaway scripts or pulling in trashgoat (the
// fleet-side synthetic-vendor service).
//
// Three modes:
//   1. Default hello-world JSON at 1Hz forever:
//        goat mock <key>
//      Sends `{"hello":"world","ts":<unix-ms>,"n":<seq>}` per tick.
//
//   2. Default protobuf with synthesized field defaults (zeros /
//      empty strings / empty repeated):
//        goat mock <key> --format <path-to.fds> --message <Name>
//
//   3. Protobuf with operator-supplied JSON seed encoded against
//      the descriptor (prost-reflect's serde adapter):
//        goat mock <key> --format <path-to.fds> --message <Name>
//          --payload-template '{"field":42,"nested":{"x":"hi"}}'
//
// Rate + count knobs cap traffic so a mistake doesn't flood the
// trial fabric.

use clap::Args;
use prost::Message;
use prost_reflect::{DescriptorPool, DynamicMessage, MessageDescriptor};
use std::path::PathBuf;
use std::time::Duration;

use crate::profile::Profile;

#[derive(Args, Debug)]
pub struct MockArgs {
    /// Topic key to publish to (e.g. `acme/sandbox/v1`).
    pub key: String,
    /// Path to a .fds (FileDescriptorSet) for protobuf encoding.
    /// Without it, sends the default hello-world JSON.
    #[arg(long)]
    pub format: Option<PathBuf>,
    /// Message name within the FDS (fully-qualified or simple).
    /// Required when --format is set.
    #[arg(long)]
    pub message: Option<String>,
    /// Operator-supplied JSON seed encoded against the message
    /// descriptor. Without it, the default protobuf path emits an
    /// all-fields-default instance. Ignored when --format is unset.
    #[arg(long)]
    pub payload_template: Option<String>,
    /// Publish rate in samples per second (default 1).
    #[arg(long, default_value = "1")]
    pub rate: f64,
    /// Send this many samples then exit. Default = forever
    /// (Ctrl-C to stop).
    #[arg(long)]
    pub count: Option<usize>,
}

pub async fn run(ctx: &super::Ctx, args: MockArgs) -> anyhow::Result<()> {
    if args.rate <= 0.0 || args.rate > 1000.0 {
        anyhow::bail!("--rate must be in (0, 1000] Hz; got {}", args.rate);
    }
    if args.format.is_some() && args.message.is_none() {
        anyhow::bail!("--message is required when --format is set");
    }
    if args.format.is_none() && (args.message.is_some() || args.payload_template.is_some()) {
        anyhow::bail!("--message / --payload-template require --format (the .fds path)");
    }

    let encoder = build_encoder(&args).await?;
    let session = open_zenoh_session(&ctx.profile).await?;
    let publisher = session
        .declare_publisher(args.key.clone())
        .await
        .map_err(|e| anyhow::anyhow!("declare publisher on `{}`: {e}", args.key))?;

    let period = Duration::from_secs_f64(1.0 / args.rate);
    let mut interval = tokio::time::interval(period);
    interval.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

    eprintln!(
        "mocking `{}` at {} Hz ({}); Ctrl-C to stop",
        args.key,
        args.rate,
        match args.count {
            Some(n) => format!("count={n}"),
            None => "forever".into(),
        }
    );

    let mut sent: usize = 0;
    loop {
        interval.tick().await;
        let payload = encoder.payload(sent)?;
        let n = payload.len();
        publisher
            .put(payload)
            .await
            .map_err(|e| anyhow::anyhow!("put on `{}`: {e}", args.key))?;
        sent += 1;
        eprintln!("sent #{sent} ({n} bytes)");
        if Some(sent) == args.count {
            break;
        }
    }

    let _ = session.close().await;
    eprintln!("done after {sent} sample(s)");
    Ok(())
}

/// Re-export the session-open helper from pubsub.rs so mock uses
/// the same client-mode TLS path. Keeping the helper definition
/// there since pub/sub depends on it too; mock is just a consumer.
async fn open_zenoh_session(profile: &Profile) -> anyhow::Result<zenoh::Session> {
    super::pubsub::open_session_for(profile).await
}

/// Encoder variant: hello-world JSON OR protobuf via a cached
/// MessageDescriptor + operator seed.
enum Encoder {
    HelloWorld,
    Protobuf {
        /// Pre-built template message — operator's JSON seed has
        /// already been deserialized into a DynamicMessage; we
        /// re-encode per sample so each carries the same shape.
        /// `n: seq` overlay isn't applied for protobuf today
        /// (operators write template explicitly when they want
        /// per-sample variance).
        template: DynamicMessage,
    },
}

impl Encoder {
    fn payload(&self, seq: usize) -> anyhow::Result<Vec<u8>> {
        match self {
            Self::HelloWorld => {
                let ts_ms = std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .map(|d| d.as_millis() as i64)
                    .unwrap_or(0);
                let body = serde_json::json!({"hello":"world","ts":ts_ms,"n":seq});
                Ok(serde_json::to_vec(&body).unwrap_or_default())
            }
            Self::Protobuf { template } => {
                let mut buf = Vec::with_capacity(template.encoded_len());
                template
                    .encode(&mut buf)
                    .map_err(|e| anyhow::anyhow!("protobuf encode: {e}"))?;
                Ok(buf)
            }
        }
    }
}

async fn build_encoder(args: &MockArgs) -> anyhow::Result<Encoder> {
    let Some(fds_path) = args.format.as_ref() else {
        return Ok(Encoder::HelloWorld);
    };
    // Bare `?` here (unlike the very next line, which names the path)
    // used to surface a missing/unreadable --format file as an unattributed
    // `No such file or directory (os error 2)`.
    use anyhow::Context;
    let bytes = tokio::fs::read(fds_path)
        .await
        .with_context(|| format!("read FDS file {}", fds_path.display()))?;
    let pool = DescriptorPool::decode(bytes.as_slice())
        .map_err(|e| anyhow::anyhow!("parse FDS {}: {e}", fds_path.display()))?;
    let name = args.message.as_ref().expect("checked in run()");
    let descriptor = select_message(&pool, name)
        .ok_or_else(|| anyhow::anyhow!("message `{name}` not in FDS {}", fds_path.display()))?;

    let template = match args.payload_template.as_ref() {
        Some(json) => {
            let mut de = serde_json::Deserializer::from_str(json);
            DynamicMessage::deserialize(descriptor.clone(), &mut de)
                .map_err(|e| anyhow::anyhow!("--payload-template not valid JSON for `{name}`: {e}"))?
        }
        None => DynamicMessage::new(descriptor.clone()),
    };
    let _ = descriptor;
    Ok(Encoder::Protobuf { template })
}

/// Mirror cmd::decode::select_message — prefer fully-qualified name,
/// then simple-name walk. Returns None when the name isn't in the
/// pool at all (caller surfaces the operator-friendly error).
fn select_message(pool: &DescriptorPool, name: &str) -> Option<MessageDescriptor> {
    if let Some(d) = pool.get_message_by_name(name) {
        return Some(d);
    }
    let found = pool.all_messages().find(|m| m.name() == name);
    found
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hello_world_encoder_includes_seq_and_hello() {
        let enc = Encoder::HelloWorld;
        let bytes = enc.payload(42).unwrap();
        let s = String::from_utf8(bytes).unwrap();
        assert!(s.contains("\"hello\":\"world\""), "got: {s}");
        assert!(s.contains("\"n\":42"), "got: {s}");
        assert!(s.contains("\"ts\":"), "got: {s}");
    }

    #[test]
    fn select_message_returns_none_for_unknown() {
        let pool = DescriptorPool::new();
        assert!(select_message(&pool, "NotThere").is_none());
    }
}
