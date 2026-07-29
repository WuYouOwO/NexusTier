mod api;
mod config;
mod gateway;
mod session;
mod session_pool;
mod telemetry;

use clap::Parser;
use config::GatewayConfig;
use gateway::Gateway;
use tokio::sync::watch;
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
    let gateway = Gateway::new(listen_url);
    let api_state = api::ApiState::new(
        gateway.session_pool(),
        std::time::Duration::from_millis(config.rpc_timeout_ms),
    );
    let (shutdown_tx, shutdown_rx) = watch::channel(false);
    let mut gateway_task = tokio::spawn(gateway.serve(shutdown_rx.clone()));
    let mut api_task = tokio::spawn(api::serve(config.api_addr, api_state, shutdown_rx.clone()));

    let result = tokio::select! {
        result = &mut gateway_task => flatten_task_result("EasyTier gateway", result),
        result = &mut api_task => flatten_task_result("gateway API", result),
        signal = tokio::signal::ctrl_c() => {
            signal.map_err(|error| anyhow::anyhow!("failed to install shutdown signal handler: {error}"))?;
            Ok(())
        }
    };

    let _ = shutdown_tx.send(true);
    let shutdown = async {
        if !gateway_task.is_finished() {
            flatten_task_result("EasyTier gateway", gateway_task.await)?;
        }
        if !api_task.is_finished() {
            flatten_task_result("gateway API", api_task.await)?;
        }
        anyhow::Ok(())
    };
    if tokio::time::timeout(std::time::Duration::from_secs(10), shutdown)
        .await
        .is_err()
    {
        tracing::warn!("gateway shutdown exceeded 10 seconds; remaining tasks were dropped");
    }
    result
}

fn flatten_task_result(
    service: &'static str,
    result: Result<anyhow::Result<()>, tokio::task::JoinError>,
) -> anyhow::Result<()> {
    result
        .map_err(|error| anyhow::anyhow!("{service} task failed: {error}"))?
        .map_err(|error| anyhow::anyhow!("{service} stopped: {error}"))
}
