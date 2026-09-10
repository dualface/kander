# D 新契约增量复审处置

任务：20260907-directory-card-form-task。PM/QA 已全部退出，工具 exit=0；目标 83f73544ef1f612a97f99d384e9386eeecb78752，两角色均无 gate finding，组工作区干净。base 仍为 a7fe54beb6ade00678e14655c0d385115eb950c8。以下保留两个完整原报告。

请先 move working，独立验证所有新增 NON-BLOCKING 项并记录处置，主控不替你确认/拒绝。PM-111..114 和 QA 三条无 ID 项按原角色/顺序保留；空行重复项可共用处置。low/recommend/suggest 不阻断审核或集成，不要求为可选建议扩张范围；若修复，保持最小范围并按实际改动验证。QA-001 主体门禁已闭合，当前仅 low 残留，勿将其虚报为原 medium 未闭合。继续保留 PM-106/109 原作者判断及此前部分 reviewer 断言不成立的证据。

保存本轮原文与逐项结论到本卡及报告，明确有无代码变化和最终交付完整 SHA，再按组流程 move review。若代码变化则提交/推送、对齐组 HEAD 并验证；若无变化保持交付 SHA。不要扩展用户契约，不触发审核、集成或清理。Windows 原生验证缺口继续保留。A 暂等你本次处置完成。

## PM 原报告

# PM Review Report（增量复审）

Role: PM
Commit: `83f73544ef1f612a97f99d384e9386eeecb78752`
Task Context: `/tmp/claude-review.39ddf64522d65f1a72eb4d767eb284b5/task-spec.md`（20260907-directory-card-form-task，11 条验收，含 USER_DECISIONS 修订后的「补写 SIZE 并调整保持引用目标所必需的相对链接地址」）
Reviewed Scope: 增量复审。已审基线 `d93f0b6e2b9f0fcd81ec7f79381cef6329321eef`，本轮只审修复区间 `d93f0b6..83f7354`（`6d44715`、`83f7354` 两个提交，22 个路径，见 FIX RANGE FILE LEDGER）。逐项核实上轮 PM-101~PM-110 的处置，并只针对修复引入/加剧/掩盖的问题报告新 finding；`d93f0b6` 以来未变更的代码按已接受处理，不重审。`a7fe54b..83f7354` 全量范围仅作上下文。

**结论：通过。0 项 gate finding。** 原子要求共 27 项：Complete 25、Partial 0、Missing 0、Contradicted 0、Unverifiable 2（与上轮同一套分解；上轮 2 项 Partial 均已转 Complete）。

---

### 上轮 finding 逐项核实

| ID | 上轮结论 | 本轮状态 | 证据（工作树 @ `83f7354`） |
|---|---|---|---|
| PM-101 | medium — 任何无效条目使 `kander init` 整体失败 | **已关闭**（按作者更严格的处置） | 恢复入口前移：`migration.go:38` 先 `recoverMigrationRecords`，`:42` 才 `scan`，故 prepared 事务重做不再被结构问题挡住（`recovery.go:262`）。结构门禁改为 `migration.go:50` 的 `migrationStructure(root, b, len(record.Revisions) > 0)`：`migration_structure.go:19` 在「无迁移且无 Blocked」且全部问题都是普通散落正规文件（`:23-26` 用 `fs.RegularFileExists` 且排除 `-task.md` 尾缀）时只 `:31` 告警放行，init 继续走 `locate.go:408` 的 `addGitExclude`；否则 `:29` 以 `board.migration_structure` 失败并列出全部问题、指向 `kander check`（`i18n/locales/zh-CN.json:2`）。回归见 `migration_new_review_test.go:12`、`:32`、`:101`。缺 spec/真实重复/未知暂存物在迁移数为零时仍拒绝，属作者明示处置，并已写入契约 `rules/KANDER-KANBAN-RULES.md:486` 与 `docs/directory-cards.md:20`，不再作为 finding。 |
| PM-102 | medium [mechanical] — `RecoverTransactions` 注释与实现不符、无生产调用方 | **已关闭** | `recovery.go:248-249` 注释已改为「compatibility entrance… Init uses MigrateCards; both share the same locked recovery core」；`:256` 委托 `recoverMigrationRecords`，与 `migration.go:38` 共用同一暂存校验/维护窗口/重做顺序（`recovery.go:267`、`:270`、`:274`），不再存在两份恢复实现或不可达的窗口分支。采用的正是上轮给出的两个最小修复之一。 |
| PM-103 | medium [mechanical] — `Entry.Kind` 注释/文档声称表示规模，`scan()` 按形态赋值 | **已关闭** | `scan.go:88`、`:120`、`:165` 不再写 `Kind`；`board.go:91` 与 `size.go:5` 注释改为「attachSize 后才表示规模」；`AGENTS.md:45`、`docs/directory-cards.md:7` 同步。已核对全部生产消费方仍取到规模：`snapshot.go:48`/`:95`、`transaction.go:166` 均先 `attachSize`；`document.go:207`/`:230` 只经 `snapshot.go:200`，`list.go:141`、`payload.go:59` 只经 `Scan`（`payload.go:100`、`:122`）。`transaction.go:105`、`recovery.go:139` 等读的是记录里的 `storageKind`（`transaction.go:311`），不受影响。回归 `migration_new_review_test.go:69`。 |
| PM-104 | low — SIZE 扫描把不可读正文升级为硬错误 | **已关闭** | `deps.go:303-305` 改为 `continue`，由 `dependencyProblems`（`deps.go:151`→`:249`）汇总一条 Problem，统计行与其余问题保留。回归 `migration_new_review_test.go:55` 断言两份非 UTF-8 正文产出 2 条 problem 且 summary 非空。 |
| PM-105 | recommend — 迁移改写 `reviews/`、`dispatches/` 生产者文档 | **已关闭（采纳）** | `migration_plan.go:133` 递归时跳过 `managedDocumentPart` 子树；`:167-171` 让 `validateMigrationFiles` 拒绝这些路径的回放写入；判据与 `update.go:184` 共用 `update.go:156` 的 `managedDocumentPart`。边界写入 `docs/directory-cards.md:33` 与 `rules/KANDER-KANBAN-RULES.md:486`（历史原件字节不变）。回归 `migration_link_scope_test.go:57`。 |
| PM-106 | suggest — `InitBoard` 无生产调用方 | **保留（作者判断）** | `locate.go:342` 仍为零选项包装并直接委托 `locate.go:348`，本 range 未改动，无运行语义损害；作者已说明不采纳删除。不再重复提出。 |
| PM-107 | suggest — `newTask` large 分支重复赋值 | **已关闭** | `move.go:38` 后不再有第二次 `target` 赋值（`:39-43` 仅设 `taskKind` / 追加 small 段）。 |
| PM-108 | suggest — `go.mod` 直接依赖留在 indirect 块 | **已关闭** | `go.mod:14`、`:15` 已移入首个 require 块，第二块（`:21-51`）只剩 `// indirect` 项。 |
| PM-109 | suggest — 迁移发布的 `spec.md` 与 `WriteTextAtomic` 叶文件模式不一致 | **保留（作者判断）** | 相关代码本 range 未改动；外层 stage 仍由 `migration.go:136` 的 `CreatePrivateDirectory` 建为 0700，无跨用户可读缺陷。不再重复提出。 |
| PM-110 | suggest — 非空 `srcset` 一律判为不支持 | **已关闭** | `migration_links.go:117-121` 改为按 `srcsetNeedsRelocation` 判断；`migration_link_scope.go:52-87` 逐候选（含 `data:` URL 的逗号处理，`:68`）调用 `relocateURL`，仅在确有目标变化或报错时才判不支持。回归 `migration_link_scope_test.go:10`（全绝对 URL 与 data URL srcset 保留）与 `:31`（真正引用迁移卡的 srcset 仍失败）。 |

与 PM 项共用修复的跨角色项一并核实：QA-001 见 PM-101 的同一证据；QA-003 见 PM-102；QA-002 的判据落在 `migration_links.go:43`、`:52`、`:142` 与 `migration_link_scope.go:11-47`（引用方不移动且可能目标不在 mapping 时保留原字节），归 QA 角色关闭。

---

### Requirement Table

Complete 表示目标提交的静态实现证据完整，不代表本轮实机运行通过。除注明外，证据均为 Observed（直接读取工作树源码）。未被修复区间触及的要求沿用上轮已核实结论，此处只列本轮受影响项。

| 要求 | 预期行为 | 代码证据 | Status |
|---|---|---|---|
| init 幂等（既有契约 `rules/KANDER-KANBAN-RULES.md:216`，本轮细化于 `:486`） | 重跑 init 只补建缺失目录并更新 exclude；普通散落文件不阻断 | `locate.go:400`、`:405`、`:408`、`migration.go:50`、`migration_structure.go:19`、`:31` | Complete |
| 文档与实现一致 | 规则/README/AGENTS/docs 与实现同步 | `board.go:91`、`size.go:5`、`AGENTS.md:45`、`docs/directory-cards.md:7`、`:20`、`:33`、`:41`、`rules/KANDER-KANBAN-RULES.md:486`、`:487` | Complete |
| init 显式、幂等、可恢复 | 待恢复记录先于结构扫描重做，不被无关问题挡住 | `migration.go:38`、`recovery.go:250`、`:262`、`:274`；回归 `migration_new_review_test.go:32` | Complete |
| 消费方按 SIZE（`Entry.Kind` 语义） | 公开快照 Kind 表示规模，结构扫描不冒充规模 | `scan.go:88`、`snapshot.go:48`、`:95`、`transaction.go:166`、`payload.go:59`、`list.go:141`、`document.go:207`、`:230` | Complete |
| 七状态迁移与正文最小改动 | 补 SIZE、只调整必要地址，其余字节/CRLF/链接文字保留 | `size.go:51`、`:60`、`:71`、`migration_links.go:174`、`:210` | Complete |
| 双向路径映射保持引用目标 | 需重定位才拒绝不支持语法；不移动且目标不变的历史正文保留 | `migration_links.go:43`、`:52`、`:117`、`:142`、`migration_link_scope.go:11`、`:52` | Complete |
| 真实异常保留 | 冲突、双入口、未登记暂存物、reparse 仍报错保留现场 | `migration.go:105`、`:126`、`:225`、`migration_structure.go:24`、`recovery.go:292`、`:346` | Complete |
| 统一受控更新入口 | 生产者专属路径不被迁移改写，也拒绝回放写入 | `migration_plan.go:133`、`:167`、`update.go:156`、`:184` | Complete |
| check 状态范围不扩张 / 聚合诊断 | 默认继续排除 done/archived；不可读正文汇总为 Problem | `deps.go:298`、`:303`、`:327` | Complete |
| guard-write 提示 | 同状态旧 `.md`、跨状态旧路径、目录内部路径分别提示 | `guard.go:50`、`:56`、`:59`；`i18n/locales/zh-CN.json:8` | Complete |
| 构建与静态检查 | build/test/vet、格式与差异检查通过 | 执行端声明通过；本轮只读，未复跑 | Unverifiable |
| Windows 原生验证 | 实机句柄、junction、路径大小写与恢复 | `migration_windows_test.go:13`、`:41`；无原生执行结果（caller 明示缺口保留） | Unverifiable |

---

### Findings

无。修复区间未引入、加剧或掩盖 blocking/high/medium 级问题，也未发现前述修复破坏其所触及的要求。

---

### NON-BLOCKING

- **PM-111 — low — Observed：`board.migration_structure` 在本来就没有迁移的场景仍称「迁移已中止」。** `internal/board/migration.go:50` 以 `len(record.Revisions) > 0` 传入 `changes`；当 `changes` 为 false 但 `b.Blocked` 非空（例如已全量迁移的看板里有一个缺 `spec.md` 的 `<id>/` 目录，`internal/board/scan.go:71` 会写入 `Blocked`），`internal/board/migration_structure.go:29` 仍返回 `board.migration_structure`，文案为「迁移已中止；请运行 kander check 检查结构问题」（`internal/i18n/locales/zh-CN.json:2`，en/ja 同义）。此时并无迁移可做，用户看到的因果被错置；补救指引本身正确。最小改动：按 `changes` 分别使用「迁移已中止」与「init 已中止」两条文案，或把文案改为不预设迁移语境的措辞。

- **PM-112 — low — Observed：无迁移时的结构告警多输出一个空行。** `internal/board/migration_structure.go:31` 用 `fmt.Fprintln` 输出 `board.kander_warning_ignored_invalid_entries_run_kander_check_for`，而该 locale 值本身已以 `\n` 结尾（`internal/i18n/locales/zh-CN.json:75`，en/ja 同）。同一条消息在 `internal/board/scan.go:234` 的既有调用点用的是 `os.Stderr.WriteString`。后果是 `kander init` 在告警后多打一个空行，与 `LoadBoard` 的输出不一致。最小改动：改用 `os.Stderr.WriteString`，与既有调用点一致。

- **PM-113 — low — Observed：缺 TYPE 的旧卡若首个 `# ` 行位于围栏代码块内，SIZE 会被插进代码示例。** `internal/board/size.go:71-80` 在找不到 `- TYPE:`/`- 类型:` 时按 `strings.SplitAfter(text, "\n")` 取第一条 `# ` 前缀行插入，不区分该行是否在 ``` 围栏或缩进代码块中。对正文 "```\n# sample\n```\n\n# 标题\n" 会产出 SIZE 行落在围栏内部，改变了代码示例；`docs/directory-cards.md:39` 对链接重写明确承诺「代码行内/围栏/缩进示例不变」。触发条件是缺 TYPE 的畸形旧卡（这类卡本来也过不了 todo 门禁），后果限于排版与示例内容。新增回归 `internal/board/migration_new_review_test.go:93` 只覆盖了三种 H1 在首行的正文，未覆盖该分支。最小改动：改用已有的 Goldmark 解析（`migration_links.go:98`）取首个 `ast.Heading` 的源码区间，或在扫描时跳过围栏区域；无 H1 时沿用现在的文首补行。

- **PM-114 — recommend — Observed：`guard-write` 新增的「未迁移旧文件卡」提示未同步到其专用文档。** `internal/board/guard.go:56-58` 为「写目录内部路径而卡片仍是同状态旧 `.md`」新增 `board.migration_required`（「旧文件卡 {0} 仅可读取；请暂停写入并运行 kander init 后再变更」，`internal/i18n/locales/zh-CN.json:8`），但 `docs/kanban-write-guard.md:24-27` 的判定表只列了「目录卡已存在→提示 spec.md 路径」与「目录卡入口不存在→写入会静默重建整个目录卡」两种理由，`docs/directory-cards.md:45` 也只写了「同状态旧 .md 拼写及跨状态旧路径」。拒绝结论本身与文档一致，缺的是这条新提示的说明；验收第 11 条要求规则/docs 与旧卡过渡同步。最小改动：在 `docs/kanban-write-guard.md:27` 的表中补一行「目录内部文件, 该 ID 仍是同状态旧 `.md`｜拒绝, 提示先运行 `kander init`」，并在 `docs/directory-cards.md:45` 补一句。

---

### Verification

- 本轮为只读增量审核：仅使用 Read/Grep/Glob 阅读工作树 `/home/dualf/works/kander/worktrees/20260907-review-archive-group` 与 `/tmp/claude-review.39ddf64522d65f1a72eb4d767eb284b5/evidence.txt` 的 FIX RANGE COMMITS/FILE LEDGER/PATCH 及 COMMIT TREE，未修改文件、索引、引用或工作树，未执行 git 命令。
- 审查边界：只覆盖 `d93f0b6..83f7354` 变更的 22 个路径及其直接调用方（用于判断 `Entry.Kind` 留空、`migrationStructure` 门禁、链接判据变更的影响面）。`d93f0b6` 以来未变更的实现按上轮结论承接，未重新审计。
- **未执行** `go build`、`go test`、`go vet`、race、gofmt 或任何迁移用例；本会话无 Bash/Write 工具。交付记录中的通过声明来自执行端自测，不计为 Reviewer 实测，故「构建与静态检查」仍为 Unverifiable。新增的 `migration_link_scope_test.go`、`migration_new_review_test.go` 与其依赖的 helper（`migration_test.go:16`、`:209`，`migration_links_test.go:51`、`:55`、`:82`、`:156`）已逐一定位存在，但未编译或运行。
- Windows 原生验证缺口按 caller 指示保留：`internal/board/migration_windows_test.go` 带 `//go:build windows`，本轮无原生执行结果；交叉编译不视为实测通过。
- 任务文件 `/tmp/claude-review.39ddf64522d65f1a72eb4d767eb284b5/prompt.txt` 未能删除：本会话处于只读计划模式，无可用的文件写入/删除工具。文件遗留不影响审核结果。

## QA 原报告

# QA Review Report

Role: QA
Commit: `83f73544ef1f612a97f99d384e9386eeecb78752`
Task Context: `/tmp/claude-review.6283bf13fc385add6d53f86f3cc86935/task-spec.md`（20260907-directory-card-form-task，11 条验收；USER_DECISIONS 授权"补 SIZE + 调整保持引用目标所必需的相对链接地址"）
Reviewed Scope: 增量复审。修复范围 `d93f0b6e2b9f0fcd81ec7f79381cef6329321eef..83f73544ef1f612a97f99d384e9386eeecb78752`（evidence.txt 的 FIX RANGE LEDGER/PATCH 为导航），完整范围 `a7fe54be..83f7354` 仅作上下文。实现事实来源为工作树 `/home/dualf/works/kander/worktrees/20260907-review-archive-group`。仅核验本角色上轮 QA-001/QA-002/QA-003 及 5 条 QA NON-BLOCKING 的处置，以及修复引入/加剧/掩盖的问题；`d93f0b6` 起未变更的代码按已接受处理，不重审。CSA/Hacker N/A。

本轮只读：未运行 `go build/test/vet`，未执行迁移，未修改任何文件、索引或工作树（本会话仅有 Read/Grep/Glob）。

---

## 上轮 finding 处置核验

| ID | 上轮结论 | 目标提交状态 | 证据（Observed） |
|---|---|---|---|
| QA-001 | medium：init 因任何结构问题整体失败，恢复亦被阻断 | **部分修复**（主体闭合，残留降为 low） | 恢复已前置：`migration.go:38` 先 `recoverMigrationRecords`，`recovery.go:262-286` 在任何 `scan` 之前完成 prepared 重做；结构门禁后移到 `migration.go:50` 的 `migrationStructure(root, b, len(record.Revisions) > 0)`，`migration_structure.go:19` 在"零迁移 + 无 Blocked + 全部问题项为普通非卡片文件"时只警告并 `return nil`，init 继续走 `locate.go:408` 的 `addGitExclude`；错误路径改为 `kanbanError("board.migration_structure", …)`，三份 locale（`zh-CN.json:2`、`en.json:2`、`ja.json:2`）均含 `kander check` 指引。回归 `migration_new_review_test.go:12`、`:32`。**残留**见 NON-BLOCKING 第 1 条 |
| QA-002 | medium：对本来不需要重定位的文档报错，单张无关历史卡阻断整批 | **已闭合** | 新增 `migration_link_scope.go:11` 的 `unsupportedTargetMoves`：引用方不移动（`migrationPathKey(from) == migrationPathKey(to)`）且解析目标不在 `mapping` 中时返回 false。三处接入：`migration_links.go:43`（无效 URL）、`:52`（反斜线路径）、`:142`（Wiki `[[…]]` 改为按 `tail[:end]` 判定）。srcset 由 `migration_links.go:117-121` 调用 `migration_link_scope.go:52` 的 `srcsetNeedsRelocation` 按候选逐个判断，绝对/data URL 保留。反向仍失败：`from != to` 一律返回 true，映射命中（含 `target+".md"` 与裸名 Wiki 回退，`migration_link_scope.go:34-44`）也返回 true。回归 `migration_link_scope_test.go:10`（archived `report.md` 字节与 mtime 不变，同批仍完成迁移）与 `:37`（反斜线/Wiki/srcset 真正引用迁移卡时仍在发布前失败且旧卡未被改动） |
| QA-003 | medium [mechanical]：`RecoverTransactions` 注释与实现不符 | **已闭合** | `recovery.go:248-249` 注释改为"compatibility entrance for recovery without new migrations. Init uses MigrateCards; both share the same locked recovery core."，与 `locate.go:405`、`recovery.go:256` 一致；两入口共用 `recoverMigrationRecords`（`recovery.go:262`），暂存校验、维护窗口与重做顺序不再有两份实现（`migration.go:38` 与 `recovery.go:256` 调用同一函数）。`docs/directory-cards.md:20` 同步说明。原注释掩盖的门禁顺序分叉已消除 |
| NON-BLOCKING 1（low，guard-write 同状态旧文件卡提示语义错误） | low | **已闭合** | `guard.go:55-58`：命中同状态旧 `.md` 时先判 `other == state` 并返回 `board.migration_required`，跨状态才用 `board.guard_task_moved`；`zh-CN.json:8` 的文案指向 `kander init`。回归 `migration_new_review_test.go:86` |
| NON-BLOCKING 2（low，CheckBoard 丢失聚合诊断） | low | **已闭合** | `deps.go:302-306` 由 `return 1, "", nil, e` 改为 `continue`，不可读正文回到 `dependencyProblems`（`deps.go:145-155`、`:246-253`）按 taskID 去重聚合一次。回归 `migration_new_review_test.go:55` 断言两张非 UTF-8 卡产生 2 条 problem 且 summary 非空 |
| NON-BLOCKING 3（low，addSize 无 TYPE 时插到 H1 之前） | low | **已闭合** | `size.go:69-80`：无 TYPE 时按行定位首个 `# ` 标题并插到其后，沿用 `\n`/`\r\n`，无 H1 才回落文首。回归 `migration_new_review_test.go:93` 覆盖三种换行/无末尾换行 |
| NON-BLOCKING 4（suggest，`newTask` large 分支重复赋值） | suggest | **已闭合** | `move.go:37-43`：`target` 只在 `:38` 赋值一次，`if large` 分支仅设 `taskKind` |
| NON-BLOCKING 5（suggest，`validateRecord` 包含式校验） | suggest | **已闭合** | `recovery.go:292-305`：第一段只校验 `r.Purpose != "migration"`，`Entries/Directories/Groups` 的禁止统一由 `r.Purpose == "migration"` 分支承担，拒绝边界不变 |

---

## Behavior/Quality

| 范围 | 结论与证据 |
|---|---|
| init 恢复与结构门禁的顺序 | Observed：`migration.go:38` → `recovery.go:262`（`operationRecords` → `validateMigrationStaging` → `requireMigrationWindow` → 重做 prepared）→ `migration.go:42` `scan` → `:46` `planMigration` → `:50` `migrationStructure`。有效 prepared 记录的重做不再受无关结构问题影响，未知暂存物（`migration.go:211`）与维护窗口仍先于重做拒绝，语义未被放宽。`migration_new_review_test.go:32` 验证"缺 spec 目录存在时事务先提交、目录保留、随后才报结构失败"，`:101` 验证未登记暂存物在零迁移时仍拒绝 |
| 结构问题的"无害"判据 | Observed：`migration_structure.go:19` 要求 `!changes && len(b.Blocked) == 0`，`:23-26` 要求每个 `Problem.Path` 都是普通文件且文件名不以 `-task.md` 结尾。因此缺 `spec.md`（`scan.go:71` 写入 Blocked）、重复 ID（`scan.go:83`，第二条目路径必为目录或 `-task.md`）、reparse（`scan.go:43`，`fs.RegularFileExists` 经 `posix.go:229` 的 `requireRegular` 返回错误）均仍失败关闭。与 `docs/directory-cards.md:20` 的措辞一致 |
| `Entry.Kind` 语义与消费链 | Observed：`scan.go:88`、`:120`、`:162` 不再预填 Kind；公开出口 `snapshot.go:48`、`:95` 与 `transaction.go:166` 经 `attachSize` 填规模。核对全部读取方：`list.go:141`（`collectRows` 在 `:133-136` 已对不可读正文提前返回，不会渲染空 Kind）、`payload.go:59`（入参来自 `Scan`，`:105`/`:126`）、`document.go:207`/`:230`（仅由 `snapshot.go:200` 用 `tx.Snapshot` 的已附加 Entry 调用）、`launch/commands.go:86`/`:146`/`:311`、`launch/notify_resume.go:75`、`launch/agent.go:174` 均消费 `TaskSummary`/公开快照。事务记录的形态编码走独立字段（`transaction.go:311` 的 `s.Entry.storageKind()`、`move.go:52`），未受影响。回归 `migration_new_review_test.go:69` 同时断言结构扫描留空与公开快照为 `small`。`board.go:91`、`size.go:5`、`AGENTS.md:45`、`docs/directory-cards.md:7` 四处措辞与实现一致 |
| 生产者子树边界 | Observed：`migration_plan.go:133` 在递归收集前跳过 `managedDocumentPart` 目录，`:167-171` 在 `validateMigrationFiles` 中拒绝任何含受管路径段的回放写入；判据与 update 入口共用同一函数（`update.go:157`、`:184`）。回归 `migration_link_scope_test.go:64` 覆盖"计划不含 reviews/dispatches"、"原件字节不变"与"伪造受管写入的记录被 `validateRecord` 拒绝"。`rules/KANDER-KANBAN-RULES.md:487`、`docs/directory-cards.md:33` 同步。注：审核/派发历史正文中指向旧 `.md` 的链接因此保持原样（作者显式处置为"历史证据不可变"），`reviews/` 目录当前不存在，本轮不可达 |
| 链接重定位的判据一致性 | Observed：HTML `href`/`src` 沿用"只在 `next != attr.Val` 时拒绝"（`migration_links.go:123-126`），Wiki/无效 URL/反斜线/srcset 现在共用等价判据，上轮指出的同函数内自相矛盾已消除。判据方向安全：任何"引用方移动"或"可能目标在映射内"仍失败关闭，不做猜测改写 |
| 幂等与正文保持 | Observed：`migration_link_scope_test.go:19-27` 断言无关历史文档字节与 mtime 不变；`migration_links_test.go:136` 的二次 `MigrateCards` 断言迁移数 0 且四份文档内容/mtime 不变，该断言在新增用例结尾（`migration_link_scope_test.go:28`）复用 |
| check 范围与聚合 | Observed：`deps.go:296-301` 仍按 `deferredCheckStates` 排除 done/archived，`continue` 不改变范围；SIZE 诊断（`deps.go:307-311`）与依赖/契约问题走同一 `allProblems` 汇总（`deps.go:327-345`） |
| i18n | Observed：新增键 `board.migration_structure` 在 `zh-CN.json:2`、`en.json:2`、`ja.json:2` 齐全，占位符一致；`board.migration_required` 复用既有键（`en.json:8`）无新增 |
| 依赖清单 | Observed：`go.mod:14-15` 的 `goldmark`/`golang.org/x/net` 已移入直接依赖块，第二块只余 `// indirect` 项，`go mod tidy` 不再产生额外 diff |
| 文件大小门禁 | Observed：修复范围新增/修改的非生成文件最大为 `recovery.go` 390 行、`deps.go` 348 行；新增 `migration_link_scope.go` 87 行、`migration_structure.go` 33 行。无文件跨越 1000 行 |
| 构建与静态检查 | Unverifiable：本会话只读，未运行 build/test/vet/race/gofmt；交付记录中的通过声明不计为 Reviewer 实测 |
| Windows 原生验证 | Unverifiable：`migration_windows_test.go:13`、`:41` 带 `//go:build windows`，本轮无原生执行结果；交叉编译不视为实测。缺口按调用方要求保留 |

---

## Gate Findings

**未发现新的 gate finding。** 修复范围未引入、加剧或掩盖 blocking/high/medium 级别问题，也未破坏被修复代码所触及的既有要求（恢复顺序、暂存校验、维护窗口、受管路径边界、SIZE 冻结与消费链均经上表逐条核对）。

---

## NON-BLOCKING

- **low — QA-001 残留：状态目录下的非常规文件条目（普通目录、非卡片命名目录）在零迁移时仍整体阻断 `init`。** `internal/board/migration_structure.go:23-26` 的"无害"判据要求 `fs.RegularFileExists` 为真，而 `internal/board/scan.go:57-59` 对状态目录下任何不匹配 `-task.md`/`-task` 的直接子项统一记为 `board.invalid_task_entry`——包括普通目录。可达场景：`kanban/backlog/attachments/` 或 `kanban/backlog/20260101-x-task-backup/`（后者不以 `-task` 结尾，不是半成品卡）存在时，即使全部卡片已是目录形态、迁移数为 0，`kander init` 仍在 `migration.go:50` 返回 `board.migration_structure` 并跳过 `locate.go:408` 的 exclude 更新。相比上轮，恢复路径已不再受阻、错误信息已指向 `kander check`，故影响显著下降。最小改动：把"无害"判据从"是普通文件"改为"该 Problem 属于 `scan.go:57` 的非卡片条目分类且非 reparse"（例如在 `scan.go` 生成 Problem 时标注该分类），使散落目录与散落文件在零迁移时同等处理，`-task` 命名的半成品、重复 ID、reparse 与 Blocked 仍失败关闭。该边界与 `docs/directory-cards.md:20` 的"普通非卡片散落文件"措辞自洽，owner 也可按现状接受。

- **suggest — `migration_structure.go:31` 的警告多输出一个空行。** `fmt.Fprintln(os.Stderr, t("board.kander_warning_ignored_invalid_entries_run_kander_check_for", …))`，而该文案本身已以 `\n` 结尾（`internal/i18n/locales/zh-CN.json:75`、`en.json:75`、`ja.json:75`）；同一文案在 `internal/board/scan.go:234` 用 `os.Stderr.WriteString` 输出。后果仅是 `init` 的 stderr 多一个空行。最小改动：改用 `os.Stderr.WriteString`（或 `fmt.Fprint`），与 `LoadBoard` 保持一致。

- **suggest — `addSize` 的新回归断言未钉住插入位置。** `internal/board/migration_new_review_test.go:93-98` 只断言 `strings.HasPrefix(got, "# Title")` 与 `strings.Contains(got, "SIZE: small")`；若实现改为把 SIZE 追加到文末，该用例仍会通过，而验收第 2 条要求的是 SIZE 紧随 TYPE/标题。最小改动：改为逐例精确期望，如 `"# Title\nbody\n"` → `"# Title\n- SIZE: small\nbody\n"`、`"# Title\r\nbody\r\n"` → `"# Title\r\n- SIZE: small\r\nbody\r\n"`。同一函数把 guard-write 判定与 `addSize` 排版两个无关行为合在一个用例中，拆分后失败定位更直接。

---

## Verification

- 证据来源：工作树 `/home/dualf/works/kander/worktrees/20260907-review-archive-group` 的实际文件内容；`/tmp/claude-review.6283bf13fc385add6d53f86f3cc86935/evidence.txt` 的 FIX RANGE COMMITS/FILE LEDGER/PATCH 仅用于确定本轮改动边界。未执行 git 命令，故不声称做过 blob hash 比对。
- 复核方式：对 FIX RANGE FILE LEDGER 中的 22 个路径逐一读取目标提交版本，并沿调用链追到未变更的消费方（`list.go`、`payload.go`、`document.go`、`launch/*`、`transaction.go`、`deps.go`）确认 `Entry.Kind` 留空、`CheckBoard` 的 `continue`、生产者路径排除三处改动没有产生下游行为回归。未变更代码只用于判断修复影响，未另行审计。
- 与作者处置的关系：PM-101/QA-001 共用修复中，作者对"缺 spec、真实重复、reparse 无论迁移数是否为零都拒绝"的判断已在 `migration_structure.go:19-27` 得到落实，本轮不再重复主张把全部 Problems 门禁后移；PM-102/QA-003、PM-104/QA 非阻断 2、PM-107/QA 非阻断 4 的共用修复分别独立核验。PM-106/PM-109 属 PM 角色的保留处置，本轮不介入。
- **未执行** `go build ./...`、`go test ./...`、`go vet ./...`、race、gofmt 或任何迁移；相关结论标记 Unverifiable。执行端"新增专项及 go test ./... 通过、build/vet/格式/差异检查通过、board/fs/liveness race 通过"的自测声明单独记录，不并入本轮结果。
- Windows 原生验证缺口按调用方要求保留：`migration_windows_test.go` 的句柄/junction/路径大小写用例需 Windows 实机执行，交叉编译不计为实测。
- 任务文件 `/tmp/claude-review.6283bf13fc385add6d53f86f3cc86935/prompt.txt` 未能删除：本会话处于只读计划模式，Write 与 Bash 均不可用。文件遗留不影响审核结果。
