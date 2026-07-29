use std::net::{IpAddr, Ipv4Addr, SocketAddr};

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
}
