# 按 s 立即弹出启动对话框, 后台读取卡片后更新

- TYPE: Feature
- SIZE: small
- TASK_GROUP:
- LANGUAGE: zh-CN
- CREATED_AT: 2026-09-08 13:09
- OWNER: codex
- SESSION: codex
- WINDOW: herdr:wX:t1J:wX:p24
- STARTED_AT: 2026-09-08 13:16
- FINISHED_AT:
- TASK_BRANCH: board-start-dialog-async
- RESULT:

## GOAL

去掉看板里按 `s` 之后到确认对话框出现之间的卡顿, 并把对话框排版对齐选项面板.

现状: `confirmSelectedStart` (`internal/tui/start.go:56-80`) 在按键处理里同步调用 `PrepareStart` -> `launch.PreviewStart` -> `board.ReadSnapshot`, 读盘返回后才把 `a.StartConfirmation` 置上, 下一帧才画对话框. 这段同步读盘发生在 Bubble Tea 的 Update 循环里, 期间整个 TUI 不响应输入; 在当前看板上实测约 1 秒. 对话框正文 (`renderStartConfirmation`, `internal/tui/start.go:131-144`) 是几行紧挨着的文本, 没有段落留白, 与选项面板的弹窗观感不一致.

## USER_DECISIONS

- 2026-09-08 补充：启动操作保持异步；确认后保留对话框并提示正在启动，成功或失败均在对话框更新结果，结果出现后用户按任意键关闭。启动中按键不关闭、不重复启动。

- 按 `s` 立即弹出对话框, 不等读取.
- 读取放到后台, 并且只读选中的那张卡, 不读其它任何卡.
- 读取完成后更新对话框内容.
- 对话框样式参考选项面板 (options), 要有适当留白的空行.

## EXPECTED_OUTCOME

- 按 `s` 的当帧就弹出对话框, 处于「读取中」态: 显示任务 ID 与状态判断结果, Agent 与 launcher 位置为占位文案, 底部提示行说明正在读取. 按键路径上不发生任何读盘.
- 读取在后台进行 (走既有的 `pendingWork` 通道), 期间看板照常响应按键, 滚动与定时刷新.
- 后台只读选中卡: 沿用 targeted 的 `board.ReadSnapshot(root, id)`, 不读其它卡片正文.
- 读取完成后原地更新对话框: 填入 Agent, launcher, backlog 迁移提示, 底部提示行换成确认/取消说明.
- 读取中按确认键不启动任务, 只给出仍在读取的提示; 取消键 (Esc 或其它键) 随时可关闭对话框.
- 对话框关闭后, 或用户已经换选另一张卡后, 迟到的读取结果被丢弃, 不覆盖当前界面, 不误启动.
- 读取失败, 或结果表明该卡不可启动 (状态已变, launcher 解析为 foreground/console) 时, 关闭对话框并把原因显示在页脚, 与改动前的判定结果一致.
- 对话框排版对齐选项面板: 同一套弹窗边框与配色, 标题在框上, 正文按段落用空行分隔 (任务 ID / Agent 与 launcher / backlog 提示), 底部空一行后是 dim 样式的操作提示行; 窄终端下先压缩底部空行, 再压缩正文, 不做横向截断.
- 确认后对话框进入正在启动态；后台启动完成后进入结果态，成功或失败原因及警告都在框内显示，结果态按任意键关闭。启动中按键不关闭、不重复启动。
- 启动本身的行为不变: 仍走 `launch.Start`, 认领, 回滚与结果提示语义与改动前一致.

## ACCEPTANCE_CRITERIA

- [ ] 按 `s` 后, 处理该按键的同一次 Update 内对话框已置为读取中态, 且该路径不调用任何读盘接口 (用注入或计数断言证明).
- [ ] 读取通过后台通道执行, 期间 TUI 可继续处理按键与刷新.
- [ ] 后台读取只读目标卡: 有用例证明未读取其它卡片正文.
- [ ] 读取结果到达后对话框内容被更新为 Agent, launcher 与 backlog 提示, 底部提示行同步切换.
- [ ] 读取中按确认键不启动任务且给出提示; 取消键可关闭对话框, 取消路径零副作用.
- [ ] 对话框已关闭时, 迟到结果被丢弃; 用户换选另一张卡并再次按 `s` 时, 旧卡结果不覆盖新卡对话框 (按任务 ID 加请求序号识别).
- [ ] 读取失败与不可启动 (状态已变, foreground/console) 时关闭对话框并在页脚显示原因; 这些判定的结果与改动前一致.
- [ ] 渲染用例断言: 标题, 段落之间的空行, 底部提示行的 dim 样式; 窄终端下的降级顺序为先压底部空行再压正文.
- [ ] `launch.Start` 的调用参数, 认领, 回滚与成功/失败提示语义未改变, 有用例证明未回归.
- [ ] 确认后异步启动且对话框保留并提示正在启动；启动中按键不关闭、不重复启动；成功或失败及警告原地显示，结果态任意键关闭，覆盖相应用例。
- [ ] 新增文案在 en / zh-CN / ja 三份 locale 中键一致, `go test ./internal/i18n` 通过.
- [ ] 在模块根运行 `go test ./...` 通过, 记录命令, 提交号与用例数.

## THREAT_MODEL

N/A

## OUT_OF_SCOPE

- 既有问题: 不改 `launch.Start` / `launch.PreviewStart` 的语义与返回内容, 不改 backlog -> todo 的受控迁移, 不改启动失败的回滚; 本卡改触发时机、并发方式、排版与用户补充的启动进度/结果对话框。
- 加固: 不加启动的额外授权或并发互斥; 认领互斥由既有移动原语保证.
- 共享契约与文档: 不改卡片格式, 配置 schema 与 `rules/*.md`; 仅在 README / 帮助文案确有描述变化时同步, 不顺手重写其它界面文案.
- 相邻功能: 不做 `.kander/operations` 日志读取的优化 (那是 `20260908-journal-pending-partition-task` 的范围), 不把定时刷新也改成异步, 不做其它热键的弹窗改造; 本卡只覆盖按 `s` 的这条路径.

## DISCUSSION

- 现有事实与行号: 按键分发 `internal/tui/app.go:574` (`case "s"`); 同步预览与判定 `internal/tui/start.go:56-80`; 确认处理 `internal/tui/start.go:83-92`; 结果消费 `internal/tui/start.go:94-127`; 当前渲染 `internal/tui/start.go:131-154`; 后台任务通道 `internal/tui/program.go:143-160` 与 `applyWork`; 选项面板的排版参照 `internal/tui/options_view.go:140-190` (正文与提示行之间固定留一空行, 空间不足时先去掉该空行).
- 读盘慢的根因不在本卡: `board.ReadSnapshot` 本身就是 targeted, 慢的是每次读盘都解析 `.kander/operations` 下的全部已提交记录, 由 `20260908-journal-pending-partition-task` 处理. 本卡把这段耗时移出按键路径, 因此在根因卡合入前, 对话框上的「读取中」仍会停留约 1 秒, 这是预期行为.
- 依赖与顺序: 用户已决定本卡在根因卡合入 develop 之后再启动; 启动前先同步 develop.
- 读取中不接受确认是刻意的: Agent 与 launcher 未知, 状态与 launcher 的可启动性也还没校验, 此时启动会绕过既有判定.
- 读取中态是否加动画: 建议先用静态占位文案, 不引入新的 tick/spinner 管线; 若执行时发现选项面板的 spinner 可以低成本复用, 可以改用 spinner 并在 IMPLEMENTATION 说明.
- SELF_REVIEW: 已按目标, 已确认方案和项目规则复核: 目标与结果一致, 用户确认的四条 (立即弹框, 后台只读目标卡, 完成后更新, 对齐 options 样式并留白) 全部落到 USER_DECISIONS 与验收条件; 迟到结果丢弃, 读取中不接受确认, 静态占位这三条是本次分析给出的方案细节, 写在 EXPECTED_OUTCOME 与 DISCUSSION, 未冒充用户决定, 其中占位形式明确允许有理由地调整. 边界四类均给出取舍与理由, 未排除达成目标必需的工作; 日志读取优化明确划给根因卡, 不在本卡内重复. 验收条件可执行可判定, 覆盖时序, 并发, 迟到结果, 失败路径, 排版与三语文案. 未发现需要用户新决策的歧义.

## IMPLEMENTATION

2026-09-08：已核对 origin/develop 包含根因修复 c2e7118 与 1e4886a；主 worktree 干净。独立 worktree：/home/dualf/works/kander/worktrees/board-start-dialog-async；集成目标 develop；base 1e4886a30d2d047175bebb18f71947795f42304d。复用 pendingWork 异步预览，以任务 ID 与递增请求序号丢弃过期结果；采用静态读取文案，复用 options 的标题边框与底部留白布局。用户追加的启动进度与结果保留已实现，移除旧的退出排队状态及对应两项旧测试，由四态对话框测试覆盖启动期间不能退出、重复启动及完成后仅关闭弹窗；launch.Start 与 runTaskStart 认领/回滚代码未变。


交付自检（最终提交 e0f75e3f30c9d92442fdae091cdd56b243a93a2a）：
1. `git diff --check 1e4886a30d2d047175bebb18f71947795f42304d HEAD`：退出 0，无输出；提交 e0f75e3f30c9d92442fdae091cdd56b243a93a2a。
2. `git diff --name-only <base> HEAD` 配合物理行数统计：8 个 Go 文件均不超过 1000 行，最大 app.go 834 行；提交 e0f75e3f30c9d92442fdae091cdd56b243a93a2a。
3. 逐项核对 README 与 AGENTS.md：已同步异步读取、启动中保留、结果态任意键关闭；三份 locale 同步新增键并移除旧退出等待键；提交 e0f75e3f30c9d92442fdae091cdd56b243a93a2a。
4. `rg -n 'startsRunning|quitAfterStarts|start_wait_exit' internal README.md AGENTS.md` 无匹配；新增状态及结果类型均有生产调用与消费，无新增死代码；提交 e0f75e3f30c9d92442fdae091cdd56b243a93a2a。
5. 测试清单核对：旧退出排队用例随用户新交互被替换；既有 launcher 参数、成功/失败地址与警告、backlog 门禁及失败保留 todo 用例继续保留；新增用例覆盖零同步 I/O、迟到结果、四态生命周期、排版与文件打开观测，无重复目标断言；提交 e0f75e3f30c9d92442fdae091cdd56b243a93a2a。
6. 提交前 `go test ./...` 退出 0；提交后 `go test -json ./...` 退出 0，21 包通过、1230 个测试/子测试通过、1 项平台跳过。含 TUI/locale、launch 认领回滚与既有 Linux PTY 冒烟；提交 e0f75e3f30c9d92442fdae091cdd56b243a93a2a。
7. `go test -race -json ./internal/tui -run 'TestStart|TestBacklog|TestPrepareTaskStart' -count=1`：退出 0，25 个测试/子测试通过，无竞态；提交 e0f75e3f30c9d92442fdae091cdd56b243a93a2a。
8. `TestPrepareTaskStartOpensOnlySelectedCard` 使用 Linux inotify：观测到目标 spec.md 打开，未观测到其它卡正文打开；提交 e0f75e3f30c9d92442fdae091cdd56b243a93a2a。Windows 未做原生运行，平台共享逻辑由测试覆盖，不宣称 Windows 原生验证。

审核计划：独立单批 PM/QA required；CSA/Hacker N/A，依据本仓库 AGENTS.md。所有审核原件只按 run ID 引用。任务分支已正常推送；待审核、闭批、develop 集成与清理。

## SUMMARY

## CONTRACT_DECISIONS

```json
{
  "at": "2026-09-08 13:19",
  "decision": "用户本会话 2026-09-08 明确补充：另外启动任务卡的操作应该是异步的. 但 kanban 界面对话框要保留, 并提示正在启动. 不管 agent 启动成功失败, 对话框里都要更新结果. 用户按任意键才关闭对话框. 据此扩充启动进度与结果态验收；不改变 launch.Start 认领、回滚协议。",
  "Before": {
    "ACCEPTANCE_CRITERIA": "- [ ] 按 `s` 后, 处理该按键的同一次 Update 内对话框已置为读取中态, 且该路径不调用任何读盘接口 (用注入或计数断言证明).\n- [ ] 读取通过后台通道执行, 期间 TUI 可继续处理按键与刷新.\n- [ ] 后台读取只读目标卡: 有用例证明未读取其它卡片正文.\n- [ ] 读取结果到达后对话框内容被更新为 Agent, launcher 与 backlog 提示, 底部提示行同步切换.\n- [ ] 读取中按确认键不启动任务且给出提示; 取消键可关闭对话框, 取消路径零副作用.\n- [ ] 对话框已关闭时, 迟到结果被丢弃; 用户换选另一张卡并再次按 `s` 时, 旧卡结果不覆盖新卡对话框 (按任务 ID 加请求序号识别).\n- [ ] 读取失败与不可启动 (状态已变, foreground/console) 时关闭对话框并在页脚显示原因; 这些判定的结果与改动前一致.\n- [ ] 渲染用例断言: 标题, 段落之间的空行, 底部提示行的 dim 样式; 窄终端下的降级顺序为先压底部空行再压正文.\n- [ ] `launch.Start` 的调用参数, 认领, 回滚与成功/失败提示语义未改变, 有用例证明未回归.\n- [ ] 新增文案在 en / zh-CN / ja 三份 locale 中键一致, `go test ./internal/i18n` 通过.\n- [ ] 在模块根运行 `go test ./...` 通过, 记录命令, 提交号与用例数.",
    "EXPECTED_OUTCOME": "- 按 `s` 的当帧就弹出对话框, 处于「读取中」态: 显示任务 ID 与状态判断结果, Agent 与 launcher 位置为占位文案, 底部提示行说明正在读取. 按键路径上不发生任何读盘.\n- 读取在后台进行 (走既有的 `pendingWork` 通道), 期间看板照常响应按键, 滚动与定时刷新.\n- 后台只读选中卡: 沿用 targeted 的 `board.ReadSnapshot(root, id)`, 不读其它卡片正文.\n- 读取完成后原地更新对话框: 填入 Agent, launcher, backlog 迁移提示, 底部提示行换成确认/取消说明.\n- 读取中按确认键不启动任务, 只给出仍在读取的提示; 取消键 (Esc 或其它键) 随时可关闭对话框.\n- 对话框关闭后, 或用户已经换选另一张卡后, 迟到的读取结果被丢弃, 不覆盖当前界面, 不误启动.\n- 读取失败, 或结果表明该卡不可启动 (状态已变, launcher 解析为 foreground/console) 时, 关闭对话框并把原因显示在页脚, 与改动前的判定结果一致.\n- 对话框排版对齐选项面板: 同一套弹窗边框与配色, 标题在框上, 正文按段落用空行分隔 (任务 ID / Agent 与 launcher / backlog 提示), 底部空一行后是 dim 样式的操作提示行; 窄终端下先压缩底部空行, 再压缩正文, 不做横向截断.\n- 启动本身的行为不变: 仍走 `launch.Start`, 认领, 回滚与结果提示语义与改动前一致.",
    "GOAL": "去掉看板里按 `s` 之后到确认对话框出现之间的卡顿, 并把对话框排版对齐选项面板.\n\n现状: `confirmSelectedStart` (`internal/tui/start.go:56-80`) 在按键处理里同步调用 `PrepareStart` -\u003e `launch.PreviewStart` -\u003e `board.ReadSnapshot`, 读盘返回后才把 `a.StartConfirmation` 置上, 下一帧才画对话框. 这段同步读盘发生在 Bubble Tea 的 Update 循环里, 期间整个 TUI 不响应输入; 在当前看板上实测约 1 秒. 对话框正文 (`renderStartConfirmation`, `internal/tui/start.go:131-144`) 是几行紧挨着的文本, 没有段落留白, 与选项面板的弹窗观感不一致.",
    "OUT_OF_SCOPE": "- 既有问题: 不改 `launch.Start` / `launch.PreviewStart` 的语义与返回内容, 不改 backlog -\u003e todo 的受控迁移, 不改启动失败的回滚; 本卡只改触发时机, 并发方式与排版.\n- 加固: 不加启动的额外授权或并发互斥; 认领互斥由既有移动原语保证.\n- 共享契约与文档: 不改卡片格式, 配置 schema 与 `rules/*.md`; 仅在 README / 帮助文案确有描述变化时同步, 不顺手重写其它界面文案.\n- 相邻功能: 不做 `.kander/operations` 日志读取的优化 (那是 `20260908-journal-pending-partition-task` 的范围), 不把定时刷新也改成异步, 不做其它热键的弹窗改造; 本卡只覆盖按 `s` 的这条路径.",
    "SIZE": "small",
    "TASK_GROUP": "",
    "USER_DECISIONS": "- 按 `s` 立即弹出对话框, 不等读取.\n- 读取放到后台, 并且只读选中的那张卡, 不读其它任何卡.\n- 读取完成后更新对话框内容.\n- 对话框样式参考选项面板 (options), 要有适当留白的空行."
  },
  "After": {
    "ACCEPTANCE_CRITERIA": "- [ ] 按 `s` 后, 处理该按键的同一次 Update 内对话框已置为读取中态, 且该路径不调用任何读盘接口 (用注入或计数断言证明).\n- [ ] 读取通过后台通道执行, 期间 TUI 可继续处理按键与刷新.\n- [ ] 后台读取只读目标卡: 有用例证明未读取其它卡片正文.\n- [ ] 读取结果到达后对话框内容被更新为 Agent, launcher 与 backlog 提示, 底部提示行同步切换.\n- [ ] 读取中按确认键不启动任务且给出提示; 取消键可关闭对话框, 取消路径零副作用.\n- [ ] 对话框已关闭时, 迟到结果被丢弃; 用户换选另一张卡并再次按 `s` 时, 旧卡结果不覆盖新卡对话框 (按任务 ID 加请求序号识别).\n- [ ] 读取失败与不可启动 (状态已变, foreground/console) 时关闭对话框并在页脚显示原因; 这些判定的结果与改动前一致.\n- [ ] 渲染用例断言: 标题, 段落之间的空行, 底部提示行的 dim 样式; 窄终端下的降级顺序为先压底部空行再压正文.\n- [ ] `launch.Start` 的调用参数, 认领, 回滚与成功/失败提示语义未改变, 有用例证明未回归.\n- [ ] 确认后异步启动且对话框保留并提示正在启动；启动中按键不关闭、不重复启动；成功或失败及警告原地显示，结果态任意键关闭，覆盖相应用例。\n- [ ] 新增文案在 en / zh-CN / ja 三份 locale 中键一致, `go test ./internal/i18n` 通过.\n- [ ] 在模块根运行 `go test ./...` 通过, 记录命令, 提交号与用例数.",
    "EXPECTED_OUTCOME": "- 按 `s` 的当帧就弹出对话框, 处于「读取中」态: 显示任务 ID 与状态判断结果, Agent 与 launcher 位置为占位文案, 底部提示行说明正在读取. 按键路径上不发生任何读盘.\n- 读取在后台进行 (走既有的 `pendingWork` 通道), 期间看板照常响应按键, 滚动与定时刷新.\n- 后台只读选中卡: 沿用 targeted 的 `board.ReadSnapshot(root, id)`, 不读其它卡片正文.\n- 读取完成后原地更新对话框: 填入 Agent, launcher, backlog 迁移提示, 底部提示行换成确认/取消说明.\n- 读取中按确认键不启动任务, 只给出仍在读取的提示; 取消键 (Esc 或其它键) 随时可关闭对话框.\n- 对话框关闭后, 或用户已经换选另一张卡后, 迟到的读取结果被丢弃, 不覆盖当前界面, 不误启动.\n- 读取失败, 或结果表明该卡不可启动 (状态已变, launcher 解析为 foreground/console) 时, 关闭对话框并把原因显示在页脚, 与改动前的判定结果一致.\n- 对话框排版对齐选项面板: 同一套弹窗边框与配色, 标题在框上, 正文按段落用空行分隔 (任务 ID / Agent 与 launcher / backlog 提示), 底部空一行后是 dim 样式的操作提示行; 窄终端下先压缩底部空行, 再压缩正文, 不做横向截断.\n- 确认后对话框进入正在启动态；后台启动完成后进入结果态，成功或失败原因及警告都在框内显示，结果态按任意键关闭。启动中按键不关闭、不重复启动。\n- 启动本身的行为不变: 仍走 `launch.Start`, 认领, 回滚与结果提示语义与改动前一致.",
    "GOAL": "去掉看板里按 `s` 之后到确认对话框出现之间的卡顿, 并把对话框排版对齐选项面板.\n\n现状: `confirmSelectedStart` (`internal/tui/start.go:56-80`) 在按键处理里同步调用 `PrepareStart` -\u003e `launch.PreviewStart` -\u003e `board.ReadSnapshot`, 读盘返回后才把 `a.StartConfirmation` 置上, 下一帧才画对话框. 这段同步读盘发生在 Bubble Tea 的 Update 循环里, 期间整个 TUI 不响应输入; 在当前看板上实测约 1 秒. 对话框正文 (`renderStartConfirmation`, `internal/tui/start.go:131-144`) 是几行紧挨着的文本, 没有段落留白, 与选项面板的弹窗观感不一致.",
    "OUT_OF_SCOPE": "- 既有问题: 不改 `launch.Start` / `launch.PreviewStart` 的语义与返回内容, 不改 backlog -\u003e todo 的受控迁移, 不改启动失败的回滚; 本卡改触发时机、并发方式、排版与用户补充的启动进度/结果对话框。\n- 加固: 不加启动的额外授权或并发互斥; 认领互斥由既有移动原语保证.\n- 共享契约与文档: 不改卡片格式, 配置 schema 与 `rules/*.md`; 仅在 README / 帮助文案确有描述变化时同步, 不顺手重写其它界面文案.\n- 相邻功能: 不做 `.kander/operations` 日志读取的优化 (那是 `20260908-journal-pending-partition-task` 的范围), 不把定时刷新也改成异步, 不做其它热键的弹窗改造; 本卡只覆盖按 `s` 的这条路径.",
    "SIZE": "small",
    "TASK_GROUP": "",
    "USER_DECISIONS": "- 2026-09-08 补充：启动操作保持异步；确认后保留对话框并提示正在启动，成功或失败均在对话框更新结果，结果出现后用户按任意键关闭。启动中按键不关闭、不重复启动。\n\n- 按 `s` 立即弹出对话框, 不等读取.\n- 读取放到后台, 并且只读选中的那张卡, 不读其它任何卡.\n- 读取完成后更新对话框内容.\n- 对话框样式参考选项面板 (options), 要有适当留白的空行."
  }
}
```
