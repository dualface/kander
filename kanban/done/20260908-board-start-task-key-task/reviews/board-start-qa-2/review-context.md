KANDER_AUTOMATIC_CONTEXT_BYTES: 24508
PREVIOUS_RUN_ID: board-start-qa-1
Prior report (verbatim):
Role: QA  
Commit: `c172eef3b1fe40331445bdea0b8b4025f61b9ce7`  
Task Context: 棋盘 `s` 确认启动 backlog/todo，保持 CLI 检查与回滚语义。  
Reviewed Scope: 审核范围 `781aa0d..c172eef`；核对提交树、完整任务规格、TUI 输入及后台生命周期、launch、board 事务、配置、三语文案与相关测试。只读环境未重跑测试；采信交付提交记录：全量 1167 项通过、1 项跳过，定向 205 项通过、1 项跳过。

| 行为／质量 | 结论与证据 |
|---|---|
| 模块边界、CLI 兼容 | Observed：`tui → launch → board`；核心流程迁入 `Start`，CLI 保留结果打印与退出处理。 |
| 状态、确认、取消、输入上下文 | Observed：入口及测试覆盖有效／无效状态、取消、详情、搜索、前台 launcher 拒绝。 |
| backlog 门禁、失败回滚 | Observed：复用 `MoveEntry`；既有启动失败、容器清理、版本冲突测试保留。 |
| 后台生命周期、成功提示 | Inferred：存在 QA-001、QA-002。现有异步测试未覆盖退出；提示测试只检查 `CopyNotice`。 |
| 文案、文档、文件规模 | Observed：新增三语键一致，共 12 个；帮助及文档同步；13 个变更 Go 文件均未超过 1000 行。 |

### Gate Findings

**QA-001 — medium — Inferred；置信度：高：正常退出会中断已认领的启动流程**

证据：`internal/tui/start.go:81–91` 将启动交给后台命令，没有记录未完成启动；`internal/tui/app.go:530–531` 接受 `q` 立即退出，`internal/tui/program.go:133–135` 返回 `tea.Quit`。Bubble Tea v1.3.10 的 `tea.go:349–365` 明确不等待命令 goroutine；随后 `cmd/kander/main.go:10` 执行 `os.Exit`。

真实路径：用户确认 herdr 启动，卡片已迁到 working（`internal/launch/start.go:117`），正在等待 pane 就绪（`internal/launch/agent.go:51–66`），此时按 `q`。进程退出，Agent 尚未启动，后续清理及回滚无法执行，留下 working 卡与空容器。

最小修复：TUI 跟踪未完成启动；收到退出请求后等待启动结果处理完毕再退出，期间保持事件循环运行。用阻塞桩验证 `q`／Ctrl+C 不会提前结束启动生命周期。

**QA-002 — medium — Inferred；置信度：高：常见窗口宽度下成功提示丢失容器地址**

证据：`internal/tui/start.go:102–106` 把完整任务 ID、Agent、launcher 放在地址前；`internal/tui/board_view.go:109–111` 将整条消息截成 `w-1` 列。`internal/tui/start_test.go:158–161` 仅断言 `CopyNotice`，未验证实际渲染。

真实路径：80 列窗口，任务 `20260908-board-start-task-key` 使用 `tmux-session`，地址 `kb-board-start-task-key-12345678:@9:%9`。英文成功消息长 102 列，实际页脚止于 `kb-board-sta...`，window/pane 完全不可见。违反成功提示包含实际容器地址的要求。

最小修复：按显示宽度组织成功提示，优先保留 Agent、launcher 与完整地址，压缩任务 ID；仍放不下时提供可查看完整结果的展示。增加 80 列真实长度数据的渲染断言。

NON-BLOCKING: none

任务文件删除已尝试；只读文件系统拒绝，文件遗留不影响审核。

```kander-findings
{
  "FINDINGS": [
    {
      "id": "QA-001",
      "tier": "medium",
      "text": "Inferred；置信度高。TUI 未管理后台启动的退出生命周期。用户确认 herdr 启动，卡片迁入 working 后，在等待 pane 就绪期间按 q 或 Ctrl+C，程序直接退出，后台启动被终止，Agent 尚未启动，容器清理和卡片回滚无法执行。最小修复：跟踪未完成启动，退出请求延迟至启动结果处理完毕，保持事件循环运行；用阻塞启动桩验证退出等待行为。",
      "evidence": "internal/tui/start.go:81–91 排队启动但不记录运行状态；internal/tui/app.go:94–96、530–531 直接退出；internal/tui/program.go:133–135 返回 tea.Quit；go.mod:7 使用 Bubble Tea v1.3.10，其 tea.go:349–365 不等待命令 goroutine；cmd/kander/main.go:10 调用 os.Exit。internal/launch/start.go:117 先迁入 working，internal/launch/agent.go:51–66 创建容器后等待就绪再启动 Agent；退出会截断该路径。"
    },
    {
      "id": "QA-002",
      "tier": "medium",
      "text": "Inferred；置信度高。成功提示把完整任务 ID、Agent、launcher 放在容器地址前，再由单行页脚截断。80 列窗口中，任务 20260908-board-start-task-key、Agent claude、launcher tmux-session、地址 kb-board-start-task-key-12345678:@9:%9 组成 102 列英文消息，实际显示止于 kb-board-sta...，window/pane 不可见，违反成功提示包含容器地址的要求。最小修复：按显示宽度保留 Agent、launcher 和完整地址，压缩任务 ID；空间仍不足时提供完整结果展示，并增加真实长度数据的渲染断言。",
      "evidence": "internal/tui/start.go:102–106 构造地址并放在消息末尾；internal/i18n/locales/en.json:9 定义成功消息顺序；internal/tui/board_view.go:109–111 将消息截成 w-1 列；internal/tui/text.go:49–74 使用省略号截断；internal/tui/start_test.go:158–161 仅检查 CopyNotice，未检查渲染后的地址可见性。"
    }
  ],
  "NON_BLOCKING": []
}
```
Author records (verbatim JSON):
{
  "assignment": {
    "run_id": "board-start-qa-1",
    "batch_id": "board-start-task-key-batch",
    "author": "codex",
    "basis": "已复核启动退出生命周期及页脚截断路径；QA-002 与 PM-001 同根因",
    "items": {
      "QA-001": [
        "20260908-board-start-task-key-task"
      ],
      "QA-002": [
        "20260908-board-start-task-key-task"
      ]
    },
    "owners": {
      "20260908-board-start-task-key-task": "codex"
    },
    "recorded_at": "2026-09-08T04:02:20.932966326Z"
  },
  "records": [
    {
      "submitted_revision": 14,
      "record_id": "fixed-qa-001",
      "run_id": "board-start-qa-1",
      "finding_id": "QA-001",
      "batch_id": "board-start-task-key-batch",
      "task_id": "20260908-board-start-task-key-task",
      "author": "codex",
      "recorded_at": "2026-09-08T04:05:58.41429839Z",
      "report_hash": "84db1e5f32fbe230be5b9d04c3a79aad9fc53130f631b21545871b1204200678",
      "original": "Inferred；置信度高。TUI 未管理后台启动的退出生命周期。用户确认 herdr 启动，卡片迁入 working 后，在等待 pane 就绪期间按 q 或 Ctrl+C，程序直接退出，后台启动被终止，Agent 尚未启动，容器清理和卡片回滚无法执行。最小修复：跟踪未完成启动，退出请求延迟至启动结果处理完毕，保持事件循环运行；用阻塞启动桩验证退出等待行为。",
      "status": "fixed",
      "basis": "已独立核对 Bubble Tea 不等待命令 goroutine、main 的 os.Exit 以及 launch 先认领再等待 pane 的路径。加入启动计数，q/Ctrl+C 延迟退出至全部结果回传，原有 launch 完成/回滚逻辑不变。",
      "fix_commit": "0eeb06bd84daf863d0817f5d1153f4b04b59f68a",
      "verification": "在 0eeb06bd84daf863d0817f5d1153f4b04b59f68a 执行 go test -count=1 -json ./internal/launch ./internal/tui ./internal/i18n，通过 208 项（含子测试）、跳过 1 项；go test -json ./... 通过 1170 项（含子测试）、跳过 1 项。新增阻塞启动桩验证 q/Ctrl+C、成功/失败、多启动等待及最终 tea.Quit。"
    },
    {
      "submitted_revision": 15,
      "record_id": "fixed-qa-002",
      "run_id": "board-start-qa-1",
      "finding_id": "QA-002",
      "batch_id": "board-start-task-key-batch",
      "task_id": "20260908-board-start-task-key-task",
      "author": "codex",
      "recorded_at": "2026-09-08T04:06:01.698105328Z",
      "report_hash": "84db1e5f32fbe230be5b9d04c3a79aad9fc53130f631b21545871b1204200678",
      "original": "Inferred；置信度高。成功提示把完整任务 ID、Agent、launcher 放在容器地址前，再由单行页脚截断。80 列窗口中，任务 20260908-board-start-task-key、Agent claude、launcher tmux-session、地址 kb-board-start-task-key-12345678:@9:%9 组成 102 列英文消息，实际显示止于 kb-board-sta...，window/pane 不可见，违反成功提示包含容器地址的要求。最小修复：按显示宽度保留 Agent、launcher 和完整地址，压缩任务 ID；空间仍不足时提供完整结果展示，并增加真实长度数据的渲染断言。",
      "status": "fixed",
      "basis": "已独立核对页脚 w-1 截断与末尾地址丢失。PM-001 与 QA-002 为同一根因；统一修复为窄屏精简任务 ID 信息、优先保留 Agent/launcher/完整地址，必要时以不接管输入的换行浮层展示完整结果。",
      "fix_commit": "0eeb06bd84daf863d0817f5d1153f4b04b59f68a",
      "verification": "在 0eeb06bd84daf863d0817f5d1153f4b04b59f68a 执行 go test -count=1 -json ./internal/launch ./internal/tui ./internal/i18n，通过 208 项（含子测试）、跳过 1 项；go test -json ./... 通过 1170 项（含子测试）、跳过 1 项。新增 80 列真实任务 ID/地址页脚断言，以及 40 列完整地址换行与输入不受阻断言。"
    }
  ]
}

Current batch disposition (tool generated):
{
  "schema": 1,
  "batch": {
    "plan_id": "board-start-task-key-plan",
    "task_context_hash": "1cac45ee4af1f9c1879a031667b11d7448794d3506a3ee54ebf3b9767bcb8d24",
    "schema": 1,
    "batch_id": "board-start-task-key-batch",
    "task_ids": [
      "20260908-board-start-task-key-task"
    ],
    "base": "781aa0dcde99eefe0d7ccea84f5a138f414e831c",
    "target_commit": "0eeb06bd84daf863d0817f5d1153f4b04b59f68a",
    "report_language": "zh-CN",
    "requirements": {
      "CSA": "N/A: 本仓库 AGENTS.md 安全角色特例",
      "Hacker": "N/A: 本仓库 AGENTS.md 安全角色特例",
      "PM": "required",
      "QA": "required"
    },
    "advances": [
      {
        "previous_target": "c172eef3b1fe40331445bdea0b8b4025f61b9ce7",
        "target": "0eeb06bd84daf863d0817f5d1153f4b04b59f68a",
        "reason": "修复已核实的启动退出生命周期和完整地址展示问题；同一基线增量复审",
        "deliveries": {
          "0eeb06bd84daf863d0817f5d1153f4b04b59f68a": "20260908-board-start-task-key-task"
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
        "task_group": "",
        "run_id": "board-start-pm-1",
        "batch_id": "board-start-task-key-batch",
        "task_ids": [
          "20260908-board-start-task-key-task"
        ],
        "role": "PM",
        "reviewer": "codex",
        "model": "gpt-6-astra",
        "effort": "medium",
        "cwd": "/home/dualf/works/kander/worktrees/board-start-task-key",
        "base": "781aa0dcde99eefe0d7ccea84f5a138f414e831c",
        "commit": "c172eef3b1fe40331445bdea0b8b4025f61b9ce7",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "task-context.md": "1cac45ee4af1f9c1879a031667b11d7448794d3506a3ee54ebf3b9767bcb8d24"
        },
        "kander_version": "20260908T033741Z-781aa0dcde99",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-08T03:57:16.159227983Z",
        "finished_at": "2026-09-08T03:59:57.207978904Z",
        "duration_ms": 161048,
        "hashes": {
          "error.log": "e102e0a68a20b14e8a2b34cd75e5efe9eb2bffadf89f4bf5607db2b9f26155fb",
          "evidence.txt": "dc833e58b95ef81b5f9b447eda74f8fb7352c566253d1e20b2fe9fffd9f51797",
          "output.raw": "8a07055fd42d9ae1f79d57470c65cd374e108d4e367ca7645e9b98447c282e79",
          "prompt.txt": "8ecda86504ae7e8bf672aff5c6e1af6a7866c4d93f0c6b9c0a493765dfef8905",
          "report.md": "8a07055fd42d9ae1f79d57470c65cd374e108d4e367ca7645e9b98447c282e79",
          "review-context.md": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "stdout.log": "aee6105769673c9475f9009b297100a20caf94eb2ba6e16493fa7750fcb132cc",
          "task-context.md": "1cac45ee4af1f9c1879a031667b11d7448794d3506a3ee54ebf3b9767bcb8d24"
        },
        "published": {
          "20260908-board-start-task-key-task": true
        }
      },
      "findings": {
        "FINDINGS": [
          {
            "id": "PM-001",
            "tier": "medium",
            "text": "Inferred，置信度高：启动成功提示将完整任务 ID 放在容器地址之前，再按页脚宽度截断，违反成功后展示实际 Agent、launcher 和容器地址的要求。80 列终端中，任务 20260908-options-workflow-flowchart-task、Agent claude、launcher tmux-session、地址 kb-kander-12345678:@9:%9 的提示末尾仅显示 | k...，用户无法取得完整容器地址。最小修复：按宽度缩短任务 ID，优先保留 Agent、launcher 与完整地址；超宽时提供完整结果展示，并补充实际渲染断言。",
            "evidence": "internal/tui/start.go:102-106 构造地址并将其置于完整任务 ID、Agent、launcher 之后；internal/tui/board_view.go:109-111 将提示裁到 w-1 列；internal/tui/text.go:49-74 丢弃超宽尾部并添加省略号。internal/i18n/locales/en.json:9 定义上述字段顺序。该示例消息为 99 列，80 列页脚仅保留地址首字符。internal/tui/start_test.go:158-162 只断言 CopyNotice 内容，未验证渲染后的地址可见性。契约依据：task-spec.md EXPECTED_OUTCOME 的启动成功提示要求。"
          }
        ],
        "NON_BLOCKING": []
      },
      "assignment": {
        "run_id": "board-start-pm-1",
        "batch_id": "board-start-task-key-batch",
        "author": "codex",
        "basis": "已复核实际页脚截断路径；与 QA-002 同根因",
        "items": {
          "PM-001": [
            "20260908-board-start-task-key-task"
          ]
        },
        "owners": {
          "20260908-board-start-task-key-task": "codex"
        },
        "recorded_at": "2026-09-08T04:02:17.65122801Z"
      },
      "records": [
        {
          "submitted_revision": 13,
          "record_id": "fixed-pm-001",
          "run_id": "board-start-pm-1",
          "finding_id": "PM-001",
          "batch_id": "board-start-task-key-batch",
          "task_id": "20260908-board-start-task-key-task",
          "author": "codex",
          "recorded_at": "2026-09-08T04:05:53.974931011Z",
          "report_hash": "8a07055fd42d9ae1f79d57470c65cd374e108d4e367ca7645e9b98447c282e79",
          "original": "Inferred，置信度高：启动成功提示将完整任务 ID 放在容器地址之前，再按页脚宽度截断，违反成功后展示实际 Agent、launcher 和容器地址的要求。80 列终端中，任务 20260908-options-workflow-flowchart-task、Agent claude、launcher tmux-session、地址 kb-kander-12345678:@9:%9 的提示末尾仅显示 | k...，用户无法取得完整容器地址。最小修复：按宽度缩短任务 ID，优先保留 Agent、launcher 与完整地址；超宽时提供完整结果展示，并补充实际渲染断言。",
          "status": "fixed",
          "basis": "已独立核对页脚 w-1 截断与末尾地址丢失。PM-001 与 QA-002 为同一根因；统一修复为窄屏精简任务 ID 信息、优先保留 Agent/launcher/完整地址，必要时以不接管输入的换行浮层展示完整结果。",
          "fix_commit": "0eeb06bd84daf863d0817f5d1153f4b04b59f68a",
          "verification": "在 0eeb06bd84daf863d0817f5d1153f4b04b59f68a 执行 go test -count=1 -json ./internal/launch ./internal/tui ./internal/i18n，通过 208 项（含子测试）、跳过 1 项；go test -json ./... 通过 1170 项（含子测试）、跳过 1 项。新增 80 列真实任务 ID/地址页脚断言，以及 40 列完整地址换行与输入不受阻断言。"
        }
      ]
    },
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "",
        "run_id": "board-start-qa-1",
        "batch_id": "board-start-task-key-batch",
        "task_ids": [
          "20260908-board-start-task-key-task"
        ],
        "role": "QA",
        "reviewer": "codex",
        "model": "gpt-6-astra",
        "effort": "medium",
        "cwd": "/home/dualf/works/kander/worktrees/board-start-task-key",
        "base": "781aa0dcde99eefe0d7ccea84f5a138f414e831c",
        "commit": "c172eef3b1fe40331445bdea0b8b4025f61b9ce7",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "task-context.md": "1cac45ee4af1f9c1879a031667b11d7448794d3506a3ee54ebf3b9767bcb8d24"
        },
        "kander_version": "20260908T033741Z-781aa0dcde99",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-08T03:57:17.272047902Z",
        "finished_at": "2026-09-08T04:00:24.169449261Z",
        "duration_ms": 186897,
        "hashes": {
          "error.log": "9ceae8da365827efe67c47411aa489efe9ab5b56d02796b8381ce6d46bc654f1",
          "evidence.txt": "dc833e58b95ef81b5f9b447eda74f8fb7352c566253d1e20b2fe9fffd9f51797",
          "output.raw": "84db1e5f32fbe230be5b9d04c3a79aad9fc53130f631b21545871b1204200678",
          "prompt.txt": "66f8e693da7ac4d8ee89abf3c106db52329e9c74e1555a2c150028744d84ff77",
          "report.md": "84db1e5f32fbe230be5b9d04c3a79aad9fc53130f631b21545871b1204200678",
          "review-context.md": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "stdout.log": "bebdd178c23f3c2c88339218cf1a0731b59859efbefa6070c9f52614a0a8e756",
          "task-context.md": "1cac45ee4af1f9c1879a031667b11d7448794d3506a3ee54ebf3b9767bcb8d24"
        },
        "published": {
          "20260908-board-start-task-key-task": true
        }
      },
      "findings": {
        "FINDINGS": [
          {
            "id": "QA-001",
            "tier": "medium",
            "text": "Inferred；置信度高。TUI 未管理后台启动的退出生命周期。用户确认 herdr 启动，卡片迁入 working 后，在等待 pane 就绪期间按 q 或 Ctrl+C，程序直接退出，后台启动被终止，Agent 尚未启动，容器清理和卡片回滚无法执行。最小修复：跟踪未完成启动，退出请求延迟至启动结果处理完毕，保持事件循环运行；用阻塞启动桩验证退出等待行为。",
            "evidence": "internal/tui/start.go:81–91 排队启动但不记录运行状态；internal/tui/app.go:94–96、530–531 直接退出；internal/tui/program.go:133–135 返回 tea.Quit；go.mod:7 使用 Bubble Tea v1.3.10，其 tea.go:349–365 不等待命令 goroutine；cmd/kander/main.go:10 调用 os.Exit。internal/launch/start.go:117 先迁入 working，internal/launch/agent.go:51–66 创建容器后等待就绪再启动 Agent；退出会截断该路径。"
          },
          {
            "id": "QA-002",
            "tier": "medium",
            "text": "Inferred；置信度高。成功提示把完整任务 ID、Agent、launcher 放在容器地址前，再由单行页脚截断。80 列窗口中，任务 20260908-board-start-task-key、Agent claude、launcher tmux-session、地址 kb-board-start-task-key-12345678:@9:%9 组成 102 列英文消息，实际显示止于 kb-board-sta...，window/pane 不可见，违反成功提示包含容器地址的要求。最小修复：按显示宽度保留 Agent、launcher 和完整地址，压缩任务 ID；空间仍不足时提供完整结果展示，并增加真实长度数据的渲染断言。",
            "evidence": "internal/tui/start.go:102–106 构造地址并放在消息末尾；internal/i18n/locales/en.json:9 定义成功消息顺序；internal/tui/board_view.go:109–111 将消息截成 w-1 列；internal/tui/text.go:49–74 使用省略号截断；internal/tui/start_test.go:158–161 仅检查 CopyNotice，未检查渲染后的地址可见性。"
          }
        ],
        "NON_BLOCKING": []
      },
      "assignment": {
        "run_id": "board-start-qa-1",
        "batch_id": "board-start-task-key-batch",
        "author": "codex",
        "basis": "已复核启动退出生命周期及页脚截断路径；QA-002 与 PM-001 同根因",
        "items": {
          "QA-001": [
            "20260908-board-start-task-key-task"
          ],
          "QA-002": [
            "20260908-board-start-task-key-task"
          ]
        },
        "owners": {
          "20260908-board-start-task-key-task": "codex"
        },
        "recorded_at": "2026-09-08T04:02:20.932966326Z"
      },
      "records": [
        {
          "submitted_revision": 14,
          "record_id": "fixed-qa-001",
          "run_id": "board-start-qa-1",
          "finding_id": "QA-001",
          "batch_id": "board-start-task-key-batch",
          "task_id": "20260908-board-start-task-key-task",
          "author": "codex",
          "recorded_at": "2026-09-08T04:05:58.41429839Z",
          "report_hash": "84db1e5f32fbe230be5b9d04c3a79aad9fc53130f631b21545871b1204200678",
          "original": "Inferred；置信度高。TUI 未管理后台启动的退出生命周期。用户确认 herdr 启动，卡片迁入 working 后，在等待 pane 就绪期间按 q 或 Ctrl+C，程序直接退出，后台启动被终止，Agent 尚未启动，容器清理和卡片回滚无法执行。最小修复：跟踪未完成启动，退出请求延迟至启动结果处理完毕，保持事件循环运行；用阻塞启动桩验证退出等待行为。",
          "status": "fixed",
          "basis": "已独立核对 Bubble Tea 不等待命令 goroutine、main 的 os.Exit 以及 launch 先认领再等待 pane 的路径。加入启动计数，q/Ctrl+C 延迟退出至全部结果回传，原有 launch 完成/回滚逻辑不变。",
          "fix_commit": "0eeb06bd84daf863d0817f5d1153f4b04b59f68a",
          "verification": "在 0eeb06bd84daf863d0817f5d1153f4b04b59f68a 执行 go test -count=1 -json ./internal/launch ./internal/tui ./internal/i18n，通过 208 项（含子测试）、跳过 1 项；go test -json ./... 通过 1170 项（含子测试）、跳过 1 项。新增阻塞启动桩验证 q/Ctrl+C、成功/失败、多启动等待及最终 tea.Quit。"
        },
        {
          "submitted_revision": 15,
          "record_id": "fixed-qa-002",
          "run_id": "board-start-qa-1",
          "finding_id": "QA-002",
          "batch_id": "board-start-task-key-batch",
          "task_id": "20260908-board-start-task-key-task",
          "author": "codex",
          "recorded_at": "2026-09-08T04:06:01.698105328Z",
          "report_hash": "84db1e5f32fbe230be5b9d04c3a79aad9fc53130f631b21545871b1204200678",
          "original": "Inferred；置信度高。成功提示把完整任务 ID、Agent、launcher 放在容器地址前，再由单行页脚截断。80 列窗口中，任务 20260908-board-start-task-key、Agent claude、launcher tmux-session、地址 kb-board-start-task-key-12345678:@9:%9 组成 102 列英文消息，实际显示止于 kb-board-sta...，window/pane 不可见，违反成功提示包含容器地址的要求。最小修复：按显示宽度保留 Agent、launcher 和完整地址，压缩任务 ID；空间仍不足时提供完整结果展示，并增加真实长度数据的渲染断言。",
          "status": "fixed",
          "basis": "已独立核对页脚 w-1 截断与末尾地址丢失。PM-001 与 QA-002 为同一根因；统一修复为窄屏精简任务 ID 信息、优先保留 Agent/launcher/完整地址，必要时以不接管输入的换行浮层展示完整结果。",
          "fix_commit": "0eeb06bd84daf863d0817f5d1153f4b04b59f68a",
          "verification": "在 0eeb06bd84daf863d0817f5d1153f4b04b59f68a 执行 go test -count=1 -json ./internal/launch ./internal/tui ./internal/i18n，通过 208 项（含子测试）、跳过 1 项；go test -json ./... 通过 1170 项（含子测试）、跳过 1 项。新增 80 列真实任务 ID/地址页脚断言，以及 40 列完整地址换行与输入不受阻断言。"
        }
      ]
    }
  ]
}


Caller supplemental context (verbatim; does not replace originals):
本轮修复原件中的退出生命周期及页脚地址问题。提交后定向测试208项通过、1跳过；全量1170项通过、1跳过，均0失败。自检已记录卡片；仅增量审核本轮非机械修复。