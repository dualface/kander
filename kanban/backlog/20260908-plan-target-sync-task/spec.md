# Sync the plan's recorded batch target with the wrap-up binding scope after review advance

- TYPE: Bug
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 16:48
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

`review advance` 与带 `--advance-file` 的审核运行推进 batch 的运行时 target 时, plan 里记录的该 batch target 不随之更新 (`internal/board/review_advance.go:46` 只写运行时 batch; `internal/board/reviews.go:331` 附近的 `--advance-file` 路径同样只推进运行时 batch; `internal/board/review_plan.go:260` 的绑定校验不比较 target). wrap-up 派回证据的绑定范围取自 plan 最后一个 batch 记录的 target (`internal/board/dispatch_wrapup.go:235`), 并要求 `source_commit` 等于它 (`internal/board/dispatch_wrapup.go:48`, `internal/launch/dispatch_evidence.go:54`). 末批经过 fix 轮后, 证据只能证明修复前的 target 已进入 develop; 若集成改写了历史, `internal/launch/dispatch_rebase.go:27` 的 patch 比较只覆盖记录区间, 证据完全无法绑定. 本卡让两个 advance 入口都把 plan 记录同步到新 target, 使 wrap-up 绑定并验证的范围就是已关闭批次链末端的最终 target.

## USER_DECISIONS

N/A(用户只决定为审核发现的工具缺口建卡修复;实现方案未指定,候选与已选方案见 DISCUSSION)

## EXPECTED_OUTCOME

任一入口的 advance 完成后, plan 中该 batch 的记录 target、plan revision 与所有成员卡的 `reviews/plan.json` 副本在同一事务内更新; `dispatchReviewRange` 取到的范围终点等于末批 closure 的最终 target; 无历史改写时 `source_commit` 等于该最终 target, 有 `rebased_base` 时比较的是 base..最终 target 的完整 patch. 既有 plan 记录 target 与运行时 batch 不一致的旧数据: `check` 与 `move done` 给出含两个 SHA 的明确错误, `review progress` 同时输出两者, 并有受控入口把记录同步到当前 batch target, 不要求手改文件. 规则文本相应改回 "绑定 closed final target".

## ACCEPTANCE_CRITERIA

- [ ] `review advance` (`internal/review/disposition.go` 的 advance 分支到 `internal/board/review_advance.go`) 在同一事务里更新 plan 中该 batch 的 TargetCommit、plan revision 与所有成员卡的 `reviews/plan.json` 副本; 测试覆盖同步后 `review progress` 与 `check` 无错误.
- [ ] 带 `--advance-file` 的审核运行 (`internal/board/reviews.go` 中 batch target CAS 推进处) 对已 planned 的 batch 做同样的 plan 同步; 未 planned 的 batch 行为不变; 两种情况各有测试.
- [ ] `dispatchReviewRange` 返回的 Descendant 等于最后一个已关闭 batch 的 closure target; 回归测试: 末批经 fix advance 后, 无历史改写时 `source_commit` 等于 closed final target 的 wrap-up 意图通过 `PrepareBoundDispatch`, 等于旧登记 target 的意图被拒绝且错误信息指出应使用最终 target; 有 `rebased_base` 时 `verifyDispatchRebase` 比较 base..closed final target.
- [ ] 对既有 plan 记录 target 与运行时 batch target 不一致的旧数据: `validateTaskReview` (check 与 move done 共用) 报出带两个 SHA 的明确错误, `review progress` 输出 `plan_target` 与 `batch_target`; 提供受控修复入口 (`review extend-plan` 的同步请求或等价的受控命令) 把记录对齐到当前 batch target, 保留旧记录为历史; 测试覆盖.
- [ ] `go test ./internal/board ./internal/launch ./internal/review` 通过; `rules/KANDER-KANBAN-RULES.md` 的 "Review Evidence Completion Gate" 与 "Durable Dispatch", `rules/KANDER-TASK-GROUP-RULES.md` 的 "Merge-Back and Cleanup Preconditions" 改为绑定 closed final target, 删除 "另验最终 target" 与 "advance 后停止并报告" 的变通描述; `docs/durable-dispatch.md` 中关于 wrap-up 绑定范围的说明同步.

## THREAT_MODEL

N/A (证据完整性属正确性问题, 不引入新的信任边界或输入来源)

## OUT_OF_SCOPE

- 既有问题: plan 只能在全员 working/review 时创建、extend-plan 不能收编已运行的 batch, 属现有设计约束, 与本卡的 target 同步无关, 且改动它们会牵动 plan 生命周期整体, 排除.
- 加固: 两个 advance 并发竞争沿用现有 batch revision CAS 冲突报错, 因为现有 CAS 已能阻止双写, 不新增锁; 跨平台无新增要求, 排除.
- 共享契约与文档: plan JSON 若新增字段须保持旧 plan 可读, 不改既有字段名, 因为已发布的看板上存在旧 plan; 规则与 `docs/durable-dispatch.md` 的同步纳入, 其余规则不改, 部分纳入.
- 相邻功能: NON_BLOCKING 绑定、accepted 恢复、cancel 后 reclaim 分别由 `20260908-fix-evidence-nonblocking-task`、`20260908-accepted-dispatch-recovery-task`、`20260908-dispatch-binding-release-task` 处理, 本卡不改它们涉及的代码路径, 排除.

## DISCUSSION

来源: 2026-09-08 规则审核 (Codex gpt-6-astra 第二至四轮的 I1/N5/N1 项). 相关代码: `internal/board/review_advance.go:46`, `internal/board/reviews.go:331`, `internal/board/review_plan.go:260`, `internal/board/dispatch_wrapup.go:235`, `internal/board/dispatch_wrapup.go:48`, `internal/launch/dispatch_evidence.go:54`, `internal/launch/dispatch_rebase.go:27`.

已选方案: advance 时同步 plan (原候选 A). 原候选 B "plan 保持登记值, 由 validateTaskReview 在不一致时报错" 与 `dispatchReviewRange` 先调用 `validateTaskReview` (`internal/board/dispatch_wrapup.go:225`) 相冲突, 合法 advance 之后 wrap-up 反而永远取不到范围, 已删除.

旧数据: 已发布看板上可能存在登记 target 落后于 batch 的 plan; 验收第 4 条要求诊断与受控修复入口, 不做静默自动修复.

本卡与 `20260908-fix-evidence-nonblocking-task`, `20260908-accepted-dispatch-recovery-task`, `20260908-dispatch-binding-release-task` 互不依赖, 可单独执行.

- SELF_REVIEW: 通过 (修订后复查). 目标与结果一致: 两个 advance 入口都同步 plan, wrap-up 范围即最终 target; 验收逐条可由测试判定, 含旧数据诊断与修复入口; 四类边界各给理由; USER_DECISIONS 未写入方案选择, 已选方案与被删方案的理由记在 DISCUSSION. 本轮修正: 删除自相矛盾的候选 B, 补 `--advance-file` 入口, 补旧数据路径与边界理由.
- CARD_REVIEW: 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08). 发现: (1) 候选 B 与 dispatchReviewRange 先调用 validateTaskReview 矛盾; (2) GOAL 提到两个 advance 入口而验收只覆盖一个; (3) 边界缺具体理由, 旧数据恢复路径未定. 三项均已修正并重新自审.

## IMPLEMENTATION

<FILL_IN>

## SUMMARY

<FILL_IN>
