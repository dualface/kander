# Fetch GitHub Issues and save them as Kander cards

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260909-github-issues-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 11:24
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

将用户选择的 GitHub Issue 以可追踪、幂等、事务安全的方式拉取并保存为普通 Kander 目录卡片，同时在 CLI 和 TUI 中清晰呈现导入结果。

## USER_DECISIONS

- 评论默认不拉取；用户显式选择后才保存。
- TUI 使用 `i` 导入；已导入 Issue 显示 task ID，并可跳转聚焦本地卡片。

## EXPECTED_OUTCOME

用户可从 CLI 或 TUI 导入指定 Issue，获得带稳定来源标识、完整且有界来源快照的 backlog 卡片；重复或并发导入返回同一既有任务，远端更新可提示但不会静默覆盖本地契约。

## ACCEPTANCE_CRITERIA

- [ ] 新增原子 board API，在一个可恢复事务中创建 `spec.md`、`source/github-issue.json` 和 `source/github-issue.md`；失败不发布缺少来源的卡片。
- [ ] 快照包含 schema version、canonical source key、仓库身份、Issue 字段、抓取时间及按需评论，不含 token 或敏感响应头。
- [ ] `kander issue import NUMBER` 与 TUI `i` 生成同一完整、可检查的 backlog 卡；原始 Markdown 不能改变卡片结构或终端状态。
- [ ] source-key 唯一性检查与卡片/附件发布位于同一 exclusive-board 事务边界；跨状态重复和并发竞争均只产生一个 canonical task ID，其余调用返回该任务，不提供一对多 duplicate 选项。
- [ ] 支持 repo、comments、type、large、language 和 JSON 输出；正文、评论数量、单条及总字节超过限制时拒绝发布任何卡片，并提示减小范围或不取评论，不保存截断快照。
- [ ] 导入始终留在 backlog，明确标出无法由 Issue 判定的契约项；不得自动生成 `SELF_REVIEW` 或 `CARD_REVIEW`。
- [ ] TUI 标示 imported/task ID；可跳转现存卡；远端 `updated_at` 较新时显示“有更新”，但不自动覆盖卡片契约或来源。
- [ ] TUI `i` 的网络和写盘均通过 pendingWork；结果绑定 request/repository/issue identity，切换选择后丢弃 stale result，后台不写 stdout/stderr。
- [ ] 来源 URL 仅从已验证 canonical identity 构造；不信任 API 返回的任意 URL。JSON 与可读 Markdown 附件保存完整来源；远端内容作为不可信数据，不直接拼入契约结构或顶层 Agent 指令。
- [ ] 测试覆盖 hostile Markdown、重复导入、评论、字段缺失、限额、事务故障恢复、并发及 CLI/TUI 一致性；`go test ./...` 通过。
- [ ] 同步维护 CLI registry/tests、AGENTS.md 包图和子命令、三语 i18n catalog、README.md/README-CN.md/README-JA.md 及来源格式文档。

## THREAT_MODEL

攻击者可控制 Issue 标题、正文、评论、作者名和链接，尝试 prompt injection、卡片结构注入、路径穿越、终端控制和资源耗尽。实现须保持来源与可信契约分离，限制数据，只写 canonical relative path，并沿用 board no-follow/reparse 和事务保护。

## OUT_OF_SCOPE

- 现有问题：不修复与导入事务无关的 board/TUI 缺陷。
- 额外加固：不抓取外链或附件，不执行代码，不建立通用内容安全平台。
- 共享契约：复用查询卡的 Issue snapshot；本卡拥有本地快照、导入事务和导入 UI。
- 相邻功能：不进入 todo、不启动 Agent、不写回或自动刷新 Issue、不实现认证。
- 文档：更新本目标所需的 AGENTS.md、命令帮助、三语 README 和来源格式；不改无关章节。

## DISCUSSION

```text
PREREQUISITES: 20260909-github-issue-tui-task
```

- 来源附件随卡片移动；导入成功后始终保留。
- JSON/Markdown 双附件和不可信内容隔离是实现及安全约束，不是用户输入字段。
- 本卡遵循 `20260909-github-issue-access-task/group-plan.md`。
- SELF_REVIEW: 已按新版规则只覆盖“拉取并保存”目标；CLI/TUI 导入是同一目标的入口，Agent 交接已移至后续卡，事务、幂等和安全验收完整。
- CARD_REVIEW: PASS；独立 Agent 最终复审确认完整快照超限拒绝、exclusive-board 唯一性、TUI pendingWork 身份、canonical URL、来源隔离、恢复/docs 及 plan 完整一致。
