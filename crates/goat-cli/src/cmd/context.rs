use crate::profile::{Profile, DEFAULT_CONTEXT};
use clap::Subcommand;
use std::io::Write;

#[derive(Subcommand, Debug)]
pub enum ContextCmd {
    /// List contexts known to this profile.
    Ls {
        /// Emit one JSON line per context instead of pretty output.
        #[arg(long)]
        json: bool,
    },
    /// Switch the active context (writes `current_context` to config.toml).
    Use { name: String },
    /// Show one context's resolved endpoints, role, org prefix, cert expiry.
    Show {
        /// Defaults to the active context.
        name: Option<String>,
    },
    /// Remove a context. Refuses to remove the active context unless
    /// another context is selected via `--switch-to <name>`.
    Rm {
        name: String,
        /// After removal, switch active context to this one.
        #[arg(long)]
        switch_to: Option<String>,
        /// Skip confirmation prompt.
        #[arg(long, short = 'y')]
        yes: bool,
    },
}

pub async fn run(ctx: &super::Ctx, op: ContextCmd) -> anyhow::Result<()> {
    match op {
        ContextCmd::Ls { json } => ls(&ctx.profile, json),
        ContextCmd::Use { name } => use_(&ctx.profile, &name),
        ContextCmd::Show { name } => show(&ctx.profile, name.as_deref()),
        ContextCmd::Rm {
            name,
            switch_to,
            yes,
        } => rm(&ctx.profile, &name, switch_to.as_deref(), yes),
    }
}

/// List context dirs under `<profile>/contexts/`. Marks the active
/// context with `*`. Pure-local; no network.
fn ls(profile: &Profile, json: bool) -> anyhow::Result<()> {
    let contexts = list_contexts(profile)?;
    let active = current_context(profile);
    if json {
        for name in &contexts {
            let entry = serde_json::json!({
                "name": name,
                "active": name == &active,
            });
            println!("{}", serde_json::to_string(&entry).unwrap_or_default());
        }
        return Ok(());
    }
    if contexts.is_empty() {
        println!("(no contexts — run `goat profile init <bundle.cbor>` to create one)");
        return Ok(());
    }
    for name in &contexts {
        let marker = if name == &active { "*" } else { " " };
        println!("{marker} {name}");
    }
    Ok(())
}

/// Switch the active context. Writes `current_context = "<name>"` to
/// the profile's config.toml. Refuses on a context that doesn't exist.
fn use_(profile: &Profile, name: &str) -> anyhow::Result<()> {
    let contexts = list_contexts(profile)?;
    if !contexts.iter().any(|c| c == name) {
        anyhow::bail!(
            "context `{name}` does not exist. `goat context ls` shows what does."
        );
    }
    write_current_context(profile, name)?;
    println!("switched to context `{name}` ({})", profile.dir.display());
    Ok(())
}

/// Show one context's profile.toml + leaf-cert expiry. The named
/// context (or the active one if name is None) must be initialized.
fn show(profile: &Profile, name: Option<&str>) -> anyhow::Result<()> {
    let target_name = name
        .map(String::from)
        .unwrap_or_else(|| current_context(profile));
    let mut target = profile.clone();
    target.context = target_name.clone();

    target.require_initialized()?;
    // read_classified (not a bare `?`), matching cmd/profile.rs's identical
    // read — before this, the two `show` commands gave different-quality
    // errors for the exact same failure (a profile.toml that vanished or
    // became unreadable between the check above and this read).
    let body = target.read_classified(&target.profile_toml_path())?;
    print!("{body}");
    // Cert expiry (live, may diverge from profile.toml if hand-edited).
    if let Ok(unix) = leaf_cert_not_after_unix(&target.cert_path()) {
        println!("# mtls.cert.pem not_after = unix {unix}");
    }
    Ok(())
}

/// Remove a context directory. Refuses on the active context unless
/// the operator names a different `--switch-to`. Refuses on the
/// `default` context if it's the only one (degrades gracefully into
/// the empty-profile state otherwise).
fn rm(
    profile: &Profile,
    name: &str,
    switch_to: Option<&str>,
    yes: bool,
) -> anyhow::Result<()> {
    let contexts = list_contexts(profile)?;
    if !contexts.iter().any(|c| c == name) {
        anyhow::bail!("context `{name}` does not exist.");
    }
    let active = current_context(profile);
    if name == active {
        match switch_to {
            Some(target) if contexts.iter().any(|c| c == target) && target != name => {
                write_current_context(profile, target)?;
                println!("active context switched to `{target}` before removing `{name}`");
            }
            Some(target) => anyhow::bail!(
                "--switch-to `{target}` is not an existing other context (have: {})",
                contexts.join(", ")
            ),
            None => anyhow::bail!(
                "cannot remove the active context `{name}`. Pass --switch-to <other> to switch first, then remove."
            ),
        }
    }

    if !yes {
        let dir = profile.contexts_dir().join(name);
        eprint!(
            "Delete context `{name}` at {}? [y/N] ",
            dir.display()
        );
        std::io::stderr().flush().ok();
        let mut answer = String::new();
        std::io::stdin().read_line(&mut answer)?;
        if !answer.trim().eq_ignore_ascii_case("y") {
            anyhow::bail!("aborted (no confirmation)");
        }
    }
    let dir = profile.contexts_dir().join(name);
    {
        use anyhow::Context;
        std::fs::remove_dir_all(&dir).with_context(|| format!("remove {}", dir.display()))?;
    }
    println!("removed context `{name}` ({})", dir.display());
    Ok(())
}

/// Enumerate contexts under `<profile>/contexts/`. A context is any
/// directory whose name doesn't end in `.bak` (rotation backups
/// from `goat profile rotate` are filtered out).
fn list_contexts(profile: &Profile) -> anyhow::Result<Vec<String>> {
    let dir = profile.contexts_dir();
    if !dir.exists() {
        return Ok(vec![]);
    }
    let mut out: Vec<String> = std::fs::read_dir(&dir)?
        .filter_map(|e| e.ok())
        .filter(|e| e.file_type().map(|t| t.is_dir()).unwrap_or(false))
        .filter_map(|e| e.file_name().into_string().ok())
        .filter(|n| !n.ends_with(".bak"))
        .collect();
    out.sort();
    Ok(out)
}

/// Read `current_context` from `<profile>/config.toml`. Falls back to
/// `default` when the file is absent or the key is missing. Delegates to
/// the shared `profile::read_current_context` parser — the same one
/// `Profile::resolve` uses to pick the active context — so the `ls`/`use`
/// view can never disagree with what commands actually resolve to.
fn current_context(profile: &Profile) -> String {
    crate::profile::read_current_context(&profile.config_path())
        .unwrap_or_else(|| DEFAULT_CONTEXT.to_string())
}

/// Write `current_context = "<name>"` to `<profile>/config.toml`.
/// The file is mode 0600 — same as the per-context secret files.
fn write_current_context(profile: &Profile, name: &str) -> anyhow::Result<()> {
    let path = profile.config_path();
    let body = format!("current_context = \"{name}\"\n");
    let mut opts = std::fs::OpenOptions::new();
    opts.write(true).create(true).truncate(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        opts.mode(0o600);
    }
    let mut f = opts.open(&path)?;
    f.write_all(body.as_bytes())?;
    // mode arg on OpenOptions only applies at create time; if the file
    // pre-existed (operator hand-edited config), tighten perms here.
    // Windows has no POSIX mode bits — user-profile ACLs cover it.
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600))?;
    }
    Ok(())
}

/// Parse the leaf cert PEM and return `notAfter` as Unix seconds.
/// Mirrors the helper in cmd/profile.rs; kept locally so this module
/// stays self-contained.
fn leaf_cert_not_after_unix(path: &std::path::Path) -> anyhow::Result<i64> {
    let pem_bytes = std::fs::read(path)?;
    let blob = pem::parse(&pem_bytes)?;
    if blob.tag() != "CERTIFICATE" {
        anyhow::bail!("first PEM block is {}, want CERTIFICATE", blob.tag());
    }
    let (_, cert) = x509_parser::parse_x509_certificate(blob.contents())
        .map_err(|e| anyhow::anyhow!("x509 parse: {e}"))?;
    Ok(cert.validity().not_after.timestamp())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use tempfile::TempDir;

    fn fixture_profile(tmp: &TempDir, contexts: &[&str], active: Option<&str>) -> Profile {
        let dir = tmp.path().to_path_buf();
        let contexts_dir = dir.join("contexts");
        fs::create_dir_all(&contexts_dir).unwrap();
        for c in contexts {
            let ctx_dir = contexts_dir.join(c);
            fs::create_dir_all(&ctx_dir).unwrap();
            fs::write(
                ctx_dir.join("profile.toml"),
                format!(
                    "portal_endpoint = \"https://portal.test\"\nrouter_endpoint = \"tls/host:7447\"\ndevice_id = \"op-{c}\"\n"
                ),
            )
            .unwrap();
        }
        if let Some(a) = active {
            fs::write(
                dir.join("config.toml"),
                format!("current_context = \"{a}\"\n"),
            )
            .unwrap();
        }
        Profile {
            dir,
            context: active.unwrap_or(DEFAULT_CONTEXT).to_string(),
        }
    }

    #[test]
    fn list_contexts_filters_bak() {
        let tmp = TempDir::new().unwrap();
        let p = fixture_profile(&tmp, &["default", "second"], Some("default"));
        // Add a .bak sibling that should be filtered.
        fs::create_dir_all(p.contexts_dir().join("default.bak")).unwrap();
        let got = list_contexts(&p).unwrap();
        assert_eq!(got, vec!["default", "second"]);
    }

    #[test]
    fn current_context_falls_back_to_default() {
        let tmp = TempDir::new().unwrap();
        let p = fixture_profile(&tmp, &["default"], None);
        assert_eq!(current_context(&p), "default");
    }

    #[test]
    fn use_switches_and_persists() {
        let tmp = TempDir::new().unwrap();
        let p = fixture_profile(&tmp, &["default", "second"], Some("default"));
        use_(&p, "second").unwrap();
        assert_eq!(current_context(&p), "second");
        // Mode 0600 enforced (unix only — Windows has no POSIX bits).
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let meta = fs::metadata(p.config_path()).unwrap();
            assert_eq!(meta.permissions().mode() & 0o777, 0o600);
        }
    }

    #[test]
    fn use_refuses_unknown_context() {
        let tmp = TempDir::new().unwrap();
        let p = fixture_profile(&tmp, &["default"], Some("default"));
        let err = use_(&p, "nope").unwrap_err();
        assert!(err.to_string().contains("does not exist"));
    }

    #[test]
    fn rm_refuses_active_without_switch_to() {
        let tmp = TempDir::new().unwrap();
        let p = fixture_profile(&tmp, &["default", "second"], Some("default"));
        let err = rm(&p, "default", None, true).unwrap_err();
        assert!(err.to_string().contains("cannot remove the active"));
    }

    #[test]
    fn rm_with_switch_to_succeeds() {
        let tmp = TempDir::new().unwrap();
        let p = fixture_profile(&tmp, &["default", "second"], Some("default"));
        rm(&p, "default", Some("second"), true).unwrap();
        // active context switched.
        assert_eq!(current_context(&p), "second");
        // dir gone.
        assert!(!p.contexts_dir().join("default").exists());
    }

    #[test]
    fn rm_refuses_unknown_context() {
        let tmp = TempDir::new().unwrap();
        let p = fixture_profile(&tmp, &["default"], Some("default"));
        let err = rm(&p, "nope", None, true).unwrap_err();
        assert!(err.to_string().contains("does not exist"));
    }
}
