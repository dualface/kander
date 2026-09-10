# Accepted dispatches recover by rotating the epoch after the executor stops

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

派回被执行者 accepted 后, 同 ID `notify` 只返回回执, 不探测、不投递、不恢复 (`internal/notify/dispatch.go:80`); `resume --agent` 的 epoch 轮换在原 `confirm_by` (默认 120 秒) 过后被拒, 即使该轮早已 accepted (`internal/board/dispatch.go:364`); 不带 `--agent` 的同 ID resume 同样直接返回 (`internal/launch/dispatch.go:165`). 此外原始期限 `Input.ConfirmBy` 还被 `internal/board/dispatch.go:314` 的接受路径、`internal/launch/dispatch.go:169` 与 `:277` 的投递上下文、`internal/notify/dispatch.go:86` 的投递期限以及订阅的 `confirm_by`/`confirmation_overdue` 直接使用, 单改轮换事务会出现 "轮换成功、投递立即超时". 执行者接受后退出时, 目前只能由用户 `dispatch fail` 处置旧轮, 再建新 ID 并重新生成证据. 本卡让已 accepted 且观测到执行者 stopped 的轮次可以在用户授权下轮换 epoch 恢复, 新 epoch 有独立的接受期限并被全部消费方一致使用, 原期限与回执作为历史保留. 另外 `kander notify --help` 与 `kander resume --help` 的 usage 行漏列 `--evidence-file` (`internal/launch/dispatch.go:231` 实际解析), 一并补齐.

## USER_DECISIONS

N/A(用户只决定为审核发现的工具缺口建卡修复;实现方案未指定,候选与已选方案见 DISCUSSION)

## EXPECTED_OUTCOME

accepted 轮次的执行者被证明 stopped 后, `resume --agent <name> --dispatch-id <id>` 为同一 ID 发放新 epoch 并投递原始 payload, 新 epoch 有自己的 120 秒接受期限; 接受、投递、恢复验证与订阅事件全部按当前 epoch 的期限计算; 旧 epoch 的 accepted 回执保留, 旧 epoch 的接受、正文/WINDOW 更新、disposition 与完成全部被拒; 同 ID `notify` 在 stopped 观测有效时同样进入恢复投递, alive、unknown、过期观测或身份变化时仍只返回回执或拒绝. help 文本列出 `--evidence-file`.

## ACCEPTANCE_CRITERIA

- [ ] `internal/board/dispatch.go` 的 epoch 轮换事务对 state=accepted 且附带有效、新鲜、身份匹配的 stopped 观测 (与 `dispatch authorize-wrap-up` 同标准, 复用 `internal/launch/dispatch_evidence.go:224` 附近的校验) 的轮次不再因原 `confirm_by` 过期而拒绝; 测试覆盖: accepted 后 10 分钟且 stopped 时轮换成功; alive、unknown、观测过期 (超过 30 秒)、SESSION/WINDOW/OWNER 与卡不一致时拒绝.
- [ ] 所有直接读取 `Input.ConfirmBy` 的消费方改读当前 epoch 的有效期限: `internal/board/dispatch.go:314` 的接受路径, `internal/launch/dispatch.go:169` 与 `:277` 的投递上下文与剩余超时, `internal/notify/dispatch.go:86` 的投递期限, 订阅事件的 `confirm_by`/`confirmation_overdue`/`dispatch-attention`; 新增回归: 原期限已过 → 轮换 → 投递 → `move working --dispatch-id --execution-epoch <new>` 接受 → 完成, 全链路成功.
- [ ] 新 epoch 的期限持久保存在该 epoch 的 execution 记录中; 原始 intent 的 `created_at` 与 `Input.ConfirmBy` 不被改写, `dispatch show` 同时显示原始期限与当前 epoch 期限.
- [ ] 旧 epoch 的 `move working|review|done --dispatch-id --execution-epoch <old>`、带旧 epoch 的 `update` 正文/WINDOW 写入、`review disposition` 的 `authorization` 引用旧 epoch, 全部被拒绝; 新 epoch 完成后回执正常; 测试覆盖每一种拒绝.
- [ ] 同 ID `notify` 在 accepted 且 stopped 观测有效时进入恢复投递并沿用同一 epoch 期限; alive 或 unknown 时仍只返回回执; 测试覆盖两种分支.
- [ ] `kander notify --help` 与 `kander resume --help` 的 usage 行包含 `--evidence-file <JSON>`.
- [ ] `go test ./internal/board ./internal/notify ./internal/launch` 通过; `rules/KANDER-KANBAN-RULES.md` 的 "Durable Dispatch"、"Failure Recovery" 与 "Subscription Events" 中关于 `confirm_by` 的说明同步更新, 删除 "accepted 后只能 dispatch fail 再建新 ID" 的描述; `docs/durable-dispatch.md` (接管轮换一节) 与 `docs/subscription-facts.md` (`confirm_by` 为当前 epoch 期限一节) 同步.

## THREAT_MODEL

资产: 派回轮次的唯一执行授权. 可信主体: 同一用户下的编排者与执行者. 攻击者能力: 同用户误操作让两个执行者同时持有同一轮次. 要求: 轮换必须使旧 epoch 立即失效 (旧 epoch 的接受、正文/WINDOW 更新、disposition、完成均被拒), 且只在新鲜、身份匹配的 stopped 观测下发生, alive、unknown 或过期观测不触发.

## OUT_OF_SCOPE

- 既有问题: tmux/herdr 存活探测的准确性与 stopped 观测的定义不变, 因为本卡只消费现有观测, 排除.
- 加固: 不引入跨机器锁; 两个编排者并发轮换沿用 dispatch revision CAS 冲突报错, 因为现有 CAS 已阻止双写, 排除.
- 共享契约与文档: dispatch JSON 新增字段须向后兼容旧记录, 因为已发布看板上存在旧 dispatch; 规则三节与两份协议文档的同步纳入, 部分纳入.
- 相邻功能: cancel 后 reclaim、plan target 同步、NON_BLOCKING 绑定由 `20260908-dispatch-binding-release-task`、`20260908-plan-target-sync-task`、`20260908-fix-evidence-nonblocking-task` 处理, 本卡不改它们涉及的代码路径, 排除.

## DISCUSSION

来源: 2026-09-08 规则审核 (Codex 第二至四轮 F6/N3 项及 help 文本). 相关代码: `internal/notify/dispatch.go:80`, `:86`, `internal/board/dispatch.go:314`, `:364`, `internal/launch/dispatch.go:165`, `:169`, `:277`, `:231`, `internal/launch/dispatch_evidence.go:224` (stopped 观测校验可复用).

SIZE 定为 large: 涉及授权事务、持久记录、两个传输入口 (notify/resume) 与订阅协议, 执行者启动时先写 `plan.md`, 建议阶段: (1) 期限读取统一到 "当前 epoch 期限"; (2) 轮换事务放宽 accepted 且附 stopped 观测; (3) notify/resume 恢复分支; (4) 订阅与 dispatch show 输出; (5) 规则与文档; 每阶段附兼容旧记录的验证.

实现提示: 轮换请求可携带与 `authorize-wrap-up` 相同结构的 stopped 观测; 新 epoch 的期限存在 execution-<epoch> 记录里, 不改 `Input.ConfirmBy`.

本卡与 `20260908-plan-target-sync-task`, `20260908-fix-evidence-nonblocking-task`, `20260908-dispatch-binding-release-task` 互不依赖, 可单独执行.

- SELF_REVIEW: 通过 (修订后复查). 目标补入原始期限的全部直接消费方, 验收要求它们一致改读当前 epoch 期限并有全链路回归; 威胁模型要求的旧 epoch 各类写入拒绝均有对应验收; 观测的 alive/unknown/过期/身份变化拒绝有验收; 边界四类各给理由; SIZE 改为 large 并写明计划阶段.
- CARD_REVIEW: 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08). 发现: (1) 验收不足以证明新 epoch 真能恢复, 多处仍直接用 Input.ConfirmBy; (2) 旧 epoch 只验收了完成拒绝; (3) 漏订阅期限规则与两份协议文档; (4) small 偏小. 四项均已修正并重新自审.

## IMPLEMENTATION

<FILL_IN>

## SUMMARY

<FILL_IN>
