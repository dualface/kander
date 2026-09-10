# 实施计划:增加多套 light/dark 主题

本文件记录实施步骤、受影响模块、验证、发布与回滚方案,不修改 `spec.md` 的契约。

## 前置

- 依赖 `20260909-tui-truecolor-palette-task` 已交付到组分支:调色板已是十六进制,且 light/dark 各有独立前景色组。
- 任务分支从最新的 `group/20260909-tui-theme-group` 创建。

## 受影响模块

| 模块 | 变更 |
| --- | --- |
| `internal/tui/theme.go` | 引入命名主题表,`themePalette` 改为查表;新增 4 套主题取值;`resolveTheme` 只做 `auto` 归一并对具名主题原样返回,新增 `themeIsDark(name) bool` |
| `internal/config/config.go` | `TUIThemes` 扩为 7 个取值 |
| `internal/tui/types.go` | `themes`、`themeIndex` 随取值集合变化 |
| `internal/tui/app.go` | `t` 键循环覆盖全部取值(逻辑不变,取值集合变大) |
| `internal/tui/context.go` | 标签映射补 4 个主题 |
| `internal/tui/options_form.go` | 下拉选项与摘要行随取值集合变化(逻辑不变) |
| `internal/tui/detail_view.go` | `markdownCanvasStyle` 改用 `themeIsDark` 选 Glamour 基础样式,再覆盖该主题自身底色 |
| `internal/i18n/locales/*.json` | 三份语言包各补 4 个标签键 |

文档不在受影响模块内:仓库当前没有面向用户的 TUI 主题取值说明,按 `spec.md` 的 `OUT_OF_SCOPE` 本卡不新增。

## 步骤

1. **主题表结构与归类入口**。在 `theme.go` 定义 `themeDef{ name string; dark bool; palette palette }` 的有序表。`themePalette(name)` 先经 `resolveTheme` 归一再查表;`resolveTheme` 保持"只处理 `auto`"的职责,具名主题原样返回;新增 `themeIsDark(name) bool` 查表返回浅/深。移除 `themePalette` 内 `if resolveTheme(name) == "light"` 的字面比较。补 `resolveTheme`/`themeIsDark` 单测。提交点 1。
2. **详情页归类**。`markdownCanvasStyle` 改用 `themeIsDark` 在 `glamstyles.DefaultStyles` 的 `light`/`dark` 两个键之间选取,再覆盖为该主题自身底色;补单测,覆盖 6 套主题各自的底色与基础样式归属。先做这一步,避免第 3 步引入新主题名后详情页静默回落 `DarkStyle`。提交点 2。
3. **4 套新主题取值**。按 `spec.md` 的观感描述给出色值,复用前置卡的语义槽位定义。每套先写逐槽位对比度单测(区分普通/高对比阈值)再填色值,以测试驱动取值。同时写入 `light`/`dark` 的黄金值兼容断言。提交点 3。
4. **配置层贯通**。扩 `TUIThemes`;补校验单测(7 个值通过、未知值被拒并定位到 `tui.theme`),以及 `prefsConfig` 写回回落与 `loadPrefs` 读取回落两条路径的行为断言。提交点 4。
5. **TUI 消费点贯通**。`types.go`/`app.go`/`context.go`/`options_form.go` 逐点确认;补 `t` 键循环单测与选项面板下拉用例;扩展既有 `internal/tui/options_test.go` 的主题用例。提交点 5。
6. **i18n**。三份语言包补 4 个键;补或复用键集合一致性测试。提交点 6。
7. **人工验证**。逐一切换 6 套主题,检查看板、详情页、选项面板、弹窗、启动对话框五处界面,结果记入 `report.md`。

## 验证

- `make test`、`make vet`、`make fmt-check`。
- 自动化重点:`resolveTheme`/`themeIsDark` 语义、逐槽位对比度(6 套 × 三类阈值)、`light`/`dark` 黄金值兼容、配置校验与两条回落路径、`t` 键循环完整性、i18n 键集合一致性、Glamour 底色与基础样式归类。
- 人工重点:五处界面 × 6 套主题的实际观感;以及在 Solarized Light / Solarized Dark 终端(truecolor)下抽查两套主题,确认渲染不受终端配色影响(前置卡的性质在扩展后仍成立)。

## 发布

- 无迁移脚本、无 schema 版本变更。`tui.theme` 的取值集合是向后兼容的放宽,旧配置文件不需要改动即可继续加载。
- 用户升级后无需操作;想用新主题时通过 `t` 键或选项面板切换。

## 回滚

- 变更全部集中在主题表、取值集合与展示层,无数据迁移,回滚即回退提交。
- **回滚不是单向安全的**:若用户在使用新版期间把 4 个新主题名之一写进了配置,回退代码后 `config.TUIThemes` 不再包含该值,`validateTUI` 会让**整份配置**校验失败,后果是:
  - `internal/tui/prefs.go` 的 `loadPrefs` 在 `config.Load` 出错时回落 `defaultPrefs()`,TUI 的列数、最小列宽、刷新间隔、single 与主题**一并**复位为默认值,不只是主题;
  - 其他调用 `config.Load` 的 kander 命令会直接报错退出。
  (仓库没有命令行 `--theme` 参数;`internal/tui/cmd.go` 校验的是从配置读出的 `prefs.Theme`。)
- 因此回滚步骤必须包含:回退代码**之前或同时**,把配置中的 `tui.theme` 改回 `auto`/`light`/`dark` 之一。该注意事项在 `report.md` 中明确记录。
