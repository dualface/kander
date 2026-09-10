Role: PM  
Commit: `162a3d8e7a53066cf96954198d63bb0662919746`  
Task Context: [task-spec.md](/tmp/codex-review.89e8983fc0225d03b847d4a5994cb922/task-spec.md)，已完整读取。  
Reviewed Scope: `f4d4eaa..162a3d8`；旧 finding 核销及修复直接回归。

**验收未通过：PM-008 关闭，新增 1 项 medium。** 以下行为结论均为 **Inferred，高置信度**。

旧 finding disposition：

- **PM-008 / QA-008：关闭。** 无效草稿允许逐键删除；剩余输入保留，全部恢复后清除草稿。保存仍校验原始覆盖。证据：`internal/menu/session_overlay.go:246–268,419–424`、`internal/menu/session_overlay_draft.go:12–47`、`internal/config/overlay_edit.go:379–384`。新增回归覆盖两项失败编辑、跨 tab、逐项恢复及保存：`internal/menu/session_overlay_test.go:475–524`。
- **PM-001 / QA-001：保持关闭。** 规则合并后同步所有绑定及预设。证据：`internal/tui/options_rules.go:109–126`。
- **PM-002 / QA-002：保持关闭。** 先展开旧格式，再删除指定规模键。证据：`internal/menu/session_overlay.go:246–250,330–350`。
- **PM-003 / QA-004：保持关闭。** 首次覆盖刷新来源与恢复入口，复用输入组件。证据：`internal/tui/options_form.go:568–577,739–748`、`internal/tui/options_panel.go:337–347`。
- **PM-004：保持关闭。** 确认期间清空 tab 命中区域并拒绝切换。证据：`internal/tui/options_tabs.go:65–67,140–142`。
- **PM-005 / QA-005：保持关闭。** 窄屏断言仅消费 resize 后输出。证据：`internal/tui/pty_test.go:323–337`。
- **PM-006 / QA-006：原有效配置路径保持关闭。** `SyncTUI` 同步完整基础预览文档。证据：`internal/menu/doctor_session.go:81–89`。新增无效草稿路径出现同类继承回归，见 PM-009。
- **PM-007 / QA-007：保持关闭。** 失败编辑保留、标脏、报告错误，保存拒绝无效覆盖。证据：`internal/menu/session_overlay.go:227–235,419–424`、`internal/tui/options_inherit.go:119–122`、`internal/config/overlay_edit.go:379–384`。
- **QA-003：原有效配置路径保持关闭。** 无草稿时，切换及恢复仍在候选验证失败后直接返回。证据：`internal/menu/session_overlay.go:117–130,251–268`。

验收表仅列状态变化：**Complete 1、Partial 1、Missing 0、Contradicted 0、Unverifiable 0**。

| 要求 | 预期行为 | 代码证据 | Status |
|---|---|---|---|
| 第5条：恢复继承 | 多个失败覆盖可逐项撤销，保留其余输入 | `session_overlay.go:246–268`；`session_overlay_draft.go:12–47` | Complete |
| 第5条：基础变更同步 | 跨 tab 后未覆盖字段采用最新基础值 | `session_overlay.go:117–122,221–233` | Partial |

**PM-009 · medium：无效草稿跨 tab 返回后，继承值停留在旧基础配置。**

合法基础配置缺少整个 `tui` 时，Project 修改主题产生失败草稿。关闭错误提示，切 Global，将 launcher 从 `auto` 改为 `foreground`，按 Esc 保留编辑，再切 Project。部分 `tui` 仍使合并失败；新增分支直接复制旧 `overlayDraft`，未覆盖 launcher 继续显示“全局：auto”。

违反第5条“继承值随基础配置变更更新”。用户看到的继承来源与当前 Global 编辑不一致。

直接引入点：[session_overlay.go:122](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:122)。草稿在 `233` 行冻结整份配置；Global setter 已通过 `options.go:279–281`、`session_overlay.go:221–225` 更新基础文档，但返回 Project 时未用于刷新草稿。界面重新建表并显示该旧值：`internal/tui/options_tabs.go:52–61`、`internal/tui/options_form.go:537–538`。

最小修复：返回无效草稿时，按最新基础文档重建展示，保留显式覆盖及失败输入；保存继续严格校验原始覆盖。补充“失败主题编辑、Global 修改 launcher、返回 Project”的回归，验证继承值刷新且不新增 launcher 覆盖。

**Observed：** 工作树干净，增量 `git diff --check` 通过。本轮只读审查，未复跑测试或 PTY；211 项定向测试及全量 1318 pass / 1 skip 为调用方记录。任务文件删除已尝试，只读文件系统拒绝，文件保留。

NON-BLOCKING: none

```kander-findings
{
  "FINDINGS": [
    {
      "id": "PM-009",
      "tier": "medium",
      "text": "Inferred，高置信度，本轮新增。违反 task-spec.md ACCEPTANCE_CRITERIA 第5条的继承值随基础配置变更更新要求。合法基础配置缺少整个 tui 时，Project 修改主题产生失败草稿；关闭错误提示，切 Global 将 launcher 从 auto 改为 foreground，按 Esc 保留编辑，再切 Project。部分 tui 仍使合并失败，新增 SetTarget 分支直接复制旧 overlayDraft，导致未覆盖 launcher 继续显示“全局：auto”，与当前 Global 编辑不一致。最小修复：返回无效草稿时按最新基础文档重建展示，保留显式覆盖及失败输入，保存继续严格校验原始覆盖。增加失败主题编辑、Global 修改 launcher、返回 Project 的回归，断言继承值刷新且不新增 launcher 覆盖。",
      "evidence": "FIX RANGE 的 internal/menu/session_overlay.go:233 保存整份 Config 快照，:117-122 在预览失败时直接复制该旧快照。internal/menu/options.go:279-281 与 internal/menu/session_overlay.go:221-225 已将 Global launcher 编辑同步到 scopeRaw，但新增回退分支不使用更新后的基础值。internal/config/config.go:738-743 允许整个 tui 缺失，:616-634 拒绝不完整 tui，证明上述回退可达。internal/tui/options_tabs.go:52-61 切换后使用 Session.Config 重建页面；internal/tui/options_form.go:537-538 显示 launcher 的继承值。新增 internal/menu/session_overlay_test.go:492-496 仅往返 tabs，没有在 Global 修改基础字段。"
    }
  ],
  "NON_BLOCKING": []
}
```