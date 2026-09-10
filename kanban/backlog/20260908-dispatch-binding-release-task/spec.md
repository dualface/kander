# Release the card binding after dispatch fail/cancel and stop deriving the execution-cycle id from minute precision

- TYPE: Bug
- SIZE: large
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

`dispatch fail|cancel` 只更新 dispatch 状态, 不清除卡片上的 `DISPATCH_ID`/`EXECUTION_EPOCH` 活动绑定 (`internal/board/dispatch.go` 的 EndDispatch); 之后任何带 `--owner` 的 move 仍被 `stageDispatchMove` 拒绝 (`internal/board/dispatch_receipt.go:47`), 因此 `move working --owner` 的 reclaim 只对从未绑定过的卡可用. 编排检查点同样消费这些事实: `internal/board/coordinator_reconcile.go:61` 在卡已无绑定而 checkpoint 仍记录旧 dispatch 时直接拒绝, `internal/board/coordinator_cycle.go:19` 拒绝已启动成员的执行周期变化. 另外执行周期只散列 task ID 与分钟精度的 `STARTED_AT` (`internal/board/review_plan.go:52`, `internal/board/board.go:123`), 同一分钟内重新 claim 不会改变周期, `review progress` 不会报 `requirements-needed`. 本卡让已终止的 dispatch 解除卡片的活动绑定, 让执行周期标识在每次 claim 时唯一, 并让 review plan 与编排检查点都能消费这两种变化.

## USER_DECISIONS

N/A(用户只决定为审核发现的工具缺口建卡修复;实现方案未指定,候选与已选方案见 DISCUSSION)

## EXPECTED_OUTCOME

`dispatch fail|cancel` 成功后卡片不再持有活动执行授权, `move <id> working --owner <agent>` 可在 review/ 卡上完成 reclaim; 活动绑定仍拒绝 `--owner`; dispatch 原件与回执保留, 旧 epoch 仍不能写入. 每次 claim (手工领取、`start`、失败回滚后的重新启动) 产生不同的执行周期标识, `resume --agent` 接管保持周期; `review progress` 在 reclaim 后可靠报告 `requirements-needed`, `coordinator reconcile` 能凭 failed/cancelled 原件与用户决策引用接受解除绑定与合法的周期交接, 既有 checkpoint 继续对账.

## ACCEPTANCE_CRITERIA

- [ ] `dispatch fail|cancel` 在同一事务里清除或归档卡片的 `DISPATCH_ID`/`EXECUTION_EPOCH` 活动绑定; dispatch 原件与回执保留, `dispatch show` 仍能查历史; 测试覆盖. 升级前已处于 failed/cancelled 但卡片仍带绑定的旧记录: 对已终止的 dispatch 再次执行 `dispatch fail|cancel ... --decision <reference>` 只做解除 (写入一条带决策引用的解除记录, 不生成第二份终止原件, 不覆盖历史), 当该 dispatch 不是终止状态、或卡片当前绑定属于另一个更新的 dispatch 时拒绝; 三种情况各有测试.
- [ ] 解除绑定后 `move <id> working --owner <agent>` 对 review/ 卡成功并写入新 OWNER 与 STARTED_AT; 绑定仍活动时继续被拒; 解除后旧 epoch 的 `move --dispatch-id --execution-epoch`、`update` 正文/WINDOW 写入与 disposition 引用仍被拒; 测试覆盖每一种.
- [ ] `coordinator reconcile` 对 checkpoint 仍记录旧 dispatch 而卡片已解除绑定的成员, 凭该 dispatch 的 failed/cancelled 原件接受解除并记入检查点 (`internal/board/coordinator_reconcile.go:61` 的 unbound 分支不再直接拒绝); 无对应终止原件时仍拒绝; 测试覆盖两种情况.
- [ ] 执行周期标识加入每次 claim 唯一的成分 (例如由 move working 事务递增的 claim 序号或纳秒时间戳); 覆盖全部 claim 入口并分别验收: 手工领取 (`internal/board/update.go:34` 附近的 OWNER/STARTED_AT 写入), `start` 的元数据写入 (`internal/launch/metadata.go:46`), start 失败回滚到 todo 后再次启动得到新周期, `resume --agent` 接管保持周期; 没有该成分的旧卡按现有 task ID + STARTED_AT 规则兼容.
- [ ] 决策证据由公开命令产生, 不靠测试预填: `dispatch fail|cancel` 增加可选 `--decision <user-decision-reference>`, 与 reason 一起写入该 dispatch 的不可变终止原件; reclaim 用 `move <id> working --owner <agent> --decision <user-decision-reference>` (仅当卡片存在已解除的绑定时接受 `--decision`, 其他 working move 仍拒绝该参数, 保持 `internal/board/update.go:59` 现有限制), 命令在同一事务写入一条 `LIFECYCLE_DECISION` 记录, 绑定旧周期、新周期、被解除的 dispatch ID 与决策引用; `coordinator reconcile` 只消费这条记录; 测试覆盖两条命令各自的成功与拒绝.
- [ ] `coordinator reconcile` 对合法 reclaim 的周期变化 (卡片有已解除的绑定与上述 `LIFECYCLE_DECISION` 交接记录) 接受交接并更新成员周期 (`internal/board/coordinator_cycle.go:19` 不再以 "changed without first-start facts" 拒绝); 无交接证据时仍拒绝; 测试覆盖. 对账先经过 `coordinatorStartCycle` (`internal/board/coordinator_start.go:56` 现在因旧 succeeded 启动原件的周期不同而报 "confirmed start cycle changed"), 该消费方同样凭交接记录接受周期变化并保留旧启动原件; 端到端验收顺序: `start` 成功 (有 succeeded 原件) -> dispatch 终止并解除 -> `move working --owner --decision` reclaim -> `review progress` 报 requirements-needed 并 extend-plan rebind -> 原 checkpoint 上 `coordinator reconcile` 接受交接; 手工领取 (无启动原件) 的路径另有一条验收.
- [ ] reclaim 后 `review progress` 报告 `requirements-needed` 与完整 `rebind_cycles`, extend-plan 重绑定的现有测试仍通过; review plan 与 coordinator 在同一 reclaim 场景下都能恢复的端到端测试.
- [ ] `go test ./internal/board` 通过; `rules/KANDER-KANBAN-RULES.md` 的 "Claiming, Starting, and Coordination"、"Review Evidence Completion Gate" 与 "Durable Dispatch" 同步: reclaim 改为 "显式终止并解除绑定后允许, 活动绑定仍拒绝", 删除 "分钟精度, 以 progress 为准" 的描述; `docs/durable-dispatch.md` 的终止与解除说明同步; `docs/coordinator-recovery.md` (第 72 行附近 "成功确认后拒绝周期替换" 一节) 增加合法交接与解除绑定的恢复契约: 交接原件、checkpoint 更新、旧 checkpoint 兼容与拒绝条件.

## THREAT_MODEL

资产: 执行授权唯一性. 要求: 解除绑定不能让旧 epoch 的执行者继续写入或完成该卡 (旧 epoch 的接受、正文/WINDOW 更新、disposition、完成均被拒); 解除只在 dispatch 已处于 failed/cancelled 且由用户显式处置后发生; 周期交接需要可核对的用户决策引用, 编排检查点不凭状态字符串推断.

## OUT_OF_SCOPE

- 既有问题: `resume --agent` 接管保持 STARTED_AT 与周期的策略不变, 因为接管是同一执行的延续, 排除.
- 加固: 无新增并发保护要求, 解除与 claim 沿用现有事务与 CAS, 排除.
- 共享契约与文档: 卡片元数据字段名不变, 新增成分放在现有 metadata 或 dispatch 记录中且旧卡可读, 因为已发布看板上存在旧卡; 规则三节、`docs/durable-dispatch.md` 与 `docs/coordinator-recovery.md` 的同步纳入, 部分纳入.
- 相邻功能: accepted 后的 epoch 轮换、plan target 同步、NON_BLOCKING 绑定由 `20260908-accepted-dispatch-recovery-task`、`20260908-plan-target-sync-task`、`20260908-fix-evidence-nonblocking-task` 处理, 本卡不改它们涉及的代码路径, 排除.

## DISCUSSION

来源: 2026-09-08 规则审核 (Codex 第二轮 N4/N15 项, 卡片审核补充 coordinator 消费链). 相关代码: `internal/board/dispatch.go` EndDispatch, `internal/board/dispatch_receipt.go:47`, `internal/board/snapshot.go:134`, `internal/board/coordinator_reconcile.go:61`, `internal/board/coordinator_cycle.go:19`, `internal/board/review_plan.go:52`, `internal/board/board.go:123`, `internal/board/review_gate.go:82`, `internal/board/update.go:34`, `internal/launch/metadata.go:46`.

SIZE 定为 large: 涉及 dispatch 终止事务、卡片元数据、review plan 与 coordinator 三方对同一变化的消费, 执行者启动时先写 `plan.md`, 建议阶段: (1) 终止事务解除绑定与旧 epoch 拒绝; (2) 周期成分与各 claim 入口; (3) review progress/extend-plan 适配; (4) coordinator reconcile 的解除与交接证据; (5) 规则与文档; 每阶段附旧卡与旧 checkpoint 的兼容验证.

实现提示: 解除绑定可以把 `DISPATCH_ID`/`EXECUTION_EPOCH` 移入归档字段而不是删除, 便于 `dispatch show` 继续查历史; 周期成分若用 claim 序号, 可由 move working 事务递增并写入 metadata, start 的元数据写入复用同一函数.

本卡与 `20260908-plan-target-sync-task`, `20260908-fix-evidence-nonblocking-task`, `20260908-accepted-dispatch-recovery-task` 互不依赖, 可单独执行.

- SELF_REVIEW: 通过 (修订后复查). 目标补入 coordinator 的两处消费; 验收列清全部 claim 入口与接管保持规则, coordinator 的解除与交接各有接受与拒绝两条路径; 规则改法改为 "显式终止并解除后允许, 活动绑定仍拒绝"; 边界四类各给理由; SIZE 改为 large 并写明计划阶段.
- CARD_REVIEW: 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08). 发现: (1) 解除绑定会被 coordinator reconcile 拒绝; (2) 周期变化被 coordinator_cycle 拒绝; (3) claim 入口未列清; (4) 漏 Durable Dispatch 与协议文档, "删除只有未绑定卡能 reclaim" 过宽. 四项均已修正并重新自审.
- CARD_REVIEW (第三轮): 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 第二轮复审). 发现: (1) 交接证据要求 `LIFECYCLE_DECISION` 却没有生产入口 (`dispatch_cmd.go:61` 只收 reason, `update.go:59` 只在 archived/trash 写决策), 已定义 `dispatch fail|cancel --decision` 与 `move working --owner --decision` 两个公开入口及其记录; (2) 漏 `docs/coordinator-recovery.md` 的周期替换契约, 已纳入. 两项已修正.
- CARD_REVIEW (第四轮): 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 第三轮复审). 发现: (1) 漏了 `coordinatorStartCycle` 这个先于 `coordinator_cycle.go` 的消费方, 有成功启动原件时会提前拒绝, 已纳入并补端到端顺序; (2) 升级前已 failed/cancelled 的旧绑定没有解除路径 (EndDispatch 拒绝再次终止), 已定义复用终止命令的只解除路径与三种拒绝/接受用例. 两项已修正.

## IMPLEMENTATION

<FILL_IN>

## SUMMARY

<FILL_IN>
