# NexusTier Controller 遥测摄取服务

`nexustier-controller` 是当前仓库中的 Go 控制面遥测摄取基础。它定时读取
Rust Gateway 的 `nexustier.topology.v1` 契约，在一个 PostgreSQL 事务中
更新当前拓扑、追加指标样本并记录采集错误。它还从规范化表提供当前拓扑 API、内嵌
只读控制台，并按配置清理过期指标和原始 payload。

该模块通过 GitHub Actions 构建并发布 GHCR 镜像。它提供内部只读拓扑查询，但不是
经过认证的公开控制面 API：不提供 Redis、WebSocket、IPAM、ACL 或配置下发。

完整安装 Gateway、Controller 和 PostgreSQL 请使用
[当前版本端到端部署指南](../docs/current-deployment-guide.zh-CN.md)；接入节点、
查看实时拓扑和持久化状态见[当前版本用户教程](../docs/current-usage-guide.zh-CN.md)。

当前已验证镜像为：

```text
ghcr.io/wuyouowo/nexustier-controller:sha-302df2f
sha256:1d1022885b54fadb83dd60346ef3ed11f35b38ce6a18301f7e684dbc27702362
```

## 能力

- 严格解码 topology v1，拒绝未知字段、未知版本、nil UUID 和越界时间戳。
- 使用 `collection_id` 保证重复轮询幂等；相同 ID 对应不同 payload 时拒绝写入。
- 规范化保存 Machine、Instance、Node、当前 Peer 链路、指标样本和局部错误。
- 使用观测时间与采集完成时间阻止乱序快照覆盖或删除较新状态。
- 只有完整枚举才把消失 Machine/Instance 标记为 inactive。
- 局部 RPC 失败保留上次成功字段，不把未观测误判为删除。
- 串行轮询带独立请求超时和抖动，不会重叠执行。
- PostgreSQL migrations 使用 advisory lock、事务和 SHA-256 checksum。
- 当前拓扑查询按 Machine UUID 稳定分页，支持 active、machine_id、cursor 和 limit 参数。
- 返回最新 collection 新鲜度、结构化错误，以及嵌套 Machine/Instance/Node/Peer 状态。
- 在 `/` 内嵌响应式拓扑控制台，无 CDN、Node.js 或额外前端容器依赖。
- 默认保留 720 小时指标和原始 payload；分批清理后仍保留 collection 元数据、错误与 SHA-256 指纹。
- 提供 JSON 结构化日志和 SIGINT/SIGTERM 优雅关闭。

## 要求

- Go `1.25` 或更新版本。
- PostgreSQL `14` 或更新版本；本地验证基线为 PostgreSQL `18.4`。
- 可从控制器访问 Rust Gateway 的内部 HTTP API。

## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `NEXUSTIER_CONTROLLER_DATABASE_URL` | 必填 | PostgreSQL 连接 URL |
| `NEXUSTIER_CONTROLLER_GATEWAY_URL` | `http://127.0.0.1:11211` | Rust Gateway 内部地址 |
| `NEXUSTIER_CONTROLLER_LISTEN_ADDR` | `127.0.0.1:8080` | 控制器状态 API 地址 |
| `NEXUSTIER_CONTROLLER_POLL_INTERVAL` | `15s` | 两轮采集之间的基础间隔 |
| `NEXUSTIER_CONTROLLER_POLL_JITTER` | `3s` | 正负随机抖动，必须小于间隔 |
| `NEXUSTIER_CONTROLLER_REQUEST_TIMEOUT` | `20s` | 单轮拉取和写入总超时 |
| `NEXUSTIER_CONTROLLER_SHUTDOWN_TIMEOUT` | `10s` | HTTP 和 worker 关闭期限 |
| `NEXUSTIER_CONTROLLER_METRIC_RETENTION` | `720h` | 指标样本和 collection 原始 payload 保留时间 |
| `NEXUSTIER_CONTROLLER_CLEANUP_INTERVAL` | `6h` | 保留清理周期 |
| `NEXUSTIER_CONTROLLER_CLEANUP_BATCH_SIZE` | `10000` | 每批删除或裁剪的最大行数 |

## 运行

推荐按[当前版本 Docker Compose 部署指南](../docs/current-deployment-guide.zh-CN.md)
同时启动 PostgreSQL、Gateway 和 Controller。Compose 内 Controller 使用
`postgres:5432` 和 `http://gateway:11211`，运维 API 只映射到宿主机回环地址。

源码开发时，先启动 PostgreSQL 和 Rust Gateway，然后在仓库根目录执行：

```bash
set -a
. "${HOME}/.config/nexustier/controller.env"
set +a
go -C controller run ./cmd/nexustier-controller
```

迁移在启动时自动执行。生产环境应使用独立数据库角色、TLS、备份和最小网络
访问策略，不要提交连接 URL 或密码。开发环境文件应放在仓库外并保持 `0600`；
至少设置数据库 URL 和 Gateway URL。

容器默认使用 UID `10001` 的非 root 用户，内部监听 `0.0.0.0:8080`，并通过
`/healthz` 执行 Docker 健康检查。镜像不包含数据库，启动时会连接外部 PostgreSQL
并自动执行嵌入二进制的 checksummed migrations。

## API

| Endpoint | 语义 |
| --- | --- |
| `GET /healthz` | 进程与 HTTP 服务存活，不检查数据库 |
| `GET /readyz` | PostgreSQL 在 1 秒内可响应时返回 `200` |
| `GET /v1/telemetry/status` | 最近尝试、成功时间、collection ID、状态和连续失败数 |
| `GET /v1/topology` | 持久化当前拓扑、最新 collection、结构化错误和 UUID cursor 分页 |
| `GET /v1/retention/status` | 保留周期、批量大小、最近/累计删除和裁剪状态 |
| `GET /` | 内嵌只读拓扑控制台 |

`GET /v1/topology` 查询参数：

- `active=true|false`：过滤 Machine 和其 Instance 活动状态；不传时返回全部。
- `machine_id=<uuid>`：只返回指定 Machine。
- `cursor=<uuid>`：从该 Machine UUID 之后继续读取。
- `limit=1..500`：每页 Machine 数，默认 `100`。

API 当前没有认证。原生运行默认绑定回环地址；容器内为了私有网络访问监听所有接口，
Compose 仅映射到宿主机 `127.0.0.1:8080`。不得直接暴露到互联网。

## 数据模型

| 表 | 用途 |
| --- | --- |
| `telemetry_collection_runs` | 每个 collection 的边界、状态、审计 payload、SHA-256 指纹和裁剪时间 |
| `machines` | 当前机器状态，含 active/disappeared 时间 |
| `network_instances` | 当前 EasyTier 实例状态 |
| `nodes` | 每个实例当前 Node 信息 |
| `peer_links_current` | 实例作用域的当前目标 Peer 路由与连接统计 |
| `metric_samples` | 按 collection 追加的指标样本 |
| `telemetry_collection_errors` | snapshot/machine/instance 层结构化错误 |
| `schema_migrations` | 已应用 migration 与 SHA-256 checksum |

raw payload 仅作为限时审计补充，规范化表是当前拓扑 API 的数据源。保留期后 raw
payload 会被裁剪为 JSON `null`，但 collection 元数据、结构化错误和 SHA-256 指纹保留。

## 验证

```bash
cd controller
go test ./...
go vet ./...
go test -race ./internal/gatewayclient ./internal/poller ./internal/api ./internal/retention
```

PostgreSQL 集成测试需要专用空数据库：

```bash
read -rsp 'URL-encoded test database password: ' TEST_DB_PASSWORD && echo
export NEXUSTIER_TEST_DATABASE_URL="postgres://nexustier:${TEST_DB_PASSWORD}@127.0.0.1:5432/nexustier_test?sslmode=disable"
go test ./internal/ingest ./internal/readmodel ./internal/retention -count=1
unset NEXUSTIER_TEST_DATABASE_URL TEST_DB_PASSWORD
```

测试会 `TRUNCATE telemetry_collection_runs CASCADE`，绝不能指向生产或包含
需要保留数据的数据库。CI 会自动创建专用测试数据库执行这些场景。

## 当前限制

- 没有历史指标时间序列查询/图表、聚合降采样或 PostgreSQL 分区策略。
- 当前拓扑 API 使用 UUID cursor，不提供任意排序、全文搜索或跨租户视图。
- 内嵌控制台是单用户运维视图，不包含登录、授权或租户隔离。
- 不创建或修改 EasyTier 网络实例，不执行 IPAM 或策略下发。
- 没有设备级准入、OIDC、RBAC 或 API 认证。
- 没有 Redis 发布、多控制器协调或高可用部署定义。
- Compose 示例适合单机部署和验收，当前没有多控制器 HA 编排定义。

源码结构与关键事务语义见 [控制器源码架构解析](../docs/controller-code.zh-CN.md)。
