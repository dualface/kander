Role: PM  
Commit: `35170cacf4ae68bf5f79bb6cf67e638524b37bee`  
Task Context: [task-spec.md](/tmp/codex-review.c4c298ae4e1391d2fb702ab0dc7a778f/task-spec.md)，已完整读取。  
Reviewed Scope: `60bc11a..35170ca`；旧 finding 核销及修复回归。

**验收未通过：4 项 medium。** 三项旧 finding 部分修复，新增一项。以下行为判断均为 **Inferred，高置信度**。

旧 finding disposition：

- **PM-001 / QA-001：部分修复。** Session 已同步合并结果，但规则表单仍保留旧绑定；下一事件会将未编辑规则写成覆盖。证据：`internal/menu/rule_options.go:10–18`、`internal/tui/options_rules.go:85–122`。
- **PM-003 / QA-004：部分修复。** 输入不再重建，光标问题解除；首次覆盖后的继承标题和恢复入口再次失去同步。证据：`internal/tui/options_form.go:548–562,717–730`。
- **PM-006 / QA-006：部分修复。** launcher、doctor 已同步缓存；Global TUI 偏好仍绕过同步。证据：`internal/menu/session_overlay.go:198–220`、`internal/menu/doctor_session.go:43–50,71–86`。
- **PM-002 / QA-002：保持关闭。** 恢复前展开编辑副本，仅删除指定规模路径。证据：`internal/menu/session_overlay.go:228–240`、`internal/config/review_stages.go:92–97`。
- **PM-004：保持关闭。** 确认状态清空 tab 命中区域并拒绝切换。证据：`internal/tui/options_tabs.go:65–67,140–142`。
- **PM-005 / QA-005：保持关闭。** 窄屏检查仅消费 resize 后输出。证据：`internal/tui/pty_test.go:323–337`。
- **QA-003：保持关闭。** 切换目标、恢复继承仍先验证候选再提交。证据：`internal/menu/session_overlay.go:115–125,228–240`。

验收表仅列状态变化；本表 Complete 2、Partial 2、Contradicted 1，不重算未变范围。

| 要求 | 预期行为 | 代码证据 | Status |
|---|---|---|---|
| 第7条：规则合并语义 | Session 有效配置匹配原始文档合并 | `internal/menu/session_overlay.go:173–185` | Complete |
| 字段编辑：输入位置保留 | 首次模型覆盖保留输入组件 | `internal/tui/options_form.go:717–730` | Complete |
| USER_DECISIONS：覆盖来源展示 | 首次修改移除继承前缀，提供恢复入口 | `internal/tui/options_form.go:548–562,717–730` | Contradicted |
| 第5条：基础变更同步 | 各类 Global 编辑更新 Project 继承值 | `internal/menu/doctor_session.go:71–86` | Partial |
| 第7条：失败处理 | 保留失败编辑、报告错误，禁止伪报成功 | `internal/menu/session_overlay.go:179–185,220,387–395` | Partial |

门禁问题：

1. **PM-001 · medium**：基础缺少 `rules`，Project 关闭 `code` 后，其余开关绑定仍为 true。下一按键或 Enter 回灌六项旧值，产生未主动编辑的覆盖。修复：合并成功后同步表单绑定、预设和展示。
2. **PM-003 · medium**：首次编辑继承模型后，同页仍显示旧继承前缀，恢复入口缺失。修复：保留输入组件，同时更新标题和恢复控件。
3. **PM-006 · medium**：Global 改主题、Esc 后切 Project，继承主题仍来自旧缓存，尽管基础文件已保存新值。修复：`SyncTUI` 同步完整 `tui` 预览文档。
4. **PM-007 · medium，本轮新增**：基础缺少 `tui` 时，Project 主题编辑触发合并失败；错误被吞，编辑候选丢弃，随后保存旧覆盖却返回成功。修复：setter 传递错误，候选配置与覆盖统一处理，保留输入并阻止错误状态提交。

`git diff --check` 通过，工作树干净。测试通过情况采用调用方记录，未复跑；真实终端未独立验证。任务文件删除已尝试，只读文件系统拒绝，文件保留。

NON-BLOCKING: none

```kander-findings
{
  "FINDINGS": [
    {
      "id": "PM-001",
      "tier": "medium",
      "text": "Inferred，高置信度，部分修复。违反 task-spec.md ACCEPTANCE_CRITERIA 第5条仅覆盖实际修改字段及 EXPECTED_OUTCOME 有效值展示要求。合法基础配置缺少整个 rules 时，Project 仅关闭 code，SetRules 已按原始文档合并更新 Config，但 TUI 其余六项绑定仍为 true。applyRules 使用旧局部 rules 判断预设，不触发刷新；下一按键或 Enter 将六项旧值写成显式覆盖，使未编辑字段失去继承。最小修复：合并成功后同步表单绑定、预设及展示，避免旧值回灌；用连续 TUI 事件及保存重读验证覆盖仅含 code。",
      "evidence": "internal/menu/rule_options.go:10-18 按传入值与当前有效值之差写覆盖；internal/menu/session_overlay.go:173-185 本轮新增原始文档合并并替换 Config；internal/tui/options_rules.go:85-95 从旧绑定构造完整 rules，:109-122 成功后仍依据旧局部 rules 判断刷新；internal/tui/options_panel.go:388-403 在 Enter 和普通事件中再次应用绑定。internal/config/config.go:721-726、internal/config/rules.go:69-82 证明缺少整个 rules 与缺少对象子键的语义不同。",
      "lineage": {
        "run_id": "tui-project-options-pm-5",
        "finding_id": "PM-001"
      }
    },
    {
      "id": "PM-003",
      "tier": "medium",
      "text": "Inferred，高置信度，部分修复。违反 task-spec.md USER_DECISIONS 中修改字段建立覆盖并移除继承前缀的要求。本轮停止重建模型输入，解除光标跳转，但首次模型覆盖后的继承标题和恢复入口再次失去同步；当前页面继续显示旧继承值，恢复入口直到重新进入页面才出现。最小修复：保留活跃输入组件，同时更新标题和恢复控件；按键测试同时验证输入位置、文本及来源展示。",
      "evidence": "internal/tui/options_form.go:548-562 在建表单时设置静态标题和恢复控件；:717-730 本轮删除首次覆盖后的刷新调用，仅写值并标脏。internal/tui/options_inherit.go:16-20、:36-48 按调用时的覆盖存在性生成标题和恢复入口。internal/tui/options_overlay_test.go:346-360 仅验证未请求重建，不能证明来源展示同步。",
      "lineage": {
        "run_id": "tui-project-options-pm-5",
        "finding_id": "PM-003"
      }
    },
    {
      "id": "PM-006",
      "tier": "medium",
      "text": "Inferred，高置信度，部分修复。违反 task-spec.md ACCEPTANCE_CRITERIA 第5条继承值随基础配置变更更新的要求。launcher 和 doctor 已同步 scopeRaw，但 Global TUI 偏好仍绕过同步。Global 修改主题并按 Esc 后切 Project，未覆盖主题及恢复继承仍采用旧值，尽管基础文件已保存新主题。最小修复：SyncTUI 同步预览文档的完整 tui section；验证 Global 主题编辑、切 tab、恢复继承与重读一致。",
      "evidence": "internal/menu/session_overlay.go:198-220 本轮新增同步仅覆盖 noteOverride 路径，:140、:233 的预览和恢复继续读取 scopeRaw。internal/menu/doctor_session.go:43-50 已刷新 doctor 结果，但:71-86 的 SyncTUI 仅更新配置和基线，不更新 scopeRaw。internal/tui/options_form.go:795-806 直接修改 Global Config.TUI 并调用 persistUI；internal/tui/options_panel.go:551-558 保存后调用 SyncTUI。",
      "lineage": {
        "run_id": "tui-project-options-pm-5",
        "finding_id": "PM-006"
      }
    },
    {
      "id": "PM-007",
      "tier": "medium",
      "text": "Inferred，高置信度，本轮新增，与当前 QA-007 同根因。违反 task-spec.md ACCEPTANCE_CRITERIA 第7条失败保留编辑并报告、不得伪报成功的要求。合法基础配置缺少 tui 时，Project 修改主题先改变 Config，再因部分 tui 覆盖缺少必填字段而合并失败；noteOverride 吞掉错误，覆盖候选被丢弃，界面仍显示新主题。随后 Save 保存旧覆盖并返回成功，用户编辑未落盘。最小修复：setter 返回错误，候选配置与覆盖统一处理；TUI 保留输入、报告错误并阻止错误状态成功提交。",
      "evidence": "internal/menu/session_overlay.go:173-185 新合并失败时不提交候选，:220 丢弃 applyOverlaySet 错误；:246-272 的 SetTUIField 已提前修改 Config；:387-395 保存旧 overlayRaw 并清除 dirty。internal/config/config.go:738-743 允许整个 tui 缺失，:616-634 要求已存在的 tui 包含完整字段。internal/tui/options_form.go:741-750 更新主题并调用 setter；internal/tui/options_panel.go:465-471 接受 Save 成功。FIX RANGE 将直接记录覆盖改为校验后记录且吞错，隐藏了原先保存阶段可见的失败。"
    }
  ],
  "NON_BLOCKING": []
}
```