use std::{sync::Arc, time::Duration};

use anyhow::Context;
use easytier::{
    tunnel::{TunnelListener, udp::UdpTunnelListener},
    web_client::security,
};
use tokio::{sync::watch, task::JoinSet};
use url::Url;

use crate::{
    session::{GatewaySession, SessionSnapshot},
    session_pool::SessionPool,
};

const FIRST_HEARTBEAT_TIMEOUT: Duration = Duration::from_secs(15);

pub struct Gateway {
    listener: UdpTunnelListener,
    sessions: SessionPool,
}

impl Gateway {
    pub fn new(listen_url: Url) -> Self {
        Self {
            listener: UdpTunnelListener::new(listen_url),
            sessions: SessionPool::default(),
        }
    }

    pub async fn run(self) -> anyhow::Result<()> {
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        let signal_task = tokio::spawn(async move {
            if let Err(error) = tokio::signal::ctrl_c().await {
                tracing::error!(%error, "failed to install shutdown signal handler");
            }
            let _ = shutdown_tx.send(true);
        });
        let result = self.serve(shutdown_rx).await;
        signal_task.abort();
        result
    }

    async fn serve(mut self, mut shutdown_rx: watch::Receiver<bool>) -> anyhow::Result<()> {
        self.listener
            .listen()
            .await
            .context("failed to listen for EasyTier clients")?;

        tracing::info!(listen_url = %self.listener.local_url(), "EasyTier gateway is listening");
        let mut tasks = JoinSet::new();

        loop {
            tokio::select! {
                accepted = self.listener.accept() => {
                    let tunnel = accepted.context("EasyTier listener stopped accepting clients")?;
                    let sessions = self.sessions.clone();
                    tasks.spawn(async move {
                        if let Err(error) = Self::accept_session(tunnel, sessions).await {
                            tracing::warn!(%error, "EasyTier session rejected");
                        }
                    });
                }
                Some(result) = tasks.join_next(), if !tasks.is_empty() => {
                    if let Err(error) = result {
                        tracing::error!(%error, "session task panicked");
                    }
                }
                changed = shutdown_rx.changed() => {
                    changed.context("gateway shutdown sender was dropped")?;
                    tracing::info!("shutdown signal received");
                    break;
                }
            }
        }

        tasks.abort_all();
        while tasks.join_next().await.is_some() {}
        Ok(())
    }

    #[cfg(test)]
    fn session_count(&self) -> usize {
        self.sessions.len()
    }

    async fn accept_session(
        tunnel: Box<dyn easytier::tunnel::Tunnel>,
        sessions: SessionPool,
    ) -> anyhow::Result<()> {
        let (tunnel, secure) = security::accept_or_upgrade_server_tunnel(tunnel)
            .await
            .context("failed to negotiate EasyTier session security")?;
        let remote_url: Url = tunnel
            .info()
            .context("accepted tunnel has no metadata")?
            .remote_addr
            .context("accepted tunnel has no remote address")?
            .into();

        let mut session = GatewaySession::new(remote_url.clone());
        session.serve(tunnel);
        let snapshot = session
            .wait_for_first_heartbeat(FIRST_HEARTBEAT_TIMEOUT)
            .await?;
        let machine_id = snapshot.machine_id()?;
        let session = Arc::new(session);

        if let Some(previous) = sessions.insert(machine_id, session.clone()) {
            previous.stop().await;
        }

        Self::log_connected(&snapshot, secure);
        while session.is_running() {
            tokio::time::sleep(Duration::from_secs(1)).await;
        }
        sessions.remove_if_current(&machine_id, &session);
        tracing::info!(%remote_url, %machine_id, "EasyTier session disconnected");
        Ok(())
    }

    fn log_connected(snapshot: &SessionSnapshot, secure: bool) {
        tracing::info!(
            remote_url = %snapshot.remote_url,
            machine_id = ?snapshot.heartbeat.machine_id,
            hostname = %snapshot.heartbeat.hostname,
            easytier_version = %snapshot.heartbeat.easytier_version,
            secure,
            "EasyTier session registered"
        );
    }
}

#[cfg(test)]
mod tests {
    use std::{sync::Arc, time::Duration};

    use easytier::{
        instance_manager::NetworkInstanceManager,
        proto::{api::manage::ListNetworkInstanceRequest, rpc_types::controller::BaseController},
        tunnel::udp::UdpTunnelConnector,
        web_client::WebClient,
    };
    use tokio::sync::watch;

    use super::Gateway;

    #[tokio::test]
    async fn native_web_client_registers_and_exposes_management_rpc() {
        let port = std::net::UdpSocket::bind(("127.0.0.1", 0))
            .expect("reserve an ephemeral UDP port")
            .local_addr()
            .expect("read ephemeral UDP port")
            .port();
        let listen_url: url::Url = format!("udp://127.0.0.1:{port}")
            .parse()
            .expect("build gateway URL");
        let gateway = Gateway::new(listen_url.clone());
        let sessions = gateway.sessions.clone();
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        let gateway_task = tokio::spawn(gateway.serve(shutdown_rx));

        let client = WebClient::new(
            UdpTunnelConnector::new(listen_url),
            "integration-test-token",
            uuid::Uuid::new_v4(),
            "native-integration-client",
            false,
            Arc::new(NetworkInstanceManager::new()),
            None,
        );

        tokio::time::timeout(Duration::from_secs(20), async {
            loop {
                if let Some(session) = sessions.sessions().into_iter().next() {
                    let response = session
                        .management_client()
                        .list_network_instance(
                            BaseController::default(),
                            ListNetworkInstanceRequest {},
                        )
                        .await
                        .expect("call native reverse management RPC");
                    assert!(response.inst_ids.is_empty());
                    break;
                }
                tokio::time::sleep(Duration::from_millis(50)).await;
            }
        })
        .await
        .expect("native client should register before timeout");

        assert_eq!(sessions.len(), 1);
        drop(client);
        shutdown_tx.send(true).expect("request gateway shutdown");
        gateway_task
            .await
            .expect("gateway task should not panic")
            .expect("gateway should shut down cleanly");
    }

    #[test]
    fn new_gateway_starts_with_an_empty_session_pool() {
        let gateway = Gateway::new("udp://127.0.0.1:22020".parse().expect("valid URL"));
        assert_eq!(gateway.session_count(), 0);
    }
}
