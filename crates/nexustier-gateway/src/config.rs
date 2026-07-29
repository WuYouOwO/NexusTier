use std::net::{IpAddr, Ipv4Addr};

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
}
