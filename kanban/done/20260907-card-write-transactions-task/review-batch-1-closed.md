# 第一批闭批证据副本

以下为编排端 s-batch1-closed.md 原文，保存供收尾追溯；其中“尚未集成”描述闭批当时状态，不是当前状态。本文件不是新工具的机器审核索引。

# S 首批闭批记录

base: 4889d3fb93f8d2d639832b9da5588d668714d9bc
closed commit: a7fe54beb6ade00678e14655c0d385115eb950c8
PM/QA 首轮未通过、第二轮增量通过，CSA/Hacker N/A。原作者全部接受并修复，无拒绝或未修复 finding。环境缺口：原生 Windows 未执行。尚未集成 develop 或收尾。后批 base 必须为此 closed commit。

## PM 第二轮原文
Role: PM  
Commit: `a7fe54beb6ade00678e14655c0d385115eb950c8`  
Task Context: `/tmp/kander-s-batch1-context/spec.md`  
Reviewed Scope: 仅 `f056af9..a7fe54b` 修复范围及所列旧 finding；未变化代码仅用于判断修复影响。

**结论：通过本轮增量复审。所列 6 项旧 finding 均关闭；无新增门禁问题。**

### Requirement Table

生产行为为静态调用链判断（Inferred）；代码、注释及测试存在性为 Observed，不代表本轮执行通过。

| Requirement | Expected Behavior | Code Evidence | Status |
|---|---|---|---|
| 验收1、11；PM-001、QA-01 | journal 枚举、读取与发布互斥，锁顺序固定 | `transaction_log.go:62、110、118`；`transaction.go:115`；`recovery.go:215` | Complete |
| 验收6、11；PM-002、QA-04 | 注释准确表达 CAS 回滚及冲突保留 | `notify_resume.go:29`；`snapshot.go:117` | Complete |
| 验收6、7；QA-02 | 订阅组展开遇读取冲突显式失败 | `subscribe.go:140、175` | Complete |
| 所列尺寸处置；QA-03 | 缩短原测试文件，保留回滚覆盖 | `launch_test.go` 989 行；`rollback_test.go:10` | Complete |
| 验收9、11；修复回归 | 覆盖 journal 两类并发窗口及成员读取冲突 | `journal_concurrency_test.go:114、178`；`journal_temporary_windows_test.go:19`；`subscribe_members_test.go:30` | Complete |
| 验收11；执行验证 | 全量、race、Windows 验证结果可独立确认 | 执行端报告通过；本轮未复跑，原生 Windows 缺口明确 | Unverifiable |

统计：Complete 5，Partial 0，Missing 0，Contradicted 0，Unverifiable 1。其余验收沿用上轮已接受结论，未重新审核。

### Findings

- **PM-001：关闭。Inferred，置信度高。** [operationRecords](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/transaction_log.go:62) 在共享 journal 锁内完成枚举与读取；[writeOperation](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/transaction_log.go:118) 独占同一稳定锁，覆盖原子写函数返回前的句柄关闭。prepared 与 committed 两个生产发布入口均已接入，原临时句柄共享冲突路径关闭。
- **PM-002：关闭。Observed。** [新版注释](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/launch/notify_resume.go:29) 明确仅当前操作仍拥有 revision 时恢复，否则保留新记录并报告冲突；与 `snapshot.go:117` 的校验及失败传播一致。
- **QA-01：关闭。Inferred，置信度高。** [committed 发布](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/recovery.go:215) 取得 journal 独占锁，等待读者关闭句柄；正常并发不再因该共享冲突留下 prepared。[双进程回归](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/journal_concurrency_test.go:114) 检查等待、完成、无 prepared 遗留及正文/revision。
- **QA-02：关闭。Inferred，置信度高。** [groupMembersFrom](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/liveness/subscribe.go:140) 遇读取错误返回 `nil, err`；调用方继续传播，无法以部分成员集成功启动。[回归](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/liveness/subscribe_members_test.go:30) 在 Scan 后真实更新，检查显式失败及重新扫描后的完整成员集。
- **QA-03：关闭。Observed。** `launch_test.go` 从 1035 行降至 989 行；原测试完整移至 [rollback_test.go](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/launch/rollback_test.go:10)，函数内容未变。仅确认尺寸处置，不新增规则来源断言。
- **QA-04：关闭。Observed。** 与 PM-002 同一注释修复，已逐项核对。

新增 gate findings：无。

### NON-BLOCKING

NON-BLOCKING: none

Observed：`git diff --check` 通过，工作树干净。Unverifiable：本轮未执行 Go 测试、race 或 Windows 实机验证，不把执行端声明视为本轮实测。

已尝试删除任务文件；只读文件系统拒绝，文件保留，不影响审核结果。

## QA 第二轮原文
Role: QA  
Commit: `a7fe54beb6ade00678e14655c0d385115eb950c8`  
Task Context: `/tmp/kander-s-batch1-context/spec.md`  
Reviewed Scope: 仅 `f056af9..a7fe54b` 修复范围及判断修复影响所需调用链；未扩审未变代码。

**结论：通过增量复审。QA-01～QA-04 全部关闭；无新增门禁问题。**

| 行为/质量项 | 结论与精确证据 |
|---|---|
| QA-01：journal 并发 | **Closed — Inferred，高置信度。** `internal/board/transaction_log.go:62` 以共享锁覆盖枚举和读取；`:110`、`:118` 提供末端锁及独占发布。`transaction.go:115`、`recovery.go:215` 均接入。Windows 临时句柄在 `internal/fs/windows_ops.go:547` 的 defer 中关闭，早于 journal 解锁，原共享冲突路径已隔离。 |
| QA-02：订阅漏成员 | **Closed — Observed。** `internal/liveness/subscribe.go:145` 遇读取错误返回 `nil, err`，`:175` 调用后继续向上传播。`subscribe_members_test.go:38` 在 Scan 后真实 update；`:43` 断言显式失败且无部分结果；`:47` 断言重新扫描得到完整成员。 |
| QA-03：文件尺寸 | **Closed — Observed。** `internal/launch/launch_test.go` 从 1035 行降至 989 行；`rollback_test.go:10` 承接原测试，新文件 54 行。逐字比较确认测试函数未变；本轮所有变更 Go 文件均不超过 1000 行。 |
| QA-04：回滚注释 | **Closed — Observed。** `internal/launch/notify_resume.go:29` 明确仅持有当前 revision 才恢复，否则保留新记录并报告冲突；与 `internal/board/snapshot.go:117` 的 CAS 校验一致。 |
| 架构与锁顺序 | **Observed。** journal 同步归 board，锁原语复用 fs；liveness 保持消费 board API。`transaction_lock.go:11` 与 `docs/card-transactions.md:31` 同步末端锁契约，无新增反向依赖。 |
| 并发回归 | **Observed。** `journal_concurrency_test.go:114`、`:178` 分别覆盖读句柄与 committed 替换、临时发布句柄与读取；断言等待、完成及无遗留 prepared。Windows 夹具见 `journal_temporary_windows_test.go:19`。 |
| 执行验证 | **Observed：** `git diff --check` 通过，工作树干净。**Unverifiable：** 本轮未复跑 Go 测试、race、交叉编译或 Windows 原生测试；执行端报告不计作本轮实测。 |

Gate Findings：none。

NON-BLOCKING: none

已尝试删除任务文件；只读文件系统拒绝，文件保留。不影响审核结论。