//! Contract clause C3 (fresh source) on a real run.
//!
//! The check is `ops/scripts/fresh-source.sh`, the one both avenues run before
//! the first host is touched (ADR 1201): a checkout behind the trunk in
//! `ops/ansible` or `ops/pins` is refused (GB-064), a trunk that cannot be
//! fetched is refused unless the run is `--offline` (GB-065), a checkout ahead
//! of the trunk is run with a warning naming its commits. The trunk commit the
//! check judged against is handed to every `ansible-preflight.sh` of the same
//! run (`GOAT_FRESH_SOURCE_TRUNK`), so all of its plays are judged against one
//! commit.

use std::path::Path;
use std::process::Command;

use anyhow::{Context, Result};

use crate::Check;

/// The environment variable that carries the judged trunk commit to the plays.
pub const TRUNK_ENV: &str = "GOAT_FRESH_SOURCE_TRUNK";

/// What the check said.
pub struct Fresh {
    /// The trunk commit judged against; empty on an offline run with no cached trunk.
    pub trunk: String,
    /// One row for the report: ok (current, ahead, offline) or refused.
    pub check: Check,
}

impl Fresh {
    pub fn refused(&self) -> bool {
        !self.check.ok
    }
}

/// Run the check for `repo`. `GOAT_DEPLOY_OFFLINE=1` (`--offline`) and an
/// already-pinned `GOAT_FRESH_SOURCE_TRUNK` reach the script through the
/// environment, as they do from the script avenue.
pub fn check(repo: &Path) -> Result<Fresh> {
    check_with(repo, &[], &[])
}

fn check_with(repo: &Path, set: &[(&str, &str)], unset: &[&str]) -> Result<Fresh> {
    let mut cmd = Command::new(repo.join("ops/scripts/fresh-source.sh"));
    cmd.args(["--repo".as_ref(), repo.as_os_str()]);
    for (k, v) in set {
        cmd.env(k, v);
    }
    for k in unset {
        cmd.env_remove(k);
    }
    let out = cmd.output().with_context(|| "ops/scripts/fresh-source.sh")?;
    let said = String::from_utf8_lossy(&out.stderr).trim_end().to_string();
    let trunk = String::from_utf8_lossy(&out.stdout).trim().to_string();
    let check = if out.status.success() {
        let detail = if said.is_empty() {
            format!("checkout current with the trunk at {}", short(&trunk))
        } else {
            said.lines().map(|l| l.trim_start_matches("fresh-source: ")).collect::<Vec<_>>().join("\n")
        };
        Check { name: "fresh-source".into(), ok: true, detail, fix: None }
    } else {
        let fix = said
            .lines()
            .find_map(|l| l.trim_start().strip_prefix("fix: "))
            .unwrap_or("git fetch <trunk remote> main and bring this checkout onto it")
            .to_string();
        let detail = said
            .lines()
            .filter(|l| !l.trim_start().starts_with("fix: "))
            .map(|l| l.trim_start_matches("fresh-source: "))
            .collect::<Vec<_>>()
            .join("\n");
        Check { name: "fresh-source".into(), ok: false, detail, fix: Some(fix) }
    };
    Ok(Fresh { trunk, check })
}

fn short(sha: &str) -> &str {
    if sha.is_empty() {
        "(none cached)"
    } else {
        &sha[..sha.len().min(9)]
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use std::path::PathBuf;

    fn git(dir: &Path, args: &[&str]) {
        let st = Command::new("git")
            .current_dir(dir)
            .args(args)
            .env("GIT_AUTHOR_NAME", "t")
            .env("GIT_AUTHOR_EMAIL", "t@t")
            .env("GIT_COMMITTER_NAME", "t")
            .env("GIT_COMMITTER_EMAIL", "t@t")
            .status()
            .unwrap();
        assert!(st.success(), "git {args:?}");
    }

    /// A bare trunk `origin` with ops/ansible and the real script, and a clone of it.
    fn pair(tag: &str) -> (PathBuf, PathBuf, PathBuf) {
        let root = std::env::temp_dir().join(format!("gb-fresh-{tag}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&root);
        fs::create_dir_all(&root).unwrap();
        let (bare, a, b) = (root.join("o.git"), root.join("a"), root.join("b"));
        git(&root, &["init", "-q", "--bare", "o.git"]);
        git(&bare, &["symbolic-ref", "HEAD", "refs/heads/main"]);
        git(&root, &["init", "-q", "-b", "main", "a"]);
        fs::create_dir_all(a.join("ops/ansible")).unwrap();
        fs::create_dir_all(a.join("ops/scripts")).unwrap();
        fs::create_dir_all(a.join("docs")).unwrap();
        fs::write(a.join("ops/ansible/x"), "1").unwrap();
        let script = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../ops/scripts/fresh-source.sh");
        fs::copy(&script, a.join("ops/scripts/fresh-source.sh")).unwrap();
        git(&a, &["add", "-A"]);
        git(&a, &["commit", "-qm", "base"]);
        git(&a, &["remote", "add", "origin", bare.to_str().unwrap()]);
        git(&a, &["push", "-q", "origin", "main"]);
        git(&root, &["clone", "-q", bare.to_str().unwrap(), "b"]);
        (root, a, b)
    }

    // The trunk remote is named on the child, as the hermetic harness names it.
    fn run(b: &Path) -> Fresh {
        check_with(b, &[("GOAT_ANSIBLE_TRUNK_REMOTE", "origin")], &[TRUNK_ENV, "GOAT_DEPLOY_OFFLINE"]).unwrap()
    }

    #[test]
    fn current_runs_behind_refuses_ahead_warns() {
        let (root, a, b) = pair("c3");
        let f = run(&b);
        assert!(!f.refused(), "{}", f.check.detail);
        assert_eq!(f.trunk.len(), 40);

        // The trunk gains a role change this checkout lacks: refused, GB-064, with the command.
        fs::write(a.join("ops/ansible/x"), "2").unwrap();
        git(&a, &["commit", "-qam", "role change"]);
        git(&a, &["push", "-q", "origin", "main"]);
        let f = run(&b);
        assert!(f.refused());
        assert!(f.check.detail.contains("GB-064") && f.check.detail.contains("role change"), "{}", f.check.detail);
        assert!(f.check.fix.as_deref().unwrap().contains("pull --ff-only origin main"));

        // Brought current, then a branch with its own commit: runs, and names it.
        git(&b, &["pull", "-q", "--ff-only", "origin", "main"]);
        git(&b, &["checkout", "-qb", "feat"]);
        fs::create_dir_all(b.join("docs")).unwrap();
        fs::write(b.join("docs/f"), "f").unwrap();
        git(&b, &["add", "-A"]);
        git(&b, &["commit", "-qm", "feature commit"]);
        let f = run(&b);
        assert!(!f.refused());
        assert!(f.check.detail.contains("WARN") && f.check.detail.contains("feature commit"), "{}", f.check.detail);

        // A trunk change outside what a deploy reads does not block a branch.
        fs::create_dir_all(a.join("docs")).unwrap();
        fs::write(a.join("docs/d"), "d").unwrap();
        git(&a, &["add", "-A"]);
        git(&a, &["commit", "-qm", "docs only"]);
        git(&a, &["push", "-q", "origin", "main"]);
        assert!(!run(&b).refused());

        // The trunk cannot be fetched: refused, GB-065, naming --offline.
        git(&b, &["remote", "set-url", "origin", "/nonexistent/o.git"]);
        let f = run(&b);
        assert!(f.refused());
        assert!(f.check.detail.contains("GB-065") && f.check.detail.contains("--offline"), "{}", f.check.detail);
        let _ = fs::remove_dir_all(root);
    }
}
