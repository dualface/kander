# TUI 打开 Options 时重新读取作用域和项目配置

- TYPE: Bug
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-09 15:16
- OWNER: grok
- SESSION: grok c064778a-a2b4-4bb2-a0cf-af36752744fb
- WINDOW: herdr:w2T:tS:w2T:pS
- STARTED_AT: 2026-09-09 15:16
- FINISHED_AT:
- TASK_BRANCH: 20260909-options-reload-config
- RESULT:

## GOAL

TUI 打开 Options 时只在第一次读盘，之后复用内存里的 `App.Session`，运行期间改过的作用域 `config.json` 和项目 `.kander-config.json` 都不会反映到面板。每次打开 Options（面板尚未打开）都重新读取这两份文件，使表单、overlay 提示和有效语言与磁盘一致。

## USER_DECISIONS

- 打开 Options 时作用域配置和项目覆盖（若有）都重新读盘，不只刷新 `config.json`。
- 可编辑 session 仍来自 `LoadScope`，不把 overlay 合并进保存缓冲。
- 项目覆盖要真正读文件内容，不只检查路径是否存在。
- 任一侧读失败走现有 `loadErr`，不继续用内存里的旧 session。
- 不实现 Global/Project 双 tab；那是 `20260909-tui-project-options-task` 的范围。
- 看板实时主题/列数仍等界面项提交后再写回，本次不改实时看板刷新。

## EXPECTED_OUTCOME

在已运行的 TUI 里按 `o` 打开 Options，面板展示的作用域值和 overlay 提示来自这一次读盘。外部改过作用域文件、新增/删除/改过 overlay、或上次放弃未保存编辑后，重开都能看到磁盘现状。保存仍只写作用域文件。

## ACCEPTANCE_CRITERIA

- [ ] `openOptionsAt` 在面板未打开时每次都重新加载，不再因已有 `App.Session` 跳过读盘。
- [ ] 作用域加载走 `config.LoadScope` + `menu.NewSession`，成功则替换 `App.Session`；失败设置 `loadErr`，不保留旧 session。
- [ ] 同一次打开真正读取项目 overlay（`readOverlay` 或等价接口）：文件新增、消失、内容变化都反映到 overlay 提示；有效语言绑定使用刚读到的合并结果。
- [ ] overlay 存在时，表单可编辑值仍是作用域文件的值，不被 overlay 键污染；保存不把 overlay 键写入作用域 `config.json`。
- [ ] 上次打开改了字段但不保存就关闭，再打开回到磁盘值。
- [ ] 临时目录测试覆盖：改作用域后重开、增删改 overlay 后重开、放弃未保存编辑后重开、overlay 与作用域隔离。已有「预先注入 Session 再 `openOptions`」的测试改为走完整加载，或只通过 `openPanel` 测表单内部。
- [ ] 针对变更行为运行 `go test ./internal/tui`；交付前 `go test ./...`。测试使用临时目录，不改写真实 HOME 配置或用户看板。

## THREAT_MODEL

N/A。配置读写继续走现有 `internal/fs` 安全边界，不放宽路径与平台约束。

## OUT_OF_SCOPE

- 既有问题：不处理 Options 以外的 TUI、启动或看板问题；不修复与本次读盘无关的 session/doctor 既有行为。
- 加固：不引入配置文件监视、热重载或新的权限模型；并发写文件仍按现有保存冲突处理。
- 共享契约与文档：不改配置 schema、合并语义或安装作用域；不重写 released rules 里 Options 只写 scope 的描述（那是项目配置卡的范围）。
- 相邻功能：不实现 Global/Project 双 tab、不把 overlay 变成可编辑缓冲、不在打开时把主题/列数立刻应用到看板、不改 CLI `kander config`。

## DISCUSSION

- 单目标、改动集中在 Options 打开路径，采用一张 small 卡。
- 当前 `detectOverlayNotice` 只查路径；本次改为真正读 overlay，读失败必须可见。
- `NewSession` 会再做一次 agent 探测，打开时可能短暂转圈，与第一次打开一致，这是接受的代价。
- SELF_REVIEW: 通过。目标与验收覆盖作用域和 overlay 两次读盘、失败可见、写入隔离和放弃编辑后恢复；双 tab 与实时看板刷新已排除。未把建议写成用户决策。

## IMPLEMENTATION

- 2026-09-09：任务分支 `20260909-options-reload-config`，工作树 `/home/dualf/works/kander/worktrees/20260909-options-reload-config`，基线 `origin/develop` `211c104282f181d5fabe6cc49b03583b0bed269f`。
- 交付提交 `26b42cf434d55c534998c4ea73db36cd69a2d497`：`openOptionsAt` 在面板未打开时每次走 `LoadScope` + `ReadOverlay` + `NewSession`；成功替换 `App.Session` 并用刚读到的合并结果绑定语言；任一侧失败写 `loadErr` 并清空旧 session。可编辑值仍来自作用域文件。已推送到 `origin/20260909-options-reload-config`。

交付自检（提交 `26b42cf434d55c534998c4ea73db36cd69a2d497`）：

1. `git diff --check 211c104282f181d5fabe6cc49b03583b0bed269f HEAD`：干净。
2. 行数：新增 `options_reload_test.go` 219 行；已有文件均未超过 1000 行（`options_test.go` 762→811，`options_panel.go` 550→564，`overlay.go` 214→249）。
3. 注释与文档与实现一致；删除只查路径的 `detectOverlayNotice`。
4. 无死代码：`ReadOverlay` / `ApplyOverlay` / `loadOptionsSession` 均有调用。
5. 无重复测试：重载、overlay 增删改、放弃编辑、隔离、读失败、面板已打开跳过分别覆盖。
6. `go test ./internal/tui -count=1`：ok，105 tests（`26b42cf434d55c534998c4ea73db36cd69a2d497`）。`go test ./internal/config -count=1`：ok，122 tests。
7. `go test ./... -count=1`：21 packages / 1279 tests 通过（`26b42cf434d55c534998c4ea73db36cd69a2d497`）。测试使用临时目录，未改写真实 HOME 或用户看板。

## SUMMARY
