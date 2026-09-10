先读取 Kander 规则、任务规格和证据文件，再按 QA 角色核对本区间实现。规格已读完。接下来按配置刷新、再对照实现与验收条款逐项核验。实现文件较多，先从解析器、模板、审核调用和 launch argv 这几条主路径读起。接着核验测试覆盖、按名分支残留，以及 launch/review 的关键调用点。核验已完成：主路径闭环成立，但 pane 投递失败与 durable 发送边界的接缝有一处真实缺陷。接下来写出完整 QA 报告。Role: QA  
Commit: `e1c01277075599cb0395bf62f16855cebf04b060`  
Task Context: 组批次 `20260908-agent-definition-group` 三条交付（process 解析器、嵌入 agent 定义、审核模板）；不含 A3 退出命令/会话钩子。  
Reviewed Scope: 以 COMMIT TREE 与 task-spec 为边界，只读核验 `internal/process`、`internal/config`（含嵌入 JSON）、`internal/launch`、`internal/review`、`internal/menu`、`internal/install`、`internal/notify` 及对应测试/文档。本环境不能执行 `go test` / 交叉编译；调用方已记录 HEAD 干净、区间 8 提交。Windows 原生行为按各卡交叉编译口径，标为 Unverifiable 一次，不逐条重复。

## 行为 / 质量对照

| 主题 | 结论 | 证据 |
| --- | --- | --- |
| process 三种原语 + ndjson 四步 + 行内合取 success | 满足。json 非 JSON 且声明 success 判失败；select 污染行带同名 `content` 被丢掉；`{{`/`}}` 与未知占位符有单测 | `internal/process/output.go`、`output_test.go`、`template.go` |
| 审核子集入口 | `ParseReviewOutput` / `ValidateReviewOutput` 拒 `stderr`；review 包经该入口，无自写解析器 | `output.go:129-141,235-241`；`review/args.go:121`；`definition_test.go:233-264` |
| 四内置 argv / env / cwd / inspection / snapshot | 表驱动对齐改动前；stdin=`instruction`、无 `prompt_files` | `definition_test.go:14-158`；四份 `internal/config/agents/*.json` |
| 成功先于提取 | Grok `stopReason`、Claude/Cursor `is_error`/`subtype`、Codex 非零退出均拒绝；错误文案走既有 incomplete | `definition_test.go:160-208`；`definition_unix_test.go:260-283` |
| 自定义 reviewer 三种 parse + ndjson select | e2e 覆盖 file/raw、stdout/json_field、regex、ndjson 污染行 | `definition_unix_test.go:118-157` |
| `review.stdin: none` + `prompt_files` | 校验组合、0600 写入、argv 展开存在；嵌套相对路径校验放行但写入不建父目录 | `agents_review.go`；`execute.go:156-182` |
| 嵌入定义驱动 start/resume argv | `agentArguments` 无按名分支；`{session=}` 保留空 session；四内置 × start/resume × scale × session 有表驱动 | `session.go:355-383`；`builtin_argv_test.go` |
| `prompt_delivery` pane | argv 不加提示词；`pane run` 后、paneSession 前投递；blocked 优先；foreground 在 claim 前拒绝；notify 直投不等 ready | `prompt_delivery.go`；`prompt_delivery_test.go`；`notify/pane_ready_test.go` |
| reviewer 名单开放 | `ReviewAgents` 删除；config/menu/doctor/`kander review` 走 `HasReviewTemplate` | `agents_review.go:77-91`；`config.go:654-660`；`menu/options.go:110-114` |
| 空 argv 占位符省略 | review 用 `ExpandArgvOmitEmpty`（`{{` 不当占位符）；start/resume 仍用 `ExpandAgentArgs`+`{session=}`，与内置模板一致 | `template.go:104-129`；`agents.go:507-316` |
| 文件 1000 行规则 | 本区间相关非生成文件均未超过 | `output.go` 456 行；`agents_embed.go` 376 行；`execute.go` 362 行 |

## Gate findings

### QA-01 — medium — Observed

`mode: pane` 的第二段就绪失败（blocked / 投递原语拒绝 / 纯超时）发生在 `sendAttempted=true` 之后。`launchAgent` 在 herdr `pane run`、tmux `respawn-pane` **之前**就把 `sendAttempted` 置位；这在 `mode: argv` 下合理（提示词已在 argv 里，pane run 即发送边界），但 pane 模式的提示词要等 `completePaneDelivery`。durable 路径（`resume` 带 authorization，或 `notify` 恢复通道且卡片有 `DISPATCH_ID`）因此把「尚未投递/投递被拒」当成 DeliveryUnknown：不关本次 tab/window，也不回滚卡片。

这直接违反本卡对三种失败原因的统一处置（抓 pane、关容器、回滚、不留 WINDOW/SESSION），也与「不能保留容器」的设计结论冲突。`start` 不传 durable，三种失败的单测都走 start，所以主启动路径看起来是绿的；任务组接管/恢复是同一套 `launchAgent`。

**失败场景：** 任务组卡片带 `DISPATCH_ID`，对 pane 模式自定义 agent 做 `resume --agent` 接管（或 notify 走恢复通道建新容器）。新 CLI 弹出信任对话框，或 TUI 未起来。`completePaneDelivery` 失败时 `fail()` 因 `sendAttempted && durable` 返回 `DeliveryUnknown`。调用方跳过关窗与 `rollbackLaunch`。容器残留；接管路径还已提前写入新 SESSION。

**影响：** pane 模式在任务组恢复/接管上会留下无提示词的活容器和不一致卡片，后续 notify/resume 无法按「无 SESSION/无 WINDOW 的回滚态」重试。

**最小修复：** pane 模式把 `sendAttempted=true` 挪到 `deliverPromptToPane` **成功之后**（三种第二段失败都保持 `false`）。argv 模式维持现有「pane run 前」边界。

## NON-BLOCKING

### QA-N1 — low — Observed

`review.prompt_files[].path` 允许规范相对路径（含 `prompts/guide.md`），但 `execute.go` 只 `WriteTextAtomic`，不创建中间目录；`openPosixParent` 对缺失父目录返回 ENOENT。单测只用 `guide.md`。作者按「runtime 相对路径」写成子目录时，校验通过、审核启动 I/O 失败。最小改动：写入前在 runtime 内按隔离规则创建父目录，或校验改为只接受单段文件名。

### QA-N2 — suggest — Observed

选项面板 reviewer 列表来自 `ReviewAgentNames`，但再经过 `reviewerUsable`（要求 `--version` 探测成功）。执行侧自定义 agent 即使探测失败仍可选；审核侧脚本型 reviewer（本卡 e2e 用的假可执行通常没有 `--version`）不会出现在面板，只能改 JSON。若希望与「定义了审核模板即可入选」对齐，可让带审核模板的名字始终出现在候选里，探测失败只影响标签。

NON-BLOCKING 共 2 条，无丢弃。

```kander-findings
{"FINDINGS":[{"id":"QA-01","tier":"medium","text":"mode: pane 的第二段就绪失败（blocked / 投递原语拒绝 / 纯超时）发生在 sendAttempted=true 之后。launchAgent 在 herdr pane run、tmux respawn-pane 之前就把 sendAttempted 置位；这在 mode: argv 下合理（提示词已在 argv 里，pane run 即发送边界），但 pane 模式的提示词要等 completePaneDelivery。durable 路径（resume 带 authorization，或 notify 恢复通道且卡片有 DISPATCH_ID）因此把「尚未投递/投递被拒」当成 DeliveryUnknown：不关本次 tab/window，也不回滚卡片。这直接违反本卡对三种失败原因的统一处置（抓 pane、关容器、回滚、不留 WINDOW/SESSION）。start 不传 durable，三种失败单测都走 start，主启动路径是绿的；任务组接管/恢复是同一套 launchAgent。失败场景：任务组卡片带 DISPATCH_ID，对 pane 模式自定义 agent 做 resume --agent 接管（或 notify 走恢复通道建新容器），新 CLI 弹出信任对话框或 TUI 未起来。completePaneDelivery 失败时 fail() 因 sendAttempted && durable 返回 DeliveryUnknown，调用方跳过关窗与 rollbackLaunch。容器残留；接管路径还已提前写入新 SESSION。最小修复：pane 模式把 sendAttempted=true 挪到 deliverPromptToPane 成功之后（三种第二段失败都保持 false）；argv 模式维持现有 pane run 前边界。","evidence":"internal/launch/agent.go:27-28,70-76,125-131; internal/launch/commands.go:260-275,289-295; internal/launch/notify_resume.go:127-133; internal/launch/prompt_delivery.go:55-63。start 不传 durable（start.go:147），prompt_delivery_test.go 三种失败均 commandStart，未覆盖 durable resume/notify。"}],"NON_BLOCKING":[{"id":"QA-N1","tier":"low","text":"review.prompt_files[].path 允许规范相对路径（含 prompts/guide.md），execute.go 只 WriteTextAtomic、不创建中间目录；openPosixParent 对缺失父目录返回 ENOENT。单测只用 guide.md。作者按 runtime 相对路径写成子目录时，校验通过、审核启动 I/O 失败。最小改动：写入前在 runtime 内按隔离规则创建父目录，或校验只接受单段文件名。","evidence":"internal/config/agents_review.go:242-256; internal/review/execute.go:171-174; internal/fs/posix.go:75-79; definition_unix_test.go:200 使用 path=guide.md。"},{"id":"QA-N2","tier":"suggest","text":"选项面板 reviewer 列表来自 ReviewAgentNames，但再经过 reviewerUsable（要求 --version 探测成功）。执行侧自定义 agent 即使探测失败仍可选；审核侧脚本型 reviewer（本卡 e2e 假可执行通常没有 --version）不会出现在面板，只能改 JSON。若希望与「定义了审核模板即可入选」对齐，可让带审核模板的名字始终出现在候选里，探测失败只影响标签。","evidence":"internal/menu/options.go:110-114 对比 104-105 执行侧「Unavailable agents remain selectable」；internal/menu/agents.go:99-125,195-201。"}]}
```