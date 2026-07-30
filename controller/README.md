# NexusTier Controller 遥测摄取服务（WIP）

`nexustier-controller` 是当前仓库中的 Go 控制面遥测摄取基础。它定时读取
Rust Gateway 的 `nexustier.topology.v1` 契约，在一个 PostgreSQL 事务中
更新当前拓扑、追加指标样本并记录采集错误。

该模块尚未发布稳定镜像，也不提供公开认证 API、Redis、WebSocket、IPAM
或 ACL。当前 API 只用于进程、数据库和摄取状态检查。

完整安装 Gateway、Controller 和 PostgreSQL 请使用
[当前版本端到端部署指南](../docs/current-deployment-guide.zh-CN.md)；接入节点、
查看实时拓扑和持久化状态见[当前版本用户教程](../docs/current-usage-guide.zh-CN.md)。

当前已验证源码提交为 `654c70fbb70289c5313f7685abff72a59b3c9f7b`。

## 能力

- 严格解码 topology v1，拒绝未知字段、未知版本、nil UUID 和越界时间戳。
- 使用 `collection_id` 保证重复轮询幂等；相同 ID 对应不同 payload 时拒绝写入。
- 规范化保存 Machine、Instance、Node、当前 Peer 链路、指标样本和局部错误。
- 使用观测时间与采集完成时间阻止乱序快照覆盖或删除较新状态。
- 只有完整枚举才把消失 Machine/Instance 标记为 inactive。
- 局部 RPC 失败保留上次成功字段，不把未观测误判为删除。
- 串行轮询带独立请求超时和抖动，不会重叠执行。
- PostgreSQL migrations 使用 advisory lock、事务和 SHA-256 checksum。
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

## 运行

先启动 PostgreSQL 和 Rust Gateway，然后在仓库根目录执行：

```bash
set -a
. "${HOME}/.config/nexustier/controller.env"
set +a
go -C controller run ./cmd/nexustier-controller
```

迁移在启动时自动执行。生产环境应使用独立数据库角色、TLS、备份和最小网络
访问策略，不要提交连接 URL 或密码。开发环境文件应放在仓库外并保持 `0600`；
至少设置数据库 URL 和 Gateway URL。

Controller 当前没有容器镜像。生产样例使用固定源码提交构建二进制并通过 systemd
运行，具体命令和加固单元见端到端部署指南。

## API

| Endpoint | 语义 |
| --- | --- |
| `GET /healthz` | 进程与 HTTP 服务存活，不检查数据库 |
| `GET /readyz` | PostgreSQL 在 1 秒内可响应时返回 `200` |
| `GET /v1/telemetry/status` | 最近尝试、成功时间、collection ID、状态和连续失败数 |

API 当前没有认证，默认只绑定回环地址。不得直接暴露到互联网。

## 数据模型

| 表 | 用途 |
| --- | --- |
| `telemetry_collection_runs` | 每个 collection 的边界、状态和审计 payload |
| `machines` | 当前机器状态，含 active/disappeared 时间 |
| `network_instances` | 当前 EasyTier 实例状态 |
| `nodes` | 每个实例当前 Node 信息 |
| `peer_links_current` | 实例作用域的当前目标 Peer 路由与连接统计 |
| `metric_samples` | 按 collection 追加的指标样本 |
| `telemetry_collection_errors` | snapshot/machine/instance 层结构化错误 |
| `schema_migrations` | 已应用 migration 与 SHA-256 checksum |

当前 raw payload 仅作为审计补充，规范化表才是查询和后续 API 的主要模型。

## 验证

```bash
cd controller
go test ./...
go vet ./...
go test -race ./internal/gatewayclient ./internal/poller ./internal/api
```

PostgreSQL 集成测试需要专用空数据库：

```bash
read -rsp 'URL-encoded test database password: ' TEST_DB_PASSWORD && echo
export NEXUSTIER_TEST_DATABASE_URL="postgres://nexustier:${TEST_DB_PASSWORD}@127.0.0.1:5432/nexustier_test?sslmode=disable"
go test ./internal/ingest -count=1
unset NEXUSTIER_TEST_DATABASE_URL TEST_DB_PASSWORD
```

测试会 `TRUNCATE telemetry_collection_runs CASCADE`，绝不能指向生产或包含
需要保留数据的数据库。CI 会自动创建专用测试数据库执行这些场景。

## 当前限制

- 没有历史指标保留/分区策略，metric samples 会持续增长。
- 没有面向控制台的机器、拓扑和时间序列查询 API。
- 没有设备级准入、OIDC、RBAC 或 API 认证。
- 没有 Redis 发布、多控制器协调或高可用部署定义。
- 没有 Controller 容器镜像、Compose 或 systemd 发布单元。

源码结构与关键事务语义见 [控制器源码架构解析](../docs/controller-code.zh-CN.md)。
