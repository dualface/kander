Role: PM  
Commit: `cc4349813a01b125c867195317f72db94e1257f3`  
Task Context: [冻结需求](/home/dualf/works/kander/kanban/working/20260909-tui-project-options-task/reviews/tui-project-options-pm-4/task-context.md)，及集成补审上下文。  
Reviewed Scope: `98a0a77..cc43498`；覆盖配置读写、Session 草稿、TUI tabs／恢复／异步加载／输入复用及直接消费者。

静态验收：Complete 20、Partial 2、Missing 0、Contradicted 0、Unverifiable 1。存在 **2 项 medium gate findings**。

Unverifiable：本轮受只读限制，未重跑测试或终端验收；调用方报告的测试通过数量未独立验证。`git diff --check` 通过。

**PM-1 — medium — Inferred，置信度高：修改项目界面语言误建代理语言覆盖**

触发：基础配置为 `language: "en"`，基础与项目均缺少 `agent_language`；在 Project 仅将界面语言改为 `ja`。

[options_form.go:679](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:679) 先调用 `SetLanguage`。项目合并重新生成 Config，按 [config.go:753](/home/dualf/works/kander/worktrees/tui-project-options/internal/config/config.go:753) 将缺失的代理语言推导为 `ja`。随后 [options_form.go:686](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:686) 把仍为 `en` 的旧绑定误判为用户编辑，建立 `agent_language: "en"` 覆盖。

违反需求第 39 行“编辑只建立实际修改字段的覆盖”。保存后代理语言被意外固定，失去继承。

最小修复：按本次事件前的表单基线识别实际编辑；合并导致的继承值变化只刷新绑定，不调用对应 setter。

**PM-2 — medium — Inferred，置信度高：Global 更换 Reviewer 后，Project 继承旧角色模型**

触发：基础配置已有 PM 的 Codex 模型；Global 将 PM Reviewer 改为 Claude，尚未保存便切到 Project。

[options_form.go:718](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:718) 更新 Reviewer 并重置模型；但 [options.go:501](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/options.go:501) 只更新 `Config.Models.ReviewRoles`，未同步 `scopeRaw`。Project 切换与恢复继承仍使用 [session_overlay.go:148](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:148) 的旧原始模型。

违反需求第 39–40 行的最新继承与跨 tab 草稿契约。Global 显示 Claude 默认模型，Project 却把旧 Codex 模型标为 Global 继承；恢复模型继承也得到旧值。

最小修复：Global 重置角色模型时，将重置后的该角色字段同步到 `scopeRaw`，保留其他模型字段及项目显式覆盖。

NON-BLOCKING: none

任务文件删除已尝试；因 `Read-only file system` 遗留，不影响审查结果。

```kander-findings
{
  "FINDINGS": [
    {
      "id": "PM-1",
      "tier": "medium",
      "text": "Inferred，置信度高。基础配置 language=en，基础与项目均缺少 agent_language 时，在 Project 仅修改界面语言为 ja，SetLanguage 的原始合并会把有效 AgentLanguage 推导为 ja；同次 apply 随后把旧表单绑定 en 误判为编辑，建立并保存 agent_language=en 覆盖。违反冻结需求第39行“编辑只建立实际修改字段的覆盖”，使未编辑的代理语言失去继承。最小修复：按事件前表单基线识别实际编辑，合并产生的继承值变化只刷新绑定，不触发对应 setter。",
      "evidence": "internal/tui/options_form.go:679-690 依次处理 language、agent_language，后者与已被重建的 Config 比较；internal/menu/options.go:296-306 两个 setter 均记录覆盖；internal/menu/session_overlay.go:182-195 合并后替换 Config；internal/config/config.go:753-757 对缺失 agent_language 按合并后的 language 推导。契约：/home/dualf/works/kander/kanban/working/20260909-tui-project-options-task/reviews/tui-project-options-pm-4/task-context.md:39。"
    },
    {
      "id": "PM-2",
      "tier": "medium",
      "text": "Inferred，置信度高。基础已有 PM 的 Codex 角色模型时，在 Global 将 PM Reviewer 改为 Claude，未保存便切换 Project：Global 模型已重置为 Claude 默认值，Project 却仍将旧 Codex 模型显示为 Global 继承；恢复项目模型继承也取旧值。原因是 Global 的 ResetReviewRoleModel 只更新 Config，未同步 Project 预览使用的 scopeRaw。违反冻结需求第39-40行的最新继承及跨 tab 草稿契约。最小修复：Global 重置角色模型时同步该角色的 scopeRaw 字段，保留其他模型字段和项目显式覆盖。",
      "evidence": "internal/tui/options_form.go:718-722 更换 Reviewer 后调用 ResetReviewRoleModel；internal/menu/options.go:276-278 仅记录 reviewers 键，internal/menu/options.go:475-502 重置并填充 Config.Models.ReviewRoles，但未同步 scopeRaw；internal/menu/session_overlay.go:114-148 切换 Project 使用 scopeRaw 合并，internal/menu/session_overlay.go:249-254 恢复继承同样使用该原始文档。契约：/home/dualf/works/kander/kanban/working/20260909-tui-project-options-task/reviews/tui-project-options-pm-4/task-context.md:39-40。"
    }
  ],
  "NON_BLOCKING": []
}
```