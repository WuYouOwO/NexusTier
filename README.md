# 🌐 NexusTier

> **Orchestrating the Next-Generation Zero-Trust Mesh Network.**  
> **连接星穹，智驭网络 —— 下一代现代零信任 SDN 编排系统。**

[![GitHub License](https://img.shields.io/github/license/WuYouOwO/NexusTier?style=flat-square)](LICENSE)
[![EasyTier Core](https://img.shields.io/badge/EasyTier-v2.6.4-blue?style=flat-square)](https://github.com/EasyTier/EasyTier)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20Windows%20%7C%20macOS-orange?style=flat-square)](#)

---

## 📖 项目简介 (Introduction)

**NexusTier** 是一款专为 **EasyTier** 打造的、企业级零信任（ZTNA）SDN 控制器与 SD-WAN 编排系统。

传统的组网工具（如 Tailscale）高度依赖中心化协调器，在大规模、高并发的路由环境下极易遇到性能瓶颈。**NexusTier** 创造性地采用 **“控制面集中编排、数据面去中心化自律（Mesh）”** 的混合架构，将 EasyTier 强悍的打洞、抗丢包加速与加密能力释放到极致，为企业和极客提供一套完全自主可控、极佳 Vibe 交互的电信级组网解决方案。

---

## ✨ 核心特性 (Key Features)

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

*   **控制面核心 (Control Plane)**: Go (Golang) / GoBGP / Gin / Gorm / Redis / PostgreSQL
*   **协议转换层 (Protocol Gateway)**: Rust / Tokio / Protobuf
*   **客户端代理 (Agent & GUI)**: Rust / Go / Tauri / TypeScript
*   **前端大屏 (Frontend Panel)**: Vue 3 / React / Vite / Tailwind CSS / ECharts GL

---

## 📅 路线图 (Roadmap)

*   [ ] **Phase 1**: 基于原生 EasyTier 核心的双向 RPC 通信对接，打通全球拓扑大屏与配置下发。 (WIP)
*   [ ] **Phase 2**: 实现分布式 IPAM（IP 地址管理）与声明式有状态 ACL 防火墙。
*   [ ] **Phase 3**: 推出 NexusTier 多端 GUI Agent，支持企业级 SSO 扫码入网。
*   [ ] **Phase 4**: 实现零信任安全 SSH 终端与基于虚拟智能卡重定向的免密 RDP 连接。

---
