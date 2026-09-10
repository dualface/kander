# D 新契约首轮派回

任务：20260907-directory-card-form-task。先通过本卡现有受控入口 move working，逐项独立验证下列原始 finding 和 NON-BLOCKING 项。主控不替你确认/拒绝，不要求照抄 Reviewer 的修法。

已知事实：新契约首轮 base a7fe54beb6ade00678e14655c0d385115eb950c8、target d93f0b6e2b9f0fcd81ec7f79381cef6329321eef；当前 PM/QA 配置为 Claude/opus/high，本轮两角色均由此配置执行且 exit=0，语义均未通过。早先 Codex+opus 的 400 参数错误无语义结论，不计此轮。组已接收 target，A 未启动。

核实所有 gate 与 NON-BLOCKING；成立的 must-fix 在本卡修复，非阻断项给出处置，拒绝项须有目标提交的调用链或实测证据。PM/QA 关于 init 与 RecoverTransactions 的重复项可共用修复，但逐角色保留引用。QA 的五条非阻断项无 ID，请按原顺序/原文定位，不把它们漏掉。

合同事实须继续保持：真实重复、未知迁移产物、reparse/冲突数据不能自动选择、删除或静默放行；读命令不自动修复。核实 Reviewer 对恢复执行顺序的描述，不能为了通过意见而放松这些边界。对不移动且引用目标也不变的历史文档，区分确实需要改写与无需改写；用户授权仅调整必要链接地址并保留其他内容。后续 A 的审核原件为不可覆盖证据，本卡对 reviews/dispatches 等机器产物的访问要与现有专用生产者边界一致，不能静默预定未来篡改原件的行为。

保持用户已确认契约，不额外扩大到未来 A/R/N/E 功能。完成后按组流程提交/推送、rebase 最新组分支并重验，记录完整原文、逐项作者结论、修复 SHA 和最终交付 SHA/组基线，再 move review。你不触发审核或集成。此后的同契约复审为增量，主控会传当前 target 为 reviewed-commit。

## PM 原报告

Write is disabled in this session, so the report is delivered inline below (the review itself is read-only by design).

---

Role: PM
Commit: `d93f0b6e2b9f0fcd81ec7f79381cef6329321eef`
Task Context: `/tmp/claude-review.5c8f8cc248a88a87f4aa5ecfe9b1c774/task-spec.md`（20260907-directory-card-form-task，11 条验收，含 USER_DECISIONS 修订后的「补写 SIZE 并调整保持引用目标所必需的相对链接地址」）
Reviewed Scope: `a7fe54beb6ade00678e14655c0d385115eb950c8..d93f0b6e2b9f0fcd81ec7f79381cef6329321eef`；目录迁移与链接重定位、SIZE 消费链、事务/恢复协议、读取快照、guard-write、check 范围、启动/通知/接管/订阅消费方及相关测试与文档。未扩入 A/R/N/E；S 已关闭且未被本批加剧的事项不重复报告。CSA/Hacker N/A。

**结论：暂不通过。3 项 medium（其中 2 项 [mechanical]）。** 原子要求共 27 项：Complete 23、Partial 2、Missing 0、Contradicted 0、Unverifiable 2。

### Requirement Table

Complete 表示目标提交的静态实现证据完整，不代表本轮实机运行通过。除注明外，证据均为 Observed（直接读取工作树源码）。

| 要求 | 预期行为 | 代码证据 | Status |
|---|---|---|---|
| 复用 S 排他维护访问 | 恢复、预检、发布全程持看板排他锁 | `internal/board/migration.go:33`、`transaction_lock.go:81`、`:98` | Complete |
| 维护窗口前置条件 | 有迁移且存在 working/review 时默认拒绝并列 ID；`--maintenance` 确认 | `migration.go:75`、`:263`、`cmd.go:99`、`:110`、`i18n/locales/zh-CN.json:8` | Complete |
| 不自动终止 Agent、保留卡与数据 | 工具只记录操作方声明 | `migration.go:24`、`docs/directory-cards.md:26`；无终止调用 | Complete |
| new 目录形态与 SIZE 位置 | `backlog/<id>/spec.md`，SIZE 紧随 TYPE，默认 small、`--large` 为 large | `move.go:38`、`:45`、`templates/contract.md.tmpl:3`、`recovery.go:139` | Complete |
| 完成规模语义 | small 要 SUMMARY；large 要非空 report.md | `document.go:226`、`:230`、`:236` | Complete |
| 消费方按 SIZE | list/TUI、Agent/模型、prompt、门禁统一取 SIZE | `snapshot.go:48`、`:95`、`payload.go:59`、`list.go:141`、`launch/commands.go:86`、`:146`、`launch/prompts.go:96`、`launch/session.go:329` | Complete |
| 规模取值与回退 | 仅 small/large；旧目录缺 SIZE 按 large、旧文件按 small；check 提示 init；非法值阻止变更 | `size.go:9`、`:16`、`:40`、`deps.go:306`、`:308` | Complete |
| todo 审卡门禁 | 全部 SELF_REVIEW；large 或组成员另需 CARD_REVIEW | `document.go:202`、`:207` | Complete |
| SIZE 冻结 | todo 后冻结；退回 backlog 不解冻；显式决定可修订 | `update.go:92`、`:138`、`:186`、`recovery.go:228` | Complete |
| 七状态迁移与正文最小改动 | ID 不变、补 SIZE、只调整必要地址，其余字节/CRLF/链接文字保留 | `migration_plan.go:34`、`size.go:51`、`migration_links.go:86`、`:198` | Complete |
| 双向路径映射保持引用目标 | 源与目标双方迁移、既有目录反向引用、看板外目标均保持 | `migration_plan.go:21`、`:47`、`:63`、`:79`、`migration_links.go:36`、`:51` | Complete |
| 幂等 | 二次 init 迁移数 0，内容与 mtime 不变 | `migration_plan.go:67`、`:109`、`migration.go:70` | Complete |
| 持久事务、同卷暂存、承认中间态 | `.kander/migrations/<op>/<id>/`，分阶段发布 | `migration.go:101`、`:150`、`:189`、`migration_write.go:28`、`docs/directory-cards.md:28` | Complete |
| 中间态读取与待恢复诊断 | 锁下读者见完整结果；中断后全量/定向读取报待恢复 | `snapshot.go:31`、`:52`、`transaction.go:80` | Complete |
| 不用点前缀隐藏、不把合法产物当垃圾 | 暂存名由操作 ID 绑定，非点前缀 | `migration_write.go:13`、`migration.go:107`、`scan.go:38` | Complete |
| 各阶段失败与 kill/restart 可重做 | 14 个受控 checkpoint 接入真实发布函数 | `recovery.go:39`、`:121`、`:127`、`:238`、`:245`、`migration.go:157`、`:179`、`:186`、`:192`、`migration_write.go:73`、`:83`、`:91`、`:97`、`:103`；用例 `migration_test.go:207`、`:224`、`:295`、`migration_links_test.go:175`、`:199` | Complete |
| 真实异常保留 | 冲突、双入口、未登记暂存物、非前缀内容、reparse 报错保留现场 | `migration.go:120`、`:141`、`:164`、`:228`、`migration_write.go:45`、`:60`、`:66`、`recovery.go:314` | Complete |
| 并发协调 | 迁移/快照/update/归档共用同一锁集 | `transaction_lock.go:81`、`migration.go:33`、`transaction.go:75` | Complete |
| 旧 schema 兼容 | 旧文件、缺 SIZE 目录、中英旧 token、无 `link_relocation` 旧记录按原语义恢复 | `size.go:16`、`schema.go:104`、`migration_plan.go:23`、`migration_write.go:13` | Complete |
| 过渡期只读与副作用前拒绝 | 读命令不触发批量迁移；变更/启动/通知/遣散先要求 init | `size.go:40`、`launch/commands.go:78`、`:233`、`launch/notify_resume.go:33`、`notify/command.go:39`、`takeover/dismiss.go:46` | Complete |
| check 状态范围不扩张 | 默认继续排除 done/archived | `deps.go:296`、`board.go:23` | Complete |
| guard-write 提示 | 同状态旧 `.md` 拼写与跨状态旧路径分别提示 | `guard.go:47`、`:50`、`docs/kanban-write-guard.md:24` | Complete |
| 统一受控更新入口 | update/move 复用 S 的事务入口 | `update.go:158`、`snapshot.go:132`、`:177` | Complete |
| 消费者使用已提交正文快照 | list/TUI/依赖/订阅读同一 Scan 快照 | `snapshot.go:234`、`list.go:133`、`payload.go:106`、`deps.go:149`、`liveness/subscribe.go:143` | Complete |
| 文档与实现一致 | 规则/README/AGENTS/docs 与实现同步 | 见 PM-102、PM-103 | Partial |
| init 幂等（既有契约 `rules/KANDER-KANBAN-RULES.md:216`） | 重跑 init 只补建缺失目录并更新 exclude | `locate.go:400`、`:405`、`migration.go:59`；见 PM-101 | Partial |
| 构建与静态检查 | build/test/vet、格式与差异检查通过 | 执行端声明通过；本轮只读，未复跑 | Unverifiable |
| Windows 原生验证 | 实机句柄、junction、路径大小写与恢复 | `migration_windows_test.go:13`、`:41`；无原生执行结果 | Unverifiable |

### Findings

**PM-101 — medium — Observed：看板存在任何无效条目时 `kander init` 整体失败，即使无需迁移**

违反既有契约 `rules/KANDER-KANBAN-RULES.md:216`（“`init` idempotently creates the board and the 7 state directories; rerunning on an existing board only creates missing directories”）与 EXPECTED_OUTCOME 的“init 显式、幂等、可恢复”。

- `internal/board/locate.go:405` 把 init 的恢复入口由 `RecoverTransactions` 换成 `MigrateCards`；range 的 patch 明确替换了这一行。
- `internal/board/migration.go:59` 无条件调用 `scan(root)`，`:63` 对 `len(b.Problems) > 0` 直接 `return count, errors.New(b.Problems[0].Message)`。
- `internal/board/scan.go:57` 把状态目录下任何非 `*-task.md` 文件或非 `*-task` 目录记为 `board.invalid_task_entry`，`:70` 把缺 `spec.md` 的目录卡记为问题。这些都是项目其他部分明确容忍的状态：`scan.go:232` 的 `LoadBoard` 只打印警告并继续。
- 失败发生在 `locate.go:408` 的 `addGitExclude` 与 `:412` 的 `rulesPath` 之前，init 的非迁移职责一并中止，且只回显第一条问题文本，不给出 `kander check` 指引。

可达路径：状态目录里遗留任意一个普通文件（例如 `kanban/backlog/README.md`），或一个缺 `spec.md` 且无有效创建记录的半成品目录卡，此后每次 `kander init`（含无迁移可做的稳态看板）都以退出码 1 结束。改动前的 `RecoverTransactions`（`recovery.go:250`）不扫描条目，不存在该失败。

用户影响：一个与迁移无关的杂散文件即可让唯一的迁移/恢复入口不可用，错误信息也不足以定位。

最小修复：仅在计划非空（`migration.go:66` 之后 `len(record.Revisions) > 0`）时才因结构性问题失败，或只对参与本批迁移的 ID 相关问题失败；失败信息改为指向 `kander check`。无迁移工作时保持 init 原有的幂等成功语义。

**PM-102 — medium [mechanical] — Observed：`RecoverTransactions` 的注释声称自己是 init 的恢复入口，实际已不再被生产代码调用**

- `internal/board/recovery.go:248` 注释：“RecoverTransactions is init's explicit, idempotent roll-forward recovery.”
- `internal/board/locate.go:405` 显示 init 现在调用 `MigrateCards`，恢复逻辑内联在 `migration.go:49`。
- 全仓引用只剩测试：`internal/board/transaction_test.go:393`、`:422`、`:543`、`:561`。
- 连带后果：`recovery.go:260` 硬编码的 `requireMigrationWindow(root, records, false)` 分支在生产中不可达，对应测试验证的是用户走不到的路径。

用户影响：权威恢复入口的文档与代码不一致；后续维护者按注释接线会绕过 `MigrateCards` 的暂存校验与维护窗口检查。

最小修复：删除 `RecoverTransactions` 并把这四处测试改为调用 `MigrateCards`；若要保留该导出函数，改写注释说明它是测试/兼容入口、init 的实际入口是 `MigrateCards`。

**PM-103 — medium [mechanical] — Observed：`Entry.Kind` 的字段注释与文档声称始终表示任务规模，`scan()` 却按物理形态赋值**

- `internal/board/board.go:91`：`Kind string // Task size, independent of storage form.`
- `internal/board/size.go:5`：“IsDirectory reports storage form; Kind always represents task size.”
- `AGENTS.md:45` 与 `docs/directory-cards.md:7`：“`Entry.Kind`/`TaskSummary.kind` 表示规模, 物理形态使用 `Entry.IsDirectory()`”；`docs/card-transactions.md:68` 进一步强调记录里的 kind “不是 Entry.Kind 的任务规模”。
- 实际实现：`internal/board/scan.go:54` 对文件条目写 `kind = "small"`、`:56` 对目录条目写 `kind = "large"`，`:88` 直接放进 `Entry.Kind`；`scanTargetsOnce` 在 `scan.go:120`、`:165` 同样按形态赋值。只有 `snapshot.go:48`、`:95` 与 `transaction.go:166` 的 `attachSize` 之后 `Kind` 才代表 SIZE。对一张 `SIZE: small` 的目录卡，`scan()` 产出的 `Entry.Kind` 是 `large`。

用户影响：本轮已核对全部内部调用方（`move.go:24` 的 `newTask`、`migration.go:59`→`migration_plan.go:41`、`migration.go:288`）均未读取该值，当前无行为缺陷；但注释与文档所述不变式在包内不成立，后续按文档消费 `scan()` 结果会静默拿到形态而非规模。

最小修复：`scan()`/`scanTargetsOnce` 不再预填 `Kind`（所有导出路径都已调用 `attachSize`），或把 `board.go:91`、`size.go:5` 的注释改为“`Kind` 在 `attachSize` 之后表示任务规模”，并同步 `AGENTS.md:45`、`docs/directory-cards.md:7` 的措辞。

### NON-BLOCKING

- **PM-104 — low — Observed**：`internal/board/deps.go:302` 新增的 SIZE 扫描在任一在范围卡片正文不可读时 `return 1, "", nil, e`，`cmd.go:324` 随即 `fail(err)`。同样的不可读正文在改动前由 `deps.go:151`、`:249` 记为一条 Problem 与其他问题一起列出。触发条件是 backlog/todo/working/review/trash 中出现非 UTF-8 或不可读的 `spec.md`（`document.go:42`），此时 `kander check` 只回显这一条错误，其余问题清单与统计行丢失。最小改动：与 `dependencyProblems` 一致，把该错误追加为 `Problem` 而不是提前返回。
- **PM-105 — recommend — Observed**：`internal/board/migration_plan.go:80` 的 `migrationMarkdownFiles` 递归收录卡片目录下全部 `.md`，因此 `reviews/`、`dispatches/` 等生产者专属 Markdown 也会被迁移批次改写并计入该卡 revision；`internal/board/update.go:174` 明确禁止 Agent 经 update 写这些路径，`docs/card-transactions.md:18` 也写明它们“由专用生产者写入”。A 卡未启动、该目录当前不存在，故本轮不可达。若希望与既有边界一致，可在 `migrationMarkdownFiles` 中跳过 `reviews/`、`dispatches/` 子树，或在 `docs/directory-cards.md:41` 明说迁移同样改写生产者文档。
- **PM-106 — suggest — Observed**：`internal/board/locate.go:342` 的 `InitBoard` 在本 range 后已无生产调用方，仅 `internal/board/board_test.go:708` 与 `internal/tui/pty_test.go:159` 使用。可让这两处直接调用 `InitBoardWithOptions` 并删除包装函数。
- **PM-107 — suggest — Observed**：`internal/board/move.go:38` 已把 `target` 赋为 `filepath.Join(root, "backlog", taskID)`，`:41` 在 `large` 分支再次赋同一表达式，是文件/目录两分形态遗留的空操作。删掉 `:41` 即可。
- **PM-108 — suggest — Observed**：`go.mod:48`、`:50` 已去掉 `// indirect` 标记，但仍留在第二个（indirect）require 块内。`go mod tidy` 会把它们移入 `go.mod:5` 的直接依赖块，造成一次额外 diff。可直接移动这两行。
- **PM-109 — suggest — Observed**：迁移发布的 `spec.md` 来自 `internal/board/migration_write.go:69` 的 `fs.OpenAppendFile`，其创建模式是 `internal/fs/posix.go:335` 的 `0o666`（受 umask），而 `kander new` 与 update 走 `fs.WriteTextAtomic`（`internal/fs/posix.go:349`，`privateFileMode = 0600`，见 `internal/fs/fs.go:18`）。卡片目录本身由 `migration.go:153` 的 `CreatePrivateDirectory` 建为 0700，因此不产生越权可读，但同一张卡内 `spec.md` 与 `report.md` 的权限位不一致。可在替换发布前把 rewrite 文件 chmod 回原文模式。
- **PM-110 — suggest — Observed**：`internal/board/migration_links.go:111` 对任何非空 `srcset` 一律判为不支持，即使其中全是绝对 URL、本来无需重定位。后果是一张与迁移无关的既有目录卡即可让整批迁移在预检失败。可先对各候选执行 `relocateURL`，仅当确有目标变化时才报错。

### Verification

- 本轮为只读审核：仅使用 Read/Grep/Glob 阅读 `/home/dualf/works/kander/worktrees/20260907-review-archive-group` 工作树与 `/tmp/claude-review.5c8f8cc248a88a87f4aa5ecfe9b1c774/evidence.txt` 的 COMMIT TREE/PATCH，未修改文件、索引、引用或工作树。
- **未执行** `go build`、`go test`、`go vet`、race、gofmt 或迁移用例；本会话的 Bash/Write 工具均不可用。交付记录中的通过声明来自执行端自测，不计为 Reviewer 实测，故“构建与静态检查”记为 Unverifiable。
- Windows 原生验证仍为 Unverifiable：`internal/board/migration_windows_test.go` 带 `//go:build windows`，本轮无原生执行结果；交叉编译不视为实测通过。
- 前轮 PM-001/PM-002/QA-001 的对应实现已在目标提交中定位到证据（`migration_write.go:28` 的受管 rewrite/original 恢复；`migration_plan.go:21`、`migration_links.go:36` 的双向映射重定位；`migration_links_test.go:82`、`:148`、`:272` 的真实目标断言与二次 init 幂等断言）。这些证据为静态 Observed，实机通过仍未由本轮验证。
- 任务文件 `/tmp/claude-review.5c8f8cc248a88a87f4aa5ecfe9b1c774/prompt.txt` 未能删除：本会话处于只读计划模式且无可用的文件写入/删除工具。文件遗留不影响审核结果。


## QA 原报告

# QA Review Report

Role: QA
Commit: `d93f0b6e2b9f0fcd81ec7f79381cef6329321eef`
Task Context: `/tmp/claude-review.b6c1543a7d13cd4a5795ec0023907dbe/task-spec.md`（20260907-directory-card-form-task，11 条验收；USER_DECISIONS 已授权"补 SIZE + 调整保持引用目标所必需的相对链接地址"）
Reviewed Scope: `a7fe54beb6ade00678e14655c0d385115eb950c8..d93f0b6e2b9f0fcd81ec7f79381cef6329321eef`。以工作树 `/home/dualf/works/kander/worktrees/20260907-review-archive-group` 为实现事实来源，evidence.txt 的 FILE LEDGER / PATCH 仅作导航。覆盖 `internal/board` 迁移与链接重定位、事务/恢复、扫描与快照、SIZE 消费链（list/TUI/payload/launch/notify/takeover/liveness）、guard-write、check、i18n 资源、rules/docs。未扩入 A/R/N/E；S 已关闭且未被本批加剧的事项不重复报告。CSA/Hacker N/A。

本轮为只读审查：未运行 `go build/test/vet`，未执行迁移，未修改任何文件、索引或工作树。交付记录中的通过声明不计为 Reviewer 实测。

---

## Behavior/Quality

| 范围 | 结论与证据 |
|---|---|
| 维护访问与互斥 | Observed：`migration.go:33` 以 `LockScope{ExclusiveBoard: true}` 取排他看板锁，`transaction_lock.go:98` 其余入口取共享锁，串行化成立。`migration.go:75`、`:263` 在"确有迁移"时才要求维护窗口，`--maintenance` 仅为操作方声明（`migration.go:24` 注释与 `cmd.go:99` 一致），不自动终止 Agent。 |
| new 与 SIZE 语义 | Observed：`templates/contract.md.tmpl:4` 使 SIZE 紧随 TYPE；`move.go:37`、`:45` 写入 small/large；`document.go:230` 完成门禁按 `Entry.Kind`（SIZE），`document.go:207` large/组员要求 CARD_REVIEW。`size.go:6` 把物理形态与规模彻底分离。 |
| 规模回退与非法值 | Observed：`size.go:16` 缺 SIZE 时文件回退 small、目录回退 large；重复/非法值报 `board.size_invalid`；`size.go:40` 在副作用前拒绝旧文件与非法 SIZE，`launch/commands.go:78`、`launch/notify_resume.go:33`、`notify/command.go:39`、`takeover/dismiss.go:46` 均已接入。 |
| 消费链按 SIZE | Observed：`payload.go:59` 有 SIZE 即按 SIZE 覆盖 `Entry.Kind`；`list.go:141`、`launch/commands.go:86`、`:146`、`launch/prompts.go:96` 一致消费规模而非目录形态。 |
| 冻结与受控更新 | Observed：`update.go:92` 将 SIZE 纳入 `frozenContract`，`update.go:139` 非 backlog 变更需 contract-decision；`update.go:189` 禁止把已有 SIZE 删掉。 |
| 七状态迁移与正文保持 | Observed：`migration_plan.go:34` 单条记录覆盖全批；`size.go:51` 仅插入一行且沿用 CRLF；`migration_links.go:198` 只替换目标地址的源字节片段，链接文字/标题/代码示例不动。`migration_test.go:115`、`migration_links_test.go:84` 以真实文件断言迁移后的解析目标。 |
| 链接重定位算法 | Observed：`migration_links.go:51-63` 按引用方原位置解析、按整批映射转换、相对新位置重新生成，且在目标不变时保留原拼写；`migration_plan.go:21` 的映射持久化在记录中，恢复不从半迁移现场重建。缺陷见 QA-002。 |
| 幂等 | Observed：`migration.go:70` 无候选返回 0 且不落记录；`migration_links_test.go:136`、`migration_test.go:143` 断言二次 init 内容与 mtime 不变。 |
| 事务、暂存与恢复 | Observed：`migration_write.go:28` 用日志绑定的 `spec.write-<id>`/`spec.original-<id>` 取代随机临时名，按 After 精确前缀续写；`migration.go:141` 只接受这三个已登记文件名，未登记残留报冲突并保留（`migration_review_test.go:12` 覆盖）。14 个 checkpoint 边界在 `migration_test.go:207` + `migration_links_test.go:176` 上做失败注入与真实子进程 kill/restart。 |
| 中间态读取隔离 | Observed：`snapshot.go:58` 对含 Entries/Migrations 的 prepared 记录显式报待恢复；`snapshot.go:234` 的 `Board.Document` 让 list/TUI/依赖/订阅消费已提交快照，不混用新路径与旧正文。 |
| 异常保留 | Observed：`migration.go:120`、`:147`、`recovery.go:334` 对重复入口、冲突正文、未登记暂存物一律失败关闭并保留现场，`migration_test.go:333` 覆盖。 |
| 旧文件只读过渡与 check 范围 | Observed：读命令保留旧文件兼容；`deps.go:296` 的 SIZE 诊断沿用 `deferredCheckStates`，默认不扩张到 done/archived（`migration_test.go:515` 覆盖）。诊断退化见 NON-BLOCKING。 |
| guard-write | Observed：`guard.go:49` 新增同状态目录卡提示。旧文件卡场景的提示语义有误，见 NON-BLOCKING。 |
| init 入口整体行为 | Observed：`locate.go:405` 将 `RecoverTransactions` 换成 `MigrateCards`，恢复与迁移合并到同一入口。由此引入 QA-001、QA-003。 |
| 文档与实现一致性 | Observed：`docs/directory-cards.md`、`docs/card-transactions.md`、`rules/KANDER-KANBAN-RULES.md:472` 与实现的 schema 字段、阶段名、暂存路径、计数语义逐条对得上。例外见 QA-003。 |
| i18n 资源 | Observed：`board.migrated`/`migration_maintenance`/`size_missing`/`size_invalid`/`migration_link_unsupported`/`guard_form_changed`/`migration_required` 七个新键在 zh-CN/en/ja 三份 locale 中齐全且占位符一致。 |
| 文件大小门禁 | Observed：本批新增/修改的非生成文件最大为 `board_test.go` 714 行，`migration.go` 302 行，均未触及 1000 行规则。 |
| 构建与静态检查 | Unverifiable：只读沙箱未运行 build/test/vet/race，交付记录中的通过声明不作为 Reviewer 实测。 |
| Windows 原生验证 | Unverifiable：`migration_windows_test.go:13`、`:41` 已就位但需 Windows 实机执行；交叉编译不等于实测，缺口保留。 |

---

## Gate Findings

**QA-001 — medium — Observed：`kander init` 现在因看板中任何既有结构问题而整体失败，连"无需迁移"和事务恢复也被一并阻断**

- **违反要求：** 验收第 1 条"保持既有卡与数据"、第 6 条"init 可恢复"；`rules/KANDER-KANBAN-RULES.md:397` 明确 "Run `kander init` explicitly to finish prepared writes and moves"。
- **代码证据：**
  - `internal/board/migration.go:59-65`：`b, err := scan(root)`，随后 `if len(b.Problems) > 0 { return count, errors.New(b.Problems[0].Message) }`。
  - 该门禁位于 `internal/board/migration.go:70` 的 `if len(record.Revisions) == 0 { return count, nil }` **之前**，因此与"是否真的需要迁移"无关。
  - `internal/board/locate.go:405`：`InitBoardWithOptions` 已把原先的 `RecoverTransactions(root)` 换成 `MigrateCards(root, options)`；基线 `RecoverTransactions`（`recovery.go:250`）不做任何 scan，因此这是本 range 引入的新行为。
  - `internal/board/scan.go:57`：状态目录下任何不匹配 `-task.md` / `-task` 的直接子项都会进入 `Problems`（`board.invalid_task_entry`）。
- **可达场景：** 看板里存在一个与任务无关的散落文件，例如 `kanban/backlog/notes.md`（或一张历史上目录卡缺 `spec.md`、一组真实重复 ID）。此后 `kander init` 一律以 `无效的任务入口: .../backlog/notes.md` 退出 1，既不再创建/更新 `.git/info/exclude`，也不再完成任何 prepared 事务重做——而 init 正是文档指定的唯一恢复入口。全部卡片早已是目录形态、迁移数本应为 0 的看板同样被拒。
- **影响：** 迁移完成后的日常 `init`（安装向导重跑、事务恢复）在一个与迁移毫无关系的历史遗留物面前变成不可用；恢复路径被同一个门禁挡住，用户只能手工清理看板文件后重试。错误只暴露 `Problems[0].Message`，不带"init 已中止/请先跑 kander check"的上下文，也不列出其余问题。
- **最小可靠修复：** 把 `Problems` 门禁移到 `len(record.Revisions) == 0` 提前返回之后（即只在确有迁移工作时才要求看板结构干净），或把检查缩小到参与本批计划的条目；同时用 `kanbanError` 包装并提示改跑 `kander check`，而不是裸 `errors.New(b.Problems[0].Message)`。补一个用例：看板含一个非卡片散落文件且全部卡片已是目录形态时，`init` 仍返回 0 且迁移数为 0。

**QA-002 — medium — Observed：迁移预检对"本来不需要重定位"的文档也报错，单张无关历史卡即可阻断整批迁移**

- **违反要求：** 验收第 5 条要求 init 覆盖七状态完成迁移；预检失败关闭本身合理，但对目标本就不变的文档失败关闭没有契约依据，且与同一函数内 HTML href/src 的处理自相矛盾。
- **代码证据：**
  - `internal/board/migration_plan.go:63` 与 `:96`：既有目录卡的 `spec.md` 与目录内 Markdown 附件都以 `from == to` 调用 `relocateMarkdown`，只要看板上还存在任意一张旧文件卡（`mapping` 非空）就会被完整解析。
  - `internal/board/migration_links.go:48-50`：`strings.ContainsAny(u.Path, "\\\x00")` 无条件返回 `non-portable relative path`；`:41-44` 的 `url.Parse` 失败同样无条件返回错误。二者经 `:170-172` 变成 `board.migration_link_unsupported`，最终由 `migration_plan.go:64` 冒泡到 `MigrateCards`。
  - `internal/board/migration_links.go:122-137`：`ast.Text` 中任何一行同时出现 `[[` 与 `]]` 即判定为 wiki 链接并整批失败，不区分该写法是否指向文件。
  - `internal/board/migration_links.go:111-113`：`srcset` 非空即失败，即便其中全是绝对 URL。
  - 对照 `internal/board/migration_links.go:115-118`：HTML `href`/`src` 只在 `next != attr.Val`（确实需要重定位）时才报错——同一函数内已经存在"只在需要改写时才拒绝"的正确判据，其余分支没有采用。
- **可达场景：** 看板里有一张 2 个月前的 `archived/20260701-x-task/report.md`，正文含 `见 [构建日志](logs\build.txt)`（Windows 上手写）或散文里的 `依据 [[RFC-7]] 定义`。这张卡本身早已是目录卡、位置不变、所有链接目标也不会变。只要 backlog 里还有一张待迁移的旧 `.md`，`kander init --maintenance` 就以 `无法安全迁移相对链接（.../report.md）：non-portable relative path` 整批失败，一张卡都迁不了。由于 `check` 默认排除 done/archived（`deps.go:296`），操作方在迁移前不会收到任何预警。
- **影响：** 全库一次性迁移这一主流程被一张与迁移无关的历史卡片阻断；修复要求操作方去编辑 archived/done 的历史正文，而这些正是最不应被改动的记录。数据未损坏，但主流程不可用直到人工干预。
- **最小可靠修复：** 在 `relocateURL` / wiki 与 srcset 检测中沿用 HTML 分支已有的判据——当引用方文档不移动（`from == to`）且该目标不在 `mapping` 中、因而解析结果不会改变时，保留原样而不报错；只有真正需要改写地址却无法安全改写时才失败关闭。补一个用例：既有目录卡的附件含反斜线路径 / `[[...]]` 散文 / 绝对 URL srcset，同批仍能完成旧文件卡迁移且该附件字节不变。

**QA-003 — medium [mechanical] — Observed：`RecoverTransactions` 的文档注释与实现不符，且已无生产调用方**

- **代码证据：**
  - `internal/board/recovery.go:248-250`：注释写 "RecoverTransactions is init's explicit, idempotent roll-forward recovery."
  - `internal/board/locate.go:405`：`InitBoardWithOptions` 实际调用的是 `MigrateCards(root, options)`；`cmd.go:110` 的 `RunInit` 也只经这条路径。
  - 全仓引用只剩 `internal/board/transaction_test.go:393`、`:422`、`:543`、`:561`（测试）；无任何生产调用方。
- **影响：** 维护者按注释会认为 init 的恢复语义由 `RecoverTransactions` 定义，从而在错误的函数上做恢复相关改动；`MigrateCards` 与它的门禁顺序也已不同（`migration.go:42` 先校验暂存后查窗口，`recovery.go:260` 相反），注释掩盖了这一分叉。
- **最小可靠修复：** 二选一——把注释改为陈述其现状（"仅供恢复相关测试使用的独立 roll-forward 入口，init 走 `MigrateCards`"），或删除该导出函数并让测试直接使用 `MigrateCards`。

未发现其他 gate findings。

---

## NON-BLOCKING

- **low — `guard-write` 对"目录内部路径 + 尚未迁移的旧文件卡"给出语义错误的提示。** `internal/board/guard.go:49-54` 移除了原来的 `if other == state { continue }`，于是当写入 `kanban/backlog/<id>/plan.md` 而卡片仍是 `kanban/backlog/<id>.md` 时，循环会命中 `:55` 的旧文件分支并返回 `board.guard_task_moved`，提示"任务 X 已在 backlog/ 中, 原路径写入会产生跨状态副本"——但根本没有跨状态，真正的原因是该卡尚未迁移。拒绝结果正确，只是诊断误导。最小改动：在 `:55` 分支中区分 `other == state`，改用 `board.migration_required` 的提示语。

- **low — `CheckBoard` 新增的 SIZE 扫描把不可读文档从"问题列表"降级为硬错误，丢失聚合诊断。** `internal/board/deps.go:302-305`：`text, e := board.Document(...); if e != nil { return 1, "", nil, e }`。此前不可读正文由 `dependencyProblems`（`deps.go:149`）收集进 `documentErrors` 并与其余问题一并列出；现在 `kander check --all` 在第一张非 UTF-8 卡片上直接中止，其余问题不再输出。最小改动：把该错误追加成 `Problem` 并 `continue`，与同函数中其余问题走同一条汇总路径。

- **low — `addSize` 在没有 TYPE 行的卡片上把 SIZE 插到 H1 标题之前。** `internal/board/size.go:60-69`：找不到 `- TYPE:`/`- 类型:` 时走 `return line + text`，产出 `- SIZE: small\n# 标题\n...`，与验收第 2 条"SIZE 行在 TYPE 后"以及模板排版不一致。触发条件是缺 TYPE 的畸形旧卡（这类卡本来也过不了 todo 门禁），后果仅是排版。最小改动：无 TYPE 时插到首个 `# ` 标题行之后而非文首。

- **suggest — `newTask` 中 `if large` 分支的 `target` 赋值是无效重复。** `internal/board/move.go:38-44`：`target := filepath.Join(root, "backlog", taskID)` 之后，`if large` 分支再次赋同一个值。这是"两种形态"改成"统一目录"后遗留的空操作，会让读者误以为 large 有不同落盘路径。最小改动：删除该行，只保留 `taskKind = "large"`。

- **suggest — `validateRecord` 中两段互相包含的前置校验。** `internal/board/recovery.go:280-293`：`len(r.Migrations) > 0` 分支里的 `len(r.Entries) > 0 || len(r.Directories) > 0 || len(r.Groups) > 0` 被紧随其后的 `r.Purpose == "migration"` 分支完全覆盖（前者已要求 `Purpose == "migration"`），三个条件永远不会成为独立的拒绝原因。最小改动：第一段只保留 `r.Purpose != "migration"` 的检查。

---

## Verification

- 证据来源：工作树 `/home/dualf/works/kander/worktrees/20260907-review-archive-group` 的实际文件内容；evidence.txt 的 COMMITS / FILE LEDGER / PATCH 仅用于定位改动边界。未执行 git 命令，因此不声称做过 blob hash 比对。
- 本轮未运行 `go build ./...`、`go test ./...`、`go vet ./...`、race 或任何迁移；相关结论标记为 Unverifiable。执行端的自测通过声明单独记录，不并入本轮结果。
- Windows 原生验证缺口保留：`migration_windows_test.go` 的句柄/junction/路径大小写用例需在 Windows 实机执行，交叉编译不计为实测。
- 任务文件 `/tmp/claude-review.b6c1543a7d13cd4a5795ec0023907dbe/prompt.txt` 未能删除：本轮仅有 Read/Grep/Glob 只读工具，无删除能力。文件遗留不影响审核结果。
