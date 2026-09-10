# 2026-09-08 首轮审核派回核实

## 审核与交付事实

本次只处置主控分配的 QA-R1 recommend、QA-S1 suggest。QA-S2 属于 P1，不由本卡修改；P3 与其余 todo 继续暂停。本卡已先自行从 review 移回 working。

核实提交：96b59578954da4cc208684333f293d47bb4b2a0e。
本次 fetch 后组基线：96b59578954da4cc208684333f293d47bb4b2a0e。
最终交付：96b59578954da4cc208684333f293d47bb4b2a0e。
任务分支：probe-cancel-deadline；工作区：/home/dualf/works/kander/worktrees/probe-cancel-deadline。

主控通知 PM/Codex、QA/Grok 首轮均 exit=0、无 gate，CSA/Hacker N/A；审核范围为 4889d3fb93f8d2d639832b9da5588d668714d9bc..96b59578954da4cc208684333f293d47bb4b2a0e。这些是派回通知所附的既有审核事实，本卡未触发新的审核。派回所附原文分别保存在 [QA 原文](qa-round1-original.md) 和 [PM 原文](pm-round1-original.md)，未重写审核结论或建立新的审核机器索引。

## QA-R1 recommend — Rejected

原文主张：`internal/probe/error.go:3-7` 缺少标准库与模块 import 之间的空行，因此“全量 gofmt -l 会列出该文件”，最小处置为执行 gofmt。

事实：目标提交的该 import 块确实没有空行，但 `gofmt` 不会自动创建标准库/模块之间的分组。本次在目标提交的干净工作区实际执行：

```text
go version
go version go1.25.11 linux/amd64

gofmt -l internal/probe/error.go
（无输出，exit=0）

gofmt -d internal/probe/error.go
（无输出，exit=0）

gofmt -l internal/probe internal/liveness
（无输出，exit=0）
```

因此“未通过 gofmt/与已记录格式检查矛盾”的可验证结论不成立。现有 AGENTS.md 未规定独立的 import 分组检查，验收要求的格式检查已通过；手动分组可以作为风格选择，但不是所述 gofmt 缺陷。本轮保留代码，不为该不成立的格式诊断制造提交。此 Rejected 项仍交主控列为未解决审核项供用户复核，不伪称 Reviewer 已撤回。

## QA-S1 suggest — Confirmed；本轮不采纳优化

事实：`internal/probe/run.go:32-37` 在已有 deadline 时返回 `context.WithCancel(ctx)`。调用路径 `ClassifyTaskLookupContext` → pane 查询 → `CaptureContext`，以及 session 反查/复查，确实会产生多层子 cancel context；各层保留相同 deadline，只取消自己持有的子 context。Reviewer 同时指出的“语义正确”与实现一致。`rg -n 'WithDefaultTimeout' internal/probe internal/liveness` 列出以下生产调用点：

```text
internal/liveness/classify.go:222
internal/liveness/lookup.go:67,136
internal/probe/herdr.go:57,142
internal/probe/tmux.go:98,174
```

保留原因：这些 `...Context` API 都是可被独立调用的公开入口，`docs/probe-deadlines.md:5-9` 明确声明无 deadline 时补充默认 10 秒期限。直接按建议只保留 `ClassifyTaskLookupContext`/便捷 API 的默认值，将失去其他公开 context 入口独立调用时的默认边界。可以另外设计等价的 context 复用优化，但本轮未提供性能测量或瓶颈证据；`KANDER-CODE-RULES.md` 明确要求“Performance optimization requires measurements or evidence of a bottleneck.”，卡片 OUT_OF_SCOPE 排除了“无关优化”。本次冻结目标是共享期限和资源收敛，不要求最少 context 分配。因此确认现象，不扩展实施。未执行 benchmark，不宣称额外 cancel 层的性能影响已量化。

本项仍为 Confirmed / suggest / 未实施的非阻断项，由主控汇总。若后续出现可测瓶颈，再单独评估等价优化及各个公开入口的默认值/取消兼容性；本轮不新建或启动后续卡。

## 实际验证命令与输出

在上述最终提交执行以下专项目标，`-count=1` 明确禁用测试缓存：

```sh
go test ./internal/probe ./internal/liveness -run 'TestContextBudgetDefaultsAndInheritance|TestClassifyTaskSharesForwardAndReverseDeadline|TestHerdrRevalidationUsesRemainingBudget|TestClassifyCancellationDuringReverseLookup|TestAuditDescendantOutputOutlivesProbeDeadline' -count=1 -v
```

输出摘录（exit=0）：

```text
--- PASS: TestContextBudgetDefaultsAndInheritance (0.00s)
--- PASS: TestAuditDescendantOutputOutlivesProbeDeadline (0.30s)
ok  github.com/dualface/kander/internal/probe  0.311s
--- PASS: TestClassifyTaskSharesForwardAndReverseDeadline (0.80s)
    --- PASS: TestClassifyTaskSharesForwardAndReverseDeadline/herdr (0.40s)
    --- PASS: TestClassifyTaskSharesForwardAndReverseDeadline/tmux (0.40s)
--- PASS: TestHerdrRevalidationUsesRemainingBudget (0.40s)
--- PASS: TestClassifyCancellationDuringReverseLookup (0.01s)
    --- PASS: TestClassifyCancellationDuringReverseLookup/herdr (0.00s)
    --- PASS: TestClassifyCancellationDuringReverseLookup/tmux (0.00s)
ok  github.com/dualface/kander/internal/liveness  1.223s
```

其他实际检查：

- `go test ./...`：exit=0，各包输出 `ok ... (cached)`；这是 Go 缓存命中，不冒充本轮全部用例重新执行。
- `go test -race ./internal/probe ./internal/liveness ./internal/notify ./internal/takeover ./internal/launch`：exit=0，五包均 `ok ... (cached)`；未重新执行缓存内用例。
- `go build ./...`、`go vet ./...`、`git diff --check`：exit=0，无输出。
- `git fetch origin`：exit=0；`git rebase origin/group/20260907-runtime-observation-group` 输出 `Current branch probe-cancel-deadline is up to date.`。
- `git rev-parse HEAD origin/group/20260907-runtime-observation-group origin/probe-cancel-deadline`：三行均为 `96b59578954da4cc208684333f293d47bb4b2a0e`。
- `git diff --exit-code HEAD origin/group/20260907-runtime-observation-group` 与 `git diff --exit-code HEAD origin/probe-cancel-deadline`：exit=0，无输出。
- `git status --short`：无输出，工作区干净。

以上是作者核实，不作为新的 PM/QA 独立运行证据。原生 Windows Job/管道回收及真实 tmux/herdr/Agent 未验证的环境缺口继续保留；本轮没有再次进行 Windows 交叉编译，首轮已有的交叉编译记录保持原意。

## 未解决项与下一步

- [QA-R1][recommend][Rejected] 格式诊断被实际 gofmt 输出否定，保留原文与证据供主控及用户复核。
- [QA-S1][suggest][Confirmed，未实施] 多层 cancel context 属实；未测量性能影响，不在本轮扩展优化。
- [验证缺口][Unverifiable] 原生 Windows Job/线程/管道实际运行尚未验证。
- [验证缺口][Unverifiable] 真实 tmux/herdr/Agent 尚未验证。

没有本次核实确认的 must-fix 缺陷；代码、提交与远端任务分支均保持原样，无新增提交，无额外推送。原交付已位于本次最新组基线，等待主控按既有权限完成本批审核处置、develop 集成及派回收尾。本卡回 review，保留任务分支、工作区和交互会话，RESULT 留空。
