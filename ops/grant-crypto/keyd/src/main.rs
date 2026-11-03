//! `grant-keyd` — the E1 key-distribution daemon.
//!
//! Loads a config, opens one Zenoh session, and sweeps the declared release-sets:
//! for each, resolve its audience (operator-declared members + cert-dir keys),
//! and rotate — mint DEK, wrap to every member, sign, publish `keys/<rs>/<gen>` —
//! only when the audience changed. `--once` does a single sweep (a manual kick /
//! the change-driven path) and exits; without it, it polls every
//! `poll_interval_secs`.
//!
//! Usage: `grant-keyd <config.json> [--once]`, or `grant-keyd --healthcheck
//! [config.json] [--addr ip:port] [--strict]` to probe a running daemon.

use std::path::Path;
use std::time::Duration;

use grant_crypto::service::{AudienceResolver, KeyService};
use grant_keyd::config::Config;
use grant_keyd::daemon::Daemon;
use grant_keyd::directory::AudienceClient;
use grant_keyd::metrics::{AudienceSource, KeySourceMode, Metrics};
use grant_keyd::portal::{PortalCertRecipientKeys, PortalCertValidator, PortalSyncedRecipientKeys};
use grant_keyd::publisher::ZenohBundlePublisher;
use grant_keyd::recipient::{CertDirRecipientKeys, ChainedRecipientKeys, RecipientKeys};
use grant_keyd::resolver::{ConfiguredAudience, GrantRowAudienceResolver};
use grant_keyd::signer::{adoptable, SignerFiles};
use grant_keyd::svid::{SvidRecipientKeys, SvidValidator, TrustBundle};

type BoxErr = Box<dyn std::error::Error + Send + Sync>;

/// The container's config mount point (see the grant-keyd role's compose
/// template). `--healthcheck` with no arguments reads it when present, so the
/// probe follows a changed `metrics_listen` without the healthcheck command
/// having to carry the path.
const CONTAINER_CONFIG: &str = "/config/config.json";

fn main() -> Result<(), BoxErr> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args
        .iter()
        .any(|a| a == "--healthcheck" || a == "healthcheck")
    {
        std::process::exit(healthcheck(&args));
    }
    let once = args.iter().any(|a| a == "--once");
    let config_path = args
        .iter()
        .find(|a| !a.starts_with("--"))
        .ok_or("usage: grant-keyd <config.json> [--once]")?;

    let cfg = Config::load(Path::new(config_path))?;

    // Stand the observability surface up FIRST — before the signing key, the
    // Zenoh session, and the loop. Everything below can block or fail, and a
    // daemon that never reaches its first sweep is exactly the state that used to
    // be invisible; with the listener already bound, /healthz reports it.
    let metrics = Metrics::new(
        cfg.signer_id.clone(),
        // Config::validate enforces both-or-neither on each pair, so one half
        // decides the mode. Kept in step with `recipient_source` below.
        KeySourceMode::from_sources(cfg.portal_ca_bundle.is_some(), cfg.trust_bundle.is_some()),
        if cfg.portal_url.is_some() {
            AudienceSource::PortalGrantRows
        } else {
            AudienceSource::OperatorDeclared
        },
        cfg.poll_interval_secs,
    );
    if once {
        eprintln!("grant-keyd: --once, metrics listener not started");
    } else if cfg.metrics_listen.trim().is_empty() {
        eprintln!(
            "grant-keyd: WARNING — metrics_listen is empty; /metrics and /healthz are OFF and \
             this daemon has no external liveness signal"
        );
    } else {
        let bound = grant_keyd::metrics::serve(&cfg.metrics_listen, metrics.clone())
            .map_err(|e| format!("metrics listener {}: {e}", cfg.metrics_listen))?;
        eprintln!(
            "grant-keyd: metrics on http://{bound}/metrics, health on http://{bound}/healthz"
        );
    }

    // The signer is re-read every sweep: spiffe-helper renews the SVID and its
    // key on disk about hourly, and a bundle carrying an expired SVID is refused
    // by every reader (see grant_keyd::signer).
    let mut signer_files =
        SignerFiles::new(cfg.signing_key_pem.clone(), cfg.signer_svid_pem.clone());
    let (signer, svid) = signer_files
        .reload_if_changed()?
        .ok_or("signer files unreadable at start")?;
    let mut svc = KeyService::new(signer, cfg.signer_id.clone());
    if let Some(svid) = svid {
        svc = svc.with_signer_svid(svid);
        eprintln!("grant-keyd: bundles will carry the key-service SVID (self-describing)");
    }
    // Authenticates this daemon's own bundles read back from storage after a
    // restart. Without a SPIRE trust bundle there is nothing to check them
    // against, so nothing is adopted and generations restart at 1 as before.
    let own_bundles = match (&cfg.trust_bundle, &cfg.trust_domain) {
        (Some(b), Some(d)) => Some((
            SvidValidator::new(d.clone(), TrustBundle::load(b)?),
            if cfg.signer_id.starts_with("spiffe://") {
                cfg.signer_id.clone()
            } else {
                format!("spiffe://{d}/{}", cfg.signer_id)
            },
        )),
        _ => None,
    };
    let mut looked_up: std::collections::BTreeSet<String> = Default::default();
    let mut resign = false;

    let publisher = ZenohBundlePublisher::connect(zenoh_config(&cfg))?;
    let key_source = recipient_source(&cfg)?;
    // Select the audience source: the portal's grant-row endpoint (a data owner's
    // grants drive the audience, Piece B) when portal_url is set, else the
    // operator-declared member lists. Either way the member id → key resolution
    // uses the same key source, and the daemon loop is identical.
    let resolver: Box<dyn AudienceResolver> = match &cfg.portal_url {
        Some(url) => {
            let token = cfg.portal_token.clone().unwrap_or_default();
            eprintln!("grant-keyd: grant-row audience from portal {url:?}");
            Box::new(GrantRowAudienceResolver::new(
                AudienceClient::new(url.clone(), token),
                key_source,
            ))
        }
        None => {
            eprintln!("grant-keyd: operator-declared audience (release_sets member lists)");
            Box::new(ConfiguredAudience::new(
                cfg.release_sets.clone(),
                key_source,
            ))
        }
    };
    let mut daemon = Daemon::new(svc, resolver, publisher);

    // The sweep list = which release-sets to rotate. In grant-row mode it is
    // DERIVED each sweep from the portal's producers-with-active-grants
    // (`GET /api/access/release-sets`), so a `goat access grant` alone puts a
    // producer in scope with no operator config edit (WS4 auto-sweep-list). The
    // operator-declared `release_sets` KEYS are the union floor — a producer the
    // operator pinned is always swept even before it has a grant row. In
    // operator-declared mode (no portal_url) the sweep list is exactly the keys.
    let sweep_client = cfg
        .portal_url
        .as_ref()
        .map(|url| AudienceClient::new(url.clone(), cfg.portal_token.clone().unwrap_or_default()));
    let configured_sets: Vec<String> = cfg.release_sets.keys().cloned().collect();
    eprintln!(
        "grant-keyd: signer {:?}, {}, {}",
        cfg.signer_id,
        if sweep_client.is_some() {
            format!(
                "grant-row sweep list (derived) + {} pinned",
                configured_sets.len()
            )
        } else {
            format!("{} configured release-set(s)", configured_sets.len())
        },
        if once {
            "single sweep".into()
        } else {
            format!("polling every {}s", cfg.poll_interval_secs)
        }
    );
    loop {
        // Derive the sweep set. Grant-row mode: portal-listed producers ∪ any
        // pinned keys. On a portal error, fall back to the pinned keys and log —
        // fail-closed on the *audience* (a producer with no resolvable audience
        // simply doesn't rotate), never on the enumeration.
        let sets: Vec<String> = match &sweep_client {
            Some(c) => match c.release_sets() {
                Ok(mut derived) => {
                    for k in &configured_sets {
                        if !derived.contains(k) {
                            derived.push(k.clone());
                        }
                    }
                    derived
                }
                Err(e) => {
                    eprintln!(
                        "grant-keyd: release-set enumeration failed ({e:?}); sweeping {} pinned only",
                        configured_sets.len()
                    );
                    metrics.record_enumeration_failure();
                    configured_sets.clone()
                }
            },
            None => configured_sets.clone(),
        };
        match signer_files.reload_if_changed() {
            Ok(Some((key, svid))) => {
                daemon.set_signer(key, svid);
                eprintln!("grant-keyd: signer SVID renewed; re-signing retained bundles");
                resign = true;
            }
            Ok(None) => {}
            Err(e) => eprintln!("grant-keyd: signer reload: {e}"),
        }
        for rs in &sets {
            if !looked_up.insert(rs.clone()) {
                continue;
            }
            let Some((validator, ks_id)) = &own_bundles else {
                continue;
            };
            match daemon.publisher().stored(rs) {
                Ok(payloads) => {
                    let (found, refused) = adoptable(&payloads, rs, validator, ks_id);
                    if !found.is_empty() || refused > 0 {
                        eprintln!(
                            "grant-keyd: {rs}: adopted {} stored generation(s) {:?}, refused {refused}",
                            found.len(),
                            found.iter().map(|b| b.gen).collect::<Vec<_>>()
                        );
                    }
                    resign |= !found.is_empty();
                    for b in found {
                        daemon.adopt(b);
                    }
                }
                Err(e) => {
                    // Try again next sweep rather than risk minting a generation
                    // number that storage already holds.
                    looked_up.remove(rs);
                    eprintln!("grant-keyd: {rs}: reading stored bundles failed ({e:?})");
                }
            }
        }
        let outcomes = daemon.sweep(&sets);
        for outcome in &outcomes {
            eprintln!("{outcome:?}");
        }
        // Recorded on every pass, including an all-Unchanged one: that is the
        // heartbeat `grant_keyd_last_sweep_timestamp_seconds` carries.
        metrics.record_sweep(sets.len(), &outcomes);
        // Drift is state, not an event: it must be re-published every sweep so
        // the gauge keeps climbing while a rotation stays stuck, and returns to
        // zero the sweep it finally lands.
        metrics.record_drift(daemon.unpublished_since(), daemon.last_rotation_ms());
        if resign {
            match daemon.republish() {
                Ok(n) => {
                    eprintln!("grant-keyd: re-signed and republished {n} bundle(s)");
                    resign = false;
                }
                Err(e) => eprintln!("grant-keyd: republish failed ({e:?}); retrying next sweep"),
            }
        }
        if once {
            break;
        }
        std::thread::sleep(Duration::from_secs(cfg.poll_interval_secs));
    }
    Ok(())
}

/// `--healthcheck` — the daemon probing its own metrics listener, and the exit
/// code a container healthcheck reads.
///
/// The runtime image is Chainguard distroless: no shell, no curl, so a
/// wget-style probe is unrunnable and would pin the container `Up (unhealthy)`
/// forever. The binary is therefore its own probe.
///
/// **Semantics — what does and does not count as unhealthy:**
///
/// - **0** — `/healthz` says the sweep loop is turning.
/// - **1** — `/healthz` returned 503: no sweep has completed for three poll
///   intervals (floor 90s), counted from process start. The loop is wedged, or
///   never reached its first sweep. This is the real liveness failure.
/// - **2** — the endpoint could not be reached at all, or there is no address to
///   probe (`metrics_listen` empty/unparseable, unreadable config).
/// - **3** — `--strict` only: healthy, but resolving members through the
///   UNVALIDATED break-glass cert-dir source.
///
/// Two conditions are deliberately NOT failures by default. **Rotation age**:
/// keyd rotates only when an audience changes, so on a correct quiet fabric the
/// last rotation is legitimately days old — failing on it would restart a
/// healthy daemon. **Break-glass mode**: a daemon deliberately running the
/// offline break-glass source must not be restart-looped by its own healthcheck,
/// and that posture is already alertable on
/// `grant_keyd_key_source_validated == 0`; `--strict` opts in for deployments
/// that treat it as unacceptable.
///
/// Output goes to stdout/stderr, which docker records in `Health.Log`.
fn healthcheck(args: &[String]) -> i32 {
    let flag = |name: &str| -> Option<String> {
        args.iter()
            .position(|a| a == name)
            .and_then(|i| args.get(i + 1))
            .cloned()
    };
    let strict = args.iter().any(|a| a == "--strict");

    // Address: --addr wins; else a named config; else the container's config
    // mount if it is there; else the documented default port.
    let target = match flag("--addr") {
        Some(addr) => grant_keyd::metrics::probe_target(&addr),
        None => {
            let named = args
                .iter()
                .find(|a| !a.starts_with("--") && a.as_str() != "healthcheck")
                .cloned();
            let path = named.or_else(|| {
                Path::new(CONTAINER_CONFIG)
                    .exists()
                    .then(|| CONTAINER_CONFIG.to_string())
            });
            match path {
                Some(p) => match Config::load(Path::new(&p)) {
                    Ok(cfg) => grant_keyd::metrics::probe_target(&cfg.metrics_listen),
                    Err(e) => Err(format!("config {p}: {e:?}")),
                },
                None => grant_keyd::metrics::probe_target("127.0.0.1:9464"),
            }
        }
    };
    let target = match target {
        Ok(t) => t,
        Err(e) => {
            eprintln!("grant-keyd healthcheck: {e}");
            return 2;
        }
    };

    let (status, body) = match grant_keyd::metrics::probe(target, "/healthz") {
        Ok(r) => r,
        Err(e) => {
            eprintln!("grant-keyd healthcheck: {target} unreachable: {e}");
            return 2;
        }
    };
    let state = body.trim();
    if status != 200 {
        eprintln!("grant-keyd healthcheck: {target} unhealthy ({status}): {state}");
        return 1;
    }
    if strict {
        match grant_keyd::metrics::probe(target, "/metrics") {
            Ok((_, metrics)) => {
                if metrics
                    .lines()
                    .any(|l| l == "grant_keyd_key_source_validated 0")
                {
                    eprintln!(
                        "grant-keyd healthcheck: --strict — the loop is healthy but the \
                         UNVALIDATED break-glass cert-dir key source is in use: {state}"
                    );
                    return 3;
                }
            }
            Err(e) => {
                eprintln!("grant-keyd healthcheck: --strict — /metrics unreadable: {e}");
                return 2;
            }
        }
    }
    println!("grant-keyd healthcheck: {state}");
    0
}

/// Select the recipient-key source. Two validated sources can be configured,
/// covering the two identity planes members live on: **portal-CA** (portal-minted
/// orgs/partners/humans/non-SPIRE machines — chain to the offline-CA root, CN +
/// URI-SAN bound to the member id) and **SVID** (SPIRE workloads). With both set
/// they chain (first validated success wins); with neither, the unvalidated
/// cert-dir stopgap runs with a loud warning. All read PEMs from `cert_dir`.
fn recipient_source(cfg: &Config) -> Result<Box<dyn RecipientKeys>, BoxErr> {
    let mut sources: Vec<Box<dyn RecipientKeys>> = Vec::new();
    if let (Some(bundle_path), Some(prefix)) = (&cfg.portal_ca_bundle, &cfg.portal_uri_prefix) {
        let bundle = TrustBundle::load(bundle_path)?;
        let validator = PortalCertValidator::new(prefix.clone(), bundle);
        // With a portal_url the source also SYNCS each member's current public
        // leaf from the portal's recipient-cert store into cert_dir before
        // validating (U7 publish-my-key distribution — a fresh mint or rotation
        // becomes wrappable with no hand-staging). Without one, it reads only
        // what the operator staged.
        match &cfg.portal_url {
            Some(url) => {
                let token = cfg.portal_token.clone().unwrap_or_default();
                eprintln!(
                    "grant-keyd: portal-CA-validated key source, portal-synced from {url:?} (identity prefix {prefix:?})"
                );
                sources.push(Box::new(PortalSyncedRecipientKeys::new(
                    AudienceClient::new(url.clone(), token),
                    &cfg.cert_dir,
                    validator,
                )));
            }
            None => {
                eprintln!(
                    "grant-keyd: portal-CA-validated key source (identity prefix {prefix:?})"
                );
                sources.push(Box::new(PortalCertRecipientKeys::new(
                    &cfg.cert_dir,
                    validator,
                )));
            }
        }
    }
    if let (Some(bundle_path), Some(domain)) = (&cfg.trust_bundle, &cfg.trust_domain) {
        let bundle = TrustBundle::load(bundle_path)?;
        let validator = SvidValidator::new(domain.clone(), bundle)
            .with_federated_trust_domains(cfg.federated_trust_domains.clone());
        if cfg.federated_trust_domains.is_empty() {
            eprintln!("grant-keyd: SVID-validated key source (trust domain {domain:?})");
        } else {
            eprintln!(
                "grant-keyd: SVID-validated key source (trust domain {domain:?}; federated: {})",
                cfg.federated_trust_domains.join(", ")
            );
        }
        sources.push(Box::new(SvidRecipientKeys::new(&cfg.cert_dir, validator)));
    }
    match sources.len() {
        0 => {
            eprintln!(
                "grant-keyd: WARNING — no portal_ca_bundle/portal_uri_prefix and no \
                 trust_bundle/trust_domain configured; using the UNVALIDATED cert-dir key \
                 source (break-glass/offline only). Set the portal pair to validate \
                 portal-minted members against the offline-CA root, and/or the SPIRE pair \
                 for workload SVIDs."
            );
            Ok(Box::new(CertDirRecipientKeys::new(&cfg.cert_dir)))
        }
        1 => Ok(sources.pop().expect("len checked")),
        _ => Ok(Box::new(ChainedRecipientKeys::new(sources))),
    }
}

/// Build the Zenoh session config: default (peer scouting) unless the operator
/// pinned connect endpoints, plus client mTLS material for a `tls/` router
/// endpoint that requires it.
///
/// Pinned endpoints mean client mode with multicast scouting off: a peer-mode
/// session scouts every other peer on its network and can route a `keys/**`
/// query to one of them instead of the router, which answers empty.
fn zenoh_config(cfg: &Config) -> zenoh::Config {
    let mut c = zenoh::Config::default();
    if !cfg.zenoh_connect.is_empty() {
        let endpoints = serde_json::to_string(&cfg.zenoh_connect).unwrap_or_else(|_| "[]".into());
        let _ = c.insert_json5("mode", "\"client\"");
        let _ = c.insert_json5("scouting/multicast/enabled", "false");
        let _ = c.insert_json5("connect/endpoints", &endpoints);
    }
    // Client mTLS: present our SVID and verify the router against its CA. Chain-to-
    // root validation still gates auth; `verify_name_on_connect: false` because the
    // router is reached by mesh IP / container name, not a SAN in its cert (matching
    // the fabric's other Rust clients). `enable_mtls: true` is required client-side
    // too — Zenoh gates client-cert loading on it.
    if let (Some(ca), Some(cert), Some(key)) = (
        &cfg.zenoh_tls_root_ca,
        &cfg.zenoh_tls_cert,
        &cfg.zenoh_tls_key,
    ) {
        for (k, v) in [
            ("transport/link/tls/root_ca_certificate", ca),
            ("transport/link/tls/connect_certificate", cert),
            ("transport/link/tls/connect_private_key", key),
        ] {
            let _ = c.insert_json5(k, &format!("{:?}", v.display().to_string()));
        }
        let _ = c.insert_json5("transport/link/tls/verify_name_on_connect", "false");
        let _ = c.insert_json5("transport/link/tls/enable_mtls", "true");
    }
    c
}
