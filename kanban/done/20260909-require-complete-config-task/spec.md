# 操作命令必须读取完整配置，缺文件或损坏则终止

- TYPE: Feature
- SIZE: large
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 15:40
- OWNER: grok
- SESSION: grok 0f2a8c4f-3b02-4867-b197-8f5c7f15ffa2
- WINDOW: herdr:w2T:t12:w2T:p12
- STARTED_AT: 2026-09-09 15:45
- FINISHED_AT: 2026-09-09 16:45
- TASK_BRANCH: require-complete-config
- RESULT: completed

## GOAL

操作类命令必须读取当前作用域的完整配置（作用域 `config.json` 叠项目 overlay，且通过校验）。配置文件不存在或内容无效时，这些命令立即失败退出，不得用默认值继续。`kander doctor` 同样完整读取，但缺文件或损坏时不中断，而是修复。TUI 打开选项面板或按 `s` 启动任务时也必须完整读取；失败则弹出错误画框，不打开表单、不认领任务。

## USER_DECISIONS

- `dismiss` 仍需读配置：它不选执行 Agent / launcher / 模型，但要用 agent 定义（方言、进程名、session 模式、HasAgent）才能正确退出并核验容器。
- `doctor` 完整读取；配置不存在或有错时不退出，而是修复。
- 下列命令必须读取完整配置，缺文件或校验失败一律终止：`config`、`new`、`start`、`resume`、`notify`、`dismiss`、`review`、`check`、`subscribe`、`coordinator`、`dispatch` 的全部子命令。
- TUI 打开选项面板、启动任务时同样读取完整配置；失败显示错误画框。

## EXPECTED_OUTCOME

- 完整配置的唯一读取入口是 `config.Load(false)`：文件必须存在且校验通过，返回作用域文件与 overlay 的合并结果。
- 上列 CLI 命令在配置缺失或校验失败时以非零状态退出，错误指向真实原因，不回落到默认配置，也不经 `Effective(nil)` 在 welcome 未完成时盖掉 agent / launcher / 模型。校验失败包括非法 JSON、根不是对象、以及 schema/取值不合法。
- `kander doctor` 在配置缺失或校验失败时继续运行并调用现有 `config.Repair`：缺文件则创建，非法 JSON 则重建，可解析但不合法的对象按字段修复并保留合法取值，修完后再用修复后的配置探测环境。
- TUI 选项面板在 `Load(false)` 失败时停在已有弹层的错误画框（`loadErr`），不回落到 `DefaultConfig()` 打开表单；成功后仍用 `LoadScope` 编辑，避免 overlay 写回作用域文件。
- TUI 按 `s` 启动时，配置失败留在启动弹层显示错误，不把弹层收成状态栏一行后继续认领。
- `LoadScope` 仍只服务写入路径；`Effective` 只用于 doctor 与 `kander config` 人类可读输出里「当前生效值」的展示。

## ACCEPTANCE_CRITERIA

- [ ] `launch.loadEffective` 与 `config.LoadAgent` 改为 `config.Load(false)`，不再调用 `Effective(nil)`。
- [ ] `kander config`、`new`、`start`、`resume`、`notify`、`dismiss`、`review`、`check`、`subscribe`、`coordinator`、`dispatch`（含 `show` / `fail` / `cancel`）在配置缺失或校验失败时退出码非 0。
- [ ] `kander new` 与启动/通知任务文件语言不再用 `Exists()` 短路推导默认 `agent_language`；无配置即失败。
- [ ] `kander review` 在配置缺失或校验失败时失败退出：不再把 reviewer 默认成 `codex`，也不把报告语言吞成空串。
- [ ] `kander subscribe` 在入口就 `Load(false)`，不拖到探活才读。
- [ ] `kander doctor` 在配置缺失或校验失败时不退出，能写出合法配置并继续环境探测。
- [ ] TUI 打开选项面板：`Load(false)` 失败则弹层显示错误，不打开 Huh 表单。
- [ ] TUI 按 `s` 启动：配置失败时错误留在启动弹层，不认领任务。
- [ ] 选项面板保存路径仍走 `LoadScope`，overlay 值不写回作用域 `config.json`。
- [ ] 针对缺文件、非法 JSON、以及可解析但校验失败的配置，终止/画框行为有测试；原先依赖「无配置即默认」的相关测试改为先写入合法配置。

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题：welcome 未完成但文件合法时是否额外拒绝启动，本轮不改；`Effective` 的 welcome 掩码语义保留给 doctor / `kander config` 展示。
- 加固：不改配置文件权限、DACL、reparse 拒绝规则，也不新增配置加密或签名校验。
- 共享契约与文档：不改 `config.json` schema，不改 CLI 参数。`kander config --json` 在文件合法时仍打印合并后的完整配置；缺文件或校验失败改为终止，这是本轮范围内的行为变更。仅当已发布规则仍写「缺文件用默认」时同步那一句，不另开文档任务。
- 相邻功能：不改 TUI 看板启动时的 `loadPrefs` 回落、不改 `install` 向导、不改 `init` / `list` / `show` / `move` / `pick` / `update` / `guard-write` / `version` / `help` 的无配置可读路径。

## DISCUSSION

用户在会话中确认：操作命令不得用缺省配置继续；doctor 负责修复；TUI 选项面板与启动失败用错误画框。`dismiss` 读配置是因为 agent 定义，不是因为选择 launcher。

完整配置定义为 `Load(false)`，不是 `Load(true)`，也不是 `Effective(nil)`。

- SELF_REVIEW: 通过。目标与用户四条决定一致；边界把 doctor 修复与操作命令终止分开；验收可判定。OUT_OF_SCOPE 中规则书同步写成条件句，不是用户决定，不写入 USER_DECISIONS。
- CARD_REVIEW: 通过（独立 grok 子代理）。首轮 FAIL：验收只写非法 JSON，未覆盖 schema 校验失败。已改为「配置缺失或校验失败」并覆盖可解析但不合法对象。复审 PASS。

## IMPLEMENTATION

2026-09-09: 任务分支 `require-complete-config`，从 `origin/develop` (`211c104282f181d5fabe6cc49b03583b0bed269f`) 创建，worktree `worktrees/require-complete-config/`。

操作命令改为 `config.Load(false)`：`launch.loadEffective`、`LoadAgent`、`kander config`/`new`/`start`/`resume`/`notify`/`dismiss`/`review`/`check`/`subscribe`/`coordinator`/`dispatch`。缺文件、非法 JSON、schema 失败立即非零退出。`new` 与任务文件语言去掉 `Exists()` 短路。`review` 不再默认 reviewer=`codex`、不再把报告语言吞成空串。`subscribe` 在入口读取。`doctor` 改 `Load(false)`，失败走既有 `Repair`。TUI 选项面板 `Load(false)` 失败写 `loadErr` 不打开表单，成功后 `LoadScope(false)` 编辑；按 `s` 配置失败留在启动弹层。已发布规则无「缺文件用默认」句，未改文档。

交付提交 `a8d5a6f86e8a53e7b10826e392c70903875835d9`。交付自检（相对 `211c104282f181d5fabe6cc49b03583b0bed269f`）：

1. `git diff --check` 干净。
2. 本交付未新增超 1000 行源码文件；相对基线的已改文件无「原 ≤1000 现 >1000」或「原 >1000 且净增」。
3. `Load`/`LoadAgent` 注释与实现一致。
4. 已删除 `Exists()` 语言短路与未再使用的 `defaultCardLanguage`。
5. 新测覆盖缺文件/非法 JSON/schema 失败与 TUI 画框；旧「无配置即默认」测试改为先写合法配置。
6. `go test ./... -count=1` 于 `a8d5a6f86e8a53e7b10826e392c70903875835d9` 通过。
7. 同上命令，21 个包全部 ok。

合入：干净 rebase 到当时 origin/develop（ef9f640），无冲突，只重跑 go test ./...。最终交付 fd24781ac0f6abf001e11422d926b566e56623f8 已推送到 origin/develop。审核批次 require-complete-config-1 已关闭（PM/QA PASS；CSA/Hacker N/A）。详见 report.md；审核原文按 run ID 引用。

## REVIEWS

- {"run_id":"require-complete-config-pm-1","batch_id":"require-complete-config-1","role":"PM","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"a8d5a6f86e8a53e7b10826e392c70903875835d9","report":"reviews/require-complete-config-pm-1/report.md"}
- {"run_id":"require-complete-config-qa-1","batch_id":"require-complete-config-1","role":"QA","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"a8d5a6f86e8a53e7b10826e392c70903875835d9","report":"reviews/require-complete-config-qa-1/report.md"}
- {"run_id":"require-complete-config-qa-2","batch_id":"require-complete-config-1","role":"QA","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"fdb68ffdef29749b56a4e7f6464b1206b0213501","previous_run_id":"require-complete-config-qa-1","report":"reviews/require-complete-config-qa-2/report.md"}
- {"run_id":"require-complete-config-pm-2","batch_id":"require-complete-config-1","role":"PM","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"fdb68ffdef29749b56a4e7f6464b1206b0213501","previous_run_id":"require-complete-config-pm-1","report":"reviews/require-complete-config-pm-2/report.md"}
