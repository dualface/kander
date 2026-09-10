# QA 审核报告

**Role**: QA(质量负责人)
**Commit**: `e1c01277075599cb0395bf62f16855cebf04b060`(区间 `8abdfe2e..e1c0127`,8 提交 / 58 文件 / +4843-574)
**Task Context**: 组分支批次一三张卡 —— `20260909-process-output-parser-task`(`internal/process` 声明式输出解析与占位符)、`20260908-agent-definition-embed-task`(四个内置 agent 迁为嵌入 JSON + `prompt_delivery`)、`20260908-agent-review-template-task`(审核调用声明化、reviewer 名单开放)。A3 不在本区间,未审。

**Reviewed Scope**: 只读核验。已执行 `go build ./...`、`go vet ./...`、`GOOS=windows go build ./...`、`go test ./...` —— 全部通过;执行前后 `git status --porcelain` 为空、`HEAD` 未移动。逐行比对了改动前后的 `agentArguments`、`reviewerArguments`/`parseReviewOutput`/`agentSettingsFor`、`execute.go` 的 spec 快照与 stdout 路由、`validate.go` 的 home 策略、`integrate.go` 的规则入口与去重、menu 显示名与 effort、以及三张内置表的默认值。未核验 Windows 原生行为与 `plan.md` 纸面表(看板附件,不在提交树);本机无真实 herdr/tmux 与四个 CLI,pane 结论基于测试桩与代码路径。

**整体判断**:三条交付的主干需求成立 —— argv、审核参数、env、cwd、结果解析、默认配置、安装器写入均与改动前逐项一致(有表驱动测试固定);解析器的四步求值、行内合取成功判定、`success` 不受 `select` 限制、子集入口拒绝都有针对性用例。门禁问题集中在两处逻辑缺口与几项机械项。

**门禁发现 7 条**(完整正文与证据见上文报告主体):

- **QA-01 medium** — `sendAttempted` 在 `pane run` **之前**置真(`agent.go:70`/`:125`),而 pane 模式的投递在其**之后**。于是 dispatch 授权的 `resume`/`notify` 恢复路径上,`blocked`/投递拒绝/`ready` 超时被误判为 `DeliveryUnknown`:容器不关、卡片不回滚、任务文件不清理 —— 与本区间刚写入的 `docs/custom-agents.md:53` 和 `KANDER-KANBAN-RULES.md:550` 的承诺直接冲突。现有用例只覆盖非 durable 的 `start`。
- **QA-02 medium** — `review.prompt_files` 接受 `prompts/guide.md` 这类规范多级相对路径,但渲染端不建父目录(`fs` 侧逐段 `openat`,无 mkdir),这类定义每次审核必然以通用 I/O 错误失败。
- **QA-03 medium [mechanical]** — `docs/output-parsing.md:91` 声称 `{{`/`}}` 规则同样适用于 argv 元素模板,但 `args.start`/`args.resume` 走另一套实现,拒绝任何字面花括号。
- **QA-04 medium [mechanical]** — `HasReviewTemplate` 的 doc 注释挂在 `validateReviewerChoice` 头上。
- **QA-05 medium [mechanical]** — `process.ReviewSources` / `TerminalSources` 声明后全仓零引用(子集判定实为硬编码)。
- **QA-06 medium [mechanical]** — 本区间删除调用点后,两条 review i18n key 在三份 locale 中共 6 条成为死数据(渲染结果无回归)。
- **QA-07 medium [mechanical]** — `TestBuiltinReviewStdinMatchesPreviousInstruction` 的 `got`/`want` 为同一表达式,恒真。

**NON-BLOCKING 5 条**:`make fmt-check` 因本区间 6 个文件失败(recommend);pane 失败正文未截断 pane 输出(low);嵌入定义的 `session` 字段被校验但从不读取(recommend,清理属 A3);codex 非归档 stdout 多一个换行(low);四份独立的 `{...}` 扫描器(suggest)。

任务文件已删除。