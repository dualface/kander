Role: QA  
Commit: `36f8f181c5d589bdf4323624120e3b7e62a4925d`  
Task Context: 增量复核启动结果裁切修复及读取态滚轮回归。  
Reviewed Scope: 仅审核 `e0f75e3..36f8f18`。Observed：工作树干净，变更路径属于证据提交树，`git diff --check` 通过。只读环境未重跑测试；采纳交付记录：21 包、1232 测试/子测试通过，1 项平台跳过；定向 race 27 项通过。

| 行为／质量 | 结论与证据 |
|---|---|
| QA-001 | **closed**，Inferred，高置信度。`internal/tui/start_dialog.go:43–45` 优先显示结果；`:82–98` 保留完整正文并夹紧滚动偏移；`:115–118` 支持滚轮。 |
| 结果保留、关闭与 resize | Observed：`internal/tui/start_scroll_test.go:37–96` 覆盖英文 40×8、40×10，成功／失败地址、长警告、通知过期、滚动、resize 及按键仅关闭。`internal/tui/start.go:149–151` 重置结果滚动位置。 |
| 读取态滚轮与迟到隔离 | Observed：`internal/tui/start_dialog.go:107–113` 复用看板滚轮，换选关闭旧框；`internal/tui/start_scroll_test.go:12–34` 验证旧预览不覆盖新框。 |
| 布局与文案 | Observed：`internal/tui/start_async_test.go:178–229` 保留留白降级、dim 提示及横向完整性断言；三份 locale 同步滚轮提示。 |
| 架构与规模 | Observed：修复局限于 TUI 展示和输入，复用 Bubbles viewport，未改变 launch/board 边界。5 个变更 Go 文件均未超过 1000 行，最大 359 行。 |

FINDINGS: none。QA-001 已关闭；未发现修复引入、加重或掩盖的新门禁缺陷。

NON-BLOCKING: none

任务文件删除已尝试；只读文件系统拒绝，遗留不影响审核结论。

```kander-findings
{"FINDINGS":[],"NON_BLOCKING":[]}
```