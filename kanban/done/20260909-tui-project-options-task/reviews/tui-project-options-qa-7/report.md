Role: QA  
Commit: `f4d4eaad922231bc161bfe565feeaa76fd978fa8`  
Task Context: TUI Options 项目覆盖编辑；增量复核 `35170ca..f4d4eaa`，前轮 `tui-project-options-qa-6`。  
Reviewed Scope: 完整读取任务文件及 spec，核验全部 8 个变更文件匹配 COMMIT TREE。Observed：`git diff --check` 通过，变更文件均未超过 1000 行。只读环境未重跑测试；采纳调用方目标提交的 menu/TUI、全量包及 PTY 测试通过记录。

| 行为/质量 | 复核结果 |
|---|---|
| 架构边界 | Inferred：保持 TUI 调用 menu、config 负责合并与持久化，无新增依赖倒置。 |
| QA-001 / PM-001 | **关闭**。合并后同步全部规则绑定，并刷新预设与展示。[options_rules.go:118](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_rules.go:118)。连续应用及稀疏保存断言见 `options_overlay_test.go:438–469`。 |
| QA-004 / PM-003 | **关闭**。复用同一输入与 accessor，刷新标题、恢复入口；按实际输入变化写回。[options_form.go:568](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:568)、同文件 739–748 行。按键回归见 `options_overlay_test.go:349–389`。 |
| QA-006 / PM-006 | **关闭**。`SyncTUI` 同步完整 TUI section 至基础缓存。[doctor_session.go:81](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/doctor_session.go:81)。切 Project、恢复继承断言见 `session_overlay_test.go:425–442`。 |
| QA-007 / PM-007 | **原问题关闭**。失败候选保留并标脏，错误显示，保存拒绝发布。[session_overlay.go:220](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:220)、[options_inherit.go:119](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_inherit.go:119)。新增恢复回归见 QA-008。 |
| QA-002 / PM-002 | 保持关闭。旧格式先展开，仅删除目标路径。`session_overlay.go:235–247`。 |
| QA-003 | 原切换、恢复原子性保持关闭。`session_overlay.go:115–125,235–247`。 |
| QA-005 / PM-005；PM-004 | 保持关闭。PTY 仅检查 resize 后输出；确认期间清除命中区并禁止切 tab。`pty_test.go:323–337`；`options_tabs.go:65–67,141`。 |
| 测试质量 | 新测试使用临时配置，覆盖连续输入、稀疏保存及单字段失败恢复；未覆盖多个失败编辑的逐项撤销。 |

FINDINGS：1 项。

**QA-008 — medium，Inferred，高置信度：保留多个失败覆盖后，恢复继承无法撤销**

合法基础配置缺少整个 `tui`。用户在 Project 修改主题，关闭错误提示，再修改刷新间隔。两项失败编辑现在均保留于 `overlayRaw`。随后恢复任一字段，剩余字段仍构成不完整的 `tui`；整份候选校验失败，删除不提交。两个恢复入口均无法撤销，保存也持续失败。

本轮新增的失败候选保留路径见 [session_overlay.go:220](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:220)（至 225 行）；恢复必须整份候选有效才提交，见同文件 235–247 行。`config.go:616–634,738–743` 证明触发条件；`options_inherit.go:56–64` 逐项恢复，无法合并撤销。

违反恢复继承删除对应键及失败后可继续编辑的要求。最小修复：区分无效编辑草稿与有效配置，允许无效草稿逐键撤销，保存继续严格校验；保留原有效状态下恢复操作的原子性。增加“两项失败编辑、逐项恢复、最终保存”的 Session 回归。

NON-BLOCKING: none

```kander-findings
{
  "FINDINGS": [
    {
      "id": "QA-008",
      "tier": "medium",
      "text": "Inferred，高置信度，本轮新增。noteOverride 现在保留校验失败的覆盖，但恢复继承仍要求删除单键后的整份候选有效。合法基础配置缺少整个 tui 时，Project 连续修改主题和刷新间隔，两个失败编辑均进入 overlayRaw；恢复任一字段后仍剩不完整 tui，校验失败使删除不提交。因此两个字段均无法通过恢复继承撤销，保存也持续失败。违反恢复继承删除对应键及失败后可继续编辑的要求。最小修复：区分无效编辑草稿与有效配置，允许无效草稿逐键撤销，保存继续严格校验，并保留原有效状态下恢复操作的原子性。增加两项失败编辑、逐项恢复及最终保存的 Session 回归。",
      "evidence": "FIX RANGE 的 internal/menu/session_overlay.go:220-225 新增将失败键保留到 overlayRaw；同文件:235-247 的 RestoreInherit 仅在整份候选合并校验成功后提交删除。internal/config/config.go:738-743 允许整个 tui 缺失，:616-634 要求存在的 tui 包含完整字段。internal/tui/options_inherit.go:56-64 逐项调用恢复，失败后不提交删除。新增 internal/menu/session_overlay_test.go:445-472 只验证一个失败字段的恢复，未覆盖多个失败键相互阻止删除的状态。"
    }
  ],
  "NON_BLOCKING": []
}
```

已尝试删除任务文件；只读文件系统拒绝，不影响审核结果。