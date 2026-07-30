# NexusTier 内部 API 契约

本目录保存 NexusTier 内部跨语言遥测边界。Rust Gateway 是 topology v1 的生产者，
Go Controller 是严格消费者。

该契约描述 Gateway 到 Controller 的内部只读快照，不是面向最终用户的公开 API，
也不包含认证、设备注册、IPAM、ACL、网络配置下发或 EasyTier 数据面协议。当前
Controller 只消费并持久化该契约，不提供同结构的公开查询接口。

部署和使用入口：

- [当前版本端到端部署指南](../docs/current-deployment-guide.zh-CN.md)
- [当前版本用户教程](../docs/current-usage-guide.zh-CN.md)

| 文件 | 用途 |
| --- | --- |
| `topology-v1.schema.json` | `/v1/topology` 的 JSON Schema 2020-12 定义 |
| `fixtures/topology-v1.json` | 固定的完整成功/局部失败混合样例 |

兼容规则：

- `schema_version` 在 v1 周期内固定为 `nexustier.topology.v1`。
- 删除字段、重命名字段、收窄已有字段类型或改变语义属于破坏性变更。
- 新增可选字段前必须同步 Rust fixture、Go 解码测试和相关文档。
- `collection_id` 唯一标识一次实际采集；缓存命中会返回相同 ID。
- `started_at_ms` 和 `completed_at_ms` 表示整次采集边界。
- `observed_at_ms` 表示对应 Machine 或 Instance 完成本轮观测的时间。
- 消费者必须根据 `errors[].code` 判断错误类别，`message` 仅用于诊断。
- 局部错误不等价于机器离线，控制器不得因此删除上次成功状态。
- 顶层 `collection_timeout` 表示总期限到达；`machines` 仍包含期限前完成的结果。
- 所有毫秒时间戳不得晚于 `9999-12-31T23:59:59.999Z`，以保持 Go/PostgreSQL 范围一致。
- Peer ID 是网络实例作用域内的 `uint32`，不是全局设备 ID。
- `errors` 表示采集结果的部分成功语义，不是命令执行或策略下发结果。

Rust 生产者通过 `topology_v1_fixture_matches_the_producer_contract` 测试锁定
fixture。Go 控制器通过 `TestClientDecodesSharedTopologyFixture` 使用同一
fixture 锁定消费者解码，并拒绝未知字段。

修改契约时必须在仓库根目录同时运行：

```bash
cargo test --locked --workspace
go -C controller test ./internal/gatewayclient ./internal/ingest
jq empty contracts/topology-v1.schema.json contracts/fixtures/topology-v1.json
```
