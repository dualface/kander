# 实施计划

1. 定义 Issue 查询类型并实现版本化 REST list/show/comments。
2. 接入 CLI 查询、筛选、分页和 JSON 输出。
3. 实现 TUI Issues overlay、双栏/分页布局、详情 viewport，并将 `g` 用于 Issues、`f` 用于聚焦 Agent 窗口。
4. 接入 pendingWork、请求身份、stale result 丢弃及安全浏览器打开。
5. 同步 CLI registry、AGENTS.md、三语 i18n、三版 README、帮助和设计文档。
6. 完成 provider、CLI、TUI、PTY 和全量测试。

验证：Issue provider/CLI 测试、`go test ./internal/tui`、PTY smoke、`go test ./...`。

发布/回滚：只读功能；关闭入口即可回滚，不影响 board 数据。
