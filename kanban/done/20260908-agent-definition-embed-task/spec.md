# Move built-in agents to embedded definition files and version the definition format

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260908-agent-definition-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 13:17
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:w2T:t18:w2T:p18
- STARTED_AT: 2026-09-09 09:54
- FINISHED_AT: 2026-09-09 23:11
- TASK_BRANCH: agent-definition-embed
- RESULT: completed

- DISPATCH_ID: codex-agentdef-wrap-embed-20260909

- EXECUTION_EPOCH: 13

## GOAL

把四个内置 agent (codex, claude, grok, cursor) 现在散在 Go 代码里的定义迁为嵌入的 JSON 定义文件, 让内置 agent 与用户在 `config.json` `agents` 节声明的自定义 agent 走同一套结构、校验与参数生成机制. 定义格式增加 `schema_version` 字段, 作为后续对外发布的扩展契约的版本锚点.

目前 `internal/config/config.go` 用 `AgentExecutables`, `kanbanModelDefaults`, `reviewModelDefaults` 三张表持有内置 agent 的可执行名与模型默认值, `internal/launch/session.go` 的 `agentArguments` 按 agent 名分支生成 start/resume argv. 本卡把这些迁到定义文件, 使 "加一个内置 agent" 变成 "加一份 JSON", 并让外部贡献者提交的定义文件与内置文件形态一致.

本卡是任务组 `20260908-agent-definition-group` 的第一张, 为后两张 (审核模板, 退出命令与会话钩子) 提供定义文件承载.

## USER_DECISIONS

- 用声明式定义文件作为扩展 agent 与终端的主要方式, Go 接口只作底层承载与少数无法声明化的钩子; 内置实现改为同一格式的嵌入定义文件, 不做二等机制.
- Agent 侧先做, 终端侧后做; 本组三张卡按 "格式与嵌入 -> 审核模板 -> 退出命令与会话钩子" 顺序.
- 定义格式一经发布即是对外契约: 必须带版本号字段; 输出解析只有三种原语; 占位符逐元素替换, 不做 shell 插值; 写入文档作为不可扩大的边界.
- 2026-09-09 授权启动本组: 按依赖启动执行, 任务分支交付组分支 group/20260908-agent-definition-group, 组完成后快进合入 develop 并 wrap-up.

## EXPECTED_OUTCOME

- 仓库内存在一个嵌入的 agent 定义目录 (位置由实现决定并写入 AGENTS.md 包表), 含 codex, claude, grok, cursor 四份 JSON. 每份包含 `schema_version`, `path`, `process_name`, `args.start`, `args.resume`, `session`, `display_name` (选项面板与 doctor 显示名), `supports_effort` (为 false 时不接受也不显示 effort 字段, 对应现有 Cursor 语义), `prompt_delivery` (提示词投递方式与 pane 就绪判据, 对象, 见下条), `rules_target.global` 与 `rules_target.project` (安装器写入规则入口的目标文件, 相对 HOME 或项目根, 为空表示该模式不集成), `rules_integration` (规则入口的写入与识别策略, 取值为 kander 内置的有名策略: `claude-import` 写 `@<path>` 一行并按导入行识别, `markdown-reference` 写 Markdown 引用块并按块识别; 对应 `internal/install/integrate.go:135` 与 `:245` 现在按 Claude 分支的两种写法), 以及 kanban 规模模型默认 (`large_model`, `small_model`, `large_effort`, `small_effort`) 与 review 模型默认 (`model`, `effort`).
- `internal/config` 的三张内置表删除, `internal/menu/agents.go` 的显示名表, `internal/install/integrate.go` 的 `integrationAgents` 与 `AgentRulesTarget` 按名分支, 以及 `menu/options.go` 与 `config/format.go` 的 "cursor 无 effort" 分支, 全部改为经定义读取.
- `agentArguments` 不再按 agent 名分支; 内置 agent 的 start/resume argv 由其定义文件的 `args` 模板与 `session` 策略生成, 与改动前逐项一致.
- 提示词投递方式成为定义字段与显式契约. 现在 `internal/launch/start.go:111` 与 `internal/launch/notify_resume.go:95` 都写死 `append(args, prompt)`, 即隐含假定 "提示词一定追加在 argv 末尾"; 该假定不属于任何一个 agent 的定义, 却决定了定义文件能表达哪些 CLI. 定义文件增加 `prompt_delivery` 对象:

```text
{"mode": "argv"}                                    // 四个内置 agent

{"mode": "pane",                                    // 裸位置参数不是提示词的 CLI
 "ready":   {"match": "<字面子串>" | "regex:<pattern>", "timeout_ms": <正整数>},
 "blocked": [{"match": "<字面子串>" | "regex:<pattern>", "reason": "<单行说明>"}, ...]}
```

  - `mode: argv` 保持现有行为; 此时 `ready` / `blocked` 不接受.
  - `mode: pane` 表示该 CLI 的裸位置参数不是提示词 (例如被解释为子命令), 提示词必须在 CLI 起来之后再送进终端 pane. `ready` 必填, `blocked` 可选.
  - 投递方式随定义解析后挂在 `LaunchPlan` 上, 由 `launchAgent` 按字段分发; 不给 `launchAgent` 增加提示词回调参数, 也不与现有 `durable ...bool` 可变参数混用.
  - **两段就绪, 不能只有一段**: 现有 `herdrWaitPaneReady` 等的是容器 shell 出第一帧输出, 发生在 `pane run` 之前, 语义不变; `mode: pane` 在 `pane run` **之后**再等一次, 等的是 agent TUI 接管终端. 只做第一段会把提示词打进还没被 TUI 取代的 shell 里. 匹配面沿用现有原语: herdr 用 `pane wait-output --match|--regex --source recent --timeout`, tmux 用有界 `capture-pane` 加字面子串或正则匹配, 都允许 TUI 在标记前加渲染前缀.
  - **`ready` 与 `blocked` 是并行的连续判据, `blocked` 命中即短路且优先于 `ready`**, 不是"超时之后再回头归因". 信任确认之类的对话框通常覆盖在已渲染的 TUI 之上, 就绪标记很可能已经出现; 若 `blocked` 只在超时后才查, 这一轮会判为就绪并直接投递, 而 tmux 的 `send-keys -l` 加 `Enter` 没有任何"对方停在对话框"的保护, 提示词会被塞进对话框, 那个回车等于替用户选了一个选项 —— 这正是 `rules/KANDER-KANBAN-RULES.md` "Notify and Recovery" 要求 herdr 侧 "reject up front … rather than stuffing the body into that dialog" 所禁止的. `blocked` 命中后立即返回, 不等满 `timeout_ms`.
  - 就绪后按 `internal/notify/deliver.go` 现有原语投递 (herdr `agent prompt <pane> <text>`, tmux `send-keys -l` 加独立 `Enter`), 不新增协议, 不改用 `pane run`; 投递在 `paneSession` 会话回调之前.
  - **三种失败原因, 一种处置**:
    1. `blocked` 命中: 该 CLI 停在需要人回答的对话框上 (例如首次进入某目录时的信任确认).
    2. 投递原语自身拒绝: herdr 的 `agent prompt` 在对方停在审批或提问 UI 时会明确拒绝, 因此存在"就绪命中 -> 投递被原语拒绝"这条路径. tmux 的 `send-keys` 不会拒绝, 该情形只在 herdr 侧真实存在.
    3. 纯超时: `ready` 超时且全程未命中任何 `blocked`, 即 TUI 根本没起来 (例如该 CLI 未登录或未配置模型).
  - 三者的处置相同: 先抓取 pane 输出留作诊断 (复用现有 `resumedAgentFailureOutput` 的取法, 避免关掉容器时把 CLI 打出的错误一起丢掉), 再关闭本次创建的 tab/window 并把卡片回滚到 `todo/`. **不保留容器, 不写 `WINDOW`**. 区别只在错误文案: 1 与 2 指出该 CLI 需要人工确认一次, 请在终端里手动跑一次该 CLI 回答对话框, 然后重试 `kander start`; 3 给出抓到的 pane 输出.
  - **为什么不能"保留容器 + 事后补投"** (2026-09-09 用户决定, 依据现行规则逐条核对): 投递发生在 `paneSession` 会话回调之前, 此刻卡片 `SESSION` 为空、pane 的 `@kander_session` 也没写, 而 `mode: pane` 的目标 CLI 恰恰是 `discovered` 类 —— 会话是第一条提示词送进去之后才存在的, 无法提前落盘. 于是三条恢复路同时不通: `resume` 按 "wakes up the original agent by the card `SESSION`" 需要 SESSION, 且按 "herdr validates the pane directly returned by **this invocation's** `tab create`" 必然新建容器, 与"保留这个容器"自相矛盾; `notify` 直投按 "When both are empty, identity cannot be proven; treat it as no direct delivery channel and fall back" 会明文拒投并转入 recovery 另起进程; `--agent` 接管按 "Cards that never went through `start` and have no original `SESSION` record still cannot be taken over" 被禁; 规则还专门写了这种 pane 是 `alive` 但 "noted as not directly deliverable". 保留容器只会留下一张 `working/` + 有 `WINDOW` + 无 `SESSION`、三条命令都不认的卡片, 外加一个漏掉的容器. 而信任确认是**按目录记住**的, 人在终端里手动回答一次即对该 worktree 永久生效, 重试 `start` 即可通过, 因此回滚是足够的处置.
  - **`mode: pane` 的 started 判定在各 launcher 现有条件之上再加一级** (用户 2026-09-09 决定): 不是把判定点统一成单点 —— 单点在两个 launcher 上的方向相反, 会写出一条错的公开承诺. 现行条件是 herdr "counts as started once `pane run` succeeds" (其后的失败只警告、不进关 tab 与回滚路径), tmux/tmux-session 则是"会话发现与 pane marker 写入成功后才算 started". 而 `mode: pane` 的投递发生在 `pane run` 之后、`paneSession` 会话回调之前, 所以"投递成功"这一级对 herdr 是**往后**加, 对 tmux 是**往前**插. 因此定义为两级都满足才算已启动:
    - herdr: `pane run` 成功 **且** 提示词投递成功.
    - tmux / tmux-session: 提示词投递成功 **且** 会话发现与 pane marker 写入成功.

    任一级失败都进入现有的 `LaunchFailure` 关容器与回滚路径 —— 这样就不会出现"已算 started 却仍要按现行条款关窗口回滚"的状态, 而那正是本卡要改的 herdr 那句想消除的情形. 规则同步按 launcher 分别给出改后原文, 不合并成一句. `mode: argv` 的判定条件逐字不变.
  - 就绪标记与 blocked 标记是**定义数据而不是 Go 常量**: 目标 CLI 的启动横幅与状态栏文案会随版本变, 也可能被用户自己的 TUI 配置改掉, 写死会静默失配.
  - **各入口的适用范围**: `start`, 以及 `resume` 中创建新容器的接管路径, 走两段就绪与上面三种失败原因的统一处置; `notify` 对**已存在容器**的直投沿用现有路径, 不等 `ready` —— 那个 TUI 早已起来, 就绪标记通常已滚出 `--source recent` 的窗口, 再等一次必然超时; `notify` 走恢复通道新建容器时与 `resume` 同. 三个入口的 argv 都不含提示词.
  - 声明 `mode: pane` 的 agent 在 launcher 实际解析为 `foreground` 或 `console` 时, 于启动前 (claim 之前) 拒绝, 卡片不移动也不改写, 错误文案指出该 agent 需要 tmux 或 herdr.
  - 四个内置定义全部为 `{"mode": "argv"}`, 其 argv 与投递路径逐项不变.
- 本卡改动已发布规则 `rules/KANDER-KANBAN-RULES.md` 的四处承诺, 随本卡同步: "Configured Execution Agents" 的 "Kander appends the prompt last."; "Resuming the Original Session" 与 "Start Checks and Rollback" 中两处 "The agent command line receives only one instruction containing that absolute path." / "receives only one instruction to read that file"; "Start Checks and Rollback" 的各 launcher 前置条件与回滚触发 (新增 `mode: pane` 在 foreground/console 上启动前拒绝, 以及第二段就绪的三种失败原因与其统一处置); 以及 "herdr counts as started once `pane run` succeeds" 与 tmux 侧对应的 started 条件, 按上面"各 launcher 现有条件之上再加一级"分别改写 (两句分别给出改后原文, 不合并). 改写只针对 `mode: pane`, `mode: argv` 的承诺逐字不变.
- 本卡结束时允许保留的按名/按方言分支 (逐一列出, 由后续卡清理): `internal/launch/session.go:90` 附近的 Cursor create-chat 分配分支与 `internal/config/agents.go` 按 dialect 推导 session 模式的分支 (属 `20260908-agent-exit-session-hook-task`); `internal/takeover` 的退出命令表与 `internal/liveness/classify.go:54` 附近的 Codex 会话提示分支 (同上, 由 A3 清理 liveness 包); `internal/review` 与 `internal/config` 中 `ReviewAgents` 及审核相关的全部按名分支 (属 `20260908-agent-review-template-task`). 其余按名业务分支在本卡结束时为零.

## ACCEPTANCE_CRITERIA

- [ ] 四份嵌入定义文件存在, 字段集合如 EXPECTED_OUTCOME 所述, 每份 `schema_version` 为 1; 加载时校验字段, 未知 `schema_version` 或结构不合法的定义文件被拒绝, 错误信息含文件名与字段.
- [ ] `internal/config/config.go` 中 `AgentExecutables`, `kanbanModelDefaults`, `reviewModelDefaults` 及按 agent 名硬编码的默认值分支删除; `grep -rn '"codex"\|"claude"\|"grok"\|"cursor"' internal --include='*.go'` (排除 `_test.go` 与嵌入定义文件) 在 config, launch, install, menu 四个包内仅剩定义目录加载与测试固件, 不再有按名分支的业务逻辑. 允许保留的分支以 EXPECTED_OUTCOME 末条的清单为准 (逐符号列出: `launch/session.go` Cursor create-chat 分支, `config/agents.go` dialect 推导 session 分支, `takeover` 退出命令表, review 与 config 的 `ReviewAgents` 及审核分支), 验收时用 grep 输出逐条对照, 清单之外的按名分支为零.
- [ ] `internal/launch` 中 `agentArguments` 不含按 agent 名分支; 新增表驱动测试固定四个内置 agent 在 {start, resume} × {large, small} × {有 session, 无 session} 组合下的 argv, 期望值取自改动前实现的输出, 全部一致.
- [ ] 定义 schema 含 `prompt_delivery` 对象并校验: `mode` 取值集合; `mode: pane` 必须有 `ready`, `ready.timeout_ms` 为正整数, `match` 的正则可编译, `blocked` 各项的 `reason` 为非空单行; `mode: argv` 出现 `ready` 或 `blocked` 时拒绝; 错误信息含 agent 名与字段名.
- [ ] `LaunchPlan` 暴露投递方式并由 `launchAgent` 分发, `launchAgent` 签名不新增提示词回调参数. 以现有 tmux/herdr 测试桩断言实际发出的命令: 一个声明 `mode: pane` 的测试用自定义 agent, 在 tmux 与 herdr 两条路径上 argv 末元素不是提示词, 且提示词在 `pane run` 之后、`paneSession` 会话回调之前经 `send-keys -l` 加独立 `Enter` / `agent prompt` 送出; 四个内置 agent 保持 `argv`, 提示词仍是 argv 末元素, 且测试桩**未收到任何** `send-keys` / `agent prompt` 调用.
- [ ] 两段就绪有用例: 桩在 `pane run` 之后先不输出就绪标记时投递不发生, 输出标记后才发生.
- [ ] 三种失败原因各有用例, 且**优先级可判定**: (1) 桩同时输出就绪标记与某条 `blocked` 标记时, 走失败路径而不是投递路径, 且在 `timeout_ms` 到达之前就返回 (用例断言返回耗时远小于 `timeout_ms`); (2) **herdr 路径**上桩使 `agent prompt` 返回拒绝时同样走失败路径 (tmux 的 `send-keys` 不存在拒绝, 不为其造用例); (3) `ready` 超时且全程无 `blocked` 标记.
- [ ] 三者的处置一致且可判定: 输出含关闭前抓取的 pane 内容, 本次创建的 tab/window 被关闭, 卡片回到 `todo/` 且文本与改动前一致, 未留下 `WINDOW` 或 `SESSION` 残留; 其中 (1) (2) 的错误文案含"手动跑一次该 CLI 回答对话框后重试 `kander start`"的指引, (3) 不含该指引.
- [ ] `mode: pane` 的 agent 在 `foreground` 下端到端被拒绝, 且拒绝发生在 claim 之前 (卡片仍在 `todo/`, 文本与 revision 不变), 错误文案含 tmux/herdr 提示; `console` 只在原生 Windows 可达, 由 `LaunchPlan` 分发层的单元测试覆盖其拒绝判定, 不写成 Linux 上跑不出来的端到端条件.
- [ ] `resume` 的接管路径 (新建容器) 对 `mode: pane` 走两段就绪与三种失败原因的统一处置, 有用例; `notify` 对已存在容器的直投不等待 `ready`, 有用例断言未调用 `wait-output` / `capture-pane` 就绪等待.
- [ ] `rules/KANDER-KANBAN-RULES.md` 的四处承诺按 EXPECTED_OUTCOME 同步改写, 逐条给出改后原文: "Kander appends the prompt last."; 两处 "only one instruction … absolute path"; "Start Checks and Rollback" 的前置条件与回滚触发; started 条件按 herdr 与 tmux/tmux-session **分别**改写为"现有条件之上再加投递成功一级", 两句各自给出改后原文. 改写只针对 `mode: pane`, `mode: argv` 的承诺逐字不变, 由测试或逐字对照固定.
- [ ] 用户自定义 agent 的现有测试 (dialect 继承, args 模板占位符丢弃规则, session 三种模式, path/process_name 校验) 全部保持通过, 未删改期望值.
- [ ] `kander config --json` 在默认配置与仓库现有测试固件配置下的输出与改动前逐字节一致, 由测试固定; 安装器在全局与项目两种模式下, 对四个内置 agent 写入的规则入口目标路径、写入内容 (Claude 的 `@<path>` 行与其他 agent 的引用块) 以及重复安装时的识别与去重行为, 与改动前逐字节一致, 由 install 测试固定; `integrate.go` 不再按 agent 名分支, 改为按定义的 `rules_integration` 策略分发.
- [ ] 三层校验各有测试: 嵌入定义声明 `discovered` 通过结构校验; 用户 `agents.<name>` 节声明 `discovered` 仍被拒绝; 加载嵌入定义不探测可执行存在, 缺少四个 CLI 之一时 `kander config --json` 与看板读取仍正常, 只有 start/doctor 报告缺失; 用户显式 `path` 不可执行时配置校验仍按现有行为拒绝; 缺少 `schema_version` 的旧用户配置按 1 处理.
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` 通过; `GOOS=windows go build ./...` 通过.
- [ ] `docs/custom-agents.md` 说明内置 agent 亦为定义文件, 列出定义文件位置、`schema_version` 与覆盖优先级, 并说明 `prompt_delivery` 两种 `mode`、`pane` 的两段就绪, 以及三种失败原因 (`blocked` 命中 / 投递原语拒绝 / 纯超时) 与其统一的关容器加回滚处置; 文中引用公共输出解析结构时只指向 `20260909-process-output-parser-task` 交付的 `docs/output-parsing.md`, 不复述字段集合; `AGENTS.md` 包表相应更新.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 已有问题: 现有自定义 agent 机制在 herdr 下不安装识别器, foreground/console 无终端地址等既有限制不在本卡处理; 排除.
- 并发与跨平台加固: 定义文件为嵌入只读数据, 无并发写; Windows 只要求交叉编译通过, 不新增 Windows 原生验证; 排除.
- 共享契约与文档: `docs/custom-agents.md` 与 `AGENTS.md` 包表的同步纳入本卡; `rules/KANDER-KANBAN-RULES.md` 的四处承诺 (提示词追加在 argv 末尾、两处"命令行只带一行含路径的指令"、Start Checks and Rollback 的前置条件与回滚触发、herdr 的 started 判定) 因 `prompt_delivery` 直接改变而**纳入本卡**; `docs/custom-agents.md` 亦为 `20260908-agent-review-template-task` 的写方, 两卡不可并行分派; 其余规则不改; 部分纳入.
- 相邻功能与后续阶段: 审核 argv 模板 (`args.review`) 与 reviewer 名单开放属 `20260908-agent-review-template-task`; 退出命令与会话发现钩子属 `20260908-agent-exit-session-hook-task`; 用户目录下的独立定义文件搜索路径 (非 config.json 内嵌) 属后续阶段; 新增第五个内置 agent (kimi) 及其定义文件属该组完成后的独立任务, 本卡只交付 `pane` 投递机制并用测试用自定义 agent 验证, 不引入任何 kimi 相关内容; 排除.

## DISCUSSION

```text
PREREQUISITES: N/A
```

- 设计结论: "内置与自定义共用一套机制" 是本组的核心验证点. 若某个内置 agent 的 argv 无法用现有模板原语 (逐元素占位符替换, 空值丢弃相邻 flag) 表达, 应扩展模板原语并在定义文档中记录, 而不是为该 agent 保留 Go 分支; 扩展原语的语义须对用户定义同样可用.
- 设计结论 (本轮用户确认, 2026-09-09): `prompt_delivery` 是上一条的直接应用, 不是为某个具体 agent 开的口子. "提示词追加在 argv 末尾" 是 Go 侧的隐含假定而非任何 agent 的属性, 只要它还写死在 `start.go` 与 `notify_resume.go` 里, 定义文件就无法表达裸位置参数被当作子命令的 CLI, 这类 agent 只能回到 Go 分支, 与本组的归零目标冲突.
- 触发本条的实测事实 (已内联, 不依赖仓库外文件): 存在这样一类交互式 CLI —— (1) 裸位置参数被解析为子命令而不是提示词, 传入提示词直接报 `unknown command`; (2) 它确实有一次性提示词旗标, 但那个旗标会强制进入非交互模式, 跑完一轮就退出, 并且拒绝与"不打断"权限档组合, 因此不能用于看板执行面 (执行卡需要一个长驻 TUI 供 `notify` / `resume` / `dismiss` 与人工接管); (3) 它首次进入某个目录时会弹出信任确认对话框, 需要人回答一次; (4) 它的启动横幅与状态栏文案在小版本间改过, 且可以被用户的 TUI 配置改掉. (1)(2) 是需要 `mode: pane` 的原因, (3) 是需要 `blocked` 的原因, (4) 是就绪标记必须是定义数据而不是 Go 常量的原因.
- 本卡不引入任何具体的第五个 agent, 只用测试用自定义 agent 验证机制. 上述实测的完整命令行记录在 `tmp/kimi-cli-integration-notes.md`, 但该文件未纳入版本控制, 只作线索; 本卡依赖的事实已全部内联在上一条.
- 覆盖优先级: 嵌入定义 < `config.json` `agents.<name>`. 与现有 path/process_name 覆盖语义一致.
- 设计结论: 不引入脚本能力, 是 "三种原语 + 不做 shell 插值" 边界的直接推论, 写入定义文档.
- 公共输出解析结构与占位符转义规则 (`internal/process`, agent 审核模板与终端定义共用) 的**权威描述在 `20260909-process-output-parser-task`**, 由该卡交付实现; 本卡、`20260908-agent-review-template-task` 与 `20260908-terminal-definition-format-task` 一律只引用, **不在卡内复述字段集合** —— 该结构此前已因三处各自复述而分叉过两次 (第三轮统一 `source`, 第四轮同步本卡). 本卡在 `docs/custom-agents.md` 里介绍该结构时同样只指向该卡交付的 `docs/output-parsing.md`.
- 组结构记录 (用户 2026-09-09 决定): 本组由 3 张扩为 4 张 —— `20260909-process-output-parser-task` 从 `20260908-agent-review-template-task` 拆出, 只交付 `internal/process` 公共解析结构与测试. 依据"独立可验收决定卡数、张数只决定分组"的口径: 该结构对现有代码是纯新增 (无调用方行为变更、无既有期望值改写、无规则同步), 与审核模板迁移之间只有"被调用"一条边, 是独立可验收的; 拆开后 `20260908-terminal-definition-format-task` 的依赖从整张审核模板卡收窄到只依赖该新卡, 终端组解锁点大幅提前. 本组四张的依赖链为: `20260909-process-output-parser-task` (无前置, 可与本卡并行) 与本卡 -> `20260908-agent-review-template-task` -> `20260908-agent-exit-session-hook-task`, 共享同一份 agent 定义格式契约.
- 同组后两张卡依赖本卡的定义文件与加载器.
- 已用 145 处 launcher 名分支与约 30 处 agent 名分支的统计作为本组与终端组拆分依据 (统计于 develop 1e1d3a6).

- SELF_REVIEW: 通过. 目标 (内置 agent 迁为定义文件, 加版本号) 与结果一致; 用户决策仅记录已确认的方案要点; 边界把 review/takeover 的分支明确留给同组后两卡, 与验收第 2 条口径一致; 验收条件可判定 (grep 范围, 表驱动 argv 对比, config --json 逐字节, 交叉编译). 无需用户新决策.
- CARD_REVIEW (第一轮, 建卡时): 需修正后通过 (独立 agent, 新会话, 只读卡片与用户原始需求). 发现: (1) 验收要求 install/menu 无按名分支, 但定义字段覆盖不了规则集成目标与显示名, 已补 display_name, supports_effort, rules_target 字段并写入 EXPECTED_OUTCOME 与验收; (2) config 包按 dialect 推导 session 的分支应由 A3 处理, 已注明允许保留; (3) "不引入脚本能力" 属推论, 已移入 DISCUSSION.
- 计划: 本卡改为 large (横跨 config, launch, install, menu 四包并要求逐字节兼容), 执行者启动时先写 `plan.md`, 建议阶段: (1) 定义结构与三层校验; (2) config 三张表迁移与 `config --json` 兼容; (3) launch argv 表驱动迁移; (4) `LaunchPlan` 投递契约与 `pane` 投递 (含 foreground/console 前置拒绝); (5) install 集成策略; (6) menu/doctor 显示名与 effort; (7) 文档.
- CARD_REVIEW: 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 对 backlog 全部 12 张卡的独立审核). 发现: (1) 定义字段表达不了安装侧 Claude 分支的导入识别与 `@path` 写法, 已增加 `rules_integration` 策略字段与写入内容/重复安装验收; (2) 允许保留的分支清单与后两卡分工冲突 (`launch/session.go:90` Cursor 分支未列入, config 的 ReviewAgents 留给 A2), 已按符号逐条列出; (3) 共用校验缺兼容口径 (用户配置禁止 discovered, 嵌入定义需要 discovered, 不应要求四个 CLI 全装), 已分为结构/覆盖/环境三层并固定缺 schema_version 的行为; (4) 改 large 并写明计划阶段. 四项已修正.
- CARD_REVIEW (第三轮): 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 第二轮复审). 发现: (1) 允许保留清单漏了 `liveness/classify.go:54` 的 Codex 会话提示分支, 与 A3 的 liveness 归零验收重叠, 已列入清单并归 A3; (2) 三层校验口径自相矛盾 (显式 path 必须可执行 vs 只在 start/doctor 探测), 已改为嵌入定义不探测、用户显式 path 保持配置期 LookPath. 两项已修正.
- CARD_REVIEW (第四轮): 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 第三轮复审). 发现: DISCUSSION 仍把公共解析结构限定为 `stdout|file`, 与 A2/B2 已统一的三值全集不一致; 已同步为全集加各自子集, 实现仍只由 A2 交付. 已修正.

- SELF_REVIEW (2026-09-09, 合同变更后复核): 通过. 本轮新增 `prompt_delivery` 与四处发布规则同步, 范围与用户当场确认的三项决定一致 (三个缺口全做, 取消保留态, started 判定按 launcher 分级); USER_DECISIONS 本轮未改动, 新增结论一律记在 DISCUSSION 并标注确认日期, 未把建议写成用户决策; 边界把公共解析结构、审核模板、退出命令与会话钩子分别留给同组另三张卡, 与各自验收口径一致; 验收条款均可判定 (schema 校验、测试桩断言实际发出的命令、卡片状态与目录位置、规则改后原文逐条给出). 无待决的用户问题.
- CARD_REVIEW (2026-09-09, 本轮四次往返): 需修正后通过 (独立子代理, 新会话, 只读本组卡片与用户原始需求, 2026-09-09 共四轮往返). 发现并已修正: (1) 公共解析结构描述再次分叉, 改为权威描述单点; (2) `pane` 缺就绪判据, 补 `ready` / `blocked` 与两段就绪, 并明确标记是定义数据不是 Go 常量; (3) `blocked` 原写成超时后的事后归因, 会在对话框覆盖已渲染 TUI 时判为就绪并把提示词打进对话框, 改为与 `ready` 并行的连续判据、命中即短路且优先; (4) 缺 "投递原语自身拒绝" 这条真实路径, 已补; (5) 原 "保留容器 + `resume` 补投" 在现行契约下三条恢复路同时不通 (`resume` 依赖 SESSION 且必然新建容器, `notify` 直投在会话身份为空时明文拒投并转入 recovery, 无 SESSION 不许接管), 经用户决定取消保留态、并入关容器加回滚; (6) `console` 拒绝验收在 Linux 上不可判定, 拆为 foreground 端到端加分发层单测; (7) "投递路径一致" 不可观测, 改为断言测试桩未收到任何投递调用; (8) 曾以未纳入版本控制的 `tmp/` 笔记为设计依据, 所需事实已内联; (9) OUT_OF_SCOPE 曾写 "规则不改", 而本卡实际改动四处已发布承诺, 已纳入并加逐条给出改后原文的验收; (10) `resume` / `notify` 路径上的 `mode: pane` 未定义, 已补各入口适用范围; (11) started 判定点简化为单点后在 tmux 上方向相反, 改为各 launcher 现有条件之上再加投递成功一级. 末轮结论为 "需修正后通过", 其列出的两条措辞级修正 (started 分级表述与 "三分支" 残留) 已按其给出的处方修正后发布, 未再复审.
## IMPLEMENTATION

- 初交付 `258c8eb3a49784d8a96bc2d4a44c325fb97d2425`：嵌入定义、pane 投递、文档与规则同步；rebase 到组分支 `116bd74`。
- fix `39bcb7c5a8cef92832e69cfe1cf9bbc6` epoch 2 / `pm-agent-def-b1-r4`：fixed PM-01/02/09，rejected PM-06；交付后 rebase 为 `77affdd`/`1f1924b`/`fbef4fa`。
- fix `581024f8a26217c9493b9392dbbc2adc` epoch 3 / `qa-agent-def-b1-r6`：QA-01 已由 PM-01 覆盖；记录 `reviews/qa-agent-def-b1-r6/dispositions/qa-01-fix-r1.json`。
- sync `ca7ec744c81ae32a35baff46b6610ae4` epoch 4：rebase 到组 `f5dc7c3`，交付 `fbef4fa`。
- fix `3f088ee6401f8c41c66e113351a46190` epoch 7 / `pm-agent-def-b1-r6`：PM-10 fixed；记录 `reviews/pm-agent-def-b1-r6/dispositions/pm-10-fix-r1.json`；当时交付 `2361d6e`（基于旧组 HEAD `8f8f0cd`）。

sync 派回 `7eb7193715ed07c0b2dede2785c32613` epoch 8：
- 组分支已含 A2 的 PM-10 `b653e810e748783979daba1b6a21f200c143722f`，本卡 `2361d6e` 无法快进。
- 已将 `agent-definition-embed` rebase 到组 HEAD `b653e810e748783979daba1b6a21f200c143722f`。
- 冲突按点处理，未整文件覆盖：文档句保留组上已接收的 A2 措辞（与本卡语义等价）；测试保留 A2 对 `reviewer` 空块不继承的断言，并保留本卡对 `dialect: claude` 成对声明后 Env/Inspection/HomeEnv 不被填充的覆盖。
- 最终交付 SHA：`d97964c06adb942b94b25c7e5c5bfb04795172e4`（已 `--force-with-lease` 更新远程任务分支）。
- 未改组分支。

核验（`d97964c06adb942b94b25c7e5c5bfb04795172e4`）：
1. `git diff --check`：干净。
2. 行数：`docs/custom-agents.md` 106 行（与组分支逐字节相同），`agents_review_test.go` 148 行。
3. `go vet ./internal/config`、`GOOS=windows go build ./internal/config` 通过。
4. `go test ./internal/config ./internal/menu -count=1` 通过。

- 2026-09-09，Codex sync epoch 10：从 Git、原始审核与 dispatch 重建进度，接受回执 replayed=false。旧交付 d97964c 已在本地/远程组分支；本轮补验收测试并正常推送 `b216ca25cb328300b3a45c54bc00a5ca442a89d7`，父提交/组基线 `d97964c06adb942b94b25c7e5c5bfb04795172e4`。全量 21 包、1459 测试及子测试通过、1 Windows 原生测试跳过；build、vet、Windows build 通过。未改组分支。详细自查及原始证据见 `verification/takeover-10.md`，16 条执行侧验收映射见 `report.md`；原作者处置保留，本次未新增 finding disposition。

- 2026-09-09，sync `codex-agentdef-dispositions-embed-r2-20260909` epoch 12：仅追加 5 条 codex Git 映射原件，均 fixed，连接原 previous_record_id；旧作者原件哈希不变。新记录为 `reviews/pm-agent-def-b1-r4/dispositions/pm-01-git-map-codex-e12.json`、`pm-02-git-map-codex-e12.json`、`pm-09-git-map-codex-e12.json`（同目录），`reviews/pm-agent-def-b1-r6/dispositions/pm-10-git-map-codex-e12.json`，`reviews/qa-agent-def-b1-r6/dispositions/qa-01-git-map-codex-e12.json`。23 项历史提交定向测试/子测试通过，映射与祖先命令见 `verification/disposition-map-12.md`。保留交付 `b216ca25cb328300b3a45c54bc00a5ca442a89d7`；无代码/组分支修改，不 rebase，不新增 finding；等待主控闭合 d97964c 上的旧批次并接收排队新交付。

## SUMMARY

四个内置 agent 已改为嵌入定义并交付 pane 投递。PM-10 文档契约以组上 A2 措辞为准，本卡补上 dialect 成对声明的测试覆盖。任务分支已 rebase 到组分支 `b653e810e748783979daba1b6a21f200c143722f`，最终提交 `d97964c06adb942b94b25c7e5c5bfb04795172e4`，等待合入组分支。


2026-09-09（epoch 13）当前结论：执行自验 16/16；两批 PM/QA PASS、计划封存闭合，CSA/Hacker N/A；已随授权 merge `a8b5afac5d79a09399ab12b097691d1f2212c1dd` 集成并同步 develop。合并后追加审核按用户要求跳过。本卡工作树/本地及远端分支已清理，原件及会话保留。既有整仓日志复核为 1682 pass / 0 fail / 1 skip；未重复测试。当前遗留 2 项：PM-06 low/rejected 契约取舍、原生 Windows 验证缺口。详见 `report.md` 最终收尾节与 `verification/wrap-up-13.md`；旧待办文字为历史记录，最终状态以本 dispatch 完成回执为准。

## TAKEOVER_AUDIT

- 2026-09-09，epoch 10：上述 SUMMARY 保留为 epoch 8 历史。核验 d97964c 时，`TestMinimalCursorFixtureJSONRoundTrip` 仅自往返、install 测试未固定四 agent 两作用域全文、pane 接管失败无入口回滚矩阵；不能仅凭旧“已交付”推定这些验收测试齐全。本轮以 b216ca2 补齐证据，没有推翻已核验并处置的 finding。补齐 `report.md`。
- 当前交付 `b216ca25cb328300b3a45c54bc00a5ca442a89d7` 已推任务分支，待组接收。执行側验收通过；组计划未封存、批次未关闭，不能进入 done。工作树与分支保留。
- [PM][low/rejected] PM-06 保留供复核：`reviews/pm-agent-def-b1-r4/dispositions/pm-06-reject-r1.json`。
- [QA][N/A/N/A] 最新调用因 unknown model id 失败：`reviews/qa-agent-def-b1-r7/sidecar.json`。
- [自查][N/A/N/A] A2 文件尾部空行；组审核/集成/wrap-up 待办：`verification/takeover-10.md`（no run produced）与 `report.md`。

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
