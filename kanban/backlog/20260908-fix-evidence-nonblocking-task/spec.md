# fix dispatch evidence supports disposition-only binding for NON_BLOCKING items

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

`--kind fix` 的派回证据要求 `evidence.fix` 的 finding 引用非空, 且只在报告的 `FINDINGS` 数组中查找 (`internal/board/dispatch_evidence.go:92`, `internal/board/dispatch_evidence.go:169`), `NON_BLOCKING` 项无法绑定; 作者原件的递归校验在 `internal/board/dispatch_evidence.go:122` 与 `:138`. 而 closure 要求每个已分配项 (含非阻塞项) 都有作者 disposition (`internal/board/review_disposition.go:435`). 只有 low/recommend/suggest 的卡目前无法收到 bound fix 派回, 规则里只能用 `--kind sync` 做 disposition-only 派发变通. 本卡让 fix 证据接受 NON_BLOCKING 引用, 且不改变 "非阻塞项不开 fix 轮" 的语义.

## USER_DECISIONS

N/A(用户只决定为审核发现的工具缺口建卡修复;实现方案未指定,候选与已选方案见 DISCUSSION)

## EXPECTED_OUTCOME

fix 派回可以只引用 NON_BLOCKING 项, 也可以与 FINDINGS 混合引用; 两类引用都要求已由 `review assign` 分配给该任务, 已存在的作者原件 (含增量轮次带 lineage 的项) 必须被引用, 不存在时不要求预先生成; 非阻塞项不使 round 成为需要 reviewer 重跑的 fix 轮; disposition-only 轮次以不变的 delivery SHA 完成; 作者提交 disposition 后该 batch 可以 `review close`. 规则与协议文档删除 `--kind sync` 变通, 改为 fix 派回直接绑定非阻塞项.

## ACCEPTANCE_CRITERIA

- [ ] `evidence.fix` 的 finding 引用在 `FINDINGS` 与 `NON_BLOCKING` 两个数组中查找; 四种情况各有测试并通过校验: 仅 NON_BLOCKING 引用, FINDINGS 与 NON_BLOCKING 混合引用, 被引用项尚无作者原件 (不要求预先生成), 被引用项带 lineage 且前驱轮已有作者原件 (必须引用全部既有原件, 缺一拒绝).
- [ ] 被引用的 NON_BLOCKING 项必须已由 `review assign` 分配给该任务; 未分配或不存在的引用被拒绝, 错误信息区分两种情况.
- [ ] disposition-only 轮次: 作者以 `move review --dispatch-id ... --execution-epoch ... --delivery-commit <与上一轮相同的 SHA>` 完成时回执正常, 且该轮不要求 `review advance` 或新的 reviewer run; 作者提交全部非阻塞 disposition 后 `review close` 对该 batch 成功; 端到端测试覆盖.
- [ ] dispatch 原件中能看出该轮引用了哪些非阻塞项 (`dispatch show` 可读, 与 FINDINGS 引用可区分).
- [ ] `go test ./internal/board ./internal/launch` 通过; `rules/KANDER-REVIEW-RULES.md` "Preconditions and Execution" 与 `rules/KANDER-TASK-GROUP-RULES.md` "Dispatching Findings Back" 删除 `--kind sync` 变通, 改为 fix 派回绑定非阻塞项; `docs/durable-dispatch.md` 中 "finding 只能属于 blocking/high/medium" 的说明改为两类数组均可引用, 并写明 "没有既有作者处置时不要求预先生成".

## THREAT_MODEL

N/A (只放宽引用范围, 引用仍需指向已分配、已发布的原件)

## OUT_OF_SCOPE

- 既有问题: disposition 的状态校验与 closure 的覆盖检查逻辑不变, 因为它们已正确要求非阻塞项的 disposition, 本卡只是补上派回入口, 排除.
- 加固: 不改 dispatch 证据 JSON 的整体结构, 不新增并发保护, 因为现有 CAS 与原件校验已覆盖, 排除.
- 共享契约与文档: `dispatch prepare` 意图字段不新增, 因为现有 `references` 结构已能承载两类引用; 规则两节与 `docs/durable-dispatch.md` 的同步纳入, 部分纳入.
- 相邻功能: plan target 同步、accepted 恢复、cancel 后 reclaim 由 `20260908-plan-target-sync-task`、`20260908-accepted-dispatch-recovery-task`、`20260908-dispatch-binding-release-task` 处理, 本卡不改它们涉及的代码路径, 排除.

## DISCUSSION

来源: 2026-09-08 规则审核 (Codex 第三、四轮 N2/I2 项). 相关代码: `internal/board/dispatch_evidence.go:92` (引用非空), `:122` 与 `:138` (既有作者原件递归校验), `:169` (只在 FINDINGS 查找), `internal/board/review_disposition.go:435` (closure 覆盖检查), `internal/board/dispatch_receipt.go:73` (完成回执不要求 delivery 变化).

实现提示: 引用查找改为遍历 `findings.all()`; 是否需要 reviewer 重跑由项的 tier 决定 (规则层面非阻塞项永不构成 fix 轮), 证据结构可加只读标记区分两类引用.

本卡与 `20260908-plan-target-sync-task`, `20260908-accepted-dispatch-recovery-task`, `20260908-dispatch-binding-release-task` 互不依赖, 可单独执行.

- SELF_REVIEW: 通过 (修订后复查). 实际核对过: 引用非空检查 (:92), 作者原件递归 (:122, :138), FINDINGS 查找 (:169), assignment 归属 (`review_disposition.go:199`), closure 覆盖 (:435), 完成回执 (`dispatch_receipt.go:73`). 验收覆盖纯非阻塞、混合、无既有原件、带 lineage 四种引用与闭批完成; 边界四类各给理由; 未写入用户未确认的决定.
- CARD_REVIEW: 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08). 发现: (1) 验收未覆盖混合引用、既有作者原件与 "不新增 fix 轮"; (2) 漏 `docs/durable-dispatch.md` 协议说明; (3) 边界缺理由, 自审为模板. 三项均已修正并重新自审.

## IMPLEMENTATION

<FILL_IN>

## SUMMARY

<FILL_IN>
