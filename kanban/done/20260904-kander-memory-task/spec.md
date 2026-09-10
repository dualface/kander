# Merge worktree memory

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor a3ce5e6a-f05f-4d95-a771-aeed082a89ea
- 窗口: herdr:wG:t1W:wG:p23
- 开始时间: 2026-09-04 11:08
- 完成时间: 2026-09-04 15:07
- 任务分支: 20260904-kander-memory-task
- 结果: completed

## 任务目标

实现 `internal/memory` 与 `kander merge-memory`, 对标 `merge-worktree-memory.py`: 集成后合并任务 worktree 的 memsearch 记忆, 清非法 UTF-8, 经 fs 层固定句柄与锁; Linux 成功后用 pidfd 停止来源 watcher 再合并最终写入.

## 用户决策

源无 `.memsearch/memory` 时为空操作且退出 0. 来源在合并期间仍被写入且无法证明稳定则失败并保留 worktree. 其他 POSIX 遇到存活 watcher 因无 pidfd 等价物而 fail-closed. Windows 不检查也不停止 watcher. `--dry-run` 只报告不终止.

## 预期成果

Git 清理步骤可调用 `kander merge-memory --source <worktree-path>` (项目安装用命令根绝对入口), 替代 Python 脚本.

## 验收条件

- [x] 固定来源/目标句柄, 拒绝 reparse, 目标记忆 DACL 迁移, `flock`/`LockFileEx`.
- [x] Linux: 读 `.memsearch/.watch.pid`, 核验用户、命令形态、词法路径; pidfd 固定后暂停复核, 独立进程组则终止并等待非 zombie 成员, 再稳定合并一次.
- [x] PID 文件缺失或进程已退出为幂等成功; 无法核验或停止则失败.
- [x] 测试对标 `tests/test-merge-worktree-memory.py`.

## 威胁模型

记忆文件可能含会话摘要. 合并必须在用户自己的树内完成, 拒绝跟随 junction 写到他处. 停止 watcher 必须核验 PID 属于当前用户且命令是固定启动形态, 防止杀错进程.

## 不在本轮范围

- 既有问题: 排除 MemSearch 插件安装 (welcome 卡).
- 并发/跨平台/安全加固: 纳入本卡的句柄、锁、pidfd 停进程.
- 共享契约与文档: `GIT-RULES.md` 已改为 `kander merge-memory`; 本卡不改 Git 集成流程其余部分.
- 相邻功能: 排除删 worktree/分支 (执行 Agent 按 GIT-RULES 在本命令成功之后做).

## 讨论与决策

```text
前置任务: 20260904-kander-cli-install-task
```

- fs 已是 cli-install 的祖先依赖, 不重复列出.
- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 任务分支 `20260904-kander-memory-task`, worktree `worktrees/20260904-kander-memory-task/`, 基于组分支当时头 `81fd336ff0f59fcfe3afa0feb14a123056e5bcf3`.
- `internal/memory` 实现 `kander merge-memory`: 稳定快照, 条目切分/哈希去重, 非法 UTF-8 只清新写入, 目标不整文件 rewrite; 经 `internal/fs` 固定句柄与 `flock`/`LockFileEx`.
- Linux 成功后读来源 `.memsearch/.watch.pid`, pidfd 固定身份, SIGSTOP 复核后再停独立进程组或该 PID, 停止后二次稳定合并. 其他 POSIX 遇存活 watcher fail-closed. Windows 不检查不停止 watcher. `--dry-run` 只报告.
- `cmd/kander` 以 side-effect import 覆写冻结命令表中的 `merge-memory` Runner, 未改命令名列表.
- 提交: `0e84bc312014e21adda5e3f32170ffc7c591b3ae`.
- 验证: 任务 worktree 执行 `go test ./...` 通过 (`internal/memory` 含切分黄金哈希, 去重/脏文件/锁/并发, Linux pidfd 停 watcher 与拒绝杀错进程; Windows DACL/junction 测试本机未跑).
- 无 `origin`, 跳过 push, 本地 `git merge --ff-only` 已把任务分支头 ff 进 `group/20260904-migrate-to-go-group`.

### 组级审核批次 6 finding

- 批次 commit: `0e84bc312014e21adda5e3f32170ffc7c591b3ae`; base: `81fd336ff0f59fcfe3afa0feb14a123056e5bcf3`.
- PM-1 / QA-1 medium [mechanical] unused Linux `signalAlive`: **成立** (同根因一次处理). `rg signalAlive internal/memory` 在 `0e84bc3` 上 Linux 文件只有定义无调用, 唯一调用在 `watcher_other.go` (`//go:build !linux`). 已删除 `watcher_linux.go` 中的 `signalAlive`; `!linux` 实现保留. 修复提交 `1ab761912ddbddabc5fbc04a134256f5689fc9ec` (rebase 到组头 `a36670a` 之后). 闭环证据: 修复后 `rg signalAlive internal/memory` 仅命中 `watcher_other.go`; `go test ./...` 通过.
- PM-2 recommend Python 套件未全移植: **成立, 本轮不修**. 生产路径已有; 属推荐档.
- PM-3 suggest POSIX 目标目录从 `/` walk: **超出契约 / 建议, 不修**. 验收要求经 fs 拒绝 reparse 与锁, 未要求 POSIX 必须从 worktree root 建目录; Windows 已从 pinned root 建.
- QA-2 suggest 内联 `joinAsRecords`: **成立为建议, 本轮不修**.

- rebase 后验证: `go test ./...` 通过. 无 `origin`, 跳过 push; 已 `git merge --ff-only` 进 `group/20260904-migrate-to-go-group`.

## 完成总结

- 交付: Git 清理可调用 `kander merge-memory --source <worktree-path>`; 源无 `.memsearch/memory` 时退出 0 且不建目录.
- 验收: 4/4; POSIX 自检通过. Windows 句柄/DACL/junction 有对应 fs 路径与测试代码, 本环境未执行.
- 验证: `go test ./...` 通过; 记忆用临时目录隔离, 未改用户真实看板或 `$HOME` 配置.
- 审核: 组级批次 6 首轮 + 增量; PM/QA 通过. CSA/Hacker N/A (仓库 AGENTS.md). 修复一轮: 删除 Linux 未使用 `signalAlive`. 最终 commit `1ab761912ddbddabc5fbc04a134256f5689fc9ec`. 未处理: PM-2 Python 测试对标未完; PM-3 POSIX 目标目录从 `/` walk; QA-2 `joinAsRecords` 建议内联.
- 收尾: 组分支已 ff 合回本地 `develop` `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e` (`merge-base --is-ancestor` 成立). `go run ./cmd/kander merge-memory --source <本卡 worktree>` 空操作退出 0. 已删本卡 worktree 与本地任务分支; 无 origin 未删远端. 未删组分支.
