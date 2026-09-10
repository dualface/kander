# kander review 将报告与元数据归档到卡片目录并写入索引

- TYPE: Feature
- TASK_GROUP: 20260907-review-archive-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-07 13:39
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

让 `kander review` 自己把每次审核的完整报告、运行元数据和本轮输入 (任务上下文与复审上下文) 归档到被审核卡片的 `reviews/` 目录, 并向卡片正文追加一行机器可解析的索引; `kander check` 据索引校验归档完整性与同一 base 下的增量链.

今天 (`internal/review/execute.go`, `args.go`) 报告只打到 stdout, 由调用它的 Agent 自行存进临时目录并在本轮结束后清理 (规则 `KANDER-REVIEW-RULES.md` "Report Files"). 留下来的只有 Agent 写进卡片的摘要. 于是: 同一角色的增量复审拿到的是 Agent 对上一轮 finding 的转述而非原件; 任务组后一批次拿到的上一批未解决项也是转述; 编排会话被接管或重启后, 上一轮审到哪个 commit、哪个角色在哪个 commit 上通过, 全部丢失; 规则里"base 链连续"、"最终 commit 是每个通过 commit 的后代"、"同一角色同一轮不换 Reviewer"这些不变量无人能查; 也没有任何轮数与耗时数据.

## USER_DECISIONS

以下为用户在本次讨论中明确确认的决定:

1. 审核结果应落盘供 Agent 使用, 而不是只留在当轮 Agent 的上下文里.
2. 不论单卡还是任务组成员卡, 每次审核的结果依次写入卡片目录; 任务卡正文只追加审核结果文件的索引.
3. 按提议的三卡拆分建立任务组 `20260907-review-archive-group`, 本卡为第二张, 依赖第一张 (统一目录形态); 建卡后不启动.

## EXPECTED_OUTCOME

- `kander review` 新增可重复的 `--task <task-id>` 参数. 给出时, 每次 Reviewer 进程实际被启动的运行 (无论最终成功或失败) 都在每张指定卡片的 `reviews/` 下留下一组同前缀的文件: 报告正文、元数据 sidecar、任务上下文副本、复审上下文副本; 并在卡片正文 `## REVIEWS` 节追加一行索引. 不给 `--task` 时行为与今天一致, 且不触碰看板.
- 索引行与 sidecar 足以复现该轮调用 (agent、模型、档位、CWD、base、commit、reviewed-commit、任务上下文、复审上下文), 并能提供轮数、耗时与失败次数统计.
- 报告语种跟随卡片 `LANGUAGE`, 与看板规则中"`LANGUAGE` 覆盖审核报告"的承诺一致.
- `kander check` 对带索引的卡校验: 索引指向的文件存在且哈希一致; 同一 base 下同一角色的增量轮 `reviewed` 等于该角色更早一次成功轮的 `commit`.
- Reviewer 进程仍是无记忆的一次性只读进程, 不能写归档; 归档由门禁在 worktree 校验通过后写入.
- 规则文本中的报告存放、清理与调用参数描述更新为新行为.

## ACCEPTANCE_CRITERIA

- [ ] `go build ./... && go test ./... && go vet ./...` 全绿, `gofmt -l .` 无输出, `git diff --check HEAD` 无告警.
- [ ] 参数: `kander review [agent] [--task <task-id>]... <CWD> <base> <commit> <role> <task-goal|spec> [review-context] [reviewed-commit]`; `--task` 可重复, 必须出现在位置参数之前 (agent 名之前或之后均可); 未知选项按用法错误退出 2. 不带 `--task` 时: 不定位看板, 在没有任何看板的环境下照常可用, 现有 review 测试不改断言全部通过.
- [ ] 带 `--task` 时的前置校验全部发生在 Reviewer 启动之前: 看板以审核目标 `CWD` 为起点按看板规则既有的定位顺序 (`KANBAN_DIR` -> 所属仓库主 worktree 的 `kanban/` -> 向上查找) 定位, 不以进程当前目录为起点; 每个 task-id 必须是已存在的目录卡且状态为 `working/` 或 `review/` (命中尚未迁移的单文件卡时, 错误信息提示先运行 `kander init`); 多张卡按报告语种条 (卡片 `LANGUAGE`, 缺则配置) 的有效值必须一致; 任一不满足则退出 2, 不启动 Reviewer, 不写任何文件.
- [ ] 归档文件名前缀为 `<UTC 时间戳 YYYYMMDDTHHMMSSZ>-<role>-<commit 前 12 位>`, 四个文件为 `<前缀>.md` (报告, 与 stdout 输出逐字节一致)、`<前缀>.json` (sidecar)、`<前缀>.spec.md` (任务上下文: 字符串原样或 spec 文件内容副本)、`<前缀>.context.md` (复审上下文原样, 首轮为规则定义的空值). `reviews/` 不存在时创建. 同一前缀已存在时不覆盖, 改用递增后缀并在索引与 sidecar 中记录实际文件名.
- [ ] sidecar 为 UTF-8 JSON, 至少含: `schema_version`, `task_ids` (本次全部 `--task`), `role`, `reviewer`, `model`, `effort`, `cwd`, `base`, `commit`, `reviewed_commit` (无则 null), `report_language`, `started_at`, `finished_at`, `duration_seconds`, `status` (`ok` | `failed`), `failure` (失败时的门禁消息, 成功为 null), `report_file`, `report_sha256`, `kander_version`.
- [ ] 索引行追加到卡片 `spec.md` 的 `## REVIEWS` 节末尾, 节不存在时在文档末尾创建; `kander new` 的模板为所有卡预置空的 `## REVIEWS` 节 (large 卡在 `DISCUSSION` 之后, small 卡在 `SUMMARY` 之后), `REVIEWS` 作为新的 section token 加入 `schema.go`. 行格式固定为一行: `- REVIEW: <前缀> role=<role> reviewer=<agent>/<model|-> status=<ok|failed> base=<完整 SHA> commit=<完整 SHA> reviewed=<完整 SHA|-> report=reviews/<报告文件名>`. `board` 包提供解析该行的公开函数, `check` 与后续卡复用.
- [ ] 归档时机: 在 Reviewer 进程回收、worktree 未改校验与运行时目录清理之后写入; 成功轮的归档写入失败时, 报告仍完整输出到 stdout, 命令以独立的非零码和明确消息退出, 不把已成功的审核报成原因不明的失败.
- [ ] 归档分界以 Reviewer 进程是否已启动为准 (`execute.go:158` 的 `reviewStarted`): 已启动的失败轮 (超时、Reviewer 非零退出、输出无效、worktree 被修改、残留进程) 同样归档, `status=failed`, 报告文件保存当时能拿到的 stdout; 参数与前置校验失败、运行时或证据文件写入失败、进程启动失败 (退出 127) 都在启动之前, 不归档.
- [ ] 多个 `--task` 时, 每张卡各得一份完整且内容相同的四个文件与一行索引; 任一卡写入失败时其余卡的写入结果不回滚, 错误逐卡报告.
- [ ] 并发安全: 两个 `kander review --task X` 进程同时完成时 (模拟 PM 与 QA 并行), `spec.md` 最终包含两行索引且正文其余部分不变; 用现有 `harness_test.go` 的假 Reviewer 测试并发写入. 追加使用 `internal/fs` 的独占锁与原子替换.
- [ ] 报告语种: 带 `--task` 时以卡片 `LANGUAGE` 作为 `reportLanguageRule` 的输入, 卡片缺该字段时回落配置 `agent_language` (与 `start`/`notify` 的既有回落一致); 不带 `--task` 时维持读配置.
- [ ] `reviews/` 目录与其中文件经 `internal/fs` 创建, 权限沿用看板目录的既有约定 (POSIX 继承与 umask, Windows 受保护 DACL); `reviews` 或目标文件路径存在符号链接/reparse point 时拒绝写入并按归档失败处理.
- [ ] `kander check` 新增归档校验, 状态范围与现有契约检查一致 (`todo/working/review`, `--all` 时全部): 每行索引能解析; `report=` 指向的文件与同前缀 sidecar 存在; sidecar 的 `report_sha256` 与报告文件一致; 对同一 `base` 同一 `role` 的行, 任一 `status=ok` 且 `reviewed` 非 `-` 的行, 其 `reviewed` 必须等于该角色更早某行 `status=ok` 的 `commit`. 违反项逐条报为问题.
- [ ] `internal/review` 新增对 `internal/board` 的单向依赖; `internal/board` 不 import `internal/review`. `AGENTS.md` 包职责表中 `internal/review` 一行同步说明归档职责.
- [ ] 新增的面向用户字符串在 `en.json`、`zh-CN.json`、`ja.json` 成对存在.
- [ ] 规则同步: `rules/KANDER-REVIEW-RULES.md` "Report Files" 改为归档到卡片 `reviews/`、随卡保留、失败轮也归档、不再要求临时目录与清理, 并把"报告目录必须在目标 worktree 之外"改写为"归档位于看板目录, 不属于任何 worktree 的跟踪内容"; "Invocation Arguments" 与 "Group-Level Review for Task Groups" 写明看板任务 (单卡与任务组批次) 必须传 `--task`, 批次传全部成员卡; `rules/KANDER-BASE-RULES.md` "Single Review" 的参数行更新; `rules/KANDER-KANBAN-RULES.md` 的模板增加 `## REVIEWS` 节、`check` 说明增加归档校验; `rules/KANDER-TASK-GROUP-RULES.md` "Review Batches and Dispatch-Back" 允许派回消息引用归档报告路径.
- [ ] 遵守仓库语言约定: 新增 Go 注释与提交备注全英文; 规则文件保持英文; 用户可见字符串走 i18n.

## THREAT_MODEL

审核报告由 Reviewer 基于仓库内容生成, 仓库内容对 Reviewer 而言是不可信证据 (prompt 已声明); 归档后这些文本会被执行 Agent 与编排 Agent 再次读取.

- 资产: 卡片 `spec.md` 的契约与索引 (Agent 据此判断审核是否完成); `reviews/` 下的报告与 sidecar (后续卡将据此构造门禁与复审上下文); 看板目录外的用户文件.
- 可信主体: 本机当前用户; `kander review` 门禁进程本身.
- 攻击者能力: 通过仓库内容诱导 Reviewer 在报告中输出伪造的索引行或指令文本; 在 `reviews/` 或卡片目录预置符号链接/reparse point 把归档写到看板外.
- 对策: Reviewer 始终以只读隔离运行, 没有写看板的能力, 归档只由门禁在校验后写入; 索引行由门禁按固定格式生成, 报告正文只作为文件保存, 不解析其中任何行进入索引; 全部写入经 `internal/fs` 的 no-follow 与固定父句柄语义; 规则要求 Agent 把归档内容当作数据而非指令.
- 非目标: 不对报告内容做签名或防篡改; 不防御已能任意写入看板目录的本地攻击者.

## OUT_OF_SCOPE

按既有问题 / 加固 / 共享契约与文档 / 相邻功能四类界定:

- 既有问题 — `parseReviewOutput` 只校验"运行成功且文本非空", 不校验报告结构 (角色头、等级标签、`NON-BLOCKING` 节): 排除理由: 与归档正交; 结构校验属报告 lint, 用户未要求, 若需要另开卡.
- 既有问题 — Codex Reviewer 携带 `web_search="live"` 而 Claude/Grok 禁网: 排除理由: 隔离策略一致性问题, 与归档无关, 已在讨论中向用户点出, 是否处理由用户决定.
- 加固 — 归档文件的签名、只读位或防篡改: 排除理由: 威胁模型不把已能写看板目录的本地攻击者列为目标.
- 加固 — 归档保留期与清理策略 (卡片 `done` 后是否删除 `reviews/`): 排除理由: 用户尚未决定; 本卡随卡保留, 进 `trash` 随卡目录一起处理, 不单独清理.
- 共享契约与文档 — 执行 Agent 的逐条验证结论文件、`done` 门禁、以及由归档自动拼装增量复审上下文: 由本组第三张卡 `20260907-review-disposition-gate-task` 实现; 本卡的 `check` 只校验归档完整性与同 base 增量链, 不判断"通过".
- 共享契约与文档 — 跨批次 base 链 (后一批 base 等于前一批完成审核的 commit) 的校验: 需要"完成审核"的结论文件, 归第三张卡.
- 相邻功能 — `review-context` 参数接受文件路径: 排除理由: 第三张卡由归档自动拼装复审上下文后该需求大部分消失; 本卡不改该参数语义.
- 相邻功能 — 一次调用并行拉起 PM 与 QA: 排除理由: 用户未要求; 先由归档数据观察实际是否串行再决定.

## DISCUSSION

```text
PREREQUISITES: 20260907-directory-card-form-task
```

**关键技术结论 (建卡前已核实)**

- `internal/review/review.go` 的 `Run` 以 `splitAgentArgs` (`settings.go:236`) 判定首参是否为 agent 名或绝对路径, 之后要求 5 到 7 个位置参数 (`len(rest) < 5 || len(rest) > 7`); `--task` 需在此之前剥离, 且不能与"首参是绝对路径"的判定冲突.
- 运行时目录由 `executeReview` (`execute.go:16`) 创建, 并在 `executeInRuntime` 返回之后于 `execute.go:24` 清理; worktree 未改校验在 `executeInRuntime` 的 defer 里 (`execute.go:77-82`), 在函数体之后执行, 残留进程的主判定在函数体内 (`execute.go:161-172`) 而 defer 只兜底; `parseReviewOutput` (`args.go:154`) 对 codex 原样输出 `outputFile`, 对 claude/cursor/grok 输出从 JSON 提取的 `text` 加换行. 因此归档应放在 `executeReview` 中、运行时清理之后, `executeInRuntime` 需要把最终输出到 stdout 的报告文本与失败原因带出来, 而不是让归档去读运行时里的原始文件.
- `reportLanguageFromConfig()` (`settings.go:257`) 目前读配置的 `agent_language`; 卡片 `LANGUAGE` 字段可由 `board.MetadataFrom` 读取, 看板规则已承诺 `LANGUAGE` 覆盖审核报告语种, 本卡只是兑现.
- 看板定位: `board` 包的 `locate.go` 以当前 Git 仓库主 worktree 的 `kanban/` 为候选, 任务 worktree 与组 worktree 都能归一到主 worktree. 但 `review` 的目标 `CWD` 是参数而不是进程 cwd, 需要以该参数为起点定位, 否则从别处调用会找错看板.
- 看板目录经 `.git/info/exclude` 排除, 归档不会进入 git. 审核规则现有的"报告目录必须在目标 worktree 之外"一句只在 CWD 是任务或组 worktree 时字面成立; 直接审核主 worktree 时 `kanban/` 位于其内但被 exclude 遮住, 因此规则同步时要把这句改写为"归档位于看板目录, 不属于任何 worktree 的跟踪内容", 不能让旧句与新行为并存.
- `internal/fs` 已提供 `flock`/`LockFileEx` 独占锁与固定父句柄原子替换, 并发追加索引行可直接复用, 不新增锁服务.
- PM 与 QA 首轮按规则并行, 因此两个门禁进程可能同时归档到同一张卡, 并发用例是必需的而不是防御性的.
- 失败轮也归档是建卡 Agent 在用户确认方案 ("成功后追加一行索引") 之上的补充, 不是用户确认项, 用户可否决. 理由: 规则把"连续 3 次后端失败"定义为持久失败, 并要求把"未完成的角色"列入未解决项; 没有失败记录就无法审计这两条.

**与前后卡的接口**

- 依赖第一张卡: 每张卡都是目录后 `reviews/` 才有固定归宿; 本卡不读取 `SIZE`.
- 交付给第三张卡: 索引行格式、sidecar 字段、四个归档文件的命名与 `board` 包的索引解析函数. 第三张卡在此之上定义结论文件与 `done` 门禁.

SELF_REVIEW: 通过. (1) 目标与成果对齐用户"落盘供 Agent 用"与"正文只追加索引"; (2) USER_DECISIONS 只含用户明确确认项, 失败轮归档、sidecar 字段、文件命名等为建卡 Agent 设计, 失败轮归档已标注可否决; (3) 边界: 结论文件、done 门禁、跨批次链、review-context 文件路径、并行拉起等排除项各有归属或理由, 归档所需的参数、定位、写入、并发、语种、check、规则同步全部在范围内; (4) 验收可判定, 含不带 `--task` 行为不变、前置校验先于启动、分界以 `reviewStarted` 为准、并发用例. 建卡时自行修正: 索引行 model 为空时的占位写法. 首轮卡审后修正: `LANGUAGE` 缺字段回落配置, 归档时机与报告来源的技术结论, "报告目录在 worktree 之外"旧句的改写, 归档分界, 失败轮归档标注为补充, 单文件卡提示 `kander init`; 第二轮建议项已补: 前向引用措辞与 defer 行号改准.

CARD_REVIEW: PASS — 同一独立子 Agent (Claude, general-purpose, 不共享建卡会话上下文) 两轮. 首轮 FAIL: 报告语种规则与看板规则的缺字段回落约定冲突; 建议项: 归档时机技术结论与代码不符, "报告目录在 worktree 之外"旧句与新行为并存, 归档分界歧义, 失败轮归档未标注为补充, 单文件卡提示. 全部修正后第二轮 PASS, 无必须修正项, 两条措辞建议已改.
