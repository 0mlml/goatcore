use crate::bundle::{self, Bundle};
use crate::errors::GoatError;
use crate::profile::Profile;
use clap::Subcommand;
use std::fs;
use std::path::{Path, PathBuf};

#[derive(Subcommand, Debug)]
pub enum ProfileCmd {
    /// Establish a profile from a bundle.cbor — verifies signature,
    /// extracts mTLS cert/key/CA + portal token + endpoints, writes to
    /// the profile directory (mode 0700 / 0600), then suggests `goat doctor`.
    Init {
        /// Path to the operator bundle (CBOR).
        bundle: PathBuf,
    },
    /// Show the active profile's endpoints, device id, publish anchor,
    /// portal token status, and cert/bundle expiry. Reads files only;
    /// does not contact the network.
    Show,
    /// Rotate the profile with a new bundle. The previous profile is
    /// preserved at <dir>.bak/ for one rotation cycle.
    Rotate { bundle: PathBuf },
    /// Print the resolved profile directory path and exit. Always works,
    /// even before `init` has been run.
    Path,
}

pub async fn run(ctx: &super::Ctx, op: ProfileCmd) -> anyhow::Result<()> {
    match op {
        ProfileCmd::Path => {
            println!("{}", ctx.profile.dir.display());
            Ok(())
        }
        ProfileCmd::Init { bundle } => init(&ctx.profile, &bundle, /* rotate = */ false),
        ProfileCmd::Rotate { bundle } => init(&ctx.profile, &bundle, /* rotate = */ true),
        ProfileCmd::Show => show(&ctx.profile),
    }
}

/// Read, verify, and install a profile from a bundle.cbor. Verification
/// is the crypto gate (signature + lifecycle); the filesystem install is
/// delegated to `install_verified` so it stays testable without a signed
/// fixture.
fn init(profile: &Profile, bundle_path: &Path, rotate: bool) -> anyhow::Result<()> {
    let cbor = fs::read(bundle_path).map_err(|e| GoatError::BundleInvalid {
        hint: format!("read bundle {}: {e}", bundle_path.display()),
    })?;
    let parsed: Bundle = bundle::parse_and_verify(&cbor).map_err(anyhow::Error::new)?;
    install_verified(profile, &parsed, &cbor, rotate)
}

/// Install (or rotate) an ALREADY-VERIFIED bundle into the profile dir.
/// Split out from `init` so the filesystem-install logic — context dir
/// 0700, secret files 0600, the refuse-without-rotate guard, the `.bak`
/// rotation move, and first-context `current_context` seeding — is
/// testable without minting + signing a bundle. The signature gate is
/// covered separately in bundle.rs (parse_and_verify_with + minted /
/// tampered / wrong-signer cases); callers MUST verify `parsed` first.
/// `cbor` is the raw bundle, persisted verbatim into the context dir.
///
/// When `rotate` is true and the context dir already exists, it's moved
/// to `<context_dir>.bak/` (replacing any prior .bak) before the new
/// content is written. When `rotate` is false and the context dir is
/// already initialized, we refuse — operator should choose `rotate`
/// explicitly to avoid clobbering live state.
fn install_verified(
    profile: &Profile,
    parsed: &Bundle,
    cbor: &[u8],
    rotate: bool,
) -> anyhow::Result<()> {
    let ctx_dir = profile.context_dir();
    let already_initialized = profile.is_initialized();
    if already_initialized && !rotate {
        return Err(GoatError::BundleInvalid {
            hint: format!(
                "context `{}` already has a profile at {}. Use `goat profile rotate <bundle>` \
                 to replace it (preserves prior at .bak/), or pick a different --context.",
                profile.context,
                ctx_dir.display()
            ),
        }
        .into());
    }
    if already_initialized && rotate {
        let bak = backup_path(&ctx_dir);
        if bak.exists() {
            fs::remove_dir_all(&bak).map_err(|e| GoatError::BundleInvalid {
                hint: format!("remove stale {} before rotate: {e}", bak.display()),
            })?;
        }
        fs::rename(&ctx_dir, &bak).map_err(|e| GoatError::BundleInvalid {
            hint: format!(
                "move existing {} to {} for rotate: {e}",
                ctx_dir.display(),
                bak.display()
            ),
        })?;
    }

    // mkdir -p ~/.goat/contexts/<name>, mode 0700 on the new context dir
    // (parents inherit the umask — that's the right behavior; we
    // only enforce on the leaf the profile owns).
    fs::create_dir_all(&ctx_dir).map_err(|e| GoatError::BundleInvalid {
        hint: format!("create {}: {e}", ctx_dir.display()),
    })?;
    set_mode(&ctx_dir, 0o700)?;

    // Sensitive files mode 0600.
    write_secret(&profile.cert_path(), &parsed.extras.mtls_cert)?;
    write_secret(&profile.key_path(), &parsed.extras.mtls_key)?;
    write_secret(&profile.ca_roots_path(), &parsed.extras.ca_roots)?;
    write_secret(&profile.portal_token_path(), &parsed.extras.portal_token)?;
    write_secret(&profile.bundle_path(), cbor)?;

    // profile.toml — plain config; not secret, but mode 0600 for symmetry.
    let toml_body = render_profile_toml(parsed);
    write_secret(&profile.profile_toml_path(), toml_body.as_bytes())?;

    // current_context in config.toml — set if it's the first context
    // for this profile dir. Subsequent inits go to whatever --context
    // names without changing the default.
    let config_path = profile.config_path();
    if !config_path.exists() {
        write_secret(
            &config_path,
            format!("current_context = \"{}\"\n", profile.context).as_bytes(),
        )?;
    }

    eprintln!(
        "goat profile {verb} → {ctx_path}",
        verb = if rotate { "rotated" } else { "init'd" },
        ctx_path = ctx_dir.display()
    );
    eprintln!("  portal: {}", parsed.extras.portal_endpoint);
    eprintln!("  router: {}", parsed.extras.router_endpoint);
    eprintln!("  device_id: {}", parsed.device_id);
    eprintln!(
        "  bundle expires_at unix={} (verify with `goat profile show`)",
        parsed.expires_at_unix
    );
    eprintln!();
    eprintln!("Next: `goat doctor` to verify reachability.");

    Ok(())
}

/// Print the active profile's endpoints, device id, publish anchor, portal
/// token status, and cert/bundle expiry. Pure-local; does NOT contact the
/// network — the publish anchor is read straight off the cert CN, never
/// resolved to its display alias (that needs a portal call; `goat access
/// who <anchor>` does that).
fn show(profile: &Profile) -> anyhow::Result<()> {
    profile.require_initialized()?;

    let toml_body =
        fs::read_to_string(profile.profile_toml_path()).map_err(|e| GoatError::ProfileMissing {
            context: profile.context.clone(),
            hint: format!("read {}: {e}", profile.profile_toml_path().display()),
        })?;
    print!("{toml_body}");

    // `bundle::parse_and_verify` checks `expires_at` only at verify time
    // (init/rotate) — nothing outside that path reads `expires_at_unix`
    // again afterward, so it's a ONE-TIME gate this profile already passed
    // by existing, not an ongoing check (a long-lived operator profile
    // outliving its original bundle window is normal). Label it as such so
    // it can never misread as an ongoing problem — the cert line right
    // below is the one that actually says whether this profile still
    // works.
    if let Some(expires_at) = crate::profile::extract_toml_field(&toml_body, "expires_at_unix")
        .and_then(|s| s.parse::<i64>().ok())
    {
        println!(
            "# bundle.cbor's original expires_at: {} — a one-time gate already satisfied at \
             init/rotate, not enforced since; see mtls.cert.pem below for ongoing validity",
            relative_days(expires_at)
        );
    }

    // Cert leaf expiry, parsed live so it stays accurate even if
    // profile.toml is stale (e.g. operator hand-edited).
    match cert_not_after(&profile.cert_path()) {
        Ok(not_after) => {
            println!(
                "# mtls.cert.pem not_after = unix {not_after} ({})",
                relative_days(not_after)
            );
        }
        Err(e) => {
            println!("# mtls.cert.pem expiry unknown ({e})");
        }
    }

    // The publish anchor: what you actually write under
    // (volatile/<anchor>/**, etc.) and what `goat access key` / --sealed /
    // --open default to when --anchor isn't passed — the exact derivation
    // those three already use (`cmd::pubsub::own_anchor`), just never
    // surfaced here before.
    match crate::cmd::pubsub::own_anchor(profile) {
        Some(anchor) => {
            println!("# publish anchor = {anchor}");
            println!(
                "#   what you write under (e.g. volatile/{anchor}/**); the default for \
                 `goat access key` / --sealed / --open. Run `goat access who {anchor}` for the \
                 human-readable alias (needs the network; this command doesn't touch it)."
            );
        }
        None => {
            println!(
                "# publish anchor = none — this cert's CN isn't a 32-hex slot anchor (a \
                 legacy/shared cert); `goat access key` / --sealed / --open need --anchor \
                 passed explicitly"
            );
        }
    }

    // Portal token: presence + kind, never the value. Which of the two
    // token kinds you're holding — vp_ vendor participant vs op_ operator
    // key — is the single most common cause of a 403 on an `admin`/
    // `machine grant` call (see docs/design/goat-cli.md).
    println!("{}", portal_token_line(profile.portal_token().as_deref()));

    Ok(())
}

/// The `# portal token = ...` line `show()` prints: presence + kind + source,
/// NEVER the token value itself. Extracted as a pure function (rather than
/// inlined `println!`s) specifically so a test can assert the one security
/// property that matters here — a real secret never appears in the
/// returned string — without needing to capture process stdout.
fn portal_token_line(tok: Option<&str>) -> String {
    match tok {
        Some(tok) => {
            let kind = if tok.starts_with("vp_") {
                "vp_ vendor participant token"
            } else if tok.starts_with("op_") {
                "op_ operator key"
            } else {
                "unrecognized token format"
            };
            let source = if crate::profile::portal_token_from_env().is_some() {
                "$GOAT_PORTAL_TOKEN"
            } else {
                "the context's portal.token file"
            };
            format!("# portal token = present, {kind} (from {source})")
        }
        None => {
            "# portal token = none found — set $GOAT_PORTAL_TOKEN or re-run `goat profile init`"
                .to_string()
        }
    }
}

/// `"expires in Nd"` / `"expired Nd ago"` for a unix timestamp, relative to
/// now. No shared crate-wide formatter exists for this yet (`status.rs`
/// computes its own inline too) — kept local rather than introduced as one.
fn relative_days(unix: i64) -> String {
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0);
    // Branch on the sign of the raw difference, not of `days` — integer
    // division truncates toward zero, so a timestamp 3 hours in the past
    // gave `days == 0` and fell into "expires in 0d", reading as still
    // valid for something already expired. That's exactly the false-calm
    // this whole enrichment exists to avoid.
    let diff = unix - now;
    if diff < 0 {
        match (-diff) / 86400 {
            0 => "expired earlier today".to_string(),
            days => format!("expired {days}d ago"),
        }
    } else {
        match diff / 86400 {
            0 => "expires today".to_string(),
            days => format!("expires in {days}d"),
        }
    }
}

/// Path used to hold the prior profile during rotate. One backup
/// generation only — the next rotate overwrites it.
fn backup_path(ctx_dir: &Path) -> PathBuf {
    let mut p = ctx_dir.to_path_buf();
    let name = match ctx_dir.file_name() {
        Some(n) => n.to_owned(),
        None => return ctx_dir.with_extension("bak"),
    };
    let mut new_name = name;
    new_name.push(".bak");
    p.set_file_name(new_name);
    p
}

/// Render the user-facing profile.toml. Endpoints + device_id +
/// expiry — everything readable without crypto state.
fn render_profile_toml(b: &Bundle) -> String {
    // Optional onboarding-engine endpoint (goat-id-console). Only bundles from a
    // goatnet that stood up the onboard route carry it; when absent the CLI
    // falls back to portal_endpoint for /api/onboard/* (back-compat), so the
    // line is simply omitted (docs/design/onboarding-extraction-plan.md).
    let onboard_line = match b.extras.onboard_endpoint.as_deref() {
        Some(u) if !u.is_empty() => format!("onboard_endpoint = \"{}\"\n", u.replace('"', "\\\"")),
        _ => String::new(),
    };
    format!(
        "# goat profile.toml — written by `goat profile init` (ADR 1019).\n\
         # Edit the bundle.cbor + re-init, not this file.\n\
         portal_endpoint = \"{portal}\"\n\
         router_endpoint = \"{router}\"\n\
         {onboard}device_id = \"{device}\"\n\
         issued_at_unix = {issued}\n\
         activation_deadline_unix = {activation}\n\
         expires_at_unix = {expires}\n",
        portal = b.extras.portal_endpoint.replace('"', "\\\""),
        router = b.extras.router_endpoint.replace('"', "\\\""),
        onboard = onboard_line,
        device = b.device_id.replace('"', "\\\""),
        issued = b.issued_at_unix,
        activation = b.activation_deadline_unix,
        expires = b.expires_at_unix,
    )
}

/// Parse the leaf cert PEM and return `notAfter` as Unix seconds.
fn cert_not_after(path: &Path) -> anyhow::Result<i64> {
    let pem = fs::read(path)?;
    let blob = pem::parse(&pem)?;
    if blob.tag() != "CERTIFICATE" {
        return Err(anyhow::anyhow!(
            "first PEM block in {} is {}, want CERTIFICATE",
            path.display(),
            blob.tag()
        ));
    }
    let (_, cert) = x509_parser::parse_x509_certificate(blob.contents())
        .map_err(|e| anyhow::anyhow!("x509 parse: {e}"))?;
    Ok(cert.validity().not_after.timestamp())
}

/// Write bytes to `path` with mode 0600 (overwriting). Used for
/// every file in the context dir — keys, cert, token, even the
/// profile.toml + bundle.cbor for symmetry. The directory is set
/// 0700 separately.
///
/// On Windows there are no POSIX mode bits; the file is written
/// normally and confidentiality rests on the user-profile directory
/// ACLs (`%USERPROFILE%\.goat`), which already deny other users.
fn write_secret(path: &Path, data: &[u8]) -> anyhow::Result<()> {
    use std::io::Write;
    let mut opts = fs::OpenOptions::new();
    opts.write(true).create(true).truncate(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        opts.mode(0o600);
    }
    let mut f = opts.open(path).map_err(|e| GoatError::BundleInvalid {
        hint: format!("open {} for write: {e}", path.display()),
    })?;
    f.write_all(data).map_err(|e| GoatError::BundleInvalid {
        hint: format!("write {}: {e}", path.display()),
    })?;
    Ok(())
}

/// Set the directory's permission bits. Used to chmod the leaf
/// context dir to 0700 (parents inherit umask). No-op on Windows
/// (no POSIX mode bits — see `write_secret`).
#[cfg(unix)]
fn set_mode(path: &Path, mode: u32) -> anyhow::Result<()> {
    use std::os::unix::fs::PermissionsExt;
    let perms = fs::Permissions::from_mode(mode);
    fs::set_permissions(path, perms).map_err(|e| GoatError::BundleInvalid {
        hint: format!("chmod {}: {e}", path.display()),
    })?;
    Ok(())
}

#[cfg(not(unix))]
fn set_mode(_path: &Path, _mode: u32) -> anyhow::Result<()> {
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::bundle::{Bundle, GoatCliExtras};
    use p256::ecdsa::SigningKey;
    use p256::elliptic_curve::Generate;
    use tempfile::TempDir;

    /// Permission bits (low 9) of a path — for asserting 0700 / 0600.
    /// Unix-only; Windows has no POSIX mode bits (see `write_secret`),
    /// so the mode assertions below are compiled out there.
    #[cfg(unix)]
    fn mode_of(p: &Path) -> u32 {
        use std::os::unix::fs::PermissionsExt;
        fs::metadata(p).unwrap().permissions().mode() & 0o777
    }

    /// A `Profile` rooted at a throwaway temp dir.
    fn temp_profile(dir: &Path, context: &str) -> Profile {
        Profile {
            dir: dir.to_path_buf(),
            context: context.into(),
        }
    }

    fn synthetic_bundle() -> Bundle {
        Bundle {
            device_id: "test-device-01".into(),
            issued_at_unix: 1_780_000_000,
            activation_deadline_unix: 1_780_100_000,
            expires_at_unix: 1_810_000_000,
            extras: GoatCliExtras {
                portal_endpoint: "https://portal.nova.netbird.example".into(),
                portal_token: b"test-token".to_vec(),
                router_endpoint: "tls/router.nova.netbird.example:7447".into(),
                onboard_endpoint: Some("https://onboard.nova.netbird.example".into()),
                mtls_cert: b"-----BEGIN CERTIFICATE-----\nstub\n-----END CERTIFICATE-----\n"
                    .to_vec(),
                // Stub bytes only (written verbatim to a file, never parsed as a real key).
                // Deliberately NOT a real PEM header — the literal `BEGIN PRIVATE KEY` trips the
                // credential-leak scanner in tests/build.bats, which scans src/** (not just tests/).
                mtls_key: b"-----BEGIN STUB KEY-----\nstub\n-----END STUB KEY-----\n".to_vec(),
                ca_roots: b"-----BEGIN CERTIFICATE-----\nca-stub\n-----END CERTIFICATE-----\n"
                    .to_vec(),
            },
        }
    }

    #[test]
    fn render_profile_toml_includes_required_keys() {
        let b = synthetic_bundle();
        let s = render_profile_toml(&b);
        assert!(s.contains("portal_endpoint = \"https://portal.nova.netbird.example\""));
        assert!(s.contains("router_endpoint = \"tls/router.nova.netbird.example:7447\""));
        // Optional onboard_endpoint renders when the bundle carries it.
        assert!(s.contains("onboard_endpoint = \"https://onboard.nova.netbird.example\""));
        assert!(s.contains("device_id = \"test-device-01\""));
        assert!(s.contains("issued_at_unix = 1780000000"));
        assert!(s.contains("activation_deadline_unix = 1780100000"));
        assert!(s.contains("expires_at_unix = 1810000000"));
    }

    #[test]
    fn render_profile_toml_omits_onboard_when_absent() {
        // Back-compat: a bundle from a goatnet without the onboard route has no
        // onboard_endpoint, so the profile omits the line (CLI portal-falls-back).
        let mut b = synthetic_bundle();
        b.extras.onboard_endpoint = None;
        let s = render_profile_toml(&b);
        assert!(!s.contains("onboard_endpoint"));
        assert!(s.contains("portal_endpoint = \"https://portal.nova.netbird.example\""));
    }

    #[test]
    fn render_profile_toml_escapes_embedded_quotes() {
        let mut b = synthetic_bundle();
        b.extras.portal_endpoint = "https://weird\"host.example".into();
        let s = render_profile_toml(&b);
        // The double-quote in the value is backslash-escaped so the
        // resulting TOML is parseable. Hostnames with quotes are
        // pathological but the escaping is structural — better to lock
        // it in than discover the gap during a vendor rebrand.
        assert!(s.contains("weird\\\"host.example"));
    }

    #[test]
    fn backup_path_appends_bak_to_context_dir() {
        let p = PathBuf::from("/tmp/goat/contexts/default");
        let b = backup_path(&p);
        assert_eq!(b, PathBuf::from("/tmp/goat/contexts/default.bak"));
    }

    #[test]
    fn backup_path_handles_named_subdir_with_dots() {
        let p = PathBuf::from("/tmp/goat/contexts/nova.A");
        let b = backup_path(&p);
        assert_eq!(b, PathBuf::from("/tmp/goat/contexts/nova.A.bak"));
    }

    #[test]
    fn show_succeeds_with_only_profile_toml_and_stub_cert() {
        // The show() path reads profile.toml + parses cert leaf
        // expiry. Cert parsing failure is non-fatal — we still emit
        // the toml + a "# expiry unknown" line. This test verifies
        // that fallback so an operator with a hand-edited / stub
        // cert can still inspect their profile.
        let td = TempDir::new().unwrap();
        let dir = td.path();
        let profile = Profile {
            dir: dir.to_path_buf(),
            context: "default".into(),
        };
        let ctx_dir = profile.context_dir();
        fs::create_dir_all(&ctx_dir).unwrap();
        let toml = render_profile_toml(&synthetic_bundle());
        fs::write(profile.profile_toml_path(), toml.as_bytes()).unwrap();
        // Garbage in mtls.cert.pem — show() should still succeed,
        // emitting the "expiry unknown" fallback.
        fs::write(profile.cert_path(), b"not a real PEM").unwrap();
        show(&profile).expect("show should succeed even with stub cert");
    }

    #[test]
    fn show_returns_profile_missing_when_uninitialized() {
        let td = TempDir::new().unwrap();
        let profile = Profile {
            dir: td.path().to_path_buf(),
            context: "default".into(),
        };
        // No context dir exists; show() must surface E_PROFILE_MISSING.
        let err = show(&profile).unwrap_err();
        let g = err.downcast_ref::<GoatError>().expect("downcast");
        assert_eq!(g.code(), "E_PROFILE_MISSING");
    }

    #[test]
    fn relative_days_reads_future_and_past_correctly() {
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;
        assert_eq!(relative_days(now + 30 * 86400), "expires in 30d");
        assert_eq!(relative_days(now - 5 * 86400), "expired 5d ago");
    }

    #[test]
    fn relative_days_does_not_read_a_sub_day_expiry_as_still_valid() {
        // REGRESSION: `(unix - now) / 86400` truncates toward zero, so a
        // timestamp 3 hours in the past gave `days == 0` and fell into the
        // "expires in Nd" branch — an already-expired cert reading as still
        // valid, exactly the false-calm this enrichment exists to prevent.
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs() as i64;
        assert_eq!(relative_days(now - 3 * 3600), "expired earlier today");
        assert_eq!(relative_days(now + 3 * 3600), "expires today");
    }

    #[test]
    fn portal_token_line_never_prints_the_token_value() {
        // The one security property that matters for this line: whatever
        // secret the operator is holding must never appear in the output,
        // only a label for its kind and where it came from.
        let secret = "vp_acme-vendor_deadbeef00112233445566778899aabbccddeeff0011223344556677";
        let line = portal_token_line(Some(secret));
        assert!(!line.contains(secret), "leaked the token value: {line}");
        assert!(line.contains("vp_ vendor participant token"));

        let op_secret = "op_deadbeef00112233445566778899aabbccddeeff0011223344556677889900";
        let op_line = portal_token_line(Some(op_secret));
        assert!(
            !op_line.contains(op_secret),
            "leaked the token value: {op_line}"
        );
        assert!(op_line.contains("op_ operator key"));

        let unknown = "sometoken";
        let unknown_line = portal_token_line(Some(unknown));
        assert!(
            !unknown_line.contains(unknown),
            "leaked the token value: {unknown_line}"
        );
        assert!(unknown_line.contains("unrecognized token format"));

        assert!(portal_token_line(None).contains("none found"));
    }

    // --- install_verified: the post-verify filesystem install ---------
    // These drive install_verified directly with a synthetic (already
    // "verified") bundle, so they cover the dir/file modes, the rotation
    // .bak move, the refuse-without-rotate guard, and current_context
    // seeding without needing a signed fixture. The signature gate is
    // covered in bundle.rs.

    const RAW_CBOR: &[u8] = b"RAW-BUNDLE-CBOR-BYTES-PERSISTED-VERBATIM";

    #[test]
    fn install_verified_writes_all_files_with_secure_modes() {
        let td = TempDir::new().unwrap();
        let profile = temp_profile(td.path(), "default");
        let b = synthetic_bundle();
        install_verified(&profile, &b, RAW_CBOR, /* rotate = */ false).unwrap();

        // Context dir is 0700.
        #[cfg(unix)]
        assert_eq!(mode_of(&profile.context_dir()), 0o700, "context dir mode");

        // Every secret file exists, is 0600, and holds the bundle bytes.
        let want_toml = render_profile_toml(&b);
        let cases: [(PathBuf, &[u8]); 6] = [
            (profile.cert_path(), &b.extras.mtls_cert),
            (profile.key_path(), &b.extras.mtls_key),
            (profile.ca_roots_path(), &b.extras.ca_roots),
            (profile.portal_token_path(), &b.extras.portal_token),
            (profile.bundle_path(), RAW_CBOR),
            (profile.profile_toml_path(), want_toml.as_bytes()),
        ];
        for (path, want) in &cases {
            assert!(path.exists(), "{} should exist", path.display());
            #[cfg(unix)]
            assert_eq!(mode_of(path), 0o600, "{} mode", path.display());
            assert_eq!(
                &fs::read(path).unwrap(),
                want,
                "{} contents",
                path.display()
            );
        }
        assert!(profile.is_initialized());
    }

    #[test]
    fn install_verified_seeds_current_context_on_first_init() {
        let td = TempDir::new().unwrap();
        let profile = temp_profile(td.path(), "default");
        install_verified(&profile, &synthetic_bundle(), RAW_CBOR, false).unwrap();

        let config = fs::read_to_string(profile.config_path()).unwrap();
        assert!(
            config.contains("current_context = \"default\""),
            "config.toml should seed current_context; got: {config}"
        );
        #[cfg(unix)]
        assert_eq!(mode_of(&profile.config_path()), 0o600, "config.toml mode");
    }

    #[test]
    fn install_verified_preserves_current_context_for_second_context() {
        // First init seeds current_context = "default". A second init
        // into a different context must NOT repoint the default.
        let td = TempDir::new().unwrap();
        let p_default = temp_profile(td.path(), "default");
        install_verified(&p_default, &synthetic_bundle(), RAW_CBOR, false).unwrap();

        let p_other = temp_profile(td.path(), "site-a");
        install_verified(&p_other, &synthetic_bundle(), RAW_CBOR, false).unwrap();

        let config = fs::read_to_string(p_default.config_path()).unwrap();
        assert!(
            config.contains("current_context = \"default\""),
            "second context must not steal current_context; got: {config}"
        );
        assert!(p_other.is_initialized(), "site-a context should be installed");
    }

    #[test]
    fn install_verified_refuses_overwrite_without_rotate() {
        let td = TempDir::new().unwrap();
        let profile = temp_profile(td.path(), "default");
        install_verified(&profile, &synthetic_bundle(), RAW_CBOR, false).unwrap();

        // Second non-rotate install into the same initialized context
        // must refuse with E_BUNDLE_INVALID rather than clobber.
        let err = install_verified(&profile, &synthetic_bundle(), RAW_CBOR, false).unwrap_err();
        let g = err.downcast_ref::<GoatError>().expect("downcast");
        assert_eq!(g.code(), "E_BUNDLE_INVALID");
        assert!(
            format!("{g:?}").contains("rotate"),
            "hint should point at `rotate`"
        );
    }

    #[test]
    fn install_verified_rotate_moves_prior_to_bak() {
        let td = TempDir::new().unwrap();
        let profile = temp_profile(td.path(), "default");

        // First install carries a marker so we can prove the prior
        // generation survives at .bak/.
        let mut gen1 = synthetic_bundle();
        gen1.device_id = "gen1-device".into();
        install_verified(&profile, &gen1, b"GEN1-CBOR", false).unwrap();

        let mut gen2 = synthetic_bundle();
        gen2.device_id = "gen2-device".into();
        install_verified(&profile, &gen2, b"GEN2-CBOR", /* rotate = */ true).unwrap();

        // Live context now holds gen2.
        assert_eq!(fs::read(profile.bundle_path()).unwrap(), b"GEN2-CBOR");

        // Prior generation preserved at <ctx>.bak/bundle.cbor.
        let bak = backup_path(&profile.context_dir());
        assert!(bak.is_dir(), "{} should exist", bak.display());
        assert_eq!(
            fs::read(bak.join("bundle.cbor")).unwrap(),
            b"GEN1-CBOR",
            "prior bundle should survive at .bak/"
        );
    }

    #[test]
    fn install_verified_rotate_twice_replaces_stale_bak() {
        let td = TempDir::new().unwrap();
        let profile = temp_profile(td.path(), "default");

        install_verified(&profile, &synthetic_bundle(), b"GEN1-CBOR", false).unwrap();
        install_verified(&profile, &synthetic_bundle(), b"GEN2-CBOR", true).unwrap();
        // Second rotate: .bak already holds gen1; it must be replaced by
        // gen2 (one backup generation only), no stale-dir error.
        install_verified(&profile, &synthetic_bundle(), b"GEN3-CBOR", true).unwrap();

        assert_eq!(fs::read(profile.bundle_path()).unwrap(), b"GEN3-CBOR");
        let bak = backup_path(&profile.context_dir());
        assert_eq!(
            fs::read(bak.join("bundle.cbor")).unwrap(),
            b"GEN2-CBOR",
            ".bak should hold only the immediately-prior generation"
        );
    }

    #[test]
    fn init_round_trips_a_real_signed_bundle_through_verify_and_show() {
        // End-to-end through the actual verifier: mint a bundle with a
        // throwaway CA key, verify it with parse_and_verify_with, install
        // it, then show() it. Proves read -> verify -> install_verified
        // -> show compose against genuinely-signed bytes (init() itself
        // uses the embedded CA root, which a test key can't satisfy, so
        // this mirrors init()'s body with the test-key verify seam).
        let signing_key = SigningKey::generate();
        let verifying_key = *signing_key.verifying_key();
        let wire = crate::bundle::mint_fixture_bundle(&signing_key, 86_400, 3600, true);
        let parsed =
            crate::bundle::parse_and_verify_with(&wire, &verifying_key).expect("verify ok");

        let td = TempDir::new().unwrap();
        let profile = temp_profile(td.path(), "default");
        install_verified(&profile, &parsed, &wire, false).unwrap();

        // Files reflect the minted bundle's extras (see bundle.rs
        // mint_fixture_bundle).
        assert_eq!(fs::read(profile.bundle_path()).unwrap(), wire);
        assert_eq!(
            fs::read(profile.portal_token_path()).unwrap(),
            b"opaque-token"
        );
        let toml = fs::read_to_string(profile.profile_toml_path()).unwrap();
        assert!(toml.contains("portal_endpoint = \"https://portal.test\""));

        // And show() reads it back without error.
        show(&profile).expect("show should succeed on a freshly installed profile");
    }
}
