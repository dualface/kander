# 交付报告:增加多套 light/dark 主题

## 实际变更

- 主题定义集中在 `internal/tui/theme.go` 的 `themeTable`。`themePalette` 先经 `resolveTheme` 再查表,直接返回查到的 `palette`;`themeIsDark` 直接返回查到的 `dark` 字段。
- `resolveTheme` 对 6 套具名主题原样返回名字,只对 `auto`/未知名探测终端底色并落到 `light`/`dark`。
- `markdownCanvasStyle` 用 `themeIsDark` 在 Glamour `light`/`dark` 基础样式之间选择,再覆盖该主题自身底色。`renderMarkdown` 传入 `glamour.WithColorProfile(lipgloss.ColorProfile())`,256 色终端上详情正文与看板使用同一降级。
- 6 套具名主题底色:
  - `light`: `#fafafa`(前置卡默认浅色,黄金值未改)
  - `light-warm`: `#f3ead8`(米色纸感)
  - `light-contrast`: `#ffffff`(纯白高对比)
  - `dark`: `#16181d`(前置卡默认深色,黄金值未改)
  - `dark-soft`: `#1c2230`(灰蓝柔和)
  - `dark-contrast`: `#000000`(纯黑高对比)
- `config.TUIThemes` = `auto, light, light-warm, light-contrast, dark, dark-soft, dark-contrast`。
- `t` 键与选项面板下拉覆盖全部 7 个取值;`themeLabel` 与 `zh-CN`/`en`/`ja` 补齐 4 个新键。

## 最终提交

- 任务分支: `tui-theme-presets`
- 基于组分支: `73afc16b8d74fe1d46c6feffe36d8baeeb0d1241`
- 最终交付: `6d882fbbf8e086e695728c02594d0d4899bfae82`
- 本轮修复: `6d882fbbf8e086e695728c02594d0d4899bfae82`(真实 TrueColor 终端走查记录)

## 验证

均在 `6d882fbbf8e086e695728c02594d0d4899bfae82`:

- `git diff --check origin/group/20260909-tui-theme-group`: 干净
- `make fmt-check`: 通过
- `make vet`: 通过
- `go test ./internal/tui -count=1`: 通过
- `go test ./... -count=1`: 通过,757 个 `Test*`

## 人工验证(五处界面 × 6 套主题,以及 Solarized 四组合)

环境:

- 终端: Herdr 0.9.0 交互 pane(`w2T:p14`),`COLORTERM=truecolor`,`TERM=xterm-256color`
- 几何: `terminal session control` 后 140 列 × 40 行(`stty size` = `40 140`)
- 二进制: 任务分支 `kander` `73afc16b8d74`(随后走查记录提交为 `6d882fbbf8e086e695728c02594d0d4899bfae82`)
- 隔离配置: 临时 `KANDER_CONFIG` / `KANBAN_DIR`,一张 backlog 演示卡;不写回用户配置
- 操作: 在真实 TUI 里按 `t` 循环具名主题; `?` 帮助弹窗、`s` 启动对话框(ready 后 `esc` 取消)、`o` 选项面板(`esc` 关闭)、Enter 详情(`q` 返回)
- 取证: `herdr pane read --format ansi --source visible` 保留 `48;2;r;g;b` 真彩;现场 dump 含「看板」「界面」「启动所选任务」等中文
- Solarized 四组合: 仅在该 pane 用 OSC 4 写入 16 色 Solarized 槽,OSC 11/10 设默认底/前景(Light `#fdf6e3`/`#657b83`,Dark `#002b36`/`#839496`),再分别启动 `light`/`dark`
- 目视: 用 DejaVu Sans Mono 把现场 ANSI 栅格成 PNG 后逐帧看底色、chrome、分区与 ASCII/盒线字形。离线字体无 CJK,中文在 PNG 上缺字,这是栅格字体限制,不是 TUI 故障;现场 dump 与 herdr 终端本身含中文

画布中心像素与主题 `Bg` 黄金值一致,主题底约占整帧 90%–93%(其余为 chrome/边框/状态栏)。六套主题互不相同。

| 主题 | 底色 | 看板 | 详情 | 选项面板 | 帮助弹窗 | 启动对话框 |
| --- | --- | --- | --- | --- | --- | --- |
| light | `#fafafa` (250,250,250) | 近白底,紫 chrome `#6b21a8`,青列框,KANDER 状态栏 | 同底,标题/GOAL 等正文可读 | 同底,中央紫边「Kander」面板,版本 `dev-73afc16b8d74` | 同底,中央紫边帮助,Enter/`t`/`o`/`s`/`? q` 可见 | 同底,中央紫边确认框,任务 ID 与 grok/herdr |
| light-warm | `#f3ead8` (243,234,216) | 米色纸感,棕 chrome `#6b4423` | 同底 | 同底,棕/暖弹边 | 同底 | 同底 |
| light-contrast | `#ffffff` (255,255,255) | 纯白底,更深紫 chrome `#3d0066` | 同底,对比更高 | 同底 | 同底 | 同底 |
| dark | `#16181d` (22,24,29) | 炭黑底,青列框,紫 chrome | 同底,浅字 | 同底 | 同底 | 同底 |
| dark-soft | `#1c2230` (28,34,48) | 灰蓝底,chrome `#4a3a69` | 同底 | 同底 | 同底 | 同底 |
| dark-contrast | `#000000` (0,0,0) | 纯黑底,更高对比青框,chrome `#490080` | 同底 | 同底 | 同底 | 同底 |

Solarized Light/Dark × light/dark(看板与详情):

| 终端配色 | 主题 | 画布中心 | Solarized Light 像素 | Solarized Dark 像素 |
| --- | --- | --- | --- | --- |
| Solarized Light | light | `#fafafa` | 0 | 0 |
| Solarized Light | dark | `#16181d` | 0 | 0 |
| Solarized Dark | light | `#fafafa` | 0 | 0 |
| Solarized Dark | dark | `#16181d` | 0 | 0 |

浅主题压在 Solarized Dark 底上仍是近白画布,深主题压在 Solarized Light 底上仍是 `#16181d`,没有跟着 0/15 号色翻转。现场 ANSI 为 `48;2;...` 真彩,不是 `48;5;15`/`4x` 索引。

更短的英文记录见仓库 `internal/tui/testdata/theme-live-walkthrough.md`。

## 偏差与未决

- 无契约偏差。`light`/`dark` 黄金值保持前置卡交付。
- CLI `list`、16 色降级、自定义主题仍按 `OUT_OF_SCOPE` 未做。
- 回滚前必须把配置里的 `tui.theme` 改回 `auto`/`light`/`dark` 之一,否则整份配置校验失败:`loadPrefs` 会把列数/刷新等一并复位,其他 `kander` 命令会直接报错退出。

## 验收结论

实现、256 色 Markdown 降级、不可达回退删除,以及契约要求的真实 TrueColor 终端人工走查(六主题×五界面与 Solarized 四组合)已完成。未决评审项按 run ID 与 disposition 路径引用,不粘贴进卡片正文。


## 2026-09-09 Codex 接管核验

本轮 sync（`f02189336e88d00998914d9951caa0fc`，epoch 5）已重新核对任务工作树、Git、审核和作者原件，未修改源码或新增提交。最终交付仍为 `6d882fbbf8e086e695728c02594d0d4899bfae82`，本地和远端任务分支、组分支一致，尚未进入 develop。

本卡 11 项验收均有证据支撑；人工走查沿用已经 PM-r4/QA-r5 核验的真实终端记录，本轮未重新进行人工走查。QA-r5 无剩余发现；PM-r4 的剩余 PM-01 仅分配给前置 palette 卡。本卡没有待处理的新作者事项；CSA/Hacker 为 N/A。

本轮 `make vet`、`make fmt-check` 通过。`GOFLAGS='-json -count=1' make test` 首次因 liveness 测试的临时 events 文件缺失失败；该用例独立连续 5 次通过，同命令全量复跑通过（最终 SHA 上 756 个顶层 Test 通过、1 个 Windows 平台跳过，21 个包通过）。完整命令、失败原文、自检与 Git 证据见 [接管核验记录](takeover/20260909-codex-sync-verification.md)。首次失败保留为调度敏感测试风险，不视为主题代码缺陷已经证实。

未决事项：
- [组级门禁][pending] 组批次仍待关闭，编排器须接收前置卡最终交付、处理 PM 记录项、集成 develop 后派发 wrap-up。参考 `reviews/tui-theme-pm-r4/assignment.json`、`reviews/plan.json` 及接管核验记录。
- [验证][N/A] liveness 测试出现一次失败，独立与全量复跑未重现；无新 review run，详见接管核验记录。

本轮返回 review 完成 sync 回执。任务分支、工作树及审核原件保留，尚未执行 done 与清理。


## 2026-09-09 最终收尾结论

本卡 11/11 验收有证据支撑，六套具名主题及 auto 已进入 develop；原组级等待项已解除。实际最终交付为 `6d882fbbf8e086e695728c02594d0d4899bfae82`，组批次关闭目标为 `461b8c3516f5864c67912a6293490851adb5d698`，集成提交为 `4dd5a07deafb60dd22b3075db1da72c1cadd32cd`。本轮 fetch 后独立核验上述提交在 develop/origin/develop 中的祖先关系，主工作树与远端同步且干净。

计划 `tui-theme-cycle`、批次 `tui-theme-batch-one` 已关闭：PM PASS（r3/r4，末项文档修复经独立机械核验关闭），QA PASS（r3/r4/r5），CSA/Hacker N/A。历史失败及作者原件完整保留。遵守用户“合回以后不用审核”的决定，本次不启动新审核。

集成执行者在与提交相同的完整 tree `715d32d7511d276e6503ce4eb10ca1ff75fdae34` 上运行全量测试：21 包、842 顶层测试、613 子测试通过，1 Windows 测试跳过；vet、格式、暂存差异检查通过，提交后 PTY 4 顶层及 4 子测试通过。本轮已读取原始日志并复核计数和 tree；未重复运行测试或人工走查。

本卡任务工作树、本地及远端 `tui-theme-presets` 分支已清理。组和临时集成工作状态由编排器清理。完成回执按受控证据使用计划初始 source_commit `1d10371b763cad83ebf0aba295b985151ef71c50`，不将该绑定值误写成本卡实际最终交付。详细操作、审核绑定、日志校验摘要见 [收尾记录](wrap-up/9b53cef420b9ef352c3e72405050e862-6.md)。

当前无未闭合审核发现。保留一个非阻塞历史观察：上轮接管出现 liveness 临时事件文件单次缺失，随后独立 5 次及完整复跑通过，本次集成测试也通过。失败原文与背景仍见 `takeover/20260909-codex-sync-verification.md`，未声称修复了该时序风险。
