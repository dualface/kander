# 增加多套 light/dark 主题

- TYPE: Feature
- SIZE: large
- TASK_GROUP: 20260909-tui-theme-group
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 12:10
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:w2T:t1C:w2T:p1C
- STARTED_AT: 2026-09-09 12:50
- FINISHED_AT: 2026-09-09 21:45
- TASK_BRANCH: tui-theme-presets
- RESULT: completed

- DISPATCH_ID: 9b53cef420b9ef352c3e72405050e862

- EXECUTION_EPOCH: 6

## GOAL

`kander tui` 目前只有 `auto`/`light`/`dark` 三个主题取值(`internal/config/config.go` 的 `config.TUIThemes`),浅色与深色各只有一套固定观感,用户无法按自己的终端环境和视觉偏好挑选。

在前置卡确立的十六进制调色板结构之上,把单一的 light/dark 扩成一张命名主题表,提供 3 套浅色与 3 套深色共 6 套具名主题(外加 `auto`),并让配置校验、TUI 内切换、选项面板、i18n 标签全部随之贯通,使用户可以在多套主题间选择。

## USER_DECISIONS

- 用户确认:在调色板改用具体颜色值之后,增加几套 light / dark 主题。
- 用户确认的计划中,本卡提供 3 浅 3 深共 6 套具名主题外加 `auto`,命名为 `light`、`light-warm`、`light-contrast`、`dark`、`dark-soft`、`dark-contrast`。
- 用户确认的计划中,`light` 与 `dark` 沿用原名并保持前置卡确立的默认观感,旧配置值不失效;`auto` 探测后落到这两套默认。
- 用户确认的计划中,`internal/board/list.go` 的 CLI `list` 输出不在本次范围内。
- 用户确认:不为本功能编写用户文档,主题由用户在 TUI 的选项面板中自行探索。

## EXPECTED_OUTCOME

- 主题以一张命名主题表的形式定义,新增主题只需在表中增加条目,不需要改动 `themePalette` 之外的分支逻辑。
- 主题取值集合为 `auto`、`light`、`light-warm`、`light-contrast`、`dark`、`dark-soft`、`dark-contrast`,各套观感为:
  - `light`:前置卡确立的近白底默认浅色主题(底色 `#fafafa`)。
  - `light-warm`:米色纸感底色的低刺激浅色主题。
  - `light-contrast`:纯白底、加大前景对比度的高对比浅色主题。
  - `dark`:前置卡确立的默认深色主题(底色 `#16181d`)。
  - `dark-soft`:灰蓝底、降低前景对比度的柔和深色主题。
  - `dark-contrast`:纯黑底、加大前景对比度的高对比深色主题。
- 浅/深归类与 `auto` 归一分成两个明确的入口,不再靠比较 `resolveTheme` 的字面返回值:
  - `resolveTheme(name) string` 只做 `auto` 归一:`auto` 按 `lipgloss.HasDarkBackground` 探测返回 `dark` 或 `light`,具名主题原样返回自身名字。
  - 新增 `themeIsDark(name) bool`,从主题表读取该主题的浅/深归属。
  - `themePalette` 中原有的 `if resolveTheme(name) == "light"` 字面比较由查表取代;`internal/tui/detail_view.go` 的 `markdownCanvasStyle` 改用 `themeIsDark` 选择 Glamour 基础样式(`glamstyles.DefaultStyles` 只有 `light`/`dark` 两个键,直接以主题名索引会 miss 后静默回落 `DarkStyle`),再覆盖为该主题自身的底色。
- 配置层:`config.TUIThemes` 覆盖上述 7 个值,`validateChoice` 接受它们并拒绝未知值;旧配置中的 `auto`/`light`/`dark` 继续有效且观感不变。
- 未知主题值的既有行为保持不变,并在测试中固定下来:`internal/tui/prefs.go` 的 `prefsConfig`(写回路径)把未知主题落到 `auto`;`loadPrefs`(读取路径)在 `config.Load` 校验失败时整段回落 `defaultPrefs()`,即列数、最小列宽、刷新、single 与主题一并复位,而不只是主题。
- TUI 层:`t` 键按固定顺序在全部主题间循环(`internal/tui/app.go`);选项面板的主题下拉列出全部主题(`internal/tui/options_form.go`);`themeLabel`(`internal/tui/context.go`)对每套主题返回本地化名称。
- i18n:`internal/i18n/locales/{zh-CN,en,ja}.json` 三份语言包各补齐 4 个新增主题的标签键,与既有 `tui.auto`/`tui.light`/`tui.dark` 同一命名风格。

## ACCEPTANCE_CRITERIA

- [ ] 主题定义集中在一张命名表中,`themePalette` 通过查表返回 `palette`;新增一套主题只需增加表条目,无需修改其他文件的分支逻辑。
- [ ] `resolveTheme` 与 `themeIsDark` 按 `EXPECTED_OUTCOME` 的签名与语义实现;单测断言:`resolveTheme` 对 6 套具名主题返回其自身名字,对 `auto` 在探测深底时返回 `dark`、浅底时返回 `light`;`themeIsDark` 对 3 套浅色返回 false、3 套深色返回 true。
- [ ] `config.TUIThemes` 等于 `auto, light, light-warm, light-contrast, dark, dark-soft, dark-contrast`;单测覆盖:7 个值全部通过配置校验,未知值被拒绝并给出 `tui.theme` 定位。
- [ ] 向后兼容以黄金值断言固定:测试中内联写死前置卡交付的 `light` 与 `dark` 两套 `palette` 的全部字段字面值(而非与当时的活动实现相比),断言本卡交付后 `themePalette("light")` 与 `themePalette("dark")` 仍逐字段等于该黄金表。
- [ ] 6 套具名主题各自的全部语义槽位均为 `#rrggbb` 值,并按下列分类逐槽位断言对比度(WCAG 相对亮度比),阈值中的"高对比"指两套 `-contrast` 主题:
  - 参照该主题的 `Bg`:`Base`、`Accent`、`Bar`、`Warn`、`OK`、`PopupFg`、`PopupEdge` 与 `headingColors` 的七个条目,普通主题不低于 4.5:1,高对比主题不低于 7:1。
  - 参照该主题的 `Bg`:`Dim` 与 `Separator` 不低于 3:1,且各自的对比度严格低于 `Base` 的对比度。
  - `ChromeFg` 参照 `ChromeBg`(二者自成前景背景对,不落在 `Bg` 上),普通主题不低于 4.5:1,高对比主题不低于 7:1。
- [ ] `t` 键连续按 7 次回到起点,期间不重复、不遗漏地经过全部 7 个取值;以单测断言。
- [ ] 选项面板主题下拉包含全部 7 个取值,选中任一主题后写回配置并即时生效;既有 `internal/tui/options_test.go` 的主题相关用例随之扩展并通过。
- [ ] `zh-CN`、`en`、`ja` 三份语言包各包含 4 个新增主题的标签键,无缺键;三份语言包的键集合一致(以既有 i18n 一致性测试或新增测试断言)。
- [ ] `markdownCanvasStyle` 对 6 套主题分别返回其自身底色作为 Glamour 文档背景,且基础样式按 `themeIsDark` 归类选取;以单测断言。
- [ ] `make test`、`make vet`、`make fmt-check` 全部通过。
- [ ] 人工验证并在 `report.md` 中记录:逐一切换 6 套主题,看板、详情页、选项面板、弹窗、启动对话框五处界面的底色与前景均符合该主题设计。

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题:`internal/board/list.go` 的 CLI `list` 输出仍使用 SGR 索引码并靠 `COLORFGBG` 判断亮底,不随本卡的主题表变化。用户已确认本次不改;CLI 与 TUI 是两个独立渲染表面,不改它不妨碍本卡目标达成。
- 既有问题:16 色终端下 lipgloss 会把十六进制降级回 ANSI 索引,颜色重新受终端调色板支配。该局限由前置卡记录,本卡不做补偿。
- 加固:不提供用户自定义主题(外部主题文件、配置内嵌色值、运行时导入)、不引入 OSC 11 终端背景色探测、不改动 `auto` 的 `lipgloss.HasDarkBackground` 探测机制。本卡目标是"提供几套可选的内置主题",自定义主题是另一个独立目标。
- 文档:仓库当前没有任何面向用户的 TUI 主题取值说明 —— `README.md`、`README-CN.md`、`README-JA.md` 与 `docs/` 中均无(全仓只有 `AGENTS.md` 一句 "theme colors" 的技术栈描述)。本卡因此不新增 README 主题章节:凭空新增三语文档章节是用户未要求的扩张,而主题的发现入口是 TUI 内的选项面板与 `t` 键,已覆盖可用性。实现中若发现确有涉及主题取值的现存说明,则同步更新该处。
- 共享契约:配置文件 schema 版本不升级(仅扩展 `tui.theme` 的允许取值集合,是向后兼容的放宽,旧值全部继续有效);不改动 `tui` 配置段的其他字段;不改动 `kander config` 的输出格式。这些变更对达成本卡目标既不必要也会扩大兼容面。
- 相邻功能:不改动 Glamour `DefaultStyles` 的前景配色本身、不改动 huh 表单主题映射 `internal/tui/options_form.go` 的结构、不调整看板布局与列宽逻辑。它们从 `palette` 取值,随主题表自动受益,改造其自身属于独立的观感调整。
- 不重做前置卡已交付的 `light`/`dark` 两套默认取值;本卡在其上扩展,不重新设计这两套的观感。

## DISCUSSION

```text
PREREQUISITES: 20260909-tui-truecolor-palette-task
```

任务组 `20260909-tui-theme-group` 的第二张卡。本组共 2 张卡,依赖链线性,组内依赖顺序 `truecolor-palette -> theme-presets`。前置卡把调色板从 ANSI 索引改为十六进制并为 light/dark 各自建立独立的前景色组,本卡在该结构上扩成命名主题表。

拆卡理由:用户提出两个可独立验收的目标 —— ①浅色/深色在任意终端渲染正确;②可在多套主题间选择。本卡承担第二个目标。

需要贯通的消费点(逐一确认过):

- `internal/config/config.go` 的 `TUIThemes` 与 `validateChoice`
- `internal/tui/types.go` 的 `themes`、`themeIndex`
- `internal/tui/app.go` 的 `t` 键循环
- `internal/tui/cmd.go` 的未知主题报错(校验的是从配置读出的 `prefs.Theme`,仓库没有命令行 `--theme` 参数)
- `internal/tui/prefs.go` 的 `prefsConfig` 写回回落与 `loadPrefs` 读取回落
- `internal/tui/context.go` 的标签映射与 `themeLabel`
- `internal/tui/options_form.go` 的摘要行、下拉选项、写回
- `internal/tui/detail_view.go` 的 `markdownCanvasStyle`
- `internal/i18n/locales/{zh-CN,en,ja}.json` 的 `tui.auto`/`tui.light`/`tui.dark` 邻位

SELF_REVIEW: 已对照用户确认的计划自查。目标与产出一致,主题命名、套数、向后兼容三项均来自用户确认的计划,未把实现建议写成用户决策(具体色值留给实现,以对比度与观感描述进入验收)。边界清晰:CLI `list`、16 色局限、自定义主题、文档、前置卡已交付的两套默认取值分别排除并说明理由,排除项不构成达成本卡目标的必要工作;与前置卡的分工无重叠 —— 前置卡只改取值不改取值集合,本卡只扩集合不重设默认两套。约束可执行:主题表结构、`resolveTheme`/`themeIsDark` 契约、配置校验、黄金值兼容断言、逐槽位对比度、i18n 键一致性、`t` 键循环、Glamour 归类均可由单测判定,人工验证项给出了明确的检查对象。验收条件覆盖目标与产出,未引入越界要求。自查未发现需要用户新决策的歧义。

CARD_REVIEW: 由独立 Agent(未参与建卡会话的 general-purpose 子代理)只读本组两张卡与用户原始需求后出具结论为"需修改",提出 6 项,其中 2 项为阻塞级:①(阻塞)`EXPECTED_OUTCOME` 与验收要求更新"三份 README 及 docs 中涉及 TUI 主题取值的说明",但该说明在仓库中并不存在,实现者只能凭空新增文档(越界)或永远无法判定该条;②(阻塞)`resolveTheme` 的返回值语义在两卡交界处未约定,plan.md 的措辞可被解读为让它返回主题名本身,而 `glamstyles.DefaultStyles` 只有 `light`/`dark` 两个键,miss 后静默回落 `DarkStyle`,会让 4 套新主题的详情页用错基础样式且单测未必覆盖;③向后兼容验收"与前置卡交付后的取值逐字段一致"是自指重言,两卡同在一条组分支上,该断言恒真无判定力;④对 `prefs.go` 未知值回落的描述不准确,混淆了写回路径 `prefsConfig` 与读取路径 `loadPrefs`(后者失败时整段回落 `defaultPrefs()`);⑤plan.md 回滚一节两处与代码不符 —— 仓库没有命令行 `--theme` 参数,且回滚后配置残留新主题名会导致整份配置校验失败、其他 kander 命令直接报错退出,后果比原文严重;⑥高对比主题的对比度阈值与卡 1 同样只覆盖了部分槽位。评审同时核对了两卡引用的全部代码位置属实,并确认两卡无重叠、合起来完整覆盖用户请求。六项均已修正:文档条款据实改为"仓库当前无此类说明,本卡不新增,是否补文档留待用户单独决定"并移入 `OUT_OF_SCOPE`;把 `resolveTheme`/`themeIsDark` 的签名与语义写进 `EXPECTED_OUTCOME` 并加验收条;兼容验收改为内联黄金值断言;按实际代码分别写明两条回落路径;plan.md 回滚一节据实重写;对比度按三类槽位逐条给出阈值与参照底,并区分普通/高对比主题。修正后重新自查,未发现新问题。

## IMPLEMENTATION

- 2026-09-09: 初交付 `1d10371b763cad83ebf0aba295b985151ef71c50`；r3 fix 轮 `73afc16b8d74fe1d46c6feffe36d8baeeb0d1241`（QA-01/PM-01/PM-02 已关闭；QA-02 当时以 View() 栅格结案，被 r4 重开）。
- 2026-09-09: QA-r4 QA-02。交付 `6d882fbbf8e086e695728c02594d0d4899bfae82`。
  - 处置计数：fixed 1。记录：`reviews/tui-theme-qa-r4/dispositions/qa02-author-fix2.json`。
  - 在 herdr TrueColor pane 按 `t`/`?`/`s`/`o`/Enter 走完六主题×五界面，并用 OSC 4/11 做 Solarized 四组合；画布中心等于主题 Bg，Solarized 像素为 0。
  - 交付自检(`6d882fbbf8e086e695728c02594d0d4899bfae82`):
    1. `git diff --check origin/group/20260909-tui-theme-group` 干净。
    2. 本轮未新增超 1000 行文件；已改 `theme_test.go` 448 行、`testdata/theme-live-walkthrough.md` 56 行。
    3. 注释与 `report.md` 已同步，不再把 View() 栅格写成交互终端走查。
    4. 无死代码。
    5. 无重复测试。
    6. 最终提交上复跑 `make fmt-check`、`make vet`、`go test ./internal/tui -count=1`、`go test ./... -count=1` 通过。
    7. `go test ./... -count=1` @ `6d882fbbf8e086e695728c02594d0d4899bfae82`，757 个 `Test*`。

- 2026-09-09（Codex 接管 sync `f02189336e88d00998914d9951caa0fc`，epoch 5）：接收 `replayed=false`；重建并核对任务工作树 `/home/dualf/works/kander/worktrees/tui-theme-presets`、交付和审核原件。本轮无源码变更；最终交付仍为 `6d882fbbf8e086e695728c02594d0d4899bfae82`，fetch 后本地/远端任务分支与组分支一致，尚未进入 develop。PM-r4 本卡项及 QA-r5 已闭合，无新增作者处置；PM-r4 剩余项仅分配给前置卡。交付自检七项及完整证据见 `takeover/20260909-codex-sync-verification.md`。`make vet`、`make fmt-check` 通过；`GOFLAGS='-json -count=1' make test` 首次出现 liveness 临时 events 文件缺失，单用例连续 5 次通过，全量复跑在最终 SHA 上通过（756 个顶层 Test 通过、1 个 Windows 平台跳过，21 个包通过）。真实终端人工记录已核对，未重复走查。`kander check` 返回 `ok: 1 tasks`；review progress 为 pending。返回 review 完成本轮 sync，等待编排器关闭组批次、集成及 wrap-up，保留任务工作树和分支。

## SUMMARY

最终收尾（2026-09-09，Codex，wrap-up `9b53cef420b9ef352c3e72405050e862` epoch 6）：本卡 11/11 验收有证据支撑，六套具名主题与 auto 已交付并合入 develop。本卡实际最终交付 `6d882fbbf8e086e695728c02594d0d4899bfae82`；已审组最终目标 `461b8c3516f5864c67912a6293490851adb5d698`；集成提交 `4dd5a07deafb60dd22b3075db1da72c1cadd32cd`。本轮 fetch 后逐一确认它们属于本地/远端 develop 的历史，主工作树与远端同步、工作树干净。

计划 tui-theme-cycle 与批次 tui-theme-batch-one 已关闭：PM PASS（r3/r4，文档末项经独立 mechanical assessment 关闭），QA PASS（r3/r4/r5），CSA/Hacker N/A。原失败记录、作者处置及人工走查引用保留，当前无未闭合审核发现；依用户“合回以后不用审核”的决定，本轮未启动新审核或重做人工走查。

集成执行者在最终提交的完整 tree 上全量测试通过：21 包、842 顶层测试、613 子测试通过，1 Windows 跳过；vet、格式、差异检查及提交后 PTY 4 顶层/4 子测试通过。本轮独立读取原始日志并匹配 tested tree，没有把他人的执行写作本轮重跑。

本卡任务工作树、本地及远端任务分支已清理；共享组/集成工作状态留给编排器。原组级等待项已解除。回执的 --delivery-commit 按证据绑定为计划初始 source_commit `1d10371b763cad83ebf0aba295b985151ef71c50`，实际最终交付和集成提交如上，全部修复均已保留。详细证据：`wrap-up/9b53cef420b9ef352c3e72405050e862-6.md`、`dispatches/9b53cef420b9ef352c3e72405050e862/integration.json`、`reviews/batches/tui-theme-batch-one/closed.json`。

非阻塞历史观察（1）：[验证][N/A] 接管首轮 liveness 临时 events 文件缺失一次，随后独立 5 次及完整复跑均通过，本次集成测试亦通过；保留原文，不宣称修复。无新增 review run，见 `takeover/20260909-codex-sync-verification.md`。

### 历史交付与接管摘要（下述等待状态已由最终收尾解除）

QA-r4 QA-02 已在真实 TrueColor 终端走查后 fixed。任务分支 `tui-theme-presets` `6d882fbbf8e086e695728c02594d0d4899bfae82`。
- QA medium fixed `reviews/tui-theme-qa-r4/dispositions/qa02-author-fix2.json`
- QA medium fixed `reviews/tui-theme-qa-r3/dispositions/qa01-author-fix1.json`
- PM medium fixed `reviews/tui-theme-pm-r3/dispositions/pm01-author-fix1.json`
- PM medium fixed `reviews/tui-theme-pm-r3/dispositions/pm02-author-fix1.json`

当前接管核验：本卡 11 项验收有证据支撑，无待处置的新审核发现；本轮 sync 完成，整卡仍待组级收尾。
- [组级门禁][pending] 编排器接收前置卡最新交付、关闭批次并集成 develop 后派发 wrap-up；`reviews/tui-theme-pm-r4/assignment.json`、`reviews/plan.json`、`takeover/20260909-codex-sync-verification.md`。
- [验证][N/A] liveness 测试首次失败，独立 5 次及全量复跑未重现；无新 review run；`takeover/20260909-codex-sync-verification.md`。

## REVIEWS

- {"run_id":"tui-theme-qa-r1","batch_id":"tui-theme-batch-one","role":"QA","execution_status":"failed","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"1d10371b763cad83ebf0aba295b985151ef71c50","report":"reviews/tui-theme-qa-r1/report.md"}
- {"run_id":"tui-theme-pm-r1","batch_id":"tui-theme-batch-one","role":"PM","execution_status":"failed","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"1d10371b763cad83ebf0aba295b985151ef71c50","report":"reviews/tui-theme-pm-r1/report.md"}
- {"run_id":"tui-theme-qa-r2","batch_id":"tui-theme-batch-one","role":"QA","execution_status":"failed","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"1d10371b763cad83ebf0aba295b985151ef71c50","report":"reviews/tui-theme-qa-r2/output.raw"}
- {"run_id":"tui-theme-pm-r2","batch_id":"tui-theme-batch-one","role":"PM","execution_status":"failed","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"1d10371b763cad83ebf0aba295b985151ef71c50","report":"reviews/tui-theme-pm-r2/output.raw"}
- {"run_id":"tui-theme-qa-r3","batch_id":"tui-theme-batch-one","role":"QA","execution_status":"ok","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"1d10371b763cad83ebf0aba295b985151ef71c50","report":"reviews/tui-theme-qa-r3/report.md"}
- {"run_id":"tui-theme-pm-r3","batch_id":"tui-theme-batch-one","role":"PM","execution_status":"ok","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"1d10371b763cad83ebf0aba295b985151ef71c50","report":"reviews/tui-theme-pm-r3/report.md"}
- {"run_id":"tui-theme-qa-r4","batch_id":"tui-theme-batch-one","role":"QA","execution_status":"ok","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"73afc16b8d74fe1d46c6feffe36d8baeeb0d1241","previous_run_id":"tui-theme-qa-r3","report":"reviews/tui-theme-qa-r4/report.md"}
- {"run_id":"tui-theme-pm-r4","batch_id":"tui-theme-batch-one","role":"PM","execution_status":"ok","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"6d882fbbf8e086e695728c02594d0d4899bfae82","previous_run_id":"tui-theme-pm-r3","report":"reviews/tui-theme-pm-r4/report.md"}
- {"run_id":"tui-theme-qa-r5","batch_id":"tui-theme-batch-one","role":"QA","execution_status":"ok","base":"aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78","commit":"6d882fbbf8e086e695728c02594d0d4899bfae82","previous_run_id":"tui-theme-qa-r4","report":"reviews/tui-theme-qa-r5/report.md"}
