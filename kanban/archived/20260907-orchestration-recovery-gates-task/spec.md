# Orchestration recovery checkpoints, review closure and end-to-end acceptance

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

整合 S/D/A/R/P/N/E 的协议，持久化编排必要阶段并改写发布规则，保证重启后从事实恢复派回、交付、审核和收尾。用端到端故障注入验证整体收敛，避免每包测试通过而链路仍等待不存在的事件。

## USER_DECISIONS

本轮用户要求全面方案并创建/更新任务卡；沿用原卡只规划、不启动。最终 Git 集成、接管、更换 Agent 和清理仍遵守既有用户授权，不由自动恢复推导新增授权。

## EXPECTED_OUTCOME

- 编排阶段有可读可校验的持久检查点，新快照可恢复，重复事件处理幂等。
- 作者结论、派回轮次、审核批次和最终集成证据连成可验证闭环。
- 每个已复现故障都有对应归属和跨模块验收，必要文档无矛盾。

## ACCEPTANCE_CRITERIA

- [ ] 通过 S 的受控事务保存 group/成员版本、已接收 delivery、batch/run 引用、待派回/待收尾集合与已观察 revisions；引用任务 ID 和相对 artifact ID，不缓存跨状态绝对路径；检查点不替代原件事实。
- [ ] 组检查点保存在 kanban/.kander/groups/<group-id>/checkpoint.json，以 group_id 取得稳定资源锁；扩展 S 的原语并遵守看板、组、任务固定锁序，不把组控制记录变成状态目录里的新卡或第二状态真相源。
- [ ] 提供读取/校验和幂等更新检查点的命令入口或现有子命令扩展，单二进制内实现；重启先校验任务/派回/批次事实再重建阶段，缺失或冲突证据明确报告，不猜已通过或已完成。
- [ ] 同组只有一个有效编排写入授权；过期编排者不能覆盖新检查点或重复派回。订阅 EOF/失败仅对已知可恢复故障做有界恢复；重复卡/reparse/真实损坏停止，不循环重试掩盖错误。
- [ ] 初始快照和后续所有相关事件走同一事实对账：已 accepted/completed 派回解除待确认，已集成的相同 delivery 只验证，错轮次/旧交付不放行；事件重复/丢失/重排后仍保持幂等。
- [ ] 改写规则中“必须见 review->working state-change”及 notify 后重启的矛盾要求；允许新 snapshot 证明业务完成。无输出本身不是失败，但持久确认期限、独立心跳和进展停滞各有明确处理。
- [ ] 审核派回引用 A/R 原件，执行端回传原作者结论，工具聚合批次；无 finding 成员不为抄写而派回；required 角色、批次 closed、delivery SHA、未解决项和实际 Git 集成共同构成收尾前置。
- [ ] wrap-up 绑定具体 dispatch_id 和已验证集成证据；快速 review-working-done 或编排重启都能关闭同一收尾轮；未取得原有集成/接管权限时保持现状并报告，不越权合并或接管。
- [ ] 规则保留既有编排代收尾例外并消费 N 的 wrap-up-only 授权：不确定投递先对账，确认旧执行者退出/被合法回收且 epoch 已隔离后才代清理与记录；记录代办作者/原因并完成同一 dispatch，禁止因超时/普通非零码并行收尾、代修代码或改写原作者结论。覆盖无 SESSION、投递不确定、仍活动、确认退出四类用例。
- [ ] 端到端覆盖：发送前/后崩溃、回执前已完成、执行端/订阅端/编排端分别重启、同 ID 重试、旧 epoch 写入、持续状态变化且另一 Agent 死亡、pending review 未接收退出、外部组中途增卡。
- [ ] 端到端覆盖：同卡 move+并发 PM/QA 归档、批次多卡部分发布恢复、required 角色缺失/全失败、错误前驱/跨批次假接续、旧无 ID 报告人工映射、无 finding 成员、机械修复与最终 HEAD 证据。
- [ ] 运行目录迁移各阶段 kill/restart、旧路径内部回滚与受控更新竞态回归；每次都断言同 ID 只有一份入口、原作者新记录不丢失、事务/派回/索引无重复提交。
- [ ] 将 analysis/ 中的 13 个复现映射到修复测试，断言必须反转原坏行为。POSIX/Windows 运行对应锁/迁移/进程测试；真实 tmux/herdr/Agent 冒烟与假命令测试分开报告，环境缺口不得写为通过。
- [ ] 全量 go test ./...、受影响并发包 -race、build/vet/格式/差异检查通过；发布 rules 为英文，README/AGENTS/docs 中文、消息三语；逐条核对命令/字段/相对路径/停止与恢复策略无旧协议残留。

## THREAT_MODEL

防编排重启后重复调度、旧授权写入和不完整证据放行。检查点仅引用已验证事实，不作为绕过 Git/用户授权/审核门禁的凭证。

## OUT_OF_SCOPE

- 既有问题：跨模块状态矛盾和故障恢复缺口在范围；无关 UI/配置/安装优化排除。
- 加固：持久必要阶段、编排写授权和故障注入为目标所需；通用工作流引擎、长期事件仓库排除。
- 共享契约与文档：本卡负责消费状态机、检查点与跨模块一致性验收；不替前卡补做其必须交付的底层功能和文档，发现缺口回归所属卡修复。
- 相邻功能：不自动升级真实看板、不默认启动/接管现有任务、不扩展安全角色策略或 TUI 新页面。

## DISCUSSION

```text
PREREQUISITES: 20260907-subscription-reconcile-task
```

统一方案见 S 的 plan.md，前置 E 已传递依赖全部七张基础卡。适用审核在代码实现后按仓库 PM/QA 门禁执行，CSA/Hacker 为 N/A；本轮独立卡审仅审契约，不冒称实现已验证。

SELF_REVIEW: 通过。已核对用户目标、范围、接口与八卡依赖；独立卡审指出的受控写入口、batch/run 提交语义、代收尾授权三项问题已修订并复核关闭。旧版 PASS 不继承。

CARD_REVIEW: PASS — 2026-09-07，独立 Codex 子 Agent /root/integrated_card_review（未继承建卡会话上下文），首次审查及修订后增量复核，八卡均无剩余阻断。完整原文见 20260907-card-write-transactions-task 的 card-review.md；仅为契约审查，不代表实现或 PM/QA 代码审核已通过。

## RESPLIT_DECISION

用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。

本卡的大范围执行方式被替代，原目标由以下卡承接：O1 `20260907-coordinator-checkpoint-cas-task`, O2 `20260907-coordinator-fact-reconcile-task`, O3 `20260907-coordinator-wrapup-reconcile-task`.

完整验收映射与新的跨卡依赖见 20260907-probe-result-classification-task 的 resplit-plan.md；原验收、USER_DECISIONS及CARD_REVIEW留作历史，新卡不继承PASS。新卡全部backlog等待独立审卡；不要再start本卡或创建旧第二组分支。

## LIFECYCLE_DECISION

{"at":"2026-09-07 19:52","decision_reference":"用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。","duplicate_of":"","reason":"原大卡执行方式按用户要求拆分替代；需求完整保留，详见 20260907-probe-result-classification-task 的 resplit-plan.md"}
