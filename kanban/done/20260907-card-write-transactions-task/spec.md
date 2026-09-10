# Card write transactions, version checks and legacy-path rollback deduplication

- TYPE: Bug
- SIZE: large
- TASK_GROUP: 20260907-review-archive-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 14:18
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:tQ:wX:p19
- STARTED_AT: 2026-09-07 14:47
- FINISHED_AT: 2026-09-07 22:12
- TASK_BRANCH: card-write-transactions
- RESULT: completed

## GOAL

建立按任务 ID 的统一读写事务，阻止迁移后旧路径被回写或回滚重建；为目录迁移、审核归档、结论和派回记录提供同一套并发与恢复接口。当前 window.WriteDocument/RestoreWindowText 使用缓存路径和允许创建的 replace 写入，已复现同 ID 跨状态副本；guard-write 的检查与实际写入也不原子。

## USER_DECISIONS

本轮用户要求结合订阅、重复卡及 co-work 三卡给出完整方案，创建新卡并更新已有卡。本轮仅规划，不启动实现。具体锁、版本和命令设计是技术方案，不冒充用户逐项选择。

## EXPECTED_OUTCOME

- 任务 ID 为身份，路径在锁内重新定位；过期版本/执行授权/回滚显式冲突，不覆盖新记录、不重建旧卡。
- 所有 Kander 写卡路径复用同一事务 API；Agent 有受控正文与附件更新入口，后续卡消费该接口。
- 持久操作记录使多文件更新和进程崩溃可诊断、可恢复；不引入服务或数据库。

## ACCEPTANCE_CRITERIA

- [ ] 按统一 plan.md 定义锁顺序：看板访问锁后按排序任务 ID 加锁；涉及组控制记录时按看板、组、任务的固定顺序。迁移排他，单卡 mutation 排他，跨文件读取取得任务共享锁或已提交版本快照；锁/版本/恢复记录位于不随卡移动的 kanban/.kander/，POSIX/Windows 均经 internal/fs。并发 new 同 ID、move 与回写、两个回滚、多个任务批量写入无死锁、无副本。
- [ ] 提供单调 revision、操作 ID、预期状态/版本校验及只更新现存卡片的语义。禁止用一次 exists 检查实现所谓并发安全。旧路径已消失时写入失败，不能创建旧小卡或旧目录根。
- [ ] 新增 kander update <id> --document <relative-path> --file <UTF8-input> --expect-revision <revision>，并为 show 提供含 revision 的机器可读形式；所有新命令仍由 cmd/kander 单二进制和 cli 注册表分发。
- [ ] 受控更新覆盖 spec/plan/report/普通附件；禁止路径逃逸和 reparse；创建任务入口只允许 new；禁止全文替换绕过冻结字段、受管 OWNER/SESSION/WINDOW/RESULT、机器索引及控制记录。保留任务语言，不把更新操作变成重建卡片。
- [ ] 受管字段必须有合法写入口：move working 的手工认领模式可记录 OWNER/STARTED_AT；move done --result completed 在验证通过后原子写 RESULT/FINISHED_AT；archive/trash 模式记录既有规则要求的 result、原因及用户决定引用。start/resume 仍管理 SESSION/WINDOW。update 的 --contract-decision-file 在预期 revision 下记录用户明确决定与原/新冻结字段后才允许契约修订，不提供无理由强制覆盖；引用只记录授权依据，不声称工具能验证用户意图。测试手工认领、完成、终止及已授权 todo 契约修订全链路，不能因禁直接编辑造成无入口。
- [ ] new/move/pick/start/resume/notify/dismiss、WINDOW 回写及失败回滚全部接入。异步操作不得缓存绝对路径到结束后盲写；回滚只撤销仍属于本操作的字段/状态，出现新 revision 时保留现场并报冲突。
- [ ] 定义对后续多文件产物发布的事务接口和恢复记录格式：已提交数据可见，未完成操作能通过 init 明确完成/回退；读者不自动修复，也不把受管事务中间态当普通缺卡。S 交付通用接口，文件转目录的迁移步骤由 D 实现。
- [ ] 兼容当前 small 文件卡与 large 目录卡：旧文件卡的 update --document spec.md 映射到锁内定位的现存 .md 正文，支持填写契约和 SUMMARY；其余附件要求目录卡。专用命令均防旧路径复活，不得在 D 交付前切断 new small 后合法填写/完成的路径。
- [ ] 将两项复现反转为回归：RestoreWindowText 对迁走的小卡不得产生副本；受控 update 与 move 竞争必须一方成功另一方冲突或串行成功。再覆盖陈旧全文回滚覆盖新正文、并发索引追加以及各事务阶段 kill/restart。
- [ ] 规则明确 Agent 用受控入口写卡，guard-write 是辅助检查，不宣称覆盖任意 shell/外部工具；真实重复保留双方报错，不猜主副本，不自动删除。
- [ ] 更新 board/window/launch/notify 的职责、命令协议及对应 i18n；go test ./... 通过，相关并发包 -race 通过；Windows 锁/reparse/恢复必须有对应测试和实际执行结果或明确环境缺口。

## THREAT_MODEL

资产为看板正文、任务唯一性和执行端新记录。可信主体是遵守协议的本机命令与 Agent；保护误写、并发、崩溃和 reparse 越界，不承诺防御任意本机进程绕过工具直接改文件。

## OUT_OF_SCOPE

- 既有问题：旧路径回滚重建、过期全文覆盖、跨状态同 ID 创建竞态均在范围；其他 TUI/配置问题排除。
- 加固：锁、revision、事务恢复是达成并发保证的必要实现；文件签名和恶意本机写入隔离不做。
- 共享契约与文档：通用写入 API、update/show 协议、受管字段、回滚与恢复格式归本卡；SIZE 迁移、reviews 和派回 schema 由后续卡负责。
- 相邻功能：不在本卡实现审核门禁或订阅重放；不添加数据库、守护进程或第二命令入口。

## DISCUSSION

```text
PREREQUISITES: N/A
```

统一设计与八卡依赖见本卡 plan.md。S 是原组新增前置卡；原 D/A/R 的 ID 与用户业务目标保留。接口必须先落到 board/fs，board 不反向依赖 review/notify。实现时如发现额外入口必须纳入同一协议，不能只修 window 的一个调用点。

SELF_REVIEW: 通过。已核对用户目标、范围、接口与八卡依赖；独立卡审指出的受控写入口、batch/run 提交语义、代收尾授权三项问题已修订并复核关闭。旧版 PASS 不继承。

CARD_REVIEW: PASS — 2026-09-07，独立 Codex 子 Agent /root/integrated_card_review（未继承建卡会话上下文），首次审查及修订后增量复核，八卡均无剩余阻断。完整原文见 20260907-card-write-transactions-task 的 card-review.md；仅为契约审查，不代表实现或 PM/QA 代码审核已通过。

## IMPLEMENTATION

- 执行工作树：`/home/dualf/works/kander/worktrees/card-write-transactions`，任务分支 `card-write-transactions`。
- 来源：`origin/group/20260907-review-archive-group`，创建基线 `4889d3fb93f8d2d639832b9da5588d668714d9bc`；无前置卡依赖。
- 实现：board 统一按看板、排序组、排序任务取得锁；internal/fs 提供私有稳定锁文件、共享/独占锁；revision、操作 ID、冻结状态及 prepared/committed 重做记录位于 `.kander` 控制目录。多文件及组控制发布共用同一事务接口。
- 命令：show --json、update 的 UTF-8 输入/CAS、受管字段与机器产物保护、带用户决定记录的契约修订；手工认领、完成和终止通过 move 专用参数原子写受管元数据。
- 兼容：现存小卡 spec.md 映射保留；window/launch/notify 的原有调用入口使用操作局部版本游标；旧回滚遇到新版本或路径变化失败，launch 原文与原状态在同一回滚事务中恢复。
- 恢复：init 显式完成 prepared 记录；读者只报待恢复，不自行修复；真实重复及恢复内容冲突保留现场。
- 已执行验证：`go test ./...` 全部通过；`go test -race ./internal/fs ./internal/board ./internal/window ./internal/launch ./internal/notify ./internal/liveness` 全部通过。随后增加恢复格式与 UTF-8 校验，最终提交前重新验证。
- Windows：六个相关包的 windows/amd64 测试程序及完整二进制编译通过。当前主机为 Linux，无 Windows/Wine，LockFileEx、DACL/reparse 与 kill/restart 用例尚未在原生 Windows 执行；不得把交叉编译算作实机通过。
- 审核：本轮执行端不触发组审核；交付后由编排端运行 PM/QA，CSA/Hacker 按仓库规则 N/A。
- 当前运行的已安装版本尚无 update；本卡记录按本轮加载的既有命令契约，通过 show 重定位和 guard-write 检查后只打开现存 spec.md 更新。未部署新二进制、未做形态迁移。

- 最终交付：`f056af9b75bb981e99d6f071fc04f36988b6cef2`；本地与远端任务分支一致，均已正常推送。
- 最新组基线：`4889d3fb93f8d2d639832b9da5588d668714d9bc`；fetch/rebase 无变更，随后全量测试和六包 -race 再次通过。
- 最终 Windows 编译：六包测试程序及完整 windows/amd64 二进制均通过，原生执行缺口保留。
- 最终恢复回归：七个子进程 kill/restart 边界通过，包含目录卡携带新附件迁移后的恢复。

## REVIEW_ROUND_1

PM 两项、QA 四项均已逐项复核并修复；详情和各修复 SHA 见 [首轮派回处置](review-round-1.md)，[完整原 finding](review-round-1-findings.md) 保留。

最新最终交付：`a7fe54beb6ade00678e14655c0d385115eb950c8`；最新组基线：`f056af9b75bb981e99d6f071fc04f36988b6cef2`。已正常推送，rebase 无冲突，随后全量与七包 race 通过；Windows 七包测试程序及二进制交叉编译通过，原生环境缺口保留。

## SUMMARY

11/11 项实现自检完成；事务接口及首轮派回修复已通过 PM/QA 第一批第二轮增量复审，CSA/Hacker N/A。六项 finding 全部关闭，原文和逐项历史判断保留，闭批证据见 [第二轮原文](review-batch-1-closed.md)。

本卡最终交付 `a7fe54beb6ade00678e14655c0d385115eb950c8` 无重写，已随最终组 HEAD `251f5d89186730ea372053a401136030710efe84` 集成到本地与远端 develop，主工作树已同步；执行端独立 fetch 和祖先核验通过。任务工作树、本地/远端任务分支已清理，组资源由主控保留。完整审核批次、集成证据和清理结果见 [收尾记录](wrapup.md)。

全量、七包 race、Windows 七包测试程序及二进制交叉编译已通过；主控在最终组 HEAD 的全量测试也通过。未解决项 1：Windows 原生执行仍缺环境，交叉编译不替代原生。当前无未闭合 finding 或非阻断残留。代码位于 develop，RESULT completed，按授权进入 done，保留交互 CLI 与终端等待用户决定 dismiss。
