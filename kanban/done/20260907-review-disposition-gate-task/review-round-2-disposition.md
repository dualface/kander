# 第二次修复：原作者逐项核实与处置

作者：codex，本卡执行 Agent。固定审核 base：`8241a49b1f50cbb99acc68b4c3dc58b5a803253e`；本轮被审目标 / 后续 PM reviewed-commit：`55e18dc727730d0bab164ce67d2bc235fb1ec8c5`；本轮修复 SHA：`6f3b708aee02546791998cf6a61fc5df7997dc14`。

完整第二次派回、PM 增量报告和 QA 增量报告保存在 [原件](review-round-2-original.md)，未改写。此前交付记录中 PM-01 的 fixed 是执行端第一次修复的判断；PM 本轮指出关系级校验残留，经真实二进制复现确认，前次 fixed 声明不代表该项当时完整闭合。本次是该项第二次修复尝试，是否闭合仍待 PM 复审。QA 已通过的结论按主控通知保留，执行端不重复发起审核。

| 条目 | 原等级 | 执行端状态 | 独立事实与修复 | 验证 |
| --- | --- | --- | --- | --- |
| PM-01 残留 | high | fixed，待 PM 复核 | 确认 finalize 仅 ParseReviewFindings，格式合法的错误 lineage 被归档 ok，aggregate/assign/增量上下文及 done 随后全部受阻。现同事务调用与 assignment/aggregate 相同的 validateFindingLineage，在冻结原件前统一标 failed、非零退出并保存具体原因。关系要求未放宽。人工旧报告映射也在不可变发布前校验关系，错误请求尚未落盘，可更正后重交。恢复时首轮失败重跑完整首轮；增量失败以新 run ID 接回同一最后有效前驱，不使用坏运行作为前驱，不另建与有效旧链无关的完整链。旧失败须由 resolved_failures 显式指向合法链上的成功替代。 | TestFinalizeRejectsInvalidFindingRelationships（无平台标签）；TestLineageFailuresRecoverThroughControlledCLI；旧/新独立二进制对照实测五类关系。 |
| PM-08 | low | fixed | 确认未建计划批次的独立 advance 只报 plan_id。保留既有“先补建计划再独立推进”边界，改为明确提示 batch has no review plan; create its plan before advancing。未跳过 CWD 校验。 | TestAdvanceWithoutPlanExplainsRecovery：无计划拒绝；受控接管已有批次为计划后，同一 advance 请求成功。 |
| QA-05 | low | fixed | 独立复核同一空 PlanID 路径，报错现指出真正缺失项和恢复动作。已有计划 CWD 精确绑定不变，inline --advance-file 的原协议未改。 | 同上，另保留 TestAdvanceAndExtensionRejectOtherWorktree。 |
| QA-06 | suggest | fixed | 确认内部字节计数与空补充段进入 Reviewer prompt。构造 prompt 时拆分已冻结上下文，以 Automatic archived review context 和非空 caller-supplied context 分别显示；空补充不生成段落。冻结 review-context.md 的字节长度封装及整体哈希不改。非任务绑定调用原样保留手工文本。 | 五类 CLI 恢复中的空补充断言；原增量测试的非空补充、原件保留、重试、内部标记不进 prompt 断言。 |

## 旧项核实

PM-02 至 PM-07 的 Closed 结论与当前源码一致：补充文本逐字冻结、整组周期原计划 CAS 恢复、规则一致、计划 CWD 绑定、tracked-cycles 校验和 slices.Equal 均保留。QA-01 至 QA-04、QA-NB-01 至 QA-NB-04 的 Closed 结论也与当前实现一致；主 Agent 机械评估及 Git 证据、作者接管记录、不丢失败的周期恢复、诊断签名和去冗余均未被本次修改削弱。PM-08/QA-05 的附带提示问题单列于上表，不覆盖两角色原文。

## 全流程实测与保留边界

用此前 `55e18dc` 构建的独立二进制，在五个隔离双卡看板分别复现：无前驱却带 lineage；FINDINGS/NON_BLOCKING 两条目指同一前驱；前驱条目不存在；复用旧 ID 遗漏 lineage；指向非立即前驱的 run。五例 JSON 格式均合法，旧命令退出 0，aggregate 因关系错误拒绝。

新二进制对五例均非零退出并归档 failed，report 原件保留；同 ID 重试不转成功；aggregate/progress 可以前进但两卡 done 仍拒绝。新成功运行接回最后有效前驱后，未填写 resolved_failures 的 close 仍拒绝，明确替代后闭批和两卡 done 成功，坏报告原文逐字不变。Reviewer 子进程是测试替身，命令入口、Git、归档和看板事务是真实实现；没有发起真实 PM/QA 组审核。

旧报告人工映射通过原映射测试验证：合法原文定位配非法 lineage 的请求拒绝，去掉非法关系后可成功提交，原报告未改写。工具不对自然语言语义作自动 PASS 推断，也不通过删除坏报告或更换批次绕过覆盖。

全部本轮意见已处置，无执行端已确认但未处理的条目；PM 复审、组分支接收和集成由编排端执行。CSA/Hacker N/A；Windows 原生测试缺口保留。验证原文见 [记录](review-round-2-validation.txt)。
