# NexusTier Gateway 当前版本专项使用指南

本文只讲当前 Gateway 的镜像拉取、服务启动、EasyTier 客户端接入、实时遥测、升级和回滚。需要部署 Controller 和 PostgreSQL 时，请直接使用[当前版本端到端部署指南](current-deployment-guide.zh-CN.md)；面向普通使用者的完整操作流程见[当前版本用户教程](current-usage-guide.zh-CN.md)。

> 本文固定到已验证提交 `302df2f`。对应镜像已经包含安全心跳、可选共享 Token 校验、topology v1 契约、单飞采集和短期缓存。

更完整的生产主机、安全加固、systemd、防火墙和 NAT 配置参见[生产部署指南](deployment-guide.zh-CN.md)；所有 API 字段及 JSON 示例参见[Rust 网关使用与部署手册](gateway-guide.zh-CN.md)。

## 1. 当前版本

| 项目 | 当前值 |
| --- | --- |
| NexusTier Gateway | `0.1.0` |
| 源码提交 | `302df2f429c26a3fefd12a233d4296a7a042dc08` |
| EasyTier 兼容基线 | `v2.6.4` |
| 容器仓库 | `ghcr.io/wuyouowo/nexustier` |
| 固定镜像标签 | `sha-302df2f` |
| 发布镜像 digest | `sha256:35ac975021ee951e1b9befaabb71d29adf566ba500b064f15fcf0879363cb3c0` |
| EasyTier 控制通道 | `22020/UDP` |
| 本地只读 API | `127.0.0.1:11211/TCP` |

当前版本已经实现：

- 接收未修改的 EasyTier v2.6.4 WebClient 连接。
- 使用 Noise 握手和 AES-GCM 建立安全配置通道。
- 拒绝明文心跳，可选校验共享准入 Token，并固定会话 Machine ID。
- 按 Machine ID 维护在线会话，并安全处理重连替换。
- 反向读取客户端本地网络实例的 Node、Peer、Route、RTT、流量和 Stats。
- 提供 `/healthz`、`/readyz`、`/v1/sessions` 和 `/v1/topology` 只读 API。

当前版本尚未实现：

- Gateway 镜像不包含 Go Controller；持久化拓扑、指标保留和内嵌只读控制台由独立的
  `ghcr.io/wuyouowo/nexustier-controller` 镜像提供。
- Redis 与跨副本会话同步。
- 用户登录、RBAC、OIDC、IPAM 和 ACL 策略下发。
- 通过网关创建或修改 EasyTier 网络实例。
- HTTP API 身份认证。

仅配置 NexusTier Gateway 可以让客户端注册并出现在会话列表中；如需看到完整拓扑，EasyTier 客户端还必须运行自己的本地网络实例。

## 2. 最快启动方式

### 2.1 准备主机

主机需要：

- Linux x86-64。
- Docker Engine 或兼容 OCI 运行时。
- 可供客户端访问的 UDP `22020`。
- 本机未占用 TCP `11211`。

公网部署时，在云安全组、防火墙和 NAT 中只开放 UDP `22020`。TCP `11211` 只映射到宿主机回环地址，不要直接暴露到互联网。

### 2.2 拉取镜像

直接使用当前稳定发布：

```bash
docker pull ghcr.io/wuyouowo/nexustier:sha-302df2f
```

需要完全固定构建产物时使用 digest：

```bash
docker pull \
  ghcr.io/wuyouowo/nexustier@sha256:35ac975021ee951e1b9befaabb71d29adf566ba500b064f15fcf0879363cb3c0
```

`latest` 跟随 `main` 更新，适合体验，不建议作为无人值守生产部署的唯一版本约束。

已安装 Cosign 时，可以验证当前镜像由本仓库 `main` 分支的发布工作流生成：

```bash
cosign verify \
  --certificate-identity \
  'https://github.com/WuYouOwO/NexusTier/.github/workflows/docker-publish.yml@refs/heads/main' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  'ghcr.io/wuyouowo/nexustier@sha256:35ac975021ee951e1b9befaabb71d29adf566ba500b064f15fcf0879363cb3c0'
```

### 2.3 启动容器

先创建只允许当前用户读取的环境文件：

```bash
umask 077
printf 'NEXUSTIER_GATEWAY_ADMISSION_TOKEN=' >gateway.env
openssl rand -hex 32 >>gateway.env
printf 'NEXUSTIER_GATEWAY_API_ADDR=0.0.0.0:11211\n' >>gateway.env
```

```bash
docker run -d \
  --name nexustier-gateway \
  --restart unless-stopped \
  --init \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --env-file ./gateway.env \
  -p 22020:22020/udp \
  -p 127.0.0.1:11211:11211/tcp \
  ghcr.io/wuyouowo/nexustier@sha256:35ac975021ee951e1b9befaabb71d29adf566ba500b064f15fcf0879363cb3c0
```

镜像使用 UID `10001` 的非 root 用户，不创建 TUN 设备，也不需要 `NET_ADMIN` 或其他额外 capabilities。
保存生成的 Token，用于 EasyTier 客户端接入；不要提交 `gateway.env`。

### 2.4 检查服务

```bash
docker ps --filter name=nexustier-gateway
docker logs --tail 50 nexustier-gateway
curl --fail --silent --show-error \
  http://127.0.0.1:11211/healthz
```

没有客户端时，健康检查的正常响应为：

```json
{"status":"ok","active_sessions":0}
```

此时 `/readyz` 返回 `503 Service Unavailable` 是正常现象，表示还没有完成心跳注册的 EasyTier 客户端。

## 3. 接入 EasyTier 客户端

### 3.1 配置服务器地址

EasyTier v2.6.4 使用以下格式连接网关：

```text
udp://<网关域名或 IP>:22020/<非空 Token>
```

例如：

```text
udp://gateway.example.com:22020/<环境文件中的共享Token>
```

Token 随心跳进入网关，但不会出现在只读 API 响应中。设置 `NEXUSTIER_GATEWAY_ADMISSION_TOKEN` 后会拒绝其他 Token；这只是共享启动凭据，不应视为完整的用户授权机制。

### 3.2 使用 CLI 启动客户端

仅注册客户端会话：

```bash
read -rsp 'Gateway admission token: ' GATEWAY_TOKEN && echo
export ET_CONFIG_SERVER="udp://gateway.example.com:22020/${GATEWAY_TOKEN}"
export ET_MACHINE_ID='11111111-2222-3333-4444-555555555555'
unset GATEWAY_TOKEN
easytier-core
```

也可以使用环境变量：

```bash
test -n "${ET_CONFIG_SERVER:-}" && test -n "${ET_MACHINE_ID:-}"
easytier-core
```

Machine ID 应在同一设备上保持稳定。更换 Machine ID 会被网关识别为另一台机器；相同 Machine ID 的新连接会替换旧连接。

### 3.3 上报完整拓扑

当前网关不会下发网络实例配置。若要让 `/v1/topology` 返回 Node、Peer、Route 和 Stats，客户端必须同时加载已有 EasyTier 配置：

```bash
read -rsp 'Gateway admission token: ' GATEWAY_TOKEN && echo
export ET_CONFIG_SERVER="udp://gateway.example.com:22020/${GATEWAY_TOKEN}"
export ET_MACHINE_ID='11111111-2222-3333-4444-555555555555'
unset GATEWAY_TOKEN
easytier-core --config-file /etc/easytier/node.toml
```

EasyTier 数据面仍按 `/etc/easytier/node.toml` 运行。节点之间的加密流量、NAT 穿透和 Mesh 路由不经过 NexusTier Gateway。

## 4. 验证客户端注册

客户端完成连接后检查网关日志：

```bash
docker logs --tail 100 nexustier-gateway
```

正常安全会话应包含类似日志：

```text
EasyTier session registered ... secure=true
```

随后检查就绪状态：

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:11211/readyz
```

正常响应示例：

```json
{"status":"ready","active_sessions":1}
```

查看在线机器：

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:11211/v1/sessions | jq
```

验收时确认：

- `machine_id` 与客户端配置一致。
- `hostname` 和 `easytier_version` 正确。
- `last_heartbeat_at_ms` 持续更新。
- 有本地网络实例时，`running_instance_ids` 非空。
- 响应中不包含配置服务器 Token。

## 5. 查询拓扑与指标

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:11211/v1/topology | jq
```

响应按以下层级组织：

```text
schema_version
collection_id
started_at_ms / completed_at_ms
machines[]
  observed_at_ms
  session
  instances[]
    observed_at_ms
    node
    peers[]
    metrics[]
    errors[]
  errors[]
```

常用字段：

| 字段 | 含义 |
| --- | --- |
| `schema_version` | 当前跨语言契约版本 `nexustier.topology.v1` |
| `collection_id` | 本次实际采集 UUID；缓存结果复用原 ID |
| `started_at_ms` / `completed_at_ms` | 整次采集时间边界 |
| `observed_at_ms` | Machine 或 Instance 的本轮观测时间 |
| `session.machine_id` | EasyTier 客户端机器标识 |
| `instances[].node.ipv4` | 当前网络实例的虚拟 IPv4 地址 |
| `peers[].direct` | 是否为一跳直连 |
| `peers[].latency_ms` | 直连统计或路由路径延迟 |
| `peers[].loss_rate` | 丢包率，`0.01` 表示约 1% |
| `peers[].rx_bytes` / `tx_bytes` | Peer 连接累计流量 |
| `peers[].tunnel_protocols` | 当前连接使用的隧道协议 |
| `metrics[]` | EasyTier Stats 指标 |
| `errors[].code` | 稳定的机器错误类型；消息仅用于诊断 |

遥测采用部分成功语义。某个实例或 RPC 超时会记录在相应 `errors` 中，不会导致其他机器和实例的数据全部丢失。

## 6. 日常操作

### 6.1 查看状态和日志

```bash
docker inspect \
  --format '{{json .State.Health}}' \
  nexustier-gateway | jq

docker logs --follow nexustier-gateway
```

临时提高日志级别时，重建容器并增加：

```bash
-e RUST_LOG=debug
```

### 6.2 调整 RPC 超时

跨地域或高延迟客户端可以增加独立遥测 RPC 超时：

```bash
docker run ... \
  -e NEXUSTIER_GATEWAY_RPC_TIMEOUT_MS=10000 \
  ghcr.io/wuyouowo/nexustier:sha-302df2f
```

默认值为 `5000` 毫秒。该超时分别应用到每次反向 RPC，不是整份拓扑请求的总超时。

当前镜像还提供三项整轮采集保护：

```bash
-e NEXUSTIER_GATEWAY_COLLECTION_TIMEOUT_MS=15000 \
-e NEXUSTIER_GATEWAY_MACHINE_CONCURRENCY=8 \
-e NEXUSTIER_GATEWAY_SNAPSHOT_TTL_MS=1000
```

并发请求共享一轮采集，TTL 内复用相同 `collection_id`。总期限超时时，响应保留已完成机器，并在顶层 `errors` 返回 `collection_timeout`。

### 6.3 停止和启动

```bash
docker stop --time 15 nexustier-gateway
docker start nexustier-gateway
```

镜像声明 `SIGINT` 为停止信号。正常停止时网关会通知会话任务退出，并在最多 10 秒内完成优雅关闭。

重启后内存 Session Pool 为空，等待 EasyTier 客户端自动重连期间 `/readyz` 短暂返回 `503` 属于正常恢复过程。

## 7. 升级与回滚

升级前记录当前镜像：

```bash
docker inspect \
  --format '{{.Config.Image}}' \
  nexustier-gateway
```

升级流程：

```bash
docker pull ghcr.io/wuyouowo/nexustier:<新标签>
docker stop --time 15 nexustier-gateway
docker rm nexustier-gateway
```

然后使用第 2.3 节相同参数创建新容器，仅替换镜像标签。升级后依次检查：

```bash
curl --fail http://127.0.0.1:11211/healthz
curl --fail http://127.0.0.1:11211/readyz
curl --fail http://127.0.0.1:11211/v1/sessions
```

回滚时删除新容器，并用先前记录的固定标签或 digest 按相同参数重新创建。网关当前不持久化状态，因此升级和回滚不涉及数据库迁移；客户端会重新建立会话。

## 8. 常见问题

| 现象 | 原因与处理 |
| --- | --- |
| 容器健康但 `/readyz` 返回 `503` | 没有已注册客户端；检查 EasyTier 配置服务器地址和网关日志 |
| 客户端无法连接 | 确认使用 `udp://`、开放 UDP `22020`，并检查 DNS、NAT 和云安全组 |
| `/v1/sessions` 有机器但拓扑实例为空 | 客户端未加载本地 EasyTier 网络配置；当前网关不会下发实例 |
| `instances[].errors` 非空 | 查看错误中的操作和消息，检查客户端实例状态及 RPC 延迟 |
| 宿主机无法访问 API | 确认映射为 `127.0.0.1:11211:11211/tcp`，容器内默认绑定 `0.0.0.0:11211` |
| 远程运维无法访问 API | 不要开放公网端口；使用 SSH 本地转发或可信私有网络 |
| 相同机器反复上下线 | 确认 Machine ID 稳定且没有两个客户端同时使用同一 ID |

## 9. 安全边界

- 对公网只开放 UDP `22020`。
- HTTP API 无认证，必须限制在回环地址或可信私有网络。
- 当前源码只允许安全会话注册，并可校验共享配置服务器 Token；共享 Token 不是完整授权机制。
- 网关不读取或转发 EasyTier 业务数据包。
- 不要为容器添加 TUN、`NET_ADMIN`、host network 或特权模式。
- 生产部署优先使用固定标签或 digest，并验证发布镜像的 Cosign 签名。

当前版本的完整生产检查清单、nftables/UFW 示例、systemd 服务和远程 API 运维方式见[生产部署指南](deployment-guide.zh-CN.md)。