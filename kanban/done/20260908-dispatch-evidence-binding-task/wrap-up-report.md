# 收尾与完成记录

- Task: [20260908-dispatch-evidence-binding-task - 派回绑定审核原件、作者处置与隔离后的收尾授权](spec.md)
- Delivery: fix 派回绑定审核原件与原作者处置；wrap-up 绑定已验证集成证据及隔离后的专用授权；订阅期限中英文文档已一致。
- Acceptance: 7/7 作者自检及适用审核完成；各行为对应证据见 [首轮报告](report.md) 与 [审核修复报告](review-fix-r1.md)。平台和真实端到端测试缺口如实保留。
- Verification: 作者最终交付 287161673bf3f11a865f2e8a40a023a65bdc1f65：go build ./...、go vet ./... 退出 0；go test -json -count=1 ./...：19 包、1001 测试项通过，1 项 TestWindowsConsoleLauncher 跳过；go test -race -json -count=1 ./internal/board ./internal/liveness：2 包、491 测试项通过，0 skip；测试统计含子用例，均 exit=0。详见 review-fix-r1.md 与 review-fix-r1-validation.txt。主控在组最终提交 26edb64fcfb654a96afedc30bbaea27e5e918ec7 实跑 go build ./...、go vet ./...、go test -count=1 ./...，均 exit=0、19 包全部 ok（来源：闭批 request.opinions；不是本执行端本轮重跑）。本轮本执行端 fetch、Git 祖先/主工作树同步检查与 review progress 已实跑通过。
- Review: 批次 g3-batch-one，sealed 计划 g3-bindings-recovery，2 个修复轮，已闭批。PM（codex）g3-pm-r2 在 5845e6fd0f2b503313030349fa211a7791a50169 PASS，最终 target 为其后代，按已通过角色承接规则保留；QA（codex）g3-qa-r5 在最终 target PASS。CSA/Hacker N/A，依据仓库 AGENTS.md。首轮 6 个来源条目合为 3 个根因，第一修复轮新增 QA-04 后共 7 个来源、4 个根因，均由原作者核实并 fixed，无 rejected/unverifiable finding，非阻断数组为空。本卡 PM-03/QA-03 同根文档问题处置原件保留。g3-qa-r3 failed 与 g3-qa-r4 interrupted 属主控报告的低内存环境中断，闭批 resolved_failures 均指向成功替代 g3-qa-r5，不算语义结论或额外修复轮。 原件见 [PM](reviews/g3-pm-r2/report.md)、[QA](reviews/g3-qa-r5/report.md)、[闭批记录](reviews/batches/g3-batch-one/closed.json)。
- Wrap-up: 本卡最终交付 287161673bf3f11a865f2e8a40a023a65bdc1f65；组分支 group/20260908-bindings-recovery-group 最终 26edb64fcfb654a96afedc30bbaea27e5e918ec7 已进入本地及远端 develop。fetch 后对 develop、origin/develop 各执行 git merge-base --is-ancestor 287161673bf3f11a865f2e8a40a023a65bdc1f65，均 exit=0；develop 与 origin/develop 双向祖先检查均 exit=0，主工作树 HEAD 相同且干净。组接收和 develop 集成全程 ff，本卡最终交付 SHA 未在集成期间改写，无新旧 SHA 映射。本执行端仅验证、未重新集成/rebase。已从主工作树依次删除本卡 worktree、本地分支和远端分支，三条命令均 exit=0；路径及本地/远端跟踪 refs 不存在。组资源留给主控处理。无本执行端单独启动的临时 Reviewer 运行目录需要清理，卡内审核原件全部保留。交互 CLI 与终端容器保留。 受控 move done --result completed 退出 0；随后定向 kander check 退出 0，输出 ok: 1 tasks；适用收尾全部完成。[本轮命令证据](wrap-up-verification.txt)。
- Unresolved issues (2):

1. [验证缺口][Unverifiable] 原生 Windows 未运行；影响：未验证原生控制台/平台行为；交叉编译不等于实机通过。
2. [验证缺口][Unverifiable] 真实 tmux/herdr/Agent 端到端场景未运行；影响：实机交互仍未验证，假 CLI 与隔离测试只覆盖对应隔离场景。

- Summary: 实现、审核、集成和任务资源清理完成；Code branch: develop；Final card state: done
