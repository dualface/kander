# 任务组编排计划

任务组：`20260909-github-issues-group`。

依赖链：

1. `20260909-github-issue-access-task`
2. `20260909-github-issue-tui-task`
3. `20260909-github-issue-import-task`
4. `20260909-github-issue-handoff-task`

启动与交付：

1. 用户另行发出启动指令并确认本编排后，检查 main/develop、origin、工作树和四卡；创建 `group/20260909-github-issues-group` 及专用 group worktree，记录最新 develop 创建锚点。
2. 每张成员卡从当时最新 group branch 创建独立 task branch/worktree。执行 Agent 完成验证和提交后，rebase 到最新 group branch，重新验证，记录 delivery SHA，进入 review。
3. Orchestrator 核验 delivery 后按依赖顺序 fast-forward 到 group branch；只有确认前置 delivery 已进入 group branch，才释放并启动下一卡。成员任务不并行。
4. 所有成员启动后，对已接收 delivery 按可读批次执行 group-level review。依仓库例外，CSA/Hacker 始终 N/A；PM 必需；QA 按当前 large stage policy 执行。发现项通过 durable dispatch 交回原执行 Agent，修复后重新接收并完成批次闭环。
5. 全部 delivery、验证和 review 闭环后，获取最新 develop，按规则 clean rebase group branch 并验证完整 patch 未变化；任何冲突或 patch 差异停止并请求用户决定。
6. 只有本计划获得执行/集成授权后，才将最终 group HEAD fast-forward 到 develop，并按 remote 路径 push/fetch/sync；验证每张成员交付已包含于 develop。
7. 向原执行 Agent 分发 wrap-up，完成卡片记录与 done gate；随后清理成员及 group worktree/branches。main 不在本计划中，绝不自动更新。

本次用户仅确认建卡和拆分调整，未授权启动或集成；后续启动指令必须同时确认本编排及 merge-back。
