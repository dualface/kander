# Kanban Task Completion Report

- Task: [20260908-subscription-facts-task — 订阅按已提交 revision 与成员事实报告变化](spec.md)
- Delivery: 订阅报告已提交 revision、同状态任务更新及动态监听组成员；不完整成员事实停止依赖放行，协调读取支持有界锁等待。
- Acceptance: 7/7；逐项自检、七项交付自检及适用审核门禁完成。原生 Windows 和真实终端两类环境验证缺口保留。
- Verification: 作者在 003e5fecf4048d8da8151d0431ea1cd040912a36 执行 go test -json -count=1 ./...（19 包，550 顶层测试通过，含子用例 821 pass / 1 skip）、四包 race（190 顶层测试，含子用例 385 pass）、定向测试、build/vet/格式及 Windows 交叉编译，均通过；[原始验证](verification.txt)。协调者在 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 实跑 build/vet/go test -count=1 ./...（19 包全部 ok）通过，本轮执行端未重跑。收尾时独立 fetch、祖先核验和 done 后定向 check 通过，详见 [收尾记录](wrapup.md)。
- Review: 单批 g2-batch-one，1 个修复轮；PM/codex g2-pm-r2、QA/codex g2-qa-r3 PASS，passed_at=ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3；CSA/Hacker N/A。g2-qa-r2 的环境中断失败由 g2-qa-r3 显式替代，失败原件保留。本卡无分配 finding、无非门禁残留；组六项 finding 由所属作者全部 fixed。计划 sealed，批次 closed。
- Wrap-up: 本卡最终提交 003e5fecf4048d8da8151d0431ea1cd040912a36 无重写，随最终组 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 进入本地和远端 develop；主工作树同步且干净。本卡工作树、本地及远端分支清理完成，done --result completed 和定向 check 完成；本执行端无 Reviewer 临时目录，审核原件保留。适用收尾步骤全部完成；组资源由协调者处理，CLI/终端保留。
- Unresolved issues (2): [验证缺口][Unverifiable] 原生 Windows 未执行，缺原生锁/reparse/控制台证据，交叉编译不替代实机；[验证缺口][Unverifiable] 真实 tmux/herdr/Agent 未执行，假 CLI 与临时看板不替代真实联调。无未修复或待处置的本卡 finding。
- Summary: 订阅事实修复已审核、集成并完成收尾；Code branch: develop；Final card state: done
