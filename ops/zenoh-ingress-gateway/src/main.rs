use anyhow::Result;
use clap::Parser;
use tracing::info;

mod config;
mod gateway;
mod provenance;
mod zenoh_client;

#[cfg(test)]
mod config_test;

use crate::config::Config;

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()),
        )
        .init();

    let config = Config::parse();
    info!(
        peering_entity = %config.peering_entity,
        peering_id = %config.peering_id,
        "zenoh-ingress-gateway starting"
    );

    gateway::run(config).await
}
