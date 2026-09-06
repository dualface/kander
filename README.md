# Kander

一个人用看板调度多个 AI Agent.

![Kander 工作流](docs/workflow.svg)

## 1. 新手指引

安装完成后即可使用.

4 步上手:

1. 新建一个 Agent 会话, 在里面讨论需求或者任务, 说清楚目标和验收条件. 推荐使用 Agent 的 Plan 模式.
2. 任务确认后, 在该会话里要求 Agent 用看板流程完成任务:

```text
用 kander new 建卡, 完善任务契约并自审后用 kander start 启动
```

3. 有多个需求时, 对每个需求重复步骤 1-2, 不断安排并启动任务.
4. 用命令行界面查看任务状态:

```sh
kander
```

## 2. 安装

需要 Go 1.25+, Git, 以及 Codex, Claude, Grok 或 Cursor 中至少一个.

拿到 kander 二进制后直接运行即可. 首次启动若尚未安装, 会进入交互向导: 选择语言 (同时决定界面、规则原文和配置语言) 与安装位置, 再释出规则并把自身拷到目的地. 之后自动进入环境检查和选项面板. 已安装用户可用 `kander install` 重跑向导 (升级规则或改安装位置).

用 Go 从源码安装:

```sh
go install github.com/dualface/kander/cmd/kander@latest
```

或在仓库根构建后运行:

```sh
make
./kander
```

Windows:

```powershell
go build -o kander.exe ./cmd/kander
.\kander.exe
```

Kander 有两种安装作用域, 共用同一套规则和程序.

### 2.1 全局安装

向导里选全局. 二进制落到 `~/.local/bin/kander` (Windows 为 `kander.exe`), 规则落到 `~/.agents/`. 命令不可用时, 先把用户主目录下的 `.local/bin` 加入 PATH.

### 2.2 项目本地安装

向导里选项目并给出 Git 仓库目录. 安装位于该仓库主 worktree 的 `.kander/`, 全部 worktree 共用这份安装, 并向 `.git/info/exclude` 幂等追加 `/.kander/`. 用向导输出的绝对命令路径打开看板, 后文的 `kander` 命令也用这个入口. 项目配置独立于全局配置, 进入项目目录不会自动切换 PATH 中的命令.

## 3. 常用命令

直接运行 `kander` 用命令行界面查看任务状态. 支持多栏浏览、搜索、任务详情、鼠标操作与剪贴板复制, 任务卡正文按 Markdown 渲染.

![终端看板](docs/kander-tui-01.jpg)

> 上图看板内容来自我的真实项目 [https://quicktui.ai](https://quicktui.ai). QuickTUI 是一个远程操作电脑上各种 Agent 的工具, 支持 iOS/Android/macOS/Linux/Windows, 免费使用.

常用按键: 方向键或 `hjkl` 移动, `Enter` 看任务卡, `/` 搜索, `y` 复制任务 ID, `-`/`=` 增减同屏栏目数, `a` 切换存档栏目, `t` 换主题, `o` 打开选项, `r` 刷新, `q` 退出. 按 `?` 调出完整按键说明.

其余命令主要给 Agent 使用: `kander new`/`pick`/`start`/`resume` 建卡与启动, `kander notify`/`dismiss` 派发消息与遣散会话, `kander check` 检查看板入口与任务契约, `kander review` 运行一次审核, `kander config`/`doctor` 查看与修复配置, `kander install` 重跑安装向导, `kander version` 查看版本号.

`kander show <task-id>` 在卡片正文前输出当前状态与绝对路径, 供 Agent 写卡前重新定位; `kander guard-write <path>` 供宿主项目的写入前 hook 拦截「旧路径复活卡片」的误写, 接入方式见 [docs/kanban-write-guard.md](docs/kanban-write-guard.md).

## 4. 工作流程

每个任务都按下面的流程进行. Kander 的开发规则按模块启用, 新安装默认全开; 关掉对应模块就让单卡沿用你已有的 Git、审核和交付流程.

### 4.1 建卡与自审

Agent 建卡后必须自审任务契约, 在 `DISCUSSION` 留下 `SELF_REVIEW:` 结论行. 大任务目录卡和任务组成员卡还要由不共享建卡上下文的独立 Agent 审卡, 留下 `CARD_REVIEW:` 行.

卡片进入待处理栏前会做机器门禁: 必填章节完整、不残留 `<FILL_IN>` 占位符、验收条件至少有一条 `- [ ]` 可判定条目、上述记录行齐全. 工具只校验记录存在, 结论质量仍由建卡者负责.

### 4.2 任务拆分

启用「任务组编排」后, Agent 会根据任务的复杂度和可拆分情况, 将任务拆分为尽可能独立的任务卡. 每个任务卡对应一个或多个 Markdown 文件.

如果一个任务拆分为多个任务卡, 则视为一个任务卡组. 关掉该模块就只跑单卡, 不强制拆组.

### 4.3 单个任务卡独立完成

对于单个任务卡, 会启动一个独立的 Agent 完成任务. 这个 Agent 称为任务 Agent.

任务 Agent 的工作步骤:

- 创建一个 git worktree, 避免和其他 Agent 的工作互相干扰.
- 生成代码或文档.
- 启动独立的审核 Agent 对成果进行审核.
- 审核通过后, 按已获授权把任务分支集成到 develop, 同步本地并清理任务 worktree 与分支.
- 输出任务总结, 结束.

关掉「Git 流程」就不要求 worktree、develop、自动提交和合回; 关掉「审核流程」就不自动起审核.

### 4.4 多个任务卡组成的任务卡组

启动任务卡组的 Agent 会转变为主控 Agent, 并负责编排任务和审核流程. 「任务组编排」需要同时启用「Git 流程」.

主控 Agent 会根据任务卡的依赖关系, 确保所有任务卡按照正确的顺序完成. 在可能的情况下, 也会同时启动多个任务卡, 提高效率.

与单个任务卡的流程有所区别, 每个任务 Agent 现在只负责生成代码或文档, 完成后把任务卡移入 review 栏等待主控接收交付.

- 主控按模块、里程碑或依赖链分批接收和审核, 不必等整组做完; 审核批次完成前, 批外交付排队.
- 收到审核结果后, 主控汇总并通过 `kander notify` 把修改意见发回对应的任务 Agent, 接收修复交付后做增量复审.
- 重复整个过程, 直到整个任务卡组完成, 最后按已获授权集成到 develop.
- 主控派原任务 Agent 清理任务分支和 worktree、补全记录并迁入 done, 再调度后继组.
- 输出任务总结, 询问用户是否遣散任务 Agent, 结束.

### 4.5 审核

Kander 中定义了四种审核角色, 每个角色在审核时侧重点不同:

- PM: 产品经理只关注功能实现是否符合目标, 不会随意扩大功能范围.
- QA: 关注功能实现是否符合项目整体架构要求, 以及代码质量和可维护性.
- CSA: 关注代码内在安全性.
- Hacker: 用对抗性视角从外部审查是否存在攻击弱点.

PM 与 QA 基于同一 commit 并行首轮, 都通过后 CSA 和 Hacker 才进入第二阶段. 每个角色可以配成自动、跳过或必须三档, 大多数任务让 PM 和 QA 跑起来就够了.

![Kander 审核流程](docs/review.svg)

即使关掉「审核流程」模块, 仍可明确调用 `kander review` 跑单次审核, 不要求采用 Kander 的分支模型.

## 5. 配置

在看板里按 `o` 打开选项面板, 随时修改配置:

- 界面: 配色主题、同屏最大栏目数、栏目最小宽度、自动刷新间隔、只显示当前栏目、显示所有栏目、默认语言
- 任务执行与模型: 大任务与小任务分别使用的 Agent、启动方式, 以及这些 Agent 的模型与推理档位
- 审核与模型: 四个审核角色各自的 Reviewer、环节策略, 以及各自的模型与推理档位
- 规则模块: 交流与协作、代码质量、Git 流程、审核流程、任务建卡引导、任务组编排、完成报告格式
- 环境检查: 就地运行 `kander doctor` 检查并修复配置

界面偏好即选即生效, 其余分页按 `Enter` 写入配置文件. 选项面板需要交互终端; 也可以用 `kander config` 查看配置, 用 `kander doctor` 检查并修复配置.

配置保存在当前安装作用域的 `config.json`, 全局安装和项目安装各自保存, 互不继承. Agent 从当前作用域的 `KANDER-AGENTS.md` 入口读取配置, 再按需读取启用的规则模块.

## 6. 许可

本项目使用 MIT License, 见 [LICENSE](LICENSE).
