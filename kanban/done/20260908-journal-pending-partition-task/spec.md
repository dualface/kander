# Transaction journal partitioning and retention: reads no longer parse committed records

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 12:36
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t1G:wX:p22
- STARTED_AT: 2026-09-08 12:37
- FINISHED_AT: 2026-09-08 13:12
- TASK_BRANCH: journal-pending-partition
- RESULT: completed

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

- 2026-09-08：独立执行；工作目录 `/home/dualf/works/kander/worktrees/journal-pending-partition`，任务分支 `journal-pending-partition`，交付目标 `develop`。base `1e1d3a630333fd6c1cac43238addbfc8b5d8939f`；初次交付 `c2e711896ad71946ab09b50ed05eb9ce01491113`，告警通道修复及最终交付 `1e4886a30d2d047175bebb18f71947795f42304d`，两次提交均已推送。
- 实现：日志 pending/committed 分区；旧根目录记录按原字节显式分拣；保留最近 100 条及对应迁移暂存证据；清理 best-effort；读者不解析 committed 内容，完整性校验仍保留。OperationRecord、锁顺序、配置 schema 不变。
- QA 修复涉及 board、launch、TUI：增加操作级 WarningLog、显式收集入口和可选 Warnings 返回字段，版本游标保留告警收集器；CLI 默认提示兼容，TUI 刷新、详情、预览、启动与回滚通过现有显示通道消费。具体原件与作者处置引用 `journal-qa-20260908-r1` / `QA-001` / `journal-qa-001-fixed`，不复制报告。
- Delivery Self-Check 1（`1e4886a30d2d047175bebb18f71947795f42304d`）：`git diff --check 1e1d3a630333fd6c1cac43238addbfc8b5d8939f HEAD` 退出 0，无空白错误或冲突残留。
- Delivery Self-Check 2（`1e4886a30d2d047175bebb18f71947795f42304d`）：统计全部 28 个改动 Go/JSON 文件物理行数，最大 910 行；无文件超过 1000 行。
- Delivery Self-Check 3（`1e4886a30d2d047175bebb18f71947795f42304d`）：逐项对照 docs/card-transactions.md 与实现；日志布局、提交顺序、保留数、维护及中断边界、WarningLog 和 CLI/TUI 告警呈现均同步；AGENTS.md board 职责同步。
- Delivery Self-Check 4（`1e4886a30d2d047175bebb18f71947795f42304d`）：`rg -n 'WarningLog|ScanWithWarnings|ReadSnapshotWithWarnings|LoadBoardWithWarnings|showJournalWarnings' internal/board internal/launch internal/tui`，结合日志函数引用核对，所有新增生产函数有实际调用，无死代码。
- Delivery Self-Check 5（`1e4886a30d2d047175bebb18f71947795f42304d`）：逐项核对测试职责：免解析、结构拒绝、旧日志提示、逐字节/幂等分拣、保留/迁移证据、清理失败、真实 kill/restart、跨进程互斥、Windows junction；新增 UI 集成分别覆盖刷新/详情/backlog 提交及启动成功/回滚的告警可见和无终端输出，无重复测试或无关断言。
- Delivery Self-Check 6/7（`1e4886a30d2d047175bebb18f71947795f42304d`）：模块根 `go test ./...` 退出 0；另以 `go test -json ./...` 计数，21 包、703 个顶层测试通过；含子用例 1222 pass、1 skip。`go test -race ./internal/board ./internal/launch ./internal/tui -run 'Journal|StartCollects'` 三包通过。`GOOS=windows GOARCH=amd64 go test -c ./internal/board` 及 launch/tui 对应命令均交叉编译通过（输出至 /tmp）；Linux 上未执行 Windows 原生测试。
- 性能（`1e4886a30d2d047175bebb18f71947795f42304d`）：`go test ./internal/board -run '^$' -bench '^BenchmarkJournalHistory$' -benchtime=3x -count=1` 通过，分区 show/list 核心读取约 2.14/2.02ms；`python3 /tmp/kander-journal-perf.py` 在隔离看板生成 825 条、85,957,467 bytes 日志，每命令 5 次取中位数：旧版 show 1.026818s、list 0.998601s；新版保留 825 条时 show 0.018175s、list 0.021367s；空看板 0.023602s/0.023231s。新版 init 后 committed=100。测量脚本及原始结果见 verification 附件；旧版二进制来自上述 base，新版来自最终提交。
- 审核：PM `journal-pm-20260908-r1` PASS；QA `journal-qa-20260908-r2` 增量确认原问题关闭，PASS；两角色 FINDINGS/NON_BLOCKING 均无剩余项。作者处置引用 `journal-qa-001-fixed`。批次 `journal-partition-20260908` 已通过受控 close 关闭，绑定最终交付；CSA/Hacker 按根 AGENTS.md 为 N/A。Windows 原生执行为明确环境缺口，不声称通过。

## SUMMARY

已完成日志分区、旧布局显式分拣、保留清理及 TUI 告警呈现。最终提交 `1e4886a30d2d047175bebb18f71947795f42304d` 已快进推送并合入本地/远端 develop；主工作区干净且同步，任务 worktree、本地/远端任务分支已清理。`git merge-base --is-ancestor 1e4886a30d2d047175bebb18f71947795f42304d develop` 及 origin/develop 对应检查均退出 0（`1e4886a30d2d047175bebb18f71947795f42304d`）。

验收 13/13 达成，验证均绑定 `1e4886a30d2d047175bebb18f71947795f42304d`：
- 条件 1–5：新记录 pending/committed 发布、版本顺序、免解析损坏及 128 MiB committed 文件、结构拒绝、prepared operation id 提示、旧布局兼容、逐字节及幂等/中断分拣均有对应通过用例。
- 条件 6–8：最近 100 条、迁移暂存证据保护、init 全量清理、清理失败只警告及真实多进程 journal 互斥用例通过。
- 条件 9–10：所有运行时新路径经 internal/fs；Windows junction 专项用例已加入，board/launch/tui Windows 交叉编译通过；本机非 Windows，未执行原生测试。prepared、phase 改写后归档前、分拣及清理中真实 kill/restart 用例通过，未丢失其他 prepared 记录。
- 条件 11：825 条、约 86 MB，CLI show 中位数由 1.026818s 降至 0.018175s，list 由 0.998601s 降至 0.021367s，与空看板约 0.023s 同量级；命令与原始输出见 IMPLEMENTATION 和 verification 附件。
- 条件 12–13：AGENTS.md 和 docs/card-transactions.md 同步；模块根 `go test ./...` 通过，JSON 计数 703 个顶层/1222 个含子用例测试通过、1 skip；相关 race 测试通过。

PM/QA 审核已完成并闭批；唯一 QA 项已修复，未解决审核项 0。环境缺口 1：Windows 原生未运行，仅有安全接口代码、平台专项用例和交叉编译证据。本次交付源码，未替换当前已安装的 kander 或迁移正在使用的真实看板；升级后应在维护窗口运行 kander init 分拣、清理旧日志。

## REVIEWS

- {"run_id":"journal-pm-20260908-r1","batch_id":"journal-partition-20260908","role":"PM","execution_status":"ok","base":"1e1d3a630333fd6c1cac43238addbfc8b5d8939f","commit":"c2e711896ad71946ab09b50ed05eb9ce01491113","report":"reviews/journal-pm-20260908-r1/report.md"}
- {"run_id":"journal-qa-20260908-r1","batch_id":"journal-partition-20260908","role":"QA","execution_status":"ok","base":"1e1d3a630333fd6c1cac43238addbfc8b5d8939f","commit":"c2e711896ad71946ab09b50ed05eb9ce01491113","report":"reviews/journal-qa-20260908-r1/report.md"}
- {"run_id":"journal-qa-20260908-r2","batch_id":"journal-partition-20260908","role":"QA","execution_status":"ok","base":"1e1d3a630333fd6c1cac43238addbfc8b5d8939f","commit":"1e4886a30d2d047175bebb18f71947795f42304d","previous_run_id":"journal-qa-20260908-r1","report":"reviews/journal-qa-20260908-r2/report.md"}
