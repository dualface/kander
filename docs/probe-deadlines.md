# 探测期限与取消

## 调用契约

单卡存活探测的前向查询、session 反查、反查后的复查及进程回收共享一个 context。`ClassifyTaskContext` 与 `ClassifyTaskLookupContext` 接受调用方 context；未设置 deadline 时补充 10 秒默认期限。已设 deadline 保持不变，包括比默认值更长的显式期限。取消或期限耗尽后的观测为 `unknown`，保留阶段与原因，不启动后续外部查询，不写卡，不改变 `check` 的结构检查退出码。

`probe.CaptureContext`、`ProbeHerdrPaneContext`、`ProbeTmuxPaneContext`、`ProbeTmuxContainerContext`，以及 `liveness.HerdrReverseLookupContext`、`TmuxReverseLookupContext` 供组合调用复用同一个期限。`probe.WithDefaultTimeout` 只在缺少 deadline 时补充默认值；调用方负责执行返回的 cancel。底层错误仍支持 `errors.Is(err, context.Canceled)` 和 `errors.Is(err, context.DeadlineExceeded)`；中文、英文、日文展示保留原始原因。

既有不带 context 的 API 及 `Within`/duration API 保留，非正 duration 使用 10 秒默认值。一次 tmux pane 查询包含事实与两个兼容 session marker 的读取，这些步骤也共享剩余预算。

## 批量采集与观测身份

`ClassifyTasksContext(ctx, []TaskInput, BatchOptions)` 返回与输入等长、同序的 `[]Report`，不写卡、不缓存。`TaskInput` 提供卡片 Entry 和不可变正文快照；正文读取失败可通过 `ReadError` 保留原因而不探测残缺身份。调用方不得在调用期间并发改写输入切片。

`BatchOptions.Budget` 默认 10 秒，`Concurrency` 默认 4；非正值使用默认值，调用方更早的 deadline 优先。总预算从批量调用开始计时，覆盖排队、前向、反查、复查及回收；每个 worker 同时只执行一个单卡调用。worker 数不超过并发上限和输入数的较小值，不按卡数创建 goroutine。取消或耗尽后不启动外部查询，已完成结果保留，其余结果为 `unknown`，保留取消或超时原因。返回前等待所有 worker 及其采集器回收；预算不是硬实时返回保证，仍有下方系统 I/O 与 O(N) 结果构造边界。

`Report` 在原有字段（包括 `NewWindow`）之外增加：

- `ObservedAt`（JSON `observed_at`）：单卡采集调用结束的 UTC 时间，包含失败尝试；未出队采集或正文读取失败为零值，不能当作新观测。
- `Identity`（JSON `identity`）：请求快照的任务 ID、原始 SESSION、WINDOW、OWNER、STARTED_AT，表示被查询身份，不冒充外部验证过的身份。`NewWindow` 是反查建议的新地址，不改写这个原始身份。
- `RuntimeState`（JSON `runtime_state`）：herdr 已确认的 `idle`、`working`、`blocked`、`done`；无法获知时为 `unknown`，tmux 的进程存活不推导 Agent 运行状态。
- `ObservationValid`（JSON `observation_valid`）：本次能否得到有效的四态存活结论。`unknown` 为 false；不表示观测永久新鲜、可直投、身份已认证或业务有进展。

消费旧结果前调用 `Report.ValidFor(entry, text)`：失败/未采集或身份任一字段变化均拒绝。调用方还须按自己的策略检查 `ObservedAt` 的年龄；本 API 不制定 TTL，不把旧身份结果重新标记为新身份事实。元数据完全相同的重启无法仅凭这些字段区分；无 session reference 或外部未报告 session 时，原有 `alive` 仍只表示当前地址的 Agent 存在，不能升级为会话认证。

`alive` 与运行状态彼此独立；`idle`、`blocked`、`done` 都可能 `alive`。这些结果不提供 `ready` 或业务进展事实，也不能据此决定投递成功、任务完成或接管。直投继续遵守 notify 的独立身份和就绪检查。

`check` 的存活段使用默认批量入口，保留结构检查退出码与任务输出顺序，并显示观测时间、有效性和运行状态。预算只约束存活采集，不覆盖此前的看板扫描/正文读取或之后的输出阻塞。现有 subscribe 仍使用单卡入口；订阅计时、停止信号、批量结果消费和写出取消由后续任务处理，本变更不改订阅调度与事件 schema。

## 进程与管道所有权

- POSIX：外部探测程序在独立进程组运行。取消时向该组发送 `SIGKILL`；直接子进程由 `Wait` 回收。正常父进程退出后也终止其遗留的组内后代。已重新设置 session/进程组的后代超出这一所有权边界，不能声称已被进程组终止；取消仍关闭本次采集的管道读端，避免它们持有输出写端拖住调用。非直接子进程的 zombie 由其新父进程回收，不能把 zombie 记录误当作仍可运行或持有管道的进程。
- Windows：先创建带 `KILL_ON_JOB_CLOSE` 的 Job Object，再挂起创建外部探测进程，加入 Job 后才恢复线程，防止分配前抢跑并产生未归属的后代。不启用 breakaway。分配、线程恢复或其他准备失败直接报错并回收，不退回裸进程。正常结束及取消均终止 Job；关闭 Job 句柄作为最终回收保障。通过外部代理服务创建的进程不属于本 Job；普通继承进程和输出管道在本次取消范围内。Job 的继承规则参见 [Microsoft Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)。
- 两个平台均由采集器持有 stdout/stderr 管道。取消回调关闭读端、终止所属进程，主调用等待直接进程、两个输出读取 goroutine 和已启动的取消回调全部退出后才返回。父进程已结束而脱离组的后代仍持有管道时，原 deadline 仍会关闭读端。不会重置一个新的回收期限，也不启动无人等待的 `Wait` goroutine 来伪装有界结束。正常输出全部读取，保留退出码与 stdout/stderr；取消时返回的输出可能是已采集的部分内容。

## 期限的实际边界

期限控制何时停止继续工作、关闭可取消管道及发出进程终止请求，不是硬实时返回保证。取消后的终止、调度、直接进程回收和 goroutine 汇合需要少量系统执行时间；回归测试明确断言 300ms 期限在 600ms 内返回、400ms 共享期限在 650ms 内返回，以及显式取消在 300ms 内返回。这个容差只属于测试，不是运行时追加的预算。

路径查找、`exec.Start`/`CreateProcess`、线程/Job 系统调用、信号投递及内核进程回收本身没有 Go context 中断入口。不可中断的内核 I/O、失去响应的文件系统、内存分配/JSON 解码及系统调度暂停可能超过期限。实现不会为满足表面期限丢弃仍阻塞的 goroutine；这些操作恢复后继续完成资源回收，并报告真实取消或错误。输出大小限制与订阅调度不在本次变更范围内。

## 验证范围

`internal/probe` 的真实测试子进程覆盖父子进程持有两路输出、取消、期限耗尽、父进程先退出、输出完整性及退出码；同一套测试可在 POSIX 与 Windows 运行，平台文件分别检查进程组及 Job 回收结果。POSIX 另测主动脱离进程组的后代，其管道等待仍受限，测试自行清理该后代。`internal/liveness` 以临时假 CLI 验证慢前向查询、慢反查、慢复查共用剩余预算，以及取消发生在反查期间的结果。

Linux 实际执行与 Windows 交叉编译须在交付记录中分别列明。交叉编译不能证明 Windows Job、管道取消和句柄回收在原生 Windows 上通过；假 CLI 也不能证明真实 tmux/herdr/Agent 的兼容性。未执行的环境检查必须保留为验证缺口。

批量回归以临时假 CLI 混合快慢任务，断言 12 张卡共用 400ms 预算、750ms 内返回、峰值并发 2，已完成的 blocked/alive 结果不会因整轮超时被抹去；另测默认并发 4、显式取消 300ms 内返回、排队任务无伪造观测时间、Linux 进程消失及 goroutine 收敛。身份变化、四种 herdr 运行状态、tmux 无运行状态、NewWindow 保留、读取失败及三语未采集原因均有覆盖。
