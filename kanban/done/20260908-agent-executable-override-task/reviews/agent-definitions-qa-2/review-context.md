KANDER_AUTOMATIC_CONTEXT_BYTES: 55882
PREVIOUS_RUN_ID: agent-definitions-qa-1
Prior report (verbatim):
Role: QA  
Commit: `56ec24d4ca46285288cccf219d971a3a8d970523`  
Task Context: 自定义执行 agent：程序路径、进程名、方言/模板、会话策略及面板配置。  
Reviewed Scope: 完整读取任务文件与 spec；依据 COMMIT TREE 检查变更及相关消费者。追踪 config、process、launch、notify、liveness、takeover、menu/TUI 和 reviewer 边界。只读环境未重跑构建及测试；采用 IMPLEMENTATION 中该提交的成功记录。独立确认 `gofmt`、diff 空白检查通过；36 个改动 Go 文件最大 864 行。

| 行为/质量 | 评估 |
|---|---|
| 配置校验、回退、深拷贝 | Observed：有确定性单元测试；Windows 解析存在不一致 |
| 方言、模板、prompt | Observed：参数逐项断言、空值省略、字面替换及 prompt 末位有覆盖 |
| 会话生成、分配、恢复 | Inferred：`none` 在方言路径及非恢复消费者中处理不完整 |
| 存活、通知、收尾 | Observed：进程名查询已接线；Inferred：退出命令仍限制内置名称 |
| 面板、保存、doctor | Observed：字段去重及保存有覆盖；Inferred：未选中的改名内置 agent 无编辑入口 |
| 架构、reviewer 隔离 | Observed：职责方向保持；review 独立使用 `*_REVIEW_BIN`，有测试锁定 |
| 构建与验收 | Observed（交付记录）：build/vet/test、Windows 交叉编译及两项真实 tmux 替身验证通过；Unverifiable：原生 Windows 运行 |

FINDINGS

**QA-001 — medium — 自定义方言无法正常退出及接管清理**  
Inferred；置信度：高。

`internal/takeover/ops.go:32-44` 仍按 agent 名查四项退出命令表。`dismiss.go:61`、`cleanup.go:89,154` 均传入原始名称。配置 `my-claude` 为 Claude 方言后，启动正常；完成后 `dismiss` 返回 unsupported，接管后旧进程也被保留。违反新增名称应贯通已接线收尾消费者的契约。

最小修复：退出命令按解析后的方言选择，保留现有身份和容器检查。用自定义 Claude/Grok 名称覆盖 dismiss 与接管清理。

**QA-002 — medium — `none` 的恢复限制误阻止 `dismiss`**  
Inferred；置信度：高。

`internal/launch/session.go:152-155` 在通用会话解析中拒绝 `none`；`internal/takeover/dismiss.go:57-59` 复用该解析。即使内置 Claude 配置 `session.mode=none`、卡片已有有效终端 UUID，任务完成后也在探测前被拒绝。禁止恢复不应禁止关闭已确认身份的终端。

最小修复：分离会话身份解析与恢复能力校验；resume 和通知直投继续检查能力，dismiss 使用身份解析。增加内置名称加 `none` 的收尾回归。

**QA-003 — medium — `none` 仅在模板路径生效，方言仍传恢复身份**  
Inferred；置信度：高。

`internal/launch/session.go:371-383` 仅为模板清空 session；Cursor 方言仍无条件追加 `--resume <Reference>`。`newAgentSession` 在 `none` 下生成标记 UUID（`:85-87`），却不分配 Cursor chat。因此合法配置 `cursor.session.mode=none` 会尝试恢复未分配的 UUID。Claude/Grok 路径也继续传 `--session-id`（`:398-409`）；通知恢复复用旧 Reference（`notify_resume.go:48-54`），并非独立的新会话。

最小修复：方言路径也落实 `none`，启动时省略会话创建/恢复参数，仅将 UUID 用作终端标记。逐方言断言 start 与通知恢复 argv。

**QA-004 — medium — Windows 接受的带扩展名 PATH 名无法启动**  
Inferred；置信度：高。

`internal/config/agents.go:137-145` 使用 `exec.LookPath` 校验，接受 PATH 中的 `wrapper.exe` 或 `wrapper.cmd`。启动却将原字符串传给 resolver（`internal/launch/session.go:420-421`）；`internal/process/process.go:75-79,97-105` 先按当前目录检查，遇到扩展名便返回 nil，不再搜索 PATH。程序位于 PATH 目录、项目目录无同名文件时，配置有效，start 和面板探测却报告不可用。分配程序同样受影响（`internal/launch/agent_allocate.go:14`）。

最小修复：统一配置与运行时的程序解析契约，让带扩展名的裸名称也搜索 PATH，并保留 batch 编码通道。增加 Windows PATH 目录与工作目录分离的 `.exe`/`.cmd` 用例。

**QA-005 — medium — 改名内置 agent 缺少面板配置入口**  
Inferred；置信度：高。

`internal/menu/options.go:91-98` 排除不可用且未显式声明方言/模板的内置 agent；`:203-204` 只补回当前选择。编辑字段又仅为已选 agent 生成（`internal/menu/agent_fields.go:10-19`、`internal/tui/options_form.go:496-500`）。

现有大小任务均选 Claude，用户安装只有 `kander-codex` 的 Codex 包装器时，Codex 不在下拉中，无法选中并填写新路径。必须先手改 JSON，未满足内置程序名可直接在面板配置的要求。

最小修复：面板允许选择待配置的内置 agent，并显示不可用状态；保存仍校验填写后的路径。通过真实 `NewSession` 测试该入口，避免仅用跳过探测的测试构造器。

NON-BLOCKING: none

任务文件已尝试删除；只读文件系统拒绝，文件遗留不影响审核结果。

```kander-findings
{
  "FINDINGS": [
    {
      "id": "QA-001",
      "tier": "medium",
      "text": "Inferred；置信度高。自定义 Claude/Grok 方言名称可以启动，但退出命令仍按四个内置名称查表，导致 dismiss 返回 unsupported，接管清理保留旧执行进程。最小修复：按解析后的方言选择退出命令，保留身份与容器门禁；增加自定义名称的 dismiss 和接管清理回归。",
      "evidence": "internal/takeover/ops.go:32-44 硬编码名称查表；internal/takeover/dismiss.go:61、internal/takeover/cleanup.go:89,154 传入原始 agent 名；internal/config/agents.go:187-192 接受自定义方言名称。新增名称未贯通相关收尾消费者。"
    },
    {
      "id": "QA-002",
      "tier": "medium",
      "text": "Inferred；置信度高。内置 Claude 设置 session.mode=none 后，即使卡片保留有效终端 UUID，dismiss 也因不支持恢复而在探测前失败。禁止恢复不应禁止关闭已确认身份的终端。最小修复：分离身份解析与恢复能力校验，让 dismiss 使用前者；保留 resume 与通知直投的能力限制，并增加 none 收尾回归。",
      "evidence": "internal/launch/session.go:152-155 在通用 resolvedTaskSession 中拒绝 none；internal/launch/notify_resume.go:26-27 暴露该解析；internal/takeover/dismiss.go:57-59 直接调用并返回错误；internal/launch/session.go:85-87 已为 none 保存 UUID。"
    },
    {
      "id": "QA-003",
      "tier": "medium",
      "text": "Inferred；置信度高。none 只在模板路径清空会话值，方言路径仍传会话参数。Cursor 的 none 启动携带 --resume 和未分配的标记 UUID；Claude/Grok 通知恢复继续用旧 UUID 创建会话，破坏 fresh-process 语义。最小修复：方言 none 启动省略会话创建/恢复参数，仅将 UUID 用于终端标记；逐方言测试 start 和通知恢复 argv。",
      "evidence": "internal/config/agents.go:205-231 接受无模板的方言加 none；internal/launch/session.go:85-87 生成 UUID 而不分配 chat，:371-375 只处理模板 none，:378-383 无条件给 Cursor 加 --resume，:398-409 给 Claude/Grok 加 --session-id；internal/launch/notify_resume.go:48-54 在 none 恢复中复用原 Reference。"
    },
    {
      "id": "QA-004",
      "tier": "medium",
      "text": "Inferred；置信度高。Windows 配置 path=wrapper.exe 或 wrapper.cmd，文件仅位于 PATH 目录时，配置校验通过，运行时却因带扩展名而只查当前目录后失败，影响启动、探测和会话分配。最小修复：统一程序解析，让带扩展名的裸名称搜索 PATH，保留 batch 编码通道；增加工作目录与 PATH 目录分离的 Windows 回归。",
      "evidence": "internal/config/agents.go:137-145 使用 exec.LookPath 校验；internal/launch/session.go:420-421 将原字符串交给 resolver；internal/process/process.go:75-79,97-105 对带扩展名名称仅检查直接路径，失败后不搜索 PATH；internal/menu/agents.go:144-145 和 internal/launch/agent_allocate.go:14 使用同一 resolver。"
    },
    {
      "id": "QA-005",
      "tier": "medium",
      "text": "Inferred；置信度高。大小任务当前均选 Claude，机器只有 kander-codex 包装器而没有 codex 时，Codex 被执行下拉过滤，程序路径字段仅为已选 agent 显示，用户无法通过面板配置该内置 agent，必须先手改 JSON。最小修复：允许选择待配置的内置 agent 并标注不可用，保存继续校验路径；通过真实 NewSession 覆盖此入口。",
      "evidence": "internal/menu/options.go:91-98 过滤不可用且无显式方言/模板的内置名称，:203-204 只补当前选择；internal/menu/agent_fields.go:10-19 仅生成已选 agent 字段；internal/tui/options_form.go:491-500 将该过滤下拉与已选字段组合，阻断新增路径的面板编辑入口。"
    }
  ],
  "NON_BLOCKING": []
}
```
Author records (verbatim JSON):
{
  "assignment": {
    "run_id": "agent-definitions-qa-1",
    "batch_id": "agent-definitions-batch",
    "author": "codex",
    "basis": "逐项回到 56ec24d4ca46285288cccf219d971a3a8d970523 的源码验证，并以新增回归在未修改源码时复现；相同根因跨角色合并处理。",
    "items": {
      "QA-001": [
        "20260908-agent-executable-override-task"
      ],
      "QA-002": [
        "20260908-agent-executable-override-task"
      ],
      "QA-003": [
        "20260908-agent-executable-override-task"
      ],
      "QA-004": [
        "20260908-agent-executable-override-task"
      ],
      "QA-005": [
        "20260908-agent-executable-override-task"
      ]
    },
    "owners": {
      "20260908-agent-executable-override-task": "codex"
    },
    "recorded_at": "2026-09-08T02:47:18.474384484Z"
  },
  "records": [
    {
      "submitted_revision": 17,
      "record_id": "fix-qa-001-1",
      "run_id": "agent-definitions-qa-1",
      "finding_id": "QA-001",
      "batch_id": "agent-definitions-batch",
      "task_id": "20260908-agent-executable-override-task",
      "author": "codex",
      "recorded_at": "2026-09-08T02:50:01.376722661Z",
      "report_hash": "dc338f79e09a5015a473e8e1673dc7af92c9826d4e200b1ed728a41a80d524c3",
      "original": "Inferred；置信度高。自定义 Claude/Grok 方言名称可以启动，但退出命令仍按四个内置名称查表，导致 dismiss 返回 unsupported，接管清理保留旧执行进程。最小修复：按解析后的方言选择退出命令，保留身份与容器门禁；增加自定义名称的 dismiss 和接管清理回归。",
      "status": "fixed",
      "basis": "合并根因退出方言：TestConfiguredAgentDismissAndCleanup 在原源码复现 my-claude/my-grok dismiss 和 cleanup 的 unsupported；现 AgentExitCommand 读取有效方言选择命令，回归核对 /exit 与 /quit，保留身份/容器门禁。",
      "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
      "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
    },
    {
      "submitted_revision": 18,
      "record_id": "fix-qa-002-1",
      "run_id": "agent-definitions-qa-1",
      "finding_id": "QA-002",
      "batch_id": "agent-definitions-batch",
      "task_id": "20260908-agent-executable-override-task",
      "author": "codex",
      "recorded_at": "2026-09-08T02:50:04.003892368Z",
      "report_hash": "dc338f79e09a5015a473e8e1673dc7af92c9826d4e200b1ed728a41a80d524c3",
      "original": "Inferred；置信度高。内置 Claude 设置 session.mode=none 后，即使卡片保留有效终端 UUID，dismiss 也因不支持恢复而在探测前失败。禁止恢复不应禁止关闭已确认身份的终端。最小修复：分离身份解析与恢复能力校验，让 dismiss 使用前者；保留 resume 与通知直投的能力限制，并增加 none 收尾回归。",
      "status": "fixed",
      "basis": "独立根因 none 身份解析：TestConfiguredAgentDismissAndCleanup/claude-dismiss 在原源码复现恢复能力错误阻止关闭。现 ResolvedSessionIdentity 仅解析与核实终端身份；dismiss 使用它，resume/notify 直投仍拒绝 none。",
      "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
      "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
    },
    {
      "submitted_revision": 19,
      "record_id": "fix-qa-003-1",
      "run_id": "agent-definitions-qa-1",
      "finding_id": "QA-003",
      "batch_id": "agent-definitions-batch",
      "task_id": "20260908-agent-executable-override-task",
      "author": "codex",
      "recorded_at": "2026-09-08T02:50:06.972831418Z",
      "report_hash": "dc338f79e09a5015a473e8e1673dc7af92c9826d4e200b1ed728a41a80d524c3",
      "original": "Inferred；置信度高。none 只在模板路径清空会话值，方言路径仍传会话参数。Cursor 的 none 启动携带 --resume 和未分配的标记 UUID；Claude/Grok 通知恢复继续用旧 UUID 创建会话，破坏 fresh-process 语义。最小修复：方言 none 启动省略会话创建/恢复参数，仅将 UUID 用于终端标记；逐方言测试 start 和通知恢复 argv。",
      "status": "fixed",
      "basis": "合并根因 session：在原源码 56ec24d4ca46285288cccf219d971a3a8d970523 上，TestDialectSessionCompatibility 复现 3 种被错误接受的组合；TestNoneDialectArgumentsDoNotReuseTerminalIdentity 复现 Claude/Grok/Cursor 携带标记身份。已校验不兼容的无模板模式，并让四种方言的 none 不传会话参数。",
      "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
      "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
    },
    {
      "submitted_revision": 20,
      "record_id": "fix-qa-004-1",
      "run_id": "agent-definitions-qa-1",
      "finding_id": "QA-004",
      "batch_id": "agent-definitions-batch",
      "task_id": "20260908-agent-executable-override-task",
      "author": "codex",
      "recorded_at": "2026-09-08T02:50:09.614128677Z",
      "report_hash": "dc338f79e09a5015a473e8e1673dc7af92c9826d4e200b1ed728a41a80d524c3",
      "original": "Inferred；置信度高。Windows 配置 path=wrapper.exe 或 wrapper.cmd，文件仅位于 PATH 目录时，配置校验通过，运行时却因带扩展名而只查当前目录后失败，影响启动、探测和会话分配。最小修复：统一程序解析，让带扩展名的裸名称搜索 PATH，保留 batch 编码通道；增加工作目录与 PATH 目录分离的 Windows 回归。",
      "status": "fixed",
      "basis": "合并根因 Windows PATH：原源码的 TestWindowsSuffixedNamesSearchPATH 对 .exe 返回 nil；现已统一带扩展名 PATH 搜索，覆盖 .exe/.cmd/.bat、工作目录同名影子、绝对路径和无扩展名绝对路径，保留 batch 编码。",
      "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
      "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
    },
    {
      "submitted_revision": 21,
      "record_id": "fix-qa-005-1",
      "run_id": "agent-definitions-qa-1",
      "finding_id": "QA-005",
      "batch_id": "agent-definitions-batch",
      "task_id": "20260908-agent-executable-override-task",
      "author": "codex",
      "recorded_at": "2026-09-08T02:50:12.316149158Z",
      "report_hash": "dc338f79e09a5015a473e8e1673dc7af92c9826d4e200b1ed728a41a80d524c3",
      "original": "Inferred；置信度高。大小任务当前均选 Claude，机器只有 kander-codex 包装器而没有 codex 时，Codex 被执行下拉过滤，程序路径字段仅为已选 agent 显示，用户无法通过面板配置该内置 agent，必须先手改 JSON。最小修复：允许选择待配置的内置 agent 并标注不可用，保存继续校验路径；通过真实 NewSession 覆盖此入口。",
      "status": "fixed",
      "basis": "合并根因面板入口：TestPanelCanConfigureUnselectedRenamedBuiltin 使用真实 NewSession 在原源码复现 Codex 不在候选；现所有声明及内置 agent 可进入下拉编辑，首次初始化仍要求实际可用 agent，显式路径保存继续校验。",
      "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
      "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
    }
  ]
}

Current batch disposition (tool generated):
{
  "schema": 1,
  "batch": {
    "plan_id": "agent-definitions-cycle",
    "task_context_hash": "8f067842dd4f5d1155a9759abfe36608f69c1d0b37616ae2a49ce71f0d25920b",
    "schema": 1,
    "batch_id": "agent-definitions-batch",
    "task_ids": [
      "20260908-agent-executable-override-task"
    ],
    "base": "26edb64fcfb654a96afedc30bbaea27e5e918ec7",
    "target_commit": "b208333dc128f4c8e72aae5308685bbaaaef417e",
    "report_language": "zh-CN",
    "requirements": {
      "CSA": "N/A: 本仓库 AGENTS.md 安全角色例外",
      "Hacker": "N/A: 本仓库 AGENTS.md 安全角色例外",
      "PM": "required",
      "QA": "required"
    },
    "advances": [
      {
        "previous_target": "56ec24d4ca46285288cccf219d971a3a8d970523",
        "target": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
        "reason": "修复 PM/QA 首轮五类已确认根因并完成最终提交验证",
        "deliveries": {
          "6dbed41ae20a5c930f47b209ea9dc4add2c1a771": "20260908-agent-executable-override-task"
        }
      },
      {
        "previous_target": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
        "target": "b208333dc128f4c8e72aae5308685bbaaaef417e",
        "reason": "修复 PM-005 低级警告文案；PM gate 已通过，QA 增量审核最终目标",
        "deliveries": {
          "b208333dc128f4c8e72aae5308685bbaaaef417e": "20260908-agent-executable-override-task"
        }
      }
    ],
    "revision": 3
  },
  "runs": [
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "",
        "run_id": "agent-definitions-pm-1",
        "batch_id": "agent-definitions-batch",
        "task_ids": [
          "20260908-agent-executable-override-task"
        ],
        "role": "PM",
        "reviewer": "codex",
        "model": "gpt-6-astra",
        "effort": "medium",
        "cwd": "/home/dualf/works/kander/worktrees/agent-executable-override",
        "base": "26edb64fcfb654a96afedc30bbaea27e5e918ec7",
        "commit": "56ec24d4ca46285288cccf219d971a3a8d970523",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "task-context.md": "8f067842dd4f5d1155a9759abfe36608f69c1d0b37616ae2a49ce71f0d25920b"
        },
        "kander_version": "20260907T173303Z-7e6fe2073ff0",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-08T02:37:17.951916251Z",
        "finished_at": "2026-09-08T02:41:13.487489207Z",
        "duration_ms": 235535,
        "hashes": {
          "error.log": "cc8a6e9bbc520183221ca7e864f5a3d038cf2ce6b77e502b00a8d9839ac309da",
          "evidence.txt": "b0cfdd854205494e379a3888d9410ace4a0eadaa94c3248d857457ef555344c0",
          "output.raw": "3f0aa25a7ec27a0a882e9b5d14e7a2e9f48ec46571d279e740dd5029f37abee8",
          "prompt.txt": "c441dbacc95d182fbbe09d61ca196f537d024182cd7aac75e803cfabdf02a082",
          "report.md": "3f0aa25a7ec27a0a882e9b5d14e7a2e9f48ec46571d279e740dd5029f37abee8",
          "review-context.md": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "stdout.log": "26f58674694af26378fd7161e402dded02dff28215f84dd4ad9165dac2c7a3e3",
          "task-context.md": "8f067842dd4f5d1155a9759abfe36608f69c1d0b37616ae2a49ce71f0d25920b"
        },
        "published": {
          "20260908-agent-executable-override-task": true
        }
      },
      "findings": {
        "FINDINGS": [
          {
            "id": "PM-001",
            "tier": "medium",
            "text": "Inferred，高置信度：配置接受无法兑现的方言/会话组合。agents.codex.session.mode=\"generated\" 生成并保存 UUID，但 Codex start 不传该 UUID，resume 却使用它，无法恢复刚启动的会话。Cursor 方言配 none 仍生成 UUID 并执行 --resume，未分配 chat，违背 fresh recovery 语义。违反会话身份与非法配置校验要求。最小修复：校验无模板方言与会话模式的兼容性，无法兑现的组合明确要求提供模板。",
            "evidence": "internal/config/agents.go:205-233 接受模式而不校验方言兼容性；internal/launch/session.go:85-95 生成 UUID；internal/launch/session.go:366-397 仅模板处理 none 空身份，Cursor 始终传 --resume，Codex start 不传身份；internal/launch/commands.go:122,161-165 保存身份且非空身份跳过发现。契约：task-spec.md ACCEPTANCE_CRITERIA 的配置校验、会话语义要求。"
          },
          {
            "id": "PM-002",
            "tier": "medium",
            "text": "Inferred，高置信度：Windows 下 path=\"kander-codex.exe\" 且程序仅在 PATH 目录时，配置校验成功，运行解析却返回 nil，导致 start、doctor、面板探测及同类 allocated 命令失败。违反 PATH 名可用于启动和可用性探测的要求。最小修复：统一校验与运行解析，支持带扩展名的 PATH 名，并保留 Windows batch 编码通道。",
            "evidence": "internal/config/agents.go:137-145 使用 exec.LookPath；internal/process/process.go:75-79,97-105 先按当前目录检查显式文件，带扩展名失败后不搜索 PATH；internal/launch/session.go:420-425、internal/menu/agents.go:143-145、internal/launch/agent_allocate.go:13-16 将配置名交给该解析器。契约：task-spec.md GOAL 第 1 项及 ACCEPTANCE_CRITERIA 路径校验、doctor 要求。"
          },
          {
            "id": "PM-003",
            "tier": "medium",
            "text": "Observed，高置信度：两个规模均选 Claude、PATH 只有 kander-codex 而无 codex、尚无覆盖定义时，面板过滤掉 Codex，且执行名输入只为已选 agent 创建。用户无法直接在面板配置改名 Codex，必须先手改 JSON。违反内置 agent 执行名和进程名可直接编辑的要求。最小修复：允许选择未探测成功的内置 agent 以编辑路径，保存时校验。",
            "evidence": "internal/menu/options.go:91-98 仅加入可用程序或已有 dialect/args 的定义；internal/menu/options.go:202-204 只补回当前选择；internal/menu/agent_fields.go:10-16 仅为已选 agent 创建输入；internal/tui/options_form.go:492-498 将这些候选与输入接入面板。契约：task-spec.md EXPECTED_OUTCOME 及 ACCEPTANCE_CRITERIA 面板要求。"
          },
          {
            "id": "PM-004",
            "tier": "medium",
            "text": "Observed，高置信度：my-claude 等自定义兼容方言可以启动，但 dismiss 与接管清理仍以原始 agent 名查询四项退出表，必然返回 unsupported agent。完成卡无法正常关闭，接管后旧执行进程保留。最小修复：通过有效定义的兼容方言选择退出命令，继续执行既有身份和单 pane 校验。",
            "evidence": "internal/takeover/ops.go:32-45 退出命令只识别四个原始名称；internal/takeover/dismiss.go:57-63、internal/takeover/cleanup.go:154-156 直接传入 session.Agent。任务 GOAL 将 takeover 列为受影响消费者；既有退出与接管清理契约位于 rules/KANDER-KANBAN-RULES.md:185,228,238。"
          }
        ],
        "NON_BLOCKING": []
      },
      "assignment": {
        "run_id": "agent-definitions-pm-1",
        "batch_id": "agent-definitions-batch",
        "author": "codex",
        "basis": "逐项回到 56ec24d4ca46285288cccf219d971a3a8d970523 的源码验证，并以新增回归在未修改源码时复现；相同根因跨角色合并处理。",
        "items": {
          "PM-001": [
            "20260908-agent-executable-override-task"
          ],
          "PM-002": [
            "20260908-agent-executable-override-task"
          ],
          "PM-003": [
            "20260908-agent-executable-override-task"
          ],
          "PM-004": [
            "20260908-agent-executable-override-task"
          ]
        },
        "owners": {
          "20260908-agent-executable-override-task": "codex"
        },
        "recorded_at": "2026-09-08T02:47:15.777717365Z"
      },
      "records": [
        {
          "submitted_revision": 13,
          "record_id": "fix-pm-001-1",
          "run_id": "agent-definitions-pm-1",
          "finding_id": "PM-001",
          "batch_id": "agent-definitions-batch",
          "task_id": "20260908-agent-executable-override-task",
          "author": "codex",
          "recorded_at": "2026-09-08T02:49:50.423393699Z",
          "report_hash": "3f0aa25a7ec27a0a882e9b5d14e7a2e9f48ec46571d279e740dd5029f37abee8",
          "original": "Inferred，高置信度：配置接受无法兑现的方言/会话组合。agents.codex.session.mode=\"generated\" 生成并保存 UUID，但 Codex start 不传该 UUID，resume 却使用它，无法恢复刚启动的会话。Cursor 方言配 none 仍生成 UUID 并执行 --resume，未分配 chat，违背 fresh recovery 语义。违反会话身份与非法配置校验要求。最小修复：校验无模板方言与会话模式的兼容性，无法兑现的组合明确要求提供模板。",
          "status": "fixed",
          "basis": "合并根因 session：在原源码 56ec24d4ca46285288cccf219d971a3a8d970523 上，TestDialectSessionCompatibility 复现 3 种被错误接受的组合；TestNoneDialectArgumentsDoNotReuseTerminalIdentity 复现 Claude/Grok/Cursor 携带标记身份。已校验不兼容的无模板模式，并让四种方言的 none 不传会话参数。",
          "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
          "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
        },
        {
          "submitted_revision": 14,
          "record_id": "fix-pm-002-1",
          "run_id": "agent-definitions-pm-1",
          "finding_id": "PM-002",
          "batch_id": "agent-definitions-batch",
          "task_id": "20260908-agent-executable-override-task",
          "author": "codex",
          "recorded_at": "2026-09-08T02:49:52.931876386Z",
          "report_hash": "3f0aa25a7ec27a0a882e9b5d14e7a2e9f48ec46571d279e740dd5029f37abee8",
          "original": "Inferred，高置信度：Windows 下 path=\"kander-codex.exe\" 且程序仅在 PATH 目录时，配置校验成功，运行解析却返回 nil，导致 start、doctor、面板探测及同类 allocated 命令失败。违反 PATH 名可用于启动和可用性探测的要求。最小修复：统一校验与运行解析，支持带扩展名的 PATH 名，并保留 Windows batch 编码通道。",
          "status": "fixed",
          "basis": "合并根因 Windows PATH：原源码的 TestWindowsSuffixedNamesSearchPATH 对 .exe 返回 nil；现已统一带扩展名 PATH 搜索，覆盖 .exe/.cmd/.bat、工作目录同名影子、绝对路径和无扩展名绝对路径，保留 batch 编码。",
          "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
          "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
        },
        {
          "submitted_revision": 15,
          "record_id": "fix-pm-003-1",
          "run_id": "agent-definitions-pm-1",
          "finding_id": "PM-003",
          "batch_id": "agent-definitions-batch",
          "task_id": "20260908-agent-executable-override-task",
          "author": "codex",
          "recorded_at": "2026-09-08T02:49:55.88078939Z",
          "report_hash": "3f0aa25a7ec27a0a882e9b5d14e7a2e9f48ec46571d279e740dd5029f37abee8",
          "original": "Observed，高置信度：两个规模均选 Claude、PATH 只有 kander-codex 而无 codex、尚无覆盖定义时，面板过滤掉 Codex，且执行名输入只为已选 agent 创建。用户无法直接在面板配置改名 Codex，必须先手改 JSON。违反内置 agent 执行名和进程名可直接编辑的要求。最小修复：允许选择未探测成功的内置 agent 以编辑路径，保存时校验。",
          "status": "fixed",
          "basis": "合并根因面板入口：TestPanelCanConfigureUnselectedRenamedBuiltin 使用真实 NewSession 在原源码复现 Codex 不在候选；现所有声明及内置 agent 可进入下拉编辑，首次初始化仍要求实际可用 agent，显式路径保存继续校验。",
          "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
          "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
        },
        {
          "submitted_revision": 16,
          "record_id": "fix-pm-004-1",
          "run_id": "agent-definitions-pm-1",
          "finding_id": "PM-004",
          "batch_id": "agent-definitions-batch",
          "task_id": "20260908-agent-executable-override-task",
          "author": "codex",
          "recorded_at": "2026-09-08T02:49:58.674646434Z",
          "report_hash": "3f0aa25a7ec27a0a882e9b5d14e7a2e9f48ec46571d279e740dd5029f37abee8",
          "original": "Observed，高置信度：my-claude 等自定义兼容方言可以启动，但 dismiss 与接管清理仍以原始 agent 名查询四项退出表，必然返回 unsupported agent。完成卡无法正常关闭，接管后旧执行进程保留。最小修复：通过有效定义的兼容方言选择退出命令，继续执行既有身份和单 pane 校验。",
          "status": "fixed",
          "basis": "合并根因退出方言：TestConfiguredAgentDismissAndCleanup 在原源码复现 my-claude/my-grok dismiss 和 cleanup 的 unsupported；现 AgentExitCommand 读取有效方言选择命令，回归核对 /exit 与 /quit，保留身份/容器门禁。",
          "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
          "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
        }
      ]
    },
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "",
        "run_id": "agent-definitions-pm-2",
        "batch_id": "agent-definitions-batch",
        "previous_run_id": "agent-definitions-pm-1",
        "task_ids": [
          "20260908-agent-executable-override-task"
        ],
        "role": "PM",
        "reviewer": "codex",
        "model": "gpt-6-astra",
        "effort": "medium",
        "cwd": "/home/dualf/works/kander/worktrees/agent-executable-override",
        "base": "26edb64fcfb654a96afedc30bbaea27e5e918ec7",
        "commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
        "reviewed_commit": "56ec24d4ca46285288cccf219d971a3a8d970523",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "a34d762330164d4bfc10fe1d4f3505c6e7c99b7ed6b09bfeab93e7be36f30970",
          "task-context.md": "8f067842dd4f5d1155a9759abfe36608f69c1d0b37616ae2a49ce71f0d25920b"
        },
        "kander_version": "20260907T173303Z-7e6fe2073ff0",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-08T02:51:35.114260093Z",
        "finished_at": "2026-09-08T02:54:15.212953857Z",
        "duration_ms": 160098,
        "hashes": {
          "error.log": "de6669208b6e123f5d08e26f7526df12f8753fb2911beca4a3d388ed94c5e40a",
          "evidence.txt": "1d642424fb3a48c529f5ea9c31100ffcb401559b16e05828d145a19d64c7bd0f",
          "output.raw": "ab5474fb174e048e9b62a565f15d9701fa8e94381be6e80a6dd7689a9630c26c",
          "prompt.txt": "7125cb7f3857c0df9e2ea4177850449465d7bccb709390ce1c2870008df3cb05",
          "report.md": "ab5474fb174e048e9b62a565f15d9701fa8e94381be6e80a6dd7689a9630c26c",
          "review-context.md": "a34d762330164d4bfc10fe1d4f3505c6e7c99b7ed6b09bfeab93e7be36f30970",
          "stdout.log": "3d2922dc2a0ef9303fd866b1ae2b27b57f3cf7b72f1886c863f1315bb8ac989c",
          "task-context.md": "8f067842dd4f5d1155a9759abfe36608f69c1d0b37616ae2a49ce71f0d25920b"
        },
        "published": {
          "20260908-agent-executable-override-task": true
        }
      },
      "findings": {
        "FINDINGS": [],
        "NON_BLOCKING": [
          {
            "id": "PM-005",
            "tier": "low",
            "text": "[outside-fix-range] Inferred，高置信度：面板候选修复使旧警告失真。程序存在但 --version 失败时，仍提示“不可作为新选择”，实际已允许选择并编辑路径。影响仅为配置提示误导。最小改动：将警告改为版本探测失败、可选择后修改可执行路径。",
            "evidence": "internal/menu/options.go:84-88 在程序存在但版本探测失败时生成旧警告；本次修复的 internal/menu/options.go:97-105 无条件保留该执行候选。internal/i18n/locales/en.json:584、internal/i18n/locales/ja.json:584、internal/i18n/locales/zh-CN.json:584 仍称其不能作为新选择。警告文本位于修复范围外，其与实际行为的冲突由本轮候选修复引入。"
          }
        ]
      },
      "assignment": {
        "run_id": "agent-definitions-pm-2",
        "batch_id": "agent-definitions-batch",
        "author": "codex",
        "basis": "核实候选已可选而旧警告仍称不可选，是本轮引入的提示误导。",
        "items": {
          "PM-005": [
            "20260908-agent-executable-override-task"
          ]
        },
        "owners": {
          "20260908-agent-executable-override-task": "codex"
        },
        "recorded_at": "2026-09-08T02:56:17.623312104Z"
      },
      "records": [
        {
          "submitted_revision": 26,
          "record_id": "fix-pm-005-1",
          "run_id": "agent-definitions-pm-2",
          "finding_id": "PM-005",
          "batch_id": "agent-definitions-batch",
          "task_id": "20260908-agent-executable-override-task",
          "author": "codex",
          "recorded_at": "2026-09-08T02:56:21.604437187Z",
          "report_hash": "ab5474fb174e048e9b62a565f15d9701fa8e94381be6e80a6dd7689a9630c26c",
          "original": "[outside-fix-range] Inferred，高置信度：面板候选修复使旧警告失真。程序存在但 --version 失败时，仍提示“不可作为新选择”，实际已允许选择并编辑路径。影响仅为配置提示误导。最小改动：将警告改为版本探测失败、可选择后修改可执行路径。",
          "status": "fixed",
          "basis": "对照 internal/menu/options.go 的候选保留逻辑和三份 locale 原文，确认提示矛盾。仅修改三语警告，说明可选中 agent 编辑路径；未改变选择或启动行为。",
          "fix_commit": "b208333dc128f4c8e72aae5308685bbaaaef417e",
          "verification": "提交 b208333dc128f4c8e72aae5308685bbaaaef417e；git diff 6dbed41ae20a5c930f47b209ea9dc4add2c1a771..HEAD 仅三个 locale 的警告各一行；原有菜单与 i18n 测试通过；最终提交全量复验另记 IMPLEMENTATION。"
        }
      ]
    },
    {
      "run": {
        "schema": 1,
        "findings_schema": 1,
        "task_group": "",
        "run_id": "agent-definitions-qa-1",
        "batch_id": "agent-definitions-batch",
        "task_ids": [
          "20260908-agent-executable-override-task"
        ],
        "role": "QA",
        "reviewer": "codex",
        "model": "gpt-6-astra",
        "effort": "medium",
        "cwd": "/home/dualf/works/kander/worktrees/agent-executable-override",
        "base": "26edb64fcfb654a96afedc30bbaea27e5e918ec7",
        "commit": "56ec24d4ca46285288cccf219d971a3a8d970523",
        "report_language": "zh-CN",
        "input_hashes": {
          "review-context.md": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "task-context.md": "8f067842dd4f5d1155a9759abfe36608f69c1d0b37616ae2a49ce71f0d25920b"
        },
        "kander_version": "20260907T173303Z-7e6fe2073ff0",
        "phase": "finalized",
        "launch_status": "started",
        "execution_status": "ok",
        "semantic_status": "unassessed",
        "exit_code": 0,
        "created_at": "2026-09-08T02:37:20.45645563Z",
        "finished_at": "2026-09-08T02:41:41.265787109Z",
        "duration_ms": 260809,
        "hashes": {
          "error.log": "4c12331d7f55f20cad9f053d66be8b701aa871c94b9efa4388e6be97f360cb62",
          "evidence.txt": "b0cfdd854205494e379a3888d9410ace4a0eadaa94c3248d857457ef555344c0",
          "output.raw": "dc338f79e09a5015a473e8e1673dc7af92c9826d4e200b1ed728a41a80d524c3",
          "prompt.txt": "0e431ff17205ff1f5aef649b5812778428b6ee314b8646cc56dd5766b825f95f",
          "report.md": "dc338f79e09a5015a473e8e1673dc7af92c9826d4e200b1ed728a41a80d524c3",
          "review-context.md": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
          "stdout.log": "f784ed7f894249e3bfb8255c9645abd709cb860cc286eb068f83aaea17f0c09d",
          "task-context.md": "8f067842dd4f5d1155a9759abfe36608f69c1d0b37616ae2a49ce71f0d25920b"
        },
        "published": {
          "20260908-agent-executable-override-task": true
        }
      },
      "findings": {
        "FINDINGS": [
          {
            "id": "QA-001",
            "tier": "medium",
            "text": "Inferred；置信度高。自定义 Claude/Grok 方言名称可以启动，但退出命令仍按四个内置名称查表，导致 dismiss 返回 unsupported，接管清理保留旧执行进程。最小修复：按解析后的方言选择退出命令，保留身份与容器门禁；增加自定义名称的 dismiss 和接管清理回归。",
            "evidence": "internal/takeover/ops.go:32-44 硬编码名称查表；internal/takeover/dismiss.go:61、internal/takeover/cleanup.go:89,154 传入原始 agent 名；internal/config/agents.go:187-192 接受自定义方言名称。新增名称未贯通相关收尾消费者。"
          },
          {
            "id": "QA-002",
            "tier": "medium",
            "text": "Inferred；置信度高。内置 Claude 设置 session.mode=none 后，即使卡片保留有效终端 UUID，dismiss 也因不支持恢复而在探测前失败。禁止恢复不应禁止关闭已确认身份的终端。最小修复：分离身份解析与恢复能力校验，让 dismiss 使用前者；保留 resume 与通知直投的能力限制，并增加 none 收尾回归。",
            "evidence": "internal/launch/session.go:152-155 在通用 resolvedTaskSession 中拒绝 none；internal/launch/notify_resume.go:26-27 暴露该解析；internal/takeover/dismiss.go:57-59 直接调用并返回错误；internal/launch/session.go:85-87 已为 none 保存 UUID。"
          },
          {
            "id": "QA-003",
            "tier": "medium",
            "text": "Inferred；置信度高。none 只在模板路径清空会话值，方言路径仍传会话参数。Cursor 的 none 启动携带 --resume 和未分配的标记 UUID；Claude/Grok 通知恢复继续用旧 UUID 创建会话，破坏 fresh-process 语义。最小修复：方言 none 启动省略会话创建/恢复参数，仅将 UUID 用于终端标记；逐方言测试 start 和通知恢复 argv。",
            "evidence": "internal/config/agents.go:205-231 接受无模板的方言加 none；internal/launch/session.go:85-87 生成 UUID 而不分配 chat，:371-375 只处理模板 none，:378-383 无条件给 Cursor 加 --resume，:398-409 给 Claude/Grok 加 --session-id；internal/launch/notify_resume.go:48-54 在 none 恢复中复用原 Reference。"
          },
          {
            "id": "QA-004",
            "tier": "medium",
            "text": "Inferred；置信度高。Windows 配置 path=wrapper.exe 或 wrapper.cmd，文件仅位于 PATH 目录时，配置校验通过，运行时却因带扩展名而只查当前目录后失败，影响启动、探测和会话分配。最小修复：统一程序解析，让带扩展名的裸名称搜索 PATH，保留 batch 编码通道；增加工作目录与 PATH 目录分离的 Windows 回归。",
            "evidence": "internal/config/agents.go:137-145 使用 exec.LookPath 校验；internal/launch/session.go:420-421 将原字符串交给 resolver；internal/process/process.go:75-79,97-105 对带扩展名名称仅检查直接路径，失败后不搜索 PATH；internal/menu/agents.go:144-145 和 internal/launch/agent_allocate.go:14 使用同一 resolver。"
          },
          {
            "id": "QA-005",
            "tier": "medium",
            "text": "Inferred；置信度高。大小任务当前均选 Claude，机器只有 kander-codex 包装器而没有 codex 时，Codex 被执行下拉过滤，程序路径字段仅为已选 agent 显示，用户无法通过面板配置该内置 agent，必须先手改 JSON。最小修复：允许选择待配置的内置 agent 并标注不可用，保存继续校验路径；通过真实 NewSession 覆盖此入口。",
            "evidence": "internal/menu/options.go:91-98 过滤不可用且无显式方言/模板的内置名称，:203-204 只补当前选择；internal/menu/agent_fields.go:10-19 仅生成已选 agent 字段；internal/tui/options_form.go:491-500 将该过滤下拉与已选字段组合，阻断新增路径的面板编辑入口。"
          }
        ],
        "NON_BLOCKING": []
      },
      "assignment": {
        "run_id": "agent-definitions-qa-1",
        "batch_id": "agent-definitions-batch",
        "author": "codex",
        "basis": "逐项回到 56ec24d4ca46285288cccf219d971a3a8d970523 的源码验证，并以新增回归在未修改源码时复现；相同根因跨角色合并处理。",
        "items": {
          "QA-001": [
            "20260908-agent-executable-override-task"
          ],
          "QA-002": [
            "20260908-agent-executable-override-task"
          ],
          "QA-003": [
            "20260908-agent-executable-override-task"
          ],
          "QA-004": [
            "20260908-agent-executable-override-task"
          ],
          "QA-005": [
            "20260908-agent-executable-override-task"
          ]
        },
        "owners": {
          "20260908-agent-executable-override-task": "codex"
        },
        "recorded_at": "2026-09-08T02:47:18.474384484Z"
      },
      "records": [
        {
          "submitted_revision": 17,
          "record_id": "fix-qa-001-1",
          "run_id": "agent-definitions-qa-1",
          "finding_id": "QA-001",
          "batch_id": "agent-definitions-batch",
          "task_id": "20260908-agent-executable-override-task",
          "author": "codex",
          "recorded_at": "2026-09-08T02:50:01.376722661Z",
          "report_hash": "dc338f79e09a5015a473e8e1673dc7af92c9826d4e200b1ed728a41a80d524c3",
          "original": "Inferred；置信度高。自定义 Claude/Grok 方言名称可以启动，但退出命令仍按四个内置名称查表，导致 dismiss 返回 unsupported，接管清理保留旧执行进程。最小修复：按解析后的方言选择退出命令，保留身份与容器门禁；增加自定义名称的 dismiss 和接管清理回归。",
          "status": "fixed",
          "basis": "合并根因退出方言：TestConfiguredAgentDismissAndCleanup 在原源码复现 my-claude/my-grok dismiss 和 cleanup 的 unsupported；现 AgentExitCommand 读取有效方言选择命令，回归核对 /exit 与 /quit，保留身份/容器门禁。",
          "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
          "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
        },
        {
          "submitted_revision": 18,
          "record_id": "fix-qa-002-1",
          "run_id": "agent-definitions-qa-1",
          "finding_id": "QA-002",
          "batch_id": "agent-definitions-batch",
          "task_id": "20260908-agent-executable-override-task",
          "author": "codex",
          "recorded_at": "2026-09-08T02:50:04.003892368Z",
          "report_hash": "dc338f79e09a5015a473e8e1673dc7af92c9826d4e200b1ed728a41a80d524c3",
          "original": "Inferred；置信度高。内置 Claude 设置 session.mode=none 后，即使卡片保留有效终端 UUID，dismiss 也因不支持恢复而在探测前失败。禁止恢复不应禁止关闭已确认身份的终端。最小修复：分离身份解析与恢复能力校验，让 dismiss 使用前者；保留 resume 与通知直投的能力限制，并增加 none 收尾回归。",
          "status": "fixed",
          "basis": "独立根因 none 身份解析：TestConfiguredAgentDismissAndCleanup/claude-dismiss 在原源码复现恢复能力错误阻止关闭。现 ResolvedSessionIdentity 仅解析与核实终端身份；dismiss 使用它，resume/notify 直投仍拒绝 none。",
          "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
          "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
        },
        {
          "submitted_revision": 19,
          "record_id": "fix-qa-003-1",
          "run_id": "agent-definitions-qa-1",
          "finding_id": "QA-003",
          "batch_id": "agent-definitions-batch",
          "task_id": "20260908-agent-executable-override-task",
          "author": "codex",
          "recorded_at": "2026-09-08T02:50:06.972831418Z",
          "report_hash": "dc338f79e09a5015a473e8e1673dc7af92c9826d4e200b1ed728a41a80d524c3",
          "original": "Inferred；置信度高。none 只在模板路径清空会话值，方言路径仍传会话参数。Cursor 的 none 启动携带 --resume 和未分配的标记 UUID；Claude/Grok 通知恢复继续用旧 UUID 创建会话，破坏 fresh-process 语义。最小修复：方言 none 启动省略会话创建/恢复参数，仅将 UUID 用于终端标记；逐方言测试 start 和通知恢复 argv。",
          "status": "fixed",
          "basis": "合并根因 session：在原源码 56ec24d4ca46285288cccf219d971a3a8d970523 上，TestDialectSessionCompatibility 复现 3 种被错误接受的组合；TestNoneDialectArgumentsDoNotReuseTerminalIdentity 复现 Claude/Grok/Cursor 携带标记身份。已校验不兼容的无模板模式，并让四种方言的 none 不传会话参数。",
          "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
          "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
        },
        {
          "submitted_revision": 20,
          "record_id": "fix-qa-004-1",
          "run_id": "agent-definitions-qa-1",
          "finding_id": "QA-004",
          "batch_id": "agent-definitions-batch",
          "task_id": "20260908-agent-executable-override-task",
          "author": "codex",
          "recorded_at": "2026-09-08T02:50:09.614128677Z",
          "report_hash": "dc338f79e09a5015a473e8e1673dc7af92c9826d4e200b1ed728a41a80d524c3",
          "original": "Inferred；置信度高。Windows 配置 path=wrapper.exe 或 wrapper.cmd，文件仅位于 PATH 目录时，配置校验通过，运行时却因带扩展名而只查当前目录后失败，影响启动、探测和会话分配。最小修复：统一程序解析，让带扩展名的裸名称搜索 PATH，保留 batch 编码通道；增加工作目录与 PATH 目录分离的 Windows 回归。",
          "status": "fixed",
          "basis": "合并根因 Windows PATH：原源码的 TestWindowsSuffixedNamesSearchPATH 对 .exe 返回 nil；现已统一带扩展名 PATH 搜索，覆盖 .exe/.cmd/.bat、工作目录同名影子、绝对路径和无扩展名绝对路径，保留 batch 编码。",
          "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
          "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
        },
        {
          "submitted_revision": 21,
          "record_id": "fix-qa-005-1",
          "run_id": "agent-definitions-qa-1",
          "finding_id": "QA-005",
          "batch_id": "agent-definitions-batch",
          "task_id": "20260908-agent-executable-override-task",
          "author": "codex",
          "recorded_at": "2026-09-08T02:50:12.316149158Z",
          "report_hash": "dc338f79e09a5015a473e8e1673dc7af92c9826d4e200b1ed728a41a80d524c3",
          "original": "Inferred；置信度高。大小任务当前均选 Claude，机器只有 kander-codex 包装器而没有 codex 时，Codex 被执行下拉过滤，程序路径字段仅为已选 agent 显示，用户无法通过面板配置该内置 agent，必须先手改 JSON。最小修复：允许选择待配置的内置 agent 并标注不可用，保存继续校验路径；通过真实 NewSession 覆盖此入口。",
          "status": "fixed",
          "basis": "合并根因面板入口：TestPanelCanConfigureUnselectedRenamedBuiltin 使用真实 NewSession 在原源码复现 Codex 不在候选；现所有声明及内置 agent 可进入下拉编辑，首次初始化仍要求实际可用 agent，显式路径保存继续校验。",
          "fix_commit": "6dbed41ae20a5c930f47b209ea9dc4add2c1a771",
          "verification": "提交 6dbed41ae20a5c930f47b209ea9dc4add2c1a771：go test ./internal/config ./internal/launch ./internal/process ./internal/menu ./internal/takeover -run Test(DialectSessionCompatibility|NoneDialectArgumentsDoNotReuseTerminalIdentity|WindowsSuffixedNamesSearchPATH|WindowsAbsoluteExtensionlessProgram|PanelCanConfigureUnselectedRenamedBuiltin|ConfiguredAgentDismissAndCleanup) -count=1 -json，退出 0；修复前相同回归已复现。Windows 为可注入平台解析测试，未声称原生执行。"
        }
      ]
    }
  ]
}


Caller supplemental context (verbatim; does not replace originals):
首轮 9 个来源 finding 合并五类，已独立复现并修复，作者原件及汇总由工具加载。PM 增量已通过，其低级警告文案 PM-005 也已修复。最终提交 b208333dc128f4c8e72aae5308685bbaaaef417e：全量 19 包、1101 条通过、1 平台跳过；build/vet/gofmt/diff 检查通过，真实 tmux 两项替身 start/check alive 再次通过，Windows 10 包交叉编译通过，未声明原生执行。只审核 QA 上轮目标之后的修复范围。