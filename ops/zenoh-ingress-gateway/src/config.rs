use clap::{Parser, ValueEnum};

/// How the gateway admits a sample received from the peered entity into the core fabric.
///
/// Per ADR 1020 (zenoh-federated-peering-trust), un-onboarded / relayed ingress is
/// QUARANTINE-rewritten to `<peering-entity>/<peering-id>/<original-key>` and never admitted to
/// a first-class namespace. An onboarded moon-pod publishing its OWN first-party data under an
/// authorized prefix is the open-reconciliation case: it MAY be admitted passthrough
/// (origin-scoped) instead. The safe default is `quarantine`.
#[derive(Copy, Clone, Debug, PartialEq, Eq, ValueEnum)]
pub enum AdmitMode {
    /// Rewrite every received key to `<peering-entity>/<peering-id>/<original-key>`.
    /// Safe default: nothing the peer sends can land in a first-class namespace.
    Quarantine,
    /// Admit keys that match `--passthrough-allow` verbatim (origin-scoped, onboarded
    /// first-party); quarantine everything else. For onboarded moon-pods only.
    Passthrough,
}

/// Configuration for the ingress gateway, sourced from CLI args + env vars.
///
/// The gateway holds two Zenoh sessions: one connects OUT to the peered entity's router
/// (partner-facing, subscribes) and one connects to the goat core router (core-facing,
/// publishes, scoped by the core ACL to write only the admit/quarantine prefix). It mirrors the
/// mTLS-client config shape of `ops/zenoh-trial-peers/audit-subscriber`.
#[derive(Parser, Debug, Clone)]
#[command(
    name = "zenoh-ingress-gateway",
    version,
    about = "goat-side ingress gateway for Tier-2 Zenoh peered ingress (ADR 1020)"
)]
pub struct Config {
    // ---- Peering identity (the one identity we can actually authenticate) ----
    /// Verified identity of the directly-connected peered entity (cert URI-SAN / CN).
    /// In v0 this is supplied by config and MUST match the peer cert the partner-facing
    /// session authenticated; a later revision derives it from the live session's peer cert.
    #[arg(long, env = "PEERING_ENTITY")]
    pub peering_entity: String,

    /// Stable identifier for this peering relationship/session (e.g. `peering-7f3a`).
    /// Reserved-space prefix so it cannot collide with an onboarded entity's real topic systems
    /// (ADR 1020 "collision note"). Defaults to a `peering-`-prefixed value the operator sets.
    #[arg(long, env = "PEERING_ID")]
    pub peering_id: String,

    // ---- Admission policy ----
    /// How received samples are admitted into the core fabric.
    #[arg(long, env = "ADMIT_MODE", value_enum, default_value_t = AdmitMode::Quarantine)]
    pub admit_mode: AdmitMode,

    /// In `passthrough` mode: key-expression prefixes admitted verbatim (origin-scoped).
    /// Repeatable. Everything not matching is quarantine-rewritten. Ignored in `quarantine` mode.
    #[arg(long = "passthrough-allow", env = "PASSTHROUGH_ALLOW", value_delimiter = ',')]
    pub passthrough_allow: Vec<String>,

    /// Key expression the gateway subscribes to on the PARTNER-facing session.
    #[arg(long, env = "INGRESS_KEY_EXPR", default_value = "**")]
    pub ingress_key_expr: String,

    // ---- Partner-facing session (connect OUT to the peered entity's router) ----
    #[arg(long, env = "PARTNER_CONNECT_ENDPOINT")]
    pub partner_connect_endpoint: String,
    #[arg(long, env = "PARTNER_TLS_ROOT_CA")]
    pub partner_tls_root_ca: String,
    #[arg(long, env = "PARTNER_TLS_CLIENT_CERT")]
    pub partner_tls_client_cert: String,
    #[arg(long, env = "PARTNER_TLS_CLIENT_KEY")]
    pub partner_tls_client_key: String,

    // ---- Core-facing session (connect to the goat core router) ----
    #[arg(long, env = "CORE_CONNECT_ENDPOINT")]
    pub core_connect_endpoint: String,
    #[arg(long, env = "CORE_TLS_ROOT_CA")]
    pub core_tls_root_ca: String,
    /// The gateway's OWN goat-CA-rooted client cert. The core ACL scopes this identity to write
    /// only the admit/quarantine prefix — so the gateway is the only writer the core router sees.
    #[arg(long, env = "CORE_TLS_CLIENT_CERT")]
    pub core_tls_client_cert: String,
    #[arg(long, env = "CORE_TLS_CLIENT_KEY")]
    pub core_tls_client_key: String,

    // ---- Provenance ----
    /// Append-only NDJSON provenance log: one line per admitted sample (verified peering
    /// identity, original key, admitted key, mode, timestamps). Complements the partner's own
    /// pod-side audit-sink. If unset, provenance is emitted to tracing only.
    #[arg(long, env = "PROVENANCE_NDJSON_PATH")]
    pub provenance_ndjson_path: Option<String>,
}

impl Config {
    /// Compute the admitted key for a received original key under the current policy.
    pub fn admitted_key(&self, original_key: &str) -> String {
        match self.admit_mode {
            AdmitMode::Passthrough
                if self
                    .passthrough_allow
                    .iter()
                    .any(|p| key_matches_prefix(original_key, p)) =>
            {
                original_key.to_string()
            }
            _ => self.quarantine_key(original_key),
        }
    }

    /// `<peering-entity>/<peering-id>/<original-key>` — the ADR 1020 quarantine shape.
    pub fn quarantine_key(&self, original_key: &str) -> String {
        format!("{}/{}/{}", self.peering_entity, self.peering_id, original_key)
    }
}

/// True if `key` falls under prefix `pfx`. `pfx` may end in `/**` (covers the prefix and all
/// descendants) or be an exact path. This is a conservative subset of Zenoh key-expr matching
/// sufficient for v0 passthrough allow-listing; the core ACL is the real enforcement boundary.
pub fn key_matches_prefix(key: &str, pfx: &str) -> bool {
    if let Some(base) = pfx.strip_suffix("/**") {
        key == base || key.starts_with(&format!("{base}/"))
    } else {
        key == pfx
    }
}
