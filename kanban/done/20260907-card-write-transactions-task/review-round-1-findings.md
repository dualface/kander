# 首轮审核完整原文

## PM 原报告

Role: PM  
Commit: `f056af9b75bb981e99d6f071fc04f36988b6cef2`  
Task Context: `/tmp/kander-s-batch1-context/spec.md`，S 卡全部 11 条验收。  
Reviewed Scope: `4889d3f..f056af9` 引入的事务、受控更新、生命周期、恢复、Windows 句柄及规则变更；追踪相关既有消费者。排除 D 形态迁移、N 持久 epoch。

**结论：不通过。2 项 medium，其中 1 项 [mechanical]。**

### Requirement Table

生产行为结论基于静态调用链，标为 Inferred；测试存在性标为 Observed，不代表本轮执行通过。

| Requirement | Expected Behavior | Code Evidence | Status |
|---|---|---|---|
| R01／验收1 | 看板、排序组、排序任务依次加锁 | `internal/board/transaction_lock.go:80` | Complete |
| R02／验收1 | 稳定控制目录；迁移独占看板，读者共享任务锁 | `transaction_lock.go:47`、`:97`；`snapshot.go:8` | Complete |
| R03／验收1 | 同 ID 创建、迁移与批量修改不产生副本或锁倒序 | `internal/board/move.go:77`；`snapshot.go:155`；`transaction_lock.go:80` | Complete |
| R04／验收2 | 单调 revision、操作 ID、预期状态及版本校验 | `internal/board/transaction.go:130`、`:171`；`recovery.go:185` | Complete |
| R05／验收2、8 | 锁内重定位；更新、回滚不创建旧卡路径 | `transaction.go:152`、`:219`；`snapshot.go:110` | Complete |
| R06／验收3 | 单二进制分发 update；show JSON 返回 revision | `internal/cli/wire_board.go:9`；`internal/board/cmd.go:162`；`update.go:201` | Complete |
| R07／验收4、8 | small 的 spec.md 映射现存正文；目录卡支持其他文档 | `internal/board/transaction.go:181`、`:219` | Complete |
| R08／验收4 | 拒绝路径逃逸、正文别名、symlink/reparse | `transaction.go:181`；`update.go:171`；`internal/fs` 安全读写入口 | Complete |
| R09／验收4 | 保留 LANGUAGE、受管字段、冻结契约及机器记录 | `internal/board/update.go:79`、`:109`、`:174` | Complete |
| R10／验收5 | 手工认领原子记录 OWNER、STARTED_AT、状态 | `update.go:33`；`snapshot.go:155` | Complete |
| R11／验收5 | 完成验证后原子记录 RESULT、FINISHED_AT、状态 | `snapshot.go:174`；`document.go:215` | Complete |
| R12／验收5 | 终止记录结果、原因、决定引用及重复目标 | `internal/board/update.go:33` | Complete |
| R13／验收5 | 授权契约修订要求 CAS，保存决定及原／新字段 | `update.go:138`、`:166` | Complete |
| R14／验收6 | 生命周期消费者接入事务；dismiss 使用受控读取 | `launch/commands.go`；`notify/command.go:85`；`takeover/dismiss.go:27`；`window/window.go:63` | Complete |
| R15／验收6 | 陈旧异步回写、全文回滚保留新 revision 并报冲突 | `internal/board/snapshot.go:110`；`launch/agent.go:137` | Complete |
| R16／验收7 | 通用任务／组多文件 API；共享锁下读取一致产物 | `transaction.go:68`、`:204`、`:348`、`:368` | Complete |
| R17／验收7 | prepared 阻止读取；init 显式恢复；冲突保留现场 | `transaction_log.go:41`；`recovery.go:220`、`:240` | Complete |
| R18／验收1、11 | Windows 并发事务与安全句柄兼容 | `transaction_log.go:57`；`windows_ops.go:349`、`:510`；见 PM-001 | Partial |
| R19／验收9 | 两项复现、陈旧回滚、并发追加、kill/restart 有回归覆盖 | `window/window_test.go:105`、`:150`；`transaction_test.go:147`、`:201`、`:332`（Observed） | Complete |
| R20／验收10 | 规则使用 update；guard-write 仅辅助；不要求 D 命令；重复保留双方 | `rules/KANDER-KANBAN-RULES.md:384`；`docs/card-transactions.md:77` | Complete |
| R21／验收11 | 职责、协议、i18n 与实际回滚语义一致 | `docs/card-transactions.md`；三份 locale；`launch/notify_resume.go:29`；见 PM-002 | Partial |
| R22／验收11 | 全量、race、Windows 验证结果可确认 | 执行端声明通过；本轮仅只读审查，未复跑；原生 Windows 缺口已明确 | Unverifiable |

状态统计：**Complete 19，Partial 2，Missing 0，Contradicted 0，Unverifiable 1。**

### Findings

**PM-001 — medium — Inferred，置信度高：全局 journal 扫描与 Windows 原子发布发生共享冲突**

违反验收1、11的并发及 Windows 句柄兼容要求。

不同任务仅持看板共享锁，各自任务锁不互斥。新加入的 [operationRecords](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/transaction_log.go:57) 每次扫描整个 `operations/`，隐藏文件过滤发生在 `fs.ListDirectory` 返回之后。

可达路径：

1. 任务 A 发布 journal。[writeBytesAtomic](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/fs/windows_ops.go:534) 创建隐藏临时文件，持有包含 `DELETE` 权限的句柄，直到发布结束才关闭。
2. 同时，任务 B 执行 `show --json` 或 `update`，进入 `pending`，枚举该临时文件。
3. [ListDirectory](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/fs/windows_ops.go:362) 使用 `shareDelete=false` 打开文件，与 A 已持有的 `DELETE` 句柄冲突；错误直接返回，尚未执行隐藏文件过滤。

影响：Windows 上操作不同卡片也会因正常并发返回共享冲突。已发布 journal 的读取同样未与其替换协调。该问题由本范围新增的全局 journal 扫描引入；无须外部进程绕过协议。

最小修复：统一同步 journal 枚举、读取及发布。例如添加稳定的 journal 共享读／独占发布锁，覆盖临时文件创建至句柄关闭，并规定固定锁顺序。补充不同任务同时读取、发布的 Windows 回归。

原生 Windows 未执行；以上为句柄权限与调用链推导，不声称实测复现。

**PM-002 — medium [mechanical] — Observed，置信度高：notify 恢复注释仍承诺无条件恢复原文**

违反验收11的职责／协议同步要求。

[notify_resume.go:29](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/launch/notify_resume.go:29) 仍写：

> “on failure it restores the card text as it was before the call.”

本范围改变了其实际调用的回滚行为：[managedMutation](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/snapshot.go:117) 在 revision 或状态变化时返回冲突、保留新正文。`NotifyViaResume` 的失败路径会传播该回滚错误。

影响：消费者可能误认为失败返回后正文必定恢复，与新增 CAS 契约相反。

最小修复：注释明确“仅当前操作仍持有该 revision 时恢复；否则保留新记录并报告冲突”。无需改变逻辑。

### NON-BLOCKING

NON-BLOCKING: none

本轮 `git diff --check` 通过，工作树干净。未修改文件或执行写入型测试。任务文件保留：本会话仅允许只读操作。

## QA 原报告

Role: QA  
Commit: `f056af9b75bb981e99d6f071fc04f36988b6cef2`  
Task Context: `/tmp/kander-s-batch1-context/spec.md`，S 卡全部 11 条验收。  
Reviewed Scope: `4889d3f..f056af9`，以及相关 board/fs/window/launch/notify/takeover/liveness/cli 消费链、测试和规则。排除 D 形态迁移、N 持久 epoch；CSA/Hacker N/A。

结论：发现 **4 项 medium 门禁问题**。

| 验收/质量项 | 证据与评估 |
|---|---|
| 1. 锁顺序、任务唯一性 | Observed：`transaction_lock.go` 按看板、排序组、排序任务加锁；new/move 取得看板独占锁。Windows journal 并发存在 QA-01。 |
| 2. revision、操作 ID、现存卡语义 | Observed：`Expect`、`managedMutation` 校验版本、状态及路径；`Put` 重定位现存卡。 |
| 3. update/show 命令 | Observed：cli 注册 update；show JSON 包含正文、位置、revision、操作 ID；有命令级回归。 |
| 4. 受控正文及附件 | Observed：路径规范、受管字段、机器记录、LANGUAGE、冻结字段检查齐备；small 仅接受逻辑 `spec.md`。 |
| 5. 合法生命周期入口 | Observed：move 提供认领、完成、终止参数；契约修订记录决定及旧/新字段。测试覆盖这些流程。 |
| 6. 生命周期及回滚 | Observed：写卡链复用 board 事务；失败回滚校验操作版本。dismiss 本身不写卡。 |
| 7. 多文件发布与恢复 | Observed：prepared 重做记录、init 显式恢复、读者 pending 检查及重复保留均有实现；Windows 见 QA-01。 |
| 8. small/large 兼容 | Observed：正文映射及目录附件接口保留；无越界要求 D 的迁移实现。 |
| 9. 并发和崩溃回归 | Observed：测试包含旧路径回滚、陈旧全文回滚、new/update/move 竞争、批量追加及七个 kill/restart 边界。订阅消费遗漏见 QA-02。 |
| 10. 发布规则 | Observed：明确 update、guard-write 辅助边界、重复保留及本阶段无形态迁移命令。 |
| 11. 架构、i18n、验证 | Observed：board 未反向依赖 launch/notify/review；三语命令消息齐备；`git diff --check` 通过。文件尺寸见 QA-03，API 注释见 QA-04。 |

Unverifiable：本轮仅静态审查，未复跑 Go 测试、race、交叉编译或 Windows 实机测试。任务上下文报告前述测试通过，并明确 Windows 原生执行缺口；不视为本轮验证结果。

### Gate Findings

**QA-01 — medium — Inferred，置信度高：Windows journal 读取与提交替换缺少共同同步。**

证据：[`transaction_log.go:70`](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/transaction_log.go:70) 读取全部操作记录；不同任务只持看板共享锁及各自任务锁。Windows `ReadRegularFile` 经 [`windows.go:401`](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/fs/windows.go:401) 打开文件时不共享 DELETE；[`recovery.go:215`](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/recovery.go:215) 同时可能原子替换对应 journal 为 committed。

场景：任务 A 已写完正文和 revision；任务 B 的 show/update 正在读取 A 的 prepared journal。A 替换 journal 时遇共享冲突，提交返回失败并留下 prepared；后续访问 A 要求 init 恢复。不同任务的正常并发因此制造恢复状态。

最小修复：为 journal 枚举、读取及发布设置共同的稳定锁，明确其顺序；覆盖枚举临时文件期间的访问。最低有效验证：Windows 双进程测试，用同步点控制 journal 读取与 committed 替换重叠，断言双方成功且无遗留 prepared。

**QA-02 — medium — Inferred，置信度高：订阅展开组成员时吞掉新增的 revision 冲突。**

证据：[`snapshot.go:93`](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/board/snapshot.go:93) 新增版本不匹配错误；[`subscribe.go:139`](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/liveness/subscribe.go:139) 对读取错误直接 `continue`。同文件第 181 行使用不完整成员列表，第 331 行固定本次订阅的 `monitored`。

场景：`subscribe --watch <group>` 展开已有两名成员；Scan 后，一名成员通过 update 写入执行记录。读取该成员发生 revision 冲突并被跳过，订阅仍成功启动，此后永久漏掉该成员的状态变化。此前同路径正文更新不会触发此错误。

最小修复：成员展开遇读取冲突必须返回错误，或重新取得完整一致快照；不能返回部分成功。最低有效验证：在 Scan 与正文读取间注入一次 update，断言完整成员集或显式失败。

**QA-03 — medium — Observed，置信度高：已有超长测试文件继续增长。**

证据：[`launch_test.go:722`](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/launch/launch_test.go:722) 所在文件，base 为 **1030 行**，目标为 **1035 行**。

违反任务明确的尺寸门禁：已有超过 1000 行的非生成代码文件不得增加最终物理行数。最小修复：将回滚相关测试移入独立测试文件；保持测试覆盖，重新检查行数并运行 launch 包测试。

**QA-04 — medium [mechanical] — Observed，置信度高：notify 恢复 API 注释仍承诺无条件还原正文。**

证据：[`notify_resume.go:29`](/home/dualf/works/kander/worktrees/20260907-review-archive-group/internal/launch/notify_resume.go:29) 声称 `"on failure it restores the card text as it was before the call"`；本次修改后，`RestoreWindowText` 会因新 revision 拒绝还原。

场景：新执行端更新正文后，旧 resume 失败；实际保留新正文并报告回滚冲突，API 注释却给调用者相反预期。

最小修复：注释明确“仅当前操作仍拥有 revision 时恢复；否则保留新记录并报告冲突”。静态对照即可验证。

NON-BLOCKING: none

已尝试删除任务文件；只读文件系统拒绝删除，文件遗留不影响审核结果。
