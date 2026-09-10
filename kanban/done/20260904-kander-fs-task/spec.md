# Cross-platform safe file layer

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor 6457dd30-dcbc-4e6a-b66b-a54ce8091aec
- 窗口: herdr:wG:t1N:wG:p1W
- 开始时间: 2026-09-04 10:18
- 完成时间: 2026-09-04 15:08
- 任务分支: 20260904-kander-fs-task
- 结果: completed

## 任务目标

实现 `internal/fs`, 对标 `~/works/onevoke/bin/onevoke_fs.py`: POSIX no-follow/openat/`0600`/`0700`/`flock`; Windows 拒绝全部 reparse point, 用已校验句柄完成读写、原子替换与锁, 私有对象使用当前用户独占 DACL.

## 用户决策

按已确认计划实现文件安全底座. 行为与 onevoke 对齐, 不发明新语义. 后续写盘一律经本包, 不得回落普通路径 API.

## 预期成果

其他包可调用本层创建私有文件/目录、原子替换、独占锁、继承 ACL 的中性追加 (Git exclude), 并在不安全链接边界失败关闭.

## 验收条件

- [x] POSIX: no-follow 打开, `openat` 语义, 私有文件 `0600`, 私有目录 `0700`, `flock` 阻塞独占锁, `rename`/`os.replace` 等价原子替换.
- [x] Windows: 从卷/UNC anchor 逐分量拒绝符号链接、junction 及其他 reparse point; 私有对象创建瞬间即 protected DACL, 不得先发布继承 ACL 再收紧; `LockFileEx` 阻塞独占锁; 原子替换相对固定父句柄; Git exclude 类中性对象可继承父 ACL 但仍 no-follow 且同一叶句柄去重追加.
- [x] 任一安全后端失败显式报错, 不静默回落.
- [x] Go 测试覆盖成功路径与拒绝路径 (POSIX 在本机跑; Windows 专项在非 Windows 上 skip). 对标 `tests/test-windows-fs.py` 的边界分类: parent/leaf junction, 创建碰撞, 校验期写入/替换, 临时根 reparse.
- [x] 单文件不超过 1000 物理行; 超限则拆文件, 不把逻辑塞进调用方.

## 威胁模型

本机同用户环境下的符号链接、junction、目录夹持与 TOCTOU. 本层用于配置、看板迁移、审核 runtime、记忆合并等私有边界. 不声称抵御同用户恶意伪造; 目标是 fail-closed, 避免跟随不安全入口写入 HOME 或项目树.

## 不在本轮范围

- 既有问题: 排除 onevoke_fs.py 的历史实现细节是否可简化; 以行为契约为准, 不借机改语义.
- 并发/跨平台/安全加固: 纳入本卡的 no-follow/ACL/锁; 排除审核 runtime 根句柄租约的 Reviewer 集成 (review 卡), 排除记忆合并的 pidfd 停 watcher (memory 卡).
- 共享契约与文档: 排除. 公开函数名写入本包 Go doc; 不改 `rules/`.
- 相邻功能: 排除 config/board/review 调用方, 本卡只提供库.

## 讨论与决策

```text
前置任务: 20260904-kander-contract-task
```

- 包路径以契约卡 `AGENTS.md` 为准: `internal/fs`.
- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 任务分支 `20260904-kander-fs-task`, worktree `/home/dualf/works/kander/worktrees/20260904-kander-fs-task`.
- 基于组分支当时头创建后, 组分支前进到 `572842f`, 已 rebase 到组头并重跑验证.
- commit `7245b8661dadd3b7e11868a3de91f3dd60a67a56` 实现 `internal/fs`. 无 `origin`, 未 push, 未同步远端.
- 已 `git merge --ff-only` 进入 `group/20260904-migrate-to-go-group` (`7245b86`).
- 验证:
  - `go test ./...` (POSIX): PASS, 含成功路径, symlink 拒绝, 私有 0600/0700, flock 跨进程阻塞, 中性追加去重, 原子写入与 rename.
  - `GOOS=windows GOARCH=amd64 go test -c ./internal/fs`: 编译通过. Windows 专项 (parent/leaf junction, 创建碰撞, 校验期写入/替换, 临时根 reparse) 在本机 skip, 由 `windows_test.go` 在 Windows 上跑.
- 单文件均不超过 1000 行 (`windows_ops.go` 941).

审核批次 2 (base `572842f`, 原批次 commit `7245b86`) finding 核实与处理; 修复 commit `6aeb9112a556d2b89d5b0a752dd1cb8994d57e20`, 已 rebase 到组头 `128ccd3` 并 ff 进 `group/20260904-migrate-to-go-group`. 无 origin, 未 push.

- PM-1 / QA-1 (high/blocking) 成立: `unix.Renameat2` 仅 Linux. 已拆 `exclusivePublish`: Linux `Renameat2(RENAME_NOREPLACE)`, Darwin `RenameatxNp(RENAME_EXCL)`, 其余 `Linkat`+`Unlinkat`. `GOOS=darwin go test -c` 通过. 顺带把 Darwin `Stat_t.Mode` 收成 `uint32` 以免交叉编译失败.
- PM-2 / QA-3 (medium) 成立: 回滚曾 `Unlinkat(parent, filename)`. 现对新建目录 `openat` 后在其 fd 上 `Unlinkat(filename)`, 再对父 fd `AT_REMOVEDIR`. 回归测试 `TestCreateDirectoryWithTextFileRollbackKeepsSibling`.
- QA-2 (high) 成立: `EnsurePrivateDirectory` 的 `defer unix.Close(dirfd)` 关掉的是根 fd. 已改为与 `walkEnsure` 相同的循环关闭, 成功路径关闭叶 fd.
- PM-3 / QA-4 `samePath` 成立: 已删.
- PM-4 / QA-4 `walkEnsure chmodPrivate` 成立: 参数与死分支已删.
- QA-4 `requireDirectory` 成立: 已删.
- PM-5 成立: 已删仅 Skip 的 `TestWindowsSpecialCasesSkippedOffWindows`; Windows 专项仍在 `windows_test.go` (`//go:build windows`).
- QA-5 成立: `TempDir` 注释改为 POSIX unlink symlink 本体, Windows 遇 reparse 失败关闭.
- PM-6 / PM-7 / QA-6 非阻塞, 本轮不修.

复验: `go test ./...` PASS; Darwin/Windows `go test -c` 通过.

## 完成总结

- 交付: `internal/fs` 可供其他包创建私有文件/目录, 原子替换, 独占锁, Git exclude 式同一叶句柄去重追加; 不安全链接边界失败关闭, 不回落普通路径 API.
- 验收: 5/5 自检通过 (POSIX 本机实测; Windows 语义已实现并交叉编译, 专项测试在非 Windows skip).
- 验证: 见「实施与验证」. 环境缺口: 本机非 Windows, 未能实跑 junction/DACL 用例.
- 审核: 批次 2 首轮 + 增量; PM/QA 通过; CSA/Hacker N/A. 修复一轮 (Renameat2/Darwin、回滚 Unlinkat、dirfd 泄漏及死代码). 未处理项: PM-6 / PM-7 / QA-6 Windows 测试覆盖与导出文档 (NON-BLOCKING).
- 收尾: 最终 commit `6aeb9112a556d2b89d5b0a752dd1cb8994d57e20` 已是 develop `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e` 祖先. 组分支已 ff 合回本地 develop; 无 origin. 记忆合并空操作退出 0. 已删本卡 worktree 与本地任务分支; 未删组分支.

