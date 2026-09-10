Review focus:
(1) 订阅端派回事实（20260907-subscription-dispatch-facts-task）：snapshot/事件携带的 dispatch ID、epoch、状态、dispatch revision、confirm_by 与 accepted/completed 的卡片 revision 是否与同一次协调读取绑定；快速 review→working→review/done 往返后能否仍按同 ID 辨认回执；prepared/delivery-unknown 是否按各自 confirm_by 独立触发 dispatch-attention，且 accepted 不被套用完成期限；订阅是否严格不自动恢复、重发、改状态或放行依赖。
(2) 派回证据绑定（20260908-dispatch-evidence-binding-task）：fix 派回是否真的引用审核原件与已有原作者处置、不因卡片移动或错轮次而错派；代收尾的 wrap-up-only 专用授权是否必须在隔离旧 epoch 之后取得，能否阻止仍活动的执行端与代办并行写入；旧未绑定通知的兼容与 sync 协议是否保留。
(3) 编排恢复（20260908-coordinator-recovery-task）：编排检查点的持久读取与 CAS 是否阻止旧编排者覆盖新记录；初始快照与后续事件是否走同一对账、快速完成或重复事件不触发重复派回；编排重启后是否只有完整审核证据和真实 Git 集成证据才能完成同一轮收尾。
(4) 三卡集成缝：三张卡依次叠加在同一组分支上（6c1cd63 → 6f5ed32 → 0b304a8），dispatch-evidence-binding 与 coordinator-recovery 都消费 board 的持久 dispatch 与审核原件接口，请重点看接口在三次叠加后的一致性、是否有半改状态或重复真相源。
(5) 文档与注释是否与实现一致（中文文档、AGENTS.md 索引、英文发布协议、三语消息）。

Verification records:
三张卡的执行者各自在其最终提交上完成交付自检并实跑验证，命令与输出见各卡 report.md / validation 附件：subscription-dispatch-facts 交付 6c1cd63db5a13e7b4b98b4a1b364dfba9c12b6fa；dispatch-evidence-binding 交付 6f5ed32072275c3bb9d96c9e40bd606b949b9c17（首提交对齐组分支时 rebase 无冲突）；coordinator-recovery 交付 0b304a8399cdbc8db2c005e390afd3f6127532af（rebase 最新组基线无变化）。主控将在本批 target 上另行实跑 build/vet/全量测试。

Environment gaps:
原生 Windows 与真实 tmux/herdr/Agent 未运行；交叉编译、假 CLI 与本机隔离测试不代表这些实机环境通过。这属于已披露的验证缺口，不得据此单独判定 blocking/high/medium。

本批次说明：
base ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 是组分支创建锚点（等于当时 develop），target 0b304a8399cdbc8db2c005e390afd3f6127532af 是三卡交付全部 ff 接收后的组分支 HEAD。三张卡的实现分别由三个执行 Agent（均为 codex）完成，主控未代改任何代码。这是本组第一批，无前轮 finding。