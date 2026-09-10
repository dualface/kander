# 任务组 20260909-config-layering-group 批次 batch-one 任务上下文

本批次覆盖组分支 `7466cd6acfd3077bdf311c0bb0ca163566d37ba6..5631b184af859cd726f07bfa119e5378a5e3ded3` 上两张卡的全部交付。每张卡的六个契约字段与 DISCUSSION 原文如下;各卡的 OUT_OF_SCOPE 是判断其发现是否越界的边界。


---

# TASK_ID: 20260909-review-stages-per-size-task

# 审核阶段策略按 large/small 任务规格分档

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260909-config-layering-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 02:04
- OWNER: cursor
- SESSION: cursor ec197269-b129-4485-ac57-313cdf331a95
- WINDOW: herdr:w2R:t6:w2R:p8
- STARTED_AT: 2026-09-09 02:09
- FINISHED_AT:
- TASK_BRANCH: 20260909-review-stages-per-size
- RESULT:

## GOAL

让 `review_stages` 配置可以为 `large` 与 `small` 两种任务规格分别指定四个审核角色(PM、QA、CSA、Hacker)的阶段策略(`auto`/`skip`/`required`),使大任务可以走更严格的审核流程、小任务走更轻的流程。现有平铺结构 `{角色: 模式}` 只能表达一套策略,无法按规格区分。

实现范式照抄现有的 `kanban_agents.large/small`:配置结构、校验、默认回填、访问器、doctor 修复、摘要折叠、TUI 按档循环。

## USER_DECISIONS

- 新结构为 `"review_stages": {"large": {角色: 模式}, "small": {角色: 模式}}`。
- 旧的平铺结构 `{角色: 模式}` 继续合法:读入时同时作用于两档;保存时统一写出新结构。
- 规则文档中"审核阶段"第 3 级优先级改为"配置中该卡 `SIZE` 对应档位的 `review_stages` 值;任务组批次含多种规格时按 `large` 档"。
- 混合形态(档名与角色出现在同一层,如 `{"large": {...}, "PM": "auto"}`)视为非法,报可读错误并指出冲突的键。
- 导出规范化函数(建议名 `config.NormalizeReviewStages(raw any) (map[string]any, error)`),把旧平铺原始 JSON 规范化为两档原始结构,供后置卡在深合并前对作用域配置调用。

## EXPECTED_OUTCOME

- `internal/config` 接受并输出按档的 `review_stages`;旧平铺配置加载后 `kander config --json` 输出两档内容相同的新结构;缺失的档或角色回填为 `auto`;未知档名、未知角色、非法模式值报错。
- 新增访问器 `config.ReviewStageFor(cfg, scale, role)`,现有读取 `cfg.ReviewStages[role]` 的调用点(`internal/menu`、`internal/tui`、`internal/flow`、`internal/config/format.go`)全部改为按档读取。
- `kander config`(非 JSON)摘要按档展示阶段策略,两档完全相同时折叠为一行。
- `kander doctor` 修复:缺失一档时从另一档回填,两档都缺时回填默认值。
- TUI 选项面板"审核与模型"分区:每个角色下按 large/small 各有一个阶段选择,保存后写入新结构;`internal/flow` 的角色列表按档列出。
- 规则文档 `rules/KANDER-REVIEW-RULES.md` "Review Stages" 一节、`AGENTS.md` 中 `review_stages` 的说明、`docs/custom-agents.md` 同步更新;i18n 三语(en、zh-CN、ja)文案补齐。

## ACCEPTANCE_CRITERIA

- [ ] `config.Validate` 接受 `{"large": {...}, "small": {...}}`,接受旧平铺 `{角色: 模式}` 并映射到两档,拒绝未知档名、未知角色、非法模式以及档名与角色同层的混合形态(错误信息含冲突键名);每种情形都有单元测试。
- [ ] 加载旧平铺配置后执行 `Save`,文件内 `review_stages` 为两档结构,且两档内容相同;有往返测试。
- [ ] 导出的规范化函数对旧平铺、两档、混合形态三种原始输入的行为有单元测试。
- [ ] 缺失整个 `review_stages` 节、缺失某一档、缺失某一角色时,均回填 `auto`,并有测试覆盖。
- [ ] `config.ReviewStageFor(cfg, scale, role)` 存在并有测试;仓库中不再有按 `cfg.ReviewStages[role]` 平铺读取的调用。
- [ ] `kander config --json` 输出新结构;`kander config` 摘要在两档相同时折叠为一行、不同时分两行展示,有 `format.go` 测试覆盖。
- [ ] `internal/config/repair.go` 的修复逻辑覆盖缺档回填,有测试。
- [ ] TUI 选项面板每个角色按 large/small 提供阶段选择并能保存,`internal/menu`、`internal/tui`、`internal/flow` 现有测试更新并通过。
- [ ] `rules/KANDER-REVIEW-RULES.md` "Review Stages" 第 3 级明确写出"按该卡 `SIZE` 对应档位取值;任务组批次含多种规格时按 `large` 档";`AGENTS.md` 与 `docs/custom-agents.md` 的配置说明同步;`rules/embed_test.go` 与 `internal/install` 的规则摘要相关测试通过。
- [ ] en、zh-CN、ja 三个 i18n 文件对新增文案键齐全,`go test ./...` 与 `go vet ./...` 通过。

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 现有问题:不修改审核流程本身(阶段顺序、触发条件、批次机制)以及 `kander review` 的参数;原因是本卡只改配置的表达粒度。
- 加固:不在 `internal/board` 或 `internal/review` 中按 SIZE 强制校验 `--requirements-file`;原因是现状即为 Agent 侧解析,强制执行是另一项设计决策。
- 共享契约与文档:不改 `kanban_agents`、`models.*` 等其他配置键的结构;README 三语不改,因为它们目前不描述 `config.json` 细节。
- 相邻功能:不实现项目目录 `.kander-config.json` 覆盖(由后续卡 `20260909-project-config-overlay-task` 负责)。

## DISCUSSION

```text
PREREQUISITES: N/A
```

- 任务组 `20260909-config-layering-group` 共 2 张卡,线性依赖:本卡先行,`20260909-project-config-overlay-task` 依赖本卡。原因:两卡都改 `internal/config/config.go`、`format.go`、`kander config` 输出与同一批规则文档,并行会冲突。
- 现状:`review_stages` 在代码中没有强制消费方,只经 `kander config --json` 和 `rules/KANDER-REVIEW-RULES.md` "Review Stages" 四级优先级传给 Agent;Go 侧消费者是 `internal/menu/options.go`、`internal/tui/options_form.go`、`internal/flow/flow.go`、`internal/config/format.go`。
- 参照实现:`kanban_agents` 的 `validateKanbanAgents`、`KanbanAgentFor`、`repair.go` 回填、`FormatKanbanAgentsSummary` 折叠、TUI 按 `config.TaskScales` 循环。
- 本仓库 `AGENTS.md` 规定 CSA/Hacker 恒为 N/A;这不影响配置结构,只影响本卡的审核角色。
- 说明(非用户决策):本卡不在 Go 代码中强制执行阶段策略,与现状一致,策略经 `kander config --json` 与规则文本传给 Agent;见 OUT_OF_SCOPE。
- 规范化函数的动机:后置卡在校验前深合并原始 JSON,若作用域配置仍是旧平铺而覆盖文件写两档,合并会产生混合形态;后置卡在合并前调用本卡导出的规范化函数消除该情形。
- CARD_REVIEW: 独立评审(新会话 Claude 子代理,只读两张卡与用户原始需求)指出三处问题并已修正:把"不在 Go 中强制执行"从 USER_DECISIONS 移到 DISCUSSION 说明;补充混合形态报错的决策与验收;补充旧平铺加载后 Save 写出两档的往返验收;为消除与后置卡的跨卡缺口,新增导出规范化函数及其验收。评审结论:修正后通过。
- SELF_REVIEW: 已对照用户需求与确认方案复核。目标与产出一致;新结构、旧结构兼容、规则第 3 级改法均来自用户确认的方案而非建议;边界排除了强制执行与项目覆盖两项并给出原因;验收条目逐条可判定且覆盖代码、TUI、文档、i18n。未发现需要新用户决策的歧义。


---

# TASK_ID: 20260909-project-config-overlay-task

# 项目目录 .kander-config.json 覆盖作用域配置

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260909-config-layering-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 02:04
- OWNER: cursor
- SESSION: cursor 409f7740-a172-4db9-b1e5-6d027a031821
- WINDOW: herdr:w2R:t7:w2R:p9
- STARTED_AT: 2026-09-09 02:24
- FINISHED_AT:
- TASK_BRANCH: 20260909-project-config-overlay
- RESULT:

## GOAL

允许项目在其 Git 主工作树根目录放置一个可提交到版本库的 `.kander-config.json`,其中的键覆盖当前作用域(全局 `~/.config/kander/config.json` 或项目安装 `.kander/config.json`)的同名配置,使同一份 `kander` 二进制在不同项目里运行时得到项目专属的执行 Agent、审核者、审核阶段策略、模型档位、模块开关等设置。

当前配置路径只由二进制所在位置决定(`internal/config/paths.go` 的 `InstallPathsFromEntry`),不看当前目录,也没有任何配置分层或合并逻辑。

## USER_DECISIONS

- 覆盖文件名固定为 `.kander-config.json`,位置在当前目录所属 Git 主工作树的根目录(不在 `.kander/` 内);非 Git 目录则从当前目录向上逐级查找第一个命中的文件。
- 合并规则:在校验前对原始 JSON 做对象级深合并——对象按键递归合并,标量和数组整体替换;合并结果再走现有 `Validate`。
- 覆盖文件中禁止出现 `schema_version` 与 `welcome_complete`;出现这两个键或任何未知键时直接报错(不像作用域配置那样静默丢弃)。
- 写入语义不变:`Save`/`Update`/`SaveIfUnchanged`、TUI 选项面板、`kander doctor` 修复、安装向导只写作用域 `config.json`,不修改项目覆盖文件。
- 优先级:`KANDER_CONFIG` 指定的文件仍作为作用域配置(由"覆盖是叠加层"推导);项目覆盖文件叠加在其上。最终优先级为 项目覆盖 > 作用域配置 > 默认值。
- 所有写入路径(`Save`/`Update`/`SaveIfUnchanged`、doctor 修复、TUI 保存、安装向导)以未合并的作用域配置为读-改-写基底,覆盖文件中的值不得反向写入作用域 `config.json`。
- 深合并前先对作用域配置的原始 JSON 调用前置卡导出的 `review_stages` 规范化函数,把旧平铺规范化为两档结构,避免合并出档名与角色同层的混合形态。

## EXPECTED_OUTCOME

- `internal/config` 新增覆盖文件定位与合并:`config.Load` 及其等价读取入口返回合并后的生效配置;`kander config --json` 输出合并结果;`kander config`(非 JSON)在存在覆盖文件时额外打印一行覆盖文件的绝对路径。
- 覆盖文件读取复用现有的 symlink/reparse 防护(`rejectLeafReparse`、`ensureWindowsPathNofollowSafe`、`readConfigBytes`),文件必须是常规文件。
- 覆盖文件不存在时行为与现状完全一致;覆盖文件存在但 JSON 非法、含禁止键或未知键时,读取配置的命令报错并指出文件路径与键名。
- TUI 选项面板加载的是作用域配置(不含覆盖),在检测到覆盖文件时于面板顶部显示一行提示:显示与编辑的是作用域配置,项目 `.kander-config.json` 中的键在运行时优先。
- 规则文档 `rules/KANDER-AGENTS.md` 的路径表新增"项目覆盖配置"一行并说明优先级;`rules/KANDER-KANBAN-RULES.md` 命令契约、`AGENTS.md`、`docs/custom-agents.md` 同步;i18n 三语文案补齐。

## ACCEPTANCE_CRITERIA

- [ ] 在 Git 主工作树根放置 `.kander-config.json` 后,从主工作树、子目录和任务 worktree 中运行 `kander config --json`,均输出合并后的值;删除该文件后输出回到作用域配置。有测试覆盖 Git 与非 Git(向上查找)两种定位方式。
- [ ] 深合并语义有单元测试:嵌套对象按键合并(如只覆盖 `review_stages.large.PM`),标量与数组整体替换,合并后仍通过 `Validate`。
- [ ] 覆盖文件含 `schema_version`、`welcome_complete` 或未知键时报错,错误信息含文件路径与键名;JSON 非法、非常规文件、symlink 时报错;每种情形有测试。
- [ ] `Save`/`Update`/`SaveIfUnchanged`、`kander doctor` 修复、TUI 选项面板保存只写作用域 `config.json`,覆盖文件字节不变;执行 doctor 修复与 TUI 保存后,作用域 `config.json` 中不含覆盖文件独有的值;两点均有测试。
- [ ] 作用域配置为旧平铺 `review_stages`、覆盖文件为两档 `review_stages` 时,合并结果通过 `Validate` 且覆盖档位生效;有测试。
- [ ] `kander config` 非 JSON 输出在有覆盖文件时多打印一行其绝对路径;TUI 选项面板在有覆盖文件时显示提示行;各有测试。
- [ ] `rules/KANDER-AGENTS.md` 路径表新增覆盖文件行并写明优先级,并明确写出"覆盖文件可指定 `agents` 自定义 Agent 的可执行路径与 argv 模板,信任层级等同于检出并运行该仓库代码"这一已接受风险;`AGENTS.md`、`docs/custom-agents.md`、`rules/KANDER-KANBAN-RULES.md` 同步;`rules/embed_test.go` 与 `internal/install` 规则摘要相关测试通过。
- [ ] en、zh-CN、ja 三个 i18n 文件对新增文案键齐全,`go test ./...` 与 `go vet ./...` 通过。

## THREAT_MODEL

- 资产:用户作用域配置的完整性;Agent/审核者/模型选择不被仓库中的文件静默篡改到未知值。
- 可信主体:当前用户,以及当前用户选择检出的项目仓库的维护者。
- 攻击者能力:向仓库提交一个 `.kander-config.json`。它只能改变已有配置键的取值(仍受 `Validate` 的取值域约束,如 Agent 名必须是已配置的 Agent),不能引入新的可执行路径以外的行为;`agents` 自定义 Agent 定义中的可执行路径与 argv 模板属于已有配置键,因此覆盖文件能够指定它们——这是接受的风险,与用户检出并运行仓库代码本身的信任层级一致,须在文档中明确写出。
- 不在范围:通过 symlink/reparse 指向仓库外文件读取——由现有防护拒绝。

## OUT_OF_SCOPE

- 现有问题:不改变 `KANDER_CONFIG` 与二进制位置决定作用域配置的现有逻辑;原因是覆盖是叠加层,不替换作用域解析。
- 加固:不为覆盖文件提供签名、白名单键集或交互式信任确认;原因是已在 THREAT_MODEL 中接受该风险并要求写入文档,进一步加固另立卡。
- 共享契约与文档:不提供 `kander config --set`、`--project` 等写入覆盖文件的命令;不改 `kanban/` 与 `KANBAN_DIR` 的定位;README 三语不改。
- 相邻功能:`review_stages` 的按档结构由前置卡 `20260909-review-stages-per-size-task` 负责,本卡只需其合并后能通过校验。

## DISCUSSION

```text
PREREQUISITES: 20260909-review-stages-per-size-task
```

- 任务组 `20260909-config-layering-group` 共 2 张卡,本卡依赖前置卡;原因见前置卡 DISCUSSION(两卡共同修改 `config.go`、`format.go`、`kander config` 输出与规则文档)。
- 现状:`config.ConfigPath()` 先看 `KANDER_CONFIG`,再由 `CurrentInstallPaths()` 按二进制位置决定;`Load` 直接 `loadValidated(path)`;`Effective` 只重新校验。`internal/board/locate.go` 已有按当前目录定位 Git 主工作树并向上查找的范式(`BoardRootAt`),定位覆盖文件可复用相同思路。
- 建议的最小实现:新增 `config.OverlayPath(cwd)` 与 `config.LoadEffective()`(或改造 `Load`),在 `decodeJSON` 后、`Validate` 前对两份 `map[string]any` 做深合并;`Save` 系列保持读写作用域文件。
- 本仓库 `AGENTS.md` 规定 CSA/Hacker 恒为 N/A,PM/QA 适用。
- CARD_REVIEW: 独立评审(新会话 Claude 子代理,只读两张卡与用户原始需求)指出问题并已修正:标注 `KANDER_CONFIG` 一条为推导;新增"写入以未合并作用域配置为基底、禁止反向污染"的决策与验收;把 THREAT_MODEL 的风险说明列为可判定的文档验收;为跨卡缺口新增"合并前规范化旧平铺 `review_stages`"的决策与验收。"非 Git 向上查找"与"禁止 `schema_version`/`welcome_complete`"两条在用户确认的方案原文中已有,保留为用户决策。评审结论:修正后通过。
- SELF_REVIEW: 已对照用户需求与确认方案复核。目标与产出一致;文件位置、深合并、禁止键报错、只写作用域配置四项决策均为用户确认的方案;补充了 THREAT_MODEL 说明覆盖文件能改 `agents` 可执行路径的已接受风险并要求写入文档;验收条目可判定,覆盖定位、合并、报错、写入隔离、输出、文档与 i18n。未发现需要新用户决策的歧义。
