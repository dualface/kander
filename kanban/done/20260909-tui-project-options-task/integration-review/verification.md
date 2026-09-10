# Verification Evidence

最终交付提交：ead0a736302a273b5ad2489fab6e1d22265b5d24。

`go test ./... -json -count=1 -timeout 240s`：进程退出 0；1413 个测试通过，1 个 Windows 专属测试跳过，21 个包通过。逐项 JSON 结果已在本次执行核对。下列日志是从原始 JSON 提取的包结论和 PTY 验证结论：

```text
ok  	github.com/dualface/kander/internal/flow	0.013s
ok  	github.com/dualface/kander/internal/focus	0.017s
ok  	github.com/dualface/kander/internal/cli	0.018s
ok  	github.com/dualface/kander/cmd/kander	0.098s
ok  	github.com/dualface/kander/internal/i18n	0.117s
ok  	github.com/dualface/kander/internal/process	0.016s
ok  	github.com/dualface/kander/internal/fs	0.494s
ok  	github.com/dualface/kander/internal/version	0.002s
ok  	github.com/dualface/kander/rules	0.002s
ok  	github.com/dualface/kander/internal/config	0.665s
ok  	github.com/dualface/kander/internal/window	0.111s
ok  	github.com/dualface/kander/internal/install	0.843s
ok  	github.com/dualface/kander/internal/probe	0.720s
ok  	github.com/dualface/kander/internal/takeover	1.069s
ok  	github.com/dualface/kander/internal/notify	6.098s
--- PASS: TestBareKanderBootstrapsConfigAndOpensInterfaceOptionsOnPTY (2.54s)
--- PASS: TestBoardOpensOptionsPanelOnPTY (1.56s)
--- PASS: TestOptionsProjectTabsAndNarrowPathsOnPTY (1.96s)
ok  	github.com/dualface/kander/internal/tui	11.945s
ok  	github.com/dualface/kander/internal/menu	15.705s
ok  	github.com/dualface/kander/internal/launch	18.432s
ok  	github.com/dualface/kander/internal/liveness	25.621s
ok  	github.com/dualface/kander/internal/review	35.625s
ok  	github.com/dualface/kander/internal/board	48.104s
```
