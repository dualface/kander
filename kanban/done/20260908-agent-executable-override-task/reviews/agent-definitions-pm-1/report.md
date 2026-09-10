Role: PM  
Commit: `56ec24d4ca46285288cccf219d971a3a8d970523`  
Task Context: `/tmp/codex-review.447b4e16e8f1ea11030cc4a67c955783/task-spec.md`  
Reviewed Scope: 配置、参数构造、会话、启动/恢复/通知、存活探测、接管清理、面板、review 隔离及相关测试。

原子需求追踪：Complete 20，Partial 6，Missing 0，Contradicted 0，Unverifiable 1。发现 4 项 medium，暂不通过。

Observed：目标提交匹配，工作区干净，范围内 `git diff --check` 无输出。只读限制下未重跑构建、测试及手工验证；IMPLEMENTATION 已记录作者验证结果，独立执行结果为 Unverifiable。

### FINDINGS

**PM-001 — medium — Inferred，高置信度：配置接受无法兑现的方言/会话组合。**

契约要求会话身份支持恢复，并在配置阶段拒绝非法定义。`agents.codex.session.mode="generated"` 会生成并保存 UUID，但 Codex start 参数不传该 UUID；后续 resume 却使用它。Cursor 方言配 `none` 时仍生成 UUID，并执行 `--resume <UUID>`，没有进行 chat 分配，违背 fresh recovery 语义。

证据：`internal/config/agents.go:205–233`、`internal/launch/session.go:85–95,366–397`、`internal/launch/commands.go:122,161–165`。最小修复：校验无模板方言与会话模式的兼容性；无法兑现的组合明确要求提供模板。

**PM-002 — medium — Inferred，高置信度：Windows 合法 PATH 名通过校验，却无法启动。**

配置 `path="kander-codex.exe"`，文件仅位于 PATH 目录时，`exec.LookPath` 校验成功；启动解析器先检查当前目录，随后因名称带扩展名直接返回 nil。doctor、面板探测及 allocated 命令同受影响。

证据：`internal/config/agents.go:137–145`、`internal/process/process.go:75–79,97–105`；消费者为 `internal/launch/session.go:420–425`、`internal/menu/agents.go:143–145`、`internal/launch/agent_allocate.go:13–16`。最小修复：统一校验和执行解析，支持带扩展名的 PATH 名，同时保留 Windows batch 编码通道。

**PM-003 — medium — Observed，高置信度：面板无法配置尚未选中的改名内置 agent。**

例如两个规模均选 Claude，PATH 只有 `kander-codex`、没有 `codex`，且尚无 Codex 覆盖配置。候选过滤会排除 Codex；输入框只为已选 agent 创建，因此无法通过面板设置其执行名。用户必须先手改 JSON，未满足内置执行名可直接编辑的要求。

证据：`internal/menu/options.go:91–98,202–204`、`internal/menu/agent_fields.go:10–16`、`internal/tui/options_form.go:492–498`。最小修复：允许选择未探测成功的内置 agent 以编辑路径，保存时再校验。

**PM-004 — medium — Observed，高置信度：自定义兼容方言未接通退出与接管清理。**

`my-claude` 声明 Claude 方言后可以启动，但 `dismiss` 和接管后的清理仍按原始 agent 名查询四项退出表，必然报 unsupported agent。完成卡无法正常关闭；接管保留旧执行进程。

既有清理契约见 `rules/KANDER-KANBAN-RULES.md:185,228,238`。证据：`internal/takeover/ops.go:32–45`、`internal/takeover/dismiss.go:57–63`、`internal/takeover/cleanup.go:154–156`。最小修复：兼容方言通过有效定义选择退出命令，保留身份与单 pane 校验。

NON-BLOCKING: none

只读权限禁止删除文件，任务文件保留。

```kander-findings
{
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
}
```