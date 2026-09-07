# 订阅的已提交事实与成员集合

`kander subscribe <task-group> <task-id>... [--watch <task-id|task-group-id>...]`
输出版本化 JSON Lines。成员 ID 固定，`--watch` 的原始组引用保留，每次观察重新展开。
订阅只报告事实，不执行依赖放行、业务确认、自动恢复或看板修复。

## 事件与 revision

所有行保留 `event`、`group_id`、`tasks`；状态变化保留 `changed` 的
`task_id/from/to`。新增字段如下：

| 字段 | 含义 |
| --- | --- |
| `schema_version` | 当前为 1 |
| `subscription_id` | 每次订阅新建的随机标识 |
| `seq` | 该订阅从 1 递增的序号；不是跨进程游标 |
| `observed_at` | 协调快照读取完成时间，UTC RFC3339 |
| `task_revisions` | 与 `tasks` 同一快照的已提交 revision |
| `read_status` | `committed`、`recoverable`、`maintenance` 或 `invalid` |
| `membership_complete` | 本次成员集合与被监听卡片是否完整可读 |
| `reconciliation_required` | 消费者必须先重新核对，不能据此自动放行 |

初始行为是 `snapshot`。状态不同发送 `state-change`；状态相同、revision 不同
发送 `task-update`。两者的 `updated` 列出 revision 改变的已监听卡片。
同一轮状态变化可同时携带 revision 变化，不再重复发送 `task-update`。

同状态正文或附件的受控更新会推进 revision；一次 refresh 内的
`review -> working -> review` 即使最后状态相同，也会出现 `task-update`。
重启重新发送当前 `snapshot`，消费者用保存的 revision 比较已发生的更新，不能用
新进程的 `seq` 补历史事件。revision 只证明受控更新发生过，不证明某轮派发已完成；
本接口没有 dispatch 回执或事件回放日志。旧版未建立版本记录的卡片 revision 为 0，
绕过事务直接编辑文件不保证推进 revision。

心跳仍独立计时，默认 refresh=1 秒、heartbeat=900 秒。只有心跳探测当前监听集合
中的 `working` 卡片；不探测全板其他卡片。探测与 writer 仍同步，本次读取期限不会
中断慢探测或阻塞 writer。`observed_at` 表示卡片快照时间，不冒充探测完成时间。

## 动态组引用与不完整事实

有 `--watch` 时，每行的 `watch_references` 保留原始引用，`watched` 表示展开后的
外部任务；外部集合为空时 `watched` 省略。`tasks` 与 `task_revisions` 包含本组显式
成员和当前外部集合。组引用另带 `memberships` 与 `membership_versions`。
成员版本是排序成员 ID 集合的确定性 SHA-256；空组在已启动订阅中使用 `empty`。
版本不因无关正文或状态更新改变，也不是全局递增计数器。

成员集合变化发送 `membership-change`，携带完整新集合及版本。新增成员立即进入
监听。移除或改组导致离开原监听组时，`removed` 保留离开的 ID（即使仍属于另一监听组），
`reconciliation_required=true` 在本次订阅后续事件中保持；不能把成员减少解释为
依赖已经满足。消费者须重新核对契约、前后成员版本及实际交付。空组在初次订阅时
拒绝；已有订阅中的组变空仍发成员变化，不把它当作成功完成。

成员 ID、显式外部 ID、组展开之间有重复则拒绝。组成员解析与 `check` 的依赖展开
复用 board 层结果，兼容旧讨论区的组字段。组引用需要全板成员归属信息：扫描问题、
不可读正文等使归属无法确定时，发送终止事件 `membership-unknown`，
`membership_complete=false`、`reconciliation_required=true`，随后非零退出。
该行若带任务事实，只是上次成功快照，不能作为新的完整快照消费。初始失败使用空映射。

仅监听显式任务 ID 时使用定向扫描，无关已知问题不会触发额外读取或 Agent 探测。
组展开必须检查所有可能成员；无法读取归属的条目不能假设属于无关组。

## 协调读取与恢复

`board.ScanContext` / `ScanTargetsContext` 将状态、正文和 revision 放在同一组共享锁
下读取，`Board.Revision` 与 `Board.Document` 保留快照值。旧 `Scan` / `ScanTargets`
接口仍可用，保持阻塞读取。`Board.GroupMembership` 返回成员、版本及完整性问题；
存在组依赖时，`TaskDependenciesOf` 和 `check` 不接受部分展开结果。

订阅每次读事实共用 2 秒期限。看板维护锁、任务写锁和 journal 锁争用都服从该期限；
无遗留后台锁等待 goroutine。耗尽后发送 `read-error`，`read_status=maintenance`，
随后非零退出；该状态也可能表示正常写入争用，并不宣称已确认有人执行 init。
识别到已准备但未提交事务时立即发送 `read-error`、`read_status=recoverable` 并退出，
由操作者按事务维护契约显式恢复。重复卡、reparse 和持久损坏失败关闭，不自动修复。

期限覆盖锁争用与各读取阶段之间的取消检查；文件打开、内核 I/O、关闭仍受操作系统
控制，不承诺硬实时终止。POSIX 使用非阻塞 flock 尝试，Windows 使用
LockFileEx 的 FAIL_IMMEDIATELY；已有阻塞写锁和安全路径入口不变。
