Role: PM  
Commit: ef9f64043854fb3c284567f602d48d627f9b7df3  
Task Context: `task-spec.md`；修复提示词与最终消息错配。  
Reviewed Scope: 三个变更文件，以及提示词投递、进程启动、报告提取和结构化校验链路。

Observed：未发现 gate findings，未见越界改动。

- `internal/review/git.go:169` 契约明确报告全文、围栏、停止输出及清理顺序；仅对 claude/cursor/grok 注入，其余提示词保持原样。
- `internal/review/args.go:70` 将同一契约传给 Claude `--append-system-prompt`；其他分支参数不变。
- `internal/review/prompt_contract_test.go:24` 起覆盖契约、普通及增量提示词、参数差异。既有 review/board 测试及 `internal/board/review_findings.go` 均无改动。

六项验收：Complete 5，Unverifiable 1；Partial、Missing、Contradicted 均为 0。

Observed：工作区干净，`gofmt -l .`、差异空白检查通过。Unverifiable：只读审查未重跑全量 test/vet；调用方记录两者通过，21 个包 ok。

NON-BLOCKING: none

```kander-findings
{"FINDINGS":[],"NON_BLOCKING":[]}
```