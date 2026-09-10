First read /home/dualf/.agents/KANDER-AGENTS.md, run kander config --json to check the current rules selection, load only enabled modules as needed, then read the command contract /home/dualf/.agents/KANDER-KANBAN-RULES.md. If configuration cannot be read, stop affected Kander operations and report the error. For disabled modules, follow existing user and project rules, not Kander requirements retained from an earlier session. Communicate with the user and write all card content, records and reports in "zh-CN". Commit messages and code comments follow the project's conventions. 

The card is still in review; first run kander move 20260907-review-disposition-gate-task working to move it back to working, then handle the items.

# R 第四批首轮审核派回

任务20260907-review-disposition-gate-task。先move working，独立核实下列两角色全部gate和NON-BLOCKING项，主控不替你确认/拒绝，不要求照抄Reviewer修法。

已知事实：PM/QA Claude/opus/high均已退出exit0，语义均未通过。真实调用base为8241a49b1f50cbb99acc68b4c3dc58b5a803253e，target d325bb9755cba0ac704a6841cd1d2d786eed42ee，组干净并冻结此target。PM报告首行base拼写为8241a49b51...，这是原文笔误；真实argv、上下文、组锚点均为上述正确完整SHA，原文不改。

PM01非法结构ok报告无法被替代与QA无ID第3建议相关；PM03/QA02周期重建、PM04/QA04规则冲突、PM05与QA第1建议CWD绑定有重合，保留各角色原文及判断。QA第2/4建议与PM06/07也需逐项处置，不能漏无ID项。

请用真实受控CLI/隔离看板核实非法结构报告后如何显式替代并闭批；重新认领一张组卡后全组计划、原件、旧失败与其他成员的状态关系；机械分类应符合既有规则与真实证据，既不可一字段绕过非机械复审，也不能把Reviewer任何标签无条件视为事实。增量手工上下文不能静默丢失，修法以用户契约及现有参数协议为准，拒绝或合并都需明确语义/测试/文档，不因最小建议就引入新的调用死路。周期变化的合法恢复不得成为同周期换计划丢弃失败记录的捷径；旧证据保持只读。

所有成立must-fix修复；拒绝/无法验证项给目标代码或实测依据。low/recommend/suggest不阻断，但必须完整处置。保持冻结契约，不扩张到后组dispatch/probe/subscription，不部署、不迁移真实看板。提交/正常推送、rebase最新组HEAD并复验，受控保存完整原文/逐项结论/测试/最终交付SHA/基线，再move review。你不审核、不集成、不清理组或任务工作区。Windows原生缺口继续保留。后续同base/spec增量reviewed-commit为本target。

## PM 原报告

Write/ExitPlanMode are unavailable in this read-only session, so I'm delivering the review report directly. The task file `/tmp/claude-review.a6259a3398c5fb4414c465805d0cfdb2/prompt.txt` could not be deleted (no write tools available); per the task file this does not affect the result.

---

**Role**: PM（规格验收）
**Commit**: d325bb9755cba0ac704a6841cd1d2d786eed42ee（base 8241a49b51f50cbb99acc68b4c3dc58b5a803253e）
**Task Context**: `/tmp/claude-review.a6259a3398c5fb4414c465805d0cfdb2/task-spec.md`（20260907-review-disposition-gate-task，任务组 20260907-review-archive-group）
**Reviewed Scope**: COMMIT TREE 中本次改动的全部路径：`internal/board/review_{plan,plan_extend,advance,closure,context,disposition,findings,gate,check}.go`、`internal/board/{reviews,snapshot,deps}.go`、`internal/review/{disposition,archive,review,roles,git}.go`、`internal/i18n/locales/*.json`、`docs/review-disposition.md`、`docs/review-evidence.md`、`rules/KANDER-{REVIEW,KANBAN,TASK-GROUP}-RULES.md`、`AGENTS.md`、`README.md` 及相关测试。为判定影响追踪了未改动的消费者：`internal/board/{board,update,transaction}.go`、`internal/review/{validate,execute,args}.go`、`internal/launch/metadata.go`、`internal/takeover`。S/D/A 已闭批的基线内容不重审。

## 需求追踪表

| # | 需求（契约出处） | 期望行为 | 代码证据 | 状态 |
| --- | --- | --- | --- | --- |
| R-00 | GOAL：建立可走完的批次完成门禁 | 门禁拒绝无效证据，但合法流程（审核→聚合→闭批→done）始终有前进路径 | `review_closure.go:99-127`、`review_gate.go:157-181`、`args.go:155-170` | Partial（PM-01） |
| R-01 | AC1 作者处置记录绑定与提交者边界 | run/finding/batch/task/author/时间/report hash/原文/修复 SHA/状态绑定；仅 working 卡当前 OWNER 提交；编排意见单列 | `review_disposition.go:190-235`（ReviewDisposition）、`:234-260`（validateDisposition 身份/归属）、`:268-296`（`tx.Expect(...,"working",rev)` + OWNER 校验）、`review_closure.go:369-386`（Opinions 独立 author/basis） | Complete |
| R-02 | AC2 按 assignment 聚合、逐卡发布、无 finding 成员不派回 | 完整 assignment 覆盖全部条目；跨卡多归属各自记录；视图发布到每张成员卡 | `review_disposition.go:151-215`（AssignReviewFindings/Owners）、`:325-395`（readDispositionLedger complete 覆盖）、`review_closure.go:205-230`（PublishReviewDisposition 逐卡 Put）、测试 `review_gate_test.go:143-184` | Complete |
| R-03 | AC3 固定结构化 FINDINGS/NON_BLOCKING + 稳定 ID + lineage | prompt 固定块；仅解析专用围栏；身份 run_id+finding_id；lineage 指向立即前驱 | `roles.go:117-133`（structuredFindingRules）、`git.go:184`（并入 prompt）、`review_findings.go:86-107`、`review_closure.go:129-180`（validateFindingLineage） | Complete |
| R-04 | AC4 非法/缺失/重复 ID 不自动通过；旧报告显式映射 | 重复围栏/重复 JSON key/未知字段/缺数组/空映射均拒绝；legacy 需逐字行范围 | `review_findings.go:52-79`、`:120-152`（validateLegacyMap）、`:158-201`（uniqueReviewJSON）、`review_disposition.go:110-150` | Complete |
| R-05 | AC5 状态集合与 waiver 限制 | must-fix 五态、非阻断三态、fixed 绑 SHA+验证、waiver 仅 CSA/Hacker | `review_disposition.go:234-266`（含 `run.Role != "CSA" && run.Role != "Hacker"` 拒绝、timed-out ≥15min 且不在未来）、`review_closure.go:322-336`（confirmed/unverifiable 阻断闭批） | Complete |
| R-06 | AC6 机械/非机械与 PASSED_AT；ok≠PASS | 仅机械项可前移 PASSED_AT；非机械 must-fix 必须新运行；执行 ok 不等于 PASS | `review_closure.go:337-353`（`non-mechanical fix requires a new run`）、`:354-356`（`PASSED_AT advance requires mechanical fixes`）、`review_advance.go:12-51`（独立 CAS） | Complete |
| R-07 | AC7 显式 plan、N/A 依据、空索引/未运行/全失败拒绝 done | 无计划不能 done；四角色 required 或 `N/A: <理由>`；非 Git 全 N/A 记录不适用 | `review_plan.go:41-51,55-170`、`review_gate.go:40-77`、`review_plan.go:255-272`（noGitReviewBatch）、`disposition.go:170-181` | Complete |
| R-08 | AC8 旧完成卡 legacy-untracked、活动卡 requirements-needed、pending 与结构错误区分、升级后的活动周期需建立要求 | 新执行周期能够建立自己的审核要求 | `review_gate.go:44-63`（legacy-untracked / requirements-needed）、`:130-145`（pending）；但 `review_plan.go:129` 禁止同卡第二份计划，`:197` 对周期变更直接报错 | Partial（PM-03） |
| R-09 | AC9 闭批条件、batch_id 不变、target CAS、后批 base 链、去重、部分发布不算完成 | 全部必需角色+作者处置+未解决项满足才 closed；后批 base = 前批 closed 目标 | `review_closure.go:243-390`、`:392-460`（CloseReviewBatch）、`review_plan.go:230-253`（validatePreviousClosure）、`review_closure.go:471-489`（verifyClosureCopies）、测试 `review_gate_test.go:293-327,455-469` | Complete |
| R-10 | AC10 Git 分层：review 验证最终 HEAD/祖先关系并绑定证据；board 只做纯结构 | board 不 import review；close 校验 CWD/HEAD/edges | `disposition.go:132-146`（verifyClosureHead）、`:110-131`（verifyGitEdge）、`:170-195`（closeWithGit）、`review_closure.go:420-424`（evidence 绑定）；`internal/board` 无 `internal/review` 导入 | Complete |
| R-11 | AC11 增量自动载入前驱原件/处置/批次视图；错身份拒绝；前轮可为 FAIL | 原文并入 prompt；显式 reviewed-commit 必须匹配；手工上下文不替代原件 | `disposition.go:198-223`（hydrateIncremental）、`review_context.go:29-93`、`reviews.go:384-392`（意图落盘前再校验快照）、测试 `disposition_test.go:101-163` | Partial（PM-02） |
| R-12 | AC12 check 与 move done 共用验证 + 新增用例 | 同一 validator；覆盖缺角色/全失败/部分发布/空结论/旧 ID/错归属/跨批/机械/N/A/旧卡/并发 | `review_gate.go:36-146` 被 `snapshot.go:210-213` 与 `deps.go:329-333` 共用；`review_gate_test.go` 全量用例 | Complete |
| R-13 | AC13 本仓库 CSA/Hacker N/A、规则与文档更新、三语字符串 | 规则文本与实现一致 | `AGENTS.md:7-8`、`rules/KANDER-KANBAN-RULES.md:638-682`、`rules/KANDER-TASK-GROUP-RULES.md:295-322`、三语 `review.gate_usage`/`review.usage`；但 `rules/KANDER-REVIEW-RULES.md:240,260` 未同步 | Partial（PM-04） |
| R-14 | AC13 `go test ./...`、`-race`、build/vet/格式检查通过 | 全量验证通过 | 本次审核仅有 Read/Grep/Glob，未执行任何命令；作者报告为声明 | Unverifiable |

**状态统计**：Complete 10 / Partial 4 / Missing 0 / Contradicted 0 / Unverifiable 1（共 15 项）。

## 门禁问题

### PM-01 — high：结构不合法但 execution_status=ok 的报告会永久堵死整个批次与所有成员卡

- 声明类型：代码路径与后果为 Observed；触发频率为 Inferred（有充分支撑：报告结构由 LLM 自由输出，工具本身即为此设置了解析校验）。
- 证据：
  - `internal/review/args.go:155-170` 归档路径只检查报告非空/UTF-8/agent 信封，**不校验 `kander-findings` 块**；`internal/review/execute.go:251-256` 随后以 exit 0 结束，`internal/review/archive.go:198-207` 据此写入 `execution_status="ok"`，`internal/board/reviews.go:493` 允许该终态。
  - `internal/board/review_disposition.go:66-72`：`findings_schema>0` 时解析失败直接返回错误，且 `:110-118` 明确拒绝用 legacy 映射补救（`new reports cannot use legacy mapping`）。
  - 该错误向所有出口传播：`review_closure.go:110-111`（aggregateReviewBatch 对**批次内每个 ok 运行**调用 runFindings）→ `ReadReviewBatchView` / `PublishReviewDisposition`(aggregate) / `CloseReviewBatch` 全部失败；`review_gate.go:172`（checkPendingDispositions）→ `ReviewTaskProgress`、`CheckReviewGate`(`deps.go:329`)、`MoveWithOptions(..., "done")`(`snapshot.go:210-213`) 全部失败；`review_check.go:121-125` 也把它报成 check 问题。
  - 没有任何取代路径：`review_closure.go:304-306` 的 `resolved_failures` 只接受 `ExecutionStatus != "ok"` 的运行；`review_disposition.go:94` 只允许对 ok 运行做后续操作；`reviews.go:350-355` 禁止在已闭批批次上新建运行；`review_plan_extend.go:62`(validatePreviousClosure) 要求前批已 closed 才能追加下一批；`review_plan.go:129` 禁止同卡第二份计划。
- 用户影响：任一次 Reviewer 输出漏掉/写坏 `kander-findings` 块（例如少一个数组、ID 重复、多一个围栏），该批次即**无法聚合、无法闭批**，其全部成员卡**永远无法进入 done**，`kander check` 对这些卡持续报错，且工具内不存在合法恢复入口（只能违反规则手工删改控制目录原件）。这与 GOAL 的"建立完成门禁"相悖：门禁变成了不可逆的陷阱，而契约在 AC9/AC12 中为"失败运行"专门提供了 `resolved_failures` 取代通道，唯独结构不合法的 ok 运行没有对应通道。
- 最小产品修复：在归档层（`internal/review/args.go` 解析报告处或 `archiveExecution.finish` 之前）当 `FindingsSchema>0` 且 `board.ParseReviewFindings(report)` 失败时，按既有"输出无效"语义把该次运行记为 `execution_status="failed"` 并写 FailureReason（原件、raw 输出照常保留）。这样它落入现有 `resolved_failures` 显式取代路径，不放宽任何结构校验，也不引入新命令。

### PM-02 — medium：task 绑定的增量调用会静默丢弃调用方传入的 review-context

- 声明类型：Observed。
- 证据：`internal/review/disposition.go:221-222`
  ```go
  result := append([]string{}, arguments[:5]...)
  return append(result, string(context), previous.Commit), nil
  ```
  `arguments[5]`（位置参数 review-context）被工具生成的上下文整体替换，无任何提示；相邻的 reviewed-commit 走的是显式校验（`:209-211` 不匹配即报错）。之所以必须丢弃，是因为 `internal/board/reviews.go:384-392` 要求 `review-context.md` 的摘要与 `incrementalReviewContext` 逐字节相等，任何追加都会触发 `incremental source changed before intent publication`。
- 用户影响：`rules/KANDER-REVIEW-RULES.md:240` 与 512 段仍要求调用方在 review-context 中提供上一轮结论、"实现由其他 Agent 完成"等说明。按规则传入这些内容的调用方，在增量轮会**无声地**失去全部自定义上下文，Reviewer 拿不到本应看到的材料，而命令返回 0、无警告。`docs/review-disposition.md` 承诺的是"手工 review-context 不替代原件"，而不是"手工内容被丢弃"。
- 最小产品修复：在 `hydrateIncremental` 中，当 `len(arguments) >= 6 && strings.TrimSpace(arguments[5]) != ""` 时返回明确错误（与 `incremental commit mismatch` 同等对待），提示增量轮上下文由工具生成、请勿手工传入。

### PM-03 — medium：计划绑定执行周期后无法重建，重新 claim 会使卡片永久无法 done

- 声明类型：Observed（代码路径）；触发条件为具体但受支持的操作路径。
- 证据：
  - `internal/board/review_plan.go:52-53` `planCycle` = SHA256(taskID + STARTED_AT)，`:197` 只要当前 STARTED_AT 变化即返回 `plan execution cycle mismatch`（该校验被 `review_gate.go:71` 的 `verifyPlanCopies` 在 check 与 done 路径上强制执行）。
  - `internal/board/update.go:44`：`kander move <id> working --owner <agent>` 会重写 STARTED_AT；`internal/board/board.go:32` 允许 `review → working`，`rules/KANDER-KANBAN-RULES.md:392,523` 正是把该命令定义为用户指定 Agent 认领卡片的入口。
  - `internal/board/review_plan.go:126-131`：只要 `task-plans/<id>.json` 指针存在就返回 `execution already has a review plan`，因此无法为新周期建立计划；`review_gate.go:49-54` 又因 tracked-cycles 存在而禁止回落为 legacy。
- 用户影响：卡片已有计划后（典型是编排端在 review 阶段建计划），用户显式让另一个 Agent 认领该卡（`move working --owner`），此后 `kander check` 对该卡恒报 `plan execution cycle mismatch`，`move done` 恒被拒，且工具内无法建立新计划——卡片进入不可完成状态。注意：规则要求的"派回不更换 owner"路径不受影响，仅显式重新认领会触发。
- 最小产品修复：`CreateReviewPlan` 在发现指针已存在时，比较 `tracked-cycles/<id>.json` 中已记录的 cycle 与当前 `planCycle(s)`；仅当二者不同（确属新执行周期）才允许指针指向新计划，旧计划、旧批次与其全部原件保持只读留存。这不放宽"计划不可被覆盖、不可换 plan ID 丢弃失败轮"的约束（同一周期内仍然拒绝）。

### PM-04 — medium [mechanical]：规则文件对增量调用的描述与本次实现及同文件新增章节冲突

- 声明类型：Observed。
- 证据：`rules/KANDER-REVIEW-RULES.md:260` 仍写着 "An incremental invocation includes `--previous-run-id` as well as the positional reviewed-commit. Its predecessor must be a fully published run for the same batch, base and role, whose commit equals reviewed-commit."；`:240` 仍写着 "the review-context must contain a non-empty list of last round's findings"。而本次新增的同文件 `:572-576` 写的是 "reviewed-commit may be omitted and an explicit value must match"，实现上 `internal/review/disposition.go:198-223` 自动补齐 reviewed-commit 与 review-context，`internal/board/reviews.go:378` 还新增了前驱必须同 `Reviewer` 的条件（`previous.Reviewer != input.Reviewer` 即拒绝），三点均未在 240/260 反映。
- 用户影响：同一规则文件对同一命令给出互相矛盾的调用契约，按 260 行操作的编排 Agent 会以为 reviewed-commit 必传、review-context 由自己组织，与工具实际行为不符。
- 最小产品修复：改写 `rules/KANDER-REVIEW-RULES.md:240,260`，说明 task 绑定增量调用可省略 reviewed-commit（显式传入必须匹配）、review-context 由工具从前驱原件生成，并补上"前驱须同 reviewer"。

## NON-BLOCKING

- **PM-05 — low**：`kander review advance` 未把 Git 校验绑定到计划的 CWD。`internal/review/disposition.go:51-66` 用调用方传入的 `cwd` 做 `verifyClosureHead` 与 `validateReviewAdvance`，`internal/board/review_advance.go:12-51` 也不读取 `batch.PlanID`/计划 CWD；而 `plan`（`disposition.go:41`）、run（`internal/board/reviews.go:356-361`）、`close`（`review_closure.go:420-424`）都做了 CWD 绑定。影响很小（提交对象与归属仍被逐一验证），但同一批次的 Git 证据来源不再一致。最小改动：在 advance 路径读取 `batchPlan` 并要求 `p.CWD == cwd`。
- **PM-06 — suggest**：`internal/board/review_plan.go:154` 把 `{"cycle": ...}` 写入 `tracked-cycles/<id>.json`，但唯一的读取点 `internal/board/review_gate.go:49` 只判断文件是否存在，该 cycle 值从未被比较（周期比较用的是计划内的 `p.Cycles`，`review_plan.go:196-197`）。作为一份持久化的"机器可查"控制记录，写入一个从不校验的值容易被误当作权威依据。可选改动：要么在门禁中真正比较它（也正好支撑 PM-03 的修复），要么写成不含推断性内容的标记文件。
- **PM-07 — recommend**：`internal/review/disposition.go:224-234` 的 `sameTasks` 重复实现了标准库 `slices.Equal`，而本仓库同批代码（如 `internal/board/review_plan.go:242` `slices.Equal`、`review_closure.go` 的 `slices.SortFunc/Compact`）已统一使用 `slices`。可选改动：删除 `sameTasks`，改用 `slices.Equal(previous.TaskIDs, options.tasks)`。

## QA 原报告

Write and Bash are disabled for this session (consistent with the reviewer's read-only tool restriction), so the report is delivered here directly. No files, index, refs, or worktree state were modified; `/tmp/claude-review.8807540d83ecda27f9458e007d9762d4/prompt.txt` could not be deleted for the same reason, which per the task file does not affect the result.

---

Role: QA
Commit: `d325bb9755cba0ac704a6841cd1d2d786eed42ee`（base `8241a49b1f50cbb99acc68b4c3dc58b5a803253e`）
Task Context: 任务组 `20260907-review-archive-group` / 卡 `20260907-review-disposition-gate-task`（R）——《原作者审核结论、批次证据汇总与不可绕过的完成门禁》，以完整 spec 为冻结契约。
Reviewed Scope: evidence.txt FILE LEDGER 全部 30 个路径，以及被其直接影响的未改动消费者（`internal/board/transaction.go`、`transaction_lock.go`、`update.go`、`board.go`、`size.go`、`document.go`、`internal/review/review.go`、`args.go`、`internal/takeover`）。S/D/A 已闭批的基线内容仅作上下文，不重审。Windows 原生行为按前批保留项处理，不据交叉编译冒称实测。

## 行为/质量表

| AC | 行为与关键证据 | 结论 |
| --- | --- | --- |
| 1 作者处置绑定 | `ReviewDisposition` 绑定 record/run/finding/batch/task/author/report_hash/original/status/basis（review_disposition.go:26-45）；`SubmitReviewDisposition` 用 `tx.Expect(d.TaskID,"working",rev)` + 当前 OWNER 校验（:264-275）；`a.Owners` 在 assign 时冻结（:190-208）；追加链由 `PreviousRecordID` CAS（:330-345） | 满足 |
| 2 全批聚合 | `aggregateReviewBatch` 按 `batchRuns` 去重 run，对每个 ok run 要求完整 assignment + 全归属作者记录（review_closure.go:100-126、review_disposition.go:322-410）；`PublishReviewDisposition` 单事务发布到全部成员（review_closure.go:169-194）；无 finding 成员不派回（review_gate_test.go:263-284） | 满足 |
| 3 结构与 lineage | 固定 `kander-findings` 单围栏、双数组、稳定 ID；重复 ID/重复 JSON key/未知字段/正文 ID 全拒（review_findings.go:59-105、160-201）；`validateFindingLineage` 只接受立即前驱且唯一（review_closure.go:132-177） | 满足 |
| 4 旧报告 | `findings_schema=0` 才可走 `map-legacy`，需 report_hash/author/basis/complete + 逐条行范围逐字 quote，空映射拒绝（review_findings.go:129-156、review_disposition.go:107-148） | 满足 |
| 5 状态与例外 | confirmed/unverifiable 阻断闭批（review_closure.go:329-331）；fixed 绑定 SHA+verification 且须严格晚于被审提交（review_disposition.go:231-233、review_closure.go:338-342）；waived 仅 CSA/Hacker 且保留非 PASS 状态（review_disposition.go:240-256、:332-334） | 满足 |
| 6 机械/非机械 | `advance` 独立 CAS 推进目标、无需空跑 Reviewer（review_advance.go:12-51）；非机械项要求新 run、无机械项禁止抬高 PASSED_AT（review_closure.go:346-359） | **见 QA-01** |
| 7 显式 plan | 四角色 required 或 `N/A: <依据>`（review_plan.go:40-51）；缺计划/未 seal/成员未分批/批未闭均不能 done（review_gate.go:48-64、73-84、148-154）；非 Git 全 N/A 记 `git.not_applicable`（review_plan.go:278-296、review/disposition.go:171-179） | 满足 |
| 8 活动及旧卡 | 旧 done/archived 无计划 = `legacy-untracked`；有 tracked-cycles 却丢计划 = 错误；活动卡缺计划 = `requirements-needed`（review_gate.go:48-64） | **见 QA-02** |
| 9 关闭与批次链 | `extend-plan` 仅在前批 closed 后追加（review_plan_extend.go:41-95）；`validatePreviousClosure` 要求前批 closed 且 `TargetCommit == b.Base`（review_plan.go:255-276）；batch_id 不变、target 仅经 CAS 推进、部分发布不算完成（review_closure.go:405-423、review_gate_test.go:277-283） | 满足 |
| 10 Git 分层 | Git 对象/祖先/干净 HEAD 全在 review 层（review/disposition.go:132-196）；board 只比对 `edges`/`CWD`/`Head`，且不 import review（grep 确认无 `internal/review` 引用） | 满足 |
| 11 自动增量 | `hydrateIncremental` 自动带入前驱原报告 + 作者记录 + 工具批次视图（review/disposition.go:198-223）；意图落盘前重算摘要（reviews.go:384-392）；同 run 重试用冻结输入（`ReadReviewInput`）；语义 FAIL 可复审（review_context.go:15-16、disposition_test.go:104-108） | 满足 |
| 12 共用门禁与回归 | `check` 与 `move done` 同一 `validateTaskReview`（deps.go:329、snapshot.go:186-213）；AC12 列举的 12 类用例均有对应测试（review_gate_test.go、review/disposition_test.go） | 满足 |
| 13 规则与验证 | 三语 `review.usage`/`review.gate_usage` 齐全；rules 三份 + `docs/review-disposition.md` 更新；本仓库 CSA/Hacker 保持 N/A | **见 QA-04** |
| 文件规模 | 本次新增/改动的非生成代码文件最大 `internal/board/reviews.go` 911 行、`review_closure.go` 531 行；无文件被创建于或推过 1000 行 | 规模规则不触发 |

## Gate findings

### QA-01 — medium — 作者自填 `mechanical` 未与 Reviewer 条目绑定，"非机械修复必须新运行" 门禁可被单字段绕过

- Claim: Observed
- 证据：`internal/board/review_disposition.go:237-239` 对处置的 `mechanical` 只校验取值合法（`documentation|dead-code|redundant-test`），**不与报告条目的 `item.Mechanical` 比较**；`internal/board/review_findings.go:73-75` 表明 Reviewer 条目本身携带权威 `mechanical` 且已入库（`ReviewRunView.Findings`，随时可比）；`internal/board/review_closure.go:346-351` 的判定分支只读 `d.Mechanical`。
- 可达路径：PM 报出一条 `tier: medium`、**不带** `mechanical` 的逻辑缺陷 → 执行端提交 `status=fixed, mechanical=documentation` 的处置（`validateDisposition` 通过）→ `review advance` 推进批次目标 → `review close` 时 `d.Mechanical != ""`，跳过 `non-mechanical fix requires a new run`，`mechanicalFix=true` 使 `PASSED_AT` 合法抬到修复提交 → 闭批成功，卡随后可 `move done`。
- 影响：AC6 的"非机械修复要求新运行"与 THREAT_MODEL 的"防结构性假通过"被一个字段解除；真实逻辑修复可在没有任何角色重跑覆盖修复提交的情况下取得 PASS 并完成执行周期。Reviewer 提示语已明确 "Never label a logic fix mechanical"（`internal/review/roles.go:124`），因此这是机器可查却未查的一致性缺口，而非"由调用者如实提供"的事实项。
- 最小修复：在 `validateDisposition`（`review_disposition.go:237` 处）加一行 `if d.Mechanical != "" && d.Mechanical != item.Mechanical { return reviewError("mechanical classification must match the reported finding") }`。

### QA-02 — medium — 计划执行周期摘要绑定 STARTED_AT，却无重建计划入口：换 OWNER 后整组卡永久无法 done

- Claim: Observed
- 证据：`internal/board/review_plan.go:52-54` `planCycle = SHA256(taskID + "\n" + STARTED_AT)`；`review_plan.go:196-198` `verifyPlanCopies` 对**计划全部成员**严格比对 `p.Cycles[id] != planCycle(s)` → `plan execution cycle mismatch`；`internal/board/update.go:44` `move ... working --owner` 会重写 `STARTED_AT`（`board.go:32` 允许 `review → working`；`rules/KANDER-KANBAN-RULES.md:392` 把该命令列为正式认领入口）；`review_plan.go:120-131` 对已有 plan 指针的任务一律拒绝新计划（`execution already has a review plan`），复用同 plan ID 又会因 `Cycles` 变化触发 `:113-115` 的 `immutable review plan conflict`。
- 可达路径：组计划覆盖 8 张成员卡（创建时全部在 working/review）→ 其中一张需要交接：`kander move <id> review` 后 `kander move <id> working --owner other` → 该卡 `STARTED_AT` 变更 → 此后任一成员卡 `move done` 都经 `snapshot.go:211 → review_gate.go:70-72 → verifyPlanCopies` 失败，`kander check` 对 8 张卡全部报 `plan execution cycle mismatch`。计划不可变、新计划被拒，工具内没有任何恢复路径。
- 影响：一次符合规则手册的所有者交接会把整个任务组的完成门禁永久锁死，只能靠手改控制目录（规则明确禁止）解除；与 AC8"升级后的活动执行周期需建立审核要求"及"check 区分合法进行中与结构错误"相悖——新周期应回落到 `requirements-needed`，而不是不可恢复的硬错误。
- 最小修复：把周期变更从硬错误改为"需要新周期计划"。`tracked-cycles/<id>.json` 已落盘 `cycle` 值但目前只用其存在性（`review_plan.go:154`、`review_gate.go:49-55`）；在 `validateTaskReview` 读到计划后比对该值，若当前 `planCycle(s)` 不同则返回 `progress.Status = "requirements-needed"`（completion 时报错），并把 `review_plan.go:128-130` 的指针检查放宽为"仅当指针指向的计划周期仍匹配时才拒绝新计划"。

### QA-03 — medium [mechanical] — `CheckBoard` 中检查 `CheckReviewGate` 错误的分支不可达

- Claim: Observed
- 证据：`internal/board/review_gate.go:204-212` `CheckReviewGate` 把每个任务的错误折叠进 `problems`，函数体唯一返回是 `return problems, nil`；因此 `internal/board/deps.go:329-333` 的 `if gateErr != nil { ... }` 永远不会执行，其 error 返回值也永不被使用。
- 影响：不可达分支加永不产生的 error 返回值，读者会误以为存在整板级失败通道，并与相邻 `CheckReviewEvidence`（确实会返回 error）的形状混淆。
- 最小修复：把签名改为 `func CheckReviewGate(root string, ids []string) []Problem`，删除 `deps.go:330-332` 的分支。

### QA-04 — medium [mechanical] — `rules/KANDER-REVIEW-RULES.md:260` 与本次实现及同文件第 574 行自相矛盾

- Claim: Observed
- 证据：`rules/KANDER-REVIEW-RULES.md:260`（本次未改动）仍写 "An incremental invocation includes `--previous-run-id` as well as the positional reviewed-commit. Its predecessor must be a fully published run for the same batch, base and role, whose commit equals reviewed-commit."；本次新增的 `:574` 写 "reviewed-commit may be omitted and an explicit value must match"；`docs/review-evidence.md:49` 已同步为"reviewed-commit 可省略""前驱须同 batch/base/role/**reviewer**"。实现侧 `internal/review/disposition.go:198-223` 自动补齐 reviewed-commit，`internal/board/reviews.go:378` 新增 `previous.Reviewer != input.Reviewer` 前驱条件。
- 影响：同一份权威规则文件对增量调用给出两条互斥指令，且第 260 行遗漏了本次新增的 reviewer 前驱约束；照第 260 行操作的 Agent 会误以为必须传 reviewed-commit，并可能在中途换 reviewer 后误判增量链合法。
- 最小修复：将 `:260` 改为"任务绑定的增量调用只需 `--previous-run-id`，positional reviewed-commit 可省略、显式传入必须等于前驱 commit；前驱须为同 batch、base、role、reviewer 且已完整发布的运行"。

## NON-BLOCKING

- low — `review advance` 与 `review extend-plan` 是唯一不绑定 plan CWD 的受控命令。`internal/review/disposition.go:51-66` 只做 `verifyClosureHead(cwd, target)`，`:67-76` 只做 `verifyGitEdge(cwd, ...)`，都未把 `cwd` 与 batch/plan 的 `CWD` 比对；而 `plan`（:41-42）、`close`（board 侧 `evidence.CWD != p.CWD`）、`PrepareReviewRun`（`reviews.go:356-361`）均强制绑定。后果很小（推进/接续的正确性由完整 SHA 与 `rev-list` 归属决定），但在"同 commit、同样干净"的另一个克隆里执行也会被接受，破坏 CWD 绑定这条不变量的一致性。最小改动：这两个分支在调用 board 前读一次 batch/plan 并比对 `p.CWD != cwd` 即报错。
- suggest — `internal/board/review_gate.go:179-188` 为判断 ledger 是否存在而完整解码一份 `dispositionLedger`（局部变量 `ledger` 此后从未被读取），随后 `readDispositionLedger` 又重新读取并解码同一文件。最小改动：去掉探测，让 `readDispositionLedger` 支持"不存在即跳过"的语义，或加一个只判存在的轻量 helper。
- suggest — `execution_status=ok` 但报告缺少合法 `kander-findings` 块的运行，在 finalize/publish 阶段没有任何提示（`FinalizeReviewRun`、`PublishReviewRun` 都不解析报告），错误只在 `review assign`/`aggregate` 或 `kander check`（`review_check.go:121-125`）才暴露；那时原件已不可变，必须换新 run ID 重跑 Reviewer。最小改动：在发布回执处（`review.archive_card_published` 附近）对 `FindingsSchema > 0 && ExecutionStatus == "ok"` 的运行做一次非致命解析并打印警告。
- suggest — `internal/board/review_gate_test.go:324-326` 先建一块临时看板并完整创建+闭批一个 sealed 全 N/A 计划，仅为得到一个 `ReviewPlan` 模板，随后 `root`/`id` 立即被覆盖，这段建板与断言完全被丢弃（该行为已由 `exemptReviewFixture` 与 `TestExplicitNAAndLegacyCompletedCard` 覆盖）。最小改动：直接构造模板结构体，去掉第一块看板。

## 说明

- 未发现 blocking / high 级问题。任务 GOAL 与 13 项 ACCEPTANCE_CRITERIA 的功能面均已落地，测试覆盖与 AC12 列举的用例一一对应；`board` 不反向依赖 `review`，Git 验证严格留在 review 层。
- 按 QA 角色规则未报告性能类问题（例如 `CheckReviewGate` 对每张卡各起一次事务并重复聚合整批，属显式排除范围）。
- Windows 原生执行缺口、PM/QA 组审核、组分支接收与 develop 集成按调用方交代保留，本报告不冒称实测。
- 作者交付报告仅作声明处理；以上结论全部来自 worktree 代码实读（Read/Grep/Glob），未执行任何测试或构建。
