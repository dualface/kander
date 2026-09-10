# 首轮交付原始记录

## IMPLEMENTATION

### 2026-09-08 最终代码交付

- 最终交付：55aa8f00f408256583ed4261070beba3e437ccad；任务分支 subscription-bounded-runtime，本地与 origin/subscription-bounded-runtime 一致，两次提交均正常推送。实现提交 14c8fe627903de0db420a66a931db4906b599c05；末次提交仅更正旧同步测试注释，所有交付验证已在最终提交重跑。
- 来源及 rebase 基线：origin/group/20260908-subscription-dispatch-group，003e5fecf4048d8da8151d0431ea1cd040912a36。fetch/rebase 输出 up to date，无冲突；`git merge-base --is-ancestor 003e5fecf4048d8da8151d0431ea1cd040912a36 HEAD` exit 0，`git status --short` 无输出。核验提交：55aa8f00f408256583ed4261070beba3e437ccad。
- 工作树：/home/dualf/works/kander/worktrees/subscription-bounded-runtime。未改组分支或 develop；代码、测试、三语消息、中文文档及英文发布规则一并交付。
- 实现：单批异步探测与扫描解耦，严格绑定 revision/身份消费；单输出 worker、有限队列和从入队开始的期限。调用方 context、旧 stop、平台信号、输出失败均回收持有的任务与回调，保留 OS 不可取消 I/O 边界。
- 提交前全量曾出现既有 TestBatchEarlierCallerDeadlineWins 的计时断言失败（199.294241ms）；因创建 200ms context 后才开始计时，现将计时起点提前，未放宽阈值。最终全量/race 重跑结果见下文，旧失败未记为通过。
- 原始测试日志：[全量](verification/all-tests.jsonl)、[定向](verification/targeted-tests.jsonl)、[race](verification/race-tests.jsonl)；汇总与物理行统计：[verification.json](verification.json)；真实 PTY 冒烟脚本：[pty-smoke.py](verification/pty-smoke.py)。

### 交付自检（七项）

1. `git diff --check 003e5fecf4048d8da8151d0431ea1cd040912a36..HEAD` exit 0、无输出；对全部改动 Go 文件搜索冲突标记，`rg` exit 1（无匹配）。提交：55aa8f00f408256583ed4261070beba3e437ccad。
2. `git diff --name-only 003e5fecf4048d8da8151d0431ea1cd040912a36..HEAD` 枚举后统计物理行：15 个非生成 Go 文件均不超过 1000 行，最大 internal/liveness/liveness_test.go 为 824 行，无既有超限文件增长；`gofmt -l <全部 15 个改动 Go 文件>` exit 0、无输出。逐文件数值见 verification.json。提交：55aa8f00f408256583ed4261070beba3e437ccad。
3. `git diff 003e5fecf4048d8da8151d0431ea1cd040912a36..HEAD -- docs/subscription-facts.md docs/probe-deadlines.md rules/KANDER-KANBAN-RULES.md rules/KANDER-TASK-GROUP-RULES.md AGENTS.md` 与最终测试注释 diff 已逐段核对：独立采集、入队时钟、观测有效性、有限背压、平台取消和不可取消内核 I/O 的说明与代码一致。中文仓库文档、英文发布规则、三语消息随代码交付，Language 入口保留。提交：55aa8f00f408256583ed4261070beba3e437ccad。
4. `rg -n` 检查旧 subscriptionLiveness 和旧 `.read(` 调用，exit 1、无匹配；逐项检查新增 helper 调用链、worker/回调汇合与错误传播，未发现新增死代码。`go vet ./...` exit 0、无输出。提交：55aa8f00f408256583ed4261070beba3e437ccad。
5. 人工检查 `git diff 003e5fecf4048d8da8151d0431ea1cd040912a36..HEAD -- internal/liveness/*test.go`：扫描隔离、死亡新观测、缓存身份/期限、队列容量、短写、上下层取消错误、实际管道与信号覆盖不同边界；旧 facts 测试按异步交付调整观察间隔，没有重复整屏快照或无关断言。提交：55aa8f00f408256583ed4261070beba3e437ccad。
6. `go build ./...` exit 0；`go test -json -count=1 ./internal/liveness -run 'TestAuditProbe|TestAuditBlockedWriter|TestSubscribeChangesStillObserveAgentDeath|TestSubscription|TestSubscribeQueueOverflow'` exit 0，1 包、12 个顶层测试通过，含子用例 22 PASS / 0 SKIP / 0 FAIL。提交：55aa8f00f408256583ed4261070beba3e437ccad。
7. `go test -json -count=1 ./...` exit 0，19 包、562 个顶层测试通过，含子用例 843 PASS / 1 SKIP / 0 FAIL（Linux 跳过 TestWindowsConsoleLauncher）；`go test -json -race -count=1 ./internal/liveness ./internal/probe ./internal/i18n` exit 0，3 包、81 个顶层测试通过，含子用例 175 PASS / 0 SKIP / 0 FAIL。所有证据均在最后提交后重跑。提交：55aa8f00f408256583ed4261070beba3e437ccad。

### 逐项验收与边界

- 验收 1：复用 P3 的 10 秒批量总预算、并发 4；订阅至多单批、一个完成槽及一份缓存，扫描不排队探测。心跳增加真实观测时间/年龄、运行状态、有效性、revision/身份、采集状态和旧观测标记。TestSubscriptionDoesNotOverlapProbeBatches、TestSubscriptionRejectsSupersededObservations 及批量用例通过。提交：55aa8f00f408256583ed4261070beba3e437ccad。
- 验收 2：TestAuditProbeBlocksScanAndCancellation 已反转，真实慢命令未结束时仍收到 state-change；取消后 400ms 内返回，并检查 Linux 下所属探测进程消失。所有已启动 worker/回调汇合；文件系统与内核不可取消操作仍明确列为边界。提交：55aa8f00f408256583ed4261070beba3e437ccad。
- 验收 3：TestSubscribeChangesStillObserveAgentDeath 通过，持续修改另一卡时仍获得时间更新的 stopped 观测；这项使用临时假 herdr。真实 PTY + 当前 herdr/Agent 只读冒烟另列：snapshot/heartbeat 共 2 行，alive、runtime_state=working、observation_valid=true，Ctrl+C exit 0，3.18ms 内退出。未终止或修改真实 Agent。提交：55aa8f00f408256583ed4261070beba3e437ccad。
- 验收 4：输出限 16 个排队行加 1 个在写行、每行 1 MiB、入队到写完共 2 秒；队列满、超限、短写、断管和超时明确失败。上层不能把队列内部取消误记为成功；stderr 诊断同样限 2 秒。TestSubscriptionOutputBounds、TestSubscribeQueueOverflowIsNotSuccessfulCancellation、TestSubscriptionShortWriteFails、TestSubscriptionBlockedDiagnosticHasDeadline 通过。提交：55aa8f00f408256583ed4261070beba3e437ccad。
- 验收 5：SubscribeContext 支持调用方取消，Subscribe 保留 stop 通道；POSIX Ctrl+C/SIGTERM/SIGHUP 在同时存在满 stdout 管道和慢探测时正常退出并回收所属进程，信号回归通过。全体 done 不自动退出。Windows signal/同步取消/overlapped 路径已实现并交叉编译，未冒称原生实机通过。提交：55aa8f00f408256583ed4261070beba3e437ccad。
- 验收 6：TestAuditBlockedWriterIgnoresStop 已反转，以真实满管道且没有救援 reader 验证 stop 后 writer 汇合；TestSubscriptionPipeDeadlineAndFailure 覆盖调用方 context、写出期限、断管，TestSubscriptionSlowConsumerPreservesLines 验证慢消费者 FIFO/字节完整；队列满显式取消且确认 Writer 已返回。相关定向/race 均通过。提交：55aa8f00f408256583ed4261070beba3e437ccad。
- 验收 7：上述全量、定向、三包 race、build/vet/格式检查通过；i18n 三语目录验证通过。Windows 完整二进制与 liveness 测试交叉编译均 exit 0。PM/QA 尚未执行，按任务组规则由协调者安排；CSA/Hacker 按仓库 AGENTS.md 为 N/A。组审核部分仍待完成。提交：55aa8f00f408256583ed4261070beba3e437ccad。

### 未解决项与兼容性

1. [平台验证缺口] 原生 Windows 未运行；交叉编译不能证明 Windows Job、同步 I/O 取消、overlapped、控制台终止与句柄回收行为。对应交付：55aa8f00f408256583ed4261070beba3e437ccad。
2. [联调验证缺口] 真实 tmux 未运行；真实 herdr/Agent 仅进行了当前 pane 的只读 alive/working 观测。慢探测、死亡、满管道组合依靠假 CLI 与实际 OS 管道/子进程回归，没有宣称完整真实 Agent 故障演练。对应交付：55aa8f00f408256583ed4261070beba3e437ccad。
3. [待协调者执行] 组分支接收、PM/QA、develop 集成及最终收尾尚未执行。本执行端不触发 Reviewer、不更新组分支、不清理任务工作树。本轮没有 review run ID，不粘贴或伪造审核报告。

自定义 Writer 新契约：普通不可取消 io.Writer 在首次写入前拒绝；需实现 ContextWriter，或使用受支持的 os.File/内存 Writer。订阅独占输出期间的写操作与文件状态标志；不承诺中断任意内核 I/O，不将 partial JSON 尾行当作完整事件。没有已知未修 must-fix 实现缺陷；上述验证缺口和待办继续保留。

### 初始实施记录

### 2026-09-08 实施计划

前置 P3 已 done，提交 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5 在 develop；facts 卡已 review，交付 003e5fecf4048d8da8151d0431ea1cd040912a36 已在本地和远端组分支。任务工作树 /home/dualf/works/kander/worktrees/subscription-bounded-runtime 从该组提交创建，来源 group/20260908-subscription-dispatch-group。

实现涉及 internal/liveness、internal/i18n 与相关文档。复用 ClassifyTasksContext 的 10 秒总预算和 4 worker 上限，订阅只持有一批在途采集，按 revision/身份消费缓存。扫描不等待探测；所有任务取消后汇合。

输出采用固定容量队列、单行大小上限和含排队时间的写出期限；增加 SubscribeContext，保留 Subscribe 停止通道包装。自定义 Writer 必须提供可取消写入契约，无法取消的 Writer 在首次写入前拒绝；os.File 使用平台取消适配，内存 Writer 保留兼容。POSIX/Windows 行为与不可取消内核 I/O 边界明确记录。

验证覆盖慢探测期间状态变化、死亡新观测、旧 revision/会话丢弃、取消汇合、停读管道/慢消费者/队列满/断管/信号；最终提交执行全量、相关包 race、build/vet/格式。PM/QA 由协调者安排；CSA/Hacker N/A。

## SUMMARY

交付已完成并推送：55aa8f00f408256583ed4261070beba3e437ccad，任务分支 subscription-bounded-runtime。六项运行行为自验通过；第七项开发验证完成，PM/QA 组审核待协调者安排，CSA/Hacker N/A。最终提交全量 19 包、562 顶层测试通过（含子用例 843 PASS / 1 SKIP），三包 race 81 顶层测试通过（含子用例 175 PASS / 0 SKIP）；定向、build/vet/格式、Windows 交叉编译与真实 PTY/herdr 只读冒烟通过。以上声明均绑定 55aa8f00f408256583ed4261070beba3e437ccad，详细命令、计数及平台分列见 [验证汇总](verification.json) 和 [交付报告](report.md)。

任务分支与工作树保留，等待协调者接收和派回；进入 review 不表示已进入组分支、develop 或达到 done 门禁。原生 Windows、真实 tmux 以及完整真实 Agent 故障演练仍为验证缺口。
