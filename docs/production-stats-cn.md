# Kander 实战数据

[English](production-stats.md) | [日本語](production-stats-ja.md)

统计日期: 2026-10-07. 数据来自 5 个使用 Kander 的项目的 `kanban/done/`, 只读统计.

## 一句话

1 个人, 6 个 Agent, 68 天, 5 个项目, **1,726 张任务卡**. 其中 975 张有结构化审核记录: 审核由独立的审核运行完成, 约 80% 的审核员与执行者不是同一个 Agent. 更早的卡审核记录格式不同, 未计入审核统计.

## 按项目

| 项目 | 是什么 | 时间 | 完成任务卡 | 审核批次 | 被拦下的批次 | 审核运行 | 阻断问题 |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |
| [QuickTUI](https://quicktui.ai/) | 手机远程操作 Agent 的终端 (iOS / Android / 服务端) | 08-01 ~ 10-07, 68 天 | 1,317 | 567 | 259 (46%) | 1,006 | 889 |
| 后端服务 | 服务端项目 (闭源) | 08-11 ~ 09-29, 50 天 | 180 | 97 | 36 (37%) | 206 | 63 |
| [Kander](https://github.com/dualface/kander) | 本项目, 用自己开发自己 | 09-04 ~ 10-05, 32 天 | 155 | 103 | 69 (67%) | 312 | 280 |
| Quick Compress | iOS App (闭源) | 10-02 ~ 10-06, 5 天 | 51 | 50 | 29 (58%) | 93 | 64 |
| [Ullage](https://github.com/dualface/ullage-cli) | 订阅用量查看 CLI | 09-16 ~ 10-03, 18 天 | 23 | 23 | 18 (78%) | 77 | 75 |
| **合计** | | **68 天** | **1,726** | **840** | **411 (49%)** | **1,694** | **1,371** |

- 平均每天约 25 张卡. 有起止时间的 1,718 张卡, 单卡耗时中位 60 分钟.
- 约一半的审核批次被拦下: 独立审核找出了阻断问题, 每条都要处置 (修复 / 驳回 / 推迟) 后才能交付.

## 项目介绍

代码量用 `cloc` 统计 git 跟踪的文件, 只计编程语言, 含测试, 不含 protobuf 等生成代码和 JSON / Markdown / YAML 等数据与文档. 创建时间取仓库首次提交.

- **QuickTUI**: 在手机上远程操作电脑上编码 Agent 的终端产品. 仓库创建于 2026-04-14, 8 月起用 Kander 推进.
  架构: 自托管的 Go 服务端 (Linux / macOS / Windows), iOS 客户端 (Swift), Android 客户端 (Kotlin), 客户端共用的 WebView 组件 (TypeScript), 云端中继与账户服务 (Go), Rust 安装器.
  代码约 110 万行: Go 50 万, Swift 24 万, Kotlin 20 万; 其中测试约 40 万行.
- **后端服务**: 一个闭源的线上服务. 仓库创建于 2026-06-22.
  架构: Go 服务端, TypeScript 管理后台, PostgreSQL 与 Redis.
  代码约 21 万行: Go 15 万, TypeScript 4.8 万; 其中测试约 9 万行.
- **Kander**: 本项目. 仓库创建于 2026-09-04.
  架构: Go 单二进制, 包含 CLI 与 TUI 看板 (Bubble Tea), 支持 macOS / Linux / Windows 的 amd64 与 arm64.
  代码约 13.7 万行 Go; 其中测试约 7 万行.
- **Quick Compress**: 一个闭源的 iOS 媒体压缩 App. 仓库创建于 2026-10-02, 第 4 天 (10-05) 发出首个 TestFlight 版本.
  架构: iOS 26 原生 App (Swift), 拆为 QCCore / QCMedia / QCUI 三个包.
  代码约 2.2 万行; 其中测试约 6.5 千行.
- **Ullage**: 查看多家 AI 订阅用量的守护进程与 CLI. 仓库创建于 2026-08-27, 9 月中旬起用 Kander 推进.
  架构: Rust workspace (7 个 crate), 单二进制同时提供守护进程、CLI、TUI 与可选的 HTTP API, 支持 macOS / Linux / Windows.
  代码约 6 万行 Rust (单元测试写在源文件内, 未单独计数).

## 按 Agent

| Agent | 执行的任务卡 | 担任审核的次数 |
| --- | ---: | ---: |
| Codex | 561 | 358 |
| Claude Code | 466 | 166 |
| Grok | 340 | 508 |
| Cursor | 203 | 251 |
| Pi | 113 | 228 |
| Devin | 42 | 183 |

每个 Agent 既写代码, 也审别人的代码. 另有 OpenCode 执行过 1 张卡, 未计入上表.

## 口径

- **完成任务卡**: `kanban/done/` 下的卡, 包括目录卡和单文件卡, 不含 `archived/` 和 `trash/`.
- **项目范围**: 另有 8 个项目各有 1 ~ 3 张卡 (合计 12 张), 未计入.
- **时间**: 按卡 ID 的日期前缀取首末日期, 含首尾两天.
- **审核批次**: `reviews/batches/<批次>/disposition.json`, 按批次 ID 去重. 一个批次覆盖任务组里多张卡时, 每张卡下各存一份, 只计一次.
- **审核运行**: 一个审核角色 (如 PMQA / Security) 的一次审核, 按 run ID 去重.
- **被拦下的批次**: 批次内任一审核运行报告了阻断问题.
- **阻断问题**: 审核报告 `FINDINGS` 中的条目 (blocking / high / medium 三档), 不含非阻断建议.
- **执行的任务卡**: 卡头 `OWNER` 字段, 较早的卡写作「负责人」. 1,726 张卡都有此字段.
- **卡数是吞吐, 不代表价值或难度.** 各项目的任务大小不同, 不宜横向比较.

更早的分析见 [Kander 生产实践回顾](KANDER_PRODUCTION_RETROSPECTIVE_CN.md) (数据截至 2026-09-18).
