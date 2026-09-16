# Kander

[English](README.md) | **简体中文** | [日本語](README-JA.md)

[![Kander - 多 AI Agent 的看板调度](docs/star-please.png)](https://github.com/dualface/kander)

一个人用看板调度多个 AI Agent.

![Kander 工作流](docs/workflow-cn.svg)

## 1. 快速开始

运行需要 Git, 以及 Codex, Claude, Grok, Cursor 或 Pi 中至少一个.

**macOS** — 使用 Homebrew 安装:

```sh
brew install dualface/tap/kander

kander
```

**Linux** — 直接下载二进制:

```sh
ARCH=$(uname -m); [ "$ARCH" = x86_64 ] && ARCH=amd64; [ "$ARCH" = aarch64 ] && ARCH=arm64
curl -fsSL "https://github.com/dualface/kander/releases/latest/download/kander-linux-${ARCH}.tar.gz" | tar xz

./kander
```

**Windows** — 从 [Releases](https://github.com/dualface/kander/releases) 下载 `kander-windows-amd64.zip`, 解压后运行 `kander.exe`.

首次启动若尚未安装, 会进入交互向导. 安装完成后即可使用.

4 步上手:

1. 新建一个 Agent 会话, 在里面讨论需求或者任务, 说清楚目标和验收条件. 推荐使用 Agent 的 Plan 模式.
2. 任务确认后, Agent 会询问是否用看板流程启动任务. 确认即可自动启动任务.
3. 有多个需求时, 对每个需求重复步骤 1-2, 不断安排并启动任务.
4. 用命令行界面查看任务状态:

```sh
kander
```

![终端看板](docs/kanban-screenshot-01.png)

> 上图看板内容来自我的真实项目 [https://quicktui.ai](https://quicktui.ai). QuickTUI 是一个远程操作电脑上各种 Agent 的工具, 支持 iOS/Android/macOS/Linux/Windows, 免费使用.

进阶阅读: 幻灯片 [如何高效推进任务](docs/how-to-advance-tasks-efficiently-cn.pdf) (PDF).

## 2. GitHub 集成

把项目关联到 GitHub 仓库需要 [GitHub CLI](https://cli.github.com/)（`gh`）。Kander 不会索要、读取或保存 token，只复用 `gh` 已管理的凭据。

在终端看板中按 `g` 会以弹窗形式打开 GitHub Issue 列表。

## 3. 许可

本项目使用 MIT License, 见 [LICENSE](LICENSE).

## 4. 更新日志

### v0.7.2 — 2026-09-16

- 全局规则改为安装到 `~/.agents/kander`, 不再使用旧位置. 升级后请重跑一次安装程序, 让规则入口指向新路径.
- 新增 `kander check delivery` 和 `kander check overlap`. 两者直接分析 Git, 不依赖看板, 在任意普通工作树中都能运行. 退出码 `0` 表示无需处理, `1` 表示发现问题, `2` 表示参数错误.
- TUI 聊天框有了独立的 Agent 和模型设置, 与看板 Agent 分开.
- 看板和详情视图的刷新移出 UI goroutine, 并从增量摘要缓存读取. 大看板的绘制明显更快.
- 主控会话现在可以只带一张卡, 不再限于多卡计划.
- 规则: 所有分册只保留面向 Agent 的条款, 工具机制移入 `docs/`. 入口文件只说明每个会话首先要做什么, 其余交给 `KANDER-LOADING-RULES.md`.

### v0.7.1 — 2026-09-15

- 安装时自动复制二进制. Y/N 询问只在启动时出现, 不再和安装复制步骤混淆.

### v0.7.0 — 2026-09-15

- 审核角色合并为 **PMQA** 和 **Security**. 仍然打开的旧四键批次继续按历史角色工作, Options 面板会对遗留的审核键给出迁移提示. 想要一次性合并审核的项目可以使用可选的整合角色.
- 终端看板中的聊天框可以直接启动 Agent 会话, 不必先建卡.
- 空看板会打开欢迎浮层; 缺少 `kanban/` 时提示 `kander init`; 缺少作用域配置时直接打开 Options.
- 所有确认对话框统一到同一套按键约定.
- Markdown 详情视图支持 vim 移动键.

### v0.6.2 — 2026-09-14

- 把二进制复制到全局位置改为可选. 安装程序会说明可选全局副本的作用, 并使用你选定的入口.

### v0.6.1 — 2026-09-14

- 安装时正确解析包管理器的可执行文件链接.

### v0.6.0 — 2026-09-14

- **GitHub 集成.** `kander issue repo` 解析规范的仓库标识. `kander issue list` / `show` 读取 issue, 在看板中按 `g` 以浮层打开 issue 列表. issue 可导入为 backlog 卡片, `kander issue triage` 启动接管会话, 完成的结果会回写到 issue. Kander 不索要, 不读取, 不保存 token, 只复用 `gh` 已管理的凭据. `kander doctor` 会报告 GitHub CLI 状态.
- **Pi** 加入内置 Agent 行列, 与 Codex, Claude, Grok, Cursor 并列, 并支持审核. 内置 Agent 现在是内嵌的带版本 JSON 定义, 声明启动 argv, 面板投递, 退出命令, 会话钩子和审核调用. 自定义 Agent 必须自行声明审核模板.
- **终端定义.** tmux 和 herdr 都由声明式定义驱动, 统一在一个后端接口之后. 新增的终端清单命令会报告可用定义, 检查一致性, 并说明后端选择顺序.
- **主题.** 六套命名主题, 包含 Tide, Dusk, Slate, 可在配置和 Options 面板中选择, 固定为真彩色取值.
- **Options 面板.** 支持编辑项目 overlay 并提供继承与恢复, 按卡片规模分别设置 Reviewer, 用 Tab 切换标签页, 实时应用界面语言, 取消时还原. 运行类命令现在要求配置完整.
- **窄终端.** 列会变成标签条, 手机大小的窗口里看板依然可用.
- `kander orchestrate` 启动多卡计划. `kander subscribe --watch` 可以监视任务组之外的卡片.
- 按状态变化的任务操作弹窗改用 `m` 打开, `g` 留给 issue 列表.
- 持久派发用带屏障的 epoch 截止时间恢复已接受的工作, 中断的交接不再丢失回执.
- 打标签的发布流程自动化, 并同步 Homebrew tap.
- 规则: 卡片 `SIZE` 按任务难度定义, 不按行数. 新增 GitHub issue 规则集. 主控会话必须持续监视自己启动的卡片. 本仓库的任务卡标题使用英文.
