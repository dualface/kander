# Options panel keeps the root menu cursor after returning from a submenu

- 类型: Bug
- SIZE: small
- 任务组:
- 创建时间: 2026-09-05 00:30
- 负责人: cursor
- 会话: cursor dce7d048-e675-45f3-a2f3-0a201892cc4c
- 窗口: herdr:wG:t28:wG:p2H
- 开始时间: 2026-09-05 00:30
- 完成时间: 2026-09-05 00:40
- 任务分支: options-root-cursor
- 结果: completed

## 任务目标

选项面板从子级返回根菜单后, 光标应停在进入子级前选中的那一项, 而不是跳回第一项. 根因是 `openRoot` 与进入子级时把绑定值 `p.section` 清成空串, Huh Select 对不上选项就落到第一项.

## 用户决策

用户确认计划: 进入子级时保留 `p.section`; `openRoot` 不再把它重置为空; 关闭确认、doctor / 保存报告返回根菜单同样沿用该取值. 首次打开仍是空串, Huh 落到第一项. 不改行式 welcome 菜单, 不改分区内部字段焦点 (`rebuildAt` 已按选择器恢复).

## 预期成果

从根菜单进入任一子级 (分区、环境检查报告、保存报告、关闭确认后选「继续编辑」) 再返回, ▸ 仍停在进入前选中的根菜单项.

## 验收条件

- [x] 根菜单移到非首项, Enter 进入分区, Esc 返回, 焦点仍在原项 (绑定值与渲染焦点标记都对).
- [x] 从分区 Enter 提交返回根菜单, 焦点仍在进入前那一项.
- [x] 关闭确认选「继续编辑」回到根菜单, 焦点仍在进入确认前那一项.
- [x] 既有选项面板测试仍通过; `go test ./internal/tui` 覆盖上述回归.

## 威胁模型

N/A

## 不在本轮范围

- 既有问题: 排除行式 welcome / `internal/menu` 的光标记忆, 那是另一套 UI, 本卡只修 TUI 选项面板. 排除分区内部字段焦点, `rebuildAt` 已按选择器恢复.
- 并发/跨平台/安全加固: 排除 Windows 专项交互录制; 逻辑与平台无关, 以包内单测为准. 不改配置、看板或文件安全边界.
- 共享契约与文档: 排除发布规则与 `AGENTS.md` 改写; 这是面板内部交互, 不新增对外命令或配置键.
- 相邻功能: 排除选项字段布局、配色、模型档位绑定; 只改根菜单选中项在往返子级时的保留.

## 讨论与决策

```text
前置任务: N/A
```

- 改动落在 `internal/tui` 的 `options_panel.go` / `options_form.go` 与 `options_test.go`.
- Huh Select 按绑定值恢复 `selected`; 重建根菜单时只要 `p.section` 仍是进入前的取值即可.

## 实施与验证

- 任务分支 `options-root-cursor` 基于本地 `develop` (`c8989f8`), 无 origin, 未同步远端.
- 不再在 `openRoot`、`openCloseConfirm`、`finishSection` 里把 `p.section` 清成空串; Huh Select 用绑定值恢复根菜单选中项.
- 提交 `a3a9f75e388bd93d0bf12f41ea44b3ed008a2b90` 保留选项根菜单往返子级时的光标位置
- 验证: `go vet ./...` 通过; `go test ./...` 通过; `go test ./internal/tui` 含三条新回归 (Esc 返回、分区提交返回、关闭确认继续编辑).
- 审核: PM (codex) PASS, QA (codex) PASS; CSA/Hacker 按本仓库规则 N/A. 无未处理项.
- 已 ff 合入本地 `develop`, 清理 worktree 与任务分支.

## 完成总结

选项面板从分区、关闭确认「继续编辑」等子级返回根菜单时, ▸ 停在进入前选中项. 改动已合入本地 `develop` (`a3a9f75e388bd93d0bf12f41ea44b3ed008a2b90`). 无 origin, 未 push. 验收四条均由包内测试覆盖并通过.
