# 卡片事务、受控更新与恢复

任务 ID 是身份; 路径只是本次定位结果. `board` 在持锁期间重定位, 不将缓存路径用于异步完成后的无条件写入. 状态仍只由七个状态目录决定, 控制目录不保存第二份状态真相.

## 命令

```sh
kander show --json <task-id>
kander update <task-id> --document spec.md --file <UTF8-input> --expect-revision <revision>
kander update <task-id> --document plan.md --file <UTF8-input> --expect-revision <revision>
kander move <task-id> working --owner codex
kander move <task-id> done --result completed
kander move <task-id> archived --result cancelled --reason <reason> --decision <user-decision-reference>
kander move <task-id> trash --result trashed --reason <reason> --decision <user-decision-reference>
```

`show --json` 的对象包含 `entry` (TaskID/State/Path/Document/Kind)、`revision`、`operation_id`、`text`. 旧卡首次读取的 revision 为 0, 尚无操作 ID; 每次成功 mutation 增加 1, 新卡创建也计一次提交. 客户端不得以时间戳推导版本. 输入文件是独立的 UTF-8 文件, 不得直接编辑现存卡片后再调用 update.

新卡一律目录; small/large 都用 `--document spec.md`, 并可写普通附件. 旧文件卡只读, mutation 在副作用前要求先运行 init 迁移. 普通附件支持相对目录, 不支持路径逃逸、隐藏控制目录、大小写正文别名、尾随点/空格、符号链接或 reparse. `reviews/`、`dispatches/`、manifest/index/checkpoint 等机器产物由专用生产者写入.

全文更新保留 LANGUAGE、受管身份/时间/结果字段及机器索引. TASK_BRANCH、IMPLEMENTATION、SUMMARY 可通过正文更新. todo 之后的契约、任务组、依赖及 SIZE 不得任意修改; 退回 backlog 也不会解除冻结. 显式用户决定可用 `--contract-decision-file <UTF8-decision>` 在同一 expected revision 下修订, 正文的受保护 CONTRACT_DECISIONS 保存时间、决定原文和修改前后字段. 工具只记录依据, 不推断或验证用户意图.

手工认领的 `--owner` 同时写 OWNER/STARTED_AT. 完成时先 update summary/report, 再 move done --result completed; RESULT/FINISHED_AT 和状态原子提交. 终止需要 result、reason、decision; duplicate 还需要 `--duplicate-of`. done 到 archived 的 completed 也需要用户决定引用. move 可加 `--expect-revision`.

## 锁与跨包 API

控制文件位于 `kanban/.kander/`, 不随卡移动:

- `locks/board.lock`: 普通读写共享; new、move、迁移和恢复独占.
- `locks/<group-id>.lock`, `locks/<task-id>.lock`: 先排序组 ID, 再排序任务 ID. 读者共享, 写者独占. 锁文件是稳定 inode/句柄, 不替换、不删除.
- `locks/journal.lock`: 在看板/组/任务锁之后短暂取得; 全局日志枚举和读取共享, prepared/committed 原子发布独占. 锁覆盖临时文件创建至所有读写句柄关闭; 持此锁时不再取得看板/组/任务锁.
- `versions/<task-id>.json`: `{revision, operation_id, contract_frozen}`. 旧卡缺文件等价于 revision 0.
- `operations/<operation-id>.json`: 写前持久记录, 完成后保留 committed 记录供诊断与恢复核验.
- `groups/<group-id>/...`: 后续生产者的组控制文档; 不作为任务卡扫描.

POSIX 以 flock 实现共享/独占; Windows 以 LockFileEx 实现, 新锁和控制文件通过 internal/fs 在创建时获得私有权限/DACL. 路径逐分量验证, 锁句柄持有到提交/读取结束. 异步 Agent/终端操作不长时间占用文件锁.

`board.ReadSnapshot(root,id)` 返回一致正文、位置、版本. `Scan`/`ScanTargets` 返回带操作局部版本游标的 Entry; `ReadDocument` 拒绝失效 Entry. `MoveEntry` 和 window 的兼容函数保留调用形式, 但写入要求有效 Entry 版本游标; 手工构造 Entry 不构成写入授权.

`board.WithTransaction(root, LockScope, callback)` 是多文件生产者入口. LockScope 一次声明 Tasks、Groups、ExclusiveBoard、ReadOnly. callback 内只使用 Transaction 方法, 不嵌套调用会重新取得锁的 board API:

- `Snapshot` / `Expect`: 持锁重定位, 校验状态与预期 revision.
- `Read` / `Put`: 读取同一已提交快照, 暂存正文或附件. 同一个事务的每个文件只暂存一次, 每张卡的 revision 只增加一次.
- `ReadGroup` / `PutGroup`: 受声明组锁保护的控制文件; 生产者验证自身 schema 和预期文档版本.
- `Relocate`: 在 ExclusiveBoard 下暂存状态目录改名.

Put 可以创建附件的父目录, 但不创建现存任务的根目录. 专用生产者可写受管目录, update 命令则执行额外的正文/附件保护. callback 返回错误时尚未发布任何数据. 多任务锁排序避免反向批量写入死锁; 长期审核只在发布时短暂取得这些锁.

`window.WriteDocument` 委托 `board.WriteManagedDocument`; launch 失败时 `RollbackDocument` 在一个事务内恢复原文与原状态. 操作游标只随自身成功写入推进. 新执行记录、迁移或另一个命令使 revision 变化时, 旧回滚显式冲突, 不覆盖新内容、不复活旧路径. 后续执行轮次协议可在 Expect 基础上增加持久 epoch; 本层的当前并发令牌为 revision 和预期状态.

## 恢复格式与可见性

schema 1 操作记录包含:

```json
{
  "schema": 1,
  "operation_id": "<random 128-bit identifier>",
  "phase": "prepared",
  "revisions": {"<task-id>": 2},
  "groups": [],
  "directories": [],
  "files": [{"path": "working/<task-id>/report.md", "after": "report text"}],
  "entries": []
}
```

`files.before` 缺失表示只创建; 有值表示必须存在且与旧内容匹配. 文件内容按字符串保存, JSON 对任意 UTF-8 文本转义. `entries` 的 from/to 为看板相对路径, kind 为旧 schema 1 的物理形态编码 (small 表示文件、large 表示目录), 不是 Entry.Kind 的任务规模; 无 from 表示 new, text 保存创建正文或移动后的预期正文. 所有路径必须属于记录列出的任务/组.

提交顺序: prepared 记录持久化; 附件目录; 文件; 入口创建/迁移; versions; committed 标记. 文件和版本通过 internal/fs 原子替换, 创建通过只创建语义, 改名拒绝既有目标. 本阶段的恢复测试针对进程 kill/restart; 不宣称提供任意硬件掉电后的文件系统持久性保证.

持锁读者不会见到正在发布的多文件中间态. 若进程中断并释放锁, prepared 记录让读命令报告明确的待恢复错误. 读者不自动修复. Scan/ScanTargets 在同一锁内捕获入口和正文; list/TUI/订阅/依赖检查通过 Board.Document 消费已提交快照, 不因随后发生的迁移混用新路径与旧正文, 不返回部分成员集. 新扫描仍对未完成事务显式失败. `kander init` 取得看板独占锁后按记录完成重做; 已完成的步骤用匹配的内容/版本确认, 未完成步骤继续, 恢复可重复执行. 未知版本、内容冲突、重复入口和 reparse 均失败关闭, 保留现场. 新卡已建立目录但缺 spec 时, 仅在有效创建记录下补完正文.

目录迁移在同一 schema 1 记录中增加 `purpose: "migration"` 及 `migrations: [{from,to,before,after,rewrite,original}]`; 暂存目录为 `.kander/migrations/<operation-id>/<task-id>/`. rewrite/original 是由操作 ID 绑定的明确临时替换与原文备份名称; 缺这两项的 schema 1 记录按相同固定命名协议解释. 恢复仅接受完整原文及 After 的精确前缀, 不采用随机命名的未知残留. 已提交记录及空的操作暂存父目录保留供核验; 未登记产物报错保留. 新记录使用 `link_relocation: true`, 同一记录的 migrations 包含整批旧文件映射, files 包含已有目录 spec/Markdown 附件的 SIZE 与链接调整; 每张变化卡片登记一次 revision. 发布前及恢复前重新按原文与已登记映射核对 After, 禁止混入其他正文修改. 无该标记的旧 schema 1 记录保留原 SIZE-only 计划语义. 维护窗口、恢复顺序与边界见 [目录卡迁移](directory-cards.md).

## 验证与能力边界

测试覆盖同 ID 并发 new、update/move 竞争、反向批量锁、并发追加、跨文件与组控制发布、原文回滚与新 revision 竞争、旧路径小卡复活回归、受管字段与正文别名、生命周期和用户批准的契约修订. 子进程在 prepared、附件目录、首文件、全部文件、rename、revision、committed 边界被 kill, 重启后验证恢复与读者隔离.

事务保护遵守命令协议的本机进程, 不隔离任意直接改文件的进程. 升级时协调旧 Agent/旧二进制停写, 再启用受控入口. `guard-write` 只在检查瞬间给出提示, 不能把外部编辑与检查变成一个事务. 发现真实重复时保留双方、显式报错; 工具不会猜主副本或自动删除.

## 审核原件发布

审核归档复用本协议。`Transaction.PutBytes` 允许专用生产者无损保存二进制附件；spec.md 仍要求 UTF-8。非法 UTF-8 的 FileChange 在 JSON 日志中采用 `binary: true`、`before_bytes`/`after_bytes`（base64）；文本记录保持既有格式。恢复解码后通过相同 fs 原子写入执行，创建与替换语义不变。运行、批次与逐卡清单见 [审核证据](review-evidence.md)。

## 持久派回授权

绑定 dispatch 的正文与附件 update 还须携带 `--dispatch-id` 和 `--execution-epoch`；当前 revision 不能替代执行授权。WINDOW/launch 回滚同时校验操作局部版本和授权，接受/完成回执与 state/revision 共用本文件的事务。意图和回执原件由 board producer 管理，普通 update 不得改写；命令及恢复见 [持久派回协议](durable-dispatch.md)。
