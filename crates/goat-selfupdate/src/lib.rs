//! goat-selfupdate — the self-update core `goat` and `goatbuilder` share.
//!
//! Both binaries update themselves from their goatnet's download surface
//! (`get.<site>.<zone>`, docs/design/goat-cli-mesh-download.md and
//! docs/design/goatbuilder-distribution.md). Each fetches with its own HTTP
//! stack (goat is async, goatbuilder is not); everything after the bytes are
//! in hand is here, once:
//!
//!   - [`asset_name`] — the platform asset the goat-get role publishes
//!     (`<bin>-<os>-<arch>[.exe]`).
//!   - [`is_outdated`] — numeric `MAJOR.MINOR.PATCH` compare, tolerant of a
//!     release-tag prefix; garbage on either side is never "outdated".
//!   - [`parse_checksum`] + [`verify`] — the surface's `<asset>.sha256`, checked
//!     before anything on disk is touched.
//!   - [`install`] — atomic replace of the running binary: a temp file next to
//!     it, renamed over it on Unix (the running process keeps its open inode);
//!     on Windows, which locks a running `.exe`, the running file is moved to
//!     `<name>.old` first and moved back if the second rename fails. No
//!     partial-write window either way.

use sha2::{Digest, Sha256};
use std::io::Write as _;
use std::path::{Path, PathBuf};

/// `<os>-<arch>` as the release assets name it, or None for a platform no
/// release builds.
pub fn platform() -> Option<&'static str> {
    Some(match (std::env::consts::OS, std::env::consts::ARCH) {
        ("macos", "aarch64") => "darwin-arm64",
        ("macos", "x86_64") => "darwin-amd64",
        ("linux", "aarch64") => "linux-arm64",
        ("linux", "x86_64") => "linux-amd64",
        ("windows", "x86_64") => "windows-amd64",
        _ => return None,
    })
}

/// The asset name for `bin` on this platform: `goat-darwin-arm64`,
/// `goatbuilder-linux-amd64`, `goat-windows-amd64.exe`.
pub fn asset_name(bin: &str) -> Option<String> {
    let p = platform()?;
    Some(if p.starts_with("windows") { format!("{bin}-{p}.exe") } else { format!("{bin}-{p}") })
}

/// True when `current` is strictly older than `latest`. If either side cannot
/// be parsed it is NOT outdated: never nag or update on garbage.
pub fn is_outdated(current: &str, latest: &str) -> bool {
    match (parse_semver(current), parse_semver(latest)) {
        (Some(c), Some(l)) => l > c,
        _ => false,
    }
}

/// `MAJOR.MINOR.PATCH` out of a version or a release tag (`0.4.8`, `v0.4.8`,
/// `goat-cli-v0.4.8`, `goatbuilder/v0.2.0`); a pre-release/build suffix is
/// dropped and the numeric core compared.
pub fn parse_semver(s: &str) -> Option<(u64, u64, u64)> {
    let s = s.trim();
    let s = s.rsplit('/').next().unwrap_or(s);
    let s = s.strip_prefix("goat-cli-v").unwrap_or(s);
    let s = s.strip_prefix('v').unwrap_or(s);
    let core = s.split(['-', '+']).next().unwrap_or(s);
    let mut it = core.split('.');
    let major = it.next()?.parse::<u64>().ok()?;
    let minor = it.next().unwrap_or("0").parse::<u64>().ok()?;
    let patch = it.next().unwrap_or("0").parse::<u64>().ok()?;
    Some((major, minor, patch))
}

/// The digest out of a checksum file (`<hex>  <name>`, or the bare hex),
/// lowercased; None unless it is 64 hex digits.
pub fn parse_checksum(text: &str) -> Option<String> {
    let want = text.split_whitespace().next()?.to_lowercase();
    (want.len() == 64 && want.bytes().all(|b| b.is_ascii_hexdigit())).then_some(want)
}

pub fn sha256_hex(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}

/// The downloaded bytes did not hash to the surface's digest.
#[derive(Debug, PartialEq, Eq)]
pub struct Mismatch {
    pub want: String,
    pub got: String,
}

pub fn verify(bytes: &[u8], want: &str) -> Result<(), Mismatch> {
    let got = sha256_hex(bytes);
    if got == want {
        Ok(())
    } else {
        Err(Mismatch { want: want.to_string(), got })
    }
}

/// Which step of [`install`] failed. Every failure leaves the installed binary
/// in place (on Windows, moved back if it had been moved aside).
#[derive(Debug)]
pub enum InstallError {
    /// The temp file next to the binary could not be created, written or made executable.
    Stage { tmp: PathBuf, source: std::io::Error },
    /// Windows: the running binary could not be moved aside.
    MoveAside { exe: PathBuf, source: std::io::Error },
    /// The new binary could not be renamed into place.
    Replace { exe: PathBuf, source: std::io::Error },
}

impl std::fmt::Display for InstallError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            InstallError::Stage { tmp, source } => write!(f, "write {} ({source}) — need write access to the install dir", tmp.display()),
            InstallError::MoveAside { exe, source } => {
                write!(f, "move the running {} aside ({source}) — need write access to the install dir", exe.display())
            }
            InstallError::Replace { exe, source } => {
                write!(f, "replace {} ({source}) — need write permission on the install dir; the previous binary is in place", exe.display())
            }
        }
    }
}

impl std::error::Error for InstallError {}

/// Put `bytes` at `exe`, atomically. `tag` names the temp file (`.<tag>.tmp`
/// next to the binary). Returns the `.old` path left behind on Windows, where
/// the running file stays locked until the process exits; the next install
/// removes it.
pub fn install(exe: &Path, bytes: &[u8], tag: &str) -> Result<Option<PathBuf>, InstallError> {
    let dir = exe.parent().unwrap_or_else(|| Path::new("."));
    let tmp = dir.join(format!(".{tag}.tmp"));
    let stage = || -> std::io::Result<()> {
        let mut f = std::fs::File::create(&tmp)?;
        f.write_all(bytes)?;
        f.sync_all().ok();
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&tmp, std::fs::Permissions::from_mode(0o755))?;
        }
        Ok(())
    };
    if let Err(source) = stage() {
        let _ = std::fs::remove_file(&tmp);
        return Err(InstallError::Stage { tmp, source });
    }

    #[cfg(windows)]
    {
        let old = exe.with_file_name(format!(
            "{}.old",
            exe.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default()
        ));
        let _ = std::fs::remove_file(&old); // a prior update's leftover, unlocked now
        if let Err(source) = std::fs::rename(exe, &old) {
            let _ = std::fs::remove_file(&tmp);
            return Err(InstallError::MoveAside { exe: exe.to_path_buf(), source });
        }
        if let Err(source) = std::fs::rename(&tmp, exe) {
            let _ = std::fs::rename(&old, exe);
            let _ = std::fs::remove_file(&tmp);
            return Err(InstallError::Replace { exe: exe.to_path_buf(), source });
        }
        Ok(Some(old))
    }
    #[cfg(not(windows))]
    {
        if let Err(source) = std::fs::rename(&tmp, exe) {
            let _ = std::fs::remove_file(&tmp);
            return Err(InstallError::Replace { exe: exe.to_path_buf(), source });
        }
        Ok(None)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_versions_and_release_tags() {
        assert_eq!(parse_semver("0.4.7"), Some((0, 4, 7)));
        assert_eq!(parse_semver("v1.2.3"), Some((1, 2, 3)));
        assert_eq!(parse_semver("goat-cli-v0.4.8"), Some((0, 4, 8)));
        assert_eq!(parse_semver("goatbuilder/v0.2.0"), Some((0, 2, 0)));
        assert_eq!(parse_semver("1.2"), Some((1, 2, 0)));
        assert_eq!(parse_semver("0.4.8-rc.1"), Some((0, 4, 8)));
        assert_eq!(parse_semver("0.4.8+build.5"), Some((0, 4, 8)));
        assert_eq!(parse_semver("not-a-version"), None);
        assert_eq!(parse_semver(""), None);
    }

    #[test]
    fn outdated_is_numeric_and_fails_safe() {
        assert!(is_outdated("0.4.9", "0.4.10"));
        assert!(is_outdated("0.9.9", "1.0.0"));
        assert!(!is_outdated("0.2.0", "0.2.0"));
        assert!(!is_outdated("0.3.0", "0.2.0"));
        assert!(!is_outdated("0.2.0", "garbage"));
        assert!(!is_outdated("garbage", "0.2.0"));
    }

    #[test]
    fn checksum_file_takes_the_first_field_and_only_a_digest() {
        let d = "38030e737cd8034cae5e4192fc1d9ab653656dd6598615b59ce1552441454d0c";
        assert_eq!(parse_checksum(&format!("{d}  goatbuilder-darwin-arm64\n")).as_deref(), Some(d));
        assert_eq!(parse_checksum(&d.to_uppercase()).as_deref(), Some(d));
        assert_eq!(parse_checksum("<html>404</html>"), None);
        assert_eq!(parse_checksum(""), None);
        assert_eq!(parse_checksum(&"z".repeat(64)), None);
    }

    #[test]
    fn verify_names_both_digests_on_a_mismatch() {
        let want = sha256_hex(b"right");
        assert!(verify(b"right", &want).is_ok());
        let e = verify(b"wrong", &want).unwrap_err();
        assert_eq!(e.want, want);
        assert_eq!(e.got, sha256_hex(b"wrong"));
    }

    #[test]
    fn asset_name_follows_the_release_names() {
        if let Some(p) = platform() {
            let a = asset_name("goatbuilder").unwrap();
            assert!(a.starts_with("goatbuilder-") && a.contains(p), "{a}");
        }
    }

    #[test]
    fn install_replaces_the_file_and_leaves_no_temp() {
        let d = tempfile::tempdir().unwrap();
        let exe = d.path().join("goatbuilder");
        std::fs::write(&exe, b"old").unwrap();
        install(&exe, b"new", "goatbuilder-update-9.9.9").unwrap();
        assert_eq!(std::fs::read(&exe).unwrap(), b"new");
        assert!(!d.path().join(".goatbuilder-update-9.9.9.tmp").exists());
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            assert_eq!(std::fs::metadata(&exe).unwrap().permissions().mode() & 0o777, 0o755);
        }
    }

    #[cfg(unix)]
    #[test]
    fn a_failed_install_leaves_the_binary_intact() {
        use std::os::unix::fs::PermissionsExt;
        let d = tempfile::tempdir().unwrap();
        let exe = d.path().join("goatbuilder");
        std::fs::write(&exe, b"old").unwrap();
        std::fs::set_permissions(d.path(), std::fs::Permissions::from_mode(0o555)).unwrap();
        let r = install(&exe, b"new", "t");
        std::fs::set_permissions(d.path(), std::fs::Permissions::from_mode(0o755)).unwrap();
        // root ignores directory modes: there the install succeeds and there is nothing to assert.
        if let Err(e) = r {
            assert!(matches!(e, InstallError::Stage { .. }), "{e}");
            assert_eq!(std::fs::read(&exe).unwrap(), b"old");
        }
    }
}
