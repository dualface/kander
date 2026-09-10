# 看板列表按 s 确认后启动 backlog/todo 任务卡

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 11:44
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t1F:wX:p21
- STARTED_AT: 2026-09-08 11:46
- FINISHED_AT:
- TASK_BRANCH: board-start-task-key
- RESULT:

## GOAL

在 `kander` 终端看板的棋盘视图里增加热键 `s`: 选中 `backlog` 或 `todo` 状态的任务卡, 确认后直接认领并启动执行 Agent. 现在用户在看板里挑好卡, 还要退出看板 (或另开一个终端) 敲 `kander start <task-id>`, ID 又长又要手抄. 目标是从看板一键完成「挑卡 -> 确认 -> 启动」, 并保持 `kander start` 既有的检查, 回滚与状态语义不变.

## USER_DECISIONS

- 热键用 `s`, 与子命令 `kander start` 同名; 作用于棋盘 (列表) 视图.
- 生效状态为 `backlog` 和 `todo` 两种.
- 启动会认领卡片并拉起 Agent 进程, 因此按下 `s` 不直接执行, 必须先确认.

## EXPECTED_OUTCOME

- 棋盘视图选中一张 `backlog` 或 `todo` 卡按 `s`, 弹出确认: 显示任务 ID, 按 SIZE 解析出的执行 Agent, 解析后的 launcher; `backlog` 卡额外写明会先迁到 `todo` 再启动. `y` 确认, 其它键取消; 取消不产生任何副作用.
- 选中卡在其它状态 (`working` / `review` / `done` / `archived` / `trash`) 时按 `s` 只给一行说明, 不启动, 不改状态.
- 确认后:
  - `backlog` 卡先迁到 `todo`; 进 `todo` 的门禁 (契约四段完整且无占位符, `SELF_REVIEW:` 记录行) 不满足时显示该错误, 不启动, 卡片留在 `backlog`.
  - launcher 解析为 `herdr` / `tmux` / `tmux-session` 时在后台执行, TUI 不被阻塞, 期间可继续操作看板.
  - launcher 解析为 `foreground` / `console` 时拒绝并提示改用终端里的 `kander start`; 不做终端交接.
- 启动成功后刷新看板 (卡片出现在 `working`), 页脚提示成功信息, 含实际 Agent, launcher 与容器地址 (herdr 的 tab/pane, tmux 的 session/window/pane).
- 启动失败时显示失败原因; 卡片状态遵循 `kander start` 既有的回滚语义, 本卡不新增或改变回滚行为.
- 帮助浮层 (`?`) 的棋盘分组列出该热键; 新文案进 `internal/i18n/locales` 的 en / zh-CN / ja 三份目录.

## ACCEPTANCE_CRITERIA

- [ ] 棋盘视图 `s` 对 `backlog` / `todo` 卡弹确认框, 对其它状态只提示; 详情视图与搜索输入态不受影响 (搜索态下 `s` 仍是输入字符).
- [ ] 确认框展示任务 ID, 解析后的 Agent (按卡片 SIZE 走 `kanban_agents` 并回落 `kanban_agent`) 与解析后的 launcher; `backlog` 卡标明先迁 `todo`.
- [ ] 取消路径零副作用: 不迁状态, 不写卡片, 不起进程.
- [ ] `backlog` 卡先执行到 `todo` 的受控迁移, 门禁失败时错误可见且卡片状态不变.
- [ ] `internal/launch` 提供可在进程内调用的启动入口, 返回结构化结果 (任务 ID, 规模, Agent, launcher, 容器地址) 而不是打印到 stdout; CLI `kander start` 改为调用该入口再打印, 输出与退出码与改动前一致.
- [ ] TUI 不向 stdout 直接打印启动结果, 不破坏 alt-screen.
- [ ] herdr / tmux / tmux-session 启动在后台执行, 不阻塞 TUI 输入与刷新; foreground / console 被拒绝并给出改用 CLI 的提示.
- [ ] 启动成功后看板刷新且页脚显示成功信息; 失败时显示失败原因, 不吞错误, 不把失败显示成成功.
- [ ] `kander start` 既有的前置检查, 认领语义与失败回滚行为不变, 并有用例证明该行为未回归.
- [ ] 新增单元测试覆盖: 各状态下 `s` 的行为, 确认与取消, `backlog` 门禁失败, foreground / console 拒绝, 启动成功后的刷新与提示, 启动失败的提示; 外部启动通过注入打桩, 不在测试里真起 Agent 或创建 herdr/tmux 容器.
- [ ] 新增文案在 en / zh-CN / ja 三份 locale 中键一致, `go test ./internal/i18n` 通过.
- [ ] 在模块根运行 `go test ./...` 通过, 记录命令, 提交号与用例数.
- [ ] 帮助浮层含 `s`, `AGENTS.md` 与 README 的相关说明同步更新, 与改动同一 diff.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题: 不改 `kander start` 的前置检查, 认领原语, 容器创建与回滚逻辑, 不改进 todo 的门禁规则; 本卡只增加一个调用入口和 TUI 触发路径, 动这些会扩大回归面.
- 加固: 不加启动频率限制, 不做并发启动互斥或额外授权校验; 认领互斥已由既有的移动原语保证, 重复加锁属于另一议题.
- 共享契约与文档: 不改卡片格式, 不改 `config.json` schema, 不改 `rules/*.md`; `internal/launch` 只做「核心返回结果 + CLI 打印」的拆分, 保持 CLI 输出与退出码不变. 仅同步 `AGENTS.md` 与 README 中与本次行为直接相关的说明.
- 相邻功能: 不做 `resume` / `notify` / `dismiss` 热键, 不做启动前选择 Agent 或 launcher 的选择器 (后续可用 `S` 另开卡), 不做前台启动的终端交接, 不做批量启动多张卡; 用户本次只要求一个启动热键, 其余待确认后另开卡.

## DISCUSSION

- 现有事实: 棋盘按键表在 `internal/tui/app.go` 的 `handleBoardKey`; 已占用 `q h/l/j/k tab 方向键 PgUp/PgDn Home/End / a t - = r o ? y Enter`, 以及已合入的跳窗口热键 `g`; `s` 空闲. 页脚临时提示复用 `CopyNotice` / `CopyNoticeUntil` (`internal/tui/status.go`), 后台任务走 `App.pendingWork` (`internal/tui/program.go`), 结果由 `applyWork` 消费.
- `internal/launch` 的 `commandStart` (`internal/launch/commands.go:61`) 末尾用 `reportLaunch` 直接写 stdout, 在 TUI 里会打乱 alt-screen; 因此需要把「执行」与「打印」拆开, 对外暴露返回结构化结果的入口, CLI 侧 `RunStart` (`internal/launch/cmd.go:96`) 改为调用它再打印. 依赖方向 `tui -> launch` 不成环 (launch 只依赖 `internal/cli` 注册表, `cli` 不反向依赖 tui).
- `backlog -> todo` 的迁移等价于 `kander pick`, 走既有受控迁移入口, 不要在 TUI 里自己拼路径或直接改文件.
- 并行卡: `20260908-options-workflow-flowchart-task` 正在改选项面板 (`internal/tui/options_*.go`), 与本卡的 `handleBoardKey` / 帮助浮层不直接冲突, 但两卡都会改 `internal/i18n/locales` 三份 JSON 与 README; 集成前按 Git 规则同步 `develop` 并解决可能的文本冲突.
- SELF_REVIEW: 已按目标, 已确认方案和项目规则复核: 目标与结果一致, 用户确认的三条 (热键 s, 生效于 backlog/todo, 必须先确认) 都落到 USER_DECISIONS 与验收条件; 后台执行, 拒绝前台 launcher, launch 拆分打印这三条是本次分析给出的方案细节, 写在 EXPECTED_OUTCOME 与 DISCUSSION, 未冒充用户决定. 边界四类均给出取舍与理由, 且没有排除达成目标必需的工作 (launch 的入口拆分是必需项, 已明确写进范围内并要求 CLI 行为不变). 验收条件可执行可判定, 覆盖成功, 取消, 门禁失败, 不支持的 launcher, 启动失败与三语文案. 未发现需要用户新决策的歧义.

## IMPLEMENTATION

2026-09-08：工作目录 `/home/dualf/works/kander/worktrees/board-start-task-key`；集成目标 `develop`；基线 `781aa0dcde99eefe0d7ccea84f5a138f414e831c`。计划拆分 launch 的结构化启动结果与 CLI 打印，TUI 复用受控迁移和后台任务，补充三语文案、回归及文档。审核 PM/QA required；CSA/Hacker 按仓库特例 N/A。


交付与自检（提交 c172eef3b1fe40331445bdea0b8b4025f61b9ce7）：
- 已完成结构化 Start/PreviewStart、CLI 打印适配、herdr 警告回传、棋盘 s 确认及后台启动、受控 backlog 迁移、三语文案与 AGENTS.md/README 同步。
- 自检 1：`git diff --check HEAD~1 HEAD` 无输出，退出 0（c172eef3b1fe40331445bdea0b8b4025f61b9ce7）。
- 自检 2：逐一统计本次 13 个 Go 文件物理行数，全部不超过 1000；最大 internal/tui/app.go 为 825 行（c172eef3b1fe40331445bdea0b8b4025f61b9ce7）。
- 自检 3：逐句核对修改的注释与文档；Start 调用和 CLI 输出分离、后台结果及警告回传、取消/不支持启动器说明与实现一致；rules 按范围未改（c172eef3b1fe40331445bdea0b8b4025f61b9ce7）。
- 自检 4：通过 `rg` 引用检索及 diff 检查，新入口、结果类型、确认/后台回调和文案均有调用；未新增死代码（c172eef3b1fe40331445bdea0b8b4025f61b9ce7）。
- 自检 5：检查新增测试清单，分别覆盖状态/取消、输入上下文/帮助、准备错误、异步回传、backlog 门禁与迁移、窄屏/鼠标、结构化结果、SIZE/default 解析及禁止交互选择；无重复行为测试（c172eef3b1fe40331445bdea0b8b4025f61b9ce7）。
- 自检 6：`go test -count=1 -json ./internal/launch ./internal/tui ./internal/i18n` 在提交后执行，3 包、205 项通过（含子测试）、1 项跳过、0 失败；相关模块编译成功，原有 start 前置检查与回滚用例保持通过（c172eef3b1fe40331445bdea0b8b4025f61b9ce7）。
- 自检 7：模块根 `go test -json ./...` 在提交后执行，20 包、1167 项通过（含子测试）、1 项跳过、0 失败；提交前同命令亦通过。日志分别为 /tmp/board-start-final-targeted.jsonl 与 /tmp/board-start-final-all.jsonl（c172eef3b1fe40331445bdea0b8b4025f61b9ce7）。
- Git：任务提交已推送 origin/board-start-task-key；工作树干净。PM/QA 待审核；CSA/Hacker 按仓库规则 N/A。验收实现自检 13/13；最终结论待审核与集成。

## SUMMARY

