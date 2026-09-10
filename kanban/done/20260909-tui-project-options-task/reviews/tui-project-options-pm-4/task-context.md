# TUI Options 支持项目配置编辑与继承

- TYPE: Feature
- SIZE: large
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 10:20
- OWNER: cursor
- SESSION: cursor b1678602-fe03-484f-8091-188cfd2c116d
- WINDOW: herdr:w2T:t6:w2T:p6
- STARTED_AT: 2026-09-09 10:24
- FINISHED_AT:
- TASK_BRANCH: tui-project-options
- RESULT:

## GOAL

在 TUI Options 中按安装模式提供配置作用域入口，使用户能够查看继承来源、编辑项目覆盖并恢复继承。

## USER_DECISIONS

- 全局安装显示 Global / Project 两个 tab；项目安装只显示 Project tab。
- Project tab 显示项目路径，始终修改项目根目录的 .kander-config.json，不能改为写 .kander/config.json。
- 全局安装的项目配置继承全局 config.json，未覆盖项显示“全局：值”。项目安装继承 .kander/config.json，未覆盖项显示“默认：值”。
- 修改字段建立项目覆盖并移除继承前缀；恢复继承删除对应覆盖键。
- “未设置”指键不存在；false、空数组及合法空字符串不直接视为继承。显式覆盖即使等于基础值也予以保留。
- 本轮仅建卡，不启动执行。

## EXPECTED_OUTCOME

用户在 Options 内完成全局配置或项目覆盖编辑，清楚看到项目路径与每个字段的继承来源。保存后重新进入界面及配置读取均反映正确有效值，不将继承值整体复制到项目文件。

## ACCEPTANCE_CRITERIA

- [ ] 使用 config.CurrentInstallPaths 的安装模式判定入口选择 tabs；项目目录中运行全局二进制仍显示 Global / Project，项目安装二进制仅显示 Project。
- [ ] Global tab 修改全局配置；Project tab 在两种安装模式下都只将配置编辑写入 .kander-config.json，不因项目安装而修改 .kander/config.json。
- [ ] Project tab 显示实际项目绝对路径与覆盖文件路径。Git 子目录及关联 worktree 解析到主 worktree；覆盖文件不存在时仍显示继承值，首次保存实际覆盖时才创建。
- [ ] 全局安装按“全局：值”、项目安装按“默认：值”展示未覆盖字段；来源按字段键存在性判断，正确处理嵌套字段、false、数组、合法空字符串及等于基础值的显式覆盖。
- [ ] 编辑只建立实际修改字段的覆盖；恢复继承删除相应键并更新有效值，不影响其他字段。继承值随基础配置变更更新，显式覆盖保持不变。
- [ ] 切换 tabs 保留各自未保存编辑；保存目标明确，关闭/取消遵循清晰一致的现有交互。单纯浏览、切 tab、进入模型设置不生成覆盖或意外保存。
- [ ] 保存只写显式覆盖，保留未编辑的合法配置；复用现有合并与校验语义，校验合并后的有效配置。保存失败保留编辑并报告，不能伪报成功；配置写入复用 internal/fs 的平台安全路径。
- [ ] 非 Git 环境保留现有向上查找覆盖文件的读取行为；无现有覆盖时明确显示将使用的目录和文件，读写目标一致。KANDER_CONFIG 生效时展示实际基础来源，避免悄悄改写另一份配置。
- [ ] TUI 继续使用 Bubble Tea / Huh，复用 internal/menu.Session 的配置逻辑；新增用户文案经 internal/i18n，维护中英日界面。更新受影响的配置文档及 released rules 中 Options 仅写 scope 配置的旧描述，README 若修改则三语同步。
- [ ] 使用临时目录验证安装模式、继承/覆盖/恢复、文件创建、保存隔离与失败、worktree 路径和 tabs 交互；运行 go test ./...，并在真实终端验证路径、前缀、切换及窄屏表现。不能改写真实 HOME 配置或用户看板作为测试数据。

## THREAT_MODEL

N/A。不是新增安全功能；配置读写继续遵守现有 internal/fs 安全边界，不放宽路径与平台约束。

## OUT_OF_SCOPE

- 既有问题：不处理与 Options 项目配置编辑无关的 TUI、启动或看板问题。
- 加固：不引入额外权限策略、配置同步服务或新的信任模型；保留现有安全写入约束。
- 共享契约与文档：本功能需要的配置编辑接口、字段来源语义、相关文档和规则更新纳入范围；不重做配置 schema 或安装作用域设计。
- 相邻功能：不新增 CLI 项目配置编辑命令、不迁移安装内容、不增加任意项目切换器、不改写任务卡已冻结的 LANGUAGE。

## DISCUSSION

- 本功能的配置持久化、Session 与 TUI 是同一用户交互的必要组成，采用单张 large 卡，执行时以 plan.md 组织跨模块步骤。
- 安装作用域由当前可执行文件入口决定，而非当前目录是否存在项目配置。现有 config.Load 叠加 overlay，LoadScope 用于独立基础读取；现有 Options 仅提示 overlay，保存链需逐处检查，避免 persistUI 或模型补全把有效值写错目标。
- 非 Git 新建目标和 KANDER_CONFIG 的具体交互属于实施边界处理，并非新增用户决策；不得改变已有读取优先级。
- 执行阶段按项目规则运行 PM、QA；CSA、Hacker 始终 N/A。建卡不代表代码已实现或测试通过。
- SELF_REVIEW: 通过。已核对最终用户决策、写入目标、继承语义、范围与验收；修正项目安装写入目标为 .kander-config.json，并补齐底层默认来源及保存隔离要求。
- CARD_REVIEW: 通过。独立审查者 card_review（独立子代理，仅接收卡片与用户原始需求，未继承建卡会话）核对目标一致性、范围、约束与验收，无阻塞问题；两种安装均写 .kander-config.json，继承与显式覆盖语义一致。
- 2026-09-09：任务分支 `tui-project-options`，基线 `8abdfe2e6430a00e19383f84f23088a8ff743be0`（origin/develop）。实施记录见 plan.md。
- 2026-09-09：Delivery Self-Check 已记入 IMPLEMENTATION，最终 commit `b2f3668049fc472d0dc3e7710b7638c8fca3c00d`。


## IMPLEMENTATION

- 2026-09-09：任务分支 `tui-project-options`，基线 `8abdfe2e6430a00e19383f84f23088a8ff743be0`。交付 `a20afa99042029137d1fd753a70735c81378f01b`（Options 项目覆盖编辑）；PTY 验收 `b2f3668049fc472d0dc3e7710b7638c8fca3c00d`。
- Delivery Self-Check（相对基线，最终代码 commit `b2f3668049fc472d0dc3e7710b7638c8fca3c00d`）：
  1. `git diff --check 8abdfe2e6430a00e19383f84f23088a8ff743be0 HEAD` 干净。`b2f3668049fc472d0dc3e7710b7638c8fca3c00d`
  2. 行数门通过：新增代码文件均 ≤1000；原 ≤1000 的未超过 1000。最大现文件 `internal/tui/options_form.go` 808 行。`b2f3668049fc472d0dc3e7710b7638c8fca3c00d`
  3. 注释/文档与行为一致：`rules/KANDER-AGENTS.md`、`docs/custom-agents.md`、`AGENTS.md` 已改写 Options 只写 scope 的旧描述。`b2f3668049fc472d0dc3e7710b7638c8fca3c00d`
  4. 无新增死代码；overlay 空父对象会剪枝，避免 `{"tui":{}}` 校验失败。`b2f3668049fc472d0dc3e7710b7638c8fca3c00d`
  5. 测试按行为分层：config/menu 覆盖键存在性与保存隔离，TUI 覆盖 tabs/继承/窄屏，PTY 覆盖真实终端路径/前缀/切 tab。`b2f3668049fc472d0dc3e7710b7638c8fca3c00d`
  6. `go test ./internal/config ./internal/menu ./internal/tui -count=1` 在最终 commit 通过。`b2f3668049fc472d0dc3e7710b7638c8fca3c00d`
  7. `go test ./... -count=1 -timeout 180s` 于 `b2f3668049fc472d0dc3e7710b7638c8fca3c00d`：1292 tests pass，0 fail，21 packages pass。
