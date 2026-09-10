# 实施计划

1. 定义 repository identity、resolver 和结构化错误。
2. 实现安全、可替换的 `gh` command runner。
3. 实现显式仓库和 worktree 仓库解析、canonical 验证、非 worktree 安全目录及 Enterprise host 支持。
4. 接入 `issue repo`、CLI registry/tests、三语 i18n 和 doctor。
5. 同步 AGENTS.md 包图/子命令及三版 README。
6. 完成单元、CLI、POSIX/Windows 进程边界及全量测试。

验证：目标包测试、fake-`gh` CLI 测试、`go test ./...`。

发布/回滚：新增只读能力，不迁移数据；回滚移除命令绑定和 resolver，不改用户凭据。
