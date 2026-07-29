use std::sync::Arc;

use async_trait::async_trait;
use easytier::{
    proto::{
        api::{
            instance::{
                PeerManageRpc, PeerManageRpcClientFactory, StatsRpc, StatsRpcClientFactory,
            },
            manage::{WebClientService, WebClientServiceClientFactory},
        },
        rpc_impl::bidirect::BidirectRpcManager,
        rpc_types::{self, controller::BaseController},
        web::{
            GetFeatureRequest, GetFeatureResponse, HeartbeatRequest, HeartbeatResponse,
            WebServerService, WebServerServiceServer,
        },
    },
    tunnel::Tunnel,
    web_client::security,
};
use tokio::sync::{RwLock, watch};
use url::Url;
use uuid::Uuid;

#[derive(Clone, Debug)]
pub struct SessionSnapshot {
    pub remote_url: Url,
    pub heartbeat: HeartbeatRequest,
    pub connected_at_ms: u64,
    pub last_heartbeat_at_ms: u64,
}

impl SessionSnapshot {
    pub fn machine_id(&self) -> anyhow::Result<Uuid> {
        self.heartbeat
            .machine_id
            .map(Into::into)
            .ok_or_else(|| anyhow::anyhow!("heartbeat is missing machine_id"))
    }
}

#[derive(Debug)]
struct SessionState {
    remote_url: Url,
    heartbeat: Option<HeartbeatRequest>,
    connected_at_ms: u64,
    last_heartbeat_at_ms: u64,
}

#[derive(Clone)]
struct GatewayRpcService {
    state: Arc<RwLock<SessionState>>,
    heartbeat_tx: watch::Sender<Option<HeartbeatRequest>>,
}

#[async_trait]
impl WebServerService for GatewayRpcService {
    type Controller = BaseController;

    async fn heartbeat(
        &self,
        _: BaseController,
        request: HeartbeatRequest,
    ) -> rpc_types::error::Result<HeartbeatResponse> {
        if request.machine_id.is_none() {
            return Err(anyhow::anyhow!("heartbeat is missing machine_id").into());
        }

        let mut state = self.state.write().await;
        state.heartbeat = Some(request.clone());
        state.last_heartbeat_at_ms = unix_time_ms();
        drop(state);
        self.heartbeat_tx.send_replace(Some(request));
        Ok(HeartbeatResponse {})
    }

    async fn get_feature(
        &self,
        _: BaseController,
        _: GetFeatureRequest,
    ) -> rpc_types::error::Result<GetFeatureResponse> {
        Ok(GetFeatureResponse {
            support_encryption: security::web_secure_tunnel_supported(),
        })
    }
}

pub struct GatewaySession {
    rpc_manager: BidirectRpcManager,
    state: Arc<RwLock<SessionState>>,
    heartbeat_rx: watch::Receiver<Option<HeartbeatRequest>>,
}

impl GatewaySession {
    pub fn new(remote_url: Url) -> Self {
        let connected_at_ms = unix_time_ms();
        let state = Arc::new(RwLock::new(SessionState {
            remote_url,
            heartbeat: None,
            connected_at_ms,
            last_heartbeat_at_ms: connected_at_ms,
        }));
        let (heartbeat_tx, heartbeat_rx) = watch::channel(None);
        let rpc_manager =
            BidirectRpcManager::new().set_rx_timeout(Some(std::time::Duration::from_secs(30)));

        rpc_manager.rpc_server().registry().register(
            WebServerServiceServer::new(GatewayRpcService {
                state: state.clone(),
                heartbeat_tx,
            }),
            "",
        );

        Self {
            rpc_manager,
            state,
            heartbeat_rx,
        }
    }

    pub fn serve(&mut self, tunnel: Box<dyn Tunnel>) {
        self.rpc_manager.run_with_tunnel(tunnel);
    }

    pub fn is_running(&self) -> bool {
        self.rpc_manager.is_running()
    }

    pub async fn stop(&self) {
        self.rpc_manager.stop().await;
    }

    pub async fn wait_for_first_heartbeat(
        &mut self,
        timeout: std::time::Duration,
    ) -> anyhow::Result<SessionSnapshot> {
        tokio::time::timeout(timeout, async {
            loop {
                if self.heartbeat_rx.borrow().is_some() {
                    let state = self.state.read().await;
                    return Ok(SessionSnapshot {
                        remote_url: state.remote_url.clone(),
                        heartbeat: state.heartbeat.clone().ok_or_else(|| {
                            anyhow::anyhow!("heartbeat notification arrived without state")
                        })?,
                        connected_at_ms: state.connected_at_ms,
                        last_heartbeat_at_ms: state.last_heartbeat_at_ms,
                    });
                }

                self.heartbeat_rx
                    .changed()
                    .await
                    .map_err(|_| anyhow::anyhow!("session closed before its first heartbeat"))?;
            }
        })
        .await
        .map_err(|_| anyhow::anyhow!("timed out waiting for the first heartbeat"))?
    }

    pub async fn snapshot(&self) -> anyhow::Result<SessionSnapshot> {
        let state = self.state.read().await;
        Ok(SessionSnapshot {
            remote_url: state.remote_url.clone(),
            heartbeat: state
                .heartbeat
                .clone()
                .ok_or_else(|| anyhow::anyhow!("session has not received a heartbeat"))?,
            connected_at_ms: state.connected_at_ms,
            last_heartbeat_at_ms: state.last_heartbeat_at_ms,
        })
    }

    pub fn management_client(
        &self,
    ) -> Box<dyn WebClientService<Controller = BaseController> + Send> {
        self.rpc_manager
            .rpc_client()
            .scoped_client::<WebClientServiceClientFactory<BaseController>>(1, 1, String::new())
    }

    pub fn peer_client(&self) -> Box<dyn PeerManageRpc<Controller = BaseController> + Send> {
        self.rpc_manager
            .rpc_client()
            .scoped_client::<PeerManageRpcClientFactory<BaseController>>(1, 1, String::new())
    }

    pub fn stats_client(&self) -> Box<dyn StatsRpc<Controller = BaseController> + Send> {
        self.rpc_manager
            .rpc_client()
            .scoped_client::<StatsRpcClientFactory<BaseController>>(1, 1, String::new())
    }
}

fn unix_time_ms() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis()
        .try_into()
        .unwrap_or(u64::MAX)
}
