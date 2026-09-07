# 持久派回协议

派回身份由 `dispatch_id` 决定，不由终端回显或栏目变化推断。`prepared` 仅证明意图已经落盘；发送前进入 `delivery-unknown`。只有执行端的受控 `move working` 才生成 `accepted`；`move review` 或 `move done` 将业务回执、卡片状态和 revision 在同一事务中提交。

## 创建、读取和重试

```sh
kander notify <task-id> --kind fix --base <40-character-SHA> --message-file <UTF8-file>
kander notify <task-id> --dispatch-id <id> --message-file <same-UTF8-file>
kander resume <task-id> --dispatch-id <id> --message-file <same-UTF8-file>
kander dispatch show <task-id> <id>
```

`kind` 为 `fix`、`sync` 或 `wrap-up`。任务组的 review 卡自动使用持久模式，省略 kind 时为 fix；working 卡或无组卡通过显式 `--kind` / `--dispatch-id` / `--base` 选择持久模式。省略 ID 时，在任何发送前生成并输出。新意图缺 base 时使用当前工作目录的 Git HEAD；重试缺 base/kind 时使用原意图，不能使用新 HEAD 替换原基线。

调用方也可先准备 UTF-8 JSON，再执行 `kander dispatch prepare <intent.json>`。字段为 `dispatch_id`（可省略）、`task_id`、`kind`、`message`、`base`、可选 `references`、`created_at` 和 `confirm_by`。每个引用用 `{ "task_id": "...", "path": "reviews/run/report.md" }` 表达，不保存状态目录绝对路径。这里只检查引用形状；审核 finding 和代办权限的语义绑定不由此存储层判断。

同 ID、同任务、同消息、同基线、同 kind/引用重复创建不会写第二份意图，也不增加卡片 revision。不同输入明确冲突。时间默认值仅在首次创建时应用；省略时间的重试保留原值。`confirm_by` 是接受期限，默认创建后 120 秒，notify/resume 的新意图使用本次 `--timeout`。它不是工作完成期限；已接受工作可以在该期限之后完成。

读者先查看持久回执。已有 accepted/completed 时直接返回，不重复发送或启动。没有回执时，同 ID 重试重新采集事实：对当前唯一、可接收的同一会话可再次发送同 ID 指令；只有身份有效的 stopped 观测允许恢复原会话。unknown、失效观测、缺身份和 busy 不授权恢复。发送调用报错也可能已经送出，保留 delivery-unknown 和消息文件，不紧接着再启恢复实例。恢复启动一旦尝试把命令交给 Agent，后续启动、标记或存活校验失败也保留该窗口、WINDOW 和任务文件；它可能已经接受工作，不能盲目清理。创建占位容器但尚未尝试发送时仍可清理本次资源。期限到期返回非零，不重置期限、不声称 accepted。

显式 `resume --agent` 延用既有用户授权接管语义。对一个未终结 dispatch 接管时增加执行 epoch，保存上一 epoch 的原始状态和回执，原消息、基线、确认期限不变。不能以相同 ID 偷换接管消息；需要新消息或已经过期时，调用方先明确处置旧意图，再创建新 ID。取消/失败意图入口为 `kander dispatch cancel|fail <task-id> <dispatch-id> <dispatch-revision> <reason>`；它记录派回处置，不取消、归档或移动任务卡，不自动授权接管。

## 执行端原子回执

```sh
kander move <task-id> working --dispatch-id <id> --execution-epoch <epoch>
kander update <task-id> --document spec.md --file <UTF8-file> \
  --expect-revision <revision> --dispatch-id <id> --execution-epoch <epoch>
kander move <task-id> review --dispatch-id <id> --execution-epoch <epoch> \
  --delivery-commit <final-40-character-SHA> --disposition <relative-artifact>
kander move <task-id> done --result completed --dispatch-id <id> \
  --execution-epoch <epoch> --delivery-commit <final-40-character-SHA>
```

接受命令返回 JSON，包含 `dispatch` 和 `replayed`。只有 `replayed=false` 才开始本轮工作。重复接受返回原回执和 `replayed=true`，不重跑工作、不增加 revision；即使第一次接受后已经快速回到 review，仍然能确认该次接受。旧 ID/epoch 不能接受新轮，也不能用重新读取到的新 revision 绕过授权检查。

fix/sync 完成目标为 review，wrap-up 为 done。完整交付 SHA 必填；适用时带 disposition 相对附件路径，附件必须存在于本卡。这里证明引用随完成原子提交，不把引用或任意 SHA 声称为 Git 集成验证。done 仍经过原有摘要/报告、审核计划和闭批门禁。完成重试只有同目标、同证据才返回原回执。

审核作者的 disposition JSON 同样携带 `authorization: { "dispatch_id": "...", "epoch": 1 }`，防止旧执行者使用新 revision 提交处置；历史未绑定记录不新增该字段。审核 finding 与 dispatch 的语义引用校验仍由后续证据绑定契约负责。

每卡只保留一个有效执行授权。新意图只能替换已完成、失败或取消的意图；显式接管可以轮换未终结意图的 epoch。普通 update 在接受前、结束后、缺 ID/epoch 或旧 epoch 时拒绝。WINDOW 和 launch/notify 回滚携带操作开始时的版本游标与授权，不能借用新执行轮的游标，也不能把已消费的旧正文写回。不会为整个 Agent 会话持有看板或卡片锁。明确携带 reason/decision 的 archive/trash 生命周期决定仍可操作绑定卡；未结束的授权在同一事务中取消，不能借此继续执行工作。

## 存储与恢复

`internal/board` 定义纯类型和受控 API；notify/launch/window 通过公开入口消费，不存在 board 到 notify 的依赖。

- `dispatches/<id>/intent.json` 保存创建原件；`state.json` 保存当前版本。
- `accepted-<epoch>.json` / `completed-<epoch>.json` 保存逐 epoch 原子回执；接管另存 `execution-<epoch>.json`。
- `.kander/groups/00000000-dispatch-group/<id>.json` 是保留的全看板 ID 注册表，防止同 ID 被另一个任务复用。创建按看板、组、任务、短期 journal 锁排序。
- 原件、状态和回执由专用 producer 管理，普通 update 拒绝改写。所有发布复用 S 的 redo journal 和 `internal/fs`，卡片移动后原件跟随卡片。
- 每个 dispatch 有独立、由操作系统管理的投递锁，先于看板锁获取。它只覆盖发送/恢复和本次确认等待；回执写入不获取此锁。发送进程被杀后锁释放，不占据执行会话生命周期。进程创建、内核 I/O、锁等待和进程回收仍受操作系统约束。
- 发布中断时，读者报告 pending，不读取中间态、不自动修复。按维护条件运行 `kander init` 重做事务，再以原 ID 对账。保留未知文件和冲突，不删除证据以凑成功。

P3 的批量 context 入口提供探测总预算（默认 10 秒），接受期限作为父 deadline；身份有效性由 `ValidFor` 校验。投递前再检查同一会话的 readiness；alive 与 ready、accepted、completed 分开。发送和确认等待共用持久期限。真实终端 marker 仅作为传输诊断；屏幕上出现提示文字不构成开工证据。

## 兼容与能力边界

未绑定模式的 working 普通消息、无组卡继续使用既有消息流程，不补造历史 accepted/completed 回执。绑定 working 卡的普通 notify 只允许直投信息，不通过旧模式恢复进程；resume 必须显式携带 dispatch ID。恢复启动成功后，resume 可返回真实的 delivery-unknown（尚未接受）状态；调用方仍须读取业务回执。foreground 的进程等待在释放投递锁后继续，不能以会话时长锁住同 ID 对账。持久模式优先于旧规则中把 review-working 栏目变化或终端 marker 当作确认的表述。已绑定卡不得用无授权 update/move 回到旧协议。旧二进制不认识本协议，升级/恢复时仍须遵守现有维护窗口规则；不得与绕过协议的旧写入者并行运行。

保证限于遵守受控入口的本机进程：重复接受不会生成第二份回执；新 epoch 拒绝旧受控写入。磁盘协议不可能让任意 Git、网络、文件编辑等外部副作用 exactly-once。若执行者在接受后、外部操作中途死亡，已有 accepted 也不会被误当 completed；后续决策须重建实际工作进度，不能盲重放。

Linux 回归使用临时目录、假 CLI 与子进程 kill/restart，覆盖意图创建、接受、review/done 完成、发送前后中断、同 ID 对账和并发回滚。Windows 交叉编译仅验证构建；原生 Windows 的锁/DACL/reparse/进程回收及真实 tmux/herdr/Agent 行为需要各自的实机证据。
