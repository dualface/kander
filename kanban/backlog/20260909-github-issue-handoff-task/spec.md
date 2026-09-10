# Hand off an imported GitHub Issue to an Agent from the TUI

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260909-github-issues-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 11:47
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

让用户从 TUI 将已导入的 GitHub Issue 完善为真实可执行契约，经创建者自审和 Kander gate 后明确确认并交给 Agent。

## USER_DECISIONS

- TUI 使用 `s` 执行“导入并确认启动”。
- 已导入 Issue 显示并复用对应 task ID。
- 网络和启动结果通过异步 TUI 工作流返回，不阻塞无关看板操作。

## EXPECTED_OUTCOME

用户选择 Issue 后可编辑并审阅完整任务契约，取消时保留 backlog 来源，确认且 gate 通过时启动正确 Agent；重复、状态冲突、large 卡缺少独立卡审及启动失败均安全可恢复。

## ACCEPTANCE_CRITERIA

- [ ] `s` 先导入或定位现存 backlog 卡，再打开可编辑契约表单；已在其他状态的卡只跳转并拒绝重复启动。
- [ ] 表单可编辑 TYPE、SIZE、GOAL、USER_DECISIONS、EXPECTED_OUTCOME、ACCEPTANCE_CRITERIA、THREAT_MODEL、OUT_OF_SCOPE、DISCUSSION，明确标记无法从 Issue 判定的内容；显示不可变 LANGUAGE，需改变时取消并用目标语言重新导入。
- [ ] UI 展示建卡自审四项并要求创建者显式 attest 和填写结论，再将该结论记录为 `SELF_REVIEW`；程序不声称验证其真实性，不代填结论，不生成 `CARD_REVIEW`。
- [ ] 取消、验证失败或写入冲突保留 backlog 卡和来源，并可重新打开编辑；large 卡缺少 `CARD_REVIEW:` 记录时由现有 gate 拒绝，文档明确该记录只能来自真实独立审阅。
- [ ] 受控 update 和只读校验成功后，先展示包含 Issue、task ID、Agent、launcher 和状态转换的最终确认；用户确认后才受控 backlog→todo，再调用 `launch.Start`，不得先 move working。失败报告实际 backlog/todo 状态。
- [ ] TUI 仅为 herdr/tmux/tmux-session 提供后台启动；foreground/console 明确提示改用 CLI，保持现有 TUI 启动契约。
- [ ] 可信 spec 固定要求 Agent 读取来源附件；task file 仍只含 task ID 和既有固定要求，私有 Issue 内容或动态附件路径不进入 shell、argv、env 或顶层 task-file 指令。
- [ ] pendingWork 结果带 request/task/issue identity；切换选择时丢弃 stale result，launch 失败报告实际卡片状态和重试方式。
- [ ] 测试覆盖表单校验、自审记录、取消重试、revision 冲突、各状态重复操作、large review gate、异步乱序及启动成功/失败；`go test ./...` 通过。
- [ ] 同步维护 AGENTS.md TUI/包职责、三语 i18n catalog、README.md/README-CN.md/README-JA.md、键位帮助及交接恢复文档。

## THREAT_MODEL

攻击者可控制 Issue 内容并诱导用户启动错误任务，或利用 stale response、状态竞态和参数注入绕过确认。实现须绑定 repository/issue/task/revision identity，分离来源与契约，要求真实确认，并复用受控 board 和 launch API。

## OUT_OF_SCOPE

- 现有问题：不重构与交接流程无关的 launch、review 或 TUI 状态。
- 额外加固：不建立通用审批系统；只保护本流程的身份、确认和注入边界。
- 共享契约：复用前置导入服务和来源快照；本卡只拥有契约完善、自审、gate 与启动 UI。
- 相邻功能：不查询、保存、刷新或写回 Issue，不实现 CLI `--start`、自动批量启动或原生认证。
- 文档：更新本目标所需的 AGENTS.md、三语 README、TUI 键位帮助和交接恢复文档；不改无关章节。

## DISCUSSION

```text
PREREQUISITES: 20260909-github-issue-import-task
```

- 闭环：导入或定位 backlog 卡、编辑契约、创建者逐项自审、受控 update、todo gate、启动确认、`launch.Start`。
- “先保存 backlog”、复用受控 move/`launch.Start`、不增加旁路执行器及 launcher 限制来自项目规则和现有契约，不是用户输入字段。
- 本卡遵循 `20260909-github-issue-access-task/group-plan.md`。
- SELF_REVIEW: 已按新版规则只覆盖“交给 Agent”目标；保存由前置卡完成，本卡验收完整覆盖编辑、自审、gate、确认、启动和失败恢复。
- CARD_REVIEW: PASS；独立 Agent 最终复审确认完整表单、显式 attest、真实 CARD_REVIEW 边界、确认先于 todo、CAS 冲突、launcher/task-file 限制、失败恢复/docs 及 plan 完整一致。
