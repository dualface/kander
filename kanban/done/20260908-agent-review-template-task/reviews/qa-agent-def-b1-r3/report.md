# QA 审核报告

**Role**: QA(质量负责人:功能正确性、回归控制、可测性、可维护性、架构契合)
**Commit**: `e1c01277075599cb0395bf62f16855cebf04b060`(基线 `8abdfe2e6430a00e19383f84f23088a8ff743be0`,8 提交 / 58 文件)
**Task Context**: `/tmp/claude-review.057227079be53daee7dfb6bede4fb955/task-spec.md` 合并的三张卡:`20260909-process-output-parser-task`(公共输出解析结构)、`20260908-agent-definition-embed-task`(内置 agent 定义嵌入 + `prompt_delivery`)、`20260908-agent-review-template-task`(审核模板声明化 + reviewer 名单开放)。A3 退出命令/会话钩子不在本区间。

**Reviewed Scope**

- 以 COMMIT TREE 为准,通读了 `internal/process/{output,template}.go`、`internal/config/{agents,agents_embed,agents_review,config,format,repair}.go` 与四份嵌入定义、`internal/launch/{agent,prompt_delivery,session,start,notify_resume,commands,types,tmux}.go`、`internal/review/{args,settings,execute,validate}.go`、`internal/install/integrate.go`、`internal/menu/*`,以及 `AGENTS.md`、`docs/output-parsing.md`、`docs/custom-agents.md`、三份 `rules/*.md` 与本区间新增/改动的测试。
- 对改动前实现逐项对照:`git show 8abdfe2e:internal/review/args.go`、`:internal/review/settings.go`、`:internal/launch/session.go`、`:internal/config/config.go`。
- 我可以执行只读命令,并在该 worktree 实跑:`go build ./...`、`go vet ./...`、`GOOS=windows go build ./...`、`go test ./...` 全部通过;运行前后 `git rev-parse HEAD` 未变、`git status --porcelain` 为空。另跑了 `gofmt -l`(结果见 NON-BLOCKING)。
- 未评估性能;`plan.md` 属看板附件、不在 COMMIT TREE,该项验收(纸面表格)标记为 Unverifiable。
- 尺寸硬规则:本区间无非生成源码文件超过 1000 行(最大 `internal/config/config.go` 773 行),不触发。

## 行为 / 质量核对表

| 需求(卡片来源) | 结论 | 依据 |
| --- | --- | --- |
| 三种解析原语 + `format`/`select`/`join` 流形态;ndjson 四步求值 切分→筛选→提取→合并 | Observed 通过 | `internal/process/output.go:277-313`;`output_test.go:124-157` 覆盖空行/非法行/截断尾行/`join` 覆盖 |
| `select` 按行筛选防同名 `content` 污染 | Observed 通过 | `output.go:302-311`;`output_test.go:159-182` 输入含带 `content` 的 tool 行与 `session.resume_hint` 元信息行 |
| ndjson 成功判定为「存在某一行同时满足全部条件」,`absent` 行内求值,`success` 不受 `select` 限制 | Observed 通过 | `output.go:289-300`(先于 select 遍历全部行);`output_test.go:184-253` |
| `format: json` 文档不可解析为 JSON 时已声明 `success` 判失败 | Observed 通过 | `output.go:258-270`;`output_test.go:255-268` |
| `ndjson` 拒绝 `parse: raw`;`json` 下 `select`/`join` 拒绝;子集入口拒 `stderr`/`file` | Observed 通过 | `output.go:104-149,165-191`;`output_test.go:21-122` |
| 占位符白名单 + `{{`/`}}` 转义,argv/文本两套控制字符规则 | Observed 通过 | `template.go:55-102,169-187`;`template_test.go` |
| 四份嵌入定义存在、`schema_version` 1、加载校验含文件名与字段 | Observed 通过 | `agents_embed.go:131-186`;`agents/*.json` |
| 内置 start/resume argv 与改动前逐项一致(含 `session.mode=none` 丢弃 flag) | Observed 通过 | `session.go:367-385` + `agents.go:293-316`;`builtin_argv_test.go`;与 `git show 8abdfe2e:internal/launch/session.go` 逐分支比对一致 |
| `agentArguments` 无按名分支;config/launch/install/menu 剩余按名分支在允许清单内 | Observed 通过 | `grep -rn '"codex"\|"claude"\|"grok"\|"cursor"' internal/{config,launch,install,menu} --include='*.go'` 仅剩 `session.go:90`、`agents.go:104,106,110,267` |
| `prompt_delivery` 校验、`LaunchPlan` 分发、`launchAgent` 签名未加提示词回调 | Observed 通过 | `agents_embed.go:328-368`;`types.go:54-55`;`agent.go:50-54,74-83,129-138` |
| 两段就绪、`blocked` 并行且短路优先、三种失败原因 | Observed 通过 | `prompt_delivery.go:66-120`;`prompt_delivery_test.go:158-266`(含耗时远小于 `timeout_ms` 断言) |
| `mode: pane` 在 foreground/console 于 claim 前拒绝 | Observed 通过 | `prompt_delivery.go:37-45`,`start.go:66`;`prompt_delivery_test.go:43-86` |
| 三种失败的统一处置(抓 pane 输出 + 关容器 + 回滚,不留 `WINDOW`/`SESSION`) | **Observed 不完整** | `start` 路径成立;durable(DISPATCH_ID / authorization)路径不成立,见 **QA-01** |
| `notify` 对已存在容器直投不等 `ready` | Observed 通过 | `internal/notify/pane_ready_test.go` |
| 审核 argv/env/cwd/home/output_name/inspection/helpers/snapshot 全部读定义,与改动前一致 | Observed 通过 | `review/args.go:42-74`、`settings.go:137-231`;`review/definition_test.go:14-118` 用改动前实现重算期望值 |
| 成功判定先于文本提取,四个内置 agent 判定与改动前逐项一致 | Observed 通过 | `args.go:112-134`;`definition_test.go:159-207` |
| `review.stdin` / `{instruction}` / `review.prompt_files` / `{prompt_file:<name>}` 校验与端到端 | Observed 基本通过,**嵌套 `path` 有缺口** | `agents_review.go:165-231`;`definition_unix_test.go:181-224`;缺口见 **QA-02** |
| `ReviewAgents` 删除,`reviewers.<role>` 改为「定义了 `args.review`」,doctor 不擅自改写 | Observed 通过 | `config.go:650-660`、`repair.go:131-140`、`agents_review.go:63-91`;`agents_review_test.go` |
| 审核可执行名优先级(`*_REVIEW_BIN` > 嵌入 `path`;自定义回落 `path`) | Observed 通过 | `agents_review.go:96-112`;`review/agent_executable_test.go` |
| `kander config --json` 默认输出与改动前逐字节一致 | Observed 通过 | `internal/config/testdata/default-config.json` + `agents_embed_test.go:13-36`,内容与 `git show 8abdfe2e:internal/config/config.go` 的三张表逐字段一致 |
| 安装器目标路径/写入内容/去重与改动前一致 | Observed 通过 | `integrate.go:36-112`:project 模式新列表按 target 去重后仍为 `claude→CLAUDE.md`、`codex→AGENTS.md`;global 模式顺序与过滤条件不变 |
| 规则四处承诺同步(KANBAN)+ REVIEW/BASE 名单开放 | Observed 通过 | `rules/KANDER-KANBAN-RULES.md` 逐条给出改后原文且 `mode: argv` 承诺逐字未变;`rules_prompt_delivery_test.go` 固定 |
| 文档落点(`docs/output-parsing.md` 新增、`custom-agents.md` 只链接、`AGENTS.md` 包表) | Observed 通过,**有一处描述与实现不符** | 见 **QA-03** |
| `plan.md` 四个内置 reviewer 的纸面表达表 | Unverifiable | 看板目录不在 COMMIT TREE;`output_test.go:270-327` `TestPaperReviewerExpressions` 以代码形式覆盖了同一组表达 |
| `go build` / `go vet` / `go test` / `GOOS=windows go build` | Observed 通过 | 本机实跑,全部通过 |

## 门禁发现(FINDINGS)

### QA-01 [medium] `mode: pane` 投递失败在 durable 派发路径上被判为「投递结果未知」,容器不关、卡片不回滚

- **Claim**: Observed
- **证据**:
  - `internal/launch/agent.go:27-38` — `fail` 闭包:`if sendAttempted && len(durable) > 0 && durable[0] { return &LaunchFailure{Err: err, DeliveryUnknown: true} }`,该分支**直接返回,不执行**后面的 `herdrCloseTab` / `tmuxCloseWindow`。
  - `internal/launch/agent.go:70` 与 `:125` — `sendAttempted = true` 分别设在 `herdrPaneRun` / `tmuxStartPane` **之前**。
  - `internal/launch/agent.go:74-77` 与 `:129-132` — `completePaneDelivery` 在其**之后**才执行。
  - `internal/launch/prompt_delivery.go:47-53` — `attachPrompt` 在 `mode: pane` 下**不把提示词放进 argv**,因此 `pane run` 阶段客观上没有投递任何提示词。
  - `internal/launch/notify_resume.go:127-137` — `durable := board.MetadataFrom(originalText, "DISPATCH_ID") != ""`;`if failure.DeliveryUnknown { taskFileHandedOff = true; return ResumeLaunch{}, err }`,无 `cleanupFailedResume`、无 `window.RestoreWindowText`。
  - `internal/launch/commands.go:289-295` — 同样以 `len(authorization) > 0` 作为 durable,`DeliveryUnknown` 时直接 `return err`,跳过 `rollbackLaunch`。
- **被违反的契约(本区间自己新增的)**:
  - `rules/KANDER-KANBAN-RULES.md`「Start Checks and Rollback」本次新增句:"For `prompt_delivery.mode` `pane`, a second-stage TUI ready failure (`blocked` match, prompt-delivery rejection, or ready timeout) is the same class of failure: capture the pane output, close this invocation's tab/window, restore the document, and move back to `todo/`. Do not keep the container or write `WINDOW`/`SESSION` from that attempt."
  - `docs/custom-agents.md` 本次新增句:"A pane-mode failure … captures the pane output, closes this invocation's tab/window, and rolls the card back."
  - 卡片 `20260908-agent-definition-embed-task` EXPECTED_OUTCOME:「`notify` 走恢复通道新建容器时与 `resume` 同」「不保留容器, 不写 `WINDOW`」。
- **可达失败场景**:一张带 `DISPATCH_ID` 的卡片,执行 agent 声明 `prompt_delivery.mode: pane`,`kander notify` 需要走恢复通道新建容器(herdr 或 tmux)。目标 CLI 首次进入该目录弹出信任确认对话框 → `waitAgentTUI` 命中 `blocked`(`prompt_delivery.go:96`)→ `completePaneDelivery` 返回错误 → `fail` 因 `sendAttempted==true && durable==true` 返回 `DeliveryUnknown:true` → 新建的 tab/window 不被关闭,卡片文本不回滚,`taskFileHandedOff` 被置为 true(任务文件不再清理)。而提示词从未送达任何进程(它不在 argv 里,`agent prompt` / `send-keys` 也没跑到)。`ready` 纯超时与 `agent prompt` 被拒绝两条路径同理。
- **影响**:遗留孤儿容器 + 卡片停在原状态且无 `SESSION`,正是卡片 DISCUSSION 明确要消除的「三条恢复路都不认」的状态;同时使本区间刚写入已发布规则的承诺与实现不符。
- **最小修复**:让 `sendAttempted` 只在提示词确实可能已经出去时为真——`mode: argv` 时维持现状(`pane run` / `tmuxStartPane` 前置位),`mode: pane` 时改为在 `completePaneDelivery` 成功之后再置 `true`。例如把 `agent.go:70` / `:125` 改成 `sendAttempted = !paneDelivery`,并在 `agent.go:77` / `:132` 的 `completePaneDelivery` 成功分支内补 `sendAttempted = true`。
- **测试建议(最便宜层)**:`internal/launch/prompt_delivery_test.go` 复用现有 herdr 假桩,增一例:卡片带 `DISPATCH_ID`、走 `commandResume(..., authorization)` 或 `NotifyViaResume`,桩输出 `TRUST_DIALOG`,断言 `herdr-*.log.close` 存在且卡片文本还原——与现有 `assertPaneStartRolledBack` 同一断言集。

### QA-02 [medium] `review.prompt_files[].path` 允许带子目录,但渲染时不建父目录,审核在启动前失败

- **Claim**: Observed
- **证据**:
  - `internal/config/agents_review.go:242-257` `validReviewPromptPath`:只拒绝空串/控制字符/绝对路径/反斜杠/`..`/非规范形式;`"agents/reviewer.md"` 通过校验。
  - `internal/review/execute.go:171-172`:`dest := filepath.Join(runtime, filepath.FromSlash(file.Path))`,随后 `fs.WriteTextAtomic(runtime, dest, rendered, false)`,**没有任何 `MkdirAll`**。
  - `internal/fs/posix.go:63-84` `openPosixParent`:对 `parts[:len(parts)-1]` 逐段 `openat(dirfd, part, O_RDONLY|O_DIRECTORY)`,父目录不存在即 `mapOpenErr` 失败(Windows 侧 `windows_ops.go` 同构)。
- **可达失败场景**:自定义 reviewer 按卡片给出的动机(「只读白名单放在提示词文件自身的格式里,一份带 YAML frontmatter 与工具白名单的 Markdown」)声明 `{"name":"guide","path":".claude/agents/reviewer.md","template":"..."}`。`kander config --json` 与配置保存全部通过;真正跑 `kander review` 时在 `execute.go:172` 以一条底层 openat 错误失败(不含字段名、不指出是 `review.prompt_files` 的路径问题),审核无法执行。
- **影响**:声明期通过、运行期失败,且诊断信息不指向定义字段;卡片要求的「按序渲染进本轮 runtime」对任何嵌套路径都不成立。现有端到端用例只用了扁平的 `guide.md`(`internal/review/definition_unix_test.go:199`),因此未暴露。
- **最小修复**:在 `execute.go:171` 与写入之间补一次受 runtime 约束的父目录创建(复用 `internal/fs` 的受保护建目录原语);若不打算支持子目录,则改为在 `validReviewPromptPath` 里显式拒绝含分隔符的路径,错误信息含 `review.prompt_files[i].path`。二选一即可,但校验与运行期必须一致。
- **测试建议**:在 `definition_unix_test.go` 的 `TestCustomReviewerStdinNonePromptFiles` 增一个 `path: "sub/guide.md"` 的子用例,断言渲染成功且权限 0600(或断言配置期即被拒绝)。

### QA-03 [medium] [mechanical] `docs/custom-agents.md` 的覆盖语义与 `review` / `args.review` 的成对校验不符

- **Claim**: Observed
- **证据**:
  - `docs/custom-agents.md:3`:"A user `agents.<name>` overlay replaces individual fields of that embedded definition (path, process name, dialect, argv templates, session, `prompt_delivery`, **review templates**); fields that are omitted keep the embedded values."
  - `internal/config/agents_review.go:143-150`:`hasArgs := d.Args != nil && d.Args.Review != nil; hasReview := d.Review != nil; if hasArgs != hasReview { return agentDefinitionError(name, Text("config.agent_review_pair")) }`。
  - `internal/config/agents.go:88-92`:`if d.Review == nil { d.Review = cloneReview(&emb.Review) }` —— 是整对象替换,不做字段级合并。
- **可达失败场景**:用户按文档只想改 claude 的一项审核设置,写 `{"agents":{"claude":{"review":{"inspection":"..."}}}}`。实现在 `validateAgentDefinitions`(`agents.go:276`)阶段直接报 `args.review and review must be declared together`,`kander config --json` 失败。要覆盖任意一个 `review.*` 子字段,用户必须原样重抄整个 `args.review` 数组以及 `review.cwd`/`output_name`/`output` 等全部必填项——文档没有任何一句提示这一点。
- **影响**:发布文档给出的覆盖模型与实现相反,用户按文档配置会拿到一条与其意图无关的错误;这也是本区间新写的文档段落。
- **最小修复**:在 `docs/custom-agents.md:3` 之后(或「Panel and Review Boundaries」审核小节)补一句:`review` 与 `args.review` 必须成对声明,且 `review` 是整对象替换而非字段级合并,覆盖任一子字段需重述整组。

### QA-04 [medium] [mechanical] 本区间引入的不可达 / 无引用代码

- **Claim**: Observed
- **证据与不可达论证**:
  1. `internal/process/output.go:432-433` + `:446-455` —— `extractText` 的 regex 分支里 `if input == "" && decodeErr == nil { input, _ = jsonStringOrRaw(doc) }` 永不成立:两个调用点分别是 `evaluateJSON`(`output.go:269`,`raw == data`,而 `data == ""` 必然使 `json.Unmarshal` 返回 "unexpected end of JSON input",即 `decodeErr != nil`)与 `evaluateNDJSON`(`output.go:306`,`row.raw` 已被 `strings.TrimSpace(line) == ""` 过滤,必非空,`decodeErr` 恒为 `nil`)。因此 `jsonStringOrRaw` 全仓库无任何可达调用。
  2. `internal/process/output.go:28-32` —— 导出变量 `ReviewSources` / `TerminalSources` 在 `internal/` 内(含 `_test.go`)零引用;子集限制实际由 `ValidateReviewOutput` / `ValidateTerminalOutput` 硬编码判断完成。
  3. `internal/config/agents.go:318-320` —— `RewriteKeepSession(template, dropEmpty)` 的 `if !dropEmpty { return template }` 分支不可达:唯一生产调用点 `internal/launch/session.go:380` 传 `true`,唯一测试调用点 `agents_embed_test.go:137` 也传 `true`。
- **影响**:三处均为本区间新增,现在就是维护噪声;(1) 还会让读者误以为 regex 原语对已解析文档有一条特殊路径,而契约文档(`docs/output-parsing.md:30-32`)只描述「对原始缓冲/行文本求值」。
- **最小修复**:删除 `jsonStringOrRaw` 与 `output.go:432-433` 的分支;删除 `ReviewSources` / `TerminalSources`(或在两个子集校验函数里改用它们,使其成为唯一事实来源);把 `RewriteKeepSession` 的 `dropEmpty` 参数去掉。

### QA-05 [medium] [mechanical] 统一审核失败文案后遗留的孤儿 i18n 条目

- **Claim**: Observed
- **证据**:`internal/review/args.go:136-138` 把 codex / grok 的专用文案统一为 `review.review_did_not_complete_with_review_text`(带 `{{.V0}}` 显示名,渲染结果与改动前逐字相同)。原两个键在三份目录里全部保留且**已无任何引用**:
  - `internal/i18n/locales/en.json:709,724`
  - `internal/i18n/locales/zh-CN.json:709,724`
  - `internal/i18n/locales/ja.json:709,724`
  全仓 `grep -rn "codex_review_did_not_complete\|grok_review_did_not_complete" --include='*.go'` 无命中。
- **影响**:6 条死目录项;后续译者/维护者会继续同步维护两条永不出现的文案,并可能据其误判 codex/grok 仍有专用失败路径。
- **最小修复**:从三份 locale 中删除 `review.codex_review_did_not_complete_with_review_text` 与 `review.grok_review_did_not_complete_with_review_text`。

### QA-06 [medium] [mechanical] `TestBuiltinReviewStdinMatchesPreviousInstruction` 的独有断言恒成立,其余断言与既有用例重复

- **Claim**: Observed
- **证据**:`internal/review/definition_test.go:211-231`。循环体内:
  - `got := process.TaskFileInstruction("Perform the QA review.", promptFile)`,与循环外的 `want := process.TaskFileInstruction("Perform the QA review.", promptFile)` **实参完全相同且不含 `agent`**,`if got != want` 永远为假——这是该用例相对其他用例唯一新增的断言,却对「四个内置 agent 的 stdin 内容与改动前逐字节一致」这一被测行为零断言。
  - 循环体剩下的 `settings.stdin != config.ReviewStdinInstruction || len(settings.promptFiles) != 0` 与 `internal/review/definition_test.go:64` 中 `TestBuiltinReviewDefinitionsMatchPrevious` 的同名断言完全重复(同样对四个内置 agent 全覆盖)。
- **影响**:该用例整体不提供任何独立覆盖,却承担着验收条款「表驱动测试固定它们的 stdin 内容与 `prompt.txt` 内容与改动前逐字节一致」的门面;真正提供该覆盖的是 `definition_unix_test.go:285` `TestBuiltinReviewStdinAndPromptBytes`(经假 codex 从 stdin 反解 `task file at <path>`)。用例存在会让后续维护者误以为该条款已被本文件固定。
- **最小修复**:删除该用例;或把它改成真正的差异断言——对每个 agent 读取审核实际写入的 stdin 文件内容并与 `process.TaskFileInstruction(...)+"\n"` 逐字节比较(即把 `definition_unix_test.go:285` 的隐式验证显式化),而不是把同一次函数调用与它自己比较。

## NON-BLOCKING

### QA-07 [recommend] 6 个本区间文件未经 gofmt,`make fmt-check` 会失败

- **证据**:在该 worktree 执行 `gofmt -l ./internal ./cmd` 输出:`internal/config/agents_review.go`、`internal/config/config.go`、`internal/menu/agent_fields_test.go`、`internal/review/agent_executable_test.go`、`internal/review/definition_unix_test.go`、`internal/review/settings.go`。全部在 FILE LEDGER 内;基线上对应文件是格式化的(`git show 8abdfe2e:internal/config/config.go` 经 `gofmt -l` 无输出)。典型未对齐处:`internal/config/config.go:31-33`(`TaskScales`/`ReviewRoles` 与 `ReviewStageModes` 的 `=` 未对齐)、`internal/config/agents_review.go:36`(`Output` 字段)、`internal/review/settings.go:71-74`(`program`/`tempRoot`/`instruction`/`promptFilePaths`)。
- **理由**:`Makefile:38-41` 定义了 `fmt-check` 目标(`gofmt -l .` 非空即 `exit 1`),这是本仓库既有的项目规则;该检查在本区间之前是绿的。
- **最小改动**:`make fmt` 或 `gofmt -w` 这 6 个文件。

### QA-08 [low] 四份嵌入定义里的 `session` 块被校验但从不消费

- **证据**:`internal/config/agents_embed.go:58`(字段)与 `:157`(取值校验)是 `embeddedAgent.Session` 的**仅有**引用;`internal/config/agents.go:101-113` 在 `d.Session == nil` 时用 `switch d.Dialect` 硬编码推导 `generated`/`discovered`/`allocated`,不读 `embeddedByName(...).Session`。
- **影响**:把 `internal/config/agents/codex.json` 的 `"session": {"mode": "discovered"}` 改成任何其他值,行为不变(当前四份文件的值恰好与推导结果一致,故今天无可观察差异)。这与卡片「加一个内置 agent = 加一份 JSON」的目标相抵,但 `agents.go` 的 dialect 推导分支被 EXPECTED_OUTCOME 末条明确允许保留至 `20260908-agent-exit-session-hook-task`,故不作门禁项。
- **最小改动**:A3 清理该分支时,把 `AgentFor` 的 session 兜底改为读 `embeddedByName(d.Dialect).Session`;在此之前可在 `agents_embed.go:58` 加一行注释说明该字段当前仅参与校验、由 A3 接管消费。

### QA-09 [suggest] argv「空值丢弃元素 + 丢弃紧邻独立 flag」的规则有两份实现

- **证据**:`internal/config/agents.go:293-316` `ExpandAgentArgs`(执行侧,额外支持 `{session=}`)与 `internal/process/template.go:106-129` `ExpandArgvOmitEmpty`(审核侧)。两处的 flag 判定条件逐字相同:`strings.HasPrefix(template[i-1], "-") && !strings.ContainsAny(template[i-1], "{}=") && len(out) > 0 && out[len(out)-1] == template[i-1]`(`agents.go:308` 与 `template.go:117`)。
- **理由**:`docs/custom-agents.md` 现在在执行模板与审核模板两节分别描述同一条规则,而实现也是两份;任一侧改动(例如支持 `--flag=value` 形式)不会自动同步到另一侧。
- **最小改动**:让 `ExpandAgentArgs` 复用 `process.ExpandArgvOmitEmpty`,把 `{session=}` 表达为「先 `RewriteKeepSession` 归一、再统一展开」,保留现有行为的表驱动测试作为回归网。

### QA-10 [low] codex 非归档审核的 stdout 多了一个换行

- **证据**:改动前 `git show 8abdfe2e:internal/review/args.go` 的 codex 分支无论是否归档都走 `_, _ = os.Stdout.Write(data); syncStream(os.Stdout)`;现在 `internal/review/args.go:126-133` 只有归档分支保持 `Write`,非归档分支走 `fmt.Println(text)`。codex 的 `output_name` 是 `output.txt`、`parse: raw`(`internal/config/agents/codex.json`),`text` 即结果文件原文(通常已以 `\n` 结尾),因此非归档的 `kander review codex` 结果末尾比改动前多一个空行。claude/cursor/grok 改动前后都是 `fmt.Println`,不受影响。
- **理由**:属「审核输出与改动前一致」的边角偏差,消费方一般不敏感,故不作门禁项。
- **最小改动**:把 `args.go:132` 的 `fmt.Println(text)` 改为与归档分支一致的 `os.Stdout.Write([]byte(text)); syncStream(os.Stdout)`,或仅在 `!strings.HasSuffix(text, "\n")` 时补换行。

```kander-findings
{"FINDINGS":[{"id":"QA-01","tier":"medium","category":"correctness","verdict":"CONFIRMED","short_summary":"pane 投递失败在 durable 路径不关容器不回滚","summary":"`mode: pane` 的第二段就绪失败在 durable 派发路径(notify 恢复通道 / 带 authorization 的 resume)上被 `sendAttempted` 判为 DeliveryUnknown,直接返回而不关闭本次创建的 tab/window、不回滚卡片,违反本区间自己写入 rules/KANDER-KANBAN-RULES.md 与 docs/custom-agents.md 的统一处置承诺。","failure_scenario":"一张带 DISPATCH_ID 的卡片,执行 agent 声明 prompt_delivery.mode=pane。kander notify 走恢复通道新建 herdr 容器,目标 CLI 首次进入该目录弹出信任确认对话框。waitAgentTUI 在 prompt_delivery.go:96 命中 blocked 并短路返回错误,completePaneDelivery 失败;此时 agent.go:70 早已把 sendAttempted 置为 true,而提示词根本不在 argv 里、agent prompt 也从未执行,fail 闭包(agent.go:27-38)因 sendAttempted && durable 命中 DeliveryUnknown 分支,直接 return,不执行 herdrCloseTab;notify_resume.go:131-134 收到 DeliveryUnknown 后置 taskFileHandedOff=true 并返回,不调用 cleanupFailedResume,也不 RestoreWindowText。结果是一个孤儿 herdr 容器 + 卡片文本未还原 + 任务文件不再清理,正是卡片明确要消除的状态。","file":"internal/launch/agent.go","line":70,"evidence":"internal/launch/agent.go:27-38(fail 闭包在 sendAttempted && durable[0] 时直接返回 DeliveryUnknown,不走 herdrCloseTab/tmuxCloseWindow);internal/launch/agent.go:70 与 :125(sendAttempted = true 设在 herdrPaneRun / tmuxStartPane 之前);internal/launch/agent.go:74-77 与 :129-132(completePaneDelivery 在其之后);internal/launch/prompt_delivery.go:47-53(attachPrompt 在 pane 模式下不把提示词放进 argv,故 pane run 阶段客观上未投递任何提示词);internal/launch/notify_resume.go:127-137(durable 来自 DISPATCH_ID;DeliveryUnknown 时只置 taskFileHandedOff 并返回);internal/launch/commands.go:289-295(len(authorization)>0 作为 durable,DeliveryUnknown 时跳过 rollbackLaunch)。被违反的契约:本区间新增的 rules/KANDER-KANBAN-RULES.md 'For prompt_delivery.mode pane, a second-stage TUI ready failure ... capture the pane output, close this invocation's tab/window, restore the document, and move back to todo/. Do not keep the container or write WINDOW/SESSION from that attempt.' 与 docs/custom-agents.md 的同义句,以及卡片 EXPECTED_OUTCOME 'notify 走恢复通道新建容器时与 resume 同'。最小修复:sendAttempted 只对 mode:argv 在 pane run 前置真(agent.go:70/:125 改为 sendAttempted = !paneDelivery),pane 模式改为在 completePaneDelivery 成功后再置真。现有 prompt_delivery_test.go 只覆盖 start(durable=false)与 resume 成功路径,未覆盖 durable 失败路径。"},{"id":"QA-02","tier":"medium","category":"correctness","verdict":"CONFIRMED","short_summary":"prompt_files 嵌套 path 通过校验但渲染时无父目录","summary":"review.prompt_files[].path 的校验接受带子目录的规范相对路径,但 execute.go 渲染时直接 WriteTextAtomic 且不创建父目录,而 internal/fs 的受保护写入要求父目录已存在,导致审核在启动前以底层 openat 错误失败。","failure_scenario":"自定义 reviewer 按卡片给出的动机(只读白名单写在提示词文件自身的 Markdown+YAML frontmatter 格式里)声明 review.prompt_files = [{\"name\":\"guide\",\"path\":\".claude/agents/reviewer.md\",\"template\":\"...\"}]。validReviewPromptPath 只检查空串/控制字符/绝对路径/反斜杠/..//非规范形式,该路径全部通过,kander config --json 与保存均成功。实际执行 kander review 时,execute.go:171 得到 <runtime>/.claude/agents/reviewer.md,fs.WriteTextAtomic -> openPosixParent 对 parts[:len-1] 逐段 openat(..., O_DIRECTORY),.claude 不存在即 ENOENT,审核以一条不含字段名的底层错误中止,reviewer 永远跑不起来。","file":"internal/review/execute.go","line":171,"evidence":"internal/config/agents_review.go:242-257(validReviewPromptPath 只拒绝空/控制字符/绝对路径/反斜杠/../非规范,未拒绝分隔符);internal/review/execute.go:171-172(dest := filepath.Join(runtime, filepath.FromSlash(file.Path)) 后直接 fs.WriteTextAtomic(runtime, dest, rendered, false),前后无任何 MkdirAll);internal/fs/posix.go:63-84(openPosixParent 对 parts[:len(parts)-1] 逐段 openat(dirfd, part, O_RDONLY|O_DIRECTORY),父目录缺失即 mapOpenErr 失败;internal/fs/windows_ops.go 同构)。现有端到端用例 internal/review/definition_unix_test.go:199 只用扁平的 guide.md,故未暴露。最小修复:写入前在 runtime 内创建受保护的父目录,或在 validReviewPromptPath 显式拒绝含分隔符的 path 并在错误信息里带上 review.prompt_files[i].path。"},{"id":"QA-03","tier":"medium","mechanical":"documentation","category":"documentation","verdict":"CONFIRMED","short_summary":"文档称 review 可按字段覆盖,实现要求成对声明","summary":"docs/custom-agents.md 描述用户 agents.<name> 覆盖为「按字段替换、省略的字段沿用嵌入值(含 review templates)」,而实现要求 args.review 与 review 必须成对声明,且 review 是整对象替换,不做字段级合并。","failure_scenario":"用户按 docs/custom-agents.md:3 的说明,只想改内置 claude 的一项审核设置,写入 {\"agents\":{\"claude\":{\"review\":{\"inspection\":\"...\"}}}}。validateAgentDefinitions(internal/config/agents.go:276)调用 validateReviewDefinition,hasArgs=false 而 hasReview=true,直接返回 config.agent_review_pair(\"args.review and review must be declared together\"),kander config --json 报错。用户要覆盖任一 review 子字段,必须原样重抄整个 args.review 数组以及 review.cwd / output_name / output 等全部必填项,而文档没有一句提示。","file":"docs/custom-agents.md","line":3,"evidence":"docs/custom-agents.md:3 'A user `agents.<name>` overlay replaces individual fields of that embedded definition (path, process name, dialect, argv templates, session, `prompt_delivery`, review templates); fields that are omitted keep the embedded values.' 与 internal/config/agents_review.go:143-150(hasArgs != hasReview 即报 config.agent_review_pair)以及 internal/config/agents.go:88-92(d.Review == nil 时整对象 cloneReview(&emb.Review),无字段级合并)矛盾。最小修复:在该段或「Panel and Review Boundaries」审核小节补一句,说明 review 与 args.review 必须成对声明且 review 为整对象替换,覆盖任一子字段需重述整组。"},{"id":"QA-04","tier":"medium","mechanical":"dead-code","category":"dead-code","verdict":"CONFIRMED","short_summary":"本区间新增三处不可达/零引用代码","summary":"本区间新增的 jsonStringOrRaw 及其守卫分支不可达,导出变量 ReviewSources / TerminalSources 全仓零引用,RewriteKeepSession 的 dropEmpty=false 分支不可达。","failure_scenario":"维护者阅读 internal/process/output.go 的 regex 原语时,会以为存在一条「原始文本为空则回退到已解析文档」的特殊路径,并据此在 docs/output-parsing.md 之外做出错误假设;实际该分支永远不执行。同理 ReviewSources / TerminalSources 看起来是子集的唯一事实来源,实际子集判断硬编码在 ValidateReviewOutput / ValidateTerminalOutput 内,两者可无声分叉。","file":"internal/process/output.go","line":432,"evidence":"(1) internal/process/output.go:432-433 的 `if input == \"\" && decodeErr == nil { input, _ = jsonStringOrRaw(doc) }` 不可达:调用点仅 output.go:269(evaluateJSON,raw==data;data==\"\" 必使 json.Unmarshal 返回 unexpected end of JSON input,即 decodeErr != nil)与 output.go:306(evaluateNDJSON,row.raw 已被 strings.TrimSpace(line)==\"\" 过滤必非空,decodeErr 恒 nil),因此 output.go:446-455 的 jsonStringOrRaw 无任何可达调用。(2) internal/process/output.go:28-32 的 ReviewSources / TerminalSources 在 internal/ 全目录(含 _test.go)零引用。(3) internal/config/agents.go:318-320 的 `if !dropEmpty { return template }` 不可达:唯一生产调用点 internal/launch/session.go:380 传 true,唯一测试调用点 internal/config/agents_embed_test.go:137 也传 true。最小修复:删除 jsonStringOrRaw 与 output.go:432-433 分支;删除 ReviewSources/TerminalSources 或改由两个子集校验函数引用它们;去掉 RewriteKeepSession 的 dropEmpty 参数。"},{"id":"QA-05","tier":"medium","mechanical":"dead-code","category":"dead-code","verdict":"CONFIRMED","short_summary":"统一审核失败文案后遗留 6 条孤儿 i18n 键","summary":"parseReviewOutput 统一为 review.review_did_not_complete_with_review_text 后,codex 与 grok 的专用文案键在三份 locale 中全部保留且已无任何引用。","failure_scenario":"后续译者维护 zh-CN/ja 目录时会继续翻译并校对这两条永不出现的文案;阅读目录的维护者会据此误判 review 包仍保留 codex/grok 的专用失败分支,而 internal/review/args.go:112-134 早已是单一统一路径。","file":"internal/i18n/locales/en.json","line":709,"evidence":"internal/review/args.go:136-138 只使用 review.review_did_not_complete_with_review_text(带 {{.V0}} 显示名,渲染结果与改动前逐字相同)。孤儿键位置:internal/i18n/locales/en.json:709,724;internal/i18n/locales/zh-CN.json:709,724;internal/i18n/locales/ja.json:709,724。全仓 grep -rn \"codex_review_did_not_complete\\|grok_review_did_not_complete\" --include='*.go' 无命中。最小修复:从三份 locale 删除 review.codex_review_did_not_complete_with_review_text 与 review.grok_review_did_not_complete_with_review_text。"},{"id":"QA-06","tier":"medium","mechanical":"redundant-test","category":"test-coverage","verdict":"CONFIRMED","short_summary":"该用例独有断言恒成立,其余断言与既有用例重复","summary":"TestBuiltinReviewStdinMatchesPreviousInstruction 的独有断言把同一次 TaskFileInstruction 调用结果与它自己比较(恒成立),其余断言与 TestBuiltinReviewDefinitionsMatchPrevious 完全重复,整体对被测行为零覆盖。","failure_scenario":"若 execute.go 停止把 instruction 写入 stdin,或写入内容与 process.TaskFileInstruction 的产物不再一致,该用例仍然通过——它从不读取审核实际写入的 stdin 文件。维护者据用例名(以及验收条款「表驱动测试固定它们的 stdin 内容与 prompt.txt 内容与改动前逐字节一致」)误以为该回归已被固定,而真正提供覆盖的是另一文件的用例。","file":"internal/review/definition_test.go","line":211,"evidence":"internal/review/definition_test.go:211-231:循环外 want := process.TaskFileInstruction(\"Perform the QA review.\", promptFile),循环体内 got := process.TaskFileInstruction(\"Perform the QA review.\", promptFile) —— 实参完全相同且不含 agent,`if got != want` 永远为假,这是该用例相对其他用例唯一新增的断言;循环体剩余的 settings.stdin != config.ReviewStdinInstruction || len(settings.promptFiles) != 0 与 internal/review/definition_test.go:64 中 TestBuiltinReviewDefinitionsMatchPrevious 的断言完全重复(同样对四个内置 agent 全覆盖)。真正的覆盖在 internal/review/definition_unix_test.go:285 TestBuiltinReviewStdinAndPromptBytes(经假 codex 从 stdin 反解 'task file at <path>')。最小修复:删除该用例,或改为读取审核实际写入的 stdin 文件内容与 process.TaskFileInstruction(...)+\"\\n\" 逐字节比较。"}],"NON_BLOCKING":[{"id":"QA-07","tier":"recommend","category":"project-convention","short_summary":"6 个本区间文件未 gofmt,make fmt-check 会失败","summary":"本区间有 6 个改动/新增文件未经 gofmt,使仓库既有的 make fmt-check 目标从绿变红。","failure_scenario":"在该 worktree 执行 make fmt-check 时 gofmt -l . 输出非空并 exit 1,阻断依赖该目标的本地或后续 CI 检查;基线 8abdfe2e 上该检查是通过的。","file":"internal/config/config.go","line":31,"evidence":"在 e1c0127 worktree 执行 gofmt -l ./internal ./cmd 输出:internal/config/agents_review.go、internal/config/config.go、internal/menu/agent_fields_test.go、internal/review/agent_executable_test.go、internal/review/definition_unix_test.go、internal/review/settings.go,均在 FILE LEDGER 内。典型未对齐处:internal/config/config.go:31-33(TaskScales / ReviewRoles 与 ReviewStageModes 的 = 未对齐)、internal/config/agents_review.go:36(Output 字段)、internal/review/settings.go:71-74(program/tempRoot/instruction/promptFilePaths)。基线对照:git show 8abdfe2e:internal/config/config.go 经 gofmt -l 无输出。项目规则来源:Makefile:38-41 的 fmt-check 目标。最小改动:对这 6 个文件执行 make fmt / gofmt -w。"},{"id":"QA-08","tier":"low","category":"maintainability","short_summary":"嵌入定义的 session 块被校验但从不消费","summary":"四份嵌入定义里的 session 字段只参与加载校验,解析时 AgentFor 仍按 dialect 硬编码推导 session 模式,修改 JSON 中的 session 不产生任何效果。","failure_scenario":"把 internal/config/agents/codex.json 的 \"session\": {\"mode\": \"discovered\"} 改成任意其他合法值,行为完全不变(当前四份文件的值恰与推导结果一致,故今天无可观察差异);后续按「加一个内置 agent 就是加一份 JSON」新增第五份定义时,其 session.mode 会被静默忽略并回落到 dialect switch 的 default 分支 generated。","file":"internal/config/agents_embed.go","line":58,"evidence":"internal/config/agents_embed.go:58(字段声明)与 :157(contains([]string{\"generated\",\"allocated\",\"none\",\"discovered\"}, agent.Session.Mode) 取值校验)是 embeddedAgent.Session 的仅有引用;internal/config/agents.go:101-113 在 d.Session == nil 时用 switch d.Dialect 硬编码 codex->discovered / cursor->allocated / default->generated,不读 embeddedByName(...).Session。该 dialect 推导分支被卡片 EXPECTED_OUTCOME 末条明确允许保留至 20260908-agent-exit-session-hook-task,故不作门禁项。最小改动:A3 清理该分支时把 session 兜底改为读 embeddedByName(d.Dialect).Session;在此之前在 agents_embed.go:58 加注释说明该字段当前仅参与校验。"},{"id":"QA-09","tier":"suggest","category":"duplication","short_summary":"argv 空值丢弃规则有两份等价实现","summary":"「空占位符丢弃该元素及紧邻独立 flag」的规则在执行侧与审核侧各有一份实现,flag 判定条件逐字重复。","failure_scenario":"任何一侧调整该规则(例如支持 --flag=value 形式,或改变 flag 判定条件)都不会同步到另一侧,执行模板与审核模板的占位符语义会静默分叉,而 docs/custom-agents.md 现在在两节里把它们描述为同一套规则。","file":"internal/config/agents.go","line":308,"evidence":"internal/config/agents.go:293-316 ExpandAgentArgs(执行侧,额外支持 {session=})与 internal/process/template.go:106-129 ExpandArgvOmitEmpty(审核侧);两处 flag 判定条件逐字相同:strings.HasPrefix(template[i-1], \"-\") && !strings.ContainsAny(template[i-1], \"{}=\") && len(out) > 0 && out[len(out)-1] == template[i-1](agents.go:308 与 template.go:117)。最小改动:让 ExpandAgentArgs 复用 process.ExpandArgvOmitEmpty,把 {session=} 表达为先 RewriteKeepSession 归一再统一展开,保留现有表驱动 argv 测试作为回归网。"},{"id":"QA-10","tier":"low","category":"regression","short_summary":"codex 非归档审核 stdout 末尾多一个换行","summary":"parseReviewOutput 统一后,codex 的非归档路径由原来的 os.Stdout.Write(data) 改为 fmt.Println(text),结果末尾比改动前多一个换行。","failure_scenario":"非归档地执行 kander review codex,codex 的结果文件(output_name=output.txt、parse=raw)通常已以 \\n 结尾,fmt.Println 再补一个 \\n,stdout 比改动前多一个空行。若有脚本对该 stdout 做逐字节比较或哈希,会与改动前不一致。claude/cursor/grok 改动前后都是 fmt.Println,不受影响。","file":"internal/review/args.go","line":132,"evidence":"git show 8abdfe2e:internal/review/args.go 的 codex 分支无论是否归档都走 `_, _ = os.Stdout.Write(data); syncStream(os.Stdout)`;现 internal/review/args.go:126-133 只有归档分支保持 Write,非归档分支为 fmt.Println(text)。codex 的 output_name / parse 见 internal/config/agents/codex.json 的 review 段(output.txt、{source:file, parse:raw}),故 text 即结果文件原文。最小改动:把 args.go:132 改为与归档分支一致的 os.Stdout.Write([]byte(text)) + syncStream(os.Stdout),或仅在 !strings.HasSuffix(text, \"\\n\") 时补换行。"}]}
```