# Author review disposition, batch evidence summary and an unbypassable completion gate

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260907-review-archive-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 13:39
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:tV:wX:p1D
- STARTED_AT: 2026-09-07 19:43
- FINISHED_AT: 2026-09-07 22:26
- TASK_BRANCH: review-disposition-gate
- RESULT: completed

## GOAL

在 A 的运行证据上建立 finding 处置、增量上下文和批次完成门禁。执行端保留自己结论的作者权；工具校验并汇总完整批次视图，消除原卡“编排手改全部结论或派回所有无 finding 卡”的二选一。门禁覆盖缺失/失败的必需角色和明确的前后批次，不以索引缺失、任意角色 PASS 或墙钟顺序代替完成证据。

## USER_DECISIONS

保留原决定：审核原件和执行 Agent 的逐条验证结论落盘，正文只留索引；原三卡不启动。本轮用户要求全面整合并更新已有任务卡。工具汇总原作者记录是本轮设计，不把未回答的原 A/B 选择写成用户决定。

## EXPECTED_OUTCOME

- 原作者处置记录保存在自己卡的 reviews，编排者只引用并可另记本人验证意见；工具生成一致的批次 disposition。
- 每条 finding 的身份、归属、处置与证据可追溯，增量轮直接读取原件。
- required 角色缺失、全部运行失败、错轮次、未闭批或无效结论都阻止新周期 done；合法 N/A 与旧完成卡有明确兼容。

## ACCEPTANCE_CRITERIA

- [ ] 定义受控提交的 finding 处置记录与机器汇总 disposition：run_id、finding_id、batch_id、task_id、作者、记录时间、原文依据、修复 SHA 和状态必须绑定；单卡由执行 Agent 提交，组卡各执行 Agent 仅提交自身归属。编排 Agent 的验证意见单独标作者，禁止覆盖或冒充执行端记录。
- [ ] 工具按批次 assignment 清单聚合作者记录并通过 S 发布；完整批次视图可归档到每张成员卡。不含 finding 的成员无需仅为补抄记录而 notify；跨卡 finding 用显式多归属及各自记录，不用任意 delegated 占位绕过总覆盖。
- [ ] Reviewer prompt 固定可解析的 FINDINGS/NON_BLOCKING 与稳定 ID，身份为 run_id+finding_id，增量通过 lineage 对应前轮；校验仅解析结构化条目，不从正文任意提及的 ID 推断条目。
- [ ] 新报告 ID 缺失/重复/结构不合法时不能自动通过；旧无结构报告保留原件，必须有显式人工映射及原文定位才能参与新门禁；禁止用空 disposition 或“未找到 ID”跳过覆盖检查。
- [ ] 处置状态覆盖 confirmed/fixed/rejected/unverifiable/waived 与 non-blocking fixed/deferred/rejected；每项有来源及依据，fixed 绑定修复 SHA，waived 绑定适用规则允许的用户决定；PM/QA 的 must-fix 不得借 CSA/Hacker 的 accepted-risk/timed-out 例外放行。
- [ ] 机械修复允许的 PASSED_AT 前移/更新必须满足既有规则，并保留机械项及验证证据；非机械修复要求新运行。工具执行 status=ok 不是 PASS；结果通过必须由合法 disposition 决定。
- [ ] 引入 review plan，记录执行周期/批次实际适用角色、N/A 原因及依据、成员与目标版本。新执行周期即使完全没有索引、某 required 角色从未运行或只有 failed 记录，done 都拒绝；审核明确不适用时有机器可查的 N/A 记录，不靠空索引推断。
- [ ] 旧已 done/archived 卡可标 legacy-untracked 并保持读取，不补造历史 PASS；升级后的活动执行周期需建立审核要求。check 区分合法进行中的最新待结论和结构错误，done 必须满足该周期全部要求。
- [ ] 批次只有全部适用角色、相关作者处置与未解决项满足规则才可 closed；A 的 batch_id 在修复增量中保持不变，target_commit 只经本批交付的 CAS 推进，关闭证据必须绑定最终 target_commit。后批 base 绑定前一 closed batch 的最终 commit，不能等于任意旧角色的一次 pass 就算接续。多卡 run 副本按 ID 去重，部分发布不算完成。
- [ ] 在 review/编排路径验证当前最终 HEAD 与 passed_at/修复提交的 Git 关系并绑定证据；board 只复用纯结构校验，不反向 import review。集成授权、实际交付及最终 Git 检查继续由既有协议执行，不把结构检查冒称代码已集成。
- [ ] 增量 review --task 自动按前驱 run_id 读取原报告与作者处置/批次 disposition，原文并入 prompt；错 batch/commit/成员/语言、缺失或不一致副本在启动前拒绝。上一轮允许处于 fail 结论以供修复复审，不能要求前轮语义 PASS 才能复审。
- [ ] check 与 move done 共用验证逻辑；新增 tested cases：required 角色缺失/全 failed、部分归档、空结论绕过、旧/跨轮次 ID、错归属、跨批次假接续、机械项与非机械项、多卡无 finding 成员不派回、合法 N/A 和旧完成卡。
- [ ] 本仓库 CSA/Hacker 保持 N/A，通用产品测试可覆盖其他角色配置；更新 review/task-group/kanban 规则，明确机器汇总不等于编排端代写作者结论。go test ./...、相关 -race、build/vet/格式检查通过，字符串三语齐全。

## THREAT_MODEL

保护完成状态与结论来源，防误引用、漏角色、错轮次和结构性假通过。报告和处置均作为数据校验，结构验证不能代替主 Agent 的真实复核；不把本机文件哈希当防恶意用户签名。

## OUT_OF_SCOPE

- 既有问题：缺索引绕过、全部失败被忽略、无 ID 报告跳过覆盖、单角色 PASS 被当闭批与本目标直接相关，纳入；完整自然语言语义 lint 不做。
- 加固：稳定 finding 身份、原件绑定、适用角色清单及 Git 证据绑定是 gate 必要范围；签名和新安全风险接受政策排除。
- 共享契约与文档：review plan、作者处置、聚合、增量上下文、批次关闭和 done 门禁归本卡；A 定义的原件及 run schema 只通过协商扩展，不另建冲突索引。
- 相邻功能：不实现 durable dispatch 或事件传输；本卡处置可先通过 S 的受控入口提交，后组再关联 dispatch_id；不做 TUI 统计。

## DISCUSSION

```text
PREREQUISITES: 20260907-review-report-archive-task
```

统一方案见 S 的 plan.md。原待裁定 A/B 块已被技术方案替换：不替用户选择编排 Agent 手改作者结论；保留执行端自己的原文并让工具机械汇总。旧版 spec 和旧 CARD_REVIEW 结论保存在 planning-history/20260907-before-integration.md；本轮独立审查须重点验证作者边界、空索引 gate 和批次链。

SELF_REVIEW: 通过。已核对用户目标、范围、接口与八卡依赖；独立卡审指出的受控写入口、batch/run 提交语义、代收尾授权三项问题已修订并复核关闭。旧版 PASS 不继承。

CARD_REVIEW: PASS — 2026-09-07，独立 Codex 子 Agent /root/integrated_card_review（未继承建卡会话上下文），首次审查及修订后增量复核，八卡均无剩余阻断。完整原文见 20260907-card-write-transactions-task 的 card-review.md；仅为契约审查，不代表实现或 PM/QA 代码审核已通过。

## IMPLEMENTATION

- 工作树：`/home/dualf/works/kander/worktrees/review-disposition-gate`；分支 `review-disposition-gate`。
- 来源组基线：`8241a49b1f50cbb99acc68b4c3dc58b5a803253e`；前置 A 的最新交付已在远端组分支，fetch 成功。
- 复用 board 的 run/batch 与 S 事务；新增 review plan、结构化 finding、不可覆盖作者处置、批次聚合及纯结构完成校验。Git 关系仍由 review 层验证。
- 执行端不触发组审核、不更新组分支；完成后记录交付并进入 review。

- 最终交付：`d325bb9755cba0ac704a6841cd1d2d786eed42ee`；本地与远端任务分支一致，已正常推送。最新组基线：`8241a49b1f50cbb99acc68b4c3dc58b5a803253e`；fetch/rebase 无变化，无冲突，随后全量复验通过。
- 完整实现及 13/13 项验收自检见 [交付报告](report.md)；全量、race、build/vet、格式及 Windows 交叉编译通过，详见 [验证记录](validation.txt)。Windows 原生环境缺口保留。
- 本卡未触发组审核；PM/QA 等待编排端执行，CSA/Hacker N/A。未部署、未迁移真实看板、未更新组分支；任务分支和 worktree 保留。

- 首轮审核修订交付：`55e18dc727730d0bab164ce67d2bc235fb1ec8c5`，已正常推送；最新组基线 `d325bb9755cba0ac704a6841cd1d2d786eed42ee`，fetch/rebase 无变化，随后全量复验通过。
- PM/QA 全部 15 项独立核实与处置见 [逐项记录](review-round-1-disposition.md)，完整原文见 [首轮原件](review-round-1-original.md)，测试和二进制隔离冒烟见 [验证记录](review-round-1-validation.txt)。
- 当前完整结果见 [交付报告](report.md)；旧实现报告单独保留。Windows 原生缺口、编排端增量复审和最终集成继续保留。

- 第二次修复交付：`6f3b708aee02546791998cf6a61fc5df7997dc14`，已正常推送；最新组基线 `55e18dc727730d0bab164ce67d2bc235fb1ec8c5`，fetch/rebase 无变化，随后全量复验通过。
- PM-01 首次修复仍有 lineage 残留，本轮已独立复现并修复；PM-08/QA-05/QA-06 同时逐项处理。完整原件、处置和测试分别见 [本轮原件](review-round-2-original.md)、[逐条记录](review-round-2-disposition.md)、[验证](review-round-2-validation.txt)。
- QA 通过结论按主控通知保留，等待 PM 再次增量；执行端不审核、不集成或部署。当前结果见 [报告](report.md)。

- PM 第三轮非阻断处置交付：`251f5d89186730ea372053a401136030710efe84`；有代码变化，已推送，对齐组基线 `6f3b708aee02546791998cf6a61fc5df7997dc14` 无冲突。PM-09 独立复现后修复为标明出错 run ID；合法按序映射恢复与原件不变测试通过。
- 原报告与实际验证分别见 [原件](review-round-3-original.md)、[作者处置](review-round-3-disposition.md)、[日志](review-round-3-validation.txt)。本轮 go test ./...、board vet、格式及定向回归通过。
- 按主控通知，PM/QA 均已通过；本轮不重跑审核。保留 Windows 原生缺口，待主控接收本交付、闭批与统一集成，另发 wrapup。

- 收尾授权及实际结果：主控已将最终组 HEAD `251f5d89186730ea372053a401136030710efe84` 正常推送 origin/develop，主工作树已 ff；本卡交付无重写。执行端独立 fetch、祖先与 SHA 核验全部通过，见 [独立记录](wrapup-validation.txt) 与 [主控集成证据](group-integration.md)。
- 第四批审核已闭批，PM/QA 通过结论按 [闭批证据](batch4-closed.md) 承接；历史原件与作者逐项判断未改写。CSA/Hacker N/A，Windows 原生缺口保留。
- 已离开任务 worktree 后成功删除本卡工作树、本地分支及远端分支，并复核远端不存在；组资源交由主控保留，未操作其他卡或关闭交互终端。完成前 kander check 本卡通过。

## SUMMARY

13/13 项验收自检完成，最终交付 `251f5d89186730ea372053a401136030710efe84` 已包含于 origin/develop 与本地 develop，无提交重写。作者全量及针对性验证通过，PM/QA 在第四批增量流程通过并由主控闭批；CSA/Hacker N/A。全部 finding/建议已处置，历史原件和各轮判断保留。唯一残留为 Windows 原生验证缺口，交叉编译不替代原生实测。本卡工作树及本地/远端任务分支已清理，代码现位于 develop；组资源与交互 CLI 保留给主控后续处理。RESULT completed，进入 done；完整证据见 [完成报告](report.md)。
