Role: PM  
Commit: `26b42cf434d55c534998c4ea73db36cd69a2d497`  
Task Context: `/tmp/codex-review.ce4a82fb23a2585653fed859768a7521/task-spec.md`  
Reviewed Scope: Options 打开、配置读取、表单绑定、语言、保存及相关测试。

验收状态：Complete 6，Partial 1，Unverifiable 1；Missing、Contradicted 均为 0。发现 1 项门禁问题。

**PM-001 · medium · Inferred · 高置信度：界面表单仍展示旧 App 值。**

契约要求重开后表单展示磁盘作用域值（`task-spec.md:18、31、38`）。新加载路径替换 session（`internal/tui/options_panel.go:178–181`），但 `internal/tui/options_form.go:322–327` 仍从 `App` 初始化主题、列数、最小列宽、刷新间隔和单列开关；摘要也读取 `App`（同文件 `267–269`）。

触发：TUI 启动后，外部把作用域主题从 `dark` 改为 `light`，再打开 Options 的界面设置。session 已加载 `light`，选择器仍显示 `dark`。存在 TUI overlay 时，选择器也可能展示 overlay 值，违反作用域表单隔离要求。本次重载未贯通这些消费者。

最小修复：表单和作用域摘要从新 session 的 `Config.TUI` 取值；保持看板更新时机，避免初始化绑定触发写盘。补充外部修改主题、列数及 overlay 隔离的表单断言。

NON-BLOCKING: none

Unverifiable：任务文件记录测试通过；本轮受只读限制未重跑。`git diff --check` 通过。已尝试删除任务文件，因只读文件系统失败。

```kander-findings
{
  "FINDINGS": [
    {
      "id": "PM-001",
      "tier": "medium",
      "text": "Inferred，高置信度：Options 重载未贯通界面表单。TUI 启动后外部把作用域主题从 dark 改为 light，再打开界面设置，新 session 已读到 light，但选择器仍从 App 取旧 dark；列数、最小列宽、刷新间隔和单列开关同样受影响。存在 TUI overlay 时，表单还可能显示 overlay 值。违反重开后展示磁盘作用域值的要求。最小修复：表单及作用域摘要从新 session.Config.TUI 初始化，保持看板更新时机，避免初始化触发写盘，并增加实际表单取值回归测试。",
      "evidence": "/tmp/codex-review.ce4a82fb23a2585653fed859768a7521/task-spec.md:18,31,38 要求表单展示本次读取的作用域值；:27,52 只排除立即应用到看板。internal/tui/options_panel.go:178-181 替换 session 并绑定语言；internal/tui/options_form.go:322-327 仍从 App 初始化五个界面字段，:402-434 将这些绑定用于选择器，:267-269 的摘要也读取 App。重载结果没有到达这些表单消费者。"
    }
  ],
  "NON_BLOCKING": []
}
```