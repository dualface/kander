# 20260908-bindings-recovery-group 批次 g3-batch-one 任务上下文

本批次包含 3 张卡，全部为本组成员，交付均已 ff 接收到组分支。base ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3（组创建锚点，等于当时 develop），target 0b304a8399cdbc8db2c005e390afd3f6127532af。每张卡的 OUT_OF_SCOPE 各自作为判断 finding 是否越界的边界。

---

## 任务卡 20260907-subscription-dispatch-facts-task

## GOAL

订阅用持久dispatch事实识别快速完成和待确认超时，而非等待采样边沿。

## USER_DECISIONS

用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。

后续实际决定：用户要求"拆好就开整吧"并答复"我授权"，授权本侧独立审卡、启动及按计划完成组交付/审核/develop集成与收尾；取代上方建卡阶段不启动限制。原审核归档组仍原主线程负责。

2026-09-08 用户在主会话决定："对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分." 本卡目标独立且可与派回证据绑定卡并行，保留不合并；仅因前置卡合并而改写 TASK_GROUP 与 PREREQUISITES，契约其余部分不变。

## EXPECTED_OUTCOME

本卡交付：订阅用持久dispatch事实识别快速完成和待确认超时，而非等待采样边沿。

## ACCEPTANCE_CRITERIA

- [ ] 快照/相关事件携带dispatch摘要及revision；review-working-review/done同一refresh内完成，下一快照仍证明同ID accepted/completed；重启不要求补历史边沿。
- [ ] pending review按原deadline触发注意事件和必要探测，不因其他事件续期；alive与无进展分开，输出状态/年龄，不自动恢复或放行。
- [ ] 反转 ReviewHasNoLiveness及RoundTrip中的业务回执缺失；覆盖notify返回前完成、订阅重启、待确认执行端退出和持续其他事件。
- [ ] 本行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包-race、build/vet/格式检查通过。平台相关测试使用临时目录，原生Windows及真实tmux/herdr/Agent未执行时列明缺口，不把假CLI或交叉编译记为实机通过。PM/QA按适用门禁，CSA/Hacker在本仓库N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

N事实接入E，不新增dispatch或编排真相源。

不引入通用服务、签名、保留期清理或无关优化。发现超出此单一行为的需求必须重新评估并另卡，不能为避免拆卡扩大本契约。

## DISCUSSION

```text
PREREQUISITES: 20260908-durable-dispatch-protocol-task,20260908-subscription-bounded-runtime-task
```

依赖必要性与不可拆分理由：持久派回协议（原N3，现 20260908-durable-dispatch-protocol-task）产生真实持久派回事实，订阅有界运行时（原E4，现 20260908-subscription-bounded-runtime-task）提供不会堵塞扫描的探测调度；核心订阅E1/E2/E3不反向依赖本卡。两个前置均为组外卡（20260908-subscription-dispatch-group），须 done 且交付在 develop 可用。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本行为交付的一部分，不单独拆成尾部测试任务。

资源边界：N事实接入E，不新增dispatch或编排真相源。 同组卡修改共同运行入口时串行；跨组只在资源可隔离且真实依赖满足时并行。无冲突的已就绪卡不为前卡非阻断建议等待；审核等待须注明具体门禁。本卡修改 subscribe，与同组 20260908-dispatch-evidence-binding-task（notify/board）可并行；20260908-coordinator-recovery-task 依赖本卡。

历史来源：20260907-subscription-reconcile-task；完整覆盖及主线程交接见 20260907-probe-result-classification-task 的 resplit-plan.md。原卡CARD_REVIEW不继承。

SELF_REVIEW: 通过粒度及契约自检：一个行为、可执行验收、必要依赖和范围排除已逐项核对；组内外依赖与全量映射另见计划。独立审卡现已通过，见下方真实CARD_REVIEW；由本侧主控按实际前置pick/start。

CARD_REVIEW: PASS — 2026-09-07，独立Agent /root/resplit_card_review（fork_turns=none），已只读完整契约、原需求、覆盖矩阵及粒度/必要依赖；本卡无阻断，旧PASS未继承。非阻断：资源依赖和技术依赖区分、OUT_OF_SCOPE正向范围措辞清晰、E5明确writer取消能力。仅契约通过，不代表实现/PM/QA通过。

2026-09-08 整合：原 dispatch-bindings-group 的 N4/N5 合并为 20260908-dispatch-evidence-binding-task，原 N1/N2/N3 合并为 20260908-durable-dispatch-protocol-task，原 E4/E5 合并为 20260908-subscription-bounded-runtime-task；本卡改入 20260908-bindings-recovery-group，前置随之改写，验收与范围未变。合并方案见 20260908-durable-dispatch-protocol-task 的 consolidation-plan.md。上方 CARD_REVIEW 针对旧组/前置；整合后的组关系与前置由新一轮独立审卡复核，结论另行追加。

CARD_REVIEW: PASS — 2026-09-08，独立子Agent（全新会话，只读整合后 6 张卡、12 张被合并原卡、resplit-plan.md 覆盖矩阵与 consolidation-plan.md）：目标单一且与用户 2026-09-08 决定一致，合并后验收完整覆盖原卡且可判定，前置存在、必要、无环且组内外分类正确，OUT_OF_SCOPE 与同组卡互斥。无阻断；非阻断建议（意图创建边界 kill 场景、保留“英文发布规则”字样、coordinator 第 8 项改为消费措辞、方案中 P3 并行表述）已由建卡者在相关新卡采纳修订。仅契约审查，不代表实现/PM/QA 通过。 本卡：契约与 Before 一致，前置改写为原 N3/E4 的正确承接，改组/改前置有 CONTRACT_DECISIONS 依据。


---

## 任务卡 20260908-dispatch-evidence-binding-task

## GOAL

派回绑定可核验的证据：fix 派回引用审核原件与已有原作者处置，不因卡片移动或错轮次而错派；代收尾保留原编排例外，但必须持有隔离旧 epoch 后的 wrap-up-only 专用授权，阻止仍活动的执行端与代办并行写入。

## USER_DECISIONS

2026-09-07 用户在侧会话要求按单一可验收行为重拆剩余卡，并答复"我授权"允许独立审卡、启动及按计划完成组交付/审核/develop 集成与收尾；原四张大卡由拆分卡替代，原契约与审核历史保留。

2026-09-08 用户在主会话决定："对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分." 本卡即按此决定合并而成；被合并的原卡归档为 duplicate 并指向本卡，原契约、独立审卡记录及覆盖矩阵保留不改写。合并不扩大目标，不改变 review 中 P1/P2/E1 及其组分支，不扩大集成/接管授权。

## EXPECTED_OUTCOME

本卡交付：fix 派回在发送前校验 run/finding/batch/task 归属与轮次并按相对 ID 引用作者处置；wrap-up 派回绑定已验证集成证据与同 dispatch，delivery-unknown 先对账，只有确认原执行者退出或合法回收并隔离旧 epoch 后才授予 wrap-up-only epoch。

## ACCEPTANCE_CRITERIA

- [ ] 消费持久派回协议与 R 审核类型，fix 记录关联 run/finding/batch 及任务归属；已有原作者处置按原作者与相对 artifact ID 引用，不要求尚未产生的本轮处置预先存在。board 不反向依赖 review 或 notify。
- [ ] 发送前校验引用存在、归属及轮次；错误/跨批/错误任务/不完整原件明确拒绝，不篡改或补造原作者结论。卡片 move 后按任务 ID 重定位，不保存失效绝对报告路径；同 dispatch 重试保持原绑定。
- [ ] 覆盖合法 fix、缺 run/finding、错误归属、错误前驱、已有作者处置缺失、引用卡片 move、同 ID 恢复。旧无结构报告只消费 R 的显式映射，不靠正文猜 finding。
- [ ] wrap-up 绑定已验证集成证据与同 dispatch；引用相对 ID，不缓存跨状态绝对报告路径。
- [ ] delivery-unknown 先对账；仅确认原执行者退出或在既有授权下合法回收并隔离旧 epoch 后，授予 wrap-up-only epoch；无派回记录先建意图，记录作者/原因。
- [ ] 普通非零、确认超时、租期到期不能独自证明退出；无 SESSION、未知投递、仍活动、确认退出四类验收。只允许清理及记录，拒绝代码修改和冒充原作者结论，不改变用户接管权限。
- [ ] 本卡全部行为的测试、必要英文发布规则和中文仓库文档及三语消息随实现交付；go test ./...、受影响包 -race、build/vet/格式检查通过。平台相关测试使用临时目录，原生 Windows 及真实 tmux/herdr/Agent 未执行时列明缺口，不把假 CLI 或交叉编译记为实机通过。PM/QA 按适用门禁，CSA/Hacker 在本仓库 N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

- fix 引用校验与 wrap-up 专用授权及证据绑定；不实现 dispatch 存储、传输恢复（由 20260908-durable-dispatch-protocol-task 交付）、R 的 finding 解析/处置、订阅端消费或编排收尾对账（由 20260908-coordinator-recovery-task 交付）。
- 不自动执行 Git 集成或删除真实用户工作区，不改变用户接管权限。
- 不引入通用服务、签名、保留期清理或无关优化。

## DISCUSSION

```text
PREREQUISITES: 20260908-durable-dispatch-protocol-task,20260907-review-disposition-gate-task
```

依赖必要性：持久派回协议提供实际 fix/wrap-up 派回入口、稳定 dispatch 与授权链（组外，须 done 且在 develop 可用）；R 提供可验证的 finding/作者处置与映射（已 done）。这才是 R 的真实消费点。

合并理由：原 N4（20260907-wrapup-fenced-authority-task）与 N5（20260907-fix-review-reference-binding-task）同为"派回绑定合法证据"目标，且两卡共享派回绑定接线处、原卡明确要求不能共享工作区，实际无法并行，按用户 2026-09-08 决定合并。N5 前三项与 N4 前三项验收原文保留；原 N 验收 8/10 的 fix 与 wrap-up 部分均在本卡。

并行边界：本卡与同组 20260907-subscription-dispatch-facts-task 分别修改 notify/board 与 subscribe，可并行；20260908-coordinator-recovery-task 依赖本卡。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本卡交付的一部分，不单独拆成尾部测试任务。合并后各行为可分次提交，但整卡一次交付、一次验收；实现中发现超出本卡目标的独立新行为时另卡，不为避免拆卡扩大契约。

历史来源：20260907-durable-dispatch-task（原 N 验收 8/10/12）；拆分历史见 20260907-probe-result-classification-task 的 resplit-plan.md 与 20260907-fix-review-reference-binding-task 的 execution-plan.md，合并方案见 20260908-durable-dispatch-protocol-task 的 consolidation-plan.md。原卡 CARD_REVIEW 不继承。

SELF_REVIEW: 已核对目标单一（派回绑定合法证据）、六项行为验收与原 N4/N5 一一对应、前置为真实技术依赖且无环、与同组两卡边界互斥。待独立审卡。

CARD_REVIEW: PASS — 2026-09-08，独立子Agent（全新会话，只读整合后 6 张卡、12 张被合并原卡、resplit-plan.md 覆盖矩阵与 consolidation-plan.md）：目标单一且与用户 2026-09-08 决定一致，合并后验收完整覆盖原卡且可判定，前置存在、必要、无环且组内外分类正确，OUT_OF_SCOPE 与同组卡互斥。无阻断；非阻断建议（意图创建边界 kill 场景、保留“英文发布规则”字样、coordinator 第 8 项改为消费措辞、方案中 P3 并行表述）已由建卡者采纳修订。仅契约审查，不代表实现/PM/QA 通过。


---

## 任务卡 20260908-coordinator-recovery-task

## GOAL

编排按持久事实恢复：编排必要阶段可持久读取和 CAS 更新且旧编排者不能覆盖新记录；初始快照与后续事件走同一对账，快速完成或重复事件不触发重复派回；编排重启后只有完整审核和真实集成证据才能完成同一轮收尾。

## USER_DECISIONS

2026-09-07 用户在侧会话要求按单一可验收行为重拆剩余卡，并答复"我授权"允许独立审卡、启动及按计划完成组交付/审核/develop 集成与收尾；原四张大卡由拆分卡替代，原契约与审核历史保留。

2026-09-08 用户在主会话决定："对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分." 本卡即按此决定合并而成；被合并的原卡归档为 duplicate 并指向本卡，原契约、独立审卡记录及覆盖矩阵保留不改写。合并不扩大目标，不改变 review 中 P1/P2/E1 及其组分支，不扩大集成/接管授权。

## EXPECTED_OUTCOME

本卡交付：kanban/.kander/groups/<group>/checkpoint.json 的受控读写与 coordinator epoch 单写授权；基于任务 revision、成员及 dispatch 原件的阶段恢复与去重派回；消费 A/R 原件、作者处置、required 角色、closed 批次、最终 delivery 与实际 Git 关系的收尾恢复，以及跨模块验收。

## ACCEPTANCE_CRITERIA

- [ ] 单二进制受控入口保存 kanban/.kander/groups/<group>/checkpoint.json；group_id 稳定锁，遵守 board/group/task 锁序，版本和 coordinator epoch 绑定单一有效写授权。
- [ ] 保存成员版本、交付、batch/run 相对引用、待派回/待收尾、已观察 revision；幂等更新、冲突报错，检查点不是新的任务状态或审核原件。
- [ ] 并发双编排者、旧 epoch、写入边界 kill/restart 验证旧写拒绝、已提交记录保留；检查点只存引用不推导业务通过或自动发通知。
- [ ] 读取任务 revision、成员及 dispatch 原件验证后更新检查点；accepted/completed 解除同轮待确认，错轮次/旧交付/未知成员不放行，重复相同 delivery 只验证。
- [ ] EOF/故障仅对已知可恢复事务有界处理；重复/reparse/真实损坏停止，无输出不等于失败；deadline、心跳和进展停滞处理区别明确。
- [ ] 发布规则改为 snapshot 可证明完成，删除必须见 review->working 边沿的要求；覆盖执行端/订阅端/编排端分别重启、重复丢失重排事件、同 ID 重试、外部增卡、旧编排 epoch。
- [ ] 消费 A/R 原件、作者处置、required 角色、closed 批次、最终 delivery 和实际 Git 关系；无 finding 成员不为抄写通知，机械修复与最终 HEAD 按规则承接。
- [ ] 消费 wrap-up 与 dispatch 及集成证据的绑定；快速 review-working-done 或重启仍只完成同轮；没有既有集成/接管授权时保持现状，代办只消费 20260908-dispatch-evidence-binding-task 的专用授权。
- [ ] 跨模块验收覆盖多卡部分归档恢复、move+PM/QA 归档、缺角色/全失败、错前驱/跨批假接续、旧报告人工映射、无 finding 成员、代办四类状态；不重复实现底层功能，缺陷回归所属卡。
- [ ] 核对原 13 个复现的修复证据映射，重跑 S/D 迁移 kill/旧路径回滚竞态并断言单一入口、新作者记录保留、无重复事务/dispatch/索引；POSIX/Windows 及真实 Agent 冒烟缺口单列。
- [ ] 本卡全部行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包 -race、build/vet/格式检查通过。平台相关测试使用临时目录，原生 Windows 及真实 tmux/herdr/Agent 未执行时列明缺口，不把假 CLI 或交叉编译记为实机通过。PM/QA 按适用门禁，CSA/Hacker 在本仓库 N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

- 检查点读写与授权、派回与交付事实对账、收尾消费规则及必要跨模块验收；不新增发送后端、不自动集成或代收尾、不做 Git 操作、不重写 A/R/N 接口、不部署或迁移真实看板、不扩展安全角色和 TUI。
- 不实现通用工作流引擎、通用服务、签名、保留期清理或无关优化。

## DISCUSSION

```text
PREREQUISITES: 20260907-card-write-transactions-task,20260907-subscription-dispatch-facts-task,20260908-dispatch-evidence-binding-task,20260907-review-disposition-gate-task
```

依赖必要性：S 提供事务与固定锁序（已 done）；20260907-subscription-dispatch-facts-task 提供完整持久业务事实，否则无法证明快速往返对应哪一轮；20260908-dispatch-evidence-binding-task 提供合法代收尾授权与 fix 绑定；R 提供完成证据（已 done）。后两者为同组卡，须 review/done 且交付在组分支。

合并理由：原 O1（20260907-coordinator-checkpoint-cas-task）、O2（20260907-coordinator-fact-reconcile-task）、O3（20260907-coordinator-wrapup-reconcile-task）是同一"编排按持久事实恢复"目标的严格串行链（O1→O2→O3），检查点本身无独立价值，按用户 2026-09-08 决定合并。三卡行为验收原文保留。代价：O1 原可只依赖 S 提前开工，合并后随整卡在组内最后启动；实现时可先做检查点部分。

并行边界：本卡是本组最后一张，依赖同组两卡；组内无并行对象。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本卡交付的一部分，不单独拆成尾部测试任务。合并后各行为可分次提交，但整卡一次交付、一次验收；实现中发现超出本卡目标的独立新行为时另卡，不为避免拆卡扩大契约。

历史来源：20260907-orchestration-recovery-gates-task（原 O 验收 1-14）；拆分历史见 20260907-probe-result-classification-task 的 resplit-plan.md，合并方案见 20260908-durable-dispatch-protocol-task 的 consolidation-plan.md。原卡 CARD_REVIEW 不继承。

SELF_REVIEW: 已核对目标单一（编排按持久事实恢复）、十项行为验收与原 O1/O2/O3 一一对应、四个前置为真实技术依赖且无环、与同组两卡边界互斥。待独立审卡。

CARD_REVIEW: PASS — 2026-09-08，独立子Agent（全新会话，只读整合后 6 张卡、12 张被合并原卡、resplit-plan.md 覆盖矩阵与 consolidation-plan.md）：目标单一且与用户 2026-09-08 决定一致，合并后验收完整覆盖原卡且可判定，前置存在、必要、无环且组内外分类正确，OUT_OF_SCOPE 与同组卡互斥。无阻断；非阻断建议（意图创建边界 kill 场景、保留“英文发布规则”字样、coordinator 第 8 项改为消费措辞、方案中 P3 并行表述）已由建卡者采纳修订。仅契约审查，不代表实现/PM/QA 通过。


