# 实施计划

1. 实现导入卡定位和完整契约审阅表单，显示不可变语言及重新导入入口。
2. 实现缺失项提示、创建者显式自审 attest/结论、受控 update 和冲突恢复。
3. 先校验和最终确认，再接入 todo 转换与 `launch.Start`；保留后台 launcher 限制。
4. 绑定异步 request/issue/task/revision identity 并处理取消、重复和失败。
5. 同步 AGENTS.md、三语 i18n、三版 README、帮助和交接恢复文档。
6. 完成交互、gate、竞态、启动及全量测试。

验证：TUI 状态机测试、board/launch gate 与 launcher 集成测试、PTY smoke、`go test ./...`。

发布/回滚：失败保留 backlog 卡和来源；回滚 UI 不删除任何已导入数据。
