# 首轮 PM/QA 原作者逐项处置

作者：codex，本卡执行 Agent。以下结论来自目标源码、规则与实际验证；不是编排端代写，也不代表 Reviewer 已复审通过。

审核基线：`8241a49b1f50cbb99acc68b4c3dc58b5a803253e`；首轮目标：`d325bb9755cba0ac704a6841cd1d2d786eed42ee`；本轮统一修复提交：`55e18dc727730d0bab164ce67d2bc235fb1ec8c5`。首轮 PM/QA 均为 Claude/opus/high，进程 exit 0，语义未通过。完整派回和两角色原报告保存在 [首轮原件](review-round-1-original.md)，未改写 PM 首行 base 笔误；真实调用基线以上述完整 SHA 为准。

原报告共 15 个需处置条目，全部独立核实并修复。QA-NB-01 至 QA-NB-04 只是本地定位索引，对应 QA NON-BLOCKING 四项，原 Reviewer 没有为它们编号。重合问题分别记录，不删除任何一方意见。

| 原条目 | 原等级 | 执行端状态 | 独立核实与处置依据 | 验证 |
| --- | --- | --- | --- | --- |
| PM-01 | high | fixed | 确认非法结构曾被归档为 ok，所有聚合出口随之永久拒绝。FinalizeReviewRun 现解析新结构；非法报告以 failed、非零 gate exit 和原因归档，report/raw 原样保留。新 ID 完整审核后通过 resolved_failures 显式替代。 | TestMalformedReviewCanBeExplicitlyReplacedThroughCLI；独立二进制两卡冒烟 |
| PM-02 | medium | fixed | 确认调用方 review-context 被静默替换。现按字节长度封装自动原件和逐字补充；board 独立 CAS 自动来源，整体输入哈希仍冻结全部文本，重试改变补充即拒绝。未采用“拒绝所有非空手工上下文”建议，保留原调用契约。 | TestIncrementalTaskReviewReadsFailingAuthorEvidenceAndRejectsWrongSources；TestIncrementalSupplementCannotReplaceAutomaticSource |
| PM-03 | medium | fixed | 确认重新认领改变 STARTED_AT 后整组无法恢复。progress 现输出 requirements-needed 和全部 rebind_cycles；extend-plan 在原计划内 CAS 重绑定，保留成员、角色要求、批次、失败轮和旧结论。新 OWNER 仅追加自己结论，旧 OWNER 不可再写。未采用单卡换计划指针：该方案会破坏其余成员绑定并可能漏掉旧失败。 | TestGroupReclaimRetainsPlanFailuresAndOriginalAuthorsThroughCLI；TestRebindPreservesClosedEvidenceAndTerminalMember；独立二进制冒烟 |
| PM-04 | medium / mechanical | fixed | 确认规则 240/260 与新增章节冲突。统一说明 task-bound 自动上下文、可省 reviewed-commit、显式值匹配、同 batch/base/role/reviewer；手工上下文是保留的补充。同步早期增量章节和归档说明。 | 逐段对照 rules/KANDER-REVIEW-RULES.md 与 hydrateIncremental/PrepareReviewRun；全量测试 |
| PM-05 | low | fixed | 确认 advance 未检查计划 CWD。调用 Git 前读取不可变计划身份并精确比较 CWD；extend-plan 同时修复。 | TestAdvanceAndExtensionRejectOtherWorktree |
| PM-06 | suggest | fixed | 确认 tracked-cycles 仅被当存在标记。现每次计划副本验证都会解码并与计划周期比对；真实 STARTED_AT 改变与 tracker 损坏明确区分。重绑定不掩盖损坏。 | TestTrackedCycleCorruptionCannotBeRebound |
| PM-07 | recommend | fixed | 确认 sameTasks 重复标准库功能。删除函数，改用 slices.Equal，原成员顺序/身份拒绝测试保留。 | 原错成员/增量身份回归与全量测试 |
| QA-01 | medium | fixed | 确认仅填 disposition.mechanical 可跳过新运行。close 现要求主 Agent 单独署名的 mechanical assessment，绑定原条目、作者记录、报告摘要、修复 SHA、Reviewer 实际分类（含缺标）、依据、实际验证事实、变化路径与差异摘要；review 核查真实 Git 差异，board 复核完整绑定。未采用直接信任 Reviewer 标签的修法：规则明确允许主 Agent 按事实补标，标签与哈希都不替代真实语义复核。 | TestMechanicalFixAndNonMechanicalRerunGate；TestMechanicalAssessmentRequiresActualGitScope |
| QA-02 | medium | fixed | 确认问题影响计划全部成员，且原 assignment 作者冻结会阻止接管人提交。重绑定同一事务更新整组计划副本和周期，完整旧计划存入历史；不改其他成员状态、不删失败轮、不覆盖前作者原文。已 done 成员和已 closed 证据也保持原状态与字节内容。 | TestGroupReclaimRetainsPlanFailuresAndOriginalAuthorsThroughCLI；TestRebindPreservesClosedEvidenceAndTerminalMember |
| QA-03 | medium / mechanical | fixed | 确认 CheckReviewGate 的 error 返回永远为 nil。签名收敛为 []Problem，CheckBoard 删除不可达 gateErr 分支；每卡错误继续进入 problems。 | 现有 CheckBoard/门禁诊断回归与全量测试 |
| QA-04 | medium / mechanical | fixed | 独立复核确认增量参数及 reviewer 前驱要求的规则冲突。与 PM-04 一并修订全部相关文本，非 task-bound 的手工上下文要求仍明确保留。 | 逐段规则与实现对照；增量 CLI 回归 |
| QA-NB-01（原文无编号） | low | fixed | 确认 advance 和 extend-plan 都缺 CWD 绑定；现在两个入口均先核对计划 CWD，另一干净 worktree 即使拥有相同 Git 对象也不能提供该计划证据。 | TestAdvanceAndExtensionRejectOtherWorktree |
| QA-NB-02（原文无编号） | suggest | fixed | 确认 checkPendingDispositions 对 ledger 重复完整解码。移除预读；readDispositionLedger 在非 complete 模式下允许尚不存在的合法待结论，已存在时仍完整验证。 | pending/closed 门禁全量回归 |
| QA-NB-03（原文无编号） | suggest | fixed | 确认非法新报告直到后续命令才暴露。采用比发布警告更完整的修复：finalize 当场标 failed 并打印解析原因，原件保留且可明确替代。仅加非致命警告仍会留下 PM-01 的永久陷阱，故未采纳该最小修法。 | TestMalformedReviewCanBeExplicitlyReplacedThroughCLI；独立二进制冒烟 |
| QA-NB-04（原文无编号） | suggest | fixed | 确认 plan extension 测试先创建并丢弃一块看板。改为直接构造 unsealed ReviewPlan，一块看板完成真实前后批次校验。 | TestPlanExtensionUsesClosedCommitNotArbitraryRolePass |

## 核实边界与未解决事项

全部 fixed 项的修复 SHA 均为上列本轮提交；具体实现集中于 internal/board/reviews.go、review_plan_extend.go、review_gate.go、review_disposition.go、review_mechanical.go、review_context_merge.go 与 internal/review/disposition.go、mechanical.go。检查通过只证明列明的测试与结构约束，不替代主 Agent 对机械类别、事实依据和语义结果的真实复核。没有把 Reviewer 分类当作权威白名单，也没有把人为事实声明当签名。

本轮未遗留已确认且未修复的 gate 或 NON-BLOCKING 条目。三处“最小修法”采用符合原契约的替代实现，原因逐项写明。PM/QA 是否接受本轮修复、组分支接收和最终集成仍由编排端完成；本仓库 CSA/Hacker N/A。Windows 原生测试缺口保留。

完整验证见 [首轮修订验证](review-round-1-validation.txt)。编译后二进制冒烟使用真实 Git 与隔离两卡，Reviewer 进程为测试替身：非法报告按 failed 原样归档、原计划重新认领后恢复、旧失败未显式替代时 close 拒绝、显式替代后两卡 done；没有触发真实 PM/QA 审核、部署或迁移真实看板。
