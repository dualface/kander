# 收尾集成验证

本页记录最终提交 `781aa0dcde99eefe0d7ccea84f5a138f414e831c`；之前 verification.md 的 b208333 验证和审核原件保留为历史证据。

用户于 2026-09-08 回复「收尾」，授权上一轮确认的合入 develop、同步并清理任务分支/worktree。收尾期间 develop 先后合入窗口跳转、tmux exec 前缀和单卡规则文档。已完成三次 rebase；最终基线为 b8a24d2。第一次仅解决三语 JSON 互不重叠新增键的插入位置，逐键断言旧任务全部键值与上游窗口跳转全部键值保留；后两次没有冲突，range-diff 三笔补丁均为等价。没有手工解决实质代码冲突，按一次审核门禁保留已闭批的 PM/QA 结论，不改写历史原件。

以下每项结果均对应最终提交 `781aa0dcde99eefe0d7ccea84f5a138f414e831c`：

- `go test ./... -json`：退出 0；20 包通过，1147 条测试记录通过（含子用例），1 条平台跳过，0 失败；stderr 为空。跳过：github.com/dualface/kander/internal/launch/TestWindowsConsoleLauncher。
- `go build ./...`、`go vet ./...`：退出 0，无输出。
- `git diff origin/develop...HEAD --check`：退出 0，无输出。三次 rebase 没有修改 Go 补丁，已审核代码的格式保持不变。
- `GOOS=windows GOARCH=amd64 go test -exec=true ./internal/config ./internal/launch ./internal/menu ./internal/tui ./internal/liveness ./internal/notify ./internal/takeover ./internal/probe ./internal/review ./internal/process`：退出 0，10 包交叉编译通过。原生 Windows 没有执行，仍为环境验证缺口。
- `go build -o /tmp/kander-agent-under-test ./cmd/kander && python3 /tmp/kander-agent-smoke.py`：退出 0。独立临时目录、配置、看板及 CLI 替身，真实 tmux；改名 codex 和自定义模板均 start 成功、check alive，prompt 保持末位，空模型参数省略。验证窗口已关闭。
- `kander review progress`：退出 0，状态 closed；`kander check 20260908-agent-executable-override-task`：退出 0，通过 1 个任务。状态迁移后的 check 在交付报告另行记录。

## Windows 交叉编译输出

```text
ok  	github.com/dualface/kander/internal/config	0.001s
ok  	github.com/dualface/kander/internal/launch	0.001s
ok  	github.com/dualface/kander/internal/menu	0.001s
ok  	github.com/dualface/kander/internal/tui	0.001s
ok  	github.com/dualface/kander/internal/liveness	0.001s
ok  	github.com/dualface/kander/internal/notify	0.001s
ok  	github.com/dualface/kander/internal/takeover	0.001s
ok  	github.com/dualface/kander/internal/probe	0.001s
ok  	github.com/dualface/kander/internal/review	0.001s
ok  	github.com/dualface/kander/internal/process	0.001s
```

## 真实 tmux 输出

```text
ROOT /tmp/kander-agent-smoke-w87x4y_l
START codex started: 20260908-smoke-codex-task	scale=small	agent=codex	session=kb-kander-agent-smoke-w87x4y_l-8622f00f	window=@57
view: tmux attach -t kb-kander-agent-smoke-w87x4y_l-8622f00f
CHECK codex liveness: 20260908-smoke-codex-task	Agent=codex	status=alive	channel=tmux-session	container=kb-kander-agent-smoke-w87x4y_l-8622f00f:@57	observed_at=2026-09-08T03:26:12.73351215Z valid=true runtime_state=unknown	reason=Agent is reachable
ok: 1 tasks
PROCESS kander-codex
ARGV ["/tmp/kander-agent-smoke-w87x4y_l/kander-codex","--model","gpt-5.6-sol","--config","model_reasoning_effort=\"medium\"","--dangerously-bypass-approvals-and-sandbox","Execute Kanban task 20260908-smoke-codex-task; full instructions are in the UTF-8 task file at /tmp/kander-20260908-smoke-codex-task-start-1276334486.md; read the complete file first and follow it exactly."]
START template started: 20260908-smoke-template-task	scale=small	agent=template	session=kb-kander-agent-smoke-w87x4y_l-8622f00f	window=@58
view: tmux attach -t kb-kander-agent-smoke-w87x4y_l-8622f00f
CHECK template liveness: 20260908-smoke-template-task	Agent=template	status=alive	channel=tmux-session	container=kb-kander-agent-smoke-w87x4y_l-8622f00f:@58	observed_at=2026-09-08T03:26:13.438223234Z valid=true runtime_state=unknown	reason=Agent is reachable
ok: 1 tasks
PROCESS template-agent
ARGV ["/tmp/kander-agent-smoke-w87x4y_l/template-agent","--session-id","cc00f629-8075-40a6-bb6b-b3220475513f","Execute Kanban task 20260908-smoke-template-task; full instructions are in the UTF-8 task file at /tmp/kander-20260908-smoke-template-task-start-3441200593.md; read the complete file first and follow it exactly."]
CLEANED_WINDOWS ['@57', '@58']
```
