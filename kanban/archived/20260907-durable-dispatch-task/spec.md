# Durable dispatch protocol, business receipts and execution-round deduplication

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260907-orchestration-reliability-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 14:18
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT: cancelled

## GOAL

将 notify 的派回意图、业务接收和完成结果持久化并绑定同一 dispatch_id，消除重启/快速往返丢确认、终端回显假 ACK及恢复重复执行；复用 S 的版本事务和 R 的审核证据。

## USER_DECISIONS

本轮用户要求把事件链路、重复卡及审核证据统一规划并建卡。保留 notify 不替执行 Agent 迁卡、原执行端处理派回、更换 Agent 需用户授权；具体 ID 和记录 schema 为技术设计。

## EXPECTED_OUTCOME

- 发送前有稳定派回事实；工作开始/完成通过受控状态迁移记录，不依赖终端文字或采样边沿。
- 同 ID 重试可对账，迟到或旧执行轮次不能覆盖当前任务。
- pending review 卡的确认期限可被 E 监控，重启不重置业务期限。

## ACCEPTANCE_CRITERIA

- [ ] 为 actionable notify 定义 --dispatch-id 和 fix/sync/wrap-up 类型、消息哈希、任务/交付/审核绑定、创建及确认期限；未显式给 ID 时在副作用前生成并持久化，再在输出返回，重试能定位既有记录。
- [ ] 派回记录区分 prepared、delivery-unknown、accepted、completed、failed/cancelled；传输成功或终端 ACK 不等于 accepted。发送成功与持久化完成之间崩溃时保留不确定状态，不新建第二轮盲发。
- [ ] 执行 Agent 用 move <id> working --dispatch-id <id> 原子接受；用对应 ID 提交交付/处置引用并 move review 或 done 原子完成。接受和完成记录与状态/任务 revision 同事务，卡片内不另设冲突 status。
- [ ] 同 ID 同 payload 重试幂等，同 ID 不同 payload/任务/基线拒绝；每卡仅一个有效执行/派回授权，执行 epoch/CAS 阻止过期 Agent 和迟到通知写入。不得靠锁住整个 Agent 会话实现互斥。
- [ ] review-working-review 或 review-working-done 在一次 refresh 内完成，仍可从持久记录判断本轮结果；notify 返回前已完成、订阅重启、执行端崩溃后同 ID 恢复都可对账。
- [ ] 终端 marker 保留为投递诊断即可，输入回显不能提升业务状态；已投递未确认可明确返回该事实，不声称已开工；待确认 review 卡保留 deadline/会话引用供 E/P 探测。
- [ ] notify/resume 的恢复重试遵守不确定投递和执行授权，复用 P 的 unknown/stopped 区分与剩余预算。更换 Agent 仍需既有用户接管授权，不能遇到 unknown 就自动另起执行者。
- [ ] 保留既有 Orchestrator Wrap-Up on Behalf 的仅清理/记录例外，但 notify 非零或确认超时不能单独证明执行端已停。delivery-unknown 先按同 dispatch_id 对账；仍有效的执行者不得与代收尾并行。确认原执行端已退出或在既有授权下回收并隔离旧 epoch 后，受控操作授予 wrap-up-only epoch，绑定同一 wrap-up dispatch 和已验证集成证据，记录代办作者/原因；无有效派回记录时先建本次 wrap-up 意图。代办不修代码、不改原作者结论；仅租期超时不得重用仍可能活动的工作区。
- [ ] 所有正文/WINDOW/派回记录回写通过 S；失败回滚不得复活旧路径或覆盖新 revision；只撤销自己尚未被新执行轮消费的写入。generic working 消息与无任务组卡的兼容行为明确，旧通知未带 ID 不伪造历史接受。
- [ ] 派回 fix 关联 R 的 run/finding/作者处置原件；wrap-up 关联最终集成证据，消息引用按任务 ID 与相对 artifact/run ID 重定位，不持有会失效的绝对报告路径。
- [ ] 覆盖假 ACK、快速完成、发送前后 kill、接受/完成边界 kill、重复同 ID、错 payload、过期 epoch、确认超时、失败回滚与 move 并发；断言任务唯一且业务记录不重复，不宣称任意外部副作用 exactly-once。
- [ ] 更新 notify/resume/move/update 的参数、任务文件与规则；记录格式可由 board 纯解析供 E/O 读取，不让 board 反向依赖 notify。go test ./...、相关 -race、build/vet/格式检查通过，三语字符串齐全。

## THREAT_MODEL

保护派回归属、执行授权与新交付，处理崩溃、迟到消息、误回执及恢复竞态。不对不遵守受控写入协议的任意 Agent 副作用保证 exactly-once，不改变用户授权边界。

## OUT_OF_SCOPE

- 既有问题：派回确认丢失、回显假 ACK、不确定投递重发和过期恢复写入在范围；聊天协议/新通知平台排除。
- 加固：持久 ID、CAS/执行 epoch 与期限是正确性所需；通用消息中间件或分布式锁服务排除。
- 共享契约与文档：dispatch schema、执行接收/完成动作及工具参数归本卡；P 提供采集，E 负责事件，R 定义 finding 原件。
- 相邻功能：不实现全组编排检查点或任务自动重分配；不自动接管外部任务。

## DISCUSSION

```text
PREREQUISITES: 20260907-probe-liveness-bounds-task
```

统一方案见 S 的 plan.md。派回状态与目录状态关联但不替代：review 既可能待派回接受，也可能已完成修复，必须用 dispatch_id 区分。N 交付后仍可人工/规则读取记录，E 后续提供完整唤醒，不提前声称 E 的事件已经可用。

SELF_REVIEW: 通过。已核对用户目标、范围、接口与八卡依赖；独立卡审指出的受控写入口、batch/run 提交语义、代收尾授权三项问题已修订并复核关闭。旧版 PASS 不继承。

CARD_REVIEW: PASS — 2026-09-07，独立 Codex 子 Agent /root/integrated_card_review（未继承建卡会话上下文），首次审查及修订后增量复核，八卡均无剩余阻断。完整原文见 20260907-card-write-transactions-task 的 card-review.md；仅为契约审查，不代表实现或 PM/QA 代码审核已通过。

## RESPLIT_DECISION

用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。

本卡的大范围执行方式被替代，原目标由以下卡承接：N1 `20260907-dispatch-intent-store-task`, N2 `20260907-dispatch-atomic-receipts-task`, N3 `20260907-notify-durable-delivery-task`, N4 `20260907-wrapup-fenced-authority-task`.

完整验收映射与新的跨卡依赖见 20260907-probe-result-classification-task 的 resplit-plan.md；原验收、USER_DECISIONS及CARD_REVIEW留作历史，新卡不继承PASS。新卡全部backlog等待独立审卡；不要再start本卡或创建旧第二组分支。

## LIFECYCLE_DECISION

{"at":"2026-09-07 19:52","decision_reference":"用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。","duplicate_of":"","reason":"原大卡执行方式按用户要求拆分替代；需求完整保留，详见 20260907-probe-result-classification-task 的 resplit-plan.md"}
