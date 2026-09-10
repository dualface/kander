# 审核结论文件, done 门禁与增量复审上下文

- TYPE: Feature
- TASK_GROUP: 20260907-review-archive-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 13:39
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

在归档 (`20260907-review-report-archive-task`) 之上补齐闭环: 定义逐条验证后写入的结论文件 (disposition), 让 `kander check` 与 `kander move <id> done` 按结构校验"每份成功报告都有结论、最后一轮结论允许收尾、跨批次 base 链连续"; 让 `kander review --task` 的增量轮直接从归档取上一轮报告与结论作为复审上下文, 使规则里"复审上下文必须列出上一轮每条 finding 及主 Agent 结论, 不得省略或改写"由构造保证.

今天这些都靠 Agent 自觉: 主 Agent 的逐条结论只以自由文本写进卡片, 工具无法知道某角色是否通过; 增量轮的复审上下文是 Agent 转述后塞进 argv 的字符串; 收尾门禁 (`internal/board/document.go` `validateTarget` 的 `done` 分支) 只看 `RESULT: completed` 与 `SUMMARY`/`report.md`, 与审核结果无关.

## USER_DECISIONS

以下为用户在本次讨论中明确确认的决定:

1. 审核结果应落盘供 Agent 使用; 卡片正文只保留索引.
2. 按提议的三卡拆分建立任务组 `20260907-review-archive-group`, 本卡为第三张; 建卡后不启动.

## EXPECTED_OUTCOME

- 每份 `status=ok` 的归档报告旁有一份同前缀的 `<前缀>.disposition.md`, 格式固定、可机器解析. 单卡由执行 Agent 在逐条验证后手写; 任务组批次由编排 Agent 把各执行 Agent 记录在卡片里的结论原文汇成一份, 写入批内每张卡, 内容相同, 行内标明归属卡 (任务组批次的写入者待用户裁定, 见 DISCUSSION).
- Reviewer prompt 固定 finding ID 格式, 结论文件必须双向覆盖报告中出现的每个 ID; 结论与等级的结构性矛盾 (如 `pass` 却含已确认的 must-fix) 被工具拒绝.
- `kander check` 与 `move done` 据索引、sidecar 与结论文件校验审核闭环: 缺结论、结论不合法、最后一轮不允许收尾、跨批次 base 不接续都阻止进入 `done/`; 没有任何索引的卡不受影响 (审核未触发或 N/A 的单卡).
- `kander review --task <id> ... <reviewed-commit>` 的增量轮自动定位该角色上一轮 (commit 等于 `reviewed-commit`) 的报告与结论文件, 把两者原文并入复审上下文; 缺结论文件时拒绝启动.
- 规则文本把上述行为写入审核、任务组与看板分册, 包括派回消息引用归档路径、未解决项汇总从结论文件汇出.

## ACCEPTANCE_CRITERIA

- [ ] `go build ./... && go test ./... && go vet ./...` 全绿, `gofmt -l .` 无输出, `git diff --check HEAD` 无告警.
- [ ] 结论文件 `<前缀>.disposition.md` 的固定结构: 首行 `# DISPOSITION`; 元数据行 `- REPORT: <报告文件名>`、`- ROLE: <role>`、`- COMMIT: <完整 SHA>`、`- RESULT: pass | fail | accepted-risk | timed-out`、`- PASSED_AT: <完整 SHA | ->` (`RESULT: pass` 时必填, 等于 `COMMIT`, 或仅机械项修复后该角色不重跑而视为通过的新 HEAD; 其他 `RESULT` 为 `-`)、`- VERIFIED_BY: <agent>`、`- VERIFIED_AT: <YYYY-MM-DD HH:MM>`; `## FINDINGS` 节每行 `- <ID> | <tier> | <归属 task-id> | confirmed|fixed|rejected|unverifiable|waived | <依据>`, 其中 `confirmed` 表示已确认尚未修复, `fixed` 表示已确认且已修复、依据列必须是修复 commit 的完整 SHA, tier 列可带后缀 ` [mechanical]` (如 `medium [mechanical]`); 无 finding 时为单行 `- none`; `## NON_BLOCKING` 节每行 `- <ID> | <tier> | <归属 task-id> | fixed|deferred|rejected | <依据>`, 无项时为单行 `- none`. `board` 包提供解析与校验函数, 输出逐条问题.
- [ ] Reviewer prompt (`internal/review/git.go`、`roles.go`) 固定 finding 与 NON-BLOCKING 项的 ID 格式为 `<ROLE>-<三位数字>` (如 `PM-001`、`Hacker-002`), 同一报告内连续编号且不重复; 增量轮沿用上一轮 ID, 新项续编. 现有 prompt 测试更新.
- [ ] 结论校验规则 (被 `check` 与 `move done` 共用): 报告中匹配 `<ROLE>-\d{3}` 的每个 ID 都出现在结论文件中, 结论中的 ID 都出现在报告中; 报告没有任何可解析 ID 时, 结论文件按同一格式自行编号列出报告中的条目 (与规则中主 Agent 为缺等级的结论补等级同理), 此时只做前向覆盖不做反向检查, 无条目时两节为 `- none`; `RESULT: pass` 时 `FINDINGS` 中不得有 tier 为 `blocking|high|medium` 且状态为 `confirmed` 或 `unverifiable` 的行 (`fixed`、`rejected`、`waived` 均允许, 这样仅机械项修复后不重跑而通过、以及增量轮重列上一轮已修复 ID 的两条主路径都能写出合法结论), `waived` 行的依据列非空; `RESULT: pass` 且 `PASSED_AT` 不等于 `COMMIT` 时, `FINDINGS` 中所有 tier 为 must-fix 且状态为 `fixed` 的行必须带 ` [mechanical]` 后缀 (非机械缺陷修复后必须重跑, 不得借此跳过复审); `RESULT: fail` 时 `FINDINGS` 至少含一行 tier 为 must-fix 且状态为 `confirmed`、`fixed` 或 `unverifiable`; `accepted-risk` 与 `timed-out` 只允许 `ROLE` 为 `CSA`/`Hacker` 的结论使用; `REPORT`/`ROLE`/`COMMIT` 必须与同前缀 sidecar 一致; 归属 task-id 必须属于该 sidecar 的 `task_ids`.
- [ ] `kander check` 新增 (状态范围同现有契约检查): 对每个角色, 除按时间戳最新的一行 `status=ok` 之外, 其余 `status=ok` 的索引都必须有合法结论文件, 缺失或不合法逐条报为问题; 最新一行缺结论只作提示不计为问题 (审核进行中的正常状态), 已存在的结论文件不合法仍计为问题. `status=failed` 的行不要求结论.
- [ ] `kander move <id> done` 门禁新增: 卡片存在索引行时, 每个实际运行过的角色 (有 `status=ok` 行的角色) 按时间戳最后一行 `status=ok` 的结论 `RESULT` 必须属于 `pass | accepted-risk | timed-out`; 任何一行 `status=ok` 缺结论即拒绝; 没有索引行的卡门禁行为不变. 错误信息指出具体角色与文件.
- [ ] 跨批次链校验 (`check` 与 `move done` 共用, 仅对 `TASK_GROUP` 非空的卡): 以任务组为单位, 聚合该组全部成员卡 (`trash/` 之外的所有状态, 含已 `done/archived` 的成员) 的索引行与结论文件, 按时间戳排序后取出各不相同的 `base`; 除最早出现的 base (组创建锚点) 外, 每个 base 必须等于组内更早某份 `RESULT: pass` 结论文件的 `PASSED_AT`; 违反时报出断链的 base 与候选行. `move done` 对该卡所属组做同一校验. 单张成员卡通常只经历一个 base, 按卡分组无法覆盖批次之间的链, 因此必须跨卡聚合.
- [ ] `kander review --task <id>... ... <reviewed-commit>` (增量轮): 对每张卡定位 `role` 相同、`commit == reviewed-commit`、`status=ok` 的最新索引行, 读取其报告与结论文件, 把两者原文按固定标题 (`PREVIOUS REPORT`、`PREVIOUS DISPOSITION`) 拼在调用方传入的 review-context 之前写入 prompt; 多张卡时各卡的两份文件内容必须一致, 不一致则退出 2. 找不到该行或缺结论文件时退出 2 且不启动 Reviewer. 带 `--task` 的增量轮不再要求 review-context 非空 (上一轮清单已由归档提供); 不带 `--task` 的增量轮维持今天的行为 (要求非空 review-context).
- [ ] 增量轮 prompt 中 `incrementalScopeRules` 引用上述两个标题, 要求 Reviewer 逐条按 `PREVIOUS DISPOSITION` 核对关闭情况并沿用上一轮 ID; 因此上一轮 ID 会出现在本轮报告里, 本轮结论文件按双向覆盖规则必须再次列出它们 (通常为 `fixed` 或 `rejected`). 测试断言 prompt 含上一轮报告与结论原文.
- [ ] 新增的面向用户字符串在 `en.json`、`zh-CN.json`、`ja.json` 成对存在.
- [ ] 规则同步: `rules/KANDER-REVIEW-RULES.md` "Main Agent Verification Duty" 与 "Incremental Re-review" 写明结论文件是逐条验证的记录载体、增量轮上下文由工具从归档拼装、review-context 参数只承载本轮验证记录与环境缺口; "Conclusions and Failure Handling" 与 "Unresolved Items Summary" 写明未解决项从结论文件汇出并仍写入卡片与交付说明; "Group-Level Review for Task Groups" 写明批次结论文件由编排 Agent 从各执行 Agent 的记录原文汇成并写入批内每张卡, 这是"编排 Agent 不改卡片正文"之外针对 `reviews/` 的明确例外; `rules/KANDER-TASK-GROUP-RULES.md` "Review Batches and Dispatch-Back" 写明执行 Agent 回到 `review/` 前必须把逐条结论写入卡片供编排 Agent 汇总 (派回消息引用归档路径一句已由第二张卡写入, 本卡不重复); `rules/KANDER-KANBAN-RULES.md` 的 `check`、`move done` 门禁说明与 "Execution and Completion" 同步.
- [ ] 本仓库 `AGENTS.md` "本仓库特例" 保持有效: `CSA`/`Hacker` 为 N/A 时不产生索引行, 门禁不要求其结论.
- [ ] 遵守仓库语言约定: 新增 Go 注释与提交备注全英文; 规则文件保持英文; 用户可见字符串走 i18n.

## THREAT_MODEL

- 资产: `done/` 状态的可信度 (下游把 `done` 视为已通过审核); 归档中的结论文件 (后续轮次的 Reviewer 会把它当作事实核对).
- 可信主体: 本机当前用户; 执行 Agent 与编排 Agent 在各自职责内的写入.
- 攻击者能力: 仓库内容诱导 Reviewer 输出伪造 ID 或伪造"已关闭"措辞; Agent 为过门禁写出与报告不符的结论.
- 对策: 结论文件由 Agent 而非 Reviewer 写入, Reviewer 只读; ID 双向覆盖校验与 `pass` 的结构矛盾校验让"漏写"与"明显自相矛盾"机械可查; 结论质量本身仍是主 Agent 的验证义务, 工具只查结构 (与 `CARD_REVIEW:` 记录行的哲学一致), 规则明文禁止为过门禁写不实结论.
- 非目标: 不由工具判断 finding 的真伪; 不防御恶意的本地用户.

## OUT_OF_SCOPE

按既有问题 / 加固 / 共享契约与文档 / 相邻功能四类界定:

- 既有问题 — 报告结构 lint (角色头、`NON-BLOCKING` 节缺失): 排除理由: 本卡只固定 ID 格式并据此做双向覆盖校验; 完整 lint 用户未要求, 若需要另开卡.
- 既有问题 — 阶段二安全 finding 15 分钟超时后继续整合的策略: 排除理由: 本卡只把 `timed-out` 作为可解析的结论值记录下来, 不改变该策略; 是否改为阻塞已在讨论中交给用户决定.
- 加固 — 结论文件的签名或与报告的密码学绑定: 排除理由: 威胁模型不把本地恶意用户列为目标.
- 加固 — 由工具校验修复 commit 或 `PASSED_AT` 是否真的包含在最终 commit 中: 排除理由: 需要在 `check` 中访问 git 对象, 与看板检查不依赖 git 的现状冲突; 属独立设计.
- 共享契约与文档 — 归档格式、索引行、sidecar 字段: 由第二张卡 `20260907-review-report-archive-task` 定义, 本卡只消费, 不改动.
- 相邻功能 — 用命令生成结论文件骨架 (如 `kander review-disposition`): 排除理由: 与卡片正文一样由 Agent 手写并由工具校验结构即可; 骨架命令是便利功能, 用户未要求.
- 相邻功能 — TUI 展示审核轮次与结论: 排除理由: 用户未要求.

## DISCUSSION

```text
PREREQUISITES: 20260907-review-report-archive-task
```

**待用户裁定 (进入 `todo/` 前必须取得确认)**

- 任务组批次结论文件的写入者. 用户确认的方案写的是"执行 Agent 逐条验证后的结论也写进 `reviews/`"; 本卡把任务组批次改为由编排 Agent 汇总写入批内每张卡, 并为此在任务组规则里为 `reviews/` 开一条"编排 Agent 不改卡片正文"之外的明确例外. 改动理由见下方技术结论. 可选方案: (A) 采用本卡现写法, 编排 Agent 把各执行 Agent 记录的结论原文汇成一份写入每张卡, 校验模型最简单; (B) 维持各执行 Agent 只写自己名下 finding 的结论, 编排 Agent 通过派回消息告知全部归属, 每张卡的结论文件把非己名下的行写成 `delegated`, 工具跨卡聚合校验总覆盖, 且编排 Agent 每轮必须派回批内全部成员卡 (包括没有 finding 的). 建卡 Agent 推荐 (A). 用户选 (B) 时需改写 EXPECTED_OUTCOME 第一条、结论校验规则与任务组规则同步条目. 单卡流程不受此项影响.

**关键技术结论 (建卡前已核实)**

- 收尾门禁在 `internal/board/document.go` `validateTarget` 的 `done` 分支, 目前只检查 `RESULT: completed` 与 `SUMMARY`/`report.md`; 新增校验在此处接入, 与 `check` 共用同一校验函数以免两处漂移.
- 增量轮的 prompt 由 `internal/review/git.go` 的 `buildPrompt` 与 `incrementalScopeRules` 生成, 目前把调用方 review-context 原样拼入 (`Additional caller-supplied review context:`); 上一轮报告与结论原文可在同一位置之前拼入, 不改变 Reviewer 隔离参数.
- 规则 "Incremental Re-review" 已要求 review-context 列出上一轮每条 finding 与主 Agent 结论且不得改写; `validate.go:30` 已把"增量轮必须有非空 review-context"做成门禁. 本卡把"非空"升级为"来自归档原件", 是同一条规则的机械化, 不是新流程.
- 规则 "Conclusions and Failure Handling" 定义的有效结论集合: PM/QA 必须无被接受的 must-fix; CSA/Hacker 允许用户接受风险或超时忽略; 不可验证的 must-fix 需用户裁定不处理才可继续 (`waived`). `RESULT` 的四个取值与 `accepted-risk`/`timed-out` 仅限安全角色的约束直接由此而来.
- 规则允许"某角色只剩机械项时修复后不重跑, 在新 HEAD 上视为通过", 此时该角色没有对应新 HEAD 的索引行; `PASSED_AT` 字段就是为了让跨批次链在这种情况下仍可校验 (后一批 base 等于前一批最后通过角色的 `PASSED_AT`).
- 任务组的批次报告由编排 Agent 运行 `kander review --task` 产生并写入批内每张卡; 结论文件由谁写需要明确. 各执行 Agent 只知道派回给自己的 finding, 让每张卡各写一份只覆盖自己 ID 的文件会使"双向覆盖"无法成立, 且没有 finding 的成员卡不会被派回. 因此批次结论文件由编排 Agent 汇成, 这与它今天"把各卡返回的清单聚合进复审上下文"的职责相同; 规则原有的"不得代判、不得改写"约束原样适用于汇总动作.
- 本仓库 `AGENTS.md` 规定 `CSA`/`Hacker` 一律 N/A, 因此在本仓库自身开发中门禁只会遇到 PM/QA 两种角色; 测试仍需覆盖安全角色的取值约束.
- 报告 ID 目前只在 prompt 中要求"role-prefixed stable IDs", 无固定格式 (`git.go:188`), 因此双向覆盖校验必须以本卡固定的格式为前提; 第二张卡交付到本卡交付之间产生的归档可能不含合规 ID, 对这些报告结论文件按格式自行编号并跳过反向检查, 是可接受的过渡代价.

**与前置卡的接口**

- 用户确认的方案写的是"第二、三张依赖第一张"; 本卡实际直接依赖第二张卡 (经它传递依赖第一张), 这是接口上的技术必要性而非用户决策: 依赖第二张卡交付的索引行解析函数、sidecar 字段 (`status`, `role`, `commit`, `task_ids`, `report_file`) 与文件命名; 本卡不修改这些格式, 只新增 `.disposition.md` 与校验.

SELF_REVIEW: 通过, 但有一项待用户裁定 (见 DISCUSSION 开头), 裁定前不进 `todo/`. (1) 目标与成果对齐"审核结果落盘供 Agent 用"的闭环; (2) USER_DECISIONS 只含用户明确确认项, 对第二张卡的依赖与任务组写入者变更分别写为技术必要性与待裁定项; (3) 边界: 报告 lint、超时策略、git 祖先校验、骨架命令、TUI 展示排除并给理由, 门禁与增量上下文所需的解析、校验、prompt、规则同步在范围内; (4) 验收可判定, 结论文件结构、pass/fail 结构约束、双向覆盖、done 门禁、跨组链、增量自动上下文均有可测条目. 建卡时自行修正: 报告无可解析 ID 的处理, 带 `--task` 的增量轮不再要求非空 review-context. 首轮卡审后修正: 增加 `fixed` 状态使机械项路径与增量轮可写出合法 `pass`, 跨批次链改为按任务组聚合, USER_DECISIONS 去掉依赖措辞, 写入者变更列为待裁定, check 对最新一行缺结论只提示, 删除与卡 2 重复的规则编辑; 第二轮建议项已补: `PASSED_AT` 不等于 `COMMIT` 时 `fixed` 行必须为机械项, `fail` 至少含一条 must-fix 行.

CARD_REVIEW: PASS (契约结构合格; 写入者方案 A/B 待用户裁定, 裁定前留在 `backlog/`) — 同一独立子 Agent (Claude, general-purpose, 不共享建卡会话上下文) 两轮. 首轮 FAIL: `pass` 的结构校验与机械项路径、增量轮双向覆盖互斥; 跨批次链按单卡分组在任务组里恒为真; USER_DECISIONS 写入未确认的依赖; 任务组写入者偏离已确认方案且新开规则例外. 全部修正后第二轮 PASS, 卡审确认待裁定块如实列出该项且未替用户选择; 两条可选建议 (机械项标签校验、`fail` 最小结构) 已采纳.
