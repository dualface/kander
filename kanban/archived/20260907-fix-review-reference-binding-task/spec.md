# fix dispatch validates review originals and author disposition references

- TYPE: Feature
- SIZE: small
- TASK_GROUP: 20260907-dispatch-bindings-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 20:30
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT: duplicate

## GOAL

fix派回引用可核验的审核原件和已有原作者处置，不因卡片移动或错轮次而错派。

## USER_DECISIONS

用户要求按每卡一个可验收行为拆分剩余工作，随后要求启动并明确授权本侧独立审卡/执行。独立审卡指出N4混入独立fix证据绑定；本卡承接原N验收10的fix部分，不扩大原目标。

## EXPECTED_OUTCOME

fix派回绑定正确run/finding/batch/task，消息与既有作者处置引用跨状态移动后仍可定位。

## ACCEPTANCE_CRITERIA

- [ ] 消费N3派回和R审核类型，fix记录关联run/finding/batch及任务归属；已有原作者处置按原作者与相对artifact ID引用，不要求尚未产生的本轮处置预先存在。board不反向依赖review或notify。
- [ ] 发送前校验引用存在、归属及轮次；错误/跨批/错误任务/不完整原件明确拒绝，不篡改或补造原作者结论。卡片move后按任务ID重定位，不保存失效绝对报告路径；同dispatch重试保持原绑定。
- [ ] 覆盖合法fix、缺run/finding、错误归属、错误前驱、已有作者处置缺失、引用卡片move、同ID恢复。旧无结构报告只消费R的显式映射，不靠正文猜finding。
- [ ] 测试、必要英文发布规则和中文仓库文档随本行为交付，用户消息三语；go test ./...、相关-race、build/vet/格式通过。原生Windows与真实Agent未执行时列明缺口。PM/QA适用，CSA/Hacker在本仓库N/A。

## THREAT_MODEL

防错误证据引用与冒充原作者结论；报告作为数据，不把哈希当任意本机篡改防护。

## OUT_OF_SCOPE

不实现wrap-up专用授权、dispatch存储、传输恢复或R的finding解析/处置；这些分别由N4、N1/N2/N3、R交付。不改变用户接管权限。

## DISCUSSION

```text
PREREQUISITES: 20260907-notify-durable-delivery-task,20260907-review-disposition-gate-task
```

依赖必要性：N3提供实际fix派回入口和稳定dispatch；R提供可验证的finding/作者处置与映射，缺少它们不能正确校验。与N4共享派回绑定接线处时须由主控按实际资源协调，不能共享工作区；N4只改wrap-up路径，本卡只改fix路径。

不可拆分理由：同一个fix的身份校验与相对引用定位共同保证指向正确原件；与wrap-up授权可以独立交付，因此单列本卡。

SELF_REVIEW: 已核对单一行为、可执行验收、原N验收10覆盖和无环依赖；独立审卡待复核，保持backlog，不继承旧PASS。


CARD_REVIEW: PASS — 2026-09-07，独立Agent /root/resplit_card_review增量复核N4/N5：fix独立绑定与wrap-up专用授权已分离，原N验收10保留，依赖必要且无环；原粒度阻断关闭。其余15卡PASS不受影响。原非阻断建议保留，不阻塞后继。仅契约审查，不代表实现/PM/QA通过。原文 /home/dualf/.local/share/kander/orchestration/20260907-resplit-execution/card-review-round2.md。

当前执行计划见20260907-fix-review-reference-binding-task的execution-plan.md，由本侧主控按前置调度；用户已明确授权启动。

## IMPLEMENTATION

尚未实现。

## SUMMARY

尚未实现或审核通过。

## LIFECYCLE_DECISION

{"at":"2026-09-08 00:34","decision_reference":"用户 2026-09-08 主会话决定：对 todo 里的卡片进行整合，单一目标的卡片合并，只有能够并行的才拆分（原文见 /tmp/claude-1000/-home-dualf-works-kander/adab651a-6a90-4d29-ac0f-84202b8abad1/scratchpad/cards/decision.md，并记录于替代卡 USER_DECISIONS 与 20260908-durable-dispatch-protocol-task/consolidation-plan.md）","duplicate_of":"20260908-dispatch-evidence-binding-task","reason":"按用户 2026-09-08 整合决定并入 20260908-dispatch-evidence-binding-task；原契约、CARD_REVIEW 与覆盖矩阵保留，需求未取消"}
