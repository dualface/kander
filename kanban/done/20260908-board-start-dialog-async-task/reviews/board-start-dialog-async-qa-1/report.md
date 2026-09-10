Role: QA  
Commit: `e0f75e3f30c9d92442fdae091cdd56b243a93a2a`  
Task Context: 异步目标卡预览、四态启动对话框、迟到结果隔离及窄屏排版。  
Reviewed Scope: 审核 `1e4886a..e0f75e3`，追踪 TUI 输入、后台消息、渲染及 launch/board 边界。Observed：变更文件与提交一致，均属于证据树。只读环境未重跑测试；采纳交付记录：21 包、1230 测试/子测试通过，1 项平台跳过；定向 race 25 项通过。Windows 原生行为 Unverifiable。

| 行为／质量 | 结论与证据 |
|---|---|
| 当帧读取态、异步预览 | Observed：`start.go:69–78` 先置状态再排后台工作；阻塞注入测试验证输入及刷新继续处理。 |
| targeted 读取 | Observed：`launch/start.go` 复用单卡快照；`board/transaction.go:173–186` 仅读取目标正文。Linux 文件打开观测测试覆盖。 |
| 取消、迟到及错误隔离 | Observed：`start.go:82–106` 校验阶段、请求序号、任务 ID 与当前选中卡；异常关闭并显示原因。 |
| 启动与结果保留 | Observed：启动中屏蔽按键，完成后保留结果；调用参数、认领及回滚实现未变。 |
| 窄屏结果完整性 | Inferred：存在裁切缺陷，见 QA-001。 |
| 架构、文案及文件规模 | Observed：保持 TUI 单向复用 launch/board；三语启动键一致；8 个变更 Go 文件均未超过 1000 行。 |

FINDINGS

**QA-001 — medium — Inferred，置信度高：窄屏结果被裁切，完整地址或警告无法查看。**

证据：`internal/tui/start_dialog.go:34–43` 在结果前保留任务 ID、旧状态；`:89–92` 按高度直接丢弃正文尾行。`internal/tui/app.go:138` 在对话框存在时禁止旧结果浮层；`internal/tui/mouse.go:329–330` 屏蔽滚动，`internal/tui/start.go:114–116` 任意键直接关闭。

具体场景：英文界面、40×10 分屏，使用 `internal/tui/start_test.go:277–281` 已有的任务 ID 与 tmux-session 地址，经过完整启动流程。正文宽 32 列，可保留 5 行；任务 ID 与状态至少占 3 行，结果只剩 2 行，尾部地址必然被截掉。长错误或警告同样受影响。等待超过 5 秒后关闭，旧通知也已过期（`internal/tui/focus.go:38–41`）。

最小持久修复：结果态先去掉重复任务信息，优先展示结果；溢出正文提供鼠标滚动，并保留至按键关闭。用现有 fixture 增加四态流程下 40×10 的地址、警告及通知过期断言。现有窄屏地址测试直接调用 `applyStartResult`，未创建对话框，未覆盖该路径。

NON-BLOCKING: none

任务文件删除已尝试；只读文件系统拒绝，文件遗留不影响审核结论。

```kander-findings
{
  "FINDINGS": [
    {
      "id": "QA-001",
      "tier": "medium",
      "text": "Inferred，置信度高：窄屏结果态直接裁掉正文尾行，违反成功、失败原因及警告完整展示并保留至按键关闭的要求。英文界面40×10分屏，使用现有测试的20260908-options-workflow-flowchart-task及kb-board-start-task-key-12345678:@9:%9，经过完整启动流程后，正文宽32列且仅保留5行；任务ID与状态至少占3行，结果只剩2行，完整地址不可见。长错误和警告同样可能被裁掉。鼠标无法滚动，任意键关闭；超过5秒后旧通知也无法补救。最小持久修复：结果态去掉重复任务信息、优先展示结果，为溢出正文提供鼠标滚动并保留至按键关闭；补充完整四态流程下40×10的地址、警告及通知过期断言。",
      "evidence": "internal/tui/start_dialog.go:34-43在结果前保留任务ID及旧状态；:65-70计算宽度与高度预算；:89-92直接截断正文。internal/tui/app.go:138在对话框存在时禁止旧结果浮层。internal/tui/mouse.go:329-330屏蔽鼠标滚动；internal/tui/start.go:114-116结果态任意键关闭；internal/tui/focus.go:38-41通知5秒过期。internal/tui/start_test.go:274-304提供现有任务与地址fixture，但直接调用applyStartResult，未创建四态对话框，因此未覆盖实际结果裁切路径。"
    }
  ],
  "NON_BLOCKING": []
}
```