//! Hidden `goatbuilder _complete` — one candidate per line on stdout, for the
//! operator-installed shell hooks that `completion <shell>` cannot generate
//! statically: the sites, components and goatnets THIS checkout declares right
//! now. Pattern lifted from goat's `_complete` (crates/goat-cli/src/cmd/complete.rs),
//! `gh __complete` and `kubectl __complete`.
//!
//! Every candidate is a pure local read of the repository — no host is touched —
//! so completion always works and never blocks a prompt. Any read error yields
//! an empty list and exit 0: a shell hook must never fail loudly, or it corrupts
//! the interactive line the operator is typing.
//!
//! Wiring example (~/.zshrc, after `goatbuilder completion zsh > …`):
//!
//!   _goatbuilder_site() {
//!     local parts=("${(@f)$(goatbuilder _complete site "${words[CURRENT]}" 2>/dev/null)}")
//!     compadd -- "${parts[@]}"
//!   }

use std::path::Path;

use clap::Subcommand;

#[derive(Subcommand, Debug)]
pub enum CompleteCmd {
    /// Goatnet site names, from `ops/ansible/group_vars/<site>_hosts/` (for
    /// `--site`). The hyphenated form `doctor --site` takes.
    Site { partial: String },
    /// Component names, from `ops/pins/fleet-pins.yaml` (for `--component`).
    Component { partial: String },
    /// Goatnet / environment names, from `ops/pins/fleet-pins.yaml` goatnets
    /// and the release ladder's rungs (for `--env`).
    Env { partial: String },
}

pub fn run(repo: &Path, op: &CompleteCmd) {
    let (candidates, partial) = match op {
        CompleteCmd::Site { partial } => (sites(repo), partial),
        CompleteCmd::Component { partial } => (components(repo), partial),
        CompleteCmd::Env { partial } => (envs(repo), partial),
    };
    for c in candidates {
        if c.starts_with(partial.as_str()) {
            println!("{c}");
        }
    }
}

/// Every `<site>_hosts` group in group_vars, as the hyphenated site name a
/// `--site` flag takes. Sorted and de-duplicated.
pub fn sites(repo: &Path) -> Vec<String> {
    let mut out: Vec<String> = Vec::new();
    if let Ok(entries) = std::fs::read_dir(repo.join("ops/ansible/group_vars")) {
        for e in entries.flatten() {
            if !e.file_type().map(|t| t.is_dir()).unwrap_or(false) {
                continue;
            }
            if let Some(name) = e.file_name().to_str() {
                if let Some(site) = name.strip_suffix("_hosts") {
                    out.push(site.replace('_', "-"));
                }
            }
        }
    }
    sort_unique(out)
}

/// The component names fleet-pins declares.
pub fn components(repo: &Path) -> Vec<String> {
    sort_unique(top_level_keys(repo, "components"))
}

/// The goatnets fleet-pins declares, plus the stages of this checkout's
/// promotion path, so `--env <TAB>` offers what this project calls them.
pub fn envs(repo: &Path) -> Vec<String> {
    let mut out = top_level_keys(repo, "goatnets");
    out.extend(stages(repo));
    sort_unique(out)
}

/// The stage names in `promotion.path` of goatbuilder.yaml. Empty on any read
/// or parse failure, like every other candidate here.
fn stages(repo: &Path) -> Vec<String> {
    let Ok(v) = crate::config::load_value(&repo.join("ops/goatbuilder/goatbuilder.yaml")) else {
        return Vec::new();
    };
    v.pointer("/promotion/path")
        .and_then(|p| p.as_array())
        .map(|path| path.iter().filter_map(|s| s.get("name")?.as_str().map(String::from)).collect())
        .unwrap_or_default()
}

/// Keys of a top-level mapping in `ops/pins/fleet-pins.yaml` (`components` or
/// `goatnets`). Empty on any read or parse failure — completion stays silent.
fn top_level_keys(repo: &Path, section: &str) -> Vec<String> {
    let Ok(text) = std::fs::read_to_string(repo.join("ops/pins/fleet-pins.yaml")) else {
        return Vec::new();
    };
    let Ok(doc) = serde_yaml::from_str::<serde_yaml::Value>(&text) else {
        return Vec::new();
    };
    doc.get(section)
        .and_then(|m| m.as_mapping())
        .map(|m| m.keys().filter_map(|k| k.as_str().map(String::from)).collect())
        .unwrap_or_default()
}

fn sort_unique(mut v: Vec<String>) -> Vec<String> {
    v.sort();
    v.dedup();
    v
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture() -> tempfile::TempDir {
        let t = tempfile::tempdir().unwrap();
        let r = t.path();
        // Two `<site>_hosts` groups, one ordinary group (no suffix), one file.
        for g in ["alpha_dev_hosts", "alpha_prod_hosts", "all"] {
            std::fs::create_dir_all(r.join("ops/ansible/group_vars").join(g)).unwrap();
        }
        std::fs::write(r.join("ops/ansible/group_vars/notes.txt"), "x").unwrap();
        std::fs::create_dir_all(r.join("ops/pins")).unwrap();
        std::fs::write(
            r.join("ops/pins/fleet-pins.yaml"),
            "components:\n  portal:\n    x: 1\n  microscope:\n    x: 2\ngoatnets:\n  alpha-dev:\n    members: []\n  alpha-prod:\n    members: []\n",
        )
        .unwrap();
        std::fs::create_dir_all(r.join("ops/goatbuilder")).unwrap();
        std::fs::write(r.join("ops/goatbuilder/goatbuilder.yaml"), "promotion:\n  path:\n    - {name: trial}\n    - {name: live}\n").unwrap();
        t
    }

    #[test]
    fn sites_are_the_hyphenated_hosts_groups_dirs_only() {
        let t = fixture();
        // `all` has no `_hosts` suffix and notes.txt is a file: both excluded.
        assert_eq!(sites(t.path()), vec!["alpha-dev", "alpha-prod"]);
    }

    #[test]
    fn components_and_envs_come_from_fleet_pins_envs_add_the_declared_stages() {
        let t = fixture();
        assert_eq!(components(t.path()), vec!["microscope", "portal"]);
        assert_eq!(envs(t.path()), vec!["alpha-dev", "alpha-prod", "live", "trial"]);
    }

    #[test]
    fn a_missing_repository_is_an_empty_list_not_an_error() {
        let t = tempfile::tempdir().unwrap();
        assert!(sites(t.path()).is_empty());
        assert!(components(t.path()).is_empty());
        assert!(envs(t.path()).is_empty());
    }
}
