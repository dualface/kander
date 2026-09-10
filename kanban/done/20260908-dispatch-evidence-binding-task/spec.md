# Dispatch binds review originals, author disposition and fenced wrap-up authority

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260908-bindings-recovery-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 00:29
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t16:wX:p1R
- STARTED_AT: 2026-09-08 04:14
- FINISHED_AT: 2026-09-08 06:56
- TASK_BRANCH: dispatch-evidence-binding
- RESULT: completed

## GOAL

派回绑定可核验的证据：fix 派回引用审核原件与已有原作者处置，不因卡片移动或错轮次而错派；代收尾保留原编排例外，但必须持有隔离旧 epoch 后的 wrap-up-only 专用授权，阻止仍活动的执行端与代办并行写入。

## USER_DECISIONS

2026-09-07 用户在侧会话要求按单一可验收行为重拆剩余卡，并答复"我授权"允许独立审卡、启动及按计划完成组交付/审核/develop 集成与收尾；原四张大卡由拆分卡替代，原契约与审核历史保留。

2026-09-08 用户在主会话决定："对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分." 本卡即按此决定合并而成；被合并的原卡归档为 duplicate 并指向本卡，原契约、独立审卡记录及覆盖矩阵保留不改写。合并不扩大目标，不改变 review 中 P1/P2/E1 及其组分支，不扩大集成/接管授权。

## EXPECTED_OUTCOME

本卡交付：fix 派回在发送前校验 run/finding/batch/task 归属与轮次并按相对 ID 引用作者处置；wrap-up 派回绑定已验证集成证据与同 dispatch，delivery-unknown 先对账，只有确认原执行者退出或合法回收并隔离旧 epoch 后才授予 wrap-up-only epoch。

## ACCEPTANCE_CRITERIA

- [ ] 消费持久派回协议与 R 审核类型，fix 记录关联 run/finding/batch 及任务归属；已有原作者处置按原作者与相对 artifact ID 引用，不要求尚未产生的本轮处置预先存在。board 不反向依赖 review 或 notify。
- [ ] 发送前校验引用存在、归属及轮次；错误/跨批/错误任务/不完整原件明确拒绝，不篡改或补造原作者结论。卡片 move 后按任务 ID 重定位，不保存失效绝对报告路径；同 dispatch 重试保持原绑定。
- [ ] 覆盖合法 fix、缺 run/finding、错误归属、错误前驱、已有作者处置缺失、引用卡片 move、同 ID 恢复。旧无结构报告只消费 R 的显式映射，不靠正文猜 finding。
- [ ] wrap-up 绑定已验证集成证据与同 dispatch；引用相对 ID，不缓存跨状态绝对报告路径。
- [ ] delivery-unknown 先对账；仅确认原执行者退出或在既有授权下合法回收并隔离旧 epoch 后，授予 wrap-up-only epoch；无派回记录先建意图，记录作者/原因。
- [ ] 普通非零、确认超时、租期到期不能独自证明退出；无 SESSION、未知投递、仍活动、确认退出四类验收。只允许清理及记录，拒绝代码修改和冒充原作者结论，不改变用户接管权限。
- [ ] 本卡全部行为的测试、必要英文发布规则和中文仓库文档及三语消息随实现交付；go test ./...、受影响包 -race、build/vet/格式检查通过。平台相关测试使用临时目录，原生 Windows 及真实 tmux/herdr/Agent 未执行时列明缺口，不把假 CLI 或交叉编译记为实机通过。PM/QA 按适用门禁，CSA/Hacker 在本仓库 N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

- fix 引用校验与 wrap-up 专用授权及证据绑定；不实现 dispatch 存储、传输恢复（由 20260908-durable-dispatch-protocol-task 交付）、R 的 finding 解析/处置、订阅端消费或编排收尾对账（由 20260908-coordinator-recovery-task 交付）。
- 不自动执行 Git 集成或删除真实用户工作区，不改变用户接管权限。
- 不引入通用服务、签名、保留期清理或无关优化。

## DISCUSSION

```text
PREREQUISITES: 20260908-durable-dispatch-protocol-task,20260907-review-disposition-gate-task
```

依赖必要性：持久派回协议提供实际 fix/wrap-up 派回入口、稳定 dispatch 与授权链（组外，须 done 且在 develop 可用）；R 提供可验证的 finding/作者处置与映射（已 done）。这才是 R 的真实消费点。

合并理由：原 N4（20260907-wrapup-fenced-authority-task）与 N5（20260907-fix-review-reference-binding-task）同为"派回绑定合法证据"目标，且两卡共享派回绑定接线处、原卡明确要求不能共享工作区，实际无法并行，按用户 2026-09-08 决定合并。N5 前三项与 N4 前三项验收原文保留；原 N 验收 8/10 的 fix 与 wrap-up 部分均在本卡。

并行边界：本卡与同组 20260907-subscription-dispatch-facts-task 分别修改 notify/board 与 subscribe，可并行；20260908-coordinator-recovery-task 依赖本卡。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本卡交付的一部分，不单独拆成尾部测试任务。合并后各行为可分次提交，但整卡一次交付、一次验收；实现中发现超出本卡目标的独立新行为时另卡，不为避免拆卡扩大契约。

历史来源：20260907-durable-dispatch-task（原 N 验收 8/10/12）；拆分历史见 20260907-probe-result-classification-task 的 resplit-plan.md 与 20260907-fix-review-reference-binding-task 的 execution-plan.md，合并方案见 20260908-durable-dispatch-protocol-task 的 consolidation-plan.md。原卡 CARD_REVIEW 不继承。

SELF_REVIEW: 已核对目标单一（派回绑定合法证据）、六项行为验收与原 N4/N5 一一对应、前置为真实技术依赖且无环、与同组两卡边界互斥。待独立审卡。

CARD_REVIEW: PASS — 2026-09-08，独立子Agent（全新会话，只读整合后 6 张卡、12 张被合并原卡、resplit-plan.md 覆盖矩阵与 consolidation-plan.md）：目标单一且与用户 2026-09-08 决定一致，合并后验收完整覆盖原卡且可判定，前置存在、必要、无环且组内外分类正确，OUT_OF_SCOPE 与同组卡互斥。无阻断；非阻断建议（意图创建边界 kill 场景、保留“英文发布规则”字样、coordinator 第 8 项改为消费措辞、方案中 P3 并行表述）已由建卡者采纳修订。仅契约审查，不代表实现/PM/QA 通过。

## IMPLEMENTATION

工作树 `/home/dualf/works/kander/worktrees/dispatch-evidence-binding`，任务分支 `dispatch-evidence-binding`；来源组分支 `group/20260908-bindings-recovery-group`，创建基线 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。前置两卡均 done，其交付已在 develop 和组基线可用。

接口影响：board 扩展持久意图中的 fix/wrap-up 证据绑定与专用 epoch；launch 验证 Git 集成和退出事实，notify/resume 在发送前复核绑定。board 不反向依赖 review/notify/launch。旧未绑定通知保留，新 fix/wrap-up 要求完整绑定；sync 保留原协议。组内执行端不触发审核、不修改组分支，交付后进入 review。


### 2026-09-08 首轮交付与 Delivery Self-Check

最终交付 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`，任务分支 `dispatch-evidence-binding`；最新组基线 `6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`。已正常推送，同名本地/远端 HEAD 一致；`git merge-base --is-ancestor 6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa 6f5ed32072275c3bb9d96c9e40bd606b949b9c17` 退出 0（提交 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`）。首提交对齐组分支时 rebase 无冲突，后续提交补齐 epoch 期限接口和合法 rebase 的补丁证明。组分支及 develop 未由本执行端更新，任务资源保留。

交付实现及逐项验收自检见 [report.md](report.md)，最终命令/统计/调用链输出见 [validation.txt](validation.txt)。实现项 1–6 已自检；第 7 项自动化验证完成，PM/QA 仍由编排端组审核。当前没有本卡审核 run ID；不生成或冒充审核结论。

1. 差异：`git diff --check 6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa HEAD` 无输出、退出 0；修改文件逐行扫描冲突标记 0 个。提交 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`。
2. 行数：`git diff --name-only 6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa HEAD` 后按物理行统计 25 个 Go 文件，最多 448 行，均不超过 1000；逐文件数字见日志。提交 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`。
3. 注释及文档：`git diff --stat 6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa HEAD` 输出与实际最终 diff 逐段人工核对；AGENTS 包边界、中文 durable-dispatch 文档、英文 kanban/task-group 规则及三语消息同步，不保留“只校验引用形状”的旧行为声明。提交 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`。
4. 死代码：`rg -n` 的实际入口调用链见日志；结合最终 diff 人工核对，新增验证/授权函数均由命令或受控入口消费，无遗留未调用逻辑。`go vet ./...` 退出 0。提交 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`。
5. 测试：最终 diff 人工核对，无重复或无关断言。原通用回执/订阅夹具改用 sync，fix/wrap-up 用例独立验证原件、作者、Git、CAS、范围限制及期限；保留原 kill/restart 场景。提交 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`。
6. 编译和定向行为：`go build ./...`、`go vet ./...`、修改 Go 文件的 `gofmt -l` 均退出 0，无格式输出；`go test -race -json -count=1 ./internal/board ./internal/launch ./internal/notify ./internal/liveness ./internal/review ./internal/window` 为 6 包、684 测试项通过，1 项跳过（统计含子用例）。提交 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`。
7. 全量：`go test -json -count=1 ./...` 为 19 包、974 测试项通过，1 项 WindowsConsoleLauncher 跳过，退出 0（统计含子用例）。所有最终验证在提交 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17` 执行；先前提交的日志未当作最终证据。

Windows 交叉构建命令 `GOOS=windows GOARCH=amd64 go build -o /tmp/kander-dispatch-binding-windows.exe ./cmd/kander` 退出 0（提交 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`）；原生 Windows 与真实 tmux/herdr/Agent 交互未执行，不能记为实机通过。真实卡片 `kander check 20260908-dispatch-evidence-binding-task` 通过 1 个任务（工作树提交 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`，使用当前作用域已安装 kander，仅为结构与实际存活核对）。

PM/QA：待编排端接收后执行；CSA/Hacker：N/A，依据本仓库 AGENTS.md 明确特例。后续由编排端接收分支、闭批并授权集成，再派回收尾。未更新组分支、未集成 develop、未清理任务工作树或关闭交互容器。

### 2026-09-08 审核派回第 1 轮（g3-batch-one）

PM-03/QA-03 合并根因：订阅 confirm_by 文档与当前 epoch 期限不一致；独立核实 confirmed，修复后均 fixed（mechanical documentation）。原件与两条独立作者处置见 [本轮报告](review-fix-r1.md)，不复制原报告。NON_BLOCKING 均为空；本卡无未解决条目。PM-01/QA-02、PM-02/QA-01 归另一执行卡，未代判。

修复及 rebase 后最终交付 `287161673bf3f11a865f2e8a40a023a65bdc1f65`，组基线 `0b304a8399cdbc8db2c005e390afd3f6127532af`；无冲突，已推送任务分支。最终提交 287161673bf3f11a865f2e8a40a023a65bdc1f65：go build ./...、go vet ./... 均退出 0；go test -json -count=1 ./...：19 包、1001 测试项通过，1 项跳过（TestWindowsConsoleLauncher）；go test -race -json -count=1 ./internal/board ./internal/liveness：2 包、491 测试项通过，0 项跳过；测试统计含子用例，两条测试命令均退出 0。TestDispatchWrapUpSnapshotUsesCurrentGrantDeadline 通过，覆盖原意图已过期、新 grant 期限仍有效且原期限不变。逐句对照 snapshotDispatch、dispatchAcceptBefore、summarizeDispatch、dispatchWait 和 docs/durable-dispatch.md:97；本轮仅 docs/subscription-facts.md 与 rules/KANDER-KANBAN-RULES.md，8 行增加、6 行删除。

Delivery Self-Check（均针对 `287161673bf3f11a865f2e8a40a023a65bdc1f65`）：

1. `git diff --check 0b304a8399cdbc8db2c005e390afd3f6127532af HEAD` 退出 0、无输出；修改文件冲突标记扫描 0。
2. `git diff --name-only` 仅两份 Markdown，无代码行数门禁新增项。
3. 最终 diff 与 board/liveness 期限和年龄计算逐句核对，已同步中英文契约。
4. 无代码改动；无新增死代码；`go vet ./...` 退出 0。
5. 无测试改动；无新增冗余断言。
6. `go build ./...`、`go vet ./...` 退出 0；board/liveness race：2 包、491 测试项通过，0 项跳过，退出 0。
7. `go test -json -count=1 ./...`：19 包、1001 测试项通过，1 项跳过（TestWindowsConsoleLauncher），退出 0，统计含子用例。

原始命令证据见 [review-fix-r1-validation.txt](review-fix-r1-validation.txt)。原生 Windows、真实终端/Agent 缺口保留。PM/QA 后续由编排端执行；CSA/Hacker N/A。任务 worktree、分支与交互 CLI 保留，进入 review 等待接收。

### 2026-09-08 授权收尾

批次 g3-batch-one，sealed 计划 g3-bindings-recovery，2 个修复轮，已闭批。PM（codex）g3-pm-r2 在 5845e6fd0f2b503313030349fa211a7791a50169 PASS，最终 target 为其后代，按已通过角色承接规则保留；QA（codex）g3-qa-r5 在最终 target PASS。CSA/Hacker N/A，依据仓库 AGENTS.md。首轮 6 个来源条目合为 3 个根因，第一修复轮新增 QA-04 后共 7 个来源、4 个根因，均由原作者核实并 fixed，无 rejected/unverifiable finding，非阻断数组为空。本卡 PM-03/QA-03 同根文档问题处置原件保留。g3-qa-r3 failed 与 g3-qa-r4 interrupted 属主控报告的低内存环境中断，闭批 resolved_failures 均指向成功替代 g3-qa-r5，不算语义结论或额外修复轮。

本卡最终交付 287161673bf3f11a865f2e8a40a023a65bdc1f65；组分支 group/20260908-bindings-recovery-group 最终 26edb64fcfb654a96afedc30bbaea27e5e918ec7 已进入本地及远端 develop。fetch 后对 develop、origin/develop 各执行 git merge-base --is-ancestor 287161673bf3f11a865f2e8a40a023a65bdc1f65，均 exit=0；develop 与 origin/develop 双向祖先检查均 exit=0，主工作树 HEAD 相同且干净。组接收和 develop 集成全程 ff，本卡最终交付 SHA 未在集成期间改写，无新旧 SHA 映射。本执行端仅验证、未重新集成/rebase。已从主工作树依次删除本卡 worktree、本地分支和远端分支，三条命令均 exit=0；路径及本地/远端跟踪 refs 不存在。组资源留给主控处理。无本执行端单独启动的临时 Reviewer 运行目录需要清理，卡内审核原件全部保留。交互 CLI 与终端容器保留。

作者最终交付 287161673bf3f11a865f2e8a40a023a65bdc1f65：go build ./...、go vet ./... 退出 0；go test -json -count=1 ./...：19 包、1001 测试项通过，1 项 TestWindowsConsoleLauncher 跳过；go test -race -json -count=1 ./internal/board ./internal/liveness：2 包、491 测试项通过，0 skip；测试统计含子用例，均 exit=0。详见 review-fix-r1.md 与 review-fix-r1-validation.txt。主控在组最终提交 26edb64fcfb654a96afedc30bbaea27e5e918ec7 实跑 go build ./...、go vet ./...、go test -count=1 ./...，均 exit=0、19 包全部 ok（来源：闭批 request.opinions；不是本执行端本轮重跑）。本轮本执行端 fetch、Git 祖先/主工作树同步检查与 review progress 已实跑通过。

未解决项（2 项验证缺口，不是未处理 finding）：

1. [验证缺口][Unverifiable] 原生 Windows 未运行；影响：未验证原生控制台/平台行为；交叉编译不等于实机通过。
2. [验证缺口][Unverifiable] 真实 tmux/herdr/Agent 端到端场景未运行；影响：实机交互仍未验证，假 CLI 与隔离测试只覆盖对应隔离场景。

逐项收尾报告见 [wrap-up-report.md](wrap-up-report.md)，本轮独立 Git 检查和删除输出见 [wrap-up-verification.txt](wrap-up-verification.txt)，机器审核闭批原件见 [closed.json](reviews/batches/g3-batch-one/closed.json)。所有原 IMPLEMENTATION、验证附件和作者处置原文保留；受控 `kander move 20260908-dispatch-evidence-binding-task done --result completed` 退出 0；随后 `kander check 20260908-dispatch-evidence-binding-task` 退出 0，输出 `ok: 1 tasks`。适用收尾全部完成，最终卡态 done/completed。

## SUMMARY

最终交付 `287161673bf3f11a865f2e8a40a023a65bdc1f65` 已进入本地和远端 develop `26edb64fcfb654a96afedc30bbaea27e5e918ec7`；g3-batch-one 经 2 个修复轮闭批，PM/QA PASS，CSA/Hacker N/A。7/7 作者自检及适用审核完成。本卡 worktree、本地与远端任务分支已清理，交互 CLI 保留。原生 Windows、真实终端/Agent 两项验证缺口保留。详见 [收尾报告](wrap-up-report.md)。done 门禁及定向 check 均通过（`ok: 1 tasks`），最终卡态 done/completed。

## REVIEWS

- {"run_id":"g3-qa-r1","batch_id":"g3-batch-one","role":"QA","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"0b304a8399cdbc8db2c005e390afd3f6127532af","report":"reviews/g3-qa-r1/report.md"}
- {"run_id":"g3-pm-r1","batch_id":"g3-batch-one","role":"PM","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"0b304a8399cdbc8db2c005e390afd3f6127532af","report":"reviews/g3-pm-r1/report.md"}
- {"run_id":"g3-pm-r2","batch_id":"g3-batch-one","role":"PM","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"5845e6fd0f2b503313030349fa211a7791a50169","previous_run_id":"g3-pm-r1","report":"reviews/g3-pm-r2/report.md"}
- {"run_id":"g3-qa-r2","batch_id":"g3-batch-one","role":"QA","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"5845e6fd0f2b503313030349fa211a7791a50169","previous_run_id":"g3-qa-r1","report":"reviews/g3-qa-r2/report.md"}
- {"run_id":"g3-qa-r3","batch_id":"g3-batch-one","role":"QA","execution_status":"failed","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"26edb64fcfb654a96afedc30bbaea27e5e918ec7","previous_run_id":"g3-qa-r2","report":"reviews/g3-qa-r3/output.raw"}
- {"run_id":"g3-qa-r4","batch_id":"g3-batch-one","role":"QA","execution_status":"interrupted","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"26edb64fcfb654a96afedc30bbaea27e5e918ec7","previous_run_id":"g3-qa-r2","report":"reviews/g3-qa-r4/output.raw"}
- {"run_id":"g3-qa-r5","batch_id":"g3-batch-one","role":"QA","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"26edb64fcfb654a96afedc30bbaea27e5e918ec7","previous_run_id":"g3-qa-r2","report":"reviews/g3-qa-r5/report.md"}
