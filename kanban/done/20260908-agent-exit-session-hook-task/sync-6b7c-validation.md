# 组基线 6b7c 同步验证

- 日期：2026-09-09；执行者 Codex；dispatch `codex-agentdef-hook-sync-6b7c-20260909`，epoch 3。首次接受 `replayed=false`，accepted card revision 30。
- 任务工作树：`/home/dualf/works/kander/worktrees/agent-exit-session-hook`；分支 `agent-exit-session-hook`。
- 原交付 `2d77411e634f7de764da00f1c3dff0203c1f3dfd`；本轮 fetch 核实本地与远程组分支均为 `6b7c132db76cc00a1245bc45fd992afe3109dcc6`，没有意外漂移。
- `git rebase origin/group/20260908-agent-definition-group` 无冲突完成；两笔提交映射为 `f8c9ce7` 与 `64d0c2cf5fa2e8c8930f56967dced09a2187478d`。没有新增功能或修改原补丁。
- `git range-diff d97964c06adb942b94b25c7e5c5bfb04795172e4..2d77411e634f7de764da00f1c3dff0203c1f3dfd 6b7c132db76cc00a1245bc45fd992afe3109dcc6..64d0c2cf5fa2e8c8930f56967dced09a2187478d` 两笔均为 `=`：退出命令、hook 原功能及 Cursor allocated 兼容、未知嵌入 hook 诊断、英文文档修正全部保留。

## 前置与审核边界

- 已核实三个前置最新 dispatch 均为 completed、卡在 review；其 completed delivery 分别为 A1 `b216ca25cb328300b3a45c54bc00a5ca442a89d7`、parser `e756f7d672bb4cc2a488a476485f6821e7b43d2e`、A2 `6b7c132db76cc00a1245bc45fd992afe3109dcc6`。逐个 `git merge-base --is-ancestor <delivery> 6b7c132db76cc00a1245bc45fd992afe3109dcc6` 均退出 0。
- 主控已闭合第一批于 `d97964c06adb942b94b25c7e5c5bfb04795172e4`，其 PM/QA PASS 不能替代本卡首次审核。`kander review progress` 现仅列 `unsealed-plan`、`unassigned-member`，不再列第一批待闭合；本卡完整交付仍待主控接收和第二批首次审核。
- 本轮不触发 Reviewer、不修改组分支、不进入 done、不清理任务分支或工作树。

## 最终提交上的验证

所有本轮命令均在 `64d0c2cf5fa2e8c8930f56967dced09a2187478d` 上执行：

- `go test -json ./... -count=1`：退出 0；1496 pass / 0 fail / 1 skip，21 packages pass。唯一 skip 为 Linux 环境下的 `TestWindowsConsoleLauncher`。
- `go build ./...`：退出 0。
- `go vet ./...`：退出 0。
- `GOOS=windows go build ./...`：退出 0。仅交叉构建，未执行 Windows 原生测试。
- 新基线组合检查通过：`TestMinimalCursorFixtureJSONRoundTrip`、`TestBuiltinAgentArgumentsMatchPreChangeOutput`、`TestBuiltinStartKeepsPromptOnArgvAndDoesNotDeliverToPane`（tmux/herdr 内置 agent 子用例）、`TestValidateOutputRejectsIllegalCombinations`、`TestDecodeOutputSpecRejectsEmptySelectOnJSON`、`TestReviewOutputParsingUsesProcessEntry`。
- hook 与退出能力检查通过：`TestEmbeddedUnknownSessionHookNamesAgentAndHook`、`TestUnregisteredSessionHookNamesAgentAndHook`、`TestDialectSessionCompatibility`、`TestRegisteredSessionHooksDocumented`、纯模板 dismiss 两项测试、现有 Cursor create-chat 及 Codex 会话发现相关测试。
- `TestSubscriptionDispatchDeadlineDoesNotWaitForProbe` 本轮通过；此前两次整包复验失败、单测五次通过及人为延迟基线对照仍保留于 [takeover-validation.md](takeover-validation.md)。本轮未复现不代表历史失败不存在，也不宣称时序风险已修复。

## Delivery Self-Check

1. `git diff --check 6b7c132db76cc00a1245bc45fd992afe3109dcc6...64d0c2cf5fa2e8c8930f56967dced09a2187478d` 退出 0；工作区干净，无新冲突或格式差异。
2. 对本卡全部触及 Go 文件逐一比较行数：均在 1000 行以内，最大 `internal/launch/session.go` 为 466 行。
3. 本轮补丁未改变；会话策略、退出命令和 Hook Catalog 文档及发布规则与代码一致，英文修正保留。
4. 未新增死代码；原调用与钩子能力映射保留，通过全量构建和相关行为测试核对。
5. 本轮未增加或削弱测试；前置新增 fixture、argv、parser 校验与本卡既有测试共同执行通过。
6. 受影响模块在最终提交编译和测试通过；Windows 原生运行仍未验证。
7. 全量命令、最终完整 SHA、测试与 package 计数如上；此前失败通过链接保留，不改写为历史 PASS。

## Git 交付

- 已使用 `git push --force-with-lease=refs/heads/agent-exit-session-hook:2d77411e634f7de764da00f1c3dff0203c1f3dfd origin HEAD:refs/heads/agent-exit-session-hook` 更新仅本任务远端分支。
- push 后再次 fetch：本地任务 HEAD 与远程任务 HEAD 均为 `64d0c2cf5fa2e8c8930f56967dced09a2187478d`；组 HEAD 仍为 `6b7c132db76cc00a1245bc45fd992afe3109dcc6`；工作区干净。
- 全 internal 生产 Go 中按内置 agent 名的字符串检索仅命中 `internal/review/settings.go` 两处默认 reviewer 返回值，不存在按 agent 名 switch/if 的业务分支。
- 最终交付仍需主控快进接收进组分支，安排第二批审核及后续集成。本轮不作组级已接收或已集成声明。
