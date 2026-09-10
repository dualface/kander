Role: QA  
Commit: `cc4349813a01b125c867195317f72db94e1257f3`  
Task Context: 已完整读取冻结 spec、任务文件及集成补审背景。  
Reviewed Scope: 审查 `98a0a77..cc43498` 引入的 Options 配置流及相关消费者；核对工作树与提交内容。覆盖 TUI、menu.Session、config、internal/fs、flow、三语文案及测试。环境只读，未重跑 Go/PTY；采纳调用方记录：1410 通过、1 Windows 专属跳过、21 包。

| 行为／质量 | 结论 |
|---|---|
| 安装模式、路径、保存目标 | Observed：复用入口判定、主 worktree 解析与安全写入；保持包依赖方向 |
| 稀疏保存、冲突、恢复继承 | Observed：原始 JSON 合并、覆盖键 CAS、失败保留机制存在 |
| 字段联动与草稿显示 | Inferred：发现 QA-1、QA-2 |
| 最新 Global 继承 | Inferred：发现 QA-3 |
| 异步重载、输入复用、质量检查 | Observed：有针对性回归；变更文件均未触发行数门，`git diff --check` 通过，新增文案三语齐全 |

**FINDINGS**

**QA-1 — medium — Inferred；置信度：高。修改界面语言会误建 agent_language 覆盖。**

基础配置缺少 `agent_language`、当前 `language=en` 时，Project 只改界面语言为 `ja`：合并校验先派生 `AgentLanguage=ja`，随后表单将未改动的旧值 `en` 误判为编辑，写入显式覆盖。结果额外保存 `agent_language=en`，破坏“只覆盖实际修改字段”及原有派生语义。

证据：[options_form.go:679](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:679) 顺序应用两个语言字段；[config.go:753](/home/dualf/works/kander/worktrees/tui-project-options/internal/config/config.go:753) 按合并后的语言派生缺失值。

最小修复：按表单编辑前的字段基线识别用户修改；语言合并后刷新未编辑的派生字段。增加 TUI 回归，断言覆盖文件只有 `language`。

**QA-2 — medium — Inferred；置信度：高。配置替换后，模型输入仍写旧映射，隐藏无效草稿。**

Project 已有 `large_model` 覆盖时，同页修改该模型，再清空尚未覆盖的 `large_effort`：第一次有效编辑替换 `Session.Config`，但未重建表单；后续 `field.Set` 写入旧映射。空 effort 校验失败，仅保留在 `overlayRaw`。继承状态变化触发表单重建，又从当前 Config 读回旧 effort；界面显示合法旧值，保存却因隐藏的空值失败。

证据：[options.go:385](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/options.go:385) 捕获模型映射；[session_overlay.go:195](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:195) 替换 Config，失败分支仅更新原始草稿（224–237）；[options_form.go:757](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:757) 使用旧字段写入；重建时按 Config 值替换输入（575–585）。

最小修复：模型字段通过 Session 的当前配置按路径读写，确保无效值同步到草稿显示。增加上述连续编辑回归，断言重建后仍显示空值、保存失败且可继续修正。

**QA-3 — medium — Inferred；置信度：高。Global Reviewer 重置模型后，Project 仍继承旧模型。**

基础配置已有 PM 的 Codex 模型时，Global 将 PM Reviewer 改为 Claude，模型重置为 Claude 默认值。未保存便切 Project：`scopeRaw` 只同步 Reviewer，模型仍为旧值；Project 因而显示 Claude 搭配旧 Codex 模型，且标为 Global 继承。违反最新 Global 草稿继承要求。

证据：[options_form.go:718](/home/dualf/works/kander/worktrees/tui-project-options/internal/tui/options_form.go:718) 联动重置模型；[options.go:276](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/options.go:276) 仅记录 Reviewer；[options.go:501](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/options.go:501) 重置模型未同步原始文档；[session_overlay.go:148](/home/dualf/works/kander/worktrees/tui-project-options/internal/menu/session_overlay.go:148) Project 从该文档合并。

最小修复：Global 模型重置后同步对应角色的 `model`、`effort` 到 `scopeRaw`。增加 Session 回归，断言切 Project 后继承新模型且不创建覆盖键。

NON-BLOCKING: none

任务文件删除已尝试；只读文件系统拒绝，文件遗留不影响结论。

```kander-findings
{
  "FINDINGS": [
    {
      "id": "QA-1",
      "tier": "medium",
      "text": "Inferred；置信度：高。基础配置缺少 agent_language 且 language=en 时，Project 只将 language 改为 ja，合并先派生 AgentLanguage=ja，表单随后把未修改的旧 agentLanguage=en 写成显式覆盖。额外持久化 agent_language=en，违反只覆盖实际修改字段及既有派生语义。最小修复：按编辑前字段基线识别用户修改，并刷新未编辑的派生字段；增加断言覆盖文件只有 language 的 TUI 回归。",
      "evidence": "internal/tui/options_form.go:679-690 顺序应用 language 和 agent_language，并将合并后的 Config 与旧绑定值比较；internal/menu/options.go:297-300 调用 noteOverride 重新合并；internal/config/config.go:753-755 对缺失 agent_language 按合并后的 language 派生。路径可由合法旧配置及一次界面语言修改触发。"
    },
    {
      "id": "QA-2",
      "tier": "medium",
      "text": "Inferred；置信度：高。Project 已有 large_model 覆盖时，同页修改模型再清空未覆盖的 large_effort：有效编辑替换 Session.Config，表单模型字段仍持有旧映射；无效 effort 只进入 overlayRaw。继承状态变化触发重建，输入被旧合法值替换，保存却因隐藏的空 effort 失败。最小修复：模型字段通过 Session 当前配置按路径读写，保证无效草稿显示同步；增加连续编辑、重建及修正回归。",
      "evidence": "internal/menu/options.go:379-387 的 ModelField.entry 捕获配置映射；internal/menu/session_overlay.go:188-195 在有效合并后替换 Config，224-237 的失败分支只更新 overlayRaw 和标记；internal/tui/options_form.go:754-763 先 field.Set 再 NoteModelOverride，575-585 重建时从 Config 读值并替换不匹配输入；internal/config/config.go:562-569 拒绝空执行 effort。"
    },
    {
      "id": "QA-3",
      "tier": "medium",
      "text": "Inferred；置信度：高。基础配置已有 PM 的 Codex 模型时，Global 将 PM Reviewer 改为 Claude，会重置模型，但只把 Reviewer 同步到 scopeRaw。未保存切 Project 后，Project 显示 Claude 搭配旧 Codex 模型并标为 Global 继承，违反最新 Global 草稿继承要求。最小修复：Global 模型重置后同步该角色的 model、effort 到 scopeRaw；增加切换后继承新模型且不创建覆盖键的 Session 回归。",
      "evidence": "internal/tui/options_form.go:717-722 修改 Reviewer 后调用 ResetReviewRoleModel；internal/menu/options.go:276-278 仅记录 reviewers.<role>，501-502 重置 Config.Models.ReviewRoles 但未同步 scopeRaw；internal/menu/session_overlay.go:224-228 只同步传入路径，144-148 的 Project 预览以 scopeRaw 为基础。"
    }
  ],
  "NON_BLOCKING": []
}
```