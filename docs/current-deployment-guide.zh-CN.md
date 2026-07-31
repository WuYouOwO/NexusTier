# NexusTier 当前版本 Docker Compose 部署指南

## 1. 文档范围

本文使用仓库提供的 `compose.example.yaml`，在一台 Linux 主机上部署当前已经实现的
完整遥测链路：

```mermaid
flowchart LR
    ET[EasyTier v2.6.4 客户端] -->|UDP 22020| GW[Rust Gateway 容器]
    GW -->|HTTP 11211<br/>Compose 私有网络| CTRL[Go Controller 容器]
    CTRL -->|TCP 5432<br/>Compose 私有网络| PG[(PostgreSQL 18 容器)]
    WEB[内嵌只读拓扑控制台] -->|同源 HTTP 8080| CTRL
    OPS[运维人员] -->|127.0.0.1:11211/8080| API[内部 API]
    ET -. 加密 Mesh 数据面 .-> PEERS[其他 EasyTier 节点]
```

当前完整栈可以：

- 接收原生 EasyTier v2.6.4 WebClient 安全连接。
- 按 Machine ID 管理在线 Session 和安全重连。
- 反向采集 Node、Peer、Route、RTT、丢包、流量和 Stats。
- 通过 `nexustier.topology.v1` 契约把实时拓扑交给 Controller。
- 将机器、实例、节点、当前 Peer 链路、指标和采集错误写入 PostgreSQL。
- 从规范化表查询持久化当前拓扑、collection 新鲜度和结构化错误。
- 在 Controller 根路径查看内嵌响应式拓扑控制台。
- 定期批量清理过期指标和原始 payload，同时保留审计元数据和 SHA-256 指纹。

当前仍不包含认证/多租户公开控制台、Redis、OIDC/RBAC、IPAM、ACL、SSH 或 RDP。Gateway
只处理 EasyTier 控制协议和遥测，不转发 EasyTier Mesh 数据面流量。

## 2. 已验证版本基线

| 项目 | 当前基线 |
| --- | --- |
| 源码提交 | `302df2f429c26a3fefd12a233d4296a7a042dc08` |
| Gateway 镜像 | `ghcr.io/wuyouowo/nexustier:sha-302df2f` |
| Gateway digest | `sha256:35ac975021ee951e1b9befaabb71d29adf566ba500b064f15fcf0879363cb3c0` |
| Controller 镜像 | `ghcr.io/wuyouowo/nexustier-controller:sha-302df2f` |
| Controller digest | `sha256:1d1022885b54fadb83dd60346ef3ed11f35b38ce6a18301f7e684dbc27702362` |
| EasyTier | `v2.6.4`，commit `8428a89d2dabc94c97d370ec607c6ca142473626` |
| PostgreSQL | `18-bookworm`；Controller 支持 PostgreSQL `14+` |
| topology 契约 | `nexustier.topology.v1` |
| GitHub Actions | Run `30542389054`，质量门禁、双镜像构建、推送和签名均成功 |
| 容器平台 | `linux/amd64` |

本文默认固定 SHA 标签，便于同时升级 Gateway 和 Controller。要求更严格的生产环境可把
`.env` 中的两个镜像引用替换为上表 digest。不要把会移动的 `latest` 作为无人值守生产
部署的唯一版本约束。

## 3. 端口与容器网络

| 端口 | 宿主机绑定 | 是否公网开放 | 用途 |
| --- | --- | --- | --- |
| `22020/UDP` | `0.0.0.0:22020` | 按客户端来源策略开放 | EasyTier WebClient 控制通道 |
| `11211/TCP` | `127.0.0.1:11211` | 禁止 | Gateway 只读运维 API |
| `8080/TCP` | `127.0.0.1:8080` | 禁止 | 内嵌控制台和 Controller 只读 API |
| `5432/TCP` | 不发布到宿主机 | 禁止 | Controller 到 PostgreSQL |

Compose 创建 `control-plane` 私有 bridge 网络。Controller 使用服务名
`http://gateway:11211` 访问 Gateway，并使用服务名 `postgres:5432` 访问数据库。
只有 UDP `22020` 需要从外部网络进入；两个 HTTP API 默认仅供宿主机本地运维。

## 4. 主机准备

示例面向 Debian/Ubuntu `linux/amd64` 主机，需要：

- Docker Engine。
- Docker Compose v2 插件，命令为 `docker compose`。
- Git、curl、jq、openssl 和 ca-certificates。
- 可访问 GHCR，或已配置可用的镜像代理。

安装普通工具；Docker Engine 和 Compose 请按 Docker 官方仓库对应发行版安装：

```bash
sudo apt-get update
sudo apt-get install -y git curl jq openssl ca-certificates
docker --version
docker compose version
docker info
```

确认架构和端口未被占用：

```bash
test "$(uname -m)" = x86_64
ss -lunp | grep ':22020' || true
ss -lntp | grep -E ':(11211|8080)\b' || true
```

若普通用户无权访问 Docker daemon，请配置受控的 Docker 用户权限，或在本文 Docker
命令前使用 `sudo`。加入 `docker` 组等同于授予高权限，应按主机安全策略决定。

## 5. 获取部署文件

获取并固定到已验证提交：

```bash
git clone https://github.com/WuYouOwO/NexusTier.git
cd NexusTier
git checkout 302df2f429c26a3fefd12a233d4296a7a042dc08
git status --short --branch
```

预期处于 detached HEAD，工作树干净，并存在以下文件：

```bash
test -f compose.example.yaml
test -f .env.example
```

升级时应获取新的审核提交，不要在生产目录中直接跟随未知的远端最新状态。

## 6. 创建部署变量

### 6.1 生成安全变量文件

数据库密码使用十六进制随机值，避免 URL 保留字符造成连接 URL 编码不一致。以下命令
只把随机值写入 `.env`，不会把真实值作为命令参数写入 shell 历史：

```bash
umask 077
POSTGRES_PASSWORD_VALUE="$(openssl rand -hex 32)"
GATEWAY_TOKEN_VALUE="$(openssl rand -hex 32)"

cat >.env <<EOF
NEXUSTIER_GATEWAY_IMAGE=ghcr.io/wuyouowo/nexustier:sha-302df2f
NEXUSTIER_CONTROLLER_IMAGE=ghcr.io/wuyouowo/nexustier-controller:sha-302df2f

POSTGRES_DB=nexustier
POSTGRES_USER=nexustier
POSTGRES_PASSWORD=${POSTGRES_PASSWORD_VALUE}
NEXUSTIER_CONTROLLER_DATABASE_URL=postgres://nexustier:${POSTGRES_PASSWORD_VALUE}@postgres:5432/nexustier?sslmode=disable
NEXUSTIER_GATEWAY_ADMISSION_TOKEN=${GATEWAY_TOKEN_VALUE}

NEXUSTIER_GATEWAY_LISTEN_PORT=22020
NEXUSTIER_GATEWAY_API_BIND=127.0.0.1
NEXUSTIER_CONTROLLER_API_BIND=127.0.0.1

NEXUSTIER_GATEWAY_RPC_TIMEOUT_MS=5000
NEXUSTIER_GATEWAY_COLLECTION_TIMEOUT_MS=15000
NEXUSTIER_GATEWAY_MACHINE_CONCURRENCY=8
NEXUSTIER_GATEWAY_SNAPSHOT_TTL_MS=1000
RUST_LOG=info

NEXUSTIER_CONTROLLER_POLL_INTERVAL=15s
NEXUSTIER_CONTROLLER_POLL_JITTER=3s
NEXUSTIER_CONTROLLER_REQUEST_TIMEOUT=20s
NEXUSTIER_CONTROLLER_SHUTDOWN_TIMEOUT=10s
NEXUSTIER_CONTROLLER_METRIC_RETENTION=720h
NEXUSTIER_CONTROLLER_CLEANUP_INTERVAL=6h
NEXUSTIER_CONTROLLER_CLEANUP_BATCH_SIZE=10000
EOF

unset POSTGRES_PASSWORD_VALUE GATEWAY_TOKEN_VALUE
chmod 0600 .env
```

`.env` 已被仓库 `.gitignore` 忽略，但仍应确认没有意外暂存：

```bash
git status --short
stat -c '%a %n' .env
```

预期 `.env` 权限为 `600`。不要把它提交、上传到工单或粘贴到日志中。

### 6.2 变量说明

| 变量 | 用途 |
| --- | --- |
| `NEXUSTIER_GATEWAY_IMAGE` | Gateway GHCR 镜像标签或 digest |
| `NEXUSTIER_CONTROLLER_IMAGE` | Controller GHCR 镜像标签或 digest |
| `POSTGRES_DB` / `POSTGRES_USER` | 初始化数据库和角色 |
| `POSTGRES_PASSWORD` | PostgreSQL 角色密码 |
| `NEXUSTIER_CONTROLLER_DATABASE_URL` | Controller 使用的完整数据库 URL |
| `NEXUSTIER_GATEWAY_ADMISSION_TOKEN` | EasyTier 客户端共享准入 Token |
| `NEXUSTIER_GATEWAY_LISTEN_PORT` | 宿主机对外 UDP 端口 |
| `NEXUSTIER_GATEWAY_API_BIND` | Gateway API 宿主机绑定地址 |
| `NEXUSTIER_CONTROLLER_API_BIND` | Controller API 宿主机绑定地址 |
| `NEXUSTIER_CONTROLLER_METRIC_RETENTION` | 指标样本和 collection 原始 payload 保留时间 |
| `NEXUSTIER_CONTROLLER_CLEANUP_INTERVAL` | 后台保留清理周期 |
| `NEXUSTIER_CONTROLLER_CLEANUP_BATCH_SIZE` | 每批删除或裁剪的最大行数 |

`POSTGRES_PASSWORD` 与数据库 URL 中的密码必须一致。若手工使用含 URL 保留字符的密码，
必须只对 URL 中的密码部分做百分号编码。不要把两个 API bind 改为公网地址，除非外层
已经提供经过审计的认证代理和网络访问控制。

`POSTGRES_PASSWORD` 只在数据目录为空、PostgreSQL 首次执行 `initdb` 时生效。若
`postgres-data` 卷已经存在，重新生成 `.env` 不会改变数据库中已有的角色密码，
Controller 会以 `SQLSTATE 28P01` 认证失败退出。在同一台主机上重复部署时，应保留原有
`.env`；确实需要更换密码时，改数据库角色而不是改 `.env`：

```bash
docker compose --env-file .env -f compose.example.yaml exec postgres \
  psql -U nexustier -d nexustier -c "\password nexustier"
```

交互输入 `.env` 中的密码。`\password` 在客户端侧计算 SCRAM 校验值，明文不会进入
服务端日志或命令历史。只有在确认可以永久丢弃全部遥测数据时，才用删除卷的方式重置。

## 7. 验证并拉取镜像

先让 Compose 完整解析变量和依赖：

```bash
docker compose --env-file .env -f compose.example.yaml config --quiet
```

如需查看展开结果，可执行下列命令，但输出会包含数据库 URL 和 Token；不要把输出保存
到日志或发送给他人：

```bash
docker compose --env-file .env -f compose.example.yaml config
```

拉取三个镜像：

```bash
docker compose --env-file .env -f compose.example.yaml pull
```

可选：安装 Cosign 后验证两个应用镜像由本仓库 `main` 工作流签名：

```bash
cosign verify \
  --certificate-identity \
  'https://github.com/WuYouOwO/NexusTier/.github/workflows/docker-publish.yml@refs/heads/main' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  'ghcr.io/wuyouowo/nexustier@sha256:35ac975021ee951e1b9befaabb71d29adf566ba500b064f15fcf0879363cb3c0'

cosign verify \
  --certificate-identity \
  'https://github.com/WuYouOwO/NexusTier/.github/workflows/docker-publish.yml@refs/heads/main' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  'ghcr.io/wuyouowo/nexustier-controller@sha256:1d1022885b54fadb83dd60346ef3ed11f35b38ce6a18301f7e684dbc27702362'
```

## 8. 启动完整栈

后台启动 PostgreSQL、Gateway 和 Controller：

```bash
docker compose --env-file .env -f compose.example.yaml up -d
```

Compose 文件也包含 Controller 的本地 `build` 定义。正常部署会使用 `.env` 中固定的
已发布镜像；只有审核当前源码并希望本机构建 Controller 时才执行：

```bash
docker compose --env-file .env -f compose.example.yaml up -d --build controller
```

启动顺序由健康检查控制：

1. PostgreSQL 通过 `pg_isready` 后标记 healthy。
2. Gateway `/healthz` 返回 `200` 后标记 healthy；不要求已有 EasyTier 客户端。
3. Controller 等待前两项 healthy，再连接数据库、自动执行 checksummed migrations，
   然后启动轮询和 HTTP API。

查看状态：

```bash
docker compose --env-file .env -f compose.example.yaml ps
docker compose --env-file .env -f compose.example.yaml logs --tail 100
```

三个服务应处于 `Up`；PostgreSQL、Gateway 和 Controller 最终都应显示 healthy。

## 9. 验证部署

### 9.1 健康和状态 API

```bash
curl --fail --silent --show-error http://127.0.0.1:11211/healthz | jq
curl --fail --silent --show-error http://127.0.0.1:8080/healthz | jq
curl --fail --silent --show-error http://127.0.0.1:8080/readyz | jq
curl --fail --silent --show-error \
  http://127.0.0.1:8080/v1/telemetry/status | jq
curl --fail --silent --show-error \
  http://127.0.0.1:8080/v1/retention/status | jq
curl --fail --silent --show-error \
  'http://127.0.0.1:8080/v1/topology?active=true&limit=100' | jq
```

预期：

- Gateway `/healthz` 返回 `status=ok`。
- Controller `/healthz` 返回 `status=ok`。
- Controller `/readyz` 返回 `status=ready`，表示 PostgreSQL 可访问。
- Controller status 会在启动后出现轮询尝试；尚无客户端时可以成功摄取空拓扑。
- Controller topology 返回持久化当前状态，而不是触发 Gateway 实时 RPC。
- 浏览器访问 `http://127.0.0.1:8080/` 可打开内嵌只读拓扑控制台。
- retention status 显示保留周期、批量大小、最近和累计删除/裁剪数量。

Gateway `/readyz` 在没有 EasyTier 客户端时会返回 `503`，这是合法状态，不表示容器
不健康：

```bash
curl --silent --show-error --include http://127.0.0.1:11211/readyz
```

### 9.2 验证数据库 migration

PostgreSQL 不发布宿主机端口，应通过容器内 `psql` 检查：

```bash
docker compose --env-file .env -f compose.example.yaml exec postgres \
  psql -U nexustier -d nexustier \
  -c 'SELECT version, applied_at FROM schema_migrations ORDER BY version;'
```

应至少包含 `001_telemetry.sql` 和 `002_metric_retention.sql`。第二个 migration 为新
collection 增加 raw payload SHA-256 指纹和裁剪时间；不要修改已经应用的文件。

如 Controller 未启动，优先查看其日志：

```bash
docker compose --env-file .env -f compose.example.yaml logs --tail 200 controller
```

## 10. 接入 EasyTier 客户端

客户端配置服务器地址格式为：

```text
udp://<Gateway 公网 IP 或域名>:22020/<.env 中的 NEXUSTIER_GATEWAY_ADMISSION_TOKEN>
```

在客户端交互读取 Token，避免真实值进入命令历史：

```bash
read -rsp 'Gateway admission token: ' GATEWAY_TOKEN && echo
export ET_CONFIG_SERVER="udp://gateway.example.com:22020/${GATEWAY_TOKEN}"
export ET_MACHINE_ID='11111111-2222-3333-4444-555555555555'
unset GATEWAY_TOKEN
easytier-core --config-file /etc/easytier/node.toml
```

替换 Gateway 地址和 Machine ID。每台设备必须使用长期稳定且唯一的 UUID；同一设备
重装或容器重建后应继续复用原 ID。NexusTier 当前不会创建 EasyTier 网络实例，客户端
必须加载自己的 EasyTier 配置才能上报 Node、Peer、Route 和 Stats。

接入后验证：

```bash
curl --fail --silent --show-error http://127.0.0.1:11211/readyz | jq
curl --fail --silent --show-error http://127.0.0.1:11211/v1/sessions | jq
curl --fail --silent --show-error http://127.0.0.1:11211/v1/topology | jq
```

确认 Gateway 日志中的会话为 `secure=true`，且 API 响应不包含共享 Token：

```bash
docker compose --env-file .env -f compose.example.yaml logs --tail 100 gateway
```

## 11. 验证 Controller 摄取

等待一个轮询周期后检查状态：

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/v1/telemetry/status | jq
```

`last_collection_state=ingested` 表示新快照已写入；`duplicate` 表示 Gateway TTL 内返回
了相同 `collection_id`，Controller 正确跳过重复写入，并不是错误。

### 11.1 使用持久化拓扑 API 和控制台

```bash
curl --fail --silent --show-error \
  'http://127.0.0.1:8080/v1/topology?active=true&limit=100' | jq
```

响应包括 `latest_collection`、`latest_errors`、按 Machine UUID 排序的 `machines` 和
`page.next_cursor`。可选参数：

- `active=true|false`：过滤 Machine 和 Instance 活动状态。
- `machine_id=<uuid>`：精确读取指定 Machine。
- `cursor=<uuid>`：从该 Machine UUID 之后继续读取。
- `limit=1..500`：每页 Machine 数，默认 `100`。

浏览器打开 `http://127.0.0.1:8080/` 后，可查看连接图、Machine 清单、Peer RTT、
丢包、流量、collection 新鲜度和结构化错误。该页面无认证且与 API 同源，只允许回环
或 SSH 转发访问。

### 11.2 检查保留状态

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/v1/retention/status | jq
```

默认保留 `720h`。到期的 `metric_samples` 被分批删除，旧 collection 的 raw payload
裁剪为 JSON `null`；collection 边界、状态、结构化错误和 SHA-256 指纹继续保留，确保
审计和重复 collection 冲突检测仍有效。

### 11.3 使用 SQL 深度排障

查询最近采集和当前机器：

```bash
docker compose --env-file .env -f compose.example.yaml exec postgres \
  psql -U nexustier -d nexustier -c '
    SELECT collection_id, status, machine_count, error_count, collected_at
    FROM telemetry_collection_runs
    ORDER BY collected_at DESC
    LIMIT 10;'

docker compose --env-file .env -f compose.example.yaml exec postgres \
  psql -U nexustier -d nexustier -c '
    SELECT machine_id, hostname, active, last_observed_at, disappeared_at
    FROM machines
    ORDER BY hostname, machine_id;'
```

查看当前 Peer 链路：

```bash
docker compose --env-file .env -f compose.example.yaml exec postgres \
  psql -U nexustier -d nexustier -c '
    SELECT source_instance_id, destination_peer_id, direct, latency_ms,
           loss_rate, rx_bytes, tx_bytes, last_observed_at
    FROM peer_links_current
    ORDER BY source_instance_id, destination_peer_id;'
```

日常查看优先使用控制台和 API；SQL 用于深度排障、审计和备份验证。

## 12. 日常运维

### 12.1 查看状态和日志

```bash
docker compose --env-file .env -f compose.example.yaml ps
docker compose --env-file .env -f compose.example.yaml logs --tail 100 gateway
docker compose --env-file .env -f compose.example.yaml logs --tail 100 controller
docker compose --env-file .env -f compose.example.yaml logs --tail 100 postgres
```

持续查看日志：

```bash
docker compose --env-file .env -f compose.example.yaml logs --follow gateway controller
```

### 12.2 重启服务

```bash
docker compose --env-file .env -f compose.example.yaml restart gateway
docker compose --env-file .env -f compose.example.yaml restart controller
```

Gateway 重启后内存 Session Pool 暂时为空，EasyTier 客户端会自动重连。期间 Gateway
`/readyz=503` 属于正常恢复过程，Controller 会继续轮询并在 Gateway 恢复后重新成功。

### 12.3 修改配置

编辑 `.env` 后，`restart` 不会应用新的环境变量，必须重建对应容器：

```bash
docker compose --env-file .env -f compose.example.yaml config --quiet
docker compose --env-file .env -f compose.example.yaml up -d
```

轮询抖动必须大于等于 0 且小于轮询间隔。排障时可以临时提高 `RUST_LOG`，完成后恢复
`info`，避免长期产生大量底层日志。

## 13. 备份与恢复

至少备份：

- 权限为 `0600` 的 `.env`。
- 当前 `compose.example.yaml` 和对应源码提交。
- 两个应用镜像标签、digest 和 Cosign 验证结果。
- PostgreSQL 数据库。

### 13.1 创建数据库备份

需要严格一致的业务快照时先暂停 Controller，Gateway 可以继续接收客户端：

```bash
mkdir -p backups
chmod 0700 backups
docker compose --env-file .env -f compose.example.yaml stop controller
docker compose --env-file .env -f compose.example.yaml exec -T postgres \
  pg_dump -U nexustier -d nexustier --format=custom \
  >"backups/nexustier-$(date +%Y%m%d-%H%M%S).dump"
docker compose --env-file .env -f compose.example.yaml start controller
```

确认备份非空并限制权限：

```bash
chmod 0600 backups/*.dump
ls -lh backups/*.dump
```

### 13.2 恢复到新数据库验证

不要直接覆盖未验证的生产库。先创建恢复库并导入：

```bash
BACKUP_FILE=backups/nexustier-YYYYMMDD-HHMMSS.dump

docker compose --env-file .env -f compose.example.yaml exec -T postgres \
  createdb -U nexustier nexustier_restore

docker compose --env-file .env -f compose.example.yaml exec -T postgres \
  pg_restore -U nexustier -d nexustier_restore --no-owner --exit-on-error \
  <"${BACKUP_FILE}"

docker compose --env-file .env -f compose.example.yaml exec postgres \
  psql -U nexustier -d nexustier_restore \
  -c 'SELECT version, applied_at FROM schema_migrations ORDER BY version;'
```

验证恢复库的 migration、最近 collection 和机器数量后，再按变更流程切换 Controller
数据库 URL。指标和 raw payload 有自动保留，但 collection 元数据、结构化错误和当前
状态不会自动删除；当前也没有 PostgreSQL 分区/长期聚合，生产环境仍必须监测卷容量。

## 14. 升级和回滚

### 14.1 升级

1. 备份 `.env` 和 PostgreSQL。
2. 审核新提交、两个镜像 digest 和 migration。
3. 在 `.env` 中同时更新 Gateway 和 Controller 镜像引用。
4. 拉取镜像并重建应用容器。
5. 验证健康、客户端重连、Controller 状态和数据库最新 collection。

```bash
docker compose --env-file .env -f compose.example.yaml config --quiet
docker compose --env-file .env -f compose.example.yaml pull gateway controller
docker compose --env-file .env -f compose.example.yaml up -d gateway controller
docker compose --env-file .env -f compose.example.yaml ps
```

Controller 启动时自动执行 checksummed migrations。不要修改已应用的 migration 文件；
升级只能新增 migration。

### 14.2 回滚

把 `.env` 中两个镜像恢复为先前记录的标签或 digest，然后执行：

```bash
docker compose --env-file .env -f compose.example.yaml pull gateway controller
docker compose --env-file .env -f compose.example.yaml up -d gateway controller
```

如果新版本已应用数据库 migration，回滚前必须确认旧 Controller 兼容新 schema。当前
迁移系统没有自动 down migration，不应只回滚镜像而忽略数据库兼容性。

## 15. 防火墙与远程运维

- 只向 EasyTier 客户端开放 UDP `22020`。
- 禁止公网开放 TCP `11211`、`8080` 和 PostgreSQL。
- 有固定客户端出口地址时，优先在云安全组或主机防火墙限制 UDP 来源。
- Gateway 不需要 TUN、`NET_ADMIN`、host network 或特权模式。
- `.env`、数据库备份和准入 Token 都应按敏感凭据管理。

远程检查内部 API 时使用 SSH 本地转发：

```bash
ssh -N \
  -L 11211:127.0.0.1:11211 \
  -L 8080:127.0.0.1:8080 \
  operator@gateway.example.com
```

然后在运维终端访问 `http://127.0.0.1:11211` 和
`http://127.0.0.1:8080`，不要为了方便修改 Compose 使其监听公网。

## 16. 常见故障

| 现象 | 原因与处理 |
| --- | --- |
| Compose 报 required variable | `.env` 缺少数据库 URL、密码或 Gateway Token；重新执行第 6 节 |
| PostgreSQL unhealthy | 检查 `postgres` 日志、卷权限、磁盘容量和密码初始化状态 |
| Controller 启动即退出 | 检查数据库 URL 与密码是否一致、PostgreSQL 是否 healthy |
| Controller 报 `SQLSTATE 28P01` | 卷中的角色密码早于当前 `.env`；按第 6.2 节改数据库角色密码，不要靠重新生成 `.env` |
| Gateway `/readyz` 返回 `503` | 尚无安全注册客户端；Gateway `/healthz=200` 时容器仍正常 |
| 客户端只有能力探测 | 检查 Noise 安全重连、Token、UDP 22020、NAT 和 EasyTier v2.6.4 |
| Session 在线但实例为空 | 客户端没有加载本地 EasyTier 网络配置 |
| Controller 没有成功 collection | 查看 `/v1/telemetry/status` 和 Controller 日志 |
| 控制台没有 Machine | 检查 `/v1/topology`、active 过滤、最近摄取状态和客户端本地实例 |
| `/v1/topology` 返回 `503` | PostgreSQL 查询失败；检查 `/readyz` 和 Controller 日志 |
| retention status 有 `last_error` | 检查数据库连接、锁等待、磁盘容量和批量大小 |
| `last_error` 为 fetch topology | Gateway API 不可达或采集超过 Controller 请求超时 |
| `last_error` 为 ingest topology | 检查 migration、schema、数据库权限和约束错误 |
| `duplicate` 持续出现 | 轮询间隔与 Gateway TTL 导致重复 ID；属于幂等正常状态 |
| API 从远程不可访问 | 默认只绑定宿主机回环；使用 SSH 转发，不要直接公网暴露 |
| 数据库仍持续增长 | 当前状态和 collection 元数据不自动删除；监测卷并规划分区/长期归档 |

## 17. 停止与卸载

停止并删除容器和私有网络，但保留 PostgreSQL 数据卷：

```bash
docker compose --env-file .env -f compose.example.yaml down
```

再次 `up -d` 会复用 `postgres-data`。只有在已经备份且确认永久删除全部数据库数据时，
才可以执行：

```bash
docker compose --env-file .env -f compose.example.yaml down --volumes
```

`--volumes` 会不可逆删除 PostgreSQL 命名卷，不应作为普通重启或升级命令。

## 18. 上线检查表

部署前：

- [ ] 源码、Gateway 镜像和 Controller 镜像固定到同一审核版本。
- [ ] 两个应用镜像 digest 已记录，必要时已验证 Cosign 签名。
- [ ] `.env` 权限为 `0600`，密码和 Token 未进入仓库或日志。
- [ ] `docker compose ... config --quiet` 通过。
- [ ] 只开放 UDP `22020`，内部端口保持回环或 Compose 私网。
- [ ] PostgreSQL 备份和容量监测方案已准备。

上线后：

- [ ] 三个服务均为 `Up`/healthy。
- [ ] Gateway `/healthz` 返回 `200`。
- [ ] Controller `/healthz` 和 `/readyz` 返回 `200`。
- [ ] Controller `/v1/topology` 返回持久化 Machine/Peer，根路径控制台可打开。
- [ ] `/v1/retention/status` 无错误且保留参数符合容量策略。
- [ ] EasyTier 接入后 Gateway `/readyz` 返回 `200`。
- [ ] Gateway 日志显示 `secure=true` Session，API 不包含 Token。
- [ ] Controller status 出现最近成功 collection。
- [ ] PostgreSQL 中 Machine、Instance 和 Peer 状态符合预期。
- [ ] 已执行一次备份并验证可恢复到新数据库。

## 19. 相关文档

- [当前版本用户教程](current-usage-guide.zh-CN.md)
- [中文文档索引](README.md)
- [Gateway 专项生产部署指南](deployment-guide.zh-CN.md)
- [Rust Gateway API 手册](gateway-guide.zh-CN.md)
- [Controller 运行说明](../controller/README.md)
- [Controller 源码架构解析](controller-code.zh-CN.md)
- [内部 topology v1 契约](../contracts/README.md)
