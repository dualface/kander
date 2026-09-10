# 执行交付报告

- 任务：20260908-agent-definition-embed-task。
- 日期与作者：2026-09-09，Codex；接管 sync `6e27a14f0a242cc189dc40767cfa8889`，epoch 10。
- 最终交付：`b216ca25cb328300b3a45c54bc00a5ca442a89d7`，分支 `agent-definition-embed`；工作树 `/home/dualf/works/kander/worktrees/agent-definition-embed`。
- 本轮基线：组分支 `d97964c06adb942b94b25c7e5c5bfb04795172e4`；旧实现已在该组提交中。新测试提交已推送任务分支，等待编排器快进接收。

## 结果与偏差

四个内置 agent 的定义迁移、pane 投递、安装和菜单元数据迁移沿用已交付实现。本轮从原始处置、代码与 Git 重建进度，补齐此前未被测试固定的验收证据：完整配置/安装基线字节、接管失败回滚、投递与会话发现顺序、两段就绪的禁止早投递断言、herdr 内置 argv、tmux notify 直投以及前置拒绝 revision。没有改生产行为。

16/16 条已完成执行侧逐项自验；该数字不表示组审核计划关闭或可以进入 done。Windows 原生运行按合同不适用，交叉编译通过。完整命令、测试计数、旧基线对照、文件行数及原始日志位置见 [核验记录](verification/takeover-10.md)。

## 验收映射

1. **定义文件与结构拒绝**：四份 agents/*.json 均 schema_version=1；TestEmbeddedUnknownSchemaRejected 与新增 TestEmbeddedInvalidStructureNamesFileAndField 校验文件名及字段错误。
2. **按 agent 名业务分支**：rg 扫描 config/launch/install/menu 非测试 Go 文件，仅剩 session.go:90 的 Cursor 分配与 agents.go 的 session 方言兼容分支，均在后续 A3 的允许清单；三张旧默认表已删除。
3. **内置 argv**：TestBuiltinAgentArgumentsMatchPreChangeOutput 固定四个 agent × start/resume × large/small × 有/无 session 的 32 组合。
4. **prompt_delivery 校验**：TestPromptDeliveryValidation 覆盖模式、ready、超时、regex、blocked reason；本轮补嵌入结构错误的文件/字段检查。
5. **投递入口与时序**：TestPaneDeliveryHerdrAndTmux、TestTmuxPaneDeliveryWaitsBeforeSessionDiscovery；内置 argv 不调用 pane 投递现覆盖 tmux 和 herdr。
6. **两段就绪**：强化 TestPaneDeliveryTwoStageReady，断言 TUI 未就绪时 prompt 文件不存在；tmux 单独覆盖两次 capture 与 send 顺序。
7. **三种失败与优先级**：blocked 与 ready 同时出现时短路优先；herdr 拒绝与纯超时用例通过；不为 tmux 虚构拒绝原语。
8. **失败诊断与回滚**：start 原文/SESSION/WINDOW 与 todo 位置回滚断言保留；新增接管入口对 herdr 三原因、tmux 两原因验证诊断、关闭和原状态/原 owner/session/window 恢复。resume 按命令协议恢复原 review 状态，不把已开始卡错误退回 todo。
9. **foreground/console 前置拒绝**：现有 foreground 端到端测试新增 revision 不变断言；console 由分发层单测覆盖，原生运行不在本卡范围。
10. **resume 与 notify**：新增 TestPaneTakeoverFailuresRestoreOriginalCard；notify 直投测试扩到 herdr/tmux，区分投递后的 marker capture 与投递前就绪等待。
11. **发布规则**：TestKanbanRulesKeepArgvPromisesAndDocumentPane 通过；改后原文见下节。
12. **旧自定义行为**：原 config/launch 自定义 dialect、session、argv 与路径测试保留期望值，全量通过。
13. **配置/安装字节兼容**：旧基线 CLI 与本轮 CLI 两种配置逐字节一致；固定 minimal fixture 基线；16 组安装路径/全文/识别/去重矩阵在旧基线与本轮均通过。
14. **三层校验**：嵌入 discovered、用户 discovered 拒绝及缺版本旧配置测试通过；空 PATH 下实际 config --json 与临时看板 list 成功；用户显式不可执行 path 既有拒绝测试保留。
15. **构建与测试**：go build ./...、go vet ./...、go test -json ./...、GOOS=windows go build ./... 全通过；21 包、1459 项通过、1 项原生 Windows 跳过。
16. **文档与包表**：docs/custom-agents.md 已记录嵌入位置、版本、优先级及 pane；公共解析只引用 docs/output-parsing.md；AGENTS.md 包表已有 agents/ 目录。

## 发布规则改后原文

以下摘自当前交付 `rules/KANDER-KANBAN-RULES.md`。argv 原有承诺保留，pane 条款另加；herdr 与 tmux 的 started 条件分开。

原文件第 132 行：

> Templates replace `{model}`, `{effort}`, `{session}` within argv elements; Kander appends the prompt last. No shell interpolation is used. `{session=}` keeps an empty session value instead of dropping the element. When `prompt_delivery.mode` is `pane`, the prompt is not appended to argv: after `pane run`, Kander waits for the agent TUI using the definition's `ready` / `blocked` marks (`blocked` is checked in parallel and wins immediately), then delivers the prompt with the same primitives as notify (herdr `agent prompt`, tmux `send-keys -l` plus a separate Enter).

原文件第 174 行：

> The agent command line receives only one instruction containing that absolute path.

原文件第 175 行：

> When `prompt_delivery.mode` is `pane`, that instruction is not an argv element: it is delivered after the agent TUI is ready, as specified under Start Checks and Rollback.

原文件第 548 行：

> An agent whose `prompt_delivery.mode` is `pane` is rejected before claiming when the resolved launcher is `foreground` or `console`; the card stays in `todo/`.

原文件第 550 行：

> For `prompt_delivery.mode` `pane`, a second-stage TUI ready failure (`blocked` match, prompt-delivery rejection, or ready timeout) is the same class of failure: capture the pane output, close this invocation's tab/window, restore the document, and move back to `todo/`. Do not keep the container or write `WINDOW`/`SESSION` from that attempt. Failures that need a person to answer a dialog say to run that CLI once manually and then retry `kander start`; a pure ready timeout reports the captured pane output without that instruction.

原文件第 554 行：

> When `prompt_delivery.mode` is `pane`, tmux/tmux-session count as started only after prompt delivery succeeds and session discovery and pane marker write succeed.

原文件第 562 行：

> When `prompt_delivery.mode` is `pane`, herdr counts as started once `pane run` succeeds and prompt delivery succeeds.

原文件第 563 行：

> The temporary task file of `start` contains only the task ID and fixed requirements; the agent command line receives only one instruction to read that file.

原文件第 564 行：

> When `prompt_delivery.mode` is `pane`, that instruction is not an argv element: it is delivered after the agent TUI is ready.

## 审核与处置

- PM：原 `pm-agent-def-b1-r4` 与 `pm-agent-def-b1-r6` 由 Claude/opus 成功执行。PM-01、PM-02、PM-09 已有修复与后轮核验；PM-10 文档机械修复在组上已有，原作者记录保留。本轮测试提交尚未被组接收及纳入审核证据，不能宣称最终 PASS。
- QA：原 `qa-agent-def-b1-r6` 由 Grok/grok-4.6 成功执行，QA-01 与 PM-01 同根因，已有修复记录；最新 `qa-agent-def-b1-r7` 使用 Grok 启动 gpt-6-astra，返回 `unknown model id`，退出码 1，semantic_status=unassessed。由编排器恢复合法调用及既有审核计划，本卡不自行触发组审核。
- CSA、Hacker：均 N/A，依据仓库 AGENTS.md 明确例外。
- 原作者 originals、REVIEWS 和 dispatches 保留；本次未覆盖、冒签或重新打开已核验 finding。

## 未解决项

- [PM][low/rejected] PM-06：自定义 agent 未配置规则目标时，doctor/面板不显示该规则入口集成状态；原作者依据空 rules_target 不集成的契约拒绝 dialect 回落。保留供用户复核，见 `reviews/pm-agent-def-b1-r4/dispositions/pm-06-reject-r1.json`。
- [QA][N/A/N/A] 最新审核执行失败，非语义结论；错误为模型与 reviewer 不匹配，见 `reviews/qa-agent-def-b1-r7/sidecar.json` 与 `error.log`。编排器完成合法重试及批次门禁后解除。
- [交付自查][N/A/N/A] 组全区间 `git diff --check` 检出 A2 文件 `internal/review/definition_unix_test.go:353` 尾部多余空行；本轮差异无此问题。交原卡执行者处理，见 `verification/takeover-10.md`，no run produced。
- [组级待办][N/A/N/A] 新提交待快进接收，`agent-def-embed-cycle` 尚 unsealed、`agent-def-batch-one` 未关闭，A3 后续批次与 develop 集成、wrap-up 尚未完成。由编排器继续；任务与组分支、工作树均保留。本轮 sync 没有 wrap-up 授权。

## 交付结论

本轮执行者补证工作完成，返回 review 等待组级接收与审核。不得将 accepted/completed 的 sync 回执当作 develop 集成或最终验收。临时 takeover 指令文件在本轮结束时按要求尝试删除；审核 originals 不清理。

## 2026-09-09 作者处置映射补录（epoch 12）

已按主控 sync 追加 5 条 codex 原件，previous_record_id 分别连接 PM-01、PM-02、PM-09、PM-10、QA-01 的旧原件；状态均保留 fixed，fix_commit 改绑经核验的组内真实祖先。23 项历史提交定向测试/子测试通过；五份旧作者原件字节不变。完整路径、SHA、命令与独立核验见 [映射核验记录](verification/disposition-map-12.md)。

当前交付仍为 `b216ca25cb328300b3a45c54bc00a5ca442a89d7`，本轮无新代码提交。新交付排队，旧批次由编排器在 d97964c 闭合。本轮未改变已有其他遗留项的处置状态，不宣称组级门禁已完成。

## 2026-09-09 最终集成与收尾（epoch 13）

本节接续前述历史记录，当前结论以此及本轮完成回执为准。执行侧 16/16 项验收完成；任务交付 `b216ca25cb328300b3a45c54bc00a5ca442a89d7` 已纳入审核组源 `64d0c2cf5fa2e8c8930f56967dced09a2187478d`，并随用户授权 merge `a8b5afac5d79a09399ab12b097691d1f2212c1dd` 推送 develop；本地主工作树已同步且干净。PR N/A。

两批 PM/QA 均 PASS、CLOSED；第一批 PM Claude/QA Grok，第二批 PM/QA Codex gpt-6-astra high。CSA/Hacker 按仓库例外 N/A。合并后追加审核按用户明确要求跳过，未记作新 PASS。旧 QA 模型失败已有合法成功链；PM-10 机械修复、五条作者处置映射及 A2 空行后续处理均保留可追溯原件。旧“待审核/集成”等表述仅记录当时状态。

复核最终合并树既有日志：`go test -json -p 2 ./... -count=1`，21 包、1682 pass、0 fail、1 skip；`go build ./...`、`go vet ./...`、`GOOS=windows go build ./...` 均退出 0。已核对测试日志摘要、SHA-256 和合并树；本轮没有重复运行整仓测试。

本卡工作树、已交付本地分支和远端分支已按身份/SHA 核验后删除；临时 takeover 提示词已清理；审核/dispatch originals 与验证日志保留。Codex 会话/herdr tab 按要求保留。组级资产由主控负责。本轮使用绑定组源 SHA 完成 done，并由后续定向 check 核验；持久回执为最终卡状态依据。

当前未解决项（2）：

- [PM][low/rejected] PM-06：rules_target 空值表示不集成，不按 dialect 回落；影响为未配置目标的自定义 agent 不显示规则入口集成状态，原因是已接受契约取舍。原件 `reviews/pm-agent-def-b1-r4/dispositions/pm-06-reject-r1.json`。
- [验证边界][N/A/N/A] 原生 Windows 未运行，仅交叉构建及平台分发测试；真实 Windows console 行为仍缺原生证据。见 `verification/wrap-up-13.md` 与所引测试日志；no run produced。

完整集成 SHA、审核闭合原件、验证日志指纹与清理命令见 [最终收尾核验](verification/wrap-up-13.md)。
