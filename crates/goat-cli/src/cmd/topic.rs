use crate::narrator::{
    derive_stethoscope_from_portal, observe_stethoscope_check, Consequence, Narrator,
};
use clap::Subcommand;
use panoscope_core::portal::{ImportDiff, NewTopic, PortalClient, Topic};
use std::io::{Read, Write};
use std::path::PathBuf;
use std::time::{Duration, Instant};

/// Best-effort deadline for stethoscope post-mutation observation in
/// `--narrate`. Short enough that a down stethoscope doesn't block the
/// command; long enough that a live one is likely to have run the
/// relevant check on a freshly-mutated topic.
const NARRATE_STETHOSCOPE_DEADLINE: Duration = Duration::from_secs(3);

/// Client-side rate limiter for `goat topic create` so a scripted loop
/// can't accidentally DoS the portal. Per ADR 1019 §"Residual risk".
const CREATE_MIN_INTERVAL: Duration = Duration::from_secs(5);

#[derive(Subcommand, Debug)]
pub enum TopicCmd {
    /// List topics. Optional prefix arg drilldown (e.g. `org11x` or
    /// `org11x/observations`); without it, lists across all orgs.
    Ls {
        /// Optional org or org/system prefix for hierarchical drilldown.
        prefix: Option<String>,
        /// Fuzzy-match against topic key + description (overrides prefix drilldown).
        #[arg(long)]
        search: Option<String>,
        /// Emit one JSON line per topic.
        #[arg(long)]
        json: bool,
    },
    /// Show one topic's full record (key, format, schema, last publish).
    Show {
        /// Topic key (e.g. `org11x/observations/v1`).
        key: String,
        /// Emit JSON instead of pretty.
        #[arg(long)]
        json: bool,
    },
    /// Create a new topic in the portal. Requires the profile's Bearer
    /// token to have org-prefix authority server-side.
    Create {
        /// Topic key (e.g. `org11x/observations/v1`). The leading
        /// slash segment is the org prefix the portal POST targets.
        key: String,
        /// Encoding format declaration (e.g. `protobuf:MyMessage` or `json`).
        #[arg(long)]
        format: String,
        /// One-line description.
        #[arg(long)]
        description: Option<String>,
        /// Path to a schema file (e.g. .proto). Optional — a topic can
        /// be registered with just a format declaration. Schema-body
        /// upload is not yet available.
        #[arg(long)]
        schema_file: Option<PathBuf>,
        /// Don't actually POST — print what would happen and exit 0.
        #[arg(long)]
        dry_run: bool,
    },
    /// Delete a topic. Default is soft delete (portal-side semantics);
    /// `--hard` passes `?hard=true` and the portal may refuse based on
    /// audit-tail retention policy.
    Delete {
        /// Topic key (e.g. `org11x/observations/v1`).
        key: String,
        /// Hard delete (default is soft / deprecate).
        #[arg(long)]
        hard: bool,
        /// Skip confirmation prompt.
        #[arg(long, short = 'y')]
        yes: bool,
        /// Don't actually DELETE — print what would happen and exit 0.
        #[arg(long)]
        dry_run: bool,
    },
    /// Export an org's topics as a bulk file (the interactive
    /// state-as-code on-ramp). Prints to stdout; redirect to a file.
    Export {
        /// Vendor prefix whose topics to export.
        vendor: String,
        /// Output format: yaml (default) or json.
        #[arg(long, default_value = "yaml", value_parser = ["yaml", "json"])]
        format: String,
    },
    /// Import topics from a bulk file (the shape `export` produces).
    /// Reads from a file or stdin. `--dry-run` previews the diff
    /// (create/update/unchanged/retire) without applying.
    Import {
        /// Org prefix to import into. Must match the file's `org` (legacy `vendor:` files accepted).
        vendor: String,
        /// Path to the bulk file; omit (or `-`) to read from stdin.
        #[arg(long)]
        file: Option<PathBuf>,
        /// Body format: yaml (default) or json. Sets the Content-Type.
        #[arg(long, default_value = "yaml", value_parser = ["yaml", "json"])]
        format: String,
        /// additive (create/update only, default) or replace (also
        /// retire topics absent from the file).
        #[arg(long, default_value = "additive", value_parser = ["additive", "replace"])]
        mode: String,
        /// Preview the diff without applying it.
        #[arg(long)]
        dry_run: bool,
    },
}

pub async fn run(ctx: &super::Ctx, op: TopicCmd) -> anyhow::Result<()> {
    // Dry-run mutators don't need a portal client — let them run with
    // no profile so an operator can validate the body shape they're
    // about to POST without first running `goat profile init`. This
    // matches ADR 1019 §12 ("Mutator trait makes `--dry-run` structural").
    match op {
        TopicCmd::Create {
            key,
            format,
            description,
            schema_file,
            dry_run: true,
        } => {
            create_dry_run(&key, &format, description.as_deref(), schema_file.as_deref())
        }
        TopicCmd::Delete {
            key: _,
            hard: _,
            yes: _,
            dry_run: true,
        } => {
            // Delete dry-run still needs to look the topic up by key
            // to render the resolved id; without portal access we
            // can't do that. Fall through to the portal-backed path
            // and let portal-missing fail with the usual hint.
            let portal = ctx.profile.portal_client()?;
            if let TopicCmd::Delete {
                key,
                hard,
                yes,
                dry_run,
            } = op
            {
                return delete(&portal, &key, hard, yes, dry_run, ctx.narrate, ctx.narrate_json)
                    .await;
            }
            unreachable!()
        }
        op => {
            let portal = ctx.profile.portal_client()?;
            match op {
                TopicCmd::Ls {
                    prefix,
                    search,
                    json,
                } => ls(&portal, prefix.as_deref(), search.as_deref(), json).await,
                TopicCmd::Show { key, json } => show(ctx, &portal, &key, json).await,
                TopicCmd::Create {
                    key,
                    format,
                    description,
                    schema_file,
                    dry_run,
                } => {
                    create(
                        &portal,
                        &key,
                        &format,
                        description.as_deref(),
                        schema_file.as_deref(),
                        dry_run,
                        ctx.narrate,
                        ctx.narrate_json,
                    )
                    .await
                }
                TopicCmd::Delete {
                    key,
                    hard,
                    yes,
                    dry_run,
                } => delete(&portal, &key, hard, yes, dry_run, ctx.narrate, ctx.narrate_json).await,
                TopicCmd::Export { vendor, format } => export(&portal, &vendor, &format).await,
                TopicCmd::Import {
                    vendor,
                    file,
                    format,
                    mode,
                    dry_run,
                } => import(&portal, &vendor, file.as_deref(), &format, &mode, dry_run).await,
            }
        }
    }
}

/// Pure dry-run path for `topic create`. Doesn't require a portal —
/// validates the key shape and renders the body the operator would
/// POST. Lifted out of `create()` so an unauth'd operator can preview
/// their command before running `goat profile init`.
fn create_dry_run(
    key: &str,
    format: &str,
    description: Option<&str>,
    schema_file: Option<&std::path::Path>,
) -> anyhow::Result<()> {
    let vendor = vendor_prefix_from_key(key)?;
    if schema_file.is_some() {
        eprintln!(
            "note: --schema-file is accepted but schema-body upload is not yet \
             available. Topic would be created without schema body."
        );
    }
    let req = build_new_topic(key, format, description);
    eprintln!("dry-run: POST <portal>/api/orgs/{vendor}/topics");
    eprintln!("  body: {}", serde_json::to_string(&req).unwrap_or_default());
    Ok(())
}

/// Extract the vendor-prefix segment (first slash-delimited token).
/// Errors with a structural-error message when the key is empty or
/// has no slash — both invalid for portal POST.
fn vendor_prefix_from_key(key: &str) -> anyhow::Result<&str> {
    let v = key.split('/').next().unwrap_or("");
    if v.is_empty() {
        anyhow::bail!("topic key must include an org prefix, e.g. `acme/topic/v1`");
    }
    Ok(v)
}

/// Build the `NewTopic` POST body from operator-supplied args. Pure
/// constructor — no side effects, no portal calls — so tests can lock
/// in the wire shape without a portal mock.
fn build_new_topic(key: &str, format: &str, description: Option<&str>) -> NewTopic {
    NewTopic {
        key_pattern: key.to_string(),
        format: format.to_string(),
        schema_ref: String::new(),
        description: description.unwrap_or("").to_string(),
        publish_to_storage: false,
    }
}

async fn ls(
    portal: &PortalClient,
    prefix: Option<&str>,
    search: Option<&str>,
    json: bool,
) -> anyhow::Result<()> {
    // Without a prefix, the whole goatnet in ONE request (`GET /api/catalog`):
    // a walk of every org is one request per org, which the
    // portal's rate limit refuses partway through (ADR 1123). A portal that
    // predates the catalog answers 404 and gets the walk. With a prefix, its
    // leading slash-segment is the org and that org alone is read.
    let catalog = match prefix {
        None => portal.catalog().await.map_err(super::vendor::map_portal_err)?,
        Some(_) => None,
    };
    let mut all: Vec<Topic> = match catalog {
        Some(cat) => catalog_topics(cat),
        None => {
            let vendors = portal
                .list_vendors()
                .await
                .map_err(super::vendor::map_portal_err)?;
            let target_vendors: Vec<String> = match prefix {
                Some(p) => {
                    let v = p.split('/').next().unwrap_or("").to_string();
                    if !vendors.iter().any(|x| x.prefix == v) {
                        anyhow::bail!("org `{v}` not registered — `goat org ls` shows what is");
                    }
                    vec![v]
                }
                None => vendors.iter().map(|v| v.prefix.clone()).collect(),
            };
            let mut all = Vec::new();
            for v in &target_vendors {
                match portal.list_topics(v).await {
                    Ok(t) => all.extend(t),
                    Err(panoscope_core::portal::PortalError::NotFound { .. }) => continue,
                    Err(e) => return Err(super::vendor::map_portal_err(e)),
                }
            }
            all
        }
    };
    if let Some(p) = prefix {
        all.retain(|t| t.key_pattern.starts_with(p));
    }
    if let Some(s) = search {
        let needle = s.to_lowercase();
        all.retain(|t| {
            t.key_pattern.to_lowercase().contains(&needle)
                || t.description.to_lowercase().contains(&needle)
        });
    }
    all.sort_by(|a, b| a.key_pattern.cmp(&b.key_pattern));

    if json {
        for t in &all {
            let line = serde_json::to_string(t).unwrap_or_default();
            println!("{line}");
        }
        return Ok(());
    }
    if all.is_empty() {
        println!("(no topics)");
        return Ok(());
    }
    let key_w = all.iter().map(|t| t.key_pattern.len()).max().unwrap_or(8).max(8);
    let fmt_w = all.iter().map(|t| t.format.len()).max().unwrap_or(6).max(6);
    println!(
        "{:<kw$}  {:<fw$}  description",
        "key", "format", kw = key_w, fw = fmt_w
    );
    for t in &all {
        println!(
            "{:<kw$}  {:<fw$}  {}",
            t.key_pattern,
            if t.format.is_empty() { "(none)" } else { &t.format },
            t.description,
            kw = key_w,
            fw = fmt_w
        );
    }
    Ok(())
}

/// The catalog's rows in the shape `goat topic ls` prints and `--json` emits.
/// A portal that predates the catalog's `id` field gives 0.
fn catalog_topics(cat: panoscope_core::portal::Catalog) -> Vec<Topic> {
    cat.orgs
        .into_iter()
        .flat_map(|o| {
            let org = o.prefix;
            o.topics.into_iter().map(move |t| Topic {
                id: t.id,
                vendor_prefix: org.clone(),
                key_pattern: t.key_pattern,
                format: t.format,
                schema_ref: t.schema_ref,
                description: t.description,
                publish_count: t.publish_count,
                last_seen_at: t.last_seen_at,
                created_at: t.created_at,
            })
        })
        .collect()
}

/// The human view deliberately does NOT print `publish_count`. That column
/// is created `DEFAULT 0` and no portal `UPDATE` ever writes it, so it read
/// as "nothing has published here" when it actually meant "nobody counts".
/// In its place: a real last-publish, resolved through the same layered
/// observers as `goat flow` (ADR 1121) — panoscope plus the registry's own
/// `last_seen_at`, which is one extra round-trip, not a fabric-wide sweep.
/// The `--json` shape is untouched: `publish_count` still serializes, so no
/// consumer breaks.
async fn show(
    ctx: &super::Ctx,
    portal: &PortalClient,
    key: &str,
    json: bool,
) -> anyhow::Result<()> {
    let topic = portal
        .find_topic_by_key(key)
        .await
        .map_err(super::vendor::map_portal_err)?
        .ok_or_else(|| {
            anyhow::anyhow!(
                "topic `{key}` not registered — `goat topic ls {}` lists this org's topics",
                key.split('/').next().unwrap_or("")
            )
        })?;
    if json {
        println!("{}", serde_json::to_string(&topic).unwrap_or_default());
        return Ok(());
    }
    println!("key_pattern:     {}", topic.key_pattern);
    println!("org_prefix:      {}", topic.vendor_prefix);
    println!("format:          {}", if topic.format.is_empty() { "(none)" } else { &topic.format });
    println!("schema_ref:      {}", if topic.schema_ref.is_empty() { "(none)" } else { &topic.schema_ref });
    println!("description:     {}", topic.description);

    let (last, state) = super::flow::last_publish_for(ctx, &topic).await;
    println!("last_publish:    {last}");
    println!("flow_state:      {state}");
    println!("created_at_unix: {}", topic.created_at);
    Ok(())
}

// Distinct create-time inputs + the narrate pair; mirrors schema::upload.
#[allow(clippy::too_many_arguments)]
async fn create(
    portal: &PortalClient,
    key: &str,
    format: &str,
    description: Option<&str>,
    schema_file: Option<&std::path::Path>,
    dry_run: bool,
    narrate: bool,
    narrate_json: bool,
) -> anyhow::Result<()> {
    let vendor = vendor_prefix_from_key(key)?;
    if dry_run {
        return create_dry_run(key, format, description, schema_file);
    }
    if schema_file.is_some() {
        eprintln!("note: --schema-file is accepted but schema-body upload is not yet available. Topic created without schema body.");
    }
    let req = build_new_topic(key, format, description);
    enforce_create_rate_limit()?;
    let narrator = Narrator::new(narrate, narrate_json);
    let id = portal
        .create_topic(vendor, req)
        .await
        .map_err(super::vendor::map_portal_err)?;
    println!("created topic `{key}` (id={id}, org={vendor})");
    if narrator.is_enabled() {
        narrator.emit(Consequence::PortalConfirmed {
            method: "POST",
            path: format!("/api/orgs/{vendor}/topics"),
            http_status: 201,
            outcome: "Created".into(),
        });
        observe_post_mutation_stethoscope(
            portal,
            &narrator,
            "topic",
            key,
            "registration",
        )
        .await;
        narrator.settled("topic-create");
    }
    // Opt-out next-step nudge (stderr, TTY-only, GOAT_NO_FUN-suppressed).
    crate::fun::hint(&format!("watch it live with `goat sub '{key}'`, or publish with `goat pub {key} <payload>`"));
    Ok(())
}

async fn delete(
    portal: &PortalClient,
    key: &str,
    hard: bool,
    yes: bool,
    dry_run: bool,
    narrate: bool,
    narrate_json: bool,
) -> anyhow::Result<()> {
    let topic = portal
        .find_topic_by_key(key)
        .await
        .map_err(super::vendor::map_portal_err)?
        .ok_or_else(|| {
            anyhow::anyhow!("topic `{key}` not registered — nothing to delete")
        })?;
    if dry_run {
        eprintln!(
            "dry-run: DELETE <portal>/api/orgs/{}/topics/{}{}",
            topic.vendor_prefix,
            topic.id,
            if hard { "?hard=true" } else { "" }
        );
        return Ok(());
    }
    if !yes {
        eprint!(
            "Delete topic `{key}` (id={}, org={}, mode={})? [y/N] ",
            topic.id,
            topic.vendor_prefix,
            if hard { "HARD" } else { "soft" }
        );
        std::io::stderr().flush().ok();
        let mut answer = String::new();
        std::io::stdin().read_line(&mut answer)?;
        if !answer.trim().eq_ignore_ascii_case("y") {
            anyhow::bail!("aborted (no confirmation)");
        }
    }
    let narrator = Narrator::new(narrate, narrate_json);
    portal
        .delete_topic(&topic.vendor_prefix, topic.id, hard)
        .await
        .map_err(super::vendor::map_portal_err)?;
    println!(
        "deleted topic `{key}` (id={}, org={}, mode={})",
        topic.id,
        topic.vendor_prefix,
        if hard { "hard" } else { "soft" }
    );
    if narrator.is_enabled() {
        let path = if hard {
            format!(
                "/api/orgs/{}/topics/{}?hard=true",
                topic.vendor_prefix, topic.id
            )
        } else {
            format!("/api/orgs/{}/topics/{}", topic.vendor_prefix, topic.id)
        };
        narrator.emit(Consequence::PortalConfirmed {
            method: "DELETE",
            path,
            http_status: 204,
            outcome: if hard { "Hard-deleted" } else { "Soft-deleted" }.into(),
        });
        // No stethoscope post-check for delete — the registration check
        // verifies presence; absence-after-delete is not a registered
        // diagnostic today. Future: add a stethoscope `tombstone` check
        // and observe it here.
        narrator.settled(if hard { "topic-delete (hard)" } else { "topic-delete (soft)" });
    }
    Ok(())
}

async fn export(portal: &PortalClient, vendor: &str, format: &str) -> anyhow::Result<()> {
    let body = portal
        .export_topics(vendor, format)
        .await
        .map_err(super::vendor::map_portal_err)?;
    // Print verbatim (no trailing newline injection — the portal's
    // encoder already terminates the document). stdout so it pipes to
    // a file cleanly: `goat topic export acme > acme-topics.yaml`.
    print!("{body}");
    Ok(())
}

async fn import(
    portal: &PortalClient,
    vendor: &str,
    file: Option<&std::path::Path>,
    format: &str,
    mode: &str,
    dry_run: bool,
) -> anyhow::Result<()> {
    let body = read_import_body(file)?;
    if body.trim().is_empty() {
        anyhow::bail!(
            "empty import body — pass --file <path> or pipe a bulk file on stdin \
             (produce one with `goat topic export {vendor}`)"
        );
    }
    let diff = portal
        .import_topics(vendor, body, content_type_for_format(format), mode, dry_run)
        .await
        .map_err(super::vendor::map_portal_err)?;
    print!("{}", format_import_summary(&diff));
    Ok(())
}

/// Read the import body from a file, or from stdin when `file` is None
/// or `-`. Stdin is the default so `goat topic export … | goat topic
/// import …` pipes without a temp file.
fn read_import_body(file: Option<&std::path::Path>) -> anyhow::Result<String> {
    match file {
        Some(p) if p.as_os_str() != "-" => std::fs::read_to_string(p)
            .map_err(|e| anyhow::anyhow!("read {}: {e}", p.display())),
        _ => {
            let mut buf = String::new();
            std::io::stdin().read_to_string(&mut buf)?;
            Ok(buf)
        }
    }
}

/// Map the `--format` value to the HTTP Content-Type the portal's
/// `parseBulk` dispatches on. Pure so the mapping is unit-tested.
fn content_type_for_format(format: &str) -> &'static str {
    match format {
        "json" => "application/json",
        _ => "application/yaml",
    }
}

/// Render the portal's import diff as operator-facing text: a one-line
/// summary tally, the mode, a dry-run marker, then one line per topic
/// that isn't unchanged (unchanged rows are elided to keep the output
/// proportional to what actually moved). Pure so it's snapshot-stable.
fn format_import_summary(diff: &ImportDiff) -> String {
    let g = |k: &str| diff.summary.get(k).copied().unwrap_or(0);
    let mut out = String::new();
    let header = if diff.dry_run { "dry-run (no changes applied)" } else { "applied" };
    out.push_str(&format!(
        "{header} · org={} · mode={}\n",
        diff.vendor, diff.mode
    ));
    out.push_str(&format!(
        "  create {} · update {} · unchanged {} · retire {}\n",
        g("create"),
        g("update"),
        g("unchanged"),
        g("retire")
    ));
    for e in &diff.entries {
        if e.action == "unchanged" {
            continue;
        }
        out.push_str(&format!("  {:<10} {}\n", e.action, e.key_pattern));
    }
    out
}

/// Resolve a stethoscope endpoint from the portal client's base URL and
/// emit a verdict consequence if we get one within the deadline. Silent
/// no-op if we can't derive an endpoint or the check doesn't come back
/// in time — `--narrate` is best-effort post-confirmation, not a gate.
async fn observe_post_mutation_stethoscope(
    portal: &PortalClient,
    narrator: &Narrator,
    target_kind: &str,
    target_value: &str,
    interesting_check: &str,
) {
    let Some(base) = portal.base_url() else {
        return;
    };
    let Some(stethoscope) = derive_stethoscope_from_portal(base) else {
        narrator.emit(Consequence::Note(format!(
            "stethoscope endpoint not derivable from portal `{base}` (non-conventional hostname); skipping post-check"
        )));
        return;
    };
    if let Some(c) = observe_stethoscope_check(
        &stethoscope,
        target_kind,
        target_value,
        interesting_check,
        NARRATE_STETHOSCOPE_DEADLINE,
    )
    .await
    {
        narrator.emit(c);
    } else {
        narrator.emit(Consequence::Note(format!(
            "stethoscope `{interesting_check}` check did not return within {:?} — substrate may still be catching up",
            NARRATE_STETHOSCOPE_DEADLINE
        )));
    }
}

/// Last-create-timestamp tracker. ADR 1019 §"Residual risk" calls
/// for a 5s minimum between create calls so scripted loops can't
/// DoS the portal. Process-local; resets on each `goat` invocation,
/// which is acceptable since the operator running the loop is the
/// only sensible target of this guard.
static LAST_CREATE: std::sync::Mutex<Option<Instant>> = std::sync::Mutex::new(None);

fn enforce_create_rate_limit() -> anyhow::Result<()> {
    let mut guard = LAST_CREATE.lock().unwrap();
    let now = Instant::now();
    rate_limit_check(*guard, now, CREATE_MIN_INTERVAL)?;
    *guard = Some(now);
    Ok(())
}

/// Pure rate-limit decision. Returns Ok(()) when `now` is at least
/// `min_interval` after `prev` (or when `prev` is None — first call).
/// Returns the rate-limit error otherwise. Lifted out of the
/// statefully-locked path so tests can drive it deterministically
/// without colliding on the LAST_CREATE static.
fn rate_limit_check(
    prev: Option<Instant>,
    now: Instant,
    min_interval: Duration,
) -> anyhow::Result<()> {
    let Some(prev) = prev else {
        return Ok(());
    };
    let since = now.saturating_duration_since(prev);
    if since < min_interval {
        let wait = min_interval - since;
        anyhow::bail!(
            "rate-limited: {}s minimum between `goat topic create` calls; wait {}ms (ADR 1019)",
            min_interval.as_secs(),
            wait.as_millis()
        );
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn vendor_prefix_extracted_from_three_segment_key() {
        assert_eq!(vendor_prefix_from_key("acme/foo/v1").unwrap(), "acme");
    }

    #[test]
    fn vendor_prefix_extracted_from_two_segment_key() {
        assert_eq!(vendor_prefix_from_key("acme/foo").unwrap(), "acme");
    }

    #[test]
    fn vendor_prefix_with_single_segment_uses_it_as_vendor() {
        // A bare "acme" (no slash) splits to ["acme"]; current behavior
        // accepts it as the vendor. The portal POST will succeed if
        // that vendor exists; this test locks that behavior in.
        assert_eq!(vendor_prefix_from_key("acme").unwrap(), "acme");
    }

    #[test]
    fn vendor_prefix_rejects_empty_key() {
        let err = vendor_prefix_from_key("").unwrap_err();
        assert!(err.to_string().contains("org prefix"));
    }

    #[test]
    fn vendor_prefix_rejects_leading_slash() {
        let err = vendor_prefix_from_key("/foo/bar").unwrap_err();
        assert!(err.to_string().contains("org prefix"));
    }

    #[test]
    fn new_topic_request_body_round_trips_required_fields() {
        let r = build_new_topic("acme/test/v1", "protobuf:Foo", Some("hello"));
        assert_eq!(r.key_pattern, "acme/test/v1");
        assert_eq!(r.format, "protobuf:Foo");
        assert_eq!(r.description, "hello");
        assert!(r.schema_ref.is_empty());
        assert!(!r.publish_to_storage);
    }

    #[test]
    fn new_topic_request_defaults_empty_description() {
        let r = build_new_topic("acme/test/v1", "json", None);
        assert!(r.description.is_empty());
    }

    #[test]
    fn rate_limit_allows_first_call() {
        let now = Instant::now();
        rate_limit_check(None, now, CREATE_MIN_INTERVAL).unwrap();
    }

    #[test]
    fn rate_limit_blocks_within_window() {
        let now = Instant::now();
        // prev=now: zero elapsed, so blocked.
        let err = rate_limit_check(Some(now), now, CREATE_MIN_INTERVAL).unwrap_err();
        assert!(err.to_string().contains("rate-limited"));
    }

    #[test]
    fn rate_limit_allows_after_window() {
        let now = Instant::now();
        let prev = now - CREATE_MIN_INTERVAL - Duration::from_millis(1);
        rate_limit_check(Some(prev), now, CREATE_MIN_INTERVAL).unwrap();
    }

    #[test]
    fn rate_limit_error_includes_remaining_ms() {
        let now = Instant::now();
        // 1s elapsed of a 5s window → 4s = 4000ms remaining.
        let prev = now - Duration::from_secs(1);
        let err = rate_limit_check(Some(prev), now, Duration::from_secs(5)).unwrap_err();
        let s = err.to_string();
        assert!(s.contains("4000ms") || s.contains("3999ms"));
    }

    #[test]
    fn content_type_maps_json_and_defaults_yaml() {
        assert_eq!(content_type_for_format("json"), "application/json");
        assert_eq!(content_type_for_format("yaml"), "application/yaml");
        // Unknown/empty falls back to yaml (a JSON superset the portal
        // parser also accepts), matching export's default.
        assert_eq!(content_type_for_format(""), "application/yaml");
    }

    fn sample_diff(dry_run: bool) -> ImportDiff {
        use panoscope_core::portal::ImportDiffEntry;
        let mut summary = std::collections::HashMap::new();
        summary.insert("create".to_string(), 1);
        summary.insert("update".to_string(), 1);
        summary.insert("unchanged".to_string(), 2);
        summary.insert("retire".to_string(), 0);
        ImportDiff {
            vendor: "acme".to_string(),
            dry_run,
            mode: "additive".to_string(),
            entries: vec![
                ImportDiffEntry { key_pattern: "acme/new/v1".into(), action: "create".into() },
                ImportDiffEntry { key_pattern: "acme/old/v1".into(), action: "update".into() },
                ImportDiffEntry { key_pattern: "acme/same/v1".into(), action: "unchanged".into() },
            ],
            summary,
        }
    }

    #[test]
    fn import_summary_marks_dry_run_and_tallies() {
        let s = format_import_summary(&sample_diff(true));
        assert!(s.contains("dry-run (no changes applied)"));
        assert!(s.contains("org=acme"));
        assert!(s.contains("create 1"));
        assert!(s.contains("update 1"));
        assert!(s.contains("unchanged 2"));
    }

    #[test]
    fn import_summary_applied_header_when_not_dry_run() {
        let s = format_import_summary(&sample_diff(false));
        assert!(s.contains("applied"));
        assert!(!s.contains("dry-run"));
    }

    #[test]
    fn import_summary_elides_unchanged_rows() {
        let s = format_import_summary(&sample_diff(true));
        // create + update rows are listed by key...
        assert!(s.contains("acme/new/v1"));
        assert!(s.contains("acme/old/v1"));
        // ...but the unchanged row is not (kept proportional to moves).
        assert!(!s.contains("acme/same/v1"));
    }
}
