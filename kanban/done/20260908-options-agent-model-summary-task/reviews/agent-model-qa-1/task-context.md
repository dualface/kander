# 简化流程图为执行与审核的 Agent 和 Model 清单

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 12:10
- OWNER: codex
- SESSION:
- WINDOW:
- STARTED_AT: 2026-09-08 12:12
- FINISHED_AT:
- TASK_BRANCH: options-agent-model-summary
- RESULT:

## GOAL

按用户对已交付流程图的纠正，改成简明的执行和审核 Agent + Model 清单，去掉冗长的执行至收尾步骤。

## USER_DECISIONS

用户原话：“流程图太复杂了。我的意思是列出：执行任务用什么 agent+model；审核阶段会跑哪些，每个阶段用什么 agent+model”。

## EXPECTED_OUTCOME

保留选项面板只读入口及滚动体验。正文只列大小任务的执行 Agent/实际生效 Model、启用的审核阶段和各角色 Agent/实际生效 Model。阶段一为 PM/QA，阶段二为 CSA/Hacker；skip 角色隐藏，required 与 auto 用短标签区分，注明审核触发后适用。review 关闭或全部 skip 时仅显示简短状态。读取当前选项会话，含未保存模型或开关变化。模型为空时显示 CLI 默认；复用现有模型回落函数。

## ACCEPTANCE_CRITERIA

- [ ] 正文仅有执行 Agent/Model 和审核阶段 Agent/Model 清单；删除模块摘要、launcher、单卡/任务组流程、Git/收尾步骤。
- [ ] 大小任务分别解析生效模型，保留旧共享 model 回落；审核按角色 model 优先、reviewer 默认回落，空模型显示 CLI 默认。
- [ ] 按 review 开关及阶段策略裁剪；skip 不列出、required/auto 可区分，保留阶段顺序及角色 Agent/Model 对应。
- [ ] 使用当前会话配置，打开与返回只读，不写配置或新增 dirty；现有滚动和60列三语换行可用。
- [ ] 三语文案及 README/AGENTS.md 与最终简化行为一致，删除废弃流程键及对应失效断言。
- [ ] 针对实际模型回落、裁剪、会话未保存变化有测试；模块根 go test ./... 通过并记录最终提交和计数。

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题：不修其他选项分区的行为或配置写入机制。
- 加固：不做规则文件解析、项目特例自动推断或配置校验告警。
- 共享契约与文档：不改 config schema、发布规则或卡片协议；仅同步本功能相关说明。
- 相邻功能：不加导出、图内跳转、命令入口或执行动作；不保留用户明确要求去掉的完整工作流。

## DISCUSSION

- 前置交付：20260908-options-workflow-flowchart-task，已 done，不重开旧卡。当前用户指令取代原卡的全流程展示范围。
- SELF_REVIEW: 已逐项核对用户纠正，契约只保留执行和审核的 Agent/Model，不再展示操作步骤；实际模型回落与阶段条件属于必要准确性，未引入额外功能。边界与验收明确。

## IMPLEMENTATION

用户补充：“按照实际配置显示”。使用面板当前会话配置及现有模型回落函数；示例仅用于说明，不固化实际值。

工作目录：/home/dualf/works/kander/worktrees/options-agent-model-summary。目标 develop；base 265aa39bd0a4919451df1f726adca85d6204863b。保留 flow 的配置读取及 TUI 单向消费边界，仅简化行类型；TUI 是唯一生产调用方，同批适配，无外部格式变更。


交付自检（c4bbc2b81a0eb0baefe0489a9e4873d10b6258aa）：
1. git diff --check 265aa39bd0a4919451df1f726adca85d6204863b HEAD 无输出（c4bbc2b81a0eb0baefe0489a9e4873d10b6258aa）。
2. wc -l internal/flow/*.go internal/tui/options_flow*.go：65、105、35、128，全部小于1000行（c4bbc2b81a0eb0baefe0489a9e4873d10b6258aa）。
3. AGENTS.md 包图与 README 功能说明已同步；三语从61个流程键简化到12个配置清单键（c4bbc2b81a0eb0baefe0489a9e4873d10b6258aa）。
4. 已检索生成/渲染调用链；删除旧行类型、步骤与废弃文案，无新增死代码（c4bbc2b81a0eb0baefe0489a9e4873d10b6258aa）。
5. 用户明确取消完整工作流展示，所以删除旧流程顺序及七模块节点断言；替换为实际模型回落、角色选择、无关模块不影响清单的测试。保留只读会话、滚动与窄宽覆盖，新增未保存模型及 CLI 默认验证，无重复测试（c4bbc2b81a0eb0baefe0489a9e4873d10b6258aa）。
6. go test -json ./internal/flow ./internal/tui ./internal/i18n：74个测试事件通过、0跳过、0失败、3包通过（c4bbc2b81a0eb0baefe0489a9e4873d10b6258aa）。
7. 模块根 go test -json ./...：1161个测试事件通过（含子测试）、1个 TestWindowsConsoleLauncher 平台跳过、0失败、21包通过；提交前后均已执行（c4bbc2b81a0eb0baefe0489a9e4873d10b6258aa）。

6/6验收项自检通过（c4bbc2b81a0eb0baefe0489a9e4873d10b6258aa），待PM/QA审核及集成。模型使用 config.KanbanModelFor/config.ReviewModelFor 实际回落，不硬编码本机当前值。

## SUMMARY

## CONTRACT_DECISIONS

```json
{
  "at": "2026-09-08 12:14",
  "decision": "用户本轮要求简化为执行和审核的实际 Agent/Model，并补充“按照实际配置显示”。删除卡片 USER_DECISIONS 中由执行者添加的执行安排陈述，避免冒充用户决定；用户功能要求不变。",
  "Before": {
    "ACCEPTANCE_CRITERIA": "- [ ] 正文仅有执行 Agent/Model 和审核阶段 Agent/Model 清单；删除模块摘要、launcher、单卡/任务组流程、Git/收尾步骤。\n- [ ] 大小任务分别解析生效模型，保留旧共享 model 回落；审核按角色 model 优先、reviewer 默认回落，空模型显示 CLI 默认。\n- [ ] 按 review 开关及阶段策略裁剪；skip 不列出、required/auto 可区分，保留阶段顺序及角色 Agent/Model 对应。\n- [ ] 使用当前会话配置，打开与返回只读，不写配置或新增 dirty；现有滚动和60列三语换行可用。\n- [ ] 三语文案及 README/AGENTS.md 与最终简化行为一致，删除废弃流程键及对应失效断言。\n- [ ] 针对实际模型回落、裁剪、会话未保存变化有测试；模块根 go test ./... 通过并记录最终提交和计数。",
    "EXPECTED_OUTCOME": "保留选项面板只读入口及滚动体验。正文只列大小任务的执行 Agent/实际生效 Model、启用的审核阶段和各角色 Agent/实际生效 Model。阶段一为 PM/QA，阶段二为 CSA/Hacker；skip 角色隐藏，required 与 auto 用短标签区分，注明审核触发后适用。review 关闭或全部 skip 时仅显示简短状态。读取当前选项会话，含未保存模型或开关变化。模型为空时显示 CLI 默认；复用现有模型回落函数。",
    "GOAL": "按用户对已交付流程图的纠正，改成简明的执行和审核 Agent + Model 清单，去掉冗长的执行至收尾步骤。",
    "OUT_OF_SCOPE": "- 既有问题：不修其他选项分区的行为或配置写入机制。\n- 加固：不做规则文件解析、项目特例自动推断或配置校验告警。\n- 共享契约与文档：不改 config schema、发布规则或卡片协议；仅同步本功能相关说明。\n- 相邻功能：不加导出、图内跳转、命令入口或执行动作；不保留用户明确要求去掉的完整工作流。",
    "SIZE": "small",
    "TASK_GROUP": "",
    "USER_DECISIONS": "用户原话：“流程图太复杂了。我的意思是列出：执行任务用什么 agent+model；审核阶段会跑哪些，每个阶段用什么 agent+model”。沿用当前任务的看板交付方式，在本会话完成修正。"
  },
  "After": {
    "ACCEPTANCE_CRITERIA": "- [ ] 正文仅有执行 Agent/Model 和审核阶段 Agent/Model 清单；删除模块摘要、launcher、单卡/任务组流程、Git/收尾步骤。\n- [ ] 大小任务分别解析生效模型，保留旧共享 model 回落；审核按角色 model 优先、reviewer 默认回落，空模型显示 CLI 默认。\n- [ ] 按 review 开关及阶段策略裁剪；skip 不列出、required/auto 可区分，保留阶段顺序及角色 Agent/Model 对应。\n- [ ] 使用当前会话配置，打开与返回只读，不写配置或新增 dirty；现有滚动和60列三语换行可用。\n- [ ] 三语文案及 README/AGENTS.md 与最终简化行为一致，删除废弃流程键及对应失效断言。\n- [ ] 针对实际模型回落、裁剪、会话未保存变化有测试；模块根 go test ./... 通过并记录最终提交和计数。",
    "EXPECTED_OUTCOME": "保留选项面板只读入口及滚动体验。正文只列大小任务的执行 Agent/实际生效 Model、启用的审核阶段和各角色 Agent/实际生效 Model。阶段一为 PM/QA，阶段二为 CSA/Hacker；skip 角色隐藏，required 与 auto 用短标签区分，注明审核触发后适用。review 关闭或全部 skip 时仅显示简短状态。读取当前选项会话，含未保存模型或开关变化。模型为空时显示 CLI 默认；复用现有模型回落函数。",
    "GOAL": "按用户对已交付流程图的纠正，改成简明的执行和审核 Agent + Model 清单，去掉冗长的执行至收尾步骤。",
    "OUT_OF_SCOPE": "- 既有问题：不修其他选项分区的行为或配置写入机制。\n- 加固：不做规则文件解析、项目特例自动推断或配置校验告警。\n- 共享契约与文档：不改 config schema、发布规则或卡片协议；仅同步本功能相关说明。\n- 相邻功能：不加导出、图内跳转、命令入口或执行动作；不保留用户明确要求去掉的完整工作流。",
    "SIZE": "small",
    "TASK_GROUP": "",
    "USER_DECISIONS": "用户原话：“流程图太复杂了。我的意思是列出：执行任务用什么 agent+model；审核阶段会跑哪些，每个阶段用什么 agent+model”。"
  }
}
```
