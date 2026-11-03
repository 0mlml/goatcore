// SPDX-License-Identifier: Apache-2.0
//
// aggregate.rs — the CF-7 aggregator-mode config
// (docs/design/consistency-foundation.md §7.8, ADR 1110 D5).
//
// Aggregator mode is the SAME BINARY with a WIDER INPUT SET: instead of one
// goatnet from CLI flags, a YAML file declares several — per goatnet a Zenoh
// endpoint, a cert/CA set (they are SEPARATE FABRICS with separate CAs, so
// one session each), the committed IntentV1 path, and the expected
// `ops/consistency/health/<site>` scope key. Everything downstream of the
// config is the single-mode code path: same ingest, same verify-at-read, same
// render — which is what keeps the aggregator from developing its own
// semantics (§7.8 constraint 1).
//
// What this file deliberately is NOT: a service registry, a fleet inventory,
// or an authority. The aggregator may render; it may not decide (ADR 1109
// §6.2), and a goatnet named here notices nothing — the config makes the
// aggregator able to LISTEN, and listening is all it does.
//
// Paths in the file resolve relative to the FILE, not the process cwd, so a
// committed example next to the intent tree works from any checkout. Loading
// does not stat the named files: a missing cert or artifact is a RUNTIME
// refusal rendered honestly per goatnet (an unopenable session renders its
// row; an unreadable intent renders its refusal), never a config error that
// takes the whole page down with it.

use anyhow::{bail, Context, Result};
use std::collections::BTreeSet;
use std::path::{Path, PathBuf};

/// One goatnet the aggregator listens to. In single mode the same struct is
/// built from the CLI flags — one slot, same code path.
#[derive(Debug, Clone, serde::Deserialize)]
#[serde(deny_unknown_fields)]
pub struct GoatnetCfg {
    /// Display + anchor identity on the page (e.g. a goatnet name).
    /// Unique across the config.
    pub name: String,
    /// The `ops/consistency/health/<site>` scope this goatnet's rollup
    /// publishes on (often the goatnet's own name, or a sandbox alias).
    /// Its absence renders indeterminate with age — never green, never
    /// omitted.
    pub site: String,
    /// Zenoh endpoint for THIS goatnet's fabric — `tls/…` (mTLS, needs the
    /// cert set below) or `tcp/…` (the local-dev/test path). On the laptop
    /// this is an env-door SSH forward of the router port (env-doors.md).
    pub endpoint: String,
    /// This goatnet's committed IntentV1 artifact, verified at EVERY render
    /// against `pubkey_dir` — independently per goatnet, so one goatnet's
    /// unverifiable intent refuses ITS grid only.
    pub intent_file: PathBuf,
    /// Dir of `<ca_id>.pem` SPKI public keys (ops/enrollment/public-keys).
    pub pubkey_dir: PathBuf,
    /// Leaf cert / key / CA root for the mTLS session into this goatnet's
    /// fabric. Required for a `tls/` endpoint; refused at session open (an
    /// honest per-goatnet bus note), not at config load.
    #[serde(default)]
    pub cert: Option<PathBuf>,
    #[serde(default)]
    pub key: Option<PathBuf>,
    #[serde(default)]
    pub root_ca: Option<PathBuf>,
    /// This goatnet's OWN site console, if it has one reachable from wherever
    /// this page is served. The aggregate view is a content superset of the
    /// site view, so this is not a link to missing information — it is the
    /// link to the copy that SURVIVES ISOLATION (§7.9): when the WAN is cut,
    /// this page is unreachable and that one still answers for its own site.
    /// Empty = the link is hidden, never dead (the §7.7 rule).
    #[serde(default)]
    pub console_url: Option<String>,
}

#[derive(Debug, serde::Deserialize)]
#[serde(deny_unknown_fields)]
struct AggregateConfig {
    goatnets: Vec<GoatnetCfg>,
}

fn resolve(base: &Path, p: &mut PathBuf) {
    if p.is_relative() {
        *p = base.join(&*p);
    }
}

/// Load and validate an aggregator config. Refuses loud on anything that
/// would make the page ambiguous (duplicate names, empty/path-shaped keys);
/// does NOT require the referenced files to exist — see the module comment.
pub fn load(path: &Path) -> Result<Vec<GoatnetCfg>> {
    let text = std::fs::read_to_string(path)
        .with_context(|| format!("cannot read aggregator config {}", path.display()))?;
    let cfg: AggregateConfig = serde_yaml::from_str(&text)
        .with_context(|| format!("{} is not a valid aggregator config", path.display()))?;
    if cfg.goatnets.is_empty() {
        bail!("{}: no goatnets configured — an aggregator with nothing to listen to has no page to serve", path.display());
    }
    let base = path.parent().unwrap_or_else(|| Path::new(".")).to_path_buf();
    let mut names = BTreeSet::new();
    let mut goatnets = cfg.goatnets;
    for g in &mut goatnets {
        if g.name.is_empty() || g.name.contains('/') {
            bail!("goatnet name {:?} is empty or path-shaped", g.name);
        }
        if g.site.is_empty() || g.site.contains('/') {
            bail!("goatnet {}: site {:?} is empty or path-shaped — it must be the <site> leaf of ops/consistency/health/<site>", g.name, g.site);
        }
        if g.endpoint.is_empty() {
            bail!("goatnet {}: no endpoint", g.name);
        }
        if !names.insert(g.name.clone()) {
            bail!("goatnet name {:?} appears twice — names anchor the page and must be unique (two goatnets MAY share a site key; sessions and state are per-goatnet)", g.name);
        }
        resolve(&base, &mut g.intent_file);
        resolve(&base, &mut g.pubkey_dir);
        for p in [&mut g.cert, &mut g.key, &mut g.root_ca].into_iter().flatten() {
            resolve(&base, p);
        }
    }
    Ok(goatnets)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn write_tmp(name: &str, text: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("gcc-agg-{}-{name}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let p = dir.join("aggregator.yaml");
        std::fs::write(&p, text).unwrap();
        p
    }

    /// The committed example config parses — pinned so the example an
    /// operator copies can never rot silently.
    #[test]
    fn the_committed_example_config_loads() {
        if !std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../../intent").is_dir() {
            return; // reads this deployment's own artifacts; a public clone has none
        }
        let example = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../../aggregator/aggregator.example.yaml");
        let goatnets = load(&example).expect("the committed example must load");
        assert_eq!(goatnets.len(), 2, "the example names both governed goatnets");
        assert_eq!(goatnets[0].name, "zagros");
        assert_eq!(goatnets[0].site, "zagros-sandbox");
        assert_eq!(goatnets[1].name, "zagros-prod");
        assert_eq!(goatnets[1].site, "zagros-prod");
        // Relative paths resolved against the config file: the committed
        // intent artifacts are reachable exactly as the example names them.
        assert!(goatnets[0].intent_file.exists(), "example intent_file resolves to the committed artifact");
        assert!(goatnets[1].intent_file.exists());
        assert!(goatnets[0].pubkey_dir.exists(), "example pubkey_dir resolves to the committed SPKI set");
    }

    #[test]
    fn duplicate_names_and_unknown_keys_refuse_loud() {
        let dup = write_tmp(
            "dup",
            "goatnets:\n  - {name: a, site: s1, endpoint: tcp/1:1, intent_file: i, pubkey_dir: p}\n  - {name: a, site: s2, endpoint: tcp/1:2, intent_file: i, pubkey_dir: p}\n",
        );
        assert!(load(&dup).unwrap_err().to_string().contains("twice"));

        let typo = write_tmp(
            "typo",
            "goatnets:\n  - {name: a, site: s, endpoint: tcp/1:1, intent_file: i, pubkey_dir: p, endpont: oops}\n",
        );
        assert!(load(&typo).is_err(), "a typo'd key is refused, never silently ignored");

        let empty = write_tmp("empty", "goatnets: []\n");
        assert!(load(&empty).unwrap_err().to_string().contains("no goatnets"));
    }

    #[test]
    fn two_goatnets_may_share_a_site_key_because_state_is_per_goatnet() {
        // Two separate fabrics can legitimately both publish
        // ops/consistency/health/site-a; each slot has its own session and
        // its own latest-map, so this is representable, not a collision.
        let p = write_tmp(
            "shared-site",
            "goatnets:\n  - {name: a, site: s, endpoint: tcp/1:1, intent_file: i, pubkey_dir: p}\n  - {name: b, site: s, endpoint: tcp/1:2, intent_file: i, pubkey_dir: p}\n",
        );
        assert_eq!(load(&p).unwrap().len(), 2);
    }
}
