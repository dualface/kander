Role: PM  
Commit: `7c2e345c385be33087c3925ab4be18d11109a353`  
Task Context: [task-spec.md](/tmp/codex-review.ce0b125626e24a356f2c417932792c70/task-spec.md)，完整首轮重试，无 predecessor。  
Reviewed Scope: `8abdfe2..7c2e345`；配置合并与持久化、Session、Options 表单/tab/鼠标、相关消费者、文案、文档及测试。

验收拆分：Complete 20、Partial 4、Contradicted 1、Unverifiable 2。发现 **5 项 medium**，尚未满足验收。

本轮只读审查，`git diff --check` 通过。全量测试采用调用方记录，未复跑；最终真实终端表现未独立验证。

1. **PM-001 · medium · Inferred · 高置信度**  
   保存校验与运行时采用不同基础文档。合法旧配置缺少 `tui` 时，Project 修改主题可保存成功，随后 `config.Load` 却因缺少 `tui.columns` 等字段失败。违反验收第7条。最小修复：编辑与保存统一复用运行时的原始文档合并、校验语义。证据：[overlay_edit.go:216](/home/dualf/works/kander/worktrees/tui-project-options/internal/config/overlay_edit.go:216)、[loadsave.go:112](/home/dualf/works/kander/worktrees/tui-project-options/internal/config/loadsave.go:112)。

2. **PM-002 · medium · Inferred · 高置信度**  
   旧格式 `review_stages: {"PM":"skip"}` 同时覆盖两种规模。恢复 `large.PM` 会删除共享 `PM` 键，连带恢复 `small.PM`。违反验收第5条“不影响其他字段”。最小修复：先展开编辑副本，再仅删除指定规模的键，保留原始保存基线。证据：[session_overlay.go:154](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:154)。

3. **PM-003 · medium · Inferred · 高置信度**  
   Project 修改 launcher、模型等字段后，覆盖已建立，但继承标题与恢复控件未刷新。界面仍显示旧“全局：值”，恢复入口须重新进入页面才出现。违反 USER_DECISIONS 的“修改字段……移除继承前缀”。最小修复：覆盖状态变化时刷新字段标题和恢复入口，并保留焦点、输入位置。证据：[options_inherit.go:16](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_inherit.go:16)、[options_form.go:662](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:662)。

4. **PM-004 · medium · Inferred · 高置信度**  
   关闭确认隐藏 tab，却保留其点击区域。点击确认框首行对应另一 tab 的位置，会切换作用域并打开根菜单，同时保留 `confirming=true`；随后 Enter 执行旧的关闭选项，默认会保存。违反验收第6条交互一致性。最小修复：确认状态禁用 tab 点击，并清除隐藏 tab 的命中区域。证据：[options_tabs.go:65](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_tabs.go:65)、[options_tabs.go:139](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_tabs.go:139)。

5. **PM-005 · medium [mechanical] · Observed · 高置信度**  
   PTY 窄屏断言读取整个历史输出；resize 前已确认其中包含覆盖文件名，因此 resize 后检查可以立即通过，未验证窄屏画面。验收第10条的窄屏证据不足。最小修复：等待 resize 后的新帧，仅检查该帧中的路径与边界。证据：[pty_test.go:291](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/pty_test.go:291)、[pty_test.go:305](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/pty_test.go:305)。

NON-BLOCKING: none

已尝试删除任务文件；只读文件系统拒绝，文件保留。

```kander-findings
{
  "FINDINGS": [
    {
      "id": "PM-001",
      "tier": "medium",
      "text": "Inferred，高置信度。违反 task-spec.md ACCEPTANCE_CRITERIA 第7条：保存必须复用运行时合并语义并校验有效配置。MergeScopeAndOverlay 将已补齐默认值的 Config 序列化后合并，而 config.Load 合并原始基础 JSON。合法旧基础配置缺少 tui 时，Project 仅修改主题会成功保存 {\"tui\":{\"theme\":\"dark\"}}，随后运行时读取却因 tui.columns 等必填字段缺失失败。同根因也使缺少 rules 的旧配置在编辑器中保留其他开关为 true，实际读取则将未列出的开关置 false。最小修复：保留基础文档的键缺失信息，编辑预览与保存统一使用 config.Load 的原始文档合并和校验路径，拒绝运行时无法读取的覆盖。",
      "evidence": "internal/config/overlay_edit.go:216 将补全后的 Config 序列化；:231 合并；:343-348 保存时采用该校验。internal/menu/session_overlay.go:124-133 编辑预览也采用此路径。internal/config/loadsave.go:101-116 使用原始 scope JSON 合并。internal/config/config.go:738-743 允许整个 tui 缺失并补默认值，:616-634 在 tui 存在时要求完整字段；:721-726 与 internal/config/rules.go:69-82 证明 rules 缺失和部分对象的语义不同。"
    },
    {
      "id": "PM-002",
      "tier": "medium",
      "text": "Inferred，高置信度。违反 task-spec.md ACCEPTANCE_CRITERIA 第5条：恢复继承只能删除对应覆盖，不影响其他字段。对于合法旧格式 review_stages={\"PM\":\"skip\"}，用户恢复 large.PM 时，RestoreInherit 删除共享 PM 键，导致 small.PM 的显式覆盖也丢失。最小修复：恢复前将编辑副本展开为两种规模，只删除目标规模的角色键；保持 overlayExisting 原始基线不变。",
      "evidence": "internal/config/review_stages.go:92-97 将旧格式角色键应用到两种规模。internal/tui/options_form.go:590-604 为每种规模提供独立恢复入口。internal/menu/session_overlay.go:154-156 删除指定路径后又删除共享角色键，:159-160 重建有效配置。"
    },
    {
      "id": "PM-003",
      "tier": "medium",
      "text": "Inferred，高置信度。违反 task-spec.md USER_DECISIONS 中修改字段建立覆盖并移除继承前缀的要求。Project 页面中的继承标题和恢复控件只在建表单时计算；修改 launcher 或模型后虽已记录覆盖，却不刷新表单，仍显示旧继承来源且没有恢复入口，直至重新进入页面。最小修复：覆盖状态变化时同步刷新标题和恢复控件，同时保留字段焦点与文本输入位置。",
      "evidence": "internal/tui/options_inherit.go:16-35 按建表单时的键存在性生成静态标题和恢复字段。internal/tui/options_form.go:513-519 构建 launcher 字段，:540-552 构建模型字段；:662-665 修改 launcher 只标记 dirty，:700-711 修改模型只写值、记录覆盖并标记 dirty，均未刷新来源展示。"
    },
    {
      "id": "PM-004",
      "tier": "medium",
      "text": "Inferred，高置信度。违反 task-spec.md ACCEPTANCE_CRITERIA 第6条的清晰、一致关闭交互。全局安装显示过 tab 后进入未保存关闭确认，tab 被隐藏但命中区域仍保留。点击确认框首行对应另一 tab 的横向位置会切换作用域、打开根菜单，却保留 confirming=true；随后按 Enter 会执行旧 closeChoice，默认触发保存，而非打开所见菜单项。最小修复：确认期间拒绝 tab 鼠标切换，并在 tab 不显示时清空命中区域。",
      "evidence": "internal/tui/options_tabs.go:65-66 确认状态提前返回但不清理 tabHits；:97 仅在绘制 tab 时清理；:139-149 鼠标切换未检查 confirming；:58-59 打开根菜单。internal/tui/options_form.go:301-307 设置 confirming 和默认 closeSave；:225-251 打开根菜单不清除 confirming。internal/tui/options_panel.go:392-393、:424-428 证明随后 Enter 仍执行关闭保存。"
    },
    {
      "id": "PM-005",
      "tier": "medium",
      "mechanical": "redundant-test",
      "text": "Observed，高置信度。新增 PTY 窄屏断言未验证 resize 后的行为，不能支持 task-spec.md ACCEPTANCE_CRITERIA 第10条的窄屏验收。resize 前已要求累计输出包含覆盖文件名，resize 后仍搜索同一累计输出，因此即使没有任何新绘制或窄屏路径已消失，检查也可立即通过。最小修复：记录 resize 前输出边界，等待 resize 后完成的新帧，并只验证新帧中的路径和屏幕边界。",
      "evidence": "internal/tui/pty_test.go:63 将输出持续追加到 s.out；:100-103 返回全部历史输出；:291-293 已确认覆盖文件名存在；:305-314 resize 后再次搜索全部历史输出，未隔离新帧。"
    }
  ],
  "NON_BLOCKING": []
}
```