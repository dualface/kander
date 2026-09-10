# Subscription reconciles on durable facts, membership changes and bounded heartbeats

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

将 subscribe 明确为当前事实的唤醒流：以任务 revision、派回记录、组成员版本补足仅比较状态目录的盲区。独立心跳与确认期限，隔离慢探测/慢输出，支持从新快照恢复而不要求历史边沿必达。

## USER_DECISIONS

本轮用户要求彻底解决已讨论的订阅、重复卡及审核闭环问题并形成任务卡。保留 CLI JSON Lines、默认 refresh 1 秒与 heartbeat 900 秒的可配置体验；字段/事件扩展为本轮设计。

## EXPECTED_OUTCOME

- 快照包含恢复所需的任务版本、派回状态和监听成员；同状态新交付、外部新增成员均能唤醒消费端。
- 存活检查、业务期限与订阅心跳有独立上界，不因其他任务活跃而无限延后。
- 阻塞、取消、事务暂态与持久结构错误有可区分的处理，重启可从事实恢复。

## ACCEPTANCE_CRITERIA

- [ ] 制定 versioned JSON Lines schema，保留 event/group_id/tasks 与 changed 原字段；增加 subscription_id、进程内 seq、observed_at、task_revisions、dispatch 摘要及 watched/group membership revision。进程内序号明确不是跨重启可重放游标。
- [ ] 初始 snapshot 读取已提交事实，后续保留 state-change，增加 task-update/membership-change 等必要事件；所有事件携带当前完整监听事实或明确可重取的版本。目录状态不变但 revision/派回接受完成/交付变化必须可见。
- [ ] 与 N 配合验证 review-working-review 在一轮内完成和订阅重启：下一快照仍有同 dispatch_id 的 accepted/completed 证据；不靠缩短 refresh、仅看 mtime 或每次状态边沿保证正确性。
- [ ] 心跳/存活 due time 独立于任何状态事件；pending review 卡也按确认期限触发注意事件和必要探测。alive 与无进展分离，E 输出运行状态/观测年龄，由 O 决定处理；无在执行或待确认任务时仍有纯心跳。
- [ ] 默认 refresh=1、heartbeat=900；interval 校验有限、正数且可表示为有效 time.Duration，极小值/巨大值明确拒绝或规范到文档界限，不能生成零/负 timer 忙循环。确认期限重启不重置。
- [ ] 保存 --watch 的原始组引用并按成员版本重新展开；新增外部修复卡加入监听并产生 membership-change。移除/归属变化不能静默视为依赖已满足；目标重复和不可确定成员集合明确报告。
- [ ] 组展开复用 board 的共用成员解析与完整性结果，不忽略 ReadDocument/Scan.Problems；将不可定位归属的错误表达为 membership-unknown 并停止依赖放行，不静默删掉成员；不对无关已知问题盲目扩大探测。
- [ ] 复用 S 的协调读取：已知未完成事务给出 recoverable/maintenance 信息，必要时有界等待后明确结束；真实重复/reparse/持久结构破坏仍失败关闭，读者不自动修复、不选择一个副本继续。
- [ ] 慢探测在独立有界调度中运行，复用 P 的 context/预算/并发限制，不能堵住状态扫描；liveness 标明观测时间与过期，不把旧任务版本的结果当当前结果。
- [ ] stdout 慢消费者有有限缓冲/写出期限与明确退出策略，禁止无限队列和不可退出的后台 Writer；Ctrl+C/平台终止、调用方 context、输出失败均释放 goroutine/探测/管道。全体 done 是否退出保留现有由消费者决定的契约。
- [ ] 覆盖连续状态变化下仍按期心跳、review 待确认超时、慢探测+状态变化、阻塞 stdout+取消、组新增/损坏成员、期间迁移/归档、interval 溢出、进程重启新快照及快速往返。
- [ ] 更新命令协议、版本兼容和任务组事件处理说明；CLI 默认字段向后兼容的范围明确，旧消费者不能被静默承诺理解新业务事件。go test ./...、并发 -race、build/vet/格式检查通过，平台差异如实记录。

## THREAT_MODEL

保护监听完整性、及时性与资源边界，处理不完整磁盘事实、错误进程输出和消费者停读。不把订阅当任务授权来源，不承诺任意底层文件系统 I/O 可强制取消。

## OUT_OF_SCOPE

- 既有问题：状态采样遗漏业务变化、心跳饥饿、静态 watch、静默省略成员、输出阻塞和 interval 溢出均纳入。
- 加固：独立期限、有界并发/输出、版本化事实为闭环必需；数据库/永久事件总线、全量历史重放排除。
- 共享契约与文档：事件 schema、监听集合、计时/退出契约归本卡；派回真相归 N，进程分类归 P，存储事务归 S，最终消费状态机归 O。
- 相邻功能：不修改卡片内容、不直接恢复/接管 Agent、不自动满足或放行依赖，不重写 TUI 的刷新架构。

## DISCUSSION

```text
PREREQUISITES: 20260907-durable-dispatch-task
```

统一方案见 S 的 plan.md。本卡不要求每次物理 rename 都产生可持久投递的事件，正确性来自 N 的持久事实；事件仅唤醒。成员变更必须与依赖解析使用同一版本语义，避免“check 等 B 但 subscribe 只看 A”。

SELF_REVIEW: 通过。已核对用户目标、范围、接口与八卡依赖；独立卡审指出的受控写入口、batch/run 提交语义、代收尾授权三项问题已修订并复核关闭。旧版 PASS 不继承。

CARD_REVIEW: PASS — 2026-09-07，独立 Codex 子 Agent /root/integrated_card_review（未继承建卡会话上下文），首次审查及修订后增量复核，八卡均无剩余阻断。完整原文见 20260907-card-write-transactions-task 的 card-review.md；仅为契约审查，不代表实现或 PM/QA 代码审核已通过。

## RESPLIT_DECISION

用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。

本卡的大范围执行方式被替代，原目标由以下卡承接：E1 `20260907-subscription-heartbeat-clock-task`, E2 `20260907-subscription-revision-snapshot-task`, E3 `20260907-subscription-watch-membership-task`, E4 `20260907-subscription-probe-isolation-task`, E5 `20260907-subscription-output-exit-task`, E6 `20260907-subscription-dispatch-facts-task`.

完整验收映射与新的跨卡依赖见 20260907-probe-result-classification-task 的 resplit-plan.md；原验收、USER_DECISIONS及CARD_REVIEW留作历史，新卡不继承PASS。新卡全部backlog等待独立审卡；不要再start本卡或创建旧第二组分支。

## LIFECYCLE_DECISION

{"at":"2026-09-07 19:52","decision_reference":"用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。","duplicate_of":"","reason":"原大卡执行方式按用户要求拆分替代；需求完整保留，详见 20260907-probe-result-classification-task 的 resplit-plan.md"}
