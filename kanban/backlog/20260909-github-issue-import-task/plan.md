# 实施计划

1. 定义 snapshot schema、source key 和安全 renderer。
2. 在同一 exclusive-board 事务中实现 source-key 唯一性、原子多文档建卡和恢复。
3. 实现 CLI import、超限拒绝、评论开关、canonical URL 和缺失契约提示。
4. 通过 pendingWork 接入 TUI `i`、请求身份、stale 丢弃、导入状态、task ID 跳转和远端更新提示。
5. 同步 CLI registry、AGENTS.md、三语 i18n、三版 README 和来源格式文档。
6. 完成事务故障注入、并发、注入内容、CLI/TUI 一致性和全量测试。

验证：board transaction、Issue import、TUI interaction、`go test ./...`。

发布/回滚：snapshot schema 从 v1 开始；回滚不得删除已导入卡片或来源附件。
