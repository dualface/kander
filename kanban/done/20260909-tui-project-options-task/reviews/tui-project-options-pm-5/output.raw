Role: PM  
Commit: `60bc11ad9b04eb987a6a117ca5ae0b0a33a735fb`  
Task Context: [task-spec.md](/tmp/codex-review.87732c7baa01804331cf72e12b9e0547/task-spec.md)，已完整读取。  
Reviewed Scope: `7c2e345..60bc11a`；旧 finding 核销与修复回归。

**验收仍未通过：3 项 medium。** 本轮复核七项：Complete 4、Partial 2、Contradicted 1；不重算未变范围。

旧 finding  disposition：

- **PM-001 / QA-001：部分修复。** 保存改用原始基础文档，已拒绝缺少 `tui` 的非法局部覆盖；规则编辑后的有效值仍不同步。证据：[overlay_edit.go:379](/home/dualf/works/kander/worktrees/tui-project-options/internal/config/overlay_edit.go:379)、[rule_options.go:10](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/rule_options.go:10)。
- **PM-002 / QA-002：关闭。** 克隆覆盖、展开旧格式后仅删除指定规模路径，另一规模保留。证据：[session_overlay.go:190](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:190)、[review_stages.go:92](/home/dualf/works/kander/worktrees/tui-project-options/internal/config/review_stages.go:92)。
- **PM-003 / QA-004：部分修复。** 标题与恢复入口刷新，但重建输入丢失光标位置。证据：[options_form.go:723](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:723)、[options_panel.go:330](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_panel.go:330)。
- **PM-004：关闭。** 确认状态清空 tab 命中区域，并拒绝 tab 鼠标切换。证据：[options_tabs.go:65](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_tabs.go:65)、[options_tabs.go:140](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_tabs.go:140)。
- **PM-005 / QA-005：关闭。** 已隔离 resize 前的累计输出；符合调用方机械关闭证据。证据：[pty_test.go:323](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/pty_test.go:323)。
- **QA-003：关闭。** 切换目标、恢复继承均先验证候选，失败不提交编辑状态。证据：[session_overlay.go:115](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:115)、[session_overlay.go:195](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:195)。

验收表仅列状态变化；PM-001、PM-003 仍为 Partial：

| 要求 | 预期行为 | 代码证据 | Status |
|---|---|---|---|
| 验收第5条：单字段恢复 | 保留另一规模覆盖 | `session_overlay.go:190–202` | Complete |
| 验收第6条：关闭确认 | 隐藏 tab 不响应点击 | `options_tabs.go:65–67,140–142` | Complete |
| 验收第7条：失败保留编辑 | 验证失败不提交候选 | `session_overlay.go:115–125,190–202` | Complete |
| 验收第10条：窄屏测试 | 断言使用 resize 后新增输出 | `pty_test.go:323–337` | Complete |
| 验收第5条：继承随基础更新 | Global 编辑反映到 Project | `session_overlay.go:136–150` | Contradicted |

门禁 finding 均为 **Inferred，高置信度**：

1. **PM-001 · medium**：基础缺少 `rules` 时，Project 仅关闭 `code`，界面保留其他规则开启；保存后运行时却全部关闭。最小修复：编辑候选与保存结果统一采用原始文档合并，并同步有效配置。
2. **PM-003 · medium**：继承模型首次编辑立即重建输入。在 `model-a` 开头逐字输入 `new-`，首字后光标移至末尾，后续文本落错位置。最小修复：保留输入组件，或完整恢复光标与输入状态。
3. **PM-006 · medium**：本轮新增，根因同当前 QA-006。Global 修改 launcher 后保留编辑并切 Project，继承仍读取旧缓存；保存后实际值与此前界面不同。最小修复：保留原始键存在性，同时将基础编辑及 doctor 结果同步至预览文档。

`git diff --check` 通过，工作树干净。全量测试采用调用方通过记录，未复跑；真实终端表现未独立验证。任务文件删除已尝试，只读文件系统拒绝，文件保留。

NON-BLOCKING: none

```kander-findings
{
  "FINDINGS": [
    {
      "id": "PM-001",
      "tier": "medium",
      "text": "Inferred，高置信度，部分修复。违反 task-spec.md EXPECTED_OUTCOME 与 ACCEPTANCE_CRITERIA 第7条的有效值一致及复用合并语义要求。合法基础配置缺少整个 rules 时，Project 仅关闭 code，SetRules 保留其他规则为 true，仅记录 code=false；保存成功后运行时将其余规则置 false，Options 仍显示开启。原始文档保存校验已修复，但编辑有效值同步尚未修复。最小修复：规则编辑候选复用原始文档合并，验证成功后统一提交覆盖与有效配置，保存后同步最终有效值。",
      "evidence": "internal/menu/rule_options.go:10-18 将完整 UI 规则写入 Config，只记录变化键；internal/config/overlay_edit.go:379-385 使用原始文档校验但丢弃合并结果；internal/menu/session_overlay.go:347-357 保存后只更新覆盖基线和 dirty；internal/config/config.go:721-726 与 internal/config/rules.go:69-82 区分整个 rules 缺失和对象内键缺失，证明保存后的其余规则为 false。internal/tui/options_rules.go:65-68 从 Session.Config 渲染规则。",
      "lineage": {
        "run_id": "tui-project-options-pm-4",
        "finding_id": "PM-001"
      }
    },
    {
      "id": "PM-003",
      "tier": "medium",
      "text": "Inferred，高置信度，部分修复。继承标题与恢复入口已刷新，但修复破坏模型编辑的输入位置保留。Project 首次修改继承模型时重建整个输入，仅恢复字段焦点；在 model-a 开头逐字输入 new-，首个 n 后光标跳到末尾，后续字符写入错误位置。违反 USER_DECISIONS 的字段编辑要求及原修复要求中的输入位置保留。最小修复：保留原输入组件，或重建时恢复光标及输入状态，并以 Home 后连续字符事件验证最终文本。",
      "evidence": "internal/tui/options_form.go:723-732 首次覆盖请求重建；internal/tui/options_inherit.go:27-33 设置 rebuildAt；internal/tui/options_panel.go:397-406 每次输入后执行重建，:330-343 仅恢复字段索引；internal/tui/options_form.go:554-559 创建新 huh.Input。go.mod:6,9 固定依赖版本；github.com/charmbracelet/huh@v1.0.0/field_input.go:73-80 调用 SetValue；github.com/charmbracelet/bubbles@v0.21.1-0.20250623103423-23b8fd6302d7/textinput/textinput.go:192-204 将新输入光标置于末尾。",
      "lineage": {
        "run_id": "tui-project-options-pm-4",
        "finding_id": "PM-003"
      }
    },
    {
      "id": "PM-006",
      "tier": "medium",
      "text": "Inferred，高置信度，本轮新增。违反 task-spec.md ACCEPTANCE_CRITERIA 第5条的继承值随基础配置变更更新要求。新增 scopeRaw 缓存未随基础编辑更新：Global 修改 launcher，按 Esc 保留编辑后切 Project，未覆盖字段仍显示旧值，恢复继承也恢复旧值；保存两份编辑后运行时采用新基础值，界面预览与实际结果不同。最小修复：保留基础原始键存在性，将未保存基础编辑同步至预览文档，并在接纳 doctor 持久化结果时刷新该文档。",
      "evidence": "internal/menu/session_overlay.go:136-150 预览使用缓存 scopeRaw，缓存存在即返回；:195 恢复继承使用同一缓存；:309-311 仅 saveScope 成功后更新缓存。internal/menu/options.go:278-280 的 Global launcher setter 只修改 Config；internal/menu/session_overlay.go:173-176 只标记 ScopeDirty；internal/menu/doctor_session.go:43-49 接纳新基础配置未更新缓存。FIX RANGE 将原先基于当前 scopeConfig 的预览替换为上述缓存路径，直接引入回归。internal/tui/options_panel.go:485-497 保留 Esc 前编辑，internal/tui/options_tabs.go:52-61 切换后使用 Session 结果构建界面。"
    }
  ],
  "NON_BLOCKING": []
}
```