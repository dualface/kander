# 任务组 20260909-tui-theme-group 本批评审契约
两张卡：Bug `20260909-tui-truecolor-palette-task`（small）与 Feature `20260909-tui-theme-presets-task`（large）。组规模按 large。

---

# 20260909-tui-truecolor-palette-task — TUI 调色板改用具体颜色值
## GOAL

`internal/tui/theme.go` 目前整套调色板使用 ANSI 调色板索引:前景/背景 `"15"`/`"0"`、`Dim "8"`、`Accent "13"`、`Bar "4"`、`ChromeFg "15"`、`ChromeBg "5"`、`PopupEdge "13"`、`Warn "1"`、`OK "2"`,七个列标题色 `headingColors` 用 `"1"`–`"6"`,只有分隔线用 256 色灰阶 `"240"`/`"250"`。

0–15 号色的实际 RGB 完全由终端自身的配色方案决定。在把 15 号定义为米色、0 号定义为深青灰的终端(Solarized 系及各类自定义主题)里,选 `light` 主题渲染出的底色并不是浅色,前景与背景的对比度塌陷,用户看到的不是所选主题。同时 light 与 dark 共用同一组前景色索引,为深底设计的 bright 色系(9–15)压在浅底上发飘;分隔线用的 240/250 是 256 色真灰,与终端可改的 0/15 底色不在同一体系,浅色下容易与文字或底色撞色。

把调色板从"终端调色板索引"改为"具体颜色值",使 `light` 与 `dark` 两套主题在 truecolor 与 256 色终端下都按设计呈现,不再受终端自身配色方案影响。
## USER_DECISIONS

- 用户确认:调色板应当使用具体颜色值,而不是颜色索引。
- 用户确认的计划中,本卡承担"浅色/深色在任意终端渲染正确"这一目标;主题套数的扩展由同组后继卡 `20260909-tui-theme-presets-task` 承担。
- 用户确认的计划中,本卡保留现有 light/dark 两套的观感基调,并按以下锚点取值:深色底 `#16181d`,浅色底 `#fafafa`。
- 用户确认的计划中,`internal/board/list.go` 的 CLI `list` 输出(SGR 索引码 + `COLORFGBG` 亮底探测)不在本次范围内。
## EXPECTED_OUTCOME

- `internal/tui/theme.go` 的 `palette` 全部字段与 `headingColors` 均为 truecolor 十六进制值(`lipgloss.Color("#rrggbb")`),源码中不再出现依赖终端调色板的裸索引色号。
- `light` 与 `dark` 各自拥有独立的前景色组:两套主题不再共用同一组前景索引,每个语义槽位(Base/Dim/Separator/Accent/Bar/ChromeFg/ChromeBg/PopupFg/PopupEdge/Warn/OK 与七个列标题色)在各自底色上都可读。
- 在把 ANSI 0/15 定义为非黑白的终端(如 Solarized Light、Solarized Dark)中运行 `kander tui`,只要终端报告 truecolor 或 256 色 profile,选 `light` 得到浅底深字,选 `dark` 得到深底浅字,底色、前景、列标题、分隔线均按设计呈现,不再随终端配色翻转。
- 终端只支持 16 色时,由 lipgloss 按 `ColorProfile` 自动降级为 ANSI 索引,界面不崩溃且前景与背景仍解析为不同的颜色;此时颜色重新受终端调色板支配,是已知且明确记录的局限(见 `OUT_OF_SCOPE`)。
- `internal/tui/detail_view.go` 的 `markdownCanvasStyle` 取 `themePalette(name).Bg` 作为 Glamour 文档背景,改为十六进制后该路径继续正常工作,详情页背景与看板底色一致。
## ACCEPTANCE_CRITERIA

- [ ] `internal/tui/theme.go` 中 `palette` 的每个字段与 `headingColors` 的每个条目都是 `#rrggbb` 形式的 truecolor 值;`grep` 该文件不再出现形如 `lipgloss.Color("0")`、`"15"`、`"240"` 的索引色号。
- [ ] `themePalette` 对 `light` 与 `dark` 分别返回完整的两套取值,包括各自的前景色与七个列标题色,而不是两套共用一组前景。
- [ ] 深色主题 `Bg` 等于 `#16181d`,浅色主题 `Bg` 等于 `#fafafa`;以单测断言这两个字面值。若实现中发现该锚点无法同时满足下一条的对比度要求,停下来向用户确认后再改,不得自行替换。
- [ ] 逐槽位的对比度断言(WCAG 相对亮度比),两套主题各断言一遍:
  - 参照底色为该主题的 `Bg`:`Base`、`Accent`、`Bar`、`Warn`、`OK`、`PopupFg`、`PopupEdge` 以及 `headingColors` 的七个条目,均不低于 4.5:1(它们都经 `p.ink(...)` 渲染为正文文本)。
  - 参照底色为该主题的 `Bg`:`Dim` 与 `Separator` 不低于 3:1,且各自的对比度严格低于 `Base` 的对比度。
  - `ChromeFg` 参照 `ChromeBg`(二者在 `styleFor` 的 `title`/`footer` 分支自成前景背景对,不落在 `Bg` 上),不低于 4.5:1。
- [ ] 补充单测覆盖降级路径:在 `lipgloss` 报告 256 色与 16 色 profile 时,渲染不 panic 且前景与背景解析为不同的颜色。
- [ ] `internal/tui/background_fill_test.go` 随取值变更同步调整并通过;该测试仍验证"主题与终端底色不一致时整屏被填充"这一原有性质。
- [ ] `make test`、`make vet`、`make fmt-check` 全部通过。
- [ ] 人工验证并在 `IMPLEMENTATION` 中记录:在 Solarized Light 与 Solarized Dark 两种终端配色(truecolor profile)下,分别选 `light` 与 `dark`,四种组合的底色与前景均符合 `EXPECTED_OUTCOME`。
## THREAT_MODEL

N/A
## OUT_OF_SCOPE

- 既有问题:`internal/board/list.go` 的 CLI `list` 输出仍使用 SGR 索引码,并靠 `COLORFGBG` 环境变量判断亮底(多数终端不设该变量,默认按深色处理)。用户已确认本次不改;它是与 TUI 独立的另一个渲染表面,改动它不构成本卡目标的必要条件。
- 既有问题:只支持 16 色的终端下,lipgloss 会把十六进制降级回 ANSI 0–15 索引,原缺陷(终端自定义调色板导致底色翻转)在这类终端里原样复发。本卡只保证降级后不崩溃且前景背景可区分,不做 16 色下的补偿(例如自绘近似色或强制 truecolor 输出)。绕开终端的能力声明超出"改用具体颜色值"这一目标。
- 加固:不引入终端背景色 OSC 11 探测、不改动 `lipgloss.HasDarkBackground` 的 `auto` 探测机制、不引入色彩配置的用户自定义入口。这些都超出"让既有两套主题渲染正确"的目标。
- 共享契约与文档:不改动 `config.TUIThemes` 的取值集合、不改动配置文件格式、不改动 `tui.light`/`tui.dark` 等 i18n 键。仓库当前没有任何面向用户的 TUI 主题取值说明(README 三份与 `docs/` 均无),本卡也不新增 —— 本卡不改变用户可见的可选项,无需文档;后继卡 `20260909-tui-theme-presets-task` 扩展取值集合时再一并决定文档口径。
- 相邻功能:不改动 Glamour 的 `DefaultStyles` 前景配色本身(仅继续沿用现有的按 light/dark 归类 + 覆盖背景色的做法)、不改动 huh 表单主题映射 `internal/tui/options_form.go` 的既有逻辑(它从 `palette` 取值,随本卡自动受益)。改造它们属于独立的观感调整,不是本卡目标所必需。
## DISCUSSION

```text
PREREQUISITES: N/A
```

任务组 `20260909-tui-theme-group` 的第一张卡,后继卡为 `20260909-tui-theme-presets-task`。本组共 2 张卡,依赖链线性,组内依赖顺序 `truecolor-palette -> theme-presets`。

拆卡理由:用户提出两个可独立验收的目标 —— ①浅色/深色在任意终端渲染正确;②可在多套主题间选择。后者依赖前者确立的十六进制调色板结构,依赖链线性,故成组而非拆为两张独立单卡。

关键实现线索:

- `lipgloss` 接受 `#rrggbb` 字符串并按当前 `ColorProfile` 自动降级到 256/16 色,无需自行实现降级映射。
- `Bg` 字段被 `internal/tui/detail_view.go` 以 `string(themePalette(name).Bg)` 传给 Glamour 的 `Document.BackgroundColor`,Glamour 接受十六进制字符串,该路径无需改结构,但需在实现时实际验证详情页渲染。
- `resolveTheme`(`internal/tui/theme.go`)的 `auto` 探测逻辑与"返回 light 或 dark"的语义在本卡不变;它的签名与语义变更由后继卡承担。
- `themePalette` 内 `if resolveTheme(name) == "light"` 的字面比较结构在本卡可以保留;后继卡会把它换成查表。

SELF_REVIEW: 已对照用户确认的计划自查。目标与产出一致,覆盖了用户确认的"改用具体颜色值"以及计划中给出的 `#16181d`/`#fafafa` 锚点;USER_DECISIONS 只记录用户已确认的四条,未把实现建议写成用户决策。边界清晰:CLI `list`、16 色终端局限、主题套数扩展分别排除并说明理由,排除项均不构成达成本卡目标的必要工作。约束可执行:逐槽位对比度阈值与参照底色、降级 profile、既有测试调整、`make test`/`make vet`/`make fmt-check` 均可判定,人工验证项给出了明确的四种终端配色组合。验收条件覆盖目标与产出,未引入越界要求。自查未发现需要用户新决策的歧义。

CARD_REVIEW: 由独立 Agent(未参与建卡会话的 general-purpose 子代理)只读本组两张卡与用户原始需求后出具结论为"需修改(轻度)",提出 5 项:①AC 第 3 条给用户已确认的锚点色留了"实现者可自行替换"的放宽口子;②AC 的对比度要求写作"全部槽位"但只给了部分槽位阈值,且未说明 `ChromeFg`/`ChromeBg` 自成前景背景对、参照底色不是 `Bg`,不可判定;③GOAL 承诺"任意终端"与 16 色降级下缺陷复发的事实冲突;④`OUT_OF_SCOPE` 提到"不更新 README 的主题说明",而仓库中并不存在这样的说明;⑤验证命令口径与后继卡不一致,缺 `make fmt-check`。评审同时核对了卡中引用的全部代码位置属实。五项均已修正:锚点改为单测断言字面值并要求偏离时回问用户;对比度按三类槽位逐条给出阈值与参照底;GOAL 与 `EXPECTED_OUTCOME` 限定为 truecolor/256 色并把 16 色局限写入 `OUT_OF_SCOPE`;文档条款改为据实陈述"仓库当前无此类说明,本卡不新增";补齐 `make fmt-check`。修正后重新自查,未发现新问题。

---

# 20260909-tui-theme-presets-task — 增加多套 light/dark 主题
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
