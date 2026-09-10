# Edit and Inherit Project Configuration in TUI Options

- TYPE: Feature
- SIZE: large
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 10:20
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:w2T:t17:w2T:p17
- STARTED_AT: 2026-09-09 10:24
- FINISHED_AT: 2026-09-09 21:06
- TASK_BRANCH: tui-project-options
- RESULT: completed

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
- 2026-09-09：Delivery Self-Check 已记入 IMPLEMENTATION，最终 commit `7c2e345c385be33087c3925ab4be18d11109a353`。
- 2026-09-09：用户授权 grok 接管。codex 完成 PM/QA 两轮增量后因同一 finding 仍打开而停止，等待用户选择是否继续。

## IMPLEMENTATION

- 2026-09-09（实现）：任务分支 `tui-project-options`，基线 `8abdfe2e6430a00e19383f84f23088a8ff743be0`。功能交付至 `7c2e345c385be33087c3925ab4be18d11109a353`。审核修复后 HEAD `35170cacf4ae68bf5f79bb6cf67e638524b37bee`。`go test ./... -count=1 -timeout 240s` 于该提交：21 packages pass。
- 2026-09-09（审核）：pm-1..qa-3 缺围栏失败。codex 首轮 pm-4/qa-4 于 `7c2e345` 通过围栏。两轮修复：`64000ab`..`60bc11a`，再 `3341425`+`35170ca`。增量 pm-5/qa-5、pm-6/qa-6 仍有 must-fix。已达两轮修复上限，未合入 develop。CSA/Hacker N/A。
- 2026-09-09（未关闭项）：QA-001/PM-001 规则表单旧绑定回灌覆盖；QA-004/PM-003 模型覆盖后继承前缀不刷新；QA-006/PM-006 Global TUI 未写入 scopeRaw；QA-007/PM-007 overlay 校验错误被吞。证据见 `reviews/tui-project-options-qa-6/` 与 `reviews/tui-project-options-pm-6/`。

- 2026-09-09（Codex 草稿恢复）：pm-7/qa-7 确认此前四类问题关闭，新增 PM-008/QA-008 同根因问题。独立核实后提交 `162a3d8e7a53066cf96954198d63bb0662919746`，已推送。2 条 fixed 原件见 `reviews/tui-project-options-pm-7/dispositions/pm-008-fix.json` 与 QA 对应路径。最终提交目标包 211 tests / 2 packages 通过；全量验证 1318 pass / 1 Windows-only skip / 21 packages。自检：diff --check 干净、无新增超限文件、注释同步、无死代码与冗余测试；补齐多字段失败草稿、逐项恢复与跨 tab 场景。原有效恢复原子性及保存严格校验保留。

- 2026-09-09（Codex 草稿继承刷新）：pm-8 确认 PM-008/QA-008 关闭，新增 PM-009 已独立核实并修复于 `be375dc63da93461c624da420dbf8be127c64015`，已推送，原件见 `reviews/tui-project-options-pm-8/dispositions/pm-009-fix.json`。草稿显示改为按最新基础文档重建，保留显式覆盖、规则原始缺键语义及失败输入。最终提交目标包 212 tests / 2 packages 通过；全量 1319 pass / 1 Windows-only skip / 21 packages。diff --check、代码行数、注释、引用与测试重复自检通过。用户授权处理全部未关闭项继续适用，批次基线未变。

- 2026-09-09（集成自检）：原审核批次已于 `be375dc63da93461c624da420dbf8be127c64015` 正式关闭，PM-9、QA-8 均无未关闭项。因 `origin/develop` 前进，按集成规则 rebase 到 `98a0a77a61d8ad561566cb344d918f1db0d48637`，逐功能处理 Options 重载及预览基线冲突，最终提交 `cc4349813a01b125c867195317f72db94e1257f3`，已以精确旧 SHA 的 force-with-lease 更新任务分支。补充 Project 语言保留、恢复继承后重建及 tab 切换不回灌覆盖的回归，并修正旧提示测试。Delivery Self-Check：diff --check 无输出；无新增超限文件、死代码或重复回归；注释与行为一致。整仓 `go test ./... -json -count=1 -timeout 240s`：1410 pass、1 Windows 专属 skip、21 packages；最终提交配置/菜单/TUI 复测 427 pass、3 packages。实质代码冲突触发一次 PM/QA 集成补审，结果另行归档。主工作树干净，原有 5 个未推送 develop 提交仍保留，不纳入本任务远端交付。

- 2026-09-09（最终交付）：集成补审三项 medium 已独立复现、修复并经 Codex PM/QA 增量确认全部关闭，详见 `integration-review/`。最终提交 `ead0a736302a273b5ad2489fab6e1d22265b5d24`，已进入 origin/develop；本地同步及 ancestry 校验完成，用户原有 5 个未推送提交补丁完整保留。整仓 1413 pass / 1 Windows-only skip / 21 packages，10/10 验收及自检通过。原计划 closed；任务分支与 worktree 已清理，kander check 退出 0。完整交付、验证、审核、保留证据和无未关闭项结论见 report.md。

## REVIEWS

- {"run_id":"tui-project-options-qa-1","batch_id":"tui-project-options-batch","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"b2f3668049fc472d0dc3e7710b7638c8fca3c00d","report":"reviews/tui-project-options-qa-1/report.md"}
- {"run_id":"tui-project-options-pm-1","batch_id":"tui-project-options-batch","role":"PM","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"b2f3668049fc472d0dc3e7710b7638c8fca3c00d","report":"reviews/tui-project-options-pm-1/report.md"}
- {"run_id":"tui-project-options-pm-2","batch_id":"tui-project-options-batch","role":"PM","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"dc478b74db628bd95efb7bc784350d641b16c44e","report":"reviews/tui-project-options-pm-2/report.md"}
- {"run_id":"tui-project-options-qa-2","batch_id":"tui-project-options-batch","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"dc478b74db628bd95efb7bc784350d641b16c44e","report":"reviews/tui-project-options-qa-2/report.md"}
- {"run_id":"tui-project-options-pm-3","batch_id":"tui-project-options-batch","role":"PM","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"47d574090b21cb0f27c666d12340eec2fb09ae73","report":"reviews/tui-project-options-pm-3/report.md"}
- {"run_id":"tui-project-options-qa-3","batch_id":"tui-project-options-batch","role":"QA","execution_status":"failed","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"47d574090b21cb0f27c666d12340eec2fb09ae73","report":"reviews/tui-project-options-qa-3/report.md"}
- {"run_id":"tui-project-options-pm-4","batch_id":"tui-project-options-batch","role":"PM","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"7c2e345c385be33087c3925ab4be18d11109a353","report":"reviews/tui-project-options-pm-4/report.md"}
- {"run_id":"tui-project-options-qa-4","batch_id":"tui-project-options-batch","role":"QA","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"7c2e345c385be33087c3925ab4be18d11109a353","report":"reviews/tui-project-options-qa-4/report.md"}
- {"run_id":"tui-project-options-qa-5","batch_id":"tui-project-options-batch","role":"QA","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"60bc11ad9b04eb987a6a117ca5ae0b0a33a735fb","previous_run_id":"tui-project-options-qa-4","report":"reviews/tui-project-options-qa-5/report.md"}
- {"run_id":"tui-project-options-pm-5","batch_id":"tui-project-options-batch","role":"PM","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"60bc11ad9b04eb987a6a117ca5ae0b0a33a735fb","previous_run_id":"tui-project-options-pm-4","report":"reviews/tui-project-options-pm-5/report.md"}
- {"run_id":"tui-project-options-qa-6","batch_id":"tui-project-options-batch","role":"QA","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"35170cacf4ae68bf5f79bb6cf67e638524b37bee","previous_run_id":"tui-project-options-qa-5","report":"reviews/tui-project-options-qa-6/report.md"}
- {"run_id":"tui-project-options-pm-6","batch_id":"tui-project-options-batch","role":"PM","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"35170cacf4ae68bf5f79bb6cf67e638524b37bee","previous_run_id":"tui-project-options-pm-5","report":"reviews/tui-project-options-pm-6/report.md"}
- {"run_id":"tui-project-options-qa-7","batch_id":"tui-project-options-batch","role":"QA","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"f4d4eaad922231bc161bfe565feeaa76fd978fa8","previous_run_id":"tui-project-options-qa-6","report":"reviews/tui-project-options-qa-7/report.md"}
- {"run_id":"tui-project-options-pm-7","batch_id":"tui-project-options-batch","role":"PM","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"f4d4eaad922231bc161bfe565feeaa76fd978fa8","previous_run_id":"tui-project-options-pm-6","report":"reviews/tui-project-options-pm-7/report.md"}
- {"run_id":"tui-project-options-pm-8","batch_id":"tui-project-options-batch","role":"PM","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"162a3d8e7a53066cf96954198d63bb0662919746","previous_run_id":"tui-project-options-pm-7","report":"reviews/tui-project-options-pm-8/report.md"}
- {"run_id":"tui-project-options-pm-9","batch_id":"tui-project-options-batch","role":"PM","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"be375dc63da93461c624da420dbf8be127c64015","previous_run_id":"tui-project-options-pm-8","report":"reviews/tui-project-options-pm-9/report.md"}
- {"run_id":"tui-project-options-qa-8","batch_id":"tui-project-options-batch","role":"QA","execution_status":"ok","base":"8abdfe2e6430a00e19383f84f23088a8ff743be0","commit":"be375dc63da93461c624da420dbf8be127c64015","previous_run_id":"tui-project-options-qa-7","report":"reviews/tui-project-options-qa-8/report.md"}

## TAKEOVER_AUDIT

- 2026-09-09（Codex）：用户明确授权继续处理全部未关闭项，覆盖前轮审核轮次暂停。核实 OWNER=codex，工作树 `/home/dualf/works/kander/worktrees/tui-project-options` 干净，HEAD 为 `35170cacf4ae68bf5f79bb6cf67e638524b37bee`；review progress 为 pending。已独立核实 pm-6/qa-6 的四类未关闭问题，保留 Grok 的 confirmed 原件；无前任已完成声明需要撤回。develop 的其他 Options 修复属于独立变更，当前批次基线保持冻结，审核闭环后集成时逐项协调。

- 2026-09-09（Codex 自检与修复）：提交 `f4d4eaad922231bc161bfe565feeaa76fd978fa8` 已推送任务分支。逐项独立验证并修复 pm-6/qa-6 四类问题，8 条继任者 fixed 处置见对应 `reviews/*-6/dispositions/*-codex-fix.json`，原 confirmed 与作者链保留。Delivery Self-Check：`git diff --check` 无输出；代码文件行数检查无新增超限；注释与行为一致；无新增死代码；替换旧的仅断言“不重建”测试，新增连续输入/恢复、规则连续事件、Global TUI 继承、无效编辑保存失败的互补回归。最终提交运行 `go test ./internal/menu ./internal/tui -json -count=1 -timeout 240s`：210 tests pass / 2 packages。提交前相同生产代码执行 `go test ./... -json -count=1 -timeout 240s`：1316 pass / 1 skip / 21 packages，之后补充的 1 项界面错误测试在提交后包测试中通过。PTY 验证含 Project 路径、基础文件、Global/Project 切换、继承前缀及 48 列缩放后帧；均使用临时配置和看板。已补齐前轮漏检的表单绑定与失败保存自检。审核基线不变；本轮只运行 PM/QA 增量，CSA/Hacker N/A。
