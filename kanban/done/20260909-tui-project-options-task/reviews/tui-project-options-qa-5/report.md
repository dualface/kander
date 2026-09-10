Role: QA  
Commit: `60bc11ad9b04eb987a6a117ca5ae0b0a33a735fb`  
Task Context: TUI Options 项目覆盖编辑；增量复核 `7c2e345..60bc11a`，前轮 `tui-project-options-qa-4`。  
Reviewed Scope: 完整读取任务文件与 spec；核验修复范围全部文件匹配 COMMIT TREE。追踪配置合并、Session 状态、表单更新及测试。只读环境未重跑测试；采纳调用方提供的目标提交包测试、全量测试通过记录。Observed：`git diff --check`、1000 行限制通过。

| 行为/质量 | 复核结果 |
|---|---|
| 架构边界 | Inferred：保持 TUI 调用 menu、config 负责合并与安全持久化，无新增依赖倒置。 |
| QA-001（对应 PM-001） | **部分修复**。保存改用原始 JSON，拒绝缺少 `tui` 的非法覆盖；规则编辑后的有效值仍不同步，见下文。 |
| QA-002（对应 PM-002） | **关闭**。Inferred：[session_overlay.go:190](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:190) 克隆覆盖、展开旧格式、仅删除目标路径；另一规模保留。测试见同目录 `session_overlay_test.go:244–269`。 |
| QA-003 | **关闭**。Inferred：[session_overlay.go:115](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:115)、`:186–204` 均先验证再提交目标、覆盖和有效配置；失败不修改 dirty 状态。 |
| QA-004（对应 PM-003） | **部分修复**。继承标题、恢复入口会刷新；文本输入重建丢失光标位置，见下文。 |
| QA-005（对应 PM-005） | **关闭**。Observed：[pty_test.go:323](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/pty_test.go:323) 记录输出边界，resize 后仅检查 `textFrom(before)`；历史宽屏输出不再满足断言。 |
| PM-004 相关修复 | Inferred：`options_tabs.go:65–67,141` 清空隐藏命中区，并拒绝确认期间的 tab 点击。 |
| 验证质量 | 已有配置、Session、TUI、PTY 分层测试；剩余缺陷可分别用 Session 保存重读测试、真实按键测试覆盖。 |

FINDINGS：3 项，均为 medium、Inferred，置信度高。

**QA-001 — medium — 规则编辑后仍保留错误有效值**

原始 JSON 保存校验已修复，但原报告中的 `rules` 场景仍成立：合法基础配置缺少整个 `rules`，Project 仅关闭 `code`；`SetRules` 保留其他规则为 `true`，仅记录 `code=false`。保存成功后，运行时读取将其他规则全部置为 `false`，当前 Options 仍显示开启。

证据：[rule_options.go:10](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/rule_options.go:10) 保留传入完整规则；[overlay_edit.go:383](/home/dualf/works/kander/worktrees/tui-project-options/internal/config/overlay_edit.go:383) 丢弃合并结果；[session_overlay.go:347](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:347) 保存后不更新 `Config`。新增 `overlay_edit_test.go:318–328` 已明确证明该覆盖使 `git=false`。

违反有效值展示与保存重读一致要求。最小修复：规则编辑候选复用原始文档合并，验证成功后同步覆盖和有效配置；保存后同步最终有效值。增加缺少 `rules` 的 Session 编辑、保存、重读比较。

**QA-004 — medium — 首次模型覆盖刷新丢失文本光标**

新增刷新解决来源展示，但首次修改继承模型时立即重建输入，仅恢复字段焦点。用户在继承值 `model-a` 开头连续输入 `new-`，首个 `n` 后光标跳到末尾，后续字符写入错误位置。

证据：[options_form.go:723](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:723) 首次覆盖请求重建；[options_panel.go:330](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_panel.go:330) 仅保存字段索引；`options_form.go:554–559` 创建新 `huh.Input`。固定依赖 Huh 的 `field_input.go:73–81` 调用 `SetValue`；Bubbles `textinput.go:192–204` 将新输入光标置于末尾。

违反原修复要求中的输入位置保留，也会改变用户实际录入值。最小修复：刷新时保留原输入组件，或恢复光标与输入状态；用 Home、连续字符事件断言最终文本。

**QA-006 — medium — 新增原始文档缓存未随基础编辑更新**

`scopeRaw` 加载后一直复用，仅 `saveScope` 更新。Global 修改 launcher、按 Esc 保留编辑，再切 Project，未覆盖 launcher 仍显示旧值；恢复继承同样恢复旧值。随后保存两份编辑，运行时使用新基础值，界面先前展示与最终结果不一致。

证据：[session_overlay.go:136](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:136) 合并固定 `scopeRaw`，`:143–150` 命中缓存直接返回，`:195` 恢复也使用缓存；Global setter `options.go:278–280` 只更新 `Config`。修复前预览使用当前 `scopeConfig`。`ApplyDoctorConfig` 更新基础配置时也未刷新此缓存。

违反继承值随基础变更更新要求。最小修复：保留原始键存在性，同时将基础编辑同步到预览文档；doctor 接纳持久化结果时刷新文档。增加跨 tab 修改基础值、恢复继承、保存重读的 Session 测试。

NON-BLOCKING: none

```kander-findings
{
  "FINDINGS": [
    {
      "id": "QA-001",
      "tier": "medium",
      "text": "Inferred，置信度高，部分修复。保存已改用原始基础 JSON，但规则编辑后的有效配置仍不同步。合法基础配置缺少整个 rules 时，Project 仅关闭 code，SetRules 保留其他规则为 true，仅写 code=false；保存成功后的运行时读取将其他规则置 false，当前 Options 仍显示开启。违反有效值展示与保存重读一致要求。最小修复：规则编辑候选复用原始文档合并，验证成功后同步覆盖与有效配置，保存后同步最终有效值；增加缺少 rules 的 Session 编辑、保存、重读比较。",
      "evidence": "internal/menu/rule_options.go:10-18 保留完整规则状态，仅记录变化键；internal/config/overlay_edit.go:379-385 使用原始文档校验但丢弃合并结果；internal/menu/session_overlay.go:347-357 保存后只更新覆盖基线和 dirty，不更新 Config；internal/config/rules.go:69-82 将 rules 对象内缺失规则设为 false；新增 internal/config/overlay_edit_test.go:318-328 明确验证该覆盖使 git=false。",
      "lineage": {
        "run_id": "tui-project-options-qa-4",
        "finding_id": "QA-001"
      }
    },
    {
      "id": "QA-004",
      "tier": "medium",
      "text": "Inferred，置信度高，部分修复。继承标题与恢复入口会刷新，但首次修改继承模型时立即重建输入，只恢复字段焦点，丢失文本光标位置。在继承值 model-a 开头连续输入 new-，首个 n 后光标跳到末尾，后续字符写入错误位置。违反原修复要求中的输入位置保留，并改变用户实际录入值。最小修复：保留原输入组件，或恢复光标与输入状态；用 Home、连续字符事件断言最终文本。",
      "evidence": "internal/tui/options_form.go:723-732 首次覆盖请求重建；internal/tui/options_inherit.go:27-33 转交 rebuildAt；internal/tui/options_panel.go:330-343 重建整个 section，仅恢复字段索引；internal/tui/options_form.go:554-559 创建新 huh.Input。固定依赖 github.com/charmbracelet/huh@v1.0.0/field_input.go:73-81 通过 Value/Accessor 调用 SetValue；github.com/charmbracelet/bubbles@v0.21.1-0.20250623103423-23b8fd6302d7/textinput/textinput.go:192-204 将新输入光标置于末尾。",
      "lineage": {
        "run_id": "tui-project-options-qa-4",
        "finding_id": "QA-004"
      }
    },
    {
      "id": "QA-006",
      "tier": "medium",
      "text": "Inferred，置信度高，本轮新增。scopeRaw 缓存未随基础配置编辑更新。Global 修改 launcher、按 Esc 保留编辑后切 Project，未覆盖 launcher 仍显示旧值，恢复继承也恢复旧值；随后保存两份编辑，运行时使用新基础值，先前界面与最终结果不一致。违反继承值随基础变更更新要求。最小修复：保留原始键存在性，同时将基础编辑同步到预览文档；doctor 接纳持久化结果时刷新文档。增加跨 tab 基础编辑、恢复继承与保存重读的 Session 测试。",
      "evidence": "internal/menu/session_overlay.go:136-150 预览合并缓存 scopeRaw，缓存存在即返回；:195 恢复继承也使用该缓存；:309-311 仅 saveScope 成功后更新缓存。internal/menu/options.go:278-280 的 Global setter 只修改 Config；internal/menu/doctor_session.go:43-49 接纳新基础配置也不更新 scopeRaw。修复前 session_overlay.go:124-133 从当前 scopeConfig 构造预览，本轮替换后产生该回归。"
    }
  ],
  "NON_BLOCKING": []
}
```

已尝试删除任务文件；只读文件系统拒绝，文件遗留不影响审核结果。