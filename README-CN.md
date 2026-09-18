# Kander

[English](README.md) | **简体中文** | [日本語](README-JA.md)

[![Kander - 多 AI Agent 的看板调度](docs/star-please.png)](https://github.com/dualface/kander)

用看板调度多 AI Agent 并行开发，内置独立审查与交付门禁。

> **为真实工程而生，而非概念演示。**  
> 自 2026 年 8 月以来，Kander 已在 [QuickTUI](https://quicktui.ai) 的生产环境中完成了近 1,000 个真实开发任务。在解决代码冲突、并发竞争与复杂缺陷中历练而成，提供真正可靠的 Agent 调度、独立代码审查与崩溃恢复机制。
> 
> 深入阅读：[Kander 生产实践回顾](docs/KANDER_PRODUCTION_RETROSPECTIVE.md) ｜ [完整分析报告（英文深度分析）](docs/KANDER_PRODUCTION_RETROSPECTIVE_FULL_EN.md)

![Kander 工作流](docs/workflow-cn.svg)

### 核心特性

- **两阶段独立审查门禁**：执行与代码审查物理隔离。PMQA 关注功能逻辑与隐性缺陷，拦截假绿测试。
- **冲突感知调度**：静态分析 Git 分支重叠与文件修改边界，受控并发，杜绝文件写冲突。
- **终端与工作区隔离**：基于 Git Worktree 和 tmux/herdr 容器，多 Agent 并行执行互不干扰。
- **无感 GitHub 集成**：复用本机 `gh` 凭据，看板内直接浏览 Issue、一键导入任务卡、同步进度。

## 1. 快速开始

运行需要 Git，以及 Codex、Claude、Grok、Cursor 或 Pi 中至少一个。

**macOS** — 使用 Homebrew 安装：

```sh
brew install dualface/tap/kander

kander
```

**Linux** — 直接下载二进制：

```sh
ARCH=$(uname -m); [ "$ARCH" = x86_64 ] && ARCH=amd64; [ "$ARCH" = aarch64 ] && ARCH=arm64
curl -fsSL "https://github.com/dualface/kander/releases/latest/download/kander-linux-${ARCH}.tar.gz" | tar xz

./kander
```

**Windows** — 从 [Releases](https://github.com/dualface/kander/releases) 下载 `kander-windows-amd64.zip`，解压后运行 `kander.exe`。

首次启动若尚未安装，会进入交互向导。安装完成后即可使用。

4 步上手：

1. 新建一个 Agent 会话，在里面讨论需求或任务，明确目标与验收条件。推荐使用 Agent 的 Plan 模式。
2. 任务确认后，Agent（安装 Kander 规则后）会确认是否通过看板流程启动任务，确认后自动创建并启动。
3. 有多个需求时，对每个需求重复步骤 1-2，持续编排并启动任务。
4. 用命令行界面查看任务状态：

```sh
kander
```

![终端看板](docs/kanban-screenshot-01.png)

> 上图看板内容来自我的真实项目 [https://quicktui.ai](https://quicktui.ai)。QuickTUI 是一个远程操作电脑上各种 Agent 的工具，支持 iOS/Android/macOS/Linux/Windows，免费使用。

### 终端看板快捷键

| 按键 | 说明 |
|---|---|
| `Space` / `Enter` | 查看任务详情，支持 Vim 模式浏览 |
| `m` | 任务操作菜单（启动、移动、归档） |
| `g` | 打开 GitHub Issues 浮层（查看与一键导入） |
| `c` | 调起独立 Chat 会话（即时讨论，不占卡片） |
| `/` | 过滤/搜索卡片 |
| `r` | 手动刷新看板数据 |

进阶阅读：幻灯片 [如何高效推进任务](docs/how-to-advance-tasks-efficiently-cn.pdf) (PDF)。

## 2. 常用命令

- `kander`：打开交互式终端看板。
- `kander doctor`：检查并修复环境依赖、Agent 可用性、启动器与规则配置。

## 3. GitHub 集成

把项目关联到 GitHub 仓库需要 [GitHub CLI](https://cli.github.com/)（`gh`）。Kander 不会索要、读取或保存 token，只复用 `gh` 已管理的凭据。

在终端看板中按 `g` 会以弹窗形式打开 GitHub Issue 列表。

## 4. 进阶文档

- [审查机制与完成门禁](docs/review-disposition.md)
- [卡片事务与崩溃恢复](docs/card-transactions.md)
- [终端后端与容器定义](docs/terminal-backend.md)
- [任务持久化派发协议](docs/durable-dispatch.md)
- [GitHub Issue 导入与结果协议](docs/github-issue-import.md)

## 5. 许可

本项目使用 MIT License，见 [LICENSE](LICENSE)。

## 6. 更新日志

发布说明见 [CHANGELOG.md](CHANGELOG.md)。
