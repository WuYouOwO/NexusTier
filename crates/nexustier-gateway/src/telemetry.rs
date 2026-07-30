use std::{
    collections::{BTreeMap, VecDeque},
    sync::Arc,
    time::Duration,
};

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
use tokio::{sync::Mutex, task::JoinSet};
use uuid::Uuid;

use crate::{session::GatewaySession, session_pool::SessionPool};

pub const TOPOLOGY_SCHEMA_VERSION: &str = "nexustier.topology.v1";

#[derive(Clone)]
pub struct TelemetryCollector {
    sessions: SessionPool,
    rpc_timeout: Duration,
    collection_timeout: Duration,
    machine_concurrency: usize,
    snapshot_ttl: Duration,
    state: Arc<Mutex<CollectorState>>,
}

#[derive(Default)]
struct CollectorState {
    cached: Option<TopologySnapshot>,
    running: Option<tokio::sync::watch::Receiver<Option<TopologySnapshot>>>,
}

#[derive(Clone, Debug, Serialize)]
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

#[derive(Clone, Debug, Serialize)]
pub struct DeviceView {
    pub os_type: String,
    pub os_version: String,
    pub distribution: String,
}

#[derive(Clone, Debug, Serialize)]
pub struct TopologySnapshot {
    pub schema_version: &'static str,
    pub collection_id: Uuid,
    pub started_at_ms: u64,
    pub completed_at_ms: u64,
    pub collected_at_ms: u64,
    pub machines: Vec<MachineTopology>,
    pub errors: Vec<TelemetryError>,
}

#[derive(Clone, Debug, Serialize)]
pub struct MachineTopology {
    pub session: SessionView,
    pub observed_at_ms: u64,
    pub instances: Vec<InstanceTopology>,
    pub errors: Vec<TelemetryError>,
}

#[derive(Clone, Debug, Serialize)]
pub struct InstanceTopology {
    pub instance_id: Uuid,
    pub observed_at_ms: u64,
    pub node: Option<NodeView>,
    pub peers: Vec<PeerView>,
    pub metrics: Vec<MetricView>,
    pub errors: Vec<TelemetryError>,
}

#[derive(Clone, Debug, Serialize)]
pub struct NodeView {
    pub peer_id: u32,
    pub ipv4: String,
    pub hostname: String,
    pub proxy_cidrs: Vec<String>,
    pub listeners: Vec<String>,
    pub version: String,
}

#[derive(Clone, Debug, Serialize)]
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

#[derive(Clone, Debug, Serialize)]
pub struct MetricView {
    pub name: String,
    pub value: u64,
    pub labels: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Serialize)]
pub struct TelemetryError {
    pub code: &'static str,
    pub operation: &'static str,
    pub message: String,
}

impl TelemetryCollector {
    pub fn new(
        sessions: SessionPool,
        rpc_timeout: Duration,
        collection_timeout: Duration,
        machine_concurrency: usize,
        snapshot_ttl: Duration,
    ) -> Self {
        assert!(
            machine_concurrency > 0,
            "machine concurrency must be non-zero"
        );
        Self {
            sessions,
            rpc_timeout,
            collection_timeout,
            machine_concurrency,
            snapshot_ttl,
            state: Arc::new(Mutex::new(CollectorState::default())),
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
        let mut receiver = {
            let mut state = self.state.lock().await;
            if let Some(snapshot) = state.cached.as_ref()
                && snapshot_age(snapshot) <= self.snapshot_ttl
            {
                return snapshot.clone();
            }

            if let Some(receiver) = state.running.as_ref() {
                receiver.clone()
            } else {
                let (sender, receiver) = tokio::sync::watch::channel(None);
                state.running = Some(receiver.clone());
                let collector = self.clone();
                tokio::spawn(async move {
                    let snapshot = collector.collect_topology().await;
                    let mut state = collector.state.lock().await;
                    state.cached = Some(snapshot.clone());
                    state.running = None;
                    drop(state);
                    sender.send_replace(Some(snapshot));
                });
                receiver
            }
        };

        loop {
            if let Some(snapshot) = receiver.borrow().clone() {
                return snapshot;
            }
            receiver
                .changed()
                .await
                .expect("topology collection task dropped before publishing a snapshot");
        }
    }

    async fn collect_topology(&self) -> TopologySnapshot {
        let started_at_ms = unix_time_ms();
        let collection_id = Uuid::new_v4();
        let deadline = tokio::time::Instant::now() + self.collection_timeout;
        let mut pending = VecDeque::from(self.sessions.sessions());
        let mut tasks = JoinSet::new();

        while tasks.len() < self.machine_concurrency
            && let Some(session) = pending.pop_front()
        {
            let collector = self.clone();
            tasks.spawn(async move { collector.collect_machine(session).await });
        }

        let mut machines = Vec::new();
        let mut errors = Vec::new();
        while !tasks.is_empty() {
            let result = tokio::select! {
                result = tasks.join_next() => result,
                _ = tokio::time::sleep_until(deadline) => {
                    let remaining = pending.len() + tasks.len();
                    tasks.abort_all();
                    while tasks.join_next().await.is_some() {}
                    errors.push(TelemetryError::collection_timeout(
                        "collect_topology",
                        format!(
                            "collection exceeded {} ms with {} machines pending",
                            self.collection_timeout.as_millis(),
                            remaining,
                        ),
                    ));
                    break;
                }
            };

            let Some(result) = result else {
                break;
            };
            match result {
                Ok(machine) => machines.push(machine),
                Err(error) => {
                    tracing::error!(%error, "telemetry task panicked");
                    errors.push(TelemetryError::task("collect_machine", error));
                }
            }
            if let Some(session) = pending.pop_front() {
                let collector = self.clone();
                tasks.spawn(async move { collector.collect_machine(session).await });
            }
        }
        machines.sort_by_key(|machine| machine.session.machine_id);
        let completed_at_ms = unix_time_ms();

        TopologySnapshot {
            schema_version: TOPOLOGY_SCHEMA_VERSION,
            collection_id,
            started_at_ms,
            completed_at_ms,
            collected_at_ms: completed_at_ms,
            machines,
            errors,
        }
    }

    async fn collect_machine(&self, session: Arc<GatewaySession>) -> MachineTopology {
        let snapshot = match session.snapshot().await {
            Ok(snapshot) => snapshot,
            Err(error) => {
                return MachineTopology {
                    session: SessionView::unavailable(),
                    observed_at_ms: unix_time_ms(),
                    instances: Vec::new(),
                    errors: vec![TelemetryError::session("session_snapshot", error)],
                };
            }
        };
        let session_view = match SessionView::try_from(snapshot) {
            Ok(view) => view,
            Err(error) => {
                return MachineTopology {
                    session: SessionView::unavailable(),
                    observed_at_ms: unix_time_ms(),
                    instances: Vec::new(),
                    errors: vec![TelemetryError::session("session_snapshot", error)],
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
                    observed_at_ms: unix_time_ms(),
                    instances: Vec::new(),
                    errors: vec![TelemetryError::rpc("list_network_instances", error)],
                };
            }
            Err(error) => {
                return MachineTopology {
                    session: session_view,
                    observed_at_ms: unix_time_ms(),
                    instances: Vec::new(),
                    errors: vec![TelemetryError::timeout("list_network_instances", error)],
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
            observed_at_ms: unix_time_ms(),
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
            observed_at_ms: unix_time_ms(),
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
    fn new(code: &'static str, operation: &'static str, error: impl std::fmt::Display) -> Self {
        Self {
            code,
            operation,
            message: error.to_string(),
        }
    }

    fn session(operation: &'static str, error: impl std::fmt::Display) -> Self {
        Self::new("session_unavailable", operation, error)
    }

    fn rpc(operation: &'static str, error: impl std::fmt::Display) -> Self {
        Self::new("rpc_error", operation, error)
    }

    fn timeout(operation: &'static str, error: impl std::fmt::Display) -> Self {
        Self::new("rpc_timeout", operation, error)
    }

    fn task(operation: &'static str, error: impl std::fmt::Display) -> Self {
        Self::new("task_failed", operation, error)
    }

    fn collection_timeout(operation: &'static str, error: impl std::fmt::Display) -> Self {
        Self::new("collection_timeout", operation, error)
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
            errors.push(TelemetryError::rpc(operation, error));
            None
        }
        Err(error) => {
            errors.push(TelemetryError::timeout(operation, error));
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

fn snapshot_age(snapshot: &TopologySnapshot) -> Duration {
    Duration::from_millis(unix_time_ms().saturating_sub(snapshot.completed_at_ms))
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeMap;

    use easytier::proto::api::instance::{PeerConnInfo, PeerConnStats, PeerInfo, Route};

    use super::{
        DeviceView, InstanceTopology, MachineTopology, MetricView, NodeView, PeerView, SessionView,
        TOPOLOGY_SCHEMA_VERSION, TelemetryError, TopologySnapshot, join_peers_and_routes,
    };
    use crate::session_pool::SessionPool;

    #[tokio::test]
    async fn concurrent_topology_requests_share_one_collection() {
        let collector = super::TelemetryCollector::new(
            SessionPool::default(),
            std::time::Duration::from_millis(100),
            std::time::Duration::from_secs(1),
            1,
            std::time::Duration::from_secs(1),
        );

        let (first, second) = tokio::join!(collector.topology(), collector.topology());

        assert_eq!(first.collection_id, second.collection_id);
        assert_eq!(first.started_at_ms, second.started_at_ms);
    }

    #[tokio::test]
    async fn expired_snapshot_starts_a_new_collection() {
        let collector = super::TelemetryCollector::new(
            SessionPool::default(),
            std::time::Duration::from_millis(100),
            std::time::Duration::from_secs(1),
            1,
            std::time::Duration::from_millis(1),
        );
        let first = collector.topology().await;
        tokio::time::sleep(std::time::Duration::from_millis(2)).await;

        let second = collector.topology().await;

        assert_ne!(first.collection_id, second.collection_id);
    }

    #[test]
    fn topology_v1_fixture_matches_the_producer_contract() {
        let fixture: serde_json::Value = serde_json::from_str(include_str!(concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../../contracts/fixtures/topology-v1.json"
        )))
        .expect("topology contract fixture must be valid JSON");
        let snapshot = TopologySnapshot {
            schema_version: TOPOLOGY_SCHEMA_VERSION,
            collection_id: "11111111-1111-4111-8111-111111111111".parse().unwrap(),
            started_at_ms: 1_785_319_201_000,
            completed_at_ms: 1_785_319_202_000,
            collected_at_ms: 1_785_319_202_000,
            machines: vec![MachineTopology {
                session: SessionView {
                    machine_id: "22222222-2222-4222-8222-222222222222".parse().unwrap(),
                    remote_url: "udp://203.0.113.10:42000".to_string(),
                    hostname: "edge-shanghai-01".to_string(),
                    easytier_version: "2.6.4".to_string(),
                    report_time: "2026-07-29T18:00:00+08:00".to_string(),
                    connected_at_ms: 1_785_319_200_000,
                    last_heartbeat_at_ms: 1_785_319_201_000,
                    running_instance_ids: vec![
                        "33333333-3333-4333-8333-333333333333".parse().unwrap(),
                    ],
                    device: Some(DeviceView {
                        os_type: "linux".to_string(),
                        os_version: "6.12.0".to_string(),
                        distribution: "Debian GNU/Linux 13".to_string(),
                    }),
                },
                observed_at_ms: 1_785_319_202_000,
                instances: vec![InstanceTopology {
                    instance_id: "33333333-3333-4333-8333-333333333333".parse().unwrap(),
                    observed_at_ms: 1_785_319_201_900,
                    node: Some(NodeView {
                        peer_id: 1001,
                        ipv4: "10.10.0.1/24".to_string(),
                        hostname: "edge-shanghai-01".to_string(),
                        proxy_cidrs: vec![],
                        listeners: vec!["udp://0.0.0.0:11010".to_string()],
                        version: "2.6.4".to_string(),
                    }),
                    peers: vec![PeerView {
                        peer_id: 1002,
                        ipv4: Some("10.10.0.2/24".to_string()),
                        hostname: "edge-beijing-01".to_string(),
                        next_hop_peer_id: 1002,
                        direct: true,
                        path_cost: 1,
                        latency_ms: Some(18.42),
                        loss_rate: Some(0.001),
                        rx_bytes: Some(1_048_576),
                        tx_bytes: Some(524_288),
                        tunnel_protocols: vec!["udp".to_string()],
                        version: "2.6.4".to_string(),
                    }],
                    metrics: vec![MetricView {
                        name: "bytes_rx".to_string(),
                        value: 1_048_576,
                        labels: BTreeMap::new(),
                    }],
                    errors: vec![TelemetryError::timeout("get_stats", "deadline elapsed")],
                }],
                errors: vec![],
            }],
            errors: vec![],
        };

        assert_eq!(serde_json::to_value(snapshot).unwrap(), fixture);
    }

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
