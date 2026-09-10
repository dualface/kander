# Durable dispatch protocol: intents, atomic receipts and uncertain-delivery reconciliation

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260908-subscription-dispatch-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 00:29
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t14:wX:p1P
- STARTED_AT: 2026-09-08 02:33
- FINISHED_AT: 2026-09-08 04:06
- TASK_BRANCH: durable-dispatch-protocol
- RESULT: completed

## GOAL

持久派回协议：派回意图可按稳定 ID 幂等创建与读取，同一 dispatch 的接受与完成可原子持久证明，过期执行授权不能覆盖当前任务；notify/resume 发送前绑定持久意图，不确定投递按同 ID 对账重试，终端回显不再代表已开工。

## USER_DECISIONS

2026-09-07 用户在侧会话要求按单一可验收行为重拆剩余卡，并答复"我授权"允许独立审卡、启动及按计划完成组交付/审核/develop 集成与收尾；原四张大卡由拆分卡替代，原契约与审核历史保留。

2026-09-08 用户在主会话决定："对 todo 里的卡片进行整合. 单一目标的卡片合并到一去. 只有能够并行的才拆分." 本卡即按此决定合并而成；被合并的原卡归档为 duplicate 并指向本卡，原契约、独立审卡记录及覆盖矩阵保留不改写。合并不扩大目标，不改变 review 中 P1/P2/E1 及其组分支，不扩大集成/接管授权。

## EXPECTED_OUTCOME

本卡交付：board 中的 dispatch 类型与受控存储、move working/review/done 的原子回执与 epoch 校验、以及 actionable notify/resume 的 --dispatch-id 接入；重启不丢失身份与期限，同 ID 重试先查询回执而不盲发第二轮，旧执行端无法提交伪完成。

## ACCEPTANCE_CRITERIA

- [ ] board 纯类型定义 dispatch_id、task、fix/sync/wrap-up、消息哈希、交付/审核引用、创建及确认期限、revision，状态含 prepared/delivery-unknown/accepted/completed/failed/cancelled。
- [ ] 受控创建/读取同 ID 同输入幂等，不同 payload/任务/基线冲突；省略 ID 在任何发送前生成并输出；deadline 重启不重置，引用按任务 ID/相对 artifact 定位；原件写入复用 S 且 board 不依赖 notify；prepared 或传输 ACK 不视为 accepted。
- [ ] move working --dispatch-id 原子接受，move review/done 同 ID 及交付/处置引用原子完成，状态/revision/回执同事务，不另设冲突 status。
- [ ] 每卡仅一个有效执行授权；epoch/CAS 由受控入口检查，迟到/旧 epoch 的正文、WINDOW、回执写入拒绝，不能锁住整个 Agent 会话；旧未绑定模式不补造历史接受。
- [ ] 覆盖同 ID 重复、错 payload、旧 epoch、接受/完成边界 kill、快速 review-working-review/done；恢复后同 ID 记录唯一且新内容保留，不承诺任意外部副作用 exactly-once。意图创建边界 kill 恢复后同 ID 唯一、deadline 不重置，创建/重复/冲突/kill 恢复测试通过。
- [ ] actionable notify 接入 --dispatch-id/kind，发送前 prepared；发送边界崩溃保留 delivery-unknown；同 ID 重试先查询回执，不盲建新轮，不重复启动未知执行者。
- [ ] 消息提示使用本卡受控回执入口；终端 marker 仅作传输诊断。generic working 消息/无组卡兼容明确，旧通知无 ID 不造历史业务回执。
- [ ] notify/resume 消费 P3 预算及 unknown/stopped；更换 Agent 仍需用户授权；所有 WINDOW/正文回写走 S 和 epoch，只撤销自己未被新执行轮消费的写入。
- [ ] 反转 PromptEchoCountsAsAcknowledgement；覆盖发送前后 kill、回执先于 notify 返回、同 ID 恢复、超时、失败回滚+move 并发。
- [ ] 本卡全部行为的测试、必要文档及三语消息随实现交付；go test ./...、受影响包 -race、build/vet/格式检查通过。平台相关测试使用临时目录，原生 Windows 及真实 tmux/herdr/Agent 未执行时列明缺口，不把假 CLI 或交叉编译记为实机通过。PM/QA 按适用门禁，CSA/Hacker 在本仓库 N/A。

## THREAT_MODEL

防崩溃、迟到写入、错误探测和不完整事实造成误判断；外部输出是数据。仅使用受控入口，不修改真实看板以做测试，不扩大接管/集成/删除授权。

## OUT_OF_SCOPE

- dispatch schema/存储、board 受控回执/授权、notify/resume 投递恢复；不实现 wrap-up-only 代办授权与 R finding 引用校验（由 20260908-dispatch-evidence-binding-task 交付）、不实现订阅端 dispatch 事实消费（由 20260907-subscription-dispatch-facts-task 交付）、不做编排检查点或恢复实例（由 20260908-coordinator-recovery-task 交付）。
- 不引入通用服务、签名、保留期清理或无关优化。

## DISCUSSION

```text
PREREQUISITES: 20260907-card-write-transactions-task,20260907-probe-batch-budget-task
```

依赖必要性：S 提供按 ID 事务/CAS 和原件路径；P3 提供不误恢复且有界的探测输入，notify/resume 的投递恢复必须消费它。两者均为组外卡，须 done 且交付在 develop 可用。意图存储与回执部分技术上只需 S，但整卡一次交付，启动以两者齐备为准。

合并理由：原 N1（20260907-dispatch-intent-store-task）、N2（20260907-dispatch-atomic-receipts-task）、N3（20260907-notify-durable-delivery-task）是同一"持久派回协议"目标的严格串行链（N1→N2→N3），没有并行收益，按用户 2026-09-08 决定合并。三卡行为验收原文保留，N1 第三项"本卡不发送消息"随合并改写为"prepared/传输 ACK 不视为 accepted"。

并行边界：本卡修改 notify/board，与同组两张 subscribe 卡资源隔离，可并行。

完成证据：以上可执行场景必须断言行为结果；原缺陷复现要反转断言。测试、文档是本卡交付的一部分，不单独拆成尾部测试任务。合并后各行为可分次提交，但整卡一次交付、一次验收；实现中发现超出本卡目标的独立新行为时另卡，不为避免拆卡扩大契约。

历史来源：20260907-durable-dispatch-task（原 N 验收 1/2/3/4/5/6/7/9/11/12）；拆分历史见 20260907-probe-result-classification-task 的 resplit-plan.md，合并方案见本卡目录 consolidation-plan.md。原卡 CARD_REVIEW 不继承。

SELF_REVIEW: 已核对目标单一（持久派回协议）、九项行为验收与原 N1/N2/N3 一一对应且无自相矛盾、两个前置为真实技术依赖且无环、与后继三卡边界互斥。待独立审卡。

CARD_REVIEW: PASS — 2026-09-08，独立子Agent（全新会话，只读整合后 6 张卡、12 张被合并原卡、resplit-plan.md 覆盖矩阵与 consolidation-plan.md）：目标单一且与用户 2026-09-08 决定一致，合并后验收完整覆盖原卡且可判定，前置存在、必要、无环且组内外分类正确，OUT_OF_SCOPE 与同组卡互斥。无阻断；非阻断建议（意图创建边界 kill 场景、保留“英文发布规则”字样、coordinator 第 8 项改为消费措辞、方案中 P3 并行表述）已由建卡者采纳修订。仅契约审查，不代表实现/PM/QA 通过。

## IMPLEMENTATION

首轮交付（历史）：`c5dcfbc401d5748da7eb0c89011ea422f51c33cc`，源组基线 `003e5fecf4048d8da8151d0431ea1cd040912a36`，创建基线 `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`；原实现与验证保留在 report-r0.md、validation.txt，不能替代本轮证据。

### 2026-09-08 审核派回第 1 轮（g2-batch-one）

独立核实目标 `ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe` 的真实路径；g2-pm-r1 与 g2-qa-r1 共 6 个来源、5 个根因全部 confirmed 后 fixed，无 rejected/unverifiable。PM-004 与 QA-002 合并根因并分别提交来源绑定记录，原件见各自 `reviews/<run_id>/`。具体作者记录均为 `dispositions/durable-r1-<角色与编号>.json`，完整路径和证据见 report.md。

| 来源 finding | 处置 | rebase 后修复提交 | 结果 |
| --- | --- | --- | --- |
| PM-001 | fixed | `58023dff85b89e0f5f37f185f78f0a33db11ddc3` | 期限耗尽仍无回执返回 pending |
| PM-002、PM-003 | fixed | `e40a98570b49b55395c2d5d6b2d9e516f10ce664` | 保留原选项值边界，并统一规范任务 ID |
| QA-001 | fixed | `cba5231c4a4a1c78c916c1d3d9f62de5b97c7111` | 固定原授权，busy 重试拒绝借用新轮 WINDOW 游标 |
| PM-004 / QA-002（同一根因） | fixed | `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3` | 注释明确发送前回滚条件与发送后保留资源 |


改动仅涉及 launch/notify 的既有接口实现及文档/回归，公共签名未变；参数字面值与 .md 别名兼容得到恢复。固定投递授权，期限耗尽后重读回执且无接受则 pending，继续保留未知执行资源。修复前 4 组回归均失败，原输出见 validation-r1.txt；目标提交与修复前相关模块的 `git diff --exit-code` 无差异。

按关注点提交后无冲突 rebase 到最新组分支 `ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe`；最终交付 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3` 已推送同名远端，`git rev-parse HEAD origin/durable-dispatch-protocol` 两行相同，源组祖先检查退出 0（验证提交 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`）。工作区 `/home/dualf/works/kander/worktrees/durable-dispatch-protocol`，任务分支 `durable-dispatch-protocol`。

#### Delivery Self-Check

1. `git diff --check ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe HEAD` 无输出、退出 0；逐文件扫描冲突标记 0 个。最终提交 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。
2. `git diff --name-only ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe HEAD` 所列 Go 文件用 Python `splitlines` 统计：8 个、最多 315 行，均不超过 1000 行。最终提交 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。
3. `git diff ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe HEAD` 逐段检查文档及注释：期限文档同步，NotifyViaResume 注释已按真实失败路径修正。上一轮该项自检遗漏，由 PM-004/QA-002 纠正。最终提交 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。
4. `git diff ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe HEAD` 核对调用路径，`go vet ./...` 退出 0：新代码进入现有 notify/resume 流程，无新增未调用接口或已知死代码。最终提交 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。
5. `rg -n '^func Test' internal/launch/dispatch*_test.go internal/notify/dispatch*_test.go` 并逐项核对：期限/参数值边界/授权各覆盖独立行为；.md 复用既有用例，注释未增加重复测试，未发现无关断言。最终提交 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。
6. `go build ./...`、`go vet ./...`、`GOOS=windows GOARCH=amd64 go build ./...` 均退出 0；`gofmt -l` 8 个变更 Go 文件无输出。最终提交 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。
7. `go test -json -count=1 ./...`：19 包、922 个测试/子测试通过，1 跳过、0 失败；`go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window`：4 包、400 个测试/子测试通过，1 跳过、0 失败；两条均退出 0。最终提交 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。


本轮没有以主控先前测试替代作者测试。PM/QA 增量复审仍待主控，CSA/Hacker N/A；唯一跳过为原生 Windows 控制台用例，真实 Windows/tmux/herdr/Agent 验证缺口继续保留。进入 review 后保留 CLI、分支与工作区，等待主控派回。

### 2026-09-08 集成后收尾

收到主控的真实收尾通知后自行 move working。本轮不改代码、不重跑 Reviewer、不 rebase 或再次集成。最终交付及组 HEAD 均为 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`；此次 develop 集成无 SHA 重写，首轮到修复的历史记录保留。

审核计划 `g2-subscription-dispatch` sealed，批次 `g2-batch-one` 已闭合，1 个修复轮。PM/codex 的 [g2-pm-r2 原件](reviews/g2-pm-r2/report.md) 与 QA/codex 的 [g2-qa-r3 原件](reviews/g2-qa-r3/report.md) 均无新 finding，passed_at 均为 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。6 个来源 finding 共 5 个根因已全部 fixed，PM-004/QA-002 同根因合并。CSA/Hacker 按仓库 AGENTS 特例 N/A。[闭批原件](reviews/batches/g2-batch-one/closed.json) 绑定最终 target `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`；本轮 `kander review progress /home/dualf/works/kander/worktrees/20260908-subscription-dispatch-group 20260908-durable-dispatch-protocol-task` 实跑返回 closed。

`g2-qa-r2` 保留 failed 原件：主控报告本机内存压力导致外部终止；闭批的 resolved_failures 明确指向成功替代 `g2-qa-r3`，不将失败改写成 PASS。原件及作者处置均保留，没有新增或修改 run/batch 机器索引。

本轮 Git 实核：fetch 成功；`git rev-parse develop origin/develop durable-dispatch-protocol origin/durable-dispatch-protocol` 清理前四行均为 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`；`git merge-base --is-ancestor ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 develop` 与同命令目标 `origin/develop` 均退出 0。主工作树及本卡工作树清理前 `git status --porcelain` 均为空。由此确认主控已把本卡交付 ff 集成到 develop，并同步主工作树。代码归属 develop。

从主工作树依次执行 `git worktree remove /home/dualf/works/kander/worktrees/durable-dispatch-protocol`、`git branch -d durable-dispatch-protocol`、`git push origin --delete durable-dispatch-protocol`，全部退出 0；路径不存在、本地分支查询为空、`git ls-remote --heads origin refs/heads/durable-dispatch-protocol` 为空。组 worktree/分支由主控负责，本轮未操作。交互 CLI 与终端容器保留，未 dismiss。审核原件不是临时清理对象，完整保留。

验证沿用作者在最终提交 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3` 上真实执行的 [validation-r1.txt](validation-r1.txt)：`go test -json -count=1 ./...` 19 包、922 测试/子测试通过、1 跳过；`go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window` 4 包、400 项通过、1 跳过，均 0 失败。build/vet/格式/Windows 交叉构建证据同附件。主控另在相同 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3` 实跑 build/vet/`go test -count=1 ./...` 19 包全部 ok，此为主控证据，不冒称本轮作者重跑。

验收：10/10 契约项完成；第 10 项按契约明确披露平台证据缺口，不视为实机通过。未解决项 2：

- [验证缺口][Unverifiable] 原生 Windows 未运行；无法证明原生锁/DACL/reparse/进程回收，交叉构建与唯一跳过的 Windows 控制台用例不能替代。
- [验证缺口][Unverifiable] 真实 tmux/herdr/Agent 联调未运行；假 CLI、本机隔离和进程中断测试仅覆盖相应测试场景。

收尾记录完成后执行 move done --result completed，再运行定向 check；实际命令结果另补在本记录之后。


## SUMMARY

持久派回协议完成，10/10 契约项已交付；代码在 develop，最终提交 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。PM `g2-pm-r2`、QA `g2-qa-r3` PASS，CSA/Hacker N/A，`g2-batch-one` 经过 1 个修复轮已 closed。作者在此提交的 `go test -json -count=1 ./...` 922 项通过、1 跳过；受影响四包 race 400 项通过、1 跳过（完整命令见 [最终验证](validation-r1.txt)）。本轮已核验本地及远端 develop 祖先关系、主工作树同步，并删除本卡工作树与两端任务分支。保留 2 项 Unverifiable：原生 Windows、真实 tmux/herdr/Agent。审核与清理细节见 [收尾记录](wrap-up.md)，原 IMPLEMENTATION 和验证证据保留。

## REVIEWS

- {"run_id":"g2-qa-r1","batch_id":"g2-batch-one","role":"QA","execution_status":"ok","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe","report":"reviews/g2-qa-r1/report.md"}
- {"run_id":"g2-pm-r1","batch_id":"g2-batch-one","role":"PM","execution_status":"ok","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe","report":"reviews/g2-pm-r1/report.md"}
- {"run_id":"g2-pm-r2","batch_id":"g2-batch-one","role":"PM","execution_status":"ok","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","previous_run_id":"g2-pm-r1","report":"reviews/g2-pm-r2/report.md"}
- {"run_id":"g2-qa-r2","batch_id":"g2-batch-one","role":"QA","execution_status":"failed","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","previous_run_id":"g2-qa-r1","report":"reviews/g2-qa-r2/output.raw"}
- {"run_id":"g2-qa-r3","batch_id":"g2-batch-one","role":"QA","execution_status":"ok","base":"021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5","commit":"ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3","previous_run_id":"g2-qa-r1","report":"reviews/g2-qa-r3/report.md"}
