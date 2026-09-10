# Read-only TUI board

- 类型: Feature
- SIZE: small
- 任务组: 20260904-migrate-to-go-group
- 创建时间: 2026-09-04 02:19
- 负责人: cursor
- 会话: cursor aa53d270-7f97-445f-bee5-6ba93081f4c4
- 窗口: herdr:wG:t10:wG:p27
- 开始时间: 2026-09-04 11:25
- 完成时间: 2026-09-04 15:08
- 任务分支: 20260904-kander-tui-task
- 结果: completed

## 任务目标

实现 `internal/tui` 与 `kander tui`, 对标 `kanban_tui.py` 的交互契约. 用 Go TUI 库 (tcell 或 bubbletea) 实现, 使 Windows 不再依赖 Python curses; 键位、栏宽、主题、搜索、详情与剪贴板行为与 onevoke 对齐.

## 用户决策

复用 board 的 scan/排序/搜索字段, 不经 `load_board` 注入 check 警告. 默认每栏最小 40 列, `-`/`=` 调节并写入当前作用域配置目录 `tui.json` (测试用 `KANDER_CONFIG` 同目录). `--single` `--refresh` `--theme` 语义不变. 要求 stdin/stdout 都是 TTY.

## 预期成果

终端可多栏浏览任务、打开详情、搜索与复制任务 ID; 默认 30s 原位刷新并尽量保留选中项与滚动位置.

## 验收条件

- [ ] 键位与 onevoke 一致: hjkl/方向键, `/` 搜索, `y` 复制 ID, Enter 详情, `a` 存档栏, `t` 主题, `r` 刷新, `q` 退出; 详情内 vim 翻页、`v`/`V` 选择、`y` 复制.
- [ ] 鼠标: 单击聚焦, 双击详情, 拖选复制, 滚轮翻卡/滚正文.
- [ ] 宽度不足时少显示栏目或按实际宽度单栏并保持选中栏可见.
- [ ] 伪终端测试覆盖启动与退出, 对标 `tests/test-kanban.py` 的 TUI 启动退出; 不要求在 CI 无 TTY 环境跑完整交互录制, 但核心布局与刷新保留选中的逻辑须有单测.

## 威胁模型

只读. 剪贴板写入仅为用户选中的任务 ID 或正文, 不读卡片外文件.

## 不在本轮范围

- 既有问题: 排除 web UI (web 卡). 排除在 TUI 内创建或迁移卡片.
- 并发/跨平台/安全加固: 纳入 TTY 检测; Windows 作为本阶段目标之一, 不再把 TUI 标成不保证, 但若所选库在某 Windows 终端完全不可用, 须失败并提示改用 `kander web`, 不得崩溃.
- 共享契约与文档: 交互契约以 `KANBAN-RULES.md` tui 条款为准; 可将「Windows 不保证 curses」改为「Go TUI, 不可用时用 web」, 仅此一句实现相关修正.
- 相邻功能: 排除 start/notify. scan 字段必须与 web 卡兼容, 不得私自改 payload 形状而不改 web.

## 讨论与决策

```text
前置任务: 20260904-kander-board-task
```

- 与 web 卡无文件冲突: 本卡只写 `internal/tui`. 若 web 尚未合并, scan API 仍以 board 卡为准.
- 组集成分支 `group/20260904-migrate-to-go-group`.

## 实施与验证

- 任务 worktree: `/home/dualf/works/kander/worktrees/20260904-kander-tui-task`, 分支 `20260904-kander-tui-task`, 基于当时组分支并在汇入前 rebase 到 `group/20260904-migrate-to-go-group`.
- 实现 `internal/tui` 与 `cmd/kander/tui.go` 空白导入接线; 用 tcell 渲染, 看板扫描走 `board.Scan` 不经 `LoadBoard`. 栏宽写入当前作用域配置目录 `tui.json` (`KANDER_CONFIG` 同目录). Windows TUI 规则改为 Go 实现, 初始化失败提示 `kander web`.
- 提交: `47291317d7bf421d39b4f4acb99c2ed0c38f680b` (实现只读终端看板 kander tui).
- 无 origin, 未 push 任务分支; 已在组 worktree `git merge --ff-only` 进入 `group/20260904-migrate-to-go-group`.
- 验证: rebase 后于任务 worktree `go test ./...` 通过 (`internal/tui` 覆盖键位/搜索/栏宽/布局/刷新保选中, Linux PTY 启动后 `q` 退出).

### 组级审核批次 12 派回 (commit `4729131`)

核实结论均为**成立**, 已在同一修复提交处理并 ff 进组分支 `506d5b67141897900b113dd75367396fa90b0d24` (subject: 修复 TUI 鼠标单击与详情光标). rebase 后 `go test ./...` 通过. 无 origin, 未 push.

- QA-1 / PM-1 high — tcell 单击/双击未到达 `handleBoardClick`: 成立. `mapMouse` 把每次 `Buttons()==0` 标成 Released|Clicked, 与 curses 的 `clicked==0` 才走点击冲突. 已改为 `mouseTracker`: Button1 为按下/拖动; 按下后 ButtonNone 为 Released (不置 Clicked); 无位移视为单击; 同格 ~500ms 二次释放为双击; 无按下的运动为 `mouseReportPos`; 不再把 Ctrl 当双击. `EnableMouse(MouseButtonEvents, MouseDragEvents)`. 单测经 tracker 覆盖 press/release、hover、双击、非 Ctrl 双击.
- QA-2 / PM-2 — 详情光标不绘制: 成立. `Render` 仅在搜索时 `ShowCursor`. 详情打开且非搜索时显示光标, `DetailCursor` 经 `charIndexToDisplayColumn` 定位, 并以 `caret` 反色绘制. 单测 `TestDetailCursorPainted`.
- QA-3 medium — 详情刷新忽略 State/Assignee 等: 成立. `refreshOpenDetail` 比较并始终 `a.Detail = &next`. 单测 `TestRefreshOpenDetailUpdatesState`.
- QA-4 medium — prefs 绕过 `internal/fs`: 成立. 读改 `ReadRegularFileIfExists`, 目录改 `EnsureDirectoryPath`, 去掉 `json.Number`.
- QA-5 / PM-3 UnknownTheme — 字段从未读取: 成立. `Run` 未知主题走 `ctx.UnknownTheme`. 单测 `TestUnknownThemeUsesContext`.
- QA-6 medium — `json.Number` 不可达: 成立. 已删除该分支 (同 QA-4).
- PM-3 HasSeparator / `Glyphs["vbar"]` — 成立. 多栏在 `x+column_width` 绘制 `vbar`. 单测 `TestRenderDrawsColumnSeparators`.
- PM-4 caret 参数与 `mouseReportPos` 死路径 — 成立. 去掉 hit 的 `caret` 参数; hover 映射 `mouseReportPos`; 保留 `displayColumnToCaretIndex` 单测.

未处理项: 无.

## 完成总结

交付: `kander tui` 可多栏浏览、详情、搜索、复制任务 ID; 默认 30s 原位刷新并尽量保留选中与滚动.
验收: 4/4 自检通过 (hjkl/搜索/y/Enter/a/t/r/q 与详情 vim/`v`/`V`/`y`; 鼠标单击/双击/拖选/滚轮; 宽度不足少栏或单栏且选中栏可见; PTY 启动退出及布局/刷新单测).
验证: `go test ./...` 通过. 未在无 TTY 的 CI 做完整交互录制 (验收允许).
审核: 组级批次 12 首轮 + 增量; PM/QA 通过; CSA/Hacker N/A (仓库 AGENTS.md). 首轮 finding 均核实成立并修复于 `506d5b67141897900b113dd75367396fa90b0d24`. 增量留下未处理项: PM-5/QA-7 TUI payload 与 `board.BoardPayload` 分叉; QA-8 `BindEffectiveLanguage` 未调用; QA-9 字符选择跨度 `end[1]++` (NON-BLOCKING).
收尾: 本卡最终 commit `506d5b67141897900b113dd75367396fa90b0d24`; 组分支 `group/20260904-migrate-to-go-group` 已 ff 合回本地 `develop` `bfa6a7f9d8f3e4ca400b4933ea2970fa15c18a5e`; 无 origin 未 push. 清理前置 `merge-base --is-ancestor` 成立. `go run ./cmd/kander merge-memory --source` 空操作退出 0 (源无 `.memsearch/memory`). 已删本卡 worktree 与本地任务分支 `20260904-kander-tui-task`; 未删组分支.
