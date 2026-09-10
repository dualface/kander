# PM 规格验收评审

**Role**: PM（规格验收）
**Commit**: `c54c6d95d3080c38bd57ab02bbd95b2fca63da98`（基线 `8abdfe2e6430a00e19383f84f23088a8ff743be0`）
**Task Context**: 任务卡「TUI 启动对话框标题随阶段更新」(`/tmp/claude-review.4dfde29623b90b32386baa3202d51add/task-spec.md`)，GOAL / USER_DECISIONS / EXPECTED_OUTCOME / ACCEPTANCE_CRITERIA / OUT_OF_SCOPE。
**Reviewed Scope**: 提交内 7 个文件 —— `internal/tui/start_dialog.go`、`internal/tui/start.go`、`internal/tui/start_async_test.go`、三份 `internal/i18n/locales/*.json`、`AGENTS.md`；为确认「不回归」另读了未改的 `internal/tui/start_test.go`、`internal/i18n/i18n_test.go` 与 `renderStartDialog` / `startDialog` 的全部构造与调用点。工作树在目标提交且干净（`git status --porcelain` 无输出）。

## 需求追溯表

| # | 需求（契约出处） | 期望行为 | 代码证据 | 状态 |
|---|---|---|---|---|
| R1 | 读取中标题非确认问句（AC1、EXPECTED_OUTCOME 表） | 标题为 `tui.start_loading` | `internal/tui/start_dialog.go:30-31`；三语值「读取中… / Loading… / 読み込み中…」`zh-CN.json:2`、`en.json:2`、`ja.json:2` | Complete（Observed） |
| R2 | 待确认标题仍为 `tui.start_confirm`（AC2） | ready 走原文案 | `internal/tui/start_dialog.go:39-40`（`default` 分支覆盖 `startReady`）；`start_async_test.go:214,238` | Complete（Observed） |
| R3 | 正在启动标题短且不含任务 ID（AC3、USER_DECISIONS） | 新键 `tui.start_title_starting` | `internal/tui/start_dialog.go:32-33`；值「正在启动… / Starting… / 開始中…」`zh-CN.json:16`、`en.json:16`、`ja.json:16`（无 `{{.V0}}`）；`start_async_test.go:242` 反证不含 `start-task` | Complete（Observed） |
| R4 | 成功标题短文案（AC4） | `tui.start_title_started` | `internal/tui/start_dialog.go:34,38`；「已启动 / Started / 開始済み」`*.json:17` | Complete（Observed） |
| R5 | 失败标题短文案（AC5） | `tui.start_title_failed` | `internal/tui/start_dialog.go:35-37`；「启动失败 / Start failed / 開始失敗」`*.json:18` | Complete（Observed） |
| R6 | 结束态由记录的成败决定，不解析 `dialog.message`（AC6、EXPECTED_OUTCOME） | 落 `failed` 标志并只读该标志 | `internal/tui/start.go:150` `dialog.phase, dialog.message, dialog.failed = startFinished, message, result.err != nil`；`start_dialog.go:35` 仅读 `dialog.failed`，全文件未出现对 `message` 的解析；`start_async_test.go:261-269` 双向验证标志优先于正文 | Complete（Observed） |
| R7 | 新增/复用标题键在 en / zh-CN / ja 键一致（AC7） | 三份 locale 同增 3 键 | 三份 locale 各新增 `tui.start_title_starting/started/failed`；`internal/i18n/i18n_test.go:41-64` `TestCatalogs` 校验数量与逐键存在，实跑 `go test ./internal/i18n -count=1` 通过 | Complete（Observed） |
| R8 | 窄终端标题仍走 `ansi.Wrap`，不横向溢出（EXPECTED_OUTCOME） | 标题继续经 `ansi.Wrap(clean(title), inner, "")` | `internal/tui/start_dialog.go:87`；宽度计算 `:84` 未变，新标题在三语下均短于原确认问句，无新增溢出面；`start_async_test.go:271-280` 在 `Width=12` 下逐 phase 断言无超宽行 | Complete（Observed） |
| R9 | 启动语义、异步预览、选卡丢弃、滚轮、结果任意键关闭不变（USER_DECISIONS、AC8） | 行为零改动 | 本提交对 `start.go` 只改 `:150` 一行赋值；`confirmSelectedStart`/`applyStartPreview`/`handleStartConfirmation`（`start.go:59-128`）与 `handleStartMouse`（`start_dialog.go:116-135`）未变；`renderStartDialog` 仅新增 `title` 形参，唯一调用点 `start_dialog.go:73`；`startDialog` 唯一构造点 `start.go:71`，`failed` 零值 false；既有 `TestStartPreviewDiscardsLateResults`、`TestStartDialogRetainsAsyncProgressAndResultUntilKey` 等实跑通过 | Complete（Observed） |
| R10 | 模块根 `go test ./...` 通过（AC9） | 全量绿 | 本次在目标提交实跑 `go test ./... -count=1`，无 FAIL 输出；`go test ./internal/tui ./internal/i18n -count=1` 亦通过。用例数 1270 属卡内记录，本轮未逐条重数（Unverifiable，不构成需求缺口） | Complete（Observed） |
| R11 | `AGENTS.md` TUI 段仅在需要时补一句（OUT_OF_SCOPE 允许项） | 一句描述标题随阶段变化 | `AGENTS.md:70` 新增 "with the frame title tracking loading, confirmation, starting, and the recorded success or failure"，与 `startDialogTitle` 的四分支＋成败一致，未越界改其它段 | Complete（Observed） |

**完成度统计**：Complete 11，Partial 0，Missing 0，Contradicted 0，Unverifiable 0。

## 补充核验（无发现，仅记录已排除的风险面）

- 死代码：`failed` 在 `start.go:150` 写、`start_dialog.go:35` 读；三个新键各有唯一调用点。带任务 ID 的 `tui.start_starting` 仍作 running 态 hint（`start_dialog.go:70`），未被新键取代而变成死键。（Observed）
- 结束态入口唯一：全仓仅 `start.go:150` 一处置为 `startFinished`，故不存在「进入结束态但 `failed` 未被写」的路径。（Observed）
- 文档一致性：仓库内无其它描述「框顶固定为确认问句」的文字，`AGENTS.md:70` 修改后无残留过时描述。（Observed）

## FINDINGS

无。本轮未发现阻断、高或中级问题。

## NON-BLOCKING

NON-BLOCKING: none

```kander-findings
{"FINDINGS":[],"NON_BLOCKING":[]}
```

任务文件 `/tmp/claude-review.4dfde29623b90b32386baa3202d51add/prompt.txt` 已删除。