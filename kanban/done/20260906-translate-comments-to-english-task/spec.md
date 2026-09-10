# Translate all code comments to English

- 类型: Chore
- SIZE: small
- 任务组:
- 创建时间: 2026-09-06 23:40
- 负责人: Claude Opus 5 (1M context)
- 会话:
- 窗口:
- 开始时间: 2026-09-06 23:50
- 完成时间: 2026-09-06 21:50
- 任务分支: translate-comments-to-english
- 结果: completed

## 任务目标

`AGENTS.md`「语言约定」已规定代码注释一律英文, 但仓库存量注释仍是中文: 94 个 `.go` 文件含中文 `//` 注释 (行注释与文档注释, 无块注释), `Makefile`, `.gitignore`, `scripts/guard-kanban-write.sh` 也各有中文注释. 把这些存量注释全部翻译为英文, 让规则与代码一致.

## 用户决策

- 用户要求提交备注和代码注释一律英文, 已写入 `AGENTS.md`, 并要求单开一张任务卡把存量注释全部翻译.
- 面向用户的字符串不动: `internal/i18n` 的中英文资源、`install.sh` / `install.ps1` 的中文提示输出、测试里作为数据的中文字面量都保持原样 (`install.sh` 与 `install.ps1` 无中文注释, 本轮不改这两个文件).
- Markdown 文档 (`AGENTS.md`, `README.md`, `rules/`, `docs/`) 及 SVG 保持中文.

## 预期成果

- `grep -rnP '^\s*//.*[\x{4e00}-\x{9fff}]' --include='*.go' .` 无输出; `Makefile`, `.gitignore`, `scripts/guard-kanban-write.sh` 的注释行同样无中文.
- 英文注释保留原注释的信息量与技术判断, 不是逐字直译后语义走样, 也不借机删掉解释「为什么」的注释.
- 除注释外无任何代码语义改动: 编译产物行为不变, 字符串字面量、i18n 资源、测试数据一字未动.

## 验收条件

- [ ] `.go` 文件的中文 `//` 注释全部译为英文, 覆盖 94 个文件; 中文残留检查为空
- [ ] `Makefile`, `.gitignore`, `scripts/guard-kanban-write.sh` 的中文注释译为英文
- [ ] `internal/i18n/locales/*.json`, `install.sh`, `install.ps1`, 所有 `.md` 与 `.svg` 未被改动 (`git diff --stat` 不含这些路径)
- [ ] `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .` 全绿
- [ ] `git diff` 逐文件复核: 非注释行零改动 (可用忽略注释的对比或人工核对确认)
- [ ] 提交备注为英文, 符合 `AGENTS.md`「语言约定」

## 威胁模型

N/A. 纯注释文本改动, 不涉及资产、可信主体或攻击面变化.

## 不在本轮范围

- 既有问题: 不顺手修注释里指出的 TODO/FIXME 或它们描述的缺陷 — 那是独立的代码改动, 混进来会让「非注释行零改动」这条验收失效.
- 加固: 不调整安全相关实现或权限逻辑 — 本轮只改注释文本, 任何行为改动都超出用户授权.
- 共享契约与文档: `AGENTS.md`, `README.md`, `rules/`, `docs/` 及 SVG 保持中文 — 用户只要求注释和提交备注英文, 文档语言未变.
- 相邻功能: `internal/i18n` 中文资源与 `install.sh` / `install.ps1` 的中文输出不译 — 它们是面向用户的文案, 由 i18n 机制而非本规则管辖.

## 讨论与决策

- 自审: 通过. 目标与用户「单开任务全部翻译」一致; 边界把面向用户文案和文档排除在外, 与 `AGENTS.md` 新增条款的适用范围一致, 未把必需工作排除; 验收条件用可执行的 grep 与构建/测试命令判定, 并补了「非注释行零改动」这条防止夹带改动. 无需要用户新决策的歧义.

## 实施与验证

- 任务分支 `translate-comments-to-english`, worktree `worktrees/translate-comments-to-english`, base `f56eacb` (origin/develop).
- `b932516` 翻译存量注释: 94 个 `.go` 文件, `Makefile`, `.gitignore`, `scripts/guard-kanban-write.sh`, 共 683 行注释, 683 增 683 删, 无文件行数变化.
- 逐行核对: 每个改动行在新旧两侧都是注释行, 非注释行零改动 (脚本比对全部改动文件).
- 审核: 本仓库 `AGENTS.md` 特例, CSA/Hacker 标 N/A, 跑 PM (grok) 与 QA (cursor) 并行首轮.
  - PM 首轮 2 条 medium 机械项: `panelChrome` 每侧/合计译反, `ReviewStageLines` 误译为 prints.
  - QA 首轮 3 条 medium 机械项 (QA-3 与 PM-1 同一条): 卡片 schema token 被译成英文字面量, `repair.go` 规则修复的因果被译反, `panelChrome`.
  - `5868525` 一次性合并修复五条; QA-4 (注释不再引用 `会话` / `窗口` / `任务组` 等真实字段名) 为非阻塞建议, 与「中文残留检查为空」的验收条件冲突, 本轮未改, 已报用户.
  - PM 与 QA 增量复审 (reviewed-commit `b932516`) 均无 finding, 全部通过.
- 验证: `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .`, `git diff --check HEAD` 全绿; 中文注释残留 grep 为 0.

## 完成总结

存量中文注释全部译为英文, 规则与代码一致. 实际范围与建卡时盘点一致: 94 个 `.go` 文件加 3 个非 Go 文件; `install.sh` / `install.ps1` 无中文注释, 未改动; i18n 资源, 中文字符串字面量, Markdown 与 SVG 均未触碰.

偏差: 首次翻译有 5 处注释与代码不符 (两处方向译反, 三处把中文 schema token 写成英文字面量), 由 PM/QA 审核发现并在 `5868525` 修复. QA-1 未按审核建议改回引用中文 token, 因为那会破坏本卡已冻结的「中文残留检查为空」验收条件; 同源的 QA-4 一并留作未处理项交用户决定.

验收结论: 六条验收条件全部满足, PM 与 QA 均通过.
