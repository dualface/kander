# TUI Options rebase 手解冲突后的重审

- TYPE: Chore
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 18:27
- OWNER: grok
- SESSION:
- WINDOW:
- STARTED_AT: 2026-09-09 18:28
- FINISHED_AT:
- TASK_BRANCH: 20260909-options-reload-config
- RESULT:

## GOAL

对任务卡 `20260909-options-reload-config-task` 在合入前 rebase 到 `origin/develop` `fd24781ac0f6abf001e11422d926b566e56623f8` 之后的交付做一次完整 PM+QA 重审。原卡计划已密封且关闭目标不在新历史上，无法再挂批次。审核范围是工作树 `/home/dualf/works/kander/worktrees/20260909-options-reload-config` 上 `fd24781ac0f6abf001e11422d926b566e56623f8..bce1ad374757cbf18db674d05e56974c6de68b81`。

原卡要实现：TUI 每次打开 Options（面板尚未打开）都重新读取作用域 `config.json` 和项目 `.kander-config.json`，表单、overlay 提示和有效语言与磁盘一致；可编辑 session 仍来自 `LoadScope`；任一侧失败走 `loadErr` 且不保留旧 session；保存不把 overlay 键写入作用域文件。rebase 手解把每次打开的 `requestSession` 与 develop 的 `Load(false)` 完整配置门槛合在一起；另有完整配置错误框测试改为匹配 en/zh/ja 文案。

## USER_DECISIONS

- 用户选择路径 1：新建本重审卡，对本范围走 PM+QA；原卡 `20260909-options-reload-config-task` 留在 working，等本卡审核通过后再把 `bce1ad374757cbf18db674d05e56974c6de68b81` 合入 `develop`。
- 复用原卡任务分支 `20260909-options-reload-config` 及其工作树，不另开分支。
- 不实现 Global/Project 双 tab；那是 `20260909-tui-project-options-task` 的范围。

## EXPECTED_OUTCOME

本卡有密封审核计划，批次对 `fd24781ac0f6abf001e11422d926b566e56623f8..bce1ad374757cbf18db674d05e56974c6de68b81`（若有修复则到最终修复提交）完成 PM required、QA auto，CSA/Hacker 按本仓库 AGENTS.md 为 N/A。批次关闭后，原卡可以按 Git 一次门继续合入。

## ACCEPTANCE_CRITERIA

- [ ] 以工作树 `/home/dualf/works/kander/worktrees/20260909-options-reload-config` 为 CWD，基线 `fd24781ac0f6abf001e11422d926b566e56623f8`，目标至少包含 `bce1ad374757cbf18db674d05e56974c6de68b81`，建立独立密封计划并跑完 PM 与 QA。
- [ ] PM 按原卡契约核对：每次打开 Options 重读作用域和 overlay、失败可见、可编辑值不被 overlay 污染、放弃未保存编辑后恢复、语言绑定使用刚读到的合并结果且 welcome 未完成时不绑定 overlay 语言。
- [ ] QA 核对手解冲突后的加载路径、完整配置门槛 `Load(false)`、测试与 `loadErr` 文案在 en/zh/ja 下可判定。
- [ ] 必须修复项处理完毕、批次关闭；原卡合入仍由原卡执行，本卡不直接 push `develop`。

## THREAT_MODEL

N/A。配置读写继续走现有 `internal/fs` 安全边界，本卡不放宽路径与平台约束。

## OUT_OF_SCOPE

- 既有问题：不处理 Options 以外的 TUI、启动或看板问题；不修复与本次读盘无关的 session/doctor 既有行为。
- 加固：不引入配置文件监视、热重载或新的权限模型。
- 共享契约与文档：不改配置 schema、合并语义或安装作用域；不重写原卡已密封的计划。
- 相邻功能：不实现 Global/Project 双 tab、不把 overlay 变成可编辑缓冲、不在打开时把主题/列数立刻应用到看板、不改 CLI `kander config`。本卡不代替原卡做 `develop` 合入。

## DISCUSSION

- 原卡 `20260909-options-reload-config` 计划已 sealed，批次 `20260909-options-reload-batch-1` 关闭于 `46ed8a32fd8f579ef9e16c9fd103b5fea200bc72`。rebase 后该 SHA 不是 HEAD 祖先，`extend-plan` 返回 `plan extension CAS/sealed conflict`。
- Git 一次门：手解 `options_panel.go` / `options_test.go` 实质冲突后必须重审。本卡是该重审的载体。
- SELF_REVIEW: 通过。目标与验收只覆盖 rebase 后范围的 PM+QA 与批次关闭；原卡合入排除在本卡之外；未把建议写成用户决策。

## IMPLEMENTATION

- 复用分支 `20260909-options-reload-config`，工作树 `/home/dualf/works/kander/worktrees/20260909-options-reload-config`。
- 审核基线 `fd24781ac0f6abf001e11422d926b566e56623f8`，当前目标 `bce1ad374757cbf18db674d05e56974c6de68b81`。

## SUMMARY
