First read /home/dualf/.agents/KANDER-AGENTS.md, run kander config --json to check the current rules selection, load only enabled modules as needed, then read the command contract /home/dualf/.agents/KANDER-KANBAN-RULES.md. If configuration cannot be read, stop affected Kander operations and report the error. For disabled modules, follow existing user and project rules, not Kander requirements retained from an earlier session. Communicate with the user and write all card content, records and reports in "zh-CN". Commit messages and code comments follow the project's conventions. Task size is large (SIZE); directory form does not determine size. Follow the corresponding completion requirements. 

The card is still in review; first run kander move 20260908-subscription-bounded-runtime-task working to move it back to working, then handle the items.

# 20260908-subscription-bounded-runtime-task 收尾通知

这是用户已授权、代码已集成到 develop 之后的真实 wrap-up 通知。请先自行 `kander move 20260908-subscription-bounded-runtime-task working`，然后只做收尾：不改代码、不重跑已通过的 Reviewer、不再 rebase 或集成。

## 本卡交付

- 最终交付 `ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe`，任务分支 `subscription-bounded-runtime`。**同步派回时发生过 rebase 重写，映射为 `14c8fe627903de0db420a66a931db4906b599c05` → `d360f92240961a5883c0f535a6dfc15e65b80350`、`55aa8f00f408256583ed4261070beba3e437ccad` → `ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe`。判断祖先关系必须用重写后的 SHA，不能用 `55aa8f0`。** 本卡在本批次没有分到任何 finding。

## 组集成证据

- 组分支 `group/20260908-subscription-dispatch-group`，创建锚点 `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`（当时 develop）。三卡交付按 ff 依次接收：`003e5fecf4048d8da8151d0431ea1cd040912a36`（subscription-facts）→ `c5dcfbc401d5748da7eb0c89011ea422f51c33cc`（durable-dispatch-protocol）→ `ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe`（subscription-bounded-runtime，rebase 后）→ `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`（durable-dispatch-protocol 的审核修复）。全程直接 ff，无合并提交。
- 最终组 HEAD `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3` 已 `git push origin ec6fdb4...:refs/heads/develop` 成功（`021ae63..ec6fdb4`），fetch 后主工作树 `git merge --ff-only origin/develop` 完成。本地 develop 与 origin/develop 均为 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`，主工作树干净，双向 `merge-base --is-ancestor` 均 exit=0。未推 main，未部署二进制，未迁移真实看板。
- 组分支 rebase：本次 develop 在审核期间没有前进，组分支以 `021ae63` 为祖先可直接 ff，因此没有集成 rebase、没有 SHA 重写。

## 审核结论（批次 g2-batch-one，1 个修复轮）

- 执行审核计划 `g2-subscription-dispatch`（sealed），单批 `g2-batch-one`，base `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`，首轮 target `ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe`，经 `review advance` CAS 推进到最终 target `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。批次已 `review close`，三张成员卡 `review progress` 均为 `closed`。
- PM / codex：首轮 `g2-pm-r1` 提出 PM-001..PM-004 共 4 条 medium（其中 PM-004 标 `[mechanical: documentation]`）；增量 `g2-pm-r2` 在推进后的目标上 FINDINGS/NON_BLOCKING 均为空数组。闭批选用 `g2-pm-r2`，passed_at `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`。
- QA / codex：首轮 `g2-qa-r1` 提出 QA-001、QA-002 共 2 条 medium；增量 `g2-qa-r3` 逐项判定两条 closed，FINDINGS/NON_BLOCKING 均为空数组。闭批选用 `g2-qa-r3`，passed_at 同上。
- `g2-qa-r2` 为一次被本机内存压力从外部杀死的增量调用，工具按协议归档 `execution_status=failed`，随后同 ID 恢复把失败证据补齐发布到三张卡，未重跑 Reviewer、未伪造报告。闭批 `resolved_failures` 已把 `g2-qa-r2` 显式指向成功替代 `g2-qa-r3`。这是环境中断，不是 Reviewer 持续性后端失败。
- CSA / Hacker：N/A，依据本仓库 `AGENTS.md` 第 7 条（第二阶段安全角色一律 N/A，不运行）。
- 6 个来源 finding（5 个根因，PM-004 与 QA-002 同根因合并）全部由 `20260908-durable-dispatch-protocol-task` 的原作者 confirmed 后 fixed，无 rejected、无 unverifiable。所有 run 的 NON_BLOCKING 均为显式空数组，本批没有非门禁条目。
- 主控独立验证：在最终 target `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3` 的组工作树实跑 `go build ./...`、`go vet ./...`、`go test -count=1 ./...`，三项均 exit=0，19 个包全部 ok。这是主控本次实跑结果，不冒称由你重新执行。

## 全组保留的验证缺口

1. [验证缺口][Unverifiable] 原生 Windows 未运行；GOOS=windows 交叉编译与交叉构建测试二进制不等于原生实机通过。作者全量测试中唯一的 skip 即原生 Windows 控制台用例。
2. [验证缺口][Unverifiable] 真实 tmux / herdr / Agent 环境未运行；假 CLI 与本机隔离测试只证明相应隔离场景。

## 收尾清单（请逐项执行并如实记录）

1. 先 `kander move 20260908-subscription-bounded-runtime-task working`。
2. fetch 后确认本卡最终交付 `ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe` 已在本地与远端 develop（`git merge-base --is-ancestor`）。本卡交付曾被 rebase 重写，务必用上面映射后的 SHA `ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe` 判断，不要用重写前的 `55aa8f00f408256583ed4261070beba3e437ccad`。
3. 前置确认后，从主工作树依次删除**你自己的**资源：任务 worktree `/home/dualf/works/kander/worktrees/subscription-bounded-runtime`、本地分支 `subscription-bounded-runtime`、远端分支 `origin/subscription-bounded-runtime`。**不要删除或修改组工作树 `/home/dualf/works/kander/worktrees/20260908-subscription-dispatch-group` 或分支 `group/20260908-subscription-dispatch-group`**，那由主控在全组收尾后清理。
4. 通过受控入口 `kander update --document spec.md --expect-revision`（大卡也可写 report.md）补全审核与收尾记录：角色结论、批次与修复轮数、最终 SHA、组分支与 develop 集成结果、上面的未解决项。保留已写的 IMPLEMENTATION 与验证原文，用相对链接引用卡内附件；不要补造新的审核 run/batch 机器索引。
5. `kander move 20260908-subscription-bounded-runtime-task done --result completed`，再跑定向 `kander check 20260908-subscription-bounded-runtime-task`。
6. 按 KANDER-REPORTING-RULES.md 的 8 字段模板出一次完成报告，最后一行 `Final card state: done`。
7. 完成后结束本轮回复，保留交互 CLI 与终端容器，不要自行 dismiss 或退出进程。

如果第 2 步的前置无法确认，保留工作状态并回报主控，不要继续删除。
