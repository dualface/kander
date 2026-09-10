# 审核模板任务交付及接管核验报告

## 2026-09-09 最终完成报告

本节是当前完成结论；以下历轮“待审核/集成”及旧提交记录均为历史原文。

- 任务：20260908-agent-review-template-task；SIZE large。
- 交付：审核 argv、提示词投递/文件、输出解析、home 策略、reviewer 候选及文档/发布规则由定义驱动。任务提交 `6b7c132db76cc00a1245bc45fd992afe3109dcc6`，绑定审核源 `64d0c2cf5fa2e8c8930f56967dced09a2187478d`，最终 develop 合并 `a8b5afac5d79a09399ab12b097691d1f2212c1dd`。
- 验收：本轮 wrap-up 6/6；原卡 16 条冻结验收与已记录非门禁偏差原样保留，闭合门禁满足，不声称全部无偏差。
- 验证：主控 `go test -json -p 2 ./... -count=1` 日志独立计数为 21 包、1682 pass、0 fail、1 skip；唯一 skip 是 TestWindowsConsoleLauncher。日志 SHA-256 为 `07fa9dee676fa6646de9cd9e530b753ec23a893d4a782dbefcee1651ceccb442`，文件树 `5e9f52f03de08e89d1f0acf2badb9d3274ff69ab` 与 merge commit 一致。build/vet/GOOS=windows build 的执行记录全部退出 0，本执行体未重复整仓测试。完整快照为 `verification/wrap-up-epoch12.json`。
- 审核：计划 revision 3 sealed；批次一闭合于 d97964c，PM Claude r6 / QA Grok r8 PASS；批次二闭合于 `64d0c2cf5fa2e8c8930f56967dced09a2187478d`，PM/QA Codex gpt-6-astra PASS、findings 和 NON_BLOCKING 均为空。CSA/Hacker 为项目例外 N/A。合并冲突由主控在独立集成分支解决，用户明确授权 merge 并跳过合并后追加审核，后者不是新一轮 PASS。
- 收尾：验证任务交付属于审核源、审核源属于本地与远端 develop；合并父提交依次为 4dd5a07deafb60dd22b3075db1da72c1cadd32cd 与 `64d0c2cf5fa2e8c8930f56967dced09a2187478d`。本任务工作树、本地及远端分支已删除；原有 kander/kander.exe 构建产物已核验摘要并保留到 `/tmp/kander-review-wrapup-builds-_1wmeu85/`。不清理组级资产，不关闭 Codex/herdr 会话。随后以绑定源 SHA 完成 done 并执行定向 check。
- 未决（5）：[PM][low/deferred] 非归档 Codex raw 输出多末尾换行，格式偏差保留；[QA][low/deferred] prompt_files 使用子目录且父目录不存在会失败，可使用单段文件名；[QA][suggest/deferred] 缺成功 --version 的 reviewer 不进面板，仍可手改 JSON；[验证][N/A] 原生 Windows 未运行，仅交叉构建；[审核][N/A] 用户明确跳过本次合并后追加审核，没有新 run。前三项保留原作者的非门禁处置，后两项引用 verification/wrap-up-epoch12.json。
- 总结：本卡已集成并完成授权清理，代码最终位于 develop；done 以本次原子完成回执为准。

PM-07 的历史 deferred 原件不可变：当前合并后的 reviewerFromConfig 已无 codex 字面量回退，核验来源为上游 fba2e46 的完整配置要求及最终合并。此处仅记录最终代码观察，不伪造本卡新修复或改写旧审核范围。原 PM-09/10 已闭合、QA r7 失败已由 r8 有效链处理，均不列当前阻塞。

## 2026-09-09 epoch 11：同步到新组基线

本节是当前交付；下方早期报告的旧 SHA、未闭合状态及当时失败属于历史。

- 2026-09-09 组分支同步轮（dispatch `codex-agentdef-review-sync-e756-20260909`，epoch 11）：fetch 后确认本地和远端组 HEAD 均为 `e756f7d672bb4cc2a488a476485f6821e7b43d2e`，前置 A1 的 b216ca25 与 parser 的 e756f7d 已有 completed 回执且实际包含在组分支。核实旧 `agent-def-batch-one` closed.json 闭合于 d97964c06adb942b94b25c7e5c5bfb04795172e4，PM/QA PASS，CSA/Hacker N/A。在本任务工作树执行 `git rebase origin/group/20260908-agent-definition-group` 无冲突，`git range-diff d97964c..fa8da8a e756f7d..HEAD` 显示 `fa8da8a = 6b7c132`；相对组基线仅 definition_unix_test.go 0 增/1 删（末尾空行），文件现 352 行，未新增代码、符号或测试。最终 `6b7c132db76cc00a1245bc45fd992afe3109dcc6`。该提交上 `go test -json -p 2 ./... -count=1`：21 包通过；1480 pass / 0 fail / 1 skip（含子测试）；跳过 TestWindowsConsoleLauncher。`go vet ./...`、`go build ./...`、`GOOS=windows go build ./...` 退出 0；`git diff --check 8abdfe2e6430a00e19383f84f23088a8ff743be0 HEAD` 退出 0。使用期望旧远端 SHA fa8da8a 的显式 force-with-lease 推送，远端任务 SHA 已核实。组分支未改；历史失败/跳过和四项非门禁处置保留；新交付等待主控接收。

### 自检与边界

- 组基线：e756f7d672bb4cc2a488a476485f6821e7b43d2e；最终任务提交：6b7c132db76cc00a1245bc45fd992afe3109dcc6。fetch 校验本地/远端组 SHA 一致，未出现漂移。
- 重放前后 range-diff 判为同一补丁；与新组基线 diff --numstat 仅一项 0/1，旧空行修复保留，无需改合同、处理非门禁意见或新开 finding。
- 改动文件 352 行，低于 1000 行；没有新增行为、死代码或重复测试；相对原审核基线 diff --check 干净。
- 验证命令和结果对应最终提交：`go test -json -p 2 ./... -count=1`：21 包通过；1480 pass / 0 fail / 1 skip（含子测试）；`go vet ./...`、`go build ./...`、`GOOS=windows go build ./...` 均退出 0。
- 测试跳过：TestWindowsConsoleLauncher。Windows 只完成交叉编译，未声称原生运行验证。历史 reviewer 失败及旧 EOF 自检失败仍保留于原件/前轮记录，不伪造为成功；QA r7 由 r8 解析的事实见 closed.json。
- 原始本轮验证日志：/tmp/kander-review-e11-tests.jsonl、/tmp/kander-review-e11-builds.json。
- 推送仅针对本任务分支，使用 `--force-with-lease=refs/heads/agent-review-template:fa8da8a9923749a80d44c7fa8038e97a0f2e620a`；远端任务最终 SHA 已读回确认。未改组分支或 develop。
- 旧批次 PM/QA PASS 仅覆盖闭合目标 d97964c；不把该 PASS 延伸为本轮新补丁已完成组级审核。当前四项非门禁未决与对应作者原件保持不变；待主控接收、后续批次、封存计划和集成 wrap-up。

## 2026-09-09 epoch 10：QA r8 非门禁作者处置

本节记录当前状态；下方早期报告中的 QA r7 参数失败属于历史，QA r8 已成功执行，报告无门禁 finding。批次是否闭合仍由主控处理。

- 2026-09-09 QA r8 非门禁处置同步轮（dispatch `codex-agentdef-qa-r8-review-20260909`，epoch 10）：完整读取 r8 report/assignment/sidecar，独立核对组目标 `d97964c06adb942b94b25c7e5c5bfb04795172e4` 对应实现与修复区间。现象未变，Codex 新增 deferred 2 / fixed 0 / rejected 0：`reviews/qa-agent-def-b1-r8/dispositions/qa-n1-codex-deferred-r8.json`、`reviews/qa-agent-def-b1-r8/dispositions/qa-n2-codex-deferred-r8.json`。两项均使用 r8 自己的 run/report_hash/original，报告 lineage 指向 r6；旧作者 r6 原件保留。核验为只读源码与 Git 对照，未新跑测试或声称动态复现；无代码/rebase/分支变化。任务交付仍为 `fa8da8a9923749a80d44c7fa8038e97a0f2e620a`。QA r8 已成功执行且报告无 gate findings；r7 失败保留为历史，不再作为当前 QA 尚未执行的结论，批次闭合仍待编排器。

- QA-N1（low/deferred）：规范路径可含 prompts/guide.md，写入不创建父目录；POSIX 缺失父目录被 openat 拒绝。用户影响：定义校验通过但审核启动失败，现阶段可使用单段文件名。原文、定位和依据见本轮 QA-N1 处置原件。
- QA-N2（suggest/deferred）：审核候选必须通过 --version 探测，而执行候选无此过滤。用户影响：无成功版本输出的自定义 reviewer 只能通过 JSON 配置。原文、定位和依据见本轮 QA-N2 处置原件。
- 既有 PM-07 recommend、PM-08 low 继续保持 deferred；不重复计数 r6/r8 同源问题。当前共四项非门禁未决，另有组级闭合/集成/wrap-up 待办。
- 验证：相关实现从组目标到任务 HEAD 无差异；r6 到 r8 修复区间未改提示词写入/父目录遍历/审核探测，options.go 仅改 effort 显隐。完整命令写入处置的 verification 字段。本轮不改代码，不需重复全量测试。
- 两条新原件均 author=codex、run_id=qa-agent-def-b1-r8，绑定真实 r8 报告 SHA-256 890fffd60298ac41962aed5f5144aaefef9a89b8e0b06354242adfcd50a34662。跨轮 lineage 来自 r8 findings，不伪造跨 run 的 previous_record_id。

## 2026-09-09 epoch 9：作者处置 Git 证据映射

本节为上一轮交付报告之后的追加记录，仅更新审核证据。

- 2026-09-09 作者处置 Git 映射同步轮（dispatch `codex-agentdef-dispositions-review-20260909`，epoch 9）：独立核验旧 PM-02/PM-05 fix_commit 均非组 HEAD `d97964c06adb942b94b25c7e5c5bfb04795172e4` 祖先。追加 Codex 本人记录 fixed 2：`reviews/pm-agent-def-b1-r4/dispositions/pm-02-codex-git-map-e9.json` 绑定 `ee8fab70b817faca93b5b3b0d900bb9ba93da2be`；`reviews/pm-agent-def-b1-r4/dispositions/pm-05-codex-git-map-e9.json` 绑定 `8f8f0cd0627fca918e7383ea4c3ebd6962864652`。各自 previous_record_id 指向原记录，原文/report_hash/status/mechanical 与旧作者原件保留。祖先验证均退出 0；PM-05 新旧 patch-id 相同，PM-02 按组内累计修改与资格校验路径核实。任务 HEAD `fa8da8a9923749a80d44c7fa8038e97a0f2e620a` 上 `go test -json ./internal/config ./internal/menu ./internal/i18n ./internal/review -count=1` 为 350 pass / 0 fail / 0 skip（含子测试），4 包通过；这些包相对组 HEAD 除末尾空行外相同。没有代码、分支或提交变更；原交付 SHA 保留，组级批次闭合仍待编排器。

### 核验事实

- 两条新记录 author 均为 codex；PM-02 的 previous_record_id 为 pm-02-fix-r4，PM-05 为 pm-05-fix-r4。原 original/report_hash/status 保留；PM-02 原来没有 mechanical，仍不增加；PM-05 保持 dead-code。
- 两个旧 fix_commit 对组 HEAD 的祖先检查均退出 1；两条新绑定提交均退出 0。
- PM-02 新旧补丁并非逐字相等：组内前序 1f1924b 已加入资格限制与拒绝用例，ee8fab7 补齐 AgentFor 的继承门禁。ee8fab7 的 agents.go/agents_review.go 与组 HEAD 完全一致；现有测试确认纯 path+dialect 或 dialect-only 不成为 reviewer。
- PM-05 stable patch-id 新旧同为 ba54f9ae34663f7113b57444dd356b329194974d；三份 locale 各删两键，新提交与组 HEAD 全仓均零引用，统一错误出口保留。
- 350 项定向测试通过；原始日志 /tmp/kander-review-disposition-epoch9-tests.jsonl；完整命令、提交及核验事实写入两个新处置原件。
- 本轮未运行新 reviewer、未重开 finding；组级 aggregate/close 仍由编排器负责。本次 sync 完成后回到 review；任务工作树、分支和 fa8da8a9923749a80d44c7fa8038e97a0f2e620a 原交付保留。

## 本轮交付

- 任务：20260908-agent-review-template-task；SIZE large；执行者 Codex；2026-09-09。
- 分支：`agent-review-template`；工作树：`/home/dualf/works/kander/worktrees/agent-review-template`。
- 最终提交：`fa8da8a9923749a80d44c7fa8038e97a0f2e620a`；远端同名任务分支已确认一致。
- 本轮基线：组分支 `d97964c06adb942b94b25c7e5c5bfb04795172e4`。原交付 `b653e810e748783979daba1b6a21f200c143722f` 已在组分支。本轮只删除测试文件末尾 1 个空行，没有业务逻辑修改。
- 派发：`edaf20040052b5574ad4ffcd1f85e2d9`，sync，epoch 8。本轮结束通过绑定 move review 发布 completed 回执。
- 主工作树 develop 与组分支均未修改。任务工作树与分支保留，等待组级接收、审核和 wrap-up。

## 验收证据

最终确认：待定/16；下列是可核对的实现与测试证据，不替代组级审核门禁或用户对偏差的接受。

1. 定义 schema 与校验：`internal/config/agents_review.go`；`TestReviewTemplateValidation` 覆盖成对声明、cwd、home_policy、stderr、空 argv、未知占位符、stdin 两种错误组合、路径穿越与未声明提示词引用。
2. 失败成功状态：`TestParseReviewOutputSuccessBeforeExtract`；Codex 非零退出另由 `TestCodexNonZeroExitRejectsReportText` 覆盖。
3. 公共解析器：审核调用 `process.ParseReviewOutput`；`TestReviewOutputParsingUsesProcessEntry` 与 stderr 拒绝用例通过。
4. 内置定义保留 JSON 默认格式；成功/失败的表驱动对照通过。
5. home 策略：`TestCustomAndBuiltinHomePolicy`、`TestBuiltinHomePolicyClaudeVsCursor` 通过。
6. 指定执行函数读定义；`TestBuiltinReviewDefinitionsMatchPrevious` 对照 argv、环境与配置标志。更广义的包内无内置名目标仍有 PM-07 历史偏差，保留 deferred。
7. reviewer 资格按声明；`TestReviewAgentNamesFollowsDefinitions`、`TestStartOnlyReviewerRejectedAndDoctorLeavesIt` 通过。PM-10 的文档修正与成对字段不填充行为一致。
8. 自定义 file/raw、stdout/json、regex、ndjson：`TestCustomReviewerOutputSources` 与 inspection 注入测试通过。
9. stdin none 与额外提示词：`TestCustomReviewerStdinNonePromptFiles` 验证注入内容、0600 权限及 stdin 未携带指令。完整路径能力存在 QA-N1 子目录偏差，未宣称完全验收。
10. 内置 stdin 与提示词内容：`TestBuiltinReviewStdinAndPromptBytes` 及相关定义测试通过。
11. 全量既有测试通过；本轮未删测试或改变断言，只删文件末尾空行。
12. 可执行优先级：`TestReviewExecutableIgnoresExecutionDefinitions`，自定义来源测试使用自身 path 回落。
13. 面板候选：`TestReviewerChoicesFollowReviewTemplates`；保留 QA-N2 对 --version 过滤的建议。
14. Go 构建、vet、全量测试和 Windows 交叉编译通过，详见下一节。
15. `docs/custom-agents.md` 已解释审核字段、可执行优先级和作者只读责任，并链接 `docs/output-parsing.md`。
16. 发布规则已同步，逐条改后原文列于本报告末节。

## 最终验证与交付自检

验证针对 `fa8da8a9923749a80d44c7fa8038e97a0f2e620a` 所对应的文件树执行；验证后提交只记录同一文件树，没有后续代码修改。

- `go test -json ./...`：退出 0；21 个包通过；1422 项通过（含子测试），0 失败，1 跳过。跳过项为 POSIX 环境的 `TestWindowsConsoleLauncher`。
- `go vet ./...`：退出 0。
- `go build ./...`：退出 0。
- `GOOS=windows go build ./...`：退出 0；未声称完成 Windows 原生运行验证。
- `git diff --check 8abdfe2e6430a00e19383f84f23088a8ff743be0`：修正后退出 0，含完整审核区间。
- 代码行数限制扫描：新增及改动文件超过限制的违规数 0。
- `git diff --exit-code --ignore-blank-lines`（本轮空行修改尚未提交时）：退出 0，确认无非空行变化。
- 文档、死代码及重复测试自检：本轮只删空行，没有新增行为、符号或测试；既有已关闭的 PM-02/05/09 不重开；四项历史非门禁问题照实保留。
- `kander check 20260908-agent-review-template-task`：写报告前检查通过（ok: 1 tasks）。最终移回 review 后再作定向检查。
- 原始本轮测试日志：`/tmp/kander-review-template-final-tests.jsonl`；构建结果：`/tmp/kander-review-template-final-builds.json`。这些是可清理的临时验证日志，以上结果保存在本报告。

## 审核与未决

- PM：r6 已确认 PM-02/05/09 关闭；PM-10 为 medium/documentation，`reviews/pm-agent-def-b1-r6/dispositions/pm-10-fixed-r6.json` 的修复提交和当前文档/测试一致。原件 author=grok 保留，接管者不冒充作者。
- QA：r7 execution_status=failed、semantic_status=unassessed。错误为 `Error: Couldn't set model 'gpt-6-astra': Invalid params: "unknown model id".`；这是调用参数失败，不能算 PASS，也不能算三次服务端故障。修正审核调用参数、继续批次由编排器承担。
- CSA/Hacker：N/A，仓库 AGENTS.md 例外。
- `kander review progress`：计划 `agent-def-embed-cycle` 为 pending，原因 `unsealed-plan`、`agent-def-batch-one`。

- PM recommend deferred：`reviews/pm-agent-def-b1-r6/dispositions/pm-07-deferred-r6.json`；reviewerFromConfig 仍用 codex 字面量兜底。
- PM low deferred：`reviews/pm-agent-def-b1-r6/dispositions/pm-08-deferred-r6.json`；Codex 非归档报告末尾多一个空行。
- QA low deferred：`reviews/qa-agent-def-b1-r6/dispositions/qa-n1-deferred-r6.json`；额外提示词使用子目录时，缺失父目录导致启动失败。
- QA suggest deferred：`reviews/qa-agent-def-b1-r6/dispositions/qa-n2-deferred-r6.json`；没有成功 --version 探测的自定义 reviewer 不进入面板候选。
- QA N/A 未完成：`reviews/qa-agent-def-b1-r7/sidecar.json`、`reviews/qa-agent-def-b1-r7/error.log`；grok 被传入 gpt-6-astra，报 unknown model id；本批次未关闭，计划未封存，组级接收、审核、集成和 wrap-up 待编排器。

前四项保持已有非门禁处置。本轮没有收到重新打开这些 finding 的具体派发，不改写作者原件。PM-09 在 r6 已关闭，从当前未决清单移除，旧 SUMMARY 和旧 deferred 原件仍保留。

解除组级阻塞的条件：编排器接收本轮最终提交，完成既有批次的所需审核与处置、封存计划、完成组分支合入 develop，并发出携带实际 Git 证据的 wrap-up 派发。当前 sync 不具备 done/wrap-up 语义。

## 发布规则改后原文

以下逐条摘自最终任务工作树，属于验收第 16 条的原文证据。

### KANDER-REVIEW-RULES.md / Reviewer Selection

```text
- Reviewers are the four built-in agents plus any configured agent that
  declares a review template (`args.review` together with `review.*`).

  The public review entry on all platforms is `kander review` under the command root, which enters the single gate implementation.

  On Windows, prefer the reviewer `.exe`.

  When only `.cmd`/`.bat` exists, launch through an explicit `cmd.exe /d /s /v:off /c` and the argument encoding of that reviewer's adapter layer.

  This is not a general invocation contract for arbitrary batch scripts.

  Built-in isolation arguments live on each agent's definition. A custom
  reviewer's read-only posture is the definition author's responsibility;
  Kander still validates the result and isolates review-private directories.

  Apart from the CLI and isolation arguments declared for that reviewer,
  every rule in this file is identical for all reviewers.

  Select the command entry per `KANDER-AGENTS.md` "Scope".

| reviewer | argument | CLI | isolation |
| -------- | -------- | --- | --------- |
| Codex | `codex` | `codex` | from the embedded definition |
| Claude | `claude` | `claude` | from the embedded definition |
| Grok | `grok` | `grok` | from the embedded definition |
| Cursor | `cursor` | `cursor-agent` | from the embedded definition |
| custom | the agent name | `review.path` or `path` | author's responsibility |
```

### KANDER-KANBAN-RULES.md / Configured Execution Agents

```text
- The optional `agents` configuration declares execution names, executable paths, pane process names, CLI dialects or argv templates, session modes, and optional review templates. A custom agent may be a reviewer when it declares `args.review` and `review.*`. Built-in review executables still prefer `*_REVIEW_BIN` over the embedded `path`; a user `agents.<name>.path` overlay does not change built-in review. Custom reviewers use `review.path` and otherwise fall back to `path`. Built-in isolation stays on the definition; a custom reviewer's read-only posture is the author's responsibility.
```

### KANDER-BASE-RULES.md / Single Review

```text
- The command keeps built-in reviewers read-only through the isolation arguments declared on each agent's definition, and validates the output. Custom reviewers that declare a review template are accepted; their read-only posture is the definition author's responsibility. For every reviewer, Kander still isolates the review-private directories and afterwards checks the Git-visible state of the target worktree, which does not detect writes outside it or to ignored paths inside it.
```

### KANDER-KANBAN-RULES.md / Command Contract

```text
`kander pick [task-id]` moves a `backlog/` card into `todo/` through the same gate as `kander move <task-id> todo`; the two are interchangeable. Without a task ID it lists the `backlog/` cards and asks which one to pick; automation passes the ID. The move options after the state name are described under "Entries and Documents" and "Durable Dispatch". After `kander review`, the words `plan`, `extend-plan`, `assign`, `disposition`, `map-legacy`, `aggregate`, `advance`, `close` and `progress` select an evidence subcommand; the optional reviewer argument accepts any configured agent that defines a review template, as described in `KANDER-REVIEW-RULES.md` "Reviewer Selection".
```

### KANDER-REVIEW-RULES.md / 提示词投递

```text
- On all platforms the full prompt is written to a UTF-8 task file in the
  review runtime. Delivery follows the reviewer definition: `review.stdin`
  is `instruction` (default) or `none`. With `instruction`, the reviewer
  receives only a short instruction that names that file path on stdin.
  With `none`, the same instruction is passed through `{instruction}` in
  argv and stdin is not that pipe. Extra files in `review.prompt_files`
  are rendered into the runtime and referenced as `{prompt_file:<name>}`.
- Built-in reviewers use `review.stdin: instruction` and declare no
  `review.prompt_files`. Grok's definition keeps `--prompt-file` pointing
  at Kander's `prompt.txt`.
```
