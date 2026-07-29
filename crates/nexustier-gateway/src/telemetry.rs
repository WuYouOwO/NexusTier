use std::{collections::BTreeMap, sync::Arc, time::Duration};

use easytier::proto::{
    api::instance::{
        GetStatsRequest, InstanceIdentifier, ListPeerRequest, ListRouteRequest, MetricSnapshot,
        NodeInfo, PeerInfo, PeerRoutePair, Route, ShowNodeInfoRequest,
        instance_identifier::Selector,
    },
    api::manage::ListNetworkInstanceRequest,
    rpc_types::controller::BaseController,
};
use serde::Serialize;
use tokio::task::JoinSet;
use uuid::Uuid;

use crate::{session::GatewaySession, session_pool::SessionPool};

#[derive(Clone)]
pub struct TelemetryCollector {
    sessions: SessionPool,
    rpc_timeout: Duration,
}

#[derive(Debug, Serialize)]
pub struct SessionView {
    pub machine_id: Uuid,
    pub remote_url: String,
    pub hostname: String,
    pub easytier_version: String,
    pub report_time: String,
    pub connected_at_ms: u64,
    pub last_heartbeat_at_ms: u64,
    pub running_instance_ids: Vec<Uuid>,
    pub device: Option<DeviceView>,
}

#[derive(Debug, Serialize)]
pub struct DeviceView {
    pub os_type: String,
    pub os_version: String,
    pub distribution: String,
}

#[derive(Debug, Serialize)]
pub struct TopologySnapshot {
    pub collected_at_ms: u64,
    pub machines: Vec<MachineTopology>,
}

#[derive(Debug, Serialize)]
pub struct MachineTopology {
    pub session: SessionView,
    pub instances: Vec<InstanceTopology>,
    pub errors: Vec<TelemetryError>,
}

#[derive(Debug, Serialize)]
pub struct InstanceTopology {
    pub instance_id: Uuid,
    pub node: Option<NodeView>,
    pub peers: Vec<PeerView>,
    pub metrics: Vec<MetricView>,
    pub errors: Vec<TelemetryError>,
}

#[derive(Debug, Serialize)]
pub struct NodeView {
    pub peer_id: u32,
    pub ipv4: String,
    pub hostname: String,
    pub proxy_cidrs: Vec<String>,
    pub listeners: Vec<String>,
    pub version: String,
}

#[derive(Debug, Serialize)]
pub struct PeerView {
    pub peer_id: u32,
    pub ipv4: Option<String>,
    pub hostname: String,
    pub next_hop_peer_id: u32,
    pub direct: bool,
    pub path_cost: i32,
    pub latency_ms: Option<f64>,
    pub loss_rate: Option<f64>,
    pub rx_bytes: Option<u64>,
    pub tx_bytes: Option<u64>,
    pub tunnel_protocols: Vec<String>,
    pub version: String,
}

#[derive(Debug, Serialize)]
pub struct MetricView {
    pub name: String,
    pub value: u64,
    pub labels: BTreeMap<String, String>,
}

#[derive(Debug, Serialize)]
pub struct TelemetryError {
    pub operation: &'static str,
    pub message: String,
}

impl TelemetryCollector {
    pub fn new(sessions: SessionPool, rpc_timeout: Duration) -> Self {
        Self {
            sessions,
            rpc_timeout,
        }
    }

    pub async fn sessions(&self) -> Vec<SessionView> {
        let mut views = Vec::new();
        for session in self.sessions.sessions() {
            match session.snapshot().await.and_then(SessionView::try_from) {
                Ok(view) => views.push(view),
                Err(error) => tracing::warn!(%error, "failed to snapshot EasyTier session"),
            }
        }
        views.sort_by_key(|view| view.machine_id);
        views
    }

    pub async fn topology(&self) -> TopologySnapshot {
        let mut tasks = JoinSet::new();
        for session in self.sessions.sessions() {
            let collector = self.clone();
            tasks.spawn(async move { collector.collect_machine(session).await });
        }

        let mut machines = Vec::new();
        while let Some(result) = tasks.join_next().await {
            match result {
                Ok(machine) => machines.push(machine),
                Err(error) => tracing::error!(%error, "telemetry task panicked"),
            }
        }
        machines.sort_by_key(|machine| machine.session.machine_id);

        TopologySnapshot {
            collected_at_ms: unix_time_ms(),
            machines,
        }
    }

    async fn collect_machine(&self, session: Arc<GatewaySession>) -> MachineTopology {
        let snapshot = match session.snapshot().await {
            Ok(snapshot) => snapshot,
            Err(error) => {
                return MachineTopology {
                    session: SessionView::unavailable(),
                    instances: Vec::new(),
                    errors: vec![TelemetryError::new("session_snapshot", error)],
                };
            }
        };
        let session_view = match SessionView::try_from(snapshot) {
            Ok(view) => view,
            Err(error) => {
                return MachineTopology {
                    session: SessionView::unavailable(),
                    instances: Vec::new(),
                    errors: vec![TelemetryError::new("session_snapshot", error)],
                };
            }
        };

        let instance_ids = match tokio::time::timeout(
            self.rpc_timeout,
            session
                .management_client()
                .list_network_instance(BaseController::default(), ListNetworkInstanceRequest {}),
        )
        .await
        {
            Ok(Ok(response)) => response.inst_ids,
            Ok(Err(error)) => {
                return MachineTopology {
                    session: session_view,
                    instances: Vec::new(),
                    errors: vec![TelemetryError::new("list_network_instances", error)],
                };
            }
            Err(error) => {
                return MachineTopology {
                    session: session_view,
                    instances: Vec::new(),
                    errors: vec![TelemetryError::new("list_network_instances", error)],
                };
            }
        };

        let mut instances = Vec::with_capacity(instance_ids.len());
        for instance_id in instance_ids {
            instances.push(self.collect_instance(&session, instance_id).await);
        }
        instances.sort_by_key(|instance| instance.instance_id);

        MachineTopology {
            session: session_view,
            instances,
            errors: Vec::new(),
        }
    }

    async fn collect_instance(
        &self,
        session: &GatewaySession,
        instance_id: easytier::proto::common::Uuid,
    ) -> InstanceTopology {
        let instance_uuid: Uuid = instance_id.into();
        let identifier = || InstanceIdentifier {
            selector: Some(Selector::Id(instance_id)),
        };

        let peer_client = session.peer_client();
        let route_client = session.peer_client();
        let node_client = session.peer_client();
        let stats_client = session.stats_client();
        let timeout = self.rpc_timeout;

        let (peers, routes, node, metrics) = tokio::join!(
            tokio::time::timeout(
                timeout,
                peer_client.list_peer(
                    BaseController::default(),
                    ListPeerRequest {
                        instance: Some(identifier()),
                    },
                ),
            ),
            tokio::time::timeout(
                timeout,
                route_client.list_route(
                    BaseController::default(),
                    ListRouteRequest {
                        instance: Some(identifier()),
                    },
                ),
            ),
            tokio::time::timeout(
                timeout,
                node_client.show_node_info(
                    BaseController::default(),
                    ShowNodeInfoRequest {
                        instance: Some(identifier()),
                    },
                ),
            ),
            tokio::time::timeout(
                timeout,
                stats_client.get_stats(
                    BaseController::default(),
                    GetStatsRequest {
                        instance: Some(identifier()),
                    },
                ),
            ),
        );

        let mut errors = Vec::new();
        let peers = rpc_value(peers, "list_peers", &mut errors)
            .map(|response| response.peer_infos)
            .unwrap_or_default();
        let routes = rpc_value(routes, "list_routes", &mut errors)
            .map(|response| response.routes)
            .unwrap_or_default();
        let node = rpc_value(node, "show_node_info", &mut errors)
            .and_then(|response| response.node_info)
            .map(NodeView::from);
        let metrics = rpc_value(metrics, "get_stats", &mut errors)
            .map(|response| response.metrics.into_iter().map(MetricView::from).collect())
            .unwrap_or_default();

        InstanceTopology {
            instance_id: instance_uuid,
            node,
            peers: join_peers_and_routes(peers, routes),
            metrics,
            errors,
        }
    }
}

impl TryFrom<crate::session::SessionSnapshot> for SessionView {
    type Error = anyhow::Error;

    fn try_from(snapshot: crate::session::SessionSnapshot) -> Result<Self, Self::Error> {
        let heartbeat = snapshot.heartbeat;
        let machine_id = heartbeat
            .machine_id
            .map(Into::into)
            .ok_or_else(|| anyhow::anyhow!("heartbeat is missing machine_id"))?;
        Ok(Self {
            machine_id,
            remote_url: snapshot.remote_url.to_string(),
            hostname: heartbeat.hostname,
            easytier_version: heartbeat.easytier_version,
            report_time: heartbeat.report_time,
            connected_at_ms: snapshot.connected_at_ms,
            last_heartbeat_at_ms: snapshot.last_heartbeat_at_ms,
            running_instance_ids: heartbeat
                .running_network_instances
                .into_iter()
                .map(Into::into)
                .collect(),
            device: heartbeat.device_os.map(|device| DeviceView {
                os_type: device.os_type,
                os_version: device.version,
                distribution: device.distribution,
            }),
        })
    }
}

impl SessionView {
    fn unavailable() -> Self {
        Self {
            machine_id: Uuid::nil(),
            remote_url: String::new(),
            hostname: String::new(),
            easytier_version: String::new(),
            report_time: String::new(),
            connected_at_ms: 0,
            last_heartbeat_at_ms: 0,
            running_instance_ids: Vec::new(),
            device: None,
        }
    }
}

impl From<NodeInfo> for NodeView {
    fn from(node: NodeInfo) -> Self {
        Self {
            peer_id: node.peer_id,
            ipv4: node.ipv4_addr,
            hostname: node.hostname,
            proxy_cidrs: node.proxy_cidrs,
            listeners: node.listeners,
            version: node.version,
        }
    }
}

impl From<MetricSnapshot> for MetricView {
    fn from(metric: MetricSnapshot) -> Self {
        Self {
            name: metric.name,
            value: metric.value,
            labels: metric.labels.into_iter().collect(),
        }
    }
}

impl TelemetryError {
    fn new(operation: &'static str, error: impl std::fmt::Display) -> Self {
        Self {
            operation,
            message: error.to_string(),
        }
    }
}

fn rpc_value<T, E: std::fmt::Display>(
    result: Result<Result<T, E>, tokio::time::error::Elapsed>,
    operation: &'static str,
    errors: &mut Vec<TelemetryError>,
) -> Option<T> {
    match result {
        Ok(Ok(value)) => Some(value),
        Ok(Err(error)) => {
            errors.push(TelemetryError::new(operation, error));
            None
        }
        Err(error) => {
            errors.push(TelemetryError::new(operation, error));
            None
        }
    }
}

fn join_peers_and_routes(peers: Vec<PeerInfo>, routes: Vec<Route>) -> Vec<PeerView> {
    let mut peers_by_id = peers
        .into_iter()
        .map(|peer| (peer.peer_id, peer))
        .collect::<BTreeMap<_, _>>();
    let mut views = routes
        .into_iter()
        .map(|route| {
            let peer = peers_by_id.remove(&route.peer_id);
            let pair = PeerRoutePair {
                route: Some(route.clone()),
                peer,
            };
            let direct = route.cost == 1;
            PeerView {
                peer_id: route.peer_id,
                ipv4: route.ipv4_addr.map(|address| address.to_string()),
                hostname: route.hostname,
                next_hop_peer_id: route.next_hop_peer_id,
                direct,
                path_cost: route.cost,
                latency_ms: if direct {
                    pair.get_latency_ms()
                } else {
                    Some(f64::from(route.path_latency))
                },
                loss_rate: pair.get_loss_rate(),
                rx_bytes: pair.get_rx_bytes(),
                tx_bytes: pair.get_tx_bytes(),
                tunnel_protocols: pair.get_conn_protos().unwrap_or_default(),
                version: route.version,
            }
        })
        .collect::<Vec<_>>();
    views.sort_by_key(|peer| peer.peer_id);
    views
}

fn unix_time_ms() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis()
        .try_into()
        .unwrap_or(u64::MAX)
}

#[cfg(test)]
mod tests {
    use easytier::proto::api::instance::{PeerConnInfo, PeerConnStats, PeerInfo, Route};

    use super::join_peers_and_routes;

    #[test]
    fn direct_peer_uses_connection_telemetry() {
        let views = join_peers_and_routes(
            vec![PeerInfo {
                peer_id: 7,
                conns: vec![PeerConnInfo {
                    conn_id: "primary".to_string(),
                    peer_id: 7,
                    stats: Some(PeerConnStats {
                        rx_bytes: 1_024,
                        tx_bytes: 2_048,
                        latency_us: 12_500,
                        ..Default::default()
                    }),
                    loss_rate: 0.025,
                    ..Default::default()
                }],
                default_conn_id: None,
                ..Default::default()
            }],
            vec![Route {
                peer_id: 7,
                hostname: "direct-peer".to_string(),
                next_hop_peer_id: 7,
                cost: 1,
                path_latency: 99,
                ..Default::default()
            }],
        );

        assert_eq!(views.len(), 1);
        assert!(views[0].direct);
        assert_eq!(views[0].latency_ms, Some(12.5));
        assert_eq!(views[0].rx_bytes, Some(1_024));
        assert_eq!(views[0].tx_bytes, Some(2_048));
        assert_eq!(views[0].loss_rate, Some(0.02500000037252903));
    }

    #[test]
    fn relayed_peer_uses_route_path_latency() {
        let views = join_peers_and_routes(
            Vec::new(),
            vec![Route {
                peer_id: 9,
                hostname: "relayed-peer".to_string(),
                next_hop_peer_id: 7,
                cost: 2,
                path_latency: 42,
                ..Default::default()
            }],
        );

        assert_eq!(views.len(), 1);
        assert!(!views[0].direct);
        assert_eq!(views[0].next_hop_peer_id, 7);
        assert_eq!(views[0].path_cost, 2);
        assert_eq!(views[0].latency_ms, Some(42.0));
        assert_eq!(views[0].rx_bytes, None);
        assert_eq!(views[0].tx_bytes, None);
    }
}
