# Integration Review Dispositions

作者：codex。复核范围：cc4349813a01b125c867195317f72db94e1257f3 之后的修复。修复提交：ead0a736302a273b5ad2489fab6e1d22265b5d24。

- PM-1 / QA-1（medium）：fixed。临时旧基础缺 agent_language，仅改 Project language，修复前复现旧 en 被写成覆盖。按本事件编辑前基线识别两个语言字段；只刷新未编辑的派生绑定。TestProjectLanguageEditDoesNotOverrideDerivedAgentLanguage 验证最终覆盖文件仅含 language，重建后代理语言为 ja。
- PM-2 / QA-3（medium）：fixed。Global 换 Reviewer 后未保存切 Project，修复前复现 old-codex/low 仍被继承。ResetReviewRoleModel 把同一角色的新默认值同步到 scopeRaw，使用原始 JSON 对象，保留其他角色和 Project 覆盖。TestGlobalReviewerResetUpdatesProjectModelInheritance 验证新 model/effort、无隐式覆盖及恢复后新继承。
- QA-2（medium）：fixed。有效模型编辑替换 Config 后清空 effort，修复前重建显示旧 high。失败 noteOverride 现在复用已有 previewOverlayDraft 从最新基础与原始编辑重建显示，不依赖旧字段映射。TestModelDraftSurvivesConfigReplacementAndFormRebuild 验证同页连续编辑、重建保留空值、严格保存失败、修正后成功且早前模型编辑保留。Huh fixture 同步输入内部值与 accessor，使测试模拟实际已输入状态。

完整首轮报告见 pm-1.md 与 qa-1.md。NON_BLOCKING 均为空。三项新增回归均已通过；整仓 go test ./... -json -count=1 -timeout 240s：1413 pass / 1 Windows-only skip / 21 packages。git diff --check 通过；注释、死代码和行数自检通过。初次修复回归暴露原始 JSON 对象类型与测试输入 fixture 问题，已修正，失败未被计作通过。
