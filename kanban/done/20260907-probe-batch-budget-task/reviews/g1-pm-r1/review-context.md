Review focus:
(1) 批量存活采集的总预算与并发上限是否真的有界：ClassifyTasksContext / BatchOptions 的默认 10 秒总预算、并发上限 4、较早父 context deadline 优先，是否在排队、前向探测、session 反查、复查和进程回收各阶段共享同一预算；超时或取消后是否仍会发起后续查询。
(2) 每条观测的新鲜度与身份绑定是否可判定：observed_at、请求身份（TaskID/SESSION/WINDOW/OWNER/STARTED_AT）、runtime_state、observation_valid 与 ValidFor 的语义是否与文档一致；是否把 herdr 的 idle/working/blocked/done 误当作 alive，或把 tmux 运行状态误报为已知。
(3) check 存活段改用批量入口后的回归：任务顺序、结构检查退出码、未采集/读取失败的三语 unknown 原因、NewWindow 保留，以及 goroutine/worker 是否在返回前全部汇合。
(4) 文档与注释是否与实现一致（README、docs/probe-deadlines.md、AGENTS.md 索引、英文发布协议、三语消息）。

Verification records:
执行者在最终提交 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5 实跑：go test -json -count=1 ./... exit=0（19 包、783 PASS、1 SKIP、0 FAIL）；go test -race -json -count=1 ./internal/liveness ./internal/probe ./internal/i18n ./internal/launch ./internal/notify ./internal/takeover exit=0（211 PASS、1 SKIP、0 FAIL）；go build ./...、go vet ./...、改动 6 个 Go 文件 gofmt -l 均 exit=0 无输出；GOOS=windows GOARCH=amd64 go build ./... 与 go test -c ./internal/liveness 均 exit=0。完整输出见卡目录 validation.txt。首次提交 b8ea5da7f3f8e64b891a4eb74a396d19860171ef 曾捕获夹具 PID 空文件竞态并在最终提交修复，未把旧失败写为通过。

Environment gaps:
原生 Windows 与真实 tmux/herdr/Agent 未运行；交叉编译、假 CLI 与 Linux /proc 检查不代表这些实机环境通过。这属于已披露的验证缺口，不得据此单独判定 blocking/high/medium。

本批次说明：
本批次只有 20260907-probe-batch-budget-task 一张卡，组分支已于 2026-09-08 按用户授权重锚到 develop 7e6fe2073ff007a6639a2517f4c7874f8e420bd1，该 SHA 即本批 base，同组 P1/P2/E1 的交付已在 base 内，不属于本批新增改动。实现由执行 Agent（codex）完成，主控未代改代码。