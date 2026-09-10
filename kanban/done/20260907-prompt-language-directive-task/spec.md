# Write the card language directive explicitly into the launch prompt

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 11:44
- OWNER: cursor
- SESSION: cursor 7bdab369-c16f-4833-a911-d992277e6438
- WINDOW: herdr:wX:tN:wX:p15
- STARTED_AT: 2026-09-07 11:45
- FINISHED_AT: 2026-09-07 12:05
- TASK_BRANCH: prompt-language-directive
- RESULT: completed

## GOAL

`kander start` / `resume` / `notify` 写给执行 Agent 的任务文件目前只要求「读入口规则并跑 config」, 沟通语种靠 Agent 读规则后自觉遵守. 本卡在这三类 prompt 的固定要求段加入一句明确的语种指令, 值取卡片 `LANGUAGE` 字段 (develop 上 e97bc34 起由 `kander new` 写入), 旧卡缺字段时回落当前作用域配置的 `agent_language`, 让语种成为 Agent 一开工就收到的硬指令.

## USER_DECISIONS

- 指令句固定英文, 语种值原样嵌入, 不随界面语言 (cn/en/ja) 翻译, 不新增 i18n 键; 避免与并行进行的日语目录卡 `20260907-japanese-ui-task` 在 `locales/*.json` 上冲突.
- 措辞: `Communicate with the user and write all card content, records and reports in "<value>". Commit messages and code comments follow the project's conventions.`
- 取值优先级: 卡片 `LANGUAGE` > 配置 `agent_language`. 两者都拿不到 (无配置文件时按界面语言推导; 配置损坏时报错) 与 `kander new` 的默认解析一致.
- 接管 (`resume --agent`) 与恢复 (`notify` 内部恢复) 走同一段, 三类 prompt 行为一致.
- `kander review` 已有等价指令, 不动.

## EXPECTED_OUTCOME

- `internal/launch` 的 start / resume / takeover 三个 prompt 组装函数都在规则加载指令之后、任务说明之前插入上述语种句; 句中的语种来自卡片正文 `LANGUAGE`, 缺字段时来自配置.
- 卡片有 `LANGUAGE: ja` 时 prompt 含 `in "ja"`; 卡片无该字段而配置 `agent_language` 为 `zh-CN` 时含 `in "zh-CN"`.
- 配置文件损坏且卡片无字段时, 启动/恢复报配置错误并按既有回滚路径不改卡片, 不猜语种.
- `promptPrefixes` 对会话反查的头部匹配不受影响 (语种句不在 head 里).
- 规则入口 `KANDER-AGENTS.md` Language 一节补一句: 启动与恢复 prompt 会带该指令, 与卡片字段一致.
- `go build ./...`, `go vet ./...`, `go test ./...` 通过.

## ACCEPTANCE_CRITERIA

- [x] `internal/launch` 有一个统一的语种解析函数: 输入卡片正文与安装作用域, 输出语种字符串或错误; 三类 prompt 都经它取值并拼入固定英文指令句.
- [x] `internal/launch/prompts_test.go` 新增用例覆盖: 卡片带 `LANGUAGE`、卡片缺字段回落配置、配置损坏报错三种情况, 且三类 prompt 都断言含 `in "<value>"`.
- [x] 现有 prompt 头部匹配测试与 codex 会话反查测试继续通过.
- [x] `rules/KANDER-AGENTS.md` Language 一节和仓库 `AGENTS.md` 各补一句说明.
- [x] 不修改 `internal/i18n/locales/*.json`, 不修改 `internal/board`, 不修改 `kander review`.
- [x] 全量 `go test ./...` 通过.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题: prompt 其余措辞、`launch.prompt.*` 随界面语言翻译的现状 (用户已确认保留) 不在本卡; 改动它们等于扩大范围.
- 加固: 并发、跨平台、安全加固不在验收条件内, Windows 启动路径不变.
- 共享契约与文档: 卡片 `LANGUAGE` 字段的生成与校验已在 develop 完成, 本卡只读取; `kander review` 的报告语种指令已存在, 不重复; i18n 目录归日语卡所有, 本卡不碰.
- 相邻功能: 让 `kander` 自身 CLI 输出跟随卡片语种、允许运行中修改 `LANGUAGE`、按角色覆盖语种均不做.

## DISCUSSION

SELF_REVIEW: 目标与成果对应用户要求「给明确指令」; 取值优先级、固定英文措辞、不新增 i18n 键三项均为用户在计划中确认的决策, 未把建议写成决定. 边界清楚: 只改 `internal/launch` 的 prompt 组装与两处文档, 与并行日语卡在文件上不重叠. 验收条件可执行可判定, 覆盖解析函数、三种取值情况、三类 prompt、头部匹配回归与文档. 无范围外要求.

REVIEW: 首轮 PM/QA 指出 `notify` 直投路径未带语种指令 (GOAL 含 notify 任务文件); 已用 `launch.RuleLoadingWithLanguage` 修复并补直投测试. 增量复审 PM/QA 均通过; 本仓库 CSA/Hacker N/A.

## IMPLEMENTATION

- 分支 `prompt-language-directive` (已合入 develop 并清理).
- `resolvePromptLanguage` + 固定英文 `promptLanguageDirective`; start/resume/takeover 经 `ruleLoadingWithLanguage` 插入; 导出 `RuleLoadingWithLanguage` 供 notify 直投复用.
- 语种回落走 `config.Exists` / `Load` (`ConfigPath` / `KANDER_CONFIG`), 与 `kander new` 一致.
- 提交: `dd0273e` feat(launch); `bcf8a07` fix(notify) — 最终交付 `bcf8a075d166afff2015acfd9cc20f40a7a7576e`.
- 验证: `go build ./...`, `go vet ./...`, `go test ./...` 通过.

## SUMMARY

start/resume/takeover 与 notify 直投/恢复的任务文件均在规则加载指令后带固定英文语种硬指令; 卡片 `LANGUAGE` 优先, 缺字段回落 `agent_language`, 配置损坏报错. 文档已更新. 已合入 develop (`bcf8a07`), 验收全部勾选通过.
