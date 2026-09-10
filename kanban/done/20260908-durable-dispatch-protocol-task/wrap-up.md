# 持久派回协议收尾记录

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


收尾结果：`kander move 20260908-durable-dispatch-protocol-task done --result completed` 退出 0；`kander check 20260908-durable-dispatch-protocol-task` 退出 0，实际输出：

```text
ok: 1 tasks
```

最终交付 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`；Final card state: done。
