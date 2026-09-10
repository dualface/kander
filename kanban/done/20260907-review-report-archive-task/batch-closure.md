# A 第三批闭批

base 839f72119b22ce8b48811fc9819de9643a4028f8；closed commit 8241a49b1f50cbb99acc68b4c3dc58b5a803253e。本地/远端组分支已接收。PM/QA Claude首轮4b0a397均工具成功但语义未通过；原作者修复e12ed3c。PM001/002全部must-fix为mechanical，主控核对规则和测试、实跑TestArchiveBatchAdvanceRangeAttribution通过，PM在e12ed3c通过免重跑。QA Claude同base/spec增量e12ed3c通过，QA01..08全闭合。最后非阻断措辞修正8241a49仅一行Markdown且为通过提交后代，按规则承接，不重新审核。CSA/Hacker N/A。

残留与拒绝必须最终报告：QA09 suggest混合行尾下整卡归一化可能改变受保护REVIEWS正文而拒绝，作者实测后保留既有行尾策略/保护，解析和归档完整性通过；PM007 suggest out-of-contract日志保存原件副本、每次事务读取全操作记录，事实已实测，性能量级未基准，不在本卡做清理，后续另案测量与设计；PM005 low发布状态事实接受并文档修正，Reviewer声称可把done移回working的恢复方案被真实MoveEntry拒绝证明不成立，提前终态未发布须另行处理，不误称已发布done重试亦失败；Windows原生缺口保留。其他建议已修复，Reviewer未实跑作者测试的事实保留。

下一批R base必须取8241a49完整SHA。尚未集成develop或全组收尾。

## review-round-2-original.md

First read /home/dualf/.agents/KANDER-AGENTS.md, run kander config --json to check the current rules selection, load only enabled modules as needed, then read the command contract /home/dualf/.agents/KANDER-KANBAN-RULES.md. If configuration cannot be read, stop affected Kander operations and report the error. For disabled modules, follow existing user and project rules, not Kander requirements retained from an earlier session. Communicate with the user and write all card content, records and reports in "zh-CN". Commit messages and code comments follow the project's conventions. 

The card is still in review; first run kander move 20260907-review-report-archive-task working to move it back to working, then handle the items.

# A 增量审核非阻断处置

任务20260907-review-report-archive-task。QA Claude增量已退出exit0，target e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a，无gate且QA01..08全闭合。PM此前两个must-fix均mechanical，主控核对规则五项冻结和真实advance-file测试并实跑TestArchiveBatchAdvanceRangeAttribution通过，按机械修正规则在同target通过，未重跑PM。组HEAD仍此target且干净，R未启动。

请move working，独立核实下列完整QA报告的QA09/QA10两条suggest并记录处置，不要求采纳可选建议或扩大范围。原报告称行尾问题不是回归；工具管理区保护边界不可为行尾归一化随意放宽。主控不替你确认/拒绝。保留全部原文、作者判断、PM007范围外日志成本与PM005恢复描述反证及Windows缺口；如有修复按实际改动验证并提交推送，对齐组HEAD；否则保持现交付SHA。最终写明是否代码变化、完整交付SHA再move review。你不审核、不集成、不清理。已通过角色无需因非阻断建议重新首轮；主控接收后按通过结论承接规则闭批。

## QA增量原报告

已完成增量复核。以下是报告全文（本会话只有 Read/Grep/Glob，无写工具，故报告以文本交付；任务文件 `/tmp/claude-review.0930fa23e6a954cd05a13ed09dcb2776/prompt.txt` 无法删除，按约定不影响结果）。

---

Role: **QA**
Commit: **e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a**（base `839f72119b22ce8b48811fc9819de9643a4028f8`，reviewed-commit `4b0a3971b08e1a6c0b42451c50b14373de61ce03`）
Task Context: `/tmp/claude-review.0930fa23e6a954cd05a13ed09dcb2776/task-spec.md` —「审核原始证据归档、稳定运行身份与可恢复索引发布」（TASK_GROUP `20260907-review-archive-group`，LANGUAGE zh-CN）

## Reviewed Scope

- 事实来源：worktree `/home/dualf/works/kander/worktrees/20260907-review-archive-group`（materializes `e12ed3c`）；路径归属以 evidence.txt 的 COMMIT TREE / FIX RANGE FILE LEDGER 为准。
- 本轮判据范围 = 修复区间 `4b0a397..e12ed3c` 的全部 12 个文件：`internal/board/{locate.go,review_check.go,reviews.go}`、`internal/review/{args.go,validate.go}`、新增 `internal/board/{locate_deleted_unix_test.go,review_revision_test.go}`、`internal/board/reviews_test.go`、`internal/review/archive_test.go`、`AGENTS.md`、`docs/review-evidence.md`、`rules/KANDER-REVIEW-RULES.md`。
- 仅为判断修复影响而读取的未改动代码（不重审、不据此提 finding）：`internal/board/{document.go,update.go,deps.go,schema.go,transaction*.go}`、`internal/review/archive.go`、`internal/review/claude_test.go`、`go.mod`。
- **未做**：未运行任何命令。作者声明的 `go test ./...`、`-race`、build/vet/gofmt、Windows amd64 交叉编译**未由本 Reviewer 实测**，仅记录为作者声明。Windows 原生缺口按批次约定保留，不据此推断缺陷。
- 前批 S/D 已闭批项、PM 侧 must-fix、`4b0a397` 时点已接受的未变更代码、后续 R 与后组范围均不在本轮判据内。

## 原 finding 逐项复核

| ID | 原结论 | 本轮核实 | 精确证据 |
| --- | --- | --- | --- |
| QA-01 medium | 成立 | **已闭合**（Observed） | `review_check.go:26-28` `add` 改为 `Message: id + ": " + e.Error()`；`review_check.go:131-136` `checkRunStructure` 用 defer 统一加 `run.RunID` 前缀（原 `:138-140,:153,:187-204` 各处不再自带前缀，避免重复）；`reviews.go:725-730` `verifyCardReview` 同样 defer 加 `manifest.Input.RunID`。check 路径全部错误串（`review_check.go:58-117`）现在至少带卡 ID，run 相关项另带 run ID。回归 `review_revision_test.go:72-107` 直接断言公开 `CheckBoard` 的每行 stderr 同时含卡 ID 与 run ID。原报告点名的 `verifyReviewHashes`（`reviews.go:628-640`）只在 `publishReviewRun:567` 可达，其错误经 `archive.go:262 archive_card_failed(id, …)` 带卡 ID 输出，run 由调用本身确定，不再是诊断缺口。 |
| QA-02 medium | 成立 | **已闭合**（Observed） | 读写共用单一定位器：`reviews.go:662` `reviewSectionRe = (?m)^## REVIEWS[ \t\r]*$` + `reviews.go:665-678` `reviewSectionBounds`（区段末尾用 `document.go:13` 的 `headingRe`）；`appendReviewIndex`（`:680-697`）与 `ParseReviewIndexes`（`:701-724`）均调用它。`TokenPattern("REVIEWS")` 无 legacy 别名（`schema.go:52-87`），故 `update.go:117` 的 `^## (?:REVIEWS)[ \t\r]*$` 与该字面量等价，写入端不再漏判已存在区段。另加发布后自检：`reviews.go:604-610` 追加后立即 `ParseReviewIndexes(text)`，失败即回滚该卡事务。回归 `review_revision_test.go:12-47` 覆盖标题尾空格 / LF 卡标题改 CRLF / CRLF 卡标题加空格+tab 三种，且经受控入口 `UpdateDocument`（`transaction_test.go:52-57` 的 `updateSnapshot`）改标题，断言二次发布后 `ParseReviewIndexes` 返回 2 条、后续章节保留、`CheckBoard` 为 0。 |
| QA-03 low | 成立 | **已闭合**（Observed） | `locate.go:188-197`：`BoardRoot()` 现先 `if os.Getenv(EnvBoardDir) != "" { return BoardRootAt("") }`，再取 cwd。`locate.go:187` 注释「in the order KANBAN_DIR -> …」重新与实现一致。回归 `locate_deleted_unix_test.go:10-34` 用真实子进程 chdir 后删除该目录，先断言 `os.Getwd()` 失败，再断言 `BoardRoot()` 返回 `KANBAN_DIR`。 |
| QA-04 low | 成立 | **已闭合**（Observed） | `args.go:206-211`：归档模式改为 `os.Stdout.Write(ctx.archive.report)` + `syncStream` 并提前 `return nil`，不再走 `fmt.Println(text)`（`:212` 仅剩无 `--task` 路径）；与重放路径 `archive.go:245-249` 的 `os.Stdout.Write(data)` 字节一致。采用了「输出原字节」而非「给原件补换行」的方案，归档 `report.md` 未被改写。回归 `archive_test.go:412-434` 用真实假 CLI 跑首轮 → 删除 CLI → 重放，断言 `first == replay == saved`，并覆盖无尾换行与有尾换行两种报告（`FAKE_CLAUDE_REPORT` 经 `claude_test.go:45-46` 嵌入 JSON 字符串，`\n` 解码为真实换行）。 |
| QA-05 low | 成立 | **已闭合**（Observed） | `review_check.go:24-25` 入参 ids 复制后排序（不改调用方切片，同时使 `reviewScope` 锁序稳定）；`:82-83` `slices.Sorted(maps.Keys(runs))` 替换 map 迭代；`:122-127` 最终对 `problems` 按 `Path`、`Message` 排序。原件哈希校验的文件名顺序也已固定：`reviews.go:632-633`、`:748-749`。排序落在公开 API 内，`deps.go:313-328` 无需预排序即稳定。回归 `review_revision_test.go:72-107` 连续 20 次 `CheckBoard` 断言四条诊断完全相同。 |
| QA-06 recommend | 成立 | **已闭合**（Observed） | `AGENTS.md:33` 的 `internal/board` 行已补「审核 run/batch 身份、原件、逐卡发布索引与完整性校验」，未改变包边界描述与依赖方向。 |
| QA-07 suggest | 成立 | **已闭合**（Observed） | `validate.go:12` 现直接是 `validateContextMode`，包装函数已删除；全仓 `rg 'validateContext\b'` 无匹配；测试改为显式传 `replay=false`（`archive_test.go:197`）。 |
| QA-08 suggest | 成立 | **已闭合**（Observed） | `archive_test.go:226-291` 中 `_, rest, _ := splitAgentArgs(...)` 与 `_ = rest` 已删除；`advance.json` 现经 `--advance-file` 由真实 CLI 读取（`:251` `nextArgs`），并断言外部成员 → exit 2 + `unattributed or foreign delivery: <sha>` 且无意图落盘（`:263-269`）、改动过的实时 spec → `batch binding conflict`（`:272-278`）、改传归档 `task-context.md` 后成功并校验 `Commit/PreviousRunID/ReviewedCommit`（`:279-290`）。原「写盘后从不读取」的死代码已消除。 |

## 修复区间行为核对

| # | 受影响契约点 | 结论 | 证据 |
| --- | --- | --- | --- |
| A | 逐卡发布仍原子、成功卡不重写、重试只补缺项 | 未被破坏（Observed） | `reviews.go:526-625` 结构未变；新增的 `appendReviewIndex`/`ParseReviewIndexes` 错误均在单卡事务内返回，落入 `failures[id]`（`:617-624`），不影响其他卡 |
| B | 已发布卡重试只校验不重写 | 未被破坏（Observed） | `reviews.go:579-581` 短路到 `verifyCardReview`，新增的 defer 只包装错误串 |
| C | 正文只保留机器索引、不解析报告正文 | 未被破坏（Observed） | `reviews.go:654-660`/`:701-724` 仍只序列化 `ReviewIndex`；CRLF 卡的索引行尾 `\r` 被 `json.Unmarshal` 作为合法空白接受 |
| D | check 识别零卡发布 / 哈希 / 语言 / 成员 / 前驱 | 未被破坏（Observed） | `review_check.go:87-117` 判定条件未变；`:36-57` 由 `return` 改为 `add(...)+continue` 只影响诊断聚合，`CheckBoard` 退出码仍非零（`deps.go:355-361`），不产生假通过 |
| E | 发布期状态要求与文档一致 | 一致（Observed） | `reviews.go:543-547` 仅对 `!current.Published[id]` 的卡重跑 `reviewCards`；`docs/review-evidence.md:17` 已改为「创建意图及向未发布卡发布时」 |
| F | 文件行数硬规则（>1000 行） | 不触发（Observed） | 修复区间最大文件 `internal/board/reviews.go` 861 行、`review_check.go` 307 行；`internal/board` 下无 >1000 行文件 |
| G | 新增/改写测试的有效性 | 满足（Observed） | 三个新测试各自断言不同行为（标题形态后的连续发布 / 公开诊断归属与顺序 / 四类损坏记录后继续检查），无重复覆盖；`review_revision_test.go:14-19` 的 CRLF fixture 用 `tx.Put` 仅用于构造卡片本体，改既有 REVIEWS 的动作全部经 `UpdateDocument` |

## 门禁 findings

**无。** 本轮修复区间未引入、加剧或掩盖任何 blocking/high/medium 级问题，也未发现前次修复破坏其所触及的要求。

## NON-BLOCKING

### QA-09 — suggest — `appendReviewIndex` 的换行风格取自单个探测点，可能在 LF 卡里写出 CRLF 的机器区

- Claim: **Observed**（写入行为）/ **Inferred**（后续操作摩擦）
- 证据：`internal/board/reviews.go:685-688`

  ```go
  newline := "\n"
  if found && start > 0 && text[start-1] == '\r' || !found && strings.Contains(text, "\r\n") {
      newline = "\r\n"
  }
  ```

  两个分支各只看一个信号：区段已存在时看标题行末是否为 `\r`；区段不存在时只要正文**任意位置**出现过一次 `\r\n` 就整段用 CRLF 写出（含新建的 `## REVIEWS` 标题与索引行，`:690`）。
- 影响：一张 LF 卡若被编辑器把 REVIEWS 标题改成 CRLF（正是 `review_revision_test.go:16` 的 `header-crlf` 用例），此后追加的索引行是 CRLF，机器区变成混合行尾。解析不受影响（`ParseReviewIndexes` 按 `\n` 切分，残留 `\r` 被 `json.Unmarshal` 当作合法空白）；但该区受 `update.go:131-137` 的托管保护，后续任何把整卡行尾归一化的受控 update 都会因 REVIEWS 正文字节变化被拒（`board.transaction_managed`）。触发需要操作者先做行尾归一化，且修复前同一场景的后果（重复区段、卡片锁死）严重得多，因此不构成回归。
- 最小改动：把 `newline` 统一取自卡片主导行尾（例如 `strings.Count(text, "\r\n")*2 > strings.Count(text, "\n")`），或在 `found` 分支沿用区段内既有索引行的行尾，而不是标题行的尾字节。

### QA-10 — suggest — `KANDER-REVIEW-RULES.md:256` 的发布期状态要求比实现更严，且与同批更新的 `docs/review-evidence.md:17` 措辞不一致

- Claim: **Observed**
- 证据：本次改为 `rules/KANDER-REVIEW-RULES.md:256`「Directory cards in working/review and a common report language are required **both at intent creation and at publication**」；同一批的 `docs/review-evidence.md:17` 写的是「创建意图及**向未发布卡**发布时均要求 working/review 目录卡」。实现按后者：`internal/board/reviews.go:543-547` 只在 `!current.Published[id]` 时调用 `reviewCards`，已完整发布的卡走 `:579-581` 的 `verifyCardReview`，该路径不校验 `Entry.State`。
- 影响/理由：不是行为缺陷——规则是给调用方的前置约束，比工具实际强制的更保守是安全方向。但同一提交里两份文档对同一约束给出宽窄不同的表述，读规则的编排端会认为「已完整发布的卡进入 done 后同 run 重试会被拒」，而实际会成功返回。属于表述精度问题，不影响门禁。
- 最小改动：在 `rules/KANDER-REVIEW-RULES.md:256` 把「at publication」限定为「when a card is first published」，与 `docs/review-evidence.md:17` 对齐；`:265` 关于终态卡的段落不需要改动。

---

**说明**：本报告只覆盖修复区间 `4b0a397..e12ed3c`，`4b0a397` 时点未变更的代码按约定视为本角色已接受，未重审。执行端的测试/构建/交叉编译结论为作者声明，本 Reviewer 无执行权限，未实测。Windows 原生环境缺口按调用方约定保留，未据此推断实际缺陷。PM 侧 findings（含与本角色共用处置的 PM002/PM003/PM004、以及作者部分拒绝的 PM005、范围外的 PM007）不在本次复核范围。

## review-round-1.md

# A 第三批首轮审核处置

任务：20260907-review-report-archive-task。作者：原执行端 Codex。本文是作者独立核实与修复记录，不替代 PM/QA 复审结论。

## 审核输入与交付

- 完整派回通知、PM/QA 原报告逐字保存于 [审核原文](review-round-1-original.md)，包含 NON-BLOCKING 项。
- 固定审核 base：`839f72119b22ce8b48811fc9819de9643a4028f8`。
- 首轮 target / 后续 reviewed-commit：`4b0a3971b08e1a6c0b42451c50b14373de61ce03`。
- 最新组基线：`4b0a3971b08e1a6c0b42451c50b14373de61ce03`；fetch 成功，任务分支 rebase 此组 HEAD，无冲突且没有重写提交。
- 最新交付：`e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a`；本地 HEAD 与 origin/review-report-archive 一致。
- 本轮提交：
  - `ebb13ee9c180be6d5e4e3ae8e1576d35e7c912a5`：显式看板覆盖优先于工作目录读取。
  - `75bcfb2a2a3d4ec0fc59d54a15f29f5b46bca4b3`：统一 REVIEWS 定位、诊断归属与排序、损坏记录聚合及发布状态文档。
  - `e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a`：报告重放字节一致、真实 CLI 推进测试、冻结上下文规则与测试入口清理。

## 逐项作者处置

| 原作者编号 | 作者结论 | 独立证据、处理及验证 |
| --- | --- | --- |
| PM001 medium mechanical | 成立，已修复规则 | `PrepareReviewRun` 确实同时冻结 task-context 哈希与 report_language，原规则漏写。`TestArchiveBatchAdvanceRangeAttribution` 现在通过真实命令入口：首次以 spec 路径运行，受控 update 添加执行记录，同批修复传实时 spec 得到 `batch binding conflict` 且不创建新 run；改用首轮归档 task-context.md 绝对路径后，推进、发布、前驱完整性校验均成功。规则补全冻结五项及后续角色/修复/恢复复用原件路径，保持同 batch。 |
| PM002 medium mechanical | 成立，已修复测试 | 原 advance.json 从未被 CLI 读取，rest 仅被丢弃，实际只有内存校验。改用 `--advance-file` 经 `captureRun` 贯穿读取、Git 范围校验、意图和发布；断言外部成员出现含目标 SHA 的 `unattributed or foreign delivery`、exit=2、无意图，纠正成员并复用冻结上下文后成功。检查 JSON 编码错误和首轮退出码，移除无效 rest 计算。 |
| PM003 low | 成立，已修复；与 QA-03 共用处置 | 子进程进入隔离临时目录并删除该 cwd，确认 Getwd 失败；绝对 KANBAN_DIR 仍有效。修复前 BoardRoot 返回找不到看板，修复后优先读取显式覆盖并正确返回。`TestBoardRootOverrideAfterCWDDeletion` 通过，不更改测试主进程 cwd。 |
| PM004 low | 成立，已修复；与 QA-04 共用处置 | Claude 假 CLI 的真实 review/重放链复现首轮多一个换行：无尾换行及已有尾换行两种原报告都不一致。采纳 QA 的输出原字节方案，归档模式首轮写入 report 原字节并 sync；不向归档原件添加换行，也不在重放时改写原件。`TestArchiveJSONReviewerReplayBytes` 断言首轮、归档 report、删除 CLI 后的重放三者字节相同；无任务绑定仍沿用原行为。 |
| PM005 low | 发布状态事实成立；采纳文档修正，拒绝移回 done 的恢复描述 | `TestReviewPublicationRejectsTerminalMove` 经受控 update 完成测试卡 SUMMARY/report，再以 MoveWithOptions(Result=completed) 实际移入 done。此后逐卡发布失败，原件可读取；默认 check 跳过 done，显式卡 ID check 报未完成发布；MoveEntry(done,working) 被拒绝。原报告所说“移回 working/review 后重试”不符合终态不可复用规则。文档限定待发布期间 working/review 互移，终态迁移前先完成发布；提前进入终态须另行确认处理。未增加 done 门禁、未放宽状态、未实现 R。 |
| PM006 suggest | 成立，采纳诊断聚合 | 四类隔离破坏分别为 runs 下非目录条目、非法 JSON、错误 schema、无效 UTF-8 卡文档。修复前均使 CheckReviewEvidence 提前返回并跳过另一张已篡改报告的卡；修复后收集局部问题并继续检查，其余卡的 task/run 诊断保留。根目录枚举、锁、事务恢复等整体失败仍返回错误，未忽略失败。`TestReviewCheckContinuesAfterInvalidRecords` 四项通过。 |
| PM007 suggest out-of-contract | 写入/读取事实成立，保留范围外；不实施清理 | 独立临时探针使用真实 Prepare/Finalize/Publish 与 operationRecords：单卡单 run 留有 5 条操作记录，report.md 有 2 份 committed 日志副本，分别对应控制 originals 与卡片 reviews。调用链为 WithTransaction → pending → operationRecords → readOperationRecords，全量读取并 JSON 解码日志；当前没有裁剪已提交日志的路径。实际耗时增长未做基准，性能量级仍是推断。契约明确排除保留期清理且不重做 S；本轮未改日志实现。后续可另卡评估头部扫描/按需解码与保留期策略，需测量及恢复兼容性设计。探针执行后删除，未把日志保留策略固化为本卡回归。 |
| QA-01 medium | 成立，已修复 | 实测公开 CheckBoard 返回的 stderr 原为仅 `report.md: hash mismatch`，缺任务及 run。CheckReviewEvidence 将任务标识写入 Message；checkRunStructure/verifyCardReview 统一补 run 标识，底层错误保留。`TestReviewCheckPublicDiagnosticsAndOrder` 同时篡改两卡的 report/manifest、各两个 run，断言公开 stderr 每行可定位卡及 run。 |
| QA-02 medium | 成立，已修复 | 不只验证字符串：`TestReviewPublicationAfterControlledHeaderUpdate` 覆盖标题尾空格、LF 卡仅标题变 CRLF、已有 CRLF 卡标题添加空格/tab。每项均首次发布、通过 UpdateDocument 改标题并追加独立正文、发布第二角色、同 run 再发布、验证两份完整证据与 CheckBoard。修复前均在后续重试出现 duplicate REVIEWS section；修复后两条索引、正文后续章节保留、完整性检查通过。读写共用同一标题/区段定位器，保留区段换行风格，提交前复核构造后的索引。测试未绕过受控 update 修改既有 REVIEWS。 |
| QA-03 low | 成立，已修复 | 同 PM003，保留此独立编号；子进程删除 cwd 的实际 BoardRoot 调用回归通过。 |
| QA-04 low | 成立，已修复 | 同 PM004，保留此独立编号；采用原字节 stdout 写入，Claude 两种尾换行真实运行与重放链回归通过。 |
| QA-05 low | 成立，已修复 | CheckReviewEvidence 对输入任务、run、最终问题排序；原件哈希校验按文件名排序，避免多份损坏时首个文件不稳定。排序在公开 evidence API 内完成，因此所有调用方一致，不要求 deps 预排序。两卡两 run 连续 20 次公开 CheckBoard 断言四条诊断精确相同且任务/run 顺序固定。 |
| QA-06 recommend | 成立，采纳文档补全 | AGENTS.md 包图的 board 职责已加入审核 run/batch 身份、原件、逐卡发布索引与完整性校验。仅说明既有依赖方向和职责，没有改变包边界。 |
| QA-07 suggest | 成立，采纳清理 | rg 核对旧 validateContext 仅为测试包装，生产调用 validateContextMode。删除未导出包装，恢复测试显式传 replay=false；推进测试改走 CLI。无外部兼容调用需要保留，代码搜索不再有旧函数调用，全量 review 测试通过。 |
| QA-08 suggest | 成立，已修复 | 同 PM002，保留此独立编号；移除只为丢弃而计算的 rest，测试改用真实 CLI 参数与推进文件。 |

## 验证及实际限制

- 修复前新回归真实失败：删除 cwd、三种受控标题更新后的连续发布、缺归属公开诊断、四类损坏记录中止、两种 JSON 报告尾换行差异。退出 1，作为复现而非通过记录。
- 终态边界测试最初因 fixture 的 SUMMARY 未完成而被既有 done 条件拒绝；补齐隔离 fixture 后才完成终态行为验证，没有据此前置失败宣称产品缺陷。
- `go test ./...` 通过，包含实际子进程崩溃/事务恢复测试。
- `go test -race ./internal/board ./internal/review ./internal/fs` 通过。
- `go build ./...`、`go vet ./...`、`git diff --check` 通过；改动 Go 文件 gofmt 无输出。
- Windows amd64：board/review/fs 测试程序与完整 cmd/kander 二进制交叉编译通过；Linux 主机未执行原生 Windows 测试，缺口继续保留。review 新增命令行/假 CLI 用例及删除 cwd 用例为 Unix 平台测试。
- 增强推进测试的错误原因断言后，专项 `go test ./internal/review -run TestArchiveBatchAdvanceRangeAttribution -count=1` 再次通过。
- 最新组 HEAD rebase 后全量复验结果与原始测试输出见 [测试记录](review-round-1-tests.txt)。
- 本轮 Reviewer 接口测试使用隔离假 CLI；未执行新的 PM/QA 审核。首轮 PM/QA 未运行测试的事实保留，以上验证由执行端实际运行。
- 安装作用域 `kander check 20260907-review-report-archive-task` 通过。卡片记录继续通过 S 基线构建的 show/update 受控入口写入；未部署构建产物、未迁移真实看板。

## 本轮结论与后续

本轮已修复所有核实成立的阻断项，low/recommend/suggest 均逐项提供作者结论。没有已知遗留的本轮代码阻断；这不是 PM/QA 语义 PASS 声明。PM007 的范围外日志成本与 Windows 原生验证缺口保留。

请编排端接收最新任务分支交付，沿用同一固定 base/spec，reviewed-commit 使用首轮 target。执行端不自行审核、不修改组分支、不集成 develop、不清理 worktree。工作区、任务分支及交互会话保留，卡片进入 review 等待复审；RESULT 仍为空，完成收尾须等待组通知。

## review-round-2.md

# A 增量审核非阻断处置

任务：20260907-review-report-archive-task。作者：原执行端 Codex。完整通知和 QA 增量原报告见 [原文](review-round-2-original.md)；本文是作者核实记录。

## 结论及交付

- 本轮仅修改发布规则的一处措辞；运行时代码、测试代码及托管区保护逻辑均无交付变更。
- 完整交付 SHA：`8241a49b1f50cbb99acc68b4c3dc58b5a803253e`；本地及远端任务分支一致。
- 最新组 HEAD / 本轮起点：`e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a`。fetch 成功，对齐该组 HEAD 的 rebase 无冲突、没有重写提交。
- 固定审核 base 仍为 `839f72119b22ce8b48811fc9819de9643a4028f8`；QA 增量输入 target 为 `e12ed3cdde82a4e3d0907cb4df1cf60df7dbbc8a`，reviewed-commit 为 `4b0a3971b08e1a6c0b42451c50b14373de61ce03`。
- 按本轮通知事实：QA 增量 exit=0、无 gate，QA-01 至 QA-08 全部闭合；主控对 PM001/PM002 机械修正完成核对并实际运行推进测试通过，PM 未重跑。本执行端没有重新审核，已通过结论的承接与闭批由主控处理。

## QA-09 suggest：核实成立，保留当前策略

`appendReviewIndex` 已有区段取标题行尾；新建区段遇正文任一 CRLF 则选 CRLF。混合行尾可能出现，索引仍能解析，归档完整性检查仍通过。这是格式取舍，未发现数据损失、重复索引或执行状态回归。

临时隔离探针通过真实 Prepare/Finalize/Publish、UpdateDocument 和 CheckBoard 核实：

| 场景 | 托管正文内部 CR 数 | 整卡转 LF 的受控 update |
| --- | --- | --- |
| LF 卡首次发布后只将 REVIEWS 标题改 CRLF，再发布第 2 个 run | 1 | 拒绝修改 REVIEWS |
| 同一路径发布至第 3 个 run | 2 | 拒绝修改 REVIEWS |
| 首次发布前仅普通 NOTE 中含一处 CRLF，发布 1 个 run | 0 | 成功 |
| 同一路径发布 2 个 run | 1 | 拒绝修改 REVIEWS |

拒绝信息为 `update 不得修改受管字段或文档：REVIEWS`。拒绝后正文和 revision 均不变；四项 CheckBoard 均通过。单条索引末尾空白会被 SectionBody 的 TrimSpace 去掉，因此不能笼统断言所有行尾归一化都会被拒；真正触发条件是受保护区段正文比较结果变化。

处置：不采用“主导行尾”或“既有索引行尾”算法，本任务没有整卡行尾归一化契约。改变行尾选择仍不能消除已有混合正文，也不能代替机器记录保护。保持现有确定规则、解析兼容与严格托管区保护；不为可选格式操作放宽 update。不构成阻断，作为有依据的非阻断保留项交回主控。若将来需要自动归一化，应另行定义受控迁移协议，而非默许改写机器正文。

## QA-10 suggest：核实成立，采纳文档修正

`PublishReviewRun` 只对该 run 尚未发布的卡调用 working/review 状态校验；已有发布回执时执行 verifyCardReview。

隔离探针先完整发布，再经 UpdateDocument 填好测试卡 SUMMARY，以 MoveWithOptions(Result=completed) 实际进入 done。相同 run 重试及 ReviewPublicationComplete 均成功，卡仍处于 done，正文与 revision 不变。针对同一 done 卡创建新 run 则返回 working/review 状态错误。

因此，规则原来的 `both at intent creation and at publication` 比实际路径宽泛。改为 `both at intent creation and when a card is first published`，与 docs/review-evidence.md 的“向未发布卡发布时”对齐。该修正只明确既有行为，不放宽任何实现状态约束，也不新增 done 门禁。

## 验证及保留事实

- 临时探针源码见 [核实源码](review-round-2-probe.txt)，执行过程见 [测试记录](review-round-2-tests.txt)。探针只在隔离临时看板运行，核实后移出仓库，不作为本次测试代码交付。
- 首次探针对 header-two 错设“归一化成功”预期，实际被 REVIEWS 保护拒绝而 exit=1；检查追加时对上一条行尾的选择后改正探针预期，重跑四个行尾场景和已发布 done 场景全部通过。保留初始及纠正后日志，不把探针预期错误记作产品缺陷。
- `go test ./...`、`git diff --check` 通过；rebase 最新组 HEAD 后全量复验通过。
- 本轮只有 Markdown 规则措辞变化，未重复运行 race/Windows 交叉编译；前轮实际通过记录仍在 [首轮处置](review-round-1.md)。Windows 原生环境仍未验证，不以交叉编译声称已验证原生行为。
- PM007 保留范围外：日志确实留存完整原件副本，后续事务读取全部操作记录；未测量长期性能、未实现清理。原始实测和后续建议保持在首轮处置。
- PM005 反证保留：尚未发布就进入 done 时发布失败，done 不可移回 working。QA-10 的已发布回执重试成功不改变此结论，不能将二者混为自动恢复方案。
- 原审核报告的只读权限、未实测作者测试声明、未删除任务文件事实原文保留。
- 未部署、未迁移真实看板、未修改组分支、未集成或清理。

## 交回

QA-09 保留为非阻断格式建议；QA-10 已修正规则。未新增已知阻断。原文、作者处置、交付与验证记录通过 S 基线 show/update 受控入口保存，RESULT 保持为空。卡片返回 review，等待主控接收并按已通过结论承接规则闭批；任务分支、worktree 与会话保留。
