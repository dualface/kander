Role: PM  
Commit: `5845e6fd0f2b503313030349fa211a7791a50169`  
Task Context: 已完整读取指定 `prompt.txt`、`task-spec.md`及前轮处置。  
Reviewed Scope: 仅 `0b304a8..5845e6f` 三个修复提交；未变代码仅用于核对修复影响。

本轮 PM 增量复审通过。PM-01、PM-02、PM-03 全部关闭；未发现新增门禁问题。

仅列状态变化的需求：

| 需求 | 预期行为 | 实现证据 | 状态 |
|---|---|---|---|
| 闭批后恢复 completed fix；coordinator 验收第4、6、7项 | 历史消费允许闭批，保留原件校验；发送仍拒绝 | `coordinator_reconcile.go:90–98`；`dispatch_evidence.go:106–124`；`review_disposition.go:99–121` | Partial → Complete |
| 未启动成员首次周期绑定；coordinator 验收第2、4、6项 | 持久启动事实与 CAS 共同验证；保留历史，禁止替换已绑定周期 | `coordinator_cycle.go:6–22`；`coordinator.go:278–319` | Partial → Complete |
| 订阅期限文档一致；dispatch-evidence-binding 验收第7项 | 明确当前 epoch 期限来源、原始年龄及重试不续期 | `docs/subscription-facts.md:43–58`；`rules/KANDER-KANBAN-RULES.md:312–314` | Contradicted → Complete |

**PM-01：closed。Inferred，置信度高。**  
历史 completed 分支改用 `reviewRunForConsumption`，仍验证成功 run 和完整发布。`dispatch_evidence.go:114–194` 保留身份、前驱、finding、assignment 和作者绑定；`review_disposition.go:353–433` 核对作者原件。创建及发送继续经过闭批拒绝门禁：`dispatch.go:224,308`、`review_disposition.go:111–121`。新增 `coordinator_closed_fix_test.go:12–94` 覆盖闭批恢复、重复对账及四类原件缺失。

**PM-02：closed。Inferred，置信度高。**  
`coordinator.go:221–229` 初始化等待标记，重新 claim 保留已有成员。`coordinator_cycle.go:18–22` 仅凭空周期、更高 revision、OWNER/STARTED_AT 和已启动卡态绑定一次；已绑定周期变化继续拒绝。任务 CAS 位于 `coordinator_reconcile.go:11`，检查点授权/CAS 位于 `coordinator.go:278–293`；历史追加位于 `coordinator.go:152–164`。元数据尚未发布的中间快照保留等待状态。新增测试覆盖重启、漏过 working、重复绑定、过期 CAS、历史保留及实际 start 生产路径。

**PM-03：closed。Observed，置信度高。**  
中英文文档已与 `dispatch_snapshot.go:59–62`、`dispatch_wrapup.go:85–89`、`subscribe_dispatch.go:22–38` 一致：`created_at` 保留原意图时间；`confirm_by` 使用当前 epoch 的有效期限；专用 grant 使用独立期限，同 epoch 重试及订阅重启不续期。

沿用前轮需求口径：Complete 36、Partial 0、Missing 0、Contradicted 0、Unverifiable 3。本轮仅只读源码审查，未重跑测试、race、build/vet；作者执行记录不计为本人验证。已披露实机缺口不单独设门禁。

NON-BLOCKING: none

任务文件保留：当前只读沙箱禁止删除。

```kander-findings
{"FINDINGS":[],"NON_BLOCKING":[]}
```