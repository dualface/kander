# 事件订阅链路可靠性分析

分析日期：2026-09-07。代码基线：bcf8a07。

结论：当前实现能为固定任务集合提供周期性的状态快照和变化通知，但任务组编排要求的一部分确认语义超出了这个接口的保证。主要风险是遗漏唤醒、等待无法收敛和错误的恢复判断。既有交付 SHA、依赖复查及集成门禁仍提供了保护，不能据此推断发生这些问题就一定会错误合并。

本次阅读了订阅、看板扫描与迁移、存活分类、探测执行、通知投递和任务组规则。主仓库没有修改。复现代码仅保存在本报告所在的临时副本。

## 已有的保护及实际保证

- 启动时检查成员归属、目标存在性和重复目标；首条完整快照可补足订阅建立前的最终状态。
- 后续事件携带整个监听集合的状态；多个变化可在一轮中合并。消费方可以重新读取目标卡片验证当前事实。
- ScanTargets 对缺失和重复的扫描结果最多复扫三次，能缓解原子迁移期间跨目录观测不一致。它不提供整个看板的事务快照。
- 通常的直接探测错误归为 unknown，订阅本身不修改卡片；外部命令已配置默认 10 秒超时。
- 编排规则要求接收交付时验证任务分支 SHA、集成状态及依赖，已有交付包含于组分支时不重复集成。
- 轮询、没有历史日志、全体 done 后由调用者停止，都不单独构成可靠性缺陷。若消费者只依赖可恢复的当前事实并保证操作幂等，这类机制可以成立。
- 看板出现持久重复、路径损坏或结构缺失时停止是既定保护，不应无条件重试或忽略错误。

## 1. 派回确认与状态采样的语义冲突

规则同时要求 notify 后重启订阅，以及观察 review 到 working 的 state-change 才解除“已派回、待确认”。

存在两个独立时序：

1. 执行 Agent 在新订阅建立前已进入 working。新订阅只输出 working 快照，没有对应的历史转换。
2. 一轮扫描间执行 Agent 完成 review、working、review 的往返。即使交付正文已经更新，订阅仍认为状态没有变化。

第二种场景已复现：改写交付正文后回到 review，输出只有 snapshot 和 heartbeat。只允许快照确认 working 可以解决第一种情况，但不能解决第二种情况，也不能区分不同派回轮次。

建议为派回建立持久的 dispatch_id 和接收、完成记录；快照和变化事件均触发按当前事实对账。回执、完成记录必须对应派回 ID，消费方的接收交付和释放依赖操作必须幂等。单纯缩短扫描间隔或增加进程内事件序号无法保证不遗漏快速往返。

证据：[订阅比较](/home/dualf/works/kander/internal/liveness/subscribe.go:366)、[订阅重启规则](/home/dualf/works/kander/rules/KANDER-TASK-GROUP-RULES.md:181)、[派回确认规则](/home/dualf/works/kander/rules/KANDER-TASK-GROUP-RULES.md:213)。

## 2. 通知回执可能来自输入回显

tmuxDirectNotify 向终端发送的指令含完整确认 marker，随后对 capture-pane 的全部文本执行 strings.Contains。若 Agent 界面回显输入内容，这段回显也能命中；代码不能区分输入文本和 Agent 的确认输出。

隔离假终端只回显收到的输入，没有生成任何 Agent 回执，函数仍返回 confirmed=true。此测试证明该判定能接受假阳性；并未验证所有真实 Agent 界面都会出现相同回显。

此外，投递后等待回执超时或 capture 失败可以返回“未确认”但不返回命令错误。因此 notify 的退出码 0 并不等于开始处理。

建议将终端 marker 当作提示信息，业务确认使用对应派回 ID 的显式持久回执。

证据：[终端回执检测](/home/dualf/works/kander/internal/notify/deliver.go:56)、[未确认仍返回成功的路径](/home/dualf/works/kander/internal/notify/command.go:109)。

## 3. 存活监测存在三种盲区

第一，任一监听任务的状态变化都重置统一心跳计时。重复变化使其他长期不动的 working 卡片一直没有存活探测。隔离测试在约 262ms 内观察到 12 次变化，心跳间隔设为 100ms，但没有心跳或存活信息。默认参数下相同逻辑意味着故障发现没有固定的最大等待时间。

第二，notify 后尚未开始处理的卡片仍在 review，而 subscriptionLiveness 只检查 working。因此 Agent 在接收前退出时，待确认卡片没有心跳存活结果。“合理等待后恢复”没有对应的明确确认期限与超时事件。

第三，alive 表示会话可探测；herdr 的 idle、blocked、done 也会归入 alive。它不能证明卡片工作在推进。规则直接要求 alive 时继续等待，缺少独立的进展停滞判断。

建议区分订阅心跳、会话存活和派回确认期限。存活探测使用独立最大间隔；待确认卡片即使还在 review，也应被其派回监控覆盖；对长时间无进展单独定义处理条件。

证据：[心跳重置](/home/dualf/works/kander/internal/liveness/subscribe.go:372)、[working 筛选](/home/dualf/works/kander/internal/liveness/subscribe.go:243)、[alive 分类](/home/dualf/works/kander/internal/liveness/classify.go:134)。

## 4. 反查失败被误报为停止

原 pane 失效后，herdr/tmux 反查的任何 error 都被 staleReport 映射为 stopped，其中包括命令失败、超时、结果无法解析和匹配不唯一。这些情形不能证明会话已经不存在，而且具体的反查错误没有放进最终 detail。

已复现 tmux list-panes 失败却报告 stopped。该状态可能触发多余的恢复尝试；notify 有自己的目标校验，所以不能直接推断一定会重复启动 Agent。

建议使用可区分的反查结果：唯一匹配、确认无匹配、匹配歧义、探测失败。只有确认无匹配才据此判断 stopped；歧义和失败应保留原因并报告 unknown。

证据：[反查结果分类](/home/dualf/works/kander/internal/liveness/classify.go:38)。

## 5. 外部任务组的监听集合可能不完整或过期

--watch 组成员仅在订阅建立时展开。规则允许后续增加修复卡，而依赖读取器始终按当前组成员解析，因此监听范围和依赖范围可能不一致。

例如先监听外部 A；外部组新增 B；A 完成后重新校验得知还需等待 B；B 完成时因不在监听集合内，不会产生唤醒事件。心跳规则也没有要求重新解析外部组成员。

另一个独立问题是 groupMembers 忽略 board.Scan 的 Problems，并跳过 ReadDocument 失败的条目，可能成功订阅一个不完整的组。已用非法 UTF-8 卡片复现：同组可读成员被监听，另一成员被静默省略。

最终启动前的依赖校验可能阻止错误放行，但不能补齐遗漏的监听与唤醒。

建议明确组成员冻结契约，或提供成员版本及变更后的重新展开；无法确认组成员完整性时显式报告，避免静默接受不完整集合。错误处理应保持作用域，不应因无关任务损坏就盲目扩大阻断。

证据：[组展开](/home/dualf/works/kander/internal/liveness/subscribe.go:132)、[当前依赖解析](/home/dualf/works/kander/internal/board/deps.go:23)、[外部等待规则](/home/dualf/works/kander/rules/KANDER-TASK-GROUP-RULES.md:149)。

## 6. 阻塞操作没有完整的期限与取消传播

状态扫描、整轮串行存活探测和事件写出都在同一个循环。慢探测会推迟状态扫描；stdout 不被消费时，写出最终会阻塞。stop 只在循环和 timer 等待处检查，不能打断正在执行的探测或写出。

已复现：探测延迟 250ms 时，关闭 stop 后仍等待约 252ms；期间卡片已进入 review，最后输出的心跳仍使用探测前的 working 快照。io.Pipe 模拟阻塞消费者时，stop 无法使订阅退出，关闭读端才释放写操作。

默认探测虽有 10 秒超时，但底层没有设置 WaitDelay 或子进程树清理。隔离脚本产生继承输出管道的后代进程后，30ms 的调用期限实际约 254ms 才返回。此结果证明当前 deadline 并非所有进程树情形下的返回时间上界；没有证明真实 tmux/herdr 已发生该情况。

建议传播 context，限制整轮探测预算与并发数，并明确慢消费者的处理策略。子进程及输出管道也应纳入取消范围。可添加观测时间帮助消费者判断数据新旧；事件时间不能替代消费时的状态复核。

证据：[订阅循环](/home/dualf/works/kander/internal/liveness/subscribe.go:340)、[输出](/home/dualf/works/kander/internal/liveness/subscribe.go:234)、[命令执行](/home/dualf/works/kander/internal/probe/run.go:26)。

## 次要问题与恢复边界

- CLI 接受有限正数，但未验证转换为 time.Duration 后的范围。--refresh 和 --heartbeat 同时为 1e300 时可通过解析，转换为负 duration，引起无有效等待的扫描。极小正数也可能转换为零。应在解析后校验可表示范围或设置合理最小值。
- drifted 分类得到的 NewWindow 没有进入订阅 JSON，消费者得到状态却拿不到新地址。notify 会自行再解析，因此这是信息完整性问题，不能单凭此判定恢复不可用。
- 事件没有任务修订版本、派回轮次和消费确认。进程重启的快照能补当前状态，但不能重建所有编排阶段。若要支持无人值守恢复，需要持久化最少的编排事实，并明确订阅退出后的恢复协议。
- ScanTargets 已有三次复扫。扫描异常并非完全没有缓解；结构损坏导致终止也不应当被通用重连机制吞掉。

## 验证

原仓库运行：

```sh
go test -race ./internal/liveness ./internal/board ./internal/probe ./internal/notify ./internal/launch
```

五包全部通过。临时副本额外运行以下 11 个测试，均成功复现所述边界。这里“通过”表示复现断言成立，不表示问题已经修复。

| 测试 | 观察 |
| --- | --- |
| TestAuditRoundTripIsInvisible | 状态快速往返且正文变化，未产生变化事件 |
| TestAuditChangesSuppressLiveness | 持续状态变化抑制另一个任务的存活探测 |
| TestAuditReverseLookupErrorBecomesStopped | 反查失败被分类为 stopped |
| TestAuditReviewHasNoLiveness | 待确认 review 卡片无存活数据 |
| TestAuditWatchedGroupMembershipIsFrozen | 新增外部组成员不进入监听 |
| TestAuditBlockedWriterIgnoresStop | 阻塞写出期间 stop 不生效 |
| TestAuditRefreshDurationOverflow | 正数参数转换为负 duration |
| TestAuditProbeBlocksScanAndCancellation | 慢探测阻塞扫描与取消 |
| TestAuditWatchedGroupSilentlyOmitsUnreadableMember | 不可读组成员被静默省略 |
| TestAuditDescendantOutputOutlivesProbeDeadline | 后代进程持有输出使调用超过期限 |
| TestAuditPromptEchoCountsAsAcknowledgement | 输入回显被计为通知确认 |

复现源码位于本副本 internal/liveness、internal/probe、internal/notify 下的 subscription_audit_test.go。测试使用临时看板、假命令或隔离子进程，不涉及真实 Agent 会话。Windows 与真实 Agent TUI 行为未做实机验证。

## 建议实施顺序

1. 先建立派回 ID、明确接收/完成记录和幂等对账；让快照及变化事件都能推进编排，消除对单次边沿必达的依赖。
2. 修正反查的错误分类；明确通知输出回执的可信边界。
3. 将存活探测与状态事件计时分开，增加派回确认期限，并覆盖尚在 review 的待确认任务。
4. 处理外部组成员变更与不完整展开；保留最终依赖和交付校验。
5. 补齐探测、输出、取消的期限，验证真实 tmux/herdr 和 Agent 界面。
6. 将这些隔离复现转为修复后的回归测试，并增加派回、快速完成、订阅重启、执行端退出的端到端用例。
