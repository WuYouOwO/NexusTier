# NexusTier 下一阶段开发计划（WIP）

## 1. 阶段目标

本阶段交付“可信、可界定、可持久化的遥测摄取基础”，把当前 Rust
EasyTier 协议网关扩展为 Go 控制器可以稳定依赖的内部数据源。

本阶段不并行开发 Redis、Web 控制台、IPAM、ACL、SSH 或 RDP。只有在
PostgreSQL 遥测摄取链路通过验收后，才进入后续产品功能。

## 2. 迭代计划

| 迭代 | 交付内容 | 验收标准 | 状态 |
| --- | --- | --- | --- |
| 1 | 可信 EasyTier 会话 | 明文探测不能注册；可选 Token 校验；Machine ID 在会话内不可变；聚焦测试通过 | Done |
| 2 | 版本化遥测契约 | JSON Schema、固定 fixture、采集 ID、开始/完成时间、结构化错误 | Done |
| 3 | 有界遥测采集 | 单飞、总期限、机器并发上限，不因并发 HTTP 请求放大反向 RPC | Done |
| 4 | 持续集成门禁 | Rust/Go test、Clippy、fmt、vet、PostgreSQL 集成和契约检查成为镜像构建前置任务 | Implemented，等待工作流首跑 |
| 5 | Go 控制器摄取基础 | typed client、PostgreSQL migrations、幂等事务、禁止重叠的轮询 worker、最小健康 API | Done |
| 6 | 联调与文档 | Rust/Go 全量检查通过，部署和开发文档与实现一致 | Done |

## 3. 设计约束

- EasyTier 继续负责 NAT 穿透、加密隧道、Mesh 路由和数据包转发。
- Rust 网关只处理原生 WebClient 控制通道和反向管理 RPC。
- Machine ID 是设备会话主键，但只有完成安全协商和准入校验后才可信。
- HTTP API 保持内部只读边界，不直接暴露到互联网。
- 局部采集失败不能导致整个拓扑快照失败，也不能删除上次成功状态。
- 当前拓扑状态与高频指标历史分开持久化，并使用不同保留策略。
- 协议依赖继续固定到经过验证的 EasyTier revision。

## 4. 验证基线

Rust 变更至少执行：

```bash
CARGO_BUILD_JOBS=1 cargo test --locked --workspace
CARGO_BUILD_JOBS=1 cargo clippy --locked --workspace --all-targets -- -D warnings
cargo fmt --all -- --check
git diff --check
```

Go 控制器创建后至少执行：

```bash
go test ./...
go vet ./...
```

摄取数据库变更还必须在专用空 PostgreSQL 数据库执行：

```bash
NEXUSTIER_TEST_DATABASE_URL='<专用测试库 URL>' go test ./internal/ingest -count=1
```

当前阶段已在 PostgreSQL 18.4 上验证 migration、幂等重试、UUID payload 冲突、
乱序保护、局部失败字段保留、消失实体状态收敛，以及实际控制器进程的首次轮询、
状态 API、数据库写入和 SIGTERM 关闭。

每个迭代完成后必须更新本计划的状态，并同步所有受影响的 README、使用、
部署、源码架构和工程交接文档。
