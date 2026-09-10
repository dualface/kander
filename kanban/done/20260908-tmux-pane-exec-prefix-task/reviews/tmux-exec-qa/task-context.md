# tmux pane 命令加 exec 前缀修复前台进程名失配

- TYPE: Bug
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 01:31
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t19:wX:p1V
- STARTED_AT: 2026-09-08 10:12
- FINISHED_AT:
- TASK_BRANCH: tmux-pane-exec-prefix
- RESULT:

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
- Delivery Self-Check（以下所有核验均对应最终提交 `b4f634edeb46daa695b73fbe433ce034c7d096ec`）：
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
- 验收 7/7 自查完成，待 PM/QA 审核；CSA/Hacker 依仓库 AGENTS.md 标记 N/A。尚无合入 develop 授权，审核完成后保持 working，保留分支和工作树等待确认。

## SUMMARY

<FILL_IN>
