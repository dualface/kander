Role: QA  
Commit: `ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe`  
Task Context: 三卡订阅事实、持久派回、订阅运行时；审核范围 `021ae63..ff04a3b`。  
Reviewed Scope: 已完整读取任务文件与规格，核对提交树，追踪 board/fs、liveness/probe、notify/launch/window、CLI、测试及协议消费者。环境只读，未重跑 Go 测试；采信调用方记录的验证结果。原生 Windows、真实终端验证仍为 Unverifiable，不单独构成 finding。

| 行为/质量 | 结论与证据 |
|---|---|
| 协调事实、revision、重启 | Observed：共享锁内捕获正文与 revision；事件消费同一快照；有往返及重启测试。 |
| 动态成员与完整性 | Observed：重新展开组引用；跨组移除保留 reconciliation；不完整读取终止。 |
| 意图、原子回执、恢复 | Observed：复用 board 事务；稳定 ID 注册、回执重放、kill 恢复有覆盖。 |
| 授权及投递重试 | Inferred：存在跨派回 WINDOW 写入漏洞，见 QA-001。 |
| 探测、输出及取消 | Observed：单批采集、有限队列、单 writer；管道、stop、context、信号测试覆盖退出路径。 |
| 架构、合并与文档 | Observed：依赖方向保持；三语键集合一致、无重复；两个文档索引均保留；无尺寸门禁违规。注释偏差见 QA-002。 |

FINDINGS

**QA-001 — medium — Inferred，置信度高：旧 notify 借用新派回的授权写入 WINDOW**

[internal/notify/dispatch.go:111](/home/dualf/works/kander/worktrees/20260908-subscription-dispatch-group/internal/notify/dispatch.go:111) 每轮读取普通 `ReadSnapshot`，没有绑定当前投递的授权；147–155 行先写 WINDOW，163 行才调用 `BeginDispatchAttempt` 校验派回身份。

可达场景：A 的 notify 等待 busy；操作者取消 A、准备 B；A 下一轮读到 B 的快照，探测发现地址漂移或应用原 `--pane`。`Transaction.Snapshot` 将 B 的授权装入游标（`internal/board/transaction.go:173`），`managedMutation` 便以 B 的授权通过写入（`internal/board/snapshot.go:49–63`）。随后 A 的发送被拒绝，但 B 的 WINDOW/revision 已改变。按 ID 的投递锁不能阻止此跨 ID 路径。

影响：违反旧执行授权不得写新轮 WINDOW 的契约；可能覆盖新轮地址、造成新执行者 CAS 冲突。最小修复：固定投递开始时的授权，每轮通过 `ReadExecutionSnapshot` 获取游标，再探测及写入。补一个暂停 busy 后取消 A、准备 B 的确定性测试，断言旧调用失败且 B 的 WINDOW/revision 不变。

**QA-002 — medium [mechanical] — Observed，置信度高：恢复失败注释遗漏持久模式保留行为**

[internal/launch/notify_resume.go:29](/home/dualf/works/kander/worktrees/20260908-subscription-dispatch-group/internal/launch/notify_resume.go:29) 注释声称失败时，只要仍持有 revision 就恢复原正文。但新增的 108–110、119–121 行在不确定投递或持久模式校验失败时直接返回，刻意保留 WINDOW/任务文件，即使 revision 未被他人改变。

影响：接口注释误导调用者及维护者判断失败后的资源状态。最小修复：注明普通回滚适用条件，以及持久模式发送后不确定失败保留资源的例外；无需修改运行逻辑。

NON-BLOCKING: none

任务文件删除已尝试；只读文件系统拒绝，文件保留。

```kander-findings
{
  "FINDINGS": [
    {
      "id": "QA-001",
      "tier": "medium",
      "text": "Inferred，置信度高：旧 notify 可借用新 dispatch 的授权写入 WINDOW。A 等待 busy 时被取消并由 B 替换，A 下一轮通过普通 ReadSnapshot 获得 B 的授权游标；地址漂移或原 --pane 触发 WINDOW 写入，之后 BeginDispatchAttempt 才拒绝 A。新轮 WINDOW/revision 已被旧调用改变，违反旧授权不得写新轮的契约，并可能造成地址覆盖或 CAS 冲突。最小修复：固定投递开始时的授权，每轮使用 ReadExecutionSnapshot 获取快照；增加暂停 busy、取消 A、准备 B 后断言旧调用失败且 B 的 WINDOW/revision 不变的确定性测试。",
      "evidence": "internal/notify/dispatch.go:104-115 每轮重读旧意图及未绑定授权的当前卡片；147-155 在身份 CAS 前写 WINDOW，163 才校验发送。internal/board/transaction.go:173 将快照当前授权放入 Version；internal/board/snapshot.go:49-63 使用该游标授权提交。internal/board/dispatch.go:222-249 允许取消后以新 ID/epoch 替换；internal/board/dispatch_delivery.go:18 投递锁按 ID 隔离，不能阻止跨 ID 替换；33-40 已提供可复用的授权绑定读取入口。"
    },
    {
      "id": "QA-002",
      "tier": "medium",
      "mechanical": "documentation",
      "text": "Observed，置信度高：NotifyViaResume 注释声称失败时只要仍持有当前 revision 就恢复原正文，但本范围新增的持久模式不确定投递及校验失败分支直接返回并保留资源，即使没有并发更新。注释误导调用者判断失败后的 WINDOW 和任务文件状态。最小修复：更新注释，明确普通回滚条件与持久模式发送后保留资源的例外；无需改运行逻辑。",
      "evidence": "internal/launch/notify_resume.go:29-31 描述失败恢复原正文；104-110 的 durable/DeliveryUnknown 分支保留任务文件并直接返回；118-121 的持久模式校验失败分支同样不回滚。该偏差由本范围新增分支造成。"
    }
  ],
  "NON_BLOCKING": []
}
```