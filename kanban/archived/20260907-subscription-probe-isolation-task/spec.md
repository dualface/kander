# Slow probes do not block subscription state scans

- TYPE: Bug
- SIZE: small
- TASK_GROUP: 20260907-subscription-io-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 19:49
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT: duplicate

## GOAL

存活采集使用独立有界调度，慢命令期间状态扫描和取消仍及时执行。

## USER_DECISIONS

用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。


后续实际决定：用户要求“拆好就开整吧”并答复“我授权”，授权本侧独立审卡、启动及按计划完成组交付/审核/develop集成与收尾；取代上方建卡阶段不启动限制。原审核归档组仍原主线程负责。

## EXPECTED_OUTCOME

本卡交付：存活采集使用独立有界调度，慢命令期间状态扫描和取消仍及时执行。

## ACCEPTANCE_CRITERIA

- [ ] 消费P3总预算/并发限制，扫描与探测解耦，队列有界；心跳输出观测时间/年龄/运行状态，旧revision或旧会话结果标过期或丢弃。
- [ ] 慢探测期间状态更新仍可见，取消回收所有调度任务；不增加无限后台goroutine或承诺不可取消文件系统I/O。
- [ ] 反转 TestAuditProbeBlocksScanAndCancellation；持续状态变化且另一Agent死亡时仍按期获得新观测，真实终端冒烟与假命令结果分列。
- [ ] 本行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包-race、build/vet/格式检查通过。平台相关测试使用临时目录，原生Windows及真实tmux/herdr/Agent未执行时列明缺口，不把假CLI或交叉编译记为实机通过。PM/QA按适用门禁，CSA/Hacker在本仓库N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

subscribe存活调度；不实现派回deadline、输出writer或恢复决策。

不引入通用服务、签名、保留期清理或无关优化。发现超出此单一行为的需求必须重新评估并另卡，不能为避免拆卡扩大本契约。

## DISCUSSION

```text
PREREQUISITES: 20260907-probe-batch-budget-task,20260907-subscription-revision-snapshot-task
```

依赖必要性与不可拆分理由：P3提供可回收批量采集，E2提供版本绑定；缺一不能保证及时且不误用旧观测。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本行为交付的一部分，不单独拆成尾部测试任务。

资源边界：subscribe存活调度；不实现派回deadline、输出writer或恢复决策。 同组卡修改共同运行入口时串行；跨组只在资源可隔离且真实依赖满足时并行。无冲突的已就绪卡不为前卡非阻断建议等待；审核等待须注明具体门禁。

历史来源：20260907-subscription-reconcile-task；完整覆盖及主线程交接见 20260907-probe-result-classification-task 的 resplit-plan.md。原卡CARD_REVIEW不继承。

SELF_REVIEW: 通过粒度及契约自检：一个行为、可执行验收、必要依赖和范围排除已逐项核对；组内外依赖与全量映射另见计划。独立审卡现已通过，见下方真实CARD_REVIEW；由本侧主控按实际前置pick/start。


CARD_REVIEW: PASS — 2026-09-07，独立Agent /root/resplit_card_review（fork_turns=none），已只读完整契约、原需求、覆盖矩阵及粒度/必要依赖；本卡无阻断，旧PASS未继承。非阻断：资源依赖和技术依赖区分、OUT_OF_SCOPE正向范围措辞清晰、E5明确writer取消能力。仅契约通过，不代表实现/PM/QA通过。

当前执行计划：20260907-fix-review-reference-binding-task 的 execution-plan.md，独立审卡要求N4/N5拆分后总17卡/6组；本卡契约未因此扩大。原16卡计划是历史。

## IMPLEMENTATION

尚未实现。

## SUMMARY

尚未实现；本轮只重拆任务，不代表修复或审核完成。

## LIFECYCLE_DECISION

{"at":"2026-09-08 00:34","decision_reference":"用户 2026-09-08 主会话决定：对 todo 里的卡片进行整合，单一目标的卡片合并，只有能够并行的才拆分（原文见 /tmp/claude-1000/-home-dualf-works-kander/adab651a-6a90-4d29-ac0f-84202b8abad1/scratchpad/cards/decision.md，并记录于替代卡 USER_DECISIONS 与 20260908-durable-dispatch-protocol-task/consolidation-plan.md）","duplicate_of":"20260908-subscription-bounded-runtime-task","reason":"按用户 2026-09-08 整合决定并入 20260908-subscription-bounded-runtime-task；原契约、CARD_REVIEW 与覆盖矩阵保留，需求未取消"}
