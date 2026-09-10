# 多任务存活采集受总预算约束

- TYPE: Bug
- SIZE: small
- TASK_GROUP: 20260907-runtime-observation-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 19:49
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t12:wX:p1M
- STARTED_AT: 2026-09-08 02:07
- FINISHED_AT:
- TASK_BRANCH: probe-batch-budget
- RESULT:

## GOAL

一批任务的存活采集不会随任务数无限叠加超时，并明确每条观测的新鲜度。

## USER_DECISIONS

用户在侧会话明确要求：你按照这一组规则对剩余的卡重新拆分. 已经在跑的如果处于等待状态也可以检查要不要拆. 按单一可验收行为拆分；原四张未启动大卡由新卡替代，原契约与审核历史保留。不是放弃原目标，也不是授权启动、接管或重置审核。


后续实际决定：用户说“拆好就开整吧”，并对本侧独立审卡及启动执行答复“我授权”。该授权取代上方建卡阶段不启动限制；按resplit-plan.md的组交付、审核、develop集成和收尾计划执行，既有审核归档组由原主线程负责。

## EXPECTED_OUTCOME

本卡交付：一批任务的存活采集不会随任务数无限叠加超时，并明确每条观测的新鲜度。

## ACCEPTANCE_CRITERIA

- [ ] 提供批量context入口、总预算与并发上限；预算耗尽的任务保留unknown原因，默认值明确。
- [ ] 结果含observed_at、任务/会话身份、运行状态和观测有效性，保留NewWindow；alive与idle/blocked/done、ready、业务进展分开。
- [ ] 慢命令与多任务混合测试断言总耗时及峰值并发；取消无残留，旧身份结果不可作为新身份事实。
- [ ] 本行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包-race、build/vet/格式检查通过。平台相关测试使用临时目录，原生Windows及真实tmux/herdr/Agent未执行时列明缺口，不把假CLI或交叉编译记为实机通过。PM/QA按适用门禁，CSA/Hacker在本仓库N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

probe/liveness批量入口与结果；不实现subscribe调度、进展超时消费策略。

不引入通用服务、签名、保留期清理或无关优化。发现超出此单一行为的需求必须重新评估并另卡，不能为避免拆卡扩大本契约。

## DISCUSSION

```text
PREREQUISITES: 20260907-probe-cancel-deadline-task
```

依赖必要性与不可拆分理由：P2确保每个执行槽可取消回收，否则无法兑现总预算和并发上限。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本行为交付的一部分，不单独拆成尾部测试任务。

资源边界：probe/liveness批量入口与结果；不实现subscribe调度、进展超时消费策略。 同组卡修改共同运行入口时串行；跨组只在资源可隔离且真实依赖满足时并行。无冲突的已就绪卡不为前卡非阻断建议等待；审核等待须注明具体门禁。

历史来源：20260907-probe-liveness-bounds-task；完整覆盖及主线程交接见 20260907-probe-result-classification-task 的 resplit-plan.md。原卡CARD_REVIEW不继承。

SELF_REVIEW: 通过粒度及契约自检：一个行为、可执行验收、必要依赖和范围排除已逐项核对；组内外依赖与全量映射另见计划。建卡时独立审卡待执行；现已获得下方真实CARD_REVIEW，通过后由主控pick/start。


CARD_REVIEW: PASS — 2026-09-07，独立Agent /root/resplit_card_review（fork_turns=none），已核对完整契约、粒度、必要依赖、覆盖及无环；本卡无阻断。原文记录：/home/dualf/.local/share/kander/orchestration/20260907-resplit-execution/card-review-round1.md。仅契约审查，不代表实现或PM/QA通过。

执行接线：本侧主控负责20260907-runtime-observation-group，组工作区worktrees/20260907-runtime-observation-group。同组P1/P2/E1已完成并集成进develop，组分支已于2026-09-08按用户明确授权重锚到develop 7e6fe2073ff007a6639a2517f4c7874f8e420bd1，该SHA即当前创建锚点与首个审核批次base；旧锚点4889d3fb93f8d2d639832b9da5588d668714d9bc及旧组SHA 96b59578954da4cc208684333f293d47bb4b2a0e仅为历史，重锚前已实测组自身交付路径（internal/probe、internal/liveness、docs/probe-deadlines.md）与develop字节一致，无交付丢失。只从该组分支准备任务工作区，不要回退develop上已集成的修复。受控写卡使用已安装的kander（~/.local/bin/kander，构建自7e6fe20）show --json / update --expect-revision；不要部署新二进制或init真实看板。SIZE: small及本卡单行为边界不变。

## IMPLEMENTATION

### 2026-09-08 首轮交付

工作区：/home/dualf/works/kander/worktrees/probe-batch-budget；任务分支：probe-batch-budget；源分支：group/20260907-runtime-observation-group。
创建及最终组基线：7e6fe2073ff007a6639a2517f4c7874f8e420bd1。
最终交付 SHA：021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5，已推送 origin/probe-batch-budget。最终 fetch/rebase 输出 `Current branch probe-batch-budget is up to date.`；本地 HEAD 与远端任务分支均为最终 SHA，组分支仍为上述基线，工作区干净（021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5）。未更新组分支或 develop；任务工作区与分支保留供主控审核及后续派回。

实现：新增 ClassifyTasksContext、TaskInput、BatchOptions，默认总预算 10 秒、并发上限 4，较早的父 context deadline 优先。排队、前向、反查、复查及回收共用预算；固定 worker 数，返回前全部汇合。已完成结果保留，未启动及超时/取消任务保留 unknown 原因。check 的存活段改用批量入口，维持任务顺序及结构检查退出码；订阅调度和事件 schema 未改。

结果保留 NewWindow，并新增 observed_at、请求身份、runtime_state、observation_valid。身份绑定 TaskID/SESSION/WINDOW/OWNER/STARTED_AT；ValidFor 拒绝失败或身份变化后的旧结果，消费方仍须检查观测年龄。herdr idle/working/blocked/done 与 alive 分开，tmux 运行状态为 unknown；不推断 ready、业务进展或完成。完全相同元数据下的重启仍无法仅凭这些字段区分，这一能力边界已记录。三语消息、README、docs/probe-deadlines.md、AGENTS.md 索引及英文发布协议同步。

验收自检（均基于 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5）：AC1 完成，批量 context/默认总预算/并发上限及 unknown 原因已交付；AC2 完成，观测时间、身份、有效性和独立运行状态已交付；AC3 完成，12 卡混合快慢命令共享 400ms（实际 0.41s），峰值并发 2，默认并发 4 的取消用例实际 0.02s，Linux 进程消失、worker/goroutine 收敛、身份变化和 NewWindow 保留通过；AC4 的实现侧测试、文档、三语消息及构建检查完成，PM/QA 等主控组级审核，原生 Windows 和真实终端/Agent 缺口保留。

交付自检（Delivery Self-Check）：

1. `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`：`git diff --check 7e6fe2073ff007a6639a2517f4c7874f8e420bd1 HEAD` 无输出、exit=0；对全部 6 个改动 Go 文件执行冲突标记 rg 检查，无匹配（exit=1）。
2. `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`：`wc -l` 输出 batch.go=80、batch_test.go=295、observation.go=30、check.go=132、classify.go=250、types.go=67；所有改动 Go 文件均少于 1000 物理行。
3. `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`：人工检查 `git diff 7e6fe2073ff007a6639a2517f4c7874f8e420bd1 HEAD`，变更行为的文档/注释已同步；现有 subscribe 单卡语义及系统 I/O 边界明确保留，无失效描述。
4. `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`：人工逐项核对新增符号及调用点；批量 API 接入 check，观测字段由分类器填写，ValidFor 为已文档化的身份消费 API 并由回归测试覆盖，无新增死代码。
5. `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`：人工核对 7 个新增测试，分别验证混合总预算、默认并发/取消回收、父期限、未采集/读取失败/三语、check 接线与顺序、身份/运行状态、反查地址/tmux 未知运行状态；未发现重复或无关断言。
6. `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`：`go test -json -count=1 ./...` exit=0，19 包通过，783 个测试/子测试 PASS，1 SKIP，0 FAIL；`go test -race -json -count=1 ./internal/liveness ./internal/probe ./internal/i18n ./internal/launch ./internal/notify ./internal/takeover` exit=0，6 包通过，211 个测试/子测试 PASS，1 SKIP，0 FAIL。两次均在最终提交实际运行，非旧提交缓存证据。
7. `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`：`go build ./...`、`go vet ./...`、6 个改动 Go 文件的 `gofmt -l` 均 exit=0、无输出；`GOOS=windows GOARCH=amd64 go build ./...` 与 `GOOS=windows GOARCH=amd64 go test -c -o /tmp/probe-batch-liveness-windows.test.exe ./internal/liveness` 均 exit=0。测试计数、包级输出及专项耗时见 [validation.txt](validation.txt)。

历史验证失败与修复：首次提交 b8ea5da7f3f8e64b891a4eb74a396d19860171ef 的全量实跑捕获夹具 PID 空文件竞态，原文为 `owned probe process  remains: <nil>`。最终提交 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5 等待有效 PID 再取消并保留进程消失断言；上述最终全量与 race 均已复测通过，未把旧失败写为通过。

审核与后续：PM/QA 由主控组级安排，本轮执行者未启动审核，尚无实现审核 run ID；CSA/Hacker 按仓库 AGENTS.md 标记 N/A。原生 Windows、真实 tmux/herdr/Agent 未执行；交叉构建、假 CLI 与 Linux /proc 检查不代表这些实机环境通过。无已知未修代码缺陷；等待主控接收、审核、集成及派回收尾。

## SUMMARY

本卡实现侧交付完成，最终提交 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5，任务分支 probe-batch-budget 已推送。默认 10 秒批量预算/并发 4、check 接入、观测身份/时间/有效性与独立运行状态已交付；AC1–3 通过，AC4 实现侧验证完成，PM/QA 等主控组级审核。最终提交 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5 上全量 19 包（783 PASS、1 SKIP、0 FAIL）、受影响 6 包 race（211 PASS、1 SKIP、0 FAIL）、build/vet/格式与 Windows 交叉检查通过，完整命令和输出见 validation.txt。原生 Windows、真实 tmux/herdr/Agent 缺口保留。CSA/Hacker N/A；组分支/develop 集成及收尾未执行，等待主控派回，任务工作区与分支保留。
