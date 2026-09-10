# Always launch the target binary into the options screen after a successful install

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- CREATED_AT: 2026-09-07 01:13
- OWNER: cursor
- SESSION: cursor a6b871f7-e977-4686-beca-35193cedd41f
- WINDOW: herdr:wX:tH:wX:p11
- STARTED_AT: 2026-09-07 01:14
- FINISHED_AT: 2026-09-07 01:28
- TASK_BRANCH: 20260907-install-launch-options-task
- RESULT: completed

## GOAL

`kander install` 只要安装成功, 就应该启动安装目标二进制, 并让用户直接落在选项面板的「界面偏好」(options -> interface) 分区.

当前实现做不到两点:

1. `internal/install/wizard.go:174-180` 用 `sameFile(source, result.DestBinary)` 短路: 源和目标是同一个文件时 (装好后原地重跑 `kander install`) 直接 `return 0`, 不启动任何东西.
2. 即使发生了 handoff, 目标进程走的是无子命令 TUI (`internal/tui/cmd.go:34`), 只有 `configExists == false` 时才 `app.openOptionsAt(sectionInterface)` (`internal/tui/cmd.go:92-94`). 已有配置的重跑会直接打开看板, 到不了选项面板.

附带缺陷: `configExists == true` 时 `board.BoardRoot()` 失败即 `fail(err)` (`internal/tui/cmd.go:62-68`), 只有 `!configExists` 才容忍看板缺失. 在家目录里重跑 `install` 后启动, 会因为找不到 `kanban/` 直接报错退出, 连选项面板都到不了.

## USER_DECISIONS

- install 只要成功就启动目标二进制, 哪怕本次没有更新任何文件 (源与目标同一文件也要启动).
- 启动后直接进入 options -> interface 分区.

## EXPECTED_OUTCOME

- `kander install` 交互向导走完且 `Perform` 返回成功后, 一律 handoff 到 `result.DestBinary`, 不再有 `sameFile` 短路.
- handoff 给目标进程带上「本次由 install 启动」的标记, 目标进程据此: 跳过首次运行向导判定 (避免同文件 exec 自身时回到向导), 容忍 `kanban/` 缺失, 并无条件打开选项面板的 interface 分区.
- 新机器首装的既有行为不回退: 装完仍然是 doctor 生成配置后落在 interface 分区.
- 该标记不泄漏给选项面板后续拉起的子进程.

## ACCEPTANCE_CRITERIA

- [x] `internal/install/wizard.go` 的 `RunInteractive` 去掉 `sameFile(source, result.DestBinary)` 短路 (以及只为该判断存在的 `lookupExecutable` 调用), `Perform` 成功后无条件调用 handoff.
- [x] `internal/install` 新增导出常量 `EnvPostInstall = "KANDER_POST_INSTALL"`, `handoff_unix.go` 与 `handoff_windows.go` 都在传给目标进程的环境里置位该变量.
- [x] `internal/tui` 的 `Run` 在入口读取该变量并立即 `os.Unsetenv`, 得到 `postInstall` 布尔值; 变量不再传给 TUI 之后拉起的任何子进程.
- [x] `postInstall` 为真时: 不调用 `install.ShouldRunWizard` (不会重新进入向导), `board.BoardRoot()` 失败且属于 `board.IsBoardNotFound` 时按 `emptyBoard` 处理而不是 `fail`, 并且无条件 `app.openOptionsAt(sectionInterface)`.
- [x] `postInstall` 为假时现有行为不变: 首次运行仍进向导, `!configExists` 仍先跑 doctor 再打开 interface 分区, 已有配置仍直接打开看板.
- [x] `internal/install` 的 handoff 改为包级变量以便测试注入, 新增用例断言: `Perform` 成功即调用 handoff (含源与目标为同一文件的情形), 且传给目标进程的环境包含 `KANDER_POST_INSTALL`.
- [x] `internal/tui` 新增用例断言: 置位该变量时打开 interface 分区, 且没有 `kanban/` 目录也不返回失败; 用例结束时环境变量已被清除.
- [x] 模块根 `go test ./...` 通过, 实际命令与结果记入 `IMPLEMENTATION`.
- [x] 新增代码注释与提交备注全部英文 (`AGENTS.md`「语言约定」).

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题: `Perform` 自身的释出、改名、legacy 清理逻辑不动 — 本卡只改「成功之后做什么」, 安装动作本身没有问题报告.
- 加固: 不改 handoff 的进程模型 (Unix 仍 `unix.Exec` 替换自身, Windows 仍起子进程并透传退出码) — 现状已满足需求, 改动会放大风险面.
- 共享契约与文档: 不新增 CLI 全局选项或子命令, 用环境变量传递一次性启动意图, 因此 `rules/`、`README` 与 `AGENTS.md` 的命令契约无需改写. 若实现中确实新增了用户可见字符串, 仍须走 `internal/i18n` 双语资源.
- 相邻功能: 选项面板各分区的字段、保存语义和 doctor 的检查项都不改 — 本卡只决定「打开哪个分区」, 不碰面板内部行为.
- 非交互场景不纳入: `RunInteractive` 前置的 `requireInteractive` 保持不变, 无 TTY 时仍然直接失败.

## DISCUSSION

- Unix 上取消 `sameFile` 短路后会出现 exec 自身的情况. 防循环有两道: 一是 `EnvPostInstall` 标记让目标进程跳过 `ShouldRunWizard`; 二是既有的 `alreadyInstalled()` 判据 (目标路径即安装路径) 本身就返回 false. 两道都保留.
- Windows 是子进程模式, install 进程会挂到 TUI 退出为止. 这是现状行为, 本卡不改.
- 选择环境变量而不是 CLI 标志: 这是一次性的启动意图, 不属于用户可调用的命令契约, 不该出现在 `usage` 里; 代价是必须在读取后立刻 `os.Unsetenv`, 否则会被 TUI 拉起的 Agent 子进程继承.

SELF_REVIEW: 通过. 对照用户两句要求逐条核对: 「install 成功哪怕没更新文件也启动」对应第 1 条验收, 「启动后进入 options -> interface」对应第 4 条验收. 自审中修正两处: (a) 初稿把「附带的 BoardRoot 缺失即失败」只写在 GOAL 里, 会导致在家目录重跑 install 后启动直接报错、验收第 4 条无法达成, 已补为独立验收条件; (b) 初稿未约束环境变量的清除, 会泄漏给选项面板拉起的 Agent 子进程, 已补入预期成果与验收条件. 排除项四类均逐条给出理由, 且没有把达成目标必需的工作排除在外. 无需用户新增决策的歧义.

## IMPLEMENTATION

- Worktree: `/home/dualf/works/kander/worktrees/20260907-install-launch-options-task` (removed after integrate)
- Branch: `20260907-install-launch-options-task` (deleted after integrate)
- Base: `77d52105533e52015c7fab5967d77875c583c5d7`
- Final commit: `a06268d2473fddc0894c3d85552314546b74e50a`
- Changes:
  - `internal/install`: `EnvPostInstall`, injectable `handoff(dest, argv, env)`, unconditional `finishSuccessfulInstall` after `Perform`.
  - `internal/tui`: `Run` reads/clears marker, skips wizard, tolerates missing board, opens interface options when post-install.
  - Tests: `TestFinishSuccessfulInstallAlwaysHandoffs`, `TestPostInstallOpensInterfaceWithoutBoard`.
- Verify: `go test ./... -count=1` → all packages ok (exit 0).
- Review: PM PASS (codex), QA PASS (codex); CSA/Hacker N/A.
- Integrate: pushed to `origin/develop`, local `develop` ff-only synced; worktree/branch cleaned.


## SUMMARY

- Delivered unconditional post-install handoff with `KANDER_POST_INSTALL`, TUI consume/clear + interface options + missing-board tolerance.
- Final commit: `a06268d2473fddc0894c3d85552314546b74e50a` on `develop`.
- Reviews: PM PASS, QA PASS; CSA/Hacker N/A (repo AGENTS.md).
- Verified with `go test ./... -count=1` (exit 0). No known follow-ups.
