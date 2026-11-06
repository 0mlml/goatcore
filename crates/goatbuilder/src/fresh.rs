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
//!
//! An assembled tree (`goatcore` at the overlay's `goatcore.ref`, the overlay
//! copied over it, `ops/goatprivate/assemble.sh`) carries a `.goat-assembled`
//! marker at its root and is judged here instead: the `goatcore_ref` it was
//! built from must equal the tree's declared `goatcore.ref` and its HEAD, and
//! the `overlay_commit` it was built from must not be behind the overlay
//! checkout's own `<remote>/main` outside `docs/` (GB-064). The overlay
//! checkout is named by `overlay_path=` in the marker or `GOAT_OVERLAY_DIR`;
//! its remote is `GOAT_OVERLAY_TRUNK_REMOTE`, else `origin`. The trunk handed
//! to the plays is `goatcore_ref`. The rollback-ledger role records both
//! commits in every row a play from the tree writes.

use std::path::{Path, PathBuf};
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
    if let Some(m) = marker(repo)? {
        let env = |k: &str| -> Option<String> {
            if unset.contains(&k) {
                return None;
            }
            set.iter().find(|(n, _)| *n == k).map(|(_, v)| v.to_string()).or_else(|| std::env::var(k).ok())
        };
        let ctx = AssembledEnv {
            offline: env("GOAT_DEPLOY_OFFLINE").as_deref() == Some("1"),
            overlay_dir: env(OVERLAY_DIR_ENV).filter(|v| !v.is_empty()),
            overlay_remote: env(OVERLAY_REMOTE_ENV).filter(|v| !v.is_empty()).unwrap_or_else(|| "origin".into()),
        };
        return Ok(assembled(repo, &m, &ctx));
    }
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

/// The marker an assembled tree carries at its root (ops/goatprivate/assemble.sh).
pub const MARKER: &str = ".goat-assembled";
/// Names the overlay checkout when the marker does not.
pub const OVERLAY_DIR_ENV: &str = "GOAT_OVERLAY_DIR";
/// The overlay checkout's trunk remote; `origin` when unset.
pub const OVERLAY_REMOTE_ENV: &str = "GOAT_OVERLAY_TRUNK_REMOTE";
/// The overlay paths whose trunk changes do not refuse a deploy.
const OVERLAY_NOT_DEPLOYED: &str = ":(exclude)docs";

/// What `.goat-assembled` says.
#[derive(Debug, PartialEq)]
pub struct Marker {
    /// The goatcore commit the tree was built from.
    pub goatcore_ref: String,
    /// The overlay commit copied over it; `-dirty` when the overlay had uncommitted changes.
    pub overlay_commit: String,
    /// Where the overlay checkout is, when the marker names it.
    pub overlay_path: Option<String>,
}

/// Read the marker at `repo`'s root: `None` when there is none (a plain checkout).
pub fn marker(repo: &Path) -> Result<Option<Marker>> {
    let p = repo.join(MARKER);
    if !p.is_file() {
        return Ok(None);
    }
    let text = std::fs::read_to_string(&p).with_context(|| p.display().to_string())?;
    parse_marker(&text).map(Some).with_context(|| format!("{MARKER} in {}", repo.display()))
}

fn parse_marker(text: &str) -> Result<Marker> {
    let get = |k: &str| {
        text.lines()
            .filter_map(|l| l.trim().split_once('='))
            .find(|(n, _)| n.trim() == k)
            .map(|(_, v)| v.trim().to_string())
            .filter(|v| !v.is_empty())
    };
    let goatcore_ref = get("goatcore_ref").ok_or_else(|| anyhow::anyhow!("no goatcore_ref= line"))?;
    let overlay_commit = get("overlay_commit").ok_or_else(|| anyhow::anyhow!("no overlay_commit= line"))?;
    Ok(Marker { goatcore_ref, overlay_commit, overlay_path: get("overlay_path") })
}

struct AssembledEnv {
    offline: bool,
    overlay_dir: Option<String>,
    overlay_remote: String,
}

fn git_out(dir: &Path, args: &[&str]) -> Option<String> {
    let out = Command::new("git").arg("-C").arg(dir).args(args).output().ok()?;
    out.status.success().then(|| String::from_utf8_lossy(&out.stdout).trim().to_string())
}

fn refuse_row(detail: String, fix: String) -> Check {
    Check { name: "fresh-source".into(), ok: false, detail, fix: Some(fix) }
}

/// Judge an assembled tree: goatcore at its declared ref, the overlay current with its trunk.
fn assembled(repo: &Path, m: &Marker, ctx: &AssembledEnv) -> Fresh {
    let trunk = m.goatcore_ref.clone();
    let reassemble = "re-assemble from the current overlay: git -C <overlay> pull --ff-only origin main && <overlay>/assemble.sh <new dir>, then run from the new dir".to_string();
    let out = |check: Check| Fresh { trunk: trunk.clone(), check };

    // goatcore: the tree was built from the ref it declares, and still sits on it.
    let declared = match std::fs::read_to_string(repo.join("goatcore.ref")) {
        Ok(t) => t.trim().to_string(),
        Err(_) => {
            return out(refuse_row(
                format!("GB-064 REFUSE — {MARKER} names goatcore {} but the tree has no goatcore.ref to declare which goatcore it builds with (contract C3).", short(&m.goatcore_ref)),
                reassemble,
            ))
        }
    };
    if declared != m.goatcore_ref {
        return out(refuse_row(
            format!("GB-064 REFUSE — this tree was assembled from goatcore {} but goatcore.ref declares {}; a deploy from it would run other code than declared (contract C3).", short(&m.goatcore_ref), short(&declared)),
            reassemble,
        ));
    }
    if let Some(head) = git_out(repo, &["rev-parse", "HEAD"]) {
        if head != m.goatcore_ref {
            return out(refuse_row(
                format!("GB-064 REFUSE — the tree's goatcore checkout is at {} but it was assembled from {} (goatcore.ref); a deploy from it would run other code than declared (contract C3).", short(&head), short(&m.goatcore_ref)),
                reassemble,
            ));
        }
    }

    // The overlay: the commit copied in is current with the overlay's trunk.
    let overlay_commit = m.overlay_commit.trim_end_matches("-dirty");
    let dirty = overlay_commit.len() != m.overlay_commit.len();
    let Some(dir) = m.overlay_path.clone().or_else(|| ctx.overlay_dir.clone()) else {
        if ctx.offline {
            return out(Check { name: "fresh-source".into(), ok: true, detail: format!("OFFLINE — assembled tree: goatcore {} as declared; overlay {} not judged (no overlay checkout named). The run is recorded as offline.", short(&m.goatcore_ref), short(overlay_commit)), fix: None });
        }
        return out(refuse_row(
            format!("GB-064 REFUSE — assembled tree: goatcore {} as declared, but overlay {} cannot be shown current with its trunk because no overlay checkout is named (contract C3).", short(&m.goatcore_ref), short(overlay_commit)),
            format!("name the overlay checkout: {OVERLAY_DIR_ENV}=<overlay checkout> and re-run"),
        ));
    };
    let odir = PathBuf::from(&dir);
    let remote = ctx.overlay_remote.as_str();
    let otrunk = format!("{remote}/main");
    if !ctx.offline {
        let fetched = Command::new("git")
            .arg("-C")
            .arg(&odir)
            .args(["fetch", "--quiet", remote, "main"])
            .env("GIT_SSH_COMMAND", std::env::var("GIT_SSH_COMMAND").unwrap_or_else(|_| "ssh -o BatchMode=yes -o ConnectTimeout=10".into()))
            .output()
            .map(|o| o.status.success())
            .unwrap_or(false);
        if !fetched {
            return out(refuse_row(
                format!("GB-065 REFUSE — could not fetch {remote} main in the overlay checkout {dir}, so overlay {} cannot be shown current with its trunk (contract C3); refusing before any host is touched. On an islanded site, re-run with --offline.", short(overlay_commit)),
                format!("restore the connection to the overlay's {remote} and re-run"),
            ));
        }
    }
    let Some(osha) = git_out(&odir, &["rev-parse", "--verify", "--quiet", &format!("{otrunk}^{{commit}}")]) else {
        if ctx.offline {
            return out(Check { name: "fresh-source".into(), ok: true, detail: format!("OFFLINE — assembled tree: goatcore {} as declared; no cached {otrunk} in {dir} to judge overlay {} against. The run is recorded as offline.", short(&m.goatcore_ref), short(overlay_commit)), fix: None });
        }
        return out(refuse_row(format!("GB-065 REFUSE — fetched {remote} in {dir}, but it has no main branch to judge overlay {} against.", short(overlay_commit)), format!("check the overlay checkout's {remote} remote")));
    };
    if git_out(&odir, &["rev-parse", "--verify", "--quiet", &format!("{overlay_commit}^{{commit}}")]).is_none() {
        return out(refuse_row(
            format!("GB-064 REFUSE — overlay commit {} the tree was assembled from is not in the overlay checkout {dir}, so it cannot be judged against {otrunk} (contract C3).", short(overlay_commit)),
            reassemble,
        ));
    }
    let missing = git_out(&odir, &["log", "--format=%h %s", &format!("{overlay_commit}..{osha}"), "--", ".", OVERLAY_NOT_DEPLOYED]).unwrap_or_default();
    if !missing.is_empty() {
        let lines: Vec<&str> = missing.lines().collect();
        let mut d = format!(
            "GB-064 REFUSE — this tree was assembled from overlay {}, which is behind {otrunk} at {} in what a deploy reads; refusing before any host is touched (contract C3).\n  {} commit(s) on {otrunk} not in it (newest first):",
            short(overlay_commit),
            short(&osha),
            lines.len()
        );
        for l in lines.iter().take(10) {
            d.push_str(&format!("\n    {l}"));
        }
        if lines.len() > 10 {
            d.push_str(&format!("\n    … and {} more", lines.len() - 10));
        }
        return out(refuse_row(d, reassemble));
    }

    let mut said = vec![];
    if ctx.offline {
        said.push(format!("OFFLINE — not fetching the overlay's {remote}; judged against the cached {otrunk} at {}. The run is recorded as offline.", short(&osha)));
    }
    said.push(format!("assembled tree: goatcore {} as declared in goatcore.ref; overlay {} current with {otrunk} at {}", short(&m.goatcore_ref), short(overlay_commit), short(&osha)));
    let ahead = git_out(&odir, &["log", "--format=%h %s", &format!("{osha}..{overlay_commit}")]).unwrap_or_default();
    if !ahead.is_empty() {
        let lines: Vec<&str> = ahead.lines().collect();
        let mut w = format!("WARN — overlay {} carries {} commit(s) not on {otrunk}; they are deployed with this run:", short(overlay_commit), lines.len());
        for l in lines.iter().take(5) {
            w.push_str(&format!("\n    {l}"));
        }
        said.push(w);
    }
    if dirty {
        said.push(format!("WARN — the overlay had uncommitted changes when this tree was assembled ({}); they are deployed with this run and no commit records them.", m.overlay_commit));
    }
    out(Check { name: "fresh-source".into(), ok: true, detail: said.join("\n"), fix: None })
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

    fn head(dir: &Path) -> String {
        git_out(dir, &["rev-parse", "HEAD"]).unwrap()
    }

    fn commit_file(dir: &Path, rel: &str, body: &str, msg: &str) {
        let f = dir.join(rel);
        fs::create_dir_all(f.parent().unwrap()).unwrap();
        fs::write(f, body).unwrap();
        git(dir, &["add", "-A"]);
        git(dir, &["commit", "-qm", msg]);
    }

    /// An overlay trunk (bare), two overlay checkouts of it, and an assembled
    /// tree: a goatcore commit with the marker and goatcore.ref beside it.
    fn assembled_fixture(tag: &str) -> (PathBuf, PathBuf, PathBuf, PathBuf) {
        let root = std::env::temp_dir().join(format!("gb-assembled-{tag}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&root);
        fs::create_dir_all(&root).unwrap();
        let (bare, ov, other, tree) = (root.join("ov.git"), root.join("ov"), root.join("other"), root.join("tree"));
        git(&root, &["init", "-q", "--bare", "ov.git"]);
        git(&bare, &["symbolic-ref", "HEAD", "refs/heads/main"]);
        git(&root, &["init", "-q", "-b", "main", "tree"]);
        commit_file(&tree, "ops/ansible/x", "1", "goatcore base");
        let gref = head(&tree);
        git(&root, &["init", "-q", "-b", "main", "ov"]);
        commit_file(&ov, "goatcore.ref", &format!("{gref}\n"), "overlay base");
        git(&ov, &["remote", "add", "origin", bare.to_str().unwrap()]);
        git(&ov, &["push", "-q", "origin", "main"]);
        git(&root, &["clone", "-q", bare.to_str().unwrap(), "other"]);
        fs::write(tree.join("goatcore.ref"), format!("{gref}\n")).unwrap();
        fs::write(tree.join(MARKER), format!("goatcore_ref={gref}\noverlay_commit={}\n", head(&ov))).unwrap();
        (root, ov, other, tree)
    }

    fn run_assembled(tree: &Path, overlay: Option<&Path>, offline: bool) -> Fresh {
        let mut set: Vec<(&str, &str)> = vec![(OVERLAY_REMOTE_ENV, "origin")];
        let mut unset = vec![TRUNK_ENV];
        let o = overlay.map(|p| p.to_str().unwrap().to_string()).unwrap_or_default();
        if overlay.is_some() {
            set.push((OVERLAY_DIR_ENV, o.as_str()));
        } else {
            unset.push(OVERLAY_DIR_ENV);
        }
        if offline {
            set.push(("GOAT_DEPLOY_OFFLINE", "1"));
        } else {
            unset.push("GOAT_DEPLOY_OFFLINE");
        }
        check_with(tree, &set, &unset).unwrap()
    }

    #[test]
    fn marker_parses_and_requires_both_commits() {
        let m = parse_marker("goatcore_ref=abc\noverlay_commit=def-dirty\n").unwrap();
        assert_eq!(m, Marker { goatcore_ref: "abc".into(), overlay_commit: "def-dirty".into(), overlay_path: None });
        let m = parse_marker("goatcore_ref=abc\noverlay_commit=def\noverlay_path=/o\n").unwrap();
        assert_eq!(m.overlay_path.as_deref(), Some("/o"));
        assert!(parse_marker("goatcore_ref=abc\n").is_err());
        assert!(parse_marker("overlay_commit=def\n").is_err());
        assert!(parse_marker("goatcore_ref=\noverlay_commit=def\n").is_err());
        // A plain checkout carries no marker: the script avenue judges it.
        let d = std::env::temp_dir().join(format!("gb-nomarker-{}", std::process::id()));
        fs::create_dir_all(&d).unwrap();
        assert!(marker(&d).unwrap().is_none());
        let _ = fs::remove_dir_all(d);
    }

    #[test]
    fn assembled_current_runs_and_hands_the_goatcore_ref() {
        let (root, ov, _, tree) = assembled_fixture("cur");
        let f = run_assembled(&tree, Some(&ov), false);
        assert!(!f.refused(), "{}", f.check.detail);
        assert_eq!(f.trunk, head(&tree));
        assert!(f.check.detail.contains("assembled tree") && f.check.detail.contains("current with origin/main"), "{}", f.check.detail);
        // The overlay checkout named in the marker instead of the environment.
        let m = fs::read_to_string(tree.join(MARKER)).unwrap();
        fs::write(tree.join(MARKER), format!("{m}overlay_path={}\n", ov.display())).unwrap();
        assert!(!run_assembled(&tree, None, false).refused());
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn assembled_goatcore_not_as_declared_refuses() {
        let (root, ov, _, tree) = assembled_fixture("gref");
        // goatcore.ref declares another commit than the tree was built from.
        fs::write(tree.join("goatcore.ref"), "0123456789012345678901234567890123456789\n").unwrap();
        let f = run_assembled(&tree, Some(&ov), false);
        assert!(f.refused());
        assert!(f.check.detail.contains("GB-064") && f.check.detail.contains("goatcore.ref declares 012345678"), "{}", f.check.detail);
        assert!(f.check.fix.as_deref().unwrap().contains("assemble.sh"));
        // The declaration matches, but the goatcore checkout moved off it.
        let gref = head(&tree);
        fs::write(tree.join("goatcore.ref"), format!("{gref}\n")).unwrap();
        commit_file(&tree, "ops/ansible/y", "1", "moved");
        let f = run_assembled(&tree, Some(&ov), false);
        assert!(f.refused());
        assert!(f.check.detail.contains("GB-064") && f.check.detail.contains("checkout is at"), "{}", f.check.detail);
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn assembled_overlay_behind_refuses_ahead_warns() {
        let (root, ov, other, tree) = assembled_fixture("ov");
        // The overlay trunk gains a docs-only change: not what a deploy reads, runs.
        commit_file(&other, "docs/findings/f.md", "f", "docs only");
        git(&other, &["push", "-q", "origin", "main"]);
        assert!(!run_assembled(&tree, Some(&ov), false).refused());

        // The overlay trunk gains a pins change the tree lacks: refused, GB-064, named.
        commit_file(&other, "ops/pins/fleet-pins.yaml", "p", "pin bump");
        git(&other, &["push", "-q", "origin", "main"]);
        let f = run_assembled(&tree, Some(&ov), false);
        assert!(f.refused());
        assert!(f.check.detail.contains("GB-064") && f.check.detail.contains("pin bump"), "{}", f.check.detail);
        assert!(!f.check.detail.contains("docs only"), "{}", f.check.detail);

        // Assembled from an overlay commit on top of the trunk: runs, names it.
        git(&ov, &["pull", "-q", "--ff-only", "origin", "main"]);
        commit_file(&ov, "ops/ansible/group_vars/g/vars.yml", "v", "overlay change");
        let gref = head(&tree);
        fs::write(tree.join(MARKER), format!("goatcore_ref={gref}\noverlay_commit={}-dirty\n", head(&ov))).unwrap();
        let f = run_assembled(&tree, Some(&ov), false);
        assert!(!f.refused(), "{}", f.check.detail);
        assert!(f.check.detail.contains("WARN") && f.check.detail.contains("overlay change"), "{}", f.check.detail);
        assert!(f.check.detail.contains("uncommitted"), "{}", f.check.detail);
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn assembled_overlay_unknown_unreachable_or_unnamed_refuses() {
        let (root, ov, _, tree) = assembled_fixture("un");
        // No overlay checkout named: refused, naming the variable; offline runs.
        let f = run_assembled(&tree, None, false);
        assert!(f.refused());
        assert!(f.check.detail.contains("GB-064") && f.check.fix.as_deref().unwrap().contains(OVERLAY_DIR_ENV), "{}", f.check.detail);
        let f = run_assembled(&tree, None, true);
        assert!(!f.refused() && f.check.detail.contains("OFFLINE"), "{}", f.check.detail);

        // The overlay trunk cannot be fetched: GB-065; offline judges the cached trunk.
        git(&ov, &["remote", "set-url", "origin", "/nonexistent/ov.git"]);
        let f = run_assembled(&tree, Some(&ov), false);
        assert!(f.refused());
        assert!(f.check.detail.contains("GB-065") && f.check.detail.contains("--offline"), "{}", f.check.detail);
        let f = run_assembled(&tree, Some(&ov), true);
        assert!(!f.refused() && f.check.detail.contains("OFFLINE"), "{}", f.check.detail);

        // An overlay commit the checkout does not have: refused.
        let gref = head(&tree);
        fs::write(tree.join(MARKER), format!("goatcore_ref={gref}\noverlay_commit=unknown\n")).unwrap();
        let f = run_assembled(&tree, Some(&ov), true);
        assert!(f.refused());
        assert!(f.check.detail.contains("not in the overlay checkout"), "{}", f.check.detail);
        let _ = fs::remove_dir_all(root);
    }
}
