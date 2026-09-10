KANDER_AUTOMATIC_CONTEXT_BYTES: 45115
PREVIOUS_RUN_ID: g2-pm-r1
Prior report (verbatim):
Role: PM  
Commit: `ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe`  
Task Context: 三卡任务规格及调用方五项审核重点。  
Reviewed Scope: 协调快照、成员展开、订阅调度与输出、dispatch 存储与回执、notify/resume、epoch 写入、相关测试、文档及三语合并块。

验收拆分：Complete 36、Partial 4、Missing 0、Contradicted 0、Unverifiable 2。存在 4 项 medium，暂不通过。

1. **PM-001 — medium，Inferred，高置信度：resume 接受期限耗尽仍可返回成功。**  
   契约要求无回执到期返回非零（`rules/KANDER-KANBAN-RULES.md:109`）。但 foreground/console 存活校验到期返回 nil（`internal/launch/agent.go:268–277`），随后 `resumeDispatch` 输出未接受状态并返回 nil（`internal/launch/dispatch.go:198–209`）。console 直接成功退出；foreground 若随后退出 0，也报告成功。调用方无法通过退出码识别接受超时。最小修复：重新读取回执后，未接受且期限耗尽必须返回 pending 错误，保留未知执行者。

2. **PM-002 — medium，Inferred，高置信度：dispatch 预解析吞掉消息正文。**  
   `ParseDispatchOptions` 逐 token 提取选项，不跳过其他选项的参数（`internal/launch/dispatch.go:213–239`）。例如 `notify <task-id> --message '--kind=sync'`，正文被当成 dispatch 选项删除，后续报缺少消息。resume 同样受影响。违反未绑定普通消息兼容要求及发布协议第 114 行。最小修复：合并参数解析，或完整识别已有选项的参数边界；正文保持数据语义。

3. **PM-003 — medium，Inferred，高置信度：任务别名在意图落盘后被拒绝。**  
   已有入口接受 `<task-id>.md` 拼写（`internal/board/scan.go:251–262`）。新流程先通过规范化快照，用 `s.Entry.TaskID` 创建意图（`internal/launch/dispatch.go:69–73`），随后 notify/resume 却用原始 task 调用 `ReadDispatch`（`internal/notify/dispatch.go:49,74`；`internal/launch/dispatch.go:134`）。该 API 不规范化，锁作用域拒绝 `.md`（`internal/board/dispatch.go:174–175`；`internal/board/transaction_lock.go:67–73`）。组 review 自动持久模式因此回归：意图已绑定，却尚未发送便失败。最小修复：首次读取后统一使用 `s.Entry.TaskID` 进入整个投递与对账流程。

4. **PM-004 — medium [mechanical]，Observed，高置信度：恢复失败注释与实现不符。**  
   `internal/launch/notify_resume.go:29–31` 声称失败时按 revision 回滚；新增的 delivery-unknown 与 durable 校验失败路径直接返回，刻意保留正文、WINDOW 和任务文件（同文件 `108–110,119–121`）。注释会误导调用方理解失败后的资源状态。最小修复：说明发送前失败的回滚条件，以及持久发送尝试后的保留行为。

三语新增 16 个消息键齐全，无重复键或占位符差异；AGENTS.md 两项新增索引均保留。`git diff --check` 通过。测试、race、build/vet 未在本次只读审查重跑；原生 Windows、真实终端验证保持 Unverifiable，不单独构成 finding。

只读沙箱禁止删除，任务文件保留。

NON-BLOCKING: none

```kander-findings
{
  "FINDINGS": [
    {
      "id": "PM-001",
      "tier": "medium",
      "text": "Inferred，高置信度：持久 resume 在 foreground/console 执行者持续存活但没有接受回执时，存活校验到期返回 nil，resumeDispatch 随后输出 delivery-unknown 并返回成功。console 可直接退出 0；foreground 后续退出 0 也使命令成功。违反接受期限耗尽且无回执必须返回非零 pending 的契约。最小修复：最终重新读取回执后，未接受且期限耗尽返回 pending 错误，继续保留未知执行者及持久意图。",
      "evidence": "契约：rules/KANDER-KANBAN-RULES.md:109。路径：internal/launch/agent.go:268–277 在 foreground/console 到期时返回 nil；internal/launch/dispatch.go:198–209 未接受且启动返回 nil 时仍返回 PrintDispatchResult；internal/launch/dispatch.go:277–287 对 console 不等待，对退出 0 的 foreground 返回 nil。"
    },
    {
      "id": "PM-002",
      "tier": "medium",
      "text": "Inferred，高置信度：新增 dispatch 预解析不识别其他选项的参数边界，将 --message 的正文当成 dispatch 选项。例如 notify <task-id> --message '--kind=sync' 原本是合法普通消息，现在正文被删除并报缺少消息；resume 同样受影响。违反未绑定普通消息兼容要求。最小修复：使用统一参数解析，或在预解析中正确跳过已有选项的参数，保证消息正文不被解释为控制选项。",
      "evidence": "契约：durable-dispatch-protocol-task 的普通消息兼容验收；rules/KANDER-KANBAN-RULES.md:114。根因：internal/launch/dispatch.go:213–239 无条件检查每个 token。消费者：internal/notify/cmd.go 的 RunNotify、internal/launch/cmd.go 的 RunResume 均先调用 ParseDispatchOptions，再解析 --message；internal/notify/cmd.go:65–70 展示原有消息参数消费方式。"
    },
    {
      "id": "PM-003",
      "tier": "medium",
      "text": "Inferred，高置信度：notify/resume 的持久模式混用规范化任务 ID 与原始任务参数。使用已有入口支持的 <task-id>.md 拼写时，意图按规范 ID 成功落盘，随后 ReadDispatch 按原始参数建立锁作用域并失败，造成已绑定但未发送的 prepared 意图，重试相同拼写仍失败。组 review 自动切入持久模式后出现该回归。最小修复：首次 ReadSnapshot 后使用 s.Entry.TaskID 贯穿发送、恢复及同 ID 对账。",
      "evidence": "既有参数契约：internal/board/scan.go:251–262 的 NormalizeTaskID/Locate 接受 .md 拼写；internal/board/transaction.go:330–337 的 ReadSnapshot 同样规范化。创建使用规范 ID：internal/launch/dispatch.go:69–73。投递却使用原始 task：internal/notify/dispatch.go:49,74；internal/launch/dispatch.go:134。ReadDispatch 不规范化：internal/board/dispatch.go:174–175；internal/board/transaction_lock.go:67–73 拒绝带扩展名的锁任务 ID。"
    },
    {
      "id": "PM-004",
      "tier": "medium",
      "mechanical": "documentation",
      "text": "Observed，高置信度：[mechanical] NotifyViaResume 的注释仍承诺失败时按 revision 回滚，但新增持久投递路径在发送结果不确定或发送后的存活校验失败时直接返回，保留正文、WINDOW 和任务文件。注释误导调用方理解失败后的资源状态。最小修复：明确发送前失败的回滚条件，以及持久发送尝试后的保留行为。",
      "evidence": "internal/launch/notify_resume.go:29–31 描述失败回滚；同文件 108–110 的 DeliveryUnknown 分支及 119–121 的 durable 校验失败分支均直接返回，不执行下方回滚。该差异由本审核范围新增的持久模式引入。"
    }
  ],
  "NON_BLOCKING": []
}
```
Author records (verbatim JSON):
{
  "assignment": {
    "run_id": "g2-pm-r1",
    "batch_id": "g2-batch-one",
    "author": "coordinator",
    "basis": "按各卡修改范围归属：四项均落在 internal/launch/dispatch.go、internal/launch/agent.go、internal/launch/notify_resume.go、internal/notify、internal/board/dispatch* 等由 20260908-durable-dispatch-protocol-task 本轮引入的持久派回路径，未触及另两卡的 subscribe/board 扫描改动",
    "items": {
      "PM-001": [
        "20260908-durable-dispatch-protocol-task"
      ],
      "PM-002": [
        "20260908-durable-dispatch-protocol-task"
      ],
      "PM-003": [
        "20260908-durable-dispatch-protocol-task"
      ],
      "PM-004": [
        "20260908-durable-dispatch-protocol-task"
      ]
    },
    "owners": {
      "20260908-durable-dispatch-protocol-task": "codex"
    },
    "recorded_at": "2026-09-07T19:39:29.685980491Z"
  },
  "records": [
    {
      "submitted_revision": 20,
      "record_id": "durable-r1-pm001",
      "run_id": "g2-pm-r1",
      "finding_id": "PM-001",
      "batch_id": "g2-batch-one",
      "task_id": "20260908-durable-dispatch-protocol-task",
      "author": "codex",
      "recorded_at": "2026-09-07T19:49:46.128838809Z",
      "report_hash": "0ba3565e4c10bd1cde53ab9fb098127df894872e54d58d565ef66fd12cfab2b8",
      "original": "Inferred，高置信度：持久 resume 在 foreground/console 执行者持续存活但没有接受回执时，存活校验到期返回 nil，resumeDispatch 随后输出 delivery-unknown 并返回成功。console 可直接退出 0；foreground 后续退出 0 也使命令成功。违反接受期限耗尽且无回执必须返回非零 pending 的契约。最小修复：最终重新读取回执后，未接受且期限耗尽返回 pending 错误，继续保留未知执行者及持久意图。",
      "status": "fixed",
      "basis": "confirmed → fixed。核实目标 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 的 internal/launch/agent.go:268-277 与 dispatch.go:198-209：进程存活到期限可返回 nil，且没有最终 pending 判定。TestDurableProcessLivenessRequiresReceiptAtDeadline 在修复前 foreground/console 两子例均失败：live executor without receipt reported success at deadline。现 validateResumedDispatch 在成功存活校验后重新读取同授权回执；预算耗尽仍未接受则 pending，resumeDispatch 同时检查父/持久期限。最终 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 的 dispatch.go:199-213、257-293；未知意图及执行资源保留。",
      "fix_commit": "58023dff85b89e0f5f37f185f78f0a33db11ddc3",
      "verification": "TestDurableProcessLivenessRequiresReceiptAtDeadline；TestDurableRecoveryAcceptsQuickCompletionBeforeLiveness；TestDurableLaunchErrorRetainsUnknownExecutorAndPayload。 最终提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：go test -json -count=1 ./... 退出 0，19 包、922 个测试/子测试通过、1 跳过、0 失败；go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window 退出 0，4 包、400 个测试/子测试通过、1 跳过、0 失败；go build ./...、go vet ./... 退出 0。"
    },
    {
      "submitted_revision": 21,
      "record_id": "durable-r1-pm002",
      "run_id": "g2-pm-r1",
      "finding_id": "PM-002",
      "batch_id": "g2-batch-one",
      "task_id": "20260908-durable-dispatch-protocol-task",
      "author": "codex",
      "recorded_at": "2026-09-07T19:49:47.018132496Z",
      "report_hash": "0ba3565e4c10bd1cde53ab9fb098127df894872e54d58d565ef66fd12cfab2b8",
      "original": "Inferred，高置信度：新增 dispatch 预解析不识别其他选项的参数边界，将 --message 的正文当成 dispatch 选项。例如 notify \u003ctask-id\u003e --message '--kind=sync' 原本是合法普通消息，现在正文被删除并报缺少消息；resume 同样受影响。违反未绑定普通消息兼容要求。最小修复：使用统一参数解析，或在预解析中正确跳过已有选项的参数，保证消息正文不被解释为控制选项。",
      "status": "fixed",
      "basis": "confirmed → fixed。核实目标 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 的 ParseDispatchOptions 无条件扫描 token，RunNotify/RunResume 先调用该入口。修复前 TestDispatchOptionsPreserveOtherOptionValues 失败，--message 的 --kind=sync 被提取为控制参数，--dispatch-id 被误吞后续 token。现预解析保留既有选项及其下一个值，仍由原解析器验证值；测试覆盖六个既有带值选项及三种类似派回选项的字面值。最终 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：internal/launch/dispatch.go:217-254。",
      "fix_commit": "e40a98570b49b55395c2d5d6b2d9e516f10ce664",
      "verification": "TestDispatchOptionsPreserveOtherOptionValues；TestDispatchPromptTranslationsAndFlagParsing。 最终提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：go test -json -count=1 ./... 退出 0，19 包、922 个测试/子测试通过、1 跳过、0 失败；go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window 退出 0，4 包、400 个测试/子测试通过、1 跳过、0 失败；go build ./...、go vet ./... 退出 0。"
    },
    {
      "submitted_revision": 22,
      "record_id": "durable-r1-pm003",
      "run_id": "g2-pm-r1",
      "finding_id": "PM-003",
      "batch_id": "g2-batch-one",
      "task_id": "20260908-durable-dispatch-protocol-task",
      "author": "codex",
      "recorded_at": "2026-09-07T19:49:48.177058879Z",
      "report_hash": "0ba3565e4c10bd1cde53ab9fb098127df894872e54d58d565ef66fd12cfab2b8",
      "original": "Inferred，高置信度：notify/resume 的持久模式混用规范化任务 ID 与原始任务参数。使用已有入口支持的 \u003ctask-id\u003e.md 拼写时，意图按规范 ID 成功落盘，随后 ReadDispatch 按原始参数建立锁作用域并失败，造成已绑定但未发送的 prepared 意图，重试相同拼写仍失败。组 review 自动切入持久模式后出现该回归。最小修复：首次 ReadSnapshot 后使用 s.Entry.TaskID 贯穿发送、恢复及同 ID 对账。",
      "status": "fixed",
      "basis": "confirmed → fixed。核实目标 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 创建使用快照规范 ID，而投递/恢复继续传原始参数。修复前已有行为用例改用 .md 别名后，notify 与 resume 均失败，输出“事务输入无效：20260908-durable-task.md”及“事务输入无效：20260908-dispatch-resume-task.md”。现两入口第一次 ReadSnapshot 后将 task 固定为 s.Entry.TaskID，贯穿准备、投递、恢复及重试。最终 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：internal/launch/dispatch.go:102-106、internal/notify/dispatch.go:22-26。",
      "fix_commit": "e40a98570b49b55395c2d5d6b2d9e516f10ce664",
      "verification": "TestDurableResumeExplicitTakeoverFencesOldEpoch；TestDispatchReceiptBeforeNotifyReturnAndRetry（首次发送和同 ID 对账均使用 .md 别名）。 最终提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：go test -json -count=1 ./... 退出 0，19 包、922 个测试/子测试通过、1 跳过、0 失败；go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window 退出 0，4 包、400 个测试/子测试通过、1 跳过、0 失败；go build ./...、go vet ./... 退出 0。"
    },
    {
      "submitted_revision": 23,
      "record_id": "durable-r1-pm004",
      "run_id": "g2-pm-r1",
      "finding_id": "PM-004",
      "batch_id": "g2-batch-one",
      "task_id": "20260908-durable-dispatch-protocol-task",
      "author": "codex",
      "recorded_at": "2026-09-07T19:49:49.047385849Z",
      "report_hash": "0ba3565e4c10bd1cde53ab9fb098127df894872e54d58d565ef66fd12cfab2b8",
      "original": "Observed，高置信度：[mechanical] NotifyViaResume 的注释仍承诺失败时按 revision 回滚，但新增持久投递路径在发送结果不确定或发送后的存活校验失败时直接返回，保留正文、WINDOW 和任务文件。注释误导调用方理解失败后的资源状态。最小修复：明确发送前失败的回滚条件，以及持久发送尝试后的保留行为。",
      "status": "fixed",
      "basis": "confirmed → fixed。合并根因：g2-pm-r1/PM-004 与 g2-qa-r1/QA-002 是同一条 NotifyViaResume 陈旧注释，计一个根因、同一修复；为保留两份来源身份分别提交绑定记录。逐段核实目标 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 的 internal/launch/notify_resume.go:29-31 与 108-121：注释无条件承诺 revision 回滚，持久不确定投递/验证失败分支实际保留资源。最终 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 的同文件:29-34 已说明发送前回滚受 revision/authorization 限制，持久发送不确定或启动后校验失败保留执行者、WINDOW、正文和任务文件；运行逻辑未因注释处置改动。",
      "fix_commit": "ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3",
      "mechanical": "documentation",
      "verification": "人工逐段核对 NotifyViaResume 注释与实际错误分支；既有 TestDurableLaunchErrorRetainsUnknownExecutorAndPayload 和 TestDispatchFailedSendKeepsConsumedNewBody 通过，未为注释新增重复测试。 最终提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：go test -json -count=1 ./... 退出 0，19 包、922 个测试/子测试通过、1 跳过、0 失败；go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window 退出 0，4 包、400 个测试/子测试通过、1 跳过、0 失败；go build ./...、go vet ./... 退出 0。"
    }
  ]
}

Current batch disposition (tool generated):
{
  "schema": 1,
  "batch": {
    "plan_id": "g2-subscription-dispatch",
    "task_context_hash": "8e8cba8aac0d11fbe3a74cabf21761124bb3744d874e6d79bd4a0ea1109d5041",
    "schema": 1,
    "batch_id": "g2-batch-one",
    "task_ids": [
      "20260908-durable-dispatch-protocol-task",
      "20260908-subscription-bounded-runtime-task",
      "20260908-subscription-facts-task"
    ],
    "base": "021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5",
    "target_commit": "ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3",
    "report_language": "zh-CN",
    "requirements": {
      "CSA": "N/A: 本仓库 AGENTS.md 第 7 条，第二阶段安全角色一律 N/A",
      "Hacker": "N/A: 本仓库 AGENTS.md 第 7 条，第二阶段安全角色一律 N/A",
      "PM": "required",
      "QA": "required"
    },
    "advances": [
      {
        "previous_target": "ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe",
        "target": "ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3",
        "reason": "g2-batch-one 第 1 轮 6 个来源 finding（5 个根因）的作者修复交付已 ff 接收到组分支，推进批次目标以便 PM/QA 增量复审",
        "deliveries": {
          "58023dff85b89e0f5f37f185f78f0a33db11ddc3": "20260908-durable-dispatch-protocol-task",
          "cba5231c4a4a1c78c916c1d3d9f62de5b97c7111": "20260908-durable-dispatch-protocol-task",
          "e40a98570b49b55395c2d5d6b2d9e516f10ce664": "20260908-durable-dispatch-protocol-task",
          "ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3": "20260908-durable-dispatch-protocol-task"
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
        "task_group": "20260908-subscription-dispatch-group",
        "run_id": "g2-pm-r1",
        "batch_id": "g2-batch-one",
        "task_ids": [
          "20260908-durable-dispatch-protocol-task",
          "20260908-subscription-bounded-runtime-task",
          "20260908-subscription-facts-task"
        ],
        "role": "PM",
        "reviewer": "codex",
        "model": "gpt-6-astra",
        "effort": "medium",
        "cwd": "/home/dualf/works/kander/worktrees/20260908-subscription-dispatch-group",
        "base": "021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5",
        "commit": "ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "082cfbec2c652269ada32a55bee38361de4f456707954cbfca7a0365d88dd109",
          "task-context.md": "8e8cba8aac0d11fbe3a74cabf21761124bb3744d874e6d79bd4a0ea1109d5041"
        },
        "kander_version": "20260907T173303Z-7e6fe2073ff0",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-07T19:33:02.593852982Z",
        "finished_at": "2026-09-07T19:38:28.837500067Z",
        "duration_ms": 326243,
        "hashes": {
          "error.log": "1c117bc357ca9c6d60e464c33a432f1cc85123181b1792831376604b01959049",
          "evidence.txt": "af6ed108d7388a8c28aa0d8f1755084f4f2da92a37c2b6b532937d36eac2d99e",
          "output.raw": "0ba3565e4c10bd1cde53ab9fb098127df894872e54d58d565ef66fd12cfab2b8",
          "prompt.txt": "5256b1609d74407c4ba0383dad3ff0943b8052b217a74ed0f72fb6ec1053df46",
          "report.md": "0ba3565e4c10bd1cde53ab9fb098127df894872e54d58d565ef66fd12cfab2b8",
          "review-context.md": "082cfbec2c652269ada32a55bee38361de4f456707954cbfca7a0365d88dd109",
          "stdout.log": "bf7f06a821708db8b9385bc03bb785ba74394f8b032192123f4fd4d7ec58c8cc",
          "task-context.md": "8e8cba8aac0d11fbe3a74cabf21761124bb3744d874e6d79bd4a0ea1109d5041"
        },
        "published": {
          "20260908-durable-dispatch-protocol-task": true,
          "20260908-subscription-bounded-runtime-task": true,
          "20260908-subscription-facts-task": true
        }
      },
      "findings": {
        "FINDINGS": [
          {
            "id": "PM-001",
            "tier": "medium",
            "text": "Inferred，高置信度：持久 resume 在 foreground/console 执行者持续存活但没有接受回执时，存活校验到期返回 nil，resumeDispatch 随后输出 delivery-unknown 并返回成功。console 可直接退出 0；foreground 后续退出 0 也使命令成功。违反接受期限耗尽且无回执必须返回非零 pending 的契约。最小修复：最终重新读取回执后，未接受且期限耗尽返回 pending 错误，继续保留未知执行者及持久意图。",
            "evidence": "契约：rules/KANDER-KANBAN-RULES.md:109。路径：internal/launch/agent.go:268–277 在 foreground/console 到期时返回 nil；internal/launch/dispatch.go:198–209 未接受且启动返回 nil 时仍返回 PrintDispatchResult；internal/launch/dispatch.go:277–287 对 console 不等待，对退出 0 的 foreground 返回 nil。"
          },
          {
            "id": "PM-002",
            "tier": "medium",
            "text": "Inferred，高置信度：新增 dispatch 预解析不识别其他选项的参数边界，将 --message 的正文当成 dispatch 选项。例如 notify \u003ctask-id\u003e --message '--kind=sync' 原本是合法普通消息，现在正文被删除并报缺少消息；resume 同样受影响。违反未绑定普通消息兼容要求。最小修复：使用统一参数解析，或在预解析中正确跳过已有选项的参数，保证消息正文不被解释为控制选项。",
            "evidence": "契约：durable-dispatch-protocol-task 的普通消息兼容验收；rules/KANDER-KANBAN-RULES.md:114。根因：internal/launch/dispatch.go:213–239 无条件检查每个 token。消费者：internal/notify/cmd.go 的 RunNotify、internal/launch/cmd.go 的 RunResume 均先调用 ParseDispatchOptions，再解析 --message；internal/notify/cmd.go:65–70 展示原有消息参数消费方式。"
          },
          {
            "id": "PM-003",
            "tier": "medium",
            "text": "Inferred，高置信度：notify/resume 的持久模式混用规范化任务 ID 与原始任务参数。使用已有入口支持的 \u003ctask-id\u003e.md 拼写时，意图按规范 ID 成功落盘，随后 ReadDispatch 按原始参数建立锁作用域并失败，造成已绑定但未发送的 prepared 意图，重试相同拼写仍失败。组 review 自动切入持久模式后出现该回归。最小修复：首次 ReadSnapshot 后使用 s.Entry.TaskID 贯穿发送、恢复及同 ID 对账。",
            "evidence": "既有参数契约：internal/board/scan.go:251–262 的 NormalizeTaskID/Locate 接受 .md 拼写；internal/board/transaction.go:330–337 的 ReadSnapshot 同样规范化。创建使用规范 ID：internal/launch/dispatch.go:69–73。投递却使用原始 task：internal/notify/dispatch.go:49,74；internal/launch/dispatch.go:134。ReadDispatch 不规范化：internal/board/dispatch.go:174–175；internal/board/transaction_lock.go:67–73 拒绝带扩展名的锁任务 ID。"
          },
          {
            "id": "PM-004",
            "tier": "medium",
            "text": "Observed，高置信度：[mechanical] NotifyViaResume 的注释仍承诺失败时按 revision 回滚，但新增持久投递路径在发送结果不确定或发送后的存活校验失败时直接返回，保留正文、WINDOW 和任务文件。注释误导调用方理解失败后的资源状态。最小修复：明确发送前失败的回滚条件，以及持久发送尝试后的保留行为。",
            "evidence": "internal/launch/notify_resume.go:29–31 描述失败回滚；同文件 108–110 的 DeliveryUnknown 分支及 119–121 的 durable 校验失败分支均直接返回，不执行下方回滚。该差异由本审核范围新增的持久模式引入。",
            "mechanical": "documentation"
          }
        ],
        "NON_BLOCKING": []
      },
      "assignment": {
        "run_id": "g2-pm-r1",
        "batch_id": "g2-batch-one",
        "author": "coordinator",
        "basis": "按各卡修改范围归属：四项均落在 internal/launch/dispatch.go、internal/launch/agent.go、internal/launch/notify_resume.go、internal/notify、internal/board/dispatch* 等由 20260908-durable-dispatch-protocol-task 本轮引入的持久派回路径，未触及另两卡的 subscribe/board 扫描改动",
        "items": {
          "PM-001": [
            "20260908-durable-dispatch-protocol-task"
          ],
          "PM-002": [
            "20260908-durable-dispatch-protocol-task"
          ],
          "PM-003": [
            "20260908-durable-dispatch-protocol-task"
          ],
          "PM-004": [
            "20260908-durable-dispatch-protocol-task"
          ]
        },
        "owners": {
          "20260908-durable-dispatch-protocol-task": "codex"
        },
        "recorded_at": "2026-09-07T19:39:29.685980491Z"
      },
      "records": [
        {
          "submitted_revision": 20,
          "record_id": "durable-r1-pm001",
          "run_id": "g2-pm-r1",
          "finding_id": "PM-001",
          "batch_id": "g2-batch-one",
          "task_id": "20260908-durable-dispatch-protocol-task",
          "author": "codex",
          "recorded_at": "2026-09-07T19:49:46.128838809Z",
          "report_hash": "0ba3565e4c10bd1cde53ab9fb098127df894872e54d58d565ef66fd12cfab2b8",
          "original": "Inferred，高置信度：持久 resume 在 foreground/console 执行者持续存活但没有接受回执时，存活校验到期返回 nil，resumeDispatch 随后输出 delivery-unknown 并返回成功。console 可直接退出 0；foreground 后续退出 0 也使命令成功。违反接受期限耗尽且无回执必须返回非零 pending 的契约。最小修复：最终重新读取回执后，未接受且期限耗尽返回 pending 错误，继续保留未知执行者及持久意图。",
          "status": "fixed",
          "basis": "confirmed → fixed。核实目标 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 的 internal/launch/agent.go:268-277 与 dispatch.go:198-209：进程存活到期限可返回 nil，且没有最终 pending 判定。TestDurableProcessLivenessRequiresReceiptAtDeadline 在修复前 foreground/console 两子例均失败：live executor without receipt reported success at deadline。现 validateResumedDispatch 在成功存活校验后重新读取同授权回执；预算耗尽仍未接受则 pending，resumeDispatch 同时检查父/持久期限。最终 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 的 dispatch.go:199-213、257-293；未知意图及执行资源保留。",
          "fix_commit": "58023dff85b89e0f5f37f185f78f0a33db11ddc3",
          "verification": "TestDurableProcessLivenessRequiresReceiptAtDeadline；TestDurableRecoveryAcceptsQuickCompletionBeforeLiveness；TestDurableLaunchErrorRetainsUnknownExecutorAndPayload。 最终提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：go test -json -count=1 ./... 退出 0，19 包、922 个测试/子测试通过、1 跳过、0 失败；go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window 退出 0，4 包、400 个测试/子测试通过、1 跳过、0 失败；go build ./...、go vet ./... 退出 0。"
        },
        {
          "submitted_revision": 21,
          "record_id": "durable-r1-pm002",
          "run_id": "g2-pm-r1",
          "finding_id": "PM-002",
          "batch_id": "g2-batch-one",
          "task_id": "20260908-durable-dispatch-protocol-task",
          "author": "codex",
          "recorded_at": "2026-09-07T19:49:47.018132496Z",
          "report_hash": "0ba3565e4c10bd1cde53ab9fb098127df894872e54d58d565ef66fd12cfab2b8",
          "original": "Inferred，高置信度：新增 dispatch 预解析不识别其他选项的参数边界，将 --message 的正文当成 dispatch 选项。例如 notify \u003ctask-id\u003e --message '--kind=sync' 原本是合法普通消息，现在正文被删除并报缺少消息；resume 同样受影响。违反未绑定普通消息兼容要求。最小修复：使用统一参数解析，或在预解析中正确跳过已有选项的参数，保证消息正文不被解释为控制选项。",
          "status": "fixed",
          "basis": "confirmed → fixed。核实目标 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 的 ParseDispatchOptions 无条件扫描 token，RunNotify/RunResume 先调用该入口。修复前 TestDispatchOptionsPreserveOtherOptionValues 失败，--message 的 --kind=sync 被提取为控制参数，--dispatch-id 被误吞后续 token。现预解析保留既有选项及其下一个值，仍由原解析器验证值；测试覆盖六个既有带值选项及三种类似派回选项的字面值。最终 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：internal/launch/dispatch.go:217-254。",
          "fix_commit": "e40a98570b49b55395c2d5d6b2d9e516f10ce664",
          "verification": "TestDispatchOptionsPreserveOtherOptionValues；TestDispatchPromptTranslationsAndFlagParsing。 最终提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：go test -json -count=1 ./... 退出 0，19 包、922 个测试/子测试通过、1 跳过、0 失败；go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window 退出 0，4 包、400 个测试/子测试通过、1 跳过、0 失败；go build ./...、go vet ./... 退出 0。"
        },
        {
          "submitted_revision": 22,
          "record_id": "durable-r1-pm003",
          "run_id": "g2-pm-r1",
          "finding_id": "PM-003",
          "batch_id": "g2-batch-one",
          "task_id": "20260908-durable-dispatch-protocol-task",
          "author": "codex",
          "recorded_at": "2026-09-07T19:49:48.177058879Z",
          "report_hash": "0ba3565e4c10bd1cde53ab9fb098127df894872e54d58d565ef66fd12cfab2b8",
          "original": "Inferred，高置信度：notify/resume 的持久模式混用规范化任务 ID 与原始任务参数。使用已有入口支持的 \u003ctask-id\u003e.md 拼写时，意图按规范 ID 成功落盘，随后 ReadDispatch 按原始参数建立锁作用域并失败，造成已绑定但未发送的 prepared 意图，重试相同拼写仍失败。组 review 自动切入持久模式后出现该回归。最小修复：首次 ReadSnapshot 后使用 s.Entry.TaskID 贯穿发送、恢复及同 ID 对账。",
          "status": "fixed",
          "basis": "confirmed → fixed。核实目标 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 创建使用快照规范 ID，而投递/恢复继续传原始参数。修复前已有行为用例改用 .md 别名后，notify 与 resume 均失败，输出“事务输入无效：20260908-durable-task.md”及“事务输入无效：20260908-dispatch-resume-task.md”。现两入口第一次 ReadSnapshot 后将 task 固定为 s.Entry.TaskID，贯穿准备、投递、恢复及重试。最终 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：internal/launch/dispatch.go:102-106、internal/notify/dispatch.go:22-26。",
          "fix_commit": "e40a98570b49b55395c2d5d6b2d9e516f10ce664",
          "verification": "TestDurableResumeExplicitTakeoverFencesOldEpoch；TestDispatchReceiptBeforeNotifyReturnAndRetry（首次发送和同 ID 对账均使用 .md 别名）。 最终提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：go test -json -count=1 ./... 退出 0，19 包、922 个测试/子测试通过、1 跳过、0 失败；go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window 退出 0，4 包、400 个测试/子测试通过、1 跳过、0 失败；go build ./...、go vet ./... 退出 0。"
        },
        {
          "submitted_revision": 23,
          "record_id": "durable-r1-pm004",
          "run_id": "g2-pm-r1",
          "finding_id": "PM-004",
          "batch_id": "g2-batch-one",
          "task_id": "20260908-durable-dispatch-protocol-task",
          "author": "codex",
          "recorded_at": "2026-09-07T19:49:49.047385849Z",
          "report_hash": "0ba3565e4c10bd1cde53ab9fb098127df894872e54d58d565ef66fd12cfab2b8",
          "original": "Observed，高置信度：[mechanical] NotifyViaResume 的注释仍承诺失败时按 revision 回滚，但新增持久投递路径在发送结果不确定或发送后的存活校验失败时直接返回，保留正文、WINDOW 和任务文件。注释误导调用方理解失败后的资源状态。最小修复：明确发送前失败的回滚条件，以及持久发送尝试后的保留行为。",
          "status": "fixed",
          "basis": "confirmed → fixed。合并根因：g2-pm-r1/PM-004 与 g2-qa-r1/QA-002 是同一条 NotifyViaResume 陈旧注释，计一个根因、同一修复；为保留两份来源身份分别提交绑定记录。逐段核实目标 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 的 internal/launch/notify_resume.go:29-31 与 108-121：注释无条件承诺 revision 回滚，持久不确定投递/验证失败分支实际保留资源。最终 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 的同文件:29-34 已说明发送前回滚受 revision/authorization 限制，持久发送不确定或启动后校验失败保留执行者、WINDOW、正文和任务文件；运行逻辑未因注释处置改动。",
          "fix_commit": "ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3",
          "mechanical": "documentation",
          "verification": "人工逐段核对 NotifyViaResume 注释与实际错误分支；既有 TestDurableLaunchErrorRetainsUnknownExecutorAndPayload 和 TestDispatchFailedSendKeepsConsumedNewBody 通过，未为注释新增重复测试。 最终提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：go test -json -count=1 ./... 退出 0，19 包、922 个测试/子测试通过、1 跳过、0 失败；go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window 退出 0，4 包、400 个测试/子测试通过、1 跳过、0 失败；go build ./...、go vet ./... 退出 0。"
        }
      ]
    },
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "20260908-subscription-dispatch-group",
        "run_id": "g2-qa-r1",
        "batch_id": "g2-batch-one",
        "task_ids": [
          "20260908-durable-dispatch-protocol-task",
          "20260908-subscription-bounded-runtime-task",
          "20260908-subscription-facts-task"
        ],
        "role": "QA",
        "reviewer": "codex",
        "model": "gpt-6-astra",
        "effort": "medium",
        "cwd": "/home/dualf/works/kander/worktrees/20260908-subscription-dispatch-group",
        "base": "021ae63f8b8a9cdb4e1914a695bd4bf9d0d28ff5",
        "commit": "ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "082cfbec2c652269ada32a55bee38361de4f456707954cbfca7a0365d88dd109",
          "task-context.md": "8e8cba8aac0d11fbe3a74cabf21761124bb3744d874e6d79bd4a0ea1109d5041"
        },
        "kander_version": "20260907T173303Z-7e6fe2073ff0",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-07T19:33:07.347371598Z",
        "finished_at": "2026-09-07T19:37:36.049799971Z",
        "duration_ms": 268702,
        "hashes": {
          "error.log": "270d80f9330984c81eb9e878be3ceeaf486d600b8365e35fdeadf1086ac4c071",
          "evidence.txt": "af6ed108d7388a8c28aa0d8f1755084f4f2da92a37c2b6b532937d36eac2d99e",
          "output.raw": "ff009fcfcf3f24e16298748e769b36e91f3e3a0774e60041d456da2f04497e6c",
          "prompt.txt": "4ee5dc62fd5d69690d7c718b13b914e8467cad3e24829956383789d3713fa9a2",
          "report.md": "ff009fcfcf3f24e16298748e769b36e91f3e3a0774e60041d456da2f04497e6c",
          "review-context.md": "082cfbec2c652269ada32a55bee38361de4f456707954cbfca7a0365d88dd109",
          "stdout.log": "4c02e52c91e5c7d2de1adbf9000822791fe6ba27cdc3139b93c61ded4723b817",
          "task-context.md": "8e8cba8aac0d11fbe3a74cabf21761124bb3744d874e6d79bd4a0ea1109d5041"
        },
        "published": {
          "20260908-durable-dispatch-protocol-task": true,
          "20260908-subscription-bounded-runtime-task": true,
          "20260908-subscription-facts-task": true
        }
      },
      "findings": {
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
            "text": "Observed，置信度高：NotifyViaResume 注释声称失败时只要仍持有当前 revision 就恢复原正文，但本范围新增的持久模式不确定投递及校验失败分支直接返回并保留资源，即使没有并发更新。注释误导调用者判断失败后的 WINDOW 和任务文件状态。最小修复：更新注释，明确普通回滚条件与持久模式发送后保留资源的例外；无需改运行逻辑。",
            "evidence": "internal/launch/notify_resume.go:29-31 描述失败恢复原正文；104-110 的 durable/DeliveryUnknown 分支保留任务文件并直接返回；118-121 的持久模式校验失败分支同样不回滚。该偏差由本范围新增分支造成。",
            "mechanical": "documentation"
          }
        ],
        "NON_BLOCKING": []
      },
      "assignment": {
        "run_id": "g2-qa-r1",
        "batch_id": "g2-batch-one",
        "author": "coordinator",
        "basis": "按各卡修改范围归属：两项均落在 internal/notify/dispatch.go 与 internal/launch/notify_resume.go 的持久派回路径，由 20260908-durable-dispatch-protocol-task 本轮引入；QA-002 与 PM-004 为同一根因（NotifyViaResume 注释与新增持久分支不符），处置时合并并同时引用两个来源 ID",
        "items": {
          "QA-001": [
            "20260908-durable-dispatch-protocol-task"
          ],
          "QA-002": [
            "20260908-durable-dispatch-protocol-task"
          ]
        },
        "owners": {
          "20260908-durable-dispatch-protocol-task": "codex"
        },
        "recorded_at": "2026-09-07T19:39:30.709073197Z"
      },
      "records": [
        {
          "submitted_revision": 24,
          "record_id": "durable-r1-qa001",
          "run_id": "g2-qa-r1",
          "finding_id": "QA-001",
          "batch_id": "g2-batch-one",
          "task_id": "20260908-durable-dispatch-protocol-task",
          "author": "codex",
          "recorded_at": "2026-09-07T19:49:49.981223677Z",
          "report_hash": "ff009fcfcf3f24e16298748e769b36e91f3e3a0774e60041d456da2f04497e6c",
          "original": "Inferred，置信度高：旧 notify 可借用新 dispatch 的授权写入 WINDOW。A 等待 busy 时被取消并由 B 替换，A 下一轮通过普通 ReadSnapshot 获得 B 的授权游标；地址漂移或原 --pane 触发 WINDOW 写入，之后 BeginDispatchAttempt 才拒绝 A。新轮 WINDOW/revision 已被旧调用改变，违反旧授权不得写新轮的契约，并可能造成地址覆盖或 CAS 冲突。最小修复：固定投递开始时的授权，每轮使用 ReadExecutionSnapshot 获取快照；增加暂停 busy、取消 A、准备 B 后断言旧调用失败且 B 的 WINDOW/revision 不变的确定性测试。",
          "status": "fixed",
          "basis": "confirmed → fixed。核实目标 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 的 notify/dispatch.go 在 busy 重试使用普通快照，WINDOW 写入早于 BeginDispatchAttempt。确定性回归用假 CLI 屏障暂停 busy，取消 A 并创建 B，再释放 A；修复前失败：old dispatch changed replacement WINDOW/revision: 7 -\u003e 8, herdr:w1:t9:w1:p9 -\u003e herdr:w1:t10:w1:p9。现投递开始固定 authorization，初次和循环均 ReadExecutionSnapshot；同 ID 新 epoch 也拒绝，错误后的回执读取不借用新 epoch。最终 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：internal/notify/dispatch.go:65-118。",
          "fix_commit": "cba5231c4a4a1c78c916c1d3d9f62de5b97c7111",
          "verification": "TestBusyDispatchCannotBorrowReplacementAuthorization；TestDispatchUnknownProbeAndBusyDoNotRecover；TestDurableExplicitPaneOverridesUnknownRecordedWindow。 最终提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：go test -json -count=1 ./... 退出 0，19 包、922 个测试/子测试通过、1 跳过、0 失败；go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window 退出 0，4 包、400 个测试/子测试通过、1 跳过、0 失败；go build ./...、go vet ./... 退出 0。"
        },
        {
          "submitted_revision": 25,
          "record_id": "durable-r1-qa002",
          "run_id": "g2-qa-r1",
          "finding_id": "QA-002",
          "batch_id": "g2-batch-one",
          "task_id": "20260908-durable-dispatch-protocol-task",
          "author": "codex",
          "recorded_at": "2026-09-07T19:49:50.949756458Z",
          "report_hash": "ff009fcfcf3f24e16298748e769b36e91f3e3a0774e60041d456da2f04497e6c",
          "original": "Observed，置信度高：NotifyViaResume 注释声称失败时只要仍持有当前 revision 就恢复原正文，但本范围新增的持久模式不确定投递及校验失败分支直接返回并保留资源，即使没有并发更新。注释误导调用者判断失败后的 WINDOW 和任务文件状态。最小修复：更新注释，明确普通回滚条件与持久模式发送后保留资源的例外；无需改运行逻辑。",
          "status": "fixed",
          "basis": "confirmed → fixed。合并根因：g2-pm-r1/PM-004 与 g2-qa-r1/QA-002 是同一条 NotifyViaResume 陈旧注释，计一个根因、同一修复；为保留两份来源身份分别提交绑定记录。逐段核实目标 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 的 internal/launch/notify_resume.go:29-31 与 108-121：注释无条件承诺 revision 回滚，持久不确定投递/验证失败分支实际保留资源。最终 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 的同文件:29-34 已说明发送前回滚受 revision/authorization 限制，持久发送不确定或启动后校验失败保留执行者、WINDOW、正文和任务文件；运行逻辑未因注释处置改动。",
          "fix_commit": "ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3",
          "mechanical": "documentation",
          "verification": "人工逐段核对 NotifyViaResume 注释与实际错误分支；既有 TestDurableLaunchErrorRetainsUnknownExecutorAndPayload 和 TestDispatchFailedSendKeepsConsumedNewBody 通过，未为注释新增重复测试。 最终提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3：go test -json -count=1 ./... 退出 0，19 包、922 个测试/子测试通过、1 跳过、0 失败；go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window 退出 0，4 包、400 个测试/子测试通过、1 跳过、0 失败；go build ./...、go vet ./... 退出 0。"
        }
      ]
    }
  ]
}


Caller supplemental context (verbatim; does not replace originals):
Fixes included this round:
本轮只包含 20260908-durable-dispatch-protocol-task 对第 1 轮 6 个来源 finding（5 个根因）的修复，共 4 个提交：e40a98570b49b55395c2d5d6b2d9e516f10ce664（PM-002/PM-003：保留其他选项的参数值边界，并统一规范任务 ID）、58023dff85b89e0f5f37f185f78f0a33db11ddc3（PM-001：期限耗尽仍无回执返回 pending）、cba5231c4a4a1c78c916c1d3d9f62de5b97c7111（QA-001：固定原授权，busy 重试不借用新轮 WINDOW 游标）、ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3（PM-004/QA-002：修正 NotifyViaResume 注释）。另两张卡本轮没有任何新提交，其代码在 ff04a3b1b2320f4e7ad234fb0ebf6745f70bb9fe 之后未变动。

Review focus:
(1) 逐条确认第 1 轮 finding 是否真的关闭，以及修复本身是否引入、加重或掩盖新问题。
(2) 重点看 dispatch 授权围栏（cba5231）是否在 busy 重试、地址漂移与 --pane 覆盖三条路径上都改为使用开始时的授权，且不会把新一轮的 WINDOW/revision 改掉。
(3) 参数解析修复（e40a985）是否既恢复了 --message 等既有选项的值边界，又没有破坏 dispatch 选项本身的解析与重复检测；任务 ID 规范化是否在创建、发送、恢复与对账四处一致。
(4) pending 返回（58023df）是否只在"期限耗尽且重读回执后仍未接受"时触发，不会把已接受或已完成误报为 pending。

Verification records:
作者在最终修复提交 ec6fdb4cebae1f33ba6241f11bd3b13f80535fe3 实跑：go test -json -count=1 ./... exit=0（19 包、922 项通过、1 跳过、0 失败）；go test -race -json -count=1 ./internal/launch ./internal/notify ./internal/board ./internal/window exit=0（4 包、400 项通过、1 跳过）；go build、go vet、GOOS=windows 交叉 build、gofmt 均 exit=0。作者记录修复前 4 组回归实际失败，原输出在 validation-r1.txt。

Environment gaps:
原生 Windows 与真实 tmux/herdr/Agent 未运行；唯一跳过的用例为原生 Windows 控制台用例。交叉编译与假 CLI 不代表实机通过，属已披露缺口，不得据此单独判定 blocking/high/medium。