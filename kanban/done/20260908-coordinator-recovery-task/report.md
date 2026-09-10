# 编排检查点、事实对账与收尾恢复交付报告

## 交付

任务分支 `coordinator-recovery`，工作树 `/home/dualf/works/kander/worktrees/coordinator-recovery`。来源组分支 `group/20260908-bindings-recovery-group`；组基线 `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`；最终交付 `0b304a8399cdbc8db2c005e390afd3f6127532af` 已正常推送。本地及远端任务 HEAD 一致，组基线是交付祖先，工作树干净 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。未更新组分支、未集成 develop、未清理任务资源。

新增 `kander coordinator show/claim/reconcile`：事务化 checkpoint/current history、单写 epoch、统一持久事实对账。launch 复用实际 Git 集成验证，并单向消费 review 的历史闭批校验 API；board 不反向依赖 launch/review/notify。初次交付验证实际任务 HEAD；绑定派回只认可原子回执。工作区清理后可从仍存在的绝对 CWD 复核相同提交。检查点不自动通知、接管、集成、代收尾或生成 PASS。

## 验收自检

验收 1–10 作者自检完成，第 11 自动化部分完成；审核与平台缺口如实保留。所有下面的测试结论均来自最终交付提交，不把早期日志当最终证据。

- 第 1 项：新增 coordinator 单二进制入口；board/group/task/journal 固定锁序；checkpoint revision 与 coordinator epoch CAS。 验证：TestCoordinatorConcurrentClaimAndFencing （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 第 2 项：成员版本、任务 revision/周期、交付、batch/run 相对引用及 pending 项；相同观察不增事务。 验证：TestCoordinatorSnapshotCompletesLostRoundTripOnce （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 第 3 项：竞争写者仅一个成功；旧 epoch 拒写；五个 kill 边界恢复后保留已提交记录。 验证：TestCoordinatorKillRestartPreservesCommittedEpoch （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 第 4 项：按原件核对同 ID/epoch/base 与最终交付；错轮、旧交付、未知成员不更新。 验证：TestCoordinatorRejectsUnprovenFactsWithoutWrites；TestCoordinatorAdvancesOnlyFromPersistedRoundAndEpoch （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 第 5 项：规则限定 EOF/输出失败一次事实对账和一次重连；仅已知 pending 事务按维护协议恢复；损坏/reparse 停止，锁等待可取消；不把无输出、心跳或 alive 当业务完成。 验证：TestCoordinatorMembershipAndCorruptionStop；TestCoordinatorDeadlinePreservesCursor；英文发布规则人工核对 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 第 6 项：删除必须观察 review-working state-change 的矛盾要求，snapshot 与后续事件同一对账；执行/订阅/编排重启、重复重排事件和同 ID 重试复跑。 验证：TestCoordinatorSnapshotCompletesLostRoundTripOnce；TestSubscribeRestartProcess；TestAuditWatchedGroupMembershipIsFrozen （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 第 7 项：消费 A/R 原件、作者处置、required 角色、闭批及实际 Git；机械修复和较晚 HEAD 保持原门禁；无 finding 不补造记录。 验证：TestCoordinatorWrapUpRestartsAfterPartialArchive；TestCoordinatorCompletedFixSurvivesBatchAdvance；TestDispositionCLIClosesOnlyCompleteRolesAtActualHead （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 第 8 项：消费同轮 wrap-up/integration 绑定；检查点不授予接管、集成或代收尾；只消费已隔离 epoch 的专用 grant。 验证：TestCoordinatorWrapUpRequiresGitAndDedicatedGrant；TestCoordinatorRechecksActualGitAfterWrapUpRestart （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 第 9 项：多卡部分归档、PM/QA 原件、缺角色/全失败、错前驱/跨批、旧报告显式映射、无 finding、代办四类状态；复用原所属包回归。 验证：TestCoordinatorAllFailedRolesRemainPending；docs/recovery-regressions.md 跨模块映射，相关用例均在最终全量与 race 日志 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 第 10 项：原 13 个坏行为已逐一映射为反向断言，并从最终日志核对全部 13 个测试 PASS；S/D kill、旧路径回滚、新作者记录及无重复事务/入口/索引复跑。 验证：verification/summary.json；docs/recovery-regressions.md （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 第 11 项：代码、必要文档、三语消息与自动化交付；PM/QA 仍由协调者组审核，CSA/Hacker N/A；平台实机缺口单列。 验证：全量、8 包 race、build/vet/gofmt/diff、Windows 交叉构建及三语 CLI 帮助 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。

## 最终验证

- `go test -json -count=1 ./...`：退出 0，19 包、1001 测试项 PASS，1 SKIP；其中顶层 630 PASS/1 SKIP，统计测试项含子用例 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- `go test -race -json -count=1 ./internal/fs ./internal/board ./internal/launch ./internal/review ./internal/cli ./internal/liveness ./internal/notify ./internal/window`：退出 0，8 包、737 测试项 PASS，1 SKIP；顶层 409 PASS/1 SKIP （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- `go vet ./...`、`go build ./...`、修改 Go 文件的 `gofmt -l`、`git diff --check 6f5ed32072275c3bb9d96c9e40bd606b949b9c17 HEAD`：退出 0，无格式/差异错误 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 22 个修改 Go 文件，最多 450 物理行，均不超过 1000；冲突标记 0 个 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- `GOOS=windows GOARCH=amd64 go build -o /tmp/coordinator-final-validation/kander.exe ./cmd/kander`：退出 0；仅交叉构建 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 最终构建二进制 `--lang cn/en/ja coordinator --help` 各退出 0，三语入口帮助可用 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。
- 13 个原始复现对应测试的最终 PASS 已逐一对照，机器映射见汇总；S/D/N 的 kill/restart、旧路径回滚和作者/索引保护用例均在最终全量与 race 中执行 （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。

证据：[机器汇总](verification/summary.json)、[检查命令及输出](verification/checks.json)、[全量测试原始 JSONL](verification/all.jsonl)、[race 原始 JSONL](verification/race.jsonl)。所有日志绑定 `0b304a8399cdbc8db2c005e390afd3f6127532af`。

实现期间修正了新增测试夹具的 SUMMARY 缺失、错误隔离 epoch 文件名，以及审核后改组导致原件绑定拒绝；这些失败均发生在最终提交前，修正后从最终提交复跑上述命令。没有把失败运行写成通过。

## 审核、风险与收尾

PM/QA：由协调者接收后按组安排，尚无本卡本轮审核 run ID；没有冒充审核 PASS。CSA/Hacker：N/A，依据仓库 AGENTS.md 明确特例。后续审核原件只能按 run ID 引用，不在卡片复制报告。

未执行：原生 Windows 测试；真实 tmux/herdr/Agent 冒烟。自动化中的假 CLI/终端、真实本机 Git 和隔离子进程 kill 均不等同上述实机验证。唯一自动跳过测试为 TestWindowsConsoleLauncher （提交 `0b304a8399cdbc8db2c005e390afd3f6127532af`）。

边界：固定成员集变化需先处理真实契约；不静默删除/添加成员。CLI context 限制准备阶段锁竞争与 Git，OS I/O 和 redo 发布保留既有边界，不宣称整条命令硬期限。用户原始集成与接管授权不扩大。

本轮仅交付 review，保留任务分支、远端分支和 worktree。组分支接收、PM/QA、develop 集成、最终 done 与清理由协调者后续派回；交互会话保留。
