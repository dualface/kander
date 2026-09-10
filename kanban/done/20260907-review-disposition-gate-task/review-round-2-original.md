First read /home/dualf/.agents/KANDER-AGENTS.md, run kander config --json to check the current rules selection, load only enabled modules as needed, then read the command contract /home/dualf/.agents/KANDER-KANBAN-RULES.md. If configuration cannot be read, stop affected Kander operations and report the error. For disabled modules, follow existing user and project rules, not Kander requirements retained from an earlier session. Communicate with the user and write all card content, records and reports in "zh-CN". Commit messages and code comments follow the project's conventions. 

The card is still in review; first run kander move 20260907-review-disposition-gate-task working to move it back to working, then handle the items.

# R 第二次修复派回

任务20260907-review-disposition-gate-task。先move working，独立核实本轮两角色报告及全部NON-BLOCKING。

已知事实：增量target55e18dc727730d0bab164ce67d2bc235fb1ec8c5、base8241a49b1f50cbb99acc68b4c3dc58b5a803253e、reviewed-commitd325bb9755cba0ac704a6841cd1d2d786eed42ee，两角色Claude已exit0。QA所有旧项闭合且无gate；PM旧六项闭合，PM01 high残留为格式合法但lineage关系错误的ok报告仍不可替代。组干净且HEAD为target。QA通过结论保留，主控不重复跑已通过角色；PM修复后再增量。

请实测PM01四类关系错误（无前驱却有lineage、重复指向、错误前驱条目、复用ID漏lineage）及显式替代后闭批的合法流程，原件保留、不放宽关系校验、不把不可变坏报告变成假PASS。主控尚未替你确认或指定修法。PM08/QA05未建计划advance提示与QA06自动上下文prompt显示也逐项核实处置；非阻断建议不要求采纳，不扩张范围。

这是PM01首轮修复后首次仍未闭合，本次为第二次修复尝试；请完整核实同类失败入口及实际恢复，不只补一个fixture。若复审仍不能闭合同一问题，主控将依规则停止循环并向用户报告，不换作者或代修。

保持同base/spec契约。受控保存全部原文、逐条作者事实/修复/拒绝依据、最新完整SHA、验证与组基线；任务分支提交/推送、rebase最新组HEAD并重验，再move review。你不审核、不改组分支、不集成/清理，不部署或迁移真实看板。Windows原生缺口继续保留。后续PM reviewed-commit为本target。

## PM 增量原报告

Write/Edit 在本会话受限，且 Reviewer 不得改写仓库，故审核报告直接在此交付。任务文件 `/tmp/claude-review.c3a7ea7df38a7bac3017dde018b344df/prompt.txt` 因无写工具无法删除；按任务文件说明，这不影响结果。

---

**Role**: PM（规格验收 · 增量复审）
**Commit**: `55e18dc727730d0bab164ce67d2bc235fb1ec8c5`（base `8241a49b1f50cbb99acc68b4c3dc58b5a803253e`，reviewed-commit `d325bb9755cba0ac704a6841cd1d2d786eed42ee`）
**Task Context**: `/tmp/claude-review.c3a7ea7df38a7bac3017dde018b344df/task-spec.md`（`20260907-review-disposition-gate-task`，任务组 `20260907-review-archive-group`）
**Reviewed Scope**: FIX RANGE FILE LEDGER 全部 24 个路径（`internal/board/{review_closure,review_context_merge,review_disposition,review_gate,review_mechanical,review_plan,review_plan_extend,review_plan_read,reviews,deps}.go`、`internal/review/{archive,disposition,mechanical}.go`、`docs/review-{disposition,evidence}.md`、`rules/KANDER-{REVIEW,KANBAN,TASK-GROUP}-RULES.md` 及新增/修改测试）。为判定修复影响追踪了未改动的消费者：`internal/board/{review_findings,review_context,review_advance,snapshot}.go`、`internal/review/{validate,review,roles,args}.go`。d325bb9 之前已接受且本轮未变更的代码不重审，仅用于判断修复影响。

## 首轮 finding 处置核验

| ID | 原等级 | 作者声明 | 55e18dc 实测 | 结论 |
| --- | --- | --- | --- | --- |
| PM-01 | high | fixed | `internal/board/reviews.go:500-506` 在 finalize 对 `FindingsSchema>0` 的 ok 运行调用 `ParseReviewFindings`，失败即改写 `ExecutionStatus="failed"`、`ExitCode=1`、`FailureReason`；`internal/review/archive.go:217-219`、`:266` 使 CLI 以非零退出并保留原件，随后可经 `resolved_failures` 显式替代。但 `ParseReviewFindings` 之外的结构校验（lineage）仍只在聚合期执行，同类永久陷阱残留（见 PM-01 残留项） | **Partial** |
| PM-02 | medium | fixed | `internal/review/disposition.go:221-224,242-246` 取 `arguments[5]` 作补充文本并经 `board.MergeReviewContext` 与自动原件按字节长度拼接；`internal/board/review_context_merge.go:15-17,20-33` 长度前缀分隔，补充文本可含分隔符本身；`internal/board/reviews.go:389-395` 改为只对 `source` 复校自动来源；replay 分支 `:234-241` 拒绝变更的补充文本。调用方上下文不再被静默丢弃 | **Closed** |
| PM-03 | medium | fixed（替代实现） | 未采用"新周期换计划指针"，改为原计划内重绑定：`internal/board/review_gate.go:74-92` 对全体成员比对 `planCycle(card)`，不一致时输出 `RebindCycles` 并置 `requirements-needed`（completion 时报错）；`internal/board/review_plan_extend.go:24-29,49-77,133-140` 以 `maps.Equal(changed, x.RebindCycles)` 做全量 CAS 重绑定，禁止同时改批次/seal，旧计划整份进 `plan-history`，同周期请求被拒。恢复路径可走完：`internal/review/disposition_cycle_test.go:22-152` 经真实 CLI 验证重绑定后旧失败仍须 `resolved_failures`、旧作者原件字节不变、终态成员不动、两卡最终可 done。契约面等价满足 AC8"升级后的活动执行周期需建立审核要求" | **Closed** |
| PM-04 | medium [mechanical] | fixed | `rules/KANDER-REVIEW-RULES.md:161`、`:240`、`:260` 均已重写为"task 绑定增量可省略 reviewed-commit、显式值须匹配、前驱须同 batch/base/role/reviewer、调用方 review-context 为逐字补充"，与 `:588-593` 新章节及 `internal/review/disposition.go:207-246`、`internal/board/reviews.go:378` 一致，同文件不再自相矛盾 | **Closed** |
| PM-05 | low | fixed | `internal/review/disposition.go:57-59`（advance）、`:74`（extend-plan）经 `verifyPlanCWD`（`:248-256`）读取不可变计划身份并精确比对 CWD；`internal/review/disposition_regression_test.go:81-99` 用真实第二 worktree 验证拒绝。边界见 PM-08 | **Closed** |
| PM-06 | suggest | fixed | `internal/board/review_plan.go:199-208` 每次计划副本验证都解码 `tracked-cycles/<id>.json` 并与 `p.Cycles[id]` 比对，缺失/不一致为结构错误；`:209-211` 才是周期变更判定，二者明确区分；`internal/board/review_recovery_test.go:103-118` 验证损坏 tracker 不能靠重绑定洗掉 | **Closed** |
| PM-07 | recommend | fixed | `internal/review/disposition.go:7,215` 改用 `slices.Equal`，`sameTasks` 已删除 | **Closed** |

**状态统计**：Closed 6 / Partial 1 / Open 0（共 7 项）。原报告 R-14（`go test`/`-race`/build/vet 全量验证）本轮仍为 **Unverifiable**：本次审核只有 Read/Grep/Glob，未执行任何命令，作者验证记录仅作声明。Windows 原生缺口按调用方交代保留，不据交叉编译冒称实测。

## 门禁问题

### PM-01 — high：残留项——lineage 结构不合法但 `execution_status=ok` 的报告，仍会永久堵死整个批次与全部成员卡

- 声明类型：代码路径与后果为 Observed；触发频率为 Inferred（有充分支撑：报告由 LLM 自由生成，lineage 规则正是工具写进 prompt 要求其自行填写的字段）。
- 证据：
  - finalize 期只做**解析级**校验：`internal/board/reviews.go:500-506` 仅调用 `ParseReviewFindings`。该函数对 lineage 只检查格式（`internal/board/review_findings.go:76-78`：`item.Lineage != nil && (!ValidReviewID(item.Lineage.RunID) || !findingIDPattern.MatchString(item.Lineage.FindingID))`），**不做任何前驱关系判定**，因此下列报告一律以 `execution_status="ok"` 归档。
  - 关系级校验只发生在聚合期 `internal/board/review_closure.go:132-188`，四条出错路径均可由格式合法的报告触发：
    - `:155-159` `reused finding ID requires explicit lineage`——增量轮沿用上一轮 ID 却漏写 `lineage`；
    - `:161-163` `finding lineage must reference unique immediate predecessor item`——首轮报告写了 lineage（此时 `run.PreviousRunID == ""`），或两条目指向同一前驱；
    - `:183-185` `lineage item not in predecessor`——前驱条目 ID 写错。
  - 错误向所有出口传播且无取代通道：`review_closure.go:117-119`（aggregate 对批次内**每个 ok 运行**调用）→ `ReadReviewBatchView`/`PublishReviewDisposition`/`CloseReviewBatch` 全部失败；`internal/board/review_gate.go:199-201`（checkPendingDispositions）→ `ReviewTaskProgress`、`CheckReviewGate`、`MoveWithOptions(..., "done")` 全部失败；`internal/board/review_disposition.go:167-169` 使 `review assign` 也失败，因而永远产生不了 ledger。
  - 本轮新增的恢复通道对它不适用：`review_closure.go:309-315` 与 `:369-374` 的 `resolved_failures` 只接受 `ExecutionStatus != "ok"` 的运行；`internal/board/review_context.go:54-60` 使该批次连下一轮增量都无法启动（`aggregateReviewBatch` 先失败）；`review_plan.go:128-131` 与 `review_gate.go:230-259` 使"另建计划/另建批次绕开"不可行；PM-03 的重绑定只换执行周期，不清除批次义务。
- 用户影响：与首轮 PM-01 完全相同的不可逆陷阱，仅入口从"解析失败"收窄为"lineage 关系失败"。Reviewer 一旦在首轮误写 lineage、或在增量轮复用上一轮 finding ID 而漏写 lineage（`internal/review/roles.go:125-127` 正是要求它自行判断何时该带 lineage），该批次即无法聚合、无法闭批，其全部成员卡永远无法 done，`kander check` 持续报错，工具内没有合法恢复入口。本卡 GOAL 要求"门禁拒绝无效证据，但合法流程始终有前进路径"，此路径仍不满足。
- 最小产品修复：在 `internal/board/reviews.go:500-506` 已有的 finalize 校验块内，紧接 `ParseReviewFindings` 成功后再调用同事务可用的 `validateFindingLineage(tx, run, f)`，失败时走同一条 `ExecutionStatus="failed"` + `FailureReason` 分支。不放宽任何结构约束，不新增命令，使这类运行同样落入既有 `resolved_failures` 显式替代通道。

## NON-BLOCKING

- **PM-08 — low**：PM-05 的 CWD 绑定使 `review advance` 对"尚未建立计划"的批次一律不可用。`internal/review/disposition.go:57-59` 无条件执行 `verifyPlanCWD(root, b.PlanID, cwd)`，而 `b.PlanID` 可以合法为空——`internal/board/reviews.go:323` 创建新批次时不写 PlanID，`internal/board/review_plan.go:141-147` 正是为"先跑审核、后建计划"提供了显式接管路径（`existing.PlanID = p.PlanID`）。此时 `board.ReadReviewPlan(root, "")` 因 `ValidReviewID("")` 为假直接返回 `plan_id`，而 board 层 `internal/board/review_advance.go:13-51` 本身并不要求计划，`--advance-file` 随 review 调用推进的路径（`internal/board/reviews.go:356`）也仍然放行。后果有限：规则本就要求先建计划，建好后即恢复，且报错文本为 `plan_id`，与真实原因不符。最小改动：`b.PlanID == ""` 时跳过该校验，或返回明确的"批次尚无审核计划"错误。

## 说明

- 本轮未发现 blocking 级问题，也未发现由修复范围新引入的 high/medium 缺陷；除 PM-01 残留外，首轮六项均已按冻结契约实际闭合。
- PM-03、QA-01、QA-NB-03 三处的替代实现按契约逐项核实：重绑定不丢失旧失败与旧作者原件（`internal/board/review_plan_extend.go:55,63,141-148` + `internal/review/disposition_cycle_test.go:118-151`）；机械例外必须由主 Agent 独立署名并绑定真实 Git 差异（`internal/board/review_mechanical.go:43-87` 与 `internal/review/mechanical.go:11-40`，且 `ReviewMechanicalClaims` 只对被选作角色结论的运行生效，与 `review_closure.go:320-364` 的 `mechanicalFix` 判定范围严格一致，既不放行裸标签也不误伤已由新运行覆盖的修复）；非法报告在 finalize 当场标 failed 而非仅告警。三者均未削弱它们所触及的既有要求。
- 文档与规则的新增描述逐条对照实现无冲突：`docs/review-disposition.md` 的 rebind、diff_hash 计算命令、`submitted_revision` 作者绑定，与 `rules/KANDER-REVIEW-RULES.md:161,240,260,264,588-593` 均与代码一致，未产生新的 [mechanical] 类不一致。
- 作者交付报告仅作声明处理；以上结论全部来自 worktree 代码实读，未执行任何测试、构建或看板写入。

## QA 增量原报告

Write（含计划文件）在本会话被禁用，与审核者只读工具限制一致，因此报告直接在此交付；未修改任何文件、索引、引用或工作树。

---

**Role**: QA
**Commit**: `55e18dc727730d0bab164ce67d2bc235fb1ec8c5`（reviewed-commit `d325bb9755cba0ac704a6841cd1d2d786eed42ee`，base `8241a49b1f50cbb99acc68b4c3dc58b5a803253e`）
**Task Context**: `/tmp/claude-review.385017932008a01f72fec769a15abb8d/task-spec.md`（任务组 `20260907-review-archive-group` / 卡 `20260907-review-disposition-gate-task`，R）
**Reviewed Scope**: FIX RANGE FILE LEDGER 的全部 24 个路径，以及被其直接影响的未改动消费者（`internal/board/{board,transaction,transaction_log,review_check,review_context,review_advance,review_findings,snapshot}.go`、`internal/review/{review,args,validate,archive,git}.go`）。首轮已接受且本轮未改动的代码只用于判断修复影响，不重审、不扩大范围。

## 行为/质量表（fix range 引入行为）

| 行为 | 关键证据 | 结论 |
| --- | --- | --- |
| 机械例外需主 Agent 独立评估并绑定真实 Git 差异 | `review_mechanical.go:43-87`；`review_closure.go:241-243,444-446,529-531`；`review_gate.go:154-156`；`internal/review/mechanical.go:10-41`；`internal/review/disposition.go:196-204` | 满足 |
| 执行周期变更可恢复（requirements-needed + extend-plan 重绑定） | `review_gate.go:71-92`；`review_plan_extend.go:24-77,132-148`；`review_plan.go:190-211` | 满足 |
| tracked-cycles 真实参与校验，损坏与周期变更区分 | `review_plan.go:199-211`；`review_recovery_test.go:114-128` | 满足 |
| 非法结构化报告在 finalize 归档为 failed 并非零退出 | `reviews.go:500-506`；`internal/review/archive.go:212-221,244-267` | 满足 |
| 增量补充上下文逐字保留且不替代自动原件 | `review_context_merge.go:9-33`；`reviews.go:389-395`；`internal/review/disposition.go:221-244` | 满足 |
| advance/extend-plan 绑定计划 CWD | `internal/review/disposition.go:56-59,74,248-257`；`review_plan_read.go:5-19` | 满足（见 QA-05） |
| CheckReviewGate 签名收敛、CheckBoard 不可达分支删除 | `review_gate.go:220-228`；`deps.go:329` | 满足 |
| 文件规模 | `internal/board/reviews.go` 922 行；其余 fix range 文件均远低于 1000 行 | 规模规则不触发 |

## 原 finding 逐条核验（对 `55e18dc7` 实读）

| 原 ID | 原等级 | 作者处置 | 本轮核验证据 | 结论 |
| --- | --- | --- | --- | --- |
| QA-01 | medium | fixed（替代实现） | 见下 | closed |
| QA-02 | medium | fixed（替代实现） | 见下 | closed |
| QA-03 | medium [mechanical] | fixed | `review_gate.go:220-228` 返回 `[]Problem`；`deps.go:329` 单行 append，`gateErr` 分支已删除 | closed |
| QA-04 | medium [mechanical] | fixed | `rules/KANDER-REVIEW-RULES.md:240,260` 已改写：task-bound 从 `--previous-run-id` 派生 reviewed-commit、显式值必须匹配、前驱须同 batch/base/role/**reviewer**、手工上下文为补充；与 `:264,528-533,577-587` 及实现一致，同文件不再自相矛盾 | closed |
| QA-NB-01 | low | fixed | `internal/review/disposition.go:58-59`（advance）、`:74`（extend-plan）→ `verifyPlanCWD`（`:248-257`）；测试 `disposition_regression_test.go:101-119` | closed（附带 QA-05） |
| QA-NB-02 | suggest | fixed | `review_gate.go:202-204` 直接调用 `readDispositionLedger(tx, run, false)`；`review_disposition.go:331-336` 在非 complete 模式把"尚不存在"当合法待处置，已存在时仍完整校验（pending 与损坏仍区分） | closed |
| QA-NB-03 | suggest | fixed（更强实现） | `reviews.go:500-506` finalize 解析失败即 `failed`/`ExitCode=1`/`FailureReason`；`archive.go:217-219` 打印原因，`publish()` 返回 `run.ExitCode`；report/raw 原件保留 | closed |
| QA-NB-04 | suggest | fixed | `review_gate_test.go:341-347` 只建一块看板，直接构造 unsealed `ReviewPlan`，前后批次校验仍真实 | closed |

### QA-01（机械标签绕过新运行）— closed

- `review_mechanical.go:68` 把作者处置、Reviewer 原条目与主 Agent 评估三方逐字段绑定：`a.Category != d.Mechanical`、`a.ReportedCategory != item.Mechanical`（缺标签必须显式空串）、`a.Author != r.Author`、`ReportHash`、`FixCommit`、`Basis`/`Facts` 非空；`:71-78` 要求 64 位十六进制 diff_hash 与排序去重的仓库相对路径。
- `review_closure.go:241-243` 让 `ReviewClosureEdges` 在所有出口先校验声明；`:444-446`（close）、`:529-531`（闭批完整性）、`review_gate.go:154-156`（check/done 共享校验）复核 `evidence.Mechanical` 与重算结果逐字节相等。
- Git 事实留在 review 层：`internal/review/mechanical.go:21-30` 用 `--name-only -z` 要求声明路径与实际变更集合完全相等，`:31-38` 用 `docs/review-disposition.md:190` 指定的同一条 `git diff` 比对 SHA-256；close 的 CWD 由 `review_closure.go:447` 绑定计划 CWD。
- 首轮建议的"标签必须等于 Reviewer 分类"未采用，但替代实现不违背冻结契约：本轮未改动的既有规则 `rules/KANDER-REVIEW-RULES.md:199` 明确"缺 `[mechanical]` 标签由主 Agent 按定义补认"，新增文本 `:580-586` 与之一致。机器可查的一致性已全部检查，其余属 THREAT_MODEL 明示的"结构验证不代替主 Agent 真实复核"。
- 反向路径确认：裸标签闭批被拒（`review_gate_test.go:276-279`）；伪造 diff_hash、未变更路径、空 facts、错报 Reviewer 分类均被拒（`disposition_regression_test.go:145-152`）。

### QA-02（换 OWNER 后整组永久无法 done）— closed

- `review_gate.go:74-92`：周期不一致不再是硬错误，progress 输出全部变化成员的 `RebindCycles` 并返回 `requirements-needed`，仅 completion 时报错并指明恢复方式。
- `review_plan_extend.go:24-29,49-77`：`rebind_cycles` 与 batch/seal 互斥；`maps.Equal(changed, x.RebindCycles)` 要求恰好列出全部变化成员（同周期请求、漏成员、过时 revision 均拒）；`:132-140` 同一事务更新 tracked-cycles 与全部成员 `reviews/plan.json`，`:141-148` 旧计划完整入 plan-history。
- 恢复不放宽义务：批次、角色要求、成员、assignment、旧失败轮与作者原件均不变——`review_recovery_test.go:40-101` 断言闭批证据字节不变、终态成员保持 done、旧计划入历史；`disposition_cycle_test.go:16-153` 走真实 CLI 验证"另建计划丢失败被拒""重绑定后 close 仍要求 `resolved_failures`""旧 OWNER 不能再写""新 OWNER 只追加"。
- 作者身份边界随之调整而未被削弱：`review_disposition.go:217` 仅在 `SubmittedRevision == 0`（旧记录/无版本记录的卡）时回落到 assignment 冻结作者，新记录改由 `:274-277` 的"当前 working OWNER + 卡 revision"绑定，旧作者原文与 lineage 不可覆盖（`:299-315`）。
- 首轮建议的"单卡换计划指针"未采用；替代实现同时满足本轮"同周期不得换计划丢旧失败""新周期恢复不影响其他成员终态与已闭证据"。
- 配套：`review_plan.go:199-208` 让 tracked-cycles 真正参与比对，损坏（`tracked cycle/plan mismatch`）与合法周期变更明确区分，且不能借重绑定掩盖损坏（`review_recovery_test.go:114-128`）。

## Gate findings

本轮未发现由 fix range 引入、加剧或掩盖的 blocking/high/medium 问题；也未发现任何一处修复破坏其触及的契约要求（AC1/AC6/AC7/AC8/AC9/AC11/AC12 的相关路径均已按上表逐条追踪）。

## NON-BLOCKING

- **QA-05 — low**：`kander review advance` 现在对"尚未建立计划的批次"以 `plan_id` 失败。Claim: Observed。`internal/review/disposition.go:56-59` 无条件调用 `verifyPlanCWD(root, b.PlanID, cwd)`，而 `review_plan_read.go:5-7` 对空 `plan_id` 返回 `reviewError("plan_id")`；board 层 `review_advance.go:13-25` 并不要求批次已有计划，且 `review_plan.go:132-148` 明确支持"批次先建、计划后补"（`CreateReviewPlan` 接管既有批次）。触发路径：先用 `review --task ... --batch-id` 建批次，在建计划前想推进目标。影响很小（规则本就要求先建计划，补建计划后即可继续，无数据损失、无证据丢失），但错误文本不指向真实原因。最小改动：advance 分支在 `b.PlanID == ""` 时返回明确错误（如 "batch has no review plan"）。
- **QA-06 — suggest**：自动增量上下文的内部分隔标记进入 Reviewer prompt。Claim: Observed。`review_context_merge.go:9-16` 生成 `KANDER_AUTOMATIC_CONTEXT_BYTES: <n>` 头与固定分隔句，`internal/review/disposition.go:243` 在调用方无补充时也一律拼接，`internal/review/git.go:175` 把整段原样接在 `"Additional caller-supplied review context: "` 之后；于是每次 task-bound 增量审核的 prompt 都含一行字节计数和一个可能为空的 "Caller supplemental context" 段落。冻结原件必须保持现状，可在构造 prompt 时用 `board.SplitReviewContext` 拆开、按可读标题分别渲染（补充为空则省略该段）。

## 说明

- 本报告仅用 Read/Grep/Glob 实读 worktree 代码与规则文本，未执行任何测试、构建或 Git 命令；作者交付报告与验证记录只作声明处理，`go test`/`-race`/交叉编译等结论本轮不可核验。
- 首轮已接受且本轮未改动的代码（含 `review_findings.go`、`review_context.go`、`snapshot.go`、`args.go`、`validate.go` 等）不重审，仅用于判断修复影响。
- Windows 原生执行缺口、PM/QA 组审核、组分支接收与 develop 集成按调用方交代保留；不以交叉编译冒称实测。新增的 CLI 回归 `disposition_cycle_test.go`、`disposition_regression_test.go` 带 `//go:build unix`，其核心逻辑另有无标签的 board 层用例覆盖（`review_recovery_test.go`、`review_gate_test.go`）。
- 按 QA 角色规则未报告性能类问题。
- 任务文件 `/tmp/claude-review.385017932008a01f72fec769a15abb8d/prompt.txt` 无法删除（本会话无写/执行工具）；按任务文件说明不影响结果。
