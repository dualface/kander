# 2026-09-08 集成测试兼容修复

### 2026-09-08 集成适配交付（最新）

- 最新完整交付 SHA：e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88；origin/subscription-heartbeat-clock 已同步，工作区干净。
- 本轮基线：本地 integrate/runtime-observation-three-20260908，06412f4f51faed0d1291e157824dfabf66cb8faa。这是主控在 notify 中明确指定的本轮集成适配基线，覆盖首轮旧组分支基线。
- 接收目标：由主控将本次新增提交接收到该临时集成引用，后续审核及 develop 交付仍由主控负责。执行者未改组工作区、组分支、临时集成引用或 develop。
- 原分支 dadad01b66fc12cc34ebaceb81ccc3acb96424ac 在 fetch 后 rebase 到上述基线，Git 明确输出 `warning: skipped previously applied commit dadad01`，rebase 后 HEAD 等于本轮基线。E1 等价映射为 2f1fa69518819949b60bb70c4b293789013699af；两者 subscribe_clock_test.go 无差异，原心跳实现已在基线，无重复重放。
- 问题确认：在本轮基线上运行 `go test ./internal/liveness -run 'TestAuditChangesSuppressLiveness|TestSubscribePureHeartbeatBeforeRefresh' -count=1`，退出 1，四种连续变化场景和纯心跳场景均报 `大任务缺少 spec.md`。失败与主控原始报告一致。完整派发、原错误和既有 PM/QA 原文见 [派发原文](dispatch-20260908.md)。
- 根因：新版 makeWorking 返回目录卡的 spec.md 路径。旧测试调用 os.Rename 搬走该文档并以 `.md` 文件路径发布，原目录遗留且缺少 spec.md，扫描在心跳断言前失败。
- 修复：仅修改 internal/liveness/subscribe_clock_test.go（15 行新增、12 行删除）。变化卡从 board.NewTask 直接创建于 backlog，避免非法 working -> backlog；所有状态变化改为 board.MoveEntry，使用 currentEntry 的真实版本快照和迁移返回的更新版本，整卡经事务迁移。working -> review 也复用受控入口。夹具正文设置仍复用已有临时看板助手。
- 断言保留：成员/外部、working/非 working 四种组合；每次状态事件推进 40ms，100ms 心跳在第 3/6 次变化出现；总计 2 次心跳、6 次变化；无活动任务不含 liveness；默认值、纯心跳、interval 上下界/溢出/非法值及入口验证均未削弱或跳过。
- 验证：定向回归通过；go test ./... 通过；go test -race ./internal/liveness ./internal/probe ./internal/i18n 通过；go build ./...、go vet ./...、git diff --check 通过；gofmt -l internal/liveness/subscribe_clock_test.go 无输出。完整成功输出见 [本轮适配报告](integration-fix-20260908.md)。
- 推送：本次 rebase 重写仅限自己的任务分支；用明确旧值 dadad01b66fc12cc34ebaceb81ccc3acb96424ac 的 --force-with-lease 成功推送。组分支仍为 96b59578954da4cc208684333f293d47bb4b2a0e，临时集成引用仍为本轮基线。
- 审核事实：收到主控提供的首批 4889d3fb93f8d2d639832b9da5588d668714d9bc..96b59578954da4cc208684333f293d47bb4b2a0e PM/Codex、QA/Grok 无 gate 原文；P1/P2 三项非阻断由各原作者核实，本卡不修改其他卡代码。本轮未触发 Reviewer；本次测试兼容修复的后续审核由主控安排。CSA/Hacker 按仓库特例为 N/A。
- 环境缺口：原生 Windows、真实 tmux/herdr/Agent 未运行；临时看板和假 CLI 用例不计为实机验证。工作区与任务分支保留，等待主控后续派发。

## 实际验证输出

### go test ./...

```text
ok  	github.com/dualface/kander/cmd/kander	0.067s
ok  	github.com/dualface/kander/internal/board	19.274s
ok  	github.com/dualface/kander/internal/cli	0.014s
ok  	github.com/dualface/kander/internal/config	0.281s
ok  	github.com/dualface/kander/internal/fs	0.511s
ok  	github.com/dualface/kander/internal/i18n	0.093s
ok  	github.com/dualface/kander/internal/install	0.821s
ok  	github.com/dualface/kander/internal/launch	9.317s
ok  	github.com/dualface/kander/internal/liveness	3.830s
ok  	github.com/dualface/kander/internal/menu	10.529s
ok  	github.com/dualface/kander/internal/notify	2.095s
ok  	github.com/dualface/kander/internal/probe	0.715s
ok  	github.com/dualface/kander/internal/process	0.018s
ok  	github.com/dualface/kander/internal/review	36.492s
ok  	github.com/dualface/kander/internal/takeover	0.454s
ok  	github.com/dualface/kander/internal/tui	5.096s
ok  	github.com/dualface/kander/internal/version	(cached)
ok  	github.com/dualface/kander/internal/window	0.128s
ok  	github.com/dualface/kander/rules	0.003s

exit=0
```

### go test -race ./internal/liveness ./internal/probe ./internal/i18n

```text
ok  	github.com/dualface/kander/internal/liveness	5.719s
ok  	github.com/dualface/kander/internal/probe	2.008s
ok  	github.com/dualface/kander/internal/i18n	1.362s

exit=0
```

### go build ./...

```text

exit=0
```

### go vet ./...

```text

exit=0
```
