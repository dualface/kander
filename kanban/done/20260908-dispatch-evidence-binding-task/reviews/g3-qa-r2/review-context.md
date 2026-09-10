KANDER_AUTOMATIC_CONTEXT_BYTES: 50240
PREVIOUS_RUN_ID: g3-qa-r1
Prior report (verbatim):
Role: QA  
Commit: `0b304a8399cdbc8db2c005e390afd3f6127532af`  
Task Context: 三卡合并范围 `ec6fdb4..0b304a8`；订阅派回事实、证据绑定、编排恢复。  
Reviewed Scope: 已完整读取任务文件与契约，核对 63 个变更路径均属于 COMMIT TREE、工作区内容与 target 一致。追踪 board、liveness、launch、notify、review 及相关测试、协议、三语消息。只读环境未运行需写入的 Go 测试；保留调用方交付验证记录作为证据。`git diff --check` 通过；无新增文件尺寸门禁违规。原生 Windows、真实终端及 Agent 验证仍为已披露缺口。

| 行为/质量 | 判断与验证层 |
|---|---|
| 协调快照、同 ID 回执、快速往返 | Observed：共享锁读取卡片与 dispatch；已有隔离测试断言回执及 revision。 |
| 独立确认期限、accepted 不设完成期限、订阅只读 | Observed：实现与定向测试支持；期限文档存在 QA-03。 |
| fix 原件、作者引用、move、旧通知与 sync | Observed：绑定与传输前复核已接入；历史闭批消费存在 QA-02。 |
| 编排 CAS、epoch、重复观察、崩溃恢复 | Observed：事务与测试支持；首次启动承接存在 QA-01。 |
| wrap-up 隔离、权限限制、审核与 Git 消费 | Observed：分层校验及隔离测试覆盖；未发现其他门禁问题。 |

FINDINGS

**QA-01 — high — Inferred；置信度：高。首次启动组员后，检查点永久拒绝对账。**

`ClaimCoordinator` 保存所有成员当时的 `planCycle`，再次 claim 跳过已有成员；对账直接拒绝周期变化。`planCycle` 包含 `STARTED_AT`，首次 `kander start` 必然填写该字段。

具体场景：组内 A 已运行、B 等待前置；按完整成员集合建立检查点。B 就绪并首次启动后，全组 reconcile 返回周期冲突。重新 claim 仍保留旧周期，无法恢复进度。

证据：[周期拒绝](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/board/coordinator_reconcile.go:15)、[claim 保留旧成员](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/board/coordinator.go:220)、[周期定义](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/board/review_plan.go:52)、[首次启动填写时间](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/launch/metadata.go:49)。

最小修复：明确保存“尚未启动”，在 CAS 下核验首次启动并绑定新周期；已有执行周期仍禁止静默替换。增加双卡隔离测试：claim 包含 todo 成员，首次启动后对账成功；后续非法周期变化仍拒绝。

**QA-02 — high — Inferred；置信度：高。fix 批次闭合后，已完成回执无法恢复对账。**

completed fix 虽走历史校验模式，内部仍无条件调用 `reviewRunForMutation`；后者发现 closure 即返回 `"batch already closed"`。历史模式只放宽 target 和后继轮次检查，没有避开闭批修改限制。

具体场景：fix 完成，PM/QA 复审通过并闭批；尚未创建 wrap-up 时编排重启。同一 completed dispatch、交付与原件全部有效，reconcile 仍失败，阻断闭批后的正常收尾恢复。

证据：[历史对账入口](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/board/coordinator_reconcile.go:86)、[误用修改校验](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/board/dispatch_evidence.go:106)、[闭批拒绝](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/board/review_disposition.go:107)。现有回归仅推进 batch target，未闭批：[测试边界](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/board/coordinator_regression_test.go:130)。

最小修复：历史消费使用只读原件校验，保留身份、完整发布、lineage、assignment 和作者校验；创建及发送仍拒绝闭批。补充真实闭批后重启、重复对账及原件损坏拒绝测试。

**QA-03 — medium [mechanical] — Observed；置信度：高。订阅协议误称 confirm_by 始终来自原始意图。**

中文订阅文档与英文协议规定输出原始 `confirm_by`，按原期限判断 overdue；实际专用 wrap-up grant 使用新 epoch 的独立接受期限。

具体场景：原意图已过期，取得新 grant。事件正确输出新期限且未 overdue，按文档比对原 intent 的消费者却会判为期限被重置或应已超时。

证据：[中文字段说明](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/docs/subscription-facts.md:43)、[中文调度说明](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/docs/subscription-facts.md:55)、[英文字段说明](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/rules/KANDER-KANBAN-RULES.md:312)、[英文 overdue 定义](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/rules/KANDER-KANBAN-RULES.md:314)、[实际期限选择](/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group/internal/board/dispatch_wrapup.go:85)。

最小修复：统一说明 `created_at` 保留原意图时间，`confirm_by` 表示当前 epoch 的有效接受期限；专用 grant 使用自身期限，同 epoch 重试与重启不续期。

NON-BLOCKING: none

任务文件已尝试删除；只读文件系统拒绝，文件保留。

```kander-findings
{
  "FINDINGS": [
    {
      "id": "QA-01",
      "tier": "high",
      "text": "Inferred；置信度高。完整成员集合中的 todo 卡首次 start 后，STARTED_AT 改变 planCycle，reconcile 永久拒绝该成员；再次 claim 保留旧 Cycle，导致串行启动后全组进度无法恢复。最小修复：明确保存未启动状态，在 CAS 下核验首次启动并绑定执行周期，继续拒绝已有周期的静默替换；补充含 todo 成员的双卡首次启动对账及非法周期变化测试。",
      "evidence": "internal/board/coordinator.go:220-228 保存周期且再次 claim 跳过已有成员；internal/board/coordinator_reconcile.go:15-18 无条件拒绝周期变化；internal/board/review_plan.go:52-53 将 STARTED_AT 纳入周期；internal/launch/metadata.go:46-57 首次启动填写 STARTED_AT，internal/launch/commands.go:122,176 发布该元数据；rules/KANDER-TASK-GROUP-RULES.md:203-204 要求完整成员集合及持续对账。"
    },
    {
      "id": "QA-02",
      "tier": "high",
      "text": "Inferred；置信度高。completed fix 的历史对账仍调用禁止闭批修改的 reviewRunForMutation。fix 完成且复审闭批后、创建 wrap-up 前重启，完整原件与同轮回执仍触发 batch already closed，阻断正常收尾恢复。最小修复：历史消费使用只读原件校验并保留身份、完整发布、lineage、assignment、作者验证，创建及发送继续拒绝闭批；补充闭批后重启、重复对账和原件损坏测试。",
      "evidence": "internal/board/coordinator_reconcile.go:86-90 对 completed fix 调用历史模式；internal/board/dispatch_evidence.go:86-106 仅放宽 target 校验，仍无条件调用 reviewRunForMutation；internal/board/review_disposition.go:107-110 遇 closure 返回 batch already closed；internal/board/coordinator_regression_test.go:130-140 现有用例只推进 target，未覆盖闭批。"
    },
    {
      "id": "QA-03",
      "tier": "medium",
      "mechanical": "documentation",
      "text": "Observed；置信度高。订阅中文文档与英文协议将 confirm_by 定义为原始意图期限，但 wrap-up-only grant 实际输出新 epoch 的独立接受期限。原意图过期后取得新 grant，按文档实现的消费者会误判 overdue 或期限重置。最小修复：说明 created_at 保留原意图时间，confirm_by 为当前 epoch 的有效期限；专用 grant 使用自身期限，同 epoch 重试及重启不续期。",
      "evidence": "docs/subscription-facts.md:43-46,55-56 与 rules/KANDER-KANBAN-RULES.md:312,314 声称原始确认期限；internal/board/dispatch_snapshot.go:59-62 使用 dispatchAcceptBefore；internal/board/dispatch_wrapup.go:85-89 优先返回 WrapUpAuthority.ConfirmBy；docs/durable-dispatch.md:97 已正确说明该例外，订阅文档尚未同步。"
    }
  ],
  "NON_BLOCKING": []
}
```
Author records (verbatim JSON):
{
  "assignment": {
    "run_id": "g3-qa-r1",
    "batch_id": "g3-batch-one",
    "author": "coordinator",
    "basis": "与 g3-pm-r1 同一归属依据。QA-01 与 PM-02 同根因（coordinator 执行周期），QA-02 与 PM-01 同根因（闭批后历史对账），QA-03 与 PM-03 同根因（订阅文档 confirm_by 语义）；处置时按“一个根因一个 ID”合并并同时引用两个来源 ID。",
    "items": {
      "QA-01": [
        "20260908-coordinator-recovery-task"
      ],
      "QA-02": [
        "20260908-coordinator-recovery-task"
      ],
      "QA-03": [
        "20260908-dispatch-evidence-binding-task"
      ]
    },
    "owners": {
      "20260908-coordinator-recovery-task": "codex",
      "20260908-dispatch-evidence-binding-task": "codex"
    },
    "recorded_at": "2026-09-07T21:49:03.491391558Z"
  },
  "records": [
    {
      "submitted_revision": 19,
      "record_id": "binding-r1-qa-03",
      "run_id": "g3-qa-r1",
      "finding_id": "QA-03",
      "batch_id": "g3-batch-one",
      "task_id": "20260908-dispatch-evidence-binding-task",
      "author": "codex",
      "recorded_at": "2026-09-07T21:58:15.008986762Z",
      "report_hash": "eeb97adabf9023edf939e7aab588e63885f250d41e80a4af9e93221cf46c7c5c",
      "original": "Observed；置信度高。订阅中文文档与英文协议将 confirm_by 定义为原始意图期限，但 wrap-up-only grant 实际输出新 epoch 的独立接受期限。原意图过期后取得新 grant，按文档实现的消费者会误判 overdue 或期限重置。最小修复：说明 created_at 保留原意图时间，confirm_by 为当前 epoch 的有效期限；专用 grant 使用自身期限，同 epoch 重试及重启不续期。",
      "status": "fixed",
      "basis": "合并根因 PM-03/QA-03，来源 (g3-pm-r1, PM-03) 与 (g3-qa-r1, QA-03)，分别保留原件绑定；独立核实后 confirmed，现 fixed。git show 0b304a8399cdbc8db2c005e390afd3f6127532af:\u003cpath\u003e 全部退出 0：internal/board/dispatch_snapshot.go:59-62 保留 Input.CreatedAt 并调用 dispatchAcceptBefore；internal/board/dispatch_wrapup.go:85-89 优先取 WrapUpAuthority.ConfirmBy；internal/liveness/subscribe_dispatch.go:22-38 按摘要期限计算 overdue/唤醒。该目标 docs/subscription-facts.md:43-46,55-56 及 rules/KANDER-KANBAN-RULES.md:312,314 却称原期限，原意图过期后授予新 epoch 即触发误导。修复统一当前 epoch 有效接受期限、普通意图/专用 grant 来源、created_at/age_seconds 来源与同 epoch 重试/订阅重启不续期。仅文档对齐既有行为，属 documentation；完整核实输出见 review-fix-r1-validation.txt。",
      "fix_commit": "287161673bf3f11a865f2e8a40a023a65bdc1f65",
      "mechanical": "documentation",
      "verification": "最终提交 287161673bf3f11a865f2e8a40a023a65bdc1f65：go build ./...、go vet ./... 均退出 0；go test -json -count=1 ./...：19 包、1001 测试项通过，1 项跳过（TestWindowsConsoleLauncher）；go test -race -json -count=1 ./internal/board ./internal/liveness：2 包、491 测试项通过，0 项跳过；测试统计含子用例，两条测试命令均退出 0。TestDispatchWrapUpSnapshotUsesCurrentGrantDeadline 通过，覆盖原意图已过期、新 grant 期限仍有效且原期限不变。逐句对照 snapshotDispatch、dispatchAcceptBefore、summarizeDispatch、dispatchWait 和 docs/durable-dispatch.md:97；本轮仅 docs/subscription-facts.md 与 rules/KANDER-KANBAN-RULES.md，8 行增加、6 行删除。"
    },
    {
      "submitted_revision": 31,
      "record_id": "cr-r1-qa-01-fixed",
      "run_id": "g3-qa-r1",
      "finding_id": "QA-01",
      "batch_id": "g3-batch-one",
      "task_id": "20260908-coordinator-recovery-task",
      "author": "codex",
      "recorded_at": "2026-09-07T22:07:26.339984149Z",
      "report_hash": "eeb97adabf9023edf939e7aab588e63885f250d41e80a4af9e93221cf46c7c5c",
      "original": "Inferred；置信度高。完整成员集合中的 todo 卡首次 start 后，STARTED_AT 改变 planCycle，reconcile 永久拒绝该成员；再次 claim 保留旧 Cycle，导致串行启动后全组进度无法恢复。最小修复：明确保存未启动状态，在 CAS 下核验首次启动并绑定执行周期，继续拒绝已有周期的静默替换；补充含 todo 成员的双卡首次启动对账及非法周期变化测试。",
      "status": "fixed",
      "basis": "根因 CR-01，合并 g3-pm-r1/PM-02 与 g3-qa-r1/QA-01，两份绑定记录共同引用同一修复，不重复计数。独立核对被审提交 0b304a8399cdbc8db2c005e390afd3f6127532af 的 coordinator.go 成员初始化、coordinator_reconcile.go 周期拒绝及 launch/metadata.go 启动元数据路径；双卡含 todo 的 TestCoordinatorBindsFirstStartFromCompleteMemberSnapshot/working 实跑输出 first start blocked full group recovery / member revision or execution cycle changed，确认缺陷。修复持久 awaiting_start；在任务与检查点 CAS 内凭更高 revision、OWNER/STARTED_AT 和已启动卡态绑定一次，保留历史，拒绝已绑定周期替换。源码与文档均核对。",
      "fix_commit": "4a55248377c8fa35edddd862acee3473d877b2e5",
      "verification": "最终提交 5845e6fd0f2b503313030349fa211a7791a50169（包含 fix_commit；rebase 后重新实跑）：go test -json -count=1 ./... 退出0，19包1016 PASS/1 SKIP（顶层635 PASS/1 SKIP）；go test -race -json -count=1 ./internal/fs ./internal/board ./internal/launch ./internal/review ./internal/cli ./internal/liveness ./internal/notify ./internal/window 退出0，8包752 PASS/1 SKIP（顶层414 PASS/1 SKIP）；go build ./...、go vet ./...、gofmt 和 git diff --check 均通过。五个新增回归在全量与 race 中 PASS，覆盖本根因、重复/重启及拒绝路径；细节与原始输出见本卡 verification/r1/summary.json、all.jsonl、race.jsonl、checks.json。原生Windows和真实tmux/herdr/Agent未执行；假终端和交叉构建不算实机验证。"
    },
    {
      "submitted_revision": 32,
      "record_id": "cr-r1-qa-02-fixed",
      "run_id": "g3-qa-r1",
      "finding_id": "QA-02",
      "batch_id": "g3-batch-one",
      "task_id": "20260908-coordinator-recovery-task",
      "author": "codex",
      "recorded_at": "2026-09-07T22:07:28.091189567Z",
      "report_hash": "eeb97adabf9023edf939e7aab588e63885f250d41e80a4af9e93221cf46c7c5c",
      "original": "Inferred；置信度高。completed fix 的历史对账仍调用禁止闭批修改的 reviewRunForMutation。fix 完成且复审闭批后、创建 wrap-up 前重启，完整原件与同轮回执仍触发 batch already closed，阻断正常收尾恢复。最小修复：历史消费使用只读原件校验并保留身份、完整发布、lineage、assignment、作者验证，创建及发送继续拒绝闭批；补充闭批后重启、重复对账和原件损坏测试。",
      "status": "fixed",
      "basis": "根因 CR-02，合并 g3-pm-r1/PM-01 与 g3-qa-r1/QA-02，两份绑定记录共同引用同一修复，不重复计数。独立核对被审提交 0b304a8399cdbc8db2c005e390afd3f6127532af 的 completed fix 历史路径，仍经 reviewRunForMutation 的闭批写入门禁。TestCoordinatorCompletedFixSurvivesClosedBatchRestart 在完成 fix、PM 增量复审及 QA、闭批生产者后实跑四个子用例均输出 closed batch blocked historical completion / batch already closed，确认缺陷。修复将成功 run 的只读原件校验提取复用，保持完整发布、身份、lineage、assignment 和作者校验；创建/发送与作者修改仍使用闭批拒绝门禁。",
      "fix_commit": "5845e6fd0f2b503313030349fa211a7791a50169",
      "verification": "最终提交 5845e6fd0f2b503313030349fa211a7791a50169（包含 fix_commit；rebase 后重新实跑）：go test -json -count=1 ./... 退出0，19包1016 PASS/1 SKIP（顶层635 PASS/1 SKIP）；go test -race -json -count=1 ./internal/fs ./internal/board ./internal/launch ./internal/review ./internal/cli ./internal/liveness ./internal/notify ./internal/window 退出0，8包752 PASS/1 SKIP（顶层414 PASS/1 SKIP）；go build ./...、go vet ./...、gofmt 和 git diff --check 均通过。五个新增回归在全量与 race 中 PASS，覆盖本根因、重复/重启及拒绝路径；细节与原始输出见本卡 verification/r1/summary.json、all.jsonl、race.jsonl、checks.json。原生Windows和真实tmux/herdr/Agent未执行；假终端和交叉构建不算实机验证。"
    }
  ]
}

Current batch disposition (tool generated):
{
  "schema": 1,
  "batch": {
    "plan_id": "g3-bindings-recovery",
    "task_context_hash": "4b654491f5ecee80391dffd444b1eb3c9edea6ca56a4f0bf7e1bbc4b7ac06ace",
    "schema": 1,
    "batch_id": "g3-batch-one",
    "task_ids": [
      "20260907-subscription-dispatch-facts-task",
      "20260908-coordinator-recovery-task",
      "20260908-dispatch-evidence-binding-task"
    ],
    "base": "ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3",
    "target_commit": "5845e6fd0f2b503313030349fa211a7791a50169",
    "report_language": "zh-CN",
    "requirements": {
      "CSA": "N/A: 本仓库 AGENTS.md 第 7 条，第二阶段安全角色一律 N/A",
      "Hacker": "N/A: 本仓库 AGENTS.md 第 7 条，第二阶段安全角色一律 N/A",
      "PM": "required",
      "QA": "required"
    },
    "advances": [
      {
        "previous_target": "0b304a8399cdbc8db2c005e390afd3f6127532af",
        "target": "5845e6fd0f2b503313030349fa211a7791a50169",
        "reason": "g3-batch-one 第 1 轮 6 个来源 finding（3 个根因）的作者修复交付已依次 ff 接收到组分支，推进批次目标以便 PM/QA 增量复审",
        "deliveries": {
          "287161673bf3f11a865f2e8a40a023a65bdc1f65": "20260908-dispatch-evidence-binding-task",
          "4a55248377c8fa35edddd862acee3473d877b2e5": "20260908-coordinator-recovery-task",
          "5845e6fd0f2b503313030349fa211a7791a50169": "20260908-coordinator-recovery-task"
        }
      }
    ],
    "revision": 2
  },
  "runs": [
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "20260908-bindings-recovery-group",
        "run_id": "g3-pm-r1",
        "batch_id": "g3-batch-one",
        "task_ids": [
          "20260907-subscription-dispatch-facts-task",
          "20260908-coordinator-recovery-task",
          "20260908-dispatch-evidence-binding-task"
        ],
        "role": "PM",
        "reviewer": "codex",
        "model": "gpt-6-astra",
        "effort": "medium",
        "cwd": "/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group",
        "base": "ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3",
        "commit": "0b304a8399cdbc8db2c005e390afd3f6127532af",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "c50ab6c799b2dd85089c43824aaf9440cfb6341151648b496465613f138f99e5",
          "task-context.md": "4b654491f5ecee80391dffd444b1eb3c9edea6ca56a4f0bf7e1bbc4b7ac06ace"
        },
        "kander_version": "20260907T173303Z-7e6fe2073ff0",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-07T21:42:28.688508327Z",
        "finished_at": "2026-09-07T21:47:27.58729152Z",
        "duration_ms": 298898,
        "hashes": {
          "error.log": "19a31708346de544eb1f236e00f8415843f08d919e444c1ee37ff1905ca8f598",
          "evidence.txt": "e89b872a71d8f2975c93642490a3924c6a2f9833be3ae64be1234bba5ffad581",
          "output.raw": "17caf97692b9cd02007b1999905a5e2f4f5f678a18b00a5b7d8e0539c331e44c",
          "prompt.txt": "6592780d51fbd3f0cb1ab732b944710d364b55cf3cf237d185f4941f160dc56c",
          "report.md": "17caf97692b9cd02007b1999905a5e2f4f5f678a18b00a5b7d8e0539c331e44c",
          "review-context.md": "c50ab6c799b2dd85089c43824aaf9440cfb6341151648b496465613f138f99e5",
          "stdout.log": "9367f6726d2b8e9e4bb4736e015bff1ebf76e8f950bdf4e81b781c4707168d43",
          "task-context.md": "4b654491f5ecee80391dffd444b1eb3c9edea6ca56a4f0bf7e1bbc4b7ac06ace"
        },
        "published": {
          "20260907-subscription-dispatch-facts-task": true,
          "20260908-coordinator-recovery-task": true,
          "20260908-dispatch-evidence-binding-task": true
        }
      },
      "findings": {
        "FINDINGS": [
          {
            "id": "PM-01",
            "tier": "high",
            "text": "Inferred，置信度高。已完成 fix 所属批次闭合后、尚未创建 wrap-up 时，编排重启或重复 reconcile 会因 batch already closed 拒绝有效历史交付，违反 completed 与闭批原件恢复契约。最小修复：历史 completed 路径使用允许合法闭批的只读原件校验，保留身份、轮次、完整发布及作者原件检查；发送路径继续拒绝闭批。",
            "evidence": "internal/board/coordinator_reconcile.go:86-90 对 completed fix 调用历史校验；internal/board/dispatch_evidence.go:106-108 仍无条件调用 reviewRunForMutation；internal/board/review_disposition.go:107-110 在存在 closure 时返回 batch already closed。historical 标志只绕过当前 target 和后继轮限制，未绕过该写入门禁。契约：coordinator-recovery ACCEPTANCE_CRITERIA 第4、6、7项。"
          },
          {
            "id": "PM-02",
            "tier": "high",
            "text": "Inferred，置信度高。完整成员 claim 包含尚未启动的 todo 卡时，保存空 STARTED_AT 对应的 cycle；后续正常 start 改变 cycle，整组 reconcile 永久冲突。重新 claim 保留旧成员，没有受控推进入口，阻断按依赖顺序启动成员后的阶段恢复。最小修复：通过 CAS 支持未启动成员首次建立执行周期，验证持久启动事实并保留历史，继续拒绝未经授权的既有周期替换。",
            "evidence": "internal/board/coordinator.go:220-228 首次保存 cycle，重新 claim 跳过已有成员；:293-295 要求完整成员；internal/board/coordinator_reconcile.go:15-18 拒绝所有 cycle 变化；internal/board/review_plan.go:52-53 以 task ID 和 STARTED_AT 计算 cycle；internal/launch/metadata.go:46-57 正常 start 填入 STARTED_AT。rules/KANDER-TASK-GROUP-RULES.md:186,203 同时要求顺序启动新就绪成员及完整成员 claim。契约：coordinator-recovery GOAL、ACCEPTANCE_CRITERIA 第2、4、6项。"
          },
          {
            "id": "PM-03",
            "tier": "medium",
            "text": "Observed，置信度高。订阅中文文档和英文协议把摘要 confirm_by 描述为原意图期限，但 wrap-up-only 新 epoch 实际使用专用 grant 的新接受期限。消费者依文档比对 intent 与 snapshot 会误判合法恢复为续期。最小修复：统一描述当前 epoch 的实际接受期限，明确普通 intent 与专用 grant 的来源，以及同 epoch 重试不续期。",
            "evidence": "docs/subscription-facts.md:43-46,55 和 rules/KANDER-KANBAN-RULES.md:312,314 声称使用原确认期限；internal/board/dispatch_snapshot.go:61 调用 dispatchAcceptBefore，internal/board/dispatch_wrapup.go:85-89 在专用授权存在时返回 WrapUpAuthority.ConfirmBy。docs/durable-dispatch.md:97 已正确说明此行为。契约要求中文文档、英文发布协议与实现一致。",
            "mechanical": "documentation"
          }
        ],
        "NON_BLOCKING": []
      },
      "assignment": {
        "run_id": "g3-pm-r1",
        "batch_id": "g3-batch-one",
        "author": "coordinator",
        "basis": "按各卡本轮实际改动归属。PM-01（闭批后历史 fix 对账被 reviewRunForMutation 拒绝）与 PM-02（todo 成员首次 start 后 planCycle 变化导致 reconcile 永久拒绝）的行为均由 0b304a8399cdbc8db2c005e390afd3f6127532af 新增的 coordinator 检查点与 historical 对账路径引入，归 20260908-coordinator-recovery-task；PM-01 的最小修复虽落在 dispatch_evidence.go，但该 historical 分支及其调用点由本轮 coordinator 卡新增，dispatch-evidence-binding 在自身交付 6f5ed32 时不存在该缺陷。PM-03 的文档与实现不符由 71a6571（属 20260908-dispatch-evidence-binding-task 交付）改变 confirm_by 语义而未同步订阅文档引入，归该卡。",
        "items": {
          "PM-01": [
            "20260908-coordinator-recovery-task"
          ],
          "PM-02": [
            "20260908-coordinator-recovery-task"
          ],
          "PM-03": [
            "20260908-dispatch-evidence-binding-task"
          ]
        },
        "owners": {
          "20260908-coordinator-recovery-task": "codex",
          "20260908-dispatch-evidence-binding-task": "codex"
        },
        "recorded_at": "2026-09-07T21:49:02.386275308Z"
      },
      "records": [
        {
          "submitted_revision": 18,
          "record_id": "binding-r1-pm-03",
          "run_id": "g3-pm-r1",
          "finding_id": "PM-03",
          "batch_id": "g3-batch-one",
          "task_id": "20260908-dispatch-evidence-binding-task",
          "author": "codex",
          "recorded_at": "2026-09-07T21:58:13.349500816Z",
          "report_hash": "17caf97692b9cd02007b1999905a5e2f4f5f678a18b00a5b7d8e0539c331e44c",
          "original": "Observed，置信度高。订阅中文文档和英文协议把摘要 confirm_by 描述为原意图期限，但 wrap-up-only 新 epoch 实际使用专用 grant 的新接受期限。消费者依文档比对 intent 与 snapshot 会误判合法恢复为续期。最小修复：统一描述当前 epoch 的实际接受期限，明确普通 intent 与专用 grant 的来源，以及同 epoch 重试不续期。",
          "status": "fixed",
          "basis": "合并根因 PM-03/QA-03，来源 (g3-pm-r1, PM-03) 与 (g3-qa-r1, QA-03)，分别保留原件绑定；独立核实后 confirmed，现 fixed。git show 0b304a8399cdbc8db2c005e390afd3f6127532af:\u003cpath\u003e 全部退出 0：internal/board/dispatch_snapshot.go:59-62 保留 Input.CreatedAt 并调用 dispatchAcceptBefore；internal/board/dispatch_wrapup.go:85-89 优先取 WrapUpAuthority.ConfirmBy；internal/liveness/subscribe_dispatch.go:22-38 按摘要期限计算 overdue/唤醒。该目标 docs/subscription-facts.md:43-46,55-56 及 rules/KANDER-KANBAN-RULES.md:312,314 却称原期限，原意图过期后授予新 epoch 即触发误导。修复统一当前 epoch 有效接受期限、普通意图/专用 grant 来源、created_at/age_seconds 来源与同 epoch 重试/订阅重启不续期。仅文档对齐既有行为，属 documentation；完整核实输出见 review-fix-r1-validation.txt。",
          "fix_commit": "287161673bf3f11a865f2e8a40a023a65bdc1f65",
          "mechanical": "documentation",
          "verification": "最终提交 287161673bf3f11a865f2e8a40a023a65bdc1f65：go build ./...、go vet ./... 均退出 0；go test -json -count=1 ./...：19 包、1001 测试项通过，1 项跳过（TestWindowsConsoleLauncher）；go test -race -json -count=1 ./internal/board ./internal/liveness：2 包、491 测试项通过，0 项跳过；测试统计含子用例，两条测试命令均退出 0。TestDispatchWrapUpSnapshotUsesCurrentGrantDeadline 通过，覆盖原意图已过期、新 grant 期限仍有效且原期限不变。逐句对照 snapshotDispatch、dispatchAcceptBefore、summarizeDispatch、dispatchWait 和 docs/durable-dispatch.md:97；本轮仅 docs/subscription-facts.md 与 rules/KANDER-KANBAN-RULES.md，8 行增加、6 行删除。"
        },
        {
          "submitted_revision": 29,
          "record_id": "cr-r1-pm-01-fixed",
          "run_id": "g3-pm-r1",
          "finding_id": "PM-01",
          "batch_id": "g3-batch-one",
          "task_id": "20260908-coordinator-recovery-task",
          "author": "codex",
          "recorded_at": "2026-09-07T22:07:22.826715673Z",
          "report_hash": "17caf97692b9cd02007b1999905a5e2f4f5f678a18b00a5b7d8e0539c331e44c",
          "original": "Inferred，置信度高。已完成 fix 所属批次闭合后、尚未创建 wrap-up 时，编排重启或重复 reconcile 会因 batch already closed 拒绝有效历史交付，违反 completed 与闭批原件恢复契约。最小修复：历史 completed 路径使用允许合法闭批的只读原件校验，保留身份、轮次、完整发布及作者原件检查；发送路径继续拒绝闭批。",
          "status": "fixed",
          "basis": "根因 CR-02，合并 g3-pm-r1/PM-01 与 g3-qa-r1/QA-02，两份绑定记录共同引用同一修复，不重复计数。独立核对被审提交 0b304a8399cdbc8db2c005e390afd3f6127532af 的 completed fix 历史路径，仍经 reviewRunForMutation 的闭批写入门禁。TestCoordinatorCompletedFixSurvivesClosedBatchRestart 在完成 fix、PM 增量复审及 QA、闭批生产者后实跑四个子用例均输出 closed batch blocked historical completion / batch already closed，确认缺陷。修复将成功 run 的只读原件校验提取复用，保持完整发布、身份、lineage、assignment 和作者校验；创建/发送与作者修改仍使用闭批拒绝门禁。",
          "fix_commit": "5845e6fd0f2b503313030349fa211a7791a50169",
          "verification": "最终提交 5845e6fd0f2b503313030349fa211a7791a50169（包含 fix_commit；rebase 后重新实跑）：go test -json -count=1 ./... 退出0，19包1016 PASS/1 SKIP（顶层635 PASS/1 SKIP）；go test -race -json -count=1 ./internal/fs ./internal/board ./internal/launch ./internal/review ./internal/cli ./internal/liveness ./internal/notify ./internal/window 退出0，8包752 PASS/1 SKIP（顶层414 PASS/1 SKIP）；go build ./...、go vet ./...、gofmt 和 git diff --check 均通过。五个新增回归在全量与 race 中 PASS，覆盖本根因、重复/重启及拒绝路径；细节与原始输出见本卡 verification/r1/summary.json、all.jsonl、race.jsonl、checks.json。原生Windows和真实tmux/herdr/Agent未执行；假终端和交叉构建不算实机验证。"
        },
        {
          "submitted_revision": 30,
          "record_id": "cr-r1-pm-02-fixed",
          "run_id": "g3-pm-r1",
          "finding_id": "PM-02",
          "batch_id": "g3-batch-one",
          "task_id": "20260908-coordinator-recovery-task",
          "author": "codex",
          "recorded_at": "2026-09-07T22:07:24.555913777Z",
          "report_hash": "17caf97692b9cd02007b1999905a5e2f4f5f678a18b00a5b7d8e0539c331e44c",
          "original": "Inferred，置信度高。完整成员 claim 包含尚未启动的 todo 卡时，保存空 STARTED_AT 对应的 cycle；后续正常 start 改变 cycle，整组 reconcile 永久冲突。重新 claim 保留旧成员，没有受控推进入口，阻断按依赖顺序启动成员后的阶段恢复。最小修复：通过 CAS 支持未启动成员首次建立执行周期，验证持久启动事实并保留历史，继续拒绝未经授权的既有周期替换。",
          "status": "fixed",
          "basis": "根因 CR-01，合并 g3-pm-r1/PM-02 与 g3-qa-r1/QA-01，两份绑定记录共同引用同一修复，不重复计数。独立核对被审提交 0b304a8399cdbc8db2c005e390afd3f6127532af 的 coordinator.go 成员初始化、coordinator_reconcile.go 周期拒绝及 launch/metadata.go 启动元数据路径；双卡含 todo 的 TestCoordinatorBindsFirstStartFromCompleteMemberSnapshot/working 实跑输出 first start blocked full group recovery / member revision or execution cycle changed，确认缺陷。修复持久 awaiting_start；在任务与检查点 CAS 内凭更高 revision、OWNER/STARTED_AT 和已启动卡态绑定一次，保留历史，拒绝已绑定周期替换。源码与文档均核对。",
          "fix_commit": "4a55248377c8fa35edddd862acee3473d877b2e5",
          "verification": "最终提交 5845e6fd0f2b503313030349fa211a7791a50169（包含 fix_commit；rebase 后重新实跑）：go test -json -count=1 ./... 退出0，19包1016 PASS/1 SKIP（顶层635 PASS/1 SKIP）；go test -race -json -count=1 ./internal/fs ./internal/board ./internal/launch ./internal/review ./internal/cli ./internal/liveness ./internal/notify ./internal/window 退出0，8包752 PASS/1 SKIP（顶层414 PASS/1 SKIP）；go build ./...、go vet ./...、gofmt 和 git diff --check 均通过。五个新增回归在全量与 race 中 PASS，覆盖本根因、重复/重启及拒绝路径；细节与原始输出见本卡 verification/r1/summary.json、all.jsonl、race.jsonl、checks.json。原生Windows和真实tmux/herdr/Agent未执行；假终端和交叉构建不算实机验证。"
        }
      ]
    },
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "20260908-bindings-recovery-group",
        "run_id": "g3-pm-r2",
        "batch_id": "g3-batch-one",
        "previous_run_id": "g3-pm-r1",
        "task_ids": [
          "20260907-subscription-dispatch-facts-task",
          "20260908-coordinator-recovery-task",
          "20260908-dispatch-evidence-binding-task"
        ],
        "role": "PM",
        "reviewer": "codex",
        "model": "gpt-6-astra",
        "effort": "medium",
        "cwd": "/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group",
        "base": "ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3",
        "commit": "5845e6fd0f2b503313030349fa211a7791a50169",
        "reviewed_commit": "0b304a8399cdbc8db2c005e390afd3f6127532af",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "7365acf01dce54a755b3c9dd7371964329df33c4ab61710d21cc794ed5ea5a24",
          "task-context.md": "4b654491f5ecee80391dffd444b1eb3c9edea6ca56a4f0bf7e1bbc4b7ac06ace"
        },
        "kander_version": "20260907T173303Z-7e6fe2073ff0",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-07T22:11:50.858281447Z",
        "finished_at": "2026-09-07T22:14:08.802992236Z",
        "duration_ms": 137944,
        "hashes": {
          "error.log": "671f3563e2520076516c6a3be82cc7eddc7183f07289b5325b504dbd2c128930",
          "evidence.txt": "47343881ca87688859ddad921dc62267f385931a92ade52717c20600c28952a1",
          "output.raw": "aeee4c4203f8cd5d3ac5fc49ab86d442b06b7809367a6029f76e5d11558cf49a",
          "prompt.txt": "ebe979b59e0a25a5a3fb0bc98c877c1a8e0bcb6454d53ff7be492f4b58b2eb61",
          "report.md": "aeee4c4203f8cd5d3ac5fc49ab86d442b06b7809367a6029f76e5d11558cf49a",
          "review-context.md": "7365acf01dce54a755b3c9dd7371964329df33c4ab61710d21cc794ed5ea5a24",
          "stdout.log": "1a19fadffe9495476bdd67d1c8b2231208078000e16216ef361f86e5c796af7b",
          "task-context.md": "4b654491f5ecee80391dffd444b1eb3c9edea6ca56a4f0bf7e1bbc4b7ac06ace"
        },
        "published": {
          "20260907-subscription-dispatch-facts-task": true,
          "20260908-coordinator-recovery-task": true,
          "20260908-dispatch-evidence-binding-task": true
        }
      },
      "findings": {
        "FINDINGS": [],
        "NON_BLOCKING": []
      },
      "assignment": {
        "run_id": "g3-pm-r2",
        "batch_id": "g3-batch-one",
        "author": "coordinator",
        "basis": "增量复审报告结构化块 FINDINGS 与 NON_BLOCKING 均为空数组，无条目可归属",
        "items": {},
        "owners": {},
        "recorded_at": "2026-09-07T22:14:31.605430176Z"
      },
      "records": []
    },
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "20260908-bindings-recovery-group",
        "run_id": "g3-qa-r1",
        "batch_id": "g3-batch-one",
        "task_ids": [
          "20260907-subscription-dispatch-facts-task",
          "20260908-coordinator-recovery-task",
          "20260908-dispatch-evidence-binding-task"
        ],
        "role": "QA",
        "reviewer": "codex",
        "model": "gpt-6-astra",
        "effort": "medium",
        "cwd": "/home/dualf/works/kander/worktrees/20260908-bindings-recovery-group",
        "base": "ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3",
        "commit": "0b304a8399cdbc8db2c005e390afd3f6127532af",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "c50ab6c799b2dd85089c43824aaf9440cfb6341151648b496465613f138f99e5",
          "task-context.md": "4b654491f5ecee80391dffd444b1eb3c9edea6ca56a4f0bf7e1bbc4b7ac06ace"
        },
        "kander_version": "20260907T173303Z-7e6fe2073ff0",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-07T21:42:34.109544878Z",
        "finished_at": "2026-09-07T21:47:17.466621777Z",
        "duration_ms": 283357,
        "hashes": {
          "error.log": "a055940e158dee59718b35eb8104eb918788436ca748b863237c418b4a04824b",
          "evidence.txt": "e89b872a71d8f2975c93642490a3924c6a2f9833be3ae64be1234bba5ffad581",
          "output.raw": "eeb97adabf9023edf939e7aab588e63885f250d41e80a4af9e93221cf46c7c5c",
          "prompt.txt": "b2aa4d655ef087661de649ff3742aeea35576c71c86d23e974eaea007eef99fc",
          "report.md": "eeb97adabf9023edf939e7aab588e63885f250d41e80a4af9e93221cf46c7c5c",
          "review-context.md": "c50ab6c799b2dd85089c43824aaf9440cfb6341151648b496465613f138f99e5",
          "stdout.log": "8167a8c695f351f95f9ef8265fc13c52ba84c49b4d1cad50d341feaa660fc981",
          "task-context.md": "4b654491f5ecee80391dffd444b1eb3c9edea6ca56a4f0bf7e1bbc4b7ac06ace"
        },
        "published": {
          "20260907-subscription-dispatch-facts-task": true,
          "20260908-coordinator-recovery-task": true,
          "20260908-dispatch-evidence-binding-task": true
        }
      },
      "findings": {
        "FINDINGS": [
          {
            "id": "QA-01",
            "tier": "high",
            "text": "Inferred；置信度高。完整成员集合中的 todo 卡首次 start 后，STARTED_AT 改变 planCycle，reconcile 永久拒绝该成员；再次 claim 保留旧 Cycle，导致串行启动后全组进度无法恢复。最小修复：明确保存未启动状态，在 CAS 下核验首次启动并绑定执行周期，继续拒绝已有周期的静默替换；补充含 todo 成员的双卡首次启动对账及非法周期变化测试。",
            "evidence": "internal/board/coordinator.go:220-228 保存周期且再次 claim 跳过已有成员；internal/board/coordinator_reconcile.go:15-18 无条件拒绝周期变化；internal/board/review_plan.go:52-53 将 STARTED_AT 纳入周期；internal/launch/metadata.go:46-57 首次启动填写 STARTED_AT，internal/launch/commands.go:122,176 发布该元数据；rules/KANDER-TASK-GROUP-RULES.md:203-204 要求完整成员集合及持续对账。"
          },
          {
            "id": "QA-02",
            "tier": "high",
            "text": "Inferred；置信度高。completed fix 的历史对账仍调用禁止闭批修改的 reviewRunForMutation。fix 完成且复审闭批后、创建 wrap-up 前重启，完整原件与同轮回执仍触发 batch already closed，阻断正常收尾恢复。最小修复：历史消费使用只读原件校验并保留身份、完整发布、lineage、assignment、作者验证，创建及发送继续拒绝闭批；补充闭批后重启、重复对账和原件损坏测试。",
            "evidence": "internal/board/coordinator_reconcile.go:86-90 对 completed fix 调用历史模式；internal/board/dispatch_evidence.go:86-106 仅放宽 target 校验，仍无条件调用 reviewRunForMutation；internal/board/review_disposition.go:107-110 遇 closure 返回 batch already closed；internal/board/coordinator_regression_test.go:130-140 现有用例只推进 target，未覆盖闭批。"
          },
          {
            "id": "QA-03",
            "tier": "medium",
            "text": "Observed；置信度高。订阅中文文档与英文协议将 confirm_by 定义为原始意图期限，但 wrap-up-only grant 实际输出新 epoch 的独立接受期限。原意图过期后取得新 grant，按文档实现的消费者会误判 overdue 或期限重置。最小修复：说明 created_at 保留原意图时间，confirm_by 为当前 epoch 的有效期限；专用 grant 使用自身期限，同 epoch 重试及重启不续期。",
            "evidence": "docs/subscription-facts.md:43-46,55-56 与 rules/KANDER-KANBAN-RULES.md:312,314 声称原始确认期限；internal/board/dispatch_snapshot.go:59-62 使用 dispatchAcceptBefore；internal/board/dispatch_wrapup.go:85-89 优先返回 WrapUpAuthority.ConfirmBy；docs/durable-dispatch.md:97 已正确说明该例外，订阅文档尚未同步。",
            "mechanical": "documentation"
          }
        ],
        "NON_BLOCKING": []
      },
      "assignment": {
        "run_id": "g3-qa-r1",
        "batch_id": "g3-batch-one",
        "author": "coordinator",
        "basis": "与 g3-pm-r1 同一归属依据。QA-01 与 PM-02 同根因（coordinator 执行周期），QA-02 与 PM-01 同根因（闭批后历史对账），QA-03 与 PM-03 同根因（订阅文档 confirm_by 语义）；处置时按“一个根因一个 ID”合并并同时引用两个来源 ID。",
        "items": {
          "QA-01": [
            "20260908-coordinator-recovery-task"
          ],
          "QA-02": [
            "20260908-coordinator-recovery-task"
          ],
          "QA-03": [
            "20260908-dispatch-evidence-binding-task"
          ]
        },
        "owners": {
          "20260908-coordinator-recovery-task": "codex",
          "20260908-dispatch-evidence-binding-task": "codex"
        },
        "recorded_at": "2026-09-07T21:49:03.491391558Z"
      },
      "records": [
        {
          "submitted_revision": 19,
          "record_id": "binding-r1-qa-03",
          "run_id": "g3-qa-r1",
          "finding_id": "QA-03",
          "batch_id": "g3-batch-one",
          "task_id": "20260908-dispatch-evidence-binding-task",
          "author": "codex",
          "recorded_at": "2026-09-07T21:58:15.008986762Z",
          "report_hash": "eeb97adabf9023edf939e7aab588e63885f250d41e80a4af9e93221cf46c7c5c",
          "original": "Observed；置信度高。订阅中文文档与英文协议将 confirm_by 定义为原始意图期限，但 wrap-up-only grant 实际输出新 epoch 的独立接受期限。原意图过期后取得新 grant，按文档实现的消费者会误判 overdue 或期限重置。最小修复：说明 created_at 保留原意图时间，confirm_by 为当前 epoch 的有效期限；专用 grant 使用自身期限，同 epoch 重试及重启不续期。",
          "status": "fixed",
          "basis": "合并根因 PM-03/QA-03，来源 (g3-pm-r1, PM-03) 与 (g3-qa-r1, QA-03)，分别保留原件绑定；独立核实后 confirmed，现 fixed。git show 0b304a8399cdbc8db2c005e390afd3f6127532af:\u003cpath\u003e 全部退出 0：internal/board/dispatch_snapshot.go:59-62 保留 Input.CreatedAt 并调用 dispatchAcceptBefore；internal/board/dispatch_wrapup.go:85-89 优先取 WrapUpAuthority.ConfirmBy；internal/liveness/subscribe_dispatch.go:22-38 按摘要期限计算 overdue/唤醒。该目标 docs/subscription-facts.md:43-46,55-56 及 rules/KANDER-KANBAN-RULES.md:312,314 却称原期限，原意图过期后授予新 epoch 即触发误导。修复统一当前 epoch 有效接受期限、普通意图/专用 grant 来源、created_at/age_seconds 来源与同 epoch 重试/订阅重启不续期。仅文档对齐既有行为，属 documentation；完整核实输出见 review-fix-r1-validation.txt。",
          "fix_commit": "287161673bf3f11a865f2e8a40a023a65bdc1f65",
          "mechanical": "documentation",
          "verification": "最终提交 287161673bf3f11a865f2e8a40a023a65bdc1f65：go build ./...、go vet ./... 均退出 0；go test -json -count=1 ./...：19 包、1001 测试项通过，1 项跳过（TestWindowsConsoleLauncher）；go test -race -json -count=1 ./internal/board ./internal/liveness：2 包、491 测试项通过，0 项跳过；测试统计含子用例，两条测试命令均退出 0。TestDispatchWrapUpSnapshotUsesCurrentGrantDeadline 通过，覆盖原意图已过期、新 grant 期限仍有效且原期限不变。逐句对照 snapshotDispatch、dispatchAcceptBefore、summarizeDispatch、dispatchWait 和 docs/durable-dispatch.md:97；本轮仅 docs/subscription-facts.md 与 rules/KANDER-KANBAN-RULES.md，8 行增加、6 行删除。"
        },
        {
          "submitted_revision": 31,
          "record_id": "cr-r1-qa-01-fixed",
          "run_id": "g3-qa-r1",
          "finding_id": "QA-01",
          "batch_id": "g3-batch-one",
          "task_id": "20260908-coordinator-recovery-task",
          "author": "codex",
          "recorded_at": "2026-09-07T22:07:26.339984149Z",
          "report_hash": "eeb97adabf9023edf939e7aab588e63885f250d41e80a4af9e93221cf46c7c5c",
          "original": "Inferred；置信度高。完整成员集合中的 todo 卡首次 start 后，STARTED_AT 改变 planCycle，reconcile 永久拒绝该成员；再次 claim 保留旧 Cycle，导致串行启动后全组进度无法恢复。最小修复：明确保存未启动状态，在 CAS 下核验首次启动并绑定执行周期，继续拒绝已有周期的静默替换；补充含 todo 成员的双卡首次启动对账及非法周期变化测试。",
          "status": "fixed",
          "basis": "根因 CR-01，合并 g3-pm-r1/PM-02 与 g3-qa-r1/QA-01，两份绑定记录共同引用同一修复，不重复计数。独立核对被审提交 0b304a8399cdbc8db2c005e390afd3f6127532af 的 coordinator.go 成员初始化、coordinator_reconcile.go 周期拒绝及 launch/metadata.go 启动元数据路径；双卡含 todo 的 TestCoordinatorBindsFirstStartFromCompleteMemberSnapshot/working 实跑输出 first start blocked full group recovery / member revision or execution cycle changed，确认缺陷。修复持久 awaiting_start；在任务与检查点 CAS 内凭更高 revision、OWNER/STARTED_AT 和已启动卡态绑定一次，保留历史，拒绝已绑定周期替换。源码与文档均核对。",
          "fix_commit": "4a55248377c8fa35edddd862acee3473d877b2e5",
          "verification": "最终提交 5845e6fd0f2b503313030349fa211a7791a50169（包含 fix_commit；rebase 后重新实跑）：go test -json -count=1 ./... 退出0，19包1016 PASS/1 SKIP（顶层635 PASS/1 SKIP）；go test -race -json -count=1 ./internal/fs ./internal/board ./internal/launch ./internal/review ./internal/cli ./internal/liveness ./internal/notify ./internal/window 退出0，8包752 PASS/1 SKIP（顶层414 PASS/1 SKIP）；go build ./...、go vet ./...、gofmt 和 git diff --check 均通过。五个新增回归在全量与 race 中 PASS，覆盖本根因、重复/重启及拒绝路径；细节与原始输出见本卡 verification/r1/summary.json、all.jsonl、race.jsonl、checks.json。原生Windows和真实tmux/herdr/Agent未执行；假终端和交叉构建不算实机验证。"
        },
        {
          "submitted_revision": 32,
          "record_id": "cr-r1-qa-02-fixed",
          "run_id": "g3-qa-r1",
          "finding_id": "QA-02",
          "batch_id": "g3-batch-one",
          "task_id": "20260908-coordinator-recovery-task",
          "author": "codex",
          "recorded_at": "2026-09-07T22:07:28.091189567Z",
          "report_hash": "eeb97adabf9023edf939e7aab588e63885f250d41e80a4af9e93221cf46c7c5c",
          "original": "Inferred；置信度高。completed fix 的历史对账仍调用禁止闭批修改的 reviewRunForMutation。fix 完成且复审闭批后、创建 wrap-up 前重启，完整原件与同轮回执仍触发 batch already closed，阻断正常收尾恢复。最小修复：历史消费使用只读原件校验并保留身份、完整发布、lineage、assignment、作者验证，创建及发送继续拒绝闭批；补充闭批后重启、重复对账和原件损坏测试。",
          "status": "fixed",
          "basis": "根因 CR-02，合并 g3-pm-r1/PM-01 与 g3-qa-r1/QA-02，两份绑定记录共同引用同一修复，不重复计数。独立核对被审提交 0b304a8399cdbc8db2c005e390afd3f6127532af 的 completed fix 历史路径，仍经 reviewRunForMutation 的闭批写入门禁。TestCoordinatorCompletedFixSurvivesClosedBatchRestart 在完成 fix、PM 增量复审及 QA、闭批生产者后实跑四个子用例均输出 closed batch blocked historical completion / batch already closed，确认缺陷。修复将成功 run 的只读原件校验提取复用，保持完整发布、身份、lineage、assignment 和作者校验；创建/发送与作者修改仍使用闭批拒绝门禁。",
          "fix_commit": "5845e6fd0f2b503313030349fa211a7791a50169",
          "verification": "最终提交 5845e6fd0f2b503313030349fa211a7791a50169（包含 fix_commit；rebase 后重新实跑）：go test -json -count=1 ./... 退出0，19包1016 PASS/1 SKIP（顶层635 PASS/1 SKIP）；go test -race -json -count=1 ./internal/fs ./internal/board ./internal/launch ./internal/review ./internal/cli ./internal/liveness ./internal/notify ./internal/window 退出0，8包752 PASS/1 SKIP（顶层414 PASS/1 SKIP）；go build ./...、go vet ./...、gofmt 和 git diff --check 均通过。五个新增回归在全量与 race 中 PASS，覆盖本根因、重复/重启及拒绝路径；细节与原始输出见本卡 verification/r1/summary.json、all.jsonl、race.jsonl、checks.json。原生Windows和真实tmux/herdr/Agent未执行；假终端和交叉构建不算实机验证。"
        }
      ]
    }
  ]
}


Caller supplemental context (verbatim; does not replace originals):
Fixes included this round:
本轮只包含对第 1 轮 6 个来源 finding（3 个根因）的修复，共 3 个提交：
- `287161673bf3f11a865f2e8a40a023a65bdc1f65`（20260908-dispatch-evidence-binding-task，PM-03/QA-03）：只改 docs/subscription-facts.md 与 rules/KANDER-KANBAN-RULES.md（+8/−6），把摘要 confirm_by 说明为当前 epoch 的实际接受期限，并说明普通 intent 与 wrap-up 专用 grant 的来源、同 epoch 重试不续期。
- `4a55248377c8fa35edddd862acee3473d877b2e5`（20260908-coordinator-recovery-task，PM-02/QA-01）：新增 coordinator_cycle 首次启动绑定，claim 记录未启动成员后允许在 CAS 下绑定首次执行周期，继续拒绝未授权的既有周期替换。
- `5845e6fd0f2b503313030349fa211a7791a50169`（20260908-coordinator-recovery-task，PM-01/QA-02）：completed fix 的历史对账改为允许合法闭批的只读原件校验，发送路径仍拒绝闭批。
20260907-subscription-dispatch-facts-task 本轮没有新提交，其代码自 6c1cd63 起未变动。

Review focus:
(1) 逐条确认第 1 轮的 3 个根因是否真的关闭，以及修复本身是否引入、加重或掩盖新问题。
(2) 首次启动绑定（4a55248）：是否只允许"未启动成员的首次周期绑定"，仍拒绝已有周期被静默替换；重新 claim、重复绑定、并发 start 是否都走 CAS 且保留历史。
(3) 闭批后历史对账（5845e6f）：只读路径是否仍完整校验身份、完整发布、lineage、assignment 与作者记录；创建与发送路径是否仍然拒绝闭批批次。
(4) 文档修复（2871616）是否与 dispatch_snapshot.go / dispatch_wrapup.go 的实际行为逐句一致，且没有把别处正确的描述改错。

Verification records:
dispatch-evidence-binding 在 287161673bf3f11a865f2e8a40a023a65bdc1f65 实跑：go build、go vet 均 exit=0；go test -json -count=1 ./... 19 包 1001 项通过、1 项跳过（TestWindowsConsoleLauncher）；go test -race ./internal/board ./internal/liveness 2 包 491 项通过。coordinator-recovery 在 5845e6fd0f2b503313030349fa211a7791a50169 重新实跑：go test -json -count=1 ./... 19 包 1016 PASS/1 SKIP；8 包 race 752 PASS/1 SKIP；build/vet/gofmt/diff 及 Windows 交叉构建通过。主控将在本批新 target 上另行实跑 build/vet/全量测试。

Environment gaps:
原生 Windows 与真实 tmux/herdr/Agent 未运行；唯一跳过的用例为原生 Windows 控制台用例。交叉编译与假 CLI 不代表实机通过，属已披露缺口，不得据此单独判定 blocking/high/medium。