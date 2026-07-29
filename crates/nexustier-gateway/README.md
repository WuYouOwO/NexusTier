# NexusTier Gateway

`nexustier-gateway` is the native EasyTier protocol boundary for NexusTier. It accepts unmodified EasyTier v2.6.4 web clients over UDP, keeps active sessions in memory, and exposes a localhost-only read API for the future Go controller.

Chinese documentation: [production deployment](../../docs/deployment-guide.zh-CN.md), [usage and API](../../docs/gateway-guide.zh-CN.md), [source architecture](../../docs/gateway-code.zh-CN.md).

## Protocol Baseline

- EasyTier tag: `v2.6.4`
- EasyTier commit: `8428a89d2dabc94c97d370ec607c6ca142473626`
- Client protocol: EasyTier WebClient over UDP, default port `22020`
- Secure configuration tunnel: Noise handshake with AES-GCM
- Session identity: EasyTier Machine ID

The gateway uses EasyTier's native bidirectional RPC transport. It does not proxy data-plane packets and does not require TUN privileges.

## Run

Prerequisites:

- Rust 1.95 or newer
- `protoc` (`protobuf-compiler` on Debian/Ubuntu)

```bash
cargo run --locked --package nexustier-gateway -- \
  --listen-addr 0.0.0.0 \
  --listen-port 22020 \
  --api-addr 127.0.0.1:11211
```

Configure an EasyTier client to use the NexusTier endpoint as its config server. The exact EasyTier CLI flag depends on the client distribution; the endpoint is:

```text
udp://<nexustier-host>:22020/<user-token>
```

The token is carried by the EasyTier heartbeat but is intentionally never returned by the NexusTier read API.

## Configuration

| CLI option | Environment variable | Default |
| --- | --- | --- |
| `--listen-addr` | `NEXUSTIER_GATEWAY_LISTEN_ADDR` | `0.0.0.0` |
| `--listen-port` | `NEXUSTIER_GATEWAY_LISTEN_PORT` | `22020` |
| `--api-addr` | `NEXUSTIER_GATEWAY_API_ADDR` | `127.0.0.1:11211` |
| `--rpc-timeout-ms` | `NEXUSTIER_GATEWAY_RPC_TIMEOUT_MS` | `5000` |

Use `RUST_LOG` to configure logging, for example `RUST_LOG=nexustier_gateway=debug`.

## HTTP API

The API is read-only and has no public authentication layer. Keep it on loopback or a private container network.

- `GET /healthz`: process health and active session count
- `GET /readyz`: returns `200` after at least one EasyTier client registers, otherwise `503`
- `GET /v1/sessions`: sanitized machine session metadata
- `GET /v1/topology`: per-machine, per-instance Node, Peer, Route and Stats telemetry

Telemetry collection uses independent RPC deadlines. A failed instance call is reported in that instance's `errors` array while other machines and instances remain available.

## Container

GitHub Actions builds pull requests without publishing. Pushes to `main`, semantic version tags such as `v0.1.0`, and manual workflow runs publish signed images to:

```text
ghcr.io/wuyouowo/nexustier
```

The `main` branch publishes `main`, `latest`, and `sha-<commit>` tags. Version tags publish the semantic version, major/minor, commit SHA, and the metadata action's release tags.

Pull a published image:

```bash
docker pull ghcr.io/wuyouowo/nexustier:latest
```

Build locally:

```bash
docker build -t nexustier-gateway:dev .
docker run --rm \
  -p 22020:22020/udp \
  -p 127.0.0.1:11211:11211/tcp \
  nexustier-gateway:dev
```

The image runs as UID `10001` and requires no Linux capabilities. The container API binds to `0.0.0.0` internally so it can be published to loopback or reached by the Go controller on a private container network.

## Verification

```bash
cargo fmt --all -- --check
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace
```

The integration suite starts EasyTier's real v2.6.4 `WebClient` and a no-TUN network instance, then verifies secure registration and reverse `show_node_info`, `list_peer`, `list_route`, and `get_stats` RPC calls.
