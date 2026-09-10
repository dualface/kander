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
