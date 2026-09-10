# Add a review argv template and result parsing to agent definitions, opening reviewer to custom agents

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260908-agent-definition-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 13:17
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:w2T:t1A:w2T:p1A
- STARTED_AT: 2026-09-09 10:55
- FINISHED_AT: 2026-09-09 23:22
- TASK_BRANCH: agent-review-template
- RESULT: completed

- DISPATCH_ID: codex-agentdef-wrap-review-20260909

- EXECUTION_EPOCH: 12

## GOAL

让 agent 定义文件能声明审核 (reviewer) 调用方式, 使自定义 agent 也可以担任审核角色. 目前 `internal/review/args.go` 的 `reviewerArguments` 按四个内置名硬编码只读沙箱参数与环境变量, `internal/review/settings.go` 的 `agentSettingsFor` 按名持有审核 home 来源, 结果文件名, 只读检查提示文本 (`inspection`) 与 "是否派生辅助进程" (`helpers`), `internal/config` 的 `ReviewAgents` 限制 `reviewers.<role>` 只能取四个内置名. 本卡在定义文件中增加 `args.review` argv 模板, `review.env`, `review.cwd` 与 `review.output` 结果来源声明, 内置四个 agent 的审核参数迁为定义文件内容, `reviewers.<role>` 接受任何定义了审核模板的 agent.

## USER_DECISIONS

- 与 `20260908-agent-definition-embed-task` 相同的整体决策: 声明式定义为主, 内置与自定义共用一套机制, 不做 shell 插值.
- 公共输出解析结构拆出为独立任务卡单独交付, 本卡改为消费方 (2026-09-09 确认).
- 2026-09-09 授权启动本组: 按依赖启动执行, 任务分支交付组分支 group/20260908-agent-definition-group, 组完成后快进合入 develop 并 wrap-up.

## EXPECTED_OUTCOME

- 定义文件新增字段: `args.review` (argv 模板), `review.env` (环境变量名到模板值的映射), `review.cwd` (取值 `root` 或 `runtime`), `review.home_env` (审核 home 取自哪个环境变量, 缺省时回落 HOME 下的默认目录, 与现有 `getenvDefault` 语义一致), `review.home_policy` (取 `required` | `optional`: `required` 要求 home 目录存在且可读写, 缺失时拒绝, 对应现有 Claude/Codex/Grok 行为; `optional` 允许 home 不存在并继续, 对应 `internal/review/validate.go:107` 现在对 Cursor 的例外), `review.output_name` (结果文件名), `review.inspection` (注入提示词的只读检查规则文本), `review.spawns_helpers` (布尔, 影响进程树收集, 对应现有 cursor 的 `helpers`), `review.snapshot_spec` (布尔, 是否把任务 spec 快照进 runtime, 对应现有按 agent 名的 `execute.go:116` 分支), `review.path` (可选审核可执行名), `review.stdin` 与 `review.prompt_files` (提示词投递与渲染, 见下), 以及 `review.output` (公共输出解析结构, 见 `20260909-process-output-parser-task`) 与 `review.output.success` (声明式成功条件, 见下).
- 输出解析复用 `20260909-process-output-parser-task` 交付的公共结构 (`internal/process`), **其权威描述在该卡**, 本卡只引用不复述字段集合. 本卡使用的 `source` 子集为 `stdout` | `file`, 经该卡提供的子集入口调用. `GIT_OPTIONAL_LOCKS=0` 是 kander 侧对所有 reviewer 统一设置的基础环境, 不进定义文件.
- 成功条件与文本提取分离: `review.output.success` 使用公共结构的行条件词汇, 语义 (含 `format: ndjson` 下"存在某一行同时满足全部条件"、`success` 不受 `select` 限制、文档不可解析为 JSON 时判失败) 由 `20260909-process-output-parser-task` 定义, 本卡不复述. 本卡负责的是: 四个内置 agent 各自的成功条件用该词汇表达后, 与现有 Grok `stopReason == end_turn` (`internal/review/args.go:182`) 以及 Claude/Cursor 的 `type`/`subtype`/`is_error` 检查 (`internal/review/args.go:189`) 判定结果逐项一致; 未声明成功条件时以退出码为零且提取后文本非空兜底.
- 审核提示词的**投递与渲染**同样声明化. 现在 kander 固定把一行指令写入 stdin 管道交给 reviewer (`internal/review/execute.go` 的 `stdinFile` -> `promptStream`), Codex 用 `-` 读 stdin, Grok 用 `--prompt-file`; 这套隐含假定了"只读约束能用 argv/env 表达, 且指令可以走 stdin". 存在把只读白名单放在**提示词文件自身的格式**里 (例如一份带 YAML frontmatter 与工具白名单的 Markdown) 且不接受 stdin 的 CLI, 这类 agent 用现有字段表达不了, 只能回 Go 分支. 因此增加三项:
  - `review.stdin` (取 `instruction` | `none`, 缺省 `instruction`): `instruction` 保持现有行为; `none` 时不创建 stdin 管道, 指令必须由 argv 携带. 两个错误方向都在校验期拒绝: `stdin: none` 且 argv 不含 `{instruction}` (reviewer 拿不到任何指令, 会静默跑空), 以及 `stdin: instruction` 且 argv 含 `{instruction}` (同一行指令送两遍).
  - argv 新占位符 `{instruction}`: 展开为 kander 今天写进 stdin 的同一行指令 (`process.TaskFileInstruction` 的产物), 供 `review.stdin: none` 的 agent 内联进 argv.
  - `review.prompt_files` (可选数组): 每项为 `{"name": "<引用名>", "path": "<runtime 相对路径>", "template": "<文本模板>"}`. kander 在启动 reviewer 前按序渲染进本轮 runtime, 权限与隔离沿用现有 runtime 文件 (POSIX 0600, Windows 受保护 DACL); `path` 必须是规范相对路径, 拒绝穿越、符号链接与 reparse point. 模板内可用 `{inspection}` (该 agent 的 `review.inspection` 文本), `{prompt}` (kander 构建的完整审核提示词正文, 即今天写进 `prompt.txt` 的内容), `{role}`, `{report_language}`, `{root}`, `{runtime}`, `{output}`. 占位符与 `{{` / `}}` 转义规则沿用 `20260909-process-output-parser-task` 的权威描述, 本卡只提供上述白名单: 模板正文是 Markdown 加 YAML frontmatter, 字面花括号 (代码块里的 JSON 片段、`{}` 示例) 极常见, 必须能转义而不被当成未知占位符; 换行是模板的正常内容, 不属于被拒的控制字符. argv 用新占位符 `{prompt_file:<name>}` 引用其渲染后的绝对路径, 引用未声明的名字在校验期拒绝.
  - 四个内置 agent 都不声明 `review.prompt_files`, `review.stdin` 全部为 `instruction`, 其 stdin 行为、`prompt.txt` 内容与 argv 逐项不变.
- 审核模板可用占位符: `{model}`, `{effort}`, `{root}` (被审核 worktree), `{runtime}` (审核私有运行目录), `{home}` (审核私有 home), `{output}` (结果文件绝对路径), `{prompt_file}` (kander 自建的审核提示词文件绝对路径), `{prompt_file:<name>}` (声明的额外提示词文件), `{instruction}` (那一行审核指令). 占位符替换规则与 start/resume 模板一致: 逐元素替换, 空值丢弃该元素及紧邻的独立 flag, 未知占位符与控制字符拒绝.
- 内置四个 agent 的审核 argv, 环境变量, home 来源, 结果文件名, inspection 文本, helpers 标志与结果解析方式全部由其定义文件表达; `reviewerArguments`, `parseReviewOutput` 与 `agentSettingsFor` 不再按 agent 名分支.
- `reviewers.<role>` 与 `kander review [agent]` 接受任何定义了 `args.review` 的 agent; 未定义审核模板的 agent 被选为 reviewer 时, config 校验, doctor, 选项面板与 `kander review` 都给出明确拒绝信息.
- 审核可执行名优先级: 内置 agent 保持 `<NAME>_REVIEW_BIN` 环境变量 > 嵌入定义的原始 `path`, 用户在 `agents.<name>.path` 对内置 agent 的覆盖只影响执行, 不进入审核 (保持 `internal/review/settings.go:197` 现在 "执行配置不覆盖审核" 的隔离); 自定义 agent 取定义文件的可选 `review.path`, 缺省回落其自身 `path`. 审核私有目录, 0600/0700 权限, Windows DACL 与进程树收集等隔离行为不变.
- `internal/review/execute.go:116` 与 `:183` (spec 快照与 stdout 路由), `internal/review/validate.go:107` (审核 home 缺失) 中按 agent 名的分支同样改为读定义字段. spec 快照用**独立布尔 `review.snapshot_spec`** 而不是挂在 `review.cwd` 上: 快照的理由是"reviewer 看不到卡片原路径", 与工作目录不是同一件事, 存在 cwd 为被审 worktree **且**需要快照的组合, 挂在 `review.cwd` 上表达不了, 落地时又得改 Go. stdout 路由读 `review.output.source`, home 缺失读 `review.home_env` 与 `review.home_policy`, 本卡结束时 review 包内不再有按内置名的业务分支.

## ACCEPTANCE_CRITERIA

- [ ] 定义文件 schema 含 `args.review`, `review.env`, `review.cwd`, `review.home_env`, `review.output_name`, `review.inspection`, `review.spawns_helpers`, `review.snapshot_spec`, `review.path`, `review.output`, `review.home_policy`, `review.stdin`, `review.prompt_files` 字段. `review.output` 自身的校验由 `20260909-process-output-parser-task` 提供, 本卡只负责经其审核子集入口调用并断言 `stderr` 被拒; 本卡自有字段 (不含公共结构本身) 的校验覆盖: `home_policy` 与 `review.cwd` 取值集合, 两个布尔, `review.stdin` 取值集合与其与 `{instruction}` 的两个错误组合, `review.prompt_files` 的路径与模板规则, 占位符白名单, 空元素拒绝; 非法值的错误信息含 agent 名与字段名.
- [ ] 成功条件先于文本提取: 表驱动测试对四个内置 agent 各构造一份 "有报告文本但状态失败" 的输出 (Grok `stopReason` 非 `end_turn`, Claude/Cursor `is_error=true` 或 `subtype` 非 success, Codex 非零退出), 结果均被拒绝且错误与改动前一致; 正常输出仍通过.
- [ ] 本卡不在 `internal/review` 或别处新写解析器: `git grep` 确认审核侧的输出解析全部经 `20260909-process-output-parser-task` 交付的 `internal/process` 入口, 且调用的是审核子集入口 (声明 `stderr` 时被拒, 有用例).
- [ ] 四个内置 agent 的定义全部保持 `format: json` (缺省), 不声明 `select`/`join`; 表驱动测试对每个内置 agent 构造"有报告文本但状态失败"与正常两组输出, 判定结果与改动前逐项一致.
- [ ] home 策略测试: 内置 cursor 定义为 `optional`, 缺 home 时审核成功; 内置 claude 定义为 `required`, 缺 home 时拒绝且错误与改动前一致; 自定义 reviewer 两种策略各一用例.
- [ ] `internal/review/args.go` 的 `reviewerArguments`, `parseReviewOutput`, `settings.go` 的 `agentSettingsFor`, `execute.go` 的 spec 快照与 stdout 路由分支 (`:116`, `:183`) 以及 `validate.go` 的 home 缺失分支 (`:107`) 不含按 agent 名的 `switch`/`if`; 表驱动测试固定四个内置 agent 生成的 argv, 定义贡献的 env 增量 (`CODEX_HOME`, `CLAUDE_CONFIG_DIR`, `CURSOR_CONFIG_DIR`, `CURSOR_DATA_DIR`, `GROK_HOME`), cwd, home 来源, 结果文件名, inspection 文本, helpers 标志与 `review.snapshot_spec`, 期望值取自改动前实现, 全部一致.
- [ ] `internal/config` 中 `ReviewAgents` 删除, 其全部调用点 (含 `config.go` 的 reviewers 校验与 review 模型段校验, `format.go` 的 `ReviewModelLines`) 改为 "定义了 `args.review` 的 agent"; `reviewers.<role>` 校验改为 "该 agent 存在且定义了 `args.review`"; 一个只定义 start 模板的自定义 agent 被写入 `reviewers.PM` 时, `kander config --json` 报错并指出缺少审核模板, doctor 不擅自改写该值.
- [ ] 一个测试用自定义 agent (以仓库内的假可执行脚本或 Go 测试二进制充当) 通过 `args.review` + `review.output` 为 `{source: file, parse: raw}` 成功完成 `kander review` 的执行与结果解析; 另三个用例分别使用 `{source: stdout, parse: json_field:result}`, `{source: stdout, parse: regex:...}` 与 `{source: stdout, format: ndjson, select: [{json_field: role, equals: assistant}], parse: json_field:content, join: "\n"}` (最后一个的假可执行输出中混有**带 `content` 的**工具结果行与元信息行) 同样通过; 自定义 reviewer 的 inspection 文本出现在注入提示词中.
- [ ] `review.stdin`, `{instruction}`, `review.prompt_files` 与 `{prompt_file:<name>}` 有校验与单元测试: `path` 规范化与穿越/符号链接/reparse point 拒绝, 模板未知占位符拒绝, argv 引用未声明的名字拒绝, `review.stdin: none` 时不创建 stdin 管道. 端到端: 一个测试用自定义 reviewer 声明 `review.stdin: none` 与一份含 `{inspection}` 和 `{prompt}` 的 `review.prompt_files` 模板, argv 用 `{instruction}` 与 `{prompt_file:<name>}`, `kander review` 完整跑通, 渲染出的文件内容与模板展开逐字节一致且权限为 0600.
- [ ] 四个内置 agent 均不声明 `review.prompt_files` 且 `review.stdin` 为 `instruction`; 表驱动测试固定它们的 stdin 内容与 `prompt.txt` 内容与改动前逐字节一致.
- [ ] 现有 review 包全部测试 (codex, claude, cursor, grok 各自的适配器测试, 归档, 处置, Windows 相关) 保持通过, 未删改期望值.
- [ ] 可执行名优先级测试: 设置 `agents.codex.path` 为包装器后, 执行走包装器而审核仍走 `CODEX_REVIEW_BIN` 或嵌入定义的原始 `path`; 自定义 agent 未声明 `review.path` 时审核回落其 `path`.
- [ ] 选项面板 "审核与模型" 分区的 reviewer 候选列表来自 "定义了审核模板的 agent", 现有面板测试通过.
- [ ] `go build ./...`, `go vet ./...`, `go test ./...`, `GOOS=windows go build ./...` 通过.
- [ ] `docs/custom-agents.md` "面板与审核边界" 一节改写: 说明审核模板字段, 占位符, 公共结构的使用方式 (只链接 `20260909-process-output-parser-task` 交付的 `docs/output-parsing.md`, 不复述取值集合与成功条件语义), 可执行名优先级, 以及 "只读性由定义作者负责, kander 仍做结果校验与目录隔离".
- [ ] 发布规则同步: `rules/KANDER-REVIEW-RULES.md` "Reviewer Selection" 的 "Four reviewers are supported" 与隔离参数表改为 "内置四个 reviewer 加任何定义了审核模板的 agent, 内置隔离参数见定义文件, 自定义 reviewer 的只读性由定义作者负责"; `rules/KANDER-KANBAN-RULES.md` "Configured Execution Agents" 的 "Custom agents are execution-only; reviewer isolation and `*_REVIEW_BIN` remain independent" 改为与本卡一致; `rules/KANDER-BASE-RULES.md` 单条 review 段中按四个 reviewer 描述只读隔离的句子同步; `rules/KANDER-KANBAN-RULES.md` "Command Contract" 中 "可选 reviewer 参数只接受四个 reviewer 名" 一句改为 "已配置且定义了审核模板的 agent"; `rules/KANDER-REVIEW-RULES.md:50` "the reviewer receives only a short instruction with the path" 与 `:51` "Grok keeps `--prompt-file`" 按 `review.stdin` / `{instruction}` / `review.prompt_files` 改写. 以上每条逐条给出改后原文, 与本卡交付的行为一致; 本卡只有这一条规则同步验收, 不另设第二条.

## THREAT_MODEL

审核阶段的资产是被审核 worktree 与审核私有目录. 内置 agent 的只读沙箱参数迁入定义文件后语义不变. 自定义 reviewer 的只读性无法由 kander 保证, 由定义作者负责; kander 保留的边界是: 审核私有目录权限与隔离不变, 结果文件由 kander 创建并校验, 定义文件由本机用户声明, 不构成新的用户间权限边界. 不新增跨用户攻击面.

## OUT_OF_SCOPE

- 已有问题: 审核私有目录在 Windows 上的原生验证缺口为既有事项, 本卡只要求交叉编译; 排除.
- 并发与跨平台加固: 审核进程启动与监控的平台差异代码 (`launch_unix.go`, `launch_windows.go`, `monitor_*.go`) 不改; 排除.
- 共享契约与文档: `docs/custom-agents.md` 审核一节纳入; `rules/KANDER-REVIEW-RULES.md` "Reviewer Selection", `rules/KANDER-KANBAN-RULES.md` "Configured Execution Agents" 与 `rules/KANDER-BASE-RULES.md` 中限定四个 reviewer 的公开边界纳入, 因为开放 reviewer 名单直接改变这些规则的承诺; `rules/KANDER-REVIEW-RULES.md:50` "the reviewer receives only a short instruction with the path" 与 `:51` "Grok keeps `--prompt-file`" 一并纳入, 因为 `review.stdin: none` + `{instruction}` + `review.prompt_files` 直接改变这两句的承诺; `docs/custom-agents.md` 与 `20260908-agent-definition-embed-task` 同为写方, 两卡不可并行分派; 其余规则不改; 部分纳入.
- 相邻功能与后续阶段: 退出命令与会话钩子属 `20260908-agent-exit-session-hook-task`; 审核角色 (PM/QA/CSA/Hacker) 数据化属后续阶段; 新增第五个内置 agent (kimi) 及其审核定义属该组完成后的独立任务, 本卡只交付 `format: ndjson` 机制, 不引入任何 kimi 相关内容; 排除.

## DISCUSSION

```text
PREREQUISITES: 20260908-agent-definition-embed-task,20260909-process-output-parser-task
```

- 设计结论: `review.output` 只用共用的三种解析原语, 不加脚本; 需要更复杂解析的 agent 应在自身侧输出可直接读取的文件.
- 记录 (2026-09-09): 公共输出解析结构原本由本卡交付, 已按用户决定拆出为 `20260909-process-output-parser-task`, **"权威描述"身份随之搬到该卡**; 本卡与 `20260908-agent-definition-embed-task`, `20260908-terminal-definition-format-task` 一律只引用不复述. 拆分理由记在组内第一张卡的 DISCUSSION.
- 设计结论 (本轮用户确认, 2026-09-09): `format` / `select` / `join` 不违反 "输出解析只有三种原语" 这条用户决策 —— `parse` 的取值集合没有变, 三种原语在每个片段上原样套用; 新增的三项描述的是切分、筛选与合并, 属于流形态而不是解析能力. 之所以必须放进公共结构而不是留给调用方: 已知存在这样一类 CLI —— 把整段报告拆成逐行 assistant 消息, 中间混杂工具调用行与元信息行, 没有 result 信封, 且**工具结果行与元信息行带着与报告行同名的字段** (`content`). 对这种输出, 三种原语中任何一种单用都取不出干净报告: 不筛选会把工具结果全文与会话恢复提示拼进报告, 无分隔符直连会破坏 Markdown 结构. 因此 `select` 与 `join` 是让 `format` 真正可用的必要条件, 缺一个 `format` 就是死字段. 本卡只交付机制并用测试用自定义 agent 验证, 不引入该 agent.
- 设计结论 (本轮用户确认, 2026-09-09): 审核侧的提示词投递与执行侧的 `prompt_delivery` (`20260908-agent-definition-embed-task`) 是同一类问题的两面 —— 都是 Go 侧的隐含投递假定挡住了定义文件的表达力. 执行侧的假定是"提示词进 argv 末尾", 审核侧的假定是"指令走 stdin, 只读约束靠 argv/env". 两者都在本轮显式化, 目的是让新增一个内置 agent 不再需要改 Go.
- 记录: 本轮变更的触发场景与命令行实测记录在 `tmp/kimi-cli-integration-notes.md`, 但该文件未纳入版本控制, 只作线索; 本卡所依赖的事实已全部内联在上面两条设计结论与 EXPECTED_OUTCOME 中, 不依赖该文件存在.
- 设计结论: `settings.go` 现有注释 "Config.Agents applies only to task execution and never overrides this choice" 描述的约束被本卡有意修改为 "内置 agent 不变, 自定义 agent 的 `review.path` 缺省回落 `path`"; 执行时同步改写该注释.
- 设计结论: 内置 agent 保留 `*_REVIEW_BIN` 优先级是为兼容现有部署; 自定义 agent 的 `review.path` 缺省回落 `path`, 是因为自定义 agent 本来就由用户声明可执行路径, 不存在 "包装器被意外用于审核" 的顾虑.

- SELF_REVIEW: 通过. 审核模板字段, 占位符, 三种输出原语与 reviewer 校验均可判定; 威胁模型明确 "只读性由定义作者负责" 与 kander 保留边界; 可执行名优先级作为设计结论记入 DISCUSSION 而非用户决策. 依赖 A1 的定义加载器, 已写入 PREREQUISITES.
- CARD_REVIEW (第一轮, 建卡时): 需修正后通过 (独立 agent). 发现: (1) review/settings.go 的 agentSettingsFor 按名持有 home 来源, 结果文件名, inspection 文本与 helpers 标志, 原字段集无法表达, 已补 review.home_env, output_name, inspection, spawns_helpers 并把 settings.go 纳入验收; (2) 与终端定义卡各定义了不同的三种原语, 已统一为 internal/process 共用的 source/parse 结构; (3) GIT_OPTIONAL_LOCKS 是基础环境, 已注明不进定义; (4) settings.go 注释与 format.go ReviewModelLines 调用点已补入.
- 设计结论: 不引入脚本能力是 "三种原语 + 不做 shell 插值" 边界的推论 (与 A1 的记录一致), 不是单独的用户决策; 成功条件用声明式相等/缺席判断, 同样不引入脚本.
- 计划: 本卡改为 large (审核执行配置、提示词投递与渲染、候选列表与兼容适配同时引入; 公共解析器已拆给 `20260909-process-output-parser-task`), 执行者启动时先写 `plan.md`, 建议阶段: (1) 定义字段与校验 (含 `review.stdin` / `review.prompt_files` / `review.snapshot_spec` 与新占位符, 输出解析经 `20260909-process-output-parser-task` 的审核子集入口); (2) args/settings/execute/validate 迁移与表驱动对比; (3) config/menu/doctor 的 reviewer 候选; (4) 自定义 reviewer 端到端; (5) 规则与文档.
- CARD_REVIEW: 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 对 backlog 全部 12 张卡的独立审核). 发现: (1) blocking: 三种提取原语承载不了现有成功状态校验 (`args.go:182`, `:189`), 已增加 `review.output.success` 声明式条件与失败拒绝验收; (2) 内置 reviewer 可执行名与用户 path 覆盖的关系不清, 已明确审核用嵌入原始 path 并加覆盖用例; (3) "发布规则不改" 事实错误, 已纳入 REVIEW/KANBAN/BASE 三处; (4) "不引入脚本能力" 误记为用户决策, 已移入 DISCUSSION; (5) `review/execute.go`, `validate.go` 的分支责任归入本卡; (6) 改 large. 六项已修正.
- CARD_REVIEW (第三轮): 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 第二轮复审). 发现: (1) `review.home_env` 表达不了 Cursor 允许 home 缺失的例外 (`validate.go:107`), 已增加 `review.home_policy` 与两种策略验收; (2) 漏 KANBAN 命令契约里 "只接受四个 reviewer 名" 一句, 已纳入; (3) 与终端格式卡的公共解析器 `source` 取值分叉, 已统一为公共全集 `stdout|stderr|file`, 各调用方限制子集, 由本卡交付. 三项已修正.

- SELF_REVIEW (2026-09-09, 合同变更后复核): 通过. 本轮新增 `review.stdin` / `{instruction}` / `review.prompt_files` / `{prompt_file:<name>}` / `review.snapshot_spec`, 并把公共解析结构交由 `20260909-process-output-parser-task` 交付, 本卡收为消费方; USER_DECISIONS 只增记了用户当场确认的拆卡一条; 边界与该卡无重叠无缝隙 (该卡管结构与语义, 本卡管四个内置 agent 用该词汇表达后判定结果与改动前逐项一致); 规则同步收敛为唯一一条验收并含 REVIEW `:50` `:51`. 无待决的用户问题.
- CARD_REVIEW (2026-09-09, 本轮四次往返): 需修正后通过 (独立子代理, 新会话, 只读本组卡片与用户原始需求, 2026-09-09 共四轮往返). 发现并已修正: (1) 原 `format: ndjson` 用 `json_field:content` 取不出干净报告 —— 工具结果行与 `session.resume_hint` 元信息行同样带 `content`, 会被拼进报告, 已增加按行筛选并要求验收输入必须包含这两类带 `content` 的行; (2) "拼接不插入分隔符" 是无依据的硬编码, 会把完整消息粘成一行破坏 Markdown 结构, 已改为显式 `join` 缺省 `"\n"`; (3) ndjson 成功条件原按 "每条条件任意一行满足" 判定, 会让失败行与成功行凑成通过, 已改为 "存在某一行同时满足全部条件"; (4) `ndjson` × `raw` 组合语义未定义, 已由校验拒绝; (5) 审核侧提示词投递与渲染无法表达 "只读白名单在提示词文件格式里" 的 agent, 已增加 `review.stdin` / `{instruction}` / `review.prompt_files`; (6) 模板缺花括号转义规则, 已引用权威描述; (7) `review.stdin` 与 `{instruction}` 的两个错误组合未堵, 已在校验期拒绝; (8) 规则同步漏 `KANDER-REVIEW-RULES.md:50` `:51`, 已纳入并与旧条合并为唯一一条; (9) spec 快照挂在 `review.cwd` 上表达不了 "cwd 为被审 worktree 且需要快照" 的组合, 已拆出独立布尔 `review.snapshot_spec`; (10) 文档条款与两处字段名枚举仍在复述公共结构, 已改为只链接 `docs/output-parsing.md`. 末轮结论为 "需修正后通过", 其列出的一条措辞级修正 (large 理由里仍写着已拆走的公共解析器) 已修正后发布, 未再复审. (1)(2)(3) 三条是本卡作者上一轮写入的错误规格, 由本次独立审核发现.
## IMPLEMENTATION

- 2026-09-09 最终 wrap-up（dispatch `codex-agentdef-wrap-review-20260909`，epoch 12）：原子接受 replayed=false。读取绑定 integration.json、revision 3 封存计划及两份 closed.json，核实两批 PM/QA PASS、CSA/Hacker N/A。独立验证任务 `6b7c132db76cc00a1245bc45fd992afe3109dcc6` 属于已审源 `64d0c2cf5fa2e8c8930f56967dced09a2187478d`，已审源属于本地和真实远端 develop `a8b5afac5d79a09399ab12b097691d1f2212c1dd`；合并双亲与文件树均与派发一致。核验主控最终日志 SHA-256 与树绑定：21 包，1682 pass / 0 fail / 1 skip；build/vet/Windows 交叉编译退出 0，本执行体未重复测试。任务工作树跟踪/未跟踪文件干净，仅两个被忽略的历史构建二进制先按 SHA-256 验证复制到 `/tmp/kander-review-wrapup-builds-_1wmeu85/` 后移出；已删除本任务工作树、本地分支及期望 SHA 保护的远端分支。未清理组/集成工作树或关闭会话。完整核验及构建保留路径见 `verification/wrap-up-epoch12.json`。合并后追加审核按用户明确指示跳过，不冒记为新 PASS。

- 2026-09-09 组分支同步轮（dispatch `codex-agentdef-review-sync-e756-20260909`，epoch 11）：fetch 后确认本地和远端组 HEAD 均为 `e756f7d672bb4cc2a488a476485f6821e7b43d2e`，前置 A1 的 b216ca25 与 parser 的 e756f7d 已有 completed 回执且实际包含在组分支。核实旧 `agent-def-batch-one` closed.json 闭合于 d97964c06adb942b94b25c7e5c5bfb04795172e4，PM/QA PASS，CSA/Hacker N/A。在本任务工作树执行 `git rebase origin/group/20260908-agent-definition-group` 无冲突，`git range-diff d97964c..fa8da8a e756f7d..HEAD` 显示 `fa8da8a = 6b7c132`；相对组基线仅 definition_unix_test.go 0 增/1 删（末尾空行），文件现 352 行，未新增代码、符号或测试。最终 `6b7c132db76cc00a1245bc45fd992afe3109dcc6`。该提交上 `go test -json -p 2 ./... -count=1`：21 包通过；1480 pass / 0 fail / 1 skip（含子测试）；跳过 TestWindowsConsoleLauncher。`go vet ./...`、`go build ./...`、`GOOS=windows go build ./...` 退出 0；`git diff --check 8abdfe2e6430a00e19383f84f23088a8ff743be0 HEAD` 退出 0。使用期望旧远端 SHA fa8da8a 的显式 force-with-lease 推送，远端任务 SHA 已核实。组分支未改；历史失败/跳过和四项非门禁处置保留；新交付等待主控接收。

- 2026-09-09 QA r8 非门禁处置同步轮（dispatch `codex-agentdef-qa-r8-review-20260909`，epoch 10）：完整读取 r8 report/assignment/sidecar，独立核对组目标 `d97964c06adb942b94b25c7e5c5bfb04795172e4` 对应实现与修复区间。现象未变，Codex 新增 deferred 2 / fixed 0 / rejected 0：`reviews/qa-agent-def-b1-r8/dispositions/qa-n1-codex-deferred-r8.json`、`reviews/qa-agent-def-b1-r8/dispositions/qa-n2-codex-deferred-r8.json`。两项均使用 r8 自己的 run/report_hash/original，报告 lineage 指向 r6；旧作者 r6 原件保留。核验为只读源码与 Git 对照，未新跑测试或声称动态复现；无代码/rebase/分支变化。任务交付仍为 `fa8da8a9923749a80d44c7fa8038e97a0f2e620a`。QA r8 已成功执行且报告无 gate findings；r7 失败保留为历史，不再作为当前 QA 尚未执行的结论，批次闭合仍待编排器。

- 2026-09-09 作者处置 Git 映射同步轮（dispatch `codex-agentdef-dispositions-review-20260909`，epoch 9）：独立核验旧 PM-02/PM-05 fix_commit 均非组 HEAD `d97964c06adb942b94b25c7e5c5bfb04795172e4` 祖先。追加 Codex 本人记录 fixed 2：`reviews/pm-agent-def-b1-r4/dispositions/pm-02-codex-git-map-e9.json` 绑定 `ee8fab70b817faca93b5b3b0d900bb9ba93da2be`；`reviews/pm-agent-def-b1-r4/dispositions/pm-05-codex-git-map-e9.json` 绑定 `8f8f0cd0627fca918e7383ea4c3ebd6962864652`。各自 previous_record_id 指向原记录，原文/report_hash/status/mechanical 与旧作者原件保留。祖先验证均退出 0；PM-05 新旧 patch-id 相同，PM-02 按组内累计修改与资格校验路径核实。任务 HEAD `fa8da8a9923749a80d44c7fa8038e97a0f2e620a` 上 `go test -json ./internal/config ./internal/menu ./internal/i18n ./internal/review -count=1` 为 350 pass / 0 fail / 0 skip（含子测试），4 包通过；这些包相对组 HEAD 除末尾空行外相同。没有代码、分支或提交变更；原交付 SHA 保留，组级批次闭合仍待编排器。

- 2026-09-09 Codex 接管同步轮（dispatch `edaf20040052b5574ad4ffcd1f85e2d9`，epoch 8）：accepted 回执 replayed=false。实际工作树 `/home/dualf/works/kander/worktrees/agent-review-template`。fetch 后核实本卡原交付 `b653e810e748783979daba1b6a21f200c143722f` 已被本地与远端组分支 `d97964c06adb942b94b25c7e5c5bfb04795172e4` 包含；两张前置卡的既有交付也已包含。前置卡本轮处于 working 接管核验状态，本轮不据此释放新依赖。任务分支 rebase 到 `d97964c06adb942b94b25c7e5c5bfb04795172e4` 无冲突、未重写既有提交；仅删除 `internal/review/definition_unix_test.go` 的末尾多余空行，交付 `fa8da8a9923749a80d44c7fa8038e97a0f2e620a`，已普通 push，远端任务分支 SHA 一致。组分支未修改。完整核验与五项未决见 `report.md`。

- 2026-09-09 首轮: 交付 `e1c01277075599cb0395bf62f16855cebf04b060`，基于当时组分支 `258c8eb3a49784d8a96bc2d4a44c325fb97d2425`。
- 2026-09-09 修复轮 (dispatch `fc1901cc70435178a70c6890954784bf` epoch 1): `64630c9` / `bfadd17`；fixed 2 / deferred 3。记录见 `reviews/pm-agent-def-b1-r4/dispositions/`。
- 2026-09-09 同步轮 (dispatch `baa13e1677f3b357eaab6428b3098d09` epoch 2): 无代码改动；QA r6 非门禁 deferred 2。记录见 `reviews/qa-agent-def-b1-r6/dispositions/`。
- 2026-09-09 同步轮 (dispatch `88623784201a3ee0a0c9e844af04e103` epoch 3): rebase 到组分支 `fbef4fa0100ea0417508a2e4f7c4c58ebaa34093`，交付 `8f8f0cd0627fca918e7383ea4c3ebd6962864652`。
- 2026-09-09 修复轮 (dispatch `187bcf576d52bf3e9fb689554f2211d7` epoch 6): grok 接管。PM-10 fixed `b653e810e748783979daba1b6a21f200c143722f`；PM-07 deferred；PM-08 deferred。记录 `reviews/pm-agent-def-b1-r6/dispositions/pm-10-fixed-r6.json`、`reviews/pm-agent-def-b1-r6/dispositions/pm-07-deferred-r6.json`、`reviews/pm-agent-def-b1-r6/dispositions/pm-08-deferred-r6.json`。基于组分支 `8f8f0cd0627fca918e7383ea4c3ebd6962864652`。`git merge-base --is-ancestor 8f8f0cd0627fca918e7383ea4c3ebd6962864652 HEAD` 成立；`git diff --check` 干净；`go test ./internal/config ./internal/review ./internal/menu ./internal/i18n ./internal/process -count=1` 403 pass / 0 fail / 0 skip；`go vet ./...`、`go build ./cmd/kander`、`GOOS=windows go build ./cmd/kander` 通过。已 push。未改组分支。

## TAKEOVER_AUDIT

- 2026-09-09：纠正上一轮 SUMMARY 的两项时效性结论。`git fetch origin` 后，`git merge-base --is-ancestor b653e810e748783979daba1b6a21f200c143722f origin/group/20260908-agent-definition-group` 返回 0，说明原交付已进入组分支；`reviews/pm-agent-def-b1-r6/report.md` 的 PM-09 行明确为 fixed/已关闭，代码 `internal/menu/options.go` 使用 `ReviewModelSupportsEffort`，因此旧 deferred 不再是当前未决项。其作者原件不改。
- 2026-09-09：上一轮只写了 `git diff --check` 干净；本轮扩大到原交付基线后，`git diff --check 258c8eb3a49784d8a96bc2d4a44c325fb97d2425 b653e810e748783979daba1b6a21f200c143722f` 报 `internal/review/definition_unix_test.go:353: new blank line at EOF.`。记为交付自检第 1 项漏检，本轮已修复。没有撤销或重开已经闭合的审核 finding。

### 接管前 SUMMARY 原文（历史，不代表当前结论）

- 交付: 任务分支 `agent-review-template` 最新提交 `b653e810e748783979daba1b6a21f200c143722f`，基于组分支 `8f8f0cd0627fca918e7383ea4c3ebd6962864652`。未合入组分支。
- 验收: 合同验收框保持冻结。
- 验证: 见 IMPLEMENTATION，引用 `b653e810e748783979daba1b6a21f200c143722f`。
- 未决: PM recommend deferred `reviews/pm-agent-def-b1-r6/dispositions/pm-07-deferred-r6.json`；PM low deferred `reviews/pm-agent-def-b1-r6/dispositions/pm-08-deferred-r6.json`；PM low deferred `reviews/pm-agent-def-b1-r4/dispositions/pm-09-deferred-r4.json`；QA low deferred `reviews/qa-agent-def-b1-r6/dispositions/qa-n1-deferred-r6.json`；QA suggest deferred `reviews/qa-agent-def-b1-r6/dispositions/qa-n2-deferred-r6.json`。
- 评审: 组级接收与后续审核待编排器。

## SUMMARY

### 2026-09-09 最终 wrap-up 结论

- 最终完成：任务交付 `6b7c132db76cc00a1245bc45fd992afe3109dcc6` 已随审核源 `64d0c2cf5fa2e8c8930f56967dced09a2187478d` 合入 develop `a8b5afac5d79a09399ab12b097691d1f2212c1dd`，本地与 origin/develop 同步；集成为用户授权保留审核历史的 merge，PR N/A。
- 本轮完成 6/6 收尾项目；旧验收和历史验证记录保留，未把已记录偏差改写成完全无缺陷。绑定 done 的 delivery_commit 使用审核源 `64d0c2cf5fa2e8c8930f56967dced09a2187478d`。
- 审核：计划 agent-def-embed-cycle revision 3 已 sealed；agent-def-batch-one 与 agent-def-batch-two 均 closed，PM/QA PASS，CSA/Hacker N/A；证据为 `reviews/batches/agent-def-batch-one/closed.json`、`reviews/batches/agent-def-batch-two/closed.json`。用户明确跳过合并后追加审核，不能视为新的 PASS。
- 最终集成验证：核验主控日志及树绑定，21 包，1682 pass / 0 fail / 1 skip，build/vet/Windows 交叉构建通过；原生 Windows 未运行。详见 `verification/wrap-up-epoch12.json`。
- 清理：本任务工作树和本地/远端任务分支已删除；两个历史构建二进制保留于 `/tmp/kander-review-wrapup-builds-_1wmeu85/`。组级工作树/分支归主控清理。Codex 会话与 herdr 容器保留。
- 当前保留项（5）：PM-08 low/deferred（非归档 Codex raw 多末尾换行）；QA-N1 low/deferred（提示词子目录缺父目录而启动失败）；QA-N2 suggest/deferred（无成功 --version 探测不入面板，可手改 JSON）；原生 Windows 未验证；合并后追加审核按用户指示跳过。分别引用 `reviews/pm-agent-def-b1-r6/dispositions/pm-08-deferred-r6.json`、`reviews/qa-agent-def-b1-r8/dispositions/qa-n1-codex-deferred-r8.json`、`reviews/qa-agent-def-b1-r8/dispositions/qa-n2-codex-deferred-r8.json` 及 `verification/wrap-up-epoch12.json`（后两项均无新 run 产生）。
- PM-07 历史 recommend/deferred 原件保留：合并后的 reviewerFromConfig 已改为返回配置/不支持 reviewer 的错误，不再出现 codex 字面量兜底；独立读取当前 settings.go:287 后核实，该行为来自上游 fba2e46 并经合并保留，不能冒记为本卡新修复。PM-09/10 已关闭，QA r7 历史失败已由 r8 成功链处理。

### 以下为 wrap-up 前 SUMMARY 历史原文


- 交付：任务分支 `agent-review-template` 最终 `6b7c132db76cc00a1245bc45fd992afe3109dcc6`，基于组 `e756f7d672bb4cc2a488a476485f6821e7b43d2e`。原 fa8da8a 的单个末尾空行删除补丁保持不变；已更新远端任务分支，待主控接收。任务工作树保留。
- 验收：本轮同步补丁 1/1 保留，无冲突或业务扩展；整卡 16 条冻结合同及既有非门禁偏差保持原样，未宣称全部无偏差。
- 验证：`6b7c132db76cc00a1245bc45fd992afe3109dcc6` 上 `go test -json -p 2 ./... -count=1` 为 21 包通过；1480 pass / 0 fail / 1 skip（含子测试）；vet、全包构建、Windows 交叉编译、完整审核区间 diff --check 通过。原生 Windows 跳过如实保留。
- 评审：旧批次已于 d97964c 闭合，PM/QA PASS，CSA/Hacker N/A，见 `reviews/batches/agent-def-batch-one/closed.json`。PM-02/05 Git 映射及 QA r8 作者记录已纳入；不重开旧范围。本轮新补丁由主控接收后安排后续门禁。
- 未决（四项非门禁，另有组级待办）：
  - PM recommend deferred：`reviews/pm-agent-def-b1-r6/dispositions/pm-07-deferred-r6.json`；reviewerFromConfig 的 codex 字面量兜底。
  - PM low deferred：`reviews/pm-agent-def-b1-r6/dispositions/pm-08-deferred-r6.json`；Codex 非归档报告多余换行。
  - QA low deferred：`reviews/qa-agent-def-b1-r8/dispositions/qa-n1-codex-deferred-r8.json`；提示词子目录缺父目录时启动失败。
  - QA suggest deferred：`reviews/qa-agent-def-b1-r8/dispositions/qa-n2-codex-deferred-r8.json`；无成功版本探测的自定义 reviewer 不进面板候选。
- 组级待办：`reviews/plan.json` 仍未封存；当前 review progress 为 pending，仅列 unsealed-plan。新交付接收、后续批次、develop 集成和 wrap-up 待主控。QA r7 参数失败已由 r8 成功轮解析并纳入闭合，旧失败原件保留，不再列作当前阻塞。

## REVIEWS

- {"run_id":"qa-agent-def-b1","batch_id":"agent-def-batch-one","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"e1c01277075599cb0395bf62f16855cebf04b060","report":"reviews/qa-agent-def-b1/report.md"}
- {"run_id":"pm-agent-def-b1","batch_id":"agent-def-batch-one","role":"PM","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"e1c01277075599cb0395bf62f16855cebf04b060","report":"reviews/pm-agent-def-b1/report.md"}
- {"run_id":"qa-agent-def-b1-r2","batch_id":"agent-def-batch-one","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"e1c01277075599cb0395bf62f16855cebf04b060","report":"reviews/qa-agent-def-b1-r2/report.md"}
- {"run_id":"pm-agent-def-b1-r2","batch_id":"agent-def-batch-one","role":"PM","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"e1c01277075599cb0395bf62f16855cebf04b060","report":"reviews/pm-agent-def-b1-r2/report.md"}
- {"run_id":"pm-agent-def-b1-r3","batch_id":"agent-def-batch-one","role":"PM","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"e1c01277075599cb0395bf62f16855cebf04b060","report":"reviews/pm-agent-def-b1-r3/report.md"}
- {"run_id":"qa-agent-def-b1-r3","batch_id":"agent-def-batch-one","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"e1c01277075599cb0395bf62f16855cebf04b060","report":"reviews/qa-agent-def-b1-r3/report.md"}
- {"run_id":"pm-agent-def-b1-r4","batch_id":"agent-def-batch-one","role":"PM","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"e1c01277075599cb0395bf62f16855cebf04b060","report":"reviews/pm-agent-def-b1-r4/report.md"}
- {"run_id":"qa-agent-def-b1-r4","batch_id":"agent-def-batch-one","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"e1c01277075599cb0395bf62f16855cebf04b060","report":"reviews/qa-agent-def-b1-r4/report.md"}
- {"run_id":"qa-agent-def-b1-r5","batch_id":"agent-def-batch-one","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"e1c01277075599cb0395bf62f16855cebf04b060","report":"reviews/qa-agent-def-b1-r5/output.raw"}
- {"run_id":"qa-agent-def-b1-r6","batch_id":"agent-def-batch-one","role":"QA","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"e1c01277075599cb0395bf62f16855cebf04b060","report":"reviews/qa-agent-def-b1-r6/report.md"}
- {"run_id":"pm-agent-def-b1-r5","batch_id":"agent-def-batch-one","role":"PM","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"8f8f0cd0627fca918e7383ea4c3ebd6962864652","previous_run_id":"pm-agent-def-b1-r4","report":"reviews/pm-agent-def-b1-r5/output.raw"}
- {"run_id":"pm-agent-def-b1-r6","batch_id":"agent-def-batch-one","role":"PM","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"8f8f0cd0627fca918e7383ea4c3ebd6962864652","previous_run_id":"pm-agent-def-b1-r4","report":"reviews/pm-agent-def-b1-r6/report.md"}
- {"run_id":"qa-agent-def-b1-r7","batch_id":"agent-def-batch-one","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"d97964c06adb942b94b25c7e5c5bfb04795172e4","previous_run_id":"qa-agent-def-b1-r6","report":"reviews/qa-agent-def-b1-r7/output.raw"}
- {"run_id":"qa-agent-def-b1-r8","batch_id":"agent-def-batch-one","role":"QA","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"d97964c06adb942b94b25c7e5c5bfb04795172e4","previous_run_id":"qa-agent-def-b1-r6","report":"reviews/qa-agent-def-b1-r8/report.md"}
- {"run_id":"qa-agent-def-b2","batch_id":"agent-def-batch-two","role":"QA","execution_status":"ok","base":"d97964c06adb942b94b25c7e5c5bfb04795172e4","commit":"64d0c2cf5fa2e8c8930f56967dced09a2187478d","report":"reviews/qa-agent-def-b2/report.md"}
- {"run_id":"pm-agent-def-b2","batch_id":"agent-def-batch-two","role":"PM","execution_status":"ok","base":"d97964c06adb942b94b25c7e5c5bfb04795172e4","commit":"64d0c2cf5fa2e8c8930f56967dced09a2187478d","report":"reviews/pm-agent-def-b2/report.md"}
