# 选项面板增加流程图分区: 按当前配置画单卡与任务组全流程

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 11:38
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t1E:wX:p10
- STARTED_AT: 2026-09-08 11:39
- FINISHED_AT:
- TASK_BRANCH: options-workflow-flowchart
- RESULT:

## GOAL

在 `kander` 终端看板的选项面板 (`o`) 里增加一个只读分区「流程图」: 按当前配置画出单卡任务和任务组从执行到收尾的完整流程. 现在用户要判断「这套配置下任务会怎么跑」, 只能自己去读 `rules/` 下的多份规则再和七个模块开关, `review_stages`, reviewers, launcher 逐条对照; 面板里已经能改这些开关, 却看不到它们合起来意味着什么流程. 目标是让用户在改配置的同一处直接看到生效的流程.

## USER_DECISIONS

- 入口放在选项面板里, 作为一个分区.
- 图要覆盖两条流程: 单卡任务, 任务组; 范围是「从执行到收尾」的完整流程.
- 图的内容按当前配置生成, 不是一张写死的静态图.

## EXPECTED_OUTCOME

- 选项面板根菜单出现「流程图」分区, Enter 进入显示只读流程图, Esc 返回根菜单, 与环境检查报告一致的只读滚动体验 (键盘上下, PgUp/PgDn, 鼠标滚轮).
- 图的顶部给出当前配置摘要: 执行 Agent (按 SIZE 的大小档), launcher, 四个审核角色的 reviewer 与 `review_stages` 档位, 七个模块开关.
- 单卡流程覆盖: 建卡与自审 -> todo -> `start` 认领与启动 (按 SIZE 选 Agent, 按 launcher 落容器) -> 执行者读配置/规则/卡片 -> [git 开] 任务分支与 worktree -> 实现 -> 交付自检 -> [review 开] 按 `review_stages` 逐角色的审核与修复轮 -> [git 开] 集成与授权 -> [reporting 开] 完成报告 -> `move done` -> 清理.
- 任务组流程覆盖: 分组与依赖 -> 组集成分支 -> 主控 `start` 与 `subscribe` -> 成员卡并行执行 -> 成员进 `review/` -> 主控 ff 到组分支 -> 批次审核与派回 (`notify` 后成员自行 `move working`) -> 修复轮与闭批 -> 组集成 -> 收尾清理 -> `done`.
- 配置裁剪生效: 关闭的模块不画对应节点, 只在配置摘要里说明; `review_stages` 为 `skip` 的角色不出现在审核节点里, `required` 与 `auto` 可区分; `task_groups` 关闭 (或 `git` 关闭导致其不可用) 时任务组段落降级为一行说明.
- 面板里尚未保存的开关改动同样反映在图上 (数据取面板会话的配置, 不是磁盘配置).
- 新文案进 `internal/i18n/locales` 的 en / zh-CN / ja 三份目录.

## ACCEPTANCE_CRITERIA

- [ ] 根菜单新增「流程图」分区, 位置在「模块开关」之后, 「环境检查」之前; Enter 进入, Esc 返回根菜单, 不改动任何配置, 不产生未保存状态.
- [ ] 流程图正文复用既有只读长文本视图 (`reportView`), 支持滚动与鼠标滚轮; 不新增交互模型, 不阻塞面板输入.
- [ ] 流程生成逻辑与 UI 分离: 输入配置, 输出结构化行, 可脱离 TUI 单测; 不违反 `AGENTS.md` 的包图方向, 且 `internal/menu` 不 import `internal/tui`.
- [ ] 数据源是选项面板当前会话的配置, 面板内未保存的开关改动即时反映.
- [ ] 单卡流程节点覆盖 EXPECTED_OUTCOME 所列全部环节, 与 `rules/` 现有规则一致 (`KANDER-KANBAN-RULES.md` 的状态机与完成门禁, `KANDER-CODE-RULES.md` 的交付自检, `KANDER-GIT-RULES.md` 的分支与集成, `KANDER-REVIEW-RULES.md` 的角色与档位, `KANDER-REPORTING-RULES.md` 的完成报告).
- [ ] 任务组流程节点覆盖 EXPECTED_OUTCOME 所列全部环节, 与 `KANDER-TASK-GROUP-RULES.md` 一致.
- [ ] 单测覆盖配置裁剪: 七开关全开; `review` 关; `git` 关; `task_groups` 关; `task_groups` 开但 `git` 关; 四个角色分别为 `required` / `auto` / `skip` 的组合; 断言应出现和不应出现的节点.
- [ ] 渲染在窄弹窗 (内宽约 60 列) 下不被折断成乱码: 采用竖排流程加连接符的排版, 并有用例或人工记录说明窄宽下的表现.
- [ ] 新增文案在 en / zh-CN / ja 三份 locale 中键一致, `go test ./internal/i18n` 通过.
- [ ] 在模块根运行 `go test ./...` 通过, 记录命令, 提交号与用例数.
- [ ] `AGENTS.md` 包图与 README 的选项面板说明同步更新, 与改动同一 diff.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题: 不修既有分区 (界面 / 执行与模型 / 审核与模型 / 模块开关 / 环境检查) 的行为与文案, 不改 `menu.Session` 的配置读写; 本卡只新增一个只读分区, 动既有分区会扩大回归面.
- 加固: 不做配置与规则的一致性校验或告警 (例如规则文件被改动, 开关组合不合法), 不改 `internal/fs` 与配置权限策略; 本卡是展示功能, 校验属于 doctor 的职责.
- 共享契约与文档: 不改 `rules/*.md` 规则本身, 不改 `config.json` schema, 不改卡片格式; 图是规则的只读呈现. 仅同步 `AGENTS.md` 包图和 README 选项说明这两处与本次改动直接相关的文档.
- 相邻功能: 不新增 `kander flow` 之类子命令, 不做图内跳转到规则原文, 不做导出为文件或图片, 不做鼠标点击展开节点; 用户本次只要求面板里的一个分区, 其余待确认后另开卡.

## DISCUSSION

- 现有事实: 根菜单在 `internal/tui/options_form.go` 的 `openRoot`, 分区常量与 `dispatch` 在 `internal/tui/options_panel.go` (常量约 17-25 行, dispatch 约 465 行); 只读长文本用 `reportView` + `p.showReport(...)`, 换行 `wrapText` 按弹窗内宽处理, 滚动与滚轮已在 `options_view.go` 与 `Update` 里接好; 弹窗内宽由 `innerWidth()` 决定, 通常 60~100 列, 因此不要用宽方框图.
- 建议实现落点: 新增包 `internal/flow`, 输入 `*config.Config`, 输出带级别的结构化行, 由 `internal/tui` 映射成 `menu.ReportLine` 交给 `reportView`; 这样 `internal/menu` 不承担无关职责, 也不出现反向依赖. 若执行时发现更贴合现有包边界的落点, 可在不违反 `AGENTS.md` 包图方向的前提下调整, 并在 IMPLEMENTATION 说明理由.
- 图的详略取舍: 画状态机, 门禁和责任人 (谁做什么, 什么条件下发生), 不逐条复述规则原文; 每条流程的节点规模控制在 25~40 个以内, 便于在弹窗里阅读.
- 本仓库 `AGENTS.md` 里 CSA / Hacker 一律 N/A 属于仓库特例, 不写进按配置生成的图; 图只反映配置 (当前配置里这两个角色本就是 `skip`).
- 规则与实现若有出入, 以 `rules/` 现有规则为准并在 IMPLEMENTATION 记录, 不在本卡顺手改规则.
- SELF_REVIEW: 已按目标, 已确认方案和项目规则复核: 目标与结果一致, 用户确认的三条 (入口在选项面板, 覆盖单卡与任务组从执行到收尾, 按当前配置生成) 都落到 USER_DECISIONS 与验收条件; 排版形式, 包边界, 数据取会话配置这三条是本次分析给出的建议, 已写在 EXPECTED_OUTCOME / DISCUSSION 而非冒充用户决定, 其中包边界明确允许有理由地调整. 边界四类都给了取舍与理由, 未排除达成目标必需的工作. 验收条件可判定, 覆盖两条流程的节点范围, 配置裁剪矩阵, 窄宽渲染与三语文案, 未引入范围外要求. 未发现需要用户新决策的歧义.

## IMPLEMENTATION

工作目录: `/home/dualf/works/kander/worktrees/options-workflow-flowchart`。交付目标: `develop`。创建 base: `781aa0dcde99eefe0d7ccea84f5a138f414e831c`。

实现边界: 新增 `internal/flow`，只读消费规范化配置并输出结构化行；`internal/tui` 单向调用并转换为 `menu.ReportLine`，复用 reportView。既有配置接口和文件格式保持兼容。

规则核对: 单卡在 working 内完成审核、集成和清理后进入 done；任务组成员 done 后主控再清理组分支。审核关闭仍保留命令协议要求的显式 N/A 计划和闭合门禁。


交付自检（提交 3dc625f9191d670f27da6879762fd638e52cf7c1）：
1. `git diff --check 781aa0dcde99eefe0d7ccea84f5a138f414e831c HEAD` 无输出，通过（3dc625f9191d670f27da6879762fd638e52cf7c1）。
2. `wc -l internal/flow/*.go internal/tui/options_flow*.go internal/tui/options_form.go internal/tui/options_panel.go`：162、188、41、109、723、533 行，均不超过 1000（3dc625f9191d670f27da6879762fd638e52cf7c1）。
3. `git diff 781aa0dcde99eefe0d7ccea84f5a138f414e831c HEAD -- AGENTS.md README.md`：包图、选项说明同批更新；61 个 flow 键三语一致（3dc625f9191d670f27da6879762fd638e52cf7c1）。
4. 检查新增调用链 Build/openFlow/dispatch：均有真实调用，无死代码；未改 menu 配置读写（3dc625f9191d670f27da6879762fd638e52cf7c1）。
5. 新测试分别验证模块裁剪、角色分阶段、顺序与翻译、全 skip/摘要、只读会话、窄宽滚动，无重复实现断言或无关断言（3dc625f9191d670f27da6879762fd638e52cf7c1）。
6. `go test -json ./internal/flow ./internal/tui ./internal/i18n`：78 个测试事件通过、0 跳过、0 失败，3 包通过（3dc625f9191d670f27da6879762fd638e52cf7c1）。60 列下 en/cn/ja 全部行保持有效 UTF-8 且显示宽度不超限；上下翻页及滚轮改变 viewport 偏移（3dc625f9191d670f27da6879762fd638e52cf7c1）。
7. 模块根 `go test -json ./...`：1165 个测试事件通过（含子测试）、1 跳过、0 失败，21 包通过（3dc625f9191d670f27da6879762fd638e52cf7c1）。提交前同命令同计数通过，提交后已复验。

验收自检：11/11 已实现并验证（3dc625f9191d670f27da6879762fd638e52cf7c1）；最终完成仍待 PM/QA 原件与 develop 集成。审核 base 固定为 781aa0dcde99eefe0d7ccea84f5a138f414e831c；PM/QA required，CSA/Hacker 按本仓库 AGENTS.md 标记 N/A。

## SUMMARY

