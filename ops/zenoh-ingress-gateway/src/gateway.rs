use anyhow::{Context, Result};
use std::sync::Arc;
use tracing::{error, info, warn};

use crate::config::{AdmitMode, Config};
use crate::provenance::{Provenance, ProvenanceLog};

/// Run the ingress gateway: subscribe on the partner-facing session, rewrite each sample's key
/// under the admission policy, and re-publish into the core fabric on the core-facing session,
/// recording provenance for every admitted sample.
///
/// Two sessions, one direction of data (partner -> core). Inbound to the partner is out of scope
/// for v0 (the pod subscribes to `release/<goatnet>/**` over its own authorized session).
pub async fn run(config: Config) -> Result<()> {
    let provenance = Arc::new(ProvenanceLog::new(config.provenance_ndjson_path.as_deref())?);

    // Core-facing session: the gateway's OWN goat identity. The core ACL scopes this to write
    // only the admit/quarantine prefix, so the gateway is the sole writer the core router sees.
    let core_cfg = crate::zenoh_client::build_session_config(
        &config.core_connect_endpoint,
        &config.core_tls_root_ca,
        &config.core_tls_client_cert,
        &config.core_tls_client_key,
    )?;
    info!(connect = %config.core_connect_endpoint, "opening core-facing session");
    let core = zenoh::open(core_cfg)
        .await
        .map_err(|e| anyhow::anyhow!("failed to open core session: {e}"))
        .context("zenoh::open core")?;

    // Partner-facing session: connects OUT to the peered entity's router and subscribes.
    let partner_cfg = crate::zenoh_client::build_session_config(
        &config.partner_connect_endpoint,
        &config.partner_tls_root_ca,
        &config.partner_tls_client_cert,
        &config.partner_tls_client_key,
    )?;
    info!(
        connect = %config.partner_connect_endpoint,
        key_expr = %config.ingress_key_expr,
        peering_entity = %config.peering_entity,
        peering_id = %config.peering_id,
        admit_mode = ?config.admit_mode,
        "opening partner-facing session"
    );
    let partner = zenoh::open(partner_cfg)
        .await
        .map_err(|e| anyhow::anyhow!("failed to open partner session: {e}"))
        .context("zenoh::open partner")?;

    let subscriber = partner
        .declare_subscriber(&config.ingress_key_expr)
        .await
        .map_err(|e| anyhow::anyhow!("failed to declare subscriber: {e}"))
        .context("declare_subscriber")?;

    let admit_mode_str = match config.admit_mode {
        AdmitMode::Quarantine => "quarantine",
        AdmitMode::Passthrough => "passthrough",
    };

    info!("ingress gateway ready; entering receive loop");

    loop {
        match subscriber.recv_async().await {
            Ok(sample) => {
                if let Err(e) =
                    admit_sample(&config, &core, &provenance, admit_mode_str, sample).await
                {
                    warn!(error = %e, "failed to admit sample");
                }
            }
            Err(e) => {
                error!(error = %e, "subscriber receive error; exiting loop");
                break;
            }
        }
    }

    Ok(())
}

/// Rewrite one received sample's key under the policy and re-publish it into the core fabric.
async fn admit_sample(
    config: &Config,
    core: &zenoh::Session,
    provenance: &Arc<ProvenanceLog>,
    admit_mode_str: &str,
    sample: zenoh::sample::Sample,
) -> Result<()> {
    let original_key = sample.key_expr().as_str().to_string();
    let payload = sample.payload().to_bytes().to_vec();
    let admitted_key = config.admitted_key(&original_key);

    core.put(&admitted_key, payload.clone())
        .await
        .map_err(|e| anyhow::anyhow!("failed to re-publish to core: {e}"))
        .context("core.put")?;

    provenance.record(&Provenance {
        received_at: chrono::Utc::now().to_rfc3339(),
        peering_entity: &config.peering_entity,
        peering_id: &config.peering_id,
        original_key: &original_key,
        admitted_key: &admitted_key,
        admit_mode: admit_mode_str,
        payload_len: payload.len(),
    })?;

    Ok(())
}
