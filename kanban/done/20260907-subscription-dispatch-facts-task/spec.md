# Pending dispatches are identifiable from snapshots and deadlines

- TYPE: Bug
- SIZE: small
- TASK_GROUP: 20260908-bindings-recovery-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 19:49
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t17:wX:p1S
- STARTED_AT: 2026-09-08 04:14
- FINISHED_AT: 2026-09-08 06:56
- TASK_BRANCH: subscription-dispatch-facts
- RESULT: completed

## GOAL

订阅用持久dispatch事实识别快速完成和待确认超时，而非等待采样边沿。

## USER_DECISIONS

用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。

后续实际决定：用户要求"拆好就开整吧"并答复"我授权"，授权本侧独立审卡、启动及按计划完成组交付/审核/develop集成与收尾；取代上方建卡阶段不启动限制。原审核归档组仍原主线程负责。

2026-09-08 用户在主会话决定："对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分." 本卡目标独立且可与派回证据绑定卡并行，保留不合并；仅因前置卡合并而改写 TASK_GROUP 与 PREREQUISITES，契约其余部分不变。

## EXPECTED_OUTCOME

本卡交付：订阅用持久dispatch事实识别快速完成和待确认超时，而非等待采样边沿。

## ACCEPTANCE_CRITERIA

- [ ] 快照/相关事件携带dispatch摘要及revision；review-working-review/done同一refresh内完成，下一快照仍证明同ID accepted/completed；重启不要求补历史边沿。
- [ ] pending review按原deadline触发注意事件和必要探测，不因其他事件续期；alive与无进展分开，输出状态/年龄，不自动恢复或放行。
- [ ] 反转 ReviewHasNoLiveness及RoundTrip中的业务回执缺失；覆盖notify返回前完成、订阅重启、待确认执行端退出和持续其他事件。
- [ ] 本行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包-race、build/vet/格式检查通过。平台相关测试使用临时目录，原生Windows及真实tmux/herdr/Agent未执行时列明缺口，不把假CLI或交叉编译记为实机通过。PM/QA按适用门禁，CSA/Hacker在本仓库N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

N事实接入E，不新增dispatch或编排真相源。

不引入通用服务、签名、保留期清理或无关优化。发现超出此单一行为的需求必须重新评估并另卡，不能为避免拆卡扩大本契约。

## DISCUSSION

```text
PREREQUISITES: 20260908-durable-dispatch-protocol-task,20260908-subscription-bounded-runtime-task
```

依赖必要性与不可拆分理由：持久派回协议（原N3，现 20260908-durable-dispatch-protocol-task）产生真实持久派回事实，订阅有界运行时（原E4，现 20260908-subscription-bounded-runtime-task）提供不会堵塞扫描的探测调度；核心订阅E1/E2/E3不反向依赖本卡。两个前置均为组外卡（20260908-subscription-dispatch-group），须 done 且交付在 develop 可用。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本行为交付的一部分，不单独拆成尾部测试任务。

资源边界：N事实接入E，不新增dispatch或编排真相源。 同组卡修改共同运行入口时串行；跨组只在资源可隔离且真实依赖满足时并行。无冲突的已就绪卡不为前卡非阻断建议等待；审核等待须注明具体门禁。本卡修改 subscribe，与同组 20260908-dispatch-evidence-binding-task（notify/board）可并行；20260908-coordinator-recovery-task 依赖本卡。

历史来源：20260907-subscription-reconcile-task；完整覆盖及主线程交接见 20260907-probe-result-classification-task 的 resplit-plan.md。原卡CARD_REVIEW不继承。

SELF_REVIEW: 通过粒度及契约自检：一个行为、可执行验收、必要依赖和范围排除已逐项核对；组内外依赖与全量映射另见计划。独立审卡现已通过，见下方真实CARD_REVIEW；由本侧主控按实际前置pick/start。

CARD_REVIEW: PASS — 2026-09-07，独立Agent /root/resplit_card_review（fork_turns=none），已只读完整契约、原需求、覆盖矩阵及粒度/必要依赖；本卡无阻断，旧PASS未继承。非阻断：资源依赖和技术依赖区分、OUT_OF_SCOPE正向范围措辞清晰、E5明确writer取消能力。仅契约通过，不代表实现/PM/QA通过。

2026-09-08 整合：原 dispatch-bindings-group 的 N4/N5 合并为 20260908-dispatch-evidence-binding-task，原 N1/N2/N3 合并为 20260908-durable-dispatch-protocol-task，原 E4/E5 合并为 20260908-subscription-bounded-runtime-task；本卡改入 20260908-bindings-recovery-group，前置随之改写，验收与范围未变。合并方案见 20260908-durable-dispatch-protocol-task 的 consolidation-plan.md。上方 CARD_REVIEW 针对旧组/前置；整合后的组关系与前置由新一轮独立审卡复核，结论另行追加。

CARD_REVIEW: PASS — 2026-09-08，独立子Agent（全新会话，只读整合后 6 张卡、12 张被合并原卡、resplit-plan.md 覆盖矩阵与 consolidation-plan.md）：目标单一且与用户 2026-09-08 决定一致，合并后验收完整覆盖原卡且可判定，前置存在、必要、无环且组内外分类正确，OUT_OF_SCOPE 与同组卡互斥。无阻断；非阻断建议（意图创建边界 kill 场景、保留“英文发布规则”字样、coordinator 第 8 项改为消费措辞、方案中 P3 并行表述）已由建卡者在相关新卡采纳修订。仅契约审查，不代表实现/PM/QA 通过。 本卡：契约与 Before 一致，前置改写为原 N3/E4 的正确承接，改组/改前置有 CONTRACT_DECISIONS 依据。

## IMPLEMENTATION

### 2026-09-08 首轮实现与交付

- 工作目录：`/home/dualf/works/kander/worktrees/subscription-dispatch-facts`；任务分支：`subscription-dispatch-facts`；交付目标：`group/20260908-bindings-recovery-group`。两个组外前置均已 done，交付在 develop 与创建基线可用；来源与最终 rebase 基线均为 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。最终交付：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
- board 新增可选 `ScanDispatchesContext` 与 `Board.CurrentDispatch`，在既有共享锁、journal 校验及读期限内捕获当前 dispatch 摘要和回执，并绑定同一快照的卡片 revision。旧扫描入口保持原行为；摘要省略消息正文，未绑定卡不补造历史回执，监听卡的原件缺失明确失败。交付：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
- subscribe 的 snapshot/相关事件携带当前 dispatch ID、epoch、状态、dispatch revision、创建与确认期限，以及 accepted/completed 的卡片 revision 与交付引用。快速 review-working-review/done 后仍可由下一快照辨认同 ID 回执；进程重启只需当前 snapshot。新授权取代后，历史 ID 仍由原 `dispatch show` 查询。交付：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
- prepared/delivery-unknown 按原 `confirm_by` 独立触发 `dispatch-attention`，输出派回年龄、待确认/超时状态与独立存活观测。pending review 纳入现有有界批量探测；已在途时合并一个待调度请求，批次结束后续采。accepted 不被套用完成期限；普通 review/completed review 不探测。订阅不自动恢复、重发、改状态或放行依赖。交付：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
- 同步更新中文 `docs/subscription-facts.md`、文档索引、英文发布协议及 cn/en/ja 消息。未新增编排真相源，未修改 notify 行为。交付：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。

#### 验收对应证据

1. 同 refresh 的 review-working-review 由原 `TestAuditRoundTripIsInvisible` 反转断言直接验证 accepted/completed；wrap-up 的 review-working-done 由 `TestSubscriptionFastDispatchWrapUp` 覆盖；快照摘要不泄露 payload。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
2. `TestAuditReviewHasNoLiveness` 覆盖 alive/stopped/unknown 与重启后的原确认期限；`TestSubscriptionDispatchDeadlineSurvivesOtherEvents` 用可控时钟证明持续其他事件不续期；`TestSubscriptionDispatchDeadlineDoesNotWaitForProbe` 证明慢探测不延迟注意事件；`TestSubscriptionAcceptanceEndsConfirmationDeadline` 区分接受与完成期限。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
3. `TestSubscribeRestartProcess` 使用两个独立测试进程，确认重启无需历史 working 边沿；`TestSubscriptionPendingExecutorExits` 覆盖待确认端从 alive 到 stopped；既有 `TestDispatchReceiptBeforeNotifyReturnAndRetry` 在本提交实际重跑，证明 notify 返回前已完成及同 ID 重试不再投递。该 notify 隔离测试与本卡订阅测试组合验证，不宣称真实终端端到端故障演练。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
4. 必要文档、三语消息、全量/race/build/vet/格式检查均已交付，具体命令与计数如下；PM/QA 由协调者在组分支安排，尚未执行本轮审核；CSA/Hacker 按本仓库 AGENTS.md 为 N/A。实现行为自验完成，完成门禁仍待组审核与集成。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。

#### Delivery Self-Check（七项）

1. `git diff --check ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3..HEAD` exit 0、无输出；对本提交改动文件扫描冲突标记，无匹配。通过。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
2. `git diff --name-only ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 HEAD` 枚举后逐文件物理行计数：12 个非生成 Go 文件均不超过 1000 行，最大为 subscribe.go 的 389 行；完整计数见验证汇总。通过。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
3. 对照 `git diff ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3..HEAD -- docs/subscription-facts.md rules/KANDER-KANBAN-RULES.md AGENTS.md internal/liveness internal/board/dispatch_snapshot.go`，文档与代码一致：已移除“没有 dispatch 回执/仅 working 探测”旧描述，补齐当前授权、原期限、失败关闭与平台边界。通过。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
4. `rg -n 'ScanDispatchesContext|CurrentDispatch|summarizeDispatch|dispatchAttention|dispatchWait|needsProbe|\.poll\(' internal/board/dispatch_snapshot.go internal/board/snapshot_context.go internal/liveness/subscribe*.go` 显示新读取、调度与序列化函数均有实际调用；逐项检查没有新增不可达/未引用代码，所有 worker 仍沿既有取消与汇合路径回收；`go vet ./...` exit 0、无输出。通过。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
5. 对照本提交测试差异，保留旧卡兼容与 review 回环测试，独立进程验证重启；新增 done、期限、执行端退出、慢探测与坏原件分别对应不同契约边界，删除重复的普通 review 完成测试分支。未加入无关断言。下述定向测试输出 12 个顶层测试、含子用例 17 PASS；无重复功能测试。通过。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
6. `go build ./...` exit 0、无输出；`go test -json -count=1 ./internal/liveness ./internal/board ./internal/notify -run 'TestAuditRoundTripIsInvisible|TestAuditReviewHasNoLiveness|TestSubscribeRestartProcess|TestSubscription.*Dispatch|TestSubscriptionPendingExecutorExits|TestSubscriptionAcceptanceEndsConfirmationDeadline|TestDispatchSnapshot|TestDispatchReceiptBeforeNotifyReturnAndRetry'` exit 0，3 包、12 个顶层测试通过，含子用例 17 PASS / 0 SKIP / 0 FAIL；完整 `gofmt -l <12 个改动 Go 文件>` exit 0、无输出（文件列表见汇总）。全部在最终交付提交实跑。通过。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
7. `go test -json -count=1 ./...` exit 0，19 包、596 个顶层测试通过，含子用例 934 PASS / 1 SKIP / 0 FAIL；唯一 skip 为非 Windows 平台的 `TestWindowsConsoleLauncher`。`go test -json -race -count=1 ./internal/board ./internal/liveness ./internal/i18n` exit 0，3 包、203 个顶层测试通过，含子用例 439 PASS / 0 SKIP / 0 FAIL。通过。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。

#### 验证边界、调试历史与交付状态

- `GOOS=windows GOARCH=amd64 go build -o /tmp/subscription-dispatch-final-kander.exe ./cmd/kander` exit 0；仅交叉构建，不代表原生 Windows 运行通过。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
- 本轮所有故障/回执测试使用临时看板及隔离 CLI，未修改真实看板来做测试。未执行原生 Windows、真实 tmux/herdr/Agent 的派回/退出端到端场景，两项平台验证缺口保留。提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
- 提交前调试曾出现测试夹具 `undefined: dispatch`、done 的 `SUMMARY` 缺失，以及 race 下 `unexpected events` / `deadline postponed: changes=1` 两个时序断言失败。已修正夹具、提前准备 done 门禁、放宽复合事务采样窗口，并以可控时钟检查持续事件；这些早期结果不是通过证据，最终提交结果以下列原件为准。
- `git rebase origin/group/20260908-bindings-recovery-group` 输出 `Current branch subscription-dispatch-facts is up to date.`；`git push -u origin subscription-dispatch-facts` 成功。`git ls-remote` 确认远端任务头为 `6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`，组头仍为 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`；`git merge-base --is-ancestor ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 HEAD` exit 0，`git status --short` 无输出。交付核验提交：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。
- PM/QA 尚未由协调端启动，无本轮审核 run ID 或 finding；CSA/Hacker N/A。组分支接收、develop 集成、工作树/分支清理均待协调者派回；任务 worktree、本地及远端任务分支保留。
- 原始证据：[验证汇总](verification/final-summary.json)、[全量测试](verification/final-all.jsonl)、[race 测试](verification/final-race.jsonl)、[定向测试](verification/final-targeted.jsonl)。每份最终验证均绑定 `6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`；全量原始日志中的子测试与顶层测试分别计数。

### 2026-09-08 集成后收尾

本段记录最终状态；前面的 IMPLEMENTATION、交付自检及验证原文保留为首轮历史。授权见 [收尾通知](wrap-up-notice.md)，核验原件见 [收尾核验](verification/wrap-up.json)。本轮先自行 review → working，没有修改代码、重跑 Reviewer、rebase 或再次集成。

- 本卡最终交付 `6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa` 未被重写，已包含于组最终提交 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`。fetch 后实查本地 develop 与 origin/develop 都等于组最终提交；分别对本卡交付及组最终提交运行 `git merge-base --is-ancestor <SHA> develop` 和 `git merge-base --is-ancestor <SHA> origin/develop`，四次 exit 0。主工作树干净，本卡 task HEAD 与通知交付相同，未跟踪与忽略文件检查均为空。核验目标：`26edb64fcfb654a96afedc30bbaea27e5e918ec7`。
- [计划 g3-bindings-recovery](reviews/plan.json) 已 sealed；[g3-batch-one 闭批原件](reviews/batches/g3-batch-one/closed.json) target 为 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`。`kander review progress /home/dualf/works/kander/worktrees/20260908-bindings-recovery-group 20260907-subscription-dispatch-facts-task` 返回 closed；组批次共 2 个修复轮，本卡没有归属 finding、没有修复提交或作者 disposition 义务。
- PM/codex PASS，闭批选用 [g3-pm-r2](reviews/g3-pm-r2/report.md)，passed_at `5845e6fd0f2b503313030349fa211a7791a50169`；QA/codex PASS，选用 [g3-qa-r5](reviews/g3-qa-r5/report.md)，passed_at `26edb64fcfb654a96afedc30bbaea27e5e918ec7`。PM 通过提交是最终 target 的祖先，本轮实查祖先关系 exit 0；已通过角色承接依据保留于闭批原件。CSA/Hacker 按本仓库 AGENTS.md 为 N/A，未执行。
- 首轮 [g3-pm-r1](reviews/g3-pm-r1/report.md)、[g3-qa-r1](reviews/g3-qa-r1/report.md) 及增量 [g3-qa-r2](reviews/g3-qa-r2/report.md) 的共 7 个来源 finding、4 个根因，均归属同组另外两卡，由其原作者 confirmed 后 fixed；本卡分配表为空。最终 FINDINGS/NON_BLOCKING 为空，无 rejected、无未核实 finding。专用 wrap-up epoch 的期限兼容与对应订阅文档修订属于 dispatch-evidence-binding 卡，本卡首轮原文保留为当时交付记录。
- [g3-qa-r3](reviews/g3-qa-r3/manifest.json) 记录 failed，[g3-qa-r4](reviews/g3-qa-r4/manifest.json) 记录 interrupted；通知和闭批意见说明为本机低内存杀手导致环境中断。本卡未独立诊断该环境根因。闭批 resolved_failures 已将两者指向成功替代 g3-qa-r5；失败原件不作为语义 PASS，也不计修复轮。
- 本作者此前在 `6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa` 的 `go test -json -count=1 ./...` 为 19 包、934 PASS / 1 SKIP，受影响包 race 439 PASS、定向 17 PASS，build/vet/格式通过；本轮没有重跑测试。协调者在 `26edb64fcfb654a96afedc30bbaea27e5e918ec7` 实跑 `go build ./...`、`go vet ./...`、`go test -count=1 ./...`，均 exit 0、19 包全部 ok，依据是闭批原件的 coordinator opinion，不能冒称本作者再次执行。
- 完成前置核验后，从主工作树执行 `git worktree remove /home/dualf/works/kander/worktrees/subscription-dispatch-facts`、`git branch -d subscription-dispatch-facts` 及以 `6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa` 为精确 lease 的远端任务分支删除，均 exit 0；复查本卡路径、本地分支、远端分支均不存在。组工作树及组分支保留。没有创建新的 Reviewer 临时输出，临时审核清理 N/A；卡内审核与验证原件全部保留，交互 CLI 与终端容器保留。核验见 [收尾核验](verification/wrap-up.json)。
- 验收 4/4：持久摘要与同 ID 原子回执、独立确认期限及必要探测、缺陷反转与快速完成/重启/退出/持续其他事件、必要文档三语消息和验证及 PM/QA 门禁均完成；下列实机缺口不冒称通过。
- 未解决项 2 项：[验证缺口][Unverifiable] 原生 Windows 未运行，仅有交叉构建与隔离测试；影响是 Windows 原生运行行为未实证，需要原生环境补验。[验证缺口][Unverifiable] 真实 tmux/herdr/Agent 派回及退出场景未运行，假 CLI 与本机临时看板只证明隔离场景，需要相应真实环境补验。
- 完成门禁 `kander move 20260907-subscription-dispatch-facts-task done --result completed` exit 0；随后定向 `kander check 20260907-subscription-dispatch-facts-task` exit 0，输出 `ok: 1 tasks`。见 [完成核验](verification/completion.json)。适用收尾步骤 all completed，Final card state: done。

## SUMMARY

本卡交付 `6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa` 已集成 develop，最终组提交 `26edb64fcfb654a96afedc30bbaea27e5e918ec7` 与本地、远端 develop 一致。验收 4/4；计划 g3-bindings-recovery sealed、g3-batch-one closed，2 个组修复轮，本卡无 finding。PM g3-pm-r2、QA g3-qa-r5 PASS；CSA/Hacker N/A。

任务 worktree、本地及远端任务分支已删除；组资源、卡内原件、交互 CLI 与终端容器保留。原生 Windows、真实 tmux/herdr/Agent 派回及退出两项验证缺口保留。本轮仅核验并收尾，未修改代码或重跑测试；首轮验证原文及最终组验证来源见上文。done 完成门禁及定向 check 均 exit 0；适用收尾步骤 all completed，Final card state: done。

## CONTRACT_DECISIONS

```json
{
  "at": "2026-09-08 00:30",
  "decision": "用户决定（2026-09-08，主会话原话）：\"对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分.\"\n\n据此：todo 中 2026-09-07 按单一行为拆出的 14 张卡，凡属同一目标且只能串行的合并为一张卡；只保留能真正并行的拆分。被合并的原卡归档为 duplicate 并指向替代卡，原契约、独立审卡记录与覆盖矩阵保留不改写。未启动任何卡，不改变 review 中 P1/P2/E1 及其组分支，不扩大集成/接管授权。\n",
  "Before": {
    "ACCEPTANCE_CRITERIA": "- [ ] 快照/相关事件携带dispatch摘要及revision；review-working-review/done同一refresh内完成，下一快照仍证明同ID accepted/completed；重启不要求补历史边沿。\n- [ ] pending review按原deadline触发注意事件和必要探测，不因其他事件续期；alive与无进展分开，输出状态/年龄，不自动恢复或放行。\n- [ ] 反转 ReviewHasNoLiveness及RoundTrip中的业务回执缺失；覆盖notify返回前完成、订阅重启、待确认执行端退出和持续其他事件。\n- [ ] 本行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包-race、build/vet/格式检查通过。平台相关测试使用临时目录，原生Windows及真实tmux/herdr/Agent未执行时列明缺口，不把假CLI或交叉编译记为实机通过。PM/QA按适用门禁，CSA/Hacker在本仓库N/A。",
    "EXPECTED_OUTCOME": "本卡交付：订阅用持久dispatch事实识别快速完成和待确认超时，而非等待采样边沿。",
    "GOAL": "订阅用持久dispatch事实识别快速完成和待确认超时，而非等待采样边沿。",
    "OUT_OF_SCOPE": "N事实接入E，不新增dispatch或编排真相源。\n\n不引入通用服务、签名、保留期清理或无关优化。发现超出此单一行为的需求必须重新评估并另卡，不能为避免拆卡扩大本契约。",
    "PREREQUISITES": "PREREQUISITES: 20260907-notify-durable-delivery-task,20260907-subscription-probe-isolation-task",
    "SIZE": "small",
    "TASK_GROUP": "20260907-dispatch-bindings-group",
    "USER_DECISIONS": "用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。\n\n\n后续实际决定：用户要求“拆好就开整吧”并答复“我授权”，授权本侧独立审卡、启动及按计划完成组交付/审核/develop集成与收尾；取代上方建卡阶段不启动限制。原审核归档组仍原主线程负责。"
  },
  "After": {
    "ACCEPTANCE_CRITERIA": "- [ ] 快照/相关事件携带dispatch摘要及revision；review-working-review/done同一refresh内完成，下一快照仍证明同ID accepted/completed；重启不要求补历史边沿。\n- [ ] pending review按原deadline触发注意事件和必要探测，不因其他事件续期；alive与无进展分开，输出状态/年龄，不自动恢复或放行。\n- [ ] 反转 ReviewHasNoLiveness及RoundTrip中的业务回执缺失；覆盖notify返回前完成、订阅重启、待确认执行端退出和持续其他事件。\n- [ ] 本行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包-race、build/vet/格式检查通过。平台相关测试使用临时目录，原生Windows及真实tmux/herdr/Agent未执行时列明缺口，不把假CLI或交叉编译记为实机通过。PM/QA按适用门禁，CSA/Hacker在本仓库N/A。",
    "EXPECTED_OUTCOME": "本卡交付：订阅用持久dispatch事实识别快速完成和待确认超时，而非等待采样边沿。",
    "GOAL": "订阅用持久dispatch事实识别快速完成和待确认超时，而非等待采样边沿。",
    "OUT_OF_SCOPE": "N事实接入E，不新增dispatch或编排真相源。\n\n不引入通用服务、签名、保留期清理或无关优化。发现超出此单一行为的需求必须重新评估并另卡，不能为避免拆卡扩大本契约。",
    "PREREQUISITES": "PREREQUISITES: 20260908-durable-dispatch-protocol-task,20260908-subscription-bounded-runtime-task",
    "SIZE": "small",
    "TASK_GROUP": "20260908-bindings-recovery-group",
    "USER_DECISIONS": "用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。\n\n后续实际决定：用户要求\"拆好就开整吧\"并答复\"我授权\"，授权本侧独立审卡、启动及按计划完成组交付/审核/develop集成与收尾；取代上方建卡阶段不启动限制。原审核归档组仍原主线程负责。\n\n2026-09-08 用户在主会话决定：\"对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分.\" 本卡目标独立且可与派回证据绑定卡并行，保留不合并；仅因前置卡合并而改写 TASK_GROUP 与 PREREQUISITES，契约其余部分不变。"
  }
}
```

## REVIEWS

- {"run_id":"g3-qa-r1","batch_id":"g3-batch-one","role":"QA","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"0b304a8399cdbc8db2c005e390afd3f6127532af","report":"reviews/g3-qa-r1/report.md"}
- {"run_id":"g3-pm-r1","batch_id":"g3-batch-one","role":"PM","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"0b304a8399cdbc8db2c005e390afd3f6127532af","report":"reviews/g3-pm-r1/report.md"}
- {"run_id":"g3-pm-r2","batch_id":"g3-batch-one","role":"PM","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"5845e6fd0f2b503313030349fa211a7791a50169","previous_run_id":"g3-pm-r1","report":"reviews/g3-pm-r2/report.md"}
- {"run_id":"g3-qa-r2","batch_id":"g3-batch-one","role":"QA","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"5845e6fd0f2b503313030349fa211a7791a50169","previous_run_id":"g3-qa-r1","report":"reviews/g3-qa-r2/report.md"}
- {"run_id":"g3-qa-r3","batch_id":"g3-batch-one","role":"QA","execution_status":"failed","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"26edb64fcfb654a96afedc30bbaea27e5e918ec7","previous_run_id":"g3-qa-r2","report":"reviews/g3-qa-r3/output.raw"}
- {"run_id":"g3-qa-r4","batch_id":"g3-batch-one","role":"QA","execution_status":"interrupted","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"26edb64fcfb654a96afedc30bbaea27e5e918ec7","previous_run_id":"g3-qa-r2","report":"reviews/g3-qa-r4/output.raw"}
- {"run_id":"g3-qa-r5","batch_id":"g3-batch-one","role":"QA","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"26edb64fcfb654a96afedc30bbaea27e5e918ec7","previous_run_id":"g3-qa-r2","report":"reviews/g3-qa-r5/report.md"}
