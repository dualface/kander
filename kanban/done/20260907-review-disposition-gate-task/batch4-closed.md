# R 第四批闭批

base8241a49b1f50cbb99acc68b4c3dc58b5a803253e，closed commit251f5d89186730ea372053a401136030710efe84。首轮PM/QA于d325bb9未通过；第一次修复55e18dc，QA增量通过、PM01 lineage残留未闭合；第二次修复6f3b708，PM第三轮增量通过。最后PM09 low诊断修复251f5d8，主控核对仅错误包装与定向回归，承接已通过角色结论，不声称Reviewer实际审过最终诊断提交。QA通过提交为55e18dc，PM为6f3b708，最终是二者后代，同base/spec/Reviewer，无重写。CSA/Hacker N/A。

全部finding与建议已由原作者独立验证修复，保留替代最小修法的理由；无未处置finding。Windows原生验证缺口保留，作者实际全量/race/build/vet/交叉编译与Reviewer只读无法实测的事实分开。第二次修复成功闭合同一gate，不触发两次修复失败停止规则。

## QA-2.md

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

## PM-3.md

Write/Edit 在本会话受限（只读审核工具），Reviewer 也不得改写仓库，故审核报告直接在此交付。任务文件 `/tmp/claude-review.6292057d8d4de7e3f6bc80153badc917/prompt.txt` 因无写工具无法删除；按任务文件说明，这不影响结果。

---

**Role**: PM（规格验收 · 第三轮增量复审）
**Commit**: `6f3b708aee02546791998cf6a61fc5df7997dc14`（base `8241a49b1f50cbb99acc68b4c3dc58b5a803253e`，reviewed-commit `55e18dc727730d0bab164ce67d2bc235fb1ec8c5`）
**Task Context**: `/tmp/claude-review.6292057d8d4de7e3f6bc80153badc917/task-spec.md`（`20260907-review-disposition-gate-task`，任务组 `20260907-review-archive-group`）
**Reviewed Scope**: FIX RANGE FILE LEDGER 全部 11 个路径（`docs/review-disposition.md`、`internal/board/{review_disposition.go,review_gate_test.go,review_lineage_finalize_test.go,reviews.go}`、`internal/review/{disposition.go,disposition_regression_test.go,disposition_test.go,git.go,lineage_recovery_test.go}`、`rules/KANDER-REVIEW-RULES.md`）。为判定修复影响追踪了未改动的消费者：`internal/board/{review_closure,review_gate,review_context,review_context_merge,review_check,review_findings}.go`、`internal/review/{archive,review}.go`。`55e18dc7` 已接受且本轮未变更的代码不重审，仅用于判断修复影响。

## 上轮 finding 处置核验

| ID | 原等级 | 作者声明 | `6f3b708a` 实测 | 结论 |
| --- | --- | --- | --- | --- |
| PM-01（残留：lineage 关系非法但 `execution_status=ok`） | high | fixed，待复核 | `internal/board/reviews.go:500-509` 在 finalize 的同一事务内、`ParseReviewFindings` 成功后调用 `validateFindingLineage(tx, run, findings)`，任一失败即置 `ExecutionStatus="failed"`、`ExitCode=1`、`FailureReason="invalid structured review report: "+e`。该函数就是聚合期使用的同一实现（`internal/board/review_closure.go:132-188`），覆盖上轮列出的全部四条出错路径（`:157` reused ID、`:162` 非唯一/错前驱、`:184` 前驱无该条目），无参数或语义差异。`FinalizeReviewRun` 是 `Phase="finalized"` 的唯一入口，`UpdateReviewRun`（`reviews.go:448`）对已 finalized 运行硬拒，故不存在绕过。归档后不再堵塞：`review_closure.go:112` 与 `review_gate.go:192` 均对非 ok 运行跳过 lineage 与 ledger 校验，`aggregateReviewBatch`→`ReadReviewBatchView`/`PublishReviewDisposition`/`ReviewTaskProgress`/`CheckReviewGate` 全部恢复可用；`review_closure.go:309-315` 的 `resolved_failures` 恰好接受 `ExecutionStatus != "ok"` 的运行，构成合法出口。CLI 侧非零退出与原件保留仍在（`internal/review/archive.go:212-221`、`:266`）。冻结原件不受影响：失败判定发生在 `reviews.go:514-532` 写 originals 之前只改 run 元数据，报告字节原样落盘 | **Closed** |
| PM-08（未建计划批次的独立 `advance` 只报 `plan_id`） | low | fixed | `internal/review/disposition.go:57-62`：`b.PlanID == ""` 时返回 `archiveError("batch has no review plan; create its plan before advancing")`，否则仍走 `verifyPlanCWD`。已有计划的 CWD 精确绑定未放宽，`--advance-file` 随 review 启动的内联路径（`internal/board/reviews.go:331-348`，`PrepareReviewRun` 仅在 `batch.PlanID != ""` 时校验计划）未改动。恢复路径可走完：`review_plan.go` 的 `CreateReviewPlan` 仍可接管既有批次后再 advance。文档同步于 `docs/review-disposition.md:160` | **Closed** |

**状态统计**：Closed 2 / Partial 0 / Open 0（本轮受理 2 项）。上轮 PM-02…PM-07 已 Closed 且本轮未改动，按已接受处理，未重审。原报告 R-14（`go test`/`-race`/`build`/`vet` 全量验证）本轮仍为 **Unverifiable**：本次审核只有 Read/Grep/Glob，未执行任何命令，作者验证记录仅作声明。Windows 原生缺口按调用方交代保留，不据交叉编译冒称实测。

## 需求追溯（限本轮修复触及的契约条款）

| 需求（来源） | 期望行为 | 代码证据 | 状态 |
| --- | --- | --- | --- |
| AC3 增量 lineage 对应前轮，身份为 run_id+finding_id | 无前驱却带 lineage、重复指向同一前驱项、错前驱/错条目、复用前驱 ID 漏 lineage 一律不成立 | `review_closure.go:132-188` 判定；`reviews.go:500-509`（finalize）、`review_disposition.go:170`（assign）、`review_disposition.go:128`（map-legacy）、`review_gate.go:199`（check/done）、`review_closure.go:117`（aggregate）共用同一实现 | Complete |
| AC4 结构不合法的新报告不能自动通过；旧报告须显式人工映射 | 非法报告在不可变归档前即 `failed`；非法人工映射被拒且不产生映射 | `reviews.go:505-509`；`review_disposition.go:118-130`（`FindingsSchema>0` 拒绝 legacy 路线，lineage 校验在 `tx.Put` 之前返回） | Complete |
| GOAL/AC12「门禁拒绝无效证据，但合法流程始终有前进路径」 | 失败运行不得永久堵死批次与成员卡，须有工具内合法恢复入口 | `review_gate.go:186-205`（非 ok 运行跳过）、`review_closure.go:302-316`+`:369-374`（`resolved_failures` 显式替代）、`review_context.go:58-60`（失败运行不得当前驱）、`reviews.go:372-397`（同 batch 新 run ID 复用最后有效前驱） | Complete |
| AC9 batch ID 不变、target 只经 CAS 推进、关闭证据绑定最终 target | 失败意图已提交的 advance 不重复；替代运行与失败运行的 Git 关系入闭批证据 | `reviews.go:331-348`（重复 advance 报 `redundant batch advance`）、`review_closure.go:314`（`add(failed.Commit, replacement.Commit)`）、`internal/review/disposition.go:142-162` 校验祖先关系 | Complete |
| AC11 增量自动上下文原文并入 prompt，冻结输入按原始字节 | prompt 呈现前驱原报告/作者记录/批次视图原文；冻结件保持字节长度封装 | `internal/review/git.go:176,204-218`（`promptReviewContext` 用 `board.SplitReviewContext` 拆分渲染，空补充省略段落）；`internal/review/disposition.go:229-250` 仍以 `MergeReviewContext` 冻结；`reviews.go:389-395` 仍只复校自动来源 | Complete |
| AC13 规则与中文 schema 文档同实现一致 | 文档/规则描述与代码不得冲突 | `rules/KANDER-REVIEW-RULES.md:264`、`:547-549` 与 `docs/review-disposition.md:103,105,107,160,162` 逐句对照 `reviews.go:500-509`、`review_disposition.go:128`、`disposition.go:57-62`、`git.go:204-218` 一致 | Complete |
| AC13 `go test ./...`、`-race`、build/vet/格式检查 | 全量验证通过 | 本次审核未执行任何命令；作者记录仅为声明 | Unverifiable |

**状态统计**：Complete 6 / Partial 0 / Missing 0 / Contradicted 0 / Unverifiable 1。

## 门禁问题

本轮未发现由 fix range 引入、加剧或掩盖的 blocking/high/medium 问题；也未发现任何一处修复破坏了它所触及的契约要求（AC3/AC4/AC9/AC11/AC13 及 GOAL 的前进路径要求均已逐条追踪）。

## NON-BLOCKING

- **PM-09 — low**：`map-legacy` 现在要求「先映射前驱、再映射后继」，且违反顺序时的报错不指向真实原因。Claim: Observed。`internal/board/review_disposition.go:128` 对旧报告映射调用 `validateFindingLineage`；该函数在 `review_closure.go:134-151` 中只要 `run.PreviousRunID != ""` 且前驱 `ExecutionStatus == "ok"`，就无条件 `runFindings(tx, previous)`，而 `review_disposition.go:59-81` 对「schema=0 且尚无 legacy-map.json」的前驱返回 `structured findings required; legacy mapping required for old reports`。触发路径：升级前遗留的两个 `findings_schema=0` 运行 A←B 同批未映射，先提交 B 的映射即失败，错误文本看起来像在说 B 缺结构。影响很小：先映射 A 再映射 B 即可完成，无数据丢失、原件不改写，且同样的顺序约束在 `AssignReviewFindings`（`review_disposition.go:170`，本轮未改动）中早已存在，聚合期也一样要求两者都已映射。最小改动：在该错误上附带出错的运行 ID（例如 `runFindings` 失败时包一层 `run.RunID + ": "`），或在 lineage 校验前显式提示「前驱运行尚未映射」。

## 说明

- 结论全部来自 `6f3b708a` 工作树代码实读，未执行任何测试、构建、Git 或看板写入；作者交付报告与 `review-round-2-validation.txt` 仅作声明处理。
- 作者本轮宣称的「五类关系实测」不可核验；但 PM-01 的闭合判定不依赖该声明，而是依赖 `reviews.go:500-509` 与 `review_closure.go:132-188` 共用同一校验实现、`FinalizeReviewRun` 为 finalized 的唯一入口、以及非 ok 运行在 `review_gate.go:192`/`review_closure.go:112` 上的一致跳过这三条可直读的代码事实。
- 新增测试不构成冗余：`internal/board/review_lineage_finalize_test.go` 无平台标签，覆盖 finalize 判定、聚合不再被污染、close 不成立 PASS、原件字节不变；`internal/review/lineage_recovery_test.go` 带 `//go:build unix`，另外覆盖真实命令入口、失败前驱拒绝、`resolved_failures` 恢复与 prompt 渲染，二者断言对象不同。
- QA-05/QA-06 对应的改动落在本轮 fix range 内，我按「修复是否破坏其触及的要求」核对了 `internal/review/disposition.go:57-62` 与 `internal/review/git.go:204-218`，未发现削弱既有契约；其处置结论属 QA 角色，本报告不代为判定。
- Windows 原生执行缺口、PM/QA 组审核、组分支接收与 develop 集成按调用方交代保留；不以交叉编译冒称实测。

## review-round-1-disposition.md

# 首轮 PM/QA 原作者逐项处置

作者：codex，本卡执行 Agent。以下结论来自目标源码、规则与实际验证；不是编排端代写，也不代表 Reviewer 已复审通过。

审核基线：`8241a49b1f50cbb99acc68b4c3dc58b5a803253e`；首轮目标：`d325bb9755cba0ac704a6841cd1d2d786eed42ee`；本轮统一修复提交：`55e18dc727730d0bab164ce67d2bc235fb1ec8c5`。首轮 PM/QA 均为 Claude/opus/high，进程 exit 0，语义未通过。完整派回和两角色原报告保存在 [首轮原件](review-round-1-original.md)，未改写 PM 首行 base 笔误；真实调用基线以上述完整 SHA 为准。

原报告共 15 个需处置条目，全部独立核实并修复。QA-NB-01 至 QA-NB-04 只是本地定位索引，对应 QA NON-BLOCKING 四项，原 Reviewer 没有为它们编号。重合问题分别记录，不删除任何一方意见。

| 原条目 | 原等级 | 执行端状态 | 独立核实与处置依据 | 验证 |
| --- | --- | --- | --- | --- |
| PM-01 | high | fixed | 确认非法结构曾被归档为 ok，所有聚合出口随之永久拒绝。FinalizeReviewRun 现解析新结构；非法报告以 failed、非零 gate exit 和原因归档，report/raw 原样保留。新 ID 完整审核后通过 resolved_failures 显式替代。 | TestMalformedReviewCanBeExplicitlyReplacedThroughCLI；独立二进制两卡冒烟 |
| PM-02 | medium | fixed | 确认调用方 review-context 被静默替换。现按字节长度封装自动原件和逐字补充；board 独立 CAS 自动来源，整体输入哈希仍冻结全部文本，重试改变补充即拒绝。未采用“拒绝所有非空手工上下文”建议，保留原调用契约。 | TestIncrementalTaskReviewReadsFailingAuthorEvidenceAndRejectsWrongSources；TestIncrementalSupplementCannotReplaceAutomaticSource |
| PM-03 | medium | fixed | 确认重新认领改变 STARTED_AT 后整组无法恢复。progress 现输出 requirements-needed 和全部 rebind_cycles；extend-plan 在原计划内 CAS 重绑定，保留成员、角色要求、批次、失败轮和旧结论。新 OWNER 仅追加自己结论，旧 OWNER 不可再写。未采用单卡换计划指针：该方案会破坏其余成员绑定并可能漏掉旧失败。 | TestGroupReclaimRetainsPlanFailuresAndOriginalAuthorsThroughCLI；TestRebindPreservesClosedEvidenceAndTerminalMember；独立二进制冒烟 |
| PM-04 | medium / mechanical | fixed | 确认规则 240/260 与新增章节冲突。统一说明 task-bound 自动上下文、可省 reviewed-commit、显式值匹配、同 batch/base/role/reviewer；手工上下文是保留的补充。同步早期增量章节和归档说明。 | 逐段对照 rules/KANDER-REVIEW-RULES.md 与 hydrateIncremental/PrepareReviewRun；全量测试 |
| PM-05 | low | fixed | 确认 advance 未检查计划 CWD。调用 Git 前读取不可变计划身份并精确比较 CWD；extend-plan 同时修复。 | TestAdvanceAndExtensionRejectOtherWorktree |
| PM-06 | suggest | fixed | 确认 tracked-cycles 仅被当存在标记。现每次计划副本验证都会解码并与计划周期比对；真实 STARTED_AT 改变与 tracker 损坏明确区分。重绑定不掩盖损坏。 | TestTrackedCycleCorruptionCannotBeRebound |
| PM-07 | recommend | fixed | 确认 sameTasks 重复标准库功能。删除函数，改用 slices.Equal，原成员顺序/身份拒绝测试保留。 | 原错成员/增量身份回归与全量测试 |
| QA-01 | medium | fixed | 确认仅填 disposition.mechanical 可跳过新运行。close 现要求主 Agent 单独署名的 mechanical assessment，绑定原条目、作者记录、报告摘要、修复 SHA、Reviewer 实际分类（含缺标）、依据、实际验证事实、变化路径与差异摘要；review 核查真实 Git 差异，board 复核完整绑定。未采用直接信任 Reviewer 标签的修法：规则明确允许主 Agent 按事实补标，标签与哈希都不替代真实语义复核。 | TestMechanicalFixAndNonMechanicalRerunGate；TestMechanicalAssessmentRequiresActualGitScope |
| QA-02 | medium | fixed | 确认问题影响计划全部成员，且原 assignment 作者冻结会阻止接管人提交。重绑定同一事务更新整组计划副本和周期，完整旧计划存入历史；不改其他成员状态、不删失败轮、不覆盖前作者原文。已 done 成员和已 closed 证据也保持原状态与字节内容。 | TestGroupReclaimRetainsPlanFailuresAndOriginalAuthorsThroughCLI；TestRebindPreservesClosedEvidenceAndTerminalMember |
| QA-03 | medium / mechanical | fixed | 确认 CheckReviewGate 的 error 返回永远为 nil。签名收敛为 []Problem，CheckBoard 删除不可达 gateErr 分支；每卡错误继续进入 problems。 | 现有 CheckBoard/门禁诊断回归与全量测试 |
| QA-04 | medium / mechanical | fixed | 独立复核确认增量参数及 reviewer 前驱要求的规则冲突。与 PM-04 一并修订全部相关文本，非 task-bound 的手工上下文要求仍明确保留。 | 逐段规则与实现对照；增量 CLI 回归 |
| QA-NB-01（原文无编号） | low | fixed | 确认 advance 和 extend-plan 都缺 CWD 绑定；现在两个入口均先核对计划 CWD，另一干净 worktree 即使拥有相同 Git 对象也不能提供该计划证据。 | TestAdvanceAndExtensionRejectOtherWorktree |
| QA-NB-02（原文无编号） | suggest | fixed | 确认 checkPendingDispositions 对 ledger 重复完整解码。移除预读；readDispositionLedger 在非 complete 模式下允许尚不存在的合法待结论，已存在时仍完整验证。 | pending/closed 门禁全量回归 |
| QA-NB-03（原文无编号） | suggest | fixed | 确认非法新报告直到后续命令才暴露。采用比发布警告更完整的修复：finalize 当场标 failed 并打印解析原因，原件保留且可明确替代。仅加非致命警告仍会留下 PM-01 的永久陷阱，故未采纳该最小修法。 | TestMalformedReviewCanBeExplicitlyReplacedThroughCLI；独立二进制冒烟 |
| QA-NB-04（原文无编号） | suggest | fixed | 确认 plan extension 测试先创建并丢弃一块看板。改为直接构造 unsealed ReviewPlan，一块看板完成真实前后批次校验。 | TestPlanExtensionUsesClosedCommitNotArbitraryRolePass |

## 核实边界与未解决事项

全部 fixed 项的修复 SHA 均为上列本轮提交；具体实现集中于 internal/board/reviews.go、review_plan_extend.go、review_gate.go、review_disposition.go、review_mechanical.go、review_context_merge.go 与 internal/review/disposition.go、mechanical.go。检查通过只证明列明的测试与结构约束，不替代主 Agent 对机械类别、事实依据和语义结果的真实复核。没有把 Reviewer 分类当作权威白名单，也没有把人为事实声明当签名。

本轮未遗留已确认且未修复的 gate 或 NON-BLOCKING 条目。三处“最小修法”采用符合原契约的替代实现，原因逐项写明。PM/QA 是否接受本轮修复、组分支接收和最终集成仍由编排端完成；本仓库 CSA/Hacker N/A。Windows 原生测试缺口保留。

完整验证见 [首轮修订验证](review-round-1-validation.txt)。编译后二进制冒烟使用真实 Git 与隔离两卡，Reviewer 进程为测试替身：非法报告按 failed 原样归档、原计划重新认领后恢复、旧失败未显式替代时 close 拒绝、显式替代后两卡 done；没有触发真实 PM/QA 审核、部署或迁移真实看板。

## review-round-2-disposition.md

# 第二次修复：原作者逐项核实与处置

作者：codex，本卡执行 Agent。固定审核 base：`8241a49b1f50cbb99acc68b4c3dc58b5a803253e`；本轮被审目标 / 后续 PM reviewed-commit：`55e18dc727730d0bab164ce67d2bc235fb1ec8c5`；本轮修复 SHA：`6f3b708aee02546791998cf6a61fc5df7997dc14`。

完整第二次派回、PM 增量报告和 QA 增量报告保存在 [原件](review-round-2-original.md)，未改写。此前交付记录中 PM-01 的 fixed 是执行端第一次修复的判断；PM 本轮指出关系级校验残留，经真实二进制复现确认，前次 fixed 声明不代表该项当时完整闭合。本次是该项第二次修复尝试，是否闭合仍待 PM 复审。QA 已通过的结论按主控通知保留，执行端不重复发起审核。

| 条目 | 原等级 | 执行端状态 | 独立事实与修复 | 验证 |
| --- | --- | --- | --- | --- |
| PM-01 残留 | high | fixed，待 PM 复核 | 确认 finalize 仅 ParseReviewFindings，格式合法的错误 lineage 被归档 ok，aggregate/assign/增量上下文及 done 随后全部受阻。现同事务调用与 assignment/aggregate 相同的 validateFindingLineage，在冻结原件前统一标 failed、非零退出并保存具体原因。关系要求未放宽。人工旧报告映射也在不可变发布前校验关系，错误请求尚未落盘，可更正后重交。恢复时首轮失败重跑完整首轮；增量失败以新 run ID 接回同一最后有效前驱，不使用坏运行作为前驱，不另建与有效旧链无关的完整链。旧失败须由 resolved_failures 显式指向合法链上的成功替代。 | TestFinalizeRejectsInvalidFindingRelationships（无平台标签）；TestLineageFailuresRecoverThroughControlledCLI；旧/新独立二进制对照实测五类关系。 |
| PM-08 | low | fixed | 确认未建计划批次的独立 advance 只报 plan_id。保留既有“先补建计划再独立推进”边界，改为明确提示 batch has no review plan; create its plan before advancing。未跳过 CWD 校验。 | TestAdvanceWithoutPlanExplainsRecovery：无计划拒绝；受控接管已有批次为计划后，同一 advance 请求成功。 |
| QA-05 | low | fixed | 独立复核同一空 PlanID 路径，报错现指出真正缺失项和恢复动作。已有计划 CWD 精确绑定不变，inline --advance-file 的原协议未改。 | 同上，另保留 TestAdvanceAndExtensionRejectOtherWorktree。 |
| QA-06 | suggest | fixed | 确认内部字节计数与空补充段进入 Reviewer prompt。构造 prompt 时拆分已冻结上下文，以 Automatic archived review context 和非空 caller-supplied context 分别显示；空补充不生成段落。冻结 review-context.md 的字节长度封装及整体哈希不改。非任务绑定调用原样保留手工文本。 | 五类 CLI 恢复中的空补充断言；原增量测试的非空补充、原件保留、重试、内部标记不进 prompt 断言。 |

## 旧项核实

PM-02 至 PM-07 的 Closed 结论与当前源码一致：补充文本逐字冻结、整组周期原计划 CAS 恢复、规则一致、计划 CWD 绑定、tracked-cycles 校验和 slices.Equal 均保留。QA-01 至 QA-04、QA-NB-01 至 QA-NB-04 的 Closed 结论也与当前实现一致；主 Agent 机械评估及 Git 证据、作者接管记录、不丢失败的周期恢复、诊断签名和去冗余均未被本次修改削弱。PM-08/QA-05 的附带提示问题单列于上表，不覆盖两角色原文。

## 全流程实测与保留边界

用此前 `55e18dc` 构建的独立二进制，在五个隔离双卡看板分别复现：无前驱却带 lineage；FINDINGS/NON_BLOCKING 两条目指同一前驱；前驱条目不存在；复用旧 ID 遗漏 lineage；指向非立即前驱的 run。五例 JSON 格式均合法，旧命令退出 0，aggregate 因关系错误拒绝。

新二进制对五例均非零退出并归档 failed，report 原件保留；同 ID 重试不转成功；aggregate/progress 可以前进但两卡 done 仍拒绝。新成功运行接回最后有效前驱后，未填写 resolved_failures 的 close 仍拒绝，明确替代后闭批和两卡 done 成功，坏报告原文逐字不变。Reviewer 子进程是测试替身，命令入口、Git、归档和看板事务是真实实现；没有发起真实 PM/QA 组审核。

旧报告人工映射通过原映射测试验证：合法原文定位配非法 lineage 的请求拒绝，去掉非法关系后可成功提交，原报告未改写。工具不对自然语言语义作自动 PASS 推断，也不通过删除坏报告或更换批次绕过覆盖。

全部本轮意见已处置，无执行端已确认但未处理的条目；PM 复审、组分支接收和集成由编排端执行。CSA/Hacker N/A；Windows 原生测试缺口保留。验证原文见 [记录](review-round-2-validation.txt)。

## review-round-3-disposition.md

# PM 第三轮：原作者非阻断处置

作者：codex，本卡执行 Agent。固定审核 base：`8241a49b1f50cbb99acc68b4c3dc58b5a803253e`；PM 第三轮目标及本轮组基线：`6f3b708aee02546791998cf6a61fc5df7997dc14`。本轮有代码变化，最终交付：`251f5d89186730ea372053a401136030710efe84`，已正常推送 review-disposition-gate；fetch/rebase 最新组 HEAD 无变化、无冲突，任务工作树干净。

完整派回通知和 PM 第三轮原报告见 [原件](review-round-3-original.md)，原文未改写。PM-01/PM-08 已由本轮 PM 判 Closed，PM 已通过；QA 在 55e18dc727730d0bab164ce67d2bc235fb1ec8c5 通过并按主控通知承接。执行端遵照本次明确指令不再发起任何审核，新增提交不冒称已被 Reviewer 实测。

## PM-09（low）

执行端结论：确认，fixed。独立新建隔离看板，归档两个 schema=0、同批次且带前驱关系的成功运行 legacy-first、legacy-second；先提交后继的完整人工映射。修改前实际诊断为 `审核证据无效：structured findings required; legacy mapping required for old reports`，没有前驱 run ID，回归断言失败。因此接受 Reviewer 对诊断不清的事实判断；顺序约束本身仍符合必须校验前驱 lineage 的契约。

最小修复：只在 runFindings 的错误返回中添加读取对象的 run ID，并以 `%w` 保留底层错误链。修改后同一请求诊断为 `legacy-first: 审核证据无效：structured findings required; legacy mapping required for old reports`。先映射前驱再重交同一后继请求成功，两份 `PASS
` 报告原件逐字不变。未改变顺序约束、解析、lineage、覆盖或完成门禁。

修复 SHA：`251f5d89186730ea372053a401136030710efe84`。新增 TestLegacyMappingIdentifiesUnmappedPredecessor 覆盖错误来源、合法恢复与原件保留；既有 TestLegacyMappingRequiresOriginalLocations 同时通过。实际前后对照及全量日志见 [验证](review-round-3-validation.txt)。本轮仅两处文件变化：board 错误诊断和针对性回归；无 schema、规则、公共 API 或模块边界变更。

## 验证与全部残留

`go test ./...`、定向旧报告映射测试、`go vet ./internal/board`、gofmt 与 diff check 均通过；对齐组 HEAD 无代码变化，随后定向回归通过（Go 缓存）。前轮 race/build/vet/Windows 交叉编译完整记录保留，本轮最小诊断改动未重复执行这些检查，不能视为本提交新跑结果。

没有未处置的审核 finding；PM-09 为 low，不阻断。PM-01/PM-08 的 Closed 是 PM 第三轮结论，旧项接受状态按其原报告及前两轮作者记录保留。PM 原报告的验证 Unverifiable 描述的是 Reviewer 只读工具无法执行测试，作者实际执行日志单列，不改写 Reviewer 判断。

保留一项环境缺口：Windows 原生执行未完成，交叉编译不替代原生测试。后续工作为编排端接收本次交付、闭批、统一集成及另发 wrapup；不是执行端代码阻断。CSA/Hacker N/A。未部署、未迁移真实看板、未更新组分支、未集成或清理。任务分支与 worktree 保留；RESULT 空白，本轮返回 review。
