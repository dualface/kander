先读 Kander 规则、权威规格和任务上下文，再按提交范围做质量审查。继续读取配置、审核规则和补丁剩余部分，并对照工作区源码核对实现。接下来对照实现源码、调用方和测试，核对三张卡的行为契约。审查结论已对齐三张卡的契约与实现；接着整理报告并尝试删除任务文件。Role: QA  
Commit: `96b59578954da4cc208684333f293d47bb4b2a0e`  
Task Context: 仅验收已交付三卡（P3 不在范围）：反查失败报 `unknown`；单次探测取消后有界退出；订阅心跳独立于状态变化并拒绝不安全 interval。  
Reviewed Scope: 提交树内本轮路径（`internal/liveness/{classify,lookup,subscribe,check}.go` 及测试、`internal/probe/{run,run_posix,run_windows,herdr,tmux,error}.go` 及测试、三语 locale、`README.md` / `AGENTS.md` / `docs/probe-deadlines.md` / `rules/KANDER-KANBAN-RULES.md` / `rules/KANDER-TASK-GROUP-RULES.md`），以及未改但消费同一 lookup/Capture 契约的 `internal/notify/{probe,resolve}.go`、`internal/takeover/{dismiss,wait,ops}.go`。只读核对；未在本轮执行 `go test`。

## Behavior / Quality

| 行为 / 质量点 | 结论 | 证据 |
|---|---|---|
| herdr/tmux 反查区分零匹配 / 唯一 / 歧义 / 采集失败 | 满足 | Observed：`lookupMatchError.matches==0` 才 `Stopped`，其余非唯一结果与非法输出走 `Unknown`（`lookup.go` 50–58、116–125、183–192；`classify.go` 42–49） |
| 唯一匹配返回 `drifted`+`NewWindow`；禁用反查 / 空引用 / Codex 空引用保持原语义 | 满足 | Observed：`classify.go` 51–56、93；`lookup_test.go` `TestLivenessIdentityAndReadinessSemantics` |
| `unknown` 不写卡、不改 `check` 结构退出码；`alive` 不是 ready | 满足 | Observed：`check.go` 96–130 仍用 `board.CheckBoard` 退出码；`ClassifyTask*` 只读；规则/README 已写明 |
| notify/takeover 非 nil 反查仍当错误，签名未改 | 满足 | Observed：`notify/resolve.go` 125–128、163–166；`takeover/dismiss.go` 74–79、124–128 仍 `if err != nil` |
| context 贯穿 Capture / pane / 反查 / liveness；无 deadline 补 10s；显式期限不缩短 | 满足 | Observed：`WithDefaultTimeout`（`run.go` 32–37）；`ClassifyTaskLookupContext`（`classify.go` 221–236）；便捷 API 走 `timeoutContext`（`run.go` 39–43） |
| 前向/反查/复查共享剩余预算，耗尽后不启动后续查询 | 满足 | Observed：`CaptureContext` 入口 `ctx.Err()`（`herdr.go` 144–146）；反查循环内检查（`lookup.go` 83–85、155–157）；`deadline_test.go` 共享 400ms / 取消不 launch |
| POSIX 独立进程组 SIGKILL；Windows Job+挂起加入再恢复；取消关管道并汇合 goroutine | 满足 | Observed：`run_posix.go` 14–31；`run_windows.go` 20–81；`run.go` 82–110。原生 Windows 运行 Unverifiable |
| 心跳 due 独立于状态事件；无 working 仍发纯心跳；默认 1s/900s | 满足 | Observed：`subscribe.go` 357–401 去掉 `lastEvent` 重置；`subscribe_clock_test.go` 反转审计与纯心跳 |
| refresh/heartbeat 有限、正、可表为 Duration；拒绝零/负/溢出；截断亚纳秒 | 满足 | Observed：`subscriptionInterval`（`subscribe.go` 64–70）；CLI 与 `Subscribe` 入口双校验 |
| 审计测试已反转且覆盖合同场景 | 满足 | Observed：`TestAuditReverseLookupErrorBecomesUnknown`、`TestAuditDescendantOutputOutlivesProbeDeadline`、`TestAuditChangesSuppressLiveness`、`TestAuditRefreshDurationOverflow` |
| 模块边界与依赖方向 | 满足 | Observed：进程回收留在 `probe`；分类政策留在 `liveness`；notify/takeover 仍走公开 `ReverseLookup`/`Capture` |
| 非生成代码 1000 行门禁 | 未触发 | Observed：本轮相关 `.go` 均远低于 1000（如 `subscribe.go` 447、`liveness_test.go` 772、`classify.go` 237） |
| 三语文档与规则 | 满足 | Observed：locale 新键齐全；README / 发布规则 / `docs/probe-deadlines.md` 与实现一致 |

验证事实（不作门禁）：本审查未重跑测试；主控记录称 HEAD 上 `go test ./...` 通过。原生 Windows Job/管道回收与真实 tmux/herdr/Agent 仍未执行，文档已列为缺口。

## Gate findings

未发现 blocking / high / medium 门禁项。

三卡主路径均有可达实现与对应断言：反查错误不再变成 `stopped`；取消/期限关闭管道并回收所属进程树；心跳时钟不再被状态事件清零；不安全 interval 在 CLI 与 `Subscribe` 入口被拒绝。notify/takeover 仍把任意非唯一反查当错误，符合「不改非 nil 错误分支」。

## NON-BLOCKING

### QA-R1 recommend  
Claim: Observed  
Location: `internal/probe/error.go:3-7`

`FailureDetail` 新增的 import 块未按 `gofmt` 分组，标准库与模块导入之间缺少空行：

```3:7:internal/probe/error.go
import (
	"context"
	"errors"
	"github.com/dualface/kander/internal/config"
)
```

Impact: 不改变运行行为；全量 `gofmt -l` 会列出该文件，与验收里的格式检查不一致。  
Smallest change: 对该文件执行 `gofmt`。

### QA-S1 suggest  
Claim: Observed  
Location: `internal/liveness/classify.go:222`、`internal/liveness/lookup.go:67,136`、`internal/probe/herdr.go:57,142`、`internal/probe/tmux.go:98,174`

同一条调用链上多次 `WithDefaultTimeout`。父级已有 deadline 时每次只是再包一层 `WithCancel` 并 `defer cancel()`，语义正确（子 cancel 不伤父 deadline），但增加了无必要的 cancel 层。  
Smallest change: 只在最外层（`ClassifyTaskLookupContext` / 便捷 API）补默认期限，内层直接用传入 `ctx`。

### QA-S2 suggest  
Claim: Observed  
Location: `internal/liveness/classify.go:69-77`

herdr 唯一反查成功后，复查 `Pane==nil` 或 agent/session 不一致仍直接 `Stopped`，且不带 `liveness.stale_address_reverse_lookup` 包装、不填 `NewWindow`。这与「零匹配才 stopped」的主路径不同，属于复查身份失败。触发窗口很窄（list 与 get 之间的竞态），且与改前语义一致。  
Smallest change: 若希望操作者看到反查已命中，将该分支改为带阶段说明的 `Unknown`（或 `Drifted`+`NewWindow` 后再由消费端决定）。不属于本卡必须改动。
