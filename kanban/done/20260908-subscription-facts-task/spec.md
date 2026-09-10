# Subscription reports changes by committed revision and membership facts

- TYPE: Bug
- SIZE: large
- TASK_GROUP: 20260908-subscription-dispatch-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 00:29
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t13:wX:p1N
- STARTED_AT: 2026-09-08 02:33
- FINISHED_AT: 2026-09-08 04:07
- TASK_BRANCH: subscription-facts
- RESULT: completed

## GOAL

订阅按已提交 revision 报告任务变化并保留组引用重新展开成员：重启和快速状态往返不会抹掉任务已更新的事实，新增成员不漏、无法确定的成员不被当作已满足依赖。

## USER_DECISIONS

2026-09-07 用户在侧会话要求按单一可验收行为重拆剩余卡，并答复"我授权"允许独立审卡、启动及按计划完成组交付/审核/develop 集成与收尾；原四张大卡由拆分卡替代，原契约与审核历史保留。

2026-09-08 用户在主会话决定："对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分." 本卡即按此决定合并而成；被合并的原卡归档为 duplicate 并指向本卡，原契约、独立审卡记录及覆盖矩阵保留不改写。合并不扩大目标，不改变 review 中 P1/P2/E1 及其组分支，不扩大集成/接管授权。

## EXPECTED_OUTCOME

本卡交付：订阅快照与事件携带已提交 revision，同状态更新、review-working-review 与重启后的变化可由 revision 识别；--watch 组引用保留并按成员版本重新展开，输出 membership-change/membership-unknown，不可读或无法归属的成员停止依赖放行。

## ACCEPTANCE_CRITERIA

- [ ] 版本化 JSONL 保留 event/group_id/tasks/changed；增加 subscription_id、进程内 seq、observed_at、task_revisions 及 task-update。seq 不是跨重启游标。
- [ ] 快照与事件使用 S 协调读取的已提交事实；已知未完成事务明确 recoverable/maintenance 及有界等待/退出，重复/reparse/持久损坏失败关闭，不自动修复。
- [ ] 同状态更新、review-working-review 和重启测试可由 revision 识别发生过变化；尚无 dispatch 时不声称能证明哪一轮业务完成。反转 RoundTrip 复现中的任务更新不可见部分。
- [ ] 复用 board 成员解析与完整性结果，保留原始 --watch 组引用；成员版本变化输出 membership-change，事件携带完整监听集合及成员版本。
- [ ] 新增成员加入；移除/归属变化不能自动放行；重复目标拒绝；ReadDocument/Scan.Problems 不吞掉，无法定位归属时 membership-unknown 并停止依赖放行，不盲目探测无关已知问题。
- [ ] 反转 WatchedGroupMembershipIsFrozen 与 WatchedGroupSilentlyOmitsUnreadableMember；覆盖新增、移除、改组、不可读和重复，重启快照与 check 依赖集合一致。
- [ ] 本卡全部行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包 -race、build/vet/格式检查通过。平台相关测试使用临时目录，原生 Windows 及真实 tmux/herdr/Agent 未执行时列明缺口，不把假 CLI 或交叉编译记为实机通过。PM/QA 按适用门禁，CSA/Hacker 在本仓库 N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

- 订阅事实 schema、协调读取、监听成员展开与事件；不实现 dispatch 摘要、业务确认、回放日志，不让 subscribe 执行依赖放行或自动修卡。
- 不实现存活探测调度、输出 writer 生命周期（由 20260908-subscription-bounded-runtime-task 交付），不接入派回事实（由 20260907-subscription-dispatch-facts-task 交付）。
- 不引入通用服务、签名、保留期清理或无关优化。

## DISCUSSION

```text
PREREQUISITES: 20260907-card-write-transactions-task,20260907-subscription-heartbeat-clock-task
```

依赖必要性：S 提供 revision 和协调读取，是必要技术前置；E1 先稳定同一订阅入口的 timer 改动。无需 N 或 R 业务接口。两个前置均为组外卡，须 done 且交付在 develop 可用。

合并理由：原 E2（20260907-subscription-revision-snapshot-task）与 E3（20260907-subscription-watch-membership-task）目标同为"订阅报告的事实正确且完整"，E3 只能在 E2 的版本事件载体之后串行实现，无并行收益，按用户 2026-09-08 决定合并。E2 前三项与 E3 前三项验收原文保留。

并行边界：本卡与 20260908-durable-dispatch-protocol-task 修改不同模块（subscribe 与 notify/board），组内可并行；20260908-subscription-bounded-runtime-task 依赖本卡，串行。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本卡交付的一部分，不单独拆成尾部测试任务。合并后各行为可分次提交，但整卡一次交付、一次验收；实现中发现超出本卡目标的独立新行为时另卡，不为避免拆卡扩大契约。

历史来源：20260907-subscription-reconcile-task（原 E 验收 1/2/6/7/8/11/12）；拆分历史与覆盖矩阵见 20260907-probe-result-classification-task 的 resplit-plan.md，合并方案见 20260908-durable-dispatch-protocol-task 的 consolidation-plan.md。原卡 CARD_REVIEW 不继承。

SELF_REVIEW: 已核对合并后目标单一（订阅事实正确完整）、六项行为验收均可执行且与原 E2/E3 一一对应无遗漏、前置为真实技术依赖且无环、OUT_OF_SCOPE 与同组两卡边界互斥。待独立审卡。

CARD_REVIEW: PASS — 2026-09-08，独立子Agent（全新会话，只读整合后 6 张卡、12 张被合并原卡、resplit-plan.md 覆盖矩阵与 consolidation-plan.md）：目标单一且与用户 2026-09-08 决定一致，合并后验收完整覆盖原卡且可判定，前置存在、必要、无环且组内外分类正确，OUT_OF_SCOPE 与同组卡互斥。无阻断；非阻断建议（意图创建边界 kill 场景、保留“英文发布规则”字样、coordinator 第 8 项改为消费措辞、方案中 P3 并行表述）已由建卡者采纳修订。仅契约审查，不代表实现/PM/QA 通过。

## IMPLEMENTATION

最后操作已完成：`kander move 20260908-subscription-facts-task done --result completed` 成功，卡片路径为 kanban/done/20260908-subscription-facts-task；随后 `kander check 20260908-subscription-facts-task` exit 0，输出 `ok: 1 tasks`。结果对应本卡交付 003e5fecf4048d8da8151d0431ea1cd040912a36、最终组提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3。交互 CLI 与终端保留，未 dismiss。

### 2026-09-08 已审核、已集成收尾（最新）

本卡最终交付 003e5fecf4048d8da8151d0431ea1cd040912a36 无重写，已包含在最终组 HEAD ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3，本轮 fetch 后独立核对二者均为 develop 与 origin/develop 祖先，四项命令全部 exit 0；主工作树干净。任务 worktree、本地与远端 subscription-facts 均已清理成功，组资源保留给协调者。详见 [收尾记录](wrapup.md)。

计划 g2-subscription-dispatch sealed，g2-batch-one closed，1 个修复轮；PM/codex g2-pm-r2 与 QA/codex g2-qa-r3 PASS，passed_at 均为 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3。g2-qa-r2 失败原件保留，由 g2-qa-r3 显式替代。CSA/Hacker N/A。本卡没有分配到 finding，无非门禁残留；组内六个来源 finding 由原所属作者全部 fixed。本执行端只核对原件和机器进度，没有重跑审核。

7/7 项验收完成，保留原生 Windows 与真实 tmux/herdr/Agent 两类环境缺口。作者在 003e5fecf4048d8da8151d0431ea1cd040912a36 的实现、验证原文不变；协调者在 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 的 build/vet/19 包全量通过属于协调者验证。本轮没有重跑代码测试，未部署、未迁移、未退出 CLI 或关闭终端。


### 2026-09-08 最终代码交付

- 最终交付：003e5fecf4048d8da8151d0431ea1cd040912a36；任务分支 subscription-facts，本地与 origin/subscription-facts 一致，已正常推送。交付来源及最新 rebase 基线：origin/group/20260908-subscription-dispatch-group，021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5。fetch 后 rebase 显示 up to date，无冲突；`git merge-base --is-ancestor origin/group/20260908-subscription-dispatch-group HEAD` exit 0，`git status --short` 无输出。核验提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
- 工作树：/home/dualf/works/kander/worktrees/subscription-facts。未修改组分支、develop 或其他卡工作树；保留本卡资源等待协调者派回。
- 实现：JSONL 新增 schema_version/subscription_id/seq/observed_at/task_revisions/task-update；同一次协调读取绑定状态、正文和 revision。组引用每次重新展开，输出完整集合及确定性成员版本；原监听组成员减少（包括改到另一监听组）保留 reconciliation_required。读取不完整发送终止告警并退出。
- board 增加 ScanContext/ScanTargetsContext、不可变 Revision、GroupMembership 及状态分类；旧 Scan/ScanTargets 兼容。成员解析与 check 依赖展开共享完整性结果。fs 增加有界共享锁获取，既有写锁 API 不变；订阅每次读取共用 2 秒锁等待预算，prepared 明确 recoverable，不自动恢复。
- 交付文档：docs/subscription-facts.md、AGENTS.md 索引、英文发布协议与组编排消费规则；新增消息具备中英日三语。

### 交付自检（七项）

1. `git diff --check 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5..HEAD` exit 0、无输出；冲突标记搜索 `rg -n '^(<<<<<<< |=======|>>>>>>> )' <15 个改动 Go 文件>` exit 1（无匹配）。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
2. 按 `git diff --name-only 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5..HEAD` 枚举改动并逐文件统计物理行：15 个非生成 Go 文件均不超过 1000 行，最大为 internal/board/deps.go 的 375 行；没有超限文件。`gofmt -l <15 个改动 Go 文件>` 无输出。详细计数见 [自检原始结果](delivery-self-check.json)。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
3. `git diff 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5..HEAD -- docs/subscription-facts.md rules/KANDER-KANBAN-RULES.md rules/KANDER-TASK-GROUP-RULES.md AGENTS.md` 已逐段核对：版本字段、动态成员、空组、改组、不完整事实、2 秒锁等待和同步探测/writer 边界均与实现相符；英文发布规则和中文仓库文档同步，入口 Language 节保留。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
4. `rg` 确认旧 groupMembers/groupMembersFrom/watchedTaskIDs/groupStateSnapshot/makeEvent/validateSubscribe 函数均已移除；逐项检查新 helper 调用链和导出 API 的使用方，未新增不可达、无调用或无引用代码。`go vet ./...` exit 0、无输出。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
5. 人工核对本轮测试 diff：状态往返、同状态更新、实际进程重启、成员新增/移除/跨组、重复/不可读、锁期限、prepared/损坏/reparse 分别覆盖独立行为；删除重复的进程内 revision 重启断言，保留组重启与依赖展开一致性检查。未增加整屏快照或镜像实现的断言。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
6. `go build ./...` exit 0；定向命令 `go test -json -count=1 ./internal/board ./internal/liveness -run 'TestSnapshotContext|TestSnapshotPending|TestSnapshotRevision|TestMembershipDependency|TestAuditRoundTrip|TestAuditWatchedGroup|TestSubscribeMembership|TestSubscribeRejects|TestSubscribeExplicit|TestSubscribeUnavailable|TestSubscribeRegroup|TestSubscribeRestartProcess'` exit 0，2 个包，15 个顶层测试通过，含子用例 40 项通过、0 项 skip、0 失败。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
7. `go test -json -count=1 ./...` exit 0，19 个包，550 个顶层测试通过，含子用例 821 项通过、1 项 skip、0 失败；`go test -json -race -count=1 ./internal/board ./internal/fs ./internal/liveness ./internal/i18n` exit 0，4 个包，190 个顶层测试通过，含子用例 385 项通过、0 项 skip、0 失败。全部在最终提交上执行；原始命令和输出见 [验证记录](verification.txt)。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。


### 逐项验收

- 验收 1：版本 JSONL 保留旧字段，新增 revision 与 task-update；schema/ID/seq/时间字段及不伪造 dispatch 完成由测试断言。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
- 验收 2：协调快照和 revision 不随后续 mutation cursor 改变；board/task/journal 争用共用期限，prepared 不修复，损坏及 reparse 失败关闭。TestSnapshotContextContentionReleasesLocks、TestSnapshotPendingFactsFailWithoutRecovery、TestSnapshotRevisionSurvivesMutationCursor、TestSubscribeUnavailableCommittedFacts 通过。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
- 验收 3：TestAuditRoundTripIsInvisible 已反转，覆盖同状态更新及 review-working-review 的 +1/+3 revision；TestSubscribeRestartProcess 通过真实子进程重启确认已提交 revision 延续且 seq 重置、subscription_id 不复用。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
- 验收 4：TestAuditWatchedGroupMembershipIsFrozen 已反转；新增成员出现在 membership-change 的完整监听集合、revision 和成员版本中，保留原组引用。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
- 验收 5：移除、改组、两个监听组间归属变化、重复/别名/交叠、不可读/缺 spec/无效组归属均覆盖；未知成员阻止完整性结论，显式任务监听不读取无关已知问题。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
- 验收 6：TestAuditWatchedGroupSilentlyOmitsUnreadableMember 已反转，启动及 refresh 都失败关闭；重启组快照与 TaskDependenciesOf 展开集合一致，CheckBoard 不接受未知归属的组依赖。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。
- 验收 7：全量、定向、四包 race、build/vet/格式、三语目录校验及 Windows 二进制与三包测试交叉编译通过，详见验证记录。提交：003e5fecf4048d8da8151d0431ea1cd040912a36。PM/QA 尚未执行，由协调者接收组交付后安排；CSA/Hacker 按仓库规则 N/A。此项的组审核部分仍待完成，不声称整卡最终门禁已通过。

### 验证边界与未解决项

- [平台验证缺口] 原生 Windows 未执行；交叉编译只验证构建，不证明原生锁、reparse、文件 I/O 行为。对应交付：003e5fecf4048d8da8151d0431ea1cd040912a36。
- [联调缺口] 真实 tmux/herdr/Agent 未执行；临时看板和假 CLI 仅算自动测试。对应交付：003e5fecf4048d8da8151d0431ea1cd040912a36。
- [契约边界] 文件打开/内核 I/O 仍由 OS 控制；慢探测、阻塞 writer 的生命周期及派回业务回执分别由后续卡交付，本卡没有回放日志或业务完成证明。
- [待协调者执行] PM/QA、组分支接收、develop 集成和收尾。本执行端不触发 Reviewer；没有本轮 review run ID，不粘贴或伪造审核原件。

### 初始实施记录

- 工作树：/home/dualf/works/kander/worktrees/subscription-facts；任务分支：subscription-facts；来源：origin/group/20260908-subscription-dispatch-group，基线 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5。
- 前置 S、E1 均为 done；交付 a7fe54beb6ade00678e14655c0d385115eb950c8 与 e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88 已确认是 develop 祖先。
- 计划：复用 board 协调快照和成员解析，增加版本事件、动态成员与完整性结果；增加有界共享读锁，保持现有阻塞写锁 API。涉及 board、fs、liveness、i18n 与文档；JSONL 保留已有字段，新增版本字段，既有 Scan/ScanTargets 调用兼容。
- 验证计划：revision 往返/重启、组新增/移除/改组/不可读/重复、事务与维护边界，随后全量、相关包 race、build/vet/格式检查。组审核与集成由协调者安排。


## SUMMARY

7/7 项验收完成，PM/QA 已通过并闭批，CSA/Hacker N/A。本卡 003e5fecf4048d8da8151d0431ea1cd040912a36 无重写，已随最终组 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 进入本地与远端 develop，执行端独立核验通过；本卡工作树、本地/远端分支清理完成。无本卡未处置 finding；原生 Windows、真实终端联调两类验证缺口保留。完整证据见 [收尾记录](wrapup.md)，原 [验证记录](verification.txt) 保留。done --result completed 与定向 check 均已成功，check 输出 ok: 1 tasks。

### 代码交付阶段历史（保留原文）


代码交付已完成并推送：003e5fecf4048d8da8151d0431ea1cd040912a36，分支 subscription-facts。六项行为验收自检通过，第七项开发验证完成、PM/QA 组审核待协调者安排；CSA/Hacker N/A。最终提交全量 19 包、550 个顶层测试通过（含子用例 821 pass / 1 skip）；四包 race 190 个顶层测试通过（含子用例 385 pass / 0 skip）；定向、build/vet/格式和 Windows 交叉编译通过。以上验证均绑定提交 003e5fecf4048d8da8151d0431ea1cd040912a36，原始记录见 [验证记录](verification.txt)，七项自检与逐项验收见 [交付报告](report.md)。

原生 Windows 与真实 tmux/herdr/Agent 尚未验证。分支和工作树保留；进入 review 仅表示等待协调者接收交付和组织审核，不表示已集成或已达到 done 门禁。


## REVIEWS

- {"run_id":"g2-qa-r1","batch_id":"g2-batch-one","role":"QA","execution_status":"ok","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe","report":"reviews/g2-qa-r1/report.md"}
- {"run_id":"g2-pm-r1","batch_id":"g2-batch-one","role":"PM","execution_status":"ok","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe","report":"reviews/g2-pm-r1/report.md"}
- {"run_id":"g2-pm-r2","batch_id":"g2-batch-one","role":"PM","execution_status":"ok","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","previous_run_id":"g2-pm-r1","report":"reviews/g2-pm-r2/report.md"}
- {"run_id":"g2-qa-r2","batch_id":"g2-batch-one","role":"QA","execution_status":"failed","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","previous_run_id":"g2-qa-r1","report":"reviews/g2-qa-r2/output.raw"}
- {"run_id":"g2-qa-r3","batch_id":"g2-batch-one","role":"QA","execution_status":"ok","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","previous_run_id":"g2-qa-r1","report":"reviews/g2-qa-r3/report.md"}
