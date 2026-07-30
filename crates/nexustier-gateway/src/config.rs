use std::{
    net::{IpAddr, Ipv4Addr, SocketAddr},
    num::{NonZeroU64, NonZeroUsize},
};

use clap::Parser;

#[derive(Clone, Debug, Parser)]
#[command(author, version, about)]
pub struct GatewayConfig {
    /// Address used by the native EasyTier configuration protocol.
    #[arg(long, env = "NEXUSTIER_GATEWAY_LISTEN_ADDR", default_value_t = IpAddr::V4(Ipv4Addr::UNSPECIFIED))]
    pub listen_addr: IpAddr,

    /// UDP port used by unmodified EasyTier web clients.
    #[arg(long, env = "NEXUSTIER_GATEWAY_LISTEN_PORT", default_value_t = 22020)]
    pub listen_port: u16,

    /// Optional shared token required in every EasyTier heartbeat.
    #[arg(long, env = "NEXUSTIER_GATEWAY_ADMISSION_TOKEN")]
    pub admission_token: Option<String>,

    /// Local HTTP API consumed by the NexusTier Go controller.
    #[arg(
        long,
        env = "NEXUSTIER_GATEWAY_API_ADDR",
        default_value = "127.0.0.1:11211"
    )]
    pub api_addr: SocketAddr,

    /// Deadline applied independently to each EasyTier telemetry RPC.
    #[arg(
        long,
        env = "NEXUSTIER_GATEWAY_RPC_TIMEOUT_MS",
        default_value_t = 5_000
    )]
    pub rpc_timeout_ms: u64,

    /// Overall deadline for one topology collection.
    #[arg(
        long,
        env = "NEXUSTIER_GATEWAY_COLLECTION_TIMEOUT_MS",
        default_value = "15000"
    )]
    pub collection_timeout_ms: NonZeroU64,

    /// Maximum number of machines collected concurrently.
    #[arg(
        long,
        env = "NEXUSTIER_GATEWAY_MACHINE_CONCURRENCY",
        default_value = "8"
    )]
    pub machine_concurrency: NonZeroUsize,

    /// Time a completed topology snapshot can be reused.
    #[arg(
        long,
        env = "NEXUSTIER_GATEWAY_SNAPSHOT_TTL_MS",
        default_value = "1000"
    )]
    pub snapshot_ttl_ms: NonZeroU64,
}
