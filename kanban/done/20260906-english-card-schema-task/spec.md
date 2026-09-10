# Rename card schema tokens to English with legacy compatibility

- 类型: Chore
- SIZE: small
- 任务组:
- 创建时间: 2026-09-06 23:40
- 负责人: Claude Opus 5 (1M context)
- 会话:
- 窗口:
- 开始时间: 2026-09-07 00:55
- 完成时间: 2026-09-07 00:01
- 任务分支: english-card-schema
- 结果: completed

## 任务目标

任务卡的元数据字段名、章节标题和占位标记目前全是中文硬编码 (`internal/board/document.go`, `internal/board/board.go`, `internal/board/payload.go`, `internal/launch/metadata.go`, `internal/launch/types.go`, `internal/window/window.go`, `internal/liveness/classify.go`, `internal/liveness/lookup.go`, `internal/board/list.go`), 模板本身也是 Go 源码里的裸字符串拼接. 把这套 token 全部改为英文 UPPER_SNAKE, 所有语种共用同一套英文 token; 解析侧同时接受英文与旧中文, 已有中文卡片继续可读可迁移. 同时把模板从 Go 字符串抽成 `go:embed` 的 Markdown 文件.

## 用户决策

- 字段名一律英文大写 UPPER_SNAKE, 所有语种的模板都用同一套英文 token (用户确认, 附预览).
- 改名范围含元数据字段、`##` 章节标题与占位/记录标记, 不留中英混排 (用户确认).
- 程序必须兼容中英文, 旧中文字段卡不得失效 (用户明确要求).
- `rules/` 下的规则分册在同一张卡里同步改, 文档正文仍为中文 (用户确认).
- token 映射:
  - 元数据: `类型`->`TYPE`, `任务组`->`TASK_GROUP`, `创建时间`->`CREATED_AT`, `负责人`->`OWNER`, `会话`->`SESSION`, `窗口`->`WINDOW`, `开始时间`->`STARTED_AT`, `完成时间`->`FINISHED_AT`, `任务分支`->`TASK_BRANCH`, `结果`->`RESULT`.
  - 章节: `任务目标`->`GOAL`, `用户决策`->`USER_DECISIONS`, `预期成果`->`EXPECTED_OUTCOME`, `验收条件`->`ACCEPTANCE_CRITERIA`, `威胁模型`->`THREAT_MODEL`, `不在本轮范围`->`OUT_OF_SCOPE`, `讨论与决策`->`DISCUSSION`, `实施与验证`->`IMPLEMENTATION`, `完成总结`->`SUMMARY`.
  - 标记: `<填写>`->`<FILL_IN>`, `自审:`->`SELF_REVIEW:`, `卡审:`->`CARD_REVIEW:`, `前置任务:`->`PREREQUISITES:`.
- `结果` 的取值 (`completed` / `cancelled` / `duplicate` / `wontfix`) 本来就是英文, 不在本次改名内.

## 预期成果

- `kander new` 生成的新卡完全使用英文 token, 与语言设置无关.
- 旧中文卡片在 `list` / `show` / `check` / `move` / `start` / `resume` / `notify` / `dismiss` / TUI 下与改名前行为一致: 字段可读、契约门禁可判、`会话`/`窗口` 可回写、`完成时间` 可写入.
- 同一张卡混用中英 token 时按字段各自命中, 不因另一字段的语种而失效.
- 模板不再是 Go 源码里的字符串拼接, 而是 `go:embed` 的 Markdown 文件, 且与门禁校验的 token 来自同一份常量定义.
- `rules/` 下 4 个分册 (`KANDER-KANBAN-RULES.md`, `KANDER-REVIEW-RULES.md`, `KANDER-TASK-GROUP-RULES.md`, `KANDER-TASK-INTAKE-RULES.md` 中实际出现模板或 token 的部分) 与新 token 一致.

## 验收条件

- [ ] 新建卡 (小任务与大任务 `spec.md`) 的元数据、章节、占位符全部为英文 token, 无中文残留
- [ ] 解析侧对每个 token 同时接受英文与旧中文: 元数据读取、章节读取、占位符判定 (英文与旧中文两种写法)、`SELF_REVIEW:`/`自审:`、`CARD_REVIEW:`/`卡审:`、`PREREQUISITES:`/`前置任务:`、旧式无 `- ` 前缀的 `任务组:` 均命中
- [ ] 写入侧只写英文 token, 但对已有中文字段的卡片就地更新该字段而不是追加英文重复字段 (`负责人`/`开始时间`/`会话`/`窗口`/`完成时间` 回写路径逐个覆盖)
- [ ] 新增回归用例: 纯中文旧卡、纯英文新卡、中英混排卡三种形态各自跑通 check 契约门禁与 `move ... done` 门禁; 中文卡的 `会话`/`窗口` 回写后仍只有一个该字段
- [ ] 模板改为 `go:embed` 的 Markdown 文件, 模板与校验共用同一份 token 定义 (改一处不会两边分叉)
- [ ] `rules/` 下分册中的模板与 token 与实现一致
- [ ] `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .`, `git diff --check HEAD` 全绿
- [ ] 提交备注与新增代码注释为英文, 符合 `AGENTS.md` 语言约定

## 威胁模型

N/A. 不涉及认证、凭据、权限边界或沙箱参数; 改动限于卡片文本 token 的解析与渲染.

## 不在本轮范围

- 既有问题: 不改 `internal/menu/rules.go` 的中文否定词正则 — 那是扫描规则文本的独立机制, 与卡片 schema 无关.
- 加固: 不改 `internal/fs` 的安全边界或卡片写入的原子性/权限逻辑 — 本轮只改 token 文本, 任何安全语义改动都超出授权.
- 共享契约与文档: 不迁移 `kanban/` 里已有的中文卡片 — 兼容读取即可, 批量改写用户数据不可逆且用户未要求; `README.md` 与 `docs/` 除非直接展示模板否则不动.
- 相邻功能: 不改 `结果` 的取值集合, 不改 `internal/i18n` 的界面文案双语资源 — 前者本就是英文, 后者是面向用户的输出, 由 i18n 机制管辖.

## 讨论与决策

- 模板抽取方式选 `go:embed` 而非外部可编辑文件: 模板是 `check` 门禁校验的契约, 外部可改会让用户改完卡在自己的门禁上; embed 保留单二进制与「模板/校验同源」.
- 兼容策略为「读双语, 写英文」: 解析用 token 别名表 (英文为规范名, 中文为 legacy 别名), 渲染与回写只产出英文; 已有中文字段就地更新, 不追加重复字段.
- 自审: 通过. 目标与用户三点要求 (全英文大写 token, 所有语种共用, 程序兼容中英文) 一一对应, 范围按用户确认扩到章节与标记并同步 rules 分册; 边界把卡片数据迁移、i18n 文案、无关正则排除在外, 均非达成目标所必需; 验收条件覆盖新卡生成、双语解析、就地回写、三种卡形态回归、模板同源与文档一致, 均可执行可判定. 无需要用户新增决策的歧义.

## 实施与验证

- 任务分支 `english-card-schema`, worktree `worktrees/english-card-schema`, base 原为 `5868525`, 集成前 rebase 到 `0dd26f8`.
- `85df957` 主改动: 新增 `internal/board/schema.go` (token 常量 + 中文 legacy 别名 + 别名感知正则), 模板抽成 `internal/board/templates/*.md.tmpl` 经 `go:embed` + `text/template` 由 schema 常量填充; 解析、门禁、payload、liveness、launch/window 写入路径全部改走常量; 引用 token 的 i18n 消息两语种同步; `rules/` 四个分册的模板与 token 同步.
- `ab3966d` 审核修复: 规则里 6 处「教 Agent 写旧 token」的散文残留 + `zh-CN.json:108`, 另按非阻塞项一并修 README 与 AGENTS.md 中的 token 引用.
- `d3581e6` rebase 后修复: develop 在审核期间新增的 `cmd/kander/check_bind_test.go` 用中文 token 填卡, 与新模板不匹配; fixture 改为从 schema 常量构造.
- 审核: 本仓库 `AGENTS.md` 特例, CSA/Hacker 标 N/A, PM (grok) 与 QA (cursor) 并行首轮.
  - 两个角色各提 1 条 medium 机械项, 实为同一条: 规则残留会让 Agent 往已有英文空字段的卡里再写中文字段. QA 另外指出首个匹配优先的失败链 (空的英文字段先命中, 填了值的中文字段被忽略).
  - 用户随后指出该重复场景在修好规则后不会真实发生 (老卡只有中文字段, 新卡只有英文字段), 结论一致: 修规则即可, 不改解析逻辑.
  - 增量复审 (reviewed-commit `d47402e`) 两个角色均无 finding. QA 首次重跑因 Cursor 连接中断 `resource_exhausted` 失败, 同参数重跑成功.
- 验证: rebase 后重跑 `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .`, `git diff --check` 全绿; 真机冒烟确认新卡全英文 token, 纯中文旧卡 `check` / `list` / `show` 正常.

## 完成总结

卡片 schema 的元数据字段、章节标题与标记全部改为英文 UPPER_SNAKE, 所有语种共用; 解析侧接受英文与旧中文两种写法, 写入侧只产出英文并就地更新旧字段, 老卡无需迁移即可继续使用. 模板从 Go 裸字符串抽成 `go:embed` 的 Markdown 模板, 且由 schema 常量填充, 模板与门禁不会分叉.

偏差 (均已报告用户):
- 改了引用 token 的 i18n 消息, 超出卡片「不在本轮范围」写的「不改 i18n 文案」. 判断依据是这些消息里的是 schema token 而非译文, 不改会误导用户; 只改 token, 未动周围中文散文.
- 按审核非阻塞项一并修了 README.md 与 AGENTS.md 的 token 引用, 略微超出「README 除非直接展示模板否则不动」.
- 审核规则的 task context 模板 (六项契约章节) 也改为英文, 与卡片章节保持同源.

未处理项:
- `TestNewCardUsesCanonicalTokensOnly` 只覆盖小卡与 cn 语言. 两个 reviewer 均判定无产品缺口 (`renderContract` 不接收语言, 大卡走同一模板), 留作覆盖度改进.
- 全局安装的 `~/.agents` 规则副本仍是旧 token, 需重装同步.
- 存量卡片未迁移, 按契约由兼容读取承担.
- 集成前 rebase 撞上的语义冲突只落在测试 fixture, 用户明确选择直接集成, 未再重审.

验收结论: 八条验收条件全部满足, PM 与 QA 均通过.
