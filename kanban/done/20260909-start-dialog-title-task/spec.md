# TUI 启动对话框标题随阶段更新

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 10:41
- OWNER: cursor
- SESSION: cursor 85be76e0-b227-4fe8-b7dc-33d9e55dc06c
- WINDOW: herdr:w2T:t7:w2T:p7
- STARTED_AT: 2026-09-09 10:42
- FINISHED_AT: 2026-09-09 11:25
- TASK_BRANCH: start-dialog-title
- RESULT: completed

## GOAL

看板按 `s` 启动 backlog/todo 任务卡时，对话框标题随启动阶段立刻更新，不再始终显示确认问句。

现状：`renderStartDialog`（`internal/tui/start_dialog.go`）把 `frame.Title` 写死为 `t("tui.start_confirm")`。正文和底部提示已按 `startLoading` / `startReady` / `startRunning` / `startFinished` 变化，标题没有。读取中、启动中、成功或失败时，框顶仍是「启动所选任务？」。

## USER_DECISIONS

- 确认方案并用看板（建卡并启动）。
- 标题随 phase 立刻切换；文案短、不含任务 ID（ID 已在正文）。
- 结束态标题区分成功与失败。
- 新增独立标题文案，不复用带任务 ID 的 hint（例如 `tui.start_starting`）。
- 启动语义、异步预览、选卡丢弃、滚轮、任意键关闭结果，都不改。

## EXPECTED_OUTCOME

按 `s` 弹出的启动对话框，框顶标题与当前阶段一致，且在该阶段的当帧即可读到：

| 阶段 | 标题（zh-CN / en / ja） |
|---|---|
| 读取预览 | 读取中… / Loading… / 読み込み中… |
| 待确认 | 启动所选任务？ / Start selected task? / 選択したタスクを開始しますか？（现有 `tui.start_confirm`） |
| 正在启动 | 正在启动… / Starting… / 開始中… |
| 成功 | 已启动 / Started / 開始済み |
| 失败 | 启动失败 / Start failed / 開始失敗 |

- `renderStartConfirmation` 按 `dialog.phase` 选标题；结束态用 `applyStartResult` 记下的成败，不解析正文。
- 新增独立标题键（读取中可复用已有短句 `tui.start_loading`；其余新增专用标题键），en / zh-CN / ja 三份 locale 键一致。
- 窄终端下标题仍走现有 `ansi.Wrap`，不横向溢出。
- 正文、hint、确认/取消/启动中吞键/结果任意键关闭的行为与改动前一致。

## ACCEPTANCE_CRITERIA

- [ ] 读取中态渲染可见读取中标题，且不是确认问句。
- [ ] 待确认态标题仍为现有 `tui.start_confirm`。
- [ ] 正在启动态标题为短「正在启动」类文案，不含任务 ID。
- [ ] 启动成功后标题为短「已启动」类文案，不是确认问句。
- [ ] 启动失败后标题为短「启动失败」类文案，不是确认问句。
- [ ] 结束态标题由记录的成败决定，不解析 `dialog.message`。
- [ ] 新增或复用的标题文案在 en / zh-CN / ja 三份 locale 中键一致，`go test ./internal/i18n` 通过。
- [ ] 现有启动语义、异步、选卡丢弃、滚轮与结果关闭用例不回归。
- [ ] 在模块根运行 `go test ./...` 通过，记录命令、提交号与用例数。

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题：不改 `launch.Start` / `launch.PreviewStart` 的语义、认领、回滚与页脚结果文案；本卡只改对话框标题随阶段更新。
- 加固：不加启动的额外授权或并发互斥。
- 共享契约与文档：不改卡片格式、配置 schema 与 `rules/*.md`；仓库 `AGENTS.md` 的 TUI 说明仅在需要描述标题随阶段变化时补一句。
- 相邻功能：不改选项面板、帮助弹窗或其它热键对话框的标题；不改启动正文段落、hint 键位或结果 viewport。

## DISCUSSION

- 现有事实：`internal/tui/start_dialog.go` 的 `renderStartDialog` 将 `frame.Title` 固定为 `t("tui.start_confirm")`；phase 枚举在同文件；结果写入在 `internal/tui/start.go` 的 `applyStartResult`。现有测试 `TestStartDialogParagraphAndFooterLayout` 只断言 ready 阶段标题为确认问句。
- 读取中标题复用 `tui.start_loading`（「读取中…」）是因为它已经是短状态句，且当前只作 Agent/launcher 占位，语义兼容；正在启动/已启动/启动失败另建标题键，避免与带任务 ID 的 `tui.start_starting` 或带原因的 `tui.start_failed` 混用。
- 结束态需要在 dialog 上记录成败（例如 `failed bool`），因为 `startFinished` 本身不区分成功与失败。
- SELF_REVIEW: 已按用户目标、已确认方案和项目规则复核。目标与结果一致：标题随四阶段加成败切换，用户确认的短文案、不含任务 ID、独立标题键、不改启动语义全部落入 USER_DECISIONS 与验收条件。结束态用记录的成败而非解析正文是方案细节，写在 EXPECTED_OUTCOME，未冒充用户决定。边界四类均给出取舍，未排除达成目标必需的工作。验收条件可执行可判定，覆盖五类标题、三语文案与既有路径不回归。未发现需要用户新决策的歧义。

## IMPLEMENTATION

- 2026-09-09：任务分支 `start-dialog-title`，工作树 `/home/dualf/works/kander/worktrees/start-dialog-title`，基线 `8abdfe2e6430a00e19383f84f23088a8ff743be0`（创建时的 `origin/develop`）。
- `startDialog` 增加 `failed`；`applyStartResult` 按 `result.err != nil` 写入结束态成败，不解析 `dialog.message`。
- `startDialogTitle` 按 phase / failed 选标题：读取中复用 `tui.start_loading`，待确认复用 `tui.start_confirm`，其余为专用键 `tui.start_title_starting` / `tui.start_title_started` / `tui.start_title_failed`（en / zh-CN / ja 键一致）。
- `TestStartDialogTitleFollowsPhase` 覆盖 loading / ready / running / 成功 / 失败标题、失败标志优先于正文、窄屏标题不溢出。
- 仓库 `AGENTS.md` TUI 段补一句：框顶标题随读取、确认、启动中与记录的成败切换。
- 交付提交：`c54c6d95d3080c38bd57ab02bbd95b2fca63da98`。
- 交付自检（提交 `c54c6d95d3080c38bd57ab02bbd95b2fca63da98`）：
  1. `git diff --check origin/develop...HEAD` 干净。
  2. 触及文件均未超过 1000 行：`start_dialog.go` 118→135，`start.go` 191→191，`start_async_test.go` 229→290，三份 locale 各 928。
  3. `AGENTS.md` 已同步标题随阶段变化；无过时注释。
  4. 无死代码：`failed` 与新标题键均被读写。
  5. 无冗余测试：新用例只断言标题与失败标志，不重复既有启动语义。
  6. `go test ./internal/tui ./internal/i18n -count=1` 于该提交通过。
  7. `go test ./... -count=1` 于该提交通过，用例 1270。
- 审核：计划 `start-dialog-title-plan`，批次 `start-dialog-title-batch` 已关闭。结论见 run `start-dialog-title-pm-2`（PM PASS）与 `start-dialog-title-qa-6`（QA PASS）；CSA / Hacker 按本仓库 AGENTS.md 为 N/A。失败 run 由 `resolved_failures` 指向上述成功 run，勿把报告贴进卡片。
- 已快进合入 `origin/develop` 与本地 `develop`；任务分支与工作树已清理。`git merge-base --is-ancestor c54c6d95d3080c38bd57ab02bbd95b2fca63da98 origin/develop` 成立。

## SUMMARY

按 `s` 弹出的启动对话框框顶标题已随读取中 / 待确认 / 正在启动 / 成功 / 失败在当帧切换；结束态只读 `applyStartResult` 记录的 `failed`，不解析正文。交付提交 `c54c6d95d3080c38bd57ab02bbd95b2fca63da98` 已在 `develop`。`go test ./... -count=1` 于该提交通过，用例 1270。验收 9/9。无实现偏差。

未决：
- [QA][suggest][rejected] reviews/start-dialog-title-qa-6/dispositions/qa-n1-author-first.json

## REVIEWS

- {"run_id":"start-dialog-title-pm-1","batch_id":"start-dialog-title-batch","role":"PM","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"c54c6d95d3080c38bd57ab02bbd95b2fca63da98","report":"reviews/start-dialog-title-pm-1/report.md"}
- {"run_id":"start-dialog-title-qa-1","batch_id":"start-dialog-title-batch","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"c54c6d95d3080c38bd57ab02bbd95b2fca63da98","report":"reviews/start-dialog-title-qa-1/report.md"}
- {"run_id":"start-dialog-title-qa-2","batch_id":"start-dialog-title-batch","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"c54c6d95d3080c38bd57ab02bbd95b2fca63da98","report":"reviews/start-dialog-title-qa-2/report.md"}
- {"run_id":"start-dialog-title-pm-2","batch_id":"start-dialog-title-batch","role":"PM","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"c54c6d95d3080c38bd57ab02bbd95b2fca63da98","report":"reviews/start-dialog-title-pm-2/report.md"}
- {"run_id":"start-dialog-title-qa-3","batch_id":"start-dialog-title-batch","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"c54c6d95d3080c38bd57ab02bbd95b2fca63da98","report":"reviews/start-dialog-title-qa-3/report.md"}
- {"run_id":"start-dialog-title-qa-4","batch_id":"start-dialog-title-batch","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"c54c6d95d3080c38bd57ab02bbd95b2fca63da98","report":"reviews/start-dialog-title-qa-4/report.md"}
- {"run_id":"start-dialog-title-qa-5","batch_id":"start-dialog-title-batch","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"c54c6d95d3080c38bd57ab02bbd95b2fca63da98","report":"reviews/start-dialog-title-qa-5/report.md"}
- {"run_id":"start-dialog-title-qa-6","batch_id":"start-dialog-title-batch","role":"QA","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"c54c6d95d3080c38bd57ab02bbd95b2fca63da98","report":"reviews/start-dialog-title-qa-6/report.md"}
