First read /home/dualf/.agents/KANDER-AGENTS.md, run kander config --json to check the current rules selection, load only enabled modules as needed, then read the command contract /home/dualf/.agents/KANDER-KANBAN-RULES.md. If configuration cannot be read, stop affected Kander operations and report the error. For disabled modules, follow existing user and project rules, not Kander requirements retained from an earlier session. Communicate with the user and write all card content, records and reports in "zh-CN". Commit messages and code comments follow the project's conventions. Task size is small (SIZE); directory form does not determine size. Follow the corresponding completion requirements. 

The card is still in review; first run kander move 20260907-subscription-dispatch-facts-task working to move it back to working, then handle the items.

# 20260907-subscription-dispatch-facts-task 收尾通知

这是用户已授权、代码已集成到 develop 之后的真实 wrap-up 通知。请先自行 `kander move 20260907-subscription-dispatch-facts-task working`，然后只做收尾：不改代码、不重跑已通过的 Reviewer、不再 rebase 或集成。

## 本卡交付

- 最终交付 `6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`，任务分支 `subscription-dispatch-facts`。本卡在本批次**没有分到任何 finding**，也没有修复提交。

## 组集成证据

- 组分支 `group/20260908-bindings-recovery-group`，创建锚点 `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`（当时 develop）。交付按 ff 依次接收：`6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa`（subscription-dispatch-facts）→ `6f5ed32072275c3bb9d96c9e40bd606b949b9c17`（dispatch-evidence-binding）→ `0b304a8399cdbc8db2c005e390afd3f6127532af`（coordinator-recovery）→ 第 1 轮修复 `287161673bf3f11a865f2e8a40a023a65bdc1f65`（dispatch-evidence-binding）与 `5845e6fd0f2b503313030349fa211a7791a50169`（coordinator-recovery，含 `4a55248377c8fa35edddd862acee3473d877b2e5`）→ 第 2 轮修复 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`（coordinator-recovery）。全程直接 ff，无合并提交，无同步派回，**没有任何交付被 rebase 重写，不存在新旧 SHA 映射**。
- 最终组 HEAD `26edb64fcfb654a96afedc30bbaea27e5e918ec7` 已 `git push origin 26edb64...:refs/heads/develop` 成功（`ec6fdb4..26edb64`，首次尝试遇 TLS 握手中断，重试成功），fetch 后主工作树 `git merge --ff-only origin/develop` 完成。本地 develop 与 origin/develop 均为 `26edb64cebae...`（完整值 `26edb64fcfb654a96afedc30bbaea27e5e918ec7`），主工作树干净，双向 `merge-base --is-ancestor` 均 exit=0。未推 main，未部署二进制，未迁移真实看板。
- develop 在本组审核期间没有前进，组分支以 `ec6fdb4` 为祖先可直接 ff，因此没有集成 rebase。

## 审核结论（批次 g3-batch-one，2 个修复轮）

- 执行审核计划 `g3-bindings-recovery`（sealed），单批 `g3-batch-one`，base `ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3`，target 经两次 `review advance` CAS 推进：`0b304a8` → `5845e6f` → `26edb64fcfb654a96afedc30bbaea27e5e918ec7`。批次已 `review close`，三张成员卡 `review progress` 均为 `closed`。
- 首轮 PM `g3-pm-r1` 与 QA `g3-qa-r1` 各 3 条，两两同根因共 3 个根因：PM-02/QA-01（编排执行周期，high）、PM-01/QA-02（闭批后历史对账，high）、PM-03/QA-03（订阅文档 confirm_by 语义，medium，`[mechanical: documentation]`）。
- 第 1 轮修复后，PM 增量 `g3-pm-r2` 在 `5845e6f` 上 0 finding、0 非阻断；QA 增量 `g3-qa-r2` 确认三项关闭，同时报出**由该轮修复新引入**的 QA-04（medium，启动失败回滚后编排周期游标被永久污染），归 coordinator-recovery。
- 第 2 轮修复后，QA 增量 `g3-qa-r5` 在最终 target 上判定 QA-04 关闭且无新缺陷，0 finding、0 非阻断。PM 已通过且该轮修复未改变其关注的功能契约，按「已通过的角色不因其他角色的修复而重跑」承接结论；最终 target 是 PM 通过提交的后代。
- 闭批选用 PM `g3-pm-r2`（passed_at `5845e6f`）与 QA `g3-qa-r5`（passed_at `26edb64`）。`g3-qa-r3`（failed）与 `g3-qa-r4`（interrupted）均因本机低内存杀手结束主控后台任务而连带中断审核门禁进程，工具按协议归档并经同 ID 恢复补齐三卡发布，未重跑 Reviewer、未伪造报告；闭批 `resolved_failures` 已把两者显式指向成功替代 `g3-qa-r5`。这是环境中断，不是 Reviewer 持续性后端失败，也未产生语义结论，不计修复轮。
- CSA / Hacker：N/A，依据本仓库 `AGENTS.md` 第 7 条。
- 全部 7 个来源 finding（4 个根因）都由原作者 confirmed 后 fixed，无 rejected、无 unverifiable。所有 run 的 NON_BLOCKING 均为显式空数组，本批没有非门禁条目。
- 主控独立验证：在最终 target `26edb64fcfb654a96afedc30bbaea27e5e918ec7` 的组工作树实跑 `go build ./...`、`go vet ./...`、`go test -count=1 ./...`，三项均 exit=0，19 个包全部 ok。这是主控本次实跑结果，不冒称由你重新执行。

## 全组保留的验证缺口

1. [验证缺口][Unverifiable] 原生 Windows 未运行；GOOS=windows 交叉编译与交叉构建测试二进制不等于原生实机通过。作者全量测试中唯一的 skip 即原生 Windows 控制台用例（TestWindowsConsoleLauncher）。
2. [验证缺口][Unverifiable] 真实 tmux / herdr / Agent 环境未运行；假 CLI 与本机隔离测试只证明相应隔离场景。

## 收尾清单（请逐项执行并如实记录）

1. 先 `kander move 20260907-subscription-dispatch-facts-task working`。
2. fetch 后确认本卡最终交付 `6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa` 已在本地与远端 develop（`git merge-base --is-ancestor`）。本卡交付没有被 rebase 重写，直接用该 SHA 判断即可。
3. 前置确认后，从主工作树依次删除**你自己的**资源：任务 worktree `/home/dualf/works/kander/worktrees/subscription-dispatch-facts`、本地分支 `subscription-dispatch-facts`、远端分支 `origin/subscription-dispatch-facts`。**不要删除或修改组工作树 `/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group` 或分支 `group/20260908-bindings-recovery-group`**，那由主控在全组收尾后清理。
4. 通过受控入口 `kander update --document spec.md --expect-revision`（大卡也可写 report.md）补全审核与收尾记录：角色结论、批次与修复轮数、最终 SHA、组分支与 develop 集成结果、上面的未解决项。保留已写的 IMPLEMENTATION 与验证原文，用相对链接引用卡内附件；不要补造新的审核 run/batch 机器索引。
5. `kander move 20260907-subscription-dispatch-facts-task done --result completed`，再跑定向 `kander check 20260907-subscription-dispatch-facts-task`。
6. 按 KANDER-REPORTING-RULES.md 的 8 字段模板出一次完成报告，最后一行 `Final card state: done`。
7. 完成后结束本轮回复，保留交互 CLI 与终端容器，不要自行 dismiss 或退出进程。

如果第 2 步的前置无法确认，保留工作状态并回报主控，不要继续删除。
