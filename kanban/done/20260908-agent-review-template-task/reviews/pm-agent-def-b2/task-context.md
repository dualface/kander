# 任务组第二批完整审核合同

任务组：20260908-agent-definition-group。
基线：已闭合第一批最终目标 d97964c06adb942b94b25c7e5c5bfb04795172e4。最终目标使用本次审核调用的 commit；仅审核基线之后所有已接收新交付，不跨批次沿用通过结论，也不重复审核旧区间。

本批覆盖四卡：A1 新验收测试与最小 Cursor fixture；解析器对显式空 select 和无效 equals JSON 值的验证补漏；A2 EOF 空行修正；A3 首次接收的退出命令/会话钩子，以及接管核验补漏。具体边界以真实 Git diff 和下列完整合同为准。代码由各卡执行体提交，主控仅做协调、组分支接收与审核。

## 前批次未决项排除说明

以下项在第一批闭合目标 d97964c 之前已存在，原件与作者依据均保留。它们本身不重复作为本批 finding；本批若实际引入、加重或掩盖问题，仍须报告真实影响。

- A1 / PM r4 PM-06 / low rejected：rules_target 为空表示不集成，不按 dialect 回落安装目标；原合同明确该取舍，PM r6 接受作者依据。
- A2 / PM r6 PM-07 / recommend deferred：reviewerFromConfig 仍有 codex 字面量兜底；旧区间未改，该项不在本轮新增交付目标中。
- A2 / PM r6 PM-08 / low deferred：非归档 Codex raw 报告经 fmt.Println 多末尾空行，判定与错误文案不受影响；旧行为保留。
- A2 / QA r8 QA-N1 / low deferred：review.prompt_files 规范子目录通过校验，但写入不创建父目录而失败；旧路径未改，单段文件名可用。
- A2 / QA r8 QA-N2 / suggest deferred：审核面板候选要求成功 --version 探测，脚本 reviewer 可手改 JSON；旧过滤未改。

PM-09 旧 deferred 已由 PM r6 核实修复，不作为当前未决项；PM-10 由主控独立核验文档机械修复后闭合；QA-01 已由 QA r8 核验关闭。旧批原件只供历史定位，不能代替本批独立审核。CSA/Hacker 按仓库 AGENTS.md 明确例外记 N/A；PM/QA 均 required。

以下每卡保留六项合同及 DISCUSSION 全文，不删用户取舍、验收或威胁模型。

# 20260908-agent-definition-embed-task

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

# 20260908-agent-exit-session-hook-task

## GOAL
补齐 agent 定义文件中最后两处仍依赖 Go 硬编码的内容: 交互退出命令与会话发现钩子.

- `internal/takeover` 以内置表决定 `dismiss` 与接管清理时向 agent 发送 `/exit` 还是 `/quit`, 无方言的自定义 agent 被直接拒绝. 本卡在定义文件中增加 `exit_command` 字段.
- Codex 的会话身份来自扫描 `CODEX_HOME` rollout 文件 (`discovered` 模式), Cursor 的会话来自调用 `create-chat`. 这类逻辑无法用模板表达, 本卡把它们收敛为有名字的 Go 钩子, 定义文件通过 `session.mode: "hook:<name>"` 引用, 并把内置钩子清单写入文档, 让贡献者一眼看到 "哪些仍需写 Go".

## USER_DECISIONS
- 与 `20260908-agent-definition-embed-task` 相同的整体决策: 声明式定义为主, 无法声明化的部分收敛为有名字的钩子清单.
- 2026-09-09 授权启动本组: 按依赖启动执行, 任务分支交付组分支 group/20260908-agent-definition-group, 组完成后快进合入 develop 并 wrap-up.

## EXPECTED_OUTCOME
- 定义文件新增 `exit_command` (单行字符串, 无控制字符, 可为空). 内置 codex/claude 定义为 `/exit`, grok/cursor 为 `/quit`. `takeover` 包的内置表删除, 改为读定义; `dialect` 继承该字段. 定义了 `exit_command` 的纯模板 agent 可以被 `dismiss`, 其余身份, 状态, 容器拓扑, 退出确认与关闭规则不变; 未定义的仍明确拒绝并保留容器.
- `internal/config/agents.go` 中按 dialect 推导 session 模式 (codex 得 `discovered`, cursor 得 `allocated`) 的分支删除, 改为读内置定义的 `session` 字段.
- `session.mode` 新增取值 `hook:<name>`. 内置钩子: `codex-rollout` (现有 rollout 扫描发现), `cursor-create-chat` (现有 create-chat 分配). codex 定义写 `hook:codex-rollout`, cursor 定义写 `hook:cursor-create-chat`. 钩子在 Go 内以名字注册, 引用未注册名字的定义被拒绝. 现有 `discovered` 不接受手工配置, `generated`/`allocated`/`none` 的规则不变.
- `docs/custom-agents.md` 新增 "钩子清单" 一节, 列出每个钩子的名字, 用途, 对应的内置 agent, 以及 "以下情形仍需提交 Go 代码" 的边界说明.
- 发布规则 `rules/KANDER-KANBAN-RULES.md` "Configured Execution Agents" 中 `generated`/`allocated`/`none` 的会话模式清单加入 `hook:<name>` 及其含义, "Dismissal and Terminal Cleanup" 中 "Claude/Codex receive `/exit`; Grok/Cursor receive `/quit`" 改为 "按 agent 定义的 `exit_command`".

## ACCEPTANCE_CRITERIA
- [ ] 定义 schema 含 `exit_command`; 校验拒绝控制字符与多行; `internal/takeover` 中按 agent 名的退出命令表删除, `AgentExitCommand` 改为读定义 (含 dialect 继承).
- [ ] 测试: 纯模板 agent 定义 `exit_command: "/bye"` 后, `dismiss` 在 tmux 路径向 pane 投递 `/bye` 与 Enter (以现有 tmux 测试桩验证); 未定义 `exit_command` 的纯模板 agent 仍被 `dismiss` 拒绝, 错误文案指出缺少该字段.
- [ ] `session.mode` 接受 `hook:codex-rollout` 与 `hook:cursor-create-chat`; 引用未注册钩子名的定义在加载时被拒绝, 错误信息含 agent 名与钩子名; codex 与 cursor 的嵌入定义改用钩子引用, 现有 codex 会话发现测试与 cursor create-chat 测试保持通过.
- [ ] 内置钩子的注册表在单一位置 (一个 Go 文件) 维护, 并有测试断言 "注册表中的每个名字都在 docs/custom-agents.md 钩子清单中出现", 防止文档漂移.
- [ ] `takeover`, `launch`, `notify`, `liveness`, `config` 包内不再有按 agent 名或 dialect 名的 `switch`/`if` 业务分支 (测试与嵌入定义除外). 本卡是组内最后一张: 验收时用 grep 核验整个 `internal` 中按内置 agent 名分支的业务逻辑为零 (review 包的分支由前置卡 `20260908-agent-review-template-task` 负责清理, 本卡只核验组级最终结果, 发现残留时报告并退回对应卡, 不代为清理).
- [ ] `go build ./...`, `go vet ./...`, `go test ./...`, `GOOS=windows go build ./...` 通过.
- [ ] `docs/custom-agents.md` "会话策略" 与 "面板与审核边界" 中关于 dismiss 拒绝与 discovered 的描述同步更新, 新增 "钩子清单" 一节; `rules/KANDER-KANBAN-RULES.md` "Configured Execution Agents" 的会话模式清单与 "Dismissal and Terminal Cleanup" 的退出命令一句按 EXPECTED_OUTCOME 同步.

## THREAT_MODEL
N/A

## OUT_OF_SCOPE
- 已有问题: herdr 对 agent 类型的识别由 herdr 自身完成, 配置不为其安装识别器, 既有限制不变; 排除.
- 并发与跨平台加固: dismiss 的超时, 容器拓扑复核与 Windows 路径不改; 排除.
- 共享契约与文档: `docs/custom-agents.md` 纳入; 发布规则 `rules/KANDER-KANBAN-RULES.md` 的退出命令一句与会话模式清单纳入; 部分纳入.
- 相邻功能与后续阶段: 审核模板属 `20260908-agent-review-template-task`; 终端侧钩子 (herdr socket) 属终端定义组; 把 agent 钩子与终端钩子合并为同一注册表属后续阶段; 排除.

## DISCUSSION
```text
PREREQUISITES: 20260908-agent-definition-embed-task,20260908-agent-review-template-task
```

- 设计结论: 钩子只在 "无法用 argv 模板 + 三种输出原语表达" 时保留. Cursor 的 create-chat 现由 `internal/launch/session.go:39` 附近直接 `cmd.Run()` 调用, 没有专门的超时 (10 秒上限属通用 `allocated` 路径); 它理论上可用 `allocated` 表达, 本卡先以钩子保留现有行为, 是否改为 `allocated` 由执行者在 IMPLEMENTATION 记录评估结论, 不强制.
- 顺序: 按 A1 记录的用户决策 "格式与嵌入 -> 审核模板 -> 退出命令与会话钩子" 执行, 本卡依赖 A2, 不与其并行; 本卡作为组内最后一张负责核验组级 "按名分支归零".

- SELF_REVIEW: 修正一处: 原第 5 条验收 "合计后为零" 只计入 A1, 漏计 A2 负责的 review 包分支, 已改为三卡合计并允许本卡单独验收时 review 包保留分支. 其余目标, 边界, 验收一致; 钩子清单与文档漂移测试可判定.
- CARD_REVIEW (第一轮, 建卡时): 需修正后通过 (独立 agent). 发现: "三卡合计为零" 依赖 A1 清理 install/menu 分支, A1 已补相应字段后成立; 同时补入 config/agents.go 按 dialect 推导 session 的分支删除, 验收范围加入 config 包. 其余事实 (takeover 退出命令表, 发布规则原句, 文档章节) 核对无误.
- 计划: 本卡改为 large (跨 takeover, launch, notify, liveness, config 五包清除会话业务分支并引入钩子注册表), 执行者启动时先写 `plan.md`, 建议阶段: (1) `exit_command` 字段与 takeover 迁移; (2) 钩子注册表与 `hook:<name>`; (3) codex/cursor 定义改用钩子并跑通现有发现/分配测试; (4) config 的 dialect 推导删除; (5) 组级 grep 核验; (6) 规则与文档.
- CARD_REVIEW: 需修正后通过 (Codex gpt-6-astra, 独立只读会话, 2026-09-08 对 backlog 全部 12 张卡的独立审核). 发现: (1) 只依赖 A1 并允许与 A2 并行, 违反 A1 记录的用户顺序, 已加 A2 为前置并删除并行表述; (2) "三卡合计归零" 的 review 包残留分支 (`review/execute.go:116`, `:183`, `validate.go:107`) 责任未归属, 已归入 A2 验收, 本卡只核验; (3) Cursor create-chat "有专门超时" 不符源码, 已改准; (4) 会话模式清单的规则同步遗漏, 已补; (5) 改 large. 五项已修正.

- SELF_REVIEW (2026-09-09, 依赖变更后复核): 通过. 本轮只改 PREREQUISITES: 因同组拆出 `20260909-process-output-parser-task`, 先加后删该直接边 —— 本卡正文没有任何需要公共解析器的依据, 且已依赖的 `20260908-agent-review-template-task` 的传递闭包已覆盖它, 直接边冗余. GOAL / EXPECTED_OUTCOME / ACCEPTANCE_CRITERIA 与 OUT_OF_SCOPE 未改动. 无待决的用户问题.
- CARD_REVIEW (2026-09-09): 通过 (独立子代理, 新会话, 只读本组卡片与用户原始需求, 2026-09-09 共四轮往返, 末轮对本卡全卡通读后给出). 结论: `exit_command`、`session.mode: hook:<name>`、钩子注册表单点维护与防文档漂移测试、组级归零 grep (含 review 包残留退回对应卡而不代为清理)、两处发布规则同步, 目标结果验收一一对应且可判定; 无公共解析结构复述, 无过期交叉引用; 虽为 `docs/custom-agents.md` 的第三个写方, 但按依赖链严格排在前两张之后, 无并行写冲突. 本轮提出的唯一一条 (对新卡的直接依赖冗余且卡内无依据) 已修正.

# 20260908-agent-review-template-task

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

# 20260909-process-output-parser-task

## GOAL
在 `internal/process` 交付一个供 agent 审核模板与终端定义共用的声明式输出解析结构, 及其校验、解析实现与单元测试. 本卡是该结构的**权威描述**: 组内其他卡与终端组的卡只引用本卡, 不复述字段集合.

本卡从 `20260908-agent-review-template-task` 拆出. 拆点是干净的: 该结构对现有代码是**纯新增** —— 没有调用方行为改变, 没有既有 `_test.go` 期望值改写, 没有发布规则同步. 它与审核模板迁移之间只有"被调用"这一条边. 拆开后 `20260908-terminal-definition-format-task` 只需依赖本卡, 而不必等整张审核模板卡 (含 reviewer 名单开放与四份规则改写) 合入.

## USER_DECISIONS
- 声明式定义文件是扩展 agent 与终端的主要方式; 输出解析只有三种原语; 不做 shell 插值, 占位符逐元素替换; 这些边界写入文档作为不可扩大的契约.
- 把公共解析结构从审核模板卡拆出单独交付, 并把终端格式卡的依赖改为只依赖本卡 (2026-09-09 确认).
- 2026-09-09 授权启动本组: 按依赖启动执行, 任务分支交付组分支 group/20260908-agent-definition-group, 组完成后快进合入 develop 并 wrap-up.

## EXPECTED_OUTCOME
- `internal/process` 提供该结构的 Go 类型、校验函数与解析函数. **本卡是权威描述**, 结构如下:

```text
{"source":  "stdout" | "stderr" | "file",
 "format":  "json" | "ndjson",                                  // 缺省 json
 "select":  [<行条件>, ...],                                     // 仅 ndjson, 可选
 "parse":   "raw" | "json_field:<dotted.path>" | "regex:<pattern>",
 "join":    "<字符串>",                                          // 仅 ndjson, 缺省 "\n"
 "success": [<行条件>, ...]}                                     // 可选
```

- `<行条件>` 是声明式条件词汇, 两种形态: `{"json_field": "<dotted.path>", "equals": <JSON 值>}` 与 `{"json_field": "<dotted.path>", "absent": true}`. `select` 与 `success` 共用同一词汇与同一求值实现.
- `source` 说明读标准输出、标准错误还是读调用方指定的结果文件. `parse` 是三种解析原语之一 (原样, JSON 字段路径, 正则单捕获组). **原语集合固定为三种; `format` / `select` / `join` 都不是第四种原语**, 它们描述的是这份输出怎么切分、哪些片段算数、怎么合并, 三种原语在每个片段上原样套用.
- 调用方只限制自己适用的 `source` 子集 (审核模板用 `stdout` | `file`, 终端定义用 `stdout` | `stderr`), 由本卡提供带子集限制的入口, 各调用方不各自定义一套解析器.
- `format: json` (缺省) 是整份输出作为一个文档, `parse` 对它求值一次; 同时出现 `select` 或 `join` 时校验拒绝. 文档不能解析为 JSON 时, 已声明的 `success` 条件一律判失败 (不是跳过, 也不是恒真).
- `format: ndjson` 的求值顺序固定为 切分 -> 筛选 -> 提取 -> 合并:
  1. 按行切分; 空行忽略; 不是合法 JSON 的行 (含被截断的尾行) 跳过, 不算失败.
  2. 有 `select` 时, 只有**同时满足全部** `select` 条件的行进入提取; 无 `select` 时全部行进入. 没有按行筛选是不够的: 真实的流里, 工具结果行与元信息行常常带着与报告行**同名**的字段 (例如同时存在 `{"role":"assistant","content":...}`, `{"role":"tool","content":...}` 与 `{"role":"meta","type":"session.resume_hint","content":"To resume this session: ..."}`), 只靠"路径缺席就跳过"会把工具结果全文和会话提示拼进结果.
  3. 对每个进入的行套用 `parse`; 路径缺席或正则不匹配的行跳过, 不算失败.
  4. 命中的结果按出现顺序用 `join` 连接, 缺省 `"\n"`. **不把"无分隔符"写死成契约**: ndjson 的一行通常是一条完整消息而不是 token 增量, 直连会把上一条的结尾与下一条的 Markdown 标题粘成一行, 破坏结构.
- `format: ndjson` 要求 `parse` 为 `json_field:` 或 `regex:`; `parse: raw` 与 ndjson 的组合被校验拒绝 (逐行取原文再连接等于原样输出, 且使 `select` 失去意义), 错误信息指出该组合无效.
- 成功判定与文本提取分离: `success` 成立才视为正常完成, 之后才按 `parse` 提取文本; 提取到文本但成功条件不成立时结果被拒绝.
  - `format: json` 时全部条件对同一个文档求值.
  - `format: ndjson` 时判据是**存在某一行同时满足全部条件** (行内合取, 行间存在量词), 而不是"每条条件各自找一行满足". 后者会让一条 `is_error=true` 的失败行与另一条 `is_error=false` 的行凑成通过; 同理 `absent: true` 只在同一行内求值, 否则任何一条不含该路径的行都能让它恒真.
  - `success` 对**全部**行求值, 不受 `select` 限制: 成功信号常常落在被 `select` 滤掉的元信息行或结果行上.
  - 未声明成功条件时, 由调用方以退出码与提取/拼接后文本非空兜底.
- 本卡同时是**占位符与转义规则的权威描述**: 白名单内的 `{name}` 是占位符, 未知占位符拒绝; 字面花括号用 `{{` / `}}` 转义. 该规则对 argv 元素模板与多行文本模板同样适用, 由调用方提供各自的占位符白名单. 终端定义卡已按同一规则设计, 改为引用本卡.
- 结构、校验与解析的实现和测试全部位于 `internal/process`; 本卡不修改 `internal/review`、`internal/terminal`、`internal/config` 的任何现有行为, 不新增调用点.
- 本卡自带文档落点 `docs/output-parsing.md`, 描述该结构、行条件词汇、四步求值、占位符与 `{{` / `}}` 转义规则. 引用方 (`20260908-agent-definition-embed-task` 的 `docs/custom-agents.md`、`20260908-agent-review-template-task`、`20260908-terminal-definition-format-task` 的 `docs/terminal-definitions.md`) 一律只链接这份文档, 不复述. 权威描述必须落在**随仓库发布的文档**里: 看板目录不进 Git, 卡片进 `done/` 之后对使用者不可见, 把权威描述只留在卡上等于没有.

## ACCEPTANCE_CRITERIA
- [ ] `internal/process` 提供该结构的类型与校验函数; 校验覆盖: `source` / `format` / `parse` 取值集合与正则可编译, `select` 与 `join` 只在 `ndjson` 下接受, `ndjson` 拒绝 `parse: raw`, `json` 下出现 `select`/`join` 拒绝, 行条件的结构与 JSON 值类型, 占位符白名单与 `{{`/`}}` 转义, 控制字符与空元素规则; 错误信息含字段名与非法值.
- [ ] 带 `source` 子集限制的入口存在并各有测试: 审核子集拒绝 `stderr`, 终端子集拒绝 `file`.
- [ ] `format: ndjson` 的四步求值有独立单元测试: 空行、非法 JSON 行与被截断的尾行被跳过而不判失败; `join` 缺省为 `"\n"` 且可被覆盖; 求值顺序为 切分 -> 筛选 -> 提取 -> 合并.
- [ ] `select` 的按行筛选有针对污染场景的用例: 构造的输入**必须**同时包含一条带 `content` 的工具结果行与一条带 `content` 的元信息行 (不得把它们写成缺少该字段, 否则用例通不过也测不出问题), 断言二者的正文都不出现在最终结果中, 只有满足 `select` 的行按序出现.
- [ ] ndjson 的成功判定按"存在某一行同时满足全部条件": 用例包含"条件分散在不同行" (一行 `is_error=true`, 另一行 `is_error=false`) 时判失败, 同一行同时满足全部条件时通过; `absent: true` 在同一行内求值, 不因存在不含该路径的其他行而恒真; `success` 对全部行求值, 含"成功信号落在被 `select` 滤掉的行上仍然成立"的用例.
- [ ] `format: json` 且文档不能解析为 JSON 时, 已声明的 `success` 一律判失败, 有用例.
- [ ] 占位符与转义有独立用例: 含字面 `{...}` 代码块的多行文本模板按 `{{`/`}}` 转义后逐字节还原; 未知占位符拒绝且错误含该占位符名.
- [ ] `plan.md` 内以表格形式固定纸面验证: 四个内置 reviewer 现有的解析方式与成功判定 (Codex 读结果文件原样, Claude/Cursor 的 `type`/`subtype`/`is_error`, Grok 的 `stopReason`), 以及带同名 `content` 字段的 ndjson 污染场景, 逐行给出用本结构的表达方式; 出现表达不了的情形时扩展本结构并在本卡记录, 而不是留给调用方回退 Go 分支. 实际迁移不在本卡.
- [ ] `docs/output-parsing.md` 新增, 内容覆盖结构、行条件词汇、四步求值、成功判定、占位符与 `{{` / `}}` 转义规则; `AGENTS.md` 包表更新. 本卡不改动 `docs/custom-agents.md` 与 `docs/terminal-definitions.md`.
- [ ] `git diff --stat` 显示本卡只改动 `internal/process`, `docs/output-parsing.md`, `AGENTS.md` 与卡片附件; `internal/review`, `internal/terminal`, `internal/config`, `internal/launch` 无改动.
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` 通过; `GOOS=windows go build ./...` 通过.

## THREAT_MODEL
本卡只新增一个纯函数式的解析与校验结构, 输入是调用方已经取得的进程输出与本机用户声明的定义文本. 不新增进程启动、文件写入、网络访问或权限边界. 正则由定义作者提供, 在校验期编译, 编译失败即拒绝; 不引入脚本能力, 不做 shell 插值. 不构成新的用户间权限边界.

## OUT_OF_SCOPE
- 已有问题: 现有 reviewer 与终端后端的解析代码不在本卡改动, 保持原样直到各自的迁移卡; 排除.
- 并发与跨平台加固: 纯解析逻辑无并发写; Windows 只要求交叉编译通过; 排除.
- 共享契约与文档: 本卡是该结构与占位符转义规则的权威描述, 并自带文档落点 `docs/output-parsing.md`; 本卡**不修改** `docs/custom-agents.md` 与 `docs/terminal-definitions.md` (分别由 `20260908-agent-definition-embed-task` 与 `20260908-terminal-definition-format-task` 拥有), 因此在文档正文上与它们无写冲突; `AGENTS.md` 包表随 `internal/process` 的新增内容更新 —— 该文件另有两个写方 (`20260908-agent-definition-embed-task` 与 `20260908-terminal-definition-format-task` 各自追加不同条目), 语义不冲突但并行会撞 Git, 分派时该文件按追加处理或与两卡串行; 发布规则 `rules/*.md` 不涉及本结构的内部细节, 不改; 部分纳入.
- 相邻功能与后续阶段: 审核模板字段、`review.stdin` / `review.prompt_files`、reviewer 名单开放与规则同步属 `20260908-agent-review-template-task`; 终端定义的 steps/poll/fields 属 `20260908-terminal-definition-format-task`; 排除.

## DISCUSSION
```text
PREREQUISITES: N/A
```

- 本卡从 `20260908-agent-review-template-task` 拆出, 拆点为 `internal/process` 公共解析结构. 拆分理由与"权威描述"身份的迁移记录在 `20260908-agent-definition-embed-task` 的 DISCUSSION (组内第一张卡).
- 本卡对现有代码是纯新增, 不依赖 `20260908-agent-definition-embed-task` 的定义加载器, 因此 `PREREQUISITES: N/A`, 可与该卡并行. 并行成立的前提是文档不撞车: 本卡自带 `docs/output-parsing.md`, 不碰 `docs/custom-agents.md`; 唯一的共同写入面是 `AGENTS.md` 包表, 按 OUT_OF_SCOPE 的追加/串行口径处理.
- 设计结论: `format` / `select` / `join` 不违反"输出解析只有三种原语"这条用户决策 —— `parse` 的取值集合没有变, 三种原语在每个片段上原样套用; 新增的三项描述的是切分、筛选与合并, 属于流形态而不是解析能力.
- 设计结论: `select` 与 `join` 是让 `format` 真正可用的必要条件, 缺一个 `format` 就是死字段. 已知存在这样一类 CLI —— 把整段报告拆成逐行 assistant 消息, 中间混杂工具调用行与元信息行, 没有 result 信封, 且工具结果行与元信息行带着与报告行同名的 `content` 字段. 对这种输出, 三种原语中任何一种单用都取不出干净结果: 不筛选会把工具结果全文与会话恢复提示拼进去, 无分隔符直连会破坏 Markdown 结构.
- 设计结论: 把占位符与 `{{`/`}}` 转义规则一并收在本卡, 是因为多行文本模板 (Markdown + YAML frontmatter) 里字面花括号极常见, 而 argv 元素模板与文本模板必须用同一套规则, 否则又是一个分叉源.

- SELF_REVIEW: 通过 (2026-09-09, 建卡). 目标 (交付公共解析结构并承接权威描述) 与结果一致; USER_DECISIONS 只记录用户已确认的三条 (声明式为主与三种原语的既有边界、把公共结构拆出单独交付、本轮只建卡); 边界把审核模板迁移与终端 steps 分别留给对应卡, 并用 `git diff --stat` 断言把 "纯新增" 钉死; 验收覆盖四步求值、按行筛选的污染场景、行内合取的成功判定、子集入口、转义规则与文档落点, 均可判定. 无待决的用户问题.
- CARD_REVIEW: 需修正后通过 (2026-09-09; 独立子代理, 新会话, 只读本组卡片与用户原始需求, 2026-09-09 共四轮往返). 发现并已修正: (1) 权威描述原本落到一份没有任何卡产出的文档上, 四张卡形成闭环空指针, 且看板目录不进 Git、卡片进 `done/` 后对使用者不可见, 已改为本卡自带 `docs/output-parsing.md` 并加独立验收; (2) "可与 A1 并行" 与 "文档随 A1 落地" 自相矛盾, 随 (1) 一并解除; (3) "与两份文档无写冲突可并行" 的断言漏了 `AGENTS.md` 这个三方写入面, 已收窄为文档正文无冲突、`AGENTS.md` 按追加或串行处理. 审核者对 SIZE 评 large 给出明确支持意见 (跨两组的共同契约, 先在 `plan.md` 摆一遍四个内置 reviewer 的表达再动手), 予以保留. 末轮列出的第 (3) 条已按其处方修正后发布, 未再复审.
