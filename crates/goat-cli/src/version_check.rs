//! Staleness check — tell the operator when the `goat` they're running is
//! behind the version the goatnet is currently handing out.
//!
//! goat-cli changes often, and on-mesh participants install it once from
//! their goatnet's download surface (`get.<site>.<zone>`,
//! docs/design/goat-cli-mesh-download.md) and then forget about it — so they
//! end up confused on an old binary whose flags/output have since moved. This
//! module closes that loop: when `goat` actually connects to the fabric, it
//! compares its own `CARGO_PKG_VERSION` against the version that goatnet's
//! download surface advertises at `<get-base>/version.json` and prints a
//! one-line nudge if behind.
//!
//! Per-goatnet by construction: the get-base is resolved from the active
//! profile's own portal host (`portal.<site>.…` → `get.<site>.…`), so each
//! context checks against ITS goatnet's surface — nothing is pinned to a
//! particular goatnet.
//!
//! Source of truth = the download surface, NOT the portal. The download
//! surface is the one place already bumped per release (`goat_get_release_tag`
//! in the `goat-get` role), so there is no second drift point to keep in
//! sync. `version.json` is rendered from that same var.
//!
//! Discipline (mirrors the `fun` module's Three Rules):
//!   - Best-effort + fail-silent. Any error — off-mesh, no `get.` host,
//!     timeout, malformed JSON — produces NO output and never fails the
//!     command. A version nag must never be the reason a real command broke.
//!   - Pretty surface only. Suppressed when stderr is not a TTY (piped / CI)
//!     and when `GOAT_NO_VERSION_CHECK` (or `GOAT_NO_FUN`) is set, so it can't
//!     pollute scripts or the `--json` contract.
//!   - At most one nag per process (multi-session commands open more than one
//!     session; the operator sees the line once).
//!
//! The check is cache-gated (`~/.goat/version-check.json`, short TTL) so the
//! common path is a cheap file read with no added latency. Only when the cache
//! is stale or cold does it do a single bounded (mesh-local, sub-2s) fetch to
//! refresh — once per TTL, not per command. `goat doctor` forces a fresh
//! synchronous check and renders the result either way (current or behind),
//! since it's the explicit "is my setup healthy" command.

use crate::profile::Profile;
use std::io::IsTerminal;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

/// How long a cached "latest" result is trusted before a refresh. goat-cli
/// ships often, so this is short enough to surface a same-day release on the
/// operator's next session, long enough that everyday commands almost never
/// touch the network.
const CACHE_TTL: Duration = Duration::from_secs(3 * 60 * 60); // 3h

/// Bounded timeout for the HTTP GET of `version.json`. Tight so a cold-cache
/// synchronous fetch can't stall a command for long; the surface is
/// mesh-local when we're connected, so this is generous in practice.
const FETCH_TIMEOUT: Duration = Duration::from_millis(1500);

/// At-most-once-per-process nag guard.
static NAGGED: AtomicBool = AtomicBool::new(false);

/// This binary's version (`goat --version` reports the same string).
pub fn current() -> &'static str {
    env!("CARGO_PKG_VERSION")
}

/// The advertised-latest result, resolved from the download surface.
#[derive(Debug, Clone)]
pub struct Latest {
    pub version: String,
    /// Where to get it — this goatnet's `get.<site>.<zone>/` landing page.
    pub install_url: String,
}

/// Full status for the `goat doctor` render: current vs latest + verdict.
#[derive(Debug, Clone)]
pub struct VersionStatus {
    pub current: String,
    pub latest: String,
    pub install_url: String,
    pub outdated: bool,
}

/// Opportunistic nag for any command that just connected. Never blocks on the
/// network when a fresh cache is present; never errors; prints at most once.
/// Wired into the shared Zenoh session-open path so every connecting command
/// (`pub`/`sub`/`health`/`top`/`who`/`inspect`/`blob`/`replay`/…) participates.
pub async fn nag_if_outdated(profile: &Profile) {
    if !nag_allowed() {
        return;
    }
    let current = current();

    // Fresh cache → nag from it, zero network (the common path).
    let cached = read_cache(profile);
    if let Some(c) = cached.as_ref().filter(|c| c.is_fresh()) {
        maybe_nag(current, &c.version, &c.install_url);
        return;
    }

    // Stale or cold cache → one bounded synchronous fetch (mesh-local, capped
    // at FETCH_TIMEOUT) to refresh, then nag from the result. The TTL gate
    // means this only touches the network once per CACHE_TTL. If the fetch
    // fails but we have a stale cache, nag from the stale value rather than go
    // silent — an old "you're behind" is still true.
    match fetch_latest(profile).await {
        Some(latest) => {
            write_cache(profile, &latest);
            maybe_nag(current, &latest.version, &latest.install_url);
        }
        None => {
            if let Some(stale) = cached {
                maybe_nag(current, &stale.version, &stale.install_url);
            }
        }
    }
}

/// Force a fresh synchronous check for `goat doctor`. Returns None on any
/// failure (off-mesh / no `get.` host / timeout) so doctor simply omits the
/// line — its four steps already cover reachability.
pub async fn doctor_status(profile: &Profile) -> Option<VersionStatus> {
    let current = current().to_string();
    let latest = fetch_latest(profile).await?;
    write_cache(profile, &latest);
    Some(VersionStatus {
        outdated: is_outdated(&current, &latest.version),
        current,
        latest: latest.version,
        install_url: latest.install_url,
    })
}

/// Whether a nag may print right now: pretty surface, not opted out, not
/// already printed this process.
fn nag_allowed() -> bool {
    if std::env::var_os("GOAT_NO_VERSION_CHECK").is_some()
        || std::env::var_os("GOAT_NO_FUN").is_some()
    {
        return false;
    }
    if !std::io::stderr().is_terminal() {
        return false;
    }
    // Claim the one-shot slot; if someone already nagged, stand down.
    !NAGGED.swap(true, Ordering::SeqCst)
}

/// Emit the nag iff `current` is behind `latest`. The `NAGGED` slot was
/// already claimed in `nag_allowed`, so this prints at most once.
fn maybe_nag(current: &str, latest: &str, install_url: &str) {
    if is_outdated(current, latest) {
        eprintln!(
            "⚠ goat {current} is out of date — {latest} is available. \
             Update: {install_url}  (silence: GOAT_NO_VERSION_CHECK=1)"
        );
    }
}

/// GET `<get-base>/version.json` and parse the advertised version. None on any
/// failure. The get-base is resolved from `GOAT_GET_ENDPOINT` /
/// `<CONTEXT>_GET_ENDPOINT`, else derived from the profile's `portal_endpoint`
/// host (`portal.<site>.…` → `get.<site>.…`, mirroring the `goat-get` role's
/// `get.<site>.<zone>` hostname convention).
async fn fetch_latest(profile: &Profile) -> Option<Latest> {
    let base = get_base_url(profile)?;
    let url = format!("{}/version.json", base.trim_end_matches('/'));
    let client = reqwest::Client::builder()
        .timeout(FETCH_TIMEOUT)
        .build()
        .ok()?;
    let body = client.get(&url).send().await.ok()?.text().await.ok()?;
    let v: serde_json::Value = serde_json::from_str(&body).ok()?;
    let version = v.get("version")?.as_str()?.trim().to_string();
    if version.is_empty() {
        return None;
    }
    // Prefer the surface's own landing URL; fall back to the get-base.
    let install_url = v
        .get("install_url")
        .and_then(|s| s.as_str())
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| format!("{}/", base.trim_end_matches('/')));
    Some(Latest {
        version,
        install_url,
    })
}

/// Resolve this context's download-surface base URL (`get.<site>.<zone>`).
/// pub(crate): the name-fallback resolver (crate::names, ADR 1082) refreshes
/// its snapshot cache from the same surface.
pub(crate) fn get_base_url(profile: &Profile) -> Option<String> {
    // Explicit overrides first.
    if let Some(v) = std::env::var("GOAT_GET_ENDPOINT")
        .ok()
        .filter(|s| !s.trim().is_empty())
    {
        return Some(v.trim().trim_end_matches('/').to_string());
    }
    let ctx_env = profile.context.to_uppercase().replace('-', "_");
    if let Some(v) = std::env::var(format!("{ctx_env}_GET_ENDPOINT"))
        .ok()
        .filter(|s| !s.trim().is_empty())
    {
        return Some(v.trim().trim_end_matches('/').to_string());
    }
    // Derive from portal_endpoint host: swap the leading `portal` label for
    // `get`. portal.<site>.netbird.<backbone> → get.<site>.netbird.<backbone>.
    let toml = std::fs::read_to_string(profile.profile_toml_path()).ok()?;
    let portal = profile_toml_extract(&toml, "portal_endpoint")?;
    let host = portal
        .trim()
        .trim_start_matches("https://")
        .trim_start_matches("http://");
    let host = host.split('/').next().unwrap_or(host);
    let mut labels: Vec<&str> = host.split('.').collect();
    if labels.first() != Some(&"portal") || labels.len() < 2 {
        return None;
    }
    labels[0] = "get";
    Some(format!("https://{}", labels.join(".")))
}

// Was a local copy of the prefix-collision-buggy parser (see
// crate::profile::extract_toml_field's doc comment) — now an alias onto
// the one shared, correct implementation.
use crate::profile::extract_toml_field as profile_toml_extract;

/// Cached "latest" result + when it was fetched.
struct Cache {
    version: String,
    install_url: String,
    checked_at_unix: u64,
}

impl Cache {
    fn is_fresh(&self) -> bool {
        now_unix()
            .checked_sub(self.checked_at_unix)
            .map(|age| age < CACHE_TTL.as_secs())
            .unwrap_or(false)
    }
}

fn cache_path(profile: &Profile) -> std::path::PathBuf {
    // Global to the binary, not per-context: the running version is one fact.
    profile.dir.join("version-check.json")
}

fn read_cache(profile: &Profile) -> Option<Cache> {
    let body = std::fs::read_to_string(cache_path(profile)).ok()?;
    let v: serde_json::Value = serde_json::from_str(&body).ok()?;
    let version = v.get("version")?.as_str()?.to_string();
    if version.is_empty() {
        return None;
    }
    let install_url = v
        .get("install_url")
        .and_then(|s| s.as_str())
        .unwrap_or("")
        .to_string();
    let checked_at_unix = v.get("checked_at_unix").and_then(|n| n.as_u64()).unwrap_or(0);
    Some(Cache {
        version,
        install_url,
        checked_at_unix,
    })
}

fn write_cache(profile: &Profile, latest: &Latest) {
    let body = serde_json::json!({
        "version": latest.version,
        "install_url": latest.install_url,
        "checked_at_unix": now_unix(),
    });
    // Best-effort: ensure the profile dir exists, then write. Ignore errors —
    // a non-writable cache just means we re-fetch next time.
    let _ = std::fs::create_dir_all(&profile.dir);
    let _ = std::fs::write(cache_path(profile), serde_json::to_string(&body).unwrap_or_default());
}

fn now_unix() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

// The version compare is the one goatbuilder uses too (crates/goat-selfupdate):
// numeric, tolerant of a release-tag prefix, never "outdated" on garbage.
pub use goat_selfupdate::is_outdated;
#[cfg(test)]
use goat_selfupdate::parse_semver;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_plain_semver() {
        assert_eq!(parse_semver("0.4.7"), Some((0, 4, 7)));
    }

    #[test]
    fn parses_tag_and_v_prefixes() {
        assert_eq!(parse_semver("goat-cli-v0.4.8"), Some((0, 4, 8)));
        assert_eq!(parse_semver("v1.2.3"), Some((1, 2, 3)));
    }

    #[test]
    fn tolerates_short_and_suffixed_versions() {
        assert_eq!(parse_semver("1.2"), Some((1, 2, 0)));
        assert_eq!(parse_semver("1"), Some((1, 0, 0)));
        assert_eq!(parse_semver("0.4.8-rc1"), Some((0, 4, 8)));
        assert_eq!(parse_semver("0.4.8+build.5"), Some((0, 4, 8)));
    }

    #[test]
    fn rejects_garbage() {
        assert_eq!(parse_semver("not-a-version"), None);
        assert_eq!(parse_semver(""), None);
    }

    #[test]
    fn outdated_compares_numerically() {
        assert!(is_outdated("0.4.7", "0.4.8"));
        assert!(is_outdated("0.4.7", "0.5.0"));
        assert!(is_outdated("0.9.9", "1.0.0"));
        // 0.4.10 > 0.4.9 numerically, not lexically — the classic trap.
        assert!(is_outdated("0.4.9", "0.4.10"));
    }

    #[test]
    fn not_outdated_when_equal_or_ahead() {
        assert!(!is_outdated("0.4.7", "0.4.7"));
        assert!(!is_outdated("0.5.0", "0.4.9"));
        assert!(!is_outdated("1.0.0", "0.9.9"));
    }

    #[test]
    fn fails_safe_on_unparseable() {
        // Never nag when either side is garbage.
        assert!(!is_outdated("0.4.7", "garbage"));
        assert!(!is_outdated("garbage", "0.4.8"));
    }

    fn portal_profile(context: &str, portal_endpoint: &str) -> (tempfile::TempDir, Profile) {
        let tmp = tempfile::tempdir().unwrap();
        let p = Profile {
            dir: tmp.path().to_path_buf(),
            context: context.into(),
        };
        std::fs::create_dir_all(p.context_dir()).unwrap();
        std::fs::write(
            p.profile_toml_path(),
            format!("portal_endpoint = \"{portal_endpoint}\"\n"),
        )
        .unwrap();
        (tmp, p)
    }

    #[test]
    fn get_base_derives_per_goatnet_from_portal_host() {
        // GOAT_GET_ENDPOINT must be unset for the derivation path to run.
        if std::env::var_os("GOAT_GET_ENDPOINT").is_some() {
            return;
        }
        // Each context derives ITS OWN goatnet's surface — only the leading
        // `portal` label is swapped for `get`; site + zone are preserved. This
        // is what makes the check per-goatnet rather than pinned to any one.
        let (_t1, nova) =
            portal_profile("default", "https://portal.nova.netbird.nova.example");
        assert_eq!(
            get_base_url(&nova).as_deref(),
            Some("https://get.nova.netbird.nova.example")
        );
        let (_t2, zagros) =
            portal_profile("zagros", "https://portal.zagros.netbird.zagros.example");
        assert_eq!(
            get_base_url(&zagros).as_deref(),
            Some("https://get.zagros.netbird.zagros.example")
        );
    }

    #[test]
    fn get_base_none_when_portal_host_unexpected() {
        let tmp = tempfile::tempdir().unwrap();
        let p = Profile {
            dir: tmp.path().to_path_buf(),
            context: "default".into(),
        };
        std::fs::create_dir_all(p.context_dir()).unwrap();
        std::fs::write(
            p.profile_toml_path(),
            "portal_endpoint = \"https://example.com\"\n",
        )
        .unwrap();
        if std::env::var_os("GOAT_GET_ENDPOINT").is_some() {
            return;
        }
        assert_eq!(get_base_url(&p), None);
    }
}
