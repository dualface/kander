# Codex 接管核验记录

- 日期：2026-09-09。
- 任务：20260909-tui-theme-presets-task。
- 派发：`f02189336e88d00998914d9951caa0fc`，sync，epoch 5；原子接收返回 `replayed=false`，接收卡版本 55。
- 实际工作树：`/home/dualf/works/kander/worktrees/tui-theme-presets`。
- 交付提交：`6d882fbbf8e086e695728c02594d0d4899bfae82`。本轮无源码变更、无新增提交。

## 进度重建与 Git

完整读取任务卡、plan.md、report.md、审核原件、作者处置与前次完成回执。前次 fix 派发 `630e0448e5e19f581baffe6db7e41b0d` 已完成，交付与 QA-02 处置绑定一致。

`git fetch origin` 成功。任务分支、origin/tui-theme-presets、本地及远端 group/20260909-tui-theme-group 均指向上述交付提交；任务及组工作树干净。`git merge-base --is-ancestor` 对本地及远端组分支返回 0，对本地及远端 develop 返回 1。因此交付已被组分支接收，尚未合入 develop。本轮不需要 rebase，也不更新组分支。

前置卡正在独立接管，其已记录的新交付 `461b8c3516f5864c67912a6293490851adb5d698` 仅在走查文档追加 5 行。组分支尚未包含该后续交付；接收该提交及后续批次处理属于编排器职责，不影响本卡既有交付核验。

## 交付自检

1. `git diff --check aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78 HEAD` 无输出，退出 0；工作树干净。
2. 逐个统计变更 Go 文件，新文件及原小于 1000 行的文件均未超过 1000 行，最大为 config.go 822 行；theme_test.go 448 行。
3. 核对主题表、分类入口、Glamour profile 传递、选项持久化、三语言标签与走查文档，相关注释及报告与现有实现相符。
4. themePalette/themeIsDark 直接返回归一化后的查表字段，两处不可达回退已删除；本轮未引入代码。
5. 核对黄金值、对比度、主题循环、配置回落、选项生效与 Markdown 分类测试，覆盖对应契约；本轮未新增测试。
6. 下列命令均在上述最终交付提交运行；目标 TUI、config、i18n 包通过。
7. 全量复跑共 757 个顶层 Test：756 通过、1 跳过（TestWindowsConsoleLauncher），21 个包通过。首次失败单独保留，不记为通过。

## 本轮实际验证

- `make vet`：退出 0。
- `make fmt-check`：退出 0。
- `GOFLAGS='-json -count=1' make test` 首次：退出 2；755 通过、1 失败、1 跳过，20 个包通过、1 个包失败。
- 唯一失败：`internal/liveness` 的 `TestSubscriptionDispatchDeadlineDoesNotWaitForProbe`，`subscribe_dispatch_runtime_test.go:62` 报告 `open /tmp/TestSubscriptionDispatchDeadlineDoesNotWaitForProbe3566809310/003/events: no such file or directory`。
- 核查该用例：100ms 派发截止后取消探测，再要求假进程已创建 events 文件；故进程尚未写入时存在调度敏感窗口。此为基于代码的推断，未认定为主题回归。相对审核基线的 `internal/liveness` 差异为空。
- `go test ./internal/liveness -run '^TestSubscriptionDispatchDeadlineDoesNotWaitForProbe$' -count=5`：退出 0，连续 5 次通过。
- 相同 `GOFLAGS='-json -count=1' make test` 完整复跑：退出 0，756 通过、1 Windows 平台跳过，21 个包通过。
- 首次完整 JSON 日志：`/tmp/kander-theme-takeover-test.log`；复跑日志：`/tmp/kander-theme-takeover-retest.log`。本附件保留独立于临时日志的计数与失败原文。
- 三份 i18n JSON 键集合相同，各有 `tui.light-warm`、`tui.light-contrast`、`tui.dark-soft`、`tui.dark-contrast`。
- `kander check 20260909-tui-theme-presets-task`：`ok: 1 tasks`。

## 审核与验收

核对 `reviews/tui-theme-pm-r4/report.md`、assignment.json、`reviews/tui-theme-qa-r5/report.md`、assignment.json，以及历史作者原件。QA-r5 的 FINDINGS/NON_BLOCKING 均为空，已关闭 QA-01/QA-02。PM-r4 已关闭 PM-02，明确本卡六主题五界面记录 Complete；剩余 PM-01 仅分配给前置 palette 卡。本卡无待处置的新发现，保留原作者记录，不重复提交或重开已验证结论。

本卡 11 项验收均有实现、测试或已复审的人工记录支撑。人工走查证据来自前任 report.md 与提交中的 testdata/theme-live-walkthrough.md，本轮核对其覆盖范围和生产代码一致性，未声称重新进行人工走查。CSA/Hacker 按仓库规则 N/A。

`kander review progress` 返回 plan `tui-theme-cycle`、status `pending`、batch `tui-theme-batch-one`。计划已封存，批次尚未关闭；不能据本卡验收完成宣称组审核或整卡收尾完成。

## 未决与交接

- [组级门禁][pending] 编排器须接收前置卡最终交付、处理 PM 记录项及批次关闭、完成 develop 集成，之后派发具有 Git 证据的 wrap-up；任务分支和工作树保留。证据：`reviews/tui-theme-pm-r4/assignment.json`、`reviews/plan.json` 和上述 review progress/Git 核验。
- [验证][N/A] 本轮出现一次 liveness 调度敏感测试失败，单独 5 次及全量复跑均未重现；保留此风险供编排器最终验证参考。本项未产生 review run，不改动本卡范围之外的代码。

本轮仅补充核验记录，以同一交付 SHA 完成 sync 并返回 review；done、分支清理及 develop 集成尚未执行，等待组级门禁和正式 wrap-up 派发。
