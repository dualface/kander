# Prefix tmux pane commands with exec to fix foreground process-name mismatch

- TYPE: Bug
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 01:31
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t19:wX:p1V
- STARTED_AT: 2026-09-08 10:12
- FINISHED_AT: 2026-09-08 11:24
- TASK_BRANCH: tmux-pane-exec-prefix
- RESULT: completed

## GOAL

修复 GitHub issue #1 的第一个问题:`tmux` / `tmux-session` launcher 启动 agent 时,tmux 在需要 shell 的情况下用 `<default-shell> -c <command>` 包裹 pane 命令,而 dash 等 shell 不做尾调用 exec 优化,于是 `pane_current_command` 是包裹 shell(`sh`/`zsh`)而不是 agent 可执行名。

现有 5 处校验都拿 `#{pane_current_command}` 与 `filepath.Base(config.AgentExecutableName(agent))` 做严格相等比较,失配后果:

- `internal/launch/tmux.go:305` `tmuxNotifyTarget`:`start` 后存活校验失败,回滚整次启动
- `internal/notify/probe.go:147` `TmuxNotifyTarget`、`:184` `TmuxNotifyProbe`:直投判 stale,每次通知都降级走 recover,重建 agent 会话
- `internal/liveness/classify.go:165` `classifyTmux`:活着的 pane 被判 stale,`kander check` 误报
- `internal/liveness/lookup.go:40` 反查、`internal/takeover/ops.go:222` dismiss/takeover:同一假设

做法:在 tmux 启动 pane 的唯一入口给命令加 `exec ` 前缀,让包裹 shell 自我替换,使 `pane_pid` 与前台进程都是 agent 本身,与 5 处校验的既有契约重新一致。

## USER_DECISIONS

- 采纳 issue #1 的修复方向(pane 命令加 `exec ` 前缀),按看板流程单独开卡处理。
- 与 issue #1 的第二个诉求(agent 可执行名可配置)分成两张独立的卡,互不阻塞。

## EXPECTED_OUTCOME

- 在包裹 shell 不做 exec 优化的环境(如 `/bin/sh -> dash`,或命令串含元字符导致 tmux 回落到 shell)下,`tmux` / `tmux-session` 启动的 pane,其 `pane_current_command` 等于 agent 可执行名。
- `kander start` 存活校验、`kander notify` 直投、`kander check` 存活分类在该环境下不再因前台进程名失配而失败或降级。
- Windows / PowerShell 形式的 pane 命令不受影响(PowerShell 无 `exec`)。
- herdr launcher 的 pane 命令不受影响(其命令送进 pane 内的交互 shell,加 `exec` 会顶掉该 shell,语义变化)。

## ACCEPTANCE_CRITERIA

- [ ] POSIX 下 `tmux` / `tmux-session` 启动时,传给 `tmux respawn-pane` 的命令串以 `exec ` 开头,后接原有 `posixJoin(argv)` 结果,原有引号转义不变。
- [ ] Windows(`runtimeWindows()` 为真,PowerShell 形式)不加 `exec ` 前缀。
- [ ] herdr launcher 传给 `herdr pane run` 的命令串不加 `exec ` 前缀,与现有 `internal/launch/herdr_pane_command_test.go` 的约定一致。
- [ ] 新增单元测试:断言 tmux 启动路径的实参带 `exec ` 前缀,herdr 与 Windows 路径不带;现有 launch / notify / liveness 相关测试全绿。
- [ ] 代码注释说明为什么加 `exec`(tmux 用 default-shell `-c` 包裹,dash 无 exec 优化,前台名会变成 shell),避免后续被当成冗余删掉。
- [ ] `go build ./...`、`go vet ./...`、`go test ./...`、`gofmt` 无输出;`kander check` 通过。
- [ ] 在本机(tmux 3.6)用 `tmux` 或 `tmux-session` 实际起一张卡,`kander check` 报告 alive,`kander notify` 走直投而非 recover;记录验证命令与输出到 IMPLEMENTATION。

## THREAT_MODEL

N/A(不涉及新的信任边界;pane 命令仍走既有的控制字符拒绝检查,前台进程名校验本来就不是安全边界)。

## OUT_OF_SCOPE

- 既有问题:不改 `#{pane_current_command}` 这一判据本身(例如改成看 `pane_pid` 子进程树、或把 `@kander_session` 标记升为主判据)。理由:`exec` 是最小且对症的修复,换判据会动 notify/liveness/takeover 的共同契约,风险与范围都超出本卡。
- 加固:不给 5 处校验加兜底或宽松匹配。理由:加了 `exec` 后前台名是确定的,宽松匹配会掩盖真实的启动失败。
- 共享契约与文档:不改 `AgentExecutables` 映射,也不新增配置项。理由:那是 issue #1 第二个诉求,已单独开卡(`20260908-agent-executable-override-task`),两卡互不依赖。
- 相邻功能:不动 herdr 与 foreground/console 的启动路径,不改 `paneCommand` 对 Windows 的 PowerShell 编码。理由:本卡只解决 tmux 包裹 shell 这一个失配来源。

## DISCUSSION

- 现场核对(HEAD `251f5d8`):命令生成在 `internal/launch/tmux.go:367` `paneCommand`(POSIX 返回 `posixJoin(argv)`,不带 `exec`);启动在 `internal/launch/tmux.go:245` `tmuxStartPane`(`respawn-pane -k -t <pane> <command>`),全仓唯一调用点是 `internal/launch/agent.go:100`。改在 `tmuxStartPane` 即可覆盖全部 tmux 启动,且天然不波及 herdr。
- 本机实测(tmux 3.6,`/bin/sh -> dash`):
  - `dash -c '/usr/bin/sleep 30'` 前台是 `dash`;`bash -c` 同命令前台是 `sleep`——dash 确实不做尾调用 exec 优化。
  - tmux 3.6 对简单命令行会自己拆分并直接 execvp,不经 shell;命令串含元字符(如 `&& true`)时才回落到 `<default-shell> -c`,此时 `pane_current_command=zsh`,`pane_pid` 为 `zsh -c ...`,真实进程是其子进程。
  - 同一条命令加 `exec ` 前缀后 `pane_current_command=sleep`,`pane_pid` 即目标进程;`default-shell` 为 `/bin/sh` 与 `/bin/bash`、简单与含元字符两种命令下均正常,未见回归。
- 因此触发条件是「包裹 shell 不做 exec 优化」,不限于 Ubuntu 的 dash;报告人 tmux 3.2a 自行拆分的优化更弱,更容易必现。issue 的 repro 里给 `exec` 版本却注释为 `-> sh`,与其「打补丁后全通过」自相矛盾,应属笔误,不按该注释设计。
- issue 建议把判断写在 `launchAgent` 并按 `plan.Launcher` 分支,本卡改在 `tmuxStartPane`:少一处 launcher 字符串比较,且必须补 issue 片段漏掉的 Windows 判据(`paneCommand` 在 `runtimeWindows()` 时返回 PowerShell 形式,没有 `exec`)。
- SELF_REVIEW: 目标与产出一致,只覆盖用户确认的 `exec` 前缀方向;边界把「换判据」「配置化可执行名」明确排除且不影响达成目标;验收项可判定(实参断言、平台分支、构建测试、真机 tmux 验证),未把建议写成用户决策。自查未发现需用户新决策的歧义。

## IMPLEMENTATION

- 2026-09-08：工作树 `/home/dualf/works/kander/worktrees/tmux-pane-exec-prefix`，任务分支 `tmux-pane-exec-prefix`，源分支 develop，审核 base `26edb64fcfb654a96afedc30bbaea27e5e918ec7`。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec` 已推送 origin/tmux-pane-exec-prefix。
- 实现：仅在 `tmuxStartPane` 的 POSIX 分支给原命令加 `exec `；注释说明 default-shell -c 和 dash 不优化导致前台名失配。新增实参捕获测试覆盖 tmux/tmux-session × POSIX/Windows，包含空参数、单引号、元字符和中文；herdr 原有测试改为精确比较原命令；fake tmux 识别 exec 后的真实程序。
- Delivery Self-Check（以下为首轮审核交付提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec` 的历史验证）：
  1. `git diff --check HEAD^ HEAD`：退出 0，无输出；未发现冲突标记、尾随空格或 EOF 漂移。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
  2. `wc -l internal/launch/{tmux.go,launch_test.go,herdr_pane_command_test.go,tmux_pane_command_test.go}`：442、989、230、78，全部不超过 1000 行。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
  3. 逐项对照 diff：新增注释准确解释 tmux 的包装 shell 和 exec 必要性；未改变共享 paneCommand、herdr 或 Windows 编码，不涉及文档/API/包边界变更。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
  4. `rg -n 'tmuxStartPane|TestLaunchAgentSendsShellSpecificCommandToTmux' internal/launch` 并逐行检查 diff：唯一既有启动入口仍被 launchAgent 调用；新增测试均被 Go 测试发现，无死代码。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
  5. 检查新增 4 个子用例与原 herdr 用例：分别覆盖 launcher 与平台分支，herdr 强化既有断言，无复制测试；fake tmux 仅适配新实参。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
  6. `go build ./...`、`go vet ./...`：退出 0，无输出。`go test -json ./...`：退出 0，19 个包通过，1035 个测试/子测试通过，1 个跳过；含 launch、notify、liveness。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
  7. `gofmt -l internal/launch/tmux.go internal/launch/launch_test.go internal/launch/herdr_pane_command_test.go internal/launch/tmux_pane_command_test.go`：退出 0，无输出。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
- 本机真机验收（提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`）：`go build -o /tmp/kander-tmux-exec ./cmd/kander`；使用 `/tmp/kander-tmux-smoke-pob9dizy` 独立临时看板、临时配置和独立 tmux server，未修改用户看板或配置。tmux 3.6，`default-shell /bin/sh`（dash），实际 Codex CLI。
  - `/tmp/kander-tmux-exec start --agent codex --launcher tmux-session 20260908-exec-smoke-task`：started，session=kb-kander-tmux-smoke-pob9dizy-7bc66389，window=@2。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
  - `tmux list-panes -a -F '#{pane_id} #{pane_pid} #{pane_current_command} #{pane_start_command}'`：%2 / 895760 / codex，pane_start_command 以 exec 开头。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
  - `/tmp/kander-tmux-exec check 20260908-exec-smoke-task`：status=alive，ok: 1 tasks。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
  - `/tmp/kander-tmux-exec notify 20260908-exec-smoke-task --message '本次为 tmux exec 前缀真机直投验证。请仅回复 smoke notified，保持会话等待退出。'`：channel=tmux-direct，acknowledgement=confirmed。随后 check 仍 alive；pane %2、PID 895760 不变，没有 recover 或会话重建。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
  - 首两次启动被 Codex 自建临时目录信任提示阻塞，实际错误 `The newly started Codex session has not appeared yet`，工具完成回滚。确认该临时目录后第三次启动成功；此前失败未记为通过。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
- 本任务 `kander check 20260908-tmux-pane-exec-prefix-task`：通过 1 个任务，herdr alive。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
- 验收 7/7 自查完成。PM 原件 `reviews/tmux-exec-pm/`、QA 原件 `reviews/tmux-exec-qa/`：两角色均无 FINDINGS/NON_BLOCKING；独立核验后标记 PASS。CSA/Hacker 依仓库 AGENTS.md 标记 N/A。批次 `tmux-exec-batch` 已关闭，`kander review progress` 返回 closed。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
- 全板 `kander check`：通过 2 个任务，两张 working 卡均 alive。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
- 真机原始命令输出和限制见 [验证证据](evidence/tmux-smoke.md)。本机 Codex 的通知正文停留输入框，需补 Enter 后才回复 smoke notified；退出命令同样需补 Enter，Agent 与 pane 实际退出，但 dismiss 返回探测格式错误。上述相邻协议没有在本提交修改，仅记录观察，不把自动正文处理或 dismiss 命令记为通过。独立 server 已关闭，自建临时目录信任条目已精确移除，无遗留验证进程。提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`。
- 2026-09-08 合入收尾：用户回复“要”，明确授权合入 develop 并完成看板收尾。已 fetch，并无冲突 rebase 到最新 develop `78c8137fba6d8a6244c2f0eb6ce84a5ccf548851`；交付提交变为 `e556a1af452ba74db915e6792d44cd0b483a898e`。`git diff b4f634edeb46daa695b73fbe433ce034c7d096ec HEAD -- internal/launch` 无输出，审核覆盖代码逐字不变，按一次审核门禁规则保留既有 PM/QA 结论；集成验证已完成：提交 `e556a1af452ba74db915e6792d44cd0b483a898e` 上 `go build ./...`、`go vet ./...`、`git diff --check HEAD^ HEAD`、变更文件 `gofmt -l` 均退出 0、无输出；`go test -json ./...` 退出 0，20 包通过、1076 项测试/子测试通过，1 项 Windows console 跳过。原真机证据保留原 SHA；`git diff b4f634edeb46daa695b73fbe433ce034c7d096ec HEAD -- internal/launch` 无输出，确认最终交付的已验实现逐字不变。

- 最终交付与清理（提交 `e556a1af452ba74db915e6792d44cd0b483a898e`）：任务分支使用限定旧 SHA 的 force-with-lease 完成 rebase 更新；随后普通 push 至 origin/develop 成功。主工作树 `git merge --ff-only origin/develop` 成功；`git merge-base --is-ancestor e556a1af452ba74db915e6792d44cd0b483a898e develop` 与对应 origin/develop 核验均退出 0。任务工作树无未提交内容，本地/远端 tmux-pane-exec-prefix 分支及其工作树已删除，其他工作树保留。审核原件与真机证据仍归档在本卡；启动任务文件已删除。

## SUMMARY

- Task: 20260908-tmux-pane-exec-prefix-task，tmux pane 命令加 exec 前缀修复前台进程名失配。
- Delivery: POSIX tmux/tmux-session 在唯一启动入口增加 exec 前缀；Windows/herdr 保持既有命令编码。
- Acceptance: 7/7。实参、平台分支、注释、测试及真实 tmux alive/tmux-direct 均已验证；最终 `e556a1af452ba74db915e6792d44cd0b483a898e` 的 launch 文件与真机验证版本逐字一致。
- Verification: 最终 `e556a1af452ba74db915e6792d44cd0b483a898e`：go build ./...、go vet ./...、gofmt 和 diff 检查通过；go test -json ./...：20 包、1076 项通过，1 项 Windows console 跳过。真机验证原件与限制见 evidence/tmux-smoke.md；审核前与收尾 kander check 均通过。
- Review: Codex PM/QA PASS，无门禁项或非阻塞项；run ID 为 tmux-exec-pm、tmux-exec-qa；CSA/Hacker N/A（仓库例外）。批次 closed。无冲突集成 rebase 未改变被审 launch 文件，按一次审核门禁规则沿用原结论。
- Wrap-up: `e556a1af452ba74db915e6792d44cd0b483a898e` 已合入 origin/develop，主工作树已快进同步，祖先关系核验成功；本地/远端任务分支、任务工作树、验证进程、独立 tmux server 和启动任务文件已清理；审核原件保留。
- Unresolved issues (2): [真机观察/范围外] Codex 输入提交需补 Enter，命令 ack 不等于正文已处理；[真机观察/范围外] dismiss 在 pane 实际退出时返回探测格式错误，实际清理已确认。两项保留为后续排查观察，不影响本卡前台进程名与直投路由验收；无本卡完成阻塞。
- Summary: 修复、验证、审核、合入与清理完成；Code branch: develop；Final card state: done。

## REVIEWS

- {"run_id":"tmux-exec-qa","batch_id":"tmux-exec-batch","role":"QA","execution_status":"ok","base":"26edb64fcfb654a96afedc30bbaea27e5e918ec7","commit":"b4f634edeb46daa695b73fbe433ce034c7d096ec","report":"reviews/tmux-exec-qa/report.md"}
- {"run_id":"tmux-exec-pm","batch_id":"tmux-exec-batch","role":"PM","execution_status":"ok","base":"26edb64fcfb654a96afedc30bbaea27e5e918ec7","commit":"b4f634edeb46daa695b73fbe433ce034c7d096ec","report":"reviews/tmux-exec-pm/report.md"}
