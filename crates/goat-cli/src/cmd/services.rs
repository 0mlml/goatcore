// `goat services` — read ops/serve/services.yaml and probe each
// service's mesh-internal health endpoint. Parallel HTTPS GETs with
// a short timeout; renders as glyph lines or one --json object so
// wrapper scripts can branch.
//
// services.yaml is the SoT per CLAUDE.md §Hosted services. We read
// it directly from the repo root (not a portal endpoint) because the
// portal doesn't expose it and the registry lives in-repo by design.

use clap::Args;
use futures::future::join_all;
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};
use std::time::Duration;

const PROBE_TIMEOUT: Duration = Duration::from_secs(5);

#[derive(Args, Debug)]
pub struct ServicesArgs {
    /// Override repo root (default: walk up from cwd for `.git`).
    #[arg(long)]
    pub repo_root: Option<PathBuf>,
    /// Emit one JSON object instead of pretty glyph lines.
    #[arg(long)]
    pub json: bool,
    /// Probe only services whose name matches this substring.
    #[arg(long)]
    pub filter: Option<String>,
}

#[derive(Debug, Deserialize)]
struct ServicesYaml {
    services: Vec<ServiceEntry>,
    domain_suffix: String,
}

#[derive(Debug, Deserialize, Clone)]
struct ServiceEntry {
    name: String,
    #[serde(default)]
    health_path: Option<String>,
}

#[derive(Debug, Serialize)]
struct ServiceVerdict {
    name: String,
    url: String,
    ok: bool,
    summary: String,
}

pub async fn run(_ctx: &super::Ctx, args: ServicesArgs) -> anyhow::Result<()> {
    let cwd = std::env::current_dir()?;
    let repo_root = match args.repo_root.as_deref() {
        Some(p) => p.to_path_buf(),
        None => discover_repo_root(&cwd).ok_or_else(|| {
            anyhow::anyhow!(
                "could not locate repo root from {} (no .git ancestor); pass --repo-root <path>",
                cwd.display()
            )
        })?,
    };

    let yaml_path = repo_root.join("ops/serve/services.yaml");
    let body = std::fs::read_to_string(&yaml_path)
        .map_err(|e| anyhow::anyhow!("read {}: {e}", yaml_path.display()))?;
    let parsed: ServicesYaml = serde_yaml::from_str(&body)
        .map_err(|e| anyhow::anyhow!("parse {}: {e}", yaml_path.display()))?;

    let filter = args.filter.as_deref();
    let entries: Vec<ServiceEntry> = parsed
        .services
        .into_iter()
        .filter(|s| filter.map(|f| s.name.contains(f)).unwrap_or(true))
        .collect();

    if entries.is_empty() {
        anyhow::bail!(
            "no services matched {} in {}",
            filter.map(|f| format!("filter `{f}`")).unwrap_or_else(|| "(no filter)".into()),
            yaml_path.display(),
        );
    }

    let client = reqwest::Client::builder()
        .timeout(PROBE_TIMEOUT)
        .build()
        .map_err(|e| anyhow::anyhow!("build reqwest client: {e}"))?;

    let domain_suffix = parsed.domain_suffix;
    let probes = entries
        .into_iter()
        .map(|svc| {
            let client = client.clone();
            let domain = domain_suffix.clone();
            async move { probe(&client, &domain, svc).await }
        })
        .collect::<Vec<_>>();

    let verdicts: Vec<ServiceVerdict> = join_all(probes).await;

    if args.json {
        println!("{}", serde_json::to_string(&verdicts).unwrap_or_default());
    } else {
        let name_w = verdicts.iter().map(|v| v.name.len()).max().unwrap_or(0);
        for v in &verdicts {
            let glyph = if v.ok { "[✓]" } else { "[✗]" };
            println!(
                "{glyph} {name:<width$}  {summary}",
                glyph = glyph,
                name = v.name,
                width = name_w,
                summary = v.summary,
            );
        }
    }

    if verdicts.iter().any(|v| !v.ok) {
        std::process::exit(crate::exit_codes::EXIT_GENERIC);
    }
    Ok(())
}

async fn probe(client: &reqwest::Client, domain: &str, svc: ServiceEntry) -> ServiceVerdict {
    let path = svc.health_path.as_deref().unwrap_or("/");
    let url = format!("https://{}.{}{}", svc.name, domain, path);
    match client.get(&url).send().await {
        Ok(resp) => {
            let status = resp.status();
            if status.is_success() {
                ServiceVerdict {
                    name: svc.name,
                    url,
                    ok: true,
                    summary: format!("{} {}", status.as_u16(), status.canonical_reason().unwrap_or("")),
                }
            } else {
                ServiceVerdict {
                    name: svc.name,
                    url,
                    ok: false,
                    summary: format!(
                        "{} {}",
                        status.as_u16(),
                        status.canonical_reason().unwrap_or("")
                    ),
                }
            }
        }
        Err(e) => {
            // Walk the error source chain so DNS / TCP / TLS failures
            // surface with their root cause, not just reqwest's
            // top-level "error sending request" wrapper. Operators
            // running `goat services` from a laptop without mesh
            // CoreDNS configured see the actual NXDOMAIN, not a
            // generic "connect failed."
            let root = root_cause(&e);
            let reason = if e.is_timeout() {
                format!("timed out in {}s", PROBE_TIMEOUT.as_secs())
            } else if e.is_connect() {
                format!("connect failed: {root}")
            } else {
                root.to_string()
            };
            ServiceVerdict {
                name: svc.name,
                url,
                ok: false,
                summary: reason,
            }
        }
    }
}

fn root_cause(e: &(dyn std::error::Error + 'static)) -> String {
    let mut cur: &dyn std::error::Error = e;
    while let Some(src) = cur.source() {
        cur = src;
    }
    cur.to_string()
}

fn discover_repo_root(start: &Path) -> Option<PathBuf> {
    let mut cur = start.to_path_buf();
    loop {
        if cur.join(".git").exists() {
            return Some(cur);
        }
        if !cur.pop() {
            return None;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn services_yaml_parses_ignoring_unrelated_fields() {
        // services.yaml carries many fields (description, container_*,
        // compose_dir, archived_services); ServiceEntry deserializes
        // only the two we use and ignores the rest via serde's default
        // permissive map deserialization.
        let yaml = r#"
services:
  - name: snitch
    description: Mesh network performance monitor
    type: container
    compose_dir: ops/snitch
    container_name: snitch
    container_port: 8080
    health_path: /health
  - name: smoketest
    description: smoketest
    container_port: 8080
domain_suffix: netbird.legacy.example
tls_certresolver: letsencrypt
"#;
        let parsed: ServicesYaml = serde_yaml::from_str(yaml).unwrap();
        assert_eq!(parsed.services.len(), 2);
        assert_eq!(parsed.services[0].name, "snitch");
        assert_eq!(parsed.services[0].health_path.as_deref(), Some("/health"));
        assert_eq!(parsed.services[1].health_path, None);
        assert_eq!(parsed.domain_suffix, "netbird.legacy.example");
    }

    #[test]
    fn verdict_serializes() {
        let v = ServiceVerdict {
            name: "snitch".into(),
            url: "https://snitch.netbird.legacy.example/health".into(),
            ok: true,
            summary: "200 OK".into(),
        };
        let s = serde_json::to_string(&v).unwrap();
        assert!(s.contains("\"ok\":true"));
        assert!(s.contains("\"name\":\"snitch\""));
    }
}
