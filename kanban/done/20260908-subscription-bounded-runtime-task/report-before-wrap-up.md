# 订阅运行时有界：同步派回第 1 轮交付报告

### 2026-09-08 同步派回第 1 轮

- 本次为组分支同步，没有审核 finding；收到通知后自行 review → working，OWNER/SESSION 保持。新组基线：c5dcfbc401d5748da7eb0c89011ea422f51c33cc，来源 origin/group/20260908-subscription-dispatch-group。
- 最终交付：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。两提交映射为 14c8fe627903de0db420a66a931db4906b599c05 → d360f92240961a5883c0f535a6dfc15e65b80350，55aa8f00f408256583ed4261070beba3e437ccad → ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- fetch 成功后 rebase 两提交完成。冲突共 4 文件：AGENTS.md 保留上游“持久派回协议”索引，同时保留本卡扩展后的订阅索引；internal/i18n/locales/en.json、ja.json、zh-CN.json 各保留上游新增派回消息和本卡六项订阅消息。只合并冲突块，没有整文件替换，没有重大代码冲突或范围扩展。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- 三语 JSON 精确比较“新组基线 + 本卡相对原基线的六项增量”，各语言通过；上游对既有消息的合法更新也保留。辅助比较脚本第一次错误要求旧基线全部字符串不变而失败，随后修正比较范围；产品测试结果以下方实际重跑为准。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- `git diff --exit-code 55aa8f00f408256583ed4261070beba3e437ccad HEAD -- internal/liveness docs/subscription-facts.md docs/probe-deadlines.md` exit 0、无输出；Go 实现、测试和两份订阅说明与首轮逐字一致。`git range-diff` 仅显示索引与消息插入处的基线上下文变化，第二提交保持等价。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- `git push --force-with-lease=refs/heads/subscription-bounded-runtime:55aa8f00f408256583ed4261070beba3e437ccad origin HEAD:refs/heads/subscription-bounded-runtime` 成功；`git ls-remote origin refs/heads/subscription-bounded-runtime refs/heads/group/20260908-subscription-dispatch-group` 确认远端任务分支为 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe、组分支仍为 c5dcfbc401d5748da7eb0c89011ea422f51c33cc。没有更新组分支或 develop。核验提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。

#### 本轮交付自检（七项）

1. `git diff --check c5dcfbc401d5748da7eb0c89011ea422f51c33cc..HEAD` exit 0、无输出；改动 Go 文件冲突标记搜索 `rg` exit 1、无匹配。冲突 JSON 解析和索引块核验完成，无残留标记。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
2. `git diff --name-only c5dcfbc401d5748da7eb0c89011ea422f51c33cc..HEAD` 枚举后的 15 个非生成 Go 文件均 ≤1000 物理行，最大 824 行；`gofmt -l <15 个文件>` exit 0、无输出。逐文件计数见本轮验证汇总。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
3. 对照最终 `git range-diff`、4 个冲突块和发布协议差异，订阅行为文档与此前代码保持一致；持久派回文档索引、三语消息及自动合并的派回/订阅规则均保留。无过期同步写出注释。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
4. 本轮没有新增 Go 代码；上面的逐字一致比较确认原调用链、worker/回调汇合及错误处理完整保留，未增加不可达或无引用代码。`go vet ./...` exit 0、无输出。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
5. 本轮未新增或复制测试；Go 测试与原交付逐字一致，原覆盖边界保持，无新增冗余断言。定向回归与全量均实际重跑。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
6. `go build ./...` exit 0；`go test -json -count=1 ./internal/liveness -run 'TestAuditProbe|TestAuditBlockedWriter|TestSubscribeChangesStillObserveAgentDeath|TestSubscription|TestSubscribeQueueOverflow'` exit 0，1 包、12 顶层测试通过，含子用例 22 PASS / 0 SKIP / 0 FAIL。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
7. `go test -json -count=1 ./...` exit 0，19 包、584 顶层测试通过，含子用例 899 PASS / 1 SKIP / 0 FAIL；`go test -json -race -count=1 ./internal/liveness ./internal/probe ./internal/i18n` exit 0，3 包、81 顶层测试通过，含子用例 175 PASS / 0 SKIP / 0 FAIL。未沿用旧 SHA 的测试结果。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。

#### 本轮附加验证与交付边界

- `GOOS=windows GOARCH=amd64 go build -o /tmp/subscription-runtime-final.exe ./cmd/kander` 与 `GOOS=windows GOARCH=amd64 go test -c -o /tmp/subscription-runtime-final-windows.test.exe ./internal/liveness` 均 exit 0。仅交叉编译，原生 Windows 仍未运行。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- 重建当前二进制后重跑真实 PTY/herdr 只读冒烟：snapshot/heartbeat 共 2 行，alive、runtime_state=working、observation_valid=true；Ctrl+C exit 0，3.19ms 退出。未改动或终止真实 Agent。真实 tmux 与完整真实 Agent 故障演练仍未运行；慢探测/死亡/停读组合依靠临时假 CLI、真实管道及子进程自动测试。提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- `git merge-base --is-ancestor c5dcfbc401d5748da7eb0c89011ea422f51c33cc HEAD` exit 0，`git status --short` 无输出；任务分支和 /home/dualf/works/kander/worktrees/subscription-bounded-runtime 保留，等待协调者接收。核验提交：ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe。
- PM/QA 未执行，由协调者安排；CSA/Hacker 按本仓库 AGENTS.md 为 N/A。没有审核 run ID，没有 finding 或作者 disposition 要求。组接收、develop 集成及收尾均待协调者后续调度。
- [本轮验证汇总](verification/sync-1.json)、[全量原始日志](verification/sync-1/all-tests.jsonl)、[定向原始日志](verification/sync-1/targeted-tests.jsonl)、[race 原始日志](verification/sync-1/race-tests.jsonl)。首轮证据保留原 SHA，仅作为历史。

## 本轮结论

已同步最新组基线 c5dcfbc401d5748da7eb0c89011ea422f51c33cc，最终交付 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 已以指定 --force-with-lease 更新远端 subscription-bounded-runtime。4 个冲突文件仅做索引/消息的逐块合并，Go 代码与测试未改；任务分支可基于所记录组基线 ff 接收。

在 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 上重新执行：全量 19 包、584 顶层测试通过（899 PASS / 1 SKIP，含子用例）；定向 12 顶层（22 PASS）、三包 race 81 顶层（175 PASS）、build/vet/格式与 Windows 交叉编译均通过；真实 PTY/herdr 只读冒烟和 Ctrl+C 正常退出通过。详情见 [本轮验证汇总](verification/sync-1.json) 与 [交付报告](report.md)。

六项行为验收保持，第七项的开发验证已重跑；PM/QA、组分支接收、develop 集成和收尾仍待协调者，CSA/Hacker N/A。原生 Windows、真实 tmux 和完整真实 Agent 故障演练缺口保留。分支、worktree 与交互 CLI 保留；本轮回到 review 等待主控接收，不宣称 done。

首轮完整实现与验收记录保留于 [delivery-before-sync.md](delivery-before-sync.md)。
