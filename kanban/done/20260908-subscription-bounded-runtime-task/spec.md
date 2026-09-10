# Neither slow probes nor stalled consumers block subscription scans and exit

- TYPE: Bug
- SIZE: large
- TASK_GROUP: 20260908-subscription-dispatch-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 00:29
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t15:wX:p1Q
- STARTED_AT: 2026-09-08 02:55
- FINISHED_AT: 2026-09-08 04:13
- TASK_BRANCH: subscription-bounded-runtime
- RESULT: completed

## GOAL

订阅运行时有界：存活采集使用独立有界调度，慢命令期间状态扫描和取消仍及时执行；stdout 背压有明确上限，消费者停读或取消时订阅仍能退出且无残留。

## USER_DECISIONS

2026-09-07 用户在侧会话要求按单一可验收行为重拆剩余卡，并答复"我授权"允许独立审卡、启动及按计划完成组交付/审核/develop 集成与收尾；原四张大卡由拆分卡替代，原契约与审核历史保留。

2026-09-08 用户在主会话决定："对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分." 本卡即按此决定合并而成；被合并的原卡归档为 duplicate 并指向本卡，原契约、独立审卡记录及覆盖矩阵保留不改写。合并不扩大目标，不改变 review 中 P1/P2/E1 及其组分支，不扩大集成/接管授权。

## EXPECTED_OUTCOME

本卡交付：subscribe 的探测调度与状态扫描解耦并消费 P3 总预算，慢探测期间状态更新仍可见、取消可回收全部调度任务；输出采用有限缓冲与写出期限，Ctrl+C、平台终止、调用方 context、管道失败均能有界退出并回收 writer 与探测任务。

## ACCEPTANCE_CRITERIA

- [ ] 消费 P3 总预算/并发限制，扫描与探测解耦，队列有界；心跳输出观测时间/年龄/运行状态，旧 revision 或旧会话结果标过期或丢弃。
- [ ] 慢探测期间状态更新仍可见，取消回收所有调度任务；不增加无限后台 goroutine 或承诺不可取消文件系统 I/O。
- [ ] 反转 TestAuditProbeBlocksScanAndCancellation；持续状态变化且另一 Agent 死亡时仍按期获得新观测，真实终端冒烟与假命令结果分列。
- [ ] 有限缓冲、写出期限与明确退出策略；禁止无限队列及不可退出后台 Writer，输出失败返回明确错误。
- [ ] Ctrl+C、平台终止、调用方 context、管道失败回收 writer 和探测任务；全体 done 是否退出仍由消费者决定。
- [ ] 反转 TestAuditBlockedWriterIgnoresStop；阻塞管道+取消、慢消费者、队列满、断管均验证有界退出及无残留，POSIX/Windows 差异分列。
- [ ] 本卡全部行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包 -race、build/vet/格式检查通过。平台相关测试使用临时目录，原生 Windows 及真实 tmux/herdr/Agent 未执行时列明缺口，不把假 CLI 或交叉编译记为实机通过。PM/QA 按适用门禁，CSA/Hacker 在本仓库 N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

- subscribe 存活调度与输出生命周期；不实现派回 deadline、恢复决策，不改事件业务含义或 TUI。
- 不实现订阅事实 schema/成员展开（由 20260908-subscription-facts-task 交付），不接入派回事实（由 20260907-subscription-dispatch-facts-task 交付）。
- 不引入通用服务、签名、保留期清理或无关优化。

## DISCUSSION

```text
PREREQUISITES: 20260907-probe-batch-budget-task,20260908-subscription-facts-task
```

依赖必要性：P3 提供可回收的批量采集与总预算（组外，须 done 且在 develop 可用）；20260908-subscription-facts-task 提供 revision 绑定和同一订阅入口的事实读取，缺一不能保证及时且不误用旧观测。

合并理由：原 E4（20260907-subscription-probe-isolation-task）与 E5（20260907-subscription-output-exit-task）同为"订阅运行时有界"目标，且 E5 原本只因共享运行循环与取消资源而串行于 E4，没有并行收益，按用户 2026-09-08 决定合并。E4 前三项与 E5 前三项验收原文保留。

并行边界：本卡与同组 20260908-durable-dispatch-protocol-task 修改不同模块，可并行；与 20260908-subscription-facts-task 串行。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本卡交付的一部分，不单独拆成尾部测试任务。合并后各行为可分次提交，但整卡一次交付、一次验收；实现中发现超出本卡目标的独立新行为时另卡，不为避免拆卡扩大契约。

历史来源：20260907-subscription-reconcile-task（原 E 验收 4/9/10/11/12）；拆分历史见 20260907-probe-result-classification-task 的 resplit-plan.md，合并方案见 20260908-durable-dispatch-protocol-task 的 consolidation-plan.md。原卡 CARD_REVIEW 不继承。

SELF_REVIEW: 已核对目标单一（订阅运行时有界）、六项行为验收与原 E4/E5 一一对应、两个前置均为真实技术依赖且无环、与同组两卡边界互斥。待独立审卡。

CARD_REVIEW: PASS — 2026-09-08，独立子Agent（全新会话，只读整合后 6 张卡、12 张被合并原卡、resplit-plan.md 覆盖矩阵与 consolidation-plan.md）：目标单一且与用户 2026-09-08 决定一致，合并后验收完整覆盖原卡且可判定，前置存在、必要、无环且组内外分类正确，OUT_OF_SCOPE 与同组卡互斥。无阻断；非阻断建议（意图创建边界 kill 场景、保留“英文发布规则”字样、coordinator 第 8 项改为消费措辞、方案中 P3 并行表述）已由建卡者采纳修订。仅契约审查，不代表实现/PM/QA 通过。

## IMPLEMENTATION

首轮历史：交付 55aa8f00f408256583ed4261070beba3e437ccad，基于 003e5fecf4048d8da8151d0431ea1cd040912a36；实现、六项行为验收及当时验证详见 [首轮完整记录](delivery-before-sync.md)。原始记录与日志保留，此 SHA 已由本轮 rebase 重写。

### 2026-09-08 同步派回第 1 轮

- 本次为组分支同步，没有审核 finding；收到通知后自行 review → working，OWNER/SESSION 保持。新组基线：c5dcfbc401d5748da7eb0c89011ea422f51c33cc，来源 origin/group/20260908-subscription-dispatch-group。
- 最终交付：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。两提交映射为 14c8fe627903de0db420a66a931db4906b599c05 → d360f92240961a5883c0f535a6dfc15e65b80350，55aa8f00f408256583ed4261070beba3e437ccad → ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- fetch 成功后 rebase 两提交完成。冲突共 4 文件：AGENTS.md 保留上游“持久派回协议”索引，同时保留本卡扩展后的订阅索引；internal/i18n/locales/en.json、ja.json、zh-CN.json 各保留上游新增派回消息和本卡六项订阅消息。只合并冲突块，没有整文件替换，没有重大代码冲突或范围扩展。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- 三语 JSON 精确比较“新组基线 + 本卡相对原基线的六项增量”，各语言通过；上游对既有消息的合法更新也保留。辅助比较脚本第一次错误要求旧基线全部字符串不变而失败，随后修正比较范围；产品测试结果以下方实际重跑为准。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- `git diff --exit-code 55aa8f00f408256583ed4261070beba3e437ccad HEAD -- internal/liveness docs/subscription-facts.md docs/probe-deadlines.md` exit 0、无输出；Go 实现、测试和两份订阅说明与首轮逐字一致。`git range-diff` 仅显示索引与消息插入处的基线上下文变化，第二提交保持等价。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- `git push --force-with-lease=refs/heads/subscription-bounded-runtime:55aa8f00f408256583ed4261070beba3e437ccad origin HEAD:refs/heads/subscription-bounded-runtime` 成功；`git ls-remote origin refs/heads/subscription-bounded-runtime refs/heads/group/20260908-subscription-dispatch-group` 确认远端任务分支为 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe、组分支仍为 c5dcfbc401d5748da7eb0c89011ea422f51c33cc。没有更新组分支或 develop。核验提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。

#### 本轮交付自检（七项）

1. `git diff --check c5dcfbc401d5748da7eb0c89011ea422f51c33cc..HEAD` exit 0、无输出；改动 Go 文件冲突标记搜索 `rg` exit 1、无匹配。冲突 JSON 解析和索引块核验完成，无残留标记。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
2. `git diff --name-only c5dcfbc401d5748da7eb0c89011ea422f51c33cc..HEAD` 枚举后的 15 个非生成 Go 文件均 ≤1000 物理行，最大 824 行；`gofmt -l <15 个文件>` exit 0、无输出。逐文件计数见本轮验证汇总。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
3. 对照最终 `git range-diff`、4 个冲突块和发布协议差异，订阅行为文档与此前代码保持一致；持久派回文档索引、三语消息及自动合并的派回/订阅规则均保留。无过期同步写出注释。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
4. 本轮没有新增 Go 代码；上面的逐字一致比较确认原调用链、worker/回调汇合及错误处理完整保留，未增加不可达或无引用代码。`go vet ./...` exit 0、无输出。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
5. 本轮未新增或复制测试；Go 测试与原交付逐字一致，原覆盖边界保持，无新增冗余断言。定向回归与全量均实际重跑。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
6. `go build ./...` exit 0；`go test -json -count=1 ./internal/liveness -run 'TestAuditProbe|TestAuditBlockedWriter|TestSubscribeChangesStillObserveAgentDeath|TestSubscription|TestSubscribeQueueOverflow'` exit 0，1 包、12 顶层测试通过，含子用例 22 PASS / 0 SKIP / 0 FAIL。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
7. `go test -json -count=1 ./...` exit 0，19 包、584 顶层测试通过，含子用例 899 PASS / 1 SKIP / 0 FAIL；`go test -json -race -count=1 ./internal/liveness ./internal/probe ./internal/i18n` exit 0，3 包、81 顶层测试通过，含子用例 175 PASS / 0 SKIP / 0 FAIL。未沿用旧 SHA 的测试结果。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。

#### 本轮附加验证与交付边界

- `GOOS=windows GOARCH=amd64 go build -o /tmp/subscription-runtime-final.exe ./cmd/kander` 与 `GOOS=windows GOARCH=amd64 go test -c -o /tmp/subscription-runtime-final-windows.test.exe ./internal/liveness` 均 exit 0。仅交叉编译，原生 Windows 仍未运行。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- 重建当前二进制后重跑真实 PTY/herdr 只读冒烟：snapshot/heartbeat 共 2 行，alive、runtime_state=working、observation_valid=true；Ctrl+C exit 0，3.19ms 退出。未改动或终止真实 Agent。真实 tmux 与完整真实 Agent 故障演练仍未运行；慢探测/死亡/停读组合依靠临时假 CLI、真实管道及子进程自动测试。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- `git merge-base --is-ancestor c5dcfbc401d5748da7eb0c89011ea422f51c33cc HEAD` exit 0，`git status --short` 无输出；任务分支和 /home/dualf/works/kander/worktrees/subscription-bounded-runtime 保留，等待协调者接收。核验提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- PM/QA 未执行，由协调者安排；CSA/Hacker 按本仓库 AGENTS.md 为 N/A。没有审核 run ID，没有 finding 或作者 disposition 要求。组接收、develop 集成及收尾均待协调者后续调度。
- [本轮验证汇总](verification/sync-1.json)、[全量原始日志](verification/sync-1/all-tests.jsonl)、[定向原始日志](verification/sync-1/targeted-tests.jsonl)、[race 原始日志](verification/sync-1/race-tests.jsonl)。首轮证据保留原 SHA，仅作为历史。


### 2026-09-08 集成后收尾

本段更新最终状态；上面的 IMPLEMENTATION 与验证原文是当时的交付历史，完整保留。授权依据为 [收尾通知原文](wrap-up-notice.md)。本轮没有修改代码、重跑 Reviewer、rebase 或集成。

- 本卡最终交付 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 已被组分支 group/20260908-subscription-dispatch-group 接收。最终组提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 已由协调者快进集成并推送 develop；本轮 fetch 后实查本地 develop、origin/develop 均等于该完整 SHA，主工作树干净。分别以本卡交付与最终组提交为祖先，对两个 develop 引用运行 `git merge-base --is-ancestor`，四次均 exit 0。旧 55aa8f0 的重写映射保留于上文，不用于本轮祖先核验。
- [计划 g2-subscription-dispatch](reviews/plan.json) 已 sealed，[批次 g2-batch-one](reviews/batches/g2-batch-one/closed.json) 已 closed，base 为 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5，最终 target 为 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3，共 1 个修复轮。`kander review progress /home/dualf/works/kander/worktrees/20260908-subscription-dispatch-group 20260908-subscription-bounded-runtime-task` 返回 closed。
- PM/codex：[g2-pm-r1](reviews/g2-pm-r1/report.md) 的 PM-001..004 已关闭；闭批选用 [g2-pm-r2](reviews/g2-pm-r2/report.md)，最终 PASS，FINDINGS/NON_BLOCKING 均为空。QA/codex：[g2-qa-r1](reviews/g2-qa-r1/report.md) 的 QA-001..002 已关闭；闭批选用 [g2-qa-r3](reviews/g2-qa-r3/report.md)，最终 PASS，FINDINGS/NON_BLOCKING 均为空。两个角色 passed_at 均为最终组 SHA。CSA、Hacker 按本仓库 AGENTS.md 特例为 N/A，未执行。
- [g2-qa-r2 失败原件](reviews/g2-qa-r2/manifest.json) 记录 execution_status=failed、exit 143；协调者说明原因为本机内存压力导致外部终止。同 ID 恢复只补齐失败证据发布，不代表重新审核或 PASS。闭批 resolved_failures 已显式绑定成功替代 g2-qa-r3，失败不再阻塞。
- [批次处置](reviews/batches/g2-batch-one/disposition.json)：6 个来源 finding、5 个根因（PM-004 与 QA-002 同为 documentation），全部归属 durable-dispatch-protocol 卡并由其作者 confirmed 后 fixed。本卡无归属 finding、无作者 disposition 义务。无 rejected、无 unverifiable finding、无 NON_BLOCKING；平台验证缺口单独保留下述两项。
- 协调者在最终组 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 实跑 `go build ./...`、`go vet ./...`、`go test -count=1 ./...`，均 exit 0，19 包全部 ok，记录见闭批原件。本轮没有重跑这些测试。本作者在本卡 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 的全量、定向、race、交叉编译与真实 PTY/herdr 只读冒烟结果仍以上文和 [验证汇总](verification/sync-1.json) 为准。
- 清理前确认任务 worktree 干净且 HEAD 为本卡交付；从主工作树执行 `git worktree remove /home/dualf/works/kander/worktrees/subscription-bounded-runtime`、`git branch -d subscription-bounded-runtime`、带 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 精确 lease 的远端任务分支删除，均 exit 0。复查路径、本地分支、远端分支均不存在。组 worktree 与组分支保留；未删除或改写审核原件、验证原件。未创建本卡临时 Reviewer 输出，临时审核文件清理 N/A；交互 CLI 与终端容器保留。
- 七项验收：1）独立有界 P3 调度、身份/版本有效性与观测字段完成；2）慢探测不阻塞扫描，取消汇合完成；3）原缺陷反转及持续变化期间 Agent 死亡回归完成，真实/隔离证据分列；4）有限队列、写出期限与失败退出完成；5）信号、context、断管回收完成，消费者仍决定 done 后退出；6）阻塞/慢读/满队列/断管测试完成，POSIX/Windows 边界保留；7）测试、文档、三语消息及 PM/QA 门禁完成，CSA/Hacker N/A。代码验收 7/7；下列平台缺口不冒称实机通过。
- 未解决项 2 项：[验证缺口][Unverifiable] 原生 Windows 未执行，交叉编译及 POSIX 隔离测试仅提供替代证据，不能证明 Windows 运行行为；[验证缺口][Unverifiable] 真实 tmux 与完整真实 Agent 故障/派回组合未执行，假 CLI 只证明隔离场景。本卡已有真实 Linux PTY、当前 herdr/Agent 的只读 snapshot/heartbeat 与 Ctrl+C 冒烟（ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe，2 事件、exit 0、3.19ms），因此组通知中“真实 herdr/Agent 未运行”的概括不覆盖这项已留存的窄范围证据；未执行范围仍需相应真实环境补验。
- Git 与清理核验见 [收尾核验](verification/wrap-up.json)。完成门禁：`kander move 20260908-subscription-bounded-runtime-task done --result completed` exit 0；随后定向 `kander check 20260908-subscription-bounded-runtime-task` exit 0，输出 `ok: 1 tasks`。收尾适用步骤 all completed，Final card state: done。

## SUMMARY

本卡交付 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 已集成 develop；最终组提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 与本地、远端 develop 一致。PM/QA PASS，CSA/Hacker N/A；g2-batch-one 闭批，1 个修复轮，本卡无 finding。验收 7/7，原生 Windows、真实 tmux/完整 Agent 故障组合两项验证缺口保留；已有真实 PTY/herdr 只读冒烟不改写。

任务 worktree、本地与远端任务分支已删除；组资源与交互会话保留。完成门禁：`kander move 20260908-subscription-bounded-runtime-task done --result completed` exit 0；随后定向 `kander check 20260908-subscription-bounded-runtime-task` exit 0，输出 `ok: 1 tasks`。收尾适用步骤 all completed，Final card state: done。详细证据见 [完成报告](report.md)、[收尾核验](verification/wrap-up.json) 与上文相对审核链接。

## REVIEWS

- {"run_id":"g2-qa-r1","batch_id":"g2-batch-one","role":"QA","execution_status":"ok","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe","report":"reviews/g2-qa-r1/report.md"}
- {"run_id":"g2-pm-r1","batch_id":"g2-batch-one","role":"PM","execution_status":"ok","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe","report":"reviews/g2-pm-r1/report.md"}
- {"run_id":"g2-pm-r2","batch_id":"g2-batch-one","role":"PM","execution_status":"ok","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","previous_run_id":"g2-pm-r1","report":"reviews/g2-pm-r2/report.md"}
- {"run_id":"g2-qa-r2","batch_id":"g2-batch-one","role":"QA","execution_status":"failed","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","previous_run_id":"g2-qa-r1","report":"reviews/g2-qa-r2/output.raw"}
- {"run_id":"g2-qa-r3","batch_id":"g2-batch-one","role":"QA","execution_status":"ok","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","previous_run_id":"g2-qa-r1","report":"reviews/g2-qa-r3/report.md"}
