Role: QA  
Commit: `5845e6fd0f2b503313030349fa211a7791a50169`  
Task Context: 增量复审 `0b304a8..5845e6f`；核验上轮三项 finding 及修复引入的回归。  
Reviewed Scope: 已完整读取任务文件与契约；14 个变更路径均属于 COMMIT TREE，工作区与 target 一致。变更遵守 board 持久事实、launch 启动编排的包边界，无反向依赖或尺寸违规。`git diff --check` 通过。只读环境未重跑 Go 测试；采信交付记录：全量 1016 PASS/1 SKIP、8 包 race 752 PASS/1 SKIP，build/vet/格式检查通过。原生 Windows、真实终端及 Agent 验证缺口保留。

| 行为/质量 | 判断与证据 |
|---|---|
| QA-01 首次启动 | 原报告场景已关闭，Inferred，高置信度。`coordinator.go:221–229` 保留等待成员；`coordinator_cycle.go:6–22` 允许一次绑定；`coordinator_reconcile.go:11–22` 与 `coordinator.go:278–319` 保持任务及检查点 CAS。`coordinator_cycle_test.go:27–179` 覆盖重启、重复观察、历史保留及非法替换。另发现启动失败回滚的新回归 QA-04。 |
| QA-02 闭批历史消费 | 已关闭，Inferred，高置信度。`dispatch_evidence.go:106–124` 改用只读消费，`:146–194` 保留 lineage、assignment、作者校验；`review_disposition.go:99–121` 保留修改路径闭批拒绝。`coordinator_closed_fix_test.go:12–94` 覆盖真实闭批生产者、重复对账及四类原件缺失。 |
| QA-03 期限文档 | 已关闭，Observed，高置信度。`docs/subscription-facts.md:43–58`、`rules/KANDER-KANBAN-RULES.md:312–314` 与 `dispatch_snapshot.go:59–62`、`dispatch_wrapup.go:85–89`、`subscribe_dispatch.go:22–38` 一致。 |
| 测试与错误恢复 | Observed：新增测试使用隔离状态和明确结果断言；board 测试配合 launch 集成测试适合本次变更。启动元数据发布后、launcher 失败前的交错路径存在以下缺陷。 |

FINDINGS

**QA-04 — medium — Inferred；置信度：高。首次启动提前绑定后，合法启动回滚使全组无法恢复对账。**

新逻辑看到 `working`、OWNER、STARTED_AT 即清除 `AwaitingStart`，永久绑定周期：[coordinator_cycle.go:18](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/board/coordinator_cycle.go:18)。但启动先提交这些元数据，再调用 launcher；launcher 仍可能失败：[commands.go:176](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/launch/commands.go:176)。

具体路径：完整 claim 包含 todo 成员 B；B 启动，编排在 launcher 等待期间 reconcile 并绑定周期；随后 launcher 失败，`rollbackLaunch` 调用受 revision 保护的 `RollbackDocument`，恢复 todo 与空 STARTED_AT：[agent.go:144](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/launch/agent.go:144)、[snapshot.go:40](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/board/snapshot.go:40)。编排只写检查点，不改变任务 revision，因此该回滚可以成功。

下一次 reconcile 因旧周期非空、`AwaitingStart=false` 拒绝 B；重新 claim 又保留该游标，阻断整组恢复。此前版本不会接受这个临时周期，因此这是修复新增的状态污染。

最小持久修复：区分启动尝试与已确认周期；根据受控启动成功或回滚的持久证据，在 CAS 下确认或撤销首次绑定，保留历史，继续拒绝任意周期替换。增加确定性交错测试：元数据发布后对账，注入 launcher 失败，完成真实回滚，再次启动及重启对账成功。

NON-BLOCKING: none

任务文件已尝试删除；只读文件系统拒绝，文件保留。

```kander-findings
{
  "FINDINGS": [
    {
      "id": "QA-04",
      "tier": "medium",
      "text": "Inferred；置信度高。新增首次启动绑定会在 launcher 成功前永久绑定执行周期。完整 claim 包含 todo 成员，start 发布 working、OWNER、STARTED_AT 后，编排先 reconcile；随后 launcher 失败并合法回滚到 todo、空 STARTED_AT。检查点不修改任务 revision，回滚可以成功，但 AwaitingStart 已清除，下一次全组 reconcile 因周期变化失败，重新 claim 仍保留该游标。此前逻辑拒绝临时周期，不会产生此状态污染。最小修复：区分启动尝试和已确认周期，凭受控启动成功或回滚的持久证据在 CAS 下确认或撤销首次绑定，保留历史且继续拒绝任意周期替换；补充元数据发布后对账、launcher 失败回滚、再次启动及重启恢复的确定性交错测试。",
      "evidence": "internal/board/coordinator_cycle.go:18-22 仅凭已启动卡态和非空元数据绑定并清除等待标记，之后拒绝周期变化；internal/launch/commands.go:176-181 在 launchAgent 前发布元数据，失败调用 rollbackLaunch；internal/launch/agent.go:50-57 存在等待 pane 就绪后失败的实际路径，:144-148 恢复原文和原状态；internal/board/snapshot.go:40-75 在任务 revision CAS 下恢复原文并迁回 todo；internal/board/coordinator.go:300-319 仅更新检查点，不使任务回滚 revision 失效，:221-223 重新 claim 保留已有成员。违反 coordinator-recovery 的持久事实恢复与失败后恢复契约。"
    }
  ],
  "NON_BLOCKING": []
}
```