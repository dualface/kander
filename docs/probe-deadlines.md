# 探测期限与取消

## 调用契约

单卡存活探测的前向查询、session 反查、反查后的复查及进程回收共享一个 context。`ClassifyTaskContext` 与 `ClassifyTaskLookupContext` 接受调用方 context；未设置 deadline 时补充 10 秒默认期限。已设 deadline 保持不变，包括比默认值更长的显式期限。取消或期限耗尽后的观测为 `unknown`，保留阶段与原因，不启动后续外部查询，不写卡，不改变 `check` 的结构检查退出码。

`probe.CaptureContext`、`ProbeHerdrPaneContext`、`ProbeTmuxPaneContext`、`ProbeTmuxContainerContext`，以及 `liveness.HerdrReverseLookupContext`、`TmuxReverseLookupContext` 供组合调用复用同一个期限。`probe.WithDefaultTimeout` 只在缺少 deadline 时补充默认值；调用方负责执行返回的 cancel。底层错误仍支持 `errors.Is(err, context.Canceled)` 和 `errors.Is(err, context.DeadlineExceeded)`；中文、英文、日文展示保留原始原因。

既有不带 context 的 API 及 `Within`/duration API 保留，非正 duration 使用 10 秒默认值。一次 tmux pane 查询包含事实与两个兼容 session marker 的读取，这些步骤也共享剩余预算。`check` 和现有订阅调用便捷 API，因此每张卡使用完整的单卡预算；整轮并发调度、订阅停止信号及写出取消不属于本契约。

## 进程与管道所有权

- POSIX：外部探测程序在独立进程组运行。取消时向该组发送 `SIGKILL`；直接子进程由 `Wait` 回收。正常父进程退出后也终止其遗留的组内后代。已重新设置 session/进程组的后代超出这一所有权边界，不能声称已被进程组终止；取消仍关闭本次采集的管道读端，避免它们持有输出写端拖住调用。非直接子进程的 zombie 由其新父进程回收，不能把 zombie 记录误当作仍可运行或持有管道的进程。
- Windows：先创建带 `KILL_ON_JOB_CLOSE` 的 Job Object，再挂起创建外部探测进程，加入 Job 后才恢复线程，防止分配前抢跑并产生未归属的后代。不启用 breakaway。分配、线程恢复或其他准备失败直接报错并回收，不退回裸进程。正常结束及取消均终止 Job；关闭 Job 句柄作为最终回收保障。通过外部代理服务创建的进程不属于本 Job；普通继承进程和输出管道在本次取消范围内。Job 的继承规则参见 [Microsoft Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)。
- 两个平台均由采集器持有 stdout/stderr 管道。取消回调关闭读端、终止所属进程，主调用等待直接进程、两个输出读取 goroutine 和已启动的取消回调全部退出后才返回。父进程已结束而脱离组的后代仍持有管道时，原 deadline 仍会关闭读端。不会重置一个新的回收期限，也不启动无人等待的 `Wait` goroutine 来伪装有界结束。正常输出全部读取，保留退出码与 stdout/stderr；取消时返回的输出可能是已采集的部分内容。

## 期限的实际边界

期限控制何时停止继续工作、关闭可取消管道及发出进程终止请求，不是硬实时返回保证。取消后的终止、调度、直接进程回收和 goroutine 汇合需要少量系统执行时间；回归测试明确断言 300ms 期限在 600ms 内返回、400ms 共享期限在 650ms 内返回，以及显式取消在 300ms 内返回。这个容差只属于测试，不是运行时追加的预算。

路径查找、`exec.Start`/`CreateProcess`、线程/Job 系统调用、信号投递及内核进程回收本身没有 Go context 中断入口。不可中断的内核 I/O、失去响应的文件系统、内存分配/JSON 解码及系统调度暂停可能超过期限。实现不会为满足表面期限丢弃仍阻塞的 goroutine；这些操作恢复后继续完成资源回收，并报告真实取消或错误。输出大小限制、整轮队列与调度不在本次变更范围内。

## 验证范围

`internal/probe` 的真实测试子进程覆盖父子进程持有两路输出、取消、期限耗尽、父进程先退出、输出完整性及退出码；同一套测试可在 POSIX 与 Windows 运行，平台文件分别检查进程组及 Job 回收结果。POSIX 另测主动脱离进程组的后代，其管道等待仍受限，测试自行清理该后代。`internal/liveness` 以临时假 CLI 验证慢前向查询、慢反查、慢复查共用剩余预算，以及取消发生在反查期间的结果。

Linux 实际执行与 Windows 交叉编译须在交付记录中分别列明。交叉编译不能证明 Windows Job、管道取消和句柄回收在原生 Windows 上通过；假 CLI 也不能证明真实 tmux/herdr/Agent 的兼容性。未执行的环境检查必须保留为验证缺口。
