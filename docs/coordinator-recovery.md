# 编排检查点与恢复

`kander coordinator` 是单次受控读写入口。它保存编排恢复所需事实，既不运行订阅循环，也不发送通知、移动卡片、改变 Git 引用或删除工作区。

## 存储、锁与授权

当前记录位于 `kanban/.kander/groups/<group-id>/checkpoint.json`。每次实际更新同时发布不可覆盖的 `checkpoints/<revision>.json` 历史版本。两者使用 board 的同一 redo 事务，读者不暴露准备阶段的中间结果；崩溃后只有显式 `init` 完成事务。内容摘要发现意外损坏，不构成针对同用户恶意篡改的安全边界。

锁序为看板、排序 group_id、排序 task_id、短期 journal 锁。claim/reconcile 在看板排他锁内确认完整成员集合，避免发现成员与写入检查点之间出现拓扑变化；审核和派回控制组锁与成员锁一并声明。检查点 revision 独立于任务 revision，不因读取而递增，也不修改任务 revision。

coordinator authority 包含 `owner`、`token`、`epoch`。新会话使用唯一 token 和既有编排授权依据，CAS 当前 checkpoint revision/epoch；只有一个竞争者成功。旧 epoch 即使拿到最新任务 revision 也不能写检查点。相同 claim JSON 重试不增加 epoch。该授权只覆盖检查点，不是任务接管、集成、通知或代收尾授权。

## 调用

```text
kander coordinator show 20260908-example-group
kander coordinator claim /absolute/claim.json
kander coordinator reconcile /absolute/observations.json
```

首次 claim 示例：

```json
{
  "group_id": "20260908-example-group",
  "expected_revision": 0,
  "expected_epoch": 0,
  "owner": "coordinator-session",
  "token": "unique-session-token",
  "basis": "用户已授权本组编排，记录对应决定",
  "members": ["20260908-example-task"]
}
```

恢复会话先 show，填写实际 revision/epoch，并更换 token。不要通过不断 claim 抢占另一个活动编排者。成员集合固定；未知、遗漏或新增组内成员均明确拒绝，需要先处理实际契约变化。外部依赖组新增成员继续由 subscription 的动态展开协议处理，不冒充本组成员。

reconcile 使用 show 返回的 authority，成员键必须完整，每个 revision 来自最新 `show --json`。下面的 SHA、revision、ID 仅示意，不能作为真实证据：

```json
{
  "group_id": "20260908-example-group",
  "expected_revision": 1,
  "authority": {
    "owner": "coordinator-session",
    "token": "unique-session-token",
    "epoch": 1
  },
  "members": {
    "20260908-example-task": {
      "revision": 12,
      "dispatch_id": "fix-round-one",
      "epoch": 1,
      "base": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "delivery_commit": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    }
  }
}
```

未完成派回不传 delivery_commit；完成时必须与同 ID、同 epoch 的原子 completed 回执一致。输入不能凭卡态、事件时间或相似 SHA 推断通过。未绑定 dispatch 的首次交付允许单独提供 delivery_commit，但必须处于 review、卡片已记录任务分支，并提供绝对 `cwd` 核对实际本地任务分支 HEAD；这只记录交付，不证明组分支已接收。

## 同一事实对账

初始 snapshot、task-update、state-change、heartbeat/attention 和订阅重启都使用同一入口。事件只触发重新读取；不持久化订阅进程的 sequence，不要求看到 review-working 边沿。

每个成员保存已观察 revision、执行周期、卡态事实、交付 SHA、dispatch ID/epoch/base/revision 和 pending confirmation/delivery/wrap-up，以及相对 intent/integration/专用授权引用。审核部分保存 plan ID、batch ID、run 相对引用及既有验证器的结构进度。失败审核引用 output.raw，不补造 report.md。检查点不复制审核正文，不替作者写处置，不产生语义 PASS。

accepted 或 completed 原件解除同轮待确认；只有 completed 及匹配交付才解除同轮待交付/收尾。执行端在 notify 返回前完成、同一扫描间隔内快速往返，或订阅/编排端重启后只见最终快照，均可恢复。重复相同观察会再次验证原件和实际 Git，保持 checkpoint revision 和事务数量不变。过期 revision、旧 epoch、错轮次、错交付保留原记录并报错。

确认期限来自 dispatch 原件，不能因其他事件或重启续期。heartbeat 表示订阅活着；alive 表示会话存在；任务 revision 与 dispatch receipt 才提供持久进展。无输出不触发重启，alive 不证明推进，超时不证明退出。入口不自动发送或恢复执行者。

## 收尾证据

board 复用 A/R 的原件、完整发布、角色要求、作者处置、批次前驱、机械修复、闭批与执行周期校验，再消费 dispatch 的 integration 绑定。launch 复用集成 Git 校验，并调用 review 的 `VerifyClosedReviewGit` 复核闭批中的实际祖先关系和机械变更摘要。依赖单向为 launch → review → board；board 不导入 review、launch 或 notify。

历史闭批不要求当前 HEAD 仍停留在该批提交。合法 rebase 仍由既有补丁对应证明校验；未声明的重写和不匹配的最终交付会失败。清理后原证据 CWD 不再存在时，可以传绝对 `cwd` 指向仍存在的仓库 worktree，重新证明原件中的同一组提交；不会改写原始路径或 SHA。

无 finding 成员只消费已发布原件，不为复制报告而派回。多卡部分归档时，按 task ID 在实际卡态定位相对附件；只有同轮 completed 且 `done` 或 `archived/RESULT: completed` 才完成该轮收尾。已完成卡不回退，集成不重做。

代收尾只能消费 dispatch 专用 wrap-up-only 授权。对账本身不授予它。无 SESSION、投递未知、仍活动、非零返回、确认或租期超时均不独自证明退出；已确认退出也必须经原专用生产者隔离旧执行 epoch。原作者记录保持不变。

## 故障与平台边界

CLI 使用 30 秒 context，限制准备阶段的锁竞争、成员循环与 Git 验证。底层 OS 打开/读取及提交 redo 后的发布仍遵守已有事务边界，不能声称整个命令具有硬墙钟上界。取消不遗留等待锁的后台 worker；发布开始后保留可恢复原件，不做半途回滚猜测。

EOF/输出错误后，对当前事实做一次重新对账；事实有效时只重建一次订阅。只有明确的 `board.transaction_pending` 才按维护前提显式 init 一次再读取；仍失败即报告。duplicate、reparse、真实损坏或未知产物立即保留证据；不能删除、改名或无限重试。锁期限耗尽是暂时无法观察，不是任务失败。

测试分别覆盖 board 结构/事务、真实本机 Git、隔离子进程 kill、假终端/Agent。原生 Windows 和真实 tmux/herdr/Agent 会话需单独实测；交叉构建及假 CLI 不替代实机通过。原始复现与回归映射见[复现验收映射](recovery-regressions.md)。
