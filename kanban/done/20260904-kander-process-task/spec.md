# Agent processes and task files

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor 3e750827-6f70-45dc-9afa-5325f52c20e1
- 窗口: herdr:wG:t1S:wG:p10
- 开始时间: 2026-09-04 10:50
- 完成时间: 2026-09-04 15:07
- 任务分支: 20260904-kander-process-task
- 结果: completed

## 任务目标

实现 `internal/process`, 对标 `~/works/onevoke/bin/onevoke_process.py`: 解析 Codex/Claude/Grok/Cursor 可执行文件, 创建 UTF-8 任务文件, 构建进程调用. 执行 Agent 与 Reviewer 都只把必需控制参数与短文件指令交给 CLI.

## 用户决策

任务文件不检查 POSIX 权限或 Windows ACL, 要求 Agent 完成后尝试删除, 遗留不影响结果. Windows 优先 `.exe`; 只有 `.cmd`/`.bat` 时用显式 `cmd.exe /d /s /v:off /c` 和环境变量承载已编码参数, 不用 shell 拼接. 不作为任意批处理脚本的通用契约.

## 预期成果

launch 卡与 review 卡共用同一套 Agent 解析与任务文件创建, 不再各写一套 argv 逻辑.

## 验收条件

- [ ] `resolve_agent_program` 按平台解析 `codex`/`claude`/`grok`/`cursor-agent`.
- [ ] `create_task_file` 写 UTF-8 临时文件, 内含完成后尝试删除的说明.
- [ ] `process_invocation` 产出可 `exec` 的 argv + env; Windows batch 参数编码对标 onevoke.
- [ ] 测试对标 `tests/test-onevoke-process.py`: 跨平台解析、临时任务文件、Windows batch 参数编码; 非 Windows 上 Windows 用例 skip.

## 威胁模型

任务文件可能含任务正文, 但不收紧 ACL (契约如此). Windows 禁用 `shell=True` 式拼接, 防止参数注入进 cmd. 不提升任务文件为安全边界.

## 不在本轮范围

- 既有问题: 排除各 Agent CLI 上游行为变化的适配, 除非本仓库测试需要的参数列表与 onevoke 当前实现不一致 (以 onevoke 源码为准).
- 并发/跨平台/安全加固: 纳入 Windows argv 编码; 排除审核隔离参数 (review 卡) 与 tmux/herdr 启动 (launch 卡).
- 共享契约与文档: 排除改 `rules/`.
- 相邻功能: 排除真正 spawn Agent、launcher、review runtime.

## 讨论与决策

```text
前置任务: 20260904-kander-config-task
```

- 与 cli-install 无文件冲突: 本卡只加 `internal/process`.
- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 仓库无 `origin`, 任务分支基于本地组分支 `group/20260904-migrate-to-go-group` (`6aeb911`) 创建 worktree `worktrees/20260904-kander-process-task`, 未同步远端.
- 新增 `internal/process`: `ResolveAgentProgram` (POSIX `LookPath`, Windows 先非空 `.exe` 再 `.cmd`/`.bat`), `CreateTaskFile`/`WriteTaskFile`/`TaskFileInstruction`, `NewProcessInvocation` (batch 用 `KANDER_CMD_*` 环境变量承载编码参数, `/c` 文本只有变量引用).
- 提交 `8c1934020e88466d07e91e0ce47f7a61fa736059` 实现 Agent 进程解析与任务文件 internal/process
- 验证: 任务 worktree 与组 worktree 均 `go test ./...` 通过 (`internal/process` `internal/config` `internal/fs`).
- 已 rebase 到组分支当时头 (无新提交, 无需重基), 本地 `git merge --ff-only` 进 `group/20260904-migrate-to-go-group`. 无 `origin`, 跳过 push.

### 上轮 finding (批次 4, base 6aeb911, commit 8c19340)

- PM-1 medium [mechanical] `TestWindowsSpecialCasesSkippedOffWindows` 空 skip: 成立. 读 `process_test.go` 该函数在 Windows 与非 Windows 均 `t.Skip`, 无断言; onevoke `test-onevoke-process.py` 无对应测试; PATH/batch 已由 `withWindows` 在本机运行. 真实 `cmd.exe` spawn 不在本卡范围. 已删除该函数.
- QA-1 medium [mechanical] 同根因: 成立, 与 PM-1 一次处理, 不另修.
- 修复提交 `8326131fabdadbdcb9fb8bfeedf4d303a8fe2c53`, rebase 到组头 `83f97dc` 后 `go test ./...` 通过, 本地 ff 进 `group/20260904-migrate-to-go-group`. 无 `origin`, 跳过 push.

## 完成总结

- 交付: `internal/process` 提供 Agent 解析, UTF-8 任务文件与可 exec 的 argv+env; launch/review 可共用, 本卡未接线 spawn.
- 验收: 4/4. POSIX 解析 `cursor-agent`; 任务文件含删除说明; batch 编码对标 onevoke; Windows PATH/batch 由 `withWindows` 注入覆盖. 真实 `cmd.exe` spawn 不在本卡, 未保留空 skip 测试.
- 验证: rebase 后任务 worktree `go test ./...` 通过; 组 worktree `go test ./internal/process` 通过. HEAD `8326131fabdadbdcb9fb8bfeedf4d303a8fe2c53`.
- 审核: 批次 4 首轮 + 增量; PM PASS, QA PASS; CSA/Hacker N/A (仓库 AGENTS.md). 修复 1 轮 (空 skip 测试, mechanical). 未处理项: 无.
- 收尾: 最终 commit `8326131fabdadbdcb9fb8bfeedf4d303a8fe2c53` 已是 `develop` `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e` 祖先. 组分支已 ff 合回本地 `develop`, 无 origin 未 push. merge-memory 空操作 (源无 `.memsearch/memory`). 已删本卡 worktree 与本地任务分支; 未删组分支.

