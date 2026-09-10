# 剩余可靠性任务重拆与主线程交接

## 用户决定及本轮边界

用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。

本轮只创建/更新本机看板任务，不启动Agent、不联系现有执行者、不修改代码、Git分支、安装配置或主线程控制记录。使用已交付组代码构建到临时路径的kander受控new/show/update/move入口；未安装或运行init。

读取时S/D/A均review且交付已完成，继续保留原审核链与集成收尾。R已working，不是等待中的未实现卡；不抢占、改约或为拆分重置其审核。原P/N/E/O仍todo、无OWNER/SESSION，目标由下表16卡完整承接；原卡使用cancelled表示原大卡执行方式被替代，不表示取消修复需求。原契约、独立卡审及分析不删除、不改写成新卡PASS。

## 粒度与审核门禁

每张卡只交付一个可观察行为；多接口只为该行为必须同时成立的不变量服务，理由写在DISCUSSION。共享文件、方便审核、标large都不是合卡理由。必要测试/文档随行为交付，不另建测试尾卡；O3的集成场景验证一个真实的收尾前置行为，不替前卡补单元测试。

新卡统一从small目录卡开始，规模由独立卡审复核；标large不能豁免粒度。每卡最多四项行为验收加一项共同质量门禁。实现发现独立新行为、接口膨胀或新增故障域时重新评估剩余范围，不搬走未闭合finding以重置审核。

全部新卡已完成建卡者自检，尚未独立审卡；保持backlog，无CARD_REVIEW行。本侧会话明确禁止使用或联系子Agent，故不伪造审查，不越过todo门禁。主线程需让独立审卡者只读新契约、用户决定和此映射，重点检查粒度/依赖/覆盖，闭合问题后再pick/start。旧卡的PASS不可继承。

## 首次改善原问题及执行顺序

P1首次直接修复反查错误误报stopped：无任何审核/迁移/派回技术前置。P2/P3分别解决单次挂起和批量预算。
E1首次直接修复订阅心跳饥饿及timer溢出；E2让同状态更新和快速往返在revision上可见；E3补动态成员。这三卡均不依赖N/R业务接口，不能再把完整durable dispatch设为订阅核心前置。
E1与P1/P2/P3放入无外部前置的runtime-observation组（4张，均直接改善观测），E1与P1均可作为首发；各自worktree隔离，启动前按实际共享资源确认可并行。E2/E3为subscription-facts组，因S事务接口仍须S done且交付进入develop。S目前在第一组内等待R与组集成，这是现存集成限制，不是E1对R的技术依赖；本侧没有提前集成或改第一组。
第一组完成后，版本事实E组与N组在代码资源隔离时可并行。P组已有交付后，I组消费P/E；N与I可并行，分别修改notify/board和subscribe。B组消费N/I/R；O组消费B及S/R。O1技术上只需S，但现行组规则令其随O组启动；这项检查点不是前面订阅修复的前置，不影响最短修复路径。组内按下表依赖推进，已接收前置交付后可释放后继；若为审核等待必须记录具体门禁，不为非阻断建议阻塞。

所有新组都尚未创建Git分支。按主线程已有合法集成授权和Git规则，从最新可用develop建group分支，每卡独立worktree，交付远端先推再本地ff。审核按实际批次连续base链；非阻断项记录处置，不无限派回；全组通过后集成develop并由原执行者收尾。未执行Git操作不写为完成，既有main/部署/接管授权不扩大。

## 卡片和必要依赖

| 编号 | ID | 唯一交付 | 组 | 前置 |
| --- | --- | --- | --- | --- |
| P1 | `20260907-probe-result-classification-task` | 反查失败明确报告 unknown | `20260907-runtime-observation-group` | N/A |
| P2 | `20260907-probe-cancel-deadline-task` | 单次探测取消后有界退出 | `20260907-runtime-observation-group` | 20260907-probe-result-classification-task |
| P3 | `20260907-probe-batch-budget-task` | 多任务存活采集受总预算约束 | `20260907-runtime-observation-group` | 20260907-probe-cancel-deadline-task |
| E1 | `20260907-subscription-heartbeat-clock-task` | 持续状态变化不再饿死订阅心跳 | `20260907-runtime-observation-group` | N/A |
| E2 | `20260907-subscription-revision-snapshot-task` | 同状态更新可从订阅快照识别 | `20260907-subscription-facts-group` | 20260907-card-write-transactions-task,20260907-subscription-heartbeat-clock-task |
| E3 | `20260907-subscription-watch-membership-task` | 外部组成员变更进入监听集合 | `20260907-subscription-facts-group` | 20260907-subscription-revision-snapshot-task |
| E4 | `20260907-subscription-probe-isolation-task` | 慢探测不阻塞订阅状态扫描 | `20260907-subscription-io-group` | 20260907-probe-batch-budget-task,20260907-subscription-revision-snapshot-task |
| E5 | `20260907-subscription-output-exit-task` | 消费者停读时订阅仍可退出 | `20260907-subscription-io-group` | 20260907-subscription-probe-isolation-task |
| N1 | `20260907-dispatch-intent-store-task` | 派回意图可按稳定ID持久查询 | `20260907-dispatch-protocol-group` | 20260907-card-write-transactions-task |
| N2 | `20260907-dispatch-atomic-receipts-task` | 执行回执与状态迁移原子提交 | `20260907-dispatch-protocol-group` | 20260907-dispatch-intent-store-task |
| N3 | `20260907-notify-durable-delivery-task` | 不确定投递重试不再盲发第二轮 | `20260907-dispatch-protocol-group` | 20260907-dispatch-atomic-receipts-task,20260907-probe-batch-budget-task |
| E6 | `20260907-subscription-dispatch-facts-task` | 待确认派回可由快照和期限识别 | `20260907-dispatch-bindings-group` | 20260907-notify-durable-delivery-task,20260907-subscription-probe-isolation-task |
| N4 | `20260907-wrapup-fenced-authority-task` | 代收尾必须持有隔离后的专用授权 | `20260907-dispatch-bindings-group` | 20260907-notify-durable-delivery-task,20260907-review-disposition-gate-task |
| O1 | `20260907-coordinator-checkpoint-cas-task` | 编排检查点拒绝过期写入 | `20260907-coordinator-recovery-group` | 20260907-card-write-transactions-task |
| O2 | `20260907-coordinator-fact-reconcile-task` | 编排重启按持久事实恢复阶段 | `20260907-coordinator-recovery-group` | 20260907-coordinator-checkpoint-cas-task,20260907-subscription-dispatch-facts-task |
| O3 | `20260907-coordinator-wrapup-reconcile-task` | 收尾恢复必须满足审核和集成证据 | `20260907-coordinator-recovery-group` | 20260907-coordinator-fact-reconcile-task,20260907-wrapup-fenced-authority-task,20260907-review-disposition-gate-task |

## 原验收覆盖矩阵

下列序号逐条对应原卡ACCEPTANCE_CRITERIA，原文仍在原卡中，目标没有删除。跨多卡的条目按单一行为拆开；每卡自己的测试、文档和平台限制仍随卡。

| 原卡 | 原验收项 | 新归属 |
| --- | --- | --- |
| P | 1 | P1 `20260907-probe-result-classification-task` |
| P | 2 | P3 `20260907-probe-batch-budget-task` |
| P | 3 | P2 `20260907-probe-cancel-deadline-task`, P3 `20260907-probe-batch-budget-task` |
| P | 4 | P1 `20260907-probe-result-classification-task`, P2 `20260907-probe-cancel-deadline-task` |
| P | 5 | P2 `20260907-probe-cancel-deadline-task` |
| P | 6 | P1 `20260907-probe-result-classification-task`, P2 `20260907-probe-cancel-deadline-task`, P3 `20260907-probe-batch-budget-task` |
| P | 7 | P1 `20260907-probe-result-classification-task`, P2 `20260907-probe-cancel-deadline-task`, P3 `20260907-probe-batch-budget-task` |
| N | 1 | N1 `20260907-dispatch-intent-store-task`, N3 `20260907-notify-durable-delivery-task` |
| N | 2 | N1 `20260907-dispatch-intent-store-task`, N3 `20260907-notify-durable-delivery-task` |
| N | 3 | N2 `20260907-dispatch-atomic-receipts-task` |
| N | 4 | N1 `20260907-dispatch-intent-store-task`, N2 `20260907-dispatch-atomic-receipts-task` |
| N | 5 | N2 `20260907-dispatch-atomic-receipts-task`, N3 `20260907-notify-durable-delivery-task`, E6 `20260907-subscription-dispatch-facts-task` |
| N | 6 | N3 `20260907-notify-durable-delivery-task`, E6 `20260907-subscription-dispatch-facts-task` |
| N | 7 | N3 `20260907-notify-durable-delivery-task` |
| N | 8 | N4 `20260907-wrapup-fenced-authority-task` |
| N | 9 | N2 `20260907-dispatch-atomic-receipts-task`, N3 `20260907-notify-durable-delivery-task` |
| N | 10 | N1 `20260907-dispatch-intent-store-task`, N4 `20260907-wrapup-fenced-authority-task` |
| N | 11 | N2 `20260907-dispatch-atomic-receipts-task`, N3 `20260907-notify-durable-delivery-task`, E6 `20260907-subscription-dispatch-facts-task` |
| N | 12 | N1 `20260907-dispatch-intent-store-task`, N2 `20260907-dispatch-atomic-receipts-task`, N3 `20260907-notify-durable-delivery-task`, N4 `20260907-wrapup-fenced-authority-task` |
| E | 1 | E2 `20260907-subscription-revision-snapshot-task`, E3 `20260907-subscription-watch-membership-task`, E6 `20260907-subscription-dispatch-facts-task` |
| E | 2 | E2 `20260907-subscription-revision-snapshot-task`, E3 `20260907-subscription-watch-membership-task`, E6 `20260907-subscription-dispatch-facts-task` |
| E | 3 | E6 `20260907-subscription-dispatch-facts-task` |
| E | 4 | E1 `20260907-subscription-heartbeat-clock-task`, E4 `20260907-subscription-probe-isolation-task`, E6 `20260907-subscription-dispatch-facts-task` |
| E | 5 | E1 `20260907-subscription-heartbeat-clock-task`, E6 `20260907-subscription-dispatch-facts-task` |
| E | 6 | E3 `20260907-subscription-watch-membership-task` |
| E | 7 | E3 `20260907-subscription-watch-membership-task` |
| E | 8 | E2 `20260907-subscription-revision-snapshot-task` |
| E | 9 | E4 `20260907-subscription-probe-isolation-task` |
| E | 10 | E5 `20260907-subscription-output-exit-task` |
| E | 11 | E1 `20260907-subscription-heartbeat-clock-task`, E2 `20260907-subscription-revision-snapshot-task`, E3 `20260907-subscription-watch-membership-task`, E4 `20260907-subscription-probe-isolation-task`, E5 `20260907-subscription-output-exit-task`, E6 `20260907-subscription-dispatch-facts-task` |
| E | 12 | E1 `20260907-subscription-heartbeat-clock-task`, E2 `20260907-subscription-revision-snapshot-task`, E3 `20260907-subscription-watch-membership-task`, E4 `20260907-subscription-probe-isolation-task`, E5 `20260907-subscription-output-exit-task`, E6 `20260907-subscription-dispatch-facts-task` |
| O | 1 | O1 `20260907-coordinator-checkpoint-cas-task` |
| O | 2 | O1 `20260907-coordinator-checkpoint-cas-task` |
| O | 3 | O1 `20260907-coordinator-checkpoint-cas-task`, O2 `20260907-coordinator-fact-reconcile-task` |
| O | 4 | O1 `20260907-coordinator-checkpoint-cas-task`, O2 `20260907-coordinator-fact-reconcile-task` |
| O | 5 | O2 `20260907-coordinator-fact-reconcile-task` |
| O | 6 | O2 `20260907-coordinator-fact-reconcile-task` |
| O | 7 | O3 `20260907-coordinator-wrapup-reconcile-task` |
| O | 8 | O3 `20260907-coordinator-wrapup-reconcile-task` |
| O | 9 | N4 `20260907-wrapup-fenced-authority-task`, O3 `20260907-coordinator-wrapup-reconcile-task` |
| O | 10 | N2 `20260907-dispatch-atomic-receipts-task`, N3 `20260907-notify-durable-delivery-task`, E3 `20260907-subscription-watch-membership-task`, E4 `20260907-subscription-probe-isolation-task`, E6 `20260907-subscription-dispatch-facts-task`, O2 `20260907-coordinator-fact-reconcile-task` |
| O | 11 | O3 `20260907-coordinator-wrapup-reconcile-task` |
| O | 12 | O3 `20260907-coordinator-wrapup-reconcile-task` |
| O | 13 | O3 `20260907-coordinator-wrapup-reconcile-task` |
| O | 14 | O1 `20260907-coordinator-checkpoint-cas-task`, O2 `20260907-coordinator-fact-reconcile-task`, O3 `20260907-coordinator-wrapup-reconcile-task` |

## 原13个复现的明确归属

原件：20260907-card-write-transactions-task 的 analysis/。以下必须反转原坏行为断言，不把原复现通过当修复通过。

| 复现函数（省略TestAudit前缀） | 归属 |
| --- | --- |
| ReverseLookupErrorBecomesStopped | P1 |
| DescendantOutputOutlivesProbeDeadline | P2 |
| ChangesSuppressLiveness | E1；慢探测组合由E4 |
| RefreshDurationOverflow | E1 |
| RoundTripIsInvisible | E2验证revision；E6验证同dispatch完成 |
| WatchedGroupMembershipIsFrozen | E3 |
| WatchedGroupSilentlyOmitsUnreadableMember | E3 |
| ProbeBlocksScanAndCancellation | E4 |
| BlockedWriterIgnoresStop | E5 |
| ReviewHasNoLiveness | E6 |
| PromptEchoCountsAsAcknowledgement | N3 |
| RollbackRecreatesMovedCard | 已交付S；O3集成时验证未回归 |
| GuardCheckDoesNotCoverLaterMove | 已交付S/D的受控更新边界；O3验证旧护栏检查不被误称原子写，断言受控路径不会复活卡 |

目录迁移kill/restart属于已交付D，O3只做跨模块回归；机器审核原件、required角色和作者处置仍归A/R，不搬到新卡重新实施。原13复现只是底线，新卡另有预算、epoch、部分发布和恢复场景，不以13个数量替代覆盖。

## 已运行/等待卡评估

- S/D：实现和复审已结束，只剩实际集成与收尾，拆成新实现卡会重复工作并损坏审核历史，保留。
- A：已完成最后非阻断处置并闭批，最终8241a49b1f50cbb99acc68b4c3dc58b5a803253e，保留；日志成本、混合行尾与原生Windows等残留按其作者报告，不塞入本次修复前置。
- R：读取时已working（任务窗口字段可查），不能以先前“尚未启动”的旧信息改约。其粒度仍偏大；由原执行者在真正等待且有清晰剩余范围时评估，保持当前finding/作者权。侧会话未联系或修改R，不冒称已替其拆卡。

## 主线程接续清单

1. 读取原四卡末尾替代说明和本计划，停止使用旧P→N→E→O链及旧第二组成员集合；新16卡都还在backlog。
2. 继续当前第一组R执行/审核/集成，不重跑S/D/A已通过角色；本侧未改其代码、卡片、会话或控制记录。
3. 独立审查新卡粒度、完整覆盖、组启动限制及资源冲突；新卡PASS必须真实产生，不沿用旧审卡。
4. 按新依赖创建组、接收交付和组织审核；明确报告原问题何时首次改善。非阻断项可以在收尾补作者记录，不制造依赖门禁。
5. 未来继续修订此计划时用受控update保留决策，禁止通过同名新卡复制已有实现或重置未闭finding。

## 后续启动授权与编排归属（优先于上文建卡阶段限制）

用户随后要求“拆好就开整吧”，并对本侧使用子Agent独立审卡和执行明确答复“我授权”。本侧现负责新16卡的独立审卡、按计划启动/交付/审核/集成与收尾；既有S/D/A/R审核归档组仍由原主线程负责。本侧不修改其卡片、任务分支或会话。上文“不启动/禁止子Agent/等待主线程审卡”是已被后续授权取代的建卡阶段事实，不再阻塞新任务。

活动控制记录：/home/dualf/.local/share/kander/orchestration/20260907-resplit-execution/control.md。原主线程读取此交接时不要重复启动新16卡；先读取各卡实际状态和本侧控制记录。用户接受本计划后启动包含既定组分支交付及develop集成步骤，main/部署/真实看板迁移仍无新增授权。
