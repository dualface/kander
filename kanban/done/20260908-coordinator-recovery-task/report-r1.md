# 第1轮审核修复交付记录

最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`，分支 coordinator-recovery，基于组头 `287161673bf3f11a865f2e8a40a023a65bdc1f65`。修复两项恢复阻断：组员首次启动的周期绑定、fix 闭批后未派收尾时的历史消费。四份来源记录合并计为两个根因；这份记录不宣称增量审核或组集成已完成。

## 独立核实与作者处置

- CR-01（PM-02 / QA-01）：独立复现后 confirmed → fixed。双卡中 todo 成员首次启动触发 cycle 冲突；新增 awaiting_start 与首次持久启动事实核验，CAS 绑定一次，保留检查点历史。覆盖 working、漏过 working、重新 claim、重复观察、缺事实、旧 revision/CAS、既有周期替换、旧等待格式及元数据尚未发布的中间快照；launch 层实际 commandStart 生产路径用隔离假终端验证顺序启动 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。
- CR-02（PM-01 / QA-02）：独立复现后 confirmed → fixed。完成 fix、PM 增量复审及 QA、闭批后，旧代码在恢复时报 batch already closed；提取成功 run 的只读消费校验，保持原件、身份、完整发布、lineage、assignment、作者验证；写入/发送仍拒绝闭批。重复恢复不增事务；四类原件缺失均拒绝且不改游标 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。

- [g3-pm-r1/PM-02：cr-r1-pm-02-fixed](reviews/g3-pm-r1/dispositions/cr-r1-pm-02-fixed.json)，status=fixed；分别绑定来源原文、报告摘要及当前作者 codex。
- [g3-pm-r1/PM-01：cr-r1-pm-01-fixed](reviews/g3-pm-r1/dispositions/cr-r1-pm-01-fixed.json)，status=fixed；分别绑定来源原文、报告摘要及当前作者 codex。
- [g3-qa-r1/QA-01：cr-r1-qa-01-fixed](reviews/g3-qa-r1/dispositions/cr-r1-qa-01-fixed.json)，status=fixed；分别绑定来源原文、报告摘要及当前作者 codex。
- [g3-qa-r1/QA-02：cr-r1-qa-02-fixed](reviews/g3-qa-r1/dispositions/cr-r1-qa-02-fixed.json)，status=fixed；分别绑定来源原文、报告摘要及当前作者 codex。

## 11项实现自检

1. 受控 checkpoint、固定锁序和 coordinator epoch：竞争/旧会话及 kill 原件回归 PASS。 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）
2. 成员周期/引用/待办与幂等：本轮首次启动修复与重复观察 PASS，旧历史保留。 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）
3. 双编排与写边界崩溃：原5个 kill 边界回归 PASS；检查点不发通知。 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）
4. 任务/dispatch 同轮对账：首次启动、closed completed fix 及错轮/旧交付拒绝 PASS。 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）
5. EOF/期限/损坏边界：文档限定一次恢复；deadline、损坏/reparse 自动化回归 PASS；OS I/O 不声称硬期限。 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）
6. snapshot/事件统一：快速往返、首次启动漏边沿、重启/重复/重排与外部组员回归 PASS。 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）
7. 审核原件/角色/作者/闭批/Git：新增闭批前后窗口回归 PASS，真实 Git 验证旧用例 PASS；原件损坏不降级。 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）
8. wrap-up 与专用授权：复用原生产者，快速完成/epoch 隔离与无权拒绝回归 PASS。 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）
9. 跨模块部分归档/缺角色/全失败/lineage/legacy/无finding/代办：相关原回归在最终全量与race复跑 PASS。 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）
10. 原13复现映射全部 PASS；S/D kill与旧路径竞态回归 PASS，平台与真实Agent缺口单列。 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）
11. 测试/文档/三语前缀/build/vet/格式通过；PM/QA增量复审待协调者安排，CSA/Hacker N/A。 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）

## 最终实跑

- `go test -json -count=1 ./...`：退出 0，19 包，1016 PASS / 1 SKIP（顶层 635 PASS / 1 SKIP）（最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。
- `go test -race -json -count=1 ./internal/fs ./internal/board ./internal/launch ./internal/review ./internal/cli ./internal/liveness ./internal/notify ./internal/window`：退出 0，8 包，752 PASS / 1 SKIP（顶层 414 PASS / 1 SKIP）（最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。
- `go build ./...`、`go vet ./...`、修改 Go 文件 `gofmt -l`、本轮及完整交付范围 `git diff --check`：全部退出 0；格式与 diff 检查无输出 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。
- `GOOS=windows GOARCH=amd64 go build -o /tmp/coordinator-r1-validation/kander.exe ./cmd/kander`：退出 0，仅交叉构建 （最终提交 `5845e6fd0f2b503313030349fa211a7791a50169`）。


全部测试计数含子用例。五个新增顶层回归在最终全量与 race 中通过；原13复现映射全部通过。27个完整交付范围修改 Go 文件最大457行，冲突标记0；本轮9个修改Go文件。命令、耗时、逐文件结果与机器计数见 [汇总](verification/r1/summary.json)、[检查](verification/r1/checks.json)、[全量原始输出](verification/r1/all.jsonl)、[race原始输出](verification/r1/race.jsonl)。七项 Delivery Self-Check、提交 rebase 映射与逐条来源结论完整追加在 spec.md 的 IMPLEMENTATION。

复现日志分别为 [首次启动](verification/r1/repro-cycle.txt)、[闭批窗口](verification/r1/repro-closed.txt)。早期夹具的缺分支、大写ID和非法同态move失败已修正，不能将其或旧提交的通过记录替代最终复跑。

## 审核与尚未完成事项

PM g3-pm-r1 / QA g3-qa-r1 首轮未通过；本卡 CR-01/CR-02 均已独立核实并 fixed，四份绑定作者原件保留精确原文/摘要。两角色 NON_BLOCKING 为空；本卡没有 rejected/unverifiable 项。等待协调者 ff 接收后触发增量复审。CSA/Hacker N/A，依据本仓库 AGENTS.md，不运行。

环境缺口2项：原生 Windows 测试；真实 tmux/herdr/Agent 冒烟。唯一自动跳过为 TestWindowsConsoleLauncher。Windows交叉构建、假终端、真实本机Git与隔离kill的通过不能替代上述实机验证。

本轮交付 review，保留交互CLI、worktree及本地/远端任务分支。组接收、develop集成、最终收尾和done尚未由本执行端执行，等待协调者后续派回。
