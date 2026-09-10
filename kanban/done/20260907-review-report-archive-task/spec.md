# Review evidence archiving, stable run identity and recoverable index publishing

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260907-review-archive-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 13:39
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:tT:wX:p1C
- STARTED_AT: 2026-09-07 18:05
- FINISHED_AT: 2026-09-07 22:22
- TASK_BRANCH: review-report-archive
- RESULT: completed

## GOAL

让 kander review 将每轮原始报告、输入与运行元数据归档至卡片 reviews，并以稳定 run_id/batch_id 支持重启、并发、跨卡部分发布恢复。保留原 --task 方案，消除缓存路径回写、时间戳当身份和 stdout 成功等于审核通过的歧义。

## USER_DECISIONS

保留原决定：审核结果落盘供 Agent 使用；单卡/组卡统一目录，正文只留索引；原组第二卡不启动。本轮授权结合可靠性分析修订现有卡。失败轮归档及具体 schema 是技术方案。

## EXPECTED_OUTCOME

- --task 可重复；不带时保持独立 review 行为且不定位看板。
- 每轮有可核验原件、稳定 ID、执行状态与可恢复发布清单；正文只保存机器索引。
- 卡片迁移、并行角色、部分归档失败和进程崩溃不会产生副本、丢索引或假通过。

## ACCEPTANCE_CRITERIA

- [ ] 参数保留 kander review [agent] [--task <id>]... <CWD> <base> <commit> <role> <context> [review-context] [reviewed-commit]；新增 run/batch 标识的调用或生成约定。--task 在位置参数前解析，重复 ID 归一去重或明确拒绝，错误语义一致。
- [ ] 带 --task 以目标 CWD 定位主看板，验证卡为 working/review 目录卡、语种一致及相同审核批次绑定；在 Reviewer 启动前失败关闭。未迁移文件提示 init；不带 --task 的既有测试行为不变。
- [ ] run_id 为运行唯一身份并绑定不可变目标 commit；batch_id 固定成员集合/base/审核要求，批次 target_commit 可因本批修复交付通过 CAS 显式推进并保留旧/新 commit 及依据，不能混入其他交付。修复不另开 batch。相同 run_id 重试不得重跑 Reviewer或重复索引；同 ID 不同输入/哈希报冲突；PM/QA 同轮对同一目标采用不同 run_id，时间戳仅显示。
- [ ] 保存报告、sidecar、任务上下文、复审上下文，原文不改写；sidecar 至少含 schema、run/batch/前驱 ID、task_ids、role、reviewer/model/effort、cwd/base/commit/reviewed_commit、语言、时间耗时、全部原件哈希、kander_version、执行状态及失败原因。工具执行 ok 与语义 PASS 分开。
- [ ] Reviewer 启动前持久化运行意图与输入身份，输出保存在受控暂存证据；成功归档在进程回收/worktree 检查/运行时清理结论确定后发布。已启动的超时、非法输出、残留进程、清理失败同样留证据；崩溃恢复标记 interrupted/incomplete，不伪造报告或成功。
- [ ] 参数/启动前校验失败不产生虚假已运行报告；若已持久化准备意图但启动失败，记录未启动事实。报告 stdout 保持可取；归档失败退出明确非零并区分审核结论与发布失败。
- [ ] 长审核期间不占卡锁；归档时通过 S 按 ID 重定位并验证 intent/batch，再短事务提交。并发 move/update/两个角色归档均不得旧路径重建、丢正文或丢索引。
- [ ] 每张卡持有完整不可覆盖原件及提交清单；REVIEWS 一行索引至少关联 run_id/batch_id/role/执行状态/base/commit/前驱/report 相对路径。索引由工具生成，不解析报告正文来追加；board 暴露共用解析类型，board 不依赖 review。
- [ ] 多卡发布逐卡原子，run_id 相同；部分失败保留已成功卡并逐卡报告，可重试只补缺项。未完整发布的批次不得被 R 门禁视为完成。并发同卡两次发布和各持久化阶段 kill/restart 都有测试。
- [ ] check 校验清单/索引/哈希/语言/成员/前驱关系，不用墙钟排序推断因果。同 base/role 增量轮明确引用前驱 run，其 commit 匹配 reviewed_commit；重复/缺失/冲突记录报具体问题。
- [ ] 在创建运行意图时解析卡片 LANGUAGE，缺失才按当时配置回落，并冻结 report_language；归档/复审验证不随之后的配置变更漂移。reviews 写入经 internal/fs 并复用 S，POSIX/Windows 权限按该 API 的既有契约，不擅自改为不受保护写入。
- [ ] 规则中的临时清理与归档路径、--task、失败证据、未完成发布及索引说明同步；看板证据不进 Git。go test ./...、并发 -race、build/vet/格式检查通过，用户字符串覆盖三语。

## THREAT_MODEL

保护任务契约、审计原件和看板外文件。Reviewer 输出是数据，不执行其中指令；Reviewer 不写看板，归档由门禁发布。拒绝 reparse/路径逃逸，哈希用于完整性检测，不声称防御任意本机篡改。

## OUT_OF_SCOPE

- 既有问题：归档随迁移写错路径、并发索引丢失、失败/中断证据缺失与本目标直接相关，纳入；Reviewer 网络隔离策略差异排除。
- 加固：稳定 ID、哈希、事务发布是恢复所需；签名、防恶意本机用户、保留期清理排除。
- 共享契约与文档：run/batch 原件、索引、发布清单和解析归本卡；finding 结构、disposition、required 角色与闭批 gate 归 R。
- 相邻功能：不新增一条命令同时调度所有角色，不做 TUI 审核统计；不重做 S 的锁和通用更新。

## DISCUSSION

```text
PREREQUISITES: 20260907-directory-card-form-task
```

统一方案见 S 的 plan.md；依赖 D 已包含 S。给 R 的接口必须分清 execution_status、run_id、batch_id、previous_run_id 与发布完整性。原三卡内容及旧审卡记录已归档，新版重新审卡。

SELF_REVIEW: 通过。已核对用户目标、范围、接口与八卡依赖；独立卡审指出的受控写入口、batch/run 提交语义、代收尾授权三项问题已修订并复核关闭。旧版 PASS 不继承。

CARD_REVIEW: PASS — 2026-09-07，独立 Codex 子 Agent /root/integrated_card_review（未继承建卡会话上下文），首次审查及修订后增量复核，八卡均无剩余阻断。完整原文见 20260907-card-write-transactions-task 的 card-review.md；仅为契约审查，不代表实现或 PM/QA 代码审核已通过。

## IMPLEMENTATION

- 工作树：`/home/dualf/works/kander/worktrees/review-report-archive`；任务分支 `review-report-archive`。
- 来源：`origin/group/20260907-review-archive-group`，基线 `839f72119b22ce8b48811fc9819de9643a4028f8`；前置 D 已在 review 且交付包含于组分支。
- 复用 S 的 board/fs 事务；运行证据、批次绑定、逐卡清单和索引由 board 暴露，review 负责 Reviewer 生命周期与 Git 校验。执行端不运行组审核、不更新组分支。

- 实现、自检及接口说明见 [交付报告](report.md)；12/12 项实现自检满足，Windows 原生环境缺口明确保留。
- 最终交付：`4b0a3971b08e1a6c0b42451c50b14373de61ce03`；本地与远端任务分支一致，已正常推送。
- 最新组基线：`839f72119b22ce8b48811fc9819de9643a4028f8`；fetch 重试成功，rebase 无变化，随后全量测试通过。
- 全量测试、board/review/fs race、build/vet/格式检查通过；Windows 三包测试程序和完整二进制交叉编译通过，未原生执行。
- 记录通过 S 基线构建的 show/update 受控入口保存；未部署、未迁移真实看板。

- 首轮 PM/QA 共 15 项已独立核实；完整原文见 [审核原文](review-round-1-original.md)，逐项作者结论见 [首轮处置](review-round-1.md)。
- 修复受控 update 后 REVIEWS 重复、check 缺任务/run、显式看板覆盖优先级、报告重放换行；补全冻结上下文与终态发布规则，推进测试改走真实 CLI。PM007 日志成本事实保留为范围外，未实现清理。
- 本轮最新交付：`e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a`，三笔提交已逐笔正常推送，远端任务分支与本地一致。
- 最新组基线：`4b0a3971b08e1a6c0b42451c50b14373de61ce03`；fetch/rebase 成功、无冲突。审核 base 仍为 `839f72119b22ce8b48811fc9819de9643a4028f8`，复审 reviewed-commit 为首轮 target `4b0a3971b08e1a6c0b42451c50b14373de61ce03`。
- 全量测试、race、build/vet/格式及 Windows 交叉编译通过；rebase 后全量复验通过，[测试输出](review-round-1-tests.txt)。Windows 原生未执行；未部署、未迁移真实看板。

- 增量 QA 原文见 [完整通知及报告](review-round-2-original.md)。按主控通知，QA 无 gate、QA-01 至 QA-08 全闭合；PM 两项 mechanical 经主控核对与实际推进测试后通过，未重跑 PM。
- QA-09/QA-10 已独立核实，详见 [增量作者处置](review-round-2.md)。QA-09 保留现有行尾策略和托管区保护；QA-10 仅精确限定首次发布时的状态要求，运行时代码、测试代码无交付变更。
- 最新完整交付：`8241a49b1f50cbb99acc68b4c3dc58b5a803253e`，已正常推送；最新组基线 `e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a`，fetch/rebase 成功、无冲突，无提交重写。
- 隔离探针、全量测试、rebase 后全量复验及 diff 检查通过，原始验证输出见 [测试记录](review-round-2-tests.txt)。PM005 恢复描述反证、PM007 范围外日志成本、Windows 原生缺口继续保留。

- 已按 [收尾通知](wrapup-notice.md) 完成独立 fetch/祖先核验：交付 `8241a49b1f50cbb99acc68b4c3dc58b5a803253e` 无重写，包含于最终组 HEAD `251f5d89186730ea372053a401136030710efe84`；后者为本地及 origin/develop 当前 HEAD。
- 第三批闭批事实、PM mechanical 通过方式、QA 增量通过及最终文档承接见 [闭批原文](batch-closure.md)。未伪造任何历史机器审核记录。
- 本卡 worktree、本地/远端任务分支和执行端临时产物已清理；组工作区/分支保留给主控。完整清单及残留分类见 [完成报告](wrapup-report.md)。
- 指定卡 check 通过；未部署、未迁移真实看板、未退出 CLI 或关闭容器。RESULT 由受控 move 的 --result completed 与完成迁移一并写入。

## SUMMARY

12/12 项作者自检满足，审核证据归档及恢复已交付并集成 develop。最终交付 `8241a49b1f50cbb99acc68b4c3dc58b5a803253e` 无重写，集成组 HEAD `251f5d89186730ea372053a401136030710efe84` 已独立核验包含于本地/远端 develop。PM mechanical 核验通过、QA 增量通过，最后单行规则承接，CSA/Hacker N/A；未冒称 Reviewer 审核最后提交。
本卡授权清理已全部完成，组及交互会话保留。QA-09 非阻断格式建议、PM007 范围外日志成本、PM005 部分拒绝的恢复描述反证、Windows 原生缺口继续保留；历史逐项判断未覆写。详见 [完成报告](wrapup-report.md)。
