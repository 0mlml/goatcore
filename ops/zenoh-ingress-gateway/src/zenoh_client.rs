use anyhow::Result;
use zenoh::Config as ZenohConfig;

/// Build a Zenoh client-session config that connects OUT to a router over mTLS.
///
/// Mirrors the proven `ops/zenoh-trial-peers/audit-subscriber` client config shape exactly:
/// `mode: client`, a single connect endpoint, and a `transport/link/tls` block with
/// `enable_mtls: true` + `verify_name_on_connect: false` (the router cert SAN binds the mesh
/// IP, not the DNS name we connect through). Built via `insert_json5` on a default config, the
/// same API the audit-subscriber uses.
pub fn build_session_config(
    connect_endpoint: &str,
    tls_root_ca: &str,
    tls_client_cert: &str,
    tls_client_key: &str,
) -> Result<ZenohConfig> {
    let mut config = ZenohConfig::default();
    config
        .insert_json5("mode", "\"client\"")
        .map_err(|e| anyhow::anyhow!("config mode: {e}"))?;
    config
        .insert_json5("connect/endpoints", &format!("[\"{connect_endpoint}\"]"))
        .map_err(|e| anyhow::anyhow!("config connect/endpoints: {e}"))?;
    let tls_block = format!(
        r#"{{ "root_ca_certificate": "{tls_root_ca}", "connect_certificate": "{tls_client_cert}", "connect_private_key": "{tls_client_key}", "enable_mtls": true, "verify_name_on_connect": false }}"#
    );
    config
        .insert_json5("transport/link/tls", &tls_block)
        .map_err(|e| anyhow::anyhow!("config transport/link/tls: {e}"))?;
    Ok(config)
}
