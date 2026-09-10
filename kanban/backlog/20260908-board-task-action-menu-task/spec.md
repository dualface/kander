# Board task action popup

- TYPE: Feature
- SIZE: large
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 16:48
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

看板列表界面现在把「启动任务」和「跳转到 Agent 窗口」各占一个顶层按键 (`s` / `g`), 状态判断藏在按键背后, 其余任务卡操作只能回到命令行。

把这两个按键撤掉, 改为 `g` 打开一个任务操作弹窗: 弹窗按所选任务卡的当前状态列出可执行的操作, 用户在弹窗内选择并执行。这样看板界面能覆盖日常的任务卡流转, 新增操作也不再需要抢占新的顶层按键。

## USER_DECISIONS

- 删除看板列表界面的 `s` (启动任务) 和 `g` (跳转 Agent 窗口) 两个直接按键; `g` 改为打开任务操作弹窗。
- 弹窗提供六项操作: 启动任务卡; 跳转到任务卡 Agent 所在的 tab; 从 backlog pick 到 todo; 从 todo 退回 backlog; 归档未启动或已完成的任务卡; 将未启动的任务卡移入 trash。
- 弹窗只列出当前状态下合法的操作, 不显示灰掉的不可用项。
- 归档只在 backlog / todo / done 三种状态出现。board 层同样允许从 working / review 归档, 但本卡不放开, 那两种状态仍走 CLI。
- 归档与移入 trash 所需的原因 (reason) 与决策依据 (decision reference) 由用户在二级表单中填写, 工具不得代填默认值; 这是 `internal/board/update.go` 中「工具无法验证用户意图」的既有约束。
- 二级表单使用 huh, 与设置面板保持一致。

## EXPECTED_OUTCOME

- 看板列表界面按 `g` 打开任务操作弹窗; 未选中任务卡时给出提示而不是打开空弹窗。
- 弹窗根据所选卡的状态列出可执行项: backlog 卡为启动 / pick 到 todo / 归档 / 移入 trash; todo 卡为启动 / 退回 backlog / 归档 / 移入 trash; done 卡为归档; 卡上存在 `WINDOW` 元数据时另有「跳转到 Agent 窗口」。
- 选择「启动任务卡」进入现有的启动确认框, 行为与原 `s` 一致; 选择「跳转到 Agent 窗口」行为与原 `g` 一致。
- 选择 pick 到 todo / 退回 backlog 后直接执行状态迁移, 完成后看板刷新并给出结果提示; board 校验失败 (例如 todo 需要的 READY 校验不通过) 时原样显示 board 的错误信息。
- 选择归档或移入 trash 后进入二级表单: 归档且来源不是 done 时先选结果 (cancelled / duplicate / wontfix), 结果为 duplicate 时要求填写替代任务 ID; 来源是 done 时结果固定为 completed 并跳过该步; 移入 trash 时结果固定为 `trashed` 且不出现选结果一步 (board 层 `document.go` 要求 trash 的 RESULT 必须是 `trashed`); 随后填写原因与决策依据, 两者都非空才能提交。
- 当前状态下没有任何可执行项时 (例如 archived / trash 上且卡上无 `WINDOW`), 给出「无可用操作」提示而不是打开空弹窗。
- 所有写操作在 UI 之外的工作协程执行, 界面在执行期间不卡死。
- `?` 帮助面板不再出现 `s` 与旧 `g` 两条, 改为一条 `g` 任务操作; README 的常用按键说明 (第 75 行起的全部按键说明, 含第 77 行的 `s` 启动对话框描述) 与 `AGENTS.md` 第 70 行附近 "棋盘视图 `s` 确认后启动" 的接线说明同步更新为新入口。
- 新增的界面文案在 en / zh-CN / ja 三个 locale 齐备。

## ACCEPTANCE_CRITERIA

- [ ] 未选中任务卡时按 `g` 给出提示且不打开弹窗; 当前状态无可执行项时给出「无可用操作」提示且不打开空弹窗; 两者有测试覆盖。
- [ ] `handleBoardKey` 中不再有 `s` 与旧 `g` 的分支, `g` 打开任务操作弹窗; 有测试覆盖。
- [ ] 弹窗菜单项按状态过滤, backlog / todo / working / review / done / archived / trash 各状态下的菜单内容有单元测试覆盖, 包含有无 `WINDOW` 两种情况。
- [ ] 「启动任务卡」进入原有启动确认框, 「跳转到 Agent 窗口」走原有 focus 流程, 两者行为不变且有测试覆盖。
- [ ] pick 到 todo 与退回 backlog 通过 `board.MoveEntry` 执行, 成功后刷新看板并提示, 失败时展示 board 返回的错误; 有测试覆盖成功与失败两条路径。
- [ ] 归档与移入 trash 通过 `board.MoveWithOptions` 执行: 归档传入用户选择的 `Result` (duplicate 时含 `DuplicateOf`), 移入 trash 固定传入 `Result: "trashed"`; 两者都传入用户填写的 `Reason` / `Decision`; 原因或决策依据为空时表单拒绝提交; 成功后卡片带有 `LIFECYCLE_DECISION` 记录; 归档与 trash 各有成功与拒绝测试。
- [ ] 归档在 done 卡上使用 `--result completed` 语义, 在 backlog / todo 卡上使用 cancelled / duplicate / wontfix 之一; 有测试覆盖。
- [ ] 所有 board 写操作经由现有的 `pendingWork` 异步机制, 不在 UI 协程内直接调用 board 事务。
- [ ] `boardHelpGroups` 中 `s` 与旧 `g` 两条合并为一条 `g` 任务操作。
- [ ] README 第 75 行起的常用按键说明 (含第 77 行 `s` 启动对话框一句) 与 `AGENTS.md` 第 70 行附近的 TUI 启动接线说明改为新的 `g` 任务操作入口, 不再出现 `s` 启动。
- [ ] 新增文案在 en / zh-CN / ja 三个 locale 齐备, `go test ./internal/i18n` 通过。
- [ ] 新弹窗 (菜单与二级表单) 纳入 `TestScreensFillBackground`, 背景填充无裸空格。
- [ ] `go build ./... && go vet ./... && go test ./...` 全部通过。

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题: 不修 board 层已有的状态迁移语义与校验规则。本卡只调用现有 `MoveEntry` / `MoveWithOptions`, 因为改动 board 语义会影响 CLI 与协调器, 超出界面改造的范围。
- 加固: 不为归档 / trash 增加额外的二次确认、审计或权限机制。board 已强制原因与决策依据, 再加一层没有已确认的需求支撑。
- 共享契约与文档: 更新 README 的按键说明、`AGENTS.md` 的 TUI 接线说明与三个 locale 的新增文案。不改 `rules/` 下的规则文档 (规则已不含任何 TUI 内容), 也不改 board 的命令契约, 因为按键调整不涉及协议变更。
- 相邻功能: 不改任务卡详情界面的按键、不改设置面板、不动看板列偏移与滚动机制。这些与本次入口调整无关, 合并进来会让改动无法独立验证。

## DISCUSSION

- 入口复用 `internal/tui/overlay.go` 的 `popup` 类型 (标题为任务 ID, body 为菜单, hint 为按键说明), 与帮助面板、启动确认框、设置面板同一套框架与背景填充规则。
- 启动与跳转窗口复用现有实现: `confirmSelectedStart` (`internal/tui/start.go:59`) 与 `focusSelectedTask` (`internal/tui/focus.go:17`), 本卡只改调用入口, 不改这两条流程本身。
- pick 与退回 backlog 对应 `board.MoveEntry(entry, root, "todo"|"backlog")`, 与 `kander pick` 走同一条路径; todo 目标会触发 `validateReady` 与评审记录校验, 失败信息直接透传给用户。
- 归档与 trash 对应 `board.MoveWithOptions`。`internal/board/update.go` 要求目标为 `archived` 或 `trash` 时 `Reason` 与 `Decision` 均非空, 并写入卡的 `LIFECYCLE_DECISION` 记录; `archived` 还要求卡上有合法 `RESULT` (非 done 卡为 cancelled / duplicate / wontfix, done 卡为 completed), `RESULT` 为 duplicate 时还需 `DuplicateOf`。这些是有意的授权记录约束, 界面必须让用户填写而不是代填。
- board 事务会加锁并读写磁盘, 必须走现有 `pendingWork` 异步机制 (启动流程同一套), 完成后刷新看板, 用 `showFocusNotice` 呈现结果。
- i18n 三个 locale 由 `internal/i18n/i18n_test.go` 的 `TestCatalogs` 强制 key 对齐, 新增文案必须三份齐备。
- SELF_REVIEW: 通过。对照用户目标、已确认方案与项目规则复查四点: 目标与结果一致, 六项操作与按键调整均来自用户确认的方案, 未把建议写成用户决策; 边界清晰, OUT_OF_SCOPE 四类各有取舍与理由, 未排除达成目标必需的工作; 约束逐条核对过 `internal/board` 与 `internal/tui` 的实际接口, 与 board 的授权记录要求一致; 验收条件覆盖目标与预期结果且可判定。本轮修正两处遗漏: 补上「未选中任务卡」与「当前状态无可执行项」两种边界的预期行为与验收条件。无需用户新决策的遗留问题。
- 计划: 本卡为 large, 执行者启动时先写 `plan.md`, 建议阶段: (1) 弹窗菜单与状态过滤; (2) 归档/trash 二级表单 (含 trash 固定 `trashed`); (3) 经 `pendingWork` 的异步迁移与结果提示; (4) 启动确认框与 focus 复用; (5) 帮助面板、README、AGENTS.md、i18n; 每阶段附交互验证.
- CARD_REVIEW: 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 对 backlog 全部 12 张卡的独立审核). 发现: (1) large 卡缺 CARD_REVIEW 记录 (`document.go:207` 拒绝进 todo); (2) trash 流程漏了 board 固定要求的 `RESULT: trashed` (`document.go:257`); (3) 排除项漏掉必须同步的 `AGENTS.md:70` 与 README 第 77 行; (4) 缺计划. 四项已修正; 本记录即独立审核结论.

## IMPLEMENTATION



## SUMMARY
