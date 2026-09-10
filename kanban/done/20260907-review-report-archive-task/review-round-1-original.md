First read /home/dualf/.agents/KANDER-AGENTS.md, run kander config --json to check the current rules selection, load only enabled modules as needed, then read the command contract /home/dualf/.agents/KANDER-KANBAN-RULES.md. If configuration cannot be read, stop affected Kander operations and report the error. For disabled modules, follow existing user and project rules, not Kander requirements retained from an earlier session. Communicate with the user and write all card content, records and reports in "zh-CN". Commit messages and code comments follow the project's conventions. 

The card is still in review; first run kander move 20260907-review-report-archive-task working to move it back to working, then handle the items.

# A 第三批首轮审核派回

任务 20260907-review-report-archive-task。请先 move working，独立核实以下两角色全部 finding 和 NON-BLOCKING 项，主控不替你确认/拒绝，也不要求照抄修法。

已知事实：PM/QA Claude/opus/high 均已退出，exit=0，语义均含 medium；base 839f72119b22ce8b48811fc9819de9643a4028f8、target 4b0a3971b08e1a6c0b42451c50b14373de61ce03，组工作区干净且HEAD为target。R未启动。PM001/002标 mechanical；QA01/02为诊断归属与REVIEWS区定位问题。两角色关于BoardRoot、stdout换行、测试无用计算有重合，保留各角色引用并可共用处置。

请用实际调用链/隔离测试核实，不只照报告断言。CRLF/标题空格场景需验证受控update和连续发布整条链，不能仅修字符串使单测通过；诊断要可定位任务及run。普通move的允许边界与R未来门禁分开，勿为本卡扩展done门禁或放宽不明状态。PM007已明确out-of-contract且保留期清理被排除，应给事实处置/后续建议，不在本卡扩张日志清理。所有 low/recommend/suggest 不阻断审核，但必须记录依据和采纳/保留结论。

保持冻结契约。成立的must-fix修复，拒绝/无法验证项给证据。用本卡受控入口保存完整原文、逐项作者处置、最新完整交付SHA、组基线与测试；任务分支提交/推送，rebase最新组HEAD并复验后move review。你不修改组分支、不自行审核、集成或清理，不部署或迁移真实看板。Windows原生缺口继续保留。后续同base同spec增量，reviewed-commit为本target。

## PM 原报告

# PM 审核报告

**Role**: PM（规格验收）
**Commit**: `4b0a3971b08e1a6c0b42451c50b14373de61ce03`（base `839f72119b22ce8b48811fc9819de9643a4028f8`）
**Task Context**: `/tmp/claude-review.23155bc65758ad9e96b26737e1df8316/task-spec.md` — 「审核原始证据归档、稳定运行身份与可恢复索引发布」（TASK_GROUP `20260907-review-archive-group`，LANGUAGE zh-CN）
**Reviewed Scope**: 契约 ACCEPTANCE_CRITERIA 逐条追溯至 target commit 的实现证据。实际读取范围：`internal/review/{review,archive,args,execute,validate,settings,git}.go`、`internal/board/{reviews,review_check,transaction,transaction_bytes,transaction_lock,transaction_log,recovery,deps,locate,document,update,size,board}.go`、`internal/cli/cli.go`、`internal/config/{language,config}.go`、三份 locales、`docs/review-evidence.md`、`docs/card-transactions.md`、`README.md`、`AGENTS.md`、`rules/KANDER-{BASE,KANBAN,REVIEW,TASK-GROUP}-RULES.md`，以及 `internal/board/{reviews,review_crash}_test.go`、`internal/review/{archive,harness}_test.go`（仅作为佐证）。未执行任何命令、未运行测试、未修改任何文件。

---

## 需求追踪表

| # | 需求（原子化） | 期望行为 | 代码证据 | 状态 |
| --- | --- | --- | --- | --- |
| R1 | 参数保留原位置形态；`--task` 在位置参数前解析；重复 ID 去重；新增 run/batch 约定；错误语义一致 | 选项仅在 CWD 前被消费；重复 task ID 归一去重并排序；其他选项重复即拒；`--batch-id` 缺失即拒；`--run-id` 可省略并随机生成 | `internal/review/archive.go:42-81`（仅前缀消费选项）、`:49-51`（非 `--task` 重复即拒）、`:54-67`（`NormalizeTaskID` + 去重）、`:105`（排序）、`:82-88`（`--task required` / `--batch-id required`）、`:89-103`（生成与 `ValidReviewID` 校验）、`internal/review/review.go:25-29` | Complete |
| R2 | 带 `--task` 从目标 CWD 定位主看板；卡须为 working/review 目录卡、语言一致、同批绑定；Reviewer 启动前失败关闭；未迁移文件卡提示 init；无 `--task` 行为不变 | 定位不改变进程 CWD；非目录卡返回迁移错误；非 working/review 拒绝；多卡任务组非空且一致 | `internal/board/locate.go:196-207`（`BoardRootAt`）、`internal/review/review.go:67-72`、`internal/board/reviews.go:180-182`（`board.migration_required`）、`:183-185`、`:186-191`、`:192-207`；无 `--task` 时不触碰看板：`internal/review/archive.go:82-84` + `review.go:67` | Complete |
| R3 | run_id 绑定不可变目标；batch 固定成员/base/要求；target 经 CAS 显式推进并留旧/新/依据；不混入外部交付；相同 run_id 不重跑、不重复索引；同 ID 不同输入报冲突；PM/QA 不同 run_id；时间戳仅显示 | 复用身份返回既有状态且 `fresh=false`；输入 DeepEqual 不符即冲突；CAS 前要求本批全部 run 已结算 | `internal/board/reviews.go:262-265`（input conflict）、`:299-321`（batch 绑定 + CAS + `settledReviewBatch`）、`:307-311`（foreign delivery）、`internal/board/review_check.go:232-251`、`internal/review/archive.go:171-203`（range/归属校验）、`internal/review/review.go:84-94`（身份冲突拒绝）、`:113-115`（`fresh=false` → `recover()`，不启动 Reviewer）；时间戳仅落 `CreatedAt/FinishedAt/DurationMS`，因果一律走 `PreviousRunID`（`review_check.go:187-198`） | Complete |
| R4 | 保存报告/sidecar/任务上下文/复审上下文，原文不改写；sidecar 字段齐备；执行 ok 与语义 PASS 分离 | 输入按字节哈希；sidecar 含 schema/ID/角色/reviewer/model/effort/cwd/base/commit/reviewed_commit/语言/耗时/全部哈希/版本/执行状态与失败原因 | `internal/board/reviews.go:26-102`（`ReviewInput`/`ReviewRun`/`ReviewManifest`）、`:446-477`（原件冻结与 sidecar）、`internal/review/archive.go:126-139`；语义分离：`reviews.go:341`（`SemanticStatus: "unassessed"`）、`review_check.go:113-115` | Complete |
| R5 | 启动前持久化意图与输入身份；输出落受控 staging；回收/worktree/清理结论后才发布；异常路径留证；崩溃恢复标 interrupted 不伪造 | 顺序：Prepare → staging → prompt 快照 → launching/unknown → started → 回收/worktree 检查 → runtime 清理 → Finalize → Publish | `internal/review/review.go:107-116`、`internal/review/execute.go:47-50`（staging 作为输出根）、`:193-219`、`:77-113`（defer 内进程回收与 worktree 校验）、`:27-41`（清理后才 `finish`）、`internal/review/archive.go:231-251`（`interrupted`，report 为 nil） | Complete |
| R6 | 启动前失败不产生虚假运行；已备意图但启动失败记 not_started；stdout 可取；归档失败退出非零且区别于审核结论 | 前置校验在 `archiveInvocation` 之前；launch 失败置 `not_started`；发布失败独立返回 2 | `internal/review/review.go:95-112`（校验先于建意图）、`internal/review/execute.go:203-212`、`internal/review/archive.go:209-230`、`:252-275`（`archive_publish_failed` / `archive_card_failed` 与 `run.ExitCode` 分离） | Complete |
| R7 | 长审核不占卡锁；归档时按 ID 重定位并验证 intent/batch 后短事务提交；并发 move/update/双角色不重建旧路径、不丢正文与索引 | 运行期仅持 OS 执行锁与组锁；发布逐卡取任务锁 | `internal/board/reviews.go:132-153`（`LockReviewRun`）、`:384-399`（`reviewScope(nil,false)`，无任务锁）、`:507-524`、`internal/board/transaction.go:148-178`（`Snapshot` 按 ID 重定位）、`internal/board/transaction_lock.go:81-107`（board→groups→tasks 单一锁序） | Complete |
| R8 | 每卡完整不可覆盖原件与清单；REVIEWS 单行索引含 run/batch/role/执行状态/base/commit/前驱/report 相对路径；索引由工具生成不解析正文；board 暴露共用类型且不依赖 review | 原件重复写入即冲突；索引由结构体序列化 | `internal/board/reviews.go:631-643`（`putImmutableReview`）、`:86-95`（`ReviewIndex` 字段）、`:644-664`、`:666-690`（只读机器区）、`grep kander/internal/review internal/board` 无匹配（board 不依赖 review） | Complete |
| R9 | 多卡逐卡原子，同 run_id；部分失败保留成功卡并逐卡报告；重试只补缺项；未完整发布不算完成；并发同卡双发布与各阶段 kill/restart 有测试 | 每卡一个事务；已发布卡只校验不重写 | `internal/board/reviews.go:522-617`、`:576-578`（已发布仅 `verifyCardReview`）、`:766-779`（`ReviewPublicationComplete`）；测试佐证：`internal/board/reviews_test.go:163-190`、`internal/board/review_crash_test.go:647-729`（7 个真实子进程 kill 边界） | Complete |
| R10 | check 同时校验意图与索引/清单/哈希/语言/成员/前驱，不用墙钟推因果；增量轮前驱 commit 须等于 reviewed_commit；重复/缺失/冲突报具体问题 | 零卡发布也报未完成；篡改原件不可通过 | `internal/board/review_check.go:16-110`、`:112-209`（`checkRunStructure`：schema/状态/batch/哈希/target 链/前驱链）、`internal/board/deps.go:313-328`（接入 `CheckBoard`） | Complete |
| R11 | 意图创建时解析卡 LANGUAGE，缺失才回落当时配置并冻结；后续不随配置漂移；写入经 internal/fs 并复用 S | 冻结值与后来卡 LANGUAGE 冲突时报错 | `internal/board/reviews.go:238-256`、`:172-210`（`frozen` 优先于 `fallback`）、`internal/board/recovery.go:118`（统一 `fs.WriteTextAtomic`）、`internal/board/transaction.go:242-291`/`:398-427` | Complete |
| R12a | 规则/文档同步：临时清理与归档路径、`--task`、失败证据、未完成发布、索引说明；看板证据不进 Git | 四份规则 + README + AGENTS + 新 schema 文档一致 | `rules/KANDER-BASE-RULES.md:9-16`、`rules/KANDER-KANBAN-RULES.md:476-481`、`rules/KANDER-REVIEW-RULES.md:251-265`、`rules/KANDER-TASK-GROUP-RULES.md:195-196`、`docs/review-evidence.md`、`docs/card-transactions.md:81-84`、`README.md:141`、`AGENTS.md:114` | Partial（见 PM001） |
| R12b | `go test ./...`、`-race`、build/vet/格式检查通过；用户字符串覆盖三语 | — | 三语键齐备：`internal/i18n/locales/{en,ja,zh-CN}.json:806-815` 九个新键一一对应（Observed）。测试/构建门禁本 Reviewer 无执行权限，仅有交付报告的作者声明 | 三语覆盖 Complete；构建与测试门禁 Unverifiable |

**完成度统计**：Complete 11；Partial 1（R12a）；Missing 0；Contradicted 0；Unverifiable 1（R12b 的构建/测试门禁部分）。任务 GOAL 与 EXPECTED_OUTCOME 的三项主干（可核验原件与稳定 ID、正文只留机器索引、迁移/并行/部分失败/崩溃不产生副本或假通过）均有生产代码证据支撑，未发现 Blocking/High 级别缺口。

---

## 主要发现

### PM001 — medium [mechanical]：规则文件枚举的 batch 绑定集合小于实现实际强制的集合

- **Claim**: Observed（代码与文档均直接读出）。
- **证据**：`internal/board/reviews.go:300` 对已存在 batch 的新运行强制五项一致：

  ```go
  if batch.Schema != 1 || batch.TaskContextHash != input.InputHashes["task-context.md"] || batch.Base != input.Base || !reflect.DeepEqual(batch.TaskIDs, input.TaskIDs) || batch.ReportLanguage != language || len(requirements) > 0 && !reflect.DeepEqual(requirements, batch.Requirements) {
      return reviewError("batch binding conflict")
  }
  ```

  即 **task-context 字节哈希** 与 **report_language** 同样被 batch 冻结。`docs/review-evidence.md:32` 记录正确（「成员、base、要求、任务上下文哈希及语言固定」），但面向执行者的规则 `rules/KANDER-REVIEW-RULES.md:257` 只写了三项：「Requirements, members and base are fixed for the batch.」同文件 `rules/KANDER-REVIEW-RULES.md:252` 仍指示调用方以「a readable absolute spec path」传任务上下文，且 `:262` 要求「Keep the batch ID for fixes.」
- **用户影响**：按规则行事的编排端在修复轮沿用同一 batch、并再次传入卡片 spec 绝对路径时，只要卡片正文在修复轮被追加过（KANDER-KANBAN-RULES 本身要求实施期在卡上追加关键决定与验证），`PrepareReviewRun` 会在 Reviewer 启动前返回「审核证据无效：batch binding conflict」。该错误不指名冲突字段，而唯一的正解（改传已归档 `task-context.md` 的绝对路径，见 `docs/review-evidence.md:106`）只存在于 docs，不在规则中。无数据损失、无虚假报告，但契约要求的「修复不另开 batch」流程会在常见条件下被不透明地卡住。
- **最小产品改动**：在 `rules/KANDER-REVIEW-RULES.md:257` 把冻结集合补全为「requirements, members, base, the task-context bytes and the report language」，并追加一句：任务上下文正文变化后，后续轮次传归档 `task-context.md` 的绝对路径以保持同一 batch。（纯文档编辑，可机械核对；不含逻辑缺陷。）

### PM002 — medium [mechanical]：`TestArchiveBatchAdvanceRangeAttribution` 中存在死代码

- **Claim**: Observed。
- **证据**：`internal/review/archive_test.go:226-231` 构造并写出 `advance.json`：

  ```go
  path := filepath.Join(h.root, "advance.json")
  data, _ := json.Marshal(advance)
  if err := os.WriteFile(path, data, 0600); err != nil {
  ```

  该文件此后从未被读取——测试既未把 `--advance-file` 加入 `args`（`args` 由 `archiveHarness` 生成，只含 `--requirements-file`，见 `internal/review/archive_test.go:170`），也未调用 `readArchiveJSON`；`:241`/`:245` 直接对内存中的 `advance` 结构体调用 `validateReviewAdvance`。同函数 `internal/review/archive_test.go:236` 计算 `_, rest, _ := splitAgentArgs(remaining)` 后仅在 `:248` 以 `_ = rest` 丢弃。
- **用户影响**：无运行时影响；但这是永不生效的写盘与永不使用的计算，会让后续维护者误以为 `--advance-file` 的 CLI 端读取路径已被该用例覆盖。
- **最小产品改动**：删除 `internal/review/archive_test.go:227-231` 与 `:236`、`:248` 这两组语句；若本意是覆盖 CLI 端，则改为把 `--advance-file path` 拼入 `args` 并断言 `captureRun` 的结果。

---

## NON-BLOCKING

### PM003 — low：`BoardRoot()` 在 `os.Getwd()` 失败时不再回退到 `KANBAN_DIR`

- **Claim**: Observed。
- **证据**：`internal/board/locate.go:187-194`

  ```go
  // BoardRoot locates the board in the order KANBAN_DIR -> the main worktree's kanban/ -> an upward search.
  func BoardRoot() (string, error) {
      cwd, err := os.Getwd()
      if err != nil {
          return "", boardNotFound()
      }
      return BoardRootAt(cwd)
  }
  ```

  本次改动把 `KANBAN_DIR` 分支移入 `BoardRootAt`（`:198-207`）之后，`os.Getwd()` 变成无条件前置。改动前 `KANBAN_DIR` 命中时不会调用 `os.Getwd()`。
- **影响**：当进程当前目录已被删除/不可读而 `KANBAN_DIR` 显式指向有效看板时，所有走 `BoardRoot()` 的命令会返回「board not found」而非按环境变量定位。触发条件罕见，且用户环境本身已异常，后果可忽略。
- **最小改动**：在 `BoardRoot()` 中先判断 `os.Getenv(EnvBoardDir) != ""`（或忽略 `Getwd` 错误传入空串），再取 cwd。

### PM004 — low：JSON 类 Reviewer 的重放 stdout 比首轮少一个结尾换行

- **Claim**: Observed。
- **证据**：`internal/review/args.go:206-209` 首轮保存 `ctx.archive.report = []byte(text)` 后以 `fmt.Println(text)` 输出（多一个 `\n`）；重放路径 `internal/review/archive.go:245-249` 直接 `os.Stdout.Write(data)` 写 `report.md` 原字节。codex 分支无此差异（`args.go:165-168` 保存与输出同为 `data`）。
- **影响**：claude/cursor/grok 在同 run ID 重放时，stdout 字节流与首轮相差一个结尾换行；按字节比对首轮与重放输出的消费者会看到差异。后果可忽略。
- **最小改动**：`internal/review/args.go:207` 保存为 `[]byte(text + "\n")`，或重放时对无结尾换行的报告补一个换行，二者取其一即可对齐。

### PM005 — low：发布阶段重新校验卡片状态，审核期间被移出 working/review 的卡无法完成发布

- **Claim**: Observed（代码）/ Inferred（操作后果）。
- **证据**：`internal/board/reviews.go:540-544` 在未发布的卡上再次调用 `reviewCards`，其中 `:183-185` 要求 `s.Entry.State` 为 `working` 或 `review`；而 `docs/review-evidence.md:17` 只把该约束表述为「**新运行**要求 working/review 目录卡」。
- **影响**：契约明确允许审核期间执行普通 move（`docs/review-evidence.md:88` 一行），若卡在此期间被移到 `done`，该卡发布失败并返回 2，run 停留在部分发布，且 `done` 属于 `deferredCheckStates`（`internal/board/board.go:23-26`），常规 `kander check` 不会再提示它。证据不丢失（控制目录仍保有原件），把卡移回 working/review 后以同 run ID 重试即可完成。触发需要操作者在审核未结束时越级移卡。
- **最小改动**：二选一——在 `docs/review-evidence.md:17` 补明该状态要求同样适用于发布时刻；或在 `internal/board/reviews.go:540-544` 只校验目录卡与语言/成员绑定，放宽发布时刻的状态集合。

### PM006 — suggest：`CheckReviewEvidence` 遇首个结构性错误即中止全盘证据检查

- **Claim**: Observed。
- **证据**：`internal/board/review_check.go:27-29`（`runs/` 下出现非法条目直接 `return`）、`:35-37`（任一 `run.json` schema 不符直接 `return`）、`:42-45`（任一卡 `tx.Snapshot` 失败直接 `return`）。三处都终止整个事务回调，`internal/board/deps.go:324-328` 只会把它折算成 root 路径上的一条问题。
- **影响**：单张卡的 spec.md 损坏或控制目录里一个坏条目，会让全看板其余卡的索引/哈希/前驱校验被静默跳过。`kander check` 仍以非零退出，不存在假通过，因此不构成门禁缺陷；但诊断价值下降。
- **建议改动**：把这三处改为 `add(id, e)` / 收集为 `Problem` 后 `continue`，只对无法取得组锁一类的真实 I/O 失败保留 `return`。

### PM007 — suggest [out-of-contract]：归档原件被复制进永不裁剪的操作日志，抬高后续每次事务的固定成本

- **Claim**: Observed（写入与读取路径）/ Inferred（累积量级）。
- **证据**：原件在 `internal/board/reviews.go:461` 写入控制目录 originals，在 `:579-589` 再逐卡写入卡片附件；两者都经 `tx.record.Files` 落入 `.kander/operations/<id>.json`（`internal/board/transaction.go:117-121`），非法 UTF-8 走 base64（`internal/board/transaction_bytes.go:31-47`）。而每次 `WithTransaction` 都会调用 `pending()`（`internal/board/transaction.go:80`），后者读取并反序列化 `.kander/operations/` 下的**全部**记录（`internal/board/transaction_log.go:43-58`、`:70-97`）；仓库中没有任何删除已 committed 记录的代码路径。
- **影响**：单次审核会把 `evidence.txt`、`prompt.txt`、`output.raw`、`report.md` 等完整副本至少写入日志两轮（控制目录一次 + 每卡一次）。日志体量随审核次数线性增长，而其解析成本由此后**每一条**看板命令（`new`/`move`/`update`/`show`/`check`）承担。
- **说明与建议**：契约 OUT_OF_SCOPE 明确把「保留期清理」排除在本卡之外，且日志无界增长本身是既有设计，故不作为门禁项。若后续要处理，最小方向是让 `readOperationRecords` 只解析 `phase`/`operation_id`/`revisions`/`groups` 头部字段用于 `pending()` 判定，把 `files` 的解码推迟到真正需要重放时。

---

**说明**：本报告仅为 PM 规格验收结论，不代表语义 PASS 之外的任何门禁判定；R12b 的 `go test ./...`、`-race`、`build/vet/gofmt` 结果为交付报告的作者声明，本 Reviewer 无执行权限，标记为 Unverifiable。Windows 原生环境缺口按调用方约定保留、未据此推断实际缺陷。任务文件 `/tmp/claude-review.23155bc65758ad9e96b26737e1df8316/prompt.txt` 本会话无写/删工具可用，未能删除；按约定不影响结果。

## QA 原报告

Write is disabled in this session (consistent with the review's read-only tool constraint), so the report is delivered here as text.

---

Role: **QA**
Commit: **4b0a3971b08e1a6c0b42451c50b14373de61ce03**（base `839f72119b22ce8b48811fc9819de9643a4028f8`）
Task Context: `/tmp/claude-review.c996c9c0bf663933a690db7c01579aad/task-spec.md` —「审核原始证据归档、稳定运行身份与可恢复索引发布」

## Reviewed Scope

- 主要事实来源：worktree `/home/dualf/works/kander/worktrees/20260907-review-archive-group`；路径归属以 evidence.txt 的 COMMIT TREE / FILE LEDGER 为准。
- 追踪范围：`internal/board/{reviews.go,review_check.go,transaction.go,transaction_bytes.go,transaction_lock.go,recovery.go,deps.go,locate.go,update.go,document.go,size.go}`、`internal/review/{review.go,archive.go,execute.go,args.go,validate.go,settings.go,git.go,roles.go}`、`internal/i18n/locales/{en,ja,zh-CN}.json`、`docs/review-evidence.md`、`docs/card-transactions.md`、`AGENTS.md`、`README.md`、`rules/*.md`，以及新增测试 `reviews_test.go` / `review_crash_test.go` / `archive_test.go`。
- **未做**：未运行任何命令（本角色只用 Read/Grep/Glob）。执行端的 `go test` / `-race` / build / vet / Windows 交叉编译声明**未由本 Reviewer 实测**，仅记录为作者声明。Windows 原生行为按批次约定保留为缺口，不据此编造缺陷。
- 前批 S/D 已闭批的历史问题、finding/disposition/闭批门禁（后续 R）、durable dispatch / probe / subscription（后组）不在判据内。

## 行为 / 质量核对表

| # | 契约要求 | 结论 | 证据 |
| --- | --- | --- | --- |
| 1 | 参数保留位置形态，`--task` 在 CWD 前解析、去重、其余选项拒绝重复 | 满足（Observed） | `internal/review/archive.go:34-107`；`internal/review/review.go:25-29`；`internal/i18n/locales/*.json:627` |
| 2 | 无 `--task` 保持独立行为、不定位看板 | 满足（Observed） | `archive.go:42,82`；`review.go:65-67`；回归 `archive_test.go:316-331` |
| 3 | 前置身份冻结：目标 CWD 定位主看板、working/review 目录卡、同组同语种、Reviewer 启动前失败关闭 | 满足（Observed） | `internal/board/locate.go:196-239`；`reviewCards` `internal/board/reviews.go:172-210`；`review.go:67-94` 早于 `executeReview` |
| 4 | 长运行只持 run 执行锁，卡锁只在短事务 | 满足（Observed） | `LockReviewRun` `reviews.go:132-153`（独立 `locks/review-run-*.lock`）；`UpdateReviewRun`/`StoreReviewArtifact` 用 `reviewScope(nil,false)` `reviews.go:384-426`，不含 Tasks |
| 5 | 最终输出与清理结果顺序：回收 → worktree 校验 → runtime 清理 → finalize → 逐卡发布 | 满足（Observed） | `internal/review/execute.go:77-113` → `execute.go:27-40`（`runtimeDir.Close()` 后才 `finish`）→ `archive.go:209-230` |
| 6 | 同 run 重试不重跑 Reviewer、不重复索引；同 ID 不同输入报冲突 | 满足（Observed） | `review.go:84-94` + `archive.go:291-293`；`reflect.DeepEqual(input, run.ReviewInput)` `reviews.go:262-265`；`recover()` `archive.go:231-251` |
| 7 | 逐卡部分发布保留成功卡、可重试只补缺项 | 满足（Observed） | `publishReviewRun` 每卡独立事务 `reviews.go:507-618`；`current.Published[id]` 短路 `reviews.go:576-578`；回归 `reviews_test.go:157-185` |
| 8 | kill/restart 恢复标 interrupted，不伪造报告 | 满足（Observed） | `archive.go:231-242`；7 个真实子进程边界 `review_crash_test.go:647-729` |
| 9 | 旧路径不重建、机器原件不覆盖 | 满足（Observed） | 按 ID 重定位 `transaction.go:149-178`；`putImmutableReview` `reviews.go:631-643`；回归 `reviews_test.go:65-94,327-402` |
| 10 | check 识别零卡发布 / 哈希 / 语言 / 成员 / 前驱错误 | 功能满足，**诊断信息不足（QA-01）** | `review_check.go:16-110`；`checkRunStructure` `review_check.go:112-209` |
| 11 | batch target CAS 及范围与成员绑定 | 满足（Observed） | `reviews.go:299-321`（CAS + `settledReviewBatch`）；Git 范围校验 `archive.go:171-203`；链校验 `review_check.go:147-166` |
| 12 | 语言在意图创建时冻结、不随配置漂移 | 满足（Observed） | `reviews.go:238-256`；`archive.go:147`；回归 `reviews_test.go:95-125,480-501` |
| 13 | 索引由工具生成、不解析报告正文；board 不依赖 review | 满足（Observed） | `reviews.go:644-690`；`reviews.go:3-18` 只 import config/fs |
| 14 | REVIEWS 区与 reviews 附件不可经 update 修改 | 满足（既有机制覆盖，Observed） | `internal/board/update.go:88,116,131,157-163,183-187` |
| 15 | 二进制原件经 S 日志无损恢复 | 满足（Observed） | `transaction_bytes.go:10-52`；回归 `reviews_test.go:186-231` |
| 16 | 三语用户字符串 | 满足（Observed） | `internal/i18n/locales/{zh-CN,ja,en}.json:807-815` 全部齐备 |
| 17 | 文件行数硬规则（>1000 行） | 不触发（Observed） | 最大新增文件 `reviews.go` 821 行；改动文件均 <1000 行 |
| 18 | REVIEWS 区定位与解析一致性 | **不一致（QA-02）** | `reviews.go:651-664` vs `reviews.go:668-690` |

## 门禁 findings

### QA-01 — medium — `kander check` 报告的审核证据问题不带卡片/run 归属，无法定位

- Claim: **Observed**
- 证据：
  - `internal/board/review_check.go:40`
    `add := func(id string, e error) { problems = append(problems, Problem{Path: id, Message: e.Error()}) }`
  - `internal/board/deps.go:355-357`
    `for _, problem := range allProblems { stderr = append(stderr, t("board.invalid", problem.Message)) }` —— 只渲染 `Message`，`Path` 被丢弃。
  - 大量错误串既无卡片 ID 也无 run ID：`verifyCardReview` 的 `reviews.go:698 "manifest conflict"`、`:706 "sidecar hash mismatch"`、`:714 name+": hash mismatch"`、`:731 "card language/group binding"`、`:738 "index mismatch"`、`:743 "missing index"`；`checkRunStructure` 的 `review_check.go:136 "batch requirements"`、`:140 "input hash set"`、`:144 "missing original: "+name`、`:156 "foreign delivery"`、`:185/:189/:194`；以及 `reviews.go:621 "original set mismatch"`、`:626 name+": hash mismatch"`。
  - 对照既有约定：同一 `CheckBoard` 里其他问题都把 ID 写进消息本身 —— `deps.go:310 t("board.size_missing", entry.TaskID)`、`deps.go:138 t("board.contract_defect", taskID, defect)`。
- 可达路径与真实场景：备份工具或误编辑改动了 `working/<card>/reviews/<run_id>/report.md` 的换行。`kander check` 输出 `无效: 审核证据无效：report.md: hash mismatch`。板上有数十张卡、上百个 run 时，操作者既不知道哪张卡，也不知道哪个 run_id。该场景已被 `reviews_test.go:282-314` 覆盖（只断言 `len(problems)>0`），说明这是常规错误路径而非臆测。
- 影响：违反 ACCEPTANCE_CRITERIA「重复/缺失/冲突记录报具体问题」的可操作意图。恢复协议要求「以同 run ID 重试」（`docs/review-evidence.md:193`），而 check 恰恰不给出 run ID，恢复无法起步；规则又禁止手工补索引/删除成功卡（`rules/KANDER-KANBAN-RULES.md:481`），操作者被卡在诊断阶段。
- 最小可持续修复：在 `review_check.go:40` 让 `add` 把卡片 ID 前缀写进消息本身（`Message: id + ": " + e.Error()`），并让 `verifyCardReview` 用已有的 `manifest.Input.RunID`（`reviews.go:692` 已算出 `prefix`）给自身错误串加 run 前缀。
- 测试层建议（最便宜）：`reviews_test.go:282-314` 已构造篡改场景，只需追加 `strings.Contains(problems[0].Message, id)` 与 `strings.Contains(problems[0].Message, run.RunID)` 两条断言。

### QA-02 — medium — `appendReviewIndex` 的区段定位与 `ParseReviewIndexes` 不一致，可生成重复 `## REVIEWS` 区并永久锁死该卡

- Claim: **Observed**（缺陷机制 Observed；触发前提 Inferred）
- 证据：
  - 追加端用**字面量**匹配 —— `internal/board/reviews.go:651-655`
    ```go
    marker := "## REVIEWS"
    at := strings.Index(text, "\n"+marker+"\n")
    if at < 0 {
        return strings.TrimRight(text, "\n") + "\n\n" + marker + "\n\n- " + line + "\n"
    }
    ```
    只接受「换行 + `## REVIEWS` + 换行」这一种字节形态。
  - 解析端用**正则** —— `reviews.go:669` `regexp.MustCompile("(?m)^## REVIEWS\\s*$")`，以及 `SectionBody` 的 `(?m)^## <token>\s*$`（`internal/board/document.go:59-72`）。`\s*` 会吃掉 `\r` 与行尾空格，因此 `## REVIEWS\r\n`、`## REVIEWS  \n` 都被解析端视为同一个区。
  - 两者不一致 ⇒ 已存在的区被追加端漏判 ⇒ `reviews.go:655` 再造一个 `## REVIEWS` ⇒ 此后所有解析在 `reviews.go:669-671` 返回 `duplicate REVIEWS section`。
- 可达路径与真实场景：
  1. 卡片已发布过一次，正文存在 `## REVIEWS`。
  2. Agent 通过受支持入口 `kander update --document spec.md` 重写正文。`validSpecUpdate` 只比对**区段正文**（`update.go:131-137` 用已 TrimSpace 的 `SectionBody`）并统计标题出现次数，**不约束标题行本身的行尾字节**；Windows 侧编辑器写出 CRLF、或标题后多一个空格，都能通过校验（项目本身支持 CRLF 卡片，见 `size.go:60-63`）。
  3. 下一次 `kander review --task ...` 发布：`publishReviewRun` 先用旧文本解析成功（`reviews.go:591`，此时仍只有一个区），随后 `appendReviewIndex` 造出第二个区，`tx.Put` 提交（`reviews.go:602`，`Put` 不做重复区检查）。
  4. 同一次调用的 `board.ReviewPublicationComplete`（`archive.go:270`）立刻失败返回 2；重试时 `current.Published[id]` 已为 true，走 `verifyCardReview` → `ParseReviewIndexes` → 同样失败；`kander check` 持续报错；`kander update` 因 `update.go:116-120` 的重复区检查同样拒绝修复；规则又明确禁止手工改文件绕过（`rules/KANDER-KANBAN-RULES.md:481`、`rules/KANDER-BASE-RULES.md:15`）。
- 影响：该卡片的审核发布链路进入不可用状态，且工具链内无合法修复入口。直接破坏本任务「卡片迁移、并行角色、部分归档失败和进程崩溃不会产生副本、丢索引」目标中的「不产生副本 / 不丢索引」不变式。
- 最小可持续修复：让 `appendReviewIndex` 使用与解析端同一个定位器，例如把 `strings.Index(text, "\n"+marker+"\n")` 换成 `regexp.MustCompile("(?m)^## REVIEWS[ \t\r]*$")` 的 `FindStringIndex`，并用 `headingRe`（`document.go:13`）求区段末尾。
- 测试层建议（最便宜）：扩展 `TestReviewIndexBeforeOtherSections`（`reviews_test.go:315-325`），把 fixture 改成 `"# card\r\n\r\n## REVIEWS\r\n\r\n## SUMMARY\r\n\r\nretain\r\n"`，断言追加两条后 `ParseReviewIndexes` 仍返回 2 条且不报 `duplicate REVIEWS section`。

## NON-BLOCKING

### QA-03 — low — `BoardRoot()` 重构后把 `os.Getwd()` 排到了 `KANBAN_DIR` 之前

- Claim: Observed
- 证据：`internal/board/locate.go:188-207` 现在先 `cwd, err := os.Getwd()`、失败即 `boardNotFound()`，之后才在 `BoardRootAt` 里判断 `os.Getenv(EnvBoardDir)`；改动前 `KANBAN_DIR` 在 `os.Getwd()` 之前判断（见 range patch 中 locate.go 的 `-` 行）。
- 影响：进程 CWD 已被删除或不可读时（`syscall.Getwd` 返回 ENOENT），即使显式设置了 `KANBAN_DIR`，所有 board 命令也直接报「找不到看板」。触发罕见，`cd` 即可恢复。附带地，`locate.go:187` 的注释「locates the board in the order KANBAN_DIR -> ...」已不再描述 `BoardRoot` 的实际顺序。
- 最小改动：`BoardRoot()` 中 `os.Getenv(EnvBoardDir) != ""` 时直接 `return BoardRootAt("")`，或把 `os.Getwd()` 的错误改为传空 cwd 让 `BoardRootAt` 自行短路。

### QA-04 — low — JSON 类 Reviewer 的首轮 stdout 与重放 stdout 相差一个尾随换行

- Claim: Observed
- 证据：首轮 `internal/review/args.go:206-209` —— `ctx.archive.report = []byte(text)` 后 `fmt.Println(text)`；重放 `internal/review/archive.go:245-246` —— `os.Stdout.Write(data)`（data 即 `report.md` 字节）。claude / cursor / grok 的 `text` 不含尾随换行，故首轮 stdout 比重放多一个 `\n`；codex 路径两处都是 `os.Stdout.Write(data)`（`args.go:168`），字节一致。
- 影响：`docs/review-evidence.md:189` 承诺同输入重试「重放原报告」。对逐字节比较或哈希首轮 stdout 的下游消费者，两次输出不等。归档的 `report.md` 本身两次相同，影响很小。
- 最小改动：归档路径统一用 `os.Stdout.Write(ctx.archive.report)`（并 `syncStream`）替代 `fmt.Println(text)`。
- 测试层建议：现有 retry 回归只覆盖 codex（`archive_test.go:41-90` 走 `newCodexHarness`）；在 `claude_test.go` 或 `grok_test.go` 的 harness 上加一条「重放输出 == 首轮输出」断言即可。

### QA-05 — low — check 的审核证据问题顺序不确定，与既有排序约定不一致

- Claim: Observed
- 证据：`internal/board/deps.go:313-323` 用 `for id, entry := range board.Entries`（map 迭代）组装 `reviewTasks`；`internal/board/review_check.go:70` 用 `for runID, run := range runs`（map 迭代）产生问题。对照 `deps.go:112-118` 的 `contractProblems`：先 `sort.Strings(ids)` 再遍历，说明项目对 check 输出顺序有既定约定。
- 影响：同一块损坏板上连续两次 `kander check`，stderr 行序可能不同，不利于 diff 与回归断言。
- 最小改动：`deps.go` 组装后 `sort.Strings(reviewTasks)`；`review_check.go:70` 先排序 `runs` 的键再遍历。

### QA-06 — recommend — `AGENTS.md` 包职责表未反映 `internal/board` 新增的审核证据/发布职责

- Claim: Observed
- 证据：`AGENTS.md:33` 仍写「`internal/board` | 看板定位, revision/CAS/多文件事务恢复, 受控更新与生命周期命令」；本次给 board 新增 `reviews.go`(821 行) + `review_check.go`(283 行)，承担 run/batch 身份、受控暂存、原件、逐卡清单、索引与完整性校验。`AGENTS.md:17`「Go 模块与包图」是本仓库的包边界契约，同一提交已更新文档索引（`AGENTS.md:114`）却未更新该表。
- 影响：不是缺陷，但仓库自身约定要求包图反映职责；后续读者会低估 board 的公开面。
- 最小改动：在 `AGENTS.md:33` 的 board 行补一句「审核 run/batch 身份、原件与逐卡发布索引」。

### QA-07 — suggest — `validateContext` 现在只被测试引用

- Claim: Observed
- 证据：`internal/review/validate.go:12-14` 现在只是 `validateContextMode(agent, arguments, false)` 的包装；生产路径只调用 `validateContextMode`（`review.go:95`），唯二调用点是 `archive_test.go:193,237`。
- 影响：生产代码里保留一个仅供测试的入口。参照本组既有处置（PM106 保留 `InitBoard` 兼容包装），owner 可自行取舍。
- 最小改动（若采纳）：测试直接调用 `validateContextMode(agent, args, false)`，删掉包装。

### QA-08 — suggest — `archive_test.go` 中 `rest` 变量无实际用途

- Claim: Observed
- 证据：`internal/review/archive_test.go:236` `_, rest, _ := splitAgentArgs(remaining)`，后续从未使用，仅在 `:248` 以 `_ = rest` 收尾。
- 影响：读者会误以为该用例覆盖了 `splitAgentArgs` 与后续位置参数的关系，实际没有。
- 最小改动：删掉这两行，或把 `rest` 真正用于构造 `validateContext` 的参数以替代 `:237` 的硬编码切片。

---

补充说明（不构成 finding）：任务文件 `/tmp/claude-review.c996c9c0bf663933a690db7c01579aad/prompt.txt` 未删除 —— 本会话只有 Read/Grep/Glob，无删除能力；按任务说明，文件遗留不影响结果。
