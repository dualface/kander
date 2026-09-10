# 事务日志分区与保留策略: 读盘不再解析已提交记录

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 12:36
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t1G:wX:p22
- STARTED_AT: 2026-09-08 12:37
- FINISHED_AT:
- TASK_BRANCH: journal-pending-partition
- RESULT:

## GOAL

让看板读盘的代价不再随事务日志的历史长度增长, 并给已提交记录设保留上限.

现状: 运行时读盘唯一关心的事实是「有没有 prepared (未完成) 事务」(`internal/board/snapshot_context.go:86-101` 与 `internal/board/transaction_log.go:43-70` 都只挑 `phase == "prepared"`), 但 phase 存在记录内容里 (`internal/board/transaction.go:49-61`), 文件名只有 operation id, 所以 `readOperationRecords` (`internal/board/transaction_log.go:86-113`) 必须打开并解析 `.kander/operations/` 下每一个 JSON 才能知道它是不是 prepared. 提交时记录只是就地改写成 `committed` 留在盘上 (`internal/board/recovery.go:241-245`), 没有任何代码清理, 目录只增不减.

本机实测: 该目录 825 条记录, 86MB, 全部 `committed`, 单条最大 1.2MB (记录内嵌 redo 用的卡片前后镜像). 此时 `kander show --json <task-id>` 1.15s, `kander list` 1.24s, 且是 CPU 时间; 把该目录清空后同样命令为 0.02s / 0.07s, 全新空看板 0.03s. 这个代价落在每一条 kander 命令, 看板 TUI 的定时刷新, 以及 TUI 里按 s 启动任务前的读盘上.

## USER_DECISIONS

- 采用两条修法: (1) 让 prepared 与 committed 分开, 使读盘不必解析已提交记录; (2) 给已提交记录设保留策略并清理存量.
- 不做「提交时剥离记录里的前后镜像字段」这条 (原讨论里的第三条), 本卡不改记录的字段结构.

## EXPECTED_OUTCOME

- `.kander/operations/` 改为分区布局: prepared 记录落在 `pending/`, 已提交记录落在 `committed/`. 事务开始写 `pending/<operation-id>.json`, 提交时改写 phase 后原子移入 `committed/`, 既有提交顺序与断点语义 (prepared -> 附件目录 -> 文件 -> 入口 -> versions -> committed) 不变.
- 运行时判定「有没有未完成事务」只枚举 `pending/`, 不打开任何已提交记录; 只有确实存在 prepared 记录时才解析那几条, 用于报错信息与 init 重放. `scanContext` 与 `pendingContext` 走该快路径, init 与恢复仍可拿到全量记录.
- 目录完整性校验保留: 非法文件名, 非 JSON 文件, 目录形态异常, reparse point 仍然失败关闭并保留现场; 校验不依赖解析已提交记录的内容.
- 旧看板兼容: 老布局把 prepared 与 committed 混放在 `operations/` 根下且文件名无法区分, 因此根下仍存在未分拣记录时, 读者保持现有行为 (解析全部) 并提示运行 `kander init`; 绝不把未分拣记录默认当作已提交而漏掉待恢复事务.
- `kander init` 在既有维护窗口内把根下记录分拣进 `pending/` 与 `committed/`; 分拣可重复执行, 中断后可恢复, 不丢记录, 不改记录内容.
- 已提交记录有保留上限: 提交成功后按策略清理 `committed/`, 保留最近 N 条 (N 在实现里定为一个明确常量并写进文档) 以及所有带 `Migrations` 且 `.kander/migrations` 下仍有对应残留的记录; 其余删除. 清理持 journal 独占锁, 是 best-effort: 失败只警告, 不推翻已提交的事务结果, 不影响命令退出码.
- `kander init` 同样执行一次全量清理, 把存量收回去.
- 读盘耗时不再随历史记录数增长: 在与现状同规模的看板上, 单卡读取与 list 的耗时回到与空看板同量级.

## ACCEPTANCE_CRITERIA

- [ ] 新布局下, 事务写入 `pending/`, 提交后记录出现在 `committed/`, `pending/` 不残留; 提交顺序与各断点的恢复语义与改动前一致.
- [ ] 运行时的「有无 prepared」判定不解析已提交记录: 有用例证明 `committed/` 下存在无法解析或超大的文件时, 正常读盘仍然成功且不受其影响 (完整性校验仍拒绝非法文件名与非 JSON 文件等结构性问题).
- [ ] 存在 prepared 记录时, 读命令仍报出明确的待恢复错误并带 operation id, 与改动前一致.
- [ ] 旧布局看板 (根下混放记录, 其中含 prepared) 仍能正确发现待恢复事务, 不被当作已提交; 同时给出运行 `kander init` 的提示.
- [ ] `kander init` 分拣旧记录到 `pending/` / `committed/`: 分拣正确, 可重复执行 (第二次报告零迁移), 中断后重跑可恢复, 记录内容逐字节不变.
- [ ] 保留策略生效: 超出保留数的已提交记录被删除; 带 `Migrations` 且 `.kander/migrations` 下仍有对应残留的记录不被删除 (`validateMigrationStaging` 仍能工作); 清理失败只警告, 命令结果不变.
- [ ] `kander init` 对存量执行清理, 清理后 `validateMigrationStaging` 与恢复检查仍通过.
- [ ] 并发安全: 提交与清理, 多进程读与清理并发时不产生半删除或读到被删记录的错误; 沿用 journal.lock 的共享/独占语义, 不在持 journal 锁时再取看板/组/任务锁.
- [ ] Windows 路径经 `internal/fs`: 句柄, reparse point 拒绝与原子替换语义不变, Windows 专项用例在非 Windows 上 skip.
- [ ] 进程 kill/restart 恢复用例覆盖新增边界: 在 pending 写入后, 移入 committed 前, 清理进行中被 kill, 重启后恢复正确且不丢待恢复事务.
- [ ] 性能: 在与现状同规模的日志 (数百条记录, 数十 MB) 上, 单卡读取与 list 的耗时回到与空看板同量级; 卡里记录测量命令与前后数字.
- [ ] `docs/card-transactions.md` 的日志布局, 提交顺序, 保留策略三处同步更新 (现文档第 31, 33 行写的是 committed 记录保留供诊断与恢复核验), `AGENTS.md` 的 board 包职责一行同步; 与改动同一 diff.
- [ ] 在模块根运行 `go test ./...` 通过, 记录命令, 提交号与用例数.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题: 不改事务的锁顺序, revision/CAS 语义, 多文件发布与迁移重放算法, 不改 `OperationRecord` 的字段结构 (含前后镜像); 本卡只改记录的存放位置, 读取方式与保留期限, 动这些会把恢复语义的回归面放大.
- 加固: 不加日志加密, 签名或跨主机同步, 不改 `.kander` 的权限与 DACL 策略; 现有 POSIX 0600/0700 与 Windows 受保护 DACL 继续沿用.
- 共享契约与文档: 不改卡片格式, 看板状态机与 `config.json` schema. 落盘布局与保留策略本身是契约, 必须改, 因此 `docs/card-transactions.md` 与 `AGENTS.md` 的对应段落属于范围内; 其余文档不动.
- 相邻功能: 不做 TUI 侧的异步弹框改造 (另一张卡, 等本卡合入后再启动), 不做日志的诊断/查询子命令, 不做记录归档导出; 用户本次只要这两条修法.

## DISCUSSION

- 现有事实与行号: 只挑 prepared 的两处消费点 `internal/board/snapshot_context.go:86-101`, `internal/board/transaction_log.go:43-70`; 全量解析入口 `internal/board/transaction_log.go:86-113`; 记录结构 `internal/board/transaction.go:49-61`; 写入点 `internal/board/transaction.go:140` 与 `internal/board/migration.go:70`; 提交改写 phase `internal/board/recovery.go:241-245`; 控制目录创建 `internal/board/transaction_lock.go:103` (新增子目录需要在这里创建); 已提交记录的唯一其它消费方 `internal/board/migration.go:211-243` 的 `validateMigrationStaging`.
- 兼容性的要害: 老布局根下 prepared 与 committed 同名形态, 无法凭文件名区分. 因此「根下存在散落记录」必须被识别为未分拣状态并走旧的解析路径, 分拣后根下只剩两个子目录, 快路径才生效. 这是本卡最容易写错的地方, 需要专门的用例.
- 保留数 N 取值在实现里定, 要求是明确常量, 有注释说明取值理由, 并写进 `docs/card-transactions.md`; 不做成配置项 (配置面扩张不在本次范围内).
- 现场测量方法: 在 `.kander/operations` 有 825 条 / 86MB 时 `time kander show --json <task-id>` 得 1.15s, `time kander list` 得 1.24s; 把该目录移走后同样命令 0.02s / 0.07s; 全新空看板 0.03s. 交付时在同规模数据上复测并记录.
- 并行关系: TUI 异步弹框那张卡按用户决定推迟到本卡合入之后再启动, 本卡不需要与其协调文件.
- SELF_REVIEW: 已按目标, 已确认方案和项目规则复核: 目标与结果一致, 用户确认的两条修法 (分区免解析, 保留策略清理) 与明确排除的第三条 (剥离镜像字段) 都落到 USER_DECISIONS, EXPECTED_OUTCOME 与边界; 分区目录形态, 旧布局提示跑 init, 保留策略的具体形状是本次分析给出的方案细节, 写在 EXPECTED_OUTCOME 与 DISCUSSION, 未冒充用户决定. 边界四类均给出取舍与理由, 且未排除达成目标必需的工作 (落盘布局与文档属于必须改, 已明确纳入范围). 验收条件可执行可判定, 覆盖新布局, 快路径, 旧布局兼容, init 分拣, 保留策略, 并发, Windows, 崩溃恢复, 性能与文档. 未发现需要用户新决策的歧义.

## IMPLEMENTATION

- 2026-09-08：独立任务工作目录 `/home/dualf/works/kander/worktrees/journal-pending-partition`，分支 `journal-pending-partition`，交付目标 `develop`。base `1e1d3a630333fd6c1cac43238addbfc8b5d8939f`；交付 `c2e711896ad71946ab09b50ed05eb9ce01491113`，已推送同名远端分支。
- 实现：prepared 写入 pending；versions 后改写 phase 并原子移入 committed；init 逐条按原字节分拣旧日志并恢复中断；最近 100 条及所有仍有对应迁移暂存目录的记录保留。正常读盘仅解析 pending/旧根目录记录，committed 仅结构校验。无公共 API、OperationRecord 字段、包依赖变更。
- Delivery Self-Check 1（`c2e711896ad71946ab09b50ed05eb9ce01491113`）：`git diff --check HEAD^ HEAD` 退出 0，无冲突标记或空白错误。
- Delivery Self-Check 2（`c2e711896ad71946ab09b50ed05eb9ce01491113`）：逐文件统计本次 Go/JSON 文件行数；最大 910 行，全部 <=1000；新增生产代码 journal_layout.go 179 行、journal_retention.go 87 行。
- Delivery Self-Check 3（`c2e711896ad71946ab09b50ed05eb9ce01491113`）：`git diff HEAD^ HEAD -- AGENTS.md docs/card-transactions.md` 与实现逐项核对；日志布局、提交顺序、维护窗口、保留数 100、迁移原件保护及失败警告均同步。原子写临时文件注释与命名校验一致。
- Delivery Self-Check 4（`c2e711896ad71946ab09b50ed05eb9ce01491113`）：`rg -n 'journalFiles|readJournalRecord|readPendingRecords|partitionJournal|commitOperation|pruneCommitted|pruneJournal|warnJournalCleanup' internal/board` 逐个检查调用；新生产函数均有实际调用，无死代码。
- Delivery Self-Check 5（`c2e711896ad71946ab09b50ed05eb9ce01491113`）：逐项复核新增测试职责；分别覆盖免解析、结构拒绝、待恢复提示、逐字节分拣、维护与保留、迁移证据、清理失败、kill/restart、多进程锁及 Windows junction。基准比较 empty/legacy/partitioned；无重复行为断言或无关断言。
- Delivery Self-Check 6/7（`c2e711896ad71946ab09b50ed05eb9ce01491113`）：模块根 `go test -json ./...`（等价 `go test ./...`，JSON 输出计数）退出 0，21 包通过；701 个顶层测试通过、1 个平台用例 skip；含子用例共 1218 pass、1 skip。`go test -race ./internal/board -run '^TestJournal'` 退出 0（6.562s）。`GOOS=windows GOARCH=amd64 go test -c ./internal/board -o /tmp/journal-board-windows.test.exe` 退出 0；本机 Linux，未执行 Windows 原生测试。
- 性能（`c2e711896ad71946ab09b50ed05eb9ce01491113`）：`go test ./internal/board -run '^$' -bench '^BenchmarkJournalHistory$' -benchtime=3x -count=1` 通过；新布局 show/list 核心读取约 1.92/1.90ms。`python3 /tmp/kander-journal-perf.py` 对隔离临时看板、825 条/85,957,467 bytes 日志进行每命令 5 次测量，中位数：empty show 0.016658s；empty list 0.017639s；before show 0.985760s；before list 0.976509s；partitioned show 0.018611s；partitioned list 0.019801s。before 二进制来自 base `1e1d3a630333fd6c1cac43238addbfc8b5d8939f`，新二进制来自本提交相同源码；init 清理后 committed=100。脚本、原始结果以附件保留。
- 审核：PM/QA 待执行；CSA/Hacker 按根 AGENTS.md 标记 N/A。环境缺口仅 Windows 原生执行；不声称已执行。

## SUMMARY

