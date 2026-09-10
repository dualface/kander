# TUI 打开 Options 时重新读取作用域和项目配置

- TYPE: Bug
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 15:16
- OWNER: grok
- SESSION: grok c064778a-a2b4-4bb2-a0cf-af36752744fb
- WINDOW: herdr:w2T:tS:w2T:pS
- STARTED_AT: 2026-09-09 15:16
- FINISHED_AT: 2026-09-09 18:49
- TASK_BRANCH: 20260909-options-reload-config
- RESULT: completed

## GOAL

TUI 打开 Options 时只在第一次读盘，之后复用内存里的 `App.Session`，运行期间改过的作用域 `config.json` 和项目 `.kander-config.json` 都不会反映到面板。每次打开 Options（面板尚未打开）都重新读取这两份文件，使表单、overlay 提示和有效语言与磁盘一致。

## USER_DECISIONS

- 打开 Options 时作用域配置和项目覆盖（若有）都重新读盘，不只刷新 `config.json`。
- 可编辑 session 仍来自 `LoadScope`，不把 overlay 合并进保存缓冲。
- 项目覆盖要真正读文件内容，不只检查路径是否存在。
- 任一侧读失败走现有 `loadErr`，不继续用内存里的旧 session。
- 不实现 Global/Project 双 tab；那是 `20260909-tui-project-options-task` 的范围。
- 看板实时主题/列数仍等界面项提交后再写回，本次不改实时看板刷新。

## EXPECTED_OUTCOME

在已运行的 TUI 里按 `o` 打开 Options，面板展示的作用域值和 overlay 提示来自这一次读盘。外部改过作用域文件、新增/删除/改过 overlay、或上次放弃未保存编辑后，重开都能看到磁盘现状。保存仍只写作用域文件。

## ACCEPTANCE_CRITERIA

- [ ] `openOptionsAt` 在面板未打开时每次都重新加载，不再因已有 `App.Session` 跳过读盘。
- [ ] 作用域加载走 `config.LoadScope` + `menu.NewSession`，成功则替换 `App.Session`；失败设置 `loadErr`，不保留旧 session。
- [ ] 同一次打开真正读取项目 overlay（`readOverlay` 或等价接口）：文件新增、消失、内容变化都反映到 overlay 提示；有效语言绑定使用刚读到的合并结果。
- [ ] overlay 存在时，表单可编辑值仍是作用域文件的值，不被 overlay 键污染；保存不把 overlay 键写入作用域 `config.json`。
- [ ] 上次打开改了字段但不保存就关闭，再打开回到磁盘值。
- [ ] 临时目录测试覆盖：改作用域后重开、增删改 overlay 后重开、放弃未保存编辑后重开、overlay 与作用域隔离。已有「预先注入 Session 再 `openOptions`」的测试改为走完整加载，或只通过 `openPanel` 测表单内部。
- [ ] 针对变更行为运行 `go test ./internal/tui`；交付前 `go test ./...`。测试使用临时目录，不改写真实 HOME 配置或用户看板。

## THREAT_MODEL

N/A。配置读写继续走现有 `internal/fs` 安全边界，不放宽路径与平台约束。

## OUT_OF_SCOPE

- 既有问题：不处理 Options 以外的 TUI、启动或看板问题；不修复与本次读盘无关的 session/doctor 既有行为。
- 加固：不引入配置文件监视、热重载或新的权限模型；并发写文件仍按现有保存冲突处理。
- 共享契约与文档：不改配置 schema、合并语义或安装作用域；不重写 released rules 里 Options 只写 scope 的描述（那是项目配置卡的范围）。
- 相邻功能：不实现 Global/Project 双 tab、不把 overlay 变成可编辑缓冲、不在打开时把主题/列数立刻应用到看板、不改 CLI `kander config`。

## DISCUSSION

- 单目标、改动集中在 Options 打开路径，采用一张 small 卡。
- 当前 `detectOverlayNotice` 只查路径；本次改为真正读 overlay，读失败必须可见。
- `NewSession` 会再做一次 agent 探测，打开时可能短暂转圈，与第一次打开一致，这是接受的代价。
- SELF_REVIEW: 通过。目标与验收覆盖作用域和 overlay 两次读盘、失败可见、写入隔离和放弃编辑后恢复；双 tab 与实时看板刷新已排除。未把建议写成用户决策。

## IMPLEMENTATION

- 2026-09-09：任务分支 `20260909-options-reload-config`，工作树 `/home/dualf/works/kander/worktrees/20260909-options-reload-config`，基线 `origin/develop` `211c104282f181d5fabe6cc49b03583b0bed269f`。
- 交付提交 `26b42cf434d55c534998c4ea73db36cd69a2d497`：`openOptionsAt` 在面板未打开时每次走 `LoadScope` + `ReadOverlay` + `NewSession`；成功替换 `App.Session` 并用刚读到的合并结果绑定语言；任一侧失败写 `loadErr` 并清空旧 session。可编辑值仍来自作用域文件。已推送到 `origin/20260909-options-reload-config`。

交付自检（提交 `26b42cf434d55c534998c4ea73db36cd69a2d497`）：

1. `git diff --check 211c104282f181d5fabe6cc49b03583b0bed269f HEAD`：干净。
2. 行数：新增 `options_reload_test.go` 219 行；已有文件均未超过 1000 行（`options_test.go` 762→811，`options_panel.go` 550→564，`overlay.go` 214→249）。
3. 注释与文档与实现一致；删除只查路径的 `detectOverlayNotice`。
4. 无死代码：`ReadOverlay` / `ApplyOverlay` / `loadOptionsSession` 均有调用。
5. 无重复测试：重载、overlay 增删改、放弃编辑、隔离、读失败、面板已打开跳过分别覆盖。
6. `go test ./internal/tui -count=1`：ok，105 tests（`26b42cf434d55c534998c4ea73db36cd69a2d497`）。`go test ./internal/config -count=1`：ok，122 tests。
7. `go test ./... -count=1`：21 packages / 1279 tests 通过（`26b42cf434d55c534998c4ea73db36cd69a2d497`）。测试使用临时目录，未改写真实 HOME 或用户看板。

- 2026-09-09：合入前 `origin/develop` 前进到 `fd24781ac0f6abf001e11422d926b566e56623f8`（完整配置门槛 + Options 重开补全）。任务分支 rebase 到该点；`options_panel.go` / `options_test.go` 有实质冲突，手解为：保留每次打开都 `requestSession`，并在 `loadOptionsSession` 里保留 develop 的 `Load(false)`；完整配置失败测试改为走 `runOptionsLoad` / `finishOptionsLoad`。已关闭审核目标 `46ed8a32fd8f579ef9e16c9fd103b5fea200bc72` 不在新历史上。
- rebase 后 HEAD 先为 `bec7bf16bd5209f3fa47ffcc5188d94743c45605`；完整配置错误框测试仍断言中文，当前环境 `en_US.UTF-8` 失败。补丁 `bce1ad374757cbf18db674d05e56974c6de68b81`：`requireErrorFrame` 同时匹配 en/zh/ja 文案。已 `git push --force-with-lease origin 20260909-options-reload-config`。

交付自检（提交 `bce1ad374757cbf18db674d05e56974c6de68b81`，基线 `fd24781ac0f6abf001e11422d926b566e56623f8`）：

1. `git diff --check origin/develop...HEAD`：干净。
2. 行数：`options_reload_test.go` 新增 469 行；触及文件均未超过 1000 行（相对基线：`options_test.go` 826→888，`options_panel.go` 556→601，`options_form.go` 753→790）。
3. 注释与文档与实现一致。
4. 无死代码。
5. 无重复测试：完整配置错误框改为三语文案，不另增用例。
6. `go test ./internal/tui ./internal/config -count=1`：ok（`bce1ad374757cbf18db674d05e56974c6de68b81`）。
7. `go test ./... -count=1`：21 packages / 1354 tests 通过（`bce1ad374757cbf18db674d05e56974c6de68b81`）。测试使用临时目录，未改写真实 HOME 或用户看板。

- 2026-09-09：用户选择路径 1。新建重审卡 `20260909-options-reload-rereview-task`，计划 `20260909-options-reload-rereview`，批次 `20260909-options-reload-rereview-batch-1`。
- 重审修复 `98a0a77a61d8ad561566cb344d918f1db0d48637`：raw overlay 校验、NewSession 不再绑定语言、打开时刷新主题/代理文案。增量 PM `c4c5ef3a330fb588dbb43135ce2d8c30`、QA `feaf13f03f428bdb87fd4f42759df469` PASS。批次关闭于该提交。重审卡已 done。
- 最终交付 `98a0a77a61d8ad561566cb344d918f1db0d48637` 已 push 到 `origin/develop`。本地 develop 在该点之上另有 5 个未推送文档提交，已 rebase 到 `origin/develop`。

交付自检（提交 `98a0a77a61d8ad561566cb344d918f1db0d48637`，基线 `fd24781ac0f6abf001e11422d926b566e56623f8`）：

1. `git diff --check` 干净。
2. 触及文件均未超过 1000 行。
3. 注释与实现一致。
4. 无死代码。
5. 无重复测试。
6. `go test ./internal/tui ./internal/config ./internal/menu -count=1`：ok（`98a0a77a61d8ad561566cb344d918f1db0d48637`）。
7. `go test ./... -count=1`：21 packages 通过（`98a0a77a61d8ad561566cb344d918f1db0d48637`）。测试使用临时目录，未改写真实 HOME 或用户看板。

## SUMMARY

TUI 每次打开 Options 都从磁盘重读作用域和 overlay。可编辑值仍是作用域文件；失败走 loadErr 并丢弃旧 session。rebase 后经独立重审卡关闭于 `98a0a77a61d8ad561566cb344d918f1db0d48637`，该提交已在 `origin/develop`。

## REVIEWS

- {"run_id":"f2000f80acd6855e7e9a936ae4d94ea0","batch_id":"20260909-options-reload-batch-1","role":"PM","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"26b42cf434d55c534998c4ea73db36cd69a2d497","report":"reviews/f2000f80acd6855e7e9a936ae4d94ea0/report.md"}
- {"run_id":"0fa4f448e696b26cb0be85ad6fc21060","batch_id":"20260909-options-reload-batch-1","role":"QA","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"26b42cf434d55c534998c4ea73db36cd69a2d497","report":"reviews/0fa4f448e696b26cb0be85ad6fc21060/report.md"}
- {"run_id":"a00c77a9c5d75699770534c8ee1b1c1e","batch_id":"20260909-options-reload-batch-1","role":"PM","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"639cc1e6e5d10592d932cdc43fcd8ee9c77d7b4a","previous_run_id":"f2000f80acd6855e7e9a936ae4d94ea0","report":"reviews/a00c77a9c5d75699770534c8ee1b1c1e/report.md"}
- {"run_id":"8815dae207a1a7a29bb9dfcdf5b01130","batch_id":"20260909-options-reload-batch-1","role":"QA","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"fb61f8672ea32ce8ab271a7420a16df2f3686a21","previous_run_id":"0fa4f448e696b26cb0be85ad6fc21060","report":"reviews/8815dae207a1a7a29bb9dfcdf5b01130/report.md"}
- {"run_id":"fa588704161fcecde278f409daf9db27","batch_id":"20260909-options-reload-batch-1","role":"PM","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"b70d56f45dd4037ae386bf04734e1cb2591d14b7","previous_run_id":"a00c77a9c5d75699770534c8ee1b1c1e","report":"reviews/fa588704161fcecde278f409daf9db27/report.md"}
- {"run_id":"e2133f7306a771b6968beb6d96719cf5","batch_id":"20260909-options-reload-batch-1","role":"PM","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"5169b6a3c2c7d26710641c5bde46723aaab7a556","previous_run_id":"fa588704161fcecde278f409daf9db27","report":"reviews/e2133f7306a771b6968beb6d96719cf5/report.md"}
- {"run_id":"e6766e1287179d13134ec523aa0679e5","batch_id":"20260909-options-reload-batch-1","role":"QA","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"5169b6a3c2c7d26710641c5bde46723aaab7a556","previous_run_id":"8815dae207a1a7a29bb9dfcdf5b01130","report":"reviews/e6766e1287179d13134ec523aa0679e5/report.md"}
- {"run_id":"cc2c7efd0bea6c62c3a423d553946677","batch_id":"20260909-options-reload-batch-1","role":"PM","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"21590e5c36d8c2f44e67fcf5cde76da4661d25bd","previous_run_id":"e2133f7306a771b6968beb6d96719cf5","report":"reviews/cc2c7efd0bea6c62c3a423d553946677/report.md"}
- {"run_id":"14a0b8b899700e1de1067e00885e42ff","batch_id":"20260909-options-reload-batch-1","role":"PM","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"46ed8a32fd8f579ef9e16c9fd103b5fea200bc72","previous_run_id":"cc2c7efd0bea6c62c3a423d553946677","report":"reviews/14a0b8b899700e1de1067e00885e42ff/report.md"}
- {"run_id":"7118318d387740c3d995c8355fbb53d5","batch_id":"20260909-options-reload-batch-1","role":"QA","execution_status":"ok","base":"211c104282f181d5fabe6cc49b03583b0bed269f","commit":"46ed8a32fd8f579ef9e16c9fd103b5fea200bc72","previous_run_id":"e6766e1287179d13134ec523aa0679e5","report":"reviews/7118318d387740c3d995c8355fbb53d5/report.md"}
