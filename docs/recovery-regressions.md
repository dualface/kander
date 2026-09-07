# 原始复现验收映射

2026-09-07 的 13 个调查测试在旧基线上断言坏行为成立。它们保留于本机任务证据，不是当前实现通过的依据。下表把每个原始名称映射到当前反向断言；同名 Audit 测试已改为要求修复后的结果。所有验证使用临时看板和测试仓库。

| 原始复现 | 当前测试位置与修复断言 |
| --- | --- |
| TestAuditRoundTripIsInvisible | liveness/subscribe_facts_test.go 同名用例要求 revision 更新可见；board/协调快照测试验证快速往返的同轮 completed，不依赖 working 边沿。 |
| TestAuditChangesSuppressLiveness | liveness/subscribe_clock_test.go 同名用例要求持续状态变化仍产生独立心跳。 |
| TestAuditReverseLookupErrorBecomesStopped | liveness/lookup_test.go 的 TestAuditReverseLookupErrorBecomesUnknown：反查失败保留 unknown。 |
| TestAuditReviewHasNoLiveness | liveness/subscribe_dispatch_test.go 同名用例要求待确认 review dispatch 纳入观察。 |
| TestAuditWatchedGroupMembershipIsFrozen | liveness/subscribe_facts_test.go 同名用例要求外部增卡重新展开。 |
| TestAuditBlockedWriterIgnoresStop | liveness/subscribe_pipe_test.go 同名用例要求停止能中断阻塞写出并回收 worker。 |
| TestAuditRefreshDurationOverflow | liveness/subscribe_clock_test.go 同名用例要求不可表示 duration 在访问看板前被拒绝。 |
| TestAuditProbeBlocksScanAndCancellation | liveness/subscribe_runtime_test.go 同名用例要求慢探测不阻塞扫描，取消可回收。 |
| TestAuditWatchedGroupSilentlyOmitsUnreadableMember | liveness/subscribe_facts_test.go 同名用例要求不完整成员事实显式失败。 |
| TestAuditDescendantOutputOutlivesProbeDeadline | probe/run_test.go 同名用例要求处理继承管道后代和有界取消。 |
| TestAuditPromptEchoCountsAsAcknowledgement | notify/dispatch_test.go 的 TestPromptEchoDoesNotCountAsAcknowledgement：仅回显不能成为 accepted。 |
| TestAuditRollbackRecreatesMovedCard | window/window_test.go 的 TestRollbackNeverResurrectsMovedSmallCard：旧路径不存在、单一入口；TestStaleRollbackPreservesNewBodyAndRejectsSecondRollback 保留新作者正文。 |
| TestAuditGuardCheckDoesNotCoverLaterMove | board/coordinator_regression_test.go 同名用例：guard 仍仅辅助；其后移动并写入新作者记录，旧 revision 的受控 update 必须失败，不能复活旧路径。 |

协调快照测试的完整名称为 `TestCoordinatorSnapshotCompletesLostRoundTripOnce`，位置 `internal/board/coordinator_test.go`。表中各包路径相对 `internal/`。guard 与写入之间仍非原子；修复是使用受控 update，不是把 guard 的辅助检查宣称为事务。

## 跨模块验收

- `TestCoordinatorConcurrentClaimAndFencing`：双写者竞争，单一有效 coordinator epoch，相同 claim 重试及旧会话拒写。
- `TestCoordinatorBindsFirstStartFromCompleteMemberSnapshot`、`TestCoordinatorFirstStartRequiresPersistentFactsAndCAS`、`TestCoordinatorLegacyWaitingCursorAndIncompleteLaunch`：双卡含 todo、编排重启、首次启动及漏过 working、重复观察、历史保留；缺持久事实、旧 revision/CAS 和已绑定周期替换均拒绝；兼容旧等待游标及启动元数据尚未发布的中间快照。`TestCoordinatorReconcilesSequentialCommandStart` 使用真实 start 生产路径与隔离的假 tmux/Agent 验证顺序启动，不能算真实终端冒烟。
- `TestCoordinatorRecoversStartRollbackAndRetry`：在真实 commandStart 的元数据提交后确定性对账，注入 launcher 失败并完成真实回滚；编排重启、显式或漏过回滚快照后再次启动，按成功原件恢复，重复对账幂等。终端/Agent 为隔离假实现。
- `TestStartResultsFenceRetryAndPreserveTaskRevision`、`TestStartOriginalDamageStopsRecovery`、`TestConfirmedStartCannotBeRolledBackOrSilentlyReplaced`、`TestStartRollbackProofRevokesPrematureCursorAndAllowsManualClaim`、`TestStartSuccessAndRollbackHaveOneWinner`：启动期间首次 claim、同分钟旧尝试隔离、快速作者新记录/review 保留、成功与回滚互斥、各原件/指针缺失拒绝、已确认周期不可任意改写。
- `TestCoordinatorKillRestartPreservesCommittedEpoch`：在意图发布前、prepared、history、checkpoint、committed 五个真实子进程 kill 边界恢复；不丢已提交版本，不增重复事务。
- `TestCoordinatorSnapshotCompletesLostRoundTripOnce`：执行端/订阅端/编排端状态重建，快速完成、重复及重排观察，dispatch/卡片不被重写。
- `TestCoordinatorRejectsUnprovenFactsWithoutWrites`、`TestCoordinatorMembershipAndCorruptionStop`：错 ID/epoch/base/delivery/revision、未知/新增组员、损坏与 reparse 不更新检查点。
- `TestCoordinatorWrapUpRestartsAfterPartialArchive`：PM/QA 发布、缺角色不能闭批、无 finding 成员不造作者记录、多卡部分归档和完整原件消费；归档后损坏报告停止对账。
- `TestCoordinatorAllFailedRolesRemainPending`：全失败保持 pending，引用真实 output.raw，不产生收尾派回。
- `TestCoordinatorWrapUpRequiresGitAndDedicatedGrant`：结构层不假装 Git 验证，不从无 SESSION/未知投递/活动会话授予代办；只消费专用隔离 epoch。
- `TestCoordinatorRechecksActualGitAfterWrapUpRestart`：真实 Git、同轮 done、旧 worktree 删除后的恢复、实际 develop 改为不相关历史时拒绝。
- `TestCoordinatorCompletedFixSurvivesBatchAdvance`：同批 target 推进后，已完成 fix 仍验证历史原件；旧派回不可再次发送，删除作者原件则对账失败。
- `TestCoordinatorCompletedFixSurvivesClosedBatchRestart`：fix 完成、PM 增量复审及 QA 通过并真实调用闭批生产者后，尚无 wrap-up 时从旧检查点恢复；重复对账不新增事务。创建/发送仍拒绝闭批；作者、报告、闭批发布或 assignment 缺失时拒绝且不改游标。此用例验证 board 结构契约，Git 事实使用既有结构夹具，不宣称运行了真实审核 Agent 或 Git 集成。
- `TestCoordinatorAdvancesOnlyFromPersistedRoundAndEpoch`：漏过旧轮完成后消费持久终结事实；执行 epoch 变更要求真实隔离原件，缺失则拒绝。
- `TestCoordinatorFirstDeliveryRequiresActualTaskHead`：首次交付 SHA 必须匹配卡片记录的实际任务分支 HEAD，不能凭输入造交付。
- `TestDispositionCLIClosesOnlyCompleteRolesAtActualHead`：PM/QA 原件闭批后，较晚 HEAD 仍能验证同一历史闭批，取消不通过。

## 原件与迁移专项复跑

底层行为保持其原有归属，不复制实现或为同一断言新增平行测试。全量与定向验收继续运行：

- S/D：`TestCrashRestartRecoveryAndReadVisibility`、`TestMigrationKillRestart`、`TestMigrationLinkKillRestart`、`TestMigrationRecoveryAfterSecondCardPublishes`、`TestStaleRecoveryDoesNotOverwriteNewRevision`，验证事务/迁移重放、单一入口及新版本不丢失。
- A/R：`TestReviewPublishFollowsMoveAndPreservesConcurrentBody`、`TestSharedFindingRequiresEachAuthorAndNoFindingMemberNeedsNoRecord`、`TestIncrementalIDsRequireActualPredecessorLineage`、`TestPlanExtensionUsesClosedCommitNotArbitraryRolePass`、`TestLegacyMappingRequiresOriginalLocations`、`TestMechanicalFixAndNonMechanicalRerunGate`，验证移动与 PM/QA 发布、缺作者、错前驱/跨批假接续、旧报告显式映射、机械修复。
- N：`TestDispatchKillRecovery`、`TestDispatchFixBindingRelocatesAndReplays`、`TestDispatchWrapUpPublicAuthorizationReconcilesAndObserves`、`TestDispatchWrapUpGrantFencesAndRestrictsWrites`，验证稳定 ID、原子回执、原作者记录及代办四类状态。

验证报告必须给出实际运行命令、最终提交与测试计数。此映射是验收说明，不是独立 PASS 声明。POSIX 原生测试、Windows 交叉构建、Windows 原生运行、假终端、真实 tmux/herdr/Agent 冒烟分开报告；缺少环境时保留缺口。
