mod config;
mod gateway;
mod session;

use clap::Parser;
use config::GatewayConfig;
use gateway::Gateway;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .with_target(false)
        .init();

    let config = GatewayConfig::parse();
    let listen_url = format!("udp://{}:{}", config.listen_addr, config.listen_port)
        .parse()
        .map_err(|error| anyhow::anyhow!("invalid gateway listen URL: {error}"))?;

    Gateway::new(listen_url).run().await
}