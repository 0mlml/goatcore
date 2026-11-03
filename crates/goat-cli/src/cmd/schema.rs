// `goat schema` — first-class schema lifecycle (Phase G G.3, ADR 1038).
//
// upload / ls / show / diff. There is deliberately NO `compile`
// subcommand: the portal's uploadSchema endpoint compiles `.proto`
// source in-process when `format=protobuf-source` (bufbuild/protocompile,
// closes F-158), so `goat schema upload foo.proto` just ships the source
// and the portal builds the FileDescriptorSet. Embedding protoc/prost in
// the Rust binary would duplicate that for no gain — see ADR 1038.

use crate::narrator::{Consequence, Narrator};
use clap::Subcommand;
use panoscope_core::portal::{
    NewSchema, PortalClient, PortalError, RenderResponse, RenderedSchema,
};
use std::path::{Path, PathBuf};

/// Portal-side body cap (schemastore.MaxFileSize). We check client-side
/// so an oversized file fails before the round-trip with a clear hint
/// rather than the portal's 413.
const MAX_SCHEMA_BYTES: u64 = 1 << 20; // 1 MiB

#[derive(Subcommand, Debug)]
pub enum SchemaCmd {
    /// Upload a schema body for a topic. `.proto` source is compiled by
    /// the portal on ingress; `.fds` (a serialized FileDescriptorSet,
    /// e.g. from `build-fds.sh` for a multi-file org source) is stored
    /// as-is. Format is inferred from the extension unless `--format` is
    /// given.
    Upload {
        /// Topic key (e.g. `acme/observations/v1`).
        key: String,
        /// Path to the schema file (`.proto` source or `.fds`).
        file: PathBuf,
        /// Version label, must match `^v\d+$` (e.g. `v1`, `v2`).
        #[arg(long)]
        version: String,
        /// Compatibility declaration: backward | forward | none.
        #[arg(long, default_value = "none")]
        compat: String,
        /// Override the inferred wire format: protobuf-source | protobuf-fds.
        #[arg(long)]
        format: Option<String>,
        /// Don't POST — print what would be uploaded and exit 0.
        #[arg(long)]
        dry_run: bool,
    },
    /// List the schema versions registered for a topic.
    Ls {
        /// Topic key (e.g. `acme/observations/v1`).
        key: String,
        /// Emit one JSON line per schema row.
        #[arg(long)]
        json: bool,
    },
    /// Render a topic's schema body as a readable message/field tree.
    Show {
        /// Topic key (e.g. `acme/observations/v1`).
        key: String,
        /// Schema version to render (default: latest).
        #[arg(long)]
        version: Option<String>,
        /// Print the reconstructed `.proto` source instead of the tree.
        #[arg(long)]
        proto: bool,
        /// Emit the raw render JSON.
        #[arg(long)]
        json: bool,
    },
    /// Diff two schema versions' message + field sets.
    Diff {
        /// Topic key (e.g. `acme/observations/v1`).
        key: String,
        /// Baseline version (e.g. `v1`).
        from: String,
        /// Compared version (e.g. `v2`).
        to: String,
        /// Emit the diff as JSON.
        #[arg(long)]
        json: bool,
    },
}

pub async fn run(ctx: &super::Ctx, op: SchemaCmd) -> anyhow::Result<()> {
    // Upload --dry-run needs no portal: it validates the args + file
    // shape an operator is about to POST. Matches `topic create`'s
    // structural dry-run (ADR 1019 §12).
    if let SchemaCmd::Upload {
        key,
        file,
        version,
        compat,
        format,
        dry_run: true,
    } = &op
    {
        return upload_dry_run(key, file, version, compat, format.as_deref());
    }

    let portal = ctx.profile.portal_client()?;
    match op {
        SchemaCmd::Upload {
            key,
            file,
            version,
            compat,
            format,
            dry_run,
        } => {
            upload(
                &portal,
                &key,
                &file,
                &version,
                &compat,
                format.as_deref(),
                dry_run,
                ctx.narrate,
                ctx.narrate_json,
            )
            .await
        }
        SchemaCmd::Ls { key, json } => ls(&portal, &key, json).await,
        SchemaCmd::Show {
            key,
            version,
            proto,
            json,
        } => show(&portal, &key, version.as_deref(), proto, json).await,
        SchemaCmd::Diff {
            key,
            from,
            to,
            json,
        } => diff(&portal, &key, &from, &to, json).await,
    }
}

// ── upload ──────────────────────────────────────────────────────────

#[allow(clippy::too_many_arguments)]
async fn upload(
    portal: &PortalClient,
    key: &str,
    file: &Path,
    version: &str,
    compat: &str,
    format: Option<&str>,
    dry_run: bool,
    narrate: bool,
    narrate_json: bool,
) -> anyhow::Result<()> {
    if dry_run {
        return upload_dry_run(key, file, version, compat, format);
    }
    validate_version(version)?;
    validate_compat(compat)?;
    let fmt = resolve_format(file, format)?;

    let bytes = std::fs::read(file)
        .map_err(|e| anyhow::anyhow!("read schema file `{}`: {e}", file.display()))?;
    if bytes.len() as u64 > MAX_SCHEMA_BYTES {
        anyhow::bail!(
            "schema file is {} bytes; portal cap is {} bytes (1 MiB). For a multi-file \
             protobuf source, compile to a single `.fds` with build-fds.sh and upload that.",
            bytes.len(),
            MAX_SCHEMA_BYTES
        );
    }

    let topic = resolve_topic(portal, key).await?;
    let file_name = file
        .file_name()
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_else(|| "schema".to_string());

    let narrator = Narrator::new(narrate, narrate_json);
    let row = portal
        .upload_schema(
            &topic.vendor_prefix,
            topic.id,
            NewSchema {
                file_name,
                bytes,
                version: version.to_string(),
                compatibility: compat.to_string(),
                format: fmt.to_string(),
            },
        )
        .await
        .map_err(super::vendor::map_portal_err)?;

    println!(
        "uploaded schema `{key}` {} ({}, {} bytes, sha256={})",
        row.version,
        if row.format.is_empty() { fmt } else { &row.format },
        row.size_bytes,
        short_sha(&row.sha256),
    );
    if narrator.is_enabled() {
        narrator.emit(Consequence::PortalConfirmed {
            method: "POST",
            path: format!(
                "/api/orgs/{}/topics/{}/schemas",
                topic.vendor_prefix, topic.id
            ),
            http_status: 201,
            outcome: format!("Uploaded {} ({})", row.version, fmt),
        });
        narrator.settled("schema-upload");
    }
    crate::fun::hint(&format!(
        "render it with `goat schema show {key}`, or diff against a prior version with `goat schema diff {key} <from> {}`",
        row.version
    ));
    Ok(())
}

/// Pure preview path — no portal, no topic lookup. Validates the args +
/// resolves the format an operator would send so they can sanity-check
/// before running `goat profile init`.
fn upload_dry_run(
    key: &str,
    file: &Path,
    version: &str,
    compat: &str,
    format: Option<&str>,
) -> anyhow::Result<()> {
    validate_version(version)?;
    validate_compat(compat)?;
    let fmt = resolve_format(file, format)?;
    let vendor = vendor_prefix_from_key(key)?;
    eprintln!("dry-run: POST <portal>/api/orgs/{vendor}/topics/<id>/schemas");
    eprintln!(
        "  multipart: version={version} compatibility={compat} format={fmt} file={}",
        file.display()
    );
    if fmt == "protobuf-source" {
        eprintln!("  note: portal compiles the .proto source to a FileDescriptorSet on ingress");
    }
    Ok(())
}

// ── ls ──────────────────────────────────────────────────────────────

async fn ls(portal: &PortalClient, key: &str, json: bool) -> anyhow::Result<()> {
    let topic = resolve_topic(portal, key).await?;
    let mut rows = portal
        .list_schemas(&topic.vendor_prefix, topic.id)
        .await
        .map_err(super::vendor::map_portal_err)?;
    rows.sort_by(|a, b| a.version.cmp(&b.version));

    if json {
        for r in &rows {
            println!("{}", serde_json::to_string(r).unwrap_or_default());
        }
        return Ok(());
    }
    if rows.is_empty() {
        println!("(no schema body uploaded for `{key}` — `goat schema upload {key} <file> --version v1`)");
        return Ok(());
    }
    println!("{:<8}  {:<16}  {:<12}  {:>10}  sha256", "version", "format", "compat", "bytes");
    for r in &rows {
        println!(
            "{:<8}  {:<16}  {:<12}  {:>10}  {}",
            r.version,
            if r.format.is_empty() { "(none)" } else { &r.format },
            if r.compatibility.is_empty() { "(none)" } else { &r.compatibility },
            r.size_bytes,
            short_sha(&r.sha256),
        );
    }
    Ok(())
}

// ── show ────────────────────────────────────────────────────────────

async fn show(
    portal: &PortalClient,
    key: &str,
    version: Option<&str>,
    proto: bool,
    json: bool,
) -> anyhow::Result<()> {
    let resp = portal
        .render_schema(key, version)
        .await
        .map_err(|e| map_render_err(e, key, version))?;

    if json {
        println!("{}", serde_json::to_string(&render_summary(&resp)).unwrap_or_default());
        return Ok(());
    }
    if proto {
        if resp.proto_source.is_empty() {
            println!("(portal returned no reconstructed .proto source)");
        } else {
            // Concatenate the per-file map in name order. Multi-file
            // descriptors get a `// file:` banner so the boundaries are
            // legible; a single file prints bare.
            let multi = resp.proto_source.len() > 1;
            for (i, (name, body)) in resp.proto_source.iter().enumerate() {
                if multi {
                    if i > 0 {
                        println!();
                    }
                    println!("// file: {name}");
                }
                print!("{body}");
            }
        }
        return Ok(());
    }
    render_pretty(&resp);
    Ok(())
}

/// Render the message/enum tree to stdout. Pure formatting over the
/// deserialized response — no I/O.
fn render_pretty(resp: &RenderResponse) {
    println!("key:             {}", resp.key);
    println!("version:         {}", resp.schema.version);
    if !resp.schema.all_versions.is_empty() {
        println!("all_versions:    {}", resp.schema.all_versions.join(", "));
    }
    println!(
        "format:          {}",
        if resp.format.is_empty() { "(none)" } else { &resp.format }
    );
    if !resp.primary_message.is_empty() {
        println!("primary_message: {}", resp.primary_message);
    }
    println!();

    for m in &resp.rendered.messages {
        let star = if m.name == resp.primary_message || m.full_name == resp.primary_message {
            " *"
        } else {
            ""
        };
        println!("message {}{}", m.full_name, star);
        for f in &m.fields {
            let ty = if f.type_ref.is_empty() { &f.ty } else { &f.type_ref };
            let card = if f.cardinality == "singular" {
                String::new()
            } else {
                format!("{} ", f.cardinality)
            };
            println!("  {:>3}  {}{}  {}", f.number, card, ty, f.name);
        }
        println!();
    }
    for e in &resp.rendered.enums {
        println!("enum {}", e.full_name);
        for v in &e.values {
            println!("  {:>3}  {}", v.number, v.name);
        }
        println!();
    }
}

// ── diff ────────────────────────────────────────────────────────────

async fn diff(
    portal: &PortalClient,
    key: &str,
    from: &str,
    to: &str,
    json: bool,
) -> anyhow::Result<()> {
    let a = portal
        .render_schema(key, Some(from))
        .await
        .map_err(|e| map_render_err(e, key, Some(from)))?;
    let b = portal
        .render_schema(key, Some(to))
        .await
        .map_err(|e| map_render_err(e, key, Some(to)))?;
    let d = diff_schemas(&a.rendered, &b.rendered);

    if json {
        println!("{}", serde_json::to_string(&d).unwrap_or_default());
        return Ok(());
    }
    println!("diff {key}: {from} → {to}");
    if d.is_empty() {
        println!("  (no message/field changes)");
        return Ok(());
    }
    for m in &d.added_messages {
        println!("  + message {m}");
    }
    for m in &d.removed_messages {
        println!("  - message {m}");
    }
    for mc in &d.changed_messages {
        println!("  ~ message {}", mc.full_name);
        for f in &mc.added_fields {
            println!("      + {f}");
        }
        for f in &mc.removed_fields {
            println!("      - {f}");
        }
        for f in &mc.changed_fields {
            println!("      ~ {f}");
        }
    }
    Ok(())
}

// ── pure helpers (unit-tested) ───────────────────────────────────────

/// Infer the portal `format` value from the file extension unless the
/// operator supplied one. `.proto` → `protobuf-source` (portal compiles
/// it); everything else (`.fds`, `.pb`, `.desc`) → `protobuf-fds`. An
/// explicit `--format` must be one of the two accepted values.
fn resolve_format<'a>(file: &Path, explicit: Option<&'a str>) -> anyhow::Result<&'a str> {
    if let Some(f) = explicit {
        return match f {
            "protobuf-source" => Ok("protobuf-source"),
            "protobuf-fds" => Ok("protobuf-fds"),
            other => anyhow::bail!(
                "--format `{other}` invalid; must be protobuf-source | protobuf-fds"
            ),
        };
    }
    let ext = file
        .extension()
        .and_then(|e| e.to_str())
        .unwrap_or("")
        .to_ascii_lowercase();
    Ok(if ext == "proto" {
        "protobuf-source"
    } else {
        "protobuf-fds"
    })
}

/// Validate a version label matches the portal's `^v\d+$` rule, so the
/// operator gets the failure locally instead of a 400 round-trip.
fn validate_version(v: &str) -> anyhow::Result<()> {
    let ok = v.len() >= 2
        && v.starts_with('v')
        && v[1..].chars().all(|c| c.is_ascii_digit());
    if !ok {
        anyhow::bail!("version `{v}` invalid; must be v<N> (e.g. v1, v2)");
    }
    Ok(())
}

/// Validate the compatibility enum (backward | forward | none).
fn validate_compat(c: &str) -> anyhow::Result<()> {
    match c {
        "backward" | "forward" | "none" => Ok(()),
        other => anyhow::bail!("compat `{other}` invalid; must be backward | forward | none"),
    }
}

/// First slash-segment of the topic key (the vendor prefix the portal
/// scopes the upload to). Errors on an empty/slashless key.
fn vendor_prefix_from_key(key: &str) -> anyhow::Result<&str> {
    let v = key.split('/').next().unwrap_or("");
    if v.is_empty() {
        anyhow::bail!("topic key must include an org prefix, e.g. `acme/topic/v1`");
    }
    Ok(v)
}

fn short_sha(sha: &str) -> &str {
    if sha.len() > 12 {
        &sha[..12]
    } else {
        sha
    }
}

/// A schema diff as added/removed messages + per-message field changes.
#[derive(Debug, Default, serde::Serialize)]
struct SchemaDiff {
    added_messages: Vec<String>,
    removed_messages: Vec<String>,
    changed_messages: Vec<MessageDiff>,
}

#[derive(Debug, Default, serde::Serialize)]
struct MessageDiff {
    full_name: String,
    added_fields: Vec<String>,
    removed_fields: Vec<String>,
    changed_fields: Vec<String>,
}

impl SchemaDiff {
    fn is_empty(&self) -> bool {
        self.added_messages.is_empty()
            && self.removed_messages.is_empty()
            && self.changed_messages.is_empty()
    }
}

/// Compare two rendered schemas. Messages are keyed by full name; for
/// messages present in both, fields are keyed by name and compared on
/// number + type (a rename reads as a remove + an add, which is the
/// honest wire-compat story). Pure — the unit tests drive it directly.
fn diff_schemas(a: &RenderedSchema, b: &RenderedSchema) -> SchemaDiff {
    use std::collections::BTreeMap;
    let am: BTreeMap<&str, &panoscope_core::portal::RenderedMessage> =
        a.messages.iter().map(|m| (m.full_name.as_str(), m)).collect();
    let bm: BTreeMap<&str, &panoscope_core::portal::RenderedMessage> =
        b.messages.iter().map(|m| (m.full_name.as_str(), m)).collect();

    let mut out = SchemaDiff::default();
    for name in bm.keys() {
        if !am.contains_key(name) {
            out.added_messages.push((*name).to_string());
        }
    }
    for name in am.keys() {
        if !bm.contains_key(name) {
            out.removed_messages.push((*name).to_string());
        }
    }
    for (name, ma) in &am {
        let Some(mb) = bm.get(name) else { continue };
        let mc = diff_message(name, ma, mb);
        if !mc.added_fields.is_empty()
            || !mc.removed_fields.is_empty()
            || !mc.changed_fields.is_empty()
        {
            out.changed_messages.push(mc);
        }
    }
    out
}

fn field_label(f: &panoscope_core::portal::RenderedField) -> String {
    let ty = if f.type_ref.is_empty() { &f.ty } else { &f.type_ref };
    let card = if f.cardinality == "singular" {
        String::new()
    } else {
        format!("{} ", f.cardinality)
    };
    format!("{} (#{} {}{})", f.name, f.number, card, ty)
}

fn diff_message(
    name: &str,
    a: &panoscope_core::portal::RenderedMessage,
    b: &panoscope_core::portal::RenderedMessage,
) -> MessageDiff {
    use std::collections::BTreeMap;
    let af: BTreeMap<&str, &panoscope_core::portal::RenderedField> =
        a.fields.iter().map(|f| (f.name.as_str(), f)).collect();
    let bf: BTreeMap<&str, &panoscope_core::portal::RenderedField> =
        b.fields.iter().map(|f| (f.name.as_str(), f)).collect();

    let mut mc = MessageDiff {
        full_name: name.to_string(),
        ..Default::default()
    };
    for (fname, fb) in &bf {
        match af.get(fname) {
            None => mc.added_fields.push(field_label(fb)),
            Some(fa) => {
                let ta = if fa.type_ref.is_empty() { &fa.ty } else { &fa.type_ref };
                let tb = if fb.type_ref.is_empty() { &fb.ty } else { &fb.type_ref };
                if fa.number != fb.number || ta != tb || fa.cardinality != fb.cardinality {
                    mc.changed_fields
                        .push(format!("{}: {} → {}", fname, field_label(fa), field_label(fb)));
                }
            }
        }
    }
    for fname in af.keys() {
        if !bf.contains_key(fname) {
            mc.removed_fields.push(field_label(af[fname]));
        }
    }
    mc
}

/// A compact JSON projection of a render response for `show --json` —
/// the full upstream payload (examples, per-file proto source) is
/// noise for a CLI consumer; this is the message/enum substance.
fn render_summary(resp: &RenderResponse) -> serde_json::Value {
    serde_json::json!({
        "key": resp.key,
        "vendor": resp.vendor,
        "format": resp.format,
        "primary_message": resp.primary_message,
        "version": resp.schema.version,
        "all_versions": resp.schema.all_versions,
        "messages": resp.rendered.messages.iter().map(|m| serde_json::json!({
            "full_name": m.full_name,
            "name": m.name,
            "fields": m.fields.iter().map(|f| serde_json::json!({
                "name": f.name,
                "number": f.number,
                "cardinality": f.cardinality,
                "type": if f.type_ref.is_empty() { f.ty.clone() } else { f.type_ref.clone() },
            })).collect::<Vec<_>>(),
        })).collect::<Vec<_>>(),
        "enums": resp.rendered.enums.iter().map(|e| serde_json::json!({
            "full_name": e.full_name,
            "values": e.values.iter().map(|v| serde_json::json!({"name": v.name, "number": v.number})).collect::<Vec<_>>(),
        })).collect::<Vec<_>>(),
    })
}

// ── shared portal glue ───────────────────────────────────────────────

/// Resolve a topic key to its `(vendor, id)` row, with a not-registered
/// hint pointing at `goat topic ls <vendor>`.
async fn resolve_topic(
    portal: &PortalClient,
    key: &str,
) -> anyhow::Result<panoscope_core::portal::Topic> {
    portal
        .find_topic_by_key(key)
        .await
        .map_err(super::vendor::map_portal_err)?
        .ok_or_else(|| {
            anyhow::anyhow!(
                "topic `{key}` not registered — create it first with \
                 `goat topic create {key} --format protobuf:<Msg>`"
            )
        })
}

/// Map a render NotFound to the schema-specific "no body uploaded" hint
/// instead of the generic vendor/topic-not-found message.
fn map_render_err(e: PortalError, key: &str, version: Option<&str>) -> anyhow::Error {
    match e {
        PortalError::NotFound { .. } => {
            let v = version.map(|v| format!(" version {v}")).unwrap_or_default();
            anyhow::anyhow!(
                "no schema body{v} for `{key}` — `goat schema ls {key}` shows what's uploaded"
            )
        }
        other => super::vendor::map_portal_err(other),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use panoscope_core::portal::{RenderedField, RenderedMessage, RenderedSchema};

    fn field(name: &str, number: i32, card: &str, ty: &str) -> RenderedField {
        RenderedField {
            name: name.to_string(),
            number,
            cardinality: card.to_string(),
            ty: ty.to_string(),
            type_ref: String::new(),
        }
    }

    fn msg(full: &str, fields: Vec<RenderedField>) -> RenderedMessage {
        RenderedMessage {
            full_name: full.to_string(),
            name: full.rsplit('.').next().unwrap_or(full).to_string(),
            fields,
        }
    }

    #[test]
    fn format_inferred_from_proto_extension() {
        assert_eq!(resolve_format(Path::new("foo.proto"), None).unwrap(), "protobuf-source");
    }

    #[test]
    fn format_inferred_from_fds_extension() {
        assert_eq!(resolve_format(Path::new("foo.fds"), None).unwrap(), "protobuf-fds");
        assert_eq!(resolve_format(Path::new("foo.pb"), None).unwrap(), "protobuf-fds");
    }

    #[test]
    fn format_extension_is_case_insensitive() {
        assert_eq!(resolve_format(Path::new("FOO.PROTO"), None).unwrap(), "protobuf-source");
    }

    #[test]
    fn explicit_format_overrides_extension() {
        assert_eq!(
            resolve_format(Path::new("foo.proto"), Some("protobuf-fds")).unwrap(),
            "protobuf-fds"
        );
    }

    #[test]
    fn explicit_format_rejects_unknown() {
        assert!(resolve_format(Path::new("foo.proto"), Some("json")).is_err());
    }

    #[test]
    fn version_accepts_v_then_digits() {
        validate_version("v1").unwrap();
        validate_version("v12").unwrap();
    }

    #[test]
    fn version_rejects_malformed() {
        assert!(validate_version("1").is_err());
        assert!(validate_version("v").is_err());
        assert!(validate_version("vX").is_err());
        assert!(validate_version("ver1").is_err());
    }

    #[test]
    fn compat_enum_enforced() {
        for ok in ["backward", "forward", "none"] {
            validate_compat(ok).unwrap();
        }
        assert!(validate_compat("full").is_err());
    }

    #[test]
    fn vendor_prefix_extracted() {
        assert_eq!(vendor_prefix_from_key("acme/foo/v1").unwrap(), "acme");
        assert!(vendor_prefix_from_key("").is_err());
    }

    #[test]
    fn short_sha_truncates() {
        assert_eq!(short_sha("0123456789abcdef"), "0123456789ab");
        assert_eq!(short_sha("short"), "short");
    }

    #[test]
    fn diff_identical_is_empty() {
        let s = RenderedSchema {
            messages: vec![msg("Track", vec![field("id", 1, "singular", "string")])],
            ..Default::default()
        };
        assert!(diff_schemas(&s, &s).is_empty());
    }

    #[test]
    fn diff_detects_added_and_removed_messages() {
        let a = RenderedSchema {
            messages: vec![msg("Track", vec![])],
            ..Default::default()
        };
        let b = RenderedSchema {
            messages: vec![msg("Plot", vec![])],
            ..Default::default()
        };
        let d = diff_schemas(&a, &b);
        assert_eq!(d.added_messages, vec!["Plot"]);
        assert_eq!(d.removed_messages, vec!["Track"]);
    }

    #[test]
    fn diff_detects_field_add_remove_change() {
        let a = RenderedSchema {
            messages: vec![msg(
                "Track",
                vec![
                    field("id", 1, "singular", "string"),
                    field("lat", 2, "singular", "double"),
                ],
            )],
            ..Default::default()
        };
        let b = RenderedSchema {
            messages: vec![msg(
                "Track",
                vec![
                    field("id", 1, "singular", "string"),
                    field("lat", 2, "singular", "float"), // type changed
                    field("alt", 3, "singular", "double"), // added
                ],
            )],
            ..Default::default()
        };
        let d = diff_schemas(&a, &b);
        assert!(d.added_messages.is_empty());
        assert_eq!(d.changed_messages.len(), 1);
        let mc = &d.changed_messages[0];
        assert_eq!(mc.full_name, "Track");
        assert_eq!(mc.added_fields.len(), 1);
        assert!(mc.added_fields[0].contains("alt"));
        assert_eq!(mc.changed_fields.len(), 1);
        assert!(mc.changed_fields[0].contains("lat"));
        assert!(mc.removed_fields.is_empty());
    }
}
