# 新契约首轮：作者独立判断与处置

审核 target：`d93f0b6e2b9f0fcd81ec7f79381cef6329321eef`；base：`a7fe54beb6ade00678e14655c0d385115eb950c8`。PM/QA 为 Claude/opus/high，两者 exit=0、语义未通过；此前 400 调用没有语义结论。本文件为原执行作者处置，完整原文见 [新契约首轮原文](review-new-contract-findings.md)，不替代审核关闭。

## Gate findings

- PM-101：部分成立。d93f0b6 的 MigrateCards 在无需迁移时仍因普通散落文件失败，已修复为无迁移时警告并提示 check；有迁移时仍拒绝全部结构问题，缺 spec、真实重复、reparse 等无论迁移数是否为零仍拒绝。不能采用直接把所有 Problems 门禁搬到零计划返回后的建议，否则会放行这些异常。新增无迁移 notes.md、需迁移 notes.md、缺 spec 和未知暂存物保留回归。
- QA-001：与 PM-101 共用修复，但“prepared 重做也被后续 scan 门禁挡住”不成立：目标提交 migration.go 的 49 行循环先 applyRecord，59 行才 scan；新测试构造有效 prepared 记录与无关缺 spec 目录，验证事务 committed 后 init 才报结构失败，原目录保留。真实重复若涉及事务 ID，仍由 validateRecord 拒绝，不能自动选择副本。已有恢复顺序保留，错误增加 check 指引。
- PM-102：注释/维护分叉成立。保留 RecoverTransactions 兼容入口，注释明确 init 走 MigrateCards；两入口改为共用 recoverMigrationRecords（同一暂存校验、维护窗口与重做顺序），不再维护两份恢复实现。目标函数原本也调用 validateMigrationStaging 和 requireMigrationWindow，因此“按旧入口必然绕过两项检查”不成立。
- QA-003：与 PM-102 共用修复，独立保留引用；入口注释及核心顺序已统一，恢复相关原测试继续验证兼容入口。
- PM-103：成立。结构扫描不再把形态写入 Entry.Kind，留空；公开快照在 attachSize 后填规模。字段注释、AGENTS 与目录说明同步。新增全量/定向结构扫描与公开 small 目录快照对照测试。
- QA-002：成立。不移动且可能目标不在映射内的历史反斜线路径、无效旧 URL 和 Wiki 散文保留；srcset 按候选判断，全为绝对 URL（含 data URL）保留。源移动或可能目标映射时，不支持语法继续拒绝，避免静默损坏。测试验证 archived 原件字节/mtime 不变，同时确认真正引用迁移旧卡的反斜线/Wiki/srcset 在发布前失败。

## PM NON-BLOCKING

- PM-104（low）：成立，已修复。SIZE 扫描遇正文错误继续交给既有 dependencyProblems 汇总一次，不新增重复 Problem；两份非 UTF-8 正文的 check 保留两个问题和统计行。
- PM-105（recommend）：边界风险成立，已落实。迁移跳过 reviews/dispatches 等生产者专属子树，与 update 共用受管路径判据；validateMigrationFiles 同样拒绝这些路径的回放写入。测试保留两种原件字节及拒绝伪造受管写入计划。不实现 A 的未来功能，不把历史原件链接当普通正文自动修订。
- PM-106（suggest）：无生产调用的观察成立，删除建议不采用。InitBoard 仍是零选项兼容包装，直接委托 InitBoardWithOptions，未复制恢复逻辑；board 与 tui 测试实际调用，保留既有 API 无运行语义损害。该建议不是 gate，未另建待办。
- PM-107（suggest）：成立，已删除 large 分支重复 target 赋值。
- PM-108（suggest）：成立，Goldmark 与 x/net 移至直接依赖块；版本和依赖集不变。
- PM-109（suggest）：模式差异观察成立，统一模式建议本轮不采用。目标提交 migration_write.go 使用 fs.OpenAppendFile；POSIX 新叶 0666 受 umask、既有模式不改，外层 stage 经 CreatePrivateDirectory 创建为 0700 并整体发布；Windows 仍走专用受保护句柄/DACL。无跨用户可读缺陷，当前契约没有保留旧叶 mode 的要求；不为统一叶文件模式新增跨平台 chmod 和恢复状态。保留已验证 PM-001 的同步/恢复协议，该项未冒充权限漏洞或已修复问题。
- PM-110（suggest）：成立，与 QA-002 共用候选判断修复，绝对 URL/data URL srcset 不再误拒绝。

## QA NON-BLOCKING（严格按原顺序）

1. low，“guard-write 对目录内部路径 + 尚未迁移旧文件卡提示错误”：成立；同状态旧文件使用 migration_required 提示 init，跨状态仍使用 moved；回归确认拒绝且提示 init。
2. low，“SIZE 扫描丢失不可读文档聚合诊断”：成立，与 PM-104 共用修复，保留独立引用。
3. low，“addSize 无 TYPE 时 SIZE 插在 H1 前”：成立；有 H1 时插到标题后，不补造 TYPE，保留 LF/CRLF。无 H1 才沿用文首补行。回归覆盖两类换行及无末尾换行。
4. suggest，“newTask large target 无效重复”：成立，与 PM-107 共用删除。
5. suggest，“validateRecord 两段包含式校验”：成立；第一段只要求迁移记录 Purpose 为 migration，第二段统一禁止 Entries/Directories/Groups 混入，拒绝边界不变。

## 验证与当前状态

新增专项及 go test ./... 通过，build/vet/格式/差异检查通过；board/fs/liveness race 通过；Windows board 测试程序和完整二进制交叉编译通过，原生环境缺口保留。

修复提交：`6d447150f78e098067d5d13f102f5407a8bd87f6`（PM-101/102/103/104/107/108；QA-001/003 与 QA 非阻断 1/2/3/4/5）；`83f73544ef1f612a97f99d384e9386eeecb78752`（QA-002、PM-105/110）。PM-106/109 的保留处置见上，无未完成 must-fix。

最终交付：`83f73544ef1f612a97f99d384e9386eeecb78752`，本地/远端 directory-card-form 一致、工作树干净，已正常推送。最新组基线 `d93f0b6e2b9f0fcd81ec7f79381cef6329321eef`；fetch/rebase 无变化，其后全量测试再次通过。审核基准链仍为 a7fe54b..d93f0b6，本次供同契约增量复审。未修改冻结契约，未触发审核或集成；回填后 move review，分支与工作树保留。
