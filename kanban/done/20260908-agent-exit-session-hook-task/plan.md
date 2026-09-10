# 实现计划：退出命令与会话钩子

## 步骤

1. 在定义 schema 增加 `exit_command`（单行、无控制字符、可为空；省略则按 dialect / 嵌入定义继承）。内置 `codex`/`claude` 写 `/exit`，`grok`/`cursor` 写 `/quit`。`takeover.AgentExitCommand` 删除按名表，改为读解析后的定义；未定义或空值明确拒绝并保留容器。
2. 在单一文件 `internal/config/session_hooks.go` 维护内置钩子注册表：`codex-rollout`（启动后扫描 rollout、空 SESSION 可解析）、`cursor-create-chat`（启动前分配）。`session.mode` 接受 `hook:<已注册名>`；未注册名在加载时拒绝，错误含 agent 名与钩子名。`discovered` 仍不接受手工配置。
3. 嵌入定义改写：`codex` → `hook:codex-rollout`，`cursor` → `hook:cursor-create-chat`。删除 `AgentFor` 按 dialect 推导 `discovered`/`allocated` 及为 cursor 合成 `create-chat` 的分支，改为继承嵌入 `session`。`launch`/`liveness` 按钩子能力分派，不再判断 agent/dialect 名。
4. Cursor `create-chat` 评估：现有实现无独立 10s 超时、取最后一行非空 stdout、使用当时解析到的可执行路径；`allocated` 有 10s 上限且输出规则不同。本卡以钩子保留现有行为，不改为 `allocated`。
5. 组级 grep：`takeover`/`launch`/`notify`/`liveness`/`config` 业务代码无按内置 agent/dialect 名的 `switch`/`if`。`review` 包残留只核验并报告，不代为清理。
6. 同步 `docs/custom-agents.md`（会话策略、面板与审核边界、新增「钩子清单」）与 `rules/KANDER-KANBAN-RULES.md`（会话模式清单、dismiss 退出命令一句）。
7. 验证：`go build ./...`、`go vet ./...`、`go test ./...`、`GOOS=windows go build ./...`，以及交付自检。

## 影响模块

- `internal/config`：字段、校验、继承、钩子注册表
- `internal/takeover`：退出命令来源与 dismiss 测试
- `internal/launch`：会话分配 / 发现按钩子分派
- `internal/liveness`：空 SESSION 不反查改为读钩子能力
- `internal/notify`：预期无按名业务分支，只核验
- `docs/custom-agents.md`、`rules/KANDER-KANBAN-RULES.md`

## 验证

- 纯模板 agent 定义 `exit_command: "/bye"` 后，tmux dismiss 投递 `/bye` 与 Enter；未定义则拒绝并指出缺少该字段
- `hook:codex-rollout` / `hook:cursor-create-chat` 加载通过；未注册钩子拒绝
- 现有 Codex 会话发现与 Cursor create-chat 测试保持通过
- 注册表每个名字都出现在 `docs/custom-agents.md` 钩子清单
- 组级 grep 与四条构建/测试命令

## 发布与回滚

- 任务分支 `agent-exit-session-hook` 从组分支 `group/20260908-agent-definition-group` 创建，交付该组分支；不更新组分支、不合入 develop
- 回滚：丢弃本任务分支即可；定义字段向后兼容（旧配置省略 `exit_command` 时按 dialect 继承）
