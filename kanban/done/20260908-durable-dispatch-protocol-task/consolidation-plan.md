# 2026-09-08 todo 卡片整合方案

## 用户决定

用户在主会话原话："对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分."

判定规则：同一目标内只能串行的子行为合并为一张卡；只有同一目标内能真正并行的子行为才保留拆分。跨目标的卡本来就分开，不受影响。

## 整合结果：14 卡/6 组 -> 7 卡/3 组

| 新卡 | 组 | 合并自 | 前置 |
| --- | --- | --- | --- |
| P3 `20260907-probe-batch-budget-task`（不变） | `20260907-runtime-observation-group` | — | 20260907-probe-cancel-deadline-task |
| `20260908-subscription-facts-task` | `20260908-subscription-dispatch-group` | E2 `20260907-subscription-revision-snapshot-task`, E3 `20260907-subscription-watch-membership-task` | S, E1 |
| `20260908-subscription-bounded-runtime-task` | `20260908-subscription-dispatch-group` | E4 `20260907-subscription-probe-isolation-task`, E5 `20260907-subscription-output-exit-task` | P3, subscription-facts |
| `20260908-durable-dispatch-protocol-task` | `20260908-subscription-dispatch-group` | N1 `20260907-dispatch-intent-store-task`, N2 `20260907-dispatch-atomic-receipts-task`, N3 `20260907-notify-durable-delivery-task` | S, P3 |
| `20260908-dispatch-evidence-binding-task` | `20260908-bindings-recovery-group` | N4 `20260907-wrapup-fenced-authority-task`, N5 `20260907-fix-review-reference-binding-task` | durable-dispatch-protocol, R |
| E6 `20260907-subscription-dispatch-facts-task`（改组/改前置，契约不变） | `20260908-bindings-recovery-group` | — | durable-dispatch-protocol, subscription-bounded-runtime |
| `20260908-coordinator-recovery-task` | `20260908-bindings-recovery-group` | O1 `20260907-coordinator-checkpoint-cas-task`, O2 `20260907-coordinator-fact-reconcile-task`, O3 `20260907-coordinator-wrapup-reconcile-task` | S, E6, dispatch-evidence-binding, R |

S = 20260907-card-write-transactions-task（done），R = 20260907-review-disposition-gate-task（done），E1 = 20260907-subscription-heartbeat-clock-task（review），P1/P2 review，均属原主线程/侧会话已在推进的组，本次不动。

## 每次合并的理由

- E2+E3：同为"订阅报告的事实正确且完整"；E3 只能在 E2 的版本事件载体之后实现，串行无并行收益。
- E4+E5：同为"订阅运行时有界"；E5 原本只因共享运行循环与取消资源而串行于 E4。
- N1+N2+N3：同一"持久派回协议"的严格串行链。N3 另需 P3，合并后整卡以 S、P3 齐备为启动条件。
- N4+N5：同为"派回绑定合法证据"；两卡共享派回绑定接线处且原卡要求不能共享工作区，实际无法并行。2026-09-07 独立审卡曾因粒度要求拆出 N5，本次按用户新决定合回，原 N 验收 8/10 仍全部覆盖。
- O1+O2+O3：同一"编排按持久事实恢复"的严格串行链；检查点本身无独立价值。代价：O1 原可只依赖 S 提前开工，合并后随整卡最后启动。

保留拆分的并行对：subscription-facts ∥ durable-dispatch-protocol（subscribe 与 notify/board 资源隔离）；dispatch-evidence-binding ∥ subscription-dispatch-facts（同上）。P3 与第 2 组无技术依赖，仅受组间集成顺序约束（第 1 组 done 后才建第 2 组分支），不构成并行对。E6 与派回绑定并行且目标独立，故不并入。

## 分组与执行顺序

1. `20260907-runtime-observation-group`（P1/P2/E1 review + P3 todo）：由原侧会话/主线程继续，本次不动。
2. `20260908-subscription-dispatch-group`：外部前置 S（done）、E1、P3（第 1 组）。第 1 组 done 且交付在 develop 后建组分支；subscription-facts 与 durable-dispatch-protocol 首发并行，subscription-bounded-runtime 在 subscription-facts 交付进组分支后启动。
3. `20260908-bindings-recovery-group`：外部前置为第 2 组全部 done 且在 develop、R（done）。dispatch-evidence-binding 与 subscription-dispatch-facts 首发并行，coordinator-recovery 最后。

组分支、交付、审核批次、develop 集成与收尾按 KANDER-TASK-GROUP-RULES.md / KANDER-GIT-RULES.md / KANDER-REVIEW-RULES.md 执行；PM required，QA 按项目要求对代码改动仍运行，CSA/Hacker N/A。本次只整合卡片，未启动任何卡，未创建新组分支，不扩大既有集成/接管授权。

## 原验收覆盖

原四张大卡的验收项到 2026-09-07 拆分卡的映射见 20260907-probe-result-classification-task 的 resplit-plan.md；本次合并只是把该映射中的多张卡归并为一张，每张新卡 DISCUSSION 列出承接的原验收编号，行为验收原文逐条保留，未删除任何验收项。原 13 个复现的归属随卡合并（P1/P2/E1/P3 不变；E2/E3 -> subscription-facts；E4/E5 -> subscription-bounded-runtime；N3 -> durable-dispatch-protocol；E6 不变；O3 -> coordinator-recovery）。

## 原卡处置

被合并的 13 张原卡以 `duplicate` 归档并 `--duplicate-of` 指向替代卡；原契约、CARD_REVIEW、execution-plan.md 随目录保留。20260907-fix-review-reference-binding-task 目录内的 execution-plan.md（17 卡/6 组）自此为历史，本文件优先。
