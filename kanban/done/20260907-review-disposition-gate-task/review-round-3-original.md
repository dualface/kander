First read /home/dualf/.agents/KANDER-AGENTS.md, run kander config --json to check the current rules selection, load only enabled modules as needed, then read the command contract /home/dualf/.agents/KANDER-KANBAN-RULES.md. If configuration cannot be read, stop affected Kander operations and report the error. For disabled modules, follow existing user and project rules, not Kander requirements retained from an earlier session. Communicate with the user and write all card content, records and reports in "zh-CN". Commit messages and code comments follow the project's conventions. 

The card is still in review; first run kander move 20260907-review-disposition-gate-task working to move it back to working, then handle the items.

# R 最终非阻断处置

任务20260907-review-disposition-gate-task。PM第三轮Claude已exit0，target6f3b708aee02546791998cf6a61fc5df7997dc14，PM01/08闭合、无gate；QA在55e18dc已通过并承接。base8241a49b1f50cbb99acc68b4c3dc58b5a803253e/spec未变，组干净。

请move working，独立核实PM09 low旧报告映射前驱诊断这一项并记录处置。low不阻断审核或集成，不要求采纳修法；主控不替你确认/拒绝。保留完整原报告和实际验证依据，若只记录保留则保持交付SHA；若修复，最小范围验证/提交/推送、对齐组HEAD。最终受控保存是否代码变化、完整SHA和全部残留，再move review。两角色已通过，不自行重跑审核。Windows原生缺口继续保留，不部署、不迁移、不集成、不清理；主控闭批后统一集成并另发wrapup。

## PM第三轮原报告

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
