# NexusTier 当前版本端到端部署指南

## 1. 文档范围

本文部署当前仓库已经实现并验证的完整遥测链路：

```mermaid
flowchart LR
    ET[EasyTier v2.6.4 客户端] -->|UDP 22020| GW[Rust Gateway]
    GW -->|HTTP 11211，仅本机/私网| CTRL[Go Controller]
    CTRL -->|TCP 5432| PG[(PostgreSQL)]
    OPS[运维人员] -->|HTTP 8080，仅本机| CTRL
    ET -. 加密 Mesh 数据面 .-> PEERS[其他 EasyTier 节点]
```

当前完整栈可以完成：

- 接收原生 EasyTier v2.6.4 WebClient 安全连接。
- 按 Machine ID 管理在线 Session 和安全重连。
- 反向采集 Node、Peer、Route、RTT、流量和 Stats。
- 通过 `nexustier.topology.v1` 契约把拓扑交给 Go Controller。
- 将 Machine、Instance、Node、当前 Peer 链路、指标和采集错误写入 PostgreSQL。
- 通过 Controller 内部 API 检查进程、数据库和最近一次摄取状态。

当前完整栈仍不包含 Web 控制台、Redis、OIDC/RBAC、IPAM、ACL、SSH 或 RDP。
Gateway 和 Controller 均由 GitHub Actions 构建并发布 GHCR 镜像；仓库提供包含
PostgreSQL 的 Compose 示例。本文仍保留固定源码构建与 systemd 部署路径，便于生产
环境独立审计和加固。

## 2. 已验证版本基线

| 项目 | 当前基线 |
| --- | --- |
| 源码提交 | `654c70fbb70289c5313f7685abff72a59b3c9f7b` |
| Gateway 包版本 | `0.1.0` |
| Gateway 镜像 | `ghcr.io/wuyouowo/nexustier:sha-654c70f` |
| Gateway 镜像 digest | `sha256:fe7dbc15f1b96955fac429f3e3825c2f71f57aacbf4e415c2b4a8c7cbb4b7028` |
| EasyTier | `v2.6.4`，commit `8428a89d2dabc94c97d370ec607c6ca142473626` |
| Rust MSRV | `1.95` |
| Go | `1.25` |
| PostgreSQL | `14+`，本地验证使用 `18.4` |
| topology 契约 | `nexustier.topology.v1` |
| GitHub Actions | Run `30518033355`，质量、构建、推送和签名全部成功 |

生产部署应固定 SHA tag 或 digest，不要只依赖会移动的 `latest`。

## 3. 推荐单机拓扑

最容易审计的当前部署方式是：

- Gateway 使用官方 GHCR 镜像运行。
- Gateway API 只发布到宿主机 `127.0.0.1:11211`。
- PostgreSQL 运行在同一台 Linux 主机。
- Controller 从固定提交构建，以 systemd 服务运行。
- Controller API 只绑定 `127.0.0.1:8080`。
- 公网或客户端网络只开放 `22020/UDP`。

| 端口 | 默认绑定 | 是否公网开放 | 用途 |
| --- | --- | --- | --- |
| `22020/UDP` | `0.0.0.0` | 按客户端来源策略开放 | EasyTier WebClient 控制通道 |
| `11211/TCP` | `127.0.0.1` | 禁止 | Gateway 内部只读 API |
| `5432/TCP` | 本机/数据库私网 | 禁止 | Controller 访问 PostgreSQL |
| `8080/TCP` | `127.0.0.1` | 禁止 | Controller 内部状态 API |

EasyTier 节点之间的数据包不经过 NexusTier，仍由 EasyTier 完成 NAT 穿透、
加密传输和 Mesh 路由。

需要快速启动完整栈时，在仓库根目录执行：

```bash
cp .env.example .env
# 修改 .env 中的数据库密码、连接 URL 和 Gateway 准入 Token。
docker compose -f compose.example.yaml pull
docker compose -f compose.example.yaml up -d
```

默认使用 `ghcr.io/wuyouowo/nexustier:latest` 和
`ghcr.io/wuyouowo/nexustier-controller:latest`。生产环境应在 `.env` 中把两个镜像
固定到同一个版本标签或审核过的 digest，不应长期依赖 `latest`。

## 4. 主机准备

示例面向 `linux/amd64` 的 Debian/Ubuntu。当前 GHCR Gateway 只发布
`linux/amd64` manifest；ARM64 主机不能直接使用本文固定镜像。

准备以下软件：

- Docker Engine，用于 Gateway。本文命令和 systemd 依赖按 Docker 编写。
- Git 和 Go 1.25，用于构建 Controller。
- PostgreSQL 14 或更新版本。
- `curl`、`jq`、`openssl`。

先按 Docker 和 Go 官方文档安装适合当前发行版的软件包，再安装其余依赖：

```bash
sudo apt-get update
sudo apt-get install -y git postgresql curl jq openssl ca-certificates uuid-runtime
docker --version
go version
psql --version
```

确认 `go version` 至少为 `go1.25`，并确认 `docker info` 可以成功连接 daemon。

确认端口未被占用：

```bash
ss -lunp | grep ':22020' || true
ss -lntp | grep -E ':(11211|5432|8080)\b' || true
```

## 5. 获取固定源码

```bash
git clone https://github.com/WuYouOwO/NexusTier.git
cd NexusTier
git checkout 654c70fbb70289c5313f7685abff72a59b3c9f7b
git status --short --branch
```

预期处于 detached HEAD 且工作树干净。记录实际提交：

```bash
git rev-parse HEAD
```

## 6. 部署 PostgreSQL

### 6.1 创建专用角色和数据库

进入 PostgreSQL 管理终端：

```bash
sudo -u postgres psql
```

在 `psql` 中执行：

```sql
CREATE ROLE nexustier LOGIN;
\password nexustier
CREATE DATABASE nexustier OWNER nexustier;
\q
```

`\password` 会交互式设置密码，避免密码进入 shell 历史。Controller 连接 URL 中的
特殊字符必须进行 URL 编码。

### 6.2 验证数据库

```bash
read -rsp 'PostgreSQL password: ' PGPASSWORD && echo
export PGPASSWORD
export PGSSLMODE=disable
psql -h 127.0.0.1 -U nexustier -d nexustier \
  -c 'select current_database(), current_user;'
unset PGPASSWORD PGSSLMODE
```

`sslmode=disable` 只适用于同机回环连接。数据库位于其他主机时，应配置 TLS 并使用
`sslmode=require` 或更严格的证书校验模式，同时限制 PostgreSQL 防火墙和 `pg_hba.conf`。

## 7. 部署 Gateway

### 7.1 验证并拉取镜像

```bash
sudo docker pull ghcr.io/wuyouowo/nexustier:sha-654c70f
```

也可以完全固定 digest：

```bash
sudo docker pull \
  ghcr.io/wuyouowo/nexustier@sha256:fe7dbc15f1b96955fac429f3e3825c2f71f57aacbf4e415c2b4a8c7cbb4b7028
```

安装 Cosign 后可验证 GitHub OIDC 无密钥签名：

```bash
cosign verify \
  --certificate-identity \
  'https://github.com/WuYouOwO/NexusTier/.github/workflows/docker-publish.yml@refs/heads/main' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  'ghcr.io/wuyouowo/nexustier@sha256:fe7dbc15f1b96955fac429f3e3825c2f71f57aacbf4e415c2b4a8c7cbb4b7028'
```

### 7.2 创建 Gateway 环境文件

```bash
sudo install -d -m 0750 /etc/nexustier
sudo touch /etc/nexustier/gateway.env
sudo chmod 0600 /etc/nexustier/gateway.env
sudo editor /etc/nexustier/gateway.env
```

写入：

```dotenv
NEXUSTIER_GATEWAY_ADMISSION_TOKEN=替换为openssl-rand-hex-32生成的随机值
NEXUSTIER_GATEWAY_API_ADDR=0.0.0.0:11211
NEXUSTIER_GATEWAY_RPC_TIMEOUT_MS=5000
NEXUSTIER_GATEWAY_COLLECTION_TIMEOUT_MS=15000
NEXUSTIER_GATEWAY_MACHINE_CONCURRENCY=8
NEXUSTIER_GATEWAY_SNAPSHOT_TTL_MS=1000
RUST_LOG=info
```

随机 Token 可以在本机生成：

```bash
openssl rand -hex 32
```

这个共享 Token 是当前启动准入保护，不是完整的用户身份系统。不要提交、打印到日志
或通过不安全渠道发送。EasyTier 客户端 URL 中的 Token 必须与它完全一致。

### 7.3 启动 Gateway

```bash
sudo docker run -d \
  --name nexustier-gateway \
  --restart unless-stopped \
  --init \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --env-file /etc/nexustier/gateway.env \
  -p 22020:22020/udp \
  -p 127.0.0.1:11211:11211/tcp \
  ghcr.io/wuyouowo/nexustier@sha256:fe7dbc15f1b96955fac429f3e3825c2f71f57aacbf4e415c2b4a8c7cbb4b7028
```

Gateway 不需要 TUN、`NET_ADMIN`、host network 或特权模式。
`sudo` 同时保证 Docker CLI 可以读取 root-only 的环境文件。使用 rootless Docker 时，
应把环境文件放入该用户的私有配置目录并保持 `0600`，不要降低 `/etc` 文件权限。

### 7.4 验证 Gateway

```bash
sudo docker ps --filter name=nexustier-gateway
sudo docker logs --tail 100 nexustier-gateway
curl --fail --silent --show-error http://127.0.0.1:11211/healthz | jq
curl --silent --show-error --include http://127.0.0.1:11211/readyz
```

没有客户端时，`/healthz` 返回 `200`，`/readyz` 返回 `503` 是正常状态。

## 8. 构建和安装 Controller

### 8.1 运行质量检查

```bash
cd controller
go test ./...
go vet ./...
cd ..
```

上述命令不运行 PostgreSQL 集成场景；未设置测试数据库 URL 时该测试会跳过。数据库
或 migration 变更上线前，必须额外创建专用空测试库并执行：

```bash
sudo -u postgres dropdb --if-exists nexustier_test
sudo -u postgres createdb --owner=nexustier nexustier_test
cd controller
read -rsp 'URL-encoded test database password: ' TEST_DB_PASSWORD && echo
export NEXUSTIER_TEST_DATABASE_URL="postgres://nexustier:${TEST_DB_PASSWORD}@127.0.0.1:5432/nexustier_test?sslmode=disable"
go test ./internal/ingest -count=1
unset NEXUSTIER_TEST_DATABASE_URL TEST_DB_PASSWORD
cd ..
```

集成测试会 `TRUNCATE telemetry_collection_runs CASCADE`，绝不能指向生产数据库。

### 8.2 构建二进制

```bash
mkdir -p target/controller
CGO_ENABLED=0 go -C controller build \
  -trimpath \
  -o ../target/controller/nexustier-controller \
  ./cmd/nexustier-controller
target/controller/nexustier-controller 2>&1 | head -n 1 || true
```

最后一条命令因为尚未提供数据库 URL 而退出属于预期，用于确认二进制可执行。

### 8.3 安装二进制和环境文件

```bash
sudo useradd \
  --system \
  --create-home \
  --home-dir /var/lib/nexustier-controller \
  --shell /usr/sbin/nologin \
  nexustier-controller || true

sudo install -o root -g root -m 0755 \
  target/controller/nexustier-controller \
  /usr/local/bin/nexustier-controller

sudo install -d -o root -g nexustier-controller -m 0750 /etc/nexustier
sudo touch /etc/nexustier/controller.env
sudo chown root:nexustier-controller /etc/nexustier/controller.env
sudo chmod 0640 /etc/nexustier/controller.env
sudo editor /etc/nexustier/controller.env
```

写入：

```dotenv
NEXUSTIER_CONTROLLER_DATABASE_URL=postgres://nexustier:<URL编码后的密码>@127.0.0.1:5432/nexustier?sslmode=disable
NEXUSTIER_CONTROLLER_GATEWAY_URL=http://127.0.0.1:11211
NEXUSTIER_CONTROLLER_LISTEN_ADDR=127.0.0.1:8080
NEXUSTIER_CONTROLLER_POLL_INTERVAL=15s
NEXUSTIER_CONTROLLER_POLL_JITTER=3s
NEXUSTIER_CONTROLLER_REQUEST_TIMEOUT=20s
NEXUSTIER_CONTROLLER_SHUTDOWN_TIMEOUT=10s
```

Controller 启动时自动运行 checksummed PostgreSQL migrations。不要修改已经应用的
migration 文件；升级时应新增 migration。

### 8.4 创建 systemd 服务

写入 `/etc/systemd/system/nexustier-controller.service`：

```ini
[Unit]
Description=NexusTier telemetry controller
Documentation=https://github.com/WuYouOwO/NexusTier
Wants=network-online.target
Wants=postgresql.service docker.service
After=network-online.target postgresql.service docker.service

[Service]
Type=simple
User=nexustier-controller
Group=nexustier-controller
EnvironmentFile=/etc/nexustier/controller.env
ExecStart=/usr/local/bin/nexustier-controller
Restart=on-failure
RestartSec=3s
TimeoutStopSec=15s

NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
CapabilityBoundingSet=
AmbientCapabilities=
UMask=0027

[Install]
WantedBy=multi-user.target
```

验证并启动：

```bash
sudo systemd-analyze verify /etc/systemd/system/nexustier-controller.service
sudo systemctl daemon-reload
sudo systemctl enable --now nexustier-controller.service
sudo systemctl status nexustier-controller.service --no-pager
```

如果 PostgreSQL 或 Gateway 位于其他主机，删除对应本机 service 依赖，并用网络就绪、
防火墙和外部服务监控保证可达。`Wants` 不是高可用健康检查；Controller 连接失败会
退出并由 `Restart=on-failure` 重试。

### 8.5 验证 Controller

```bash
curl --fail --silent --show-error http://127.0.0.1:8080/healthz | jq
curl --fail --silent --show-error http://127.0.0.1:8080/readyz | jq
curl --fail --silent --show-error \
  http://127.0.0.1:8080/v1/telemetry/status | jq
journalctl -u nexustier-controller.service -n 100 --no-pager
```

解释：

- `/healthz=200`：Controller 进程和 HTTP 服务存活。
- `/readyz=200`：Controller 能在 1 秒内访问 PostgreSQL。
- `last_collection_state=ingested`：最近一轮新快照已写入。
- `last_collection_state=duplicate`：Gateway TTL 内返回同一 `collection_id`，无需重复写入。
- `consecutive_failures>0`：查看 `last_error` 和 Controller 日志。

## 9. 接入 EasyTier 客户端

EasyTier v2.6.4 配置服务器地址格式：

```text
udp://<Gateway 公网 IP 或域名>:22020/<Gateway 环境文件中的相同 Token>
```

仅注册 Session：

```bash
read -rsp 'Gateway admission token: ' GATEWAY_TOKEN && echo
export ET_CONFIG_SERVER="udp://gateway.example.com:22020/${GATEWAY_TOKEN}"
export ET_MACHINE_ID='11111111-2222-3333-4444-555555555555'
unset GATEWAY_TOKEN
easytier-core
```

同时上报本地网络实例：

```bash
read -rsp 'Gateway admission token: ' GATEWAY_TOKEN && echo
export ET_CONFIG_SERVER="udp://gateway.example.com:22020/${GATEWAY_TOKEN}"
export ET_MACHINE_ID='11111111-2222-3333-4444-555555555555'
unset GATEWAY_TOKEN
easytier-core --config-file /etc/easytier/node.toml
```

Machine ID 必须是稳定 UUID。同一设备重装或容器重建后仍应复用原 ID。当前 Gateway
不会创建或修改 EasyTier 网络实例；要看到完整拓扑，客户端必须自行运行网络配置。

## 10. 端到端验收

### 10.1 检查在线 Session 和实时拓扑

```bash
curl --fail --silent --show-error http://127.0.0.1:11211/readyz | jq
curl --fail --silent --show-error http://127.0.0.1:11211/v1/sessions | jq
curl --fail --silent --show-error http://127.0.0.1:11211/v1/topology | jq
```

确认：

- Gateway 日志显示 `secure=true`。
- `/v1/sessions` 中 Machine ID、hostname 和版本正确。
- Token 未出现在 API 和消费日志中。
- 有本地实例时，拓扑包含 `node`、`peers`、`metrics` 或可解释的局部 `errors`。

### 10.2 检查 Controller 摄取

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/v1/telemetry/status | jq
```

检查 PostgreSQL：

```bash
read -rsp 'PostgreSQL password: ' PGPASSWORD && echo
export PGPASSWORD PGSSLMODE=disable
psql -h 127.0.0.1 -U nexustier -d nexustier
```

```sql
SELECT collection_id, status, machine_count, error_count, collected_at
FROM telemetry_collection_runs
ORDER BY collected_at DESC
LIMIT 10;

SELECT machine_id, hostname, active, last_observed_at, disappeared_at
FROM machines
ORDER BY hostname, machine_id;

SELECT p.source_instance_id, p.destination_peer_id, p.direct, p.latency_ms,
       p.loss_rate, p.rx_bytes, p.tx_bytes, p.last_observed_at
FROM peer_links_current AS p
JOIN network_instances AS i ON i.instance_id = p.source_instance_id
JOIN machines AS m ON m.machine_id = i.machine_id
WHERE i.active AND m.active
ORDER BY p.source_instance_id, p.destination_peer_id;
```

退出 `psql` 后执行 `unset PGPASSWORD PGSSLMODE`。

Controller 当前没有公开拓扑查询 API，上述 SQL 是当前版本查看持久化状态的方式。

## 11. 防火墙与远程运维

只向 EasyTier 客户端开放 `22020/UDP`。不要公网开放 `11211`、`5432` 或 `8080`。

远程检查时使用 SSH 本地转发：

```bash
ssh -N \
  -L 11211:127.0.0.1:11211 \
  -L 8080:127.0.0.1:8080 \
  operator@gateway.example.com
```

然后在本地访问：

```bash
curl http://127.0.0.1:11211/healthz
curl http://127.0.0.1:8080/v1/telemetry/status
```

## 12. 备份和恢复

至少备份：

- `/etc/nexustier/gateway.env`。
- `/etc/nexustier/controller.env`。
- `/etc/systemd/system/nexustier-controller.service`。
- 固定源码 commit、Controller 二进制及 SHA-256。
- Gateway 镜像 tag、digest 和 Cosign 验证结果。
- PostgreSQL 数据库。

数据库备份前记录 Controller 版本和 migration 状态。需要严格一致的业务快照时，先停止
Controller；Gateway 可以继续接收客户端，但停止期间不会持久化新 collection。

```bash
sudo systemctl stop nexustier-controller.service
umask 077
install -d -m 0700 "${HOME}/nexustier-backups"
read -rsp 'PostgreSQL password: ' PGPASSWORD && echo
export PGPASSWORD PGSSLMODE=disable
pg_dump \
  -h 127.0.0.1 -U nexustier -d nexustier \
  --format=custom \
  --file="${HOME}/nexustier-backups/nexustier-$(date +%Y%m%d-%H%M%S).dump"
unset PGPASSWORD PGSSLMODE
sudo systemctl start nexustier-controller.service
```

恢复到新建空数据库：

```bash
sudo systemctl stop nexustier-controller.service
sudo -u postgres dropdb --if-exists nexustier_restore
sudo -u postgres createdb --owner=nexustier nexustier_restore
read -rsp 'PostgreSQL password: ' PGPASSWORD && echo
export PGPASSWORD PGSSLMODE=disable
pg_restore \
  -h 127.0.0.1 -U nexustier -d nexustier_restore \
  --no-owner --exit-on-error \
  nexustier-YYYYMMDD-HHMMSS.dump
unset PGPASSWORD PGSSLMODE
```

先用 `psql` 检查 `schema_migrations`、最新 collection 和机器数量，再把
`NEXUSTIER_CONTROLLER_DATABASE_URL` 指向恢复库并启动 Controller。不要在未验证时覆盖
原生产数据库；确认恢复成功后再按组织流程切换或归档旧库。

当前 `metric_samples` 没有自动保留或分区策略，会持续增长。生产使用必须监测数据库
容量，并在下一版本迁移提供正式保留策略前制定经过验证的外部归档方案。

## 13. 升级和回滚

升级顺序建议：

1. 备份 PostgreSQL 和两个环境文件。
2. 固定并审核新源码提交、镜像 digest 和 migration。
3. 先执行 Rust/Go/契约测试。
4. 升级 Gateway，等待 EasyTier 客户端重连。
5. 升级 Controller；启动时自动执行新 migration。
6. 验证两个服务的健康、状态 API 和 PostgreSQL 最新 collection。

Gateway 回滚可以恢复旧 digest。Controller 如果新版本已经应用数据库 migration，回滚
前必须确认旧二进制兼容新 schema；当前迁移系统没有自动 down migration。

## 14. 常见故障

| 现象 | 原因与处理 |
| --- | --- |
| Gateway `/readyz` 返回 `503` | 没有已注册 EasyTier 客户端；进程仍可能健康 |
| 客户端只出现能力探测 | 未完成 Noise 安全重连、Token 不匹配或版本不兼容 |
| Session 在线但实例为空 | 客户端没有加载本地 EasyTier 网络配置 |
| Controller 启动即退出 | 检查数据库 URL、密码 URL 编码、PostgreSQL 和 Gateway URL |
| Controller `/readyz` 返回 `503` | PostgreSQL 不可达；检查日志、监听和 `pg_hba.conf` |
| `last_error` 为 fetch topology | Gateway API 不可达或采集超过 Controller 请求超时 |
| `last_error` 为 ingest topology | migration/schema/数据库权限或数据约束错误 |
| `duplicate` 持续出现 | Gateway TTL 或轮询间隔导致重复 collection，属于幂等正常状态 |
| 数据库持续增长 | 当前没有 metric retention；监测并规划归档 |
| 老快照没有覆盖新状态 | Controller 的乱序保护正常工作 |

## 15. 上线检查表

部署前：

- [ ] 源码固定为审核过的 commit。
- [ ] Gateway 镜像固定 SHA tag 或 digest，并验证 Cosign 签名。
- [ ] Rust 14 项测试、Clippy、格式检查通过。
- [ ] Go test、vet 和 PostgreSQL 专用测试库集成测试通过。
- [ ] Token 和数据库密码不在仓库、命令历史或日志中。
- [ ] 只开放 `22020/UDP`，内部端口保持回环/私网。
- [ ] PostgreSQL 备份和容量监测已配置。

上线后：

- [ ] Gateway `/healthz` 返回 `200`。
- [ ] EasyTier 接入后 Gateway `/readyz` 返回 `200`。
- [ ] Gateway 日志显示安全 Session。
- [ ] Controller `/healthz` 和 `/readyz` 返回 `200`。
- [ ] Controller status 出现最近成功 collection。
- [ ] PostgreSQL 中 Machine、Instance 和 Peer 状态符合预期。
- [ ] Gateway SIGINT/Docker stop 和 Controller SIGTERM 均可优雅退出。

## 16. 相关文档

- [当前版本用户教程](current-usage-guide.zh-CN.md)
- [中文文档索引](README.md)
- [Gateway 专项生产部署指南](deployment-guide.zh-CN.md)
- [Rust Gateway API 手册](gateway-guide.zh-CN.md)
- [Controller 运行说明](../controller/README.md)
- [Controller 源码架构解析](controller-code.zh-CN.md)
- [内部 topology v1 契约](../contracts/README.md)
