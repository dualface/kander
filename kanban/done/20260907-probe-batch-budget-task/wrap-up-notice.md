First read /home/dualf/.agents/KANDER-AGENTS.md, run kander config --json to check the current rules selection, load only enabled modules as needed, then read the command contract /home/dualf/.agents/KANDER-KANBAN-RULES.md. If configuration cannot be read, stop affected Kander operations and report the error. For disabled modules, follow existing user and project rules, not Kander requirements retained from an earlier session. Communicate with the user and write all card content, records and reports in "zh-CN". Commit messages and code comments follow the project's conventions. Task size is small (SIZE); directory form does not determine size. Follow the corresponding completion requirements. 

The card is still in review; first run kander move 20260907-probe-batch-budget-task working to move it back to working, then handle the items.

# 20260907-probe-batch-budget-task 收尾通知

这是用户已授权、代码已集成到 develop 之后的真实 wrap-up 通知。请先自行 `kander move 20260907-probe-batch-budget-task working`，然后只做收尾：不改代码、不重跑已通过的 Reviewer、不再 rebase 或集成。

## 集成证据

- 组分支：group/20260907-runtime-observation-group。本轮开始前，同组 P1/P2/E1 已完成并集成，组分支按用户明确授权重锚到 develop `7e6fe2073ff007a6639a2517f4c7874f8e420bd1`（远端先 delete 再新建同名分支，未使用 --force；重锚前实测组自身交付路径 internal/probe、internal/liveness、docs/probe-deadlines.md 与 develop 字节一致，无交付丢失）。该 SHA 即本卡创建锚点与本批审核 base。
- 本卡最终交付 `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`。**没有发生 rebase 重写，交付 SHA 前后一致，无新旧 SHA 映射。**
- 主控已 `git push origin 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5:refs/heads/group/20260907-runtime-observation-group` 接收交付，再 `merge --ff-only` 同步组工作树。
- 集成：`git push origin 021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5:refs/heads/develop` 成功（`7e6fe20..021ae63`），fetch 后主工作树 `git merge --ff-only origin/develop` 完成。本地 develop 与 origin/develop 均为 `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`，主工作树干净。`git merge-base --is-ancestor` 对本地 develop 与 origin/develop 均 exit=0。未推 main，未部署二进制，未迁移真实看板。
- 集成为直接 ff，无合并提交。

## 审核结论

- 执行审核计划 `g1-runtime-observation-p3`（sealed），单批 `g1-batch-one`，base `7e6fe2073ff007a6639a2517f4c7874f8e420bd1`，target `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5`。**修复轮数 0。**
- PM / codex，run `g1-pm-r1`，exit=0：无 gate finding，NON_BLOCKING 空。需求核对 Complete 16、Partial 0、Missing 0、Contradicted 0、Unverifiable 1（原生 Windows 与真实 tmux/herdr/Agent，已披露缺口，不单独阻断）。PM 明确说明本轮仅静态审查、未重跑测试，测试结论采用你提供的最终提交记录。
- QA / codex，run `g1-qa-r1`，exit=0：无 gate finding，NON_BLOCKING 空。
- CSA / Hacker：N/A，依据本仓库 AGENTS.md 第 7 条（第二阶段安全角色一律 N/A，不运行）。
- 两角色均无 finding，因此本卡没有 assignment 条目、没有作者处置义务；批次已通过 `kander review close` 正常闭批，状态 `closed`，原件保留在卡内 `reviews/g1-pm-r1`、`reviews/g1-qa-r1` 及批次 disposition。
- 主控独立验证（在 target `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5` 的组工作树实跑）：`go build ./...` exit=0、`go vet ./...` exit=0、`go test -count=1 ./...` exit=0（19 包全部 ok）。这是主控本次实跑结果，不冒称由你重新执行。

## 本卡未解决项

1. [验证缺口][Unverifiable] 原生 Windows 行为未运行；GOOS=windows 交叉编译与交叉构建测试二进制不等于原生实机通过。
2. [验证缺口][Unverifiable] 真实 tmux / herdr / Agent 环境未运行；假 CLI 与 Linux /proc 检查只证明相应隔离场景。
3. [能力边界][已记录] 元数据完全相同情况下的重启，仅凭 observed_at / 身份 / runtime_state 字段仍无法区分，该边界已在实现与文档中记录。

没有已确认且未修复的 must-fix 缺陷，没有被拒绝或不可验证的 gate finding（两角色 FINDINGS 均为空数组）。

## 收尾清单（请逐项执行并如实记录）

1. 先 `kander move 20260907-probe-batch-budget-task working`。
2. fetch 后确认本卡最终交付 `021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5` 已在本地与远端 develop（`git merge-base --is-ancestor`），不要用其他 SHA 判断祖先关系。
3. 前置确认后，从主工作树依次删除**你自己的**资源：任务 worktree `/home/dualf/works/kander/worktrees/probe-batch-budget`、本地分支 `probe-batch-budget`、远端分支 `origin/probe-batch-budget`。**不要删除、不要修改组工作树 `/home/dualf/works/kander/worktrees/20260907-runtime-observation-group` 或分支 `group/20260907-runtime-observation-group`**，那是主控在全组收尾后自行清理的。
4. 通过受控入口 `kander update --document spec.md --expect-revision` 补全 SUMMARY 的审核与收尾部分（角色结论、批次与轮数、最终 SHA、组分支与 develop 集成结果、上面的未解决项），保留已写的 IMPLEMENTATION 与验证原文，使用相对链接引用卡内附件；不要补造新的审核 run/batch 机器索引。
5. `kander move 20260907-probe-batch-budget-task done --result completed`，再跑定向 `kander check 20260907-probe-batch-budget-task`。
6. 按 KANDER-REPORTING-RULES.md 的 8 字段模板出一次完成报告，最后一行 `Final card state: done`。
7. 完成后结束本轮回复，保留交互 CLI 与终端容器，不要自行 dismiss 或退出进程。

如果第 2 步的前置无法确认，保留工作状态并回报主控，不要继续删除。
