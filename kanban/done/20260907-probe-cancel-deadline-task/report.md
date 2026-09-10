# Kanban 任务完成报告

- Task: [20260907-probe-cancel-deadline-task — 单次探测取消后有界退出](spec.md)
- Delivery: 单卡前向探测、session 反查、复查和进程回收共享期限；取消关闭输出管道并终止所属进程组/Job，等待直接子进程、读取 goroutine 和取消回调收敛。保留便捷 API 默认值及三语错误，平台及不可中断 I/O 边界已交付。
- Acceptance: 4/4，按冻结契约完成实现与约定验证。第 1 项 context、默认期限和剩余预算已验证；第 2 项 POSIX/Windows 后端、管道取消与资源汇合已实现，Linux 运行通过、Windows 仅交叉编译；第 3 项原审计反转、慢反查、预算耗尽、进程/goroutine 收敛及时间上界断言通过；第 4 项测试、文档、三语消息、自动检查与适用审核完成。原生 Windows 和真实终端/Agent 未运行已按契约披露，不记为实机通过。
- Verification: 作者首轮全量、受影响五包 race、build/vet/gofmt/diff 与 Windows 交叉编译通过；审核派回的专项目标以 -count=1 实际重跑通过，详见 [作者处置](review-round1-disposition.md)。主控在最终 e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88 的全量、五包 race、build/vet/diff、Windows amd64 交叉 build 均 exit=0，已读取并附存 [最终验证日志](final-validation.txt)，不冒称由作者本次重新执行。本次作者实际 fetch、最终交付及映射提交到本地/远端 develop 的四次祖先检查、核心路径内容比对均通过；定向 `kander check 20260907-probe-cancel-deadline-task` 输出 `ok: 1 tasks`，exit=0。
- Review: PM/Codex、QA/Grok 首轮在 base 4889d3fb93f8d2d639832b9da5588d668714d9bc、target 96b59578954da4cc208684333f293d47bb4b2a0e 无 gate；[PM 原文](pm-round1-original.md)、[QA 原文](qa-round1-original.md) 保留。PM 按重放规则承接原结论，不宣称其实际审过最终新 SHA。QA/Grok 对映射 reviewed-commit 06412f4f51faed0d1291e157824dfabf66cb8faa 至最终 e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88 的 E1 夹具适配增量通过，无新增建议，见 [增量原文](qa-incremental-original.md)。CSA/Hacker N/A。本卡两项非阻断保持作者处置，无审核后生产代码修改，无新 Reviewer 调用。
- Wrap-up: 最终交付 e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88；本卡原交付 96b59578954da4cc208684333f293d47bb4b2a0e 映射为 06412f4f51faed0d1291e157824dfabf66cb8faa。本地 develop 与 origin/develop 均为最终交付，主工作树干净。作者从主工作树依次成功删除自身 worktree、本地 probe-cancel-deadline 分支及远端任务分支；目录不存在、本地 ref 查询 exit=1、远端 ref 查询 exit=2 且无匹配。旧 SHA 未直接成为 HEAD 祖先的删除提示已按映射验证，不是失败。初次 update 设置 RESULT 被托管字段校验拒绝，随后用 `move done --result completed --expect-revision` 成功原子写入；未绕过工具。卡片已为 done、RESULT completed，定向 check 通过。作者未创建临时审核目录，N/A；普通审核与验证附件保留，不补造机器索引。本卡适用收尾全部完成；组工作树、group/20260907-runtime-observation-group、integrate/runtime-observation-three-20260908、P3/其余 todo 及交互终端按通知保留。
- Unresolved issues (4): [QA-R1][recommend][Rejected] 实际 gofmt -l/-d 无输出，格式失败主张不成立，但 Reviewer 仍保留原项，供用户复核，不冒称撤回；[QA-S1][suggest][Confirmed，未实施] 多层 WithCancel 属实，保持公共入口默认期限，性能影响未经测量，本轮未优化；[验证缺口][Unverifiable] 原生 Windows Job/线程/管道回收未运行，交叉编译不足以证明实机行为；[验证缺口][Unverifiable] 真实 tmux/herdr/Agent 未运行，假 CLI 与真实测试子进程只证明相应隔离场景。完整依据沿用 [作者处置](review-round1-disposition.md)。没有已确认且未修复的 must-fix 缺陷，QA-S2 属 P1，不重复计入本卡。
- Summary: 单次探测取消与期限行为已集成并完成本卡收尾；审核原文和 [集成通知](wrap-up-notice.md) 已按相对链接保留。Code branch: develop；Final card state: done


---

以下保留首次交付和审核派回的历史原文，当前状态以上方完成报告为准。

# 2026-09-08 审核派回状态

最新处置见 [首轮核实记录](review-round1-disposition.md)，所附 [QA 原文](qa-round1-original.md) 与 [PM 原文](pm-round1-original.md) 保留原样。QA-R1 Rejected；QA-S1 Confirmed / suggest / 本轮未实施。无须修改代码，未创建新提交。最终交付与本次最新组基线均为 96b59578954da4cc208684333f293d47bb4b2a0e，远端相同、工作区干净。专项目标实际重跑通过；全量/race 本轮缓存命中，build/vet/格式通过。PM/QA 首轮无 gate（主控通知），未运行新审核。原生 Windows 与真实终端/Agent 缺口不变。回 review 等主控集成及收尾，RESULT 留空。

以下为首轮交付历史，审核状态以本节及首轮核实记录为准。

# 单次探测取消后有界退出：交付记录

## 交付

- 任务：20260907-probe-cancel-deadline-task。
- 任务工作区：/home/dualf/works/kander/worktrees/probe-cancel-deadline。
- 任务分支：probe-cancel-deadline，已推送 origin/probe-cancel-deadline。
- 源分支：group/20260907-runtime-observation-group。
- 创建及最终组基线：387d2362ee91662ef3cc998116c68debf5e08854。
- 最终交付 SHA：96b59578954da4cc208684333f293d47bb4b2a0e。
- 最终 fetch/rebase 无需修改提交；本地 HEAD 与远端任务分支相同，工作区干净，组基线是交付提交祖先。执行 Agent 未更新组分支。

新增 context API 贯穿 Capture、herdr/tmux pane 与 container 查询、session 反查及 liveness。已有 API 保留，未提供 deadline 时使用 10 秒默认值；显式期限原样继承。单卡前向查询、marker 查询、反查、复查及回收共享期限，取消或耗尽预算后不再发起后续查询，liveness 返回 unknown，保留阶段和三语原因。

采集器自持 stdout/stderr 管道；取消时关闭读端、终止所属进程，再等待直接子进程、两个读取 goroutine 及已启动的取消回调退出。POSIX 使用独立进程组；Windows 挂起创建进程、分配带 KILL_ON_JOB_CLOSE 的 Job Object 后恢复线程。准备失败不回落裸进程；正常父进程结束后也回收其所属后代。没有通过丢弃 Wait/读取 goroutine 宣称调用已结束，也没有追加新的回收期限。

README、docs/probe-deadlines.md、AGENTS.md 文档索引和英文发布协议同步说明 API、期限、平台与不可中断系统 I/O 边界。取消及期限错误补充中文、英文、日文消息，并保留底层 errors.Is 语义及原始错误文本。

## 验收与回归证据

1. 共享 context 与默认值：新增 deadline 继承测试，明确覆盖短于和长于默认值的调用方期限；前向 300ms 后进入慢反查的场景，400ms 总预算必须在 650ms 内返回。herdr 两个 150ms 阶段后进入慢复查，也必须满足同一上界。取消期间反查返回 unknown，保留反查原因；预算预先耗尽时不得查找或启动程序。
2. 进程和输出回收：真实测试子进程继承两路输出，覆盖父子仍运行、父进程先结束、正常大量输出与非零退出码、显式取消、循环取消后 goroutine 数量恢复。300ms 期限必须在 600ms 内返回；显式取消须在 300ms 内返回，随后确认父子进程停止。POSIX 另覆盖后代主动 setsid 脱离组后仍持有输出管道，调用仍在原期限内退出，测试显式清理脱离组的后代。Windows 具备同套测试及进程存活检查，已交叉编译，尚无原生运行证据。
3. 反转原审计：基线源码未包含原审计函数，依据审计记录的“后代持有输出导致 deadline 失效”重建 TestAuditDescendantOutputOutlivesProbeDeadline，并在基线 387d2362ee91662ef3cc998116c68debf5e08854 的隔离临时副本运行相同测试。300ms 期限实际 5.011534234s 才返回，新断言按预期失败；修复后测试通过。临时基线位于 /tmp/probe-deadline-baseline-2b0e2dui，仅为复现证据，不是任务工作区。
4. 必要测试、文档、三语消息和自动检查已交付；PM/QA 待主控按组级门禁执行。CSA/Hacker 按仓库特例 N/A。

## 实际验证

最终交付及最终组基线核对后，下列检查通过：

- go test ./...
- go test -race ./internal/probe ./internal/liveness ./internal/notify ./internal/takeover ./internal/launch
- go build ./...
- go vet ./...
- git diff --check；git diff origin/group/20260907-runtime-observation-group --check
- gofmt -l internal/probe internal/liveness，无输出。
- GOOS=windows GOARCH=amd64 go build ./...
- GOOS=windows GOARCH=amd64 go vet ./internal/probe ./internal/liveness
- GOOS=windows GOARCH=amd64 go test -c ./internal/probe -o /tmp/probe-cancel-deadline-windows.test.exe
- GOOS=windows GOARCH=amd64 go test -c ./internal/liveness -o /tmp/liveness-cancel-deadline-windows.test.exe
- git merge-base --is-ancestor origin/group/20260907-runtime-observation-group HEAD
- git diff --exit-code HEAD origin/probe-cancel-deadline

实现中首次进程停止断言在 Linux zombie 转为 dead/reaped 的瞬间重复检查导致误报；测试已将 zombie/dead 均视为不可运行，并在首次观察到停止时结束检查。首次 race 运行受 Go race 子进程默认退出时睡眠 1 秒影响，父进程先退出场景越过测试上界；只为测试子进程设置 GORACE 的 atexit_sleep_ms=0，保留 race 检测，修正后全量及 race 通过。上述失败均未记录为通过。

## 边界及未完成事项

- 原生 Windows 的 Job 分配、线程恢复、管道取消、进程回收测试尚未执行；交叉编译不能代替该验证。
- 真实 tmux/herdr/Agent 未运行。liveness 行为测试使用临时目录和假 CLI，进程回收测试使用真实测试子进程；未修改真实看板或用户配置来测试。
- POSIX 主动脱离进程组的后代、Windows 经外部代理服务创建的进程不在所属组/Job 的终止范围内；采集器的输出等待仍可由原 context 关闭。非直接子进程 zombie 由系统新父进程负责回收。
- 系统路径查找、进程创建、Job/线程 API、内核 I/O/回收、JSON 解码及调度暂停不是可由 Go context 硬中断的操作，不能作硬实时承诺；实现会等待已拥有的资源收敛，不丢弃阻塞 goroutine。这些边界已写入正式文档。
- 已知未修复的本卡代码缺陷：无。多卡调度、订阅停止信号与输出取消仍属后续卡范围。
- PM/QA 尚未执行，由主控安排；CSA/Hacker N/A。组分支接收、组审核、develop 集成和清理尚未执行，保留任务分支及工作区，等待主控派回。

## 结论

本轮实现和 Linux 自动验证完成，交付提交已推送。进入 review 等待主控接收、审核及集成；RESULT 保持空白，不宣称已完成组级交付或原生 Windows 验证。
