# 第 1 轮审核派回交付

批次 `g3-batch-one`，固定审核 base `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`，被审目标/本轮 rebase 组基线 `0b304a8399cdbc8db2c005e390afd3f6127532af`。最终修复交付 `287161673bf3f11a865f2e8a40a023a65bdc1f65`，分支 `dispatch-evidence-binding`。rebase 无冲突，已推送；组分支及 develop 由编排端负责。

## 作者处置

合并根因 PM-03/QA-03，来源 (g3-pm-r1, PM-03) 与 (g3-qa-r1, QA-03)，分别保留原件绑定；独立核实后 confirmed，现 fixed。git show 0b304a8399cdbc8db2c005e390afd3f6127532af:<path> 全部退出 0：internal/board/dispatch_snapshot.go:59-62 保留 Input.CreatedAt 并调用 dispatchAcceptBefore；internal/board/dispatch_wrapup.go:85-89 优先取 WrapUpAuthority.ConfirmBy；internal/liveness/subscribe_dispatch.go:22-38 按摘要期限计算 overdue/唤醒。该目标 docs/subscription-facts.md:43-46,55-56 及 rules/KANDER-KANBAN-RULES.md:312,314 却称原期限，原意图过期后授予新 epoch 即触发误导。修复统一当前 epoch 有效接受期限、普通意图/专用 grant 来源、created_at/age_seconds 来源与同 epoch 重试/订阅重启不续期。仅文档对齐既有行为，属 documentation；完整核实输出见 review-fix-r1-validation.txt。

PM-03/QA-03 均为 medium、mechanical documentation，同一根因各保留独立作者记录：

- [reviews/g3-pm-r1/dispositions/binding-r1-pm-03.json](reviews/g3-pm-r1/dispositions/binding-r1-pm-03.json)
- [reviews/g3-qa-r1/dispositions/binding-r1-qa-03.json](reviews/g3-qa-r1/dispositions/binding-r1-qa-03.json)

原报告分别见 [PM](reviews/g3-pm-r1/report.md)、[QA](reviews/g3-qa-r1/report.md)。NON_BLOCKING 均为空。本卡无 rejected/unverifiable 或未修复 finding；其他两组根因由 coordinator-recovery 卡负责，本执行端未代判。

## 最终验证

最终提交 287161673bf3f11a865f2e8a40a023a65bdc1f65：go build ./...、go vet ./... 均退出 0；go test -json -count=1 ./...：19 包、1001 测试项通过，1 项跳过（TestWindowsConsoleLauncher）；go test -race -json -count=1 ./internal/board ./internal/liveness：2 包、491 测试项通过，0 项跳过；测试统计含子用例，两条测试命令均退出 0。TestDispatchWrapUpSnapshotUsesCurrentGrantDeadline 通过，覆盖原意图已过期、新 grant 期限仍有效且原期限不变。逐句对照 snapshotDispatch、dispatchAcceptBefore、summarizeDispatch、dispatchWait 和 docs/durable-dispatch.md:97；本轮仅 docs/subscription-facts.md 与 rules/KANDER-KANBAN-RULES.md，8 行增加、6 行删除。

命令输出、包级结果与目标源文件证据见 [review-fix-r1-validation.txt](review-fix-r1-validation.txt)。原生 Windows、真实 tmux/herdr/Agent 交互缺口沿用首轮，不能记为实机通过。本轮未新增或复跑交叉编译。

## 交付自检

1. 最终 SHA `287161673bf3f11a865f2e8a40a023a65bdc1f65`：git diff --check 0b304a8399cdbc8db2c005e390afd3f6127532af HEAD 退出 0、无输出；修改路径冲突标记扫描 0。
2. 同 SHA：git diff --name-only 仅两个 Markdown 文件，代码物理行数门禁无新增适用项。
3. 同 SHA：逐句核对中英文订阅定义与 board/liveness 代码及持久派回文档；已补齐首轮遗漏。
4. 同 SHA：仅文档变更，无新增或遗留待删除代码；go vet ./... 退出 0。
5. 同 SHA：无测试改动，无新增重复或无关断言。
6. 同 SHA：构建、vet 和 board/liveness race 结果如上，专用 grant 快照回归通过。
7. 同 SHA：全量命令、包与测试项数量如上；没有复用先前提交结果。

PM/QA 的组审核后续由编排端处理；本次仅作者机械修复核验，不声称组审核通过。CSA/Hacker N/A，依据仓库 AGENTS.md。保留任务 worktree、分支与交互 CLI，进入 review 等待接收。
