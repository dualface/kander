# 订阅事实代码交付报告

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


## 当前结论

代码交付已完成并推送：003e5fecf4048d8da8151d0431ea1cd040912a36，分支 subscription-facts。六项行为验收自检通过，第七项开发验证完成、PM/QA 组审核待协调者安排；CSA/Hacker N/A。最终提交全量 19 包、550 个顶层测试通过（含子用例 821 pass / 1 skip）；四包 race 190 个顶层测试通过（含子用例 385 pass / 0 skip）；定向、build/vet/格式和 Windows 交叉编译通过。以上验证均绑定提交 003e5fecf4048d8da8151d0431ea1cd040912a36，原始记录见 [验证记录](verification.txt)，七项自检与逐项验收见 [交付报告](report.md)。

原生 Windows 与真实 tmux/herdr/Agent 尚未验证。分支和工作树保留；进入 review 仅表示等待协调者接收交付和组织审核，不表示已集成或已达到 done 门禁。


# 审核与收尾结果（最新）

### 2026-09-08 已审核、已集成收尾（最新）

本卡最终交付 003e5fecf4048d8da8151d0431ea1cd040912a36 无重写，已包含在最终组 HEAD ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3，本轮 fetch 后独立核对二者均为 develop 与 origin/develop 祖先，四项命令全部 exit 0；主工作树干净。任务 worktree、本地与远端 subscription-facts 均已清理成功，组资源保留给协调者。详见 [收尾记录](wrapup.md)。

计划 g2-subscription-dispatch sealed，g2-batch-one closed，1 个修复轮；PM/codex g2-pm-r2 与 QA/codex g2-qa-r3 PASS，passed_at 均为 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3。g2-qa-r2 失败原件保留，由 g2-qa-r3 显式替代。CSA/Hacker N/A。本卡没有分配到 finding，无非门禁残留；组内六个来源 finding 由原所属作者全部 fixed。本执行端只核对原件和机器进度，没有重跑审核。

7/7 项验收完成，保留原生 Windows 与真实 tmux/herdr/Agent 两类环境缺口。作者在 003e5fecf4048d8da8151d0431ea1cd040912a36 的实现、验证原文不变；协调者在 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 的 build/vet/19 包全量通过属于协调者验证。本轮没有重跑代码测试，未部署、未迁移、未退出 CLI 或关闭终端。


原交付报告中的“审核待安排”属于当时记录，现以本节及 [收尾记录](wrapup.md) 为准。最后 done/check 待受控命令执行后追加。


## 完成命令结果

最后操作已完成：`kander move 20260908-subscription-facts-task done --result completed` 成功，卡片路径为 kanban/done/20260908-subscription-facts-task；随后 `kander check 20260908-subscription-facts-task` exit 0，输出 `ok: 1 tasks`。结果对应本卡交付 003e5fecf4048d8da8151d0431ea1cd040912a36、最终组提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3。交互 CLI 与终端保留，未 dismiss。
