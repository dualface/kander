# Browse and query GitHub Issues in the CLI and TUI

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

基于已关联仓库，让用户通过 CLI 和现有终端看板查询、筛选并查看 GitHub Issues 及按需评论，同时保持看板状态语义和交互响应性。

## USER_DECISIONS

- TUI 使用 `g` 打开的 overlay，不新增 GitHub 看板列。
- 宽屏为列表/详情双栏，窄屏为列表页/详情页。
- 支持 `Enter` 详情、`Tab` 状态、`l` label、`r` 刷新、`o` 浏览器、`Esc` 关闭；评论进入详情后按需加载。
- 原 `g` 聚焦 Agent 窗口迁移为 `f`；`g` 专用于 GitHub Issues。
- 网络操作异步执行，不阻塞看板输入和刷新。

## EXPECTED_OUTCOME

用户可在 CLI 或 TUI 中识别当前仓库，按状态、label、搜索和数量限制浏览 Issues，安全查看正文与按需评论；慢请求、乱序结果、窄屏、空列表、认证和网络错误均保持界面响应及状态准确。

## ACCEPTANCE_CRITERIA

- [ ] 定义 provider-neutral issue summary、snapshot、comment、query 接口；通过固定 REST API version 查询 list/show/comments，并过滤 REST 返回的 Pull Requests。
- [ ] `kander issue list|show` 提供人类可读和 `--json` 输出，支持 open/closed/all、label、search、limit、分页及可操作错误。
- [ ] `g` 打开由 `overlay()` 合成的 Issues 弹窗；关闭后底层选择、滚动和布局不变，Issues 不成为 board column 或虚假 card state。
- [ ] 宽屏稳定显示双栏；窄屏可在列表和详情间返回；列表显示 number、title、labels、updated time，详情使用 viewport 和 Glamour。
- [ ] 键位语义完整：`g` 打开、`f` 聚焦 Agent 窗口、`Enter` 详情、`/` 搜索、`Tab` 状态、`l` label、`r` 刷新、`o` 浏览器、`Esc` 关闭；同步迁移 help、测试和三语文案。
- [ ] list/show/comments/refresh 经结构化 `pendingWork` 返回；请求携带序号和 repository/issue identity，丢弃 stale result，后台不直接写 stdout/stderr。
- [ ] Issue 正文、评论及渲染结果去除危险 C0、ANSI、OSC 和 bidi 控制；光标、选择、hit testing 基于清理后的 plain text；不自动抓取外链，不渲染未验证 host 的 terminal hyperlink。正文、评论数量、单条字节和分页均有界，超限明确报错。
- [ ] `o` 仅从 canonical identity 构造 URL、验证 host 和组成部分，并以直接 argv 打开浏览器；不得采用 Issue 提供的任意 URL 或 shell。
- [ ] 测试覆盖筛选分页、PR 过滤、 hostile JSON/Markdown、light/dark、宽窄布局、键鼠、异步乱序、错误恢复和 PTY smoke；`go test ./...` 通过。
- [ ] 同步维护 CLI registry/tests、AGENTS.md 包图和子命令、三语 i18n catalog、README.md/README-CN.md/README-JA.md 的查询和键位说明。

## THREAT_MODEL

攻击者可控制 Issue 字段、评论、链接、响应大小和时序，尝试 prompt/terminal/hyperlink 注入、资源耗尽和 stale response 覆盖。实现须限制内容、验证身份、丢弃过期结果，并分离远端文本与可信控件。

## OUT_OF_SCOPE

- 现有问题：不重构与 Issues 浏览无关的看板几何、拖选复制或 options panel。
- 额外加固：不实现通用 Markdown sanitizer；仅满足本视图的控制字符、链接和大小边界。
- 共享契约：复用前置 repository resolver；本卡拥有 Issue 查询契约和只读展示。
- 相邻功能：不保存为卡片、不启动 Agent、不写回 Issue、不后台常驻同步、不实现原生登录或 PR 浏览。
- 文档：更新本目标所需的 AGENTS.md、命令帮助、三语 README、TUI 键位和设计文档；不改无关章节。

## DISCUSSION

```text
PREREQUISITES: 20260909-github-issue-access-task
```

- TUI 是“查询 Issues”目标的交付界面，不再作为独立技术层卡片。
- 数据层采用 `gh api` 的固定版本 JSON REST 响应；这是实现约束，不是用户输入字段。
- `/` 复用现有搜索习惯作为 Issues 搜索入口；这是实现选择，不是用户输入字段。
- 本卡及后续卡均遵循 `20260909-github-issue-access-task/group-plan.md`。
- SELF_REVIEW: 已按新版规则将查询与其 CLI/TUI 展示归为一个可独立验收目标；未混入保存和交接，响应式布局、异步模型、安全浏览均有可判定验收项。
- CARD_REVIEW: PASS；独立 Agent 最终复审确认查询目标、g/f 键位、布局、按需评论、异步 stale 防护、安全渲染/浏览、CLI/TUI/docs 及 plan 完整一致。
