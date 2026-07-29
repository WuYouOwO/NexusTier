use std::{sync::Arc, time::Duration};

use anyhow::Context;
use dashmap::DashMap;
use easytier::{
    tunnel::{TunnelListener, udp::UdpTunnelListener},
    web_client::security,
};
use tokio::{sync::RwLock, task::JoinSet};
use url::Url;

use crate::session::{GatewaySession, SessionSnapshot};

const FIRST_HEARTBEAT_TIMEOUT: Duration = Duration::from_secs(15);

pub struct Gateway {
    listener: UdpTunnelListener,
    sessions: Arc<DashMap<Url, Arc<RwLock<GatewaySession>>>>,
}

impl Gateway {
    pub fn new(listen_url: Url) -> Self {
        Self {
            listener: UdpTunnelListener::new(listen_url),
            sessions: Arc::new(DashMap::new()),
        }
    }

    pub async fn run(mut self) -> anyhow::Result<()> {
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
                signal = tokio::signal::ctrl_c() => {
                    signal.context("failed to install shutdown signal handler")?;
                    tracing::info!("shutdown signal received");
                    break;
                }
            }
        }

        tasks.abort_all();
        while tasks.join_next().await.is_some() {}
        Ok(())
    }

    async fn accept_session(
        tunnel: Box<dyn easytier::tunnel::Tunnel>,
        sessions: Arc<DashMap<Url, Arc<RwLock<GatewaySession>>>>,
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
        let session = Arc::new(RwLock::new(session));

        if let Some(previous) = sessions.insert(remote_url.clone(), session.clone()) {
            previous.read().await.stop().await;
        }

        Self::log_connected(&snapshot, secure);
        while session.read().await.is_running() {
            tokio::time::sleep(Duration::from_secs(1)).await;
        }
        sessions.remove_if(&remote_url, |_, current| Arc::ptr_eq(current, &session));
        tracing::info!(%remote_url, "EasyTier session disconnected");
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
