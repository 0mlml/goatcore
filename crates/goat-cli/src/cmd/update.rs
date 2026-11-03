// `goat update` — self-update the running binary from this goatnet's
// download surface (`get.<site>.<zone>`, docs/design/goat-cli-mesh-download.md).
//
// Closes the loop the version_check nag only *reports*: an operator who is
// told "you're behind" can now just run `goat update` instead of hunting for
// the install page. Per-goatnet by construction — it fetches from the SAME
// surface `version_check` resolves (get.<site> derived from the active
// profile's portal host), so `goat update` on an ZAGROS context updates from
// get.zagros, on nova from get.nova. Nothing is pinned to a particular goatnet or
// to any external host.
//
// Contract:
//   - Verifies the downloaded binary against the surface's own
//     `<asset>.sha256` before touching the installed binary. A checksum
//     mismatch aborts with the installed binary untouched.
//   - Atomic replace: download to a temp file next to the running binary,
//     verify, then rename into place on the same filesystem. On Unix the
//     rename lands directly over `current_exe()` (the running process keeps
//     its open inode; the new binary is in place for the next invocation).
//     Windows locks a running `.exe` and refuses the overwrite, so there the
//     running exe is renamed to `<name>.old` first and the new one moved in
//     (rolled back on failure). No partial-write window either way.
//   - Idempotent: already-latest prints one line and exits 0 (unless
//     `--force` re-installs the same version, e.g. to repair a corrupt file).
//   - Honest + fail-closed: any resolve/fetch/verify/replace error is a
//     non-zero exit with a specific message and the old binary intact.

use crate::version_check::{current, get_base_url};
use anyhow::{anyhow, bail, Context, Result};
use clap::Args;
use std::time::Duration;

// version.json and the .sha256 are a few bytes: each request is capped whole.
const FETCH_TIMEOUT: Duration = Duration::from_secs(30);
// The binary is ~40 MB and the mesh path to get.<site> can run at a few hundred
// KB/s (zagros-dev: 38.7 MB in 117 s), so a whole-request cap fails a
// healthy download. It is bounded instead by a stall: no bytes for this long.
const STALL_TIMEOUT: Duration = Duration::from_secs(30);

#[derive(Args, Debug)]
pub struct UpdateArgs {
    /// Re-install even if already at the advertised-latest version (repairs a
    /// corrupt binary; re-verifies the checksum either way).
    #[arg(long)]
    pub force: bool,

    /// Resolve + report what would happen, but don't touch the installed
    /// binary.
    #[arg(long)]
    pub dry_run: bool,
}

/// Platform asset name the goat-get surface serves (matches the release asset
/// names + the goat-get www dir: goat-<os>-<arch>[.exe]); the one mapping
/// goatbuilder uses too (crates/goat-selfupdate).
fn asset_name() -> Result<String> {
    goat_selfupdate::asset_name("goat").ok_or_else(|| {
        anyhow!(
            "no goat binary published for this platform ({}/{}); \
             install manually from the goatnet's get.<site> page",
            std::env::consts::OS,
            std::env::consts::ARCH
        )
    })
}

pub async fn run(ctx: &super::Ctx, args: UpdateArgs) -> Result<()> {
    let base = get_base_url(&ctx.profile).ok_or_else(|| {
        anyhow!(
            "cannot resolve this context's download surface (get.<site>). \
             Set GOAT_GET_ENDPOINT or run from a context whose profile has a \
             portal.<site>.… endpoint."
        )
    })?;
    let base = base.trim_end_matches('/');
    let running = current();

    // 1. What does the surface advertise?
    let client = reqwest::Client::builder()
        .connect_timeout(FETCH_TIMEOUT)
        .read_timeout(STALL_TIMEOUT)
        .build()
        .context("build http client")?;
    let vj = client
        .get(format!("{base}/version.json"))
        .timeout(FETCH_TIMEOUT)
        .send()
        .await
        .with_context(|| format!("fetch {base}/version.json (are you on this goatnet's mesh?)"))?
        .error_for_status()
        .context("version.json request")?
        .text()
        .await
        .context("read version.json")?;
    let vjson: serde_json::Value = serde_json::from_str(&vj).context("parse version.json")?;
    let latest = vjson
        .get("version")
        .and_then(|s| s.as_str())
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .ok_or_else(|| anyhow!("version.json has no version field"))?;

    println!("running: {running}   latest: {latest}   surface: {base}/");

    if latest == running && !args.force {
        println!("already up to date.");
        return Ok(());
    }
    if args.dry_run {
        println!(
            "dry-run: would install {latest} for {} from {base}/{}",
            std::env::consts::OS,
            asset_name()?
        );
        return Ok(());
    }

    // 2. Download the platform binary + its checksum.
    let asset = asset_name()?;
    let bin_url = format!("{base}/{asset}");
    let sha_url = format!("{bin_url}.sha256");
    println!("downloading {bin_url} …");
    let bin = client
        .get(&bin_url)
        .send()
        .await
        .with_context(|| format!("download {bin_url}"))?
        .error_for_status()
        .context("binary request")?
        .bytes()
        .await
        .context("read binary body")?;
    let sha_txt = client
        .get(&sha_url)
        .timeout(FETCH_TIMEOUT)
        .send()
        .await
        .with_context(|| format!("download {sha_url}"))?
        .error_for_status()
        .context("checksum request")?
        .text()
        .await
        .context("read checksum")?;
    // Checksum files are "<hex>  <name>" — take the first field.
    let want = goat_selfupdate::parse_checksum(&sha_txt)
        .ok_or_else(|| anyhow!("malformed checksum from {sha_url}"))?;

    // 3. Verify BEFORE touching the installed binary.
    if let Err(m) = goat_selfupdate::verify(&bin, &want) {
        bail!(
            "checksum mismatch — refusing to install.\n  expected {}\n  got      {}\n\
             The download surface may be mid-deploy; retry shortly.",
            m.want,
            m.got
        );
    }

    // 4. Replace the installed binary next to the running one, atomically
    // (crates/goat-selfupdate): Unix renames over the running file; Windows,
    // which locks a running `.exe`, moves it to `<name>.old` first and puts it
    // back if the second move fails. The `.old` stays locked until this process
    // exits; the next `goat update` clears it.
    let exe = std::env::current_exe().context("resolve current executable path")?;
    let left_old = goat_selfupdate::install(&exe, &bin, &format!("goat-update-{latest}"))
        .map_err(|e| anyhow!("{e} (try the same shell that installed goat)"))?;

    println!("updated {running} → {latest} at {}", exe.display());
    if let Some(old) = left_old {
        println!("(previous binary left as {}; removed on your next `goat update`)", old.display());
    }
    println!("run `goat --version` to confirm.");
    Ok(())
}
