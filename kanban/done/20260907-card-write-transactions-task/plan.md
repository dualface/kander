# 卡片存储、审核证据与编排恢复的统一方案

本轮范围：结合 co-work 的三张审核归档卡和本会话发现的订阅、通知、重复卡问题，更新规划与任务契约；不启动实现、不迁移现有看板、不部署。用户已明确要求创建新卡和更新已有卡，因此原 todo 卡的契约调整有本轮授权。

## 保留的用户决定

- 所有新卡最终统一为目录，正文为 spec.md，规模由 SIZE: small|large 表达。
- 审核原始结果和执行端的逐条验证结论随卡保存，正文只留索引。
- 保留原三卡的 ID 与任务组 20260907-review-archive-group，不另建替代卡。
- 保留目录表示状态、单二进制 cmd/kander、internal/fs 的跨平台边界、既有语言与配置兼容。
- 本仓库 CSA/Hacker 为 N/A；PM/QA 按适用规则运行。建卡审查与实现后的角色审核是不同步骤。

以下并发协议、命令接口、证据格式是本轮技术方案，不冒充用户逐项确认过的决定。

## 任务与依赖

| 简称 | 任务 ID | 直接依赖 | 交付 |
| --- | --- | --- | --- |
| S | 20260907-card-write-transactions-task | 无 | 按任务 ID 的写入事务、版本与受控更新 |
| D | 20260907-directory-card-form-task | S | 目录卡、SIZE、可恢复迁移 |
| A | 20260907-review-report-archive-task | D | 审核运行证据、归档及索引 |
| R | 20260907-review-disposition-gate-task | A | 原作者结论、批次闭环、完成门禁 |
| P | 20260907-probe-liveness-bounds-task | 整个前组 done 且交付可用 | 探测分类、期限、取消 |
| N | 20260907-durable-dispatch-task | P | 派回 ID、接收/完成、执行轮次 |
| E | 20260907-subscription-reconcile-task | N | 持久事实订阅、动态成员、独立心跳 |
| O | 20260907-orchestration-recovery-gates-task | E | 恢复检查点、规则与全链路验收 |

S/D/A/R 属于既有 20260907-review-archive-group；P/N/E/O 属于新增 20260907-orchestration-reliability-group。两组均四卡，依赖无环。共享 board、规则和 i18n 修改按依赖串行，避免承诺无冲突并行。前组按既有 Git 规则独立集成并完成后再启动后组。

## 一、身份与写入

任务 ID 是逻辑身份；绝对路径是当次定位结果，不能用作跨异步操作的身份。状态仍由状态目录决定，控制元数据不得另建一个冲突的 status 真相源。

在 kanban/.kander/ 下保存不随卡迁移的锁、revision、事务恢复记录。board 提供统一入口：取得看板访问锁，再按排序后的任务 ID 取得锁，在锁内重新定位、检查预期 revision/执行轮次，再提交。涉及组控制记录时固定顺序为看板、组、任务。迁移使用看板独占锁；普通读写使用看板共享锁，单卡写入取得任务独占锁，跨文件读取取得任务共享锁或已提交版本快照。internal/fs 补齐所需 POSIX/Windows 句柄和锁能力；不引入守护进程、数据库或第二个二进制。

新增受控正文/附件更新入口，方案命令为：
`kander update <task-id> --document <relative-path> --file <UTF8-input> --expect-revision <revision>`。
show 的机器可读形式提供 revision 与当前路径。update 允许 spec.md、plan.md、report.md 及普通说明附件；创建任务入口只能 new，reviews 的机器索引/manifest 和派回控制记录由专用逻辑生成。任务组、SIZE 等冻结字段及 OWNER/SESSION/WINDOW 等受管字段不能借全文替换绕过门禁。S 的兼容期，旧 small 文件卡也可 update --document spec.md，逻辑正文名映射到当前 .md 文件，支持完整填写契约和 SUMMARY；其余附件要求目录卡。D 发布后再按迁移协议限制旧形态写入，不能在 D 之前切断合法建卡流程。

revision 是单任务已提交变更的单调版本。事务要绑定任务 ID、预期版本、操作 ID 与受管文件；冲突报错不覆盖。生命周期回滚只撤销本操作仍拥有的字段/状态，不能拿早先全文覆盖执行端的新记录。更新现存卡片必须拒绝缺失目标，不得 mkdir 旧卡根或在原状态重建文件。一次 exists 检查或单纯原子 rename 不能代替并发协议。

禁止任意改受管字段不等于取消合法入口：move working 的手工认领模式记录 OWNER/STARTED_AT；move done --result completed 原子完成验证、结果与时间；archive/trash 模式记录所需原因和用户决定引用；start/resume 管理会话窗口。update --contract-decision-file 在 revision 校验下记录用户明确决定和冻结字段差异，支持合法重规划，禁止无理由 force。任务语言继续按既有冻结约定，不由全文编辑改变。

所有 Kander 写入口必须接入，包括 new/move/pick/start/resume/notify/dismiss、WINDOW 回写、归档和结论写入。Agent 规则改为用受控入口改卡；guard-write 保留为误写提示，明确它不覆盖 shell 或 Kander 内部写入，也不能保证检查与写入原子。对绕过协议的任意本机写入不承诺防篡改；发现重复保留双方并报冲突，不猜哪份正确。

## 二、目录迁移

D 消费 S 的锁与事务恢复接口。禁止将“单文件移入暂存目录再发布”描述成无中间状态的单次原子操作。迁移用持久事务记录和可恢复阶段保证：正常读者不读取半成品；崩溃后 init 能依据记录完成或回退；缺记录的异常文件不得猜测删除。

迁移必须阻止并发迁移/受控写入。working 卡不在执行端仍活动时被静默迁移；默认拒绝活动 working 的形态迁移，明确列出需要先停写、确认执行端已静止的卡。review 卡也需确保无在途通知、归档或写操作。不能靠“迁完打印新路径”替代互斥。旧 Agent 的直接文件编辑不受锁保护，所以显式维护窗口是迁移前置条件。

迁移保存正文与附属文件，补写 SIZE，覆盖所有七状态，重复执行无变化。读者遇到带恢复记录的未完成事务应报告可恢复状态/要求 init，不能报成普通缺卡后永久结束编排；读命令不自动执行修复写入。旧单文件只读兼容，迁移后的写命令提示明确。

## 三、审核证据

A 的 run_id 标识一次 Reviewer 运行，batch_id 标识固定成员集合、base 和审核要求的逻辑批次；run 的目标 commit 不可变，批次 target_commit 可在接收本批修复交付后通过 CAS 显式推进，并记录旧/新 commit 与依据。不得因修复 HEAD 变化就另开 batch，也不得混入本批以外的交付。时间戳用于显示和统计，不承担唯一性、先后链或幂等身份。PM/QA 同轮针对同一 target_commit 运行，有不同 run_id。

报告、输入、sidecar 不可覆盖，sidecar 分开记录工具执行状态与审核结论，进程退出 0 绝不等于审核 PASS。文件按 run_id 绑定；元数据保存所有原件哈希、前驱 run_id、卡片/批次绑定及语言。报告语言在运行意图建立时从卡片 LANGUAGE 或当时配置解析并冻结，后续配置变化不影响原运行。记录缺失、格式错误、归档不完整不产生“通过”。

Reviewer 启动前持久化运行意图及输入身份；运行过程输出进入受控暂存证据。只有进程回收、worktree 校验等门禁完成后才发布成功运行记录。失败、进程中断、清理失败同样留可诊断证据；崩溃恢复不能编造最终报告，标为 interrupted/incomplete。不带 --task 保持独立审核行为。

长时间审核不占卡片写锁；结束后按 ID 重定位，验证批次/运行意图，再事务追加归档与索引，允许卡片在期间迁移。不得写入启动时缓存的路径。多卡归档采用每卡原子提交和共同 run_id，逐卡记录完成情况；失败保留已发布证据，重试同 run_id 补齐缺项，不能重复启动 Reviewer。发布清单为权威，正文 REVIEWS 为受控索引；不完整运行不能进入完成门禁。

## 四、结论作者与审核闭环

原第三卡的 A/B 分歧改用“原作者记录 + 工具汇总”。执行 Agent 仅为自己的 finding 提交原文、判断、依据和修复 SHA，保存到自己的 reviews；编排 Agent 验证并引用这些记录，不改写作者判断。Kander 校验引用后机械生成批次结论并分发到成员卡，无 finding 的卡无需仅为抄写结论而被派回。编排端可以记录自己的审核验证意见，但必须保留作者与来源，不冒充执行端结论。

每个 finding 以 run_id + finding_id 唯一标识，增量轮有显式 lineage，不能扫描任意正文中出现的 ID 就推断报告条目。新格式要求清晰的 findings/non-blocking 结构与唯一 ID；无结构的旧报告保留原件并标记需要人工映射，禁止用“没找到 ID”或空结论自动过门禁。

review plan 保存实际适用角色、N/A 原因、批次成员和目标版本。新执行周期不能因没有索引就绕过 required 角色；全部失败运行也不构成“没有运行过所以放行”。旧完成卡可标记 legacy-untracked 保持可读，不伪造历史通过；升级后的活动周期需补齐审核要求。

batch closed 只能由所有适用角色的有效结论、未解决项及交付证据共同确定，后批 base 绑定前批 closed commit，而非任意角色的一次 pass。mechanical 修复的 PASSED_AT 必须有适用规则允许的证据，不能借字段自行提升已审核范围。Git 祖先/当前 HEAD 校验放在 review/编排路径，board 只复用纯结构校验；最终集成授权和 Git 交付验证继续保留。

## 五、持久派回与执行身份

N 在发送之前持久化 dispatch_id、task_id、kind(fix/sync/wrap-up)、消息哈希、所依据的 delivery/review 版本、deadline。终端回显及退出码 0 只是投递信息，不是业务 ACK。

接收端通过 `move working --dispatch-id <id>` 原子记录 accepted；完成时用对应 ID 提交交付 SHA/结果并进入 review 或 done。记录随卡保留，即使 review-working-review 在一秒内完成，下一次快照仍能识别完成轮次。同 ID 同 payload 重试幂等，换 payload 报冲突；旧轮次和过期执行 epoch 不得写入新轮次。

区分 prepared、delivery-unknown、accepted、completed、failed/cancelled 等业务事实；“投递可能发生但没有确认”不得归为确定失败后新建派回轮次。传输允许重试，接收端通过持久 CAS 去重，不宣称任意 Agent 副作用全局 exactly-once。每卡同时只有一个有效执行/派回授权；恢复不得与仍有效的持有者并行写卡，更换 Agent 仍遵守用户接管授权。

保留原有编排代收尾例外，但只适用于清理和记录。不确定投递先对账，普通非零码/确认期限/租期到期不能证明原执行端已停；确认已退出或在既有授权下回收并隔离旧 epoch 后，受控操作才授予 wrap-up-only epoch，绑定同一 wrap-up dispatch 和实际集成证据，记录代办作者与原因。不代修代码、不改原作者结论，无派回记录时先创建受控 wrap-up 意图。

## 六、存活与订阅

P 将“唯一找到、确认无匹配、歧义、探测失败”分开；只有确认无匹配才据此 stopped。unknown 保留错误，drifted 携带新地址。alive 只描述会话可探测，运行状态和最后观测时间独立给出。context 贯穿探测/反查，设置整轮期限与并发上限，回收子进程和输出管道。

E 保留 JSON Lines 和 tasks 状态表，增加版本化的 task revisions、派回事实、成员版本和观测时间。进程内 seq 用于诊断，不是重放游标。snapshot/state-change/task-update/membership-change/heartbeat 等事件都触发按当前持久事实对账，不要求逐次状态转换必达。

心跳与存活截止时间独立于状态变化；待确认 review 卡也受确认期限监控。静默期间不要求 Agent 自行短轮询。--watch 组引用按成员版本重新展开，新增成员产生可消费事件；组成员无法完整确定时显式报告，不静默省略。真实重复、reparse、持久结构损坏仍失败关闭；已知事务瞬态走明确的可恢复路径。

扫描不被慢探测或慢消费者无限拖住；输出有队列/时间上限，超限明确结束并允许新订阅从快照恢复。取消必须释放相关 goroutine、子进程及管道。有限正数 interval 还须验证 time.Duration 可表示范围。心跳携带 liveness 的 freshness，不能把旧结果当成当前事实。

## 七、编排恢复与验收

O 将编排阶段保存为检查点：group、成员版本、已接收 delivery、batch、待派回、待收尾、观察到的 revisions。检查点位于 kanban/.kander/groups/<group-id>/checkpoint.json，复用 S 的锁/事务原语，遵守看板、组、任务顺序。它不是状态目录中的组卡，也不替代任何原件真相。持久事实是恢复依据，事件只负责唤醒。事件处理幂等；相同交付已包含时只复核，相同派回不重发，新快照能确认 accepted/completed。

本轮共确认 13 个隔离复现：快速往返、心跳饥饿、反查误报、review 监控缺口、成员集合冻结、不完整成员展开、阻塞输出、interval 溢出、慢探测/取消、后代管道拖延、回显假 ACK、内部回滚复活小卡、写前检查竞态。任务验收应将这些坏行为反转为回归断言，而非照抄原测试的“复现成功即通过”。

端到端追加：同卡并发归档与 move；迁移各阶段 kill/restart；派回先完成后返回；编排/订阅/执行端重启；归档多卡部分成功补齐；required 角色缺失或全部失败；旧结论引用错轮次；外部组加修复卡；无 finding 成员无需派回；收尾消息与集成证据绑定。POSIX 和 Windows 均覆盖对应文件/锁/进程行为，真实 tmux/herdr 冒烟如环境缺失必须如实记录，假命令测试不冒称实机通过。

## 分工与发布约束

S 负责通用存储/更新/回滚；D 负责形态与迁移；A 负责证据发布；R 负责结论及门禁；P 负责采集分类和取消；N 负责通知及执行轮次；E 负责事件流；O 负责消费协议、检查点与跨模块验收。各卡同步自己改变的命令与 schema 文档，O 做全链路一致性复核，不代替前卡必要文档。

通用检查为 go test ./...；涉及并发的包运行有实际竞态意义的 -race 用例；新增注释、提交和 rules/*.md 为英文，用户字符串覆盖 cn/en/ja。不改变 models/kanban_agents 既有 schema，不把看板或运行证据提交进 Git。

阶段发布时，依赖尚未交付的未来协议不能被规则要求为可用命令。S 保持现有存储形态兼容；D 启用目录迁移；A/R 启用审核证据；P/N/E/O 依次启用可靠编排。所有永久规则/实现修改由对应任务执行时交付，本轮只保存计划和卡片。
