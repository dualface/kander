# 公共输出解析结构: source/format/select/parse/join 与行条件成功判定

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260908-agent-definition-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 02:51
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:w2T:t1B:w2T:p1B
- STARTED_AT: 2026-09-09 09:54
- FINISHED_AT: 2026-09-09 23:27
- TASK_BRANCH: 20260909-process-output-parser
- RESULT: completed

- DISPATCH_ID: codex-agentdef-wrap-parser-20260909

- EXECUTION_EPOCH: 5

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
## IMPLEMENTATION

- 2026-09-09 首轮: 交付解析器与文档, 提交 `116bd740953cce2040d13934038cb6601c01966a`; 组分支随后已包含该提交.
- 2026-09-09 修复轮 (dispatch `e65206d1b04fca0d76d191122a1f18f7` epoch 1): 首轮 Self-Check 第 4 项漏检死代码, 记为失败并在本轮与 PM-03/PM-04 一并修复. 任务分支 rebase 到组分支 HEAD `e1c01277075599cb0395bf62f16855cebf04b060` 后提交 `f5dc7c31c665c347b63bb8fd389822197371118a` (已 push). 处置: fixed 2 / 其余 0. 记录 `reviews/pm-agent-def-b1-r4/dispositions/pm-03-fix-r1.json`, `reviews/pm-agent-def-b1-r4/dispositions/pm-04-fix-r1.json`.
- 验证 @ `f5dc7c31c665c347b63bb8fd389822197371118a`: `git grep` 对 `ReviewSources`/`TerminalSources`/`jsonStringOrRaw` 零命中; `go test ./internal/process -count=1` 53 pass / 0 fail; `go vet ./internal/process` 与 `go build ./internal/process` 通过; `git diff --check` 干净; `output.go` 434 行.

- 2026-09-09 Codex 接管轮（dispatch `33c325095a4def4810d3db2ffa2aa849` epoch 3）：复核历史交付后补齐两处验收校验缺口，提交 `df567055d3d884c9acb4b2d5bec0a926b87b1a5a`，已 push；无冲突 rebase 基线 `d97964c06adb942b94b25c7e5c5bfb04795172e4`。修改 3 文件 +65/-3。旧 PM-03/PM-04 保持已关闭，本轮未新增或改写审核作者处置。Delivery Self-Check 1–7 的逐项命令与输出、最终 process 74 pass、全量复跑 1443 pass / 0 fail / 1 skip（含子测试）、build/vet/Windows build 结果见 [verification.md](verification.md)。初次全量测试的现有 liveness 失败同样保留；接管依据见 TAKEOVER_AUDIT。

- 2026-09-09 基线同步轮（dispatch `codex-agentdef-parser-sync-b216-20260909` epoch 4）：fetch 重试后核实组 SHA `b216ca25cb328300b3a45c54bc00a5ca442a89d7`；从 `df567055d3d884c9acb4b2d5bec0a926b87b1a5a` 无冲突 rebase 得 `e756f7d672bb4cc2a488a476485f6821e7b43d2e`，原补丁逐字节不变（3 文件 +65/-3），已用绑定旧 SHA 的 force-with-lease 推送并核对远端。最终全量测试 1480 pass / 0 fail / 1 skip、process 74 pass（均含子测试），build/vet/Windows build 与 targeted kander check 通过；自检 1–7、空 select / equals 重点用例、TLS 首次失败和历史 liveness 失败见 [verification-sync-b216.md](verification-sync-b216.md)。旧批次已核对正式关闭，不重开；新交付待编排器接收。

## SUMMARY

- 交付：`20260909-process-output-parser` @ `e756f7d672bb4cc2a488a476485f6821e7b43d2e`，本地与远端一致，基于组分支 `b216ca25cb328300b3a45c54bc00a5ca442a89d7`；新交付待编排器串行接收。
- 验收与验证：原修复补丁逐字节保留；最终全量测试 1480 pass / 0 fail / 1 skip，process 74 pass（含子测试），build/vet/Windows build 与 kander check 通过。自检与完整证据见 [verification-sync-b216.md](verification-sync-b216.md)，当前报告见 [report.md](report.md)。
- 审核：旧 batch `agent-def-batch-one` 已关闭于 `d97964c06adb942b94b25c7e5c5bfb04795172e4`（PM/QA PASS；CSA/Hacker N/A），证据 `reviews/batches/agent-def-batch-one/closed.json`。历史范围不重开；新增修复等待后续批次，现有计划仍未封存；本轮 no run produced。
- 未决（2）：后续组级接收、审核、合入与 wrap-up；上一轮 liveness 偶发失败根因未确定，本轮未复现，原记录见 `verification.md`。任务分支与工作树保留。首次 fetch TLS 失败已正常重试恢复。

### 2026-09-09 wrap-up 当前结论（旧条目保留为历史）

- 两批适用审核已 CLOSED，计划 revision 3 已封存；本卡无未决审核项。合并后追加审核由用户明确跳过，不记新 PASS。
- 本卡提交 `e756f7d672bb4cc2a488a476485f6821e7b43d2e` 已包含于组源 `64d0c2cf5fa2e8c8930f56967dced09a2187478d`，并通过合并提交 `a8b5afac5d79a09399ab12b097691d1f2212c1dd` 进入本地与远端 develop；原历史未重写。
- 本卡工作树、本地及远端任务分支已删除；核验与清理输出见 [wrap-up/codex-agentdef-wrap-parser-20260909-5.md](wrap-up/codex-agentdef-wrap-parser-20260909-5.md)。最终合并树全量日志核实为 1682 pass / 0 fail / 1 skip，构建/vet/Windows 交叉构建证据通过；本轮未重复测试。
- 剩余验证边界为历史 liveness 偶发失败根因未关闭、原生 Windows 未运行；均保留原记录。会话与 herdr tab 保留；本轮以 epoch 5 的组源 SHA 完成 done。

## TAKEOVER_AUDIT

- 2026-09-09 Codex 接管（dispatch `33c325095a4def4810d3db2ffa2aa849`，epoch 3，accepted receipt 的 `replayed=false`）：旧实现与修复提交 `116bd740953cce2040d13934038cb6601c01966a`、`f5dc7c31c665c347b63bb8fd389822197371118a` 均存在，后者已包含于本地和已 fetch 的远端组分支 `d97964c06adb942b94b25c7e5c5bfb04795172e4`。工作树原本干净。PM-03/PM-04 的旧作者原件及 PM 增量复核与实际代码一致，保持关闭，不替换作者处置。
- 前任的「合同 11 项仍由首轮覆盖」作为历史声明保留于本节，首轮完整报告另存 [report-initial.md](report-initial.md)。本轮不沿用该完整性结论：在 `f5dc7c31c665c347b63bb8fd389822197371118a` 加入回归用例后，`go test ./internal/process -run 'TestValidateConditionEqualsJSONValues|TestDecodeOutputSpecRejectsEmptySelectOnJSON|TestValidateOutputRejectsIllegalCombinations' -count=1` 退出 1。JSON 下显式空 `select` 和 select/success 的非法 `equals` 均返回 `<nil>`，违反验收第 1 项。修复为检查 Select 是否声明、校验 RawMessage 是合法 JSON；不改公开类型/函数签名，合法调用保持兼容。
- 新提交 `df567055d3d884c9acb4b2d5bec0a926b87b1a5a` 已无冲突 rebase 到组分支 `d97964c06adb942b94b25c7e5c5bfb04795172e4`，只改 `internal/process/output.go`、`internal/process/output_test.go`、`docs/output-parsing.md`。另外补充 absent 与 equals 跨行不能凑成成功的合同用例。
- 最终提交默认并行的 `go test -json ./... -count=1` 出现一次 `TestSubscriptionDispatchDeadlineDoesNotWaitForProbe` 失败（`subscribe_dispatch_runtime_test.go:62`：临时 `events` 文件 `no such file or directory`），1442 pass / 1 fail / 1 skip（含子测试）。该模块相对前任交付无改动；同提交单项 `-count=5` 通过。尚不能确定根因，未修改范围外代码；随后 `go test -json -p 2 ./... -count=1` 退出 0（1443 pass / 0 fail / 1 skip，含子测试），完整证据见 verification.md。最终提交的 `go build ./...`、`go vet ./...`、`GOOS=windows go build ./...` 均已通过。

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

## WRAP_UP_RECORDS

- 2026-09-09 Codex 原执行体，dispatch `codex-agentdef-wrap-parser-20260909` epoch 5：已核验封存计划、两批关闭原件、主控最终测试日志哈希与真实 Git 集成；完成本卡工作树及本地/远端任务分支清理。容量暂停后的补充通知沿用原 accepted 授权，仅继续记录和 done，没有重做清理。详细记录 [wrap-up/codex-agentdef-wrap-parser-20260909-5.md](wrap-up/codex-agentdef-wrap-parser-20260909-5.md)；当前报告在 report.md 末尾追加。所有旧交付、验收、验证、失败与作者处置保留。
