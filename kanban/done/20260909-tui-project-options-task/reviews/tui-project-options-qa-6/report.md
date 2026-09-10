Role: QA  
Commit: `35170cacf4ae68bf5f79bb6cf67e638524b37bee`  
Task Context: TUI Options 项目覆盖编辑；增量复核 `60bc11a..35170ca`，前轮 `tui-project-options-qa-5`。  
Reviewed Scope: 完整读取任务文件与 spec；核验全部 7 个变更文件及引用源码匹配 COMMIT TREE。只读环境未重跑测试；采纳调用方在目标提交执行的定向测试、包测试及全量测试通过记录。Observed：`git diff --check` 通过，变更文件均未超过 1000 行。

| 行为/质量 | 复核结果 |
|---|---|
| 架构边界 | Inferred：保持 TUI 调用 menu、config 负责合并与持久化；未发现新增依赖倒置。 |
| QA-001 / PM-001 | **部分修复**：Session 有效规则已同步；TUI 旧绑定会重新生成未编辑字段的覆盖。 |
| QA-004 / PM-003 | **部分修复**：输入组件不再重建，光标问题解除；继承提示和恢复入口重新失去同步。 |
| QA-006 / PM-006 | **部分修复**：launcher 经 `noteOverride` 同步，doctor 刷新缓存；Global TUI 偏好更新仍绕过缓存。 |
| QA-002 / PM-002 | 保持关闭。`session_overlay.go:228–240` 展开旧格式，仅删除目标路径。 |
| QA-003 | 原切换与恢复场景保持关闭。`session_overlay.go:115–125,224–240` 验证成功后提交状态。新增 setter 问题另列 QA-007。 |
| QA-005 / PM-005 | 保持关闭。`pty_test.go:323–337` 仅检查 resize 后输出。 |
| PM-004 | 保持关闭。`options_tabs.go:65–67,141` 清除命中区并禁止确认期间切 tab。 |
| 测试质量 | 新测试覆盖 Session 单次规则编辑、launcher 继承及禁止重建；未覆盖后续表单事件、继承提示、Global TUI 同步及拒绝编辑后的保存。 |

FINDINGS：4 项，均为 **medium、Inferred、高置信度**。

**QA-001 — 规则合并后，旧表单值被重新写成覆盖**

基础缺少整个 `rules`，Project 仅关闭 `code`。新 `SetRules` 正确将有效规则全部合并为关闭，但当前表单其余开关仍为开启。`applyRules` 使用旧局部值判断是否重建，预设仍匹配 `full`，因此不刷新。下一次按键或 Enter 再次应用这些旧值，将其余六项写成显式覆盖。

证据：[rule_options.go:12](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/rule_options.go:12)、[session_overlay.go:179](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:179)、[options_rules.go:85](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_rules.go:85)（至 121 行）。

违反“仅覆盖实际修改字段”与有效值展示要求。最小修复：合并成功后同步表单绑定、预设及展示，避免下一事件回灌旧值。用 TUI 连续事件及保存重读测试验证覆盖仅含 `code`。

**QA-004 — 模型覆盖仍显示继承，缺少即时恢复入口**

本轮删除模型修改后的刷新，保住光标；标题与恢复控件仍仅在建表单时计算。首次修改继承模型后，同页仍显示旧“全局：值”，恢复入口也不出现，直到重新进入页面。

证据：[options_form.go:548](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:548)（至 562 行静态构建）、[options_form.go:717](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:717)（至 730 行已无刷新）、[options_inherit.go:16](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_inherit.go:16) 与 36–48 行。

违反修改字段后移除继承前缀的明确要求。最小修复：保留活跃输入组件，同时更新标题与恢复控件。真实按键测试同时断言文本、光标及来源展示。

**QA-006 — Global TUI 偏好仍未同步到继承缓存**

Global 修改主题后，`applyInterface` 直接更新 `Config.TUI`，经 `persistUI` 保存并调用 `SyncTUI`；这些路径均未更新 `scopeRaw`。按 Esc 后切 Project，未覆盖主题仍从旧缓存读取；恢复继承也使用旧值。此时基础文件已保存新主题。

证据：[options_form.go:795](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:795)（至 806 行）、[options_panel.go:551](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_panel.go:551)、[doctor_session.go:71](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/doctor_session.go:71)（至 86 行）；缓存读取见 `session_overlay.go:140,233`。

launcher 与 doctor 修复成立，但继承随基础更新的要求仍未完整满足。最小修复：`SyncTUI` 同步预览文档的完整 `tui` section；测试 Global 修改主题、Esc、切 Project、恢复继承及重读一致性。

**QA-007 — 新增合并校验错误被吞掉，编辑丢失却保存成功**

合法旧基础配置缺少 `tui` 时，在 Project 修改主题。`SetTUIField` 先修改 `Config`；新合并校验拒绝只有主题的覆盖，但 `noteOverride` 丢弃错误，候选覆盖没有保留。界面继续显示新主题，随后 Save 保存旧覆盖并成功返回。修复前该编辑保留在覆盖缓冲，保存会报告校验失败。

证据：[session_overlay.go:173](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:173)（至 185 行）、[session_overlay.go:220](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:220)、同文件 246–272、387–395 行；`config.go:616–634,738–743` 证明触发条件；`options_panel.go:465–471` 接受保存成功。

违反失败保留编辑并报告、不得伪报成功的要求。最小修复：setter 返回校验错误，候选配置与覆盖统一处理；TUI 保留输入、显示错误并阻止成功提交。增加拒绝编辑后提交的 Session/TUI 测试。

NON-BLOCKING: none

```kander-findings
{
  "FINDINGS": [
    {
      "id": "QA-001",
      "tier": "medium",
      "text": "Inferred，高置信度，部分修复。基础缺少整个 rules，Project 仅关闭 code 时，SetRules 已正确更新有效配置，但 TUI 其余开关绑定仍为 true。applyRules 使用旧局部 rules 判断预设，未触发刷新；下一按键或 Enter 将其余六项旧值重新写成显式覆盖。违反仅覆盖实际修改字段及有效值展示要求。最小修复：合并成功后同步表单绑定、预设与展示，避免后续事件回灌旧值；以连续 TUI 事件和保存重读验证覆盖仅含 code。",
      "evidence": "internal/menu/rule_options.go:12-18 按传入值和当前有效值之差写覆盖；internal/menu/session_overlay.go:179-185 合并后替换 Config；internal/tui/options_rules.go:85-95 从旧绑定构造完整 rules，:109-121 调用 SetRules 后仍使用旧局部 rules 判断是否重建。internal/tui/options_panel.go:388-403 在 Enter 和普通事件中再次应用绑定。新增 internal/menu/session_overlay_test.go:361-396 仅验证单次 Session 调用，未覆盖第二次表单应用。",
      "lineage": {
        "run_id": "tui-project-options-qa-5",
        "finding_id": "QA-001"
      }
    },
    {
      "id": "QA-004",
      "tier": "medium",
      "text": "Inferred，高置信度，部分修复。本轮停止重建模型输入，解除光标跳转，却使首次模型覆盖后的继承标题及恢复入口再次失去同步。同页仍显示旧继承值，恢复入口直到重新进入页面才出现。违反修改字段后移除继承前缀的要求。最小修复：保留活跃输入组件，同时更新标题与恢复控件；真实按键测试同时验证输入文本、光标和来源展示。",
      "evidence": "internal/tui/options_form.go:548-562 仅在建表单时设置静态标题与恢复控件；:717-730 本轮删除覆盖存在性变化后的刷新。internal/tui/options_inherit.go:16-20、:36-48 按调用当时的覆盖存在性生成展示。internal/tui/options_overlay_test.go:346-360 仅断言没有请求重建，未验证继承提示和恢复入口。",
      "lineage": {
        "run_id": "tui-project-options-qa-5",
        "finding_id": "QA-004"
      }
    },
    {
      "id": "QA-006",
      "tier": "medium",
      "text": "Inferred，高置信度，部分修复。launcher 和 doctor 已同步原始文档缓存，但 Global TUI 偏好修改仍绕过该同步。Global 改主题并按 Esc 后切 Project，未覆盖主题及恢复继承仍使用旧 scopeRaw，尽管基础文件已保存新主题。违反继承值随基础配置更新要求。最小修复：SyncTUI 同步预览文档的完整 tui section；增加 Global 主题修改、切 tab、恢复继承与重读一致性测试。",
      "evidence": "internal/tui/options_form.go:795-806 直接修改 Config.TUI 并调用 persistUI；internal/tui/options_panel.go:551-558 保存后调用 SyncTUI；internal/menu/doctor_session.go:71-86 仅更新 Config 和保存基线，不更新 scopeRaw。internal/menu/session_overlay.go:198-220 的新同步只覆盖 noteOverride 调用，:140、:233 的预览和恢复仍消费缓存。",
      "lineage": {
        "run_id": "tui-project-options-qa-5",
        "finding_id": "QA-006"
      }
    },
    {
      "id": "QA-007",
      "tier": "medium",
      "text": "Inferred，高置信度，本轮新增。noteOverride 吞掉新合并校验的错误，使编辑丢失却可保存成功。合法基础配置缺少 tui 时，Project 修改主题先改变 Config，再因部分 tui 覆盖缺少必填字段而校验失败；覆盖候选被丢弃，界面仍显示新主题，Save 保存旧覆盖并返回成功。违反失败保留编辑并报告、不得伪报成功要求。最小修复：setter 返回错误，候选配置与覆盖统一处理；TUI 保留输入、显示错误并阻止成功提交。增加拒绝编辑后提交的 Session/TUI 测试。",
      "evidence": "internal/menu/session_overlay.go:173-185 新合并失败时不提交候选；:220 丢弃错误；:246-272 的 SetTUIField 已提前修改 Config；:387-395 保存旧 overlayRaw 并清除 dirty。internal/config/config.go:738-743 允许整个 tui 缺失，:616-634 要求存在的 tui 含完整字段。internal/tui/options_panel.go:465-471 接受 Save 成功。FIX RANGE 将原直接记录覆盖改为校验后记录且吞错，原本保存阶段可见的失败被隐藏。"
    }
  ],
  "NON_BLOCKING": []
}
```

已尝试删除任务文件；只读文件系统拒绝，文件遗留不影响审核结果。