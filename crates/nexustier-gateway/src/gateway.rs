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
    admission_token: Option<Arc<str>>,
}

impl Gateway {
    pub fn new(listen_url: Url, admission_token: Option<String>) -> Self {
        Self {
            listener: UdpTunnelListener::new(listen_url),
            sessions: SessionPool::default(),
            admission_token: admission_token.map(Into::into),
        }
    }

    pub fn session_pool(&self) -> SessionPool {
        self.sessions.clone()
    }

    pub(crate) async fn serve(
        mut self,
        mut shutdown_rx: watch::Receiver<bool>,
    ) -> anyhow::Result<()> {
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
                    let admission_token = self.admission_token.clone();
                    tasks.spawn(async move {
                        if let Err(error) = Self::accept_session(tunnel, sessions, admission_token).await {
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
        admission_token: Option<Arc<str>>,
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

        let mut session = GatewaySession::new(remote_url.clone(), secure, admission_token);
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
        common::config::{ConfigFileControl, ConfigLoader, TomlConfigLoader},
        instance_manager::NetworkInstanceManager,
        proto::{
            api::manage::ListNetworkInstanceRequest,
            common::CompressionAlgoPb,
            rpc_impl::packet::{compress_packet, decompress_packet},
            rpc_types::controller::BaseController,
        },
        tunnel::udp::UdpTunnelConnector,
        web_client::WebClient,
    };
    use tokio::sync::watch;

    use super::Gateway;
    use crate::telemetry::TelemetryCollector;

    #[tokio::test]
    async fn zstd_rpc_compression_is_enabled() {
        let payload = vec![0x5a; 4096];
        let (compressed, algorithm) = compress_packet(CompressionAlgoPb::Zstd, &payload)
            .await
            .expect("compress RPC payload");

        assert_eq!(algorithm, CompressionAlgoPb::Zstd);
        assert_eq!(
            decompress_packet(algorithm, &compressed)
                .await
                .expect("decompress RPC payload"),
            payload
        );
    }

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
        let gateway = Gateway::new(listen_url.clone(), None);
        let sessions = gateway.sessions.clone();
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        let gateway_task = tokio::spawn(gateway.serve(shutdown_rx));
        let machine_id = uuid::Uuid::new_v4();
        let manager = Arc::new(NetworkInstanceManager::new());
        let instance_config = TomlConfigLoader::default();
        instance_config.set_hostname(Some("telemetry-node".to_string()));
        instance_config.set_listeners(Vec::new());
        let mut flags = instance_config.get_flags();
        flags.no_tun = true;
        instance_config.set_flags(flags);
        let instance_id = manager
            .run_network_instance(instance_config, true, ConfigFileControl::STATIC_CONFIG)
            .expect("start no-TUN EasyTier instance");

        let client = WebClient::new(
            UdpTunnelConnector::new(listen_url),
            "integration-test-token",
            machine_id,
            "native-integration-client",
            false,
            manager.clone(),
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
                    assert_eq!(response.inst_ids.len(), 1);
                    break;
                }
                tokio::time::sleep(Duration::from_millis(50)).await;
            }
        })
        .await
        .expect("native client should register before timeout");

        assert_eq!(sessions.len(), 1);
        let telemetry = TelemetryCollector::new(
            sessions.clone(),
            Duration::from_secs(2),
            Duration::from_secs(10),
            2,
            Duration::from_millis(100),
        );
        let session_views = telemetry.sessions().await;
        assert_eq!(session_views.len(), 1);
        assert_eq!(session_views[0].machine_id, machine_id);
        assert_eq!(session_views[0].hostname, "native-integration-client");
        assert_eq!(session_views[0].running_instance_ids, vec![instance_id]);

        let topology = telemetry.topology().await;
        assert_eq!(topology.machines.len(), 1);
        assert_eq!(topology.machines[0].session.machine_id, machine_id);
        assert!(topology.machines[0].errors.is_empty());
        assert_eq!(topology.machines[0].instances.len(), 1);
        let instance = &topology.machines[0].instances[0];
        assert_eq!(instance.instance_id, instance_id);
        assert_eq!(instance.node.as_ref().unwrap().hostname, "telemetry-node");
        assert!(instance.peers.is_empty());
        assert!(!instance.metrics.is_empty());
        assert!(instance.errors.is_empty());

        drop(client);
        manager
            .delete_network_instance(vec![instance_id])
            .expect("stop no-TUN EasyTier instance");
        shutdown_tx.send(true).expect("request gateway shutdown");
        gateway_task
            .await
            .expect("gateway task should not panic")
            .expect("gateway should shut down cleanly");
    }

    #[test]
    fn new_gateway_starts_with_an_empty_session_pool() {
        let gateway = Gateway::new("udp://127.0.0.1:22020".parse().expect("valid URL"), None);
        assert_eq!(gateway.session_count(), 0);
    }
}
