# NexusTier 内部 API 契约

本目录保存跨语言控制面边界。Rust 网关是 topology v1 的生产者，当前 Go
控制器 WIP 是严格消费者。

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

Rust 生产者通过 `topology_v1_fixture_matches_the_producer_contract` 测试锁定
fixture。Go 控制器通过 `TestClientDecodesSharedTopologyFixture` 使用同一
fixture 锁定消费者解码，并拒绝未知字段。
