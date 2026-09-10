# claude 审核报告缺 kander-findings 围栏

- TYPE: Bug
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 15:37
- OWNER: grok
- SESSION: grok ad3c9950-ea73-4edd-af8c-2e693e5ca54d
- WINDOW: herdr:w2T:t11:w2T:p11
- STARTED_AT: 2026-09-09 15:38
- FINISHED_AT:
- TASK_BRANCH: 20260909-claude-review-final-message
- RESULT:

## GOAL

claude 当 reviewer 时大量运行以 `structured findings required; legacy mapping required for old reports` 失败: 审核实际做完了, 但 claude 把最后一条消息写成了给人看的结论摘要, 完整报告与 ```kander-findings 围栏从未输出。

根因在提示词与取报告方式的错配, 不在解析器:

- `internal/review/args.go` 的 claude 分支用 `--print --output-format json`, `parseReviewOutput` 取 JSON 的 `result` 字段, 即最后一条 assistant 消息。cursor 同理, grok 取 `text`, 三者的报告都等于最后一条消息。只有 codex 用 `--output-last-message <file>` 把报告落到文件, 不受聊天语气影响。
- `internal/review/execute.go` 只经 stdin 递一行 `Perform the <role> review; 完整指令位于 UTF-8 任务文件 <path>; 先读取完整文件并严格遵守.`, 完整契约在任务文件里, 对 claude 而言更像"读来的材料"而非"用户刚说的话"。
- `internal/process/process.go` 的 `taskPayload` 又把"完成本次任务后尝试删除任务文件"追加在任务文件最末尾, 成为最后一条指令。失败样本里绝大多数报告第一句就是"审核完成, 任务文件已删除"。

本卡在 `internal/review` 内修复提示词与启动参数, 让"最后一条消息即报告全文"成为显式契约, 使 claude/cursor/grok 的报告能稳定通过既有校验。

## USER_DECISIONS

- 采用三步修复: buildPrompt 增加 output contract 段落; claude 启动参数加 `--append-system-prompt`; 补提示词构造测试。
- 归为单卡, 不建任务组。
- 不改 `internal/board/review_findings.go` 的解析与校验逻辑。

## EXPECTED_OUTCOME

- 对报告取自最后一条消息的 reviewer (claude, cursor, grok), `buildPrompt` 产出的提示词包含一段显式 output contract, 内容涵盖: 最终消息本身即报告全文并包含 kander-findings 围栏; 一次写完即停止; 删除任务文件须在写最终消息之前完成, 之后不得再发任何后续消息。
- codex 的提示词不含该段落, 其余提示词内容对四种 reviewer 均保持不变。
- claude 的启动参数包含 `--append-system-prompt`, 其内容把同一契约提到 system prompt 层, 抵消 Claude Code CLI 默认的终端简答倾向。
- `internal/board/review_findings.go` 的解析与校验行为不变。
- 新增测试覆盖上述提示词与参数差异, `go test ./...` 全绿。

## ACCEPTANCE_CRITERIA

- [ ] `buildPrompt` 对 claude/cursor/grok 注入 output contract 段落, 该段落明确"最终消息即报告全文且含 kander-findings 围栏""一次写完即停止""先删任务文件再写最终消息, 之后不再发消息"三点。
- [ ] `buildPrompt` 对 codex 不注入该段落; 四种 reviewer 的其余提示词内容与修改前逐字一致。
- [ ] `reviewerArguments` 的 claude 分支包含 `--append-system-prompt` 及其内容参数, 其余分支的参数不变。
- [ ] 新增测试断言以上提示词差异与 claude 参数差异; 已有 review 与 board 测试不被修改以迁就实现。
- [ ] `internal/board/review_findings.go` 无改动。
- [ ] `go test ./...`, `go vet ./...`, `gofmt -l .` 全部通过, 工作区干净。

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题: 不修 `internal/process/process.go` 中 `taskPayload` 把清理指令写死为 `i18n.Text("cn", ...)` 的语言问题, 也不调整清理指令在任务文件中的位置 — 那条链路同时服务于执行 Agent 启动, 改动面超出本卡。
- 加固: 不新增审核失败后的自动重试, 不改 `internal/board/review_findings.go` 的解析宽容度 (例如接受非围栏 JSON), 校验严格性是当前的正确行为, 放宽会掩盖问题。
- 共享契约与文档: `rules/KANDER-REVIEW-RULES.md` 与 `docs/review-disposition.md` 描述的是围栏契约本身, 本卡不改契约只改提示词投递方式, 故不动这些文档。
- 邻近功能: 不改 codex 的 `--output-last-message` 路径, 不改 `parseReviewOutput` 的取值逻辑, 不调整 reviewer 的沙箱与只读参数。

## DISCUSSION

- 证据 (归档于 `kanban/.kander/groups/00000000-review-archive-group/runs/`): claude 作为 reviewer 共 42 次运行, 27 次因结构化报告无效失败 (25 次 `structured findings required`, 2 次 JSON 字段不合法), 12 次 ok, 3 次进程退出码 1; 同期 codex 32 次几乎全部 ok。
- 典型失败 `start-dialog-title-qa-1`: `originals/report.md` 仅 822 字节, 首句"审核完成, 任务文件已删除"; 同一次运行的 `output.raw` 显示 22579 output tokens (含 14266 thinking), 说明审核已实际执行, 只是没输出完整报告。
- 关键对照: `start-dialog-title-qa-5` (失败) 与 `start-dialog-title-qa-6` (通过) 的提示词只差一行。qa-5 用的是 `Mandatory structured output: ... A report without this block fails validation.` 仍失败; qa-6 改成 `Output contract: write one complete final message that already contains the analysis and this exact fence, then stop. Do not send a follow-up after deleting any file.` 后产出 8952 字节完整报告并通过。约束"最后一条消息的形态"比强调"围栏必须存在"有效, 但该句此前只能由调用方手工塞进 review-context, 未进代码。
- `claude --help` 确认 `--append-system-prompt <prompt>` 可用。
- SELF_REVIEW: 已对照用户目标与确认的三步计划复核。目标与结果一致, 三步计划逐条落在 EXPECTED_OUTCOME 与 ACCEPTANCE_CRITERIA 上; 边界四类均已说明且互不冲突, 达成目标所必需的提示词与参数改动未被排除; 约束与实际接口一致 (已核实 `buildPrompt`, `reviewerArguments`, `parseReviewOutput` 的现状与 claude CLI 选项); 验收条件可判定, 未引入用户未确认的要求。无待澄清项。

## IMPLEMENTATION

- 2026-09-09: 从 `origin/develop` (`211c104282f181d5fabe6cc49b03583b0bed269f`) 建任务分支 `20260909-claude-review-final-message` 与 worktree。
- `buildPrompt` 对 claude/cursor/grok 追加 `lastMessageOutputContract`; codex 不加, 其余提示词与修改前逐字一致。
- claude `reviewerArguments` 增加 `--append-system-prompt` 同一契约; 其余 reviewer 参数不变。
- 新增 `internal/review/prompt_contract_test.go`; 未改已有 review/board 测试, 未改 `internal/board/review_findings.go`。
- 交付提交 `ef9f64043854fb3c284567f602d48d627f9b7df3`。
- 交付自检 (commit `ef9f64043854fb3c284567f602d48d627f9b7df3`):
  1. `git diff --check` 干净。
  2. 新增测试 125 行; `git.go` 230→250, `args.go` 220→221, 均未超 1000。
  3. 注释只说明 last-message 契约原因, 无过时行为描述。
  4. 无死代码: `usesLastMessageReport` 仅服务 `buildPrompt`。
  5. 无冗余测试: 新测试分别钉契约三点、codex 不加段且其余逐字相同、增量同样附加、claude 参数与其他 reviewer 不加该旗标。
  6. `go test ./...`、`go vet ./...`、`gofmt -l .` 于该提交通过。
  7. `go test ./...` 于 `ef9f64043854fb3c284567f602d48d627f9b7df3` 共 21 个包 ok。

## SUMMARY

<FILL_IN>
