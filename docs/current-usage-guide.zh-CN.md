# NexusTier 当前版本用户教程

## 1. 当前版本能做什么

本文面向已经按[Docker Compose 部署指南](current-deployment-guide.zh-CN.md)启动
Gateway、Controller 和 PostgreSQL 的用户。当前版本可以：

- 让未修改的 EasyTier v2.6.4 客户端安全注册到 Gateway。
- 查看 Gateway 当前在线 Session 和实时反向 RPC 拓扑。
- 在 Controller 内嵌控制台查看持久化 Machine、Instance、Node 和 Peer。
- 通过 Controller API 按活动状态、Machine UUID 和 UUID cursor 查询当前拓扑。
- 查看最新 collection 新鲜度、结构化错误、摄取状态和指标保留状态。
- 自动删除过期指标样本并裁剪过期 collection 原始 payload。

控制台和 `/v1` API 由单运维账号的会话守卫保护。当前仍不提供多租户、OIDC/RBAC、
IPAM、ACL、网络配置下发、Redis、多副本 HA、历史指标图表或 PostgreSQL 分区。

单账号会话是最小可用边界，不是完整的身份体系。即使已启用认证，仍建议按第 10 节
用 SSH 转发访问，不要把 `8080` 直接开放到互联网。

## 2. 开始前确认

在 NexusTier 主机的部署目录执行：

```bash
docker compose --env-file .env -f compose.example.yaml ps
curl --fail --silent --show-error http://127.0.0.1:11211/healthz | jq
curl --fail --silent --show-error http://127.0.0.1:8080/healthz | jq
curl --fail --silent --show-error http://127.0.0.1:8080/readyz | jq
```

预期：

- PostgreSQL、Gateway 和 Controller 都是 `Up`/healthy。
- Gateway `/healthz` 返回 `status=ok`。
- Controller `/healthz` 返回 `status=ok`。
- Controller `/readyz` 返回 `status=ready`，表示 PostgreSQL 可访问。

没有 EasyTier 客户端时，Gateway `/readyz` 返回 `503` 是正常状态，不表示容器不健康。

你还需要：

- Gateway 可从客户端访问的 IP 或域名。
- `.env` 中的 `NEXUSTIER_GATEWAY_ADMISSION_TOKEN`。
- 每台客户端一个长期稳定且唯一的 UUID Machine ID。
- 若要看完整 Node/Peer/Route/Stats，客户端需要自己的 EasyTier 网络配置。

生成 Machine ID：

```bash
uuidgen
```

## 3. 接入 EasyTier 客户端

### 3.1 启动客户端

在客户端交互读取 Token，避免真实值进入 shell history：

```bash
read -rsp 'Gateway admission token: ' GATEWAY_TOKEN && echo
export ET_CONFIG_SERVER="udp://gateway.example.com:22020/${GATEWAY_TOKEN}"
export ET_MACHINE_ID='11111111-2222-3333-4444-555555555555'
unset GATEWAY_TOKEN
easytier-core --config-file /etc/easytier/node.toml
```

替换 Gateway 地址和 Machine ID。同一设备重装或容器重建后应继续使用原 Machine ID；
两台设备不能共用同一个 ID。无人值守服务应把 `ET_CONFIG_SERVER` 与
`ET_MACHINE_ID` 放入权限为 `0600/0640` 的环境文件。

NexusTier 当前不会创建 EasyTier 网络实例。仅指定配置服务器可以注册 Session，但
若没有加载本地 EasyTier 配置，拓扑中的 `network_instances` 会为空。

### 3.2 确认安全注册

```bash
docker compose --env-file .env -f compose.example.yaml logs --tail 100 gateway
curl --fail --silent --show-error http://127.0.0.1:11211/readyz | jq
```

日志应包含 `secure=true` 的 Session 注册记录。客户端先建立明文能力探测，再以
Noise + AES-GCM 重连；探测连接不会加入 Session Pool。

## 4. 查看 Gateway 实时状态

Gateway API 读取内存 Session，并在查询实时拓扑时触发反向 EasyTier RPC。

### 4.1 在线 Session

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:11211/v1/sessions | jq
```

重点字段：

| 字段 | 含义 |
| --- | --- |
| `machine_id` | 客户端稳定身份 UUID |
| `remote_url` | Gateway 观察到的 UDP 地址，不是设备身份 |
| `hostname` | 客户端主机名 |
| `easytier_version` | 客户端版本 |
| `last_heartbeat_at_ms` | 最近心跳时间 |
| `running_instance_ids` | 当前运行的 EasyTier 网络实例 |

响应不会包含共享准入 Token。

### 4.2 实时拓扑

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:11211/v1/topology | jq
```

Gateway 拓扑包含 `collection_id`、采集时间、`machines[]`、`instances[]`、Node、Peer、
Metric 和分层 `errors[]`。同一秒内的并发请求会共享单飞采集和短期缓存，因此可能返回
相同 `collection_id`。

快速列出 Peer：

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

`direct=true` 表示一跳直连；中继 Peer 的延迟来自路由路径。`loss_rate=0.01` 表示约
1% 丢包。

## 5. 使用内嵌拓扑控制台

在 NexusTier 主机浏览器打开：

```text
http://127.0.0.1:8080/
```

控制台提供：

- Machine、Instance、Peer 数量和最新 collection 新鲜度。
- Machine 到 Peer 的直连/中继 SVG 连通图。
- Peer RTT、丢包、RX/TX、隧道协议和最近观测时间。
- 当前 Machine 清单、active 过滤、文本过滤和自动刷新。
- 最新 collection 的状态和结构化错误。

页面由 Controller 二进制内嵌提供，不依赖 CDN 或额外前端容器。页面和 JSON API 都设置
`Cache-Control: no-store`；页面还设置 CSP、禁止 frame 嵌入和 MIME 嗅探。

### 5.1 登录

除 `auth_mode=disabled` 外，首次访问会跳转到 `/login`。输入 `.env` 中
`NEXUSTIER_CONTROLLER_AUTH_USERNAME` 对应的用户名和生成哈希时使用的原始密码。
登录成功后浏览器保存一个 `HttpOnly`、`SameSite=Strict` 的会话 Cookie，默认有效期
`12h`，点击右上角「退出」立即失效。

未配置 `NEXUSTIER_CONTROLLER_SESSION_KEY` 时，签名密钥在每次启动随机生成，
Controller 重启会使所有会话失效，需要重新登录。

连续登录失败会按来源 IP 限流：初始额度 8 次，之后每 30 秒恢复 1 次，超出返回
`429` 并带 `Retry-After`。日志只记录来源地址，不记录提交的用户名和密码。

未携带有效会话时，浏览器导航跳转 `/login`，API 请求返回 `401` 与
`{"error":{"code":"unauthenticated"}}`。

## 6. 查询 Controller 持久化拓扑

Controller `/v1/topology` 从 PostgreSQL 规范化当前状态读取，不会触发 Gateway RPC：

```bash
curl --fail --silent --show-error \
  'http://127.0.0.1:8080/v1/topology?active=true&limit=100' | jq
```

响应结构：

```text
generated_at
latest_collection
latest_errors[]
machines[]
  device
  network_instances[]
    node
    peers[]
page.limit
page.next_cursor
```

查询参数：

| 参数 | 语义 |
| --- | --- |
| `active=true|false` | 同时过滤 Machine 和其 Instance 活动状态 |
| `machine_id=<uuid>` | 精确读取一台 Machine |
| `cursor=<uuid>` | 从该 Machine UUID 之后继续读取 |
| `limit=1..500` | 每页 Machine 数，默认 `100` |

读取指定 Machine：

```bash
curl --fail --silent --show-error \
  'http://127.0.0.1:8080/v1/topology?machine_id=11111111-2222-3333-4444-555555555555' | jq
```

返回 `page.next_cursor` 时读取下一页：

```bash
curl --fail --silent --show-error \
  'http://127.0.0.1:8080/v1/topology?active=true&limit=100&cursor=<next_cursor>' | jq
```

Gateway 和 Controller 都有 `/v1/topology`，但语义不同：

| 地址 | 数据源 | 用途 |
| --- | --- | --- |
| `127.0.0.1:11211/v1/topology` | Gateway 内存 Session + 实时反向 RPC | 诊断客户端当前实时状态 |
| `127.0.0.1:8080/v1/topology` | PostgreSQL 规范化当前表 | 控制台、稳定分页和持久化视图 |

## 7. 查看摄取与保留状态

### 7.1 摄取状态

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/v1/telemetry/status | jq
```

| 字段 | 含义 |
| --- | --- |
| `running` | 当前是否正在执行 fetch + ingest |
| `last_attempt_at` | 最近尝试时间 |
| `last_success_at` | 最近成功时间 |
| `last_collection_id` | 最近处理的 Gateway collection |
| `last_collection_state` | `ingested` 或 `duplicate` |
| `last_error` | 最近失败原因，成功后清空 |
| `consecutive_failures` | 连续失败轮数 |

`duplicate` 表示该 collection 已存在且 SHA-256 指纹一致，不是错误。

### 7.2 保留状态

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/v1/retention/status | jq
```

默认配置：

- 保留时间：`720h`（30 天）。
- 清理周期：`6h`。
- 每批最大行数：`10000`。

到期的 `metric_samples` 被分批删除，旧 collection 的 `raw_payload` 裁剪为 JSON
`null`。collection 边界、状态、Machine/Error 计数、结构化错误和 SHA-256 指纹继续
保留，重复 collection 冲突检测不会因 payload 裁剪失效。

`last_deleted_samples` 与 `last_pruned_payloads` 是最近一轮数量；`total_*` 是当前
Controller 进程启动后的累计数量。`last_error` 非空时检查数据库、磁盘、锁等待和日志。

## 8. 使用 SQL 深度排障

日常查看优先使用控制台和 Controller API。PostgreSQL 不发布宿主机端口，通过容器内
`psql` 查询：

```bash
docker compose --env-file .env -f compose.example.yaml exec postgres \
  psql -U nexustier -d nexustier
```

最近 collection：

```sql
SELECT collection_id, status, machine_count, error_count,
       completed_at, ingested_at, payload_pruned_at
FROM telemetry_collection_runs
ORDER BY completed_at DESC
LIMIT 20;
```

当前 Machine：

```sql
SELECT machine_id, hostname, active, last_heartbeat_at,
       last_observed_at, disappeared_at
FROM machines
ORDER BY machine_id;
```

当前 Peer：

```sql
SELECT source_instance_id, destination_peer_id, hostname, direct,
       next_hop_peer_id, latency_ms, loss_rate, rx_bytes, tx_bytes,
       tunnel_protocols, last_observed_at
FROM peer_links_current
ORDER BY source_instance_id, destination_peer_id;
```

最近结构化错误：

```sql
SELECT e.collection_id, r.completed_at, e.scope, e.machine_id,
       e.instance_id, e.code, e.operation, e.message
FROM telemetry_collection_errors AS e
JOIN telemetry_collection_runs AS r USING (collection_id)
ORDER BY r.completed_at DESC, e.error_index
LIMIT 100;
```

退出：

```text
\q
```

## 9. 理解部分成功语义

单个 Machine、Instance 或 RPC 失败不会丢掉其他成功数据。常见错误码：

| code | 含义 |
| --- | --- |
| `rpc_error` | EasyTier 反向 RPC 返回错误 |
| `rpc_timeout` | 单个 RPC 超过期限 |
| `task_failed` | Machine 采集任务异常退出 |
| `collection_timeout` | 整轮采集达到总期限，已完成 Machine 仍返回 |
| `session_unavailable` | Session 快照不可用；nil Machine ID 会被严格 Controller 拒绝 |

Controller 对局部失败采用保留语义：

- `list_network_instances` 失败时不把缺失 Instance 标记 inactive。
- `list_routes` 失败时不删除当前 Peer。
- `list_peers` 失败时保留已有直连 RTT、丢包、流量和协议。
- `show_node_info` 失败时保留上次 Node。
- 顶层 collection error 时不把缺失 Machine 标记 inactive。

只有完整枚举成功后，消失实体才变为 `active=false`。

## 10. 日常运维

### 10.1 查看服务和日志

```bash
docker compose --env-file .env -f compose.example.yaml ps
docker compose --env-file .env -f compose.example.yaml logs --tail 100 gateway
docker compose --env-file .env -f compose.example.yaml logs --tail 100 controller
docker compose --env-file .env -f compose.example.yaml logs --tail 100 postgres
```

### 10.2 重启服务

```bash
docker compose --env-file .env -f compose.example.yaml restart gateway
docker compose --env-file .env -f compose.example.yaml restart controller
```

Gateway 重启后内存 Session Pool 暂时为空，客户端会自动重连。期间 Gateway
`/readyz=503` 属于正常恢复过程，Controller 会在 Gateway 恢复后重新成功。

### 10.3 修改配置

编辑部署目录中的 `.env`。例如：

```dotenv
NEXUSTIER_CONTROLLER_POLL_INTERVAL=30s
NEXUSTIER_CONTROLLER_POLL_JITTER=5s
NEXUSTIER_CONTROLLER_METRIC_RETENTION=2160h
```

抖动必须小于轮询间隔。修改后重建 Controller；单纯 `restart` 不会应用新环境变量：

```bash
docker compose --env-file .env -f compose.example.yaml config --quiet
docker compose --env-file .env -f compose.example.yaml up -d controller
```

### 10.4 远程访问

使用 SSH 本地转发：

```bash
ssh -N \
  -L 11211:127.0.0.1:11211 \
  -L 8080:127.0.0.1:8080 \
  operator@gateway.example.com
```

然后在本地浏览器打开 `http://127.0.0.1:8080/`。不要为了远程查看把 `11211` 或
`8080` 直接绑定公网。

## 11. 常见问题

| 现象 | 检查与处理 |
| --- | --- |
| Gateway 健康但 `/readyz=503` | 没有安全注册的 EasyTier 客户端 |
| 日志只有能力探测 | 检查客户端版本、Token、UDP 22020 和 Noise 安全重连 |
| Session 在线但 Instance 为空 | 客户端没有加载本地 EasyTier 网络配置 |
| Gateway 拓扑没有 Peer | 先确认 EasyTier 节点自身已经组网 |
| Controller `/readyz=503` | PostgreSQL 不可达或数据库配置错误 |
| Controller 没有成功 collection | 查看 telemetry status 和 Controller 日志 |
| 控制台没有 Machine | 检查持久化 `/v1/topology`、active 过滤和摄取状态 |
| `/v1/topology=503` | PostgreSQL 查询失败；检查 `/readyz` 和 Controller 日志 |
| 状态长期为 `duplicate` | 轮询快于 Gateway snapshot TTL；不会重复写数据库 |
| retention status 有错误 | 检查数据库连接、磁盘容量、锁等待和批量大小 |
| 数据库持续增长 | 当前状态和 collection 元数据不自动删除；规划长期归档/分区 |

## 12. 安全边界

- 公网只开放 UDP `22020`。
- Gateway API、Controller API、控制台和 PostgreSQL 保持回环或可信私网访问。
- 共享 Token 是敏感启动凭据，不是设备级身份或完整零信任授权。
- 数据库密码、`.env` 和备份不得进入仓库、日志或聊天记录。
- Gateway 不需要 TUN、`NET_ADMIN`、host network 或特权模式。
- 生产使用固定镜像 digest 并验证 Cosign 签名。
- 自动指标保留不能替代 PostgreSQL 备份、容量监测和长期归档。

## 13. 下一步阅读

- [Docker Compose 部署指南](current-deployment-guide.zh-CN.md)
- [Controller 运行与边界说明](../controller/README.md)
- [Controller 源码架构解析](controller-code.zh-CN.md)
- [Gateway API 手册](gateway-guide.zh-CN.md)
- [内部 topology v1 契约](../contracts/README.md)