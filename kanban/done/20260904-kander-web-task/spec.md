# Read-only web board

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor ccabbcad-d324-44ef-b727-a823708cf1d8
- 窗口: herdr:wG:t1Z:wG:p26
- 开始时间: 2026-09-04 11:25
- 完成时间: 2026-09-04 15:07
- 任务分支: 20260904-kander-web-task
- 结果: completed

## 任务目标

实现 `internal/web` 与 `kander web`, 对标 `kanban_web.py`: 标准库 HTTP, `share/kanban-web/` 资源, SSE 仅在内容变化时推送. 从 `~/works/onevoke/share/kanban-web/` 拷入资源, 用户可见文案中的命令名改为 `kander`.

## 用户决策

默认 `127.0.0.1:8080`, 服务端默认 60s 扫描. `--host`/`--port`/`--refresh`/`--assets`/`--open` 语义不变. 资源来自当前作用域 `share/kanban-web/`, 项目模式不回落全局. 只读, 不提供创建/迁移/启动 Agent. 扫描忽略无效入口, 不向终端注入 check 警告.

## 预期成果

浏览器可看栏目与卡片详情, 任务 ID 原位更新. Windows 第一阶段保证的看板 UI 仍由本命令承担.

## 验收条件

- [x] 扫描/排序/搜索字段与 onevoke web payload 一致, 供 tui 卡复用同一 `board.scan` (不经 `load_board` 打警告).
- [x] SSE 仅内容变化时推送; 客户端按任务 ID 原位更新.
- [x] 测试对标 `tests/test-windows-web.py` 的 UTF-8 HTTP 端到端; POSIX 有等价 HTTP 测试.
- [x] `share/kanban-web/` 的 html/css/js 已迁入; 文案中的 `kanban` 命令改为 `kander`, 产品名 Kander.

## 威胁模型

只绑定默认回环地址. 不鉴权; `--host` 绑非回环时的风险与 onevoke 相同, 不在本卡新做鉴权. 资源目录经作用域解析, 项目模式不读全局 share.

## 不在本轮范围

- 既有问题: 排除看板写操作 UI.
- 并发/跨平台/安全加固: 纳入只读扫描的 fs 边界; 排除 TUI.
- 共享契约与文档: 不改状态模型.
- 相邻功能: 排除 `kander tui` (tui 卡). 共享 scan API 放在 board 包, 本卡不得把 scan 做成 web 私有而逼 tui 再扫一套不兼容字段.

## 讨论与决策

```text
前置任务: 20260904-kander-board-task
```

- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 任务分支 `20260904-kander-web-task`, worktree `worktrees/20260904-kander-web-task/`.
- `internal/board.BoardPayload` / `TaskPayload` 走 `Scan` 而非 `LoadBoard`, 字段与 onevoke web payload 对齐, 供 tui 复用.
- `internal/web` 标准库 HTTP: `/` `/api/board` `/api/events` `/api/tasks/{id}` `/static/`; SSE 指纹排除 `generated_at`, 仅内容变化推送.
- 默认 `127.0.0.1:8080`, `--refresh` 默认 60; 项目模式只读 `.kander/share/kanban-web`, 全局用 `KANDER_SHARE` / `~/.local/share/kander/kanban-web`, 不回落.
- `share/kanban-web/` 已与 onevoke 资源一致; 错误文案命令名为 `kander`, 产品名 Kander; Windows 缺资源提示 `install.ps1`.
- 最终 commit: `18e363cc4ac891c579b1c70e41673f9c184ba1fd` (已 ff 进 `group/20260904-migrate-to-go-group`).
- 验证: 在任务 worktree 对 rebase 后的 HEAD 运行 `go test ./...`, 通过 (含 POSIX HTTP/SSE/UTF-8 与 payload 无警告扫描). 仓库无 `origin`, 任务分支未 push.

## 完成总结

- 交付: `kander web` 只读看板, 浏览器可看栏目与卡片详情, 任务 ID 原位更新; 扫描 API 在 `internal/board`.
- 验收: 4/4; 扫描字段、SSE 变化推送、POSIX HTTP 端到端、资源与 kander 文案均已自检.
- 验证: `go test ./...` 在 `18e363cc4ac891c579b1c70e41673f9c184ba1fd` 通过. Windows 专项与 POSIX 共用同一 HTTP 测试, 缺省资源恢复文案按 `GOOS` 分支断言.
- 审核: 批次 9 首轮; PM/QA 通过无闸口; CSA/Hacker N/A (仓库 AGENTS.md); 无修复轮次; 归属本卡未处理项无.
- 收尾: 最终 commit `18e363cc4ac891c579b1c70e41673f9c184ba1fd` 已是 `develop` (`bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e`) 祖先; 组分支已 ff 合回本地 `develop` (无 origin 未 push). 记忆合并空操作; 已删本卡 worktree 与本地任务分支; 未删组分支.
