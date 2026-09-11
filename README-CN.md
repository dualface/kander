# Kander

[English](README.md) | **简体中文** | [日本語](README-JA.md)

[![Kander - 多 AI Agent 的看板调度](docs/star-please.png)](https://github.com/dualface/kander)

一个人用看板调度多个 AI Agent.

![Kander 工作流](docs/workflow-cn.svg)

## 1. 快速开始

运行需要 Git, 以及 Codex, Claude, Grok 或 Cursor 中至少一个.

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

在 `o` → 界面 → 主题中选择 **Tide**、**Dusk**、**Slate Dark** 或 **Slate Light**。Slate 深浅两款搭配 herdr 等灰蓝色终端外层；原有主题和默认设置保持可用。

> 上图看板内容来自我的真实项目 [https://quicktui.ai](https://quicktui.ai). QuickTUI 是一个远程操作电脑上各种 Agent 的工具, 支持 iOS/Android/macOS/Linux/Windows, 免费使用.

进阶阅读: 幻灯片 [如何高效推进任务](docs/how-to-advance-tasks-efficiently-cn.pdf) (PDF).

## 2. GitHub 集成

把项目关联到 GitHub 仓库需要 [GitHub CLI](https://cli.github.com/)（`gh`）。Kander 不会索要、读取或保存 token，只复用 `gh` 已管理的凭据。

```sh
kander issue repo                                   # 解析当前工作树对应的仓库
kander issue repo --repo HOST/OWNER/REPO --json     # 或显式传入仓库引用
kander issue list --state open --label bug          # 列出 Issue：--state open|closed|all、可重复 --label、--search、--limit、--json
kander issue show 42 --comments                     # 查看单个 Issue 及其评论
kander issue import 42 --comments                   # 导入为 backlog 卡片，并附带 Issue 正文与评论
```

`kander issue repo` 会向 GitHub 确认仓库的规范身份，而不是相信目录名。当一个工作树存在多个不同 remote 时，它不会猜测，而是提示使用 `--repo` 或 `gh repo set-default`。`kander doctor` 会报告 `gh` 的路径、版本和各 host 的认证状态，且不修改凭据、remote 或账号。

`kander issue list` 按状态、标签和搜索词筛选，并限制获取的 Issue 数量；Pull Request 不会被混入结果。`kander issue show NUMBER` 渲染单个 Issue，`--comments` 会在明确的评论上限内加载评论，而不是静默截断。两个命令都支持 `--json` 供脚本使用，并给出可操作的错误（缺少 `gh`、host 未认证、限流、remote 歧义），而不是原始报错。

`kander issue import NUMBER` 会创建一张与 Issue 绑定的普通 backlog 卡片：`--comments` 附带评论，`--type` 覆盖按标签推断的类型，`--large` 指定规模，`--language` 固定卡片语言，`--json` 输出可供脚本使用的结果。Issue 的标题、正文和评论会保存在 `spec.md` 旁边的 `source/github-issue.json` 与 `source/github-issue.md` 中；清洗为单行后的标题同时作为卡片标题，而卡片契约只根据已确认的仓库身份生成。重复导入同一个 Issue 会返回已有卡片，来源唯一性检查与卡片发布在同一个看板事务中完成，因此并发导入也不会产生重复卡片。超出上限的 Issue 会被拒绝并给出提示，而不是截断。快照格式与安全模型见 [docs/github-issue-import.md](docs/github-issue-import.md)。

在终端看板中按 `g` 会以弹窗形式打开同一份数据：选中一行即自动加载该 Issue 及其评论（本地快照缓存先出内容，后台再刷新；内容未变不重置画面，有变更则给出提示），`Enter` 打开详情页，`Tab` 切换状态，`/` 搜索，`l` 按标签筛选，`i` 将 Issue 导入为 backlog 卡片（已导入时跳转到对应卡片，`I` 则连评论一起导入），`s` 为选中的 Issue 确认启动一次接手会话：未绑定时启动新的 Agent 读取本地重新抓取的证据、调查并与你确认方案后再导入卡片；已绑定到卡片的 Issue 会在看板中定位该卡片；绑定卡片处于 backlog 时还额外提供启动同一会话补全契约。TUI 只启动后台启动器（`herdr`、`tmux`、`tmux-session`），其他情况会指向命令行；这条路径不会写入 `CARD_REVIEW:`，也不会创建或移动卡片。`r` 刷新，`o` 在浏览器中打开，`Esc`/`q` 关闭弹窗且不改变看板状态。已导入的 Issue 会显示绑定的任务 ID 与状态，远端有更新时只做标记，不会自动覆盖卡片。所有请求都在后台执行，看板不会阻塞。

## 3. 许可

本项目使用 MIT License, 见 [LICENSE](LICENSE).
