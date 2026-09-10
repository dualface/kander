# Proxy wrap-up must hold a fenced dedicated authority

- TYPE: Feature
- SIZE: small
- TASK_GROUP: 20260907-dispatch-bindings-group
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

保留原编排代收尾例外，同时阻止仍活动的执行端与代办并行写入。

## USER_DECISIONS

用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。

## EXPECTED_OUTCOME

本卡交付：保留原编排代收尾例外，同时阻止仍活动的执行端与代办并行写入。

## ACCEPTANCE_CRITERIA

- [ ] wrap-up绑定已验证集成证据与同dispatch；引用相对ID，不缓存跨状态绝对报告路径。fix审核证据绑定由20260907-fix-review-reference-binding-task独立承接。
- [ ] delivery-unknown先对账；仅确认原执行者退出或在既有授权下合法回收并隔离旧epoch后，授予wrap-up-only epoch；无派回记录先建意图，记录作者/原因。
- [ ] 普通非零、确认超时、租期到期不能独自证明退出；无SESSION、未知投递、仍活动、确认退出四类验收。只允许清理及记录，拒绝代码修改和冒充原作者结论，不改变用户接管权限。
- [ ] 本行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包-race、build/vet/格式检查通过。平台相关测试使用临时目录，原生Windows及真实tmux/herdr/Agent未执行时列明缺口，不把假CLI或交叉编译记为实机通过。PM/QA按适用门禁，CSA/Hacker在本仓库N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

专用授权及证据绑定；不自动执行Git集成或删除真实用户工作区。

不引入通用服务、签名、保留期清理或无关优化。发现超出此单一行为的需求必须重新评估并另卡，不能为避免拆卡扩大本契约。

## DISCUSSION

```text
PREREQUISITES: 20260907-notify-durable-delivery-task,20260907-review-disposition-gate-task
```

依赖必要性与不可拆分理由：N3给出不确定投递和授权链，R给出合法审核/处置证据；这才是R的真实消费点，不应阻塞前面的探测和订阅。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本行为交付的一部分，不单独拆成尾部测试任务。

资源边界：专用授权及证据绑定；不自动执行Git集成或删除真实用户工作区。 同组卡修改共同运行入口时串行；跨组只在资源可隔离且真实依赖满足时并行。无冲突的已就绪卡不为前卡非阻断建议等待；审核等待须注明具体门禁。

历史来源：20260907-durable-dispatch-task；完整覆盖及主线程交接见 20260907-probe-result-classification-task 的 resplit-plan.md。原卡CARD_REVIEW不继承。

SELF_REVIEW: 通过粒度及契约自检：一个行为、可执行验收、必要依赖和范围排除已逐项核对；组内外依赖与全量映射另见计划。独立审卡尚未执行，保持backlog；不得仅添加PASS文字或直接启动。

独立审卡修订：N4首项中可独立交付的fix绑定已迁至20260907-fix-review-reference-binding-task；原N验收10的fix部分由该卡承接，wrap-up部分仍归N4。未删除需求。


CARD_REVIEW: PASS — 2026-09-07，独立Agent /root/resplit_card_review增量复核N4/N5：fix独立绑定与wrap-up专用授权已分离，原N验收10保留，依赖必要且无环；原粒度阻断关闭。其余15卡PASS不受影响。原非阻断建议保留，不阻塞后继。仅契约审查，不代表实现/PM/QA通过。原文 /home/dualf/.local/share/kander/orchestration/20260907-resplit-execution/card-review-round2.md。

当前执行计划见20260907-fix-review-reference-binding-task的execution-plan.md，由本侧主控按前置调度；用户已明确授权启动。

## IMPLEMENTATION

尚未实现。

## SUMMARY

尚未实现；本轮只重拆任务，不代表修复或审核完成。

## LIFECYCLE_DECISION

{"at":"2026-09-08 00:34","decision_reference":"用户 2026-09-08 主会话决定：对 todo 里的卡片进行整合，单一目标的卡片合并，只有能够并行的才拆分（原文见 /tmp/claude-1000/-home-dualf-works-kander/adab651a-6a90-4d29-ac0f-84202b8abad1/scratchpad/cards/decision.md，并记录于替代卡 USER_DECISIONS 与 20260908-durable-dispatch-protocol-task/consolidation-plan.md）","duplicate_of":"20260908-dispatch-evidence-binding-task","reason":"按用户 2026-09-08 整合决定并入 20260908-dispatch-evidence-binding-task；原契约、CARD_REVIEW 与覆盖矩阵保留，需求未取消"}
