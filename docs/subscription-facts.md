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
当前 dispatch 的持久回执由下述 `dispatches` 提供，接口没有事件回放日志。旧版未建立版本记录的卡片 revision 为 0，
绕过事务直接编辑文件不保证推进 revision。

心跳独立计时，默认 refresh=1 秒、heartbeat=900 秒。仅采集当前监听集合中的
`working` 卡片，以及持久派回尚待确认的 `review` 卡片；扫描不等待探测或输出。事件 `observed_at` 表示卡片快照时间，
不冒充探测完成时间。探测与输出的有界生命周期见下节。

## 派回事实与确认期限

快照、心跳及相关变化事件可携带 `dispatches`，以任务 ID 为键。每个摘要包含：

- `dispatch_id`、`task_id`、`kind`、`epoch`、`state` 和 dispatch 自身的 `revision`。
- 原始 `created_at`、`confirm_by`，以及快照时计算的 `age_seconds`。
- `accepted` / `completed` 原子回执，包含时间、卡片 revision、状态及适用的交付/处置引用。
- `confirmation_pending`：仅 prepared/delivery-unknown 为 true；`confirmation_overdue`
  表示这两种状态已达到原确认期限。accepted 后不是完成超时，不继续使用确认期限。

这些字段与同一事件的卡片状态、正文、`task_revisions` 在同组共享锁内读取；
摘要只报告当前执行授权，不暴露消息正文。未绑定的旧卡省略对应条目，不能补造历史回执。
同 ID 的 `review -> working -> review/done` 在一个 refresh 内完成，即使 notify 尚未返回，
下一事件或重启首个 snapshot 仍包含 accepted/completed，无需捕捉 working 边沿。
新 dispatch 取代当前授权后，旧 ID 仍通过 `kander dispatch show` 查询；摘要不是历史列表。
完成回执证明受控完成操作，不能替代审核闭批或 Git 集成验证。

每次扫描读取原 `confirm_by`，以该绝对期限独立唤醒；无关状态、正文、成员变化和心跳
不续期。首次发现已过期也立即发 `dispatch-attention`，`attention` 列出超时任务 ID，
`reconciliation_required=true` 要求消费者核对。事件附当前 dispatch 摘要及存活缓存，
必要时启动有界探测；已有批次在途时保留一个合并请求，待其完成后调度，不卡住扫描。
探测批次完成后，仍超时的派回再发注意事件，携带最新可用观测。后续心跳继续采集。
没有派回进展时即使 `liveness.status=alive`，仍保持 `confirmation_pending=true`；
存活的观测年龄与派回年龄分别输出。unknown、不完整事实和期限到达均不自动恢复、
重发、改写卡片或放行依赖。

仅 working 和 pending review 参与探测；普通 review、已 completed 的 review 不探测。
缺失/冲突的 dispatch 原件或回执使监听卡事实不可用，沿用终止错误事件，不能当作未绑定。
任务组展开时不相关卡片的 dispatch 错误不冒充成员归属错误；监听集合中的错误仍失败关闭。

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
接口仍可用，保持阻塞读取。`ScanDispatchesContext` 可选捕获当前派回，
`Board.CurrentDispatch` 返回与正文、revision 一致的摘要；旧扫描入口不增加派回读取。`Board.GroupMembership` 返回成员、版本及完整性问题；
存在组依赖时，`TaskDependenciesOf` 和 `check` 不接受部分展开结果。

订阅每次读事实共用 2 秒期限。看板维护锁、任务写锁和 journal 锁争用都服从该期限；
无遗留后台锁等待 goroutine。耗尽后发送 `read-error`，`read_status=maintenance`，
随后非零退出；该状态也可能表示正常写入争用，并不宣称已确认有人执行 init。
识别到已准备但未提交事务时立即发送 `read-error`、`read_status=recoverable` 并退出，
由操作者按事务维护契约显式恢复。重复卡、reparse 和持久损坏失败关闭，不自动修复。

期限覆盖锁争用与各读取阶段之间的取消检查；文件打开、内核 I/O、关闭仍受操作系统
控制，不承诺硬实时终止。POSIX 使用非阻塞 flock 尝试，Windows 使用
LockFileEx 的 FAIL_IMMEDIATELY；已有阻塞写锁和安全路径入口不变。

## 有界探测与输出生命周期

扫描、探测和输出分别持有自己的运行资源。扫描不等待外部 Agent CLI；初始快照
入队后启动第一批存活采集，后续心跳或派回确认期限在上一批已结束时启动下一批。每次调用复用
`ClassifyTasksContext` 的默认总预算 10 秒、并发 4。订阅最多持有一批在途采集、
一个完成结果槽和一份缓存，不因 refresh 或心跳积压更多探测批次。

心跳沿用 `agent/status/channel/detail`，增加以下字段：

- `revision` 和 `identity`：本次卡片 revision 与请求身份；结果必须同时匹配才可消费。
- `observed_at`、`age_seconds`：真实采集结束时间与生成心跳时的年龄；没有采集则省略时间。
- `runtime_state`、`observation_valid`：复用批量采集事实，不从 alive 推导 ready 或业务进展。
- `collection_state`：`pending` 表示当前 revision 没有结果，`complete` 表示已尝试采集，
  `not-observed` 表示批量未实际采集；`collecting` 独立表示订阅是否有批次运行。
- `stale`：缓存年龄超过 heartbeat 加 10 秒时为 true，此时 status/runtime_state 变为
  unknown、observation_valid 为 false；保留原观测时间与年龄，不冒充新观测。
- `new_window`：有反查地址建议时保留，不回写卡片。

revision 或 SESSION/WINDOW/OWNER/STARTED_AT 改变时丢弃旧结果，输出 pending/unknown。
即使其他卡持续变化，心跳仍按独立时钟报告当前缓存。慢批次未结束时允许 pending，
不能把这种 unknown 当作 Agent 已停止。结果覆盖与输入集合均为 O(监听卡数)，
不是无限历史队列；单批总预算耗尽后未出队项维持 unknown。

输出只有一个 worker，最多排队 16 行，另有一行正在写出；每行最多 1 MiB（含换行）。
每行的 2 秒期限从入队开始计算，包含排队时间。队列满、单行超限、短写、断管或
写出超时均明确报错并结束订阅，不丢弃事件后继续伪装完整流。退出可能留下尚未写出的
行或部分末行；消费者只解析完整 JSON 行，重连后重新核对 snapshot/revision。
心跳间隔从快照/心跳入队后开始，不等待消费者读取；慢消费者在限额内保持 FIFO。
全体 done 不自动退出，仍由消费者决定何时停止。错误诊断的 stderr 写入同样限 2 秒；
即使 stdout/stderr 指向同一停读管道，也不会因诊断再次无限等待。无法写出诊断时
以非零退出通知失败，最坏会比输出失败多等待一个诊断期限。

`SubscribeContext` 接收调用方 context；旧 `Subscribe` 保留停止通道包装，关闭 stop
返回成功。CLI 将 Ctrl+C 作为正常退出，POSIX 同时处理 SIGTERM/SIGHUP；Windows
处理 Go 暴露的 console Ctrl+Break、关闭、注销、关机通知。强制 kill 不运行清理钩子。
所有正常退出路径取消并等待探测 worker、输出 worker、停止通道适配器与平台取消回调，
不遗弃阻塞 goroutine。读锁争用也复用调用方 context 与原 2 秒读取期限。

自定义 `io.Writer` 必须实现 `ContextWriter.WriteContext`，在取消后返回且不遗留后台
写入；普通无法取消的 Writer 在首次写入前拒绝。`bytes.Buffer`、`strings.Builder`、
`io.Discard` 保持兼容。调用期间输出目标由订阅独占，调用方不得同时写入、关闭或
修改描述符属性；违反 ContextWriter 契约的实现无法获得退出保证。

平台适配的边界如下：

- POSIX：对传入文件描述符启用 nonblocking，EAGAIN 时每 5ms 检查取消/期限；
  不等待满管道，writer 汇合后恢复原文件状态标志，不关闭调用方文件。描述符副本
  共享这些标志，因此调用方必须同时约束别名的使用。
- Windows：支持 deadline 的 overlapped 文件使用 Go poller 和写期限；同步句柄在
  固定 OS 线程写入，以 CancelSynchronousIo 取消，并等待取消线程汇合。取消请求
  与进入写调用之间的竞争以 5ms 重试覆盖。控制台保留 Go 的 Unicode 写出。
  [Microsoft 的取消契约](https://learn.microsoft.com/en-us/windows/win32/api/ioapiset/nf-ioapiset-cancelsynchronousio)
  不保证每一种内核 I/O 都立即完成；实现保留并等待真实完成，不伪造已回收。

文件打开、普通磁盘/网络文件系统 I/O、驱动响应、进程创建/回收及系统调度仍受 OS
控制；上述期限不是不可取消内核操作的硬实时保证。JSON 编码在单行大小检查之前，
仍需要与当前监听集合成比例的临时空间。自动测试使用临时看板和假 CLI，真实
终端、真实 tmux/herdr/Agent、原生 Windows 与交叉编译结果必须分别记录。
