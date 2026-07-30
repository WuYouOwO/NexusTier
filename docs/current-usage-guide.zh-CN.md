# NexusTier 当前版本用户教程

## 1. 这份教程适合谁

这份教程面向已经按[当前版本端到端部署指南](current-deployment-guide.zh-CN.md)
启动 Gateway、Controller 和 PostgreSQL，希望接入 EasyTier 节点并确认遥测工作的用户。

当前版本是可用的遥测底座，不是完整图形化 SD-WAN 产品。你可以：

- 让 EasyTier v2.6.4 客户端注册到 NexusTier Gateway。
- 查看在线机器、网络实例、Peer、路由、延迟、丢包、流量和 Stats。
- 让 Controller 自动把拓扑和指标写入 PostgreSQL。
- 查看最近一次摄取是否成功、失败原因和数据库中的当前状态。

当前还不能：

- 在 Web 页面查看拓扑。
- 通过 NexusTier 创建 EasyTier 网络、分配 IP 或下发 ACL。
- 使用 OIDC/RBAC 登录。
- 通过 NexusTier 使用 SSH/RDP。

## 2. 开始前确认

在 NexusTier 主机上执行：

```bash
curl --fail --silent --show-error http://127.0.0.1:11211/healthz | jq
curl --fail --silent --show-error http://127.0.0.1:8080/healthz | jq
curl --fail --silent --show-error http://127.0.0.1:8080/readyz | jq
```

预期：

- Gateway `/healthz` 返回 `status=ok`。
- Controller `/healthz` 返回 `status=ok`。
- Controller `/readyz` 返回 `status=ready`，表示 PostgreSQL 可用。

Gateway `/readyz` 在还没有 EasyTier 客户端时返回 `503` 是正常状态。

你还需要准备：

- Gateway 可从客户端访问的 IP 或域名。
- Gateway 环境文件中的共享准入 Token。
- 每台客户端一个持久、唯一的 UUID Machine ID。
- 如果要看完整拓扑，客户端还要有可运行的 EasyTier 网络配置。

生成 Machine ID：

```bash
uuidgen
```

如果系统没有 `uuidgen`：

```bash
python3 -c 'import uuid; print(uuid.uuid4())'
```

## 3. 接入第一台 EasyTier 客户端

### 3.1 只注册机器

在客户端交互式读取 Token，避免真实值进入 shell history：

```bash
read -rsp 'Gateway admission token: ' GATEWAY_TOKEN && echo
export ET_CONFIG_SERVER="udp://gateway.example.com:22020/${GATEWAY_TOKEN}"
export ET_MACHINE_ID='11111111-2222-3333-4444-555555555555'
unset GATEWAY_TOKEN
easytier-core
```

替换：

- `gateway.example.com`：Gateway 的实际可达地址。
- 交互输入：Gateway `NEXUSTIER_GATEWAY_ADMISSION_TOKEN` 的值。
- Machine ID：为这台设备生成并永久保存的 UUID。

无人值守服务应把 `ET_CONFIG_SERVER` 和 `ET_MACHINE_ID` 放入仅服务用户可读的
`0600/0640` 环境文件，而不是写入命令行。客户端退出后清理当前 shell：

```bash
unset ET_CONFIG_SERVER ET_MACHINE_ID
```

Machine ID 是 NexusTier 当前识别设备的主键：

- 同一设备重装或容器重建后应继续使用原 ID。
- 两台设备不能共用同一个 ID。
- 同一 ID 的新连接会替换旧连接。
- 已建立连接不能通过后续心跳切换 Machine ID。

### 3.2 确认安全注册

在 Gateway 主机检查：

```bash
docker logs --tail 100 nexustier-gateway
```

正常日志包含：

```text
EasyTier session registered ... secure=true
```

客户端首先建立一次明文能力探测连接，然后使用 Noise + AES-GCM 重连。能力探测
不会进入在线 Session；只有安全连接、正确 Token 和合法 Machine ID 才能注册。

查看 Gateway 就绪状态：

```bash
curl --fail --silent --show-error http://127.0.0.1:11211/readyz | jq
```

预期：

```json
{
  "status": "ready",
  "active_sessions": 1
}
```

## 4. 查看在线机器

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:11211/v1/sessions | jq
```

重点字段：

| 字段 | 含义 |
| --- | --- |
| `machine_id` | 客户端稳定 UUID |
| `remote_url` | Gateway 观察到的 UDP 地址，可能因 NAT 变化 |
| `hostname` | 客户端主机名 |
| `easytier_version` | 客户端版本 |
| `last_heartbeat_at_ms` | Gateway 最近收到心跳的时间 |
| `running_instance_ids` | 客户端当前运行的 EasyTier 网络实例 |
| `device` | 操作系统信息 |

API 不会返回共享 Token。不要把 `remote_url` 当作设备身份，NAT 重绑定时它可能改变。

如果 `running_instance_ids` 为空，机器已经注册，但没有运行本地 EasyTier 网络实例。

## 5. 上报完整网络拓扑

在客户端同时加载 EasyTier 配置：

```bash
test -n "${ET_CONFIG_SERVER:-}" && test -n "${ET_MACHINE_ID:-}"
easytier-core --config-file /etc/easytier/node.toml
```

如果换了新 shell，按第 3 节再次交互读取 Token 并设置两个 EasyTier 环境变量。

NexusTier 当前不会替你创建这个配置。EasyTier 仍负责实际虚拟网卡、隧道、打洞、
加密和 Mesh 路由；NexusTier 只读取控制面状态。

接入第二台或更多节点时：

1. 为每台设备生成不同 Machine ID。
2. 使用相同 Gateway 地址和共享 Token。
3. 加载属于各自节点的 EasyTier 网络配置。
4. 确认节点本身已经能通过 EasyTier 互联。

## 6. 查看实时拓扑

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:11211/v1/topology | jq
```

响应层级：

```text
schema_version
collection_id
started_at_ms / completed_at_ms
machines[]
  session
  observed_at_ms
  instances[]
    node
    peers[]
    metrics[]
    errors[]
  errors[]
errors[]
```

### 6.1 快速列出机器和实例

```bash
curl --fail --silent --show-error http://127.0.0.1:11211/v1/topology |
  jq '.machines[] | {
    machine_id: .session.machine_id,
    hostname: .session.hostname,
    instances: [.instances[].instance_id],
    errors
  }'
```

### 6.2 快速列出 Peer 链路

```bash
curl --fail --silent --show-error http://127.0.0.1:11211/v1/topology |
  jq '.machines[].instances[] as $instance |
    $instance.peers[] | {
      instance_id: $instance.instance_id,
      peer_id,
      hostname,
      direct,
      next_hop_peer_id,
      latency_ms,
      loss_rate,
      rx_bytes,
      tx_bytes,
      tunnel_protocols
    }'
```

字段解释：

- `direct=true`：一跳直连；RTT 来自连接统计。
- `direct=false`：经过中继；RTT 使用路由路径延迟。
- `path_cost=1`：通常表示直连。
- `next_hop_peer_id`：到目标 Peer 的下一跳。
- `loss_rate=0.01`：约 1% 丢包。
- `rx_bytes` / `tx_bytes`：当前 Peer 连接累计字节数。
- `tunnel_protocols`：当前连接使用的隧道协议。

### 6.3 为什么两次查询的 collection ID 一样

Gateway 对拓扑采集实现了单飞和短期缓存：

- 同时到达的请求共享一轮反向 RPC。
- 默认 1 秒 TTL 内复用同一快照和 `collection_id`。
- 一轮采集默认总期限为 15 秒。
- 默认最多同时采集 8 台机器。

这不是数据写入失败，而是避免重复压迫客户端的正常行为。

## 7. 查看 Controller 摄取状态

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/v1/telemetry/status | jq
```

示例：

```json
{
  "running": false,
  "last_attempt_at": "2026-07-30T06:30:00Z",
  "last_success_at": "2026-07-30T06:30:00Z",
  "last_collection_id": "11111111-1111-4111-8111-111111111111",
  "last_collection_state": "ingested",
  "consecutive_failures": 0
}
```

字段解释：

| 字段 | 含义 |
| --- | --- |
| `running` | 当前是否正在执行一轮 fetch + ingest |
| `last_attempt_at` | 最近尝试时间 |
| `last_success_at` | 最近成功时间 |
| `last_collection_id` | 最近处理的 Gateway collection |
| `last_collection_state` | `ingested` 或 `duplicate` |
| `last_error` | 最近失败原因，成功后清空 |
| `consecutive_failures` | 连续失败轮数，成功后归零 |

`duplicate` 表示相同 `collection_id` 已存在，Controller 正确跳过重复写入；它不是错误。

## 8. 查看持久化数据

Controller 当前没有面向用户的拓扑查询 API，因此使用只读 SQL 查看 PostgreSQL。

进入数据库：

```bash
read -rsp 'PostgreSQL password: ' PGPASSWORD && echo
export PGPASSWORD PGSSLMODE=disable
psql -h 127.0.0.1 -U nexustier -d nexustier
```

### 8.1 最近采集

```sql
SELECT collection_id, status, machine_count, error_count, collected_at
FROM telemetry_collection_runs
ORDER BY collected_at DESC
LIMIT 20;
```

`status=partial` 表示本轮存在局部错误，成功数据仍然已经保存。

### 8.2 当前机器

```sql
SELECT machine_id, hostname, active, last_heartbeat_at,
       last_observed_at, disappeared_at
FROM machines
ORDER BY hostname, machine_id;
```

完整快照中消失的机器不会被物理删除，而是标记 `active=false` 并记录
`disappeared_at`。

### 8.3 当前实例和节点

```sql
SELECT i.instance_id, i.machine_id, i.active, i.last_observed_at,
       n.peer_id, n.ipv4, n.hostname
FROM network_instances AS i
LEFT JOIN nodes AS n ON n.instance_id = i.instance_id
ORDER BY i.machine_id, i.instance_id;
```

### 8.4 当前 Peer 链路

```sql
SELECT p.source_instance_id, p.destination_peer_id, p.hostname, p.direct,
       p.next_hop_peer_id, p.path_cost, p.latency_ms, p.loss_rate,
       p.rx_bytes, p.tx_bytes, p.tunnel_protocols, p.last_observed_at
FROM peer_links_current AS p
JOIN network_instances AS i ON i.instance_id = p.source_instance_id
JOIN machines AS m ON m.machine_id = i.machine_id
WHERE i.active AND m.active
ORDER BY p.source_instance_id, p.destination_peer_id;
```

### 8.5 局部采集错误

```sql
SELECT e.collection_id, r.collected_at, e.scope, e.machine_id, e.instance_id,
       e.code, e.operation, e.message
FROM telemetry_collection_errors AS e
JOIN telemetry_collection_runs AS r ON r.collection_id = e.collection_id
ORDER BY r.collected_at DESC, e.error_index;
```

退出 `psql` 后执行 `unset PGPASSWORD PGSSLMODE`。

## 9. 如何理解局部错误

拓扑通常采用“部分成功”语义。能够归属到合法 Machine/Instance 的 RPC 错误不会让
整个 HTTP 请求或数据库事务丢掉其他机器和实例的成功数据。当前
`session_unavailable` 分支是例外：Gateway 会使用 nil Machine ID 占位，严格 Controller
会拒绝整份 topology v1，并把它记录为 fetch/validate 失败，而不是保存 partial collection。

常见 `code`：

| code | 含义 |
| --- | --- |
| `session_unavailable` | Session 快照不可用；当前严格 Controller 会拒绝包含 nil Machine ID 的整份契约 |
| `rpc_error` | EasyTier 反向 RPC 返回错误 |
| `rpc_timeout` | 单个 RPC 超过期限 |
| `task_failed` | 机器采集任务异常退出 |
| `collection_timeout` | 整轮采集达到总期限 |

常见 `operation`：

- `list_network_instances`
- `show_node_info`
- `list_peers`
- `list_routes`
- `get_stats`

Controller 不会因为没有观测到某个字段就立即删除旧状态：

- `list_routes` 失败时保留当前 Peer。
- `list_peers` 失败时保留已有直连 RTT、丢包、流量和隧道协议。
- `show_node_info` 失败时保留上次 Node。
- 顶层 collection 失败时不把缺失机器标记离线。
- 只有完整枚举成功，消失实体才会变为 inactive。

## 10. 日常操作

### 10.1 查看服务

```bash
docker ps --filter name=nexustier-gateway
docker logs --tail 100 nexustier-gateway
systemctl status nexustier-controller.service --no-pager
journalctl -u nexustier-controller.service --since '30 minutes ago' --no-pager
```

### 10.2 重启服务

```bash
docker restart nexustier-gateway
sudo systemctl restart nexustier-controller.service
```

Gateway 重启后内存 Session 暂时为空，EasyTier 客户端会自动重连。期间 Gateway
`/readyz=503` 属于正常恢复过程。Controller 会继续轮询，并在 Gateway 恢复后重新成功。

### 10.3 调整轮询间隔

编辑 `/etc/nexustier/controller.env`：

```dotenv
NEXUSTIER_CONTROLLER_POLL_INTERVAL=30s
NEXUSTIER_CONTROLLER_POLL_JITTER=5s
```

抖动必须大于等于 0 且小于轮询间隔。修改后：

```bash
sudo systemctl restart nexustier-controller.service
```

### 10.4 临时提高 Gateway 日志级别

把 Gateway 环境文件中的 `RUST_LOG` 改为 `debug` 并重建容器。排障后恢复 `info`，
避免长期产生大量底层日志。

## 11. 常见问题

| 现象 | 检查与处理 |
| --- | --- |
| Gateway 健康但 `/readyz=503` | 没有安全注册的 EasyTier 客户端 |
| 客户端无法注册 | 检查 UDP 22020、DNS/NAT、Token、EasyTier v2.6.4 和 Machine ID |
| 日志只有能力探测 | 客户端没有完成安全重连，通常是版本、网络或 Token 问题 |
| Session 在线但实例为空 | 客户端没有加载本地 EasyTier 网络配置 |
| 拓扑没有 Peer | 先确认 EasyTier 节点自身已经组网并能互联 |
| 拓扑有 `errors` | 查看 `code`、`operation`、`message` 和对应客户端状态 |
| Controller `/readyz=503` | PostgreSQL 不可达或数据库配置错误 |
| Controller 没有成功 collection | 查看 `last_error` 和 systemd 日志 |
| 状态长期为 `duplicate` | 轮询快于 Gateway TTL；不会重复写数据库，可适当增大间隔 |
| 历史数据持续增长 | 当前没有 metric retention，必须监测数据库容量 |
| 想远程查看 API | 使用 SSH 端口转发，不要开放公网 11211/8080 |

## 12. 安全提醒

- 只向 EasyTier 客户端开放 `22020/UDP`。
- Gateway API、Controller API 和 PostgreSQL 都保持回环或可信私网访问。
- 共享 Token 不是完整零信任身份系统，应视为敏感启动凭据。
- 数据库密码不要写入仓库、聊天记录、命令输出或不受保护的文件。
- 不要给 Gateway 容器添加 TUN、`NET_ADMIN`、host network 或特权模式。
- 生产使用固定镜像 digest，并验证 Cosign 签名。
- 定期备份 PostgreSQL；当前没有自动指标保留策略。

## 13. 下一步阅读

- [当前版本端到端部署指南](current-deployment-guide.zh-CN.md)
- [Rust Gateway API 手册](gateway-guide.zh-CN.md)
- [Controller 运行说明](../controller/README.md)
- [topology v1 契约](../contracts/README.md)
- [Gateway 源码架构](gateway-code.zh-CN.md)
- [Controller 源码架构](controller-code.zh-CN.md)
