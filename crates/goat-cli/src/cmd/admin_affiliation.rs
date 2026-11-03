//! Resolve the native Ory affiliation without guessing sovereign membership.
use super::{MfAffiliation, MfOrg};
use anyhow::Context;
use std::path::Path;

#[path = "admin_affiliation_kind.rs"]
mod kind;

#[derive(serde::Deserialize)]
struct Catalog {
    orgs: Vec<Org>,
}

#[derive(serde::Deserialize)]
struct Org {
    id: String,
    #[serde(default)]
    aliases: Vec<String>,
    #[serde(default)]
    sector: String,
}

pub(super) fn resolve(
    root: &Path,
    env: &str,
    org: &MfOrg,
    source: &MfAffiliation,
    enabled: bool,
) -> anyhow::Result<Option<MfAffiliation>> {
    if !enabled {
        return Ok(None);
    }
    let mut aff = source.clone();
    if !aff.kind.trim().is_empty() {
        return Ok(Some(aff));
    }
    let tag = super::org_trait_short_tag(org, source);
    if aff.sector.trim().is_empty() && !tag.is_empty() {
        let catalog = read_catalog(root, env)?;
        // Match org_catalog.load_catalog: trim keys, skip blank ids, last id
        // wins while an alias never replaces an already registered key.
        let mut by_slug = std::collections::HashMap::new();
        for row in &catalog.orgs {
            let id = row.id.trim();
            if id.is_empty() {
                continue;
            }
            by_slug.insert(id, row);
            for alias in &row.aliases {
                by_slug.entry(alias.trim()).or_insert(row);
            }
        }
        if let Some(row) = by_slug.get(tag) {
            aff.sector.clone_from(&row.sector);
            if aff.id.trim().is_empty() {
                aff.id = row.id.trim().to_string();
            }
        }
    }
    if let Some(k) = kind::derive_kind(&aff.sector) {
        aff.kind = k.to_string();
        if aff.id.trim().is_empty() {
            aff.id = tag.to_string();
        }
    }
    Ok(Some(aff))
}

fn read_catalog(root: &Path, env: &str) -> anyhow::Result<Catalog> {
    anyhow::ensure!(
        super::is_valid_env(env),
        "invalid affiliation catalog environment"
    );
    let base = root.join("ops/identity/orgs").join(env);
    let json = base.with_extension("json");
    let path = if json.try_exists()? {
        json
    } else {
        base.with_extension("yaml")
    };
    let text = std::fs::read_to_string(&path)
        .with_context(|| format!("read affiliation catalog {}", path.display()))?;
    let catalog: Catalog = serde_yaml::from_str(&text)
        .with_context(|| format!("parse affiliation catalog {}", path.display()))?;
    anyhow::ensure!(
        catalog.orgs.iter().any(|row| !row.id.trim().is_empty()),
        "affiliation catalog has no organizations"
    );
    Ok(catalog)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn manifest_sector_matches_ansible_and_explicit_kind_wins() {
        let root = Path::new("/does-not-exist");
        for (sector, expected) in [
            ("commercial", "vendor"),
            ("civil-society", "civil"),
            ("ngo", "civil"),
            ("media", "civil"),
            ("academic", "civil"),
            (" Commercial ", "vendor"),
            ("state", ""),
            ("unknown", ""),
            ("", ""),
        ] {
            let input = MfAffiliation {
                sector: sector.into(),
                ..Default::default()
            };
            let got = resolve(root, "fixture", &MfOrg::default(), &input, true)
                .unwrap()
                .unwrap();
            assert_eq!(got.kind, expected, "{sector}");
        }
        let explicit = MfAffiliation {
            kind: "partner".into(),
            sector: "commercial".into(),
            ..Default::default()
        };
        assert_eq!(
            resolve(root, "fixture", &MfOrg::default(), &explicit, true)
                .unwrap()
                .unwrap()
                .kind,
            "partner"
        );
        assert!(
            resolve(root, "fixture", &MfOrg::default(), &explicit, false)
                .unwrap()
                .is_none()
        );
    }

    #[test]
    fn catalog_fallback_and_identity_body() {
        let tmp = tempfile::tempdir().unwrap();
        let dir = tmp.path().join("ops/identity/orgs");
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(
            dir.join("fixture.yaml"),
            "orgs:\n  - id: example-org\n    aliases: [old-org]\n    sector: commercial\n",
        )
        .unwrap();
        let org = MfOrg {
            short_tag: "old-org".into(),
            ..Default::default()
        };
        let aff = resolve(tmp.path(), "fixture", &org, &MfAffiliation::default(), true)
            .unwrap()
            .unwrap();
        assert_eq!(aff.id, "example-org");
        assert_eq!(aff.kind, "vendor");
        let body: serde_json::Value = serde_json::from_str(&super::super::build_identity_body(
            "x@example.invalid",
            "Fixture",
            &[],
            &org,
            Some(&aff),
            None,
        ))
        .unwrap();
        assert_eq!(body["metadata_public"]["aff"]["kind"], "vendor");
        assert_eq!(body["metadata_public"]["aff"]["id"], "example-org");
        assert!(body["traits"].get("aff").is_none());
        // Exercise the actual backfill verification function against the native
        // identity payload, including a negative control without affiliation.
        let payload = tmp.path().join("identity.json");
        std::fs::write(&payload, body.to_string()).unwrap();
        let identity_dir = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../ops/identity");
        let out = std::process::Command::new("python3")
            .arg("-c").arg(r#"import importlib.util,json,sys
sys.path.insert(0,sys.argv[1])
spec=importlib.util.spec_from_file_location('backfill',sys.argv[1]+'/backfill-aff-trait.py')
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
i=json.load(open(sys.argv[2]))
c={'example-org':{'id':'example-org','kind':'vendor','status':'active'}}
assert m.report_verify([i],c,{}) == 0
i['metadata_public'].pop('aff');i['metadata_public']['org']['short_tag']='example-org'
assert m.report_verify([i],c,{}) == 1
"#).arg(identity_dir).arg(payload).output().unwrap();
        assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));

        std::fs::write(dir.join("fixture.json"), "broken").unwrap();
        assert!(resolve(tmp.path(), "fixture", &org, &MfAffiliation::default(), true).is_err());
        assert!(resolve(tmp.path(), "missing", &org, &MfAffiliation::default(), true).is_err());
    }
    #[test]
    fn catalog_keys_match_python_precedence() {
        let tmp = tempfile::tempdir().unwrap();
        let dir = tmp.path().join("ops/identity/orgs");
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(dir.join("fixture.yaml"), "orgs:\n  - id: ' padded '\n    aliases: [' alias ']\n    sector: commercial\n  - id: ''\n    aliases: [blank-alias]\n    sector: commercial\n  - id: duplicate\n    sector: commercial\n  - id: duplicate\n    sector: civil-society\n").unwrap();
        for (tag, expected) in [
            ("alias", "vendor"),
            ("padded", "vendor"),
            ("blank-alias", ""),
            ("duplicate", "civil"),
        ] {
            let org = MfOrg {
                short_tag: tag.into(),
                ..Default::default()
            };
            let aff = resolve(tmp.path(), "fixture", &org, &MfAffiliation::default(), true)
                .unwrap()
                .unwrap();
            assert_eq!(aff.kind, expected, "{tag}");
        }
    }
}
