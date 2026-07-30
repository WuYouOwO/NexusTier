# NexusTier Gateway

`nexustier-gateway` is the native EasyTier protocol boundary for NexusTier. It accepts unmodified EasyTier v2.6.4 web clients over UDP, keeps active sessions in memory, and exposes an internal read API consumed by the Go telemetry controller.

Chinese documentation: [full-stack deployment](../../docs/current-deployment-guide.zh-CN.md), [user tutorial](../../docs/current-usage-guide.zh-CN.md), [Gateway-only deployment](../../docs/deployment-guide.zh-CN.md), [usage and API](../../docs/gateway-guide.zh-CN.md), [source architecture](../../docs/gateway-code.zh-CN.md).

## Protocol Baseline

- EasyTier tag: `v2.6.4`
- EasyTier commit: `8428a89d2dabc94c97d370ec607c6ca142473626`
- Client protocol: EasyTier WebClient over UDP, default port `22020`
- Secure configuration tunnel: Noise handshake with AES-GCM
- Session identity: EasyTier Machine ID

The gateway uses EasyTier's native bidirectional RPC transport. It does not create network instances, assign addresses, compile policies, or proxy data-plane packets, and it does not require TUN privileges.

## Run

Prerequisites:

- Rust 1.95 or newer
- `protoc` and standard Protobuf definitions (`protobuf-compiler` and `libprotobuf-dev` on Debian/Ubuntu)

```bash
cargo run --locked --package nexustier-gateway -- \
  --listen-addr 0.0.0.0 \
  --listen-port 22020 \
  --api-addr 127.0.0.1:11211
```

For a task-oriented Chinese guide covering the current published image, EasyTier client
registration, telemetry queries, upgrades, and rollback, see the
[Gateway usage guide](../../docs/usage-guide.zh-CN.md).

Configure an EasyTier client to use the NexusTier endpoint as its config server. The exact EasyTier CLI flag depends on the client distribution; the endpoint is:

```text
udp://<nexustier-host>:22020/<user-token>
```

The token is carried by the EasyTier heartbeat but is intentionally never returned by the NexusTier read API. The current source can optionally require one shared admission token. This is a bootstrap control, not device identity, tenant isolation, revocable credentials, or a replacement for future OIDC enrollment.

## Configuration

| CLI option | Environment variable | Default |
| --- | --- | --- |
| `--listen-addr` | `NEXUSTIER_GATEWAY_LISTEN_ADDR` | `0.0.0.0` |
| `--listen-port` | `NEXUSTIER_GATEWAY_LISTEN_PORT` | `22020` |
| `--admission-token` | `NEXUSTIER_GATEWAY_ADMISSION_TOKEN` | unset |
| `--api-addr` | `NEXUSTIER_GATEWAY_API_ADDR` | `127.0.0.1:11211` |
| `--rpc-timeout-ms` | `NEXUSTIER_GATEWAY_RPC_TIMEOUT_MS` | `5000` |
| `--collection-timeout-ms` | `NEXUSTIER_GATEWAY_COLLECTION_TIMEOUT_MS` | `15000` |
| `--machine-concurrency` | `NEXUSTIER_GATEWAY_MACHINE_CONCURRENCY` | `8` |
| `--snapshot-ttl-ms` | `NEXUSTIER_GATEWAY_SNAPSHOT_TTL_MS` | `1000` |

Use `RUST_LOG` to configure logging, for example `RUST_LOG=nexustier_gateway=debug`.

Plaintext connections remain available only for the native EasyTier feature probe. A session must reconnect with Noise + AES-GCM before heartbeat registration. When `--admission-token` is configured, every heartbeat must carry that exact token. The Machine ID accepted by the first heartbeat is immutable for the lifetime of the connection.

## HTTP API

The API is read-only and has no public authentication layer. Keep it on loopback or a private container network.

- `GET /healthz`: process health and active session count
- `GET /readyz`: returns `200` after at least one EasyTier client registers, otherwise `503`
- `GET /v1/sessions`: sanitized machine session metadata
- `GET /v1/topology`: per-machine, per-instance Node, Peer, Route and Stats telemetry

Telemetry collection uses independent RPC deadlines. A failed instance call is reported in that instance's `errors` array while other machines and instances remain available.

Topology collection is single-flight. Concurrent requests wait for the same collection, and requests within the snapshot TTL reuse its `collection_id`. Each collection also has an overall deadline and a bounded number of concurrently collected machines. A deadline returns completed machines plus a snapshot-level `collection_timeout` error instead of waiting without bound.

The current source emits the versioned `nexustier.topology.v1` contract with a collection UUID, collection start/completion timestamps, machine/instance observation timestamps, and machine-readable error codes. The JSON Schema and shared fixture live under [`contracts`](../../contracts/README.md).

## Container

GitHub Actions builds both application images for pull requests without publishing. Pushes to `main`, semantic version tags such as `v0.1.0`, and manual workflow runs publish signed images to:

```text
Gateway:    ghcr.io/wuyouowo/nexustier
Controller: ghcr.io/wuyouowo/nexustier-controller
```

The `main` branch publishes `main`, `latest`, and `sha-<commit>` tags. Version tags publish the semantic version, major/minor, commit SHA, and the metadata action's release tags.

Every image build now depends on a separate quality job that runs locked tests, strict Clippy, rustfmt, and topology contract asset checks. Pull requests receive read-only repository/package permissions and no OIDC token; publishing and signing permissions are enabled only for non-PR builds.

Current verified image baseline:

```text
ghcr.io/wuyouowo/nexustier:sha-44044b0
sha256:acf3a2f1bdad378928addee3c040c0ca1de0516ddebf5d8217c16d648f90e417
```

GitHub Actions run `30527587634` passed Rust, Go, PostgreSQL integration, both image publications, Cosign signing, and summary steps.

Pull a published image:

```bash
docker pull ghcr.io/wuyouowo/nexustier:sha-44044b0
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

## Current Boundaries

- Sessions are in memory and are rebuilt when EasyTier clients reconnect.
- The HTTP API is internal, read-only, and unauthenticated; it must stay on loopback or a trusted private network.
- `/readyz` requires an active client, so `503` is expected before any client registers and must not be used as the container liveness probe.
- The gateway reports live telemetry but does not persist it; persistence belongs to the Go controller and PostgreSQL.
- There is no device-level identity, OIDC/RBAC, public management API, configuration distribution, IPAM, ACL engine, Redis synchronization, or multi-replica session HA.
- Only EasyTier v2.6.4 at the pinned commit is a tested protocol baseline.

## Verification

```bash
cargo fmt --all -- --check
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace
```

The integration suite starts EasyTier's real v2.6.4 `WebClient` and a no-TUN network instance, then verifies secure registration and reverse `show_node_info`, `list_peer`, `list_route`, and `get_stats` RPC calls. Focused tests also reject plaintext heartbeat registration, invalid admission tokens, and Machine ID changes within an established session.
