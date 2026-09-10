Role: PM  
Commit: `fdb68ffdef29749b56a4e7f6464b1206b0213501`  
Task Context: 完整配置校验、doctor 修复、TUI 错误弹层。  
Reviewed Scope: 仅 `a8d5a6f..fdb68ff` 五个文件及修复影响路径。

增量复审通过。未发现新增门禁问题。

| 状态变化条目 | 预期行为 | 实现证据 | 状态 |
|---|---|---|---|
| 每次打开选项面板完整校验配置 | 读取失败显示 `loadErr`，不打开表单 | [options_panel.go:94](/home/dualf/works/kander/worktrees/require-complete-config/internal/tui/options_panel.go:94) 在复用会话前校验，失败立即返回 | Partial → Complete |

沿用上轮已接受条目：Complete 10；Partial、Missing、Contradicted、Unverifiable 均为 0。

逐项核查（Inferred，高置信度）：

- **PM-001 / QA-001：closed。** 校验覆盖缓存会话重开路径；[options_view.go:122](/home/dualf/works/kander/worktrees/require-complete-config/internal/tui/options_view.go:122) 渲染错误画框。[options_test.go:137](/home/dualf/works/kander/worktrees/require-complete-config/internal/tui/options_test.go:137) 覆盖关闭、损坏配置、重开。
- **QA-002：closed。** [doctor.go:77](/home/dualf/works/kander/worktrees/require-complete-config/internal/menu/doctor.go:77) 在 Repair 后重新读取合并配置、更新 Agent 探测结果；后续能力报告及第 173 行资源校验使用更新结果。[overlay_cli_test.go:53](/home/dualf/works/kander/worktrees/require-complete-config/internal/menu/overlay_cli_test.go:53) 覆盖 overlay 中失效 wrapper。
- **QA-003：closed。** [check_bind_test.go:40](/home/dualf/works/kander/worktrees/require-complete-config/cmd/kander/check_bind_test.go:40) 设置临时 `KANDER_CONFIG` 并保存合法配置；第 107–111 行仍断言退出码和真实 liveness 输出。

NON-BLOCKING: none

Observed：目标提交一致，工作树干净，修复范围 `git diff --check` 通过。只读审查未重跑测试；测试通过依据调用方记录。只读权限限制，任务文件保留。

```kander-findings
{"FINDINGS":[],"NON_BLOCKING":[]}
```