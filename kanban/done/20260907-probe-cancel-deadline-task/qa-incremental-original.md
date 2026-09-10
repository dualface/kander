先读取 Kander 规则、权威规格和本次增量复审上下文，再对照修复区间核对上一轮结论。继续读取配置与审核规则，并定位修复区间里实际改动的测试代码。接下来读取当前配置、审核规则，以及修复区间里 `subscribe_clock_test.go` 的完整差异。先刷新本轮 Kander 配置，再核对测试夹具、看板事务入口和相关辅助函数。接着核对看板事务、测试夹具和订阅扫描路径，确认这次夹具改动是否真正修掉集成失败。接下来核对状态迁移合法性、订阅扫描，以及测试里绕过事务写卡是否会让版本快照失效。核对扫描在目录卡上的失败路径，以及测试夹具是否仍覆盖心跳时序与 interval 断言。Role: QA  
Commit: `e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88`  
Task Context: 增量复核 `06412f4f51faed0d1291e157824dfabf66cb8faa..e0c3dab3a31b59b2d1cd027aaf9b0ac50eefec88`。仅确认集成夹具是否消除「目录卡缺 spec.md」、原心跳/时间/溢出断言是否保留，以及修复是否引入新问题。P3 与原生平台实机仍不在范围。  
Reviewed Scope: 修复区间唯一路径 `internal/liveness/subscribe_clock_test.go`；为核对上一轮意见，只读对照未改生产代码 `internal/probe/error.go`、`WithDefaultTimeout` 调用点、`internal/liveness/classify.go` 复查分支。未扩大审计未变实现。本轮仅用只读检查，未执行 `go test`。

## Prior findings

| ID | 上一轮 | 作者处置 | 本轮结论 | 证据 |
|---|---|---|---|---|
| QA-R1 | recommend | Rejected（`gofmt` 不会插入该空行） | **仍开放 / 未改代码**；原「`gofmt -l` 会列出该文件」主张无新事实可推翻 | Observed：`internal/probe/error.go:3-7` 仍无标准库与模块空行；该文件不在修复区间。争议点只对照作者给出的 `gofmt -l/-d` 无输出；Go 的 `gofmt` 本来就不做 import 分组。不作为新门禁、不重写该 recommend。 |
| QA-S1 | suggest | Confirmed，本轮不实施 | **仍开放 / 未实施** | Observed：`WithDefaultTimeout` 仍在 `classify.go:222`、`lookup.go:67,136`、`herdr.go:57,142`、`tmux.go:98,174`。修复区间未改这些入口。 |
| QA-S2 | suggest | Rejected（契约保留身份不匹配语义） | **仍开放 / 未改** | Observed：`classify.go:69-77` 唯一反查后 pane/agent/session 不一致仍 `Stopped`、空 `NewWindow`、无 `stale_address_reverse_lookup`。不在修复区间。 |

## Behavior / Quality

| 行为 / 质量点 | 结论 | 证据 |
|---|---|---|
| 根因：旧夹具把目录卡 `spec.md` 单独 `Rename` 走 | 已消除 | Observed：修复区间删除 `os` / `os.Rename`；`subscribe_clock_test.go` 已无 `Rename`。旧路径会把 `working/<id>/spec.md` 挪成 `backlog/<id>.md` 或 `review/<id>.md`，目录仍在且缺文档；`scan.go:65-72` / `scan.go:158-163` 会报 `large_task_is_missing_spec_md`（即「大任务缺少 spec.md」）。 |
| 变化卡经合法事务创建与迁移 | 满足 | Observed：`board.NewTask` 直接落 backlog 目录卡（`subscribe_clock_test.go:42-48`），避免非法 working→backlog。`idle` 非 working 与纯心跳用 `MoveEntry` 到 review（`52-54`、`132-134`）。循环内 `MoveEntry(movingEntry, …)` 并用返回快照更新 `movingEntry`/`state`（`109-113`）。`allowedMove` 允许 backlog↔todo、working→review。 |
| todo 门禁与夹具正文 | 满足 | Observed：`makeReady` 填占位并写入 `SELF_REVIEW`/`CARD_REVIEW`；非 watch 路径先 `setTaskGroup`。`validateTarget("todo")` 需要就绪段；有 `TASK_GROUP` 时还要 CARD_REVIEW。与现有 `makeWorking` 同一套助手。 |
| 心跳不随状态事件重置 | 断言保留 | Observed：`Subscribe` 生产逻辑未改（`subscribe.go:361-405`，`heartbeatDue` 不在 state-change 上清零）。测试仍：4 组合 watched×working；每次 state-change `clock += 40ms`；`Heartbeat: .1`；第 3/6 次变化对心跳（`changes != 3*heartbeats`）；收束 `heartbeats==2 && changes==6`（`75-80`、`91-93`、`119-121`）。 |
| 无 working 仍发纯心跳 | 断言保留 | Observed：`TestSubscribePureHeartbeatBeforeRefresh` 仍把卡迁到 review，期望 snapshot 后一条无 liveness 的 heartbeat（`127-151`）。 |
| 不安全 interval / 溢出 | 断言保留 | Observed：`TestAuditRefreshDurationOverflow`、`TestSubscribeIntervalBoundariesAndDefaults`、`TestSubscribeRejectsInvalidIntervalsBeforeBoardAccess` 正文未改；仍覆盖 CLI 与 `Subscribe` 入口、默认 1/900、亚纳秒截断、上下界。 |
| 扫描与目录卡形态一致 | 满足 | Observed：`Relocate` 搬整目录（`transaction.go:309-323`，`Kind: storageKind()`）。`ScanTargets` 认 `<id>/spec.md`。夹具不再留下无文档目录。 |
| 模块边界 | 满足 | Observed：只改测试；生产 `subscribe.go` / probe / classify 未动。测试经公开 `board.NewTask`/`MoveEntry`，未改真实看板。 |
| 1000 行门禁 | 未触发 | Observed：`subscribe_clock_test.go` 终态 213 行。 |

验证事实（不作门禁）：本审查未重跑测试。作者记录定向回归、`go test ./...`、`-race`、build/vet/gofmt 通过。原生 Windows 与真实 tmux/herdr/Agent 仍 Unverifiable。

## Gate findings

未发现 blocking / high / medium 门禁项。

修复区间只改测试夹具：目录卡整卡事务迁移，去掉会拆走 `spec.md` 的 `Rename`。心跳时钟、纯心跳、interval 拒绝与溢出断言未削弱。未改生产行为，也未掩盖上一轮已接受的实现。

## NON-BLOCKING

NON-BLOCKING: none

上一轮 QA-R1 / QA-S1 / QA-S2 仍按上表开放或维持作者处置，不属于本修复区间新项。
