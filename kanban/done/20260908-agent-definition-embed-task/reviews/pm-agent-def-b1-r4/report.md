# PM 审核报告

**Role**: PM（规格验收）
**Commit**: `e1c01277075599cb0395bf62f16855cebf04b060`（基线 `8abdfe2e6430a00e19383f84f23088a8ff743be0`，8 提交 / 58 文件）
**Task Context**: `/tmp/claude-review.34d378302bb1211802238cf88593e9b6/task-spec.md` 合并的三张卡：`20260909-process-output-parser-task`（B1 解析器）、`20260908-agent-definition-embed-task`（A1 嵌入定义）、`20260908-agent-review-template-task`（A2 审核模板）。A3 退出命令/会话钩子不在本区间，未审。
**Reviewed Scope**: `internal/process`、`internal/config`（含 `agents/*.json`）、`internal/launch`、`internal/review`、`internal/install`、`internal/menu`、`internal/notify`、`rules/*.md`、`docs/*.md`、`AGENTS.md`，以及三卡各自的验收条款与被改契约的调用方。实测 `go build ./...` / `go vet ./...` / `go test ./...` / `GOOS=windows go build ./...` 全通过，`git status --porcelain` 空、HEAD 未移动（Observed）。

---

## 需求追踪（只列非 Complete 行与需要说明的行）

| 卡 | 需求 | 期望行为 | 代码证据 | 状态 |
|---|---|---|---|---|
| B1 | `plan.md` 纸面验证表（四个内置 reviewer + 污染场景逐行表达） | 卡片附件内以表格固定 | 看板目录不入 Git，COMMIT TREE 无该路径；代码侧等价物为 `internal/process/output_test.go:270` `TestPaperReviewerExpressions` | Unverifiable |
| A1 | 三种失败原因「一种处置」：抓 pane 输出 → 关本次容器 → 回滚，不保留容器、不写 `WINDOW` | 所有入口一致 | `internal/launch/agent.go:26-35` 的 `durable` 短路；`notify_resume.go:127` / `commands.go:289` | **Partial** → PM-01 |
| A1 | `resume`/`notify` 恢复通道对 `mode: pane` 走统一处置 | 与 `start` 同 | `internal/launch/prompt_delivery_test.go:268` 仅覆盖成功路径；durable 分支无用例 | **Partial** → PM-01 |
| A2 | 「执行配置不覆盖审核」的隔离在迁移后保持；`reviewers.<role>` 只接受**定义了** `args.review` 的 agent | 执行包装器不进入审核 | `internal/config/agents.go:80-88`（dialect 回填 `args.review` 与 `review`）+ `agents_review.go:96-108`；`internal/menu/agent_probe_test.go:23,30` 断言纯包装器 `Review == true` | **Contradicted** → PM-02 |
| B1 | 结构、校验、解析全部位于 `internal/process`，纯新增 | 只改 `internal/process`、`docs/output-parsing.md`、`AGENTS.md` | 提交 `116bd74 --stat` 仅这三处 | Complete |
| A1 | `kander config --json` 默认输出逐字节一致 | 与改动前一致 | `internal/config/testdata/default-config.json` 与被删的 `kanbanModelDefaults`/`reviewModelDefaults` 逐键一致；`agents_embed_test.go:13` | Complete |
| A1 | 安装器两种模式写入目标/内容/去重逐字节一致 | 与改动前一致 | `internal/install/integrate.go:36-52` 按 target 去重，项目模式仍只落 `CLAUDE.md` + `AGENTS.md` | Complete |
| A1/A2 | `rules/` 四处 + 六处承诺同步 | 逐条给出改后原文 | `rules/KANDER-KANBAN-RULES.md:76,131-132,548,551,562,564`、`KANDER-REVIEW-RULES.md:24-66`、`KANDER-BASE-RULES.md:16`；`internal/launch/rules_prompt_delivery_test.go:11` 逐字固定 | Complete |
| A2 | 四个内置 reviewer argv/env/cwd/home/output_name/inspection/helpers/snapshot_spec 与改动前逐项一致 | 表驱动固定 | 逐项比对 `git show 8abdfe2e:internal/review/args.go:24-110` 与四份定义文件，全部一致；`internal/review/definition_test.go:14` | Complete |

其余 A1 / A2 / B1 验收条款均已逐条比对到实现与用例，状态 Complete（不在此重复罗列）。

**完成度统计**：Complete 34 / Partial 2 / Contradicted 1 / Missing 0 / Unverifiable 1。

---

## 门禁发现

### PM-01 — medium — durable 派发下 `mode: pane` 的投递失败不走「关容器 + 回滚」，被判为「投递结果未知」

**Claim: Observed.** `launchAgent` 的 `sendAttempted` 在 `herdrPaneRun` / `tmuxStartPane` 之前置真（`internal/launch/agent.go:66,120`），而 `mode: pane` 的第二段就绪与投递发生在其后（`agent.go:71-79,124-132`）。因此当 `durable` 为真时，`fail()` 直接返回 `&LaunchFailure{Err: err, DeliveryUnknown: true}`（`agent.go:27-29`），**跳过 `herdrCloseTab` / `tmuxCloseWindow`**；调用方 `notify_resume.go:130-134` 收到 `DeliveryUnknown` 后 `taskFileHandedOff = true` 并直接返回，不执行 `window.RestoreWindowText`。

触发路径：卡片带 `DISPATCH_ID`（任务组派发，`notify_resume.go:127`）或接管带授权（`commands.go:289` 的 `len(authorization) > 0`），执行 agent 声明 `prompt_delivery.mode: pane`，随后 `blocked` 命中或 `ready` 超时。此时提示词**确定未送出**（`blocked` 在投递前短路，超时同理），但结果被当作「可能已投递」处理，留下一个停在信任确认对话框上的容器、未回滚的卡片文本与未清理的临时任务文件。这正是卡片 A1 明文禁止的状态：「三者的处置相同：先抓取 pane 输出留作诊断，再关闭本次创建的 tab/window 并把卡片回滚到 `todo/`。**不保留容器，不写 `WINDOW`**」，也是 A1 论证过的「保留容器 + 事后补投」三条恢复路都不通的那种残留卡片。

最小修复：`fail()` 的 `DeliveryUnknown` 短路只对 `PromptDelivery.Mode != "pane"` 生效；`mode: pane` 下投递失败一律走关容器与回滚（`blocked`/超时本来就确定未投递）。

---

### PM-02 — medium — 只声明 `path` + `dialect` 的执行包装器被自动列为 reviewer，且审核直接使用该包装器

**Claim: Observed.** `AgentFor` 在用户定义未声明审核字段时，从 dialect 的嵌入定义回填 `args.review` 与整个 `review` 块（`internal/config/agents.go:80-88`）。于是 `HasReviewTemplate`（`agents_review.go:77-80`）对一个纯执行包装器返回 true，该名字进入 `ReviewAgentNames`，被 `validateReviewerChoice`（`agents_review.go:63-75`）接受写入 `reviewers.<role>`，并出现在选项面板候选列表（`internal/menu/options.go:110`）。随后 `ReviewExecutable`（`agents_review.go:96-108`）因该名字不在 `ExecutionAgents` 中而回落到用户声明的 `path`——**执行包装器成为审核可执行文件**。

现有用例已把这一行为钉死：`internal/menu/agent_probe_test.go:23` 声明 `"helper": {Path: renamed-agent, Dialect: "claude"}`（无任何 review 声明），`:30` 断言 `!states["helper"].Review` 失败即 `Review == true`；同一测试的 `:27` 又断言内置名 `codex` 的同一包装器**不得**被当作 reviewer。

这与卡片 A2 的两条契约冲突：(1)「`reviewers.<role>` 与 `kander review [agent]` 接受任何**定义了** `args.review` 的 agent」——继承而来的模板不是该 agent 定义的；(2) A2 保留的隔离「用户在 `agents.<name>.path` 对内置 agent 的覆盖只影响执行，不进入审核」与其 DISCUSSION 的理由「自定义 agent 本来就由用户声明可执行路径，不存在『包装器被意外用于审核』的顾虑」——此处用户只声明了执行覆盖，从未编写审核定义，A2 THREAT_MODEL 里「只读性由定义作者负责」的责任转移前提因此不成立：kander 会用 Claude 的只读 argv 去跑一个作者未按只读语义审阅过的包装器。

最小修复：`HasReviewTemplate` / `ReviewAgentNames` 只认用户定义**自身**声明了 `args.review`（或 `review`）的自定义 agent，dialect 回填仍可保留供已声明者补全缺省字段。

---

### PM-03 — medium [mechanical] dead-code — `process.ReviewSources` / `process.TerminalSources` 无任何引用

**Claim: Observed.** `internal/process/output.go:29` 与 `:32` 导出两个子集切片，全仓（含 `_test.go`）除声明行与其上方注释外零引用。子集限制实际由 `ValidateReviewOutput` / `ValidateTerminalOutput`（`output.go:129-149`）里的硬编码分支实现，两个变量从未被读取。卡片 B1 只要求「带 `source` 子集限制的**入口**存在并各有测试」，入口已由函数满足，这两个变量是纯冗余。最小修复：删除两个变量，或让两个 Validate 函数改用它们做集合判定。

---

### PM-04 — medium [mechanical] dead-code — `extractText` 正则分支的 `jsonStringOrRaw` 回退不可达

**Claim: Observed.** `internal/process/output.go:432-434` 的 `if input == "" && decodeErr == nil { input, _ = jsonStringOrRaw(doc) }` 无法进入：`extractText` 只有两个调用点，`evaluateJSON`（`output.go:269`）传入的 `decodeErr` 来自对同一份 `data` 的 `json.Unmarshal`，`data == ""` 时该调用必然返回 `unexpected end of JSON input`（`decodeErr != nil`）；`evaluateNDJSON`（`output.go:306`）传入的 `row.raw` 已被 `output.go:280` 的 `strings.TrimSpace(line) == ""` 过滤，恒非空。因此 `jsonStringOrRaw`（`output.go:446-455`）也只被这一不可达行调用。最小修复：删除该回退分支与 `jsonStringOrRaw`。

---

### PM-05 — medium [mechanical] dead-code — 两个 i18n 文案键在解析统一后成为孤儿

**Claim: Observed.** `parseReviewOutput` 统一为 `incompleteReviewError`（`internal/review/args.go:104-132`）后，`review.codex_review_did_not_complete_with_review_text` 与 `review.grok_review_did_not_complete_with_review_text` 在全仓（Go、Markdown、测试）零引用，仅存在于 `internal/i18n/locales/en.json:709,724`、`ja.json:709,724`、`zh-CN.json:709,724`。与基线对比，本区间新增的孤儿键恰为这两个（其余 38 个为改动前既有，不在本次范围内）。最小修复：三份 locale 各删除这两行。

---

## NON-BLOCKING

- **PM-06 — low** — `AgentRulesTarget` 现在只查嵌入定义（`internal/install/integrate.go:87-90` 的 `config.RulesSpec(agent)`），对自定义 agent 名一律返回 `""`，不按 dialect 回落。因此 `internal/menu/doctor.go:186-188` 与 `internal/menu/options.go:476-478` 新增的 `if target == "" { continue }` 会让「配置的执行 agent 全是自定义名」的用户在 `kander doctor` 与选项面板结束时**完全看不到**规则入口集成状态（改动前该场景在项目模式下会检查项目根 `AGENTS.md`）。安装器本身不受影响（`integrateAgentRules` 只遍历内置名），所以文件仍会被创建，影响限于诊断可见性。最小改动：`RulesSpec` 按 `AgentFor(cfg, name).Dialect` 解析后再查嵌入定义。

- **PM-07 — recommend** — A2 EXPECTED_OUTCOME 写明「本卡结束时 review 包内不再有按内置名的业务分支」，但 `internal/review/settings.go:290,295` 的 `reviewerFromConfig` 仍以字面量 `"codex"` 作为兜底返回值（配置加载失败或 `reviewers.<role>` 不可用时）。该函数不在 A2 逐条枚举的验收清单内、代码本身未在本区间改动，故不作门禁；但它是该目标下唯一剩余的按内置名取值点，建议改为 `defaultAgentName()` 或 `ReviewAgentNames(cfg)` 的首项。

- **PM-08 — low** — Codex 审核在非归档路径下的报告输出多出一个换行：改动前 `os.Stdout.Write(data)`（`git show 8abdfe2e:internal/review/args.go:165`），改动后统一为 `fmt.Println(text)`（`internal/review/args.go:129`），而 `text` 即原始文件内容（`parse: raw`），本身已以换行结尾。判定结果与错误文案不受影响（`review.review_did_not_complete_with_review_text` 的 `{{.V0}}` 展开为 `Codex`，与旧键逐字相同），仅 stdout 尾部多一空行。最小改动：`raw` 来源直接 `os.Stdout.Write` 而非 `Println`。

- **PM-09 — low** — 审核 effort 字段的显隐与实际生效条件脱钩：`internal/menu/options.go:402` 改用 `config.AgentSupportsEffort(s.Config, reviewer)`，该函数在用户为该名字声明了 `args` 时无条件返回 true（`internal/config/agents_embed.go:233-235`）；而审核 effort 是否生效取决于 `models.review.<agent>` 是否存在 `effort` 键（`internal/review/settings.go:110-112`），后者由嵌入的 `supports_effort` 决定。用户为内置 `cursor` 声明 `agents.cursor.args`（执行包装）后，审核面板会多出一个 effort 输入框，填入的值在 `configuredModel` 中被丢弃、实际仍用 `"high"`。改动前 `reviewer == "cursor"` 的按名判断不存在这个错配。最小改动：审核侧改用「该 agent 的 `models.review` 条目是否含 `effort` 键」判定显隐。

（候选项未超过十条，无丢弃项。）

```kander-findings
{"FINDINGS":[{"id":"PM-01","tier":"medium","text":"durable 派发下 mode: pane 的投递失败不走「关容器 + 回滚」的统一处置。launchAgent 在 herdrPaneRun / tmuxStartPane 之前就把 sendAttempted 置真，而 mode: pane 的第二段就绪与提示词投递发生在其后；因此 durable 为真时 fail() 直接返回 &LaunchFailure{DeliveryUnknown: true}，跳过 herdrCloseTab / tmuxCloseWindow，调用方 notify_resume.go 收到 DeliveryUnknown 后设 taskFileHandedOff = true 并直接返回，不执行 window.RestoreWindowText。触发路径：卡片带 DISPATCH_ID（任务组派发）或接管带授权，执行 agent 声明 prompt_delivery.mode: pane，随后 blocked 命中或 ready 超时。此时提示词确定未送出（blocked 在投递前短路，超时同理），却被当作「可能已投递」，留下一个停在信任确认对话框上的容器、未回滚的卡片文本与未清理的临时任务文件。卡片 A1 明文要求「三者的处置相同：先抓取 pane 输出留作诊断，再关闭本次创建的 tab/window 并把卡片回滚到 todo/。不保留容器，不写 WINDOW」，且专门论证过这种残留卡片三条恢复路都不通。最小修复：fail() 的 DeliveryUnknown 短路只对 PromptDelivery.Mode != \"pane\" 生效，mode: pane 下投递失败一律走关容器与回滚。","evidence":"internal/launch/agent.go:26-35（fail 闭包的 DeliveryUnknown 短路）; internal/launch/agent.go:66,120（sendAttempted 早于投递置真）; internal/launch/agent.go:71-79,124-132（completePaneDelivery 在 pane run 之后）; internal/launch/notify_resume.go:127（durable = DISPATCH_ID != \"\"）与 :130-134（DeliveryUnknown 直接返回，不 RestoreWindowText）; internal/launch/commands.go:289（durable = len(authorization) > 0）; 契约见 task-spec.md 卡 20260908-agent-definition-embed-task EXPECTED_OUTCOME「三种失败原因, 一种处置」与 ACCEPTANCE_CRITERIA「三者的处置一致且可判定」; internal/launch/prompt_delivery_test.go:268 的 resume 用例只覆盖成功路径，durable 分支无用例"},{"id":"PM-02","tier":"medium","text":"只声明 path + dialect 的执行包装器被自动列为 reviewer，且审核直接使用该包装器。AgentFor 在用户定义未声明审核字段时从 dialect 的嵌入定义回填 args.review 与整个 review 块，于是 HasReviewTemplate 对一个纯执行包装器返回 true：该名字进入 ReviewAgentNames、被 validateReviewerChoice 接受写入 reviewers.<role>、出现在选项面板候选列表；随后 ReviewExecutable 因该名字不在 ExecutionAgents 中而回落到用户声明的 path，执行包装器成为审核可执行文件。这与卡片 A2 的两条契约冲突：(1)「reviewers.<role> 与 kander review [agent] 接受任何定义了 args.review 的 agent」——继承而来的模板不是该 agent 定义的；(2) A2 保留的隔离「用户在 agents.<name>.path 对内置 agent 的覆盖只影响执行, 不进入审核」及其 DISCUSSION 理由「自定义 agent 本来就由用户声明可执行路径, 不存在『包装器被意外用于审核』的顾虑」——此处用户只声明了执行覆盖，从未编写审核定义，A2 THREAT_MODEL 里「只读性由定义作者负责」的责任转移前提因此不成立：kander 会用内置 reviewer 的只读 argv 去跑一个作者未按只读语义审阅过的包装器。最小修复：HasReviewTemplate / ReviewAgentNames 只认用户定义自身声明了 args.review（或 review）的自定义 agent，dialect 回填仍可供已声明者补全缺省字段。","evidence":"internal/config/agents.go:80-88（d.Args == nil 时整体回填 emb.Args，d.Review == nil 时回填 emb.Review）; internal/config/agents_review.go:77-80（HasReviewTemplate）、:83-91（ReviewAgentNames）、:63-75（validateReviewerChoice）、:96-108（ReviewExecutable 对非内置名回落 d.Path）; internal/menu/options.go:110（面板候选）; internal/menu/agent_probe_test.go:23 声明 {\"helper\": {Path: renamed-agent, Dialect: \"claude\"}} 且无任何 review 声明，:30 断言 helper.Review == true，同一测试 :27 又断言内置名 codex 的同一包装器不得成为 reviewer; 契约见 task-spec.md 卡 20260908-agent-review-template-task EXPECTED_OUTCOME 的 reviewers 开放条款、可执行名优先级条款与 DISCUSSION 末两条"},{"id":"PM-03","tier":"medium","mechanical":"dead-code","text":"process.ReviewSources 与 process.TerminalSources 两个导出变量在全仓（含 _test.go）除声明行与其上方注释外零引用。source 子集限制实际由 ValidateReviewOutput / ValidateTerminalOutput 内的硬编码分支实现，这两个切片从未被读取。卡片 B1 只要求「带 source 子集限制的入口存在并各有测试」，该要求已由四个子集函数满足，两个变量是纯冗余。最小修复：删除这两个变量，或让两个 Validate 函数改用它们做集合判定。","evidence":"internal/process/output.go:28-29（ReviewSources）与 :31-32（TerminalSources）; grep -rn \"ReviewSources|TerminalSources\" --include='*.go' internal/ 仅命中这四行（声明 + 注释），无任何读取点; 子集判定实际写在 internal/process/output.go:134 与 :145"},{"id":"PM-04","tier":"medium","mechanical":"dead-code","text":"extractText 正则分支中的 jsonStringOrRaw 回退不可达。该分支的进入条件是 input == \"\" 且 decodeErr == nil，但 extractText 只有两个调用点：evaluateJSON 传入的 decodeErr 来自对同一份 data 的 json.Unmarshal，data 为空串时该调用必然返回 unexpected end of JSON input（decodeErr != nil）；evaluateNDJSON 传入的 row.raw 已被空行过滤（strings.TrimSpace(line) == \"\" 时 continue），恒非空。因此该 if 体永不执行，其唯一调用的 jsonStringOrRaw 函数也随之不可达。最小修复：删除该回退分支与 jsonStringOrRaw。","evidence":"internal/process/output.go:432-434（不可达的 if input == \"\" && decodeErr == nil 分支）; 调用点 internal/process/output.go:269（evaluateJSON，err 来自 :260 的 json.Unmarshal）与 :306（evaluateNDJSON，decodeErr 恒为 nil、row.raw 由 :280 的空行过滤保证非空）; internal/process/output.go:446-455（jsonStringOrRaw，唯一调用者是 :433）"},{"id":"PM-05","tier":"medium","mechanical":"dead-code","text":"parseReviewOutput 统一为 incompleteReviewError 后，两个按内置名定制的文案键成为孤儿：review.codex_review_did_not_complete_with_review_text 与 review.grok_review_did_not_complete_with_review_text 在全仓的 Go 源码、测试与文档中零引用，只剩三份 locale 里的条目。与基线 8abdfe2e 逐键对比，本区间新增的未引用键恰为这两个（其余 38 个未引用键为改动前既有，不在本次范围内）。最小修复：三份 locale 各删除这两行。","evidence":"internal/i18n/locales/en.json:709,724; internal/i18n/locales/ja.json:709,724; internal/i18n/locales/zh-CN.json:709,724; 替代实现见 internal/review/args.go:104-132（parseReviewOutput 统一走 incompleteReviewError）与 :134-136（使用 review.review_did_not_complete_with_review_text）; 基线实现见 git show 8abdfe2e:internal/review/args.go:158,185"}],"NON_BLOCKING":[{"id":"PM-06","tier":"low","text":"AgentRulesTarget 现在只查嵌入定义（config.RulesSpec），对自定义 agent 名一律返回空串且不按 dialect 回落。因此 doctor 与选项面板结束时新增的 target == \"\" 跳过分支，会让「配置的执行 agent 全是自定义名」的用户完全看不到规则入口集成状态——改动前该场景在项目模式下会检查项目根 AGENTS.md。安装器本身不受影响（integrateAgentRules 只遍历内置名，项目模式仍落 CLAUDE.md + AGENTS.md），影响限于诊断与面板的可见性。最小改动：RulesSpec 先按 AgentFor(cfg, name).Dialect 解析再查嵌入定义。","evidence":"internal/install/integrate.go:87-90（RulesSpec 查不到即返回 \"\"）; internal/config/agents_embed.go:245-255（RulesSpec 直接用 embeddedByName，不解析 dialect）; internal/menu/doctor.go:186-188 与 internal/menu/options.go:476-478 新增的跳过分支; 基线行为见 git show 8abdfe2e:internal/install/integrate.go 的 AgentRulesTarget 项目模式默认 AGENTS.md 分支"},{"id":"PM-07","tier":"recommend","text":"卡片 A2 的 EXPECTED_OUTCOME 写明「本卡结束时 review 包内不再有按内置名的业务分支」，但 reviewerFromConfig 仍以字面量 \"codex\" 作为兜底返回值（配置加载失败，或 reviewers.<role> 指向的 agent 不再定义审核模板时）。该函数不在 A2 逐条枚举的验收清单内、代码本身未在本区间改动，因此不作门禁；但它是该目标下 review 包内唯一剩余的按内置名取值点。建议改为 config 侧的默认 agent 名或 ReviewAgentNames(cfg) 的首项。","evidence":"internal/review/settings.go:288-296（reviewerFromConfig 的两处 return \"codex\"）; 契约见 task-spec.md 卡 20260908-agent-review-template-task EXPECTED_OUTCOME 末段"},{"id":"PM-08","tier":"low","text":"Codex 审核在非归档路径下的报告输出比改动前多一个换行：改动前直接 os.Stdout.Write(data)，改动后统一走 fmt.Println(text)，而 text 即原始文件内容（codex 定义为 parse: raw），本身已以换行结尾。判定结果与错误文案不受影响——新键 review.review_did_not_complete_with_review_text 的 {{.V0}} 展开为 AgentDisplayName(\"codex\") = \"Codex\"，与被弃用的旧键逐字相同。仅 stdout 尾部多一空行。最小改动：raw 来源的报告直接 os.Stdout.Write 而非 Println。","evidence":"internal/review/args.go:129（fmt.Println(text)）; 基线 git show 8abdfe2e:internal/review/args.go:165（os.Stdout.Write(data) 后 syncStream，无额外换行）; internal/config/agents/codex.json 的 review.output 为 {\"source\":\"file\",\"parse\":\"raw\"}"},{"id":"PM-09","tier":"low","text":"审核 effort 字段的显隐与其实际生效条件脱钩。ReviewModelFieldsFor 改用 config.AgentSupportsEffort(s.Config, reviewer)，该函数在用户为该名字声明了 args 时无条件返回 true；而审核 effort 是否生效取决于 models.review.<agent> 是否存在 effort 键（由嵌入定义的 supports_effort 决定）。用户为内置 cursor 声明 agents.cursor.args（执行包装）后，审核面板会多出一个 effort 输入框，填入的值在 configuredModel 中被丢弃、实际仍用 \"high\"。改动前的 reviewer == \"cursor\" 按名判断不存在这个错配。最小改动：审核侧改用「该 agent 的 models.review 条目是否含 effort 键」判定显隐。","evidence":"internal/menu/options.go:402（!config.AgentSupportsEffort(s.Config, reviewer)）; internal/config/agents_embed.go:233-235（userArgs 为真即返回 true）; internal/review/settings.go:110-112（entry 无 effort 键时直接丢弃 role 的 effort）; internal/config/agents_embed.go:273-278（reviewFields 对 supports_effort=false 不产出 effort 键）; 基线 git show 8abdfe2e:internal/menu/options.go 的 reviewer == \"cursor\" 判断"}]}
```