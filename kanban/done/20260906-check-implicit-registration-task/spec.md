# check subcommand binding depends on init order, risking silent degradation

- 类型: Bug
- SIZE: small
- 任务组:
- 创建时间: 2026-09-06 23:30
- 负责人: cursor
- 会话: cursor 5eba8c16-7384-4e79-9478-f60fb9c2099c
- 窗口: herdr:wX:tG:wX:p0
- 开始时间: 2026-09-06 23:28
- 完成时间: 2026-09-06 23:45
- 任务分支: check-implicit-registration
- 结果: completed

## 任务目标

`check` 子命令在两处被注册到同一个 map key:

- `internal/cli/wire_board.go:12` — `Commands["check"] = board.RunCheck`
- `internal/liveness/bind.go:6` — `cli.Commands["check"] = RunCheck`

`internal/liveness` 反向导入 `internal/cli`, 因此它的 `init()` 晚于 `cli` 自身的 `wire_board.go` 执行, `liveness.RunCheck` 覆盖 `board.RunCheck` 胜出.

**当前行为是正确的**, 不是功能错误: `board/cmd.go:229` 的注释即写明该版本 "without liveness probing", 而 `liveness.RunCheck` 是它的超集 — 同样解析 `--all`、同样调用 `board.CheckBoard`、同样输出 stderr, 之后额外追加 `livenessLines` 的存活信息. 二进制里 `check` 应当带存活探测, 覆盖结果符合预期.

问题在于这个正确结果**只由包初始化顺序隐式保证**, 没有任何显式声明或测试守护:

- `cmd/kander/liveness.go` 只是一行 blank import. 一旦它被删除、或 `liveness` 包被重构成不再导入 `cli`, `check` 会静默退化为不带存活探测的版本.
- 退化不会产生编译错误, 现有测试也不覆盖「完整二进制里 check 必须带存活探测」这一不变量.
- 同一 key 被写两次本身也是可读性陷阱: 只看 `wire_board.go` 会得出错误结论.

本卡消除这个隐式依赖, 不改变 `check` 的对外行为.

## 用户决策

- 用户明确要求本问题从「自安装二进制」卡中拆出, 单独建卡处理.

## 预期成果

- `check` 的最终绑定是显式且可读的, 不再依赖两个 `init()` 的相对执行顺序.
- 存在回归测试保证完整二进制中 `check` 带存活探测, 该不变量被破坏时测试失败而非静默退化.
- `check` 的命令行行为、输出与退出码相对本卡改动前保持不变.

## 验收条件

- [x] `internal/cli` 与 `internal/liveness` 不再对 `Commands["check"]` 存在两处无声互相覆盖的写入; 最终绑定在代码中显式可读.
- [x] `board.RunCheck` **保留**, 不删除 — `internal/board/board_test.go` 有 16 处调用它作为不带存活探测的基础实现.
- [x] 新增回归测试: 在链接了 `internal/liveness` 的完整命令注册下, `check` 解析到带存活探测的实现; 若退化为 `board.RunCheck` 则测试失败.
- [x] `kander check`、`kander check --all`、`kander check <task-id>` 的输出与退出码相对改动前不变 (含未知参数返回 2 的用法错误路径).
- [x] `go build ./... && go test ./... && go vet ./...` 全绿, `gofmt -l .` 无输出, `git diff --check HEAD` 无告警.
- [x] 遵守仓库「语言约定」(`AGENTS.md`, 提交 f56eacb): 新增代码注释与提交备注全部英文.

## 威胁模型

N/A — 本卡不涉及信任边界、权限或外部输入, 只调整进程内的命令注册方式.

## 不在本轮范围

- **既有问题 — `check` 的功能行为本身**: 存活分类、`livenessLines` 的判定逻辑与输出格式都不在本轮调整. 排除理由: 本卡只解决绑定方式的脆弱性, 混入行为变更会让「行为不变」这条验收条件无法判定.
- **加固 — 其他子命令的注册方式**: `start`/`resume`/`review`/`notify`/`dismiss`/`subscribe` 同样走反向导入覆盖 `Unimplemented` 占位, 但那是**占位被实现覆盖**的既定模式, 不存在两个真实实现互相覆盖的情况. 排除理由: 与本缺陷性质不同, 全面重构注册机制超出本卡目标, 需要独立设计.
- **共享契约与文档 — `internal/cli/cli.go` 的命令表冻结约定**: 本卡不新增或删除命令名, 不触碰 `commandNames`. 排除理由: 无需改动即可达成目标.
- **相邻功能 — 并行进行的「自安装二进制」卡 (`20260906-self-install-binary-task`)**: 该卡会向 `commandNames` 新增 `install` 并在 `cli.Run` 插入首次运行钩子. 排除理由: 两卡目标不同; 但二者都触及 `internal/cli`, 集成时需注意先后顺序与潜在冲突, 已在讨论中记录.

## 讨论与决策

- 该问题是在规划「自安装二进制」卡时顺带发现的, 与安装无因果关系, 故未并入那张卡, 按用户要求单独处理.
- 已核实的事实: `liveness.RunCheck` 与 `board.RunCheck` 都调用同一个 `board.CheckBoard(root, tasks, all)`, 前者在其后追加 `livenessLines`. 两者的 `--all` 解析和未知参数处理等价, 差异只在用法错误的输出通道 (`board` 走 `usageFail`, `liveness` 走 `usageCheck` + 显式 `return 2`), 因此「行为不变」这条验收需要覆盖用法错误路径.
- 修法倾向: 移除 `wire_board.go` 中的 `check` 注册, 让 `liveness` 成为唯一注册点并加注释说明; `board.RunCheck` 作为不带探测的基础实现保留供 board 包测试使用. 另一种更保守的做法是保留双写但补注释与回归测试. 两者都能满足验收, 由执行者按代码实际情况选择, 不需要新增用户决策.
- 与「自安装二进制」卡的协调: 那张卡改 `cli.go` 的 `commandNames` 与 `Run`, 本卡改 `wire_board.go` 与 `liveness/bind.go`, 文件不重叠, 但同属 `internal/cli` 附近, 集成时按 Git 规则各自 rebase 到最新 `develop` 并重跑验证.

自审: 通过. 核对项与修正如下 — (1) 目标与成果一致: 卡片只承诺消除隐式绑定并加测试守护, 不承诺改变 check 行为, 与用户「单独建卡处理」的要求相符; (2) 未把建议写成用户决策 — 用户决策一节只记录了「拆出单独建卡」这一条真实指令, 修法倾向明确放在讨论与决策中标注为倾向而非定论; (3) 边界方面修正一处初始误判 — 起初拟将 `board.RunCheck` 判为死代码并删除, 核查后发现 board 包测试有 16 处调用, 已改为明确要求保留, 避免执行者据错误前提删函数; (4) 验收条件覆盖目标与成果且可判定, 其中「行为不变」一条按核查结果补充了用法错误路径, 因为两个实现在该路径上的输出通道确有差异, 不补则该条无法严格判定; (5) 如实记录了与并行大卡在 `internal/cli` 上的集成协调点. 无需新增用户决策的歧义项.

## 实施与验证

- 工作目录: /home/dualf/works/kander/worktrees/check-implicit-registration
- 任务分支: check-implicit-registration (from origin/develop @ fff916f)
- 修法: 移除 wire_board.go 对 Commands["check"] 的注册; liveness/bind.go 为唯一注册点并加注释; board.RunCheck 保留供 board 包测试
- cli_test: check 不再列入 cli 包内已实现集合 (未链接 liveness 时保持 Unimplemented 占位)
- 回归测试: internal/liveness/bind_test.go 断言 cli.Commands["check"] 绑定 RunCheck 而非 board.RunCheck

- Commits:
  - 8eabf33 fix(menu): align Windows herdr doctor tests with enabled herdr (base develop was red after fff916f)
  - 920ca15 fix(cli): register check only through the liveness package
- Verification: gofmt clean; go build ./...; go vet ./...; go test ./...; git diff --check HEAD — all green
- Push: origin/check-implicit-registration

- Review fix: moved regression test to cmd/kander/check_bind_test.go (no direct liveness import); asserts != board.RunCheck and emits 存活 lines via production package graph. Removed package-local bind_test.go.
- PM-001/QA-001: package-liveness-only test insufficient for full graph — 成立, fixed. Claim that deleting only cmd/kander/liveness.go unlinks liveness — 不成立 (notify/takeover import liveness); verified by deleting the blank-import file; binding remained.



## 完成总结

- 移除 `wire_board.go` 对 `Commands["check"]` 的注册; `liveness/bind.go` 为唯一注册点并注释说明; `board.RunCheck` 保留。
- 回归测试 `cmd/kander/check_bind_test.go` 在完整二进制包图下断言非 `board.RunCheck` 且输出含存活段。
- 附带修复 develop 上 `fff916f` 遗留的 Windows herdr doctor 测试期望 (否则 `go test ./...` 无法全绿)。
- 最终交付: `0dd26f8fb080ee7d4ac350bac032919155921288` 已进 `origin/develop`; 任务分支与 worktree 已清理。
- 审核: PM/QA 通过 (codex); CSA/Hacker N/A (本仓库特例)。未处理: PM-002 low 已顺手修掉 (用 NewTask 返回路径派生 id)。
- 验收: 六条全部自检通过。

