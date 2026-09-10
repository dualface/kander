# 最终交付验证

提交：`b208333dc128f4c8e72aae5308685bbaaaef417e`。

- `go build ./...`、`go vet ./...`：退出 0、无输出。
- `go test ./... -json`：{"test pass": 1101, "package pass": 19, "test skip": 1}。跳过项为 Linux 上的 `TestWindowsConsoleLauncher`。
- 40 个变更 Go 文件均不超过 1000 行，最大 864 行；`gofmt -l`、`git diff <base>..HEAD --check` 无输出。

## 真实 tmux 手工验证

提交 `b208333dc128f4c8e72aae5308685bbaaaef417e` 构建的程序，独立临时配置/看板和 CLI 测试替身。用于验证程序解析、argv、会话标记与存活；不声称真实 LLM 服务的认证或输出已验证。

```text
ROOT /tmp/kander-agent-smoke-vtxocymk
START codex started: 20260908-smoke-codex-task	scale=small	agent=codex	session=kb-kander-agent-smoke-vtxocymk-9e3c058a	window=@51
view: tmux attach -t kb-kander-agent-smoke-vtxocymk-9e3c058a
CHECK codex liveness: 20260908-smoke-codex-task	Agent=codex	status=alive	channel=tmux-session	container=kb-kander-agent-smoke-vtxocymk-9e3c058a:@51	observed_at=2026-09-08T02:56:16.915477666Z valid=true runtime_state=unknown	reason=Agent is reachable
ok: 1 tasks
PROCESS kander-codex
ARGV ["/tmp/kander-agent-smoke-vtxocymk/kander-codex","--model","gpt-5.6-sol","--config","model_reasoning_effort=\"medium\"","--dangerously-bypass-approvals-and-sandbox","Execute Kanban task 20260908-smoke-codex-task; full instructions are in the UTF-8 task file at /tmp/kander-20260908-smoke-codex-task-start-1485565355.md; read the complete file first and follow it exactly."]
START template started: 20260908-smoke-template-task	scale=small	agent=template	session=kb-kander-agent-smoke-vtxocymk-9e3c058a	window=@52
view: tmux attach -t kb-kander-agent-smoke-vtxocymk-9e3c058a
CHECK template liveness: 20260908-smoke-template-task	Agent=template	status=alive	channel=tmux-session	container=kb-kander-agent-smoke-vtxocymk-9e3c058a:@52	observed_at=2026-09-08T02:56:17.705524707Z valid=true runtime_state=unknown	reason=Agent is reachable
ok: 1 tasks
PROCESS template-agent
ARGV ["/tmp/kander-agent-smoke-vtxocymk/template-agent","--session-id","c2f90d84-0db0-4e75-87c0-ef5e796c8651","Execute Kanban task 20260908-smoke-template-task; full instructions are in the UTF-8 task file at /tmp/kander-20260908-smoke-template-task-start-3411467044.md; read the complete file first and follow it exactly."]
CLEANED_WINDOWS ['@51', '@52']
```

## Windows 交叉编译

提交 `b208333dc128f4c8e72aae5308685bbaaaef417e`：`GOOS=windows GOARCH=amd64 go test -exec=true` 对以下 10 包交叉编译，未在原生 Windows 执行。

```text
ok  	github.com/dualface/kander/internal/config	0.001s
ok  	github.com/dualface/kander/internal/launch	0.002s
ok  	github.com/dualface/kander/internal/menu	0.002s
ok  	github.com/dualface/kander/internal/tui	0.001s
ok  	github.com/dualface/kander/internal/liveness	0.002s
ok  	github.com/dualface/kander/internal/notify	0.000s
ok  	github.com/dualface/kander/internal/takeover	0.001s
ok  	github.com/dualface/kander/internal/probe	0.001s
ok  	github.com/dualface/kander/internal/review	0.002s
ok  	github.com/dualface/kander/internal/process	0.002s
```

最终收尾核对：提交 `b208333dc128f4c8e72aae5308685bbaaaef417e` 的任务 worktree 无改动，HEAD 与 origin/agent-executable-override 的左右差异为 0/0；`kander check 20260908-agent-executable-override-task` 返回 0，通过 1 个任务；`kander review progress` 返回 0，状态 closed。
