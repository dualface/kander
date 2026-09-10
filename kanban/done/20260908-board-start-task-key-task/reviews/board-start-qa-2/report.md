Role: QA  
Commit: `0eeb06bd84daf863d0817f5d1153f4b04b59f68a`  
Task Context: 棋盘 `s` 启动任务；复核退出生命周期与完整地址展示。  
Reviewed Scope: 完整读取任务指令、规格及增量补丁；仅审核 `c172eef..0eeb06b`。12 个变更文件均匹配 COMMIT TREE。只读环境未重跑测试；采信目标提交记录：定向 208 项通过、1 项跳过；全量 1170 项通过、1 项跳过，均零失败。

| 行为／质量 | 结论与证据 |
|---|---|
| QA-001 | **已关闭 — Observed，高置信度。** `internal/tui/start.go:90–94、98–106、159–165` 在排队时计数，退出请求等待全部结果处理；`internal/tui/program.go:129–138` 随后发出 `tea.Quit`。`internal/tui/start_test.go:266–334` 覆盖 q、Ctrl+C、成功／失败、等待期间事件处理及多个启动。 |
| QA-002 | **已关闭 — Observed，高置信度。** `internal/tui/start.go:118–127、168–183` 提供精简提示及溢出判断；`internal/tui/app.go:139–142` 展示换行浮层。`internal/tui/start_test.go:336–368` 断言 80 列完整页脚地址、40 列换行地址、输入不受阻及到期隐藏。 |
| 模块与回归边界 | **Observed。** 修复位于 TUI 生命周期和展示层，沿用 `pendingWork`／`applyWork`；未改变 launch、board 或 CLI 契约。 |
| 文案、文档、代码规模 | **Observed。** 三语新增键一致；文档与实现一致。7 个变更 Go 文件均未超过 1000 行，最大 `app.go` 为 832 行。 |

Gate Findings: 未发现新增门禁问题；两项历史发现均关闭。

NON-BLOCKING: none

任务文件删除已尝试；只读文件系统拒绝，文件遗留不影响审核结果。

```kander-findings
{"FINDINGS":[],"NON_BLOCKING":[]}
```