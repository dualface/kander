# A cancelled single probe exits within bounds

- TYPE: Bug
- SIZE: small
- TASK_GROUP: 20260907-runtime-observation-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 19:49
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:tY:wX:p1G
- STARTED_AT: 2026-09-07 20:40
- FINISHED_AT: 2026-09-08 01:34
- TASK_BRANCH: probe-cancel-deadline
- RESULT: completed

## GOAL

同一任务的前向探测、反查及进程回收共享一个期限，取消不会挂在后代输出管道上。

## USER_DECISIONS

用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。


后续实际决定：用户说“拆好就开整吧”，并对本侧独立审卡及启动执行答复“我授权”。该授权取代上方建卡阶段不启动限制；按resplit-plan.md的组交付、审核、develop集成和收尾计划执行，既有审核归档组由原主线程负责。

## EXPECTED_OUTCOME

本卡交付：同一任务的前向探测、反查及进程回收共享一个期限，取消不会挂在后代输出管道上。

## ACCEPTANCE_CRITERIA

- [ ] context 贯穿 Capture、pane、session 反查及 liveness；前向和反查共用剩余预算，保留便捷API合理默认值。
- [ ] POSIX/Windows 取消覆盖子进程和继承输出管道，回收及输出等待有界；不以丢弃goroutine冒充结束，列明不可中断系统I/O边界。
- [ ] 反转 TestAuditDescendantOutputOutlivesProbeDeadline，覆盖慢反查、预算耗尽、取消后进程和goroutine收敛；实际期限边界必须断言。
- [ ] 本行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包-race、build/vet/格式检查通过。平台相关测试使用临时目录，原生Windows及真实tmux/herdr/Agent未执行时列明缺口，不把假CLI或交叉编译记为实机通过。PM/QA按适用门禁，CSA/Hacker在本仓库N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

单次采集调用链和进程后端；不实现多任务队列、订阅计时或业务确认期限。

不引入通用服务、签名、保留期清理或无关优化。发现超出此单一行为的需求必须重新评估并另卡，不能为避免拆卡扩大本契约。

## DISCUSSION

```text
PREREQUISITES: 20260907-probe-result-classification-task
```

依赖必要性与不可拆分理由：P1提供统一错误结果；同一probe API及返回分类不能在两个任务中并发改写。期限贯穿和子进程回收不可拆开，否则调用仍可能不退出。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本行为交付的一部分，不单独拆成尾部测试任务。

资源边界：单次采集调用链和进程后端；不实现多任务队列、订阅计时或业务确认期限。 同组卡修改共同运行入口时串行；跨组只在资源可隔离且真实依赖满足时并行。无冲突的已就绪卡不为前卡非阻断建议等待；审核等待须注明具体门禁。

历史来源：20260907-probe-liveness-bounds-task；完整覆盖及主线程交接见 20260907-probe-result-classification-task 的 resplit-plan.md。原卡CARD_REVIEW不继承。

SELF_REVIEW: 通过粒度及契约自检：一个行为、可执行验收、必要依赖和范围排除已逐项核对；组内外依赖与全量映射另见计划。建卡时独立审卡待执行；现已获得下方真实CARD_REVIEW，通过后由主控pick/start。


CARD_REVIEW: PASS — 2026-09-07，独立Agent /root/resplit_card_review（fork_turns=none），已核对完整契约、粒度、必要依赖、覆盖及无环；本卡无阻断。原文记录：/home/dualf/.local/share/kander/orchestration/20260907-resplit-execution/card-review-round1.md。仅契约审查，不代表实现或PM/QA通过。

执行接线：本侧主控负责20260907-runtime-observation-group，组工作区worktrees/20260907-runtime-observation-group，创建锚点4889d3fb93f8d2d639832b9da5588d668714d9bc。只从该组分支准备任务工作区。需要受控写卡时可使用已交付S/D/A构建的/tmp/kander-side-resplit-20260907/kander show --json / update --expect-revision；不要部署或init真实看板。安装命令当前仍按目录识别large，因此为旧门禁兼容保留report.md；SIZE: small及本卡单行为边界不变。

## IMPLEMENTATION

工作区：/home/dualf/works/kander/worktrees/probe-cancel-deadline。
任务分支：probe-cancel-deadline；源分支：group/20260907-runtime-observation-group。
创建及最终组基线：387d2362ee91662ef3cc998116c68debf5e08854。
最终交付 SHA：96b59578954da4cc208684333f293d47bb4b2a0e，已推送 origin/probe-cancel-deadline；最终 fetch/rebase 无需改动，本地/远端一致，工作区干净。

context API 贯穿 Capture、pane/container、session 反查及 liveness，保留旧 API 与 10 秒合理默认值，显式期限原样继承。前向、反查、复查和回收共享预算，取消/耗尽后不再启动后续查询，结果 unknown，保留阶段、三语原因和 errors.Is 语义。

POSIX 用独立进程组；Windows 挂起创建后加入 Job，再恢复线程。取消时关闭采集器自持输出管道，终止所属进程，并等待直接子进程、两路读取 goroutine 及取消回调退出。正常父进程退出也回收遗留所属后代；没有丢弃 goroutine 或重新分配回收预算。不可中断系统 I/O、脱离组/外部代理进程、非直接后代 zombie 边界已写入 docs/probe-deadlines.md，README、AGENTS.md 索引及英文发布协议同步。

重建并反转 TestAuditDescendantOutputOutlivesProbeDeadline：相同回归在基线隔离副本实际耗时 5.011534234s，违反 300ms 期限及 600ms 返回上界，按预期失败；修复后通过。补充慢前向/反查/复查共享 400ms 预算且 650ms 内返回、显式取消 300ms 内返回、父子进程停止、goroutine 收敛、父进程先退出、POSIX 脱离组后代持有输出、正常输出完整性及退出码测试。

最终 go test ./...、受影响五包 -race、go build ./...、go vet ./...、diff/gofmt 检查通过；Windows amd64 全量交叉 build、probe/liveness vet 及测试二进制编译通过。首次 Linux 进程终止检查及 race 子进程退出延迟导致的测试失败已修正并复测，详见 report.md。

PM/QA 待主控组级审核；CSA/Hacker N/A。原生 Windows 与真实 tmux/herdr/Agent 未验证，不能以交叉编译或假 CLI 代替。未更新组分支或 develop；保留任务工作区及分支。


### 2026-09-08 首轮审核派回核实

本卡已按派回通知先回 working。逐项核实 QA-R1、QA-S1；QA-S2 归 P1，不代改。QA-R1：Rejected，缺少 import 空行属实，但实际 gofmt -l/-d 均无输出，原文声称格式检查失败不成立。QA-S1：Confirmed / suggest / 本轮未实施，多层 WithCancel 属实且共享同一 deadline；公开 context API 仍需独立保留无 deadline 默认期限，缺少性能测量依据，不扩展到等价 context 复用优化。两项均保留在未解决审核项供主控汇总。

专项目标使用 -count=1 实际重跑，通过：300ms 后代输出期限回归 0.30s；herdr/tmux 共享预算各 0.40s；herdr 复查 0.40s；显式反查取消及默认期限继承通过。go test ./... 与受影响五包 -race 通过（本轮均缓存命中）；build/vet/diff/gofmt 检查通过。完整命令、输出、事实依据及处置见 [首轮核实](review-round1-disposition.md)。派回所附 [QA 原文](qa-round1-original.md)、[PM 原文](pm-round1-original.md) 已原样保存，未制造新审核机器索引。

主控通知 PM/Codex 与 QA/Grok 首轮均 exit=0、无 gate，CSA/Hacker N/A；本卡未运行新审核。原生 Windows、真实终端/Agent 验证缺口仍保留。最终 fetch/rebase 后 HEAD、origin/probe-cancel-deadline、本次最新组基线均为 96b59578954da4cc208684333f293d47bb4b2a0e；工作区干净，无新增代码/提交/推送，未修改组分支或 develop。等待主控接收处置并集成、派回收尾。


### 2026-09-08 集成后作者收尾

收到真实收尾通知后已自行 review → working。通知原文及全部审核/集成证据见 [收尾通知](wrap-up-notice.md)，[QA 增量原文](qa-incremental-original.md) 已另存；均为普通附件，未生成审核机器 run/batch 历史。

原任务交付 96b59578954da4cc208684333f293d47bb4b2a0e 映射为 06412f4f51faed0d1291e157824dfabf66cb8faa；最终集成交付 e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88 已在本地及远端 develop。作者本轮实际 git fetch origin 成功，git rev-parse develop origin/develop 输出两行均为最终集成交付；对 e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88 和 06412f4f51faed0d1291e157824dfabf66cb8faa 分别执行到 develop/origin/develop 的 merge-base --is-ancestor，四次 exit=0。原任务 SHA 与最终 develop 在 internal/probe、internal/liveness/classify.go、lookup.go、deadline_test.go、docs/probe-deadlines.md 上 git diff --exit-code 无输出、exit=0，本卡核心实现和回归未丢失。没有使用旧 SHA 的祖先关系判断重写后交付。

任务工作区清理前干净、HEAD 与远端任务分支均为 96b59578954da4cc208684333f293d47bb4b2a0e。从主工作树依次执行 git worktree remove /home/dualf/works/kander/worktrees/probe-cancel-deadline、git branch -d probe-cancel-deadline、git push origin --delete probe-cancel-deadline，均成功。本地删除提示旧 SHA 已合入其 upstream 但未合入 HEAD；这是重写映射后的预期提示，映射后交付祖先关系已独立验证，不是清理失败。复核任务目录不存在、本地 ref 查询 exit=1、远端 ls-remote --exit-code 查询 exit=2 且无匹配，确认本卡资源已删除；主工作树仍干净。组工作树、group/20260907-runtime-observation-group、integrate/runtime-observation-three-20260908 保留，未操作 P3 或其他 todo。未改代码、未重跑 Reviewer、未 rebase 或再次集成，交互 CLI/终端保留。

审核沿用：首轮 PM/Codex、QA/Grok 在 4889d3fb93f8d2d639832b9da5588d668714d9bc..96b59578954da4cc208684333f293d47bb4b2a0e 无 gate；PM 结论按集成规则承接，不声称 PM 审过最终 SHA。QA/Grok 增量核验 06412f4f51faed0d1291e157824dfabf66cb8faa..e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88 的 E1 夹具适配，通过且无新增建议；CSA/Hacker N/A。QA-R1 recommend/Rejected、QA-S1 suggest/Confirmed 未实施按 [作者处置](review-round1-disposition.md) 保留，Reviewer 未撤回原项。QA-S2 属 P1，不计作本卡新增问题。

已读取并附存主控 [最终验证日志](final-validation.txt)：最终 e0c3dab 上 go test ./...、受影响五包 -race、go build ./...、go vet ./...、git diff --check、Windows amd64 交叉 build 均 exit=0。日志显示全量测试部分缓存命中，race 五包均有实际耗时。此处为主控执行证据，本次作者只做只读核验和清理，未冒称自己重跑。原生 Windows、真实 tmux/herdr/Agent 未执行的缺口继续保留。

首次用 update 修改 RESULT 被工具拒绝：`Managed field or document cannot be edited through update: RESULT`，未写入任何该次正文变更。按 move 的实际协议改用受控 `move done --result completed --expect-revision` 原子设置托管字段及状态，不绕过工具；最终状态和定向检查结果写入完成报告。


最终受控 move done --result completed 成功；定向 kander check 输出 `ok: 1 tasks`，exit=0。完整八字段完成报告见 [report.md](report.md)。首次实施/验证及派回核实正文均保留，仅追加收尾事实。

## SUMMARY

本卡已完成，RESULT completed。最终集成交付 e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88 已在本地/远端 develop，本卡原交付 96b59578954da4cc208684333f293d47bb4b2a0e 映射为 06412f4f51faed0d1291e157824dfabf66cb8faa，祖先关系与核心路径内容已独立核验。4/4 按契约完成实现、约定验证及审核；原生 Windows、真实终端/Agent 验证缺口继续保留，不冒称实机通过。PM/Codex 首轮结论承接，QA/Grok 首轮与 E1 夹具适配增量无 gate，CSA/Hacker N/A。QA-R1 recommend/Rejected、QA-S1 suggest/Confirmed 未实施按作者处置保留，Reviewer 未撤回原项。自身任务 worktree、本地及远端分支已删除，主工作树干净，定向 check 通过；组资源、暂停任务和交互 CLI/终端保留。八字段完成报告及所有原文/验证证据见 report.md 的相对链接。
