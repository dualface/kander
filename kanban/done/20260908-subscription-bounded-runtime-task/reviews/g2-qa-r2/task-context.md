# 20260908-subscription-dispatch-group 批次 g2-batch-one 任务上下文

本批次包含 3 张卡，全部为本组成员，交付均已 ff 接收到组分支。base 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5（组创建锚点，等于当时 develop），target ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。每张卡的 OUT_OF_SCOPE 各自作为判断 finding 是否越界的边界。

---

## 任务卡 20260908-subscription-facts-task

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


---

## 任务卡 20260908-durable-dispatch-protocol-task

## GOAL

持久派回协议：派回意图可按稳定 ID 幂等创建与读取，同一 dispatch 的接受与完成可原子持久证明，过期执行授权不能覆盖当前任务；notify/resume 发送前绑定持久意图，不确定投递按同 ID 对账重试，终端回显不再代表已开工。

## USER_DECISIONS

2026-09-07 用户在侧会话要求按单一可验收行为重拆剩余卡，并答复"我授权"允许独立审卡、启动及按计划完成组交付/审核/develop 集成与收尾；原四张大卡由拆分卡替代，原契约与审核历史保留。

2026-09-08 用户在主会话决定："对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分." 本卡即按此决定合并而成；被合并的原卡归档为 duplicate 并指向本卡，原契约、独立审卡记录及覆盖矩阵保留不改写。合并不扩大目标，不改变 review 中 P1/P2/E1 及其组分支，不扩大集成/接管授权。

## EXPECTED_OUTCOME

本卡交付：board 中的 dispatch 类型与受控存储、move working/review/done 的原子回执与 epoch 校验、以及 actionable notify/resume 的 --dispatch-id 接入；重启不丢失身份与期限，同 ID 重试先查询回执而不盲发第二轮，旧执行端无法提交伪完成。

## ACCEPTANCE_CRITERIA

- [ ] board 纯类型定义 dispatch_id、task、fix/sync/wrap-up、消息哈希、交付/审核引用、创建及确认期限、revision，状态含 prepared/delivery-unknown/accepted/completed/failed/cancelled。
- [ ] 受控创建/读取同 ID 同输入幂等，不同 payload/任务/基线冲突；省略 ID 在任何发送前生成并输出；deadline 重启不重置，引用按任务 ID/相对 artifact 定位；原件写入复用 S 且 board 不依赖 notify；prepared 或传输 ACK 不视为 accepted。
- [ ] move working --dispatch-id 原子接受，move review/done 同 ID 及交付/处置引用原子完成，状态/revision/回执同事务，不另设冲突 status。
- [ ] 每卡仅一个有效执行授权；epoch/CAS 由受控入口检查，迟到/旧 epoch 的正文、WINDOW、回执写入拒绝，不能锁住整个 Agent 会话；旧未绑定模式不补造历史接受。
- [ ] 覆盖同 ID 重复、错 payload、旧 epoch、接受/完成边界 kill、快速 review-working-review/done；恢复后同 ID 记录唯一且新内容保留，不承诺任意外部副作用 exactly-once。意图创建边界 kill 恢复后同 ID 唯一、deadline 不重置，创建/重复/冲突/kill 恢复测试通过。
- [ ] actionable notify 接入 --dispatch-id/kind，发送前 prepared；发送边界崩溃保留 delivery-unknown；同 ID 重试先查询回执，不盲建新轮，不重复启动未知执行者。
- [ ] 消息提示使用本卡受控回执入口；终端 marker 仅作传输诊断。generic working 消息/无组卡兼容明确，旧通知无 ID 不造历史业务回执。
- [ ] notify/resume 消费 P3 预算及 unknown/stopped；更换 Agent 仍需用户授权；所有 WINDOW/正文回写走 S 和 epoch，只撤销自己未被新执行轮消费的写入。
- [ ] 反转 PromptEchoCountsAsAcknowledgement；覆盖发送前后 kill、回执先于 notify 返回、同 ID 恢复、超时、失败回滚+move 并发。
- [ ] 本卡全部行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包 -race、build/vet/格式检查通过。平台相关测试使用临时目录，原生 Windows 及真实 tmux/herdr/Agent 未执行时列明缺口，不把假 CLI 或交叉编译记为实机通过。PM/QA 按适用门禁，CSA/Hacker 在本仓库 N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

- dispatch schema/存储、board 受控回执/授权、notify/resume 投递恢复；不实现 wrap-up-only 代办授权与 R finding 引用校验（由 20260908-dispatch-evidence-binding-task 交付）、不实现订阅端 dispatch 事实消费（由 20260907-subscription-dispatch-facts-task 交付）、不做编排检查点或恢复实例（由 20260908-coordinator-recovery-task 交付）。
- 不引入通用服务、签名、保留期清理或无关优化。

## DISCUSSION

```text
PREREQUISITES: 20260907-card-write-transactions-task,20260907-probe-batch-budget-task
```

依赖必要性：S 提供按 ID 事务/CAS 和原件路径；P3 提供不误恢复且有界的探测输入，notify/resume 的投递恢复必须消费它。两者均为组外卡，须 done 且交付在 develop 可用。意图存储与回执部分技术上只需 S，但整卡一次交付，启动以两者齐备为准。

合并理由：原 N1（20260907-dispatch-intent-store-task）、N2（20260907-dispatch-atomic-receipts-task）、N3（20260907-notify-durable-delivery-task）是同一"持久派回协议"目标的严格串行链（N1→N2→N3），没有并行收益，按用户 2026-09-08 决定合并。三卡行为验收原文保留，N1 第三项"本卡不发送消息"随合并改写为"prepared/传输 ACK 不视为 accepted"。

并行边界：本卡修改 notify/board，与同组两张 subscribe 卡资源隔离，可并行。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本卡交付的一部分，不单独拆成尾部测试任务。合并后各行为可分次提交，但整卡一次交付、一次验收；实现中发现超出本卡目标的独立新行为时另卡，不为避免拆卡扩大契约。

历史来源：20260907-durable-dispatch-task（原 N 验收 1/2/3/4/5/6/7/9/11/12）；拆分历史见 20260907-probe-result-classification-task 的 resplit-plan.md，合并方案见本卡目录 consolidation-plan.md。原卡 CARD_REVIEW 不继承。

SELF_REVIEW: 已核对目标单一（持久派回协议）、九项行为验收与原 N1/N2/N3 一一对应且无自相矛盾、两个前置为真实技术依赖且无环、与后继三卡边界互斥。待独立审卡。

CARD_REVIEW: PASS — 2026-09-08，独立子Agent（全新会话，只读整合后 6 张卡、12 张被合并原卡、resplit-plan.md 覆盖矩阵与 consolidation-plan.md）：目标单一且与用户 2026-09-08 决定一致，合并后验收完整覆盖原卡且可判定，前置存在、必要、无环且组内外分类正确，OUT_OF_SCOPE 与同组卡互斥。无阻断；非阻断建议（意图创建边界 kill 场景、保留“英文发布规则”字样、coordinator 第 8 项改为消费措辞、方案中 P3 并行表述）已由建卡者采纳修订。仅契约审查，不代表实现/PM/QA 通过。


---

## 任务卡 20260908-subscription-bounded-runtime-task

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


