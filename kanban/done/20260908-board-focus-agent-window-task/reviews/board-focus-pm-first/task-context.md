# 看板列表按 g 跳转到任务卡 Agent 所在窗口

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 10:54
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t1C:wX:p1Y
- STARTED_AT: 2026-09-08 10:55
- FINISHED_AT:
- TASK_BRANCH: board-focus-agent-window
- RESULT:

## GOAL

在 `kander` 终端看板 (internal/tui) 的棋盘视图里增加热键 `g`: 按下后把终端焦点切到当前选中任务卡所记录的执行 Agent 所在的 herdr tab/pane 或 tmux window/pane. 目前卡片虽然记录了 `WINDOW` 地址 (`internal/launch/metadata.go` 的 `locationOf`), 但用户只能自己复制地址再手动切换; 卡多的时候找不到对应窗口. 目标是从看板一键跳到干活的窗口, 并在窗口已关闭或本来就无法跳转时给出明确提示, 而不是静默失败.

## USER_DECISIONS

- 热键使用 `g`, 作用于棋盘 (列表) 视图.
- 目标窗口/标签页已关闭时提示用户无法跳转.
- 详情视图不加该热键 (那里 `g` 已被 `gg` 前缀占用).
- 跳转只使用卡片 `WINDOW` 记录的地址, 不做会话反查发现.

## EXPECTED_OUTCOME

- 在看板棋盘视图选中一张卡按 `g`:
  - 卡片 `WINDOW` 为 herdr 地址 (`herdr:<tab-id>:<pane-id>`) 且 pane 仍存在时, herdr 焦点切到该 tab, 并尽力聚焦到该 pane.
  - 卡片 `WINDOW` 为 tmux 地址 (`tmux|tmux-session:<session>:<window>:<pane>`) 且 pane 仍存在, 且看板自身运行在 tmux 客户端内时, 焦点切到该 session/window/pane.
  - 目标 pane 已不存在 (tab/window 已关闭) 时, 看板页脚给出「窗口已关闭, 无法跳转」类提示, 不做任何切换.
  - 卡片没有 `WINDOW` (未通过 start 启动), 地址是 foreground/console 等不可跳转形态, 所需的 herdr/tmux 命令不在 PATH, 或目标是 tmux 但当前不在 tmux 客户端内时, 各自给出可区分的原因提示.
- 帮助浮层 (`?`) 的棋盘分组列出该热键.
- 面向用户的新文案进 `internal/i18n/locales` 的 en / zh-CN / ja 三份目录.
- `AGENTS.md` 的包图与 README 的按键说明同步更新.

## ACCEPTANCE_CRITERIA

- [ ] 棋盘视图 `g` 触发跳转; 详情视图与搜索输入态的按键行为不受影响 (搜索态下 `g` 仍是输入字符).
- [ ] 地址解析覆盖 `herdr:<tab>:<pane>`, `tmux:<session>:<window>:<pane>`, `tmux-session:<session>:<window>:<pane>` 三种形态, 与 `internal/launch/metadata.go` 的 `locationOf` 写入格式一致; 无法解析或不可跳转的取值走明确的失败原因, 不静默返回成功.
- [ ] 跳转前先用 `internal/probe` 的既有事实采集判定目标 pane 是否存在; 判定为 gone 时只提示不切换.
- [ ] herdr 分支通过 `herdr tab focus <tab-id>` 切标签, pane 级聚焦为尽力而为 (失败只影响提示细节, 不推翻 tab 切换成功的结论).
- [ ] tmux 分支在 `TMUX` 环境变量存在时执行 select-window / select-pane / switch-client; 不在 tmux 客户端内时报告无法跳转.
- [ ] 失败与成功提示都出现在看板页脚 (复用既有的临时提示通道), 不弹新窗口, 不中断 TUI, 不阻塞输入.
- [ ] 新增文案在 en / zh-CN / ja 三份 locale 中键一致, `go test ./internal/i18n` 通过.
- [ ] 新增单元测试覆盖: herdr 成功, tmux 成功, 目标已关闭, 无 WINDOW, 不支持的 launcher, 命令不在 PATH, 不在 tmux 内; 外部命令通过注入的执行器打桩, 不依赖真实 herdr/tmux.
- [ ] `internal/tui` 补 `g` 的按键用例, 断言触发了跳转动作并把结果写进页脚提示.
- [ ] 在模块根运行 `go test ./...` 通过, 记录命令, 提交号与用例数.
- [ ] `AGENTS.md` 包图收录新包 (若采用新包), README 的看板热键说明包含 `g`; 与改动同一 diff.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题: 不修 `WINDOW` 地址写入, notify/resume 的地址回写与反查, 以及 herdr/tmux 探测的既有行为; 本卡只读消费这些既有事实, 改动它们会扩大回归面.
- 加固: 不做跨用户/跨主机的权限或身份校验, 不改 `internal/fs` 的句柄与 reparse 策略; 跳转只在同一主机同一用户的本地终端内发生, 与既有 launch/notify 的信任边界一致.
- 共享契约与文档: 不改卡片 `WINDOW` 字段格式, 不改 `internal/board` schema, 不改发布规则 `rules/*.md`; 本功能是只读消费方. 仅同步 `AGENTS.md` 包图和 README 热键说明这两处与本次行为直接相关的文档.
- 相邻功能: 不新增 `kander focus` 之类子命令, 不做会话反查发现窗口, 不做详情视图热键, 不做鼠标点击跳转; 用户本次只要求列表里的一个热键, 其余待确认后另开卡.

## DISCUSSION

- 现有事实: 棋盘按键表在 `internal/tui/app.go` 的 `handleBoardKey`; 卡片正文经 `App.GetTask` 拿到 (`TaskPayload` 会带上 `Document`), `WINDOW` 用 `board.MetadataFrom(text, board.FieldWindow)` 取; 地址格式见 `internal/launch/metadata.go:158` 的 `locationOf`; 存活事实用 `internal/probe` 的 `ProbeHerdrPane` / `ProbeTmuxPane`; 后台执行走 `App.pendingWork` (`internal/tui/program.go`), 结果由 `applyWork` 消费, 目前 `applyWork` 在选项面板未打开时直接返回 nil, 需要扩展到能消费本功能的结果; 页脚临时提示复用 `CopyNotice` / `CopyNoticeUntil` (`internal/tui/status.go`).
- 建议实现落点: 新增 `internal/focus` 包, 单一职责是把卡片 `WINDOW` 解析成一次终端跳转并返回可判定结果, 依赖 `internal/probe` 与 `internal/config` (文案), 不被 launch/notify 反向依赖, 也不让 `internal/tui` 去 import `internal/launch`. 若执行时发现更贴合现有包边界的落点, 可在不违反 AGENTS.md 包图方向的前提下调整, 并在 IMPLEMENTATION 说明理由.
- herdr 的 `pane focus` CLI 只支持方向移动, 按 ID 精确聚焦要走 socket API 的 `pane.focus` (仓库已有直连 `HERDR_SOCKET_PATH` 的先例 `internal/launch/herdr.go`); 因此 tab 切换是主结果, pane 聚焦尽力而为.
- 真机冒烟注意: 验证时不要抢走用户当前 tab 的焦点, 用执行者自己的 tab/pane 做目标; 自动化测试一律打桩, 不调真实 herdr/tmux.
- SELF_REVIEW: 已按目标, 已确认方案和项目规则复核: 目标与结果一致, 用户确认的四条决定 (热键 g, 关闭时提示, 不做详情视图, 不做反查) 都落到 USER_DECISIONS 与验收条件, 没有把建议写成用户决定 (包边界一项写为建议并允许有理由地调整); 边界四类都给了取舍与理由, 且没有排除达成目标必需的工作; 验收条件可执行可判定, 覆盖成功, 失败与三语文案, 未引入范围外要求. 未发现需要用户新决策的歧义.

## IMPLEMENTATION

- 2026-09-08：工作树 `/home/dualf/works/kander/worktrees/board-focus-agent-window`，目标分支 develop，冻结 base `26edb64fcfb654a96afedc30bbaea27e5e918ec7`。采用 internal/focus，依赖 probe 与 config，tui 单向调用；不修改既有 WINDOW、probe 接口。实现异步动作与三语提示，补单元测试后提交并运行 PM/QA，CSA/Hacker 按仓库规则 N/A。集成授权待最终交付可审核后确认。


- 交付自检（提交 `78c8137fba6d8a6244c2f0eb6ce84a5ccf548851`）：
  1. `git diff --check 26edb64fcfb654a96afedc30bbaea27e5e918ec7 HEAD`：退出 0，无输出；冲突标记人工检查无遗留（`78c8137fba6d8a6244c2f0eb6ce84a5ccf548851`）。
  2. `wc -l internal/focus/*.go internal/tui/{app,focus,focus_test,help_view,options_panel,program}.go`：所有变更代码文件不超过 1000 行，最大 app.go 810 行（`78c8137fba6d8a6244c2f0eb6ce84a5ccf548851`）。
  3. `git diff 26edb64..HEAD -- AGENTS.md README.md internal/tui/program.go`：包图、列表按键与后台工作注释同步；发布规则按契约不变（`78c8137fba6d8a6244c2f0eb6ce84a5ccf548851`）。
  4. `rg -n 'Window|focusSelectedTask|sendPaneFocus|focusResult' internal/focus internal/tui`：新增生产入口由 TUI/执行器实际调用，无死代码（`78c8137fba6d8a6244c2f0eb6ce84a5ccf548851`）。
  5. 人工核对新增测试清单：分别覆盖分支/失败截断、socket 协议与取消、TUI 后台动作与视图隔离；无冗余覆盖（`78c8137fba6d8a6244c2f0eb6ce84a5ccf548851`）。
  6. `go test -json ./...`：20 个包通过；1071 项通过、1 项跳过，含子测试，退出 0；包含 focus/tui/i18n 编译与定向行为测试（`78c8137fba6d8a6244c2f0eb6ce84a5ccf548851`）。
  7. 全量结果以 `/tmp/kander-focus-tests-final.jsonl` 为原始命令输出；以上通过结论均绑定最终交付提交 `78c8137fba6d8a6244c2f0eb6ce84a5ccf548851`。
- 实现结果：herdr 带 workspace 前缀的 ID 与短 ID 均正确解析；窗口已关闭时不切换；tab 成功、pane 失败保留成功并补充详情。tmux 依次 select-window/select-pane/switch-client，缺 TMUX 时明确失败。异步动作限制同一时刻一项，搜索、详情 gg 与选项面板不受结果处理影响。三语键一致；验收自检 11/11 完成（`78c8137fba6d8a6244c2f0eb6ce84a5ccf548851`）。
- 本地 herdr 源码 `/home/dualf/works/herdr/src/app/api/panes.rs` 的 handle_pane_focus 返回 pane_info；socket 用例核对响应类型与目标 pane。外部 herdr/tmux 调用均在自动化中打桩；未执行真实窗口切换冒烟（`78c8137fba6d8a6244c2f0eb6ce84a5ccf548851`）。
- 已正常推送 origin/board-focus-agent-window；PM/QA 待运行，CSA/Hacker 根据仓库 AGENTS.md 标记 N/A。

## SUMMARY

