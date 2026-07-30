# 🌐 NexusTier

> **Orchestrating the Next-Generation Zero-Trust Mesh Network.**  
> **连接星穹，智驭网络 —— 下一代现代零信任 SDN 编排系统。**

[![GitHub License](https://img.shields.io/github/license/WuYouOwO/NexusTier?style=flat-square)](LICENSE)
[![EasyTier Core](https://img.shields.io/badge/EasyTier-v2.6.4-blue?style=flat-square)](https://github.com/EasyTier/EasyTier)

---

## 📖 项目简介 (Introduction)

**NexusTier** 是一款专为 **EasyTier** 打造的、企业级零信任（ZTNA）SDN 控制器与 SD-WAN 编排系统。

传统的组网工具（如 Tailscale）高度依赖中心化协调器，在大规模、高并发的路由环境下极易遇到性能瓶颈。**NexusTier** 创造性地采用 **“控制面集中编排、数据面去中心化自律（Mesh）”** 的混合架构，将 EasyTier 强悍的打洞、抗丢包加速与加密能力释放到极致，为企业和极客提供一套完全自主可控、极佳 Vibe 交互的电信级组网解决方案。

---

## ✅ 当前可用能力 (Available Today)

*   **原生 EasyTier 安全接入**：兼容 EasyTier v2.6.4 UDP WebClient，支持 Noise + AES-GCM、安全心跳、共享准入 Token 和 Machine ID 重连保护。
*   **实时拓扑遥测**：反向采集 Node、Peer、Route、RTT、丢包、流量和 Stats，提供部分成功、结构化错误、单飞和有界采集。
*   **版本化控制面契约**：Rust Gateway 生产 `nexustier.topology.v1`，Go Controller 严格校验并消费同一 Schema/fixture。
*   **PostgreSQL 持久化**：幂等保存 Machine、Instance、Node、当前 Peer 链路、指标样本和采集错误，保护乱序和局部失败状态。
*   **内部运维接口**：Gateway 和 Controller 提供回环/私网健康、就绪和摄取状态 API。

当前没有 Web UI、OIDC/RBAC、IPAM、ACL、SSH/RDP 或 Redis。当前部署和操作见
[端到端部署指南](docs/current-deployment-guide.zh-CN.md)与
[用户教程](docs/current-usage-guide.zh-CN.md)。

## 🎯 产品目标 (Planned Capabilities)

*   **📊 全球拓扑遥测 (Global Topology Telemetry)**
    *   基于 IP 离线库自动进行全球节点地理定位。
    *   提供炫酷的 2D/3D 全球资源大屏，实时渲染 P2P 直连（实线）与中继转发（虚线）路径，动态显示物理延迟与带宽。
*   **🔑 零信任免密准入 (Passwordless ZTNA)**
    *   **免密安全 SSH**：Agent 托管极轻量 SSH 进程，基于控制器统一身份校验（RBAC），支持免密直接登录与 MFA 网页二次质询。
    *   **免密 Windows RDP（黑科技）**：在客户端 Agent 中集成虚拟智能卡驱动（UMDF/KSP），通过 RDP 智能卡通道重定向（SCARD），实现无密码/扫码一键登录远程 Windows 桌面。
*   **🛡️ 声明式有状态防火墙 (Stateful ACL & Tags)**
    *   摒弃复杂的 JSON 手写规则，提供对非专业人员极度友好的“安全标签（Tags）”防火墙。
    *   控制器在后台自动将策略“翻译”为 IP/CIDR 与中继转发链规则，实现毫秒级安全隔离。
*   **🚄 多协议高可用热备 (Multi-Protocol Failover)**
    *   支持在同一条链路上并行配置 WireGuard (UDP) 与 WSS (TCP) 双通道。
    *   物理网络波动时，数据面在毫秒级内零感知切换通道，提供电信级的防封锁与网络抗震能力。
*   **☁️ 零摩擦统一登录 (Zero-Touch Provisioning)**
    *   Agent GUI 支持主流企业级 OIDC / OAuth2（飞书、企业微信、Okta 等）扫码一键登录。
    *   自动下发网卡配置与 IP 分配（IPAM），实现小白级“零配置”快速入网。

---

## 🏗️ 系统架构 (Architecture)

```text
               +----------------------------------+
               |      NexusTier 控制台 (Web UI)   |  <--- 极致交互与 Vibe
               +----------------------------------+
                                |
               +----------------------------------+
               |     SDN 控制中心 (Go / GoBGP)     |  <--- 静态配置与拓扑计算
               +----------------------------------+
                                |
               +----------------------------------+
               |     Rust 协议网关 / 翻译官       |  <--- Protobuf/WS 双向 RPC 中转
               +----------------------------------+
                                |
                      (WSS / TCP / UDP 隧道)
                                |
               +----------------------------------+
               |     客户端 Agent + EasyTier Core |  <--- 纯用户态 L3 转发与安全隧道
               +----------------------------------+
```

---

## 🛠️ 技术栈 (Tech Stack)

*   **控制面核心 (Control Plane)**: Go / `net/http` / pgx / PostgreSQL；后续 GoBGP / Redis
*   **协议转换层 (Protocol Gateway)**: Rust / Tokio / Protobuf
*   **客户端代理 (Agent & GUI)**: Rust / Go / Tauri / TypeScript
*   **前端大屏 (Frontend Panel)**: Vue 3 / React / Vite / Tailwind CSS / ECharts GL

---

## 🚀 当前实现 (Current Implementation)

第一阶段的 Rust 原生接入网关已落地：

*   兼容 EasyTier `v2.6.4` 原生 UDP WebClient 协议，默认监听 `22020/UDP`。
*   支持 Noise + AES-GCM 安全配置通道，不修改 `easytier-core` 客户端代码。
*   使用 Machine ID 维护并发内存 Session Pool，安全处理断线、重连与连接替换。
*   当前源码 WIP 只允许 Noise + AES-GCM 安全重连注册，可选校验共享准入 Token，并禁止会话内切换 Machine ID。
*   通过原生双向 RPC 反向采集 Node、Peer、Route、RTT、流量和 Stats 指标。
*   当前源码 WIP 提供 `nexustier.topology.v1` JSON Schema、固定跨语言 fixture、采集 ID、分层观测时间和结构化局部错误。
*   在 `127.0.0.1:11211` 暴露只读 `/healthz`、`/readyz`、`/v1/sessions` 和 `/v1/topology` API。
*   提供非 root 多阶段容器镜像，不需要 TUN 权限或额外 Linux capabilities。

Go 控制器遥测摄取基础当前可作为 WIP 完整栈运行：

*   严格消费共享的 `nexustier.topology.v1` 契约。
*   提供 PostgreSQL migrations 与 Machine、Instance、Node、Peer、Metric、Error 规范化模型。
*   使用单事务、`collection_id` 和时序门控实现幂等摄取、乱序保护与局部失败保留。
*   使用超时、抖动和禁止重叠的轮询 worker，并暴露内部健康、就绪与摄取状态 API。
*   已通过 PostgreSQL 18 集成测试和进程级 Gateway fixture 联调；Gateway 与 Controller 均由 GitHub Actions 构建并发布到 GHCR。

当前已验证发布基线：

*   源码：`654c70fbb70289c5313f7685abff72a59b3c9f7b`。
*   Gateway 镜像：`ghcr.io/wuyouowo/nexustier:sha-654c70f`。
*   镜像 digest：`sha256:fe7dbc15f1b96955fac429f3e3825c2f71f57aacbf4e415c2b4a8c7cbb4b7028`。

开发运行：

```bash
cargo run --locked --package nexustier-gateway
```

控制器开发运行：

```bash
set -a
. "${HOME}/.config/nexustier/controller.env"
set +a
go -C controller run ./cmd/nexustier-controller
```

该文件至少包含 `NEXUSTIER_CONTROLLER_DATABASE_URL`，应位于仓库外并保持 `0600`。
完整配置见端到端部署指南。

容器编排示例会启动 PostgreSQL、Gateway 与 Controller。先修改示范变量中的密码和准入 Token：

```bash
cp .env.example .env
docker compose -f compose.example.yaml pull
docker compose -f compose.example.yaml up -d
```

默认镜像分别为 `ghcr.io/wuyouowo/nexustier:latest` 与
`ghcr.io/wuyouowo/nexustier-controller:latest`。发布版本时，建议在 `.env` 中将两者固定为相同的版本标签；Gateway UDP 端口公开，两个 HTTP 运维 API 仅绑定宿主机回环地址。

中文文档：

*   [中文文档索引](docs/README.md)
*   [当前版本端到端部署指南](docs/current-deployment-guide.zh-CN.md)
*   [当前版本用户教程](docs/current-usage-guide.zh-CN.md)
*   [Gateway 专项使用指南](docs/usage-guide.zh-CN.md)
*   [Gateway 专项生产部署指南](docs/deployment-guide.zh-CN.md)
*   [Rust 网关使用与部署手册](docs/gateway-guide.zh-CN.md)
*   [Rust 网关源码架构解析](docs/gateway-code.zh-CN.md)
*   [Go 控制器 WIP 运行说明](controller/README.md)
*   [Go 控制器源码架构解析](docs/controller-code.zh-CN.md)
*   [AI Agent Engineering Handoff (English)](docs/AGENT_HANDOFF.md)
*   [遥测摄取基础开发计划（已完成）](docs/development-plan.zh-CN.md)

英文 Gateway 配置、API 与容器说明见 [Rust 网关文档](crates/nexustier-gateway/README.md)。

---

## 📅 路线图 (Roadmap)

*   [ ] **Phase 1**: 基于原生 EasyTier 核心的双向 RPC 通信对接，打通全球拓扑大屏与配置下发。 (WIP)
*   [ ] **Phase 2**: 实现分布式 IPAM（IP 地址管理）与声明式有状态 ACL 防火墙。
*   [ ] **Phase 3**: 推出 NexusTier 多端 GUI Agent，支持企业级 SSO 扫码入网。
*   [ ] **Phase 4**: 实现零信任安全 SSH 终端与基于虚拟智能卡重定向的免密 RDP 连接。

---
