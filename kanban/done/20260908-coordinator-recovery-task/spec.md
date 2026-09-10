# Orchestration checkpoints, fact reconciliation and wrap-up recovery

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260908-bindings-recovery-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 00:29
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t18:wX:p1T
- STARTED_AT: 2026-09-08 05:01
- FINISHED_AT: 2026-09-08 07:05
- TASK_BRANCH: coordinator-recovery
- RESULT: completed

## GOAL

编排按持久事实恢复：编排必要阶段可持久读取和 CAS 更新且旧编排者不能覆盖新记录；初始快照与后续事件走同一对账，快速完成或重复事件不触发重复派回；编排重启后只有完整审核和真实集成证据才能完成同一轮收尾。

## USER_DECISIONS

2026-09-07 用户在侧会话要求按单一可验收行为重拆剩余卡，并答复"我授权"允许独立审卡、启动及按计划完成组交付/审核/develop 集成与收尾；原四张大卡由拆分卡替代，原契约与审核历史保留。

2026-09-08 用户在主会话决定："对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分." 本卡即按此决定合并而成；被合并的原卡归档为 duplicate 并指向本卡，原契约、独立审卡记录及覆盖矩阵保留不改写。合并不扩大目标，不改变 review 中 P1/P2/E1 及其组分支，不扩大集成/接管授权。

## EXPECTED_OUTCOME

本卡交付：kanban/.kander/groups/<group>/checkpoint.json 的受控读写与 coordinator epoch 单写授权；基于任务 revision、成员及 dispatch 原件的阶段恢复与去重派回；消费 A/R 原件、作者处置、required 角色、closed 批次、最终 delivery 与实际 Git 关系的收尾恢复，以及跨模块验收。

## ACCEPTANCE_CRITERIA

- [ ] 单二进制受控入口保存 kanban/.kander/groups/<group>/checkpoint.json；group_id 稳定锁，遵守 board/group/task 锁序，版本和 coordinator epoch 绑定单一有效写授权。
- [ ] 保存成员版本、交付、batch/run 相对引用、待派回/待收尾、已观察 revision；幂等更新、冲突报错，检查点不是新的任务状态或审核原件。
- [ ] 并发双编排者、旧 epoch、写入边界 kill/restart 验证旧写拒绝、已提交记录保留；检查点只存引用不推导业务通过或自动发通知。
- [ ] 读取任务 revision、成员及 dispatch 原件验证后更新检查点；accepted/completed 解除同轮待确认，错轮次/旧交付/未知成员不放行，重复相同 delivery 只验证。
- [ ] EOF/故障仅对已知可恢复事务有界处理；重复/reparse/真实损坏停止，无输出不等于失败；deadline、心跳和进展停滞处理区别明确。
- [ ] 发布规则改为 snapshot 可证明完成，删除必须见 review->working 边沿的要求；覆盖执行端/订阅端/编排端分别重启、重复丢失重排事件、同 ID 重试、外部增卡、旧编排 epoch。
- [ ] 消费 A/R 原件、作者处置、required 角色、closed 批次、最终 delivery 和实际 Git 关系；无 finding 成员不为抄写通知，机械修复与最终 HEAD 按规则承接。
- [ ] 消费 wrap-up 与 dispatch 及集成证据的绑定；快速 review-working-done 或重启仍只完成同轮；没有既有集成/接管授权时保持现状，代办只消费 20260908-dispatch-evidence-binding-task 的专用授权。
- [ ] 跨模块验收覆盖多卡部分归档恢复、move+PM/QA 归档、缺角色/全失败、错前驱/跨批假接续、旧报告人工映射、无 finding 成员、代办四类状态；不重复实现底层功能，缺陷回归所属卡。
- [ ] 核对原 13 个复现的修复证据映射，重跑 S/D 迁移 kill/旧路径回滚竞态并断言单一入口、新作者记录保留、无重复事务/dispatch/索引；POSIX/Windows 及真实 Agent 冒烟缺口单列。
- [ ] 本卡全部行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包 -race、build/vet/格式检查通过。平台相关测试使用临时目录，原生 Windows 及真实 tmux/herdr/Agent 未执行时列明缺口，不把假 CLI 或交叉编译记为实机通过。PM/QA 按适用门禁，CSA/Hacker 在本仓库 N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

- 检查点读写与授权、派回与交付事实对账、收尾消费规则及必要跨模块验收；不新增发送后端、不自动集成或代收尾、不做 Git 操作、不重写 A/R/N 接口、不部署或迁移真实看板、不扩展安全角色和 TUI。
- 不实现通用工作流引擎、通用服务、签名、保留期清理或无关优化。

## DISCUSSION

```text
PREREQUISITES: 20260907-card-write-transactions-task,20260907-subscription-dispatch-facts-task,20260908-dispatch-evidence-binding-task,20260907-review-disposition-gate-task
```

依赖必要性：S 提供事务与固定锁序（已 done）；20260907-subscription-dispatch-facts-task 提供完整持久业务事实，否则无法证明快速往返对应哪一轮；20260908-dispatch-evidence-binding-task 提供合法代收尾授权与 fix 绑定；R 提供完成证据（已 done）。后两者为同组卡，须 review/done 且交付在组分支。

合并理由：原 O1（20260907-coordinator-checkpoint-cas-task）、O2（20260907-coordinator-fact-reconcile-task）、O3（20260907-coordinator-wrapup-reconcile-task）是同一"编排按持久事实恢复"目标的严格串行链（O1→O2→O3），检查点本身无独立价值，按用户 2026-09-08 决定合并。三卡行为验收原文保留。代价：O1 原可只依赖 S 提前开工，合并后随整卡在组内最后启动；实现时可先做检查点部分。

并行边界：本卡是本组最后一张，依赖同组两卡；组内无并行对象。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本卡交付的一部分，不单独拆成尾部测试任务。合并后各行为可分次提交，但整卡一次交付、一次验收；实现中发现超出本卡目标的独立新行为时另卡，不为避免拆卡扩大契约。

历史来源：20260907-orchestration-recovery-gates-task（原 O 验收 1-14）；拆分历史见 20260907-probe-result-classification-task 的 resplit-plan.md，合并方案见 20260908-durable-dispatch-protocol-task 的 consolidation-plan.md。原卡 CARD_REVIEW 不继承。

SELF_REVIEW: 已核对目标单一（编排按持久事实恢复）、十项行为验收与原 O1/O2/O3 一一对应、四个前置为真实技术依赖且无环、与同组两卡边界互斥。待独立审卡。

CARD_REVIEW: PASS — 2026-09-08，独立子Agent（全新会话，只读整合后 6 张卡、12 张被合并原卡、resplit-plan.md 覆盖矩阵与 consolidation-plan.md）：目标单一且与用户 2026-09-08 决定一致，合并后验收完整覆盖原卡且可判定，前置存在、必要、无环且组内外分类正确，OUT_OF_SCOPE 与同组卡互斥。无阻断；非阻断建议（意图创建边界 kill 场景、保留“英文发布规则”字样、coordinator 第 8 项改为消费措辞、方案中 P3 并行表述）已由建卡者采纳修订。仅契约审查，不代表实现/PM/QA 通过。

## IMPLEMENTATION

### 2026-09-08 首轮交付与 Delivery Self-Check

工作树 `/home/dualf/works/kander/worktrees/coordinator-recovery`，任务分支 `coordinator-recovery`；来源 `group/20260908-bindings-recovery-group`，基线 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`。最终提交 `0b304a8399cdbc8db2c005e390afd3f6127532af` 已正常推送。四个前置交付在组基线可用；接口与实现、11 项验收自检见 [report.md](report.md)。本轮执行端不触发组审核、不更新组分支。

1. `git diff --check 6f5ed32072275c3bb9d96c9e40bd606b949b9c17 HEAD`：无输出、退出 0；修改文件冲突标记扫描 0 个 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
2. `git diff --name-only 6f5ed32072275c3bb9d96c9e40bd606b949b9c17 HEAD` 后统计修改 Go 文件物理行：22 文件，最大 450，均 ≤1000；逐文件输出见 checks.json （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
3. `git diff --stat 6f5ed32072275c3bb9d96c9e40bd606b949b9c17 HEAD` 与最终 diff 人工核对：包边界、中文仓库文档/README、英文 kanban/task-group 规则及三语消息同步；必须见 working 边沿的旧描述已删除，历史闭批与真实 Git 边界明确 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
4. 最终 diff 与生产入口调用链人工核对：cli 注册 RunCoordinator，launch 调用 board claim/reconcile 与 review.VerifyClosedReviewGit，board 复用原事务/派回/审核生产者；无新增不可达、未引用实现或遗留调试代码。`go vet ./...` 退出 0 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
5. 最终测试 diff 人工核对：新用例覆盖消费者、CAS、历史证据和 kill 边界；底层 13 个复现和 S/D/N/A/R 场景复用所属包测试，没有复制实现或无关断言。夹具问题在最终提交前修正，最终复验通过 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
6. `go build ./...`、`go vet ./...`、修改 Go 文件 `gofmt -l` 均退出 0；`go test -race -json -count=1 ./internal/fs ./internal/board ./internal/launch ./internal/review ./internal/cli ./internal/liveness ./internal/notify ./internal/window` 为 8 包、737 PASS/1 SKIP（顶层 409 PASS/1 SKIP），包括所有受影响行为、S/D kill 和回滚保护 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
7. `go test -json -count=1 ./...`：退出 0，19 包、1001 PASS/1 SKIP（顶层 630 PASS/1 SKIP）；所有声明绑定最终提交，统计含子用例 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。

Windows 交叉构建和三语 coordinator CLI 帮助各退出 0；原生 Windows 与真实 tmux/herdr/Agent 未执行，不视为实机通过 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。

交付 Git：rebase 最新组基线无变化；`git ls-remote` 确认远端任务头 `0b304a8399cdbc8db2c005e390afd3f6127532af`、组头 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`；`git merge-base --is-ancestor 6f5ed32072275c3bb9d96c9e40bd606b949b9c17 0b304a8399cdbc8db2c005e390afd3f6127532af` 退出 0，`git status --short` 无输出 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。

原始验证：[汇总](verification/summary.json)、[命令输出](verification/checks.json)、[全量](verification/all.jsonl)、[race](verification/race.jsonl)。PM/QA 待协调者安排；CSA/Hacker N/A。组接收、develop 集成、资源清理均未由本执行端执行；暂无本轮审核 run ID。

### 2026-09-08 g3-batch-one 第1轮派回修复与 Delivery Self-Check

被审目标 `0b304a8399cdbc8db2c005e390afd3f6127532af`，批次 base `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。本卡接收4条 high、合并为2个根因：CR-01：g3-pm-r1/PM-02、g3-qa-r1/QA-01；CR-02：g3-pm-r1/PM-01、g3-qa-r1/QA-02。两角色 NON_BLOCKING 均为空；没有 rejected/unverifiable/waived。上一轮交付记录保留为历史，本轮独立复现并纠正其中的首次启动与闭批恢复缺口；不继承旧测试结果。

- CR-01（PM-02 / QA-01）：独立复现后 confirmed → fixed。双卡中 todo 成员首次启动触发 cycle 冲突；新增 awaiting_start 与首次持久启动事实核验，CAS 绑定一次，保留检查点历史。覆盖 working、漏过 working、重新 claim、重复观察、缺事实、旧 revision/CAS、既有周期替换、旧等待格式及元数据尚未发布的中间快照；launch 层实际 commandStart 生产路径用隔离假终端验证顺序启动 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。
- CR-02（PM-01 / QA-02）：独立复现后 confirmed → fixed。完成 fix、PM 增量复审及 QA、闭批后，旧代码在恢复时报 batch already closed；提取成功 run 的只读消费校验，保持原件、身份、完整发布、lineage、assignment、作者验证；写入/发送仍拒绝闭批。重复恢复不增事务；四类原件缺失均拒绝且不改游标 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。

作者原件（均通过受控 disposition 发布）：

- [g3-pm-r1/PM-02：cr-r1-pm-02-fixed](reviews/g3-pm-r1/dispositions/cr-r1-pm-02-fixed.json)，status=fixed；分别绑定来源原文、报告摘要及当前作者 codex。
- [g3-pm-r1/PM-01：cr-r1-pm-01-fixed](reviews/g3-pm-r1/dispositions/cr-r1-pm-01-fixed.json)，status=fixed；分别绑定来源原文、报告摘要及当前作者 codex。
- [g3-qa-r1/QA-01：cr-r1-qa-01-fixed](reviews/g3-qa-r1/dispositions/cr-r1-qa-01-fixed.json)，status=fixed；分别绑定来源原文、报告摘要及当前作者 codex。
- [g3-qa-r1/QA-02：cr-r1-qa-02-fixed](reviews/g3-qa-r1/dispositions/cr-r1-qa-02-fixed.json)，status=fixed；分别绑定来源原文、报告摘要及当前作者 codex。

按关注点提交并 rebase 到最新组头 `287161673bf3f11a865f2e8a40a023a65bdc1f65`，无冲突：

- CR-01：`c7aa1f598703cfa01898512bfe693a05e855d44e` → `4a55248377c8fa35edddd862acee3473d877b2e5`。
- CR-02：`7d4cb92b13edb43c25db71f303c5311c59b76972` → `5845e6fd0f2b503313030349fa211a7791a50169`（最终交付）。

最终任务头 `5845e6fd0f2b503313030349fa211a7791a50169` 已正常推送 origin/coordinator-recovery；git ls-remote 与本地 HEAD 相同。最新组头是另一卡期限文档修复 `287161673bf3f11a865f2e8a40a023a65bdc1f65`，是本交付祖先；未更新组分支/develop。

1. `git diff --check 287161673bf3f11a865f2e8a40a023a65bdc1f65 HEAD` 与 `git diff --check 6f5ed32072275c3bb9d96c9e40bd606b949b9c17 HEAD` 无输出、退出 0；完整任务差异冲突标记扫描 0 个 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。
2. `git diff --name-only 6f5ed32072275c3bb9d96c9e40bd606b949b9c17 HEAD` 后逐文件物理行统计：27 个 Go 文件，最大 457，全部 ≤1000；本轮 9 个 Go 文件。逐文件输出见 checks.json （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。
3. `git diff --stat 287161673bf3f11a865f2e8a40a023a65bdc1f65 HEAD` 及最终源码/文档 diff 人工核对：本轮 12 文件、402 增/12 删；中文恢复文档/复现映射、英文 task-group 发布规则说明首次启动；历史 completed fix 的只读边界同步，无过时行为注释。三语错误前缀复用既有消息键，不新增未翻译键 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。
4. 最终 diff 与 `rg -n 'coordinatorMemberCycle|reviewRunForConsumption|reviewRunForMutation' internal/board` 调用检索人工核对：首次绑定函数用于成员对账；只读 helper 同时用于修改门禁和历史 fix；无新增不可达/未引用实现；包依赖不变。`go vet ./...` 退出 0 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。
5. 最终测试 diff 人工核对：新增首次启动、持久事实/CAS及真实 start 生产链、闭批窗口及4类损坏；旧 target 推进用例抽取准备夹具复用。断言围绕业务结果、CAS、原件及不可重发，不复制底层实现或保留调试代码 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。
6. `go build ./...` 退出 0；8 包 `go test -race -json -count=1` 退出 0，752 PASS/1 SKIP；本轮 5 个新顶层回归在全量和 race 中全部 PASS，具体命令与路径见 verification/r1/summary.json （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。
7. `go test -json -count=1 ./...` 退出 0：19 包、1016 PASS/1 SKIP（顶层635 PASS/1 SKIP）。13 原始复现映射及 S/D kill/restart、旧路径回滚、作者记录与审核原件门禁均随最终全量/race 重跑。唯一跳过 TestWindowsConsoleLauncher （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。

原始结果：[汇总](verification/r1/summary.json)、[命令与逐文件统计](verification/r1/checks.json)、[全量](verification/r1/all.jsonl)、[race](verification/r1/race.jsonl)、[首次启动复现](verification/r1/repro-cycle.txt)、[闭批复现](verification/r1/repro-closed.txt)。完整本轮报告见 [report-r1.md](report-r1.md)。

早期回归夹具曾出现缺 TASK_BRANCH、含大写 run ID、working→working 不合法；均在最终提交前修正，最终重新执行通过。复现日志中的失败不冒充最终 PASS。

PM g3-pm-r1、QA g3-qa-r1 首轮未通过；本卡两根因均 fixed，四份作者原件等待协调者接收后的增量复审，未自行启动 Reviewer。CSA/Hacker N/A（仓库 AGENTS.md）。原生 Windows 与真实 tmux/herdr/Agent 仍未执行；交叉构建/假终端不是实机通过。保留 CLI、任务 worktree 和分支；本轮只交付 review，未声称组接收、develop 集成或 done。

### 2026-09-08 g3-batch-one 第2轮派回：QA-04 与 Delivery Self-Check

本轮被审目标 `5845e6fd0f2b503313030349fa211a7791a50169`，QA run g3-qa-r2、finding QA-04、medium。独立追踪首次绑定、launch元数据/失败和board rollback CAS，在真实commandStart元数据提交后先对账，再注入tmux失败并执行rollbackLaunch；回滚快照子用例输出 `member execution cycle changed without first-start facts`，确认是前轮修复引入。结论 confirmed → fixed；无rejected/unverifiable。原文、hash、作者codex、修复SHA与实跑验证绑定到[作者原件 cr-r2-qa-04-fixed](reviews/g3-qa-r2/dispositions/cr-r2-qa-04-fixed.json)。[复现输出](verification/r2/repro.txt)保留，不冒充最终通过。

修复：managed metadata与pending启动尝试同事务；合法恢复原文/状态与rolled-back原件同事务；launcher成功后写succeeded原件，不改任务revision/正文。检查点保留待启动/尝试ID，凭成功确认绑定周期，凭回滚链承接重试；同分钟尝试按ID/revision隔离。每次重验完整重试链，缺指针不降级成旧卡。成功不覆盖快速执行者的新记录/review；成功与回滚只允许一个结果；已确认周期不可任意替换。旧卡/显式manual owner保持既有元数据入口；未知结果保持等待，不伪造交付或执行授权。实现和文档同提交。

修复提交及rebase后最终交付均为 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`。`git rebase origin/group/20260908-bindings-recovery-group` 返回up to date，基线 `5845e6fd0f2b503313030349fa211a7791a50169`，无改写、无冲突。最终任务头已正常推送；未更新组分支/develop。

1. `git diff --check 5845e6fd0f2b503313030349fa211a7791a50169 HEAD`、`git diff --check 6f5ed32072275c3bb9d96c9e40bd606b949b9c17 HEAD`均退出0、无输出；完整交付冲突标记0 （提交 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`）。
2. 完整交付 `git diff --name-only 6f5ed32072275c3bb9d96c9e40bd606b949b9c17 HEAD`逐文件统计：33个Go文件，最大457行，均≤1000；本轮10个Go文件，逐文件值见checks.json （提交 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`）。
3. `git diff --stat 5845e6fd0f2b503313030349fa211a7791a50169 HEAD`：14文件、617增/11删。人工核对最终源码/文档diff：中文恢复说明/复现映射/AGENTS与英文task-group规则同步描述尝试/成功/回滚。三语错误前缀复用既有消息键 （提交 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`）。
4. `rg -n 'ConfirmTaskStart|stageTaskStart|coordinatorStartCycle|coordinatorStartLineage' internal/board internal/launch`及最终调用链人工核对：launch→board成功入口、managed事务→启动/回滚原件、coordinator→原件消费均有实际调用，无新增反向依赖、未引用或不可达代码。`go vet ./...`退出0 （提交 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`）。
5. 人工核对最终测试diff：6个新增顶层测试覆盖真实启动交错、结果隔离/快执行者、原件损坏、确认后拒绝替换、凭证据撤销临时游标/手动claim、并发结果互斥；旧中间启动测试增加成功原件核验，未弱化为仅看metadata。无复制实现或无关断言 （提交 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`）。
6. `go build ./...`、`go vet ./...`、修改Go文件gofmt均退出0；`go test -race -json -count=1 ./internal/fs ./internal/board ./internal/launch ./internal/review ./internal/cli ./internal/liveness ./internal/notify ./internal/window`退出0：8包766 PASS/1 SKIP（顶层420 PASS/1 SKIP），6个新增回归全部PASS （提交 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`）。
7. `go test -json -count=1 ./...`退出0：19包1030 PASS/1 SKIP（顶层641 PASS/1 SKIP）；原13复现映射全部PASS，S/D kill与旧路径回滚门禁随全量及race重新运行。唯一跳过TestWindowsConsoleLauncher （提交 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`）。

`GOOS=windows GOARCH=amd64 go build -o /tmp/coordinator-r2-validation/kander.exe ./cmd/kander`退出0，仅交叉构建 （提交 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`）。原生Windows、真实tmux/herdr/Agent仍未执行，假终端不算实机通过。开发期间发现并修正了同revision成功确认被旧条件拒绝、fs包装缺失路径误判两项问题，最终提交全部重新实跑，未沿用中间结果。

[验证汇总](verification/r2/summary.json)、[命令/输出/行数](verification/r2/checks.json)、[全量](verification/r2/all.jsonl)、[race](verification/r2/race.jsonl)、[本轮报告](report-r2.md)。此前三根因由g3-pm-r2/g3-qa-r2判定closed；PM g3-pm-r2在 `5845e6fd0f2b503313030349fa211a7791a50169` PASS，按主控通知承接，不自行重跑。QA-04已fixed，待主控ff接收后QA增量复审；两个角色NON_BLOCKING为空。CSA/Hacker N/A。当前第2修复轮，未越过轮次上限。

保留交互CLI、任务worktree、本地及远端任务分支。本轮仅交付review，组接收/develop集成/最终收尾未由执行端执行，不声明done。

### 2026-09-08 审核闭批、develop 集成确认与资源收尾

按收尾通知由本执行端自行回 working。只读核验 sealed 计划 g3-bindings-recovery 和 g3-batch-one 闭批原件，`kander review progress` 为 closed。PM g3-pm-r2 PASS（passed_at `5845e6fd0f2b503313030349fa211a7791a50169`，规则承接）；QA g3-qa-r5 PASS（passed_at 最终交付）；CSA/Hacker N/A。本卡5个来源 finding、3根因经2修复轮全部关闭，无非门禁条目。QA r3/r4 的环境中断保留为 failed/interrupted，闭批显式由 r5 解决，不计修复轮。

`git fetch origin` 后实际确认最终交付 `26edb64fcfb654a96afedc30bbaea27e5e918ec7` 已在本地与远端 develop，两次祖先检查退出0，主工作树干净；组接收及 develop 集成没有改写已交付SHA。前置满足后从主工作树依次删除本卡worktree、本地coordinator-recovery、远端coordinator-recovery，三项退出0，事后目录/引用/远端检查确认不存在。组工作树/分支及交互CLI保留。完整结论与原始Git证据相对链接见[完成记录](report-wrapup.md)。本次未改代码、未重跑测试或Reviewer；此前IMPLEMENTATION、作者原件与验证日志全部保留。

## SUMMARY

已交付编排检查点CAS/epoch、持久事实与闭批恢复，修复首次启动、闭批历史对账及启动失败回滚三个根因。11/11项作者验收自检完成，逐项映射和两轮纠正见[完成记录](report-wrapup.md)及此前报告。最终提交 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`；最终交付前实跑全量19包1030 PASS/1 SKIP、8包race 766 PASS/1 SKIP，build/vet/gofmt/diff与Windows交叉构建通过；详见[最终验证](verification/r2/summary.json)，计数含子用例。

审核计划g3-bindings-recovery已sealed，单批g3-batch-one经2修复轮closed；PM(codex) g3-pm-r2 PASS承接，QA(codex) g3-qa-r5最终PASS，CSA/Hacker按仓库特例N/A。全部来源finding已关闭，NON_BLOCKING为空；QA r3/r4环境中断由r5替代，未冒充语义通过。见[闭批原件](reviews/batches/g3-batch-one/closed.json)。

主控完成组分支ff接收、develop推送与主工作树同步；本执行端fetch后复核本地/远端develop与最终交付一致、祖先检查通过、工作树干净。本卡工作树、本地及远端任务分支清理全部完成；组资源由主控后续处理，交互CLI与审核原件保留。未解决项2：[验证缺口][Unverifiable] 原生Windows未运行；[验证缺口][Unverifiable] 真实tmux/herdr/Agent未运行，不能据交叉构建或假CLI声称实机通过。记录发布后执行done/completed和定向check，状态以受控回执为准。

## REVIEWS

- {"run_id":"g3-qa-r1","batch_id":"g3-batch-one","role":"QA","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"0b304a8399cdbc8db2c005e390afd3f6127532af","report":"reviews/g3-qa-r1/report.md"}
- {"run_id":"g3-pm-r1","batch_id":"g3-batch-one","role":"PM","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"0b304a8399cdbc8db2c005e390afd3f6127532af","report":"reviews/g3-pm-r1/report.md"}
- {"run_id":"g3-pm-r2","batch_id":"g3-batch-one","role":"PM","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"5845e6fd0f2b503313030349fa211a7791a50169","previous_run_id":"g3-pm-r1","report":"reviews/g3-pm-r2/report.md"}
- {"run_id":"g3-qa-r2","batch_id":"g3-batch-one","role":"QA","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"5845e6fd0f2b503313030349fa211a7791a50169","previous_run_id":"g3-qa-r1","report":"reviews/g3-qa-r2/report.md"}
- {"run_id":"g3-qa-r3","batch_id":"g3-batch-one","role":"QA","execution_status":"failed","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"26edb64fcfb654a96afedc30bbaea27e5e918ec7","previous_run_id":"g3-qa-r2","report":"reviews/g3-qa-r3/output.raw"}
- {"run_id":"g3-qa-r4","batch_id":"g3-batch-one","role":"QA","execution_status":"interrupted","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"26edb64fcfb654a96afedc30bbaea27e5e918ec7","previous_run_id":"g3-qa-r2","report":"reviews/g3-qa-r4/output.raw"}
- {"run_id":"g3-qa-r5","batch_id":"g3-batch-one","role":"QA","execution_status":"ok","base":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","commit":"26edb64fcfb654a96afedc30bbaea27e5e918ec7","previous_run_id":"g3-qa-r2","report":"reviews/g3-qa-r5/report.md"}
