# 实现计划

阶段按卡片建议执行，全部在任务 worktree `worktrees/agent-definition-embed`、分支 `agent-definition-embed`（基于 `group/20260908-agent-definition-group`）完成。

## 1. 定义结构与三层校验

- 嵌入目录：`internal/config/agents/`，含 `index.json`（固定加载顺序 `codex, claude, grok, cursor`）与四份定义。
- 每份含卡片要求的全部字段；`schema_version` 必须为 1。
- 新增占位符 `{session=}`：空 session 也替换、不丢相邻 flag，用于复现改动前内置 argv 在空 session 时仍带空参数的行为；`session.mode=none` 时先改写成 `{session}` 再展开。现有 `{session}` 丢弃规则不变。
- 三层校验：嵌入允许 `discovered` 且不 LookPath；用户 `agents.<name>` 仍拒 `discovered`，显式 `path` 仍 LookPath；缺 `schema_version` 按 1。

## 2. config 三张表迁移与 `config --json`

- 删除 `AgentExecutables` / `kanbanModelDefaults` / `reviewModelDefaults`。
- `ExecutionAgents`、默认可执行名、规模/审核模型默认、显示名、`supports_effort`、规则目标均从嵌入定义读取。
- `DefaultConfig` 的默认 agent 取 `ExecutionAgents[0]`。`ReviewAgents` 与 dialect 推导 session 按合同保留。
- 用改动前 `json.MarshalIndent(DefaultConfig())` 快照固定逐字节兼容。

## 3. launch argv 表驱动

- `AgentFor` 在无用户 `args` 时从方言/名字的嵌入定义拷贝模板，`agentArguments` 只做模板展开，不再按名分支。
- 表驱动覆盖四个内置 × {start,resume} × {large,small} × {有/无 session}，期望值取自改动前输出。

## 4. `LaunchPlan` 与 `pane` 投递

- `LaunchPlan` 增加 `PromptDelivery` 与 `Prompt`；`launchAgent` 按字段分发，不新增回调。
- `mode: argv`：调用方仍 `append` 提示词；桩不得出现 `send-keys` / `agent prompt`。
- `mode: pane`：argv 不含提示词；`pane run` 之后、`paneSession` 之前做第二段就绪（ready/blocked 并行，blocked 优先短路），再按 notify 原语投递。
- 三种失败同一处置：抓 pane 输出 → 关本次容器 → `LaunchFailure` 回滚；1/2 含手动跑 CLI 指引，3 不含。
- `foreground`/`console` 在 claim 之前拒绝。`start` 与 `resume` 接管走两段就绪；`notify` 直投不等 ready。

## 5. install / menu

- `AgentRulesTarget` / 写入与识别按 `rules_target` + `rules_integration` 分发，不再按名分支。
- 项目模式按策略排序（`claude-import` 先），`seen[target]` 去重后仍是 CLAUDE.md + AGENTS.md。
- 无 `rules_target` 的自定义 agent 不回落到 grok 路径。
- 选项面板与 doctor 显示名、effort 显隐读定义。

## 6. 文档与规则

- `docs/custom-agents.md`、`AGENTS.md` 包表；解析结构只引用 `docs/output-parsing.md`。
- `rules/KANDER-KANBAN-RULES.md` 四处 argv 原文保留，只追加 `mode: pane` 条款；started 按 herdr / tmux 分别改写。
